package middleware

import (
	"github.com/google/uuid"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func isVideoResourceRequest(method, path string, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodDelete {
		return false
	}
	if service.IsArkVideoPath(path) {
		// Ark 任务协议：五家平台与 composite 的查询 / 列表 / 删除 / 目录都是只读账务外的资源访问。
		if apiKey.Group.Platform != service.PlatformComposite && !service.IsArkVideoPlatform(apiKey.Group.Platform) {
			return false
		}
		if service.IsArkVideoModelsPath(path) {
			return method == http.MethodGet
		}
		trimmed := strings.TrimRight(path, "/")
		for _, prefix := range []string{"/api/v3", "/v3", "/v1"} {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		rest := strings.TrimPrefix(trimmed, service.ArkVideoTasksPathSuffix)
		if rest == "" {
			return method == http.MethodGet
		}
		parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
		switch len(parts) {
		case 1:
			return (method == http.MethodGet || method == http.MethodDelete) && service.IsValidVideoTaskID(parts[0])
		case 2:
			return (method == http.MethodGet || method == http.MethodHead) && parts[1] == "content" && service.IsValidVideoTaskID(parts[0])
		default:
			return false
		}
	}
	if apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite && apiKey.Group.Platform != service.PlatformGrok {
		return false
	}
	if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || parts[0] != "videos" {
		return false
	}
	if apiKey.Group.Platform == service.PlatformGrok && (method == http.MethodDelete || (len(parts) > 1 && parts[1] == "characters")) {
		return false
	}
	if apiKey.Group.Platform == service.PlatformGrok || apiKey.Group.Platform == service.PlatformComposite {
		ids := parts[1:]
		if len(ids) > 0 && (ids[0] == "generations" || ids[0] == "edits" || ids[0] == "extensions") {
			ids = ids[1:]
		}
		if len(ids) > 0 {
			if _, err := uuid.Parse(ids[0]); err == nil {
				if len(ids) == 1 {
					return method == http.MethodGet
				}
				if len(ids) == 2 && ids[1] == "content" {
					return method == http.MethodGet || method == http.MethodHead
				}
			}
		}
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

func isVideoGenerationRequest(method, path string, apiKey *service.APIKey, grokManaged ...bool) bool {
	if method != http.MethodPost || apiKey == nil || apiKey.Group == nil {
		return false
	}
	if service.IsArkVideoTasksPath(path) {
		// 只有创建路径本身是生成请求；/tasks/{id} 与 /tasks/{id}/content 的 POST 不是任何路由。
		if apiKey.Group.Platform != service.PlatformComposite && !service.IsArkVideoPlatform(apiKey.Group.Platform) {
			return false
		}
		_, supported := service.ManagedVideoOperationForPath(path)
		return supported
	}
	managed := len(grokManaged) > 0 && grokManaged[0]
	normalized := strings.TrimPrefix(path, "/v1")
	if normalized == "/videos/generations" {
		return managed && (apiKey.Group.Platform == service.PlatformGrok || apiKey.Group.Platform == service.PlatformComposite)
	}
	if apiKey.Group.Platform == service.PlatformGrok {
		if !managed {
			return false
		}
		op, ok := service.ManagedVideoOperationForPath(path)
		return ok && op != service.VideoOperationCharacterCreate
	}
	if apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite {
		return false
	}
	_, supported := service.ManagedVideoOperationForPath(path)
	return supported
}
