package provider

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

const (
	wiseDefaultAPIBase  = "https://api.wise.com"
	wiseDefaultStrategy = "exact_only"
	wiseHTTPTimeout     = 15 * time.Second
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
	if _, err := payment.AmountToMinorUnit(amount, w.currency()); err != nil {
		return nil, fmt.Errorf("wise create payment: invalid amount: %w", err)
	}
	payURL, err := w.quickPayURL(amount, orderID)
	if err != nil {
		return nil, err
	}
	return &payment.CreatePaymentResponse{
		TradeNo:    orderID,
		PayURL:     payURL,
		Currency:   w.currency(),
		ResultType: payment.CreatePaymentResultOrderCreated,
	}, nil
}

func (w *Wise) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	return nil, fmt.Errorf("wise QueryOrder not implemented")
}

func (w *Wise) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	return nil, fmt.Errorf("wise VerifyNotification not implemented")
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
	if !tx.NetAmount.Equal(order.PayAmount) {
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
	return strings.Contains(tx.Description, outTradeNo) || strings.Contains(tx.Reference, outTradeNo)
}

var _ payment.Provider = (*Wise)(nil)
