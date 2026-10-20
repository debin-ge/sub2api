package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func registerPaymentRoutesForTest(router *gin.Engine, clients ...*redis.Client) {
	var client *redis.Client
	if len(clients) > 0 {
		client = clients[0]
	}
	RegisterPaymentRoutes(
		router.Group("/api/v1"),
		&handler.PaymentHandler{},
		&handler.PaymentWebhookHandler{},
		admin.NewPaymentHandler(nil, nil),
		middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
		nil,
		nil,
		client,
	)
}

var paymentPublicRouteKeys = []string{
	"POST /api/v1/payment/public/orders/verify",
	"POST /api/v1/payment/public/orders/resolve",
}

// SEC-018：匿名支付查询接口必须挂按 IP 的面板公开限流；SEC-012：并且请求体受 64KiB 上限约束。
func TestRegisterPaymentRoutesPublicEndpointsAreRateLimitedAndBodyBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	probe := newHandlerChainProbe(router)
	registerPaymentRoutesForTest(router)
	probe.collect(t, router)

	for _, key := range paymentPublicRouteKeys {
		names, ok := probe.chains[key]
		require.True(t, ok, "route %s not registered", key)
		require.True(t, chainContains(names, panelPublicIPRateLimitHandlerName), "%s lacks PanelRateLimiter.PublicIP: %v", key, names)
		require.True(t, chainContains(names, rejectOversizedBodyHandlerName), "%s lacks rejectOversizedBody: %v", key, names)
		require.True(t, chainContains(names, requestBodyLimitHandlerName), "%s lacks RequestBodyLimit: %v", key, names)
	}
}

func TestRegisterPaymentRoutesPublicVerifyRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	registerPaymentRoutesForTest(router, rdb)

	oversized := `{"out_trade_no":"` + strings.Repeat("9", 8<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/public/orders/verify", strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	// 正常大小但格式错误的 body 必须穿过限制层到达 handler（handler 绑定失败返回 400）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/payment/public/orders/verify", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Invalid request")
}

func TestRegisterPaymentRoutesPublicLookupsFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"verify", "resolve"} {
		t.Run(path, func(t *testing.T) {
			// A closed client fails locally. The nil payment services would panic if
			// a valid lookup passed the limiter and reached the handler.
			rdb := redis.NewClient(&redis.Options{Addr: "unused.invalid:6379"})
			require.NoError(t, rdb.Close())
			router := gin.New()
			registerPaymentRoutesForTest(router, rdb)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/public/orders/"+path,
				strings.NewReader(`{"out_trade_no":"synthetic","resume_token":"synthetic"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusTooManyRequests, w.Code)
			require.NotEmpty(t, w.Header().Get("Retry-After"))
		})
	}
}

func TestRegisterPaymentRoutesIncludesWiseWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterPaymentRoutes(
		router.Group("/api/v1"),
		&handler.PaymentHandler{},
		&handler.PaymentWebhookHandler{},
		admin.NewPaymentHandler(nil, nil),
		middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
		nil,
		nil,
		nil,
	)

	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/v1/payment/webhook/wise" {
			return
		}
	}
	require.Fail(t, "POST /api/v1/payment/webhook/wise route was not registered")
}
