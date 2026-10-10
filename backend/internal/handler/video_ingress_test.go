package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videoIngressAPIFake struct {
	videoTaskAPIFake
	route                         *service.VideoIngressRoute
	err                           error
	calls                         int
	model, source, key, operation string
}

type videoGrokMediaHandlerFake struct {
	operation   string
	body        []byte
	contentType string
}

func (fake *videoGrokMediaHandlerFake) handle(c *gin.Context, operation string) {
	fake.operation = operation
	fake.contentType = c.GetHeader("Content-Type")
	fake.body, _ = io.ReadAll(c.Request.Body)
	c.JSON(http.StatusAccepted, gin.H{"id": "grok-result"})
}

func (fake *videoGrokMediaHandlerFake) GrokVideoGeneration(c *gin.Context) {
	fake.handle(c, service.VideoOperationGenerate)
}

func (fake *videoGrokMediaHandlerFake) GrokVideoEdit(c *gin.Context) {
	fake.handle(c, service.VideoOperationEdit)
}

func (fake *videoGrokMediaHandlerFake) GrokVideoExtension(c *gin.Context) {
	fake.handle(c, service.VideoOperationExtend)
}

func (fake *videoIngressAPIFake) ResolveVideoIngress(_ context.Context, _ *service.APIKey, operation, model, source, key string) (*service.VideoIngressRoute, error) {
	fake.calls++
	fake.operation, fake.model, fake.source, fake.key = operation, model, source, key
	return fake.route, fake.err
}

func TestVideoCompositeIngressPreservesManagedBodyButRewritesLegacyGrokBody(t *testing.T) {
	// 两套公开形态各走各的路径：OpenAI / Grok 从 /v1/videos 进，五家 Ark 形态平台从
	// /contents/generations/tasks 进；托管平台的请求体原样保留，Grok 的 model 被改写。
	for _, scenario := range []struct{ platform, path string }{
		{service.PlatformOpenAI, "/v1/videos"},
		{service.PlatformByteDance, "/api/v3/contents/generations/tasks"},
		{service.PlatformFlova, "/v3/contents/generations/tasks"},
		{service.PlatformFal, "/v1/contents/generations/tasks"},
		{service.PlatformWaveSpeed, "/contents/generations/tasks"},
		{service.PlatformRunware, "/api/v3/contents/generations/tasks"},
		{service.PlatformGrok, "/v1/videos"},
	} {
		t.Run(scenario.platform, func(t *testing.T) {
			body := []byte(` { "model": "video-alias", "prompt": "test prompt", "seconds": 8 } `)
			fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{Decision: service.CompositeRouteDecision{
				Matched: true, GroupID: 8, TargetPlatform: scenario.platform, PublicModel: "video-alias", UpstreamModel: "native-model", Endpoint: service.CompositeRouteEndpointVideos,
			}}}
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, scenario.path, "application/json", bytes.NewReader(body))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
			key.Status = service.StatusActive
			key.User = &service.User{ID: key.UserID, Status: service.StatusActive, Balance: 100}
			ctx.Request.Header.Set("Idempotency-Key", "same-intent")
			handler := newVideoHandler(fake, nil, nil)
			handler.PrepareCompositeVideoRoute(ctx)
			require.False(t, ctx.IsAborted(), recorder.Body.String())
			actual, err := io.ReadAll(ctx.Request.Body)
			require.NoError(t, err)
			if scenario.platform == service.PlatformGrok {
				require.Equal(t, "native-model", gjson.GetBytes(actual, "model").String())
			} else {
				require.Equal(t, body, actual)
			}
			require.Equal(t, service.VideoOperationGenerate, fake.operation)
			require.Equal(t, "video-alias", fake.model)
			require.Equal(t, "same-intent", fake.key)
			decision, ok := service.CompositeRouteDecisionFromContext(ctx.Request.Context())
			require.True(t, ok)
			require.Equal(t, "video-alias", decision.PublicModel)
			require.Equal(t, scenario.platform, decision.TargetPlatform)
			require.Zero(t, fake.submitCalls)
		})
	}
}

