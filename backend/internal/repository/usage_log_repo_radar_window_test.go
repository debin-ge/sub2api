package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const accountWindowStatsBatchSQL = `
	SELECT
		account_id,
		COUNT(*) as requests,
		COALESCE(SUM(input_tokens::bigint + output_tokens::bigint + cache_creation_tokens::bigint + cache_read_tokens::bigint), 0) as tokens,
		COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as cost,
		COALESCE(SUM(total_cost), 0) as standard_cost,
		COALESCE(SUM(actual_cost), 0) as user_cost
	FROM usage_logs
	WHERE account_id = ANY($1) AND created_at >= $2
	GROUP BY account_id
`

func TestUsageLogRepositoryGetAccountWindowStatsBatchUsesBigintOperands(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 15, 1, 2, 3, 0, time.UTC)
	accountIDs := []int64{7, 11}

	mock.ExpectQuery(regexp.QuoteMeta(accountWindowStatsBatchSQL)).
		WithArgs(pq.Array(accountIDs), startTime).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "requests", "tokens", "cost", "standard_cost", "user_cost",
		}).AddRow(int64(7), int64(1), int64(4_000_000_000), 1.25, 1.0, 0.75)).
		RowsWillBeClosed()

	got, err := repo.GetAccountWindowStatsBatch(context.Background(), accountIDs, startTime)

	require.NoError(t, err)
	require.Equal(t, int64(4_000_000_000), got[7].Tokens)
	require.NoError(t, mock.ExpectationsWereMet())
}
