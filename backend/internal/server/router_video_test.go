package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routerVideoSettingRepo struct{}

func (*routerVideoSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (*routerVideoSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (*routerVideoSettingRepo) Set(context.Context, string, string) error { return nil }

func (*routerVideoSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*routerVideoSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }

func (*routerVideoSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*routerVideoSettingRepo) Delete(context.Context, string) error { return nil }

// TestSetupRouterInstallsNoGlobalVideoMiddleware 钉死 SEC-014 的修复：视频内容代理不再
// 作为全局中间件挂在所有请求前面（曾经每个 GET/HEAD 都会按请求 URI 反查视频任务并
// 用账号凭据把上游内容回给任何持有链接的人）。
func TestSetupRouterInstallsNoGlobalVideoMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Gateway.MaxBodySize = 1024 * 1024
	cfg.Gateway.Video.Enabled = true
	cfg.Gateway.Video.ContentProxy.Enabled = true
	settingService := service.NewSettingService(&routerVideoSettingRepo{}, cfg)
	radarHandler, err := handler.NewRadarHandler(cfg, &routerRadarServiceStub{})
	require.NoError(t, err)
	pass := func(c *gin.Context) { c.Next() }
	apiKeyCalls := 0
	router := SetupRouter(
		gin.New(),
		&handler.Handlers{Radar: radarHandler, Admin: &handler.AdminHandlers{}, Video: &handler.VideoHandler{}},
		middleware2.JWTAuthMiddleware(pass),
		middleware2.AdminAuthMiddleware(pass),
		middleware2.APIKeyAuthMiddleware(func(c *gin.Context) {
			apiKeyCalls++
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		nil,
		nil,
		nil,
		nil,
		nil,
		settingService,
		nil,
		cfg,
		nil,
	)

	// 全局中间件链里不允许出现任何 VideoHandler 方法。
	for _, middleware := range router.Handlers {
		name := runtime.FuncForPC(reflect.ValueOf(middleware).Pointer()).Name()
		require.NotContains(t, name, "VideoHandler", "global middleware %s must not be a video handler", name)
		require.NotContains(t, name, "PublicContentProxy", "global middleware %s must not be a video handler", name)
	}

	// 非视频路径不会被任何视频逻辑接管：没有匹配路由时就是 gin 自己的纯文本 404，
	// 而不是视频 handler 写出的 JSON 错误或内容响应。
	for _, target := range []string{
		"/assets/x.js",
		"/assets/render/final.mp4?token=signed&disposition=inline",
		"/1fbe6da3be7446b2af1602ca2a2feeea?preview=1&auth_key=signed",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, target, nil))
			require.Equal(t, http.StatusNotFound, w.Code, "%s %s", method, target)
			require.NotContains(t, w.Body.String(), "video", "%s %s", method, target)
			require.NotContains(t, w.Header().Get("Content-Type"), "application/json", "%s %s", method, target)
			require.Empty(t, w.Header().Get("Content-Disposition"), "%s %s", method, target)
			if method == http.MethodGet {
				require.Equal(t, "404 page not found", w.Body.String(), "%s %s", method, target)
			}
		}
	}
	require.Zero(t, apiKeyCalls)

	// 唯一的公开内容入口是签名路由，且它不走 API Key 鉴权。
	var signed []string
	for _, route := range router.Routes() {
		if strings.HasSuffix(route.Path, "/content/signed") {
			signed = append(signed, route.Method+" "+route.Path)
		}
	}
	require.ElementsMatch(t, []string{
		"GET /v1/videos/:request_id/content/signed",
		"HEAD /v1/videos/:request_id/content/signed",
	}, signed)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/videos/video_0123456789abcdef0123456789abcdef/content/signed?exp=1&sig=00", nil))
	require.Contains(t, w.Body.String(), "video_disabled")
	require.Zero(t, apiKeyCalls)
}
