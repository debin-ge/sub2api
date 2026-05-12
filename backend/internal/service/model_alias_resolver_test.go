package service

import "testing"

func TestResolveProviderModelAliasDomesticProviders(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		model    string
		want     string
		pattern  string
	}{
		{name: "minimax exact sonnet", platform: PlatformMiniMax, model: "claude-sonnet-4-5", want: "MiniMax-M2.7", pattern: "claude-sonnet-4-5"},
		{name: "minimax latest sonnet", platform: PlatformMiniMax, model: "claude-3-5-sonnet-latest", want: "MiniMax-M2.7", pattern: "claude-3-5-sonnet-latest"},
		{name: "minimax haiku wildcard", platform: PlatformMiniMax, model: "claude-haiku-3-5", want: "MiniMax-M2.7-highspeed", pattern: "claude-haiku-*"},
		{name: "glm sonnet wildcard", platform: PlatformGLM, model: "claude-sonnet-4-5", want: "GLM-5.1", pattern: "claude-sonnet-*"},
		{name: "glm opus wildcard", platform: PlatformGLM, model: "claude-opus-4-5", want: "GLM-5.1", pattern: "claude-opus-*"},
		{name: "glm haiku wildcard", platform: PlatformGLM, model: "claude-haiku-4-5", want: "GLM-4.5-air", pattern: "claude-haiku-*"},
		{name: "kimi exact sonnet", platform: PlatformKimi, model: "claude-sonnet-4-5", want: "kimi-for-coding", pattern: "claude-sonnet-4-5"},
		{name: "kimi wildcard sonnet", platform: PlatformKimi, model: "claude-sonnet-4-0", want: "kimi-for-coding", pattern: "claude-sonnet-*"},
		{name: "deepseek chat", platform: PlatformDeepSeek, model: "deepseek-chat", want: "deepseek-v4-flash", pattern: "deepseek-chat"},
		{name: "deepseek v3", platform: PlatformDeepSeek, model: "deepseek-v3", want: "deepseek-v4-flash", pattern: "deepseek-v3"},
		{name: "deepseek reasoner", platform: PlatformDeepSeek, model: "deepseek-reasoner", want: "deepseek-v4-pro", pattern: "deepseek-reasoner"},
		{name: "deepseek r1", platform: PlatformDeepSeek, model: "deepseek-r1", want: "deepseek-v4-pro", pattern: "deepseek-r1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveProviderModelAlias(tc.platform, tc.model)
			if !ok {
				t.Fatalf("ResolveProviderModelAlias(%q, %q) did not match", tc.platform, tc.model)
			}
			if got.RequestedModel != tc.model {
				t.Fatalf("RequestedModel = %q, want %q", got.RequestedModel, tc.model)
			}
			if got.UpstreamModel != tc.want {
				t.Fatalf("UpstreamModel = %q, want %q", got.UpstreamModel, tc.want)
			}
			if got.Provider != tc.platform {
				t.Fatalf("Provider = %q, want %q", got.Provider, tc.platform)
			}
			if got.Source != ModelAliasSourceProviderDefault {
				t.Fatalf("Source = %q, want %q", got.Source, ModelAliasSourceProviderDefault)
			}
			if got.MatchedPattern != tc.pattern {
				t.Fatalf("MatchedPattern = %q, want %q", got.MatchedPattern, tc.pattern)
			}
		})
	}
}

func TestResolveProviderModelAliasNoMatch(t *testing.T) {
	if got, ok := ResolveProviderModelAlias(PlatformKimi, "claude-haiku-4-5"); ok {
		t.Fatalf("unexpected alias match: %#v", got)
	}
	if got, ok := ResolveProviderModelAlias("unknown", "claude-sonnet-4-5"); ok {
		t.Fatalf("unexpected alias match for unknown provider: %#v", got)
	}
	if got, ok := ResolveProviderModelAlias(PlatformMiniMax, ""); ok {
		t.Fatalf("unexpected alias match for empty model: %#v", got)
	}
}

func TestMatchModelPattern(t *testing.T) {
	cases := []struct {
		pattern string
		model   string
		want    bool
	}{
		{pattern: "claude-sonnet-*", model: "claude-sonnet-4-5", want: true},
		{pattern: "claude-sonnet-*", model: "claude-haiku-4-5", want: false},
		{pattern: "deepseek-chat", model: "deepseek-chat", want: true},
		{pattern: "deepseek-chat", model: "deepseek-chat-preview", want: false},
		{pattern: "", model: "deepseek-chat", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+"/"+tc.model, func(t *testing.T) {
			if got := matchModelPattern(tc.pattern, tc.model); got != tc.want {
				t.Fatalf("matchModelPattern(%q, %q) = %v, want %v", tc.pattern, tc.model, got, tc.want)
			}
		})
	}
}
