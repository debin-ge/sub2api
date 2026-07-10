//go:build unit

package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

const stripeWebhookTestSecret = "whsec_task_5_test_secret"

type fakeStripePaymentMethodRetriever struct {
	paymentMethod *stripe.PaymentMethod
	err           error
	requestedID   string
	calls         int
}

func (f *fakeStripePaymentMethodRetriever) Retrieve(
	_ context.Context,
	id string,
	_ *stripe.PaymentMethodRetrieveParams,
) (*stripe.PaymentMethod, error) {
	f.calls++
	f.requestedID = id
	return f.paymentMethod, f.err
}

func TestResolveStripeMethodTypesMapsGooglePayToDeduplicatedCard(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"card"}, resolveStripeMethodTypes("card,google_pay"))
	require.Equal(t, []string{"card", "link"}, resolveStripeMethodTypes("google_pay,card,link"))
	require.Equal(t, []string{"link", "card"}, resolveStripeMethodTypes("link,google_pay"))
}

func TestStripeWebhookGooglePayPaymentMethodSetsActualPaymentType(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{
		paymentMethod: &stripe.PaymentMethod{
			Card: &stripe.PaymentMethodCard{
				Wallet: &stripe.PaymentMethodCardWallet{
					Type: stripe.PaymentMethodCardWalletTypeGooglePay,
				},
			},
		},
	}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.NoError(t, err)
	require.NotNil(t, notification)
	require.Equal(t, payment.TypeGooglePay, notification.Metadata[payment.NotificationMetadataPaymentType])
	require.Equal(t, "pm_123", retriever.requestedID)
	require.Equal(t, 1, retriever.calls)
}

func TestStripeWebhookCardPaymentMethodSetsStripePaymentType(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{
		paymentMethod: &stripe.PaymentMethod{Card: &stripe.PaymentMethodCard{}},
	}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.NoError(t, err)
	require.NotNil(t, notification)
	require.Equal(t, payment.TypeStripe, notification.Metadata[payment.NotificationMetadataPaymentType])
	require.Equal(t, "pm_123", retriever.requestedID)
	require.Equal(t, 1, retriever.calls)
}

func TestStripeWebhookPaymentMethodRetrieveErrorIsSanitizedAndRetryable(t *testing.T) {
	const sensitive = "must-not-log-card-payload"
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	retriever := &fakeStripePaymentMethodRetriever{err: &stripe.Error{
		Type: stripe.ErrorTypeAPI,
		Code: stripe.ErrorCodeAPIKeyExpired,
		Msg:  sensitive,
	}}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe retrieve payment method failed")
	require.Equal(t, "pm_123", retriever.requestedID)
	require.Equal(t, 1, retriever.calls)
	require.Contains(t, logs.String(), "orderID=sub2_selected_42")
	require.Contains(t, logs.String(), "providerInstanceID=stripe-instance-42")
	require.Contains(t, logs.String(), "type=api_error")
	require.Contains(t, logs.String(), "code=api_key_expired")
	require.NotContains(t, logs.String(), sensitive)
	require.NotContains(t, logs.String(), "pm_123")
	require.NotContains(t, err.Error(), sensitive)
}

func TestStripeWebhookInvalidSignatureDoesNotRetrievePaymentMethod(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{
		paymentMethod: &stripe.PaymentMethod{Card: &stripe.PaymentMethodCard{}},
	}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, _ := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)
	badSignature := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: []byte(rawBody),
		Secret:  "whsec_wrong_secret",
	}).Header

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": badSignature,
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe verify notification failed")
	require.NotContains(t, err.Error(), webhook.ErrNoValidSignature.Error())
	var safeErr interface {
		WebhookErrorType() string
		WebhookErrorCode() string
	}
	require.ErrorAs(t, err, &safeErr)
	require.Equal(t, "stripe_webhook", safeErr.WebhookErrorType())
	require.Equal(t, "invalid_signature", safeErr.WebhookErrorCode())
	require.Zero(t, retriever.calls)
	require.Empty(t, retriever.requestedID)
}

