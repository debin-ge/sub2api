package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type videoCreateMiddlewareAPI struct {
	videoTaskAPIFake
	calls       int
	body        []byte
	contentType string
}

func (api *videoCreateMiddlewareAPI) BeginVideoCreateIntent(_ context.Context, _ *service.APIKey, _, _, contentType string, body []byte) (*service.VideoCreateIntentSession, error) {
	api.calls++
	api.body, api.contentType = body, contentType
	return nil, service.ErrBillingServiceUnavailable
}

func TestVideoCreateIntentMiddlewareMediaAdmission(t *testing.T) {
	for _, scenario := range []struct {
		name, mediaType, encoding, key, platform string
		limit                                    int64
		calls, status                            int
	}{
		{"legacy", "text/plain", "", "", service.PlatformGrok, 0, 0, 200},
		{"native_streaming", "multipart/form-data; boundary=test", "", "key", service.PlatformOpenAI, 0, 0, 200},
		{"composite_native_streaming", "multipart/form-data; boundary=test", "", "key", service.PlatformComposite, 0, 0, 200},
		{"grok_multipart_bypasses_native_intent", "multipart/form-data; boundary=test", "", "key", service.PlatformGrok, 0, 0, 200},
		{"grok_json_bypasses_native_intent", "application/json", "", "key", service.PlatformGrok, 0, 0, 200},
		{"grok_media_type_is_not_reinterpreted", "text/plain", "", "key", service.PlatformGrok, 0, 0, 200},
		{"grok_encoding_is_not_reinterpreted", "application/json", "gzip", "key", service.PlatformGrok, 0, 0, 200},
		{"native_json_uses_intent", "application/json", "", "key", service.PlatformOpenAI, 0, 1, 503},
		{"native_encoded_json_rejected", "application/json", "gzip", "key", service.PlatformOpenAI, 0, 0, 400},
		{"bytedance_json_uses_intent", "application/json", "", "key", service.PlatformByteDance, 0, 1, 503},
		{"bytedance_encoded_json_rejected", "application/json", "gzip", "key", service.PlatformByteDance, 0, 0, 400},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			api := &videoCreateMiddlewareAPI{}
			body := `{"model":"alias"}`
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/videos", scenario.mediaType, strings.NewReader(body))
			key, _ := middleware.GetAPIKeyFromContext(ctx)
			key.Group = &service.Group{ID: 8, Platform: scenario.platform}
			ctx.Request.Header.Set("Idempotency-Key", scenario.key)
			ctx.Request.Header.Set("Content-Encoding", scenario.encoding)
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: scenario.limit}}
			newVideoHandler(api, nil, cfg).CreateIntentMiddleware(ctx)
			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			require.Equal(t, scenario.calls, api.calls)
			if api.calls > 0 {
				require.Equal(t, body, string(api.body))
				require.Equal(t, scenario.mediaType, api.contentType)
			}
		})
	}
}

