package service

import "testing"

// isDomesticModelSupported 汇总六个国产网关的准入入口，供本文件和
// provider_passthrough_test.go 共用。
func isDomesticModelSupported(t *testing.T, account *Account, model string) bool {
	t.Helper()
	switch CanonicalCNPlatform(account.Platform) {
	case PlatformZhipu:
		return account.IsGLMModelSupported(model)
	case PlatformMiniMax:
		return account.IsMiniMaxModelSupported(model)
	case PlatformKimi:
		return account.IsKimiModelSupported(model)
	case PlatformDeepSeek:
		return account.IsDeepSeekModelSupported(model)
	case PlatformWindsurf:
		return account.IsWindsurfModelSupported(model)
	case PlatformOpenCode:
		return account.IsOpenCodeModelSupported(model)
	}
	t.Fatalf("unhandled platform %q", account.Platform)
	return false
}

// TestGLMModelSupportSharesFlexibleSemantics 确认 GLM 已并回通用实现，不再自带型号表：
// 一个能力表里根本没登记的型号，在无 mapping 的账号上必须放行。
func TestGLMModelSupportSharesFlexibleSemantics(t *testing.T) {
	acc := &Account{
		Platform:    PlatformGLM,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}
	for _, model := range []string{"glm-5.2", "GLM-5.2", "glm-6", "GLM-9.9-turbo"} {
		if !acc.IsGLMModelSupported(model) {
			t.Fatalf("account without mapping must admit %q", model)
		}
	}
	if acc.IsGLMModelSupported("  ") {
		t.Fatalf("blank model must stay rejected")
	}
}

// TestAccountMappingFallsBackToCaseInsensitiveMatch 取代 GLM 原先靠硬编码型号表做的
// 大小写补救：z.ai 文档写 GLM-5.2，客户端普遍发 glm-5.2，两边只是大小写不同时不该判成
// "账号不支持该模型"。这条兜底对六个平台一视同仁。
func TestAccountMappingFallsBackToCaseInsensitiveMatch(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		mapping  map[string]any
		request  string
		want     string
	}{
		{
			name:     "glm exact key differs only by case",
			platform: PlatformGLM,
			mapping:  map[string]any{"GLM-5.2": "GLM-5.2"},
			request:  "glm-5.2",
			want:     "GLM-5.2",
		},
		{
			name:     "glm wildcard pattern differs only by case",
			platform: PlatformGLM,
			mapping:  map[string]any{"GLM-*": "GLM-5.2"},
			request:  "glm-5.2",
			want:     "GLM-5.2",
		},
		{
			name:     "minimax key differs only by case",
			platform: PlatformMiniMax,
			mapping:  map[string]any{"MiniMax-M3": "MiniMax-M3"},
			request:  "minimax-m3",
			want:     "MiniMax-M3",
		},
		{
			name:     "kimi key differs only by case",
			platform: PlatformKimi,
			mapping:  map[string]any{"Kimi-For-Coding": "kimi-for-coding"},
			request:  "kimi-for-coding",
			want:     "kimi-for-coding",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc := &Account{
				Platform:    tc.platform,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test", "model_mapping": tc.mapping},
			}
			resolved, ok := ResolveAccountProviderModel(acc, tc.request)
			if !ok {
				t.Fatalf("ResolveAccountProviderModel(%q) did not match", tc.request)
			}
			if resolved.UpstreamModel != tc.want {
				t.Fatalf("UpstreamModel = %q, want %q", resolved.UpstreamModel, tc.want)
			}
			if !isDomesticModelSupported(t, acc, tc.request) {
				t.Fatalf("model %q should be supported via the case-insensitive fallback", tc.request)
			}
		})
	}
}

// TestAccountMappingPrefersCaseSensitiveMatch 保证大小写兜底只在严格匹配全部落空后才生效，
// 今天已经能命中的配置解析结果不变。
func TestAccountMappingPrefersCaseSensitiveMatch(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
			"model_mapping": map[string]any{
				"GLM-5.1": "GLM-4.5-air", // 仅大小写不同的精确 key
				"glm-*":   "GLM-5.2",     // 大小写完全一致的通配符
			},
		},
	}
	resolved, ok := ResolveAccountProviderModel(acc, "glm-5.1")
	if !ok {
		t.Fatalf("ResolveAccountProviderModel did not match")
	}
	if resolved.MatchedPattern != "glm-*" {
		t.Fatalf("MatchedPattern = %q, want the case-sensitive wildcard %q", resolved.MatchedPattern, "glm-*")
	}
	if resolved.UpstreamModel != "GLM-5.2" {
		t.Fatalf("UpstreamModel = %q, want %q", resolved.UpstreamModel, "GLM-5.2")
	}
}
