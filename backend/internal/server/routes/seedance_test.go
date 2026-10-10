package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Ark（Seedance）任务协议是五家托管视频平台（ByteDance / fal / WaveSpeed / Runware / Flova）
// 的唯一公开视频入口，挂在 /api/v3、/v3、/v1 与无前缀四组别名下。下面的契约测试用一个零值
// VideoHandler：它没有配置与任务服务，任何到达 handler 的请求都会回 video_disabled——这条消息
// 只能由 VideoHandler 写出，是"请求穿过了全部中间件并分派到了 Ark handler"的凭证。

var arkVideoRoutePrefixes = []string{"/api/v3", "/v3", "/v1", ""}

var arkVideoPlatforms = []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova}

const arkVideoRouteTaskID = "video_0123456789abcdef0123456789abcdef"

const arkVideoRouteCreateBody = `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}]}`

func arkVideoRouteRequests(prefix string) []*http.Request {
	tasks := prefix + "/contents/generations/tasks"
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, tasks, strings.NewReader(arkVideoRouteCreateBody)),
		httptest.NewRequest(http.MethodGet, tasks, nil),
		httptest.NewRequest(http.MethodGet, tasks+"?page_num=1&page_size=10&filter.status=succeeded", nil),
		httptest.NewRequest(http.MethodGet, tasks+"/"+arkVideoRouteTaskID, nil),
		httptest.NewRequest(http.MethodDelete, tasks+"/"+arkVideoRouteTaskID, nil),
		httptest.NewRequest(http.MethodGet, prefix+"/contents/generations/models", nil),
	}
	for _, request := range requests {
		request.Header.Set("Content-Type", "application/json")
	}
	return requests
}

func TestArkVideoTaskRoutesAreRegisteredUnderEveryPrefix(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformByteDance)
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, prefix := range arkVideoRoutePrefixes {
		for _, route := range []string{
			"POST " + prefix + "/contents/generations/tasks",
			"GET " + prefix + "/contents/generations/tasks",
			"GET " + prefix + "/contents/generations/tasks/:task_id",
			"DELETE " + prefix + "/contents/generations/tasks/:task_id",
			"GET " + prefix + "/contents/generations/tasks/:task_id/content",
			"HEAD " + prefix + "/contents/generations/tasks/:task_id/content",
			"GET " + prefix + "/contents/generations/models",
		} {
			require.True(t, registered[route], "%s should be registered", route)
		}
	}
	// 旧的 Seedance 透传已退役：OpenAIGatewayHandler 不再有透传方法，端点常量改名但值不变。
	_, exists := reflect.TypeOf(&handler.OpenAIGatewayHandler{}).MethodByName("SeedanceTasks")
	require.False(t, exists)
	require.Equal(t, "/api/v3/contents/generations/tasks", handler.EndpointVideoTasks)
}

func TestArkVideoTaskRoutesDispatchForVideoOnlyPlatforms(t *testing.T) {
	for _, platform := range arkVideoPlatforms {
		handlers := defaultGatewayRoutesTestHandlers(platform)
		handlers.Video = &handler.VideoHandler{}
		router := newGatewayRoutesTestRouterForPlatformWithHandlers(platform, handlers)
		for _, prefix := range arkVideoRoutePrefixes {
			for _, request := range arkVideoRouteRequests(prefix) {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				require.Contains(t, w.Body.String(), "video_disabled", "platform=%s %s %s must reach the Ark handler", platform, request.Method, request.URL.Path)
				require.NotContains(t, w.Body.String(), "video_ark_platform_unsupported", "platform=%s %s %s", platform, request.Method, request.URL.Path)
				require.NotContains(t, w.Body.String(), "supports only", "platform=%s %s %s", platform, request.Method, request.URL.Path)
				require.NotEqual(t, http.StatusForbidden, w.Code, "platform=%s %s %s", platform, request.Method, request.URL.Path)
			}
		}
	}
}

