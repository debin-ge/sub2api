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
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentwebhookdelivery"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	paymentprovider "github.com/Wei-Shaw/sub2api/internal/payment/provider"
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

func TestReconcilePendingWiseOrdersScansBeyondFirstPage(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-page@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-page-user").
		Save(ctx)
	require.NoError(t, err)

	var lastOutTradeNo string
	for i := 0; i < pendingWiseReconcileLimit+1; i++ {
		outTradeNo := fmt.Sprintf("sub2_wise_page_%03d", i)
		_, err := client.PaymentOrder.Create().
			SetUserID(user.ID).
			SetUserEmail(user.Email).
			SetUserName(user.Username).
			SetAmount(88).
			SetPayAmount(88).
			SetFeeRate(0).
			SetRechargeCode(fmt.Sprintf("WISE-PAGE-%03d", i)).
			SetOutTradeNo(outTradeNo).
			SetPaymentType(payment.TypeWise).
			SetPaymentTradeNo(fmt.Sprintf("wise-upstream-page-%03d", i)).
			SetOrderType(payment.OrderTypeBalance).
			SetStatus(OrderStatusPending).
			SetExpiresAt(time.Now().Add(time.Hour)).
			SetClientIP("127.0.0.1").
			SetSrcHost("api.example.com").
			Save(ctx)
		require.NoError(t, err)
		lastOutTradeNo = outTradeNo
	}

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-page",
			Status:  payment.ProviderStatusPending,
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

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, pendingWiseReconcileLimit+1, result.Scanned)
	require.Equal(t, pendingWiseReconcileLimit+1, provider.queryCalls)
	require.Equal(t, lastOutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, "event_verified_no_auto_fulfill", result.Reason)
}

func TestReconcilePendingWiseOrdersBatchesProviderStatementQuery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-batch-page@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-batch-page-user").
		Save(ctx)
	require.NoError(t, err)

	responses := make(map[string]*payment.QueryOrderResponse, pendingWiseReconcileLimit+1)
	for i := 0; i < pendingWiseReconcileLimit+1; i++ {
		outTradeNo := fmt.Sprintf("sub2_wise_batch_page_%03d", i)
		_, err := client.PaymentOrder.Create().
			SetUserID(user.ID).
			SetUserEmail(user.Email).
			SetUserName(user.Username).
			SetAmount(88).
			SetPayAmount(88).
			SetFeeRate(0).
			SetRechargeCode(fmt.Sprintf("WISE-BATCH-PAGE-%03d", i)).
			SetOutTradeNo(outTradeNo).
			SetPaymentType(payment.TypeWise).
			SetPaymentTradeNo(fmt.Sprintf("wise-upstream-batch-page-%03d", i)).
			SetOrderType(payment.OrderTypeBalance).
			SetStatus(OrderStatusPending).
			SetExpiresAt(time.Now().Add(time.Hour)).
			SetClientIP("127.0.0.1").
			SetSrcHost("api.example.com").
			Save(ctx)
		require.NoError(t, err)
		responses[outTradeNo] = &payment.QueryOrderResponse{
			TradeNo: outTradeNo,
			Status:  payment.ProviderStatusPending,
			Metadata: map[string]string{
				"reconcile_decision": "no_match",
				"reconcile_reason":   "activity_not_found",
			},
		}
	}

	provider := &wiseBatchReconcileProvider{responses: responses}
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
	require.Equal(t, pendingWiseReconcileLimit+1, result.Scanned)
	require.Equal(t, 1, provider.batchCalls)
	require.Equal(t, 0, provider.queryCalls)
	require.Len(t, provider.lastBatchTradeNos, pendingWiseReconcileLimit+1)
	require.Equal(t, "event_verified_no_auto_fulfill", result.Reason)
}

