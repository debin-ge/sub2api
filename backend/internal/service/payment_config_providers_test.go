//go:build unit

package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderwebhooksubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProviderRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		providerKey    string
		providerName   string
		supportedTypes string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "valid easypay with types",
			providerKey:    "easypay",
			providerName:   "MyProvider",
			supportedTypes: "alipay,wxpay",
			wantErr:        false,
		},
		{
			name:           "valid stripe with empty types",
			providerKey:    "stripe",
			providerName:   "Stripe Provider",
			supportedTypes: "",
			wantErr:        false,
		},
		{
			name:           "valid airwallex provider",
			providerKey:    payment.TypeAirwallex,
			providerName:   "Airwallex Provider",
			supportedTypes: payment.TypeAirwallex,
			wantErr:        false,
		},
		{
			name:           "valid alipay provider",
			providerKey:    "alipay",
			providerName:   "Alipay Direct",
			supportedTypes: "alipay",
			wantErr:        false,
		},
		{
			name:           "valid wxpay provider",
			providerKey:    "wxpay",
			providerName:   "WeChat Pay",
			supportedTypes: "wxpay",
			wantErr:        false,
		},
		{
			name:           "valid wise provider",
			providerKey:    payment.TypeWise,
			providerName:   "Wise Provider",
			supportedTypes: payment.TypeWise,
			wantErr:        false,
		},
		{
			name:           "wise rejects future wallet method",
			providerKey:    payment.TypeWise,
			providerName:   "Wise Provider",
			supportedTypes: "wise,card",
			wantErr:        true,
			errContains:    "wise supports only wise payment type",
		},
		{
			name:           "invalid provider key",
			providerKey:    "invalid",
			providerName:   "Name",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "invalid provider key",
		},
		{
			name:           "empty name",
			providerKey:    "easypay",
			providerName:   "",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
		{
			name:           "whitespace-only name",
			providerKey:    "easypay",
			providerName:   "  ",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
		{
			name:           "tab-only name",
			providerKey:    "easypay",
			providerName:   "\t",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateProviderRequest(tc.providerKey, tc.providerName, tc.supportedTypes)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWiseProviderConfigRegistration(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateProviderRequest(payment.TypeWise, "Wise Provider", payment.TypeWise))
	require.True(t, isSensitiveProviderConfigField(payment.TypeWise, "apiToken"))
	require.True(t, isSensitiveProviderConfigField(payment.TypeWise, "apitoken"))
	require.False(t, isSensitiveProviderConfigField(payment.TypeWise, "quickPayBaseUrl"))
	require.False(t, isSensitiveProviderConfigField(payment.TypeWise, "webhookPublicKey"))
}

func TestWiseProtectedConfigChanges(t *testing.T) {
	t.Parallel()

	current := map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "old-token",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   "old-public-key",
		"settlementStrategy": "exact_only",
	}
	for _, tc := range []struct {
		field string
		value string
	}{
		{field: "quickPayBaseUrl", value: "https://wise.com/pay/business/updated-account"},
		{field: "apiBase", value: "https://api.sandbox.transferwise.tech"},
		{field: "apiToken", value: "new-token"},
		{field: "profileId", value: "profile-456"},
		{field: "balanceId", value: "balance-456"},
		{field: "currency", value: "EUR"},
		{field: "webhookPublicKey", value: "new-public-key"},
		{field: "settlementStrategy", value: "updated-strategy"},
		{field: "reconcileWindowHours", value: "168"},
	} {
		tc := tc
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()

			next := cloneStringMap(current)
			next[tc.field] = tc.value
			require.True(t, hasPendingOrderProtectedConfigChange(payment.TypeWise, current, next))
		})
	}

	next := cloneStringMap(current)
	next["allowedMethodsNote"] = "bank transfer only"
	require.False(t, hasPendingOrderProtectedConfigChange(payment.TypeWise, current, next))
}

