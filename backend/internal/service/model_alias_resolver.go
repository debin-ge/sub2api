package service

import (
	"sort"
	"strings"
)

const ModelAliasSourceProviderDefault = "provider_default_alias"
const ModelAliasSourceAccountMapping = "account_model_mapping"

type ModelAliasRule struct {
	AliasPattern string
	TargetModel  string
}

type ModelAliasResolution struct {
	RequestedModel string
	UpstreamModel  string
	Provider       string
	Source         string
	MatchedPattern string
}

func ResolveProviderModelAlias(provider, requested string) (ModelAliasResolution, bool) {
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return ModelAliasResolution{}, false
	}
	caps, ok := domesticProviderCapabilities[provider]
	if !ok {
		return ModelAliasResolution{}, false
	}
	for _, rule := range caps.AliasRules {
		if matchModelPattern(rule.AliasPattern, trimmed) {
			return ModelAliasResolution{
				RequestedModel: trimmed,
				UpstreamModel:  rule.TargetModel,
				Provider:       provider,
				Source:         ModelAliasSourceProviderDefault,
				MatchedPattern: rule.AliasPattern,
			}, true
		}
	}
	return ModelAliasResolution{}, false
}

func ResolveAccountProviderModel(account *Account, requested string) (ModelAliasResolution, bool) {
	if account == nil {
		return ModelAliasResolution{}, false
	}
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return ModelAliasResolution{}, false
	}
	mapping := account.GetModelMapping()
	if mapped, pattern, ok := resolveAccountMappingWithPattern(mapping, trimmed); ok {
		mapped = strings.TrimSpace(mapped)
		if !providerSupportsUpstreamModel(account.Platform, mapped) {
			return ModelAliasResolution{}, false
		}
		return ModelAliasResolution{
			RequestedModel: trimmed,
			UpstreamModel:  mapped,
			Provider:       account.Platform,
			Source:         ModelAliasSourceAccountMapping,
			MatchedPattern: pattern,
		}, true
	}
	// An explicit account mapping is also an allow-list. Falling through to
	// provider defaults here would make a configured restriction ineffective.
	if len(mapping) > 0 {
		return ModelAliasResolution{}, false
	}
	return ResolveProviderModelAlias(account.Platform, trimmed)
}

func resolveAccountMappingWithPattern(mapping map[string]string, requested string) (mappedModel, pattern string, matched bool) {
	if requested == "" || len(mapping) == 0 {
		return "", "", false
	}
	if mapped, ok := mapping[requested]; ok {
		return mapped, requested, true
	}

	type patternMatch struct {
		pattern string
		target  string
	}
	matches := make([]patternMatch, 0)
	for pattern, target := range mapping {
		if matchWildcard(pattern, requested) {
			matches = append(matches, patternMatch{pattern: pattern, target: target})
		}
	}
	if len(matches) == 0 {
		return "", "", false
	}
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i].pattern) != len(matches[j].pattern) {
			return len(matches[i].pattern) > len(matches[j].pattern)
		}
		return matches[i].pattern < matches[j].pattern
	})
	return matches[0].target, matches[0].pattern, true
}
