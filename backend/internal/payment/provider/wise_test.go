//go:build unit

package provider

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCreateProviderWise(t *testing.T) {
	t.Parallel()

	prov, err := CreateProvider(payment.TypeWise, "1", map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-123",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   testWisePublicKeyPEM,
		"settlementStrategy": "exact_only",
	})
	require.NoError(t, err)
	require.Equal(t, payment.TypeWise, prov.ProviderKey())
	require.Equal(t, []payment.PaymentType{payment.TypeWise}, prov.SupportedTypes())
}

const testWisePublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwj/6fR4W9HutC0Dh9CDk
gOmqZp3esJwprRXb1p6BV9kPfLQOELutQKiqSgNZW5eSKrYpR4xZJg1Aht2nDHNH
RceCKJxaj0QyJzUtMuD1qH9OGqHMWwctQJaMYhzGzS8xZpblNf3WFYsD+iHqJq1E
0U1t6fXB+QwBfQkM3RI/cDyJiH2+trTcTHFIAxchkw+Q2U8BfV02+U5tNNfrjxpt
xHVEH3qPXUgoSQP6sE8MT2mSOjrU8bS7dK7gxsVDjLMsMyHIyASenxcrS5U1XyXm
8Xx+uJtD3Z3RCRlDa67S0twTP+w6S0s2BguMQ8s0F+BAFfZf4d1FiaNmN7uwqblP
NQIDAQAB
-----END PUBLIC KEY-----`

func TestNewWiseValidatesConfig(t *testing.T) {
	t.Parallel()

	base := validWiseConfig()

	for _, key := range []string{"quickPayBaseUrl", "apiToken", "profileId", "balanceId", "webhookPublicKey"} {
		cfg := cloneStringMap(base)
		cfg[key] = ""
		_, err := NewWise("1", cfg)
		require.ErrorContains(t, err, key)
	}

	cfg := cloneStringMap(base)
	cfg["quickPayBaseUrl"] = "http://wise.com/pay/business/account"
	_, err := NewWise("1", cfg)
	require.ErrorContains(t, err, "quickPayBaseUrl must be an HTTPS URL")

	cfg = cloneStringMap(base)
	cfg["apiBase"] = "http://api.wise.com"
	_, err = NewWise("1", cfg)
	require.ErrorContains(t, err, "apiBase must be an HTTPS URL")

	cfg = cloneStringMap(base)
	cfg["currency"] = "US"
	_, err = NewWise("1", cfg)
	require.ErrorContains(t, err, "currency")

	cfg = cloneStringMap(base)
	cfg["settlementStrategy"] = "gross_with_fee"
	_, err = NewWise("1", cfg)
	require.ErrorContains(t, err, "exact_only")
}

func TestNewWiseRejectsInvalidWebhookPublicKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		publicKey  func(t *testing.T) string
		wantErrMsg string
	}{
		{
			name: "malformed PEM",
			publicKey: func(t *testing.T) string {
				t.Helper()
				return "not a pem public key"
			},
			wantErrMsg: "webhookPublicKey must be PEM encoded",
		},
		{
			name: "non-RSA PKIX public key",
			publicKey: func(t *testing.T) string {
				t.Helper()
				return testWiseECDSAPublicKeyPEM(t)
			},
			wantErrMsg: "webhookPublicKey must be an RSA public key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validWiseConfig()
			cfg["webhookPublicKey"] = tt.publicKey(t)
			_, err := NewWise("1", cfg)
			require.ErrorContains(t, err, tt.wantErrMsg)
		})
	}
}

func validWiseConfig() map[string]string {
	return map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-123",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "usd",
		"webhookPublicKey":   testWisePublicKeyPEM,
		"settlementStrategy": "exact_only",
	}
}

func testWiseECDSAPublicKeyPEM(t *testing.T) string {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}))
}

func TestWiseVerifyNotificationVerifiesRSASignature(t *testing.T) {
	t.Parallel()

	prov, priv := newWiseWebhookTestProvider(t)
	rawBody := `{"event_type":"balances#credit","data":{"resource":{"id":"transfer-123","profile_id":"profile-123","account_id":"balance-123","type":"balance"}}}`
	headers := map[string]string{
		"x-signature-sha256": testWiseWebhookSignature(t, priv, rawBody),
	}

	notification, err := prov.VerifyNotification(context.Background(), rawBody, headers)
	require.NoError(t, err)
	require.Nil(t, notification)
}

func TestWiseVerifyNotificationRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		signature func(t *testing.T, priv *rsa.PrivateKey, rawBody string) string
	}{
		{
			name: "invalid base64",
			signature: func(t *testing.T, _ *rsa.PrivateKey, _ string) string {
				t.Helper()
				return "not-base64!"
			},
		},
		{
			name: "mismatched signature",
			signature: func(t *testing.T, priv *rsa.PrivateKey, _ string) string {
				t.Helper()
				return testWiseWebhookSignature(t, priv, `{"event_type":"balances#update"}`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prov, priv := newWiseWebhookTestProvider(t)
			rawBody := `{"event_type":"balances#credit"}`
			headers := map[string]string{
				"x-signature-sha256": tt.signature(t, priv, rawBody),
			}

			notification, err := prov.VerifyNotification(context.Background(), rawBody, headers)
			require.ErrorContains(t, err, "invalid signature")
			require.Nil(t, notification)
		})
	}
}

func TestWiseVerifyNotificationRejectsMissingSignature(t *testing.T) {
	t.Parallel()

	prov, _ := newWiseWebhookTestProvider(t)

	notification, err := prov.VerifyNotification(context.Background(), `{"event_type":"balances#credit"}`, nil)
	require.ErrorContains(t, err, "missing x-signature-sha256")
	require.Nil(t, notification)
}

func TestWiseVerifyNotificationIgnoresUnsupportedEventWithValidSignature(t *testing.T) {
	t.Parallel()

	prov, priv := newWiseWebhookTestProvider(t)
	rawBody := `{"event_type":"unsupported#event","data":{"resource":{"id":"resource-123"}}}`
	headers := map[string]string{
		"x-signature-sha256": testWiseWebhookSignature(t, priv, rawBody),
	}

	notification, err := prov.VerifyNotification(context.Background(), rawBody, headers)
	require.NoError(t, err)
	require.Nil(t, notification)
}

func TestWiseVerifyNotificationRejectsMalformedJSONWithValidSignature(t *testing.T) {
	t.Parallel()

	prov, priv := newWiseWebhookTestProvider(t)
	rawBody := `{"event_type":`
	headers := map[string]string{
		"x-signature-sha256": testWiseWebhookSignature(t, priv, rawBody),
	}

	notification, err := prov.VerifyNotification(context.Background(), rawBody, headers)
	require.ErrorContains(t, err, "wise parse webhook")
	require.Nil(t, notification)
}

func newWiseWebhookTestProvider(t *testing.T) (*Wise, *rsa.PrivateKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := validWiseConfig()
	cfg["webhookPublicKey"] = testWiseRSAPublicKeyPEM(t, &priv.PublicKey)
	prov, err := NewWise("1", cfg)
	require.NoError(t, err)
	return prov, priv
}

func testWiseRSAPublicKeyPEM(t *testing.T, publicKey *rsa.PublicKey) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(publicKey)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}))
}

func testWiseWebhookSignature(t *testing.T, priv *rsa.PrivateKey, rawBody string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(rawBody))
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(signature)
}

func TestWiseCreatePaymentBuildsQuickPayURL(t *testing.T) {
	t.Parallel()

	prov, err := NewWise("1", validWiseConfig())
	require.NoError(t, err)

	resp, err := prov.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "sub2_20260612AbCdEf12",
		Amount:  "123.45",
	})
	require.NoError(t, err)
	require.Equal(t, "sub2_20260612AbCdEf12", resp.TradeNo)
	require.Equal(t, "USD", resp.Currency)
	require.Equal(t, payment.CreatePaymentResultOrderCreated, resp.ResultType)

	parsed, err := url.Parse(resp.PayURL)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "wise.com", parsed.Host)
	require.Equal(t, "/pay/business/account", parsed.Path)
	require.Equal(t, "123.45", parsed.Query().Get("amount"))
	require.Equal(t, "USD", parsed.Query().Get("currency"))
	require.Equal(t, "sub2_20260612AbCdEf12", parsed.Query().Get("description"))
}

func TestWiseCreatePaymentMergesExistingQuickPayQuery(t *testing.T) {
	t.Parallel()

	cfg := validWiseConfig()
	cfg["quickPayBaseUrl"] = "https://wise.com/pay/business/account?locale=en"
	prov, err := NewWise("1", cfg)
	require.NoError(t, err)

	resp, err := prov.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "sub2_order",
		Amount:  "88",
	})
	require.NoError(t, err)
	parsed, err := url.Parse(resp.PayURL)
	require.NoError(t, err)
	require.Equal(t, "en", parsed.Query().Get("locale"))
	require.Equal(t, "88", parsed.Query().Get("amount"))
	require.Equal(t, "USD", parsed.Query().Get("currency"))
	require.Equal(t, "sub2_order", parsed.Query().Get("description"))
}

func TestWiseCreatePaymentRejectsNonPositiveAmount(t *testing.T) {
	t.Parallel()

	prov, err := NewWise("1", validWiseConfig())
	require.NoError(t, err)

	for _, amount := range []string{"0", "-1"} {
		t.Run(amount, func(t *testing.T) {
			t.Parallel()

			_, err := prov.CreatePayment(context.Background(), payment.CreatePaymentRequest{
				OrderID: "sub2_order",
				Amount:  amount,
			})
			require.ErrorContains(t, err, "amount must be positive")
		})
	}
}

func TestWiseQueryOrderFindsCompletedCreditStatementTransaction(t *testing.T) {
	const outTradeNo = "sub2_20260612AbCdEf12"
	statement := `{
		"transactions": [
			{
				"type": "CREDIT",
				"date": "2026-06-12T10:20:30Z",
				"referenceNumber": "wise-ref-1",
				"amount": {"value": "123.45", "currency": "usd"},
				"totalFees": {"value": "0", "currency": "USD"},
				"details": {
					"type": "DEPOSIT",
					"description": "Payment received",
					"paymentReference": "sub2_20260612AbCdEf12"
				}
			}
		]
	}`
	prov, req, cleanup := newWiseStatementTestProvider(t, statement)
	defer cleanup()

	resp, err := prov.QueryOrder(context.Background(), outTradeNo)
	require.NoError(t, err)

	require.Equal(t, "/v1/profiles/profile-123/balance-statements/balance-123/statement.json", req.path)
	require.Equal(t, "Bearer token-123", req.authorization)
	require.Equal(t, "application/json", req.accept)
	require.Equal(t, "COMPACT", req.query.Get("type"))
	require.Equal(t, "USD", req.query.Get("currency"))
	assertWiseStatementIntervalQuery(t, req.query)
	require.Equal(t, outTradeNo, resp.TradeNo)
	require.Equal(t, payment.ProviderStatusPaid, resp.Status)
	require.InDelta(t, 123.45, resp.Amount, 0.000001)
	require.Equal(t, "USD", resp.Metadata["currency"])
	require.Equal(t, "balance-123", resp.Metadata["balance_id"])
	require.Equal(t, "profile-123", resp.Metadata["profile_id"])
	require.Equal(t, "wise-ref-1", resp.Metadata["wise_transaction_id"])
	require.Equal(t, "exact_only", resp.Metadata["settlement_strategy"])
	require.Equal(t, "auto_fulfill", resp.Metadata["reconcile_decision"])
}

func TestWiseQueryOrderReturnsPaidWithFeeAndNetAmountMetadata(t *testing.T) {
	const outTradeNo = "sub2_20260612AbCdEf12"
	statement := `{
		"transactions": [
			{
				"type": "credit",
				"date": "2026-06-12T10:20:30Z",
				"referenceNumber": "wise-ref-fee",
				"amount": {"value": "120.00", "currency": "USD"},
				"totalFees": {"value": "3.45", "currency": "USD"},
				"details": {
					"type": "DEPOSIT",
					"description": "Customer paid order",
					"reference": "sub2_20260612AbCdEf12"
				}
			}
		]
	}`
	prov, _, cleanup := newWiseStatementTestProvider(t, statement)
	defer cleanup()

	resp, err := prov.QueryOrder(context.Background(), outTradeNo)
	require.NoError(t, err)

	require.Equal(t, outTradeNo, resp.TradeNo)
	require.Equal(t, payment.ProviderStatusPaid, resp.Status)
	require.InDelta(t, 120.00, resp.Amount, 0.000001)
	require.Equal(t, "wise-ref-fee", resp.Metadata["wise_transaction_id"])
	require.Equal(t, "3.45", resp.Metadata["fee_amount"])
	require.Equal(t, "120", resp.Metadata["net_amount"])
	require.Equal(t, "123.45", resp.Metadata["gross_amount"])
	require.Equal(t, "auto_fulfill", resp.Metadata["reconcile_decision"])
}

func TestWiseQueryOrderReturnsPendingWhenStatementHasNoSafeOrderMatch(t *testing.T) {
	const outTradeNo = "sub2_20260612AbCdEf12"
	statement := `{
		"transactions": [
			{
				"type": "credit",
				"date": "2026-06-12T10:20:30Z",
				"referenceNumber": "wise-ref-other",
				"amount": {"value": "123.45", "currency": "USD"},
				"totalFees": {"value": "0", "currency": "USD"},
				"details": {
					"type": "DEPOSIT",
					"description": "Payment for sub2_20260612AbCdEf123",
					"reference": "sub2_20260612AbCdEf123"
				}
			}
		]
	}`
	prov, _, cleanup := newWiseStatementTestProvider(t, statement)
	defer cleanup()

	resp, err := prov.QueryOrder(context.Background(), outTradeNo)
	require.NoError(t, err)

	require.Equal(t, outTradeNo, resp.TradeNo)
	require.Equal(t, payment.ProviderStatusPending, resp.Status)
	require.Zero(t, resp.Amount)
	require.Equal(t, "no_match", resp.Metadata["reconcile_decision"])
}

func TestWiseQueryOrderRejectsEmptyTradeNo(t *testing.T) {
	t.Parallel()

	prov, err := NewWise("1", validWiseConfig())
	require.NoError(t, err)

	_, err = prov.QueryOrder(context.Background(), " ")
	require.ErrorContains(t, err, "missing order reference")
}

func TestWiseDoJSONReturnsNon2xxSummary(t *testing.T) {
	t.Parallel()

	prov, cleanup := newWiseDoJSONTestProvider(t, http.StatusBadGateway, " upstream unavailable ")
	defer cleanup()

	var out wiseStatementResponse
	err := prov.doJSON(context.Background(), http.MethodGet, "/fail", &out)
	require.ErrorContains(t, err, "wise HTTP 502: upstream unavailable")
}

func TestWiseDoJSONTruncatesNon2xxSummary(t *testing.T) {
	t.Parallel()

	prov, cleanup := newWiseDoJSONTestProvider(t, http.StatusTeapot, strings.Repeat("x", wiseMaxErrorSummaryBytes+20))
	defer cleanup()

	var out wiseStatementResponse
	err := prov.doJSON(context.Background(), http.MethodGet, "/fail", &out)
	require.Error(t, err)
	require.Len(t, strings.TrimPrefix(err.Error(), "wise HTTP 418: "), wiseMaxErrorSummaryBytes)
}

func TestWiseDoJSONReturnsMalformedJSONError(t *testing.T) {
	t.Parallel()

	prov, cleanup := newWiseDoJSONTestProvider(t, http.StatusOK, `{"transactions":`)
	defer cleanup()

	var out wiseStatementResponse
	err := prov.doJSON(context.Background(), http.MethodGet, "/statement.json", &out)
	require.ErrorContains(t, err, "wise parse response")
}

type wiseStatementTestRequest struct {
	path          string
	authorization string
	accept        string
	query         url.Values
}

func newWiseStatementTestProvider(t *testing.T, statement string) (*Wise, *wiseStatementTestRequest, func()) {
	t.Helper()

	req := &wiseStatementTestRequest{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req.path = r.URL.Path
		req.authorization = r.Header.Get("Authorization")
		req.accept = r.Header.Get("Accept")
		req.query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(statement))
	}))

	cfg := validWiseConfig()
	cfg["apiBase"] = server.URL
	prov, err := NewWise("1", cfg)
	require.NoError(t, err)
	prov.httpClient = server.Client()

	return prov, req, server.Close
}

func newWiseDoJSONTestProvider(t *testing.T, statusCode int, body string) (*Wise, func()) {
	t.Helper()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))

	cfg := validWiseConfig()
	cfg["apiBase"] = server.URL
	prov, err := NewWise("1", cfg)
	require.NoError(t, err)
	prov.httpClient = server.Client()

	return prov, server.Close
}

func assertWiseStatementIntervalQuery(t *testing.T, query url.Values) {
	t.Helper()

	intervalStart, err := time.Parse(time.RFC3339, query.Get("intervalStart"))
	require.NoError(t, err)
	intervalEnd, err := time.Parse(time.RFC3339, query.Get("intervalEnd"))
	require.NoError(t, err)

	require.InDelta(t, (wiseStatementLookback + wiseStatementLookahead).Seconds(), intervalEnd.Sub(intervalStart).Seconds(), 1)
}

func TestWiseTransactionReferencesOrder(t *testing.T) {
	t.Parallel()

	const outTradeNo = "sub2_20260612AbCdEf12"
	tests := []struct {
		name string
		tx   wiseTransaction
		want bool
	}{
		{
			name: "exact reference matches",
			tx: wiseTransaction{
				Reference: outTradeNo,
			},
			want: true,
		},
		{
			name: "description with surrounding text matches",
			tx: wiseTransaction{
				Description: "Payment for sub2_20260612AbCdEf12",
			},
			want: true,
		},
		{
			name: "prefix collision does not match",
			tx: wiseTransaction{
				Description: "Payment for sub2_20260612AbCdEf123",
				Reference:   "sub2_20260612AbCdEf123",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, wiseTransactionReferencesOrder(tt.tx, outTradeNo))
		})
	}
}

func TestWiseExactSettlementStrategy(t *testing.T) {
	t.Parallel()

	strategy := wiseExactSettlementStrategy{}
	order := wiseOrderContext{
		OutTradeNo: "sub2_20260612AbCdEf12",
		PayAmount:  decimal.RequireFromString("123.45"),
		Currency:   "USD",
		ProfileID:  "profile-123",
		BalanceID:  "balance-123",
	}
	tx := wiseTransaction{
		ID:          "wise-tx-1",
		ProfileID:   "profile-123",
		BalanceID:   "balance-123",
		Direction:   "credit",
		Status:      "completed",
		Currency:    "USD",
		NetAmount:   decimal.RequireFromString("123.45"),
		Description: "Invoice sub2_20260612AbCdEf12",
	}

	decision := strategy.Match(order, tx)
	require.True(t, decision.Matched)
	require.True(t, decision.AutoFulfill)
	require.Equal(t, "exact_only", strategy.Name())
	require.Equal(t, "123.45", decision.NetAmount.StringFixed(2))
	require.True(t, decision.FeeAmount.IsZero())
}

func TestWiseExactSettlementStrategyRequiresExactNetAmount(t *testing.T) {
	t.Parallel()

	strategy := wiseExactSettlementStrategy{}
	order := wiseOrderContext{
		OutTradeNo: "sub2_20260612AbCdEf12",
		PayAmount:  decimal.RequireFromString("123.45"),
		Currency:   "USD",
		ProfileID:  "profile-123",
		BalanceID:  "balance-123",
	}
	tx := wiseTransaction{
		ID:          "wise-tx-1",
		ProfileID:   "profile-123",
		BalanceID:   "balance-123",
		Direction:   "credit",
		Status:      "completed",
		Currency:    "USD",
		NetAmount:   decimal.RequireFromString("120.00"),
		Description: "Invoice sub2_20260612AbCdEf12",
	}

	decision := strategy.Match(order, tx)
	require.True(t, decision.Matched)
	require.False(t, decision.AutoFulfill)
	require.Equal(t, "amount_mismatch", decision.Reason)
}

func TestWiseExactSettlementStrategyTreatsZeroPayAmountAsUnknown(t *testing.T) {
	t.Parallel()

	strategy := wiseExactSettlementStrategy{}
	order := wiseOrderContext{
		OutTradeNo: "sub2_20260612AbCdEf12",
		Currency:   "USD",
		ProfileID:  "profile-123",
		BalanceID:  "balance-123",
	}
	tx := wiseTransaction{
		ID:          "wise-tx-1",
		ProfileID:   "profile-123",
		BalanceID:   "balance-123",
		Direction:   "credit",
		Status:      "completed",
		Currency:    "USD",
		NetAmount:   decimal.RequireFromString("120.00"),
		FeeAmount:   decimal.RequireFromString("3.45"),
		Description: "Invoice sub2_20260612AbCdEf12",
	}

	decision := strategy.Match(order, tx)
	require.True(t, decision.Matched)
	require.True(t, decision.AutoFulfill)
	require.Equal(t, "exact_match", decision.Reason)
	require.Equal(t, "120", decision.NetAmount.String())
	require.Equal(t, "3.45", decision.FeeAmount.String())
}

func TestWiseExactSettlementStrategyRejectsMismatchedSettlementFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*wiseOrderContext, *wiseTransaction)
		wantReason string
	}{
		{
			name: "profile mismatch",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.ProfileID = "profile-other"
			},
			wantReason: "profile_mismatch",
		},
		{
			name: "balance mismatch",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.BalanceID = "balance-other"
			},
			wantReason: "balance_mismatch",
		},
		{
			name: "currency mismatch",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.Currency = "EUR"
			},
			wantReason: "currency_mismatch",
		},
		{
			name: "direction not credit",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.Direction = "debit"
			},
			wantReason: "direction_not_credit",
		},
		{
			name: "status not completed",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.Status = "pending"
			},
			wantReason: "status_not_completed",
		},
		{
			name: "missing out trade number reference",
			mutate: func(_ *wiseOrderContext, tx *wiseTransaction) {
				tx.Description = "Invoice unrelated"
				tx.Reference = "wise-reference"
			},
			wantReason: "reference_mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			strategy := wiseExactSettlementStrategy{}
			order := wiseOrderContext{
				OutTradeNo: "sub2_20260612AbCdEf12",
				PayAmount:  decimal.RequireFromString("123.45"),
				Currency:   "USD",
				ProfileID:  "profile-123",
				BalanceID:  "balance-123",
			}
			tx := wiseTransaction{
				ID:          "wise-tx-1",
				ProfileID:   "profile-123",
				BalanceID:   "balance-123",
				Direction:   "credit",
				Status:      "completed",
				Currency:    "USD",
				NetAmount:   decimal.RequireFromString("123.45"),
				Description: "Invoice sub2_20260612AbCdEf12",
				Reference:   "sub2_20260612AbCdEf12",
			}
			tt.mutate(&order, &tx)

			decision := strategy.Match(order, tx)
			require.False(t, decision.Matched)
			require.False(t, decision.AutoFulfill)
			require.Equal(t, tt.wantReason, decision.Reason)
		})
	}
}
