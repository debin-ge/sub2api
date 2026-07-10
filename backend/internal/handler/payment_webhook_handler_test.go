//go:build unit

package handler

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/paymentwebhookdelivery"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	paymentprovider "github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
	stripewebhook "github.com/stripe/stripe-go/v85/webhook"
	_ "modernc.org/sqlite"
)

const wiseWebhookHandlerTestEncryptionKey = "12345678901234567890123456789012"

func TestWriteSuccessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		providerKey     string
		wantCode        int
		wantContentType string
		wantBody        string
		checkJSON       bool
		wantJSONCode    string
		wantJSONMessage string
	}{
		{
			name:            "wxpay returns JSON with code SUCCESS",
			providerKey:     "wxpay",
			wantCode:        http.StatusOK,
			wantContentType: "application/json",
			checkJSON:       true,
			wantJSONCode:    "SUCCESS",
			wantJSONMessage: "成功",
		},
		{
			name:            "stripe returns empty 200",
			providerKey:     "stripe",
			wantCode:        http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "",
		},
		{
			name:            "airwallex returns empty 200",
			providerKey:     payment.TypeAirwallex,
			wantCode:        http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "",
		},
		{
			name:            "easypay returns plain text success",
			providerKey:     "easypay",
			wantCode:        http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "success",
		},
		{
			name:            "alipay returns plain text success",
			providerKey:     "alipay",
			wantCode:        http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "success",
		},
		{
			name:            "unknown provider returns plain text success",
			providerKey:     "unknown_provider",
			wantCode:        http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			writeSuccessResponse(c, tt.providerKey)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), tt.wantContentType)

			if tt.checkJSON {
				var resp wxpaySuccessResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err, "response body should be valid JSON")
				assert.Equal(t, tt.wantJSONCode, resp.Code)
				assert.Equal(t, tt.wantJSONMessage, resp.Message)
			} else {
				assert.Equal(t, tt.wantBody, w.Body.String())
			}
		})
	}
}

func TestWriteSuccessResponseWiseReturnsEmpty200(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeSuccessResponse(c, payment.TypeWise)

	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
}

func TestWiseWebhookHandlerEndpointCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name             string
		rawBody          string
		deliveryID       string
		testNotification bool
		signBody         string
		wantStatus       int
		wantBody         string
		wantDeliveryID   string
		wantNoDelivery   bool
		repeatRequest    bool
	}{
		{
			name:             "test notification returns empty 200 and records ignored delivery",
			rawBody:          `{"event_type":"balances#credit","data":{"resource":{"id":"resource-123"}}}`,
			deliveryID:       "handler-delivery-test",
			testNotification: true,
			wantStatus:       http.StatusOK,
			wantDeliveryID:   "handler-delivery-test",
		},
		{
			name:           "duplicate delivery returns empty 200",
			rawBody:        `{"event_type":"balances#credit"}`,
			deliveryID:     "handler-delivery-dup",
			wantStatus:     http.StatusOK,
			wantDeliveryID: "handler-delivery-dup",
			repeatRequest:  true,
		},
		{
			name:           "unsupported signed event returns empty 200 and records ignored delivery",
			rawBody:        `{"event_type":"unsupported#event"}`,
			deliveryID:     "handler-delivery-unsupported",
			wantStatus:     http.StatusOK,
			wantDeliveryID: "handler-delivery-unsupported",
		},
		{
			name:           "invalid signature returns 400 and does not record delivery",
			rawBody:        `{"event_type":"balances#credit"}`,
			deliveryID:     "handler-delivery-invalid-signature",
			signBody:       `{"event_type":"different"}`,
			wantStatus:     http.StatusBadRequest,
			wantBody:       "verify failed",
			wantNoDelivery: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := newWiseWebhookHandlerTestClient(t)
			priv := createWiseWebhookHandlerProvider(t, ctx, client)
			handler := newWiseWebhookHandlerForTest(client)

			w := postWiseWebhookHandlerRequest(t, handler, priv, tt.rawBody, tt.signBody, tt.deliveryID, tt.testNotification)
			require.Equal(t, tt.wantStatus, w.Code)
			if tt.wantBody != "" {
				require.Contains(t, w.Body.String(), tt.wantBody)
			} else {
				require.Empty(t, w.Body.String())
			}

			if tt.repeatRequest {
				w = postWiseWebhookHandlerRequest(t, handler, priv, tt.rawBody, tt.signBody, tt.deliveryID, tt.testNotification)
				require.Equal(t, tt.wantStatus, w.Code)
				require.Empty(t, w.Body.String())
			}

			if tt.wantNoDelivery {
				count, err := client.PaymentWebhookDelivery.Query().
					Where(paymentwebhookdelivery.ProviderKeyEQ(payment.TypeWise), paymentwebhookdelivery.DeliveryIDEQ(tt.deliveryID)).
					Count(ctx)
				require.NoError(t, err)
				require.Zero(t, count)
				return
			}
			if tt.wantDeliveryID != "" {
				delivery, err := client.PaymentWebhookDelivery.Query().
					Where(paymentwebhookdelivery.ProviderKeyEQ(payment.TypeWise), paymentwebhookdelivery.DeliveryIDEQ(tt.wantDeliveryID)).
					Only(ctx)
				require.NoError(t, err)
				require.Equal(t, tt.wantDeliveryID, delivery.DeliveryID)
			}
		})
	}
}