func TestReconcilePendingWiseOrdersCachesWiseStatementAcrossProviderGroups(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	apiBase, statementCalls := newWiseReconcileHTTPServer(t)
	_, publicKeyPEM := newWiseReconcileWebhookKey(t)
	config := validWiseReconcileConfig(publicKeyPEM)
	config["apiBase"] = apiBase
	config["apiRateLimitRPS"] = "1000"
	config["apiRateLimitBurst"] = "1000"

	firstInstance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-cache-first").
		SetConfig(encryptWebhookProviderConfig(t, config)).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	secondInstance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-cache-second").
		SetConfig(encryptWebhookProviderConfig(t, config)).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetEmail("wise-cache-groups@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-cache-groups-user").
		Save(ctx)
	require.NoError(t, err)

	for _, tc := range []struct {
		outTradeNo string
		instanceID int64
	}{
		{outTradeNo: "sub2_wise_cache_group_001", instanceID: firstInstance.ID},
		{outTradeNo: "sub2_wise_cache_group_002", instanceID: secondInstance.ID},
	} {
		_, err := client.PaymentOrder.Create().
			SetUserID(user.ID).
			SetUserEmail(user.Email).
			SetUserName(user.Username).
			SetAmount(88).
			SetPayAmount(88).
			SetFeeRate(0).
			SetRechargeCode(tc.outTradeNo).
			SetOutTradeNo(tc.outTradeNo).
			SetPaymentType(payment.TypeWise).
			SetPaymentTradeNo("wise-" + tc.outTradeNo).
			SetOrderType(payment.OrderTypeBalance).
			SetStatus(OrderStatusPending).
			SetExpiresAt(time.Now().Add(time.Hour)).
			SetClientIP("127.0.0.1").
			SetSrcHost("api.example.com").
			SetProviderInstanceID(strconv.FormatInt(tc.instanceID, 10)).
			SetProviderKey(payment.TypeWise).
			Save(ctx)
		require.NoError(t, err)
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.Scanned)
	require.Equal(t, int32(1), statementCalls.Load())
	require.Equal(t, "event_verified_no_auto_fulfill", result.Reason)
}

func TestReconcilePendingWiseOrdersBatchPassesEarliestOrderCreatedAtToProvider(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now().UTC()
	earliestCreatedAt := now.Add(-90 * time.Hour).Truncate(time.Second)
	laterCreatedAt := now.Add(-2 * time.Hour).Truncate(time.Second)

	user, err := client.User.Create().
		SetEmail("wise-batch-created-at@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-batch-created-at-user").
		Save(ctx)
	require.NoError(t, err)

	for _, tc := range []struct {
		outTradeNo string
		createdAt  time.Time
	}{
		{outTradeNo: "sub2_wise_batch_created_at_001", createdAt: laterCreatedAt},
		{outTradeNo: "sub2_wise_batch_created_at_002", createdAt: earliestCreatedAt},
	} {
		_, err := client.PaymentOrder.Create().
			SetUserID(user.ID).
			SetUserEmail(user.Email).
			SetUserName(user.Username).
			SetAmount(88).
			SetPayAmount(88).
			SetFeeRate(0).
			SetRechargeCode(tc.outTradeNo).
			SetOutTradeNo(tc.outTradeNo).
			SetPaymentType(payment.TypeWise).
			SetPaymentTradeNo("wise-upstream-" + tc.outTradeNo).
			SetOrderType(payment.OrderTypeBalance).
			SetStatus(OrderStatusPending).
			SetExpiresAt(now.Add(time.Hour)).
			SetCreatedAt(tc.createdAt).
			SetClientIP("127.0.0.1").
			SetSrcHost("api.example.com").
			Save(ctx)
		require.NoError(t, err)
	}

	provider := &wiseBatchReconcileProvider{}
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
	require.Equal(t, 2, result.Scanned)
	require.Equal(t, 1, provider.batchCalls)
	gotCreatedAt, ok := paymentprovider.WiseOrderCreatedAtFromContext(provider.lastBatchContext)
	require.True(t, ok)
	require.WithinDuration(t, earliestCreatedAt, gotCreatedAt, time.Second)
}

