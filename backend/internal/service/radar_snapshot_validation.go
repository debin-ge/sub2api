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
	if snapshot.CalculationVersion != radarQuotaCalculationVersion {
		return ErrInvalidRadarBucketSnapshot
	}
	snapshot = NormalizeRadarBucketSnapshot(snapshot)
	threshold := snapshot.PrivacyThreshold
	if threshold < defaultRadarPublicMinBucketAccounts || snapshot.AccountsCount < threshold {
		return ErrInvalidRadarBucketSnapshot
	}
	definitions := radarQuotaWindowDefinitionsForPlan(snapshot.Platform, snapshot.PlanTier)
	if len(definitions) == 0 || len(snapshot.Windows) != len(definitions) {
		return ErrInvalidRadarBucketSnapshot
	}
	for index, window := range snapshot.Windows {
		definition := definitions[index]
		if window.Key != definition.Key || window.Label != definition.Label ||
			window.DurationSeconds != int64(definition.Duration.Seconds()) || window.Currency != definition.Currency {
			return ErrInvalidRadarBucketSnapshot
		}
		if window.Stats != nil && (window.Stats.ContributorsCount < threshold || window.Stats.ContributorsCount > snapshot.AccountsCount) {
			return ErrInvalidRadarBucketSnapshot
		}
		for _, modelWindow := range window.ModelWindows {
			if modelWindow.SampleSize < threshold || modelWindow.SampleSize > snapshot.AccountsCount ||
				!isCanonicalRadarSnapshotModel(snapshot.Platform, modelWindow.Model) {
				return ErrInvalidRadarBucketSnapshot
			}
		}
		for _, model := range window.ModelBreakdown {
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
