package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newWaveSpeedTestAccount(baseURL string) *Account {
	return &Account{
		ID: 11, Platform: PlatformWaveSpeed, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"api_key": "ws-secret-key", "base_url": baseURL},
	}
}

func newWaveSpeedTestProvider(t *testing.T, handler http.HandlerFunc) (*VideoVendorProvider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	provider := newVideoVendorProvider(newWaveSpeedVideoAdapter(), &runwareTestUpstream{client: server.Client()}, nil)
	return provider, server
}

func TestWaveSpeedAdapterRegisteredAsManagedPlatform(t *testing.T) {
	provider, ok := managedVideoProviderForPlatform(PlatformWaveSpeed)
	require.True(t, ok)
	require.Equal(t, VideoProviderWaveSpeed, provider)
	require.True(t, IsVideoOnlyPlatform(PlatformWaveSpeed))

	capabilities, ok := builtinVideoCapabilitiesForProvider(VideoProviderWaveSpeed)
	require.True(t, ok)
	require.NoError(t, validateVideoCapabilities(VideoProviderWaveSpeed, capabilities))
	require.Equal(t, waveSpeedDefaultVideoModel, capabilities.DefaultModel)
	require.True(t, capabilities.SupportedModels["bytedance/seedance-2.5/image-to-video"])
	require.Empty(t, capabilities.SupportedSizes)
	require.Contains(t, capabilities.SupportedResolutions["bytedance/seedance-2.0/text-to-video"], "4k")
	require.Contains(t, capabilities.SupportedRatios["bytedance/seedance-2.0/text-to-video"], "21:9")
	require.NotContains(t, capabilities.SupportedRatios["bytedance/seedance-2.0/text-to-video"], ArkVideoRatioAdaptive, "text-to-video has no image to follow")
	require.Contains(t, capabilities.SupportedRatios["bytedance/seedance-2.5/image-to-video"], ArkVideoRatioAdaptive)
	require.Equal(t, []int{4, 6, 8}, capabilities.SupportedSeconds["google/veo3.1/text-to-video"])
	require.False(t, capabilities.Supports(VideoCapabilityCancel))
	require.NoError(t, ValidateVideoCapabilityCatalog(DefaultVideoCapabilityCatalogDocument()))

	ids := videoVendorDefaultModelIDs(PlatformWaveSpeed)
	require.Len(t, ids, 30)
	require.Contains(t, ids, "bytedance/seedance-v1-pro-i2v-1080p")
}

func TestWaveSpeedRequestResolutionReadsModelIDWhenFixed(t *testing.T) {
	adapter := newWaveSpeedVideoAdapter()
	require.Equal(t, "1080p", videoVendorRequestResolution(adapter, VideoCreateRequest{Model: "bytedance/seedance-v1-pro-i2v-1080p"}))
	require.Equal(t, "720p", videoVendorRequestResolution(adapter, VideoCreateRequest{Model: "bytedance/seedance-2.0/text-to-video", Resolution: "720p"}))
	require.Equal(t, "4k", providerRequestResolution(VideoProviderWaveSpeed, VideoCreateRequest{Model: "bytedance/seedance-2.5/text-to-video", Resolution: "4k"}))
	require.Equal(t, "480p", providerRequestResolution(VideoProviderWaveSpeed, VideoCreateRequest{Model: "bytedance/seedance-2.0/text-to-video"}), "profile default (its first size) fills a missing resolution")
}

