package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCompositeRouteEndpoint_VideoEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		CompositeRouteEndpointVideos,
		CompositeRouteEndpointVideoCharacters,
		CompositeRouteEndpointVideoEdits,
		CompositeRouteEndpointVideoExtensions,
	} {
		t.Run(endpoint, func(t *testing.T) {
			require.Equal(t, endpoint, normalizeCompositeRouteEndpoint("  "+endpoint+"  "))
		})
	}
}
