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
	return &config.Config{Gateway: config.GatewayConfig{
		MaxBodySize:     1024 * 1024,
		TextMaxBodySize: 1024 * 1024,
	}}
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
		KimiGateway:     &handler.KimiGatewayHandler{},
		DeepSeekGateway: &handler.DeepSeekGatewayHandler{},
		WindsurfGateway: &handler.WindsurfGatewayHandler{},
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
				ID:      7,
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

// /v1/usage 是纯本地端点（用量/额度/限速/余额均取自本地库），对所有分组平台都应可用，
// 包括曾被路由层门禁拦截的 MiniMax/GLM/Kimi/DeepSeek/Windsurf/OpenCode。
func TestGatewayRoutesUsageIsAvailableForAllPlatforms(t *testing.T) {
	for _, platform := range []string{
		service.PlatformMiniMax,
		service.PlatformGLM,
		service.PlatformZhipu,
		service.PlatformKimi,
		service.PlatformDeepSeek,
		service.PlatformWindsurf,
		service.PlatformOpenCodeGo,
		service.PlatformByteDance,
	} {
		router := newGatewayRoutesTestRouterForPlatform(platform)

		req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "platform=%s", platform)
		require.Contains(t, w.Body.String(), `"mode":"unrestricted"`, "platform=%s", platform)
		require.Contains(t, w.Body.String(), `"balance":12.34`, "platform=%s", platform)
	}
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

func TestGatewayRoutesAlphaSearchRejectsUnsupportedGroup(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "only available for OpenAI and Composite groups")
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
		"/v1/videos",
		"/videos",
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
		"/v1/videos/generations/request-123",
		"/videos/generations/request-123",
		"/v1/videos/edits/request-123",
		"/videos/edits/request-123",
		"/v1/videos/extensions/request-123",
		"/videos/extensions/request-123",
		"/v1/videos/request-123/content",
		"/videos/request-123/content",
		"/v1/videos/generations/request-123/content",
		"/videos/generations/request-123/content",
		"/v1/videos/edits/request-123/content",
		"/videos/edits/request-123/content",
		"/v1/videos/extensions/request-123/content",
		"/videos/extensions/request-123/content",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit Grok video handler", path)
		require.NotContains(t, w.Body.String(), "not supported for this platform")
	}
}

func TestGatewayRoutesOpenAIVideoPlatformSurfaceIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformOpenAI)
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, prefix := range []string{"/v1", ""} {
		for _, route := range []string{
			"POST " + prefix + "/videos",
			"GET " + prefix + "/videos",
			"GET " + prefix + "/videos/models",
			"GET " + prefix + "/videos/:request_id",
			"DELETE " + prefix + "/videos/:request_id",
			"GET " + prefix + "/videos/:request_id/content",
			"HEAD " + prefix + "/videos/:request_id/content",
			"POST " + prefix + "/videos/characters",
			"GET " + prefix + "/videos/characters/:character_id",
			"DELETE " + prefix + "/videos/characters/:character_id",
			"POST " + prefix + "/videos/edits",
			"POST " + prefix + "/videos/extensions",
		} {
			require.True(t, registered[route], "%s should be registered", route)
		}
	}
	require.True(t, registered["POST /webhooks/videos/:provider/:account_id"])
}

