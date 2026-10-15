package service

import (
	"maps"
	"math"
	"strings"
)

const claudeFable51MaxReasoningEffortMultiplier = 3.0

func defaultMaxReasoningEffortMultiplier(model string) *float64 {
	if !isClaudeFable51Model(model) {
		return nil
	}
	multiplier := claudeFable51MaxReasoningEffortMultiplier
	return &multiplier
}

// maxReasoningEffortBillingMultiplier retains its legacy name for callers, but
// resolves every effort. Select exactly one value: explicit effort, legacy max,
// then the original model default. An explicit 1 disables either fallback.
func maxReasoningEffortBillingMultiplier(model, effort string, pricing *ModelPricing) float64 {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort != "none" {
		effort = NormalizeMaxReasoningEffort(effort)
	}
	if effort == "" {
		return 1
	}
	if pricing != nil {
		if multiplier, ok := pricing.ReasoningEffortMultipliers[effort]; ok && validReasoningMultiplier(multiplier) {
			return multiplier
		}
		if effort == "max" && pricing.MaxReasoningEffortMultiplier != nil && validReasoningMultiplier(*pricing.MaxReasoningEffortMultiplier) {
			return *pricing.MaxReasoningEffortMultiplier
		}
	}
	if effort == "max" {
		if multiplier := defaultMaxReasoningEffortMultiplier(model); multiplier != nil {
			return *multiplier
		}
	}
	return 1
}

func validReasoningMultiplier(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// EffectiveReasoningEffortMultipliers preserves legacy API inputs when writing
// the new map column. Missing max inherits legacy max; an explicit max wins.
func (p *ChannelModelPricing) EffectiveReasoningEffortMultipliers() map[string]float64 {
	if p == nil {
		return nil
	}
	result := maps.Clone(p.ReasoningEffortMultipliers)
	if _, explicit := result["max"]; !explicit && p.MaxReasoningEffortMultiplier != nil {
		if result == nil {
			result = make(map[string]float64)
		}
		result["max"] = *p.MaxReasoningEffortMultiplier
	}
	return result
}
