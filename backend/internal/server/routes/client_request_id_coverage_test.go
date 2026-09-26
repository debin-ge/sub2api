package routes

import (
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// handlerChainProbe 记录 gin 为每条路由组装出的完整 handler 链（函数名）。
//
// gin.RouteInfo 只暴露最后一个 handler，无法直接检查中间件；这里在注册任何路由之前
// 往 engine 挂一个最前置的探针中间件，对每条路由发一个假请求，在探针处读出
// c.HandlerNames() 后立刻 Abort —— 真实 handler/中间件一个都不会执行，因此可以用
// 零依赖的空 handler 构造路由表。
type handlerChainProbe struct {
	chains map[string][]string // key: "METHOD /full/path/template"
}

// newHandlerChainProbe 必须在 RegisterXxxRoutes 之前调用（gin 在注册时拼接 handler 链）。
func newHandlerChainProbe(router *gin.Engine) *handlerChainProbe {
	p := &handlerChainProbe{chains: map[string][]string{}}
	router.Use(func(c *gin.Context) {
		p.chains[routeKey(c.Request.Method, c.FullPath())] = c.HandlerNames()
		c.Abort()
	})
	return p
}

// collect 遍历 router.Routes()，对每条路由触发一次探针并确认被观察到。
func (p *handlerChainProbe) collect(t *testing.T, router *gin.Engine) {
	t.Helper()
	for _, route := range router.Routes() {
		req := httptest.NewRequest(route.Method, concreteRoutePath(route.Path), nil)
		router.ServeHTTP(httptest.NewRecorder(), req)
		require.Contains(t, p.chains, routeKey(route.Method, route.Path),
			"probe did not observe %s %s (dispatched elsewhere?)", route.Method, route.Path)
	}
}

func routeKey(method, path string) string {
	return method + " " + path
}

// concreteRoutePath 把路由模板中的 :param / *catchall 段替换成一个具体值，便于发假请求。
func concreteRoutePath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			segments[i] = "probe"
		}
	}
	return strings.Join(segments, "/")
}

func chainContains(names []string, needle string) bool {
	for _, name := range names {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}

// 各中间件在 runtime.FuncForPC 中的稳定片段（含包路径尾部，避免同名误匹配）。
const (
	clientRequestIDHandlerName        = "internal/server/middleware.ClientRequestID.func1"
	googleAPIKeyAuthHandlerName       = "internal/server/middleware.APIKeyAuthWithSubscriptionGoogle"
	rejectOversizedBodyHandlerName    = "internal/server/routes.rejectOversizedBody.func1"
	requestBodyLimitHandlerName       = "internal/server/middleware.RequestBodyLimit.func1"
	panelPublicIPRateLimitHandlerName = "PanelRateLimiter).PublicIP.func1"
)

// gatewayCoverageAPIKeyAuthSentinel 是可被 runtime.FuncForPC 识别的具名 API-Key 鉴权桩，
// 用来判定"这条路由挂了 apiKeyAuth"。
func gatewayCoverageAPIKeyAuthSentinel(c *gin.Context) { c.Next() }

// gatewayBillingPathPrefixes 是即便没识别出 API-Key 鉴权，也一律视为计费路由的路径前缀。
var gatewayBillingPathPrefixes = []string{
	"/v1", "/v1beta", "/antigravity", "/backend-api",
	"/images", "/videos", "/messages", "/responses", "/chat",
}

func isGatewayBillingRoute(key string, names []string) bool {
	path := key[strings.Index(key, " ")+1:]
	for _, prefix := range gatewayBillingPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return chainContains(names, "routes.gatewayCoverageAPIKeyAuthSentinel") ||
		chainContains(names, googleAPIKeyAuthHandlerName)
}

// SEC-016：计费幂等键只信任 middleware.ClientRequestID 注入的 client_request_id
// （resolveUsageBillingRequestID 已删除 "local:" 回退）。因此任何经 API-Key 鉴权、
// 会记录用量的网关路由都必须挂 ClientRequestID，否则该路由会退化成 upstream/generated
// 键，丢失客户端重试去重能力；反过来若有人把客户端可控的 X-Request-ID 重新接回计费，
// 缺中间件的路由就会变成免单重放口。本测试把这条约束钉死在路由表上。
func TestEveryAPIKeyGatewayRouteHasClientRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	probe := newHandlerChainProbe(router)
	RegisterGatewayRoutes(
		router,
		defaultGatewayRoutesTestHandlers(service.PlatformOpenAI),
		servermiddleware.APIKeyAuthMiddleware(gatewayCoverageAPIKeyAuthSentinel),
		nil, nil, nil, nil, nil,
		defaultGatewayRoutesTestConfig(),
	)
	probe.collect(t, router)

	// 明确放行的非计费路由：只读目录类接口，不会产生 usage_logs / 扣费，因此没有需要保护的幂等键。
	// 新增条目必须写明理由；条目对应的路由消失时测试会报 stale。
	allowlist := map[string]string{
		"GET /antigravity/models": "Antigravity 模型目录列表：只读、不记录用量，无计费幂等键可保护",
	}

	var checked, missing []string
	for key, names := range probe.chains {
		if reason, ok := allowlist[key]; ok {
			require.NotEmpty(t, strings.TrimSpace(reason), "allowlist entry %s needs a reason", key)
			continue
		}
		if !isGatewayBillingRoute(key, names) {
			continue
		}
		checked = append(checked, key)
		if !chainContains(names, clientRequestIDHandlerName) {
			missing = append(missing, key)
		}
	}
	sort.Strings(checked)
	sort.Strings(missing)

	require.Empty(t, missing, "billing gateway routes without middleware.ClientRequestID (add it, or allowlist with a reason if the route never records usage)")
	// 防止判定条件失效后测试空转：几条核心计费路由必须被覆盖到。
	for _, must := range []string{
		"POST /v1/messages",
		"POST /v1/chat/completions",
		"POST /responses",
		"POST /v1beta/models/*modelAction",
		"POST /backend-api/codex/responses",
		"POST /images/generations",
		"POST /videos",
		"POST /antigravity/v1/messages",
	} {
		require.Contains(t, checked, must, "expected %s to be classified as a billing route", must)
	}
	for key := range allowlist {
		require.Contains(t, probe.chains, key, "stale allowlist entry %s: route no longer registered", key)
	}
}