func TestGatewayRoutesOpenAIVideosDispatchToVideoHandler(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformOpenAI)
	handlers.Video = &handler.VideoHandler{}
	router := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformOpenAI, handlers)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"sora-2","prompt":"waves"}`)),
		httptest.NewRequest(http.MethodGet, "/v1/videos/models", nil),
		httptest.NewRequest(http.MethodGet, "/v1/videos/video_0123456789abcdef0123456789abcdef", nil),
	} {
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		require.Contains(t, w.Body.String(), "video_disabled")
		require.NotContains(t, w.Body.String(), "not supported for this platform")
	}
}

// TestGatewayRoutesVideoModelsDoesNotShadowTaskLookup 钉死 /videos/models 与
// /videos/:request_id 的共存关系：静态段先匹配，但任何可达的任务都不会被它遮蔽。
func TestGatewayRoutesVideoModelsDoesNotShadowTaskLookup(t *testing.T) {
	// 任务 public_id 必须是 video_ + 32 位十六进制，"models" 永远不是合法任务 id，
	// 所以静态段抢先匹配不会让任何真实任务查不到。
	require.False(t, service.IsValidVideoTaskID("models"))

	// Grok 分组下两个 handler 的响应可区分：models 走 videoUnsupported，
	// 任务查询走 Grok 转发器。以此确认路由确实分派到了各自的 handler，
	// 而不是靠"gin 静态段优先"这条推理。
	router := newGatewayRoutesTestRouter(service.PlatformGrok)
	for _, path := range []string{"/v1/videos/models", "/videos/models"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		// 这条消息只能由 videoModelsHandler 写出；gin 自己的 404 是空 body。
		require.Contains(t, w.Body.String(), "not supported for this platform", "path=%s", path)
	}
	for _, path := range []string{"/v1/videos/request-123", "/videos/request-123"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s", path)
		require.NotContains(t, w.Body.String(), "not supported for this platform", "path=%s", path)
	}
}

func TestGatewayRoutesByteDanceVideosDispatchToManagedVideoHandler(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformByteDance)
	handlers.Video = &handler.VideoHandler{}
	router := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformByteDance, handlers)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"doubao-seedance-1-0-lite-t2v-250428","prompt":"waves","seconds":8}`)),
		httptest.NewRequest(http.MethodPost, "/v1/videos/edits", strings.NewReader(`{"model":"doubao-seedance-1-0-lite-t2v-250428","prompt":"edit","video":{"id":"video_0123456789abcdef0123456789abcdef"}}`)),
		httptest.NewRequest(http.MethodPost, "/v1/videos/extensions", strings.NewReader(`{"model":"doubao-seedance-1-0-lite-t2v-250428","prompt":"extend","video":{"id":"video_0123456789abcdef0123456789abcdef"}}`)),
		httptest.NewRequest(http.MethodGet, "/v1/videos/video_0123456789abcdef0123456789abcdef", nil),
	} {
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		require.NotContains(t, w.Body.String(), "not supported for this platform", "path=%s", request.URL.Path)
		require.NotContains(t, w.Body.String(), "ByteDance gateway supports only", "path=%s", request.URL.Path)
	}
}

