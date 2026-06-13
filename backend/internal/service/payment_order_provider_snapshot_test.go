//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBuildPaymentOrderProviderSnapshot_ExcludesSensitiveConfig(t *testing.T) {
	t.Parallel()

	sel := &payment.InstanceSelection{
		InstanceID:     "12",
		ProviderKey:    payment.TypeWxpay,
		SupportedTypes: "wxpay,wxpay_direct",
		PaymentMode:    "popup",
		Config: map[string]string{
			"privateKey": "secret",
			"apiV3Key":   "secret-v3",
			"appId":      "wx-app-id",
		},
	}

	snapshot := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{})
	require.Equal(t, map[string]any{
		"schema_version":       2,
		"provider_instance_id": "12",
		"provider_key":         payment.TypeWxpay,
		"payment_mode":         "popup",
		"merchant_app_id":      "wx-app-id",
		"currency":             "CNY",
	}, snapshot)
	require.NotContains(t, snapshot, "config")
	require.NotContains(t, snapshot, "privateKey")
	require.NotContains(t, snapshot, "apiV3Key")
	require.NotContains(t, snapshot, "supported_types")
	require.NotContains(t, snapshot, "instance_name")
	require.NotContains(t, snapshot, "merchant_id")
}

func TestCreateOrderInTx_WritesProviderSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("snapshot@example.com").
		SetPasswordHash("hash").
		SetUsername("snapshot-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("Primary Alipay").
		SetConfig(`{"secretKey":"do-not-copy"}`).
		SetSupportedTypes("alipay,alipay_direct").
		SetPaymentMode("redirect").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(
		ctx,
		CreateOrderRequest{
			UserID:      user.ID,
			PaymentType: payment.TypeAlipay,
			OrderType:   payment.OrderTypeBalance,
			ClientIP:    "127.0.0.1",
			SrcHost:     "app.example.com",
		},
		&User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
		},
		nil,
		&PaymentConfig{
			MaxPendingOrders: 3,
			OrderTimeoutMin:  30,
		},
		88,
		88,
		0,
		88,
		&payment.InstanceSelection{
			InstanceID:     strconv.FormatInt(instance.ID, 10),
			ProviderKey:    payment.TypeAlipay,
			SupportedTypes: "alipay,alipay_direct",
			PaymentMode:    "redirect",
			Config: map[string]string{
				"secretKey": "do-not-copy",
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(instance.ID, 10), valueOrEmpty(order.ProviderInstanceID))
	require.Equal(t, payment.TypeAlipay, valueOrEmpty(order.ProviderKey))
	require.Equal(t, float64(2), order.ProviderSnapshot["schema_version"])
	require.Equal(t, strconv.FormatInt(instance.ID, 10), order.ProviderSnapshot["provider_instance_id"])
	require.Equal(t, payment.TypeAlipay, order.ProviderSnapshot["provider_key"])
	require.Equal(t, "redirect", order.ProviderSnapshot["payment_mode"])
	require.NotContains(t, order.ProviderSnapshot, "config")
	require.NotContains(t, order.ProviderSnapshot, "secretKey")
	require.NotContains(t, order.ProviderSnapshot, "supported_types")
	require.NotContains(t, order.ProviderSnapshot, "instance_name")
}

func TestBuildPaymentOrderProviderSnapshot_UsesWxpayJSAPIAppIDForOpenIDOrders(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "88",
		ProviderKey: payment.TypeWxpay,
		Config: map[string]string{
			"appId":   "wx-open-app",
			"mpAppId": "wx-mp-app",
			"mchId":   "mch-88",
		},
		PaymentMode: "jsapi",
	}, CreateOrderRequest{OpenID: "openid-123"})

	require.Equal(t, "wx-mp-app", snapshot["merchant_app_id"])
	require.Equal(t, "mch-88", snapshot["merchant_id"])
	require.Equal(t, "CNY", snapshot["currency"])
}

