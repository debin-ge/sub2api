package service

import "testing"

func TestProviderGatewayCapabilitiesDomesticDefaults(t *testing.T) {
	cases := []struct {
		platform string
		models   []string
	}{
		{platform: PlatformMiniMax, models: []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"}},
		{platform: PlatformGLM, models: []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"}},
		{platform: PlatformKimi, models: []string{"kimi-for-coding"}},
		{platform: PlatformDeepSeek, models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}},
		{platform: PlatformWindsurf, models: windsurfOfficialModelIDs},
		{platform: PlatformOpenCode, models: []string{"opencode/big-pickle", "opencode/gpt5-nano", "gpt5-nano"}},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			caps, ok := GetProviderGatewayCapabilities(tc.platform)
			if !ok {
				t.Fatalf("capabilities for %q not found", tc.platform)
			}
			if caps.Platform != tc.platform {
				t.Fatalf("Platform = %q, want %q", caps.Platform, tc.platform)
			}
			assertStringSlicesEqual(t, caps.DefaultModelIDs, tc.models)
			assertStringSlicesEqual(t, caps.PublicModelIDs, tc.models)
			if tc.platform == PlatformWindsurf {
				mustContainStrings(t, caps.DefaultModelIDs, []string{
					"claude-opus-4-7-xhigh",
					"claude-opus-4-7-max",
					"gpt-5-5-high",
					"gpt-5-5-xhigh-priority",
				})
				mustContainStrings(t, caps.SupportedModelIDs, []string{
					"claude-sonnet-4.6",
					"gpt-5.4",
				})
			}
		})
	}
}

func TestDefaultDomesticProviderModelIDsReturnsClone(t *testing.T) {
	models := DefaultDomesticProviderModelIDs(PlatformMiniMax)
	assertStringSlicesEqual(t, models, []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"})

	models[0] = "mutated"

	again := DefaultDomesticProviderModelIDs(PlatformMiniMax)
	assertStringSlicesEqual(t, again, []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"})
}

func TestDefaultMiniMaxModelIDs(t *testing.T) {
	assertStringSlicesEqual(t, DefaultMiniMaxModelIDs(), []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"})
}

func TestFlexibleDomesticProvidersAllowUnknownModelsAndLiveDiscovery(t *testing.T) {
	for _, platform := range []string{PlatformMiniMax, PlatformGLM, PlatformKimi, PlatformDeepSeek} {
		if !providerSupportsUpstreamModel(platform, "provider-future-model") {
			t.Fatalf("platform %q should allow future upstream models", platform)
		}
		if !providerSupportsLiveModelDiscovery(platform) {
			t.Fatalf("platform %q should support live model discovery", platform)
		}
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d = %q, want %q; got %#v", i, got[i], want[i], got)
		}
	}
}