// 路由决策落到的平台与请求路径的协议形态不符时直接拒绝：五家 Ark 形态平台从 /v1/videos
// 进来是 404 use_contents_generations_tasks，OpenAI / Grok 从 Ark 路径进来是 403。
func TestVideoCompositeIngressRejectsShapeMismatchBetweenPathAndTargetPlatform(t *testing.T) {
	for _, scenario := range []struct {
		name, platform, path string
		status               int
		code                 string
	}{
		{"bytedance via /v1/videos", service.PlatformByteDance, "/v1/videos", http.StatusNotFound, "use_contents_generations_tasks"},
		{"fal via /videos", service.PlatformFal, "/videos", http.StatusNotFound, "use_contents_generations_tasks"},
		{"wavespeed via /v1/videos/edits", service.PlatformWaveSpeed, "/v1/videos/edits", http.StatusNotFound, "use_contents_generations_tasks"},
		{"runware via /v1/videos/extensions", service.PlatformRunware, "/v1/videos/extensions", http.StatusNotFound, "use_contents_generations_tasks"},
		{"flova via /v1/videos", service.PlatformFlova, "/v1/videos", http.StatusNotFound, "use_contents_generations_tasks"},
		{"openai via ark path", service.PlatformOpenAI, "/api/v3/contents/generations/tasks", http.StatusForbidden, "video_ark_platform_unsupported"},
		{"grok via ark path", service.PlatformGrok, "/v1/contents/generations/tasks", http.StatusForbidden, "video_ark_platform_unsupported"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{Decision: service.CompositeRouteDecision{
				Matched: true, GroupID: 8, TargetPlatform: scenario.platform, PublicModel: "video-alias", UpstreamModel: "native-model", Endpoint: service.CompositeRouteEndpointVideos,
			}}}
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, scenario.path, "application/json", strings.NewReader(`{"model":"video-alias","prompt":"test"}`))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
			key.Status = service.StatusActive
			key.User = &service.User{ID: key.UserID, Status: service.StatusActive, Balance: 100}
			newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
			require.True(t, ctx.IsAborted())
			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), scenario.code)
			require.Equal(t, 1, fake.calls, "the route decision is what tells us the target platform")
			_, resolved := service.CompositeRouteDecisionFromContext(ctx.Request.Context())
			require.False(t, resolved, "a rejected request must not carry a route decision downstream")
			require.Zero(t, fake.submitCalls)
		})
	}
}

func TestValidateVideoRoutePlatform(t *testing.T) {
	arkPaths := []string{"/api/v3/contents/generations/tasks", "/v3/contents/generations/tasks", "/v1/contents/generations/tasks", "/contents/generations/tasks", "/api/v3/contents/generations/tasks/"}
	openAIPaths := []string{"/v1/videos", "/videos", "/v1/videos/edits", "/videos/extensions", "/v1/videos/characters"}
	arkPlatforms := []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova}
	openAIPlatforms := []string{service.PlatformOpenAI, service.PlatformGrok}
	for _, path := range arkPaths {
		for _, platform := range arkPlatforms {
			require.NoError(t, validateVideoRoutePlatform(path, platform), "%s -> %s", path, platform)
		}
		for _, platform := range openAIPlatforms {
			require.ErrorIs(t, validateVideoRoutePlatform(path, platform), service.ErrVideoArkPlatformUnsupported, "%s -> %s", path, platform)
		}
	}
	for _, path := range openAIPaths {
		for _, platform := range openAIPlatforms {
			require.NoError(t, validateVideoRoutePlatform(path, platform), "%s -> %s", path, platform)
		}
		for _, platform := range arkPlatforms {
			require.ErrorIs(t, validateVideoRoutePlatform(path, platform), service.ErrVideoUseArkEndpoint, "%s -> %s", path, platform)
		}
	}
	// 能力目录不是任务路径：它不参与创建请求的形态校验。
	require.NoError(t, validateVideoRoutePlatform("/api/v3/contents/generations/models", service.PlatformOpenAI))
	require.ErrorIs(t, validateVideoRoutePlatform("/api/v3/contents/generations/models", service.PlatformByteDance), service.ErrVideoUseArkEndpoint)
}

func TestVideoCompositeMultipartRoutesParsedRequestToGrok(t *testing.T) {
	fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{Decision: service.CompositeRouteDecision{
		Matched: true, GroupID: 8, TargetPlatform: service.PlatformGrok, PublicModel: "video-alias", UpstreamModel: "grok-upstream", Endpoint: service.CompositeRouteEndpointVideos,
	}}}
	grok := &videoGrokMediaHandlerFake{}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	handler.grok = grok
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "video-alias"))
	require.NoError(t, writer.WriteField("prompt", "test prompt"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)
	key, ok := middleware.GetAPIKeyFromContext(ctx)
	require.True(t, ok)
	key.Status = service.StatusActive
	key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
	key.User = &service.User{ID: key.UserID, Status: service.StatusActive, Balance: 100}

	handler.Create(ctx)

	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, fake.calls)
	require.Zero(t, fake.submitCalls)
	require.Equal(t, service.VideoOperationGenerate, grok.operation)
	require.Equal(t, "grok-upstream", service.ParseGrokMediaRequest(grok.contentType, grok.body).Model)
}

