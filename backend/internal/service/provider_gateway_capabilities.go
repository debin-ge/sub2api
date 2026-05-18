package service

type ProviderGatewayCapabilities struct {
	Platform          string
	DefaultModelIDs   []string
	PublicModelIDs    []string
	SupportedModelIDs []string
	AliasRules        []ModelAliasRule
}

// windsurfOfficialModelIDs is the static fallback catalog used when the
// reverse proxy cannot expose /v1/models. Keep it aligned with Windsurf's
// public model_uid list.
var windsurfOfficialModelIDs = []string{
	"claude-sonnet-4-6",
	"claude-sonnet-4-6-thinking",
	"claude-sonnet-4-6-1m",
	"claude-sonnet-4-6-thinking-1m",
	"claude-opus-4-7-xhigh",
	"claude-opus-4-7-xhigh-fast",
	"claude-opus-4-7-max",
	"claude-opus-4-7-max-fast",
	"claude-opus-4-7-high",
	"claude-opus-4-7-high-fast",
	"claude-opus-4-7-medium",
	"claude-opus-4-7-medium-fast",
	"claude-opus-4-7-low",
	"claude-opus-4-7-low-fast",
	"claude-opus-4-6",
	"claude-opus-4-6-thinking",
	"claude-opus-4-6-fast",
	"claude-opus-4-6-thinking-fast",
	"claude-opus-4-6-1m",
	"claude-opus-4-6-thinking-1m",
	"gpt-5-5-high",
	"gpt-5-5-high-priority",
	"gpt-5-5-xhigh",
	"gpt-5-5-xhigh-priority",
	"gpt-5-5-medium",
	"gpt-5-5-medium-priority",
	"gpt-5-5-low",
	"gpt-5-5-low-priority",
	"gpt-5-5-none",
	"gpt-5-5-none-priority",
	"gpt-5-5-review",
	"gpt-5-4-high",
	"gpt-5-4-high-priority",
	"gpt-5-4-xhigh",
	"gpt-5-4-xhigh-priority",
	"gpt-5-4-medium",
	"gpt-5-4-medium-priority",
	"gpt-5-4-low",
	"gpt-5-4-low-priority",
	"gpt-5-4-none",
	"gpt-5-4-none-priority",
	"gpt-5-4-mini-high",
	"gpt-5-4-mini-xhigh",
	"gpt-5-4-mini-medium",
	"gpt-5-4-mini-low",
	"gpt-5-3-codex-high",
	"gpt-5-3-codex-high-priority",
	"gpt-5-3-codex-xhigh",
	"gpt-5-3-codex-xhigh-priority",
	"gpt-5-3-codex-medium",
	"gpt-5-3-codex-medium-priority",
	"gpt-5-3-codex-low",
	"gpt-5-3-codex-low-priority",
	"gpt-5-3-codex-spark-medium",
	"gemini-3-1-pro-high",
	"gemini-3-1-pro-low",
	"deepseek-v4",
	"glm-5-1",
	"kimi-k2-6",
	"kimi-k2-5",
	"minimax-m2-5",
	"swe-1-6",
	"swe-1-6-fast",
	"swe-check",
	"adaptive",
	"arena-smart",
	"arena-mixed",
	"arena-fast",
	"opus-4-7-review",
}

var windsurfLegacyModelIDs = []string{
	"claude-sonnet-4.6",
	"claude-sonnet-4.6-thinking",
	"claude-opus-4.6",
	"claude-opus-4.6-thinking",
	"claude-opus-4.6-fast",
	"claude-opus-4.6-fast-thinking",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex-spark",
	"gemini-3.1-pro",
	"glm-5",
	"minimax-m2.5",
	"swe-1.6",
	"swe-1.6-fast",
	"swe-1.5",
	"swe-1",
	"phoenix-alpha",
}