// TestUnknownOrderWebhookAcksWithSuccess exercises the response contract that
// handleNotify relies on when HandlePaymentNotification returns ErrOrderNotFound:
// we still need to emit the provider-specific 2xx so the provider stops
// retrying. We can't easily drive handleNotify end-to-end without mocking the
// concrete *service.PaymentService, so this test locks down the two ingredients
// the fix depends on:
//  1. errors.Is recognises the sentinel through fmt.Errorf %w wrapping (which
//     is how service layer wraps it with the out_trade_no context).
//  2. writeSuccessResponse produces the provider-specific body for Stripe
//     (empty 200) — matching what handleNotify calls on the ack path.
//
// If either contract breaks, the Stripe "unknown order → 500 loop" regresses.
func TestUnknownOrderWebhookAcksWithSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1) Sentinel recognition through wrapping.
	wrapped := fmt.Errorf("%w: out_trade_no=sub2_missing_42", service.ErrOrderNotFound)
	require.True(t, errors.Is(wrapped, service.ErrOrderNotFound),
		"handleNotify uses errors.Is on the wrapped service error; regression here "+
			"would mean unknown-order webhooks go back to returning 500 and looping forever")

	// A distinct error must NOT match — otherwise a DB failure would be silently
	// swallowed as an ack.
	other := errors.New("lookup order failed: connection refused")
	require.False(t, errors.Is(other, service.ErrOrderNotFound))

	// 2) Provider-specific success body is what handleNotify emits on the
	// ack path. Asserted again here because this is the shape Stripe expects
	// to consider the webhook acknowledged.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeSuccessResponse(c, payment.TypeStripe)
	require.Equal(t, http.StatusOK, w.Code,
		"Stripe requires 2xx to stop retrying; anything else restarts the retry loop")
	require.Empty(t, w.Body.String(), "Stripe expects an empty body on the ack path")
}

func TestWebhookConstants(t *testing.T) {
	t.Run("maxWebhookBodySize is 1MB", func(t *testing.T) {
		assert.Equal(t, int64(1<<20), int64(maxWebhookBodySize))
	})

	t.Run("webhookLogTruncateLen is 200", func(t *testing.T) {
		assert.Equal(t, 200, webhookLogTruncateLen)
	})
}

func TestExtractOutTradeNo(t *testing.T) {
	tests := []struct {
		name        string
		providerKey string
		rawBody     string
		want        string
	}{
		{
			name:        "easypay query payload",
			providerKey: "easypay",
			rawBody:     "out_trade_no=sub2_123&trade_status=TRADE_SUCCESS",
			want:        "sub2_123",
		},
		{
			name:        "alipay query payload",
			providerKey: "alipay",
			rawBody:     "notify_time=2026-04-20+12%3A00%3A00&out_trade_no=sub2_456",
			want:        "sub2_456",
		},
		{
			name:        "unknown provider",
			providerKey: "wxpay",
			rawBody:     "{}",
			want:        "",
		},
		{
			name:        "airwallex payment intent payload",
			providerKey: payment.TypeAirwallex,
			rawBody:     `{"name":"payment_intent.succeeded","data":{"object":{"merchant_order_id":"sub2_awx_123"}}}`,
			want:        "sub2_awx_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractOutTradeNo(tt.rawBody, tt.providerKey))
		})
	}
}

