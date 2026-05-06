package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	return newGatewayRoutesTestRouterForPlatform(service.PlatformOpenAI)
}

func newGatewayRoutesTestRouterForPlatform(platform string) *gin.Engine {
	return newGatewayRoutesTestRouterForPlatformWithHandlers(platform, &handler.Handlers{
		Gateway:        &handler.GatewayHandler{},
		OpenAIGateway:  &handler.OpenAIGatewayHandler{},
		MiniMaxGateway: &handler.MiniMaxGatewayHandler{},
		GLMGateway:     &handler.GLMGatewayHandler{},
		KimiGateway:    &handler.KimiGatewayHandler{},
	})
}

func newGatewayRoutesTestRouterForPlatformWithHandlers(platform string, handlers *handler.Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterGatewayRoutes(
		router,
		handlers,
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: platform},
			})
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 101, Concurrency: 1})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024}},
	)

	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}

func TestGatewayRoutesMiniMaxMessagesDispatchesToMiniMaxHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "minimax gateway service unavailable")
}

func TestGatewayRoutesMiniMaxChatCompletionsDispatchesToMiniMaxHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "minimax gateway service unavailable")
}

func TestGatewayRoutesMiniMaxUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"claude-sonnet-4-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be MiniMax unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesMiniMaxUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
		"/v1/models",
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be MiniMax unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesGLMMessagesDispatchesToGLMHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "glm gateway service unavailable")
}

func TestGatewayRoutesGLMChatCompletionsDispatchesToGLMHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	for _, path := range []string{"/v1/chat/completions", "/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"glm-4.5-air","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "glm gateway service unavailable", "path=%s", path)
	}
}

func TestGatewayRoutesGLMUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"glm-5.1"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be GLM unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "GLM gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesGLMUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	for _, path := range []string{
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be GLM unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "GLM gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesGLMModelsReturnsDefaultList(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformGLM)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "GLM-5.1")
	require.Contains(t, w.Body.String(), "GLM-4.7")
	require.Contains(t, w.Body.String(), "GLM-4.5-air")
}

func TestGatewayRoutesGLMDispatchDiffersWhenHandlerIsPresent(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
		nilRouter := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformGLM, &handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		})
		presentRouter := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformGLM, &handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
			GLMGateway:    &handler.GLMGatewayHandler{},
		})

		nilReq := httptest.NewRequest(http.MethodPost, path, nil)
		nilW := httptest.NewRecorder()
		nilRouter.ServeHTTP(nilW, nilReq)

		presentReq := httptest.NewRequest(http.MethodPost, path, nil)
		presentW := httptest.NewRecorder()
		presentRouter.ServeHTTP(presentW, presentReq)

		require.Equal(t, http.StatusServiceUnavailable, nilW.Code, "path=%s nil handler", path)
		require.Contains(t, nilW.Body.String(), "glm gateway service unavailable", "path=%s nil handler", path)
		require.Equal(t, http.StatusBadRequest, presentW.Code, "path=%s present handler", path)
		require.Contains(t, presentW.Body.String(), "Request body is empty", "path=%s present handler", path)
	}
}

func TestGatewayRoutesKimiMessagesDispatchesToKimiHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "kimi gateway service unavailable")
}

func TestGatewayRoutesKimiChatCompletionsDispatchesToKimiHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	for _, path := range []string{"/v1/chat/completions", "/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "kimi gateway service unavailable", "path=%s", path)
	}
}

func TestGatewayRoutesKimiUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"kimi-for-coding"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be Kimi unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "Kimi gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesKimiUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	for _, path := range []string{
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be Kimi unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "Kimi gateway supports /v1/messages and /v1/chat/completions only", "path=%s", path)
	}
}

func TestGatewayRoutesKimiModelsReturnsDefaultList(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "kimi-for-coding")
	require.NotContains(t, w.Body.String(), "claude-sonnet")
}
