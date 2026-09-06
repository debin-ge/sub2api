//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The admin dashboard's "today" card is served from the hourly rollup, cut on
// the caller's timezone. Whatever zone the admin is in, it must report exactly
// what the usage-records page reports for that same local day — that page is
// the raw-table reference. Rows are spread over ±30h so every zone's midnight
// lands somewhere inside the fixture.
//
// Asia/Kolkata (+05:30) is deliberately included: its midnight is not on a
// server-hour bucket boundary (the harness server timezone is UTC), which must
// route through the raw usage_logs fallback rather than drop a partial bucket.
func TestAdminDashboardStats_TodayMatchesUsageRecordsInEveryTimezone(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	aggRepo := newDashboardAggregationRepositoryWithSQL(tx)

	userA := mustCreateUser(t, client, &service.User{Email: "admin-today-a@test.com"})
	userB := mustCreateUser(t, client, &service.User{Email: "admin-today-b@test.com"})
	keyA := mustCreateApiKey(t, client, &service.APIKey{UserID: userA.ID, Key: "sk-admin-today-a", Name: "a"})
	keyB := mustCreateApiKey(t, client, &service.APIKey{UserID: userB.ID, Key: "sk-admin-today-b", Name: "b"})
	account := mustCreateAccount(t, client, &service.Account{Name: "admin-today-account"})

	base := time.Now().UTC()
	for hours := -30; hours <= 30; hours += 3 {
		user, key := userA, keyA
		if hours%2 != 0 {
			user, key = userB, keyB
		}
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
			RequestID: "client:admin-today-" + strconv.Itoa(hours), Model: "gpt-5",
			InputTokens: 100, OutputTokens: 1, CacheReadTokens: 2,
			TotalCost: 0.1, ActualCost: 0.05,
			CreatedAt: base.Add(time.Duration(hours) * time.Hour),
		})
		require.NoError(t, err)
	}
	require.NoError(t, aggRepo.AggregateRange(ctx, base.Add(-31*time.Hour), base.Add(32*time.Hour)))

	for _, tz := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Asia/Kolkata", "Pacific/Kiritimati"} {
		t.Run(tz, func(t *testing.T) {
			loc, err := time.LoadLocation(tz)
			require.NoError(t, err)
			local := time.Now().In(loc)
			start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
			end := start.AddDate(0, 0, 1)

			reference, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
				StartTime: &start,
				EndTime:   &end,
			})
			require.NoError(t, err)
			require.Positive(t, reference.TotalRequests, "fixture should place rows inside every local day")

			aggregated, err := repo.GetDashboardStats(ctx, tz)
			require.NoError(t, err)
			require.Equal(t, reference.TotalRequests, aggregated.TodayRequests, "rollup path: requests")
			require.Equal(t, reference.TotalTokens, aggregated.TodayTokens, "rollup path: tokens")
			require.InDelta(t, reference.TotalActualCost, aggregated.TodayActualCost, 1e-9, "rollup path: actual cost")
			require.InDelta(t, reference.TotalCost, aggregated.TodayCost, 1e-9, "rollup path: cost")

			raw, err := repo.GetDashboardStatsWithRange(ctx, base.Add(-48*time.Hour), base.Add(48*time.Hour), tz)
			require.NoError(t, err)
			require.Equal(t, reference.TotalRequests, raw.TodayRequests, "raw path: requests")
			require.Equal(t, reference.TotalTokens, raw.TodayTokens, "raw path: tokens")
			require.Equal(t, aggregated.ActiveUsers, raw.ActiveUsers, "both paths must agree on today's active users")
		})
	}
}
