package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

// Stripe constants.
const (
	stripeEventPaymentSuccess = "payment_intent.succeeded"
	stripeEventPaymentFailed  = "payment_intent.payment_failed"

	stripeWebhookErrorType               = "stripe_webhook"
	stripePaymentMethodErrorType         = "stripe_api"
	stripeWebhookVerifyFailureMessage    = "stripe verify notification failed"
	stripePaymentMethodFailureMessage    = "stripe retrieve payment method failed"
	stripeWebhookErrorCodeInvalidPayload = "invalid_payload"
)

type stripeSafeWebhookError struct {
	message   string
	errorType string
	errorCode string
}

func (e stripeSafeWebhookError) Error() string            { return e.message }
func (e stripeSafeWebhookError) WebhookErrorType() string { return e.errorType }
func (e stripeSafeWebhookError) WebhookErrorCode() string { return e.errorCode }

type stripePaymentMethodRetriever interface {
	Retrieve(context.Context, string, *stripe.PaymentMethodRetrieveParams) (*stripe.PaymentMethod, error)
}

type stripeSDKPaymentMethodRetriever struct {
	client *stripe.Client
}

func (r stripeSDKPaymentMethodRetriever) Retrieve(
	ctx context.Context,
	id string,
	params *stripe.PaymentMethodRetrieveParams,
) (*stripe.PaymentMethod, error) {
	return r.client.V1PaymentMethods.Retrieve(ctx, id, params)
}

// Stripe implements the payment.CancelableProvider interface for Stripe payments.
type Stripe struct {
	instanceID string
	config     map[string]string

	mu             sync.Mutex
	initialized    bool
	sc             *stripe.Client
	paymentMethods stripePaymentMethodRetriever
}

// NewStripe creates a new Stripe provider instance.
func NewStripe(instanceID string, config map[string]string) (*Stripe, error) {
	if config["secretKey"] == "" {
		return nil, fmt.Errorf("stripe config missing required key: secretKey")
	}
	cfg := cloneStringMap(config)
	currency, err := payment.NormalizePaymentCurrency(cfg["currency"])
	if err != nil {
		return nil, fmt.Errorf("stripe config currency: %w", err)
	}
	cfg["currency"] = currency
	return &Stripe{
		instanceID: instanceID,
		config:     cfg,
	}, nil
}

func (s *Stripe) ensureInit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		s.sc = stripe.NewClient(s.config["secretKey"])
		s.paymentMethods = stripeSDKPaymentMethodRetriever{client: s.sc}
		s.initialized = true
	}
}

// GetPublishableKey returns the publishable key for frontend use.
func (s *Stripe) GetPublishableKey() string {
	return s.config["publishableKey"]
}

func (s *Stripe) Name() string        { return "Stripe" }
func (s *Stripe) ProviderKey() string { return payment.TypeStripe }
func (s *Stripe) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}

func (s *Stripe) MerchantIdentityMetadata() map[string]string {
	if s == nil {
		return nil
	}
	return map[string]string{"currency": s.currency()}
}

func (s *Stripe) currency() string {
	if s == nil {
		return payment.DefaultPaymentCurrency
	}
	currency, err := payment.NormalizePaymentCurrency(s.config["currency"])
	if err != nil {
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// stripePaymentMethodTypes maps our PaymentType to Stripe payment_method_types.
var stripePaymentMethodTypes = map[string][]string{
	payment.TypeCard:      {"card"},
	payment.TypeAlipay:    {"alipay"},
	payment.TypeWxpay:     {"wechat_pay"},
	payment.TypeLink:      {"link"},
	payment.TypeGooglePay: {"card"},
}

// CreatePayment creates a Stripe PaymentIntent.
func (s *Stripe) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	s.ensureInit()

	currency := s.currency()
	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, currency)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	// Collect all Stripe payment_method_types from the instance's configured sub-methods
	methods := resolveStripeMethodTypes(req.InstanceSubMethods)

	pmTypes := make([]*string, len(methods))
	for i, m := range methods {
		pmTypes[i] = stripe.String(m)
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:             stripe.Int64(amountInMinorUnit),
		Currency:           stripe.String(strings.ToLower(currency)),
		PaymentMethodTypes: pmTypes,
		Description:        stripe.String(req.Subject),
		Metadata:           map[string]string{"orderId": req.OrderID},
	}

	// WeChat Pay requires payment_method_options with client type
	if hasStripeMethod(methods, "wechat_pay") {
		params.PaymentMethodOptions = &stripe.PaymentIntentCreatePaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentCreatePaymentMethodOptionsWeChatPayParams{
				Client: stripe.String("web"),
			},
		}
	}

	params.SetIdempotencyKey(fmt.Sprintf("pi-%s", req.OrderID))
	params.Context = ctx

	pi, err := s.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	return &payment.CreatePaymentResponse{
		TradeNo:      pi.ID,
		ClientSecret: pi.ClientSecret,
		Currency:     currency,
	}, nil
}

