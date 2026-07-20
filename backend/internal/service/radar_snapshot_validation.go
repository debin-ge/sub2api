package service

import (
	"errors"
	"strings"
)

// ErrInvalidRadarBucketSnapshot deliberately omits stored values so callers
// can fail closed without leaking raw model labels or cache contents.
var ErrInvalidRadarBucketSnapshot = errors.New("invalid radar bucket snapshot")

// ValidateRadarBucketSnapshot enforces the privacy metadata carried with every
// persisted and public quota snapshot. Inference SampleSize is not used as a
// contributor count because the inference filter can legitimately reject
// otherwise publishable contributors.
func ValidateRadarBucketSnapshot(snapshot BucketSnapshotDTO) error {
	threshold := snapshot.PrivacyThreshold
	minimumThreshold := defaultRadarPublicMinBucketAccounts
	if snapshot.Platform == PlatformOpenAI && normalizeRadarOpenAIPlanTier(snapshot.PlanTier) == "pro" {
		minimumThreshold = radarQuotaOpenAIProMinBucketAccounts
		if threshold == radarQuotaOpenAIProMinBucketAccounts && snapshot.AccountsCount != 1 {
			return ErrInvalidRadarBucketSnapshot
		}
	}
	if threshold < minimumThreshold || snapshot.AccountsCount < threshold {
		return ErrInvalidRadarBucketSnapshot
	}
	for _, window := range []*WindowStatsDTO{snapshot.FiveHour, snapshot.SevenDay} {
		if window != nil && (window.ContributorsCount < threshold || window.ContributorsCount > snapshot.AccountsCount) {
			return ErrInvalidRadarBucketSnapshot
		}
	}
	for _, window := range []*ModelWindowStatsDTO{snapshot.SevenDaySonnet, snapshot.SevenDayFable} {
		if window != nil && (window.SampleSize < threshold || window.SampleSize > snapshot.AccountsCount || !isCanonicalRadarSnapshotModel(snapshot.Platform, window.Model)) {
			return ErrInvalidRadarBucketSnapshot
		}
	}
	for _, breakdown := range [][]ModelCostBreakdownDTO{snapshot.ModelBreakdown5h, snapshot.ModelBreakdown7d} {
		for _, model := range breakdown {
			if model.ContributorsCount < threshold || model.ContributorsCount > snapshot.AccountsCount || !isCanonicalRadarSnapshotModel(snapshot.Platform, model.Model) {
				return ErrInvalidRadarBucketSnapshot
			}
		}
	}
	return nil
}

func isCanonicalRadarSnapshotModel(platform, model string) bool {
	if model == "other" {
		return true
	}
	if platform == PlatformAnthropic && (model == "claude-sonnet" || model == "claude-fable") {
		return true
	}
	if model == "" || model != strings.TrimSpace(model) {
		return false
	}
	for _, canonical := range DefaultModelCatalogIDs(platform) {
		if model == canonical {
			return true
		}
	}
	return false
}
