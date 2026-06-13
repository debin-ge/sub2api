//go:build unit

package service

import (
	"context"
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