func TestWaveSpeedCreateTextToVideoEncodesAspectResolutionAndAudio(t *testing.T) {
	var captured map[string]any
	var path, authorization string
	provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &captured))
		_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"id":"pred_abc123","model":"bytedance/seedance-2.0/text-to-video","status":"created","urls":{"get":"https://api.wavespeed.ai/api/v3/predictions/pred_abc123/result"},"created_at":"2026-10-03T05:00:00.000Z"}}`))
	})
	account := newWaveSpeedTestAccount(server.URL + "/api/v3")
	audio := false
	request := VideoCreateRequest{
		ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate,
		Model: "bytedance/seedance-2.0/text-to-video", Prompt: "sunrise over the sea", Seconds: 8, Resolution: "720p", Ratio: "21:9", AudioEnabled: &audio,
		ProviderOptions: map[string]any{"enable_web_search": true},
	}
	task, err := provider.Create(context.Background(), account, request, nil)
	require.NoError(t, err)
	require.Equal(t, "/api/v3/bytedance/seedance-2.0/text-to-video", path)
	require.Equal(t, "Bearer ws-secret-key", authorization)
	require.Equal(t, "sunrise over the sea", captured["prompt"])
	require.Equal(t, float64(8), captured["duration"])
	require.Equal(t, "21:9", captured["aspect_ratio"])
	require.Equal(t, "720p", captured["resolution"])
	require.Equal(t, false, captured["generate_audio"])
	require.Equal(t, true, captured["enable_web_search"])
	_, hasImage := captured["image"]
	require.False(t, hasImage)

	require.Equal(t, "pred_abc123", task.ProviderTaskID)
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.Equal(t, "created", task.RawStatus)
	require.NotNil(t, task.ProviderCreatedAt)
	require.Equal(t, "bytedance/seedance-2.0/text-to-video", task.Metadata["model"])
	require.Equal(t, 8, task.Metadata["seconds"])
	require.Equal(t, "720p", task.Metadata["resolution"])
	require.Equal(t, "21:9", task.Metadata["ratio"])
}

func TestWaveSpeedCreateUploadsSpoolImageBeforeSubmit(t *testing.T) {
	var uploadContentType, uploadFilename, uploadBody string
	var submit map[string]any
	provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/media/upload/binary":
			uploadContentType = r.Header.Get("Content-Type")
			require.NoError(t, r.ParseMultipartForm(1<<20))
			file, header, err := r.FormFile("file")
			require.NoError(t, err)
			defer func() { _ = file.Close() }()
			payload, _ := io.ReadAll(file)
			uploadFilename = header.Filename
			uploadBody = string(payload)
			_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"type":"image","download_url":"https://media.wavespeed.ai/u/first.png","filename":"input.png","size":3}}`))
		case "/api/v3/bytedance/seedance-2.5/image-to-video":
			body, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(body, &submit))
			_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"id":"pred_i2v","status":"created"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	account := newWaveSpeedTestAccount(server.URL + "/api/v3")
	input := VideoInput{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, MIMEType: "image/png", Size: 3},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil },
	}
	request := VideoCreateRequest{
		ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate,
		Model: "bytedance/seedance-2.5/image-to-video", Prompt: "animate", Seconds: 5, Resolution: "1080p",
		Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, URL: "https://cdn.example.com/last.png"}},
	}
	task, err := provider.Create(context.Background(), account, request, []VideoInput{input})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(uploadContentType, "multipart/form-data"))
	require.Equal(t, "input.png", uploadFilename)
	require.Equal(t, "abc", uploadBody)
	require.Equal(t, "pred_i2v", task.ProviderTaskID)
	// spool 字节补首帧并先上传换 URL；content[] 里的 https 尾帧按角色落位。
	require.Equal(t, "https://media.wavespeed.ai/u/first.png", submit["image"])
	require.Equal(t, "https://cdn.example.com/last.png", submit["last_image"])
	require.Equal(t, "1080p", submit["resolution"])
	_, hasAspect := submit["aspect_ratio"]
	require.False(t, hasAspect, "image-to-video follows the image aspect ratio")
}

func TestWaveSpeedCreateRejectsEnvelopeAndHTTPErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		kind      string
		code      string
		certainty VideoSubmissionCertainty
		retryable bool
	}{
		{"moderation rejected in 200", http.StatusOK, `{"code":1200,"message":"sensitive content","data":null}`, "validation", "content_moderation_rejected", VideoSubmissionRejected, false},
		{"insufficient credits", http.StatusForbidden, `{"code":1407,"message":"insufficient_credits"}`, "upstream", "insufficient_balance", VideoSubmissionRejected, false},
		{"invalid parameter", http.StatusBadRequest, `{"code":1401,"message":"The parameter value is invalid."}`, "validation", "wavespeed_1401", VideoSubmissionRejected, false},
		{"pricing failed", http.StatusBadRequest, `{"code":4005,"message":"Pricing Failed"}`, "validation", "pricing_failed", VideoSubmissionRejected, false},
		{"rate limited", http.StatusTooManyRequests, `{"code":429,"message":"Rate limit exceeded"}`, "rate_limit", "wavespeed_429", VideoSubmissionUnknown, true},
		{"service unavailable in 200", http.StatusOK, `{"code":5003,"message":"Service Unavailable"}`, "upstream", "wavespeed_5003", VideoSubmissionUnknown, true},
		{"gateway error without body", http.StatusBadGateway, ``, "upstream", "http_502", VideoSubmissionUnknown, true},
		{"bad key", http.StatusUnauthorized, `{"code":401,"message":"Invalid API key"}`, "authentication", "wavespeed_401", VideoSubmissionRejected, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			account := newWaveSpeedTestAccount(server.URL + "/api/v3")
			request := VideoCreateRequest{ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}
			_, err := provider.Create(context.Background(), account, request, nil)
			require.Error(t, err)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.kind, providerErr.Kind)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, tc.certainty, providerErr.Certainty)
			require.Equal(t, tc.retryable, providerErr.Retryable)
			require.NotContains(t, providerErr.Message, "ws-secret-key")
		})
	}
}