func TestBuildPaymentOrderProviderSnapshot_IncludesAlipayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "21",
		ProviderKey: payment.TypeAlipay,
		Config: map[string]string{
			"appId":      "alipay-app-21",
			"privateKey": "secret",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{})

	require.Equal(t, "alipay-app-21", snapshot["merchant_app_id"])
	require.NotContains(t, snapshot, "privateKey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesEasyPayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "66",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid":  "easypay-merchant-66",
			"pkey": "secret",
		},
		PaymentMode: "popup",
	}, CreateOrderRequest{PaymentType: payment.TypeAlipay})

	require.Equal(t, "easypay-merchant-66", snapshot["merchant_id"])
	require.NotContains(t, snapshot, "pkey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesProviderCurrency(t *testing.T) {
	t.Parallel()

	stripeSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "77",
		ProviderKey: payment.TypeStripe,
		Config: map[string]string{
			"currency": "hkd",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "HKD", stripeSnapshot["currency"])

	airwallexSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "78",
		ProviderKey: payment.TypeAirwallex,
		Config: map[string]string{
			"currency":  "usd",
			"accountId": "acct-78",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "USD", airwallexSnapshot["currency"])
	require.Equal(t, "acct-78", airwallexSnapshot["merchant_id"])
}

func TestBuildPaymentOrderProviderSnapshotWise(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "88",
		ProviderKey: payment.TypeWise,
		Config: map[string]string{
			"profileId":          "profile-123",
			"balanceId":          "balance-123",
			"currency":           "USD",
			"settlementStrategy": "exact_only",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{PaymentType: payment.TypeWise})

	require.Equal(t, 2, snapshot["schema_version"])
	require.Equal(t, payment.TypeWise, snapshot["provider_key"])
	require.Equal(t, "88", snapshot["provider_instance_id"])
	require.Equal(t, "redirect", snapshot["payment_mode"])
	require.Equal(t, "profile-123", snapshot["merchant_id"])
	require.Equal(t, "balance-123", snapshot["balance_id"])
	require.Equal(t, "USD", snapshot["currency"])
	require.Equal(t, "exact_only", snapshot["settlement_strategy"])

	defaultStrategySnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "89",
		ProviderKey: payment.TypeWise,
		Config: map[string]string{
			"profileId": "profile-456",
			"balanceId": "balance-456",
			"currency":  "EUR",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{PaymentType: payment.TypeWise})
	require.Equal(t, "exact_only", defaultStrategySnapshot["settlement_strategy"])
}

func TestValidateProviderSnapshotMetadataWise(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		ProviderSnapshot: map[string]any{
			"schema_version":       2,
			"provider_key":         payment.TypeWise,
			"merchant_id":          "profile-123",
			"balance_id":           "balance-123",
			"currency":             "USD",
			"settlement_strategy":  "exact_only",
			"provider_instance_id": "88",
		},
	}

	require.NoError(t, validateProviderSnapshotMetadata(order, payment.TypeWise, map[string]string{
		"profile_id":          "profile-123",
		"balance_id":          "balance-123",
		"currency":            "USD",
		"settlement_strategy": "exact_only",
	}))
	require.NoError(t, validateProviderSnapshotMetadata(order, payment.TypeWise, map[string]string{
		"merchant_id":         "profile-123",
		"balance_id":          "balance-123",
		"currency":            "usd",
		"settlement_strategy": "exact_only",
	}))

	tests := []struct {
		name    string
		patch   map[string]string
		wantErr string
	}{
		{
			name: "profile mismatch",
			patch: map[string]string{
				"profile_id": "profile-999",
			},
			wantErr: "wise profile_id mismatch",
		},
		{
			name: "balance mismatch",
			patch: map[string]string{
				"balance_id": "balance-999",
			},
			wantErr: "wise balance_id mismatch",
		},
		{
			name: "currency mismatch",
			patch: map[string]string{
				"currency": "EUR",
			},
			wantErr: "wise currency mismatch",
		},
		{
			name: "settlement strategy mismatch",
			patch: map[string]string{
				"settlement_strategy": "gross_with_fee",
			},
			wantErr: "wise settlement_strategy mismatch",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metadata := map[string]string{
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
			}
			for key, value := range tt.patch {
				metadata[key] = value
			}
			require.ErrorContains(t, validateProviderSnapshotMetadata(order, payment.TypeWise, metadata), tt.wantErr)
		})
	}
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
