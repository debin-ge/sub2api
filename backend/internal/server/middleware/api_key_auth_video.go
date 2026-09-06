package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func isVideoResourceRequest(method, path string, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil ||
		(apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite) {
		return false
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodDelete {
		return false
	}
	if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || parts[0] != "videos" {
		return false
	}
	switch len(parts) {
	case 1:
		return method == http.MethodGet
	case 2:
		return (method == http.MethodGet || method == http.MethodDelete) && service.IsValidVideoTaskID(parts[1])
	case 3:
		if parts[1] == "characters" {
			return (method == http.MethodGet || method == http.MethodDelete) && service.IsValidVideoResourceID(parts[2])
		}
		return (method == http.MethodGet || method == http.MethodHead) && parts[2] == "content" && service.IsValidVideoTaskID(parts[1])
	default:
		return false
	}
}

func isVideoGenerationRequest(method, path string, apiKey *service.APIKey) bool {
	if method != http.MethodPost || apiKey == nil || apiKey.Group == nil ||
		(apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite) {
		return false
	}
	_, supported := service.ManagedVideoOperationForPath(path)
	return supported
}