// Ark 任务路径（四组前缀）对五家平台与 composite 同样走共享创建意图：带 Idempotency-Key 的
// JSON 创建先经意图仲裁；multipart 不是 Ark 传输形态，意图层直接放行给 handler 去拒绝。
func TestVideoCreateIntentMiddlewareCoversArkTaskPaths(t *testing.T) {
	body := `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}]}`
	for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
		for _, scenario := range []struct {
			name, mediaType, encoding, key, platform string
			calls, status                            int
		}{
			{"bytedance_json_uses_intent", "application/json", "", "key", service.PlatformByteDance, 1, 503},
			{"fal_json_uses_intent", "application/json", "", "key", service.PlatformFal, 1, 503},
			{"wavespeed_json_uses_intent", "application/json", "", "key", service.PlatformWaveSpeed, 1, 503},
			{"runware_json_uses_intent", "application/json", "", "key", service.PlatformRunware, 1, 503},
			{"flova_json_uses_intent", "application/json", "", "key", service.PlatformFlova, 1, 503},
			{"composite_json_uses_intent", "application/json", "", "key", service.PlatformComposite, 1, 503},
			{"without_idempotency_key_bypasses_intent", "application/json", "", "", service.PlatformByteDance, 0, 200},
			{"multipart_bypasses_intent", "multipart/form-data; boundary=test", "", "key", service.PlatformByteDance, 0, 200},
			{"encoded_json_rejected", "application/json", "gzip", "key", service.PlatformFlova, 0, 400},
			{"grok_bypasses_intent", "application/json", "", "key", service.PlatformGrok, 0, 200},
		} {
			t.Run(prefix+"/"+scenario.name, func(t *testing.T) {
				api := &videoCreateMiddlewareAPI{}
				ctx, recorder := newVideoHandlerTestContext(http.MethodPost, prefix+"/contents/generations/tasks", scenario.mediaType, strings.NewReader(body))
				key, _ := middleware.GetAPIKeyFromContext(ctx)
				key.Group = &service.Group{ID: 8, Platform: scenario.platform}
				ctx.Request.Header.Set("Idempotency-Key", scenario.key)
				ctx.Request.Header.Set("Content-Encoding", scenario.encoding)
				newVideoHandler(api, nil, &config.Config{}).CreateIntentMiddleware(ctx)
				require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
				require.Equal(t, scenario.calls, api.calls)
				if api.calls > 0 {
					require.Equal(t, body, string(api.body))
					require.Equal(t, scenario.mediaType, api.contentType)
				}
			})
		}
	}

	t.Run("ark body limit is the task json limit", func(t *testing.T) {
		cfg := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{TaskJSONBodyMaxBytes: 2 << 20}}}
		padded := `{"model":"m","content":[{"type":"text","text":"` + strings.Repeat("a", int(videoJSONBodyMaxBytes)) + `"}]}`
		require.Greater(t, len(padded), int(videoJSONBodyMaxBytes), "the body must exceed the /v1/videos JSON limit")

		api := &videoCreateMiddlewareAPI{}
		ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json", strings.NewReader(padded))
		key, _ := middleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformByteDance}
		ctx.Request.Header.Set("Idempotency-Key", "key")
		newVideoHandler(api, nil, cfg).CreateIntentMiddleware(ctx)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, "the Ark limit admits the body: %s", recorder.Body.String())
		require.Equal(t, 1, api.calls)

		api = &videoCreateMiddlewareAPI{}
		ctx, recorder = newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(padded))
		key, _ = middleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformOpenAI}
		ctx.Request.Header.Set("Idempotency-Key", "key")
		newVideoHandler(api, nil, cfg).CreateIntentMiddleware(ctx)
		require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, "/v1/videos keeps the 1MB JSON limit: %s", recorder.Body.String())
		require.Zero(t, api.calls)

		oversized := `{"model":"m","content":[{"type":"text","text":"` + strings.Repeat("a", 2<<20) + `"}]}`
		api = &videoCreateMiddlewareAPI{}
		ctx, recorder = newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json", strings.NewReader(oversized))
		key, _ = middleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformByteDance}
		ctx.Request.Header.Set("Idempotency-Key", "key")
		newVideoHandler(api, nil, cfg).CreateIntentMiddleware(ctx)
		require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
		require.Zero(t, api.calls)
	})
}

func TestVideoCreateIntentMiddlewareLeavesResolvedLegacyRouteUntouched(t *testing.T) {
	api := &videoCreateMiddlewareAPI{}
	body := `{"model":"legacy-model","prompt":"test"}`
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(body))
	key, _ := middleware.GetAPIKeyFromContext(ctx)
	key.Group = &service.Group{ID: 8, Platform: service.PlatformComposite}
	ctx.Request.Header.Set("Idempotency-Key", "legacy-client-key")
	ctx.Request = ctx.Request.WithContext(service.WithResolvedTargetPlatform(ctx.Request.Context(), service.PlatformGrok))
	newVideoHandler(api, nil, nil).CreateIntentMiddleware(ctx)
	require.Zero(t, api.calls)
	require.Equal(t, http.StatusOK, recorder.Code)
	_, guarded := service.VideoCreateSessionFromContext(ctx.Request.Context())
	require.False(t, guarded)
}