func TestWaveSpeedUploadFailureIsRejectedNotUnknown(t *testing.T) {
	provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/media/upload/binary", r.URL.Path)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"code":413,"message":"file too large"}`))
	})
	account := newWaveSpeedTestAccount(server.URL + "/api/v3")
	input := VideoInput{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, MIMEType: "image/jpeg", Size: 3},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil },
	}
	request := VideoCreateRequest{ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/image-to-video", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}
	_, err := provider.Create(context.Background(), account, request, []VideoInput{input})
	require.Error(t, err)
	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, VideoSubmissionRejected, providerErr.Certainty, "a failed upload never creates an upstream task")
	require.Equal(t, "input_too_large", providerErr.Code)
}

func TestWaveSpeedGetMapsPredictionStatuses(t *testing.T) {
	ref := ProviderTaskRef{Provider: VideoProviderWaveSpeed, AccountID: 11, ProviderTaskID: "pred_abc123"}
	cases := []struct {
		name     string
		body     string
		status   string
		videoURL string
		errCode  string
	}{
		{"created", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"created","outputs":[]}}`, VideoGenerationQueued, "", ""},
		{"processing", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"processing","outputs":[]}}`, VideoGenerationInProgress, "", ""},
		{"completed", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"completed","outputs":["https://cdn.wavespeed.ai/out.mp4"],"timings":{"inference":25729}}}`, VideoGenerationCompleted, "https://cdn.wavespeed.ai/out.mp4", ""},
		{"failed with business code", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"failed","error":"Content Moderation","code":1200}}`, VideoGenerationFailed, "", "wavespeed_1200"},
		{"timeout", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"timeout","error":"Timeout"}}`, VideoGenerationFailed, "", "timeout"},
		{"cancelled", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"cancelled"}}`, VideoGenerationCancelled, "", ""},
		{"deleted", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"deleted"}}`, VideoGenerationFailed, "", "deleted"},
		{"completed without output", `{"code":200,"message":"success","data":{"id":"pred_abc123","status":"completed","outputs":[]}}`, VideoGenerationFailed, "", "missing_output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				require.Equal(t, http.MethodGet, r.Method)
				_, _ = w.Write([]byte(tc.body))
			})
			account := newWaveSpeedTestAccount(server.URL + "/api/v3")
			task, err := provider.Get(context.Background(), account, ref)
			require.NoError(t, err)
			require.Equal(t, "/api/v3/predictions/pred_abc123/result", path)
			require.Equal(t, tc.status, task.Status)
			require.Equal(t, tc.videoURL, task.VideoURL)
			require.Equal(t, tc.errCode, task.ErrorCode)
			require.Equal(t, waveSpeedDefaultPoll, task.SuggestedPollInterval)
			if tc.videoURL != "" {
				require.Equal(t, float64(25729), task.Usage["inference_ms"])
				require.NotNil(t, task.ContentExpiresAt)
				require.Equal(t, task.ProviderFinishedAt.Add(waveSpeedContentTTL), *task.ContentExpiresAt)
			}
		})
	}
}

func TestWaveSpeedGetRejectsIdentityMismatchAndEnvelopeErrors(t *testing.T) {
	ref := ProviderTaskRef{Provider: VideoProviderWaveSpeed, AccountID: 11, ProviderTaskID: "pred_abc123"}
	for _, body := range []string{
		`{"code":200,"message":"success","data":{"id":"pred_other","status":"completed","outputs":["https://cdn.wavespeed.ai/x.mp4"]}}`,
		`{"code":5000,"message":"Internal Error"}`,
		`{"code":200,"message":"success","data":{"id":"pred_abc123","status":"weird"}}`,
	} {
		provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		account := newWaveSpeedTestAccount(server.URL + "/api/v3")
		_, err := provider.Get(context.Background(), account, ref)
		require.Error(t, err)
		var providerErr *VideoProviderError
		require.ErrorAs(t, err, &providerErr)
		require.True(t, providerErr.Retryable, "control errors stay retryable so the worker keeps polling")
	}
}

func TestWaveSpeedDeleteSearchAndProbe(t *testing.T) {
	var method, path string
	var captured map[string]any
	provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"deleted_count":0,"deleted_ids":[],"balance":12.5}}`))
	})
	account := newWaveSpeedTestAccount(server.URL + "/api/v3")
	ref := ProviderTaskRef{Provider: VideoProviderWaveSpeed, AccountID: 11, ProviderTaskID: "pred_abc123"}

	require.NoError(t, provider.Delete(context.Background(), account, ref))
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/api/v3/predictions/delete", path)
	require.Equal(t, []any{"pred_abc123"}, captured["ids"])

	require.ErrorIs(t, provider.Cancel(context.Background(), account, ref), ErrVideoCapabilityUnsupported)

	observed, err := provider.SearchByClientToken(context.Background(), account, "video_0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Nil(t, observed, "wavespeed has no idempotency key, so a lost submission cannot be found")

	probe, ok := newWaveSpeedVideoAdapter().BuildProbe()
	require.True(t, ok)
	require.Equal(t, http.MethodGet, probe.Method)
	require.Equal(t, "/balance", probe.Path)
}

