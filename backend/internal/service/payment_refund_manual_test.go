package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestConfirmManualRefundDefaultFlowMarksRefundedAndDeductsLocally(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("manual-refund-default@example.com").
		SetPasswordHash("hash").
		SetUsername("manual-refund-default-user").
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise-manual-refund-default").
		SetConfig("{}").
		SetSupportedTypes("wise").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(50).
		SetFeeRate(0).
		SetRechargeCode("MANUAL-REFUND-DEFAULT-ORDER").
		SetOutTradeNo("sub2_manual_refund_default").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-tx-default").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusRefunding).
		SetRefundAmount(50).
		SetRefundReason("manual required").
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)

	var deducted float64
	svc := &PaymentService{
		entClient: client,
		userRepo: &manualRefundUserRepo{
			user: &User{ID: user.ID, Balance: 500},
			deductBalanceFn: func(ctx context.Context, id int64, amount float64) error {
				require.Equal(t, user.ID, id)
				deducted += amount
				return nil
			},
		},
	}

	result, err := svc.ConfirmManualRefund(ctx, order.ID, 50, "manual completed", "wise-refund-default", true, true)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, 50.0, deducted)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunded, reloaded.Status)
	require.NotNil(t, reloaded.RefundAt)

	confirmedAudits, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("WISE_MANUAL_REFUND_CONFIRMED")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, confirmedAudits)
}

type manualRefundUserRepo struct {
	UserRepository
	user            *User
	deductBalanceFn func(ctx context.Context, id int64, amount float64) error
}

func (r *manualRefundUserRepo) GetByID(context.Context, int64) (*User, error) {
	return r.user, nil
}

func (r *manualRefundUserRepo) DeductBalance(ctx context.Context, id int64, amount float64) error {
	return r.deductBalanceFn(ctx, id, amount)
}