// QueryOrder retrieves a PaymentIntent by ID.
func (s *Stripe) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	s.ensureInit()

	pi, err := s.sc.V1PaymentIntents.Retrieve(ctx, tradeNo, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query order: %w", err)
	}

	status := payment.ProviderStatusPending
	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded:
		status = payment.ProviderStatusPaid
	case stripe.PaymentIntentStatusCanceled:
		status = payment.ProviderStatusFailed
	}

	currency := stripeIntentCurrency(pi.Currency, s.currency())
	return &payment.QueryOrderResponse{
		TradeNo: pi.ID,
		Status:  status,
		Amount:  payment.MinorUnitToAmount(pi.Amount, currency),
		Metadata: map[string]string{
			"currency": currency,
		},
	}, nil
}

// VerifyNotification verifies a Stripe webhook event.
func (s *Stripe) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	s.ensureInit()

	webhookSecret := s.config["webhookSecret"]
	if webhookSecret == "" {
		return nil, stripeSafeWebhookError{
			message:   stripeWebhookVerifyFailureMessage,
			errorType: stripeWebhookErrorType,
			errorCode: "missing_webhook_secret",
		}
	}

	sig := headers["stripe-signature"]
	if sig == "" {
		return nil, stripeSafeWebhookError{
			message:   stripeWebhookVerifyFailureMessage,
			errorType: stripeWebhookErrorType,
			errorCode: "missing_signature",
		}
	}

	event, err := webhook.ConstructEvent([]byte(rawBody), sig, webhookSecret)
	if err != nil {
		return nil, stripeSafeWebhookError{
			message:   stripeWebhookVerifyFailureMessage,
			errorType: stripeWebhookErrorType,
			errorCode: stripeWebhookVerificationErrorCode(err),
		}
	}

	switch event.Type {
	case stripeEventPaymentSuccess:
		notification, pi, err := parseStripePaymentIntent(&event, payment.ProviderStatusSuccess, rawBody)
		if err != nil {
			return nil, err
		}
		resolvedType, err := s.resolvedPaymentType(ctx, pi)
		if err != nil {
			return nil, err
		}
		notification.Metadata[payment.NotificationMetadataPaymentType] = resolvedType
		return notification, nil
	case stripeEventPaymentFailed:
		notification, _, err := parseStripePaymentIntent(&event, payment.ProviderStatusFailed, rawBody)
		return notification, err
	}

	return nil, nil
}

func parseStripePaymentIntent(event *stripe.Event, status string, rawBody string) (*payment.PaymentNotification, *stripe.PaymentIntent, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return nil, nil, fmt.Errorf("stripe parse payment_intent: %w", err)
	}
	currency := stripeIntentCurrency(pi.Currency, payment.DefaultPaymentCurrency)
	notification := &payment.PaymentNotification{
		TradeNo: pi.ID,
		OrderID: pi.Metadata["orderId"],
		Amount:  payment.MinorUnitToAmount(pi.Amount, currency),
		Status:  status,
		RawData: rawBody,
		Metadata: map[string]string{
			"currency": currency,
		},
	}
	return notification, &pi, nil
}

func (s *Stripe) resolvedPaymentType(ctx context.Context, pi *stripe.PaymentIntent) (string, error) {
	if pi.PaymentMethod == nil || strings.TrimSpace(pi.PaymentMethod.ID) == "" {
		return "", fmt.Errorf("stripe succeeded payment intent missing payment method")
	}
	if s.paymentMethods == nil {
		s.logPaymentMethodLookupFailure(pi, stripePaymentMethodErrorType, "payment_method_retriever_unavailable")
		return "", stripePaymentMethodFailure("payment_method_retriever_unavailable")
	}

	method, err := s.paymentMethods.Retrieve(ctx, pi.PaymentMethod.ID, nil)
	if err != nil {
		errorType, errorCode := "unknown", "unknown"
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			errorType = string(stripeErr.Type)
			errorCode = string(stripeErr.Code)
			if strings.TrimSpace(errorType) == "" {
				errorType = "unknown"
			}
			if strings.TrimSpace(errorCode) == "" {
				errorCode = "unknown"
			}
		}
		s.logPaymentMethodLookupFailure(pi, errorType, errorCode)
		return "", stripeSafeWebhookError{
			message:   stripePaymentMethodFailureMessage,
			errorType: errorType,
			errorCode: errorCode,
		}
	}
	if method == nil {
		s.logPaymentMethodLookupFailure(pi, stripePaymentMethodErrorType, "empty_payment_method")
		return "", stripePaymentMethodFailure("empty_payment_method")
	}

	if method.Card != nil && method.Card.Wallet != nil &&
		method.Card.Wallet.Type == stripe.PaymentMethodCardWalletTypeGooglePay {
		return payment.TypeGooglePay, nil
	}
	return payment.TypeStripe, nil
}