func TestListProviderInstancesWithWiseSubscriptionStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	wiseInst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "Wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        false,
		SortOrder:      1,
	})
	require.NoError(t, err)
	stripeInst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeStripe,
		Name:           "Stripe",
		Config:         validStripeProviderConfig(t),
		SupportedTypes: []string{payment.TypeStripe},
		Enabled:        false,
		SortOrder:      2,
	})
	require.NoError(t, err)

	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	_, err = client.PaymentProviderWebhookSubscription.Create().
		SetProviderInstanceID(wiseInst.ID).
		SetProviderKey(payment.TypeWise).
		SetExternalSubscriptionID("sub-old").
		SetTriggerOn(wiseWebhookSubscriptionTrigger).
		SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
		SetDeliveryURL("https://old.example.com/api/v1/payment/webhook/wise").
		SetStatus(wiseWebhookSubscriptionStatusFailed).
		SetLastError("old failure").
		SetSyncedAt(older).
		SetUpdatedAt(older).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderWebhookSubscription.Create().
		SetProviderInstanceID(wiseInst.ID).
		SetProviderKey(payment.TypeWise).
		SetExternalSubscriptionID("sub-current").
		SetTriggerOn(wiseWebhookSubscriptionTrigger).
		SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
		SetDeliveryURL("https://api.example.com/api/v1/payment/webhook/wise").
		SetStatus(wiseWebhookSubscriptionStatusActive).
		SetSyncedAt(newer).
		SetUpdatedAt(newer).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderWebhookSubscription.Create().
		SetProviderInstanceID(stripeInst.ID).
		SetProviderKey(payment.TypeWise).
		SetExternalSubscriptionID("sub-wrong-instance").
		SetTriggerOn(wiseWebhookSubscriptionTrigger).
		SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
		SetDeliveryURL("https://wrong.example.com/api/v1/payment/webhook/wise").
		SetStatus(wiseWebhookSubscriptionStatusFailed).
		SetLastError("wrong instance").
		SetUpdatedAt(newer.Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	resp, err := svc.ListProviderInstancesWithConfig(ctx)
	require.NoError(t, err)
	require.Len(t, resp, 2)

	require.Equal(t, int64(wiseInst.ID), resp[0].ID)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, resp[0].WebhookSubscriptionStatus)
	require.Equal(t, "sub-current", resp[0].WebhookSubscriptionID)
	require.Empty(t, resp[0].WebhookSubscriptionError)
	require.Equal(t, "https://api.example.com/api/v1/payment/webhook/wise", resp[0].WebhookDeliveryURL)

	require.Equal(t, int64(stripeInst.ID), resp[1].ID)
	require.Empty(t, resp[1].WebhookSubscriptionStatus)
	require.Empty(t, resp[1].WebhookSubscriptionID)
	require.Empty(t, resp[1].WebhookSubscriptionError)
	require.Empty(t, resp[1].WebhookDeliveryURL)

	count, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(wiseInst.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestIsSensitiveProviderConfigField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		providerKey string
		field       string
		wantSen     bool
	}{
		// Stripe: publishableKey is public, only secretKey/webhookSecret are secrets
		{"stripe", "secretKey", true},
		{"stripe", "webhookSecret", true},
		{"stripe", "SecretKey", true}, // case-insensitive
		{"stripe", "publishableKey", false},
		{"stripe", "currency", false},
		{"stripe", "appId", false},

		// Alipay
		{"alipay", "privateKey", true},
		{"alipay", "publicKey", true},
		{"alipay", "alipayPublicKey", true},
		{"alipay", "appId", false},
		{"alipay", "notifyUrl", false},

		// Wxpay
		{"wxpay", "privateKey", true},
		{"wxpay", "apiV3Key", true},
		{"wxpay", "publicKey", true},
		{"wxpay", "publicKeyId", false},
		{"wxpay", "certSerial", false},
		{"wxpay", "mchId", false},

		// EasyPay
		{"easypay", "pkey", true},
		{"easypay", "pid", false},
		{"easypay", "apiBase", false},

		// Airwallex
		{payment.TypeAirwallex, "apiKey", true},
		{payment.TypeAirwallex, "webhookSecret", true},
		{payment.TypeAirwallex, "clientId", false},
		{payment.TypeAirwallex, "apiBase", false},
		{payment.TypeAirwallex, "accountId", false},
		{payment.TypeAirwallex, "currency", false},

		// Unknown provider: never sensitive
		{"unknown", "secretKey", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.providerKey+"/"+tc.field, func(t *testing.T) {
			t.Parallel()

			got := isSensitiveProviderConfigField(tc.providerKey, tc.field)
			assert.Equal(t, tc.wantSen, got, "isSensitiveProviderConfigField(%q, %q)", tc.providerKey, tc.field)
		})
	}
}

func TestJoinTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "multiple types",
			input: []string{"alipay", "wxpay"},
			want:  "alipay,wxpay",
		},
		{
			name:  "single type",
			input: []string{"stripe"},
			want:  "stripe",
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  "",
		},
		{
			name:  "nil slice",
			input: nil,
			want:  "",
		},
		{
			name:  "three types",
			input: []string{"alipay", "wxpay", "stripe"},
			want:  "alipay,wxpay,stripe",
		},
		{
			name:  "types with spaces are not trimmed",
			input: []string{" alipay ", " wxpay "},
			want:  " alipay , wxpay ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := joinTypes(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCreateProviderInstanceAllowsVisibleMethodProvidersFromDifferentSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	_, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey: "easypay",
		Name:        "EasyPay Alipay",
		Config: map[string]string{
			"pid":       "1001",
			"pkey":      "pkey-1001",
			"apiBase":   "https://pay.example.com",
			"notifyUrl": "https://merchant.example.com/notify",
			"returnUrl": "https://merchant.example.com/return",
		},
		SupportedTypes: []string{"alipay"},
		Enabled:        true,
	})
	require.NoError(t, err)

	_, err = svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "alipay",
		Name:           "Official Alipay",
		Config:         map[string]string{"appId": "app-1", "privateKey": "private-key"},
		SupportedTypes: []string{"alipay"},
		Enabled:        true,
	})
	require.NoError(t, err)
}

func TestUpdateProviderInstanceAllowsEnablingVisibleMethodProviderFromDifferentSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	existing, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey: "easypay",
		Name:        "EasyPay WeChat",
		Config: map[string]string{
			"pid":       "2001",
			"pkey":      "pkey-2001",
			"apiBase":   "https://pay.example.com",
			"notifyUrl": "https://merchant.example.com/notify",
			"returnUrl": "https://merchant.example.com/return",
		},
		SupportedTypes: []string{"wxpay"},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.NotNil(t, existing)

	candidate, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "wxpay",
		Name:           "Official WeChat",
		Config:         validWxpayProviderConfig(t),
		SupportedTypes: []string{"wxpay"},
		Enabled:        false,
	})
	require.NoError(t, err)

	_, err = svc.UpdateProviderInstance(ctx, candidate.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(true),
	})
	require.NoError(t, err)
}

func TestUpdateProviderInstancePersistsEnabledAndSupportedTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey: "easypay",
		Name:        "EasyPay",
		Config: map[string]string{
			"pid":       "3001",
			"pkey":      "pkey-3001",
			"apiBase":   "https://pay.example.com",
			"notifyUrl": "https://merchant.example.com/notify",
			"returnUrl": "https://merchant.example.com/return",
		},
		SupportedTypes: []string{"alipay"},
		Enabled:        false,
	})
	require.NoError(t, err)

	_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Enabled:        boolPtrValue(true),
		SupportedTypes: []string{"alipay", "wxpay"},
	})
	require.NoError(t, err)

	saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	require.Equal(t, "alipay,wxpay", saved.SupportedTypes)
}