func TestReconcilePendingWiseOrdersAutoFulfillsExpiredOrderInsideWindow(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	user, err := client.User.Create().
		SetEmail("wise-expired@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-expired-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-EXPIRED").
		SetOutTradeNo("sub2_wise_expired_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-expired").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(now.Add(-time.Hour)).
		SetUpdatedAt(now.Add(-time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		userRepo.getByIDUser.Balance += amount
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-expired",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
			Metadata: map[string]string{
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
				"wise_transaction_id": "wise-upstream-expired",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.AutoFulfill)
	require.Equal(t, 1, result.Fulfilled)
	require.Equal(t, "event_verified_reconciled", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, 88.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestReconcilePendingWiseOrdersDoesNotAutoFulfillExpiredAmountMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	user, err := client.User.Create().
		SetEmail("wise-expired-mismatch@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-expired-mismatch-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-EXPIRED-MISMATCH").
		SetOutTradeNo("sub2_wise_expired_mismatch_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-expired-mismatch").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(now.Add(-time.Hour)).
		SetUpdatedAt(now.Add(-time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-expired-mismatch",
			Status:  payment.ProviderStatusPaid,
			Amount:  88.001,
			Metadata: map[string]string{
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

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, 0, result.Fulfilled)
	require.Equal(t, "event_verified_no_auto_fulfill", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
	require.Equal(t, 88.0, reloaded.PayAmount)
}

func TestQueryWiseReconciliationCandidatesIncludesCancelledOrdersInsideWindow(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	user, err := client.User.Create().
		SetEmail("wise-cancelled-candidate@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-cancelled-candidate-user").
		Save(ctx)
	require.NoError(t, err)

	cancelledOrder, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CANCELLED-CANDIDATE").
		SetOutTradeNo("sub2_wise_cancelled_candidate_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-cancelled-candidate").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCancelled).
		SetExpiresAt(now.Add(-time.Hour)).
		SetUpdatedAt(now.Add(-time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	outsideWindowOrder, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CANCELLED-OLD").
		SetOutTradeNo("sub2_wise_cancelled_old_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-cancelled-old").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCancelled).
		SetExpiresAt(now.Add(-wiseReconcileWindow - time.Hour)).
		SetUpdatedAt(now.Add(-wiseReconcileWindow - time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}

	candidates, _, _, err := svc.queryWiseReconciliationCandidates(ctx, 0, now)
	require.NoError(t, err)

	ids := make(map[int64]bool, len(candidates))
	for _, candidate := range candidates {
		ids[candidate.ID] = true
	}
	require.True(t, ids[cancelledOrder.ID])
	require.False(t, ids[outsideWindowOrder.ID])
}

func TestQueryWiseReconciliationCandidatesUsesConfiguredProviderWindow(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	priv, publicKeyPEM := newWiseReconcileWebhookKey(t)
	_ = priv
	config := validWiseReconcileConfig(publicKeyPEM)
	config["reconcileWindowHours"] = "168"
	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-long-window").
		SetConfig(encryptWebhookProviderConfig(t, config)).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetEmail("wise-configured-window@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-configured-window-user").
		Save(ctx)
	require.NoError(t, err)

	instanceID := fmt.Sprintf("%d", instance.ID)
	longWindowOrder, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CONFIG-WINDOW-LONG").
		SetOutTradeNo("sub2_wise_config_window_long").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-config-window-long").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(now.Add(-100 * time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeWise).
		Save(ctx)
	require.NoError(t, err)

	oldOrder, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CONFIG-WINDOW-OLD").
		SetOutTradeNo("sub2_wise_config_window_old").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-config-window-old").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(now.Add(-200 * time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeWise).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	candidates, _, _, err := svc.queryWiseReconciliationCandidates(ctx, 0, now)
	require.NoError(t, err)

	ids := make(map[int64]bool, len(candidates))
	for _, candidate := range candidates {
		ids[candidate.ID] = true
	}
	require.True(t, ids[longWindowOrder.ID])
	require.False(t, ids[oldOrder.ID])
}

func TestWiseToPaidDoesNotRecoverCancelledOrderOutsideConfiguredWindow(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	user, err := client.User.Create().
		SetEmail("wise-old-cancelled@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-old-cancelled-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-OLD-CANCELLED").
		SetOutTradeNo("sub2_wise_old_cancelled").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-old-cancelled-original").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCancelled).
		SetExpiresAt(now.Add(-wiseReconcileWindow - time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	err = svc.toPaid(ctx, order, "wise-old-cancelled-paid", 88, payment.TypeWise)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
	require.Equal(t, "wise-old-cancelled-original", reloaded.PaymentTradeNo)
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

func TestHandleWiseWebhookFastIgnoresTestNotification(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv := newWiseWebhookFastTestService(t, ctx, client)
	rawBody := `{"event_type":"balances#credit","data":{"resource":{"id":"resource-123"}}}`

	result, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-test", true))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "delivery-test", result.DeliveryID)
	require.Equal(t, "balances#credit", result.EventType)
	require.True(t, result.TestNotification)
	require.False(t, result.Duplicate)
	require.False(t, result.Queued)
	require.Equal(t, "test_notification", result.Reason)

	delivery := requireWiseWebhookDelivery(t, ctx, client, "delivery-test")
	require.Equal(t, PaymentWebhookDeliveryStatusIgnored, delivery.Status)
	require.NotNil(t, delivery.ProcessedAt)
}

func TestHandleWiseWebhookFastDuplicateDeliveryDoesNotQueue(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv := newWiseWebhookFastTestService(t, ctx, client)
	rawBody := `{"event_type":"balances#credit"}`

	first, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-dup", false))
	require.NoError(t, err)
	require.True(t, first.Queued)
	require.Equal(t, "queued", first.Reason)

	second, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-dup", false))
	require.NoError(t, err)
	require.NotNil(t, second)
	require.True(t, second.Duplicate)
	require.False(t, second.Queued)
	require.Equal(t, "duplicate_delivery", second.Reason)
}

func TestHandleWiseWebhookFastCoalescesSameBalanceReconciliation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv, statementCalls := newWiseWebhookFastHTTPTestService(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("wise-webhook-coalesce@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-webhook-coalesce-user").
		Save(ctx)
	require.NoError(t, err)

	instanceID := strconv.FormatInt(svcTestWiseProviderInstanceID(t, ctx, client), 10)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-WEBHOOK-COALESCE").
		SetOutTradeNo("sub2_wise_webhook_coalesce").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-webhook-coalesce").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeWise).
		Save(ctx)
	require.NoError(t, err)

	rawBody := `{"event_type":"balances#credit","data":{"resource":{"id":"balance-123","profile_id":"profile-123","account_id":"balance-123","type":"balance"}}}`
	first, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-coalesce-1", false))
	require.NoError(t, err)
	require.True(t, first.Queued)
	second, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-coalesce-2", false))
	require.NoError(t, err)
	require.True(t, second.Queued)

	require.Eventually(t, func() bool {
		firstDelivery := requireWiseWebhookDelivery(t, ctx, client, "delivery-coalesce-1")
		secondDelivery := requireWiseWebhookDelivery(t, ctx, client, "delivery-coalesce-2")
		return firstDelivery.Status == PaymentWebhookDeliveryStatusProcessed &&
			secondDelivery.Status == PaymentWebhookDeliveryStatusProcessed
	}, 2*time.Second, 20*time.Millisecond)
	require.Equal(t, int32(1), statementCalls.Load())
}

func TestHandleWiseWebhookFastRetriesExistingReceivedDelivery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv := newWiseWebhookFastTestService(t, ctx, client)
	rawBody := `{"event_type":"balances#credit"}`

	record, err := svc.RecordPaymentWebhookDelivery(ctx, PaymentWebhookDeliveryInput{
		ProviderKey: payment.TypeWise,
		DeliveryID:  "delivery-retry-received",
		EventType:   "balances#credit",
		RawBody:     rawBody,
	})
	require.NoError(t, err)

	result, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-retry-received", false))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Duplicate)
	require.True(t, result.Queued)
	require.Equal(t, "queued", result.Reason)

	delivery, err := client.PaymentWebhookDelivery.Get(ctx, int64(record.ID))
	require.NoError(t, err)
	require.NotEqual(t, PaymentWebhookDeliveryStatusReceived, delivery.Status)
}

func TestHandleWiseWebhookFastIgnoresUnsupportedEvent(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv := newWiseWebhookFastTestService(t, ctx, client)
	rawBody := `{"event_type":"unsupported#event"}`

	result, err := svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "delivery-unsupported", false))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Queued)
	require.Equal(t, "event_ignored_unsupported", result.Reason)

	delivery := requireWiseWebhookDelivery(t, ctx, client, "delivery-unsupported")
	require.Equal(t, PaymentWebhookDeliveryStatusIgnored, delivery.Status)
	require.NotNil(t, delivery.ProcessedAt)
}

func TestHandleWiseWebhookFastRequiresDeliveryIDAndValidSignature(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc, priv := newWiseWebhookFastTestService(t, ctx, client)
	rawBody := `{"event_type":"balances#credit"}`

	_, err := svc.HandleWiseWebhookFast(ctx, rawBody, map[string]string{
		"x-signature-sha256": "bad-signature",
		"x-delivery-id":      "delivery-invalid-signature",
	})
	require.Error(t, err)
	count, countErr := client.PaymentWebhookDelivery.Query().
		Where(paymentwebhookdelivery.ProviderKeyEQ(payment.TypeWise), paymentwebhookdelivery.DeliveryIDEQ("delivery-invalid-signature")).
		Count(ctx)
	require.NoError(t, countErr)
	require.Zero(t, count)

	_, err = svc.HandleWiseWebhookFast(ctx, rawBody, signedWiseWebhookFastHeaders(t, priv, rawBody, "", false))
	require.Error(t, err)
	require.Contains(t, err.Error(), "delivery id")
}

func TestReconcilePendingWiseOrdersReturnsErrorWhenProviderQueryFails(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-query-fails@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-query-fails-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-QUERY-FAILS").
		SetOutTradeNo("sub2_wise_query_fails_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-query-fails-upstream").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &wiseFailingReconcileProvider{err: errors.New("wise statement timeout")}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wise reconciliation failed")
	require.NotNil(t, result)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, provider.queryCalls)
}

func TestReconcilePendingWiseOrdersSkipsWhenWiseLeaderLockHeld(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-lock-held@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-lock-held-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-LOCK-HELD").
		SetOutTradeNo("sub2_wise_lock_held").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-lock-held").
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
			TradeNo: "wise-lock-held",
			Status:  payment.ProviderStatusPending,
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}
	svc.SetWiseReconcileLock(&wiseTestLeaderLock{acquired: false}, nil)

	result, err := svc.ReconcilePendingWiseOrders(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "reconcile_in_progress", result.Reason)
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

func TestReconcileWiseOrderByOutTradeNoPassesOrderCreatedAtToProvider(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	createdAt := time.Now().UTC().Add(-96 * time.Hour).Truncate(time.Second)

	user, err := client.User.Create().
		SetEmail("wise-single-created-at@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-single-created-at-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-SINGLE-CREATED-AT").
		SetOutTradeNo("sub2_wise_single_created_at").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-single-created-at").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetCreatedAt(createdAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-single-created-at",
			Status:  payment.ProviderStatusPending,
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
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	gotCreatedAt, ok := paymentprovider.WiseOrderCreatedAtFromContext(provider.lastQueryContext)
	require.True(t, ok)
	require.WithinDuration(t, createdAt, gotCreatedAt, time.Second)
}

func TestHandleWiseQueryOrderResponseWritesAuditForNoMatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-no-match-audit@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-no-match-audit-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-NO-MATCH-AUDIT").
		SetOutTradeNo("sub2_wise_no_match_audit_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-no-match-audit").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	result, err := svc.handleWiseQueryOrderResponse(ctx, order, &payment.QueryOrderResponse{
		TradeNo: "sub2_wise_no_match_audit_123",
		Status:  payment.ProviderStatusPending,
		Metadata: map[string]string{
			"reconcile_decision": "no_match",
			"reconcile_reason":   "no_matching_transaction",
			"profile_id":         "profile-123",
			"balance_id":         "balance-123",
			"currency":           "USD",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "no_matching_transaction", result.Reason)

	detail := requireWiseAuditDetail(t, ctx, client, order.ID, "PAYMENT_WISE_RECONCILE_NO_MATCH")
	require.Equal(t, "no_matching_transaction", detail["reason"])
	require.Equal(t, "no_match", detail["reviewAction"])
	require.Equal(t, "no_match", detail["reconcileDecision"])
	require.Equal(t, "sub2_wise_no_match_audit_123", detail["outTradeNo"])
	require.Equal(t, payment.ProviderStatusPending, detail["providerStatus"])
	require.Equal(t, float64(88), detail["expected"])
	require.Equal(t, "profile-123", detail["profile_id"])
	require.Equal(t, "balance-123", detail["balance_id"])
	require.Equal(t, "USD", detail["currency"])
}

func TestHandleWiseQueryOrderResponseWritesAuditForRejectedReferencedTransaction(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-rejected-audit@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-rejected-audit-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-REJECTED-AUDIT").
		SetOutTradeNo("sub2_wise_rejected_audit_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-rejected-audit").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	result, err := svc.handleWiseQueryOrderResponse(ctx, order, &payment.QueryOrderResponse{
		TradeNo: "wise-rejected-tx",
		Status:  payment.ProviderStatusPending,
		Metadata: map[string]string{
			"reconcile_decision":  "rejected",
			"reconcile_reason":    "currency_mismatch",
			"wise_transaction_id": "wise-rejected-tx",
			"description":         "Payment for sub2_wise_rejected_audit_123",
			"reference":           "sub2_wise_rejected_audit_123",
			"transaction_status":  "completed",
			"direction":           "credit",
			"occurred_at":         "2026-06-12T10:20:30Z",
			"currency":            "EUR",
			"net_amount":          "88",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "currency_mismatch", result.Reason)

	detail := requireWiseAuditDetail(t, ctx, client, order.ID, "PAYMENT_WISE_RECONCILE_MANUAL_REVIEW")
	require.Equal(t, "currency_mismatch", detail["reason"])
	require.Equal(t, "rejected", detail["reconcileDecision"])
	require.Equal(t, "wise-rejected-tx", detail["wise_transaction_id"])
	require.Equal(t, "Payment for sub2_wise_rejected_audit_123", detail["description"])
	require.Equal(t, "sub2_wise_rejected_audit_123", detail["reference"])
	require.Equal(t, "2026-06-12T10:20:30Z", detail["occurred_at"])
	require.Equal(t, "EUR", detail["currency"])
	require.Equal(t, "88", detail["net_amount"])
}

func TestHandleWiseQueryOrderResponseMarksProviderCancelledOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-reconcile-cancelled@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-reconcile-cancelled-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-RECONCILE-CANCELLED").
		SetOutTradeNo("sub2_wise_reconcile_cancelled").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	result, err := svc.handleWiseQueryOrderResponse(ctx, order, &payment.QueryOrderResponse{
		TradeNo: "wise-cancelled-1",
		Status:  payment.ProviderStatusCancelled,
		Metadata: map[string]string{
			"reconcile_reason":    "status_cancelled",
			"wise_transaction_id": "wise-cancelled-1",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "status_cancelled", result.Reason)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
	require.True(t, svc.hasAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_CANCELLED"))
}

func TestPaymentOrderExpiryRunOnceReconcilesPendingWiseOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-expiry-runner@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-expiry-runner-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-EXPIRY-RUNNER").
		SetOutTradeNo("sub2_wise_expiry_runner_123").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-expiry-runner-upstream").
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
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Metadata: map[string]string{
				"reconcile_decision": "no_match",
				"reconcile_reason":   "no_matching_transaction",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	paymentSvc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}
	expirySvc := NewPaymentOrderExpiryService(paymentSvc, time.Minute)

	expirySvc.runOnce()

	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
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
				"wise_transaction_id": "wise-upstream-amount-mismatch",
				"description":         "Card-like fee deducted for sub2_wise_amount_mismatch_123",
				"reference":           "sub2_wise_amount_mismatch_123",
				"transaction_status":  "completed",
				"direction":           "credit",
				"occurred_at":         "2026-06-12T10:20:30Z",
				"gross_amount":        "88",
				"fee_amount":          "0.01",
				"net_amount":          "87.99",
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

	detail := requireWiseAuditDetail(t, ctx, client, order.ID, "PAYMENT_AMOUNT_MISMATCH")
	require.Equal(t, "amount_mismatch", detail["reason"])
	require.Equal(t, "manual_review", detail["reconcile_decision"])
	require.Equal(t, "manual_review", detail["reconcileDecision"])
	require.Equal(t, "wise-upstream-amount-mismatch", detail["wise_transaction_id"])
	require.Equal(t, "Card-like fee deducted for sub2_wise_amount_mismatch_123", detail["description"])
	require.Equal(t, "sub2_wise_amount_mismatch_123", detail["reference"])
	require.Equal(t, "0.01", detail["fee_amount"])
	require.Equal(t, "87.99", detail["net_amount"])
	require.Equal(t, "manual_review", detail["reviewAction"])
}

func TestReconcileWiseOrderByOutTradeNoRejectsReusedWiseTransactionID(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-reused-tx@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-reused-tx-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-REUSED-TX-COMPLETED").
		SetOutTradeNo("sub2_wise_reused_tx_completed").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-tx-reused").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-REUSED-TX-PENDING").
		SetOutTradeNo("sub2_wise_reused_tx_pending").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-reused-tx-pending").
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
			TradeNo: "wise-tx-reused",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
			Metadata: map[string]string{
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
				"wise_transaction_id": "wise-tx-reused",
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
	require.Equal(t, "transaction_reused", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
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

func TestHandleWiseQueryOrderResponseDoesNotAutoFulfillMissingSnapshotMetadata(t *testing.T) {
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

	svc := &PaymentService{
		entClient: client,
	}

	result, err := svc.handleWiseQueryOrderResponse(ctx, order, &payment.QueryOrderResponse{
		TradeNo: "wise-upstream-missing-snapshot-metadata",
		Status:  payment.ProviderStatusPaid,
		Amount:  88,
	})
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
		"apiRateLimitRPS":    "1000",
		"apiRateLimitBurst":  "1000",
	}
}

func requireWiseAuditDetail(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64, action string) map[string]any {
	t.Helper()

	log, err := client.PaymentAuditLog.Query().
		Where(
			paymentauditlog.OrderIDEQ(fmt.Sprintf("%d", orderID)),
			paymentauditlog.ActionEQ(action),
		).
		Only(ctx)
	require.NoError(t, err)

	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(log.Detail), &detail))
	return detail
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

	require.True(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":"balances#credit"}`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":"balances#update"}`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":"account-details-payment#state-change"}`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":"unsupported#event"}`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{"event_type":`))
	require.False(t, wiseWebhookEventIsReconcileTrigger(`{}`))
}

func TestWiseWebhookEnvelopeFromBody(t *testing.T) {
	t.Parallel()

	envelope := wiseWebhookEnvelopeFromBody(`{"event_type":" balances#credit "}`)
	require.Equal(t, "balances#credit", envelope.EventType)

	require.Empty(t, wiseWebhookEnvelopeFromBody(`{"event_type":`).EventType)
	require.Empty(t, wiseWebhookEnvelopeFromBody(`{}`).EventType)
}

type wiseBatchReconcileProvider struct {
	responses         map[string]*payment.QueryOrderResponse
	batchCalls        int
	queryCalls        int
	lastBatchContext  context.Context
	lastBatchTradeNos []string
}

type wiseFailingReconcileProvider struct {
	err        error
	queryCalls int
}

func (p *wiseFailingReconcileProvider) Name() string {
	return "wise-failing-reconcile-provider"
}

func (p *wiseFailingReconcileProvider) ProviderKey() string {
	return payment.TypeWise
}

func (p *wiseFailingReconcileProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeWise}
}

func (p *wiseFailingReconcileProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (p *wiseFailingReconcileProvider) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	p.queryCalls++
	return nil, p.err
}

func (p *wiseFailingReconcileProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *wiseFailingReconcileProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func (p *wiseBatchReconcileProvider) Name() string {
	return "wise-batch-reconcile-provider"
}

func (p *wiseBatchReconcileProvider) ProviderKey() string {
	return payment.TypeWise
}

func (p *wiseBatchReconcileProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeWise}
}

func (p *wiseBatchReconcileProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (p *wiseBatchReconcileProvider) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	p.queryCalls++
	return &payment.QueryOrderResponse{
		Status: payment.ProviderStatusPending,
		Metadata: map[string]string{
			"reconcile_decision": "no_match",
			"reconcile_reason":   "activity_not_found",
		},
	}, nil
}

func (p *wiseBatchReconcileProvider) QueryOrders(ctx context.Context, tradeNos []string) (map[string]*payment.QueryOrderResponse, error) {
	p.batchCalls++
	p.lastBatchContext = ctx
	p.lastBatchTradeNos = append([]string(nil), tradeNos...)
	result := make(map[string]*payment.QueryOrderResponse, len(tradeNos))
	for _, tradeNo := range tradeNos {
		if resp, ok := p.responses[tradeNo]; ok {
			result[tradeNo] = resp
			continue
		}
		result[tradeNo] = &payment.QueryOrderResponse{
			TradeNo: tradeNo,
			Status:  payment.ProviderStatusPending,
			Metadata: map[string]string{
				"reconcile_decision": "no_match",
				"reconcile_reason":   "activity_not_found",
			},
		}
	}
	return result, nil
}

func (p *wiseBatchReconcileProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *wiseBatchReconcileProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func newWiseWebhookFastTestService(t *testing.T, ctx context.Context, client *dbent.Client) (*PaymentService, *rsa.PrivateKey) {
	t.Helper()

	priv, publicKeyPEM := newWiseReconcileWebhookKey(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-webhook-fast").
		SetConfig(encryptWebhookProviderConfig(t, validWiseReconcileConfig(publicKeyPEM))).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}
	return svc, priv
}

func newWiseWebhookFastHTTPTestService(t *testing.T, ctx context.Context, client *dbent.Client) (*PaymentService, *rsa.PrivateKey, *atomic.Int32) {
	t.Helper()

	priv, publicKeyPEM := newWiseReconcileWebhookKey(t)
	apiBase, statementCalls := newWiseReconcileHTTPServer(t)

	config := validWiseReconcileConfig(publicKeyPEM)
	config["apiBase"] = apiBase
	config["apiRateLimitRPS"] = "1000"
	config["apiRateLimitBurst"] = "1000"
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-webhook-fast-http").
		SetConfig(encryptWebhookProviderConfig(t, config)).
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}
	return svc, priv, statementCalls
}

func newWiseReconcileHTTPServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()

	statementCalls := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/profiles/profile-123/balance-statements/balance-123/statement.json":
			statementCalls.Add(1)
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte(`{"transactions":[]}`))
		case "/v1/profiles/profile-123/activities":
			_, _ = w.Write([]byte(`{"activities":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected path"}`))
		}
	}))
	originalTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
		server.Close()
	})
	return server.URL, statementCalls
}