func TestStripeWebhookNilPaymentMethodRetrieverReturnsRetryableError(t *testing.T) {
	provider := newStripeWebhookProviderForTest(nil)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	var notification *payment.PaymentNotification
	var err error
	require.NotPanics(t, func() {
		notification, err = provider.VerifyNotification(context.Background(), rawBody, map[string]string{
			"stripe-signature": signature,
		})
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe retrieve payment method failed")
}

func TestStripeWebhookNilPaymentMethodResultReturnsRetryableError(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	var notification *payment.PaymentNotification
	var err error
	require.NotPanics(t, func() {
		notification, err = provider.VerifyNotification(context.Background(), rawBody, map[string]string{
			"stripe-signature": signature,
		})
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe retrieve payment method failed")
	require.Equal(t, 1, retriever.calls)
}

func TestStripeWebhookFailedEventDoesNotRetrievePaymentMethod(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{
		paymentMethod: &stripe.PaymentMethod{Card: &stripe.PaymentMethodCard{}},
	}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentFailed, true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.NoError(t, err)
	require.NotNil(t, notification)
	require.Equal(t, payment.ProviderStatusFailed, notification.Status)
	require.NotContains(t, notification.Metadata, payment.NotificationMetadataPaymentType)
	require.Zero(t, retriever.calls)
}

func TestStripeWebhookUnrelatedEventDoesNotRetrievePaymentMethod(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{
		paymentMethod: &stripe.PaymentMethod{Card: &stripe.PaymentMethodCard{}},
	}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, "customer.created", true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.NoError(t, err)
	require.Nil(t, notification)
	require.Zero(t, retriever.calls)
}

func TestStripeWebhookMissingPaymentMethodReturnsRetryableError(t *testing.T) {
	retriever := &fakeStripePaymentMethodRetriever{}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, false)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe succeeded payment intent missing payment method")
	require.Zero(t, retriever.calls)
}

func TestStripeWebhookPaymentMethodRetrieveReturnsGenericErrorForNonStripeError(t *testing.T) {
	const sensitive = "must-not-log-card-payload"
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	retriever := &fakeStripePaymentMethodRetriever{err: errors.New(sensitive)}
	provider := newStripeWebhookProviderForTest(retriever)
	rawBody, signature := signedStripePaymentIntentEvent(t, stripeEventPaymentSuccess, true)

	notification, err := provider.VerifyNotification(context.Background(), rawBody, map[string]string{
		"stripe-signature": signature,
	})

	require.Nil(t, notification)
	require.EqualError(t, err, "stripe retrieve payment method failed")
	require.Contains(t, logs.String(), "type=unknown")
	require.NotContains(t, logs.String(), sensitive)
	require.NotContains(t, err.Error(), sensitive)
}

func newStripeWebhookProviderForTest(retriever stripePaymentMethodRetriever) *Stripe {
	return &Stripe{
		instanceID:     "stripe-instance-42",
		config:         map[string]string{"secretKey": "sk_test", "webhookSecret": stripeWebhookTestSecret},
		initialized:    true,
		paymentMethods: retriever,
	}
}

func signedStripePaymentIntentEvent(t *testing.T, eventType string, includePaymentMethod bool) (string, string) {
	t.Helper()
	paymentMethod := ""
	if includePaymentMethod {
		paymentMethod = `,"payment_method":"pm_123"`
	}
	rawBody := fmt.Sprintf(
		`{"id":"evt_task_5","object":"event","api_version":%q,"type":%q,"data":{"object":{"id":"pi_123","object":"payment_intent","amount":1234,"currency":"usd"%s,"metadata":{"orderId":"sub2_selected_42"}}}}`,
		stripe.APIVersion,
		eventType,
		paymentMethod,
	)
	require.False(t, strings.Contains(rawBody, "\n"))
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: []byte(rawBody),
		Secret:  stripeWebhookTestSecret,
	})
	return rawBody, signed.Header
}