func TestUpdateProviderInstanceRejectsProtectedConfigChangesWhilePendingOrders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		providerKey   string
		createConfig  func(*testing.T) map[string]string
		supportedType []string
		updateConfig  map[string]string
		fieldName     string
		wantValue     string
	}{
		{
			name:          "wxpay appId",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfig,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"appId": "wx-app-updated"},
			fieldName:     "appId",
			wantValue:     "wx-app-test",
		},
		{
			name:          "wxpay mpAppId",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfigWithJSAPIAppID,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"mpAppId": "wx-mp-app-updated"},
			fieldName:     "mpAppId",
			wantValue:     "wx-mp-app-test",
		},
		{
			name:          "wxpay mchId",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfig,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"mchId": "mch-updated"},
			fieldName:     "mchId",
			wantValue:     "mch-test",
		},
		{
			name:          "wxpay publicKeyId",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfig,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"publicKeyId": "public-key-id-updated"},
			fieldName:     "publicKeyId",
			wantValue:     "public-key-id-test",
		},
		{
			name:          "wxpay certSerial",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfig,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"certSerial": "cert-serial-updated"},
			fieldName:     "certSerial",
			wantValue:     "cert-serial-test",
		},
		{
			name:          "alipay appId",
			providerKey:   payment.TypeAlipay,
			createConfig:  validAlipayProviderConfig,
			supportedType: []string{payment.TypeAlipay},
			updateConfig:  map[string]string{"appId": "alipay-app-updated"},
			fieldName:     "appId",
			wantValue:     "alipay-app-test",
		},
		{
			name:          "easypay pid",
			providerKey:   payment.TypeEasyPay,
			createConfig:  validEasyPayProviderConfig,
			supportedType: []string{payment.TypeAlipay},
			updateConfig:  map[string]string{"pid": "pid-updated"},
			fieldName:     "pid",
			wantValue:     "pid-test",
		},
		{
			name:          "stripe currency",
			providerKey:   payment.TypeStripe,
			createConfig:  validStripeProviderConfig,
			supportedType: []string{payment.TypeStripe},
			updateConfig:  map[string]string{"currency": "HKD"},
			fieldName:     "currency",
			wantValue:     "CNY",
		},
		{
			name:          "airwallex accountId",
			providerKey:   payment.TypeAirwallex,
			createConfig:  validAirwallexProviderConfig,
			supportedType: []string{payment.TypeAirwallex},
			updateConfig:  map[string]string{"accountId": "acct-updated"},
			fieldName:     "accountId",
			wantValue:     "acct-test",
		},
		{
			name:          "airwallex currency",
			providerKey:   payment.TypeAirwallex,
			createConfig:  validAirwallexProviderConfig,
			supportedType: []string{payment.TypeAirwallex},
			updateConfig:  map[string]string{"currency": "HKD"},
			fieldName:     "currency",
			wantValue:     "CNY",
		},
		{
			name:          "airwallex webhookSecret",
			providerKey:   payment.TypeAirwallex,
			createConfig:  validAirwallexProviderConfig,
			supportedType: []string{payment.TypeAirwallex},
			updateConfig:  map[string]string{"webhookSecret": "whsec-updated"},
			fieldName:     "webhookSecret",
			wantValue:     "whsec-test",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc := &PaymentConfigService{
				entClient:     client,
				encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
			}

			instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
				ProviderKey:    tc.providerKey,
				Name:           "protected-config-instance",
				Config:         tc.createConfig(t),
				SupportedTypes: tc.supportedType,
				Enabled:        true,
			})
			require.NoError(t, err)

			createPendingProviderConfigOrder(t, ctx, client, instance)

			updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
				Config: tc.updateConfig,
			})
			require.Nil(t, updated)
			require.Error(t, err)
			require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))

			saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
			require.NoError(t, err)
			cfg, err := svc.decryptConfig(saved.Config)
			require.NoError(t, err)
			require.Equal(t, tc.wantValue, cfg[tc.fieldName])
		})
	}
}

func TestUpdateProviderInstanceAllowsSafeConfigChangesWhilePendingOrders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		providerKey   string
		createConfig  func(*testing.T) map[string]string
		supportedType []string
		updateConfig  map[string]string
		fieldName     string
		wantValue     string
	}{
		{
			name:          "wxpay notifyUrl",
			providerKey:   payment.TypeWxpay,
			createConfig:  validWxpayProviderConfig,
			supportedType: []string{payment.TypeWxpay},
			updateConfig:  map[string]string{"notifyUrl": "https://merchant.example.com/wxpay/notify-v2"},
			fieldName:     "notifyUrl",
			wantValue:     "https://merchant.example.com/wxpay/notify-v2",
		},
		{
			name:          "alipay same appId",
			providerKey:   payment.TypeAlipay,
			createConfig:  validAlipayProviderConfig,
			supportedType: []string{payment.TypeAlipay},
			updateConfig:  map[string]string{"appId": "alipay-app-test"},
			fieldName:     "appId",
			wantValue:     "alipay-app-test",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc := &PaymentConfigService{
				entClient:     client,
				encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
			}

			instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
				ProviderKey:    tc.providerKey,
				Name:           "safe-config-instance",
				Config:         tc.createConfig(t),
				SupportedTypes: tc.supportedType,
				Enabled:        true,
			})
			require.NoError(t, err)

			createPendingProviderConfigOrder(t, ctx, client, instance)

			updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
				Config: tc.updateConfig,
			})
			require.NoError(t, err)
			require.NotNil(t, updated)

			saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
			require.NoError(t, err)
			cfg, err := svc.decryptConfig(saved.Config)
			require.NoError(t, err)
			require.Equal(t, tc.wantValue, cfg[tc.fieldName])
		})
	}
}

