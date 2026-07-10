package service

import "strings"

// ApplyGroupModelsList filters available models by a group's selected list while preserving selection order.
func ApplyGroupModelsList(available, selected []string) []string {
	if len(selected) == 0 {
		return normalizeCatalogModelIDs(available)
	}
	return filterSelectedModelsByPatterns(available, selected)
}

func filterSelectedModelsByPatterns(available, selected []string) []string {
	patterns := make([]string, 0, len(available))
	for _, pattern := range available {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}

	seen := make(map[string]struct{}, len(selected))
	filtered := make([]string, 0, len(selected))
	for _, model := range selected {
		model = strings.TrimSpace(model)
		if model == "" || !modelAllowedByPatterns(patterns, model) {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		filtered = append(filtered, model)
	}
	return filtered
}

func modelAllowedByPatterns(patterns []string, model string) bool {
	for _, pattern := range patterns {
		if pattern == model {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func normalizeGroupModelsListConfig(cfg GroupModelsListConfig) GroupModelsListConfig {
	out := GroupModelsListConfig{Enabled: cfg.Enabled}
	if len(cfg.Models) == 0 {
		return out
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		out.Models = nil
	}
	return out
}

func (g *Group) CustomModelsListEnabled() bool {
	return g != nil && g.ModelsListConfig.Enabled && len(g.ModelsListConfig.Models) > 0
}
