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

// The dashboard's "today" window must be a half-open [start, end) range anchored
// on the caller's timezone, so that the dashboard card and the usage-records page
// (which filters on the same half-open range) report identical totals.
//
// Before the fix the query was an open-ended `created_at >= today` in the
// server-configured timezone, which both counted future-dated rows forever and
// drifted away from the usage page whenever the viewer's timezone differed.
func TestUserDashboardStats_TodayWindowIsHalfOpenAndTimezoneAnchored(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "dashboard-today@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-dashboard-today", Name: "today"})
	account := mustCreateAccount(t, client, &service.Account{Name: "dashboard-today-account"})

	// Deliberately different from the harness's server timezone (UTC) so a
	// server-anchored window would produce a different answer.
	const userTZ = "Asia/Shanghai"
	loc, err := time.LoadLocation(userTZ)
	require.NoError(t, err)

	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayEnd := todayStart.AddDate(0, 0, 1)

	cases := []struct {
		name      string
		createdAt time.Time
		tokens    int
		inToday   bool
	}{
		{"just before local midnight", todayStart.Add(-time.Second), 1000, false},
		{"exactly local midnight", todayStart, 10, true},
		{"now", now, 20, true},
		{"future row beyond today", todayEnd.Add(time.Hour), 2000, false},
	}

	var wantTodayTokens int64
	for _, tc := range cases {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			RequestID: "client:" + tc.name, Model: "gpt-5",
			InputTokens: tc.tokens, OutputTokens: tc.tokens,
			TotalCost: 0.1, ActualCost: 0.1, CreatedAt: tc.createdAt,
		})
		require.NoError(t, err)
		if tc.inToday {
			wantTodayTokens += int64(tc.tokens) * 2
		}
	}

	stats, err := repo.GetUserDashboardStats(ctx, user.ID, userTZ)
	require.NoError(t, err)
	require.Equal(t, wantTodayTokens, stats.TodayTokens,
		"dashboard today tokens must exclude yesterday and future-dated rows")
	require.Equal(t, int64(2), stats.TodayRequests)

	// The usage-records page filters the same half-open window; both views must agree.
	listStats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
		UserID:    user.ID,
		StartTime: &todayStart,
		EndTime:   &todayEnd,
	})
	require.NoError(t, err)
	require.Equal(t, listStats.TotalTokens, stats.TodayTokens,
		"dashboard today tokens must match the usage-records total for the same day")
	require.Equal(t, listStats.TotalRequests, stats.TodayRequests)
}

// Whatever timezone the caller is in, the dashboard's "today" must cover exactly
// the same rows as the usage-records page filtered to that same local day. Rows
// are spread across a wide span so that each timezone's day boundary lands in a
// different place — an anchoring regression breaks at least one of them
// regardless of what time of day the suite runs at.
func TestUserDashboardStats_TodayMatchesUsageRecordsInEveryTimezone(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "dashboard-today-tz@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-dashboard-tz", Name: "tz"})
	account := mustCreateAccount(t, client, &service.Account{Name: "dashboard-tz-account"})

	base := time.Now().UTC()
	for hours := -30; hours <= 30; hours += 3 {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			RequestID: "client:tz-" + strconv.Itoa(hours), Model: "gpt-5",
			InputTokens: 100, OutputTokens: 1,
			TotalCost: 0.1, ActualCost: 0.1,
			CreatedAt: base.Add(time.Duration(hours) * time.Hour),
		})
		require.NoError(t, err)
	}

	// Spread across the UTC offset range, including a +14 zone where the local
	// date is often a day ahead of the server's.
	for _, tz := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Pacific/Kiritimati", "Asia/Kolkata"} {
		t.Run(tz, func(t *testing.T) {
			loc, err := time.LoadLocation(tz)
			require.NoError(t, err)
			local := time.Now().In(loc)
			start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
			end := start.AddDate(0, 0, 1)

			stats, err := repo.GetUserDashboardStats(ctx, user.ID, tz)
			require.NoError(t, err)

			listStats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
				UserID:    user.ID,
				StartTime: &start,
				EndTime:   &end,
			})
			require.NoError(t, err)

			require.Equal(t, listStats.TotalRequests, stats.TodayRequests,
				"dashboard today requests must match the usage-records count for the same local day")
			require.Equal(t, listStats.TotalTokens, stats.TodayTokens,
				"dashboard today tokens must match the usage-records total for the same local day")
			require.Positive(t, stats.TodayRequests, "fixture should place rows inside every local day")
		})
	}
}

// An empty timezone must keep the previous behaviour: fall back to the
// server-configured timezone rather than erroring or silently using UTC.
func TestUserDashboardStats_EmptyTimezoneFallsBackToServerTimezone(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "dashboard-today-fallback@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-dashboard-fallback", Name: "fallback"})
	account := mustCreateAccount(t, client, &service.Account{Name: "dashboard-fallback-account"})

	_, err := repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: "client:fallback-now", Model: "gpt-5",
		InputTokens: 7, OutputTokens: 3,
		TotalCost: 0.1, ActualCost: 0.1, CreatedAt: time.Now(),
	})
	require.NoError(t, err)

	stats, err := repo.GetUserDashboardStats(ctx, user.ID, "")
	require.NoError(t, err)
	require.Equal(t, int64(10), stats.TodayTokens)
	require.Equal(t, int64(1), stats.TodayRequests)
}
