package routes

import (
	"context"
	"encoding/json"
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

type gatewayRoutesModelCatalogAccountRepo struct {
	service.AccountRepository
}

type gatewayRoutesModelCatalogGroupRepo struct {
	service.GroupRepository
	group service.Group
}

func (gatewayRoutesModelCatalogAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return []service.Account{{
		ID:          1,
		Platform:    platform,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
	}}, nil
}

func (gatewayRoutesModelCatalogAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return gatewayRoutesModelCatalogAccountRepo{}.ListSchedulableByGroupIDAndPlatform(ctx, 0, platform)
}

func (r gatewayRoutesModelCatalogGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if r.group.ID != id {
		return nil, service.ErrGroupNotFound
	}
	group := r.group
	return &group, nil
}

func newGatewayRoutesGatewayHandler(platform string) *handler.GatewayHandler {
	catalog := service.NewModelCatalogService(
		gatewayRoutesModelCatalogAccountRepo{},
		gatewayRoutesModelCatalogGroupRepo{group: service.Group{ID: 1, Platform: platform}},
		nil,
		nil,
		config.ModelCatalogConfig{},
	)
	return handler.NewGatewayHandler(
		nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, catalog,
	)
}

func defaultGatewayRoutesTestConfig() *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024}}
}

func newGatewayRoutesTestRouter(platform ...string) *gin.Engine {
	groupPlatform := service.PlatformOpenAI
	if len(platform) > 0 && platform[0] != "" {
		groupPlatform = platform[0]
	}
	return newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(
		groupPlatform,
		defaultGatewayRoutesTestConfig(),
		defaultGatewayRoutesTestHandlers(groupPlatform),
	)
}

func newGatewayRoutesTestRouterWithConfig(cfg *config.Config, platform ...string) *gin.Engine {
	groupPlatform := service.PlatformOpenAI
	if len(platform) > 0 && platform[0] != "" {
		groupPlatform = platform[0]
	}
	return newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(
		groupPlatform,
		cfg,
		defaultGatewayRoutesTestHandlers(groupPlatform),
	)
}

func defaultGatewayRoutesTestHandlers(platform string) *handler.Handlers {
	return &handler.Handlers{
		Gateway:         newGatewayRoutesGatewayHandler(platform),
		OpenAIGateway:   &handler.OpenAIGatewayHandler{},
		AsyncImage:      handler.NewAsyncImageHandler(nil, nil),
		MiniMaxGateway:  &handler.MiniMaxGatewayHandler{},
		GLMGateway:      &handler.GLMGatewayHandler{},
		KimiGateway:     &handler.KimiGatewayHandler{},
		DeepSeekGateway: &handler.DeepSeekGatewayHandler{},
		WindsurfGateway: &handler.WindsurfGatewayHandler{},
		OpenCodeGateway: &handler.OpenCodeGatewayHandler{},
	}
}

func newGatewayRoutesTestRouterForPlatform(platform string) *gin.Engine {
	return newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(
		platform,
		defaultGatewayRoutesTestConfig(),
		defaultGatewayRoutesTestHandlers(platform),
	)
}

func newGatewayRoutesTestRouterForPlatformWithoutProviderHandlers(platform string) *gin.Engine {
	return newGatewayRoutesTestRouterForPlatformWithHandlers(platform, &handler.Handlers{
		Gateway:       newGatewayRoutesGatewayHandler(platform),
		OpenAIGateway: &handler.OpenAIGatewayHandler{},
	})
}

func newGatewayRoutesTestRouterForPlatformWithHandlers(platform string, handlers *handler.Handlers) *gin.Engine {
	return newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(
		platform,
		defaultGatewayRoutesTestConfig(),
		handlers,
	)
}

func newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(platform string, cfg *config.Config, handlers *handler.Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterGatewayRoutes(
		router,
		handlers,
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				UserID:  101,
				GroupID: &groupID,
				Group:   &service.Group{ID: groupID, Platform: platform},
				User:    &service.User{ID: 101, Status: service.StatusActive, Balance: 12.34},
			})
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 101, Concurrency: 1})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		cfg,
	)

	return router
}

func TestGatewayRoutesBalanceIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/v1/balance", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"balance":12.34`)
	require.Contains(t, w.Body.String(), `"user_id":101`)
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

func TestGatewayRoutesOpenAIAlphaSearchPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost {
			registered[route.Path] = true
		}
	}

	for _, path := range []string{
		"/v1/alpha/search",
		"/alpha/search",
		"/backend-api/codex/alpha/search",
	} {
		require.True(t, registered[path], "POST %s should be registered", path)
	}
}

