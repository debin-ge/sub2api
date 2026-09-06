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
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			body := []byte(` { "model": "video-alias", "prompt": "test prompt", "seconds": 8 } `)
			fake := &videoIngressAPIFake{route: &service.VideoIngressRoute{Decision: service.CompositeRouteDecision{
				Matched: true, GroupID: 8, TargetPlatform: platform, PublicModel: "video-alias", UpstreamModel: "native-model", Endpoint: service.CompositeRouteEndpointVideos,
			}}}
			ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", bytes.NewReader(body))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
			key.Status = service.StatusActive
			key.User = &service.User{ID: key.UserID, Status: service.StatusActive, Balance: 100}
			ctx.Request.Header.Set("Idempotency-Key", "same-intent")
			handler := newVideoHandler(fake, nil, nil)
			handler.PrepareCompositeVideoRoute(ctx)
			require.False(t, ctx.IsAborted())
			actual, err := io.ReadAll(ctx.Request.Body)
			require.NoError(t, err)
			if platform == service.PlatformOpenAI {
				require.Equal(t, body, actual)
			} else {
				require.Equal(t, "native-model", gjson.GetBytes(actual, "model").String())
			}
			require.Equal(t, "video-alias", fake.model)
			require.Equal(t, "same-intent", fake.key)
			decision, ok := service.CompositeRouteDecisionFromContext(ctx.Request.Context())
			require.True(t, ok)
			require.Equal(t, "video-alias", decision.PublicModel)
			require.Equal(t, platform, decision.TargetPlatform)
			require.Zero(t, fake.submitCalls)
		})
	}
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

func TestVideoCompositeReplayAndSourceRoutesPinEditingToOpenAI(t *testing.T) {
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
