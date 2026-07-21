package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validPrivacySnapshot() BucketSnapshotDTO {
	return BucketSnapshotDTO{
		BucketKey: "anthropic/pro", Platform: "anthropic", PlanTier: "pro", DisplayName: "Claude Pro",
		PrivacyThreshold: 3, AccountsCount: 3, CapturedAt: time.Now().UTC(),
		FiveHour:         &WindowStatsDTO{ContributorsCount: 3, SampleSize: 1},
		SevenDay:         &WindowStatsDTO{ContributorsCount: 3, SampleSize: 2},
		SevenDaySonnet:   &ModelWindowStatsDTO{Model: "claude-sonnet", SampleSize: 3},
		ModelBreakdown5h: []ModelCostBreakdownDTO{{Model: "other", ContributorsCount: 3}},
		ModelBreakdown7d: []ModelCostBreakdownDTO{},
	}
}

func TestValidateRadarBucketSnapshotFailsClosedOnPrivacyMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BucketSnapshotDTO)
	}{
		{"missing threshold", func(s *BucketSnapshotDTO) { s.PrivacyThreshold = 0 }},
		{"single account", func(s *BucketSnapshotDTO) { s.AccountsCount = 1 }},
		{"window contributors", func(s *BucketSnapshotDTO) { s.FiveHour.ContributorsCount = 2 }},
		{"model contributors", func(s *BucketSnapshotDTO) { s.SevenDaySonnet.SampleSize = 2 }},
		{"breakdown contributors", func(s *BucketSnapshotDTO) { s.ModelBreakdown5h[0].ContributorsCount = 2 }},
		{"raw model label", func(s *BucketSnapshotDTO) { s.ModelBreakdown5h[0].Model = "secret-custom-model" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validPrivacySnapshot()
			tt.mutate(&snapshot)
			require.Error(t, ValidateRadarBucketSnapshot(snapshot))
		})
	}

	snapshot := validPrivacySnapshot()
	require.NoError(t, ValidateRadarBucketSnapshot(snapshot))
	require.Less(t, snapshot.FiveHour.SampleSize, snapshot.PrivacyThreshold,
		"inference sample size is intentionally distinct from contributor count")
}

func TestValidateRadarBucketSnapshotAllowsOnlySingletonChatGPTProException(t *testing.T) {
	pro := BucketSnapshotDTO{
		BucketKey: "openai/pro", Platform: PlatformOpenAI, PlanTier: "pro", DisplayName: "ChatGPT Pro",
		PrivacyThreshold: 1, AccountsCount: 1, CapturedAt: time.Now().UTC(),
		SevenDay: &WindowStatsDTO{ContributorsCount: 1, SampleSize: 1},
	}
	require.NoError(t, ValidateRadarBucketSnapshot(pro))
	overbroad := pro
	overbroad.AccountsCount = 2
	overbroad.SevenDay = &WindowStatsDTO{ContributorsCount: 2, SampleSize: 2}
	require.ErrorIs(t, ValidateRadarBucketSnapshot(overbroad), ErrInvalidRadarBucketSnapshot)

	plus := pro
	plus.BucketKey = "openai/plus"
	plus.PlanTier = "plus"
	plus.DisplayName = "ChatGPT Plus"
	require.ErrorIs(t, ValidateRadarBucketSnapshot(plus), ErrInvalidRadarBucketSnapshot)
}
