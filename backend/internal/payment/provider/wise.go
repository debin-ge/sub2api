package provider

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
	"golang.org/x/time/rate"
)

const (
	wiseDefaultAPIBase  = "https://api.wise.com"
	wiseDefaultStrategy = "exact_only"
	wiseHTTPTimeout     = 15 * time.Second

	wiseMaxResponseSize            = 1 << 20
	wiseMaxErrorSummaryBytes       = 512
	wiseStatementLookback          = 72 * time.Hour
	wiseStatementLookahead         = 5 * time.Minute
	wiseOrderCreatedAtQueryPadding = 10 * time.Minute
	wiseMaxStatementLookback       = 720 * time.Hour

	wiseActivityPageSize            = 100
	wiseMaxAllowedMethodsNoteLength = 500

	wiseHTTPMaxAttempts        = 3
	wiseHTTPRetryInitialDelay  = 200 * time.Millisecond
	wiseHTTPRetryMaxDelay      = 3 * time.Second
	wiseHTTPRetryJitterRatio   = 0.5
	wiseDefaultAPIRateLimitRPS = 1.0
	wiseDefaultAPIRateBurst    = 2
)

type wiseOrderCreatedAtContextKey struct{}
type wiseQueryNowContextKey struct{}
type wiseQueryCacheContextKey struct{}

var wiseLimiterCache sync.Map

func WithWiseOrderCreatedAt(ctx context.Context, createdAt time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if createdAt.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, wiseOrderCreatedAtContextKey{}, createdAt.UTC())
}

func WiseOrderCreatedAtFromContext(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	createdAt, ok := ctx.Value(wiseOrderCreatedAtContextKey{}).(time.Time)
	if !ok || createdAt.IsZero() {
		return time.Time{}, false
	}
	return createdAt.UTC(), true
}

type WiseQueryCache struct {
	mu         sync.Mutex
	statements map[string][]wiseTransaction
	activities map[string][]wiseTransaction
}

func NewWiseQueryCache() *WiseQueryCache {
	return &WiseQueryCache{
		statements: make(map[string][]wiseTransaction),
		activities: make(map[string][]wiseTransaction),
	}
}

func WithWiseQueryCache(ctx context.Context, cache *WiseQueryCache) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if cache == nil {
		return ctx
	}
	return context.WithValue(ctx, wiseQueryCacheContextKey{}, cache)
}

func WithWiseQueryNow(ctx context.Context, now time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, wiseQueryNowContextKey{}, now.UTC())
}

func wiseQueryCacheFromContext(ctx context.Context) *WiseQueryCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(wiseQueryCacheContextKey{}).(*WiseQueryCache)
	return cache
}

func wiseQueryNowFromContext(ctx context.Context) time.Time {
	if ctx != nil {
		if now, ok := ctx.Value(wiseQueryNowContextKey{}).(time.Time); ok && !now.IsZero() {
			return now.UTC()
		}
	}
	return time.Now().UTC()
}

func (c *WiseQueryCache) getStatement(key string) ([]wiseTransaction, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	transactions, ok := c.statements[key]
	return cloneWiseTransactions(transactions), ok
}

func (c *WiseQueryCache) setStatement(key string, transactions []wiseTransaction) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements[key] = cloneWiseTransactions(transactions)
}

func (c *WiseQueryCache) getActivities(key string) ([]wiseTransaction, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	transactions, ok := c.activities[key]
	return cloneWiseTransactions(transactions), ok
}

func (c *WiseQueryCache) setActivities(key string, transactions []wiseTransaction) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activities[key] = cloneWiseTransactions(transactions)
}

func cloneWiseTransactions(transactions []wiseTransaction) []wiseTransaction {
	if transactions == nil {
		return nil
	}
	return append([]wiseTransaction(nil), transactions...)
}

type Wise struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
	limiter    *rate.Limiter
}

