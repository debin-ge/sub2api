package service

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelCatalogIDs_AllPlatforms(t *testing.T) {
	platforms := append(domain.ConcretePlatformIDs(), PlatformGLM)
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			first := DefaultModelCatalogIDs(platform)
			require.NotEmpty(t, first)
			first[0] = "mutated"
			require.NotEqual(t, "mutated", DefaultModelCatalogIDs(platform)[0])
		})
	}
	require.Contains(t, DefaultModelCatalogIDs(PlatformByteDance), ByteDanceVideoModelSeedance10Pro)
	require.Nil(t, DefaultModelCatalogIDs("unknown"))
}
