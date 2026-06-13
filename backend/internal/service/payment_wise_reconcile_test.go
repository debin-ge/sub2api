//go:build unit

package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestReconcilePendingWiseOrdersQueriesPendingWiseOrdersByOutTradeNo(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-PENDING").
		SetOutTradeNo("sub2_wise_pending_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-123").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-123",
			Status:  payment.ProviderStatusPending,
			Amount:  88,
			Metadata: map[string]string{
				"reconcile_reason": "still_pending",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.Scanned)
	require.False(t, result.AutoFulfill)
	require.Equal(t, 0, result.Fulfilled)
	require.Equal(t, "event_verified_no_auto_fulfill", result.Reason)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestWiseWebhookUnsupportedSignedEventDoesNotTriggerReconcile(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	priv, publicKeyPEM := newWiseReconcileWebhookKey(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-webhook").
		SetConfig(encryptWebhookProviderConfig(t, validWiseReconcileConfig(publicKeyPEM))).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetEmail("wise-unsupported@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-unsupported-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-UNSUPPORTED").
		SetOutTradeNo("sub2_wise_unsupported_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-unsupported").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-unsupported",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		loadBalancer:    newWebhookProviderTestLoadBalancer(client),
		registry:        registry,
		providersLoaded: true,
	}
	rawBody := `{"event_type":"unsupported#event","data":{"resource":{"id":"resource-123"}}}`

	result, err := svc.HandleWiseWebhook(ctx, rawBody, map[string]string{
		"x-signature-sha256": signWiseReconcileWebhook(t, priv, rawBody),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, 0, result.Scanned)
	require.Equal(t, 0, result.Fulfilled)
	require.Equal(t, "event_ignored_unsupported", result.Reason)
	require.Equal(t, 0, provider.queryCalls)
}

func TestReconcileWiseOrderByOutTradeNoDoesNotFallbackWhenPinnedProviderMissing(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-pinned-missing@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-pinned-missing-user").
		Save(ctx)
	require.NoError(t, err)

	missingInstanceID := "999999"
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-PINNED-MISSING").
		SetOutTradeNo("sub2_wise_pinned_missing_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-pinned-missing").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(missingInstanceID).
		SetProviderKey(payment.TypeWise).
		SetProviderSnapshot(map[string]any{
			"schema_version":       1,
			"provider_instance_id": missingInstanceID,
			"provider_key":         payment.TypeWise,
		}).
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "registry-should-not-be-used",
			Status:  payment.ProviderStatusPending,
			Amount:  88,
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
	require.Nil(t, result)
	require.ErrorContains(t, err, "provider snapshot instance 999999 is missing")
	require.Equal(t, 0, provider.queryCalls)
}

func TestReconcileWiseOrderByOutTradeNoDoesNotAutoFulfillNonPaidNoMatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-no-match@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-no-match-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-NO-MATCH").
		SetOutTradeNo("sub2_wise_no_match_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-no-match").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-no-match",
			Status:  payment.ProviderStatusPending,
			Amount:  0,
			Metadata: map[string]string{
				"reconcile_decision": "no_match",
				"reconcile_reason":   "activity_not_found",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, order.OutTradeNo, result.OrderID)
	require.Equal(t, "wise-upstream-no-match", result.TradeNo)
	require.Equal(t, "activity_not_found", result.Reason)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestReconcileWiseOrderByOutTradeNoDoesNotAutoFulfillAmountMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-amount-mismatch@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-amount-mismatch-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-AMOUNT-MISMATCH").
		SetOutTradeNo("sub2_wise_amount_mismatch_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-amount-mismatch").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-amount-mismatch",
			Status:  payment.ProviderStatusPaid,
			Amount:  87.99,
			Metadata: map[string]string{
				"reconcile_decision":  "auto_fulfill",
				"reconcile_reason":    "exact_match",
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, order.OutTradeNo, result.OrderID)
	require.Equal(t, "wise-upstream-amount-mismatch", result.TradeNo)
	require.Equal(t, "amount_mismatch", result.Reason)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Equal(t, 88.0, reloaded.PayAmount)
}

func TestReconcileWiseOrderByOutTradeNoDoesNotAutoFulfillSubCentMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-sub-cent@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-sub-cent-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-SUB-CENT").
		SetOutTradeNo("sub2_wise_sub_cent_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-sub-cent").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-sub-cent",
			Status:  payment.ProviderStatusPaid,
			Amount:  88.001,
			Metadata: map[string]string{
				"reconcile_decision":  "auto_fulfill",
				"reconcile_reason":    "exact_match",
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "amount_mismatch", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestReconcileWiseOrderByOutTradeNoDoesNotAutoFulfillMissingSnapshotMetadata(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-missing-snapshot-metadata@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-missing-snapshot-metadata-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-MISSING-SNAPSHOT-METADATA").
		SetOutTradeNo("sub2_wise_missing_snapshot_metadata_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-missing-snapshot-metadata").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_key":         payment.TypeWise,
			"merchant_id":          "profile-123",
			"balance_id":           "balance-123",
			"currency":             "USD",
			"settlement_strategy":  "exact_only",
			"provider_instance_id": "88",
		}).
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-missing-snapshot-metadata",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "metadata_mismatch", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func validWiseReconcileConfig(publicKeyPEM string) map[string]string {
	return map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-123",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   publicKeyPEM,
		"settlementStrategy": "exact_only",
	}
}

func newWiseReconcileWebhookKey(t *testing.T) (*rsa.PrivateKey, string) {
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

func signWiseReconcileWebhook(t *testing.T, priv *rsa.PrivateKey, rawBody string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(rawBody))
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(signature)
}

func TestWiseWebhookEventIsReconcileTrigger(t *testing.T) {
	t.Parallel()

	for _, rawBody := range []string{
		`{"event_type":"balances#credit"}`,
		`{"event_type":"balances#update"}`,
		`{"event_type":"account-details-payment#state-change"}`,
	} {
		require.True(t, wiseWebhookEventIsReconcileTrigger(rawBody), rawBody)
	}

	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":"unsupported#event"}`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{}`))
}
