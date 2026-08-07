//go:build unit

package provider

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
	stripewebhook "github.com/stripe/stripe-go/v85/webhook"
)

const stripeWebhookTestSecret = "whsec_test_stripe"

func TestNewStripeKeepsSecretOnlyRuntimeCompatibility(t *testing.T) {
	t.Parallel()

	prov, err := NewStripe("legacy", map[string]string{
		"secretKey": "sk_test_legacy",
		"currency":  "CNY",
	})
	require.NoError(t, err)
	require.NotNil(t, prov)

	_, err = prov.VerifyNotification(context.Background(), `{}`, nil)
	require.ErrorContains(t, err, "webhookSecret not configured")
}

func TestStripeVerifyNotificationAcceptsSignedLegacyAPIVersionWithMinimalSchema(t *testing.T) {
	t.Parallel()

	rawBody := `{
		"id":"evt_legacy",
		"object":"event",
		"api_version":"2020-08-27",
		"type":"payment_intent.succeeded",
		"data":{"object":{
			"id":"pi_legacy",
			"object":"payment_intent",
			"amount":1000,
			"currency":"cny",
			"metadata":{"orderId":"sub2_legacy_order"}
		}}
	}`
	prov := newStripeWebhookTestProvider(t)
	notification, err := prov.VerifyNotification(
		context.Background(),
		rawBody,
		map[string]string{"stripe-signature": signStripeWebhookTestPayload(rawBody, stripeWebhookTestSecret)},
	)
	require.NoError(t, err)
	require.NotNil(t, notification)
	require.Equal(t, "pi_legacy", notification.TradeNo)
	require.Equal(t, "sub2_legacy_order", notification.OrderID)
	require.Equal(t, 10.0, notification.Amount)
	require.Equal(t, payment.NotificationStatusSuccess, notification.Status)
	require.Equal(t, "CNY", notification.Metadata["currency"])
}

func TestStripeVerifyNotificationRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	rawBody := `{"id":"evt_bad_sig","object":"event","api_version":"2020-08-27","type":"payment_intent.succeeded","data":{"object":{"id":"pi_bad_sig","object":"payment_intent","amount":1000,"currency":"cny","metadata":{"orderId":"sub2_bad_sig"}}}}`
	prov := newStripeWebhookTestProvider(t)
	notification, err := prov.VerifyNotification(
		context.Background(),
		rawBody,
		map[string]string{"stripe-signature": signStripeWebhookTestPayload(rawBody, "whsec_wrong")},
	)
	require.Nil(t, notification)
	require.ErrorContains(t, err, "stripe verify notification")
}

func TestStripeVerifyNotificationEventTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		eventType  string
		wantNil    bool
		wantStatus string
	}{
		{
			name:       "payment succeeded",
			eventType:  stripeEventPaymentSuccess,
			wantStatus: payment.NotificationStatusSuccess,
		},
		{
			name:       "payment failed",
			eventType:  stripeEventPaymentFailed,
			wantStatus: payment.ProviderStatusFailed,
		},
		{
			name:      "irrelevant event",
			eventType: "customer.updated",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rawBody := `{"id":"evt_type","object":"event","api_version":"2020-08-27","type":"` + tt.eventType + `","data":{"object":{"id":"pi_type","object":"payment_intent","amount":525,"currency":"usd","metadata":{"orderId":"sub2_type"}}}}`
			prov := newStripeWebhookTestProvider(t)
			notification, err := prov.VerifyNotification(
				context.Background(),
				rawBody,
				map[string]string{"stripe-signature": signStripeWebhookTestPayload(rawBody, stripeWebhookTestSecret)},
			)
			require.NoError(t, err)
			if tt.wantNil {
				require.Nil(t, notification)
				return
			}
			require.NotNil(t, notification)
			require.Equal(t, tt.wantStatus, notification.Status)
		})
	}
}

func TestStripeVerifyNotificationRejectsIncompletePaymentIntent(t *testing.T) {
	t.Parallel()

	rawBody := `{"id":"evt_missing_order","object":"event","api_version":"2020-08-27","type":"payment_intent.succeeded","data":{"object":{"id":"pi_missing_order","object":"payment_intent","amount":1000,"currency":"cny","metadata":{}}}}`
	prov := newStripeWebhookTestProvider(t)
	notification, err := prov.VerifyNotification(
		context.Background(),
		rawBody,
		map[string]string{"stripe-signature": signStripeWebhookTestPayload(rawBody, stripeWebhookTestSecret)},
	)
	require.Nil(t, notification)
	require.ErrorContains(t, err, "metadata.orderId")
}

func newStripeWebhookTestProvider(t *testing.T) *Stripe {
	t.Helper()

	prov, err := NewStripe("stripe-test", map[string]string{
		"secretKey":      "sk_test_stripe",
		"publishableKey": "pk_test_stripe",
		"webhookSecret":  stripeWebhookTestSecret,
		"currency":       "CNY",
	})
	require.NoError(t, err)
	return prov
}

func signStripeWebhookTestPayload(rawBody, secret string) string {
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   []byte(rawBody),
		Secret:    secret,
		Timestamp: time.Now(),
	})
	return signed.Header
}

type stripeRefundBackend struct {
	params []*stripe.RefundCreateParams
}

func (b *stripeRefundBackend) Call(_ string, _ string, _ string, params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	b.params = append(b.params, params.(*stripe.RefundCreateParams))
	refund := v.(*stripe.Refund)
	refund.ID = "re_123"
	refund.Status = stripe.RefundStatusSucceeded
	return nil
}

func (*stripeRefundBackend) CallStreaming(string, string, string, stripe.ParamsContainer, stripe.StreamingLastResponseSetter) error {
	return nil
}

func (*stripeRefundBackend) CallRaw(string, string, string, []byte, *stripe.Params, stripe.LastResponseSetter) error {
	return nil
}

func (*stripeRefundBackend) CallMultipart(string, string, string, string, *bytes.Buffer, *stripe.Params, stripe.LastResponseSetter) error {
	return nil
}

func (*stripeRefundBackend) SetMaxNetworkRetries(int64) {}

func TestStripeRefundUsesStableAmountSpecificIdempotencyKey(t *testing.T) {
	backend := &stripeRefundBackend{}
	client := stripe.NewClient("sk_test", stripe.WithBackends(&stripe.Backends{API: backend}))
	provider := &Stripe{
		config:      map[string]string{"currency": "CNY"},
		initialized: true,
		sc:          client,
	}

	refund := func(amount string) {
		_, err := provider.Refund(context.Background(), payment.RefundRequest{
			TradeNo: "pi_123",
			OrderID: "sub2_order_456",
			Amount:  amount,
		})
		require.NoError(t, err)
	}

	refund("12.34")
	refund("12.34")
	refund("12.35")

	require.Len(t, backend.params, 3)
	require.Equal(t, int64(1234), *backend.params[0].Amount)
	require.Equal(t, "re-sub2_order_456-1234", *backend.params[0].IdempotencyKey)
	require.Equal(t, backend.params[0].IdempotencyKey, backend.params[1].IdempotencyKey)
	require.Equal(t, int64(1235), *backend.params[2].Amount)
	require.Equal(t, "re-sub2_order_456-1235", *backend.params[2].IdempotencyKey)
	require.NotEqual(t, *backend.params[0].IdempotencyKey, *backend.params[2].IdempotencyKey)
}
