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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

const (
	wiseDefaultAPIBase  = "https://api.wise.com"
	wiseDefaultStrategy = "exact_only"
	wiseHTTPTimeout     = 15 * time.Second

	wiseMaxResponseSize      = 1 << 20
	wiseMaxErrorSummaryBytes = 512
	wiseStatementLookback    = 72 * time.Hour
	wiseStatementLookahead   = 5 * time.Minute
	wiseMaxStatementLookback = 720 * time.Hour
)

type Wise struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
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
	if strings.TrimSpace(cfg["webhookPublicKey"]) == "" {
		return nil, fmt.Errorf("wise config missing required key: webhookPublicKey")
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
	if _, err := parseWiseWebhookPublicKey(cfg["webhookPublicKey"]); err != nil {
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
	if err := normalizeWiseReconcileWindowConfig(cfg); err != nil {
		return nil, err
	}
	return &Wise{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: wiseHTTPTimeout},
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

	transactions, err := w.queryStatement(ctx)
	if err != nil {
		return nil, fmt.Errorf("wise query order: %w", err)
	}

	return w.queryOrderFromTransactions(outTradeNo, transactions), nil
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

	transactions, err := w.queryStatement(ctx)
	if err != nil {
		return nil, fmt.Errorf("wise query orders: %w", err)
	}

	responses := make(map[string]*payment.QueryOrderResponse, len(normalized))
	for _, outTradeNo := range normalized {
		responses[outTradeNo] = w.queryOrderFromTransactions(outTradeNo, transactions)
	}
	return responses, nil
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
	var event wiseWebhookEvent
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

type wiseWebhookEvent struct {
	EventType string `json:"event_type"`
	Data      struct {
		Resource struct {
			ID        string `json:"id"`
			ProfileID string `json:"profile_id"`
			AccountID string `json:"account_id"`
			Type      string `json:"type"`
		} `json:"resource"`
	} `json:"data"`
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
	q.Set("description", outTradeNo)
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
	ID          string
	ProfileID   string
	BalanceID   string
	Direction   string
	Status      string
	Currency    string
	GrossAmount decimal.Decimal
	FeeAmount   decimal.Decimal
	NetAmount   decimal.Decimal
	Description string
	Reference   string
	OccurredAt  time.Time
	Raw         json.RawMessage
}

type wiseStatementResponse struct {
	Transactions []wiseStatementTransaction `json:"transactions"`
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

func (w *Wise) doJSON(ctx context.Context, method, path string, out any) error {
	baseURL := strings.TrimRight(w.config["apiBase"], "/")
	if baseURL == "" {
		baseURL = wiseDefaultAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("wise build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(w.config["apiToken"]))
	req.Header.Set("Accept", "application/json")

	client := w.httpClient
	if client == nil {
		client = &http.Client{Timeout: wiseHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("wise request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, wiseMaxResponseSize))
	if err != nil {
		return fmt.Errorf("wise read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("wise HTTP %d: %s", resp.StatusCode, summarizeWiseResponse(body))
	}
	if len(strings.TrimSpace(string(body))) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("wise parse response: %w", err)
	}
	return nil
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
	now := time.Now().UTC()
	profileID := strings.TrimSpace(w.config["profileId"])
	balanceID := strings.TrimSpace(w.config["balanceId"])
	query := url.Values{}
	query.Set("intervalStart", now.Add(-w.statementLookback()).Format(time.RFC3339))
	query.Set("intervalEnd", now.Add(wiseStatementLookahead).Format(time.RFC3339))
	query.Set("type", "COMPACT")
	query.Set("currency", w.currency())
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
	return transactions, nil
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
	return wiseTextReferencesOrderID(tx.Description, outTradeNo) || wiseTextReferencesOrderID(tx.Reference, outTradeNo)
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