func TestExtractOutTradeNoWiseReturnsEmptyBecauseWebhookTriggersReconcile(t *testing.T) {
	got := extractOutTradeNo(`{"event_type":"balances#credit"}`, payment.TypeWise)
	require.Empty(t, got)
}

func TestStripeWebhookExtractOutTradeNoUsesPaymentIntentOrderIDForInstanceRouting(t *testing.T) {
	rawBody := `{"id":"evt_123","type":"payment_intent.succeeded","data":{"object":{"metadata":{"orderId":"  sub2_selected_42  "}}}}`

	require.Equal(t, "sub2_selected_42", extractOutTradeNo(rawBody, payment.TypeStripe))
}

func TestStripeWebhookVerifyNotificationErrorPropagatesForRetry(t *testing.T) {
	providers := []payment.Provider{
		webhookHandlerProviderStub{
			key:       payment.TypeStripe,
			verifyErr: errors.New("stripe retrieve payment method failed"),
		},
	}

	providerKey, notification, err := verifyNotificationWithProviders(context.Background(), providers, "{}", nil)

	require.Empty(t, providerKey)
	require.Nil(t, notification)
	require.EqualError(t, err, "stripe retrieve payment method failed")
}

func TestStripeWebhookVerifyFailureBodyIsNeverLogged(t *testing.T) {
	require.False(t, shouldLogWebhookVerifyFailureBody(payment.TypeStripe))
	require.True(t, shouldLogWebhookVerifyFailureBody(payment.TypeWxpay))
}

func TestStripeWebhookHandlerFailureLogsAreSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		provider      func(t *testing.T) payment.Provider
		rawBody       string
		signature     func(rawBody string) string
		forbidden     []string
		wantErrorType string
		wantErrorCode string
	}{
		{
			name: "invalid signature",
			provider: func(t *testing.T) payment.Provider {
				t.Helper()
				provider, err := paymentprovider.NewStripe("stripe-handler-instance", map[string]string{
					"secretKey":     "sk_test_handler",
					"webhookSecret": "whsec_handler_expected",
				})
				require.NoError(t, err)
				return provider
			},
			rawBody: `{"id":"evt_sensitive","object":"event","data":{"object":{"metadata":{"orderId":"must-not-log-unverified-order-pm_evil_payload"}}},"marker":"must-not-log-invalid-signature-body"}`,
			signature: func(rawBody string) string {
				return stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
					Payload: []byte(rawBody),
					Secret:  "whsec_handler_wrong",
				}).Header
			},
			forbidden: []string{
				"must-not-log-invalid-signature-body",
				"must-not-log-unverified-order-pm_evil_payload",
				"pm_evil_payload",
				stripewebhook.ErrNoValidSignature.Error(),
			},
			wantErrorType: "stripe_webhook",
			wantErrorCode: "invalid_signature",
		},
		{
			name: "sdk lookup failure",
			provider: func(t *testing.T) payment.Provider {
				t.Helper()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("Stripe SDK Retrieve method = %s, want GET", r.Method)
					}
					if r.URL.Path != "/v1/payment_methods/pm_sensitive_123" {
						t.Errorf("Stripe SDK Retrieve path = %s, want /v1/payment_methods/pm_sensitive_123", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusPaymentRequired)
					_, _ = w.Write([]byte(`{"error":{"type":"api_error","code":"api_key_expired","message":"must-not-log-card-payload sdk exploded","payment_method":{"id":"pm_sensitive_123","card":{"wallet":{"type":"google_pay"}}}}}`))
				}))
				t.Cleanup(server.Close)

				originalBackend := stripe.GetBackend(stripe.APIBackend)
				backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
					URL:               stripe.String(server.URL),
					MaxNetworkRetries: stripe.Int64(0),
					LeveledLogger:     &stripe.LeveledLogger{Level: stripe.LevelNull},
				})
				stripe.SetBackend(stripe.APIBackend, backend)
				t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, originalBackend) })

				provider, err := paymentprovider.NewStripe("stripe-handler-instance", map[string]string{
					"secretKey":     "sk_test_handler",
					"webhookSecret": "whsec_handler_expected",
				})
				require.NoError(t, err)
				return provider
			},
			rawBody: fmt.Sprintf(
				`{"id":"evt_sdk_sensitive","object":"event","api_version":%q,"type":"payment_intent.succeeded","data":{"object":{"id":"pi_sensitive","object":"payment_intent","amount":1234,"currency":"usd","payment_method":"pm_sensitive_123","metadata":{"orderId":"sub2_verified_order"}}},"marker":"must-not-log-sdk-body"}`,
				stripe.APIVersion,
			),
			signature: func(rawBody string) string {
				return stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
					Payload: []byte(rawBody),
					Secret:  "whsec_handler_expected",
				}).Header
			},
			forbidden: []string{
				"must-not-log-card-payload",
				"pm_sensitive_123",
				"sdk exploded",
				"must-not-log-sdk-body",
			},
			wantErrorType: "api_error",
			wantErrorCode: "api_key_expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })

			handler := newStripeWebhookHandlerForTest(t, tt.provider(t))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/stripe", bytes.NewBufferString(tt.rawBody))
			req.Header.Set("Stripe-Signature", tt.signature(tt.rawBody))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			handler.StripeWebhook(c)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "verify failed")
			require.Contains(t, logs.String(), "type="+tt.wantErrorType)
			require.Contains(t, logs.String(), "code="+tt.wantErrorCode)
			forbiddenValues := append([]string{
				"outTradeNo=",
				"provider=stripe",
				"method=POST",
				"bodyLen=",
			}, tt.forbidden...)
			for _, forbidden := range forbiddenValues {
				require.NotContains(t, logs.String(), forbidden)
			}
		})
	}
}