func stripePaymentMethodFailure(code string) error {
	return stripeSafeWebhookError{
		message:   stripePaymentMethodFailureMessage,
		errorType: stripePaymentMethodErrorType,
		errorCode: code,
	}
}

func (s *Stripe) logPaymentMethodLookupFailure(pi *stripe.PaymentIntent, errorType, errorCode string) {
	slog.Warn("stripe payment method lookup failed",
		"orderID", pi.Metadata["orderId"],
		"providerInstanceID", s.instanceID,
		"type", errorType,
		"code", errorCode,
	)
}

func stripeWebhookVerificationErrorCode(err error) string {
	switch {
	case errors.Is(err, webhook.ErrInvalidHeader):
		return "invalid_header"
	case errors.Is(err, webhook.ErrNoValidSignature):
		return "invalid_signature"
	case errors.Is(err, webhook.ErrNotSigned):
		return "missing_signature"
	case errors.Is(err, webhook.ErrTooOld):
		return "signature_expired"
	default:
		return stripeWebhookErrorCodeInvalidPayload
	}
}

// Refund creates a Stripe refund.
func (s *Stripe) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, s.currency())
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(req.TradeNo),
		Amount:        stripe.Int64(amountInMinorUnit),
		Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
	}
	params.Context = ctx

	r, err := s.sc.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	refundStatus := payment.ProviderStatusPending
	if r.Status == stripe.RefundStatusSucceeded {
		refundStatus = payment.ProviderStatusSuccess
	}

	return &payment.RefundResponse{
		RefundID: r.ID,
		Status:   refundStatus,
	}, nil
}

// QueryRefund retrieves a Stripe refund by refund ID when available, otherwise
// falls back to the latest refund for the PaymentIntent.
func (s *Stripe) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	var r *stripe.Refund
	var err error
	if refundID := strings.TrimSpace(req.RefundID); refundID != "" {
		r, err = s.sc.V1Refunds.Retrieve(ctx, refundID, nil)
		if err != nil {
			return nil, fmt.Errorf("stripe query refund: %w", err)
		}
	} else {
		tradeNo := strings.TrimSpace(req.TradeNo)
		if tradeNo == "" {
			return nil, fmt.Errorf("stripe query refund: missing payment intent id")
		}
		params := &stripe.RefundListParams{PaymentIntent: stripe.String(tradeNo)}
		params.Limit = stripe.Int64(1)
		list := s.sc.V1Refunds.List(ctx, params)
		if list.Err() != nil {
			return nil, fmt.Errorf("stripe query refund: %w", list.Err())
		}
		refunds := list.Data()
		if len(refunds) == 0 {
			return nil, fmt.Errorf("stripe query refund: no refund found")
		}
		r = refunds[0]
	}

	return &payment.RefundResponse{RefundID: r.ID, Status: stripeRefundProviderStatus(r.Status)}, nil
}

func stripeRefundProviderStatus(status stripe.RefundStatus) string {
	switch status {
	case stripe.RefundStatusSucceeded:
		return payment.ProviderStatusSuccess
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func stripeIntentCurrency(raw stripe.Currency, fallback string) string {
	currency, err := payment.NormalizePaymentCurrency(string(raw))
	if err != nil || currency == payment.DefaultPaymentCurrency && strings.TrimSpace(string(raw)) == "" {
		normalizedFallback, fallbackErr := payment.NormalizePaymentCurrency(fallback)
		if fallbackErr == nil {
			return normalizedFallback
		}
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// resolveStripeMethodTypes converts instance supported_types (comma-separated)
// into Stripe API payment_method_types. Falls back to ["card"] if empty.
func resolveStripeMethodTypes(instanceSubMethods string) []string {
	if instanceSubMethods == "" {
		return []string{"card"}
	}
	methods := make([]string, 0)
	seen := make(map[string]struct{})
	for _, paymentType := range strings.Split(instanceSubMethods, ",") {
		paymentType = strings.TrimSpace(paymentType)
		for _, method := range stripePaymentMethodTypes[paymentType] {
			if _, exists := seen[method]; exists {
				continue
			}
			seen[method] = struct{}{}
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 {
		return []string{"card"}
	}
	return methods
}

// hasStripeMethod checks if the given Stripe method list contains the target method.
func hasStripeMethod(methods []string, target string) bool {
	for _, m := range methods {
		if m == target {
			return true
		}
	}
	return false
}

// CancelPayment cancels a pending PaymentIntent.
func (s *Stripe) CancelPayment(ctx context.Context, tradeNo string) error {
	s.ensureInit()

	_, err := s.sc.V1PaymentIntents.Cancel(ctx, tradeNo, nil)
	if err != nil {
		return fmt.Errorf("stripe cancel payment: %w", err)
	}
	return nil
}

// Ensure interface compliance.
var (
	_ payment.Provider                 = (*Stripe)(nil)
	_ payment.CancelableProvider       = (*Stripe)(nil)
	_ payment.MerchantIdentityProvider = (*Stripe)(nil)
)