func TestArkVideoTaskRoutesDispatchForCompositeGroups(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformComposite)
	handlers.Video = &handler.VideoHandler{}
	router := newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformComposite, handlers)
	for _, prefix := range arkVideoRoutePrefixes {
		for _, request := range arkVideoRouteRequests(prefix) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.NotEqual(t, http.StatusForbidden, w.Code, "%s %s", request.Method, request.URL.Path)
			require.NotContains(t, w.Body.String(), "video_ark_platform_unsupported", "%s %s", request.Method, request.URL.Path)
			require.NotContains(t, w.Body.String(), "not supported for this platform", "%s %s", request.Method, request.URL.Path)
			if request.Method != http.MethodPost {
				// 读路径直达 Ark handler；创建路径先经合成路由解析（零值 handler 没有任务服务，回 503），
				// 两者都说明请求没有被平台门禁拦下。
				require.Contains(t, w.Body.String(), "video_disabled", "%s %s", request.Method, request.URL.Path)
			}
		}
	}
}

func TestArkVideoTaskRoutesRejectOtherPlatforms(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformAnthropic, service.PlatformGemini} {
		handlers := defaultGatewayRoutesTestHandlers(platform)
		handlers.Video = &handler.VideoHandler{}
		router := newGatewayRoutesTestRouterForPlatformWithHandlers(platform, handlers)
		for _, prefix := range arkVideoRoutePrefixes {
			for _, request := range arkVideoRouteRequests(prefix) {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				require.Equal(t, http.StatusForbidden, w.Code, "platform=%s %s %s: %s", platform, request.Method, request.URL.Path, w.Body.String())
				require.Contains(t, w.Body.String(), "video_ark_platform_unsupported", "platform=%s %s %s", platform, request.Method, request.URL.Path)
			}
		}
	}
}

func TestArkVideoTaskRoutesWithoutVideoHandlerFallBackToVideosUnsupported(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformOpenAI)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v3/contents/generations/tasks/"+arkVideoRouteTaskID, nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "Videos API is not supported for this platform")

	// 五家平台在没有视频处理器时同样得到指向 Ark 入口的提示，而不是空的 gin 404。
	w = httptest.NewRecorder()
	newGatewayRoutesTestRouter(service.PlatformByteDance).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v3/contents/generations/tasks/"+arkVideoRouteTaskID, nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "use_contents_generations_tasks")
}

// 签名内容链接自带 HMAC 与过期时间，不挂 API Key 鉴权；其余 Ark 路由全部要求 API Key。
func TestArkVideoSignedContentRouteIsRegisteredWithoutAPIKeyAuth(t *testing.T) {
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformByteDance)
	handlers.Video = &handler.VideoHandler{}
	apiKeyCalls := 0
	router := newGatewayRoutesTestRouterRejectingAPIKeys(handlers, &apiKeyCalls)
	for _, prefix := range arkVideoRoutePrefixes {
		target := prefix + "/contents/generations/tasks/" + arkVideoRouteTaskID + "/content?variant=video&exp=1&sig=00"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		require.NotEqual(t, http.StatusUnauthorized, w.Code, target)
		require.Contains(t, w.Body.String(), "video_disabled", target)
		getStatus := w.Code
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, target, nil))
		require.Equal(t, getStatus, w.Code, target)
		require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	}
	require.Zero(t, apiKeyCalls, "signed content routes must not run API key auth")

	// 对照：任务查询、列表、删除与目录都被 401 拦住。
	expectedCalls := 0
	for _, prefix := range arkVideoRoutePrefixes {
		for _, request := range arkVideoRouteRequests(prefix) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			expectedCalls++
			require.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", request.Method, request.URL.Path)
		}
	}
	require.Equal(t, expectedCalls, apiKeyCalls)
}