func TestWaveSpeedProfilesEncodeVendorSpecificFields(t *testing.T) {
	adapter := newWaveSpeedVideoAdapter()
	profiles := adapter.Profiles()

	// Kling：音频字段叫 sound，尺寸只传比例，图生视频不传比例。
	kling := profiles["kwaivgi/kling-v3.0-pro/text-to-video"]
	require.NotNil(t, kling)
	audio := true
	upstream, err := adapter.BuildCreate(VideoCreateRequest{Model: kling.PublicModel, Prompt: "p", Seconds: 10, Resolution: "1080p", Ratio: "9:16", AudioEnabled: &audio, ProviderOptions: map[string]any{"cfg_scale": 0.7, "shot_type": "intelligence"}}, kling, nil, "")
	require.NoError(t, err)
	var body map[string]any
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, "9:16", body["aspect_ratio"])
	require.Equal(t, true, body["sound"])
	require.Equal(t, 0.7, body["cfg_scale"])
	require.Equal(t, "intelligence", body["shot_type"])
	_, hasResolution := body["resolution"]
	require.False(t, hasResolution)

	// Seedance 1.0 Pro：分辨率固化在 ID，只传比例；图生视频什么尺寸字段都不传。
	v1 := profiles["bytedance/seedance-v1-pro-t2v-720p"]
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: v1.PublicModel, Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}, v1, nil, "")
	require.NoError(t, err)
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, "16:9", body["aspect_ratio"])
	_, hasResolution = body["resolution"]
	require.False(t, hasResolution)

	// MiniMax H3：720p 档位改写成厂商词表的 768p。
	h3 := profiles["wavespeed-ai/minimax-h3/image-to-video"]
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: h3.PublicModel, Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}, h3, []videoVendorInput{{URL: "https://media.wavespeed.ai/u/a.png"}}, "")
	require.NoError(t, err)
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, "768p", body["resolution"])
	require.Equal(t, "https://media.wavespeed.ai/u/a.png", body["image"])

	// 图生视频模型没有图片时被校验拒绝；文生视频模型带图也被拒绝。
	provider := newVideoVendorProvider(adapter, nil, nil)
	account := newWaveSpeedTestAccount("")
	err = provider.ValidateSubmission(account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.5/image-to-video", Prompt: "p", Seconds: 5}, nil)
	require.Error(t, err)
	firstFrame := []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://x/y.png"}}
	err = provider.ValidateSubmission(account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Content: firstFrame}, nil)
	require.Error(t, err)
	// 带图走兜底档案的未知模型可以提交。
	require.NoError(t, provider.ValidateSubmission(account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "vendor/new-model", Prompt: "p", Seconds: 5, Content: firstFrame}, nil))
}