func TestGatewayRoutesAlphaSearchRejectsNonOpenAIGroup(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "only available for OpenAI groups")
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

func TestGatewayRoutesAsyncImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"POST /v1/images/generations/async",
		"POST /v1/images/edits/async",
		"GET /v1/images/tasks/:task_id",
		"POST /images/generations/async",
		"POST /images/edits/async",
		"GET /images/tasks/:task_id",
	} {
		require.True(t, registered[route], "%s should be registered", route)
	}
}

func TestGatewayRoutesGrokImagesAndVideosPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
		"/v1/videos/generations",
		"/videos/generations",
		"/v1/videos/edits",
		"/videos/edits",
		"/v1/videos/extensions",
		"/videos/extensions",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok-imagine","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit Grok media handler", path)
		require.NotContains(t, w.Body.String(), "not supported for this platform")
	}

	for _, path := range []string{
		"/v1/videos/request-123",
		"/videos/request-123",
		"/v1/videos/request-123/content",
		"/videos/request-123/content",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit Grok video handler", path)
		require.NotContains(t, w.Body.String(), "not supported for this platform")
	}
}

func TestGatewayRoutesNonGrokVideosAreRejectedAtPlatformGate(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformOpenAI)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/videos/generations", `{"model":"grok-imagine-video-1.5","prompt":"waves"}`},
		{http.MethodPost, "/videos/generations", `{"model":"grok-imagine-video-1.5","prompt":"waves"}`},
		{http.MethodPost, "/v1/videos/edits", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/videos/edits", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/v1/videos/extensions", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/videos/extensions", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodGet, "/v1/videos/request-123", ""},
		{http.MethodGet, "/videos/request-123", ""},
		{http.MethodGet, "/v1/videos/request-123/content", ""},
		{http.MethodGet, "/videos/request-123/content", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "method=%s path=%s", tc.method, tc.path)
		require.Contains(t, w.Body.String(), "Videos API is not supported for this platform")
	}
}

