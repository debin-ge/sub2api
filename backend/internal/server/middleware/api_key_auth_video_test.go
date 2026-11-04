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
	require.True(t, isVideoResourceRequest(http.MethodGet, "/videos/"+taskID, &service.APIKey{Group: &service.Group{Platform: service.PlatformGrok}}))
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

// Ark 任务协议（五家托管视频平台与 composite）：GET / HEAD / DELETE 的任务、内容与目录访问
// 是只读账务外的资源请求；创建（POST）是延迟计费的视频生成请求。OpenAI / Grok 分组不认这些路径。
func TestVideoResourceRequestClassificationRecognisesArkPaths(t *testing.T) {
	taskID := "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	arkPlatforms := []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova, service.PlatformComposite}
	for _, platform := range arkPlatforms {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
			tasks := prefix + "/contents/generations/tasks"
			for _, request := range []struct {
				method string
				path   string
			}{
				{http.MethodGet, tasks},
				{http.MethodGet, tasks + "/"},
				{http.MethodGet, tasks + "/" + taskID},
				{http.MethodDelete, tasks + "/" + taskID},
				{http.MethodGet, tasks + "/" + taskID + "/content"},
				{http.MethodHead, tasks + "/" + taskID + "/content"},
				{http.MethodGet, prefix + "/contents/generations/models"},
			} {
				require.True(t, isVideoResourceRequest(request.method, request.path, key), "platform=%s %s %s", platform, request.method, request.path)
				require.False(t, isVideoGenerationRequest(request.method, request.path, key), "platform=%s %s %s", platform, request.method, request.path)
			}
			require.True(t, isVideoGenerationRequest(http.MethodPost, tasks, key), "platform=%s POST %s", platform, tasks)
			require.True(t, isVideoGenerationRequest(http.MethodPost, tasks+"/", key), "platform=%s POST %s/", platform, tasks)
			require.False(t, isVideoResourceRequest(http.MethodPost, tasks, key), "platform=%s POST %s", platform, tasks)
			for _, request := range []struct {
				method string
				path   string
			}{
				{http.MethodDelete, tasks},
				{http.MethodHead, tasks},
				{http.MethodHead, tasks + "/" + taskID},
				{http.MethodGet, tasks + "/not-a-local-id"},
				{http.MethodDelete, tasks + "/not-a-local-id"},
				{http.MethodGet, tasks + "/" + taskID + "/content/extra"},
				{http.MethodGet, tasks + "/" + taskID + "/thumbnail"},
				{http.MethodDelete, tasks + "/" + taskID + "/content"},
				{http.MethodDelete, prefix + "/contents/generations/models"},
				{http.MethodGet, prefix + "/contents/generations"},
				{http.MethodGet, prefix + "/contents/generations/other"},
			} {
				require.False(t, isVideoResourceRequest(request.method, request.path, key), "platform=%s %s %s", platform, request.method, request.path)
				require.False(t, isVideoGenerationRequest(request.method, request.path, key), "platform=%s %s %s", platform, request.method, request.path)
			}
			for _, path := range []string{tasks + "/" + taskID, prefix + "/contents/generations/models", prefix + "/contents/generations/tasks/" + taskID + "/content"} {
				require.False(t, isVideoGenerationRequest(http.MethodPost, path, key), "platform=%s POST %s", platform, path)
			}
		}
		// /v11 不是合法前缀。
		require.False(t, isVideoResourceRequest(http.MethodGet, "/v11/contents/generations/tasks", key))
		require.False(t, isVideoGenerationRequest(http.MethodPost, "/v11/contents/generations/tasks", key))
	}

	// 五家平台不再通过 /v1/videos* 访问任务：那些路径对它们既不是资源请求也不是生成请求。
	for _, platform := range []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova} {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		for _, prefix := range []string{"/v1", ""} {
			require.False(t, isVideoResourceRequest(http.MethodGet, prefix+"/videos/"+taskID, key), platform)
			require.False(t, isVideoResourceRequest(http.MethodGet, prefix+"/videos", key), platform)
			require.False(t, isVideoGenerationRequest(http.MethodPost, prefix+"/videos", key), platform)
			require.False(t, isVideoGenerationRequest(http.MethodPost, prefix+"/videos/edits", key), platform)
		}
	}

	// OpenAI / Grok / 其他分组与无分组 Key 不认 Ark 路径。
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformAnthropic, service.PlatformGemini} {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
			tasks := prefix + "/contents/generations/tasks"
			require.False(t, isVideoResourceRequest(http.MethodGet, tasks, key), platform)
			require.False(t, isVideoResourceRequest(http.MethodGet, tasks+"/"+taskID, key), platform)
			require.False(t, isVideoResourceRequest(http.MethodDelete, tasks+"/"+taskID, key), platform)
			require.False(t, isVideoResourceRequest(http.MethodGet, tasks+"/"+taskID+"/content", key), platform)
			require.False(t, isVideoResourceRequest(http.MethodGet, prefix+"/contents/generations/models", key), platform)
			require.False(t, isVideoGenerationRequest(http.MethodPost, tasks, key), platform)
		}
	}
	require.False(t, isVideoResourceRequest(http.MethodGet, "/api/v3/contents/generations/tasks/"+taskID, nil))
	require.False(t, isVideoResourceRequest(http.MethodGet, "/api/v3/contents/generations/tasks/"+taskID, &service.APIKey{}))
	require.False(t, isVideoGenerationRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil))
	require.False(t, isVideoGenerationRequest(http.MethodPost, "/api/v3/contents/generations/tasks", &service.APIKey{}))
}

func TestManagedGrokCreationDefersToAtomicVideoAdmission(t *testing.T) {
	key := &service.APIKey{Group: &service.Group{Platform: service.PlatformGrok}}
	for _, path := range []string{"/v1/videos", "/v1/videos/generations", "/v1/videos/edits", "/v1/videos/extensions"} {
		require.True(t, isVideoGenerationRequest(http.MethodPost, path, key, true), path)
		require.False(t, isVideoGenerationRequest(http.MethodPost, path, key, false), path)
	}
	require.False(t, isVideoGenerationRequest(http.MethodPost, "/v1/videos/characters", key, true))
	require.True(t, isVideoResourceRequest(http.MethodGet, "/v1/videos/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", key))
	require.False(t, isVideoResourceRequest(http.MethodDelete, "/v1/videos/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", key))
}