func NewWise(instanceID string, config map[string]string) (*Wise, error) {
	cfg := cloneStringMap(config)
	if strings.TrimSpace(cfg["quickPayBaseUrl"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: quickPayBaseUrl")
	}
	if strings.TrimSpace(cfg["apiToken"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: apiToken")
	}
	if strings.TrimSpace(cfg["profileId"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: profileId")
	}
	if strings.TrimSpace(cfg["balanceId"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: balanceId")
	}
	if strings.TrimSpace(cfg["currency"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: currency")
	}
	if strings.TrimSpace(cfg["apiBase"]) == "" {
		cfg["apiBase"] = wiseDefaultAPIBase
	}
	quickPayURL, err := normalizeWiseHTTPSURL(cfg["quickPayBaseUrl"], "quickPayBaseUrl")
	if err != nil {
		return nil, err
	}
	cfg["quickPayBaseUrl"] = quickPayURL
	apiBase, err := normalizeWiseHTTPSURL(cfg["apiBase"], "apiBase")
	if err != nil {
		return nil, err
	}
	cfg["apiBase"] = strings.TrimRight(apiBase, "/")
	if err := normalizeWiseWebhookConfig(cfg); err != nil {
		return nil, err
	}
	currency, err := payment.NormalizePaymentCurrency(cfg["currency"])
	if err != nil {
		return nil, fmt.Errorf("wise config currency: %w", err)
	}
	cfg["currency"] = currency
	strategy := strings.TrimSpace(cfg["settlementStrategy"])
	if strategy == "" {
		strategy = wiseDefaultStrategy
	}
	if strategy != wiseDefaultStrategy {
		return nil, fmt.Errorf("wise settlementStrategy must be exact_only")
	}
	cfg["settlementStrategy"] = strategy
	if strings.EqualFold(strings.TrimSpace(cfg["autoFulfillFeePayments"]), "true") {
		return nil, fmt.Errorf("wise autoFulfillFeePayments is not supported in v1")
	}
	note := strings.TrimSpace(cfg["allowedMethodsNote"])
	if len([]rune(note)) > wiseMaxAllowedMethodsNoteLength {
		return nil, fmt.Errorf("wise allowedMethodsNote must be <= %d characters", wiseMaxAllowedMethodsNoteLength)
	}
	cfg["allowedMethodsNote"] = note
	if err := normalizeWiseReconcileWindowConfig(cfg); err != nil {
		return nil, err
	}
	return &Wise{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: wiseHTTPTimeout},
		limiter:    sharedWiseRateLimiter(instanceID, cfg),
	}, nil
}

func (w *Wise) Name() string        { return "Wise" }
func (w *Wise) ProviderKey() string { return payment.TypeWise }
func (w *Wise) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeWise}
}

func (w *Wise) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	_ = ctx
	orderID := strings.TrimSpace(req.OrderID)
	if orderID == "" {
		return nil, fmt.Errorf("wise create payment: missing order id")
	}
	amount := strings.TrimSpace(req.Amount)
	if amount == "" {
		return nil, fmt.Errorf("wise create payment: missing amount")
	}
	currency := w.currency()
	minorAmount, err := payment.AmountToMinorUnit(amount, currency)
	if err != nil {
		return nil, fmt.Errorf("wise create payment: invalid amount: %w", err)
	}
	if minorAmount <= 0 {
		return nil, fmt.Errorf("wise create payment: amount must be positive")
	}
	payURL, err := w.quickPayURL(amount, orderID)
	if err != nil {
		return nil, err
	}
	return &payment.CreatePaymentResponse{
		TradeNo:    orderID,
		PayURL:     payURL,
		Currency:   currency,
		ResultType: payment.CreatePaymentResultOrderCreated,
	}, nil
}

func (w *Wise) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	outTradeNo := strings.TrimSpace(tradeNo)
	if outTradeNo == "" {
		return nil, fmt.Errorf("wise query order: missing order reference")
	}

	transactions, statementErr := w.queryStatement(ctx)
	var statementResp *payment.QueryOrderResponse
	if statementErr == nil {
		statementResp = w.queryOrderFromTransactions(outTradeNo, transactions)
		if !wiseQueryResponseIsNoMatch(statementResp) {
			return statementResp, nil
		}
	}

	activityTransactions, activityErr := w.queryActivities(ctx)
	if activityErr != nil {
		if statementErr != nil {
			return nil, fmt.Errorf("wise query order: statement: %v; activities: %w", statementErr, activityErr)
		}
		return statementResp, nil
	}
	activityResp := w.queryOrderFromTransactions(outTradeNo, activityTransactions)
	if !wiseQueryResponseIsNoMatch(activityResp) {
		return activityResp, nil
	}
	if statementResp != nil {
		return statementResp, nil
	}
	return activityResp, nil
}

func (w *Wise) QueryOrders(ctx context.Context, tradeNos []string) (map[string]*payment.QueryOrderResponse, error) {
	normalized := make([]string, 0, len(tradeNos))
	seen := make(map[string]struct{}, len(tradeNos))
	for _, tradeNo := range tradeNos {
		outTradeNo := strings.TrimSpace(tradeNo)
		if outTradeNo == "" {
			continue
		}
		if _, ok := seen[outTradeNo]; ok {
			continue
		}
		seen[outTradeNo] = struct{}{}
		normalized = append(normalized, outTradeNo)
	}
	if len(normalized) == 0 {
		return map[string]*payment.QueryOrderResponse{}, nil
	}

	responses := make(map[string]*payment.QueryOrderResponse, len(normalized))
	needsActivity := make([]string, 0, len(normalized))
	transactions, statementErr := w.queryStatement(ctx)
	if statementErr == nil {
		for _, outTradeNo := range normalized {
			resp := w.queryOrderFromTransactions(outTradeNo, transactions)
			responses[outTradeNo] = resp
			if wiseQueryResponseIsNoMatch(resp) {
				needsActivity = append(needsActivity, outTradeNo)
			}
		}
	} else {
		needsActivity = append(needsActivity, normalized...)
	}

	if len(needsActivity) > 0 {
		activityTransactions, activityErr := w.queryActivities(ctx)
		if activityErr != nil {
			if statementErr != nil {
				return nil, fmt.Errorf("wise query orders: statement: %v; activities: %w", statementErr, activityErr)
			}
			return responses, nil
		}
		for _, outTradeNo := range needsActivity {
			resp := w.queryOrderFromTransactions(outTradeNo, activityTransactions)
			if statementErr != nil || !wiseQueryResponseIsNoMatch(resp) {
				responses[outTradeNo] = resp
			}
		}
	}
	return responses, nil
}

