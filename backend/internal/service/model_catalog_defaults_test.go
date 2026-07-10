package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelCatalogIDs_AllPlatforms(t *testing.T) {
	platforms := []string{
		PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity,
		PlatformGrok, PlatformMiniMax, PlatformGLM, PlatformKimi,
		PlatformDeepSeek, PlatformWindsurf, PlatformOpenCode,
	}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			first := DefaultModelCatalogIDs(platform)
			require.NotEmpty(t, first)
			first[0] = "mutated"
			require.NotEqual(t, "mutated", DefaultModelCatalogIDs(platform)[0])
		})
	}
	require.Nil(t, DefaultModelCatalogIDs("unknown"))
}
