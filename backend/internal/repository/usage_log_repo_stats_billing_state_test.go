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

	columns := []string{
		"inbound_grouped", "upstream_grouped", "inbound_endpoint", "upstream_endpoint",
		"requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"cost", "actual_cost", "account_cost", "avg_duration_ms",
	}
	rows := sqlmock.NewRows(columns).
		AddRow(1, 1, nil, nil, 1, 2, 3, 1, 3, 0.5, 0, 0.5, 10).
		AddRow(0, 1, "/v1/messages", nil, 1, 2, 3, 1, 3, 0.5, 0, 0.5, 10).
		AddRow(1, 0, nil, "/v1/responses", 1, 2, 3, 1, 3, 0.5, 0, 0.5, 10).
		AddRow(0, 0, "/v1/messages", "/v1/responses", 1, 2, 3, 1, 3, 0.5, 0, 0.5, 10)
	mock.ExpectQuery(`WITH scoped AS[\s\S]*billing_state = \$1[\s\S]*GROUP BY GROUPING SETS`).
		WithArgs(int16(state), start, end).
		WillReturnRows(rows)

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