func wiseQueryResponseIsNoMatch(resp *payment.QueryOrderResponse) bool {
	if resp == nil {
		return true
	}
	return resp.Status == payment.ProviderStatusPending &&
		strings.TrimSpace(resp.Metadata["reconcile_decision"]) == "no_match"
}

func (w *Wise) queryOrderFromTransactions(outTradeNo string, transactions []wiseTransaction) *payment.QueryOrderResponse {
	strategy := wiseExactSettlementStrategy{}
	order := wiseOrderContext{
		OutTradeNo: outTradeNo,
		Currency:   w.currency(),
		ProfileID:  strings.TrimSpace(w.config["profileId"]),
		BalanceID:  strings.TrimSpace(w.config["balanceId"]),
	}
	var rejected *payment.QueryOrderResponse
	for _, tx := range transactions {
		if !wiseTransactionReferencesOrder(tx, outTradeNo) {
			continue
		}
		decision := strategy.Match(order, tx)
		if !decision.Matched {
			if rejected == nil {
				rejected = wisePendingQueryResponse(outTradeNo, tx, decision)
			}
			continue
		}

		metadata := transactionMetadata(tx, decision)
		paidAt := ""
		if !tx.OccurredAt.IsZero() {
			paidAt = tx.OccurredAt.UTC().Format(time.RFC3339)
		}
		status := payment.ProviderStatusPending
		if decision.AutoFulfill {
			status = payment.ProviderStatusPaid
		}
		tradeNo := strings.TrimSpace(tx.ID)
		if tradeNo == "" {
			tradeNo = outTradeNo
		}
		return &payment.QueryOrderResponse{
			TradeNo:  tradeNo,
			Status:   status,
			Amount:   tx.NetAmount.InexactFloat64(),
			PaidAt:   paidAt,
			Metadata: metadata,
		}
	}
	if rejected != nil {
		return rejected
	}

	return &payment.QueryOrderResponse{
		TradeNo: outTradeNo,
		Status:  payment.ProviderStatusPending,
		Metadata: map[string]string{
			"profile_id":          strings.TrimSpace(w.config["profileId"]),
			"balance_id":          strings.TrimSpace(w.config["balanceId"]),
			"settlement_strategy": strategy.Name(),
			"reconcile_decision":  "no_match",
			"reconcile_reason":    "no_matching_transaction",
		},
	}
}

func wisePendingQueryResponse(outTradeNo string, tx wiseTransaction, decision wiseSettlementDecision) *payment.QueryOrderResponse {
	metadata := transactionMetadata(tx, decision)
	tradeNo := strings.TrimSpace(tx.ID)
	if tradeNo == "" {
		tradeNo = outTradeNo
	}
	return &payment.QueryOrderResponse{
		TradeNo:  tradeNo,
		Status:   payment.ProviderStatusPending,
		Amount:   tx.NetAmount.InexactFloat64(),
		Metadata: metadata,
	}
}

func (w *Wise) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	_ = ctx
	if err := verifyWiseWebhookSignature(rawBody, headers, w.config["webhookPublicKey"]); err != nil {
		return nil, err
	}
	var event wiseWebhookEnvelope
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, fmt.Errorf("wise parse webhook: %w", err)
	}
	if !wiseWebhookEventSupported(event.EventType) {
		return nil, nil
	}
	return nil, nil
}

func (w *Wise) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("wise refund is not supported")
}

func normalizeWiseHTTPSURL(raw, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("wise %s is required", fieldName)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("wise %s must be an HTTPS URL", fieldName)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func parseWiseWebhookPublicKey(raw string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, fmt.Errorf("wise webhookPublicKey must be PEM encoded")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("wise webhookPublicKey parse failed: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("wise webhookPublicKey must be an RSA public key")
	}
	return rsaPub, nil
}

type wiseWebhookEnvelope struct {
	EventType string `json:"event_type"`
}

func wiseWebhookEventSupported(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "balances#credit", "balances#update", "account-details-payment#state-change":
		return true
	default:
		return false
	}
}

