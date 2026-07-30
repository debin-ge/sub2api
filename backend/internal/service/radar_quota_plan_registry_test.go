package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRadarQuotaPlanRegistryDeclaresPlatformWindowContracts(t *testing.T) {
	t.Parallel()

	for _, definition := range radarQuotaPlanDefinitions() {
		windowKeys := make([]string, 0, len(definition.WindowKeys))
		for _, window := range radarQuotaWindowDefinitionsForPlan(definition.Platform, definition.Tier) {
			windowKeys = append(windowKeys, window.Key)
			require.NotEmpty(t, window.Label)
			require.Positive(t, window.Duration)
			require.Equal(t, "USD", window.Currency)
		}
		if definition.Platform == PlatformOpenAI {
			require.Equal(t, []string{radarQuotaWindowSevenDay}, windowKeys)
		} else {
			require.Equal(t, []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay}, windowKeys)
		}
	}

	require.Equal(t, radarQuotaOpenAIPlanPro5x, normalizeRadarOpenAIPlanTier("prolite"))
	require.Equal(t, radarQuotaOpenAIPlanPro20x, normalizeRadarOpenAIPlanTier("pro"))
	require.NotEqual(t, normalizeRadarOpenAIPlanTier("prolite"), normalizeRadarOpenAIPlanTier("pro"))
}

func TestNormalizeRadarBucketSnapshotUpgradesLegacyWindowsAndDeepClones(t *testing.T) {
	t.Parallel()

	limit5h := 100.0
	limit7d := 700.0
	input := BucketSnapshotDTO{
		Platform:         PlatformAnthropic,
		PlanTier:         radarQuotaAnthropicPlanMax20x,
		FiveHour:         &WindowStatsDTO{AvgCost: 10, InferredLimitUSD: &limit5h},
		SevenDay:         &WindowStatsDTO{AvgCost: 70, InferredLimitUSD: &limit7d},
		SevenDaySonnet:   &ModelWindowStatsDTO{Model: "claude-sonnet", AvgUtilization: 25, SampleSize: 3},
		SevenDayFable:    &ModelWindowStatsDTO{Model: "claude-fable", AvgUtilization: 15, SampleSize: 3},
		ModelBreakdown5h: []ModelCostBreakdownDTO{{Model: "claude-sonnet", AvgCost: 8}},
		ModelBreakdown7d: []ModelCostBreakdownDTO{{Model: "claude-fable", AvgCost: 40}},
	}

	got := NormalizeRadarBucketSnapshot(input)

	require.Equal(t, []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay}, []string{
		got.Windows[0].Key,
		got.Windows[1].Key,
	})
	require.Equal(t, int64((5*time.Hour)/time.Second), got.Windows[0].DurationSeconds)
	require.Equal(t, int64((7*24*time.Hour)/time.Second), got.Windows[1].DurationSeconds)
	require.Equal(t, "USD", got.Windows[0].Currency)
	require.Equal(t, "USD", got.Windows[1].Currency)
	require.Equal(t, 10.0, got.Windows[0].Stats.AvgCost)
	require.Equal(t, 70.0, got.Windows[1].Stats.AvgCost)
	require.Equal(t, []string{"claude-sonnet", "claude-fable"}, []string{
		got.Windows[1].ModelWindows[0].Model,
		got.Windows[1].ModelWindows[1].Model,
	})
	require.Equal(t, "claude-sonnet", got.Windows[0].ModelBreakdown[0].Model)
	require.Equal(t, "claude-fable", got.Windows[1].ModelBreakdown[0].Model)

	got.Windows[0].Stats.AvgCost = 999
	*got.Windows[0].Stats.InferredLimitUSD = 999
	got.Windows[0].ModelBreakdown[0].Model = "mutated"
	got.Windows[1].ModelWindows[0].Model = "mutated"
	require.Equal(t, 10.0, input.FiveHour.AvgCost)
	require.Equal(t, 100.0, *input.FiveHour.InferredLimitUSD)
	require.Equal(t, "claude-sonnet", input.ModelBreakdown5h[0].Model)
	require.Equal(t, "claude-sonnet", input.SevenDaySonnet.Model)

	require.Equal(t, 10.0, got.FiveHour.AvgCost)
	require.Equal(t, 100.0, *got.FiveHour.InferredLimitUSD)
	require.Equal(t, "claude-sonnet", got.ModelBreakdown5h[0].Model)
	require.Equal(t, "claude-sonnet", got.SevenDaySonnet.Model)
}

func TestNormalizeRadarBucketSnapshotRemovesUnsupportedLegacyOpenAIWindows(t *testing.T) {
	t.Parallel()

	input := BucketSnapshotDTO{
		Platform:       PlatformOpenAI,
		PlanTier:       radarQuotaOpenAIPlanPro20x,
		FiveHour:       &WindowStatsDTO{AvgCost: 5},
		SevenDay:       &WindowStatsDTO{AvgCost: 70},
		SevenDaySonnet: &ModelWindowStatsDTO{Model: "claude-sonnet"},
		ModelBreakdown5h: []ModelCostBreakdownDTO{{
			Model: "other",
		}},
	}

	got := NormalizeRadarBucketSnapshot(input)

	require.Len(t, got.Windows, 1)
	require.Equal(t, radarQuotaWindowSevenDay, got.Windows[0].Key)
	require.Equal(t, 70.0, got.Windows[0].Stats.AvgCost)
	require.Nil(t, got.FiveHour)
	require.Empty(t, got.ModelBreakdown5h)
	require.Nil(t, got.SevenDaySonnet)
	require.Nil(t, got.SevenDayFable)
}
