//go:build unit

package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"

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
