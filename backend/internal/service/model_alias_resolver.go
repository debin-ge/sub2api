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
	if mapped, matchedPattern, ok := matchAccountMapping(mapping, requested); ok {
		return mapped, matchedPattern, true
	}
	// 大小写兜底：mapping 的 key 由管理员手写，而各家上游对模型 ID 的大小写并不统一
	// （z.ai 文档写 GLM-5.2，客户端普遍发 glm-5.2）。严格匹配全部落空后忽略大小写重试一次，
	// 避免账号仅因大小写差异被判成"不支持该模型"，进而在调度阶段被整个排除掉。
	//
	// 刻意放在最后而不是紧跟精确匹配：这样任何今天已经能命中的配置，解析结果保持不变，
	// 本次改动只可能让原本落空的请求命中，不会改变既有路由。
	return matchAccountMappingFold(mapping, requested)
}

// matchAccountMapping 大小写敏感匹配：精确 key 优先，其次通配符。
func matchAccountMapping(mapping map[string]string, requested string) (string, string, bool) {
	if mapped, ok := mapping[requested]; ok {
		return mapped, requested, true
	}
	return longestMappingPatternMatch(mapping, requested, false)
}

// matchAccountMappingFold 与 matchAccountMapping 同序，但两侧统一转小写后比较。
func matchAccountMappingFold(mapping map[string]string, requested string) (string, string, bool) {
	lowered := strings.ToLower(requested)
	exact := make([]string, 0, 1)
	for pattern := range mapping {
		if strings.ToLower(pattern) == lowered {
			exact = append(exact, pattern)
		}
	}
	if len(exact) > 0 {
		// 多个 key 仅大小写不同时按字典序定序，保证结果不随 map 遍历顺序漂移。
		sort.Strings(exact)
		return mapping[exact[0]], exact[0], true
	}
	return longestMappingPatternMatch(mapping, requested, true)
}

// longestMappingPatternMatch 最长 pattern 优先，同长按字典序，保证结果稳定。
func longestMappingPatternMatch(mapping map[string]string, requested string, foldCase bool) (string, string, bool) {
	type patternMatch struct {
		pattern string
		target  string
	}
	candidate := requested
	if foldCase {
		candidate = strings.ToLower(requested)
	}
	matches := make([]patternMatch, 0)
	for pattern, target := range mapping {
		comparable := pattern
		if foldCase {
			comparable = strings.ToLower(pattern)
		}
		if matchWildcard(comparable, candidate) {
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