type videoCreateResponseRepository struct {
	videoMultipartIntentRepositoryFake
	readErr     error
	releases    int
	quarantines int
}

func (repo *videoCreateResponseRepository) ReadVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) (*service.VideoCreateIntent, error) {
	return &repo.intent, repo.readErr
}

func (repo *videoCreateResponseRepository) ReleasePreparedVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) error {
	repo.releases++
	return nil
}

func (repo *videoCreateResponseRepository) QuarantineUntrackedVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) error {
	repo.quarantines++
	return nil
}

func TestVideoCreateResponseWaitsForFinishBeforePublishing(t *testing.T) {
	for _, scenario := range []struct {
		name, state            string
		status, expectedStatus int
		readErr                error
		panics, overflow       bool
		releases, quarantines  int
	}{
		{name: "native", state: service.VideoCreateIntentNative, status: 201, expectedStatus: 201},
		{name: "rejected", state: service.VideoCreateIntentPrepared, status: 400, expectedStatus: 400, releases: 1},
		{name: "untracked_success", state: service.VideoCreateIntentPrepared, status: 201, expectedStatus: 409, quarantines: 1},
		{name: "finish_error", state: service.VideoCreateIntentNative, status: 201, expectedStatus: 503, readErr: service.ErrBillingServiceUnavailable},
		{name: "panic", state: service.VideoCreateIntentPrepared, status: 201, panics: true, releases: 1},
		{name: "overflow", state: service.VideoCreateIntentNative, status: 201, expectedStatus: 500, overflow: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{}`))
			repo := &videoCreateResponseRepository{readErr: scenario.readErr}
			repo.intent = service.VideoCreateIntent{ID: 91, State: scenario.state}
			session := service.NewVideoCreateIntentSession(repo, &service.VideoCreateIntentClaim{Intent: &repo.intent, Owned: true})
			handler := newVideoHandler(&videoTaskAPIFake{}, nil, nil)
			execute := func() {
				handler.executeVideoCreateIntentSession(ctx, session, func() {
					ctx.Header("X-Test-Buffered", "yes")
					ctx.Status(scenario.status)
					body := "buffered-body"
					if scenario.overflow {
						body = strings.Repeat("x", videoCreateResponseMaxBytes+1)
					}
					_, err := ctx.Writer.WriteString(body)
					if scenario.overflow {
						require.ErrorIs(t, err, errVideoCreateResponseTooLarge)
					} else {
						require.NoError(t, err)
					}
					require.Empty(t, recorder.Body.String(), "response must wait for the finish check")
					if scenario.panics {
						panic("test panic")
					}
				}, func() { t.Fatal("owned requests must not replay") })
			}
			if scenario.panics {
				require.PanicsWithValue(t, "test panic", execute)
				require.Empty(t, recorder.Body.String())
			} else {
				execute()
				require.Equal(t, scenario.expectedStatus, recorder.Code, recorder.Body.String())
				if scenario.name == "native" || scenario.name == "rejected" {
					require.Equal(t, "buffered-body", recorder.Body.String())
					require.Equal(t, "yes", recorder.Header().Get("X-Test-Buffered"))
				} else {
					require.NotContains(t, recorder.Body.String(), "buffered-body")
					require.Empty(t, recorder.Header().Get("X-Test-Buffered"))
				}
			}
			require.Equal(t, scenario.releases, repo.releases)
			require.Equal(t, scenario.quarantines, repo.quarantines)
		})
	}
}