func verifyWiseWebhookSignature(rawBody string, headers map[string]string, publicKeyPEM string) error {
	header := strings.TrimSpace(headers["x-signature-sha256"])
	if header == "" {
		return fmt.Errorf("wise notification missing x-signature-sha256 header")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return fmt.Errorf("wise invalid signature")
	}
	publicKey, err := parseWiseWebhookPublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(rawBody))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signatureBytes); err != nil {
		return fmt.Errorf("wise invalid signature")
	}
	return nil
}

func wiseWebhookDeliveryID(headers map[string]string) string {
	return strings.TrimSpace(wiseWebhookHeader(headers, "x-delivery-id"))
}

func wiseWebhookIsTestNotification(headers map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(wiseWebhookHeader(headers, "x-test-notification")), "true")
}

func wiseWebhookHeader(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	if value, ok := headers[name]; ok {
		return value
	}
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return value
		}
	}
	return ""
}

func (w *Wise) currency() string {
	if w == nil {
		return payment.DefaultPaymentCurrency
	}
	currency, err := payment.NormalizePaymentCurrency(w.config["currency"])
	if err != nil {
		return payment.DefaultPaymentCurrency
	}
	return currency
}

func normalizeWiseReconcileWindowConfig(cfg map[string]string) error {
	raw := strings.TrimSpace(cfg["reconcileWindowHours"])
	if raw == "" {
		cfg["reconcileWindowHours"] = strconv.Itoa(int(wiseStatementLookback / time.Hour))
		return nil
	}
	hours, err := strconv.Atoi(raw)
	if err != nil || hours <= 0 {
		return fmt.Errorf("wise reconcileWindowHours must be a positive integer")
	}
	maxHours := int(wiseMaxStatementLookback / time.Hour)
	if hours > maxHours {
		return fmt.Errorf("wise reconcileWindowHours must be <= %d", int(wiseMaxStatementLookback/time.Hour))
	}
	cfg["reconcileWindowHours"] = strconv.Itoa(hours)
	return nil
}

func (w *Wise) statementLookback() time.Duration {
	if w == nil {
		return wiseStatementLookback
	}
	hours, err := strconv.Atoi(strings.TrimSpace(w.config["reconcileWindowHours"]))
	if err != nil || hours <= 0 {
		return wiseStatementLookback
	}
	maxHours := int(wiseMaxStatementLookback / time.Hour)
	if hours > maxHours {
		return wiseMaxStatementLookback
	}
	return time.Duration(hours) * time.Hour
}

