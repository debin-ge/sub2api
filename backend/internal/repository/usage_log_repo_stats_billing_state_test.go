package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestGetStatsWithFiltersPropagatesBillingStateToEndpointBreakdowns(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	state := service.BillingStatePricingUnavailable

	mock.ExpectQuery(`FROM usage_logs WHERE billing_state = \$1 AND created_at >= \$2 AND created_at < \$3`).
		WithArgs(int16(state), start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(1, 2, 3, 4, 1, 3, 0.5, 0, 0.5, 10))

	endpointRows := func(endpoint string) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}).
			AddRow(endpoint, 1, 9, 0.5, 0)
	}
	mock.ExpectQuery(`TRIM\(inbound_endpoint\)[\s\S]*billing_state = \$3 GROUP BY endpoint`).
		WithArgs(start, end, int16(state)).
		WillReturnRows(endpointRows("/v1/messages"))
	mock.ExpectQuery(`TRIM\(upstream_endpoint\)[\s\S]*billing_state = \$3 GROUP BY endpoint`).
		WithArgs(start, end, int16(state)).
		WillReturnRows(endpointRows("/v1/responses"))
	mock.ExpectQuery(`CONCAT\([\s\S]*billing_state = \$3 GROUP BY endpoint`).
		WithArgs(start, end, int16(state)).
		WillReturnRows(endpointRows("/v1/messages -> /v1/responses"))

	stats, err := repo.GetStatsWithFilters(context.Background(), usagestats.UsageLogFilters{
		StartTime:    &start,
		EndTime:      &end,
		BillingState: &state,
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Len(t, stats.Endpoints, 1)
	require.Len(t, stats.UpstreamEndpoints, 1)
	require.Len(t, stats.EndpointPaths, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}
