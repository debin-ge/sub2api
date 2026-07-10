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
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
