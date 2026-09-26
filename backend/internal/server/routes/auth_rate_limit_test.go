package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAuthRoutesTestRouter(redisClient *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAuthRoutesForTest(router, redisClient)
	return router
}

func registerAuthRoutesForTest(router *gin.Engine, redisClient *redis.Client) {
	v1 := router.Group("/api/v1")

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    &handler.AuthHandler{},
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		redisClient,
		nil,
		nil,
	)
}

// newAuthRoutesTestRouterWithWorkingRedis 用 miniredis 提供可用的限流后端，
// 让请求能穿过 fail-close 限流层到达 body 限制 / handler。
func newAuthRoutesTestRouterWithWorkingRedis(t *testing.T) *gin.Engine {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return newAuthRoutesTestRouter(rdb)
}

func postAuthJSON(router *gin.Engine, path, body string, chunked bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:12345"
	if chunked {
		// 模拟未声明长度的 chunked 上传：Content-Length 预检无法命中，只能靠 MaxBytesReader 截断
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// SEC-012：匿名认证入口的请求体必须被 64KiB 上限拦住，而不是只受 256MiB 全局上限约束。
func TestAuthRoutesRegisterRejectsOversizedBodyWith413(t *testing.T) {
	router := newAuthRoutesTestRouterWithWorkingRedis(t)

	oversized := `{"email":"` + strings.Repeat("a", 8<<20) + `"}`
	w := postAuthJSON(router, "/api/v1/auth/register", oversized, false)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Contains(t, w.Body.String(), "request body too large")
}

func TestAuthRoutesRegisterOversizedChunkedBodyIsCutAtLimit(t *testing.T) {
	router := newAuthRoutesTestRouterWithWorkingRedis(t)

	oversized := `{"email":"` + strings.Repeat("a", 8<<20) + `"}`
	w := postAuthJSON(router, "/api/v1/auth/register", oversized, true)
	// 没有 Content-Length 时无法提前 413；handler 在读到 64KiB 时即收到 MaxBytesError 并以 400 拒绝，
	// 8MB 不会被整体读入内存。
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "request body too large")
}

func TestAuthRoutesRegisterNormalBodyReachesHandler(t *testing.T) {
	router := newAuthRoutesTestRouterWithWorkingRedis(t)

	// 空对象体积正常，会穿过限流与 body 限制到达 handler；handler 因缺少必填字段返回 400
	// （而非 413/429），证明限制层没有误伤正常请求。
	w := postAuthJSON(router, "/api/v1/auth/register", `{}`, false)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Invalid request")
	require.NotContains(t, w.Body.String(), "too large")
}

// 结构性断言：/auth、/settings、/channels 三个匿名组下的每条路由都挂了 body 上限中间件。
func TestAuthRoutesEveryPublicRouteHasBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	probe := newHandlerChainProbe(router)
	registerAuthRoutesForTest(router, nil)
	probe.collect(t, router)

	var checked int
	for key, names := range probe.chains {
		path := key[strings.Index(key, " ")+1:]
		if !strings.HasPrefix(path, "/api/v1/auth/") && !strings.HasPrefix(path, "/api/v1/settings/") && !strings.HasPrefix(path, "/api/v1/channels/") {
			continue
		}
		switch path {
		case "/api/v1/auth/me", "/api/v1/auth/revoke-all-sessions", "/api/v1/auth/oauth/bind-token":
			// 需要 JWT 的当前用户接口注册在 authenticated 组，不在匿名组内，不属于本测试范围
			continue
		}
		checked++
		require.True(t, chainContains(names, rejectOversizedBodyHandlerName), "%s lacks rejectOversizedBody: %v", key, names)
		require.True(t, chainContains(names, requestBodyLimitHandlerName), "%s lacks RequestBodyLimit: %v", key, names)
	}
	require.Greater(t, checked, 40, "expected the full anonymous auth surface to be checked")
}

func TestAuthRoutesRateLimitFailCloseWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAuthRoutesTestRouter(rdb)
	paths := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/login/2fa",
		"/api/v1/auth/send-verify-code",
		"/api/v1/auth/oauth/pending/send-verify-code",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:12345"

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusTooManyRequests, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "rate limit exceeded", "path=%s", path)
	}
}
