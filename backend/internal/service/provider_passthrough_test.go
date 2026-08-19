package service

import "testing"

// domesticPassthroughPlatforms 列出共用 isFlexibleProviderModelSupported 的六个国产网关。
// 每条给一个"账号里已配好的老模型"和一个"上游刚上线、mapping 里没有的新模型"。
var domesticPassthroughPlatforms = []struct {
	platform  string
	oldModel  string
	upstream  string
	newModel  string
	mappedFor func(*Account, string) string
}{
	{
		platform: PlatformGLM, oldModel: "GLM-5.1", upstream: "GLM-5.1", newModel: "glm-6",
		mappedFor: func(a *Account, m string) string { return a.GetGLMMappedModel(m) },
	},
	{
		platform: PlatformZhipu, oldModel: "GLM-5.1", upstream: "GLM-5.1", newModel: "glm-6",
		mappedFor: func(a *Account, m string) string { return a.GetGLMMappedModel(m) },
	},
	{
		platform: PlatformMiniMax, oldModel: "MiniMax-M2.7", upstream: "MiniMax-M2.7", newModel: "MiniMax-M4",
		mappedFor: func(a *Account, m string) string { return a.GetMiniMaxMappedModel(m) },
	},
	{
		platform: PlatformKimi, oldModel: "kimi-for-coding", upstream: "kimi-for-coding", newModel: "kimi-k4",
		mappedFor: func(a *Account, m string) string { return a.GetKimiMappedModel(m) },
	},
	{
		platform: PlatformDeepSeek, oldModel: "deepseek-v4-pro", upstream: "deepseek-v4-pro", newModel: "deepseek-v5",
		mappedFor: func(a *Account, m string) string { return a.GetDeepSeekMappedModel(m) },
	},
	{
		platform: PlatformWindsurf, oldModel: "swe-1-6", upstream: "swe-1-6", newModel: "swe-2-0",
		mappedFor: func(a *Account, m string) string { return a.GetWindsurfMappedModel(m) },
	},
	{
		platform: PlatformOpenCode, oldModel: "gpt5-nano", upstream: "gpt5-nano", newModel: "opencode/tiny-pickle",
		mappedFor: func(a *Account, m string) string { return a.GetOpenCodeMappedModel(m) },
	},
}

func domesticAccountWithMapping(platform, from, to string, passthrough bool) *Account {
	acc := &Account{
		Platform: platform,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":       "sk-test",
			"model_mapping": map[string]any{from: to},
		},
	}
	if passthrough {
		acc.Extra = map[string]any{"provider_passthrough": true}
	}
	return acc
}

// TestDomesticMappingActsAsAllowListWithoutPassthrough 固定住"没开透传时 mapping 仍是白名单"
// 这个既有语义——透传开关是逃生阀，不能顺手把默认行为也放开了。
func TestDomesticMappingActsAsAllowListWithoutPassthrough(t *testing.T) {
	for _, tc := range domesticPassthroughPlatforms {
		t.Run(tc.platform, func(t *testing.T) {
			acc := domesticAccountWithMapping(tc.platform, tc.oldModel, tc.upstream, false)
			if !isDomesticModelSupported(t, acc, tc.oldModel) {
				t.Fatalf("mapped model %q should stay supported", tc.oldModel)
			}
			if isDomesticModelSupported(t, acc, tc.newModel) {
				t.Fatalf("unmapped model %q must stay rejected while passthrough is off", tc.newModel)
			}
		})
	}
}

// TestDomesticPassthroughAdmitsUnmappedModel 是本次修复的核心：上游上新 SKU 时，账号里
// 残留的旧 model_mapping 会把整个账号排除出候选集（表现为 503 且无调度日志）。开启透传后
// 模型语义交由上游决定，新模型必须直接放行，且不被映射改写。
func TestDomesticPassthroughAdmitsUnmappedModel(t *testing.T) {
	for _, tc := range domesticPassthroughPlatforms {
		t.Run(tc.platform, func(t *testing.T) {
			acc := domesticAccountWithMapping(tc.platform, tc.oldModel, tc.upstream, true)

			if !acc.IsProviderPassthroughEnabled() {
				t.Fatalf("IsProviderPassthroughEnabled = false, want true")
			}
			if !isDomesticModelSupported(t, acc, tc.newModel) {
				t.Fatalf("passthrough must admit unmapped model %q", tc.newModel)
			}
			if got := tc.mappedFor(acc, tc.newModel); got != tc.newModel {
				t.Fatalf("mapped model = %q, want verbatim %q", got, tc.newModel)
			}
			// 透传下连已配好的映射也不再改写：认证之外一切交给上游。
			if got := tc.mappedFor(acc, tc.oldModel); got != tc.oldModel {
				t.Fatalf("mapped model = %q, want verbatim %q", got, tc.oldModel)
			}
		})
	}
}

// TestProviderPassthroughIsScopedToDomesticPlatforms 确认这个开关不会外溢到
// Anthropic / OpenAI —— 它们各有自己的透传字段和语义。
func TestProviderPassthroughIsScopedToDomesticPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformAntigravity, PlatformGrok} {
		acc := &Account{
			Platform: platform,
			Type:     AccountTypeAPIKey,
			Extra:    map[string]any{"provider_passthrough": true},
		}
		if acc.IsProviderPassthroughEnabled() {
			t.Fatalf("platform %q must not honour provider_passthrough", platform)
		}
	}

	var nilAccount *Account
	if nilAccount.IsProviderPassthroughEnabled() {
		t.Fatalf("nil account must not report passthrough enabled")
	}
	noExtra := &Account{Platform: PlatformGLM, Type: AccountTypeAPIKey}
	if noExtra.IsProviderPassthroughEnabled() {
		t.Fatalf("missing extra must default to passthrough off")
	}
	badType := &Account{Platform: PlatformGLM, Type: AccountTypeAPIKey, Extra: map[string]any{"provider_passthrough": "true"}}
	if badType.IsProviderPassthroughEnabled() {
		t.Fatalf("non-bool extra must default to passthrough off")
	}
}
