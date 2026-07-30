//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAppendUsageLogBillingStateWhereCondition(t *testing.T) {
	pending := service.BillingStatePricingUnavailable

	t.Run("no filter adds nothing", func(t *testing.T) {
		conds, args := appendUsageLogBillingStateWhereCondition(nil, nil, nil, false)
		require.Empty(t, conds)
		require.Empty(t, args)
	})

	t.Run("unsettled uses the partial index predicate", func(t *testing.T) {
		conds, args := appendUsageLogBillingStateWhereCondition(nil, nil, nil, true)
		// 谓词形状必须与 idx_usage_logs_billing_state_pending 一致，否则扫的是全表。
		require.Equal(t, []string{"billing_state = 1"}, conds)
		require.Empty(t, args)
	})

	t.Run("exact state is parameterized", func(t *testing.T) {
		conds, args := appendUsageLogBillingStateWhereCondition(nil, nil, &pending, false)
		require.Equal(t, []string{"billing_state = $1"}, conds)
		require.Equal(t, []any{int16(pending)}, args)
	})

	t.Run("placeholder index continues existing args", func(t *testing.T) {
		conds, args := appendUsageLogBillingStateWhereCondition(
			[]string{"user_id = $1", "model = $2"},
			[]any{int64(7), "gpt-5"},
			&pending,
			true,
		)
		require.Equal(t, []string{"user_id = $1", "model = $2", "billing_state = 1", "billing_state = $3"}, conds)
		require.Equal(t, []any{int64(7), "gpt-5", int16(pending)}, args)
	})
}

func TestListPendingSettlementUsesRecoveryPartialIndexPredicate(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	mock.ExpectQuery(`FROM usage_logs WHERE billing_state = 1 AND id > \$1 ORDER BY id ASC LIMIT \$2`).
		WithArgs(int64(100), 50).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.ListPendingSettlement(context.Background(), 100, 50)

	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkSettlementRecoveredKeepsActualCostZero(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	cost := service.SettlementCost{
		InputCost:         0.1,
		ImageInputCost:    0.2,
		OutputCost:        0.3,
		CacheCreationCost: 0.4,
		CacheReadCost:     0.5,
		ImageOutputCost:   0.6,
		TotalCost:         2.1,
		BillingMode:       string(service.BillingModeToken),
	}

	mock.ExpectExec(`UPDATE usage_logs SET[\s\S]*actual_cost = 0,[\s\S]*account_stats_cost = \$9,[\s\S]*WHERE id = \$1 AND billing_state = \$12`).
		WithArgs(
			int64(9),
			cost.InputCost,
			cost.ImageInputCost,
			cost.OutputCost,
			cost.CacheCreationCost,
			cost.CacheReadCost,
			cost.ImageOutputCost,
			cost.TotalCost,
			cost.AccountStatsCost,
			cost.BillingMode,
			int16(service.BillingStatePricingRecovered),
			int16(service.BillingStatePricingUnavailable),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.MarkSettlementRecovered(context.Background(), 9, cost)

	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 待结算行是全表里的异常少数，走部分索引后精确 COUNT(*) 很便宜；而这个视图问的就是
// "还欠多少笔账"，给个估算总数等于没答。
func TestShouldUseFastUsageLogTotalRejectsBillingStateFilters(t *testing.T) {
	pending := service.BillingStatePricingUnavailable

	require.True(t, shouldUseFastUsageLogTotal(usagestats.UsageLogFilters{}),
		"前提：无筛选时本来会走估算总数")
	require.False(t, shouldUseFastUsageLogTotal(usagestats.UsageLogFilters{BillingStateUnsettled: true}))
	require.False(t, shouldUseFastUsageLogTotal(usagestats.UsageLogFilters{BillingState: &pending}))
}

func TestUsageLogSuccessFilterIncludesPricingRecoveryStates(t *testing.T) {
	require.Contains(t, usageLogSuccessFilterUL, "ul.billing_state <> 0")
}