func TestUpdateProviderInstanceClearsAirwallexAccountID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeAirwallex,
		Name:           "airwallex-clear-account",
		Config:         validAirwallexProviderConfig(t),
		SupportedTypes: []string{payment.TypeAirwallex},
		Enabled:        true,
	})
	require.NoError(t, err)

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"accountId": ""},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	cfg, err := svc.decryptConfig(saved.Config)
	require.NoError(t, err)
	require.Empty(t, cfg["accountId"])
	require.Equal(t, "client-id-test", cfg["clientId"])
}

func TestWiseExpiredOrdersInsideReconcileWindowProtectProviderInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return &wiseSubscriptionCreatorStub{}
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-protected-expired",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	createWiseExpiredProviderConfigOrder(t, ctx, client, instance, time.Now().Add(-time.Hour))

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.Nil(t, updated)
	require.Error(t, err)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))

	updated, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(false),
	})
	require.Nil(t, updated)
	require.Error(t, err)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))

	err = svc.DeleteProviderInstance(ctx, instance.ID)
	require.Error(t, err)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
}

func TestWiseCancelledOrdersInsideReconcileWindowProtectProviderInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return &wiseSubscriptionCreatorStub{}
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-protected-cancelled",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	createWiseProviderConfigOrder(t, ctx, client, instance, OrderStatusCancelled, time.Now().Add(-time.Hour))

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.Nil(t, updated)
	require.Error(t, err)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
}

func TestWiseProviderConfigProtectionUsesConfiguredReconcileWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return &wiseSubscriptionCreatorStub{}
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	config["reconcileWindowHours"] = "168"
	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-protected-configured-window",
		Config:         config,
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	createWiseProviderConfigOrder(t, ctx, client, instance, OrderStatusExpired, time.Now().Add(-100*time.Hour))

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.Nil(t, updated)
	require.Error(t, err)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
}

func TestWiseExpiredOrdersOutsideReconcileWindowDoNotProtectProviderInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return &wiseSubscriptionCreatorStub{}
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-unprotected-expired",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	createWiseExpiredProviderConfigOrder(t, ctx, client, instance, time.Now().Add(-wiseReconcileWindow-time.Hour))

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	updated, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(false),
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	err = svc.DeleteProviderInstance(ctx, instance.ID)
	require.NoError(t, err)
}

func TestUpdateWiseProviderDisableDeletesRemoteSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-disable-delete-remote",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	err = client.PaymentProviderWebhookSubscription.Update().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(instance.ID)).
		SetExternalSubscriptionID("sub-disable-123").
		Exec(ctx)
	require.NoError(t, err)
	disabled := false

	saved, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Enabled: &disabled})

	require.NoError(t, err)
	require.False(t, saved.Enabled)
	require.Equal(t, []string{"profile-123:sub-disable-123"}, creator.deleted)
	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(instance.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusDeleted, sub.Status)
}

