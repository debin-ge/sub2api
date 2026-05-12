package service

import "testing"

func TestResolveAccountProviderModelPrefersAccountMapping(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-haiku-4-5": "GLM-5.1",
			},
		},
	}

	got, ok := ResolveAccountProviderModel(acc, "claude-haiku-4-5")
	if !ok {
		t.Fatalf("ResolveAccountProviderModel did not match")
	}
	if got.UpstreamModel != "GLM-5.1" {
		t.Fatalf("UpstreamModel = %q, want GLM-5.1", got.UpstreamModel)
	}
	if got.Source != ModelAliasSourceAccountMapping {
		t.Fatalf("Source = %q, want %q", got.Source, ModelAliasSourceAccountMapping)
	}
	if got.MatchedPattern != "claude-haiku-4-5" {
		t.Fatalf("MatchedPattern = %q, want claude-haiku-4-5", got.MatchedPattern)
	}
}

func TestResolveAccountProviderModelFallsBackToProviderAlias(t *testing.T) {
	acc := &Account{Platform: PlatformKimi}

	got, ok := ResolveAccountProviderModel(acc, "claude-sonnet-4-5")
	if !ok {
		t.Fatalf("ResolveAccountProviderModel did not match")
	}
	if got.UpstreamModel != "kimi-for-coding" {
		t.Fatalf("UpstreamModel = %q, want kimi-for-coding", got.UpstreamModel)
	}
	if got.Source != ModelAliasSourceProviderDefault {
		t.Fatalf("Source = %q, want %q", got.Source, ModelAliasSourceProviderDefault)
	}
}

func TestResolveAccountProviderModelRejectsUnsupportedMappingTarget(t *testing.T) {
	acc := &Account{
		Platform: PlatformDeepSeek,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"deepseek-chat": "unsupported-upstream",
			},
		},
	}

	if got, ok := ResolveAccountProviderModel(acc, "deepseek-chat"); ok {
		t.Fatalf("unexpected unsupported mapping match: %#v", got)
	}
}

func TestResolveAccountUpstreamModelUsesDomesticProviderAlias(t *testing.T) {
	acc := &Account{Platform: PlatformDeepSeek}

	if got := resolveAccountUpstreamModel(acc, "deepseek-chat"); got != "deepseek-v4-flash" {
		t.Fatalf("resolveAccountUpstreamModel(deepseek-chat) = %q", got)
	}
}