func TestGatewayRoutesByteDanceRejectsNonVideoProtocols(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformByteDance)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[]}`)),
		httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude","messages":[]}`)),
		httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt","input":"hello"}`)),
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt","messages":[]}`)),
		httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embed","input":"hello"}`)),
		httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt","input":"hello"}`)),
		httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"gpt","messages":[]}`)),
		httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`{"model":"gpt","input":"hello"}`)),
	} {
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s", request.URL.Path)
		require.Contains(t, w.Body.String(), "ByteDance gateway supports only the Videos API", "path=%s", request.URL.Path)
	}
}

func TestGatewayRoutesGrokCustomVoiceCRUDPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"POST /v1/custom-voices",
		"GET /v1/custom-voices",
		"GET /v1/custom-voices/:voice_id",
		"PATCH /v1/custom-voices/:voice_id",
		"DELETE /v1/custom-voices/:voice_id",
		"GET /v1/custom-voices/:voice_id/audio",
		"POST /custom-voices",
		"GET /custom-voices",
		"GET /custom-voices/:voice_id",
		"PATCH /custom-voices/:voice_id",
		"DELETE /custom-voices/:voice_id",
		"GET /custom-voices/:voice_id/audio",
	} {
		require.True(t, registered[route], "%s should be registered", route)
	}
}

func TestGrokCustomVoiceEndpointUsesRouteTemplateNotRawPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var got string
	capture := func(c *gin.Context) {
		got = grokCustomVoiceEndpoint(c)
		c.Status(http.StatusOK)
	}
	router.GET("/v1/custom-voices/:voice_id/audio", capture)
	router.GET("/v1/custom-voices/:voice_id", capture)

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/v1/custom-voices/voice-123", want: "custom-voices/voice-123"},
		{path: "/v1/custom-voices/voice-123/audio", want: "custom-voices/voice-123/audio"},
		// A voice literally named "audio" matches /:voice_id, not /:voice_id/audio.
		// A raw-path suffix check would turn this profile lookup into an audio download.
		{path: "/v1/custom-voices/audio", want: "custom-voices/audio"},
		{path: "/v1/custom-voices/audio/audio", want: "custom-voices/audio/audio"},
	} {
		got = ""
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "path=%s", tc.path)
		require.Equal(t, tc.want, got, "path=%s", tc.path)
	}
}

func TestGatewayRoutesCompositeVideoLookupsUseGrokHandler(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformComposite)

	for _, path := range []string{
		"/v1/videos/request-123",
		"/videos/request-123",
		"/v1/videos/request-123/content",
		"/videos/request-123/content",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit Grok video lookup handler", path)
		require.NotContains(t, w.Body.String(), "not supported for this platform")
	}
}

func TestGatewayRoutesCompositeMessagesWithGrokModelUsesOpenAIGateway(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformComposite)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"grok-4.3","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.NotContains(t, w.Body.String(), "not supported")
	require.NotContains(t, w.Body.String(), "OpenAI-compatible endpoint")
	require.NotContains(t, w.Body.String(), "composite groups")
}

func TestGatewayRoutesCompositeChatCompletionsWithGrokModelUsesOpenAIGateway(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformComposite)

	for _, path := range []string{"/v1/chat/completions", "/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok-4.3","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s", path)
		require.NotContains(t, w.Body.String(), "not supported")
		require.NotContains(t, w.Body.String(), "OpenAI-compatible endpoint")
		require.NotContains(t, w.Body.String(), "composite groups")
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
		{http.MethodPost, "/v1/videos", `{"model":"grok-imagine-video-1.5","prompt":"waves"}`},
		{http.MethodPost, "/videos", `{"model":"grok-imagine-video-1.5","prompt":"waves"}`},
		{http.MethodPost, "/videos/generations", `{"model":"grok-imagine-video-1.5","prompt":"waves"}`},
		{http.MethodPost, "/v1/videos/edits", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/videos/edits", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/v1/videos/extensions", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodPost, "/videos/extensions", `{"model":"grok-imagine-video","prompt":"waves","video":{"url":"https://example.com/in.mp4"}}`},
		{http.MethodGet, "/v1/videos/request-123", ""},
		{http.MethodGet, "/videos/request-123", ""},
		{http.MethodGet, "/v1/videos/generations/request-123", ""},
		{http.MethodGet, "/videos/generations/request-123", ""},
		{http.MethodGet, "/v1/videos/edits/request-123", ""},
		{http.MethodGet, "/videos/edits/request-123", ""},
		{http.MethodGet, "/v1/videos/extensions/request-123", ""},
		{http.MethodGet, "/videos/extensions/request-123", ""},
		{http.MethodGet, "/v1/videos/request-123/content", ""},
		{http.MethodGet, "/videos/request-123/content", ""},
		{http.MethodGet, "/v1/videos/generations/request-123/content", ""},
		{http.MethodGet, "/videos/generations/request-123/content", ""},
		{http.MethodGet, "/v1/videos/edits/request-123/content", ""},
		{http.MethodGet, "/videos/edits/request-123/content", ""},
		{http.MethodGet, "/v1/videos/extensions/request-123/content", ""},
		{http.MethodGet, "/videos/extensions/request-123/content", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "method=%s path=%s", tc.method, tc.path)
		require.Contains(t, w.Body.String(), "Videos API is not supported for this platform")
	}
}

func TestGatewayRoutesCompositeVideoGenerationAllowed(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformComposite)

	req := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(`{"model":"grok-imagine-video-1.5","prompt":"waves"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.NotContains(t, w.Body.String(), "not supported")
}

func TestGatewayRoutesCompositeOpenAIOnlyEndpointsRequireOpenAITarget(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformComposite)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"gemini-2.5-pro","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.NotEqual(t, http.StatusNotFound, w.Code)
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

// TestGatewayRoutesResponsesSubpathRejectsNonConformingSubpaths 端到端锁定不变式：
// /responses/*subpath 的子路径会被转发到上游同名端点之后，因此不合规的子路径必须
// 在入口就被拒绝，不得进入调度与转发流程。
func TestGatewayRoutesResponsesSubpathRejectsNonConformingSubpaths(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/../../x/y",
		"/v1/responses/..%2f..%2fx/y",
		"/v1/responses/%2e%2e/%2e%2e/x",
		"/responses/%2e%2e%2fx",
		"/backend-api/codex/responses/..%2f..%2fx",
		`/v1/responses/..\..\x`,
		"/v1/responses/%3fa=b",
		"/v1/responses/x%23frag",
		"/v1/responses/compact%2f..",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s must be rejected at the edge", path)
		require.Contains(t, w.Body.String(), "Unsupported responses subpath", "path=%s", path)
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
		require.Contains(t, w.Body.String(), "MiniMax gateway does not support this endpoint", "path=%s", path)
	}
}

func TestGatewayRoutesMiniMaxUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be MiniMax unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "MiniMax gateway does not support this endpoint", "path=%s", path)
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

func TestGatewayRoutesCNProvidersDispatchToOpenAIGateway(t *testing.T) {
	for _, platform := range []string{
		service.PlatformGLM,
		service.PlatformZhipu,
		service.PlatformKimi,
		service.PlatformDeepSeek,
	} {
		router := newGatewayRoutesTestRouterForPlatformWithoutProviderHandlers(platform)
		for _, path := range []string{
			"/v1/messages",
			"/v1/chat/completions",
			"/chat/completions",
			"/v1/responses",
			"/responses",
			"/v1/messages/count_tokens",
			"/v1/responses/compact",
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.NotEqual(t, http.StatusNotFound, w.Code, "platform=%s path=%s", platform, path)
			require.NotContains(t, w.Body.String(), "gateway service unavailable", "platform=%s path=%s", platform, path)
			require.NotContains(t, w.Body.String(), "does not support this endpoint", "platform=%s path=%s", platform, path)
		}
	}
}

func TestGatewayRoutesCNProvidersImagesRemainUnsupported(t *testing.T) {
	for _, platform := range []string{
		service.PlatformGLM,
		service.PlatformZhipu,
		service.PlatformKimi,
		service.PlatformDeepSeek,
	} {
		router := newGatewayRoutesTestRouterForPlatformWithoutProviderHandlers(platform)
		for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"test"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusNotFound, w.Code, "platform=%s path=%s", platform, path)
			require.Contains(t, w.Body.String(), "Images API is not supported for this platform", "platform=%s path=%s", platform, path)
		}
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

func TestGatewayRoutesKimiModelsReturnsDefaultList(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformKimi)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "kimi-for-coding")
	require.NotContains(t, w.Body.String(), "claude-sonnet")
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
		require.Contains(t, w.Body.String(), "Windsurf gateway does not support this endpoint", "path=%s", path)
	}
}

func TestGatewayRoutesProviderResponsesRootsDispatchToProviderHandlers(t *testing.T) {
	for _, tc := range []struct {
		platform string
		body     string
		message  string
	}{
		{platform: service.PlatformMiniMax, body: `{"model":"MiniMax-M2.7","input":"hello"}`, message: "minimax gateway service unavailable"},
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
		{platform: service.PlatformMiniMax, message: "MiniMax gateway does not support this endpoint"},
		{platform: service.PlatformWindsurf, message: "Windsurf gateway does not support this endpoint"},
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

// Scenario: Grok 的语音 / 搜索能力同时挂在 /v1 与根路径两个入口上。ByteDance 网关只
// 放行 Videos API，若只有 /v1 入口挂了 byteDanceVideoOnly、根路径漏挂，根路径就成了
// 绕过口。这里把「两个入口对 ByteDance 表现一致」钉成不变量，而不是只钉某一侧的 404。
func TestGatewayRoutesByteDanceGatesRootAndV1VoiceSearchPathsIdentically(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformByteDance)
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/tts"},
		{http.MethodPost, "/stt"},
		{http.MethodPost, "/custom-voices"},
		{http.MethodGet, "/custom-voices"},
		{http.MethodGet, "/custom-voices/voice_0123456789abcdef"},
		{http.MethodPatch, "/custom-voices/voice_0123456789abcdef"},
		{http.MethodDelete, "/custom-voices/voice_0123456789abcdef"},
		{http.MethodGet, "/custom-voices/voice_0123456789abcdef/audio"},
		{http.MethodGet, "/realtime"},
		{http.MethodPost, "/web_search"},
		{http.MethodPost, "/x_search"},
	} {
		for _, path := range []string{route.path, "/v1" + route.path} {
			request := httptest.NewRequest(route.method, path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, http.StatusNotFound, w.Code, "%s %s", route.method, path)
			require.Contains(t, w.Body.String(), "ByteDance gateway supports only the Videos API",
				"%s %s 未被 byteDanceVideoOnly 拦下；根路径与 /v1 必须一致，否则根路径是绕过口", route.method, path)
		}
	}
}