func TestDeleteWiseProviderDeletesRemoteSubscriptionBeforeLocalDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise-delete-remote",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        false,
	})
	require.NoError(t, err)
	createActiveWiseWebhookSubscriptionForTest(t, ctx, client, instance.ID, "sub-delete-123")

	err = svc.DeleteProviderInstance(ctx, instance.ID)

	require.NoError(t, err)
	require.Equal(t, []string{"profile-123:sub-delete-123"}, creator.deleted)
	exists, err := client.PaymentProviderInstance.Query().
		Where(paymentproviderinstance.IDEQ(instance.ID)).
		Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func createPendingProviderConfigOrder(t *testing.T, ctx context.Context, client *dbent.Client, instance *dbent.PaymentProviderInstance) {
	t.Helper()

	user, err := client.User.Create().
		SetEmail("provider-config-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("provider-config-pending-user").
		Save(ctx)
	require.NoError(t, err)

	instanceID := strconv.FormatInt(instance.ID, 10)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("PENDING-PROVIDER-CONFIG-" + instanceID).
		SetOutTradeNo("sub2_pending_provider_config_" + instanceID).
		SetPaymentType(providerPendingOrderPaymentType(instance.ProviderKey)).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(instance.ProviderKey).
		Save(ctx)
	require.NoError(t, err)
}

func createWiseExpiredProviderConfigOrder(t *testing.T, ctx context.Context, client *dbent.Client, instance *dbent.PaymentProviderInstance, expiresAt time.Time) {
	t.Helper()
	createWiseProviderConfigOrder(t, ctx, client, instance, OrderStatusExpired, expiresAt)
}

func createWiseProviderConfigOrder(t *testing.T, ctx context.Context, client *dbent.Client, instance *dbent.PaymentProviderInstance, status string, expiresAt time.Time) {
	t.Helper()

	user, err := client.User.Create().
		SetEmail("provider-config-wise-expired@example.com").
		SetPasswordHash("hash").
		SetUsername("provider-config-wise-expired-user").
		Save(ctx)
	require.NoError(t, err)

	instanceID := strconv.FormatInt(instance.ID, 10)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("EXPIRED-WISE-PROVIDER-CONFIG-" + instanceID).
		SetOutTradeNo("sub2_expired_wise_provider_config_" + instanceID).
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(status).
		SetExpiresAt(expiresAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(instance.ProviderKey).
		Save(ctx)
	require.NoError(t, err)
}

func providerPendingOrderPaymentType(providerKey string) string {
	switch providerKey {
	case payment.TypeWxpay:
		return payment.TypeWxpay
	case payment.TypeAlipay:
		return payment.TypeAlipay
	case payment.TypeAirwallex:
		return payment.TypeAirwallex
	case payment.TypeStripe:
		return payment.TypeStripe
	default:
		return payment.TypeAlipay
	}
}

func validWiseProviderConfigForConfigService(t *testing.T) map[string]string {
	t.Helper()

	_, publicKeyPEM := newWiseConfigServiceWebhookKey(t)
	return map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-test",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   publicKeyPEM,
		"settlementStrategy": "exact_only",
	}
}

func newWiseConfigServiceWebhookKey(t *testing.T) (*rsa.PrivateKey, string) {
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

func validStripeProviderConfig(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"secretKey":      "sk_test_123",
		"publishableKey": "pk_test_123",
		"webhookSecret":  "whsec-test",
		"currency":       "CNY",
	}
}

func boolPtrValue(v bool) *bool {
	return &v
}

func validAlipayProviderConfig(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"appId":      "alipay-app-test",
		"privateKey": "alipay-private-key-test",
		"notifyUrl":  "https://merchant.example.com/alipay/notify",
		"returnUrl":  "https://merchant.example.com/alipay/return",
	}
}

func validEasyPayProviderConfig(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"pid":       "pid-test",
		"pkey":      "pkey-test",
		"apiBase":   "https://pay.example.com",
		"notifyUrl": "https://merchant.example.com/easypay/notify",
		"returnUrl": "https://merchant.example.com/easypay/return",
	}
}

func validAirwallexProviderConfig(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"clientId":      "client-id-test",
		"apiKey":        "api-key-test",
		"webhookSecret": "whsec-test",
		"apiBase":       "https://api-demo.airwallex.com/api/v1",
		"accountId":     "acct-test",
		"currency":      "CNY",
	}
}

func validWxpayProviderConfig(t *testing.T) map[string]string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	return map[string]string{
		"appId":       "wx-app-test",
		"mchId":       "mch-test",
		"privateKey":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})),
		"apiV3Key":    "12345678901234567890123456789012",
		"publicKey":   string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
		"publicKeyId": "public-key-id-test",
		"certSerial":  "cert-serial-test",
	}
}

func validWxpayProviderConfigWithJSAPIAppID(t *testing.T) map[string]string {
	t.Helper()

	cfg := validWxpayProviderConfig(t)
	cfg["mpAppId"] = "wx-mp-app-test"
	return cfg
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