func (w *Wise) quickPayURL(amount, outTradeNo string) (string, error) {
	parsed, err := url.Parse(w.config["quickPayBaseUrl"])
	if err != nil {
		return "", fmt.Errorf("wise quickPayBaseUrl parse failed: %w", err)
	}
	q := parsed.Query()
	q.Set("amount", amount)
	q.Set("currency", w.currency())
	q.Set("description", wiseSafePaymentReference(outTradeNo))
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

type wiseOrderContext struct {
	OutTradeNo string
	PayAmount  decimal.Decimal
	Currency   string
	ProfileID  string
	BalanceID  string
}

type wiseTransaction struct {
	ID           string
	ProfileID    string
	BalanceID    string
	Direction    string
	Status       string
	Currency     string
	GrossAmount  decimal.Decimal
	FeeAmount    decimal.Decimal
	NetAmount    decimal.Decimal
	Description  string
	Reference    string
	OccurredAt   time.Time
	RejectReason string
	Metadata     map[string]string
	Raw          json.RawMessage
}

type wiseStatementResponse struct {
	Transactions []wiseStatementTransaction `json:"transactions"`
}

type wiseActivityResponse struct {
	Cursor     string         `json:"cursor"`
	Activities []wiseActivity `json:"activities"`
}

type wiseActivity struct {
	ID              string               `json:"id"`
	Type            string               `json:"type"`
	Resource        wiseActivityResource `json:"resource"`
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	PrimaryAmount   string               `json:"primaryAmount"`
	SecondaryAmount string               `json:"secondaryAmount"`
	Status          string               `json:"status"`
	CreatedOn       string               `json:"createdOn"`
	UpdatedOn       string               `json:"updatedOn"`
}

type wiseActivityResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type wiseStatementTransaction struct {
	Type            string          `json:"type"`
	Date            string          `json:"date"`
	ReferenceNumber string          `json:"referenceNumber"`
	Amount          wiseMoneyAmount `json:"amount"`
	TotalFees       wiseMoneyAmount `json:"totalFees"`
	Details         struct {
		Type             string `json:"type"`
		Description      string `json:"description"`
		PaymentReference string `json:"paymentReference"`
		Reference        string `json:"reference"`
	} `json:"details"`
}

type wiseMoneyAmount struct {
	Value    decimal.Decimal `json:"value"`
	Currency string          `json:"currency"`
}

type wiseSettlementDecision struct {
	Matched     bool
	AutoFulfill bool
	GrossAmount decimal.Decimal
	FeeAmount   decimal.Decimal
	NetAmount   decimal.Decimal
	Reason      string
	Metadata    map[string]string
}

type wiseSettlementStrategy interface {
	Name() string
	Match(order wiseOrderContext, tx wiseTransaction) wiseSettlementDecision
}

type wiseExactSettlementStrategy struct{}

func (wiseExactSettlementStrategy) Name() string { return wiseDefaultStrategy }

func (wiseExactSettlementStrategy) Match(order wiseOrderContext, tx wiseTransaction) wiseSettlementDecision {
	if reason := strings.TrimSpace(tx.RejectReason); reason != "" {
		return wiseSettlementDecision{Matched: false, Reason: reason}
	}
	if !strings.EqualFold(strings.TrimSpace(tx.ProfileID), strings.TrimSpace(order.ProfileID)) {
		return wiseSettlementDecision{Matched: false, Reason: "profile_mismatch"}
	}
	if !strings.EqualFold(strings.TrimSpace(tx.BalanceID), strings.TrimSpace(order.BalanceID)) {
		return wiseSettlementDecision{Matched: false, Reason: "balance_mismatch"}
	}
	if !strings.EqualFold(strings.TrimSpace(tx.Currency), strings.TrimSpace(order.Currency)) {
		return wiseSettlementDecision{Matched: false, Reason: "currency_mismatch"}
	}
	if !strings.EqualFold(strings.TrimSpace(tx.Direction), "credit") {
		return wiseSettlementDecision{Matched: false, Reason: "direction_not_credit"}
	}
	if !wiseTransactionStatusCompleted(tx.Status) {
		return wiseSettlementDecision{Matched: false, Reason: "status_not_completed"}
	}
	if !wiseTransactionReferencesOrder(tx, order.OutTradeNo) {
		return wiseSettlementDecision{Matched: false, Reason: "reference_mismatch"}
	}
	if !order.PayAmount.IsZero() && !tx.NetAmount.Equal(order.PayAmount) {
		return wiseSettlementDecision{
			Matched:     true,
			AutoFulfill: false,
			GrossAmount: tx.GrossAmount,
			FeeAmount:   tx.FeeAmount,
			NetAmount:   tx.NetAmount,
			Reason:      "amount_mismatch",
		}
	}
	gross := tx.GrossAmount
	if gross.IsZero() {
		gross = tx.NetAmount
	}
	return wiseSettlementDecision{
		Matched:     true,
		AutoFulfill: true,
		GrossAmount: gross,
		FeeAmount:   tx.FeeAmount,
		NetAmount:   tx.NetAmount,
		Reason:      "exact_match",
	}
}

type wiseRateLimiterEntry struct {
	rps     float64
	burst   int
	limiter *rate.Limiter
}

func sharedWiseRateLimiter(instanceID string, cfg map[string]string) *rate.Limiter {
	rps := wiseDefaultAPIRateLimitRPS
	if parsed, err := strconv.ParseFloat(strings.TrimSpace(cfg["apiRateLimitRPS"]), 64); err == nil && parsed > 0 {
		rps = parsed
	}
	burst := wiseDefaultAPIRateBurst
	if parsed, err := strconv.Atoi(strings.TrimSpace(cfg["apiRateLimitBurst"])); err == nil && parsed > 0 {
		burst = parsed
	}
	key := strings.TrimSpace(instanceID)
	if key == "" {
		key = "_default"
	}
	if value, ok := wiseLimiterCache.Load(key); ok {
		entry, ok := value.(*wiseRateLimiterEntry)
		if ok && entry.rps == rps && entry.burst == burst && entry.limiter != nil {
			return entry.limiter
		}
	}
	entry := &wiseRateLimiterEntry{
		rps:     rps,
		burst:   burst,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
	wiseLimiterCache.Store(key, entry)
	return entry.limiter
}

type wiseHTTPError struct {
	StatusCode    int
	Summary       string
	RetryAfter    time.Duration
	HasRetryAfter bool
}

func (e *wiseHTTPError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("wise HTTP %d: %s", e.StatusCode, e.Summary)
}

func (w *Wise) doJSON(ctx context.Context, method, path string, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL := strings.TrimRight(w.config["apiBase"], "/")
	if baseURL == "" {
		baseURL = wiseDefaultAPIBase
	}

	client := w.httpClient
	if client == nil {
		client = &http.Client{Timeout: wiseHTTPTimeout}
	}

	for attempt := 0; attempt < wiseHTTPMaxAttempts; attempt++ {
		if w.limiter != nil {
			if err := w.limiter.Wait(ctx); err != nil {
				return fmt.Errorf("wise rate limit wait: %w", err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
		if err != nil {
			return fmt.Errorf("wise build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(w.config["apiToken"]))
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			if attempt < wiseHTTPMaxAttempts-1 && wiseRequestErrorIsRetryable(ctx, err) {
				if waitErr := waitWiseRetryDelay(ctx, wiseRetryDelay(attempt, 0, false)); waitErr != nil {
					return waitErr
				}
				continue
			}
			return fmt.Errorf("wise request: %w", err)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, wiseMaxResponseSize))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("wise read response: %w", readErr)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			retryAfter, hasRetryAfter := wiseParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			httpErr := &wiseHTTPError{
				StatusCode:    resp.StatusCode,
				Summary:       summarizeWiseResponse(body),
				RetryAfter:    retryAfter,
				HasRetryAfter: hasRetryAfter,
			}
			if attempt < wiseHTTPMaxAttempts-1 && wiseHTTPStatusIsRetryable(resp.StatusCode) {
				if waitErr := waitWiseRetryDelay(ctx, wiseRetryDelay(attempt, httpErr.RetryAfter, httpErr.HasRetryAfter)); waitErr != nil {
					return waitErr
				}
				continue
			}
			return httpErr
		}
		if len(strings.TrimSpace(string(body))) == 0 || out == nil {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("wise parse response: %w", err)
		}
		return nil
	}
	return fmt.Errorf("wise request retry exhausted")
}

func wiseHTTPStatusIsRetryable(statusCode int) bool {
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout {
		return true
	}
	return statusCode >= http.StatusInternalServerError && statusCode <= 599
}

func wiseRequestErrorIsRetryable(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func wiseRetryDelay(attempt int, retryAfter time.Duration, hasRetryAfter bool) time.Duration {
	if hasRetryAfter {
		if retryAfter < 0 {
			return 0
		}
		if retryAfter > wiseHTTPRetryMaxDelay {
			return wiseHTTPRetryMaxDelay
		}
		return retryAfter
	}
	delay := wiseHTTPRetryInitialDelay
	if attempt > 0 {
		delay *= time.Duration(1 << attempt)
	}
	if delay > wiseHTTPRetryMaxDelay {
		delay = wiseHTTPRetryMaxDelay
	}
	jitter := time.Duration(float64(delay) * wiseHTTPRetryJitterRatio)
	if jitter <= 0 {
		return delay
	}
	delta := time.Duration(rand.Int64N(int64(2*jitter)+1)) - jitter
	delay += delta
	if delay < 0 {
		return 0
	}
	return delay
}

func waitWiseRetryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func wiseParseRetryAfter(raw string, now time.Time) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds < 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	parsed, err := http.ParseTime(raw)
	if err != nil {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return parsed.Sub(now), true
}

func summarizeWiseResponse(body []byte) string {
	if len(body) > wiseMaxErrorSummaryBytes {
		body = body[:wiseMaxErrorSummaryBytes]
	}
	summary := strings.TrimSpace(string(body))
	if summary == "" {
		return "empty response"
	}
	return summary
}

func (w *Wise) queryStatement(ctx context.Context) ([]wiseTransaction, error) {
	now := wiseQueryNowFromContext(ctx)
	profileID := strings.TrimSpace(w.config["profileId"])
	balanceID := strings.TrimSpace(w.config["balanceId"])
	intervalStart := wiseQueryWindowStart(ctx, now, w.statementLookback())
	intervalEnd := now.Add(wiseStatementLookahead)
	query := url.Values{}
	query.Set("intervalStart", intervalStart.Format(time.RFC3339))
	query.Set("intervalEnd", intervalEnd.Format(time.RFC3339))
	query.Set("type", "COMPACT")
	query.Set("currency", w.currency())
	cacheKey := strings.Join([]string{
		"statement",
		profileID,
		balanceID,
		w.currency(),
		intervalStart.Format(time.RFC3339),
		intervalEnd.Format(time.RFC3339),
	}, ":")
	if cache := wiseQueryCacheFromContext(ctx); cache != nil {
		if transactions, ok := cache.getStatement(cacheKey); ok {
			return transactions, nil
		}
	}
	path := fmt.Sprintf(
		"/v1/profiles/%s/balance-statements/%s/statement.json?%s",
		url.PathEscape(profileID),
		url.PathEscape(balanceID),
		query.Encode(),
	)

	var statement wiseStatementResponse
	if err := w.doJSON(ctx, http.MethodGet, path, &statement); err != nil {
		return nil, err
	}

	transactions := make([]wiseTransaction, 0, len(statement.Transactions))
	for _, row := range statement.Transactions {
		transactions = append(transactions, w.normalizeStatementTransaction(row, profileID, balanceID))
	}
	if cache := wiseQueryCacheFromContext(ctx); cache != nil {
		cache.setStatement(cacheKey, transactions)
	}
	return transactions, nil
}

func (w *Wise) queryActivities(ctx context.Context) ([]wiseTransaction, error) {
	now := wiseQueryNowFromContext(ctx)
	profileID := strings.TrimSpace(w.config["profileId"])
	since := wiseQueryWindowStart(ctx, now, w.statementLookback())
	until := now.Add(wiseStatementLookahead)
	query := url.Values{}
	query.Set("since", since.Format(time.RFC3339))
	query.Set("until", until.Format(time.RFC3339))
	query.Set("size", strconv.Itoa(wiseActivityPageSize))
	query.Set("status", "COMPLETED")
	cacheKey := strings.Join([]string{
		"activities",
		profileID,
		w.currency(),
		since.Format(time.RFC3339),
		until.Format(time.RFC3339),
	}, ":")
	if cache := wiseQueryCacheFromContext(ctx); cache != nil {
		if transactions, ok := cache.getActivities(cacheKey); ok {
			return transactions, nil
		}
	}
	path := fmt.Sprintf(
		"/v1/profiles/%s/activities?%s",
		url.PathEscape(profileID),
		query.Encode(),
	)

	var response wiseActivityResponse
	if err := w.doJSON(ctx, http.MethodGet, path, &response); err != nil {
		return nil, err
	}

	transactions := make([]wiseTransaction, 0, len(response.Activities))
	for _, row := range response.Activities {
		transactions = append(transactions, w.normalizeActivity(row, profileID))
	}
	if cache := wiseQueryCacheFromContext(ctx); cache != nil {
		cache.setActivities(cacheKey, transactions)
	}
	return transactions, nil
}

func wiseQueryWindowStart(ctx context.Context, now time.Time, lookback time.Duration) time.Time {
	start := now.UTC().Add(-lookback)
	createdAt, ok := WiseOrderCreatedAtFromContext(ctx)
	if !ok {
		return start
	}
	orderStart := createdAt.UTC().Add(-wiseOrderCreatedAtQueryPadding)
	hardFloor := now.UTC().Add(-wiseMaxStatementLookback)
	if orderStart.Before(hardFloor) {
		return hardFloor
	}
	return orderStart
}

func (w *Wise) normalizeStatementTransaction(row wiseStatementTransaction, profileID, balanceID string) wiseTransaction {
	id := strings.TrimSpace(row.ReferenceNumber)
	if id == "" {
		id = strings.TrimSpace(row.Details.Reference)
	}

	var occurredAt time.Time
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(row.Date)); err == nil {
		occurredAt = parsed
	}

	reference := strings.TrimSpace(row.Details.PaymentReference)
	if reference == "" {
		reference = strings.TrimSpace(row.Details.Reference)
	}

	raw, _ := json.Marshal(row)
	netAmount := row.Amount.Value
	feeAmount := row.TotalFees.Value
	grossAmount := netAmount.Add(feeAmount)
	return wiseTransaction{
		ID:          id,
		ProfileID:   profileID,
		BalanceID:   balanceID,
		Direction:   strings.ToLower(strings.TrimSpace(row.Type)),
		Status:      "completed",
		Currency:    strings.ToUpper(strings.TrimSpace(row.Amount.Currency)),
		GrossAmount: grossAmount,
		FeeAmount:   feeAmount,
		NetAmount:   netAmount,
		Description: strings.TrimSpace(row.Details.Description),
		Reference:   reference,
		OccurredAt:  occurredAt,
		Raw:         raw,
	}
}

func (w *Wise) normalizeActivity(row wiseActivity, profileID string) wiseTransaction {
	id := strings.TrimSpace(row.Resource.ID)
	if id == "" {
		id = strings.TrimSpace(row.ID)
	}
	title := stripWiseActivityMarkup(row.Title)
	description := stripWiseActivityMarkup(row.Description)
	amount, currency, ok := parseWiseActivityPrimaryAmount(row.PrimaryAmount)
	direction := "credit"
	if amount.IsNegative() {
		direction = "debit"
	}

	var occurredAt time.Time
	for _, raw := range []string{row.CreatedOn, row.UpdatedOn} {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw)); err == nil {
			occurredAt = parsed
			break
		}
	}

	raw, _ := json.Marshal(row)
	metadata := map[string]string{}
	if activityID := strings.TrimSpace(row.ID); activityID != "" {
		metadata["activity_id"] = activityID
	}
	if activityType := strings.TrimSpace(row.Type); activityType != "" {
		metadata["activity_type"] = activityType
	}
	if resourceType := strings.TrimSpace(row.Resource.Type); resourceType != "" {
		metadata["activity_resource_type"] = resourceType
	}
	if resourceID := strings.TrimSpace(row.Resource.ID); resourceID != "" {
		metadata["activity_resource_id"] = resourceID
	}
	if title != "" {
		metadata["activity_title"] = title
	}
	if description != "" {
		metadata["activity_description"] = description
	}
	if primaryAmount := strings.TrimSpace(row.PrimaryAmount); primaryAmount != "" {
		metadata["activity_primary_amount"] = primaryAmount
	}

	tx := wiseTransaction{
		ID:          id,
		ProfileID:   profileID,
		Direction:   direction,
		Status:      strings.ToLower(strings.TrimSpace(row.Status)),
		Currency:    currency,
		GrossAmount: amount,
		NetAmount:   amount,
		Description: description,
		Reference:   description,
		OccurredAt:  occurredAt,
		Metadata:    metadata,
		Raw:         raw,
	}
	if !ok {
		tx.RejectReason = "activity_amount_unparseable"
	} else {
		tx.RejectReason = "balance_unverified"
	}
	return tx
}

func parseWiseActivityPrimaryAmount(raw string) (decimal.Decimal, string, bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) != 2 {
		return decimal.Zero, "", false
	}
	amountText := strings.TrimPrefix(parts[0], "+")
	if amountText == "" {
		return decimal.Zero, "", false
	}
	amount, err := decimal.NewFromString(amountText)
	if err != nil {
		return decimal.Zero, "", false
	}
	currency, err := payment.NormalizePaymentCurrency(parts[1])
	if err != nil {
		return decimal.Zero, "", false
	}
	return amount, currency, true
}

func stripWiseActivityMarkup(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	inTag := false
	for _, r := range raw {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func transactionMetadata(tx wiseTransaction, decision wiseSettlementDecision) map[string]string {
	metadata := map[string]string{
		"currency":            tx.Currency,
		"profile_id":          tx.ProfileID,
		"balance_id":          tx.BalanceID,
		"direction":           tx.Direction,
		"transaction_status":  tx.Status,
		"settlement_strategy": wiseDefaultStrategy,
		"reconcile_reason":    decision.Reason,
		"reconcile_decision":  wiseReconcileDecision(decision),
	}
	addDecimalMetadata(metadata, "gross_amount", tx.GrossAmount)
	addDecimalMetadata(metadata, "fee_amount", tx.FeeAmount)
	addDecimalMetadata(metadata, "net_amount", tx.NetAmount)
	if id := strings.TrimSpace(tx.ID); id != "" {
		metadata["wise_transaction_id"] = id
	}
	if description := strings.TrimSpace(tx.Description); description != "" {
		metadata["description"] = description
	}
	if reference := strings.TrimSpace(tx.Reference); reference != "" {
		metadata["reference"] = reference
	}
	if !tx.OccurredAt.IsZero() {
		metadata["occurred_at"] = tx.OccurredAt.UTC().Format(time.RFC3339)
	}
	for key, value := range tx.Metadata {
		if strings.TrimSpace(value) != "" {
			metadata[key] = value
		}
	}
	for key, value := range decision.Metadata {
		if strings.TrimSpace(value) != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func wiseReconcileDecision(decision wiseSettlementDecision) string {
	if decision.AutoFulfill {
		return "auto_fulfill"
	}
	if decision.Matched {
		return "manual_review"
	}
	return "rejected"
}

func addDecimalMetadata(metadata map[string]string, key string, amount decimal.Decimal) {
	if !amount.IsZero() {
		metadata[key] = amount.String()
	}
}

func wiseTransactionStatusCompleted(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "posted", "available":
		return true
	default:
		return false
	}
}

func wiseTransactionReferencesOrder(tx wiseTransaction, outTradeNo string) bool {
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return false
	}
	wiseReference := wiseSafePaymentReference(outTradeNo)
	return wiseTextReferencesOrderID(tx.Description, outTradeNo) ||
		wiseTextReferencesOrderID(tx.Reference, outTradeNo) ||
		(wiseReference != outTradeNo &&
			(wiseTextReferencesOrderID(tx.Description, wiseReference) ||
				wiseTextReferencesOrderID(tx.Reference, wiseReference)))
}

func wiseSafePaymentReference(outTradeNo string) string {
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(outTradeNo))
	for _, ch := range outTradeNo {
		if ch >= 'a' && ch <= 'z' ||
			ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func wiseTextReferencesOrderID(text, outTradeNo string) bool {
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return false
	}
	for start := 0; start < len(text); {
		idx := strings.Index(text[start:], outTradeNo)
		if idx == -1 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !wiseIsOrderIDChar(text[idx-1])
		afterIdx := idx + len(outTradeNo)
		afterOK := afterIdx == len(text) || !wiseIsOrderIDChar(text[afterIdx])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
	return false
}

func wiseIsOrderIDChar(ch byte) bool {
	return ch >= 'a' && ch <= 'z' ||
		ch >= 'A' && ch <= 'Z' ||
		ch >= '0' && ch <= '9' ||
		ch == '_' ||
		ch == '-'
}

var _ payment.Provider = (*Wise)(nil)