func TestStripeWebhookProviderLookupFailureLogsOnlySafeClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := newWiseWebhookHandlerTestClient(t)
	for i := 1; i <= 2; i++ {
		_, err := client.PaymentProviderInstance.Create().
			SetProviderKey(payment.TypeStripe).
			SetName(fmt.Sprintf("stripe-lookup-%d", i)).
			SetConfig("unused-encrypted-config").
			SetSupportedTypes(payment.TypeStripe).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
	}

	registry := payment.NewRegistry()
	loadBalancer := payment.NewDefaultLoadBalancer(client, []byte(wiseWebhookHandlerTestEncryptionKey))
	paymentSvc := service.NewPaymentService(client, registry, loadBalancer, nil, nil, nil, nil, nil, nil)
	handler := NewPaymentWebhookHandler(paymentSvc, registry)

	const maliciousOrderID = "must-not-log-provider-lookup-pm_evil_payload"
	rawBody := fmt.Sprintf(
		`{"id":"evt_lookup_sensitive","object":"event","data":{"object":{"metadata":{"orderId":%q}}},"marker":"must-not-log-provider-lookup-body"}`,
		maliciousOrderID,
	)
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/stripe", bytes.NewBufferString(rawBody))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.StripeWebhook(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "verify failed")
	require.Contains(t, logs.String(), "type=stripe_webhook")
	require.Contains(t, logs.String(), "code=provider_lookup_failed")
	for _, forbidden := range []string{
		maliciousOrderID,
		"pm_evil_payload",
		"must-not-log-provider-lookup-body",
		"provider=stripe",
		"outTradeNo=",
		"error=",
		"method=",
		"bodyLen=",
		"rawBody=",
	} {
		require.NotContains(t, logs.String(), forbidden)
	}
}

