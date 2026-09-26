package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newGatewayRoutesTestRouterRejectingAPIKeys 用一个总是 401 的 API Key 中间件建路由，
// 以此区分"挂了 API Key 鉴权"与"没挂"的路由。
func newGatewayRoutesTestRouterRejectingAPIKeys(handlers *handler.Handlers, apiKeyCalls *int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterGatewayRoutes(
		router,
		handlers,
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			*apiKeyCalls++
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "api_key_required"}})
		}),
		nil, nil, nil, nil, nil,
		defaultGatewayRoutesTestConfig(),
	)
	return router
}

func TestGatewayRoutesSignedVideoContentIsRegisteredWithoutAPIKeyAuth(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformOpenAI)
	handlers.Video = &handler.VideoHandler{}
	apiKeyCalls := 0
	router := newGatewayRoutesTestRouterRejectingAPIKeys(handlers, &apiKeyCalls)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["GET /v1/videos/:request_id/content/signed"])
	require.True(t, registered["HEAD /v1/videos/:request_id/content/signed"])
	// 签名路由只有一条，不再在无前缀根路径下重复暴露。
	require.False(t, registered["GET /videos/:request_id/content/signed"])
	// 需要 API Key 的内容路由原样保留。
	require.True(t, registered["GET /v1/videos/:request_id/content"])
	require.True(t, registered["HEAD /v1/videos/:request_id/content"])

	target := "/v1/videos/video_0123456789abcdef0123456789abcdef/content/signed?exp=1&sig=00"
	// 一个零值 VideoHandler（无配置）会在 SignedContent 里回 video_disabled 的 JSON 错误：
	// 这条消息只能由 handler 写出，说明请求绕过了 API Key 鉴权直达 handler。
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	require.NotEqual(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "video_disabled")
	getStatus := w.Code
	// HEAD 走同一条链：状态码与 GET 一致，没有正文。
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, target, nil))
	require.Equal(t, getStatus, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	require.Zero(t, apiKeyCalls, "signed content route must not run API key auth")

	// 对照：需要 API Key 的内容路由仍然被 401 拦住。
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/videos/video_0123456789abcdef0123456789abcdef/content", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, 1, apiKeyCalls)
}

func TestGatewayRoutesSignedVideoContentWithoutVideoHandlerIsNotFound(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformOpenAI)
	handlers.Video = nil
	apiKeyCalls := 0
	router := newGatewayRoutesTestRouterRejectingAPIKeys(handlers, &apiKeyCalls)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/videos/video_0123456789abcdef0123456789abcdef/content/signed?exp=1&sig=00", nil))

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not_found_error")
	require.Zero(t, apiKeyCalls)
}

func TestGatewayRoutesSignedVideoContentDoesNotShadowAuthenticatedContentRoute(t *testing.T) {
	// 两条路由共用 /v1/videos/:request_id 前缀：签名路由多一个静态段 /signed，
	// 不会把带 API Key 的 /content 请求吞掉，也不会被它吞掉。
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformOpenAI)
	handlers.Video = &handler.VideoHandler{}
	router := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformOpenAI, handlers)

	for _, target := range []string{
		"/v1/videos/video_0123456789abcdef0123456789abcdef/content",
		"/v1/videos/video_0123456789abcdef0123456789abcdef/content/signed?exp=1&sig=00",
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		// 两条路径都到达了各自的 handler（video_disabled 只会由 handler 写出），
		// 而不是 gin 的纯文本 404。
		require.Contains(t, w.Body.String(), "video_disabled", "target=%s", target)
		require.NotEqual(t, "404 page not found", w.Body.String(), "target=%s", target)
	}
}