func svcTestWiseProviderInstanceID(t *testing.T, ctx context.Context, client *dbent.Client) int64 {
	t.Helper()

	inst, err := client.PaymentProviderInstance.Query().Only(ctx)
	require.NoError(t, err)
	return inst.ID
}

type wiseTestLeaderLock struct {
	acquired bool
}

func (l *wiseTestLeaderLock) TryAcquireLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	return l.acquired, nil
}

func (l *wiseTestLeaderLock) ReleaseLeaderLock(context.Context, string, string) error {
	return nil
}

func signedWiseWebhookFastHeaders(t *testing.T, priv *rsa.PrivateKey, rawBody, deliveryID string, testNotification bool) map[string]string {
	t.Helper()

	headers := map[string]string{
		"x-signature-sha256": signWiseReconcileWebhook(t, priv, rawBody),
	}
	if deliveryID != "" {
		headers["x-delivery-id"] = deliveryID
	}
	if testNotification {
		headers["x-test-notification"] = "true"
	}
	return headers
}

func requireWiseWebhookDelivery(t *testing.T, ctx context.Context, client *dbent.Client, deliveryID string) *dbent.PaymentWebhookDelivery {
	t.Helper()

	delivery, err := client.PaymentWebhookDelivery.Query().
		Where(paymentwebhookdelivery.ProviderKeyEQ(payment.TypeWise), paymentwebhookdelivery.DeliveryIDEQ(deliveryID)).
		Only(ctx)
	require.NoError(t, err)
	return delivery
}
