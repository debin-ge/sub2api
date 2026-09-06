package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoQuotaTimeCalendarChangeCannotReinterpretPendingIntent(t *testing.T) {
	zone := "Asia/Shanghai"
	if timezone.Location().String() == zone {
		zone = "UTC"
	}
	command := &service.UsageBillingCommand{QuotaTime: &service.UsageBillingQuotaTime{Version: 1, TimeZone: zone}}
	_, err := prepareVideoQuotaPostingCommand(context.Background(), nil, command)
	require.ErrorIs(t, err, service.ErrUsageBillingQuotaCalendarMismatch)
}

func TestVideoQuotaTimePlatformLateEventPreservesNewerWindows(t *testing.T) {
	day := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	week := day.AddDate(0, 0, -5)
	occurredAt := day.Add(12 * time.Hour)
	clock := &service.UsageBillingQuotaTime{Version: 1, DayStart: day, WeekStart: week}
	for _, source := range []string{"database", "cache_snapshot"} {
		t.Run(source, func(t *testing.T) {
			future := day.AddDate(0, 0, 35)
			row := usageBillingPlatformQuotaRow{
				daily:   usageBillingQuotaWindow{usage: 7, start: &future},
				weekly:  usageBillingQuotaWindow{usage: 8, start: &future},
				monthly: usageBillingQuotaWindow{usage: 9, start: &future},
			}
			var snapshot *service.UsageBillingPlatformQuotaSnapshot
			if source == "cache_snapshot" {
				snapshot = &service.UsageBillingPlatformQuotaSnapshot{
					DailyUsageUSD: 7, WeeklyUsageUSD: 8, MonthlyUsageUSD: 9,
					DailyWindowStart: &future, WeeklyWindowStart: &future, MonthlyWindowStart: &future,
				}
				row = usageBillingPlatformQuotaRow{}
			}
			result := reconciledUsageBillingPlatformQuotaAtEvent(row, snapshot, 3, occurredAt, clock)
			require.Equal(t, 7.0, result.daily.usage)
			require.Equal(t, 8.0, result.weekly.usage)
			require.Equal(t, 9.0, result.monthly.usage)
			for _, window := range []usageBillingQuotaWindow{result.daily, result.weekly, result.monthly} {
				require.Equal(t, future, *window.start)
			}
		})
	}
}

func TestVideoQuotaTimePlatformCountsOnlyApplicableWindows(t *testing.T) {
	day := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	week := day.AddDate(0, 0, -5)
	month := day.AddDate(0, 0, -14)
	nextDay := day.AddDate(0, 0, 1)
	row := usageBillingPlatformQuotaRow{
		daily:   usageBillingQuotaWindow{usage: 7, start: &nextDay},
		weekly:  usageBillingQuotaWindow{usage: 8, start: &week},
		monthly: usageBillingQuotaWindow{usage: 9, start: &month},
	}
	clock := &service.UsageBillingQuotaTime{Version: 1, DayStart: day, WeekStart: week}
	result := reconciledUsageBillingPlatformQuotaAtEvent(row, nil, 3, day.Add(12*time.Hour), clock)
	require.Equal(t, 7.0, result.daily.usage)
	require.Equal(t, 11.0, result.weekly.usage)
	require.Equal(t, 12.0, result.monthly.usage)
	require.Equal(t, nextDay, *result.daily.start)
	require.Equal(t, week, *result.weekly.start)
	require.Equal(t, month, *result.monthly.start)
}

func TestVideoQuotaTimePlatformEmptyAndExpiredWindowsUseFrozenTime(t *testing.T) {
	day := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	week := day.AddDate(0, 0, -5)
	occurredAt := day.Add(12 * time.Hour)
	clock := &service.UsageBillingQuotaTime{Version: 1, DayStart: day, WeekStart: week}
	for _, existing := range []bool{false, true} {
		row := usageBillingPlatformQuotaRow{}
		if existing {
			oldStart := occurredAt.Add(-30 * 24 * time.Hour)
			row.daily, row.weekly, row.monthly = usageBillingQuotaWindow{usage: 99, start: &oldStart}, usageBillingQuotaWindow{usage: 99, start: &oldStart}, usageBillingQuotaWindow{usage: 99, start: &oldStart}
		}
		result := reconciledUsageBillingPlatformQuotaAtEvent(row, nil, 3, occurredAt, clock)
		require.Equal(t, 3.0, result.daily.usage)
		require.Equal(t, 3.0, result.weekly.usage)
		require.Equal(t, 3.0, result.monthly.usage)
		require.Equal(t, day, *result.daily.start)
		require.Equal(t, week, *result.weekly.start)
		require.Equal(t, occurredAt, *result.monthly.start)
	}
}
