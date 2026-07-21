package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var _ service.AccountModelBreakdownBatchReader = (*usageLogRepository)(nil)

const accountModelBreakdownSQL = `
	SELECT
		account_id,
		model,
		COUNT(*) as requests,
		COALESCE(SUM(input_tokens::bigint + output_tokens::bigint + cache_creation_tokens::bigint + cache_read_tokens::bigint), 0) as tokens,
		COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as account_cost
	FROM usage_logs
	WHERE account_id = ANY($1) AND created_at >= $2
	GROUP BY account_id, model
`

var accountModelBreakdownColumns = []string{"account_id", "model", "requests", "tokens", "account_cost"}

func expectAccountModelBreakdownQuery(mock sqlmock.Sqlmock, accountIDs []int64, startTime time.Time) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(regexp.QuoteMeta(accountModelBreakdownSQL)).
		WithArgs(pq.Array(accountIDs), startTime).
		RowsWillBeClosed()
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchEmptyIDsDoNotQuery(t *testing.T) {
	for _, tt := range []struct {
		name string
		ids  []int64
	}{
		{name: "nil"},
		{name: "empty", ids: []int64{}},
		{name: "all_non_positive", ids: []int64{0, -1, -99}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := newUsageLogRepositoryWithSQL(nil, db)

			got, err := repo.GetAccountModelBreakdownBatch(context.Background(), tt.ids, time.Now())

			require.NoError(t, err)
			require.NotNil(t, got)
			require.Empty(t, got)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchAggregatesRawModels(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	rows := sqlmock.NewRows(accountModelBreakdownColumns).
		AddRow(int64(7), "claude-opus-4-raw", int64(3), int64(66), 1.25).
		AddRow(int64(7), "claude-sonnet-4-raw", int64(2), int64(44), 0.75).
		AddRow(int64(11), "gpt-5-raw", int64(1), int64(10), 0.0)
	expectAccountModelBreakdownQuery(mock, []int64{7, 11}, startTime).WillReturnRows(rows)

	got, err := repo.GetAccountModelBreakdownBatch(
		context.Background(),
		[]int64{7, 0, 11, 7, -4, 11},
		startTime,
	)

	require.NoError(t, err)
	require.Equal(t, int64(3), got[7]["claude-opus-4-raw"].Requests)
	require.Equal(t, int64(66), got[7]["claude-opus-4-raw"].Tokens)
	require.InDelta(t, 1.25, got[7]["claude-opus-4-raw"].AccountCost, 0)
	require.Equal(t, int64(2), got[7]["claude-sonnet-4-raw"].Requests)
	require.Equal(t, int64(44), got[7]["claude-sonnet-4-raw"].Tokens)
	require.InDelta(t, 0.75, got[7]["claude-sonnet-4-raw"].AccountCost, 0)
	require.Equal(t, int64(1), got[11]["gpt-5-raw"].Requests)
	require.Equal(t, int64(10), got[11]["gpt-5-raw"].Tokens)
	require.Zero(t, got[11]["gpt-5-raw"].AccountCost, "zero-cost rows follow account window stats and remain included")
	require.Len(t, got, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchNoRowsReturnsEmptyMap(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	expectAccountModelBreakdownQuery(mock, []int64{7, 11}, startTime).
		WillReturnRows(sqlmock.NewRows(accountModelBreakdownColumns))

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7, 11}, startTime)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got, "accounts and models without rows must not be synthesized")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchUsesOneArrayParameterForLargeInput(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	accountIDs := make([]int64, 4096)
	for i := range accountIDs {
		accountIDs[i] = int64(i + 1)
	}
	expectAccountModelBreakdownQuery(mock, accountIDs, startTime).
		WillReturnRows(sqlmock.NewRows(accountModelBreakdownColumns))

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), accountIDs, startTime)

	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchPropagatesQueryErrors(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	queryErr := errors.New("breakdown query failed")
	mock.ExpectQuery(regexp.QuoteMeta(accountModelBreakdownSQL)).
		WithArgs(pq.Array([]int64{7}), startTime).
		WillReturnError(queryErr)

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7}, startTime)

	require.Nil(t, got)
	require.ErrorIs(t, err, queryErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchPassesCanceledContext(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := repo.GetAccountModelBreakdownBatch(ctx, []int64{7}, time.Now())

	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchRejectsPartialResultsOnScanError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	rows := sqlmock.NewRows(accountModelBreakdownColumns).
		AddRow(int64(7), "valid-model", int64(1), int64(2), 0.25).
		AddRow("not-an-account-id", "bad-model", int64(1), int64(2), 0.25)
	expectAccountModelBreakdownQuery(mock, []int64{7}, startTime).WillReturnRows(rows)

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7}, startTime)

	require.Nil(t, got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-an-account-id")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchRejectsPartialResultsOnRowsError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	rowsErr := errors.New("breakdown rows failed")
	rows := sqlmock.NewRows(accountModelBreakdownColumns).
		AddRow(int64(7), "valid-model", int64(1), int64(2), 0.25).
		AddRow(int64(8), "unread-model", int64(3), int64(4), 0.5).
		RowError(1, rowsErr)
	expectAccountModelBreakdownQuery(mock, []int64{7, 8}, startTime).WillReturnRows(rows)

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7, 8}, startTime)

	require.Nil(t, got)
	require.ErrorIs(t, err, rowsErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchPropagatesCloseError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	closeErr := errors.New("breakdown close failed")
	rows := sqlmock.NewRows(accountModelBreakdownColumns).
		AddRow(int64(7), "valid-model", int64(1), int64(2), 0.25).
		CloseError(closeErr)
	expectAccountModelBreakdownQuery(mock, []int64{7}, startTime).WillReturnRows(rows)

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7}, startTime)

	require.Nil(t, got)
	require.ErrorIs(t, err, closeErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountModelBreakdownBatchKeepsScanErrorWhenCloseAlsoFails(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	startTime := time.Date(2026, time.July, 13, 1, 2, 3, 0, time.UTC)
	closeErr := errors.New("secondary close failure")
	rows := sqlmock.NewRows(accountModelBreakdownColumns).
		AddRow("not-an-account-id", "bad-model", int64(1), int64(2), 0.25).
		CloseError(closeErr)
	expectAccountModelBreakdownQuery(mock, []int64{7}, startTime).WillReturnRows(rows)

	got, err := repo.GetAccountModelBreakdownBatch(context.Background(), []int64{7}, startTime)

	require.Nil(t, got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-an-account-id")
	require.False(t, strings.Contains(err.Error(), closeErr.Error()), "scan error is the primary error")
	require.NoError(t, mock.ExpectationsWereMet())
}
