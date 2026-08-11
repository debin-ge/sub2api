package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageLogBusinessStatsFilterSupportsAliases(t *testing.T) {
	require.Equal(t, "(request_id IS NULL OR request_id NOT LIKE 'internal-relay:%')", usageLogBusinessStatsFilter(""))
	require.Equal(t, "(ul.request_id IS NULL OR ul.request_id NOT LIKE 'internal-relay:%')", usageLogBusinessStatsFilter(" ul "))
}

func TestGetGlobalStatsExcludesInternalRelayRows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("request_id NOT LIKE 'internal-relay:%'")).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_tokens",
			"total_cost", "total_actual_cost", "avg_duration_ms",
		}).AddRow(int64(2), int64(10), int64(20), int64(3), 0.3, 0.2, 25.0))

	stats, err := repo.GetGlobalStats(context.Background(), start, end)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.TotalRequests)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardPreaggregationExcludesInternalRelayRows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	filter := regexp.QuoteMeta("request_id NOT LIKE 'internal-relay:%'")

	mock.ExpectExec(`(?s)INSERT INTO usage_dashboard_hourly_users.*`+filter).
		WithArgs(start, end, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.insertHourlyActiveUsers(context.Background(), start, end))

	mock.ExpectExec(`(?s)WITH hourly AS.*FROM usage_logs.*`+filter).
		WithArgs(start, end, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.upsertHourlyAggregates(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}