var domesticProviderCapabilities = map[string]ProviderGatewayCapabilities{
	PlatformMiniMax: {
		Platform:        PlatformMiniMax,
		DefaultModelIDs: []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
		PublicModelIDs:  []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
		SupportedModelIDs: []string{
			"MiniMax-M2.7",
			"MiniMax-M2.7-highspeed",
			"abab6.5-chat",
			"abab6.5s-chat",
			"abab6.5s-chat-pro",
			"abab6-chat",
			"abab5.5-chat",
			"abab5.5s-chat",
		},
		AliasRules: []ModelAliasRule{
			{AliasPattern: "claude-sonnet-4-5", TargetModel: "MiniMax-M2.7"},
			{AliasPattern: "claude-3-5-sonnet-latest", TargetModel: "MiniMax-M2.7"},
			{AliasPattern: "claude-sonnet-*", TargetModel: "MiniMax-M2.7"},
			{AliasPattern: "claude-haiku-*", TargetModel: "MiniMax-M2.7-highspeed"},
		},
	},
	PlatformGLM: {
		Platform:          PlatformGLM,
		DefaultModelIDs:   []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"},
		PublicModelIDs:    []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"},
		SupportedModelIDs: []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"},
		AliasRules: []ModelAliasRule{
			{AliasPattern: "claude-sonnet-*", TargetModel: "GLM-5.1"},
			{AliasPattern: "claude-opus-*", TargetModel: "GLM-5.1"},
			{AliasPattern: "claude-haiku-*", TargetModel: "GLM-4.5-air"},
		},
	},
	PlatformKimi: {
		Platform:          PlatformKimi,
		DefaultModelIDs:   []string{"kimi-for-coding"},
		PublicModelIDs:    []string{"kimi-for-coding"},
		SupportedModelIDs: []string{"kimi-for-coding"},
		AliasRules: []ModelAliasRule{
			{AliasPattern: "claude-sonnet-4-5", TargetModel: "kimi-for-coding"},
			{AliasPattern: "claude-3-5-sonnet-latest", TargetModel: "kimi-for-coding"},
			{AliasPattern: "claude-sonnet-*", TargetModel: "kimi-for-coding"},
		},
	},
	PlatformDeepSeek: {
		Platform:          PlatformDeepSeek,
		DefaultModelIDs:   []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		PublicModelIDs:    []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		SupportedModelIDs: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		AliasRules: []ModelAliasRule{
			{AliasPattern: "deepseek-chat", TargetModel: "deepseek-v4-flash"},
			{AliasPattern: "deepseek-v3", TargetModel: "deepseek-v4-flash"},
			{AliasPattern: "deepseek-reasoner", TargetModel: "deepseek-v4-pro"},
			{AliasPattern: "deepseek-r1", TargetModel: "deepseek-v4-pro"},
		},
	},
	PlatformWindsurf: {
		Platform:          PlatformWindsurf,
		DefaultModelIDs:   windsurfOfficialModelIDs,
		PublicModelIDs:    windsurfOfficialModelIDs,
		SupportedModelIDs: mergeProviderModelIDs(windsurfOfficialModelIDs, windsurfLegacyModelIDs),
	},
}

func GetProviderGatewayCapabilities(platform string) (ProviderGatewayCapabilities, bool) {
	caps, ok := domesticProviderCapabilities[platform]
	if !ok {
		return ProviderGatewayCapabilities{}, false
	}
	caps.DefaultModelIDs = cloneStringSlice(caps.DefaultModelIDs)
	caps.PublicModelIDs = cloneStringSlice(caps.PublicModelIDs)
	caps.SupportedModelIDs = cloneStringSlice(caps.SupportedModelIDs)
	caps.AliasRules = cloneModelAliasRules(caps.AliasRules)
	return caps, true
}

func DefaultDomesticProviderModelIDs(platform string) []string {
	caps, ok := GetProviderGatewayCapabilities(platform)
	if !ok {
		return nil
	}
	return caps.DefaultModelIDs
}

func DefaultMiniMaxModelIDs() []string {
	return DefaultDomesticProviderModelIDs(PlatformMiniMax)
}

func mergeProviderModelIDs(slices ...[]string) []string {
	seen := make(map[string]struct{})
	var merged []string
	for _, models := range slices {
		for _, model := range models {
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	return merged
}

func providerSupportsUpstreamModel(platform, model string) bool {
	caps, ok := domesticProviderCapabilities[platform]
	if !ok {
		return false
	}
	for _, supported := range caps.SupportedModelIDs {
		if model == supported {
			return true
		}
	}
	return false
}

func cloneModelAliasRules(src []ModelAliasRule) []ModelAliasRule {
	if len(src) == 0 {
		return nil
	}
	dst := make([]ModelAliasRule, len(src))
	copy(dst, src)
	return dst
}