// 五家平台的 /v1/videos* 已下线：所有方法与子路径都回 404 use_contents_generations_tasks，
// 并在消息里指向新入口；composite 与 OpenAI 分组的 /v1/videos* 不受影响。
func TestVideoOnlyPlatformsAreRedirectedFromOpenAIVideosRoutes(t *testing.T) {
	for _, platform := range arkVideoPlatforms {
		handlers := defaultGatewayRoutesTestHandlers(platform)
		handlers.Video = &handler.VideoHandler{}
		router := newGatewayRoutesTestRouterForPlatformWithHandlers(platform, handlers)
		label := service.VideoOnlyPlatformLabel(platform)
		for _, prefix := range []string{"/v1", ""} {
			for _, request := range []*http.Request{
				httptest.NewRequest(http.MethodPost, prefix+"/videos", strings.NewReader(`{"model":"doubao-seedance-1-0-lite-t2v-250428","prompt":"waves","seconds":8}`)),
				httptest.NewRequest(http.MethodGet, prefix+"/videos", nil),
				httptest.NewRequest(http.MethodGet, prefix+"/videos/models", nil),
				httptest.NewRequest(http.MethodGet, prefix+"/videos/"+arkVideoRouteTaskID, nil),
				httptest.NewRequest(http.MethodDelete, prefix+"/videos/"+arkVideoRouteTaskID, nil),
				httptest.NewRequest(http.MethodGet, prefix+"/videos/"+arkVideoRouteTaskID+"/content", nil),
				httptest.NewRequest(http.MethodPost, prefix+"/videos/edits", strings.NewReader(`{"prompt":"edit","video":{"id":"`+arkVideoRouteTaskID+`"}}`)),
				httptest.NewRequest(http.MethodPost, prefix+"/videos/extensions", strings.NewReader(`{"prompt":"extend","video":{"id":"`+arkVideoRouteTaskID+`"}}`)),
				httptest.NewRequest(http.MethodPost, prefix+"/videos/characters", strings.NewReader(`{"name":"x"}`)),
				httptest.NewRequest(http.MethodPost, prefix+"/videos/generations", strings.NewReader(`{"model":"grok-imagine-video","prompt":"waves"}`)),
			} {
				request.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				require.Equal(t, http.StatusNotFound, w.Code, "platform=%s %s %s", platform, request.Method, request.URL.Path)
				require.Contains(t, w.Body.String(), `"code":"use_contents_generations_tasks"`, "platform=%s %s %s", platform, request.Method, request.URL.Path)
				require.Contains(t, w.Body.String(), label+" serves video tasks at /api/v3/contents/generations/tasks", "platform=%s %s %s", platform, request.Method, request.URL.Path)
				require.NotContains(t, w.Body.String(), "video_disabled", "the request must never reach the OpenAI-shape handler")
			}
		}
	}

	// /v1/videos 的签名内容路由不挂分组门禁：它只认链接签名（这里零值 handler 回 video_disabled）。
	handlers := defaultGatewayRoutesTestHandlers(service.PlatformByteDance)
	handlers.Video = &handler.VideoHandler{}
	w := httptest.NewRecorder()
	newGatewayRoutesTestRouterForPlatformWithHandlers(service.PlatformByteDance, handlers).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/videos/"+arkVideoRouteTaskID+"/content/signed?exp=1&sig=00", nil))
	require.Contains(t, w.Body.String(), "video_disabled")
}

