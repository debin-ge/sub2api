//go:build unit

package provider

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
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
