package repository

import (
	"context"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryDerivesVIPActivationPendingFromCompletedPayment(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email:        "vip-pending@example.com",
		Username:     "vip-pending",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, user))

	createCompletedVIPPaymentOrder(t, ctx, client, user)

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, got.IsVIP)
	require.True(t, got.ActivationPending)
	require.Equal(t, service.VIPAccessStateActivationPending, got.AccessState())
}

func TestUserRepositoryForceOffTakesPriorityOverActivationPending(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email:        "vip-restricted@example.com",
		Username:     "vip-restricted",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, user))
	_, err := client.User.UpdateOneID(user.ID).
		SetVipManualOverride(false).
		SetVipOverrideAt(time.Now().UTC()).
		SetVipOverrideBy(9).
		SetVipOverrideReason("risk review").
		SetIsVip(false).
		SetVipEffectiveSource(string(service.VIPEffectiveSourceManualOff)).
		Save(ctx)
	require.NoError(t, err)
	createCompletedVIPPaymentOrder(t, ctx, client, user)

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, got.ActivationPending)
	require.Equal(t, service.VIPAccessStateRestricted, got.AccessState())
}

func TestUserRepositoryClassifiesVIPActivationProjectionSLA(t *testing.T) {
	for _, tt := range []struct {
		name         string
		exists       bool
		withinWindow bool
		wantPending  bool
		wantFailed   bool
		wantAccess   service.VIPAccessState
	}{
		{
			name:   "recent payment remains pending",
			exists: true, withinWindow: true,
			wantPending: true, wantAccess: service.VIPAccessStateActivationPending,
		},
		{
			name:   "overdue payment requires support",
			exists: true, withinWindow: false,
			wantFailed: true, wantAccess: service.VIPAccessStateActivationFailed,
		},
		{
			name:   "no payment requires payment",
			exists: false, withinWindow: false,
			wantAccess: service.VIPAccessStatePaymentRequired,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			repo := newUserRepositoryWithSQL(dbent.NewClient(), db)
			repo.SetVIPActivationPendingWindow(30 * time.Minute)
			mock.ExpectQuery(regexp.QuoteMeta("WITH latest AS (")).
				WithArgs(int64(42), "30m0s").
				WillReturnRows(sqlmock.NewRows([]string{"exists", "within_window"}).
					AddRow(tt.exists, tt.withinWindow))

			user := &service.User{ID: 42}
			repo.hydrateVIPActivationPending(context.Background(), repo.client, user)

			require.Equal(t, tt.wantPending, user.ActivationPending)
			require.Equal(t, tt.wantFailed, user.ActivationFailed)
			require.Equal(t, tt.wantAccess, user.AccessState())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func createCompletedVIPPaymentOrder(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	user *service.User,
) {
	t.Helper()
	now := time.Now().UTC()
	_, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("VIP-PENDING-" + strconv.FormatInt(user.ID, 10)).
		SetOutTradeNo("vip_pending_" + strconv.FormatInt(user.ID, 10)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-vip-pending-" + strconv.FormatInt(user.ID, 10)).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(service.OrderStatusCompleted).
		SetPaidAt(now.Add(-time.Minute)).
		SetCompletedAt(now).
		SetExpiresAt(now.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
}