// 仅视频平台的非视频请求一律 404，消息指向 Ark 入口；/models、/balance、/usage 这几条只读
// 账务接口保持可用。
func TestVideoOnlyPlatformsRejectNonVideoProtocolsWithArkHint(t *testing.T) {
	for _, platform := range arkVideoPlatforms {
		router := newGatewayRoutesTestRouterForPlatform(platform)
		label := service.VideoOnlyPlatformLabel(platform)
		for _, request := range []*http.Request{
			httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[]}`)),
			httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt","input":"hello"}`)),
			httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt","messages":[]}`)),
			httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image","prompt":"x"}`)),
			httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"gpt","messages":[]}`)),
			httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`{"model":"gpt","input":"hello"}`)),
		} {
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, http.StatusNotFound, w.Code, "platform=%s path=%s", platform, request.URL.Path)
			require.Contains(t, w.Body.String(), label+" gateway supports only the video task API at /api/v3/contents/generations/tasks", "platform=%s path=%s", platform, request.URL.Path)
		}
		for _, path := range []string{"/v1/models", "/v1/balance", "/v1/usage"} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.NotContains(t, w.Body.String(), "supports only the video task API", "platform=%s path=%s", platform, path)
		}
	}
}

// Ark 创建路由的体上限独立于通用 MaxBodySize（content[] 里的 data: URI 图片内联在请求体里）。
// 用一个真实的 VideoHandler + 只有仓储桩的任务服务：请求体能完整读到并解析，才会走到 Submit
// 并因缺少计费依赖回 503；若通用上限先把请求体截断，解析层只会回 400 video_invalid_request。
func TestArkVideoCreateRouteHonoursTaskJSONBodyLimit(t *testing.T) {
	cfg := defaultGatewayRoutesTestConfig()
	cfg.Gateway.MaxBodySize = 1024
	cfg.Gateway.Video.TaskJSONBodyMaxBytes = 1 << 20
	cfg.Gateway.Video.Enabled = true
	cfg.Gateway.Video.CreationEnabled = true
	cfg.JWT.Secret = "ark-body-limit-test"
	taskService := service.NewVideoTaskService(&compositeReplayTaskRepo{task: &service.VideoTask{}}, nil, nil, nil, nil, nil, nil,
		service.NewCompositeRouteResolver(nil), service.NewVideoProviderRegistry(), nil, nil, nil, nil, cfg)
	newRouter := func(platform string) *gin.Engine {
		handlers := defaultGatewayRoutesTestHandlers(platform)
		handlers.Video = handler.NewVideoHandler(taskService, nil, cfg, nil, nil)
		return newGatewayRoutesTestRouterForPlatformWithConfigAndHandlers(platform, cfg, handlers)
	}
	post := func(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		return w
	}
	arkBody := func(size int) string {
		return `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"` + strings.Repeat("a", size) + `"}]}`
	}

	for _, prefix := range arkVideoRoutePrefixes {
		w := post(newRouter(service.PlatformByteDance), prefix+"/contents/generations/tasks", arkBody(2048))
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s: a 2KB Ark body must pass the Ark limit and reach Submit: %s", prefix, w.Body.String())
		require.NotContains(t, w.Body.String(), "video_invalid_request", prefix)
		require.NotContains(t, w.Body.String(), "video_input_too_large", prefix)
	}
	// 超过 Ark 上限的请求体在解析层就被拒绝，不会到达 Submit。
	w := post(newRouter(service.PlatformByteDance), "/api/v3/contents/generations/tasks", arkBody(1<<20))
	require.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusRequestEntityTooLarge, "status=%d body=%s", w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "billing_service_error")

	// 对照：/v1/videos 仍受通用上限约束，同样 2KB 的请求体被截断后只能回 400。
	w = post(newRouter(service.PlatformOpenAI), "/v1/videos", `{"model":"sora-2","prompt":"`+strings.Repeat("a", 2048)+`"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "video_invalid_request")
}

// 与 gateway_model_allowlist_test.go 同一套源码级约定：Ark 创建链必须用独立的 arkTaskBodyLimit，
// 且在 compositeTarget / byteDanceVideoOnly 之后、handler 之前挂 videoCreateIntent。
func TestArkVideoCreateRouteChainIsPinnedInSource(t *testing.T) {
	routeSource, err := os.ReadFile("gateway.go")
	require.NoError(t, err)
	source := string(routeSource)
	chain := regexp.MustCompile(regexp.QuoteMeta(`r.Handle(http.MethodPost, path, arkTaskBodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth),`) +
		`\s+` + regexp.QuoteMeta(`groupModelAllowlist, compositeTarget, byteDanceVideoOnly, videoCreateIntent, requireGroupAnthropic,`))
	require.Regexp(t, chain, source, "arkCreateRoute must use arkTaskBodyLimit and mount videoCreateIntent after the platform gates")
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`arkTaskBodyLimit = middleware.RequestBodyLimit(cfg.Gateway.Video.TaskJSONBodyMaxBytes)`)), source)
	// 读路由与目录复用 rootRoute（通用上限 + byteDanceVideoOnly），签名内容路由不挂 apiKeyAuth。
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`rootRoute(http.MethodGet, prefix+service.ArkVideoTasksPathSuffix+"/:task_id", bodyLimit,`)), source)
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`rootRoute(http.MethodDelete, prefix+service.ArkVideoTasksPathSuffix+"/:task_id", bodyLimit,`)), source)
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`rootRoute(http.MethodGet, prefix+service.ArkVideoModelsPathSuffix, bodyLimit,`)), source)
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`r.GET(prefix+service.ArkVideoTasksPathSuffix+"/:task_id/content", bodyLimit, clientRequestID, opsErrorLogger, arkSignedContentHandler)`)), source)
	require.Regexp(t, regexp.MustCompile(regexp.QuoteMeta(`r.HEAD(prefix+service.ArkVideoTasksPathSuffix+"/:task_id/content", bodyLimit, clientRequestID, opsErrorLogger, arkSignedContentHandler)`)), source)
}
