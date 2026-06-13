package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
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
	return nil, fmt.Errorf("wise CreatePayment not implemented")
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

var _ payment.Provider = (*Wise)(nil)
