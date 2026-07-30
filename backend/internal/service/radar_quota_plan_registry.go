package service

import (
	"strings"
	"time"
)

const (
	radarQuotaWindowFiveHour = "5h"
	radarQuotaWindowSevenDay = "7d"
)

type radarQuotaWindowDefinition struct {
	Key      string
	Label    string
	Duration time.Duration
	Currency string
}

var radarQuotaWindowDefinitions = map[string]radarQuotaWindowDefinition{
	radarQuotaWindowFiveHour: {
		Key:      radarQuotaWindowFiveHour,
		Label:    "5H",
		Duration: 5 * time.Hour,
		Currency: "USD",
	},
	radarQuotaWindowSevenDay: {
		Key:      radarQuotaWindowSevenDay,
		Label:    "7D",
		Duration: 7 * 24 * time.Hour,
		Currency: "USD",
	},
}

// radarQuotaPlanDefinition centralizes the public quota taxonomy. Account
// classification, cache validation, and display names must all use this
// registry so adding or retiring a plan cannot leave conflicting switch
// statements behind.
type radarQuotaPlanDefinition struct {
	Platform    string
	Tier        string
	DisplayName string
	Aliases     []string
	WindowKeys  []string
}

func radarQuotaPlanDefinitions() []radarQuotaPlanDefinition {
	return []radarQuotaPlanDefinition{
		{
			Platform:    PlatformAnthropic,
			Tier:        radarQuotaAnthropicPlanGeneric,
			DisplayName: "Claude Subscription",
			WindowKeys:  []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformAnthropic,
			Tier:        radarQuotaAnthropicPlanPro,
			DisplayName: "Claude Pro",
			Aliases:     []string{"pro", "claude_pro", "claudepro"},
			WindowKeys:  []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformAnthropic,
			Tier:        radarQuotaAnthropicPlanMax5x,
			DisplayName: "Claude Max 5x",
			Aliases:     []string{"max_5x", "max5x", "5x_max", "5xmax", "claude_max_5x"},
			WindowKeys:  []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformAnthropic,
			Tier:        radarQuotaAnthropicPlanMax20x,
			DisplayName: "Claude Max 20x",
			Aliases:     []string{"max_20x", "max20x", "20x_max", "20xmax", "claude_max_20x"},
			WindowKeys:  []string{radarQuotaWindowFiveHour, radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformOpenAI,
			Tier:        radarQuotaOpenAIPlanPlus,
			DisplayName: "ChatGPT Plus",
			Aliases:     []string{"plus", "chatgpt_plus", "chatgptplus"},
			WindowKeys:  []string{radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformOpenAI,
			Tier:        radarQuotaOpenAIPlanPro5x,
			DisplayName: "ChatGPT Pro 5x",
			Aliases: []string{
				"prolite",
				"5x_pro", "5xpro", "pro_5x", "pro5x", "pro-5x", "chatgpt_pro_5x", "chatgpt_5x_pro",
			},
			WindowKeys: []string{radarQuotaWindowSevenDay},
		},
		{
			Platform:    PlatformOpenAI,
			Tier:        radarQuotaOpenAIPlanPro20x,
			DisplayName: "ChatGPT Pro 20x",
			Aliases: []string{
				"pro", "chatgpt_pro", "chatgptpro",
				"20x_pro", "20xpro", "pro_20x", "pro20x", "pro-20x", "chatgpt_pro_20x", "chatgpt_20x_pro",
			},
			WindowKeys: []string{radarQuotaWindowSevenDay},
		},
	}
}

func radarQuotaPlanDefinitionFor(platform, planTier string) (radarQuotaPlanDefinition, bool) {
	for _, definition := range radarQuotaPlanDefinitions() {
		if definition.Platform == platform && definition.Tier == planTier {
			return definition, true
		}
	}
	return radarQuotaPlanDefinition{}, false
}

func normalizeRadarQuotaPlanTier(platform, planTier string) string {
	normalized := strings.ToLower(strings.TrimSpace(planTier))
	for _, definition := range radarQuotaPlanDefinitions() {
		if definition.Platform != platform {
			continue
		}
		for _, alias := range definition.Aliases {
			if alias == normalized {
				return definition.Tier
			}
		}
	}
	return ""
}

func normalizeRadarAnthropicPlanTier(planTier string) string {
	return normalizeRadarQuotaPlanTier(PlatformAnthropic, planTier)
}

func normalizeRadarOpenAIPlanTier(planTier string) string {
	// OpenAI reports the standard personal Pro subscription as plan_type=pro.
	// The registry therefore treats that canonical upstream value as 20x.
	return normalizeRadarQuotaPlanTier(PlatformOpenAI, planTier)
}

func isSupportedRadarQuotaPlanTier(platform, planTier string) bool {
	_, ok := radarQuotaPlanDefinitionFor(platform, planTier)
	return ok
}

func radarQuotaDisplayName(platform, planTier string) string {
	definition, ok := radarQuotaPlanDefinitionFor(platform, planTier)
	if !ok {
		return ""
	}
	return definition.DisplayName
}

func radarQuotaWindowDefinitionsForPlan(platform, planTier string) []radarQuotaWindowDefinition {
	plan, ok := radarQuotaPlanDefinitionFor(platform, planTier)
	if !ok {
		return nil
	}
	result := make([]radarQuotaWindowDefinition, 0, len(plan.WindowKeys))
	for _, key := range plan.WindowKeys {
		if definition, exists := radarQuotaWindowDefinitions[key]; exists {
			result = append(result, definition)
		}
	}
	return result
}

// NormalizeRadarBucketSnapshot upgrades legacy Redis snapshots to the generic
// window representation while preserving the legacy fields for rolling
// frontend/backend deployments.
func NormalizeRadarBucketSnapshot(snapshot BucketSnapshotDTO) BucketSnapshotDTO {
	existing := make(map[string]QuotaWindowDTO, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		if _, duplicate := existing[window.Key]; window.Key == "" || duplicate {
			continue
		}
		existing[window.Key] = cloneRadarServiceQuotaWindow(window)
	}

	windows := make([]QuotaWindowDTO, 0, len(radarQuotaWindowDefinitionsForPlan(snapshot.Platform, snapshot.PlanTier)))
	for _, definition := range radarQuotaWindowDefinitionsForPlan(snapshot.Platform, snapshot.PlanTier) {
		window, exists := existing[definition.Key]
		if !exists {
			window = legacyRadarQuotaWindow(snapshot, definition.Key)
		}
		window.Key = definition.Key
		window.Label = definition.Label
		window.DurationSeconds = int64(definition.Duration / time.Second)
		window.Currency = definition.Currency
		if window.ModelWindows == nil {
			window.ModelWindows = make([]ModelWindowStatsDTO, 0)
		}
		if window.ModelBreakdown == nil {
			window.ModelBreakdown = make([]ModelCostBreakdownDTO, 0)
		}
		windows = append(windows, window)
	}
	snapshot.Windows = windows

	// Rebuild every legacy compatibility field from the canonical window list.
	// This prevents unsupported or removed window data from surviving when a
	// snapshot already contains the generic representation.
	snapshot.FiveHour = nil
	snapshot.SevenDay = nil
	snapshot.SevenDaySonnet = nil
	snapshot.SevenDayFable = nil
	snapshot.ModelBreakdown5h = make([]ModelCostBreakdownDTO, 0)
	snapshot.ModelBreakdown7d = make([]ModelCostBreakdownDTO, 0)
	for index := range snapshot.Windows {
		window := snapshot.Windows[index]
		switch window.Key {
		case radarQuotaWindowFiveHour:
			snapshot.FiveHour = cloneRadarServiceWindowStats(window.Stats)
			snapshot.ModelBreakdown5h = cloneRadarServiceModelBreakdown(window.ModelBreakdown)
		case radarQuotaWindowSevenDay:
			snapshot.SevenDay = cloneRadarServiceWindowStats(window.Stats)
			snapshot.ModelBreakdown7d = cloneRadarServiceModelBreakdown(window.ModelBreakdown)
			for _, modelWindow := range window.ModelWindows {
				switch modelWindow.Model {
				case "claude-sonnet":
					snapshot.SevenDaySonnet = cloneRadarServiceModelWindowStats(&modelWindow)
				case "claude-fable":
					snapshot.SevenDayFable = cloneRadarServiceModelWindowStats(&modelWindow)
				}
			}
		}
	}
	return snapshot
}

func legacyRadarQuotaWindow(snapshot BucketSnapshotDTO, key string) QuotaWindowDTO {
	window := QuotaWindowDTO{
		ModelWindows:   make([]ModelWindowStatsDTO, 0),
		ModelBreakdown: make([]ModelCostBreakdownDTO, 0),
	}
	switch key {
	case radarQuotaWindowFiveHour:
		window.Stats = cloneRadarServiceWindowStats(snapshot.FiveHour)
		window.ModelBreakdown = cloneRadarServiceModelBreakdown(snapshot.ModelBreakdown5h)
	case radarQuotaWindowSevenDay:
		window.Stats = cloneRadarServiceWindowStats(snapshot.SevenDay)
		window.ModelBreakdown = cloneRadarServiceModelBreakdown(snapshot.ModelBreakdown7d)
		if snapshot.Platform == PlatformAnthropic {
			for _, modelWindow := range []*ModelWindowStatsDTO{snapshot.SevenDaySonnet, snapshot.SevenDayFable} {
				if cloned := cloneRadarServiceModelWindowStats(modelWindow); cloned != nil {
					window.ModelWindows = append(window.ModelWindows, *cloned)
				}
			}
		}
	}
	return window
}
