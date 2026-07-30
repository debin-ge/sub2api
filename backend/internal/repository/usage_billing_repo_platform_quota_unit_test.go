//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestReconciledUsageBillingPlatformQuota_ExpiredMonthlySnapshotDoesNotDiscardSerializedUsage(t *testing.T) {
	firstCommitAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	secondCommitAt := firstCommitAt.Add(time.Second)
	expiredSnapshotStart := secondCommitAt.Add(-31 * 24 * time.Hour)

	row := usageBillingPlatformQuotaRow{
		monthly: usageBillingQuotaWindow{
			usage: 1,
			start: &firstCommitAt,
		},
	}
	snapshot := &service.UsageBillingPlatformQuotaSnapshot{
		MonthlyUsageUSD:    10,
		MonthlyWindowStart: &expiredSnapshotStart,
	}

	got := reconciledUsageBillingPlatformQuota(row, snapshot, 2, secondCommitAt)

	require.NotNil(t, got.monthly.start)
	require.Equal(t, firstCommitAt, *got.monthly.start)
	require.InDelta(t, 3, got.monthly.usage, 1e-9)
}

func TestReconciledUsageBillingPlatformQuota_NilSnapshotAccumulatesSerializedUsage(t *testing.T) {
	firstCommitAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)

	first := reconciledUsageBillingPlatformQuota(
		usageBillingPlatformQuotaRow{},
		nil,
		1,
		firstCommitAt,
	)
	second := reconciledUsageBillingPlatformQuota(
		first,
		nil,
		2,
		firstCommitAt.Add(time.Second),
	)

	require.NotNil(t, second.monthly.start)
	require.Equal(t, firstCommitAt, *second.monthly.start)
	require.InDelta(t, 3, second.monthly.usage, 1e-9)
}

func TestReconciledUsageBillingPlatformQuota_CurrentSnapshotStillMergesBeforeIncrement(t *testing.T) {
	windowStart := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	now := windowStart.Add(9 * 24 * time.Hour)
	row := usageBillingPlatformQuotaRow{
		monthly: usageBillingQuotaWindow{usage: 4, start: &windowStart},
	}
	snapshot := &service.UsageBillingPlatformQuotaSnapshot{
		MonthlyUsageUSD:    10,
		MonthlyWindowStart: &windowStart,
	}

	got := reconciledUsageBillingPlatformQuota(row, snapshot, 2, now)

	require.NotNil(t, got.monthly.start)
	require.Equal(t, windowStart, *got.monthly.start)
	require.InDelta(t, 12, got.monthly.usage, 1e-9)
}
