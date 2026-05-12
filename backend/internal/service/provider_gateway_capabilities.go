package service

type ProviderGatewayCapabilities struct {
	Platform          string
	DefaultModelIDs   []string
	PublicModelIDs    []string
	SupportedModelIDs []string
	AliasRules        []ModelAliasRule
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