func TestVideoCompositeReplayAndSourceRoutesPinManagedProvider(t *testing.T) {
	t.Run("openai edits replay and source routes", func(t *testing.T) {
		for _, sourceRoute := range []bool{false, true} {
			fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{ManagedReplay: !sourceRoute, ResolveAfterParsing: sourceRoute,
				Decision: service.CompositeRouteDecision{TargetPlatform: service.PlatformOpenAI}}}
			body := `{"prompt":"test","video":{"id":"video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/videos/edits", "application/json", strings.NewReader(body))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
			newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
			require.False(t, ctx.IsAborted(), recorder.Body.String())
			require.Equal(t, http.StatusOK, recorder.Code)
			platform, ok := service.ResolvedTargetPlatformFromContext(ctx.Request.Context())
			require.True(t, ok)
			require.Equal(t, service.PlatformOpenAI, platform)
			_, resolved := service.CompositeRouteDecisionFromContext(ctx.Request.Context())
			require.False(t, resolved)
			require.Equal(t, "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", fake.source)
			require.Equal(t, service.VideoOperationEdit, fake.operation)
		}
	})
	t.Run("ark replay pins the five-platform provider", func(t *testing.T) {
		for _, platform := range []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova} {
			fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{ManagedReplay: true, Decision: service.CompositeRouteDecision{TargetPlatform: platform}}}
			body := `{"model":"video-alias","content":[{"type":"text","text":"waves"}]}`
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json", strings.NewReader(body))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
			ctx.Request.Header.Set("Idempotency-Key", "ark-replay")
			newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
			require.False(t, ctx.IsAborted(), recorder.Body.String())
			resolved, ok := service.ResolvedTargetPlatformFromContext(ctx.Request.Context())
			require.True(t, ok)
			require.Equal(t, platform, resolved)
			require.Equal(t, service.VideoOperationGenerate, fake.operation, "the Ark path only creates generation tasks")
			require.Equal(t, "video-alias", fake.model)
			require.Equal(t, "ark-replay", fake.key)
			actual, err := io.ReadAll(ctx.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(actual))
		}
	})
	t.Run("source routes to an ark provider cannot come from /videos/edits", func(t *testing.T) {
		fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{ResolveAfterParsing: true, Decision: service.CompositeRouteDecision{TargetPlatform: service.PlatformByteDance}}}
		ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/videos/edits", "application/json", strings.NewReader(`{"prompt":"test","video":{"id":"video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
		key, _ := middleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
		newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
		require.True(t, ctx.IsAborted())
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), "use_contents_generations_tasks")
		_, ok := service.ResolvedTargetPlatformFromContext(ctx.Request.Context())
		require.False(t, ok)
	})
}

func TestVideoCompositeIngressRejectsInvalidBodiesBeforeRouting(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"model":1}`, `{"model":"one","model":"two"}`, strings.Repeat("x", int(videoJSONBodyMaxBytes)+1)} {
		fake := &videoIngressAPIFake{}
		ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(body))
		key, _ := middleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
		newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
		require.True(t, ctx.IsAborted())
		require.GreaterOrEqual(t, recorder.Code, 400)
		require.Zero(t, fake.calls)
	}
}

func TestVideoCompositeIngressDoesNotTurnTextIntoJSON(t *testing.T) {
	fake := &videoIngressAPIFake{}
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "text/plain", strings.NewReader(`{"model":"alias"}`))
	key, _ := middleware.GetAPIKeyFromContext(ctx)
	key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
	newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, ctx.IsAborted())
	require.Zero(t, fake.calls)
}

func TestVideoCompositeIngressFailsClosedWhenReplayLookupFails(t *testing.T) {
	fake := &videoIngressAPIFake{err: errors.New("lookup unavailable")}
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{"model":"video-alias"}`))
	key, _ := middleware.GetAPIKeyFromContext(ctx)
	key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
	newVideoHandler(fake, nil, nil).PrepareCompositeVideoRoute(ctx)
	require.True(t, ctx.IsAborted())
	require.GreaterOrEqual(t, recorder.Code, 500)
	require.Zero(t, fake.submitCalls)
}