func TestStripeWebhookUnknownSignedOrderTriesAllInstancesThenAcks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := newWiseWebhookHandlerTestClient(t)
	for _, suffix := range []string{"first", "matched"} {
		_, err := client.PaymentProviderInstance.Create().
			SetProviderKey(payment.TypeStripe).
			SetName("stripe-unknown-signed-" + suffix).
			SetConfig(encryptWiseWebhookHandlerConfig(t, map[string]string{
				"secretKey":     "sk_test_" + suffix,
				"webhookSecret": "whsec_" + suffix,
				"currency":      "USD",
			})).
			SetSupportedTypes(payment.TypeStripe).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
	}

	paymentMethodRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paymentMethodRequests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/payment_methods/pm_unknown_signed", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pm_unknown_signed","object":"payment_method","type":"card","card":{"wallet":{"type":"google_pay"}}}`))
	}))
	t.Cleanup(server.Close)
	originalBackend := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:               stripe.String(server.URL),
		MaxNetworkRetries: stripe.Int64(0),
		LeveledLogger:     &stripe.LeveledLogger{Level: stripe.LevelNull},
	}))
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, originalBackend) })

	registry := payment.NewRegistry()
	loadBalancer := payment.NewDefaultLoadBalancer(client, []byte(wiseWebhookHandlerTestEncryptionKey))
	paymentSvc := service.NewPaymentService(client, registry, loadBalancer, nil, nil, nil, nil, nil, nil)
	handler := NewPaymentWebhookHandler(paymentSvc, registry)
	rawBody := fmt.Sprintf(
		`{"id":"evt_unknown_signed","object":"event","api_version":%q,"type":"payment_intent.succeeded","data":{"object":{"id":"pi_unknown_signed","object":"payment_intent","amount":8800,"currency":"usd","payment_method":"pm_unknown_signed","metadata":{"orderId":"sub2_unknown_signed"}}}}`,
		stripe.APIVersion,
	)
	signature := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: []byte(rawBody),
		Secret:  "whsec_matched",
	}).Header
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/stripe", bytes.NewBufferString(rawBody))
	req.Header.Set("Stripe-Signature", signature)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.StripeWebhook(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, 1, paymentMethodRequests)
}

func TestStripeWebhookUnknownOrderInvalidSignatureRemainsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := newWiseWebhookHandlerTestClient(t)
	for _, suffix := range []string{"a", "b"} {
		_, err := client.PaymentProviderInstance.Create().
			SetProviderKey(payment.TypeStripe).
			SetName("stripe-invalid-signature-" + suffix).
			SetConfig(encryptWiseWebhookHandlerConfig(t, map[string]string{
				"secretKey":     "sk_test_" + suffix,
				"webhookSecret": "whsec_" + suffix,
				"currency":      "USD",
			})).
			SetSupportedTypes(payment.TypeStripe).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
	}

	registry := payment.NewRegistry()
	loadBalancer := payment.NewDefaultLoadBalancer(client, []byte(wiseWebhookHandlerTestEncryptionKey))
	paymentSvc := service.NewPaymentService(client, registry, loadBalancer, nil, nil, nil, nil, nil, nil)
	handler := NewPaymentWebhookHandler(paymentSvc, registry)
	rawBody := `{"id":"evt_unknown_invalid","object":"event","type":"payment_intent.succeeded","data":{"object":{"metadata":{"orderId":"sub2_unknown_invalid"}}}}`
	signature := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: []byte(rawBody),
		Secret:  "whsec_wrong",
	}).Header
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/stripe", bytes.NewBufferString(rawBody))
	req.Header.Set("Stripe-Signature", signature)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.StripeWebhook(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "verify failed")
}

func TestVerifyNotificationWithProvidersReturnsMatchedProvider(t *testing.T) {
	firstErr := errors.New("wrong provider")
	providers := []payment.Provider{
		webhookHandlerProviderStub{
			key:       payment.TypeWxpay,
			verifyErr: firstErr,
		},
		webhookHandlerProviderStub{
			key: payment.TypeWxpay,
			notification: &payment.PaymentNotification{
				OrderID: "sub2_42",
				TradeNo: "trade-42",
				Status:  payment.NotificationStatusSuccess,
			},
		},
	}

	providerKey, notification, err := verifyNotificationWithProviders(context.Background(), providers, "{}", map[string]string{"wechatpay-signature": "sig"})
	require.NoError(t, err)
	require.Equal(t, payment.TypeWxpay, providerKey)
	require.NotNil(t, notification)
	require.Equal(t, "sub2_42", notification.OrderID)
}

func TestVerifyNotificationWithProvidersFailsWhenAllProvidersReject(t *testing.T) {
	providers := []payment.Provider{
		webhookHandlerProviderStub{
			key:       payment.TypeWxpay,
			verifyErr: errors.New("verify failed a"),
		},
		webhookHandlerProviderStub{
			key:       payment.TypeWxpay,
			verifyErr: errors.New("verify failed b"),
		},
	}

	_, _, err := verifyNotificationWithProviders(context.Background(), providers, "{}", nil)
	require.Error(t, err)
}

func newWiseWebhookHandlerTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:wise_webhook_handler_"+dbName+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newWiseWebhookHandlerForTest(client *dbent.Client) *PaymentWebhookHandler {
	registry := payment.NewRegistry()
	loadBalancer := payment.NewDefaultLoadBalancer(client, []byte(wiseWebhookHandlerTestEncryptionKey))
	paymentSvc := service.NewPaymentService(client, registry, loadBalancer, nil, nil, nil, nil, nil, nil)
	return NewPaymentWebhookHandler(paymentSvc, registry)
}

func newStripeWebhookHandlerForTest(t *testing.T, provider payment.Provider) *PaymentWebhookHandler {
	t.Helper()
	client := newWiseWebhookHandlerTestClient(t)
	registry := payment.NewRegistry()
	registry.Register(provider)
	loadBalancer := payment.NewDefaultLoadBalancer(client, []byte(wiseWebhookHandlerTestEncryptionKey))
	paymentSvc := service.NewPaymentService(client, registry, loadBalancer, nil, nil, nil, nil, nil, nil)
	return NewPaymentWebhookHandler(paymentSvc, registry)
}

func createWiseWebhookHandlerProvider(t *testing.T, ctx context.Context, client *dbent.Client) *rsa.PrivateKey {
	t.Helper()

	priv, publicKeyPEM := newWiseWebhookHandlerKey(t)
	encryptedConfig := encryptWiseWebhookHandlerConfig(t, map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-123",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   publicKeyPEM,
		"settlementStrategy": "exact_only",
	})
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-webhook-handler").
		SetConfig(encryptedConfig).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	return priv
}

func postWiseWebhookHandlerRequest(t *testing.T, handler *PaymentWebhookHandler, priv *rsa.PrivateKey, rawBody, signBody, deliveryID string, testNotification bool) *httptest.ResponseRecorder {
	t.Helper()

	if signBody == "" {
		signBody = rawBody
	}
	req := httptest.NewRequest(http.MethodPost, "/wise", bytes.NewBufferString(rawBody))
	req.Header.Set("X-Signature-Sha256", signWiseWebhookHandlerBody(t, priv, signBody))
	req.Header.Set("X-Delivery-Id", deliveryID)
	if testNotification {
		req.Header.Set("X-Test-Notification", "true")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	handler.WiseWebhook(c)
	return w
}

func encryptWiseWebhookHandlerConfig(t *testing.T, config map[string]string) string {
	t.Helper()

	data, err := json.Marshal(config)
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(data), []byte(wiseWebhookHandlerTestEncryptionKey))
	require.NoError(t, err)
	return encrypted
}

func newWiseWebhookHandlerKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	return priv, string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}))
}

func signWiseWebhookHandlerBody(t *testing.T, priv *rsa.PrivateKey, rawBody string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(rawBody))
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(signature)
}

type webhookHandlerProviderStub struct {
	key          string
	notification *payment.PaymentNotification
	verifyErr    error
}

func (p webhookHandlerProviderStub) Name() string        { return p.key }
func (p webhookHandlerProviderStub) ProviderKey() string { return p.key }
func (p webhookHandlerProviderStub) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.PaymentType(p.key)}
}
func (p webhookHandlerProviderStub) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}
func (p webhookHandlerProviderStub) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	panic("unexpected call")
}
func (p webhookHandlerProviderStub) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	if p.verifyErr != nil {
		return nil, p.verifyErr
	}
	return p.notification, nil
}
func (p webhookHandlerProviderStub) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}