func TestGatewayRoutesGrokAllowsCLICompatibilityEntrypoints(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/chat/completions"},
		{http.MethodGet, "/v1/responses"},
		{http.MethodGet, "/responses"},
		{http.MethodGet, "/backend-api/codex/responses"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"model":"grok"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "method=%s path=%s", tc.method, tc.path)
		require.NotContains(t, w.Body.String(), "not supported for Grok groups")
	}

	countTokensRouter := newGatewayRoutesTestRouterWithConfig(&config.Config{
		Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024},
	}, service.PlatformGrok)
	for _, path := range []string{"/v1/messages/count_tokens", "/messages/count_tokens"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		countTokensRouter.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "path=%s", path)
		var response struct {
			InputTokens int `json:"input_tokens"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "path=%s", path)
		require.Positive(t, response.InputTokens, "path=%s", path)
	}

	for _, path := range []string{
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok","input":"hi"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should still reach Responses handler", path)
	}
}

func TestGatewayRoutesOpenAICountTokensPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformOpenAI)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.NotEqual(t, http.StatusNotFound, w.Code)
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
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses/compact",
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
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages, /v1/chat/completions, /v1/responses, and /v1/models only", "path=%s", path)
	}
}

func TestGatewayRoutesMiniMaxUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
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
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages, /v1/chat/completions, /v1/responses, and /v1/models only", "path=%s", path)
	}
}

func TestGatewayRoutesMiniMaxModelsReturnsDefaultList(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "MiniMax-M2.7")
	require.Contains(t, w.Body.String(), "MiniMax-M2.7-highspeed")
	require.NotContains(t, w.Body.String(), "claude-sonnet")
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
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses/compact",
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
		require.Contains(t, w.Body.String(), "GLM gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
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
		require.Contains(t, w.Body.String(), "GLM gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
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
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses/compact",
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
		require.Contains(t, w.Body.String(), "Kimi gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
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
		require.Contains(t, w.Body.String(), "Kimi gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
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

func TestGatewayRoutesDeepSeekMessagesDispatchesToDeepSeekHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformDeepSeek)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "deepseek gateway service unavailable")
}

func TestGatewayRoutesDeepSeekChatCompletionsDispatchesToDeepSeekHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformDeepSeek)

	for _, path := range []string{"/v1/chat/completions", "/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "deepseek gateway service unavailable", "path=%s", path)
	}
}

func TestGatewayRoutesDeepSeekUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformDeepSeek)

	for _, path := range []string{
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses/compact",
		"/backend-api/codex/responses/compact",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"deepseek-v4-flash"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be DeepSeek unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "DeepSeek gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
	}
}

func TestGatewayRoutesDeepSeekUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformDeepSeek)

	for _, path := range []string{
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be DeepSeek unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "DeepSeek gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
	}
}

func TestGatewayRoutesDeepSeekModelsReturnsDefaultList(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformDeepSeek)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "deepseek-v4-flash")
	require.Contains(t, w.Body.String(), "deepseek-v4-pro")
	require.NotContains(t, w.Body.String(), "deepseek-chat")
	require.NotContains(t, w.Body.String(), "claude-sonnet")
}

func TestGatewayRoutesWindsurfResponsesWebSocketAliasesReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformWindsurf)

	for _, path := range []string{
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be Windsurf unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "Windsurf gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only", "path=%s", path)
	}
}

func TestGatewayRoutesProviderResponsesRootsDispatchToProviderHandlers(t *testing.T) {
	for _, tc := range []struct {
		platform string
		body     string
		message  string
	}{
		{platform: service.PlatformMiniMax, body: `{"model":"MiniMax-M2.7","input":"hello"}`, message: "minimax gateway service unavailable"},
		{platform: service.PlatformGLM, body: `{"model":"glm-5.1","input":"hello"}`, message: "glm gateway service unavailable"},
		{platform: service.PlatformKimi, body: `{"model":"kimi-for-coding","input":"hello"}`, message: "kimi gateway service unavailable"},
		{platform: service.PlatformDeepSeek, body: `{"model":"deepseek-chat","input":"hello"}`, message: "deepseek gateway service unavailable"},
		{platform: service.PlatformWindsurf, body: `{"model":"claude-sonnet-4.6","input":"hello"}`, message: "windsurf gateway service unavailable"},
	} {
		router := newGatewayRoutesTestRouterForPlatformWithoutProviderHandlers(tc.platform)

		for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusServiceUnavailable, w.Code, "platform=%s path=%s", tc.platform, path)
			require.Contains(t, w.Body.String(), `"type":"api_error"`, "platform=%s path=%s", tc.platform, path)
			require.Contains(t, w.Body.String(), tc.message, "platform=%s path=%s", tc.platform, path)
		}
	}
}

func TestGatewayRoutesProviderResponsesSubpathsRemainUnsupported(t *testing.T) {
	for _, tc := range []struct {
		platform string
		message  string
	}{
		{platform: service.PlatformMiniMax, message: "MiniMax gateway supports /v1/messages, /v1/chat/completions, /v1/responses, and /v1/models only"},
		{platform: service.PlatformGLM, message: "GLM gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only"},
		{platform: service.PlatformKimi, message: "Kimi gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only"},
		{platform: service.PlatformDeepSeek, message: "DeepSeek gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only"},
		{platform: service.PlatformWindsurf, message: "Windsurf gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only"},
	} {
		router := newGatewayRoutesTestRouterForPlatformWithoutProviderHandlers(tc.platform)

		for _, path := range []string{
			"/v1/responses/compact",
			"/v1/responses/custom",
			"/responses/compact",
			"/responses/custom",
			"/backend-api/codex/responses/compact",
			"/backend-api/codex/responses/custom",
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"test"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusNotFound, w.Code, "platform=%s path=%s", tc.platform, path)
			require.Contains(t, w.Body.String(), "not_found_error", "platform=%s path=%s", tc.platform, path)
			require.Contains(t, w.Body.String(), tc.message, "platform=%s path=%s", tc.platform, path)
		}
	}
}

func TestGatewayRoutesOpenCodeMessagesUnsupported(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"opencode/big-pickle","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "OpenCode gateway supports /v1/models, /v1/chat/completions, and /v1/responses only")
}

func TestGatewayRoutesOpenCodeChatCompletionsDispatchesToOpenCodeHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	for _, path := range []string{"/v1/chat/completions", "/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"opencode/big-pickle","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "opencode gateway service unavailable", "path=%s", path)
	}
}

func TestGatewayRoutesOpenCodeResponsesDispatchesToOpenCodeHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/compact",
		"/responses",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"opencode/big-pickle","input":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "opencode gateway service unavailable", "path=%s", path)
	}
}

func TestGatewayRoutesOpenCodeUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	for _, path := range []string{
		"/v1/messages/count_tokens",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"opencode/big-pickle"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be OpenCode unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "OpenCode gateway supports /v1/models, /v1/chat/completions, and /v1/responses only", "path=%s", path)
	}
}

func TestGatewayRoutesOpenCodeUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	for _, path := range []string{
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be OpenCode unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "OpenCode gateway supports /v1/models, /v1/chat/completions, and /v1/responses only", "path=%s", path)
	}
}

func TestGatewayRoutesOpenCodeModelsDispatchesToOpenCodeHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "opencode gateway service unavailable")
}
