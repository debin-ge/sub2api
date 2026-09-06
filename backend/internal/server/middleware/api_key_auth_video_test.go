package middleware

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoResourceRequestClassification(t *testing.T) {
	taskID := "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resourceID := "char_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		for _, prefix := range []string{"/v1", ""} {
			for _, request := range []struct {
				method string
				path   string
			}{
				{http.MethodGet, "/videos"},
				{http.MethodGet, "/videos/" + taskID},
				{http.MethodDelete, "/videos/" + taskID},
				{http.MethodGet, "/videos/" + taskID + "/content"},
				{http.MethodHead, "/videos/" + taskID + "/content"},
				{http.MethodGet, "/videos/characters/" + resourceID},
				{http.MethodDelete, "/videos/characters/" + resourceID},
			} {
				require.True(t, isVideoResourceRequest(request.method, prefix+request.path, key), "%s %s", request.method, prefix+request.path)
			}
		}
		for _, path := range []string{"/videos", "/videos/edits", "/videos/extensions", "/videos/characters"} {
			require.False(t, isVideoResourceRequest(http.MethodPost, path, key))
		}
		for _, path := range []string{"/videos/not-a-local-id", "/videos/generations/legacy", "/videos/" + taskID + "/content/extra", "/v11/videos", "/videos/characters"} {
			require.False(t, isVideoResourceRequest(http.MethodGet, path, key))
		}
	}
	require.False(t, isVideoResourceRequest(http.MethodGet, "/videos/"+taskID, &service.APIKey{Group: &service.Group{Platform: service.PlatformGrok}}))
	require.False(t, isVideoResourceRequest(http.MethodGet, "/videos/"+taskID, nil))
}

func TestVideoCreationDeferralIncludesCompositeButNotLegacyGrokRoutes(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite, service.PlatformGrok} {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		for _, prefix := range []string{"", "/v1"} {
			for _, path := range []string{"/videos", "/videos/edits", "/videos/extensions", "/videos/characters"} {
				require.Equal(t, platform != service.PlatformGrok, isVideoGenerationRequest(http.MethodPost, prefix+path, key))
				require.False(t, isVideoGenerationRequest(http.MethodGet, prefix+path, key))
			}
			for _, path := range []string{"/videos/generations", "/videos/characters/id", "/videos/edits/extra", "/images/generations"} {
				require.False(t, isVideoGenerationRequest(http.MethodPost, prefix+path, key))
			}
		}
	}
}
