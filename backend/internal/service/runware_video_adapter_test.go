package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// runwareTestUpstream 把 HTTPUpstream 直接接到 httptest 服务器，绕过代理与 TLS 指纹。
type runwareTestUpstream struct {
	client *http.Client
	calls  atomic.Int32
}

func (u *runwareTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	return u.client.Do(req)
}

func (u *runwareTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func newRunwareTestAccount(baseURL string) *Account {
	return &Account{
		ID: 7, Platform: PlatformRunware, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"api_key": "rw-secret-key", "base_url": baseURL},
	}
}

func newRunwareTestProvider(t *testing.T, handler http.HandlerFunc) (*VideoVendorProvider, *httptest.Server, *runwareTestUpstream) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	upstream := &runwareTestUpstream{client: server.Client()}
	provider := newVideoVendorProvider(newRunwareVideoAdapter(), upstream, nil)
	return provider, server, upstream
}

func TestRunwareTaskUUIDRoundTripsLocalToken(t *testing.T) {
	local := NewVideoTaskID()
	uuid := runwareTaskUUID(local)
	require.Len(t, uuid, 36)
	require.Equal(t, strings.TrimPrefix(local, "video_"), strings.ReplaceAll(uuid, "-", ""))
	require.Equal(t, uuid, runwareTaskUUID(local), "derivation must be deterministic")
	require.Equal(t, uuid, runwareTaskUUID(uuid), "a formatted uuid passes through unchanged")

	derived := runwareTaskUUID("not-a-video-token")
	require.Len(t, derived, 36)
	require.Equal(t, "4", derived[14:15], "fallback derivation keeps the v4 version nibble")
	require.Equal(t, derived, runwareTaskUUID("not-a-video-token"))
}

func TestRunwareAdapterRegisteredAsManagedPlatform(t *testing.T) {
	provider, ok := managedVideoProviderForPlatform(PlatformRunware)
	require.True(t, ok)
	require.Equal(t, VideoProviderRunware, provider)
	require.Equal(t, PlatformRunware, videoPricingPlatformForProvider(VideoProviderRunware))
	require.True(t, IsManagedVideoPlatform(PlatformRunware))
	require.True(t, IsVideoOnlyPlatform(PlatformRunware))
	require.False(t, IsVideoOnlyPlatform(PlatformOpenAI))
	require.Contains(t, ManagedVideoProviderNames(), VideoProviderRunware)

	capabilities, ok := builtinVideoCapabilitiesForProvider(VideoProviderRunware)
	require.True(t, ok)
	require.NoError(t, validateVideoCapabilities(VideoProviderRunware, capabilities))
	require.True(t, capabilities.SupportedModels["bytedance:seedance@2.0"])
	require.True(t, capabilities.SupportedModels["bytedance:seedance@2.5"])
	require.Equal(t, 5, capabilities.DefaultSeconds["bytedance:seedance@2.0"])
	// 画幅按 Ark 维度声明：分辨率档位 + 比例，没有 WxH。
	require.Empty(t, capabilities.SupportedSizes)
	require.Equal(t, []string{"480p", "720p", "1080p", "4k"}, capabilities.SupportedResolutions["bytedance:seedance@2.0"])
	require.Equal(t, "720p", capabilities.DefaultResolutions["bytedance:seedance@2.0"])
	require.Equal(t, []string{"16:9", "4:3", "1:1", "3:4", "9:16", "21:9", ArkVideoRatioAdaptive}, capabilities.SupportedRatios["bytedance:seedance@2.0"])
	require.Equal(t, "16:9", capabilities.DefaultRatios["bytedance:seedance@2.0"])
	require.Equal(t, map[VideoInputRole]int{VideoInputRoleFirstFrame: 1, VideoInputRoleLastFrame: 1}, capabilities.MaxContentByRole)

	document := DefaultVideoCapabilityCatalogDocument()
	require.NoError(t, ValidateVideoCapabilityCatalog(document))
	require.Contains(t, document.Providers, VideoProviderRunware)
}

func TestVideoVendorResolutionLabel(t *testing.T) {
	require.Equal(t, "1080p", videoVendorResolutionLabel("1920x1080"))
	require.Equal(t, "1080p", videoVendorResolutionLabel("1080x1920"))
	require.Equal(t, "720p", videoVendorResolutionLabel("1470x630"))
	require.Equal(t, "480p", videoVendorResolutionLabel("992x432"))
	require.Equal(t, "4k", videoVendorResolutionLabel("3840x2160"))
	require.Equal(t, "4k", videoVendorResolutionLabel("4398x1886"))
	require.Equal(t, "1080p", videoVendorResolutionLabel("2206x946"))
	require.Equal(t, "1080p", videoVendorResolutionLabel("2176x928"))
	require.Equal(t, "480p", videoVendorResolutionLabel("960x416"))
	require.Equal(t, "720p", videoVendorResolutionLabel("1344x768"))
	require.Equal(t, "", videoVendorResolutionLabel(""))

	adapter := newRunwareVideoAdapter()
	require.Equal(t, "1080p", videoVendorRequestResolution(adapter, VideoCreateRequest{Model: "bytedance:seedance@2.0", Resolution: "1080p"}))
	require.Equal(t, "1080p", providerRequestResolution(VideoProviderRunware, VideoCreateRequest{Model: "bytedance:seedance@2.0", Resolution: "1080p"}))
	// 没带分辨率时回落到档案默认档位（Seedance 2.0 默认 1280x720）。
	require.Equal(t, "720p", providerRequestResolution(VideoProviderRunware, VideoCreateRequest{Model: "bytedance:seedance@2.0"}))
}

func TestRunwareValidateSubmissionRejectsUnsupportedShapes(t *testing.T) {
	provider := newVideoVendorProvider(newRunwareVideoAdapter(), nil, nil)
	account := newRunwareTestAccount("")
	base := VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance:seedance@2.0", Prompt: "a cat", Seconds: 5, Resolution: "720p", Ratio: "16:9"}

	require.NoError(t, provider.ValidateSubmission(account, base, nil))

	cases := []struct {
		name   string
		mutate func(*VideoCreateRequest)
		code   string
	}{
		{"seconds outside range", func(r *VideoCreateRequest) { r.Seconds = 40 }, "unsupported_seconds"},
		{"resolution outside table", func(r *VideoCreateRequest) { r.Resolution = "2k" }, "unsupported_resolution"},
		{"ratio outside table", func(r *VideoCreateRequest) { r.Ratio = "3:2" }, "unsupported_ratio"},
		{"adaptive ratio without image", func(r *VideoCreateRequest) { r.Ratio = ArkVideoRatioAdaptive }, "unsupported_ratio"},
		{"openai size", func(r *VideoCreateRequest) { r.Size = "1280x720" }, "unsupported_option"},
		{"smart duration", func(r *VideoCreateRequest) { r.Seconds, r.SmartDuration = 0, true }, "unsupported_parameter"},
		{"frames", func(r *VideoCreateRequest) { r.Seconds, r.Frames = 0, 29 }, "unsupported_parameter"},
		{"return last frame", func(r *VideoCreateRequest) { flag := true; r.ReturnLastFrame = &flag }, "unsupported_parameter"},
		{"watermark unmapped", func(r *VideoCreateRequest) { flag := true; r.Watermark = &flag }, "unsupported_parameter"},
		{"unknown option", func(r *VideoCreateRequest) { r.ProviderOptions = map[string]any{"steps": 20} }, "unsupported_option"},
		{"non scalar option", func(r *VideoCreateRequest) { r.ProviderOptions = map[string]any{"seed": []any{1}} }, "unsupported_option"},
		{"file id reference", func(r *VideoCreateRequest) { r.InputReference = &ProviderInputReference{FileID: "file_1"} }, "unsupported_option"},
		{"reference videos", func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: "https://x/y.mp4"}}
		}, "unsupported_reference_media"},
		{"http image url", func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "http://x/y.png"}}
		}, "unsupported_reference_media"},
		{"last frame alone", func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, URL: "https://x/y.png"}}
		}, "unsupported_reference_media"},
		{"edit operation", func(r *VideoCreateRequest) { r.Operation = VideoOperationEdit }, "unsupported_operation"},
		{"empty prompt", func(r *VideoCreateRequest) { r.Prompt = " " }, "missing_prompt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.mutate(&request)
			err := provider.ValidateSubmission(account, request, nil)
			require.Error(t, err)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, VideoSubmissionRejected, providerErr.Certainty)
		})
	}

	// 音频：Seedance 1.0 Pro 没有音频字段，audio=true 必须被拒；audio=false 可以忽略。
	enabled, disabled := true, false
	seedance10 := base
	seedance10.Model = "bytedance:2@1"
	seedance10.Resolution = "1080p"
	seedance10.AudioEnabled = &enabled
	err := provider.ValidateSubmission(account, seedance10, nil)
	require.Error(t, err)
	seedance10.AudioEnabled = &disabled
	require.NoError(t, provider.ValidateSubmission(account, seedance10, nil))

	// 未知模型走兜底档案：只要在 Ark 词表内就不校验档位与秒数集合，仍校验白名单。
	fallback := base
	fallback.Model = "unknown:model@0"
	fallback.Resolution = "2k"
	fallback.Ratio = "21:9"
	fallback.Seconds = 7
	require.NoError(t, provider.ValidateSubmission(account, fallback, nil))

	// 非 runware 账号直接拒绝。
	other := newRunwareTestAccount("")
	other.Platform = PlatformByteDance
	require.Error(t, provider.ValidateSubmission(other, base, nil))
}

func TestRunwareCreateEncodesRequestAndDecodesAcceptance(t *testing.T) {
	var captured []map[string]any
	var authorization string
	provider, server, _ := newRunwareTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &captured))
		require.Equal(t, "/v1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"taskType":"videoInference","taskUUID":"` + captured[0]["taskUUID"].(string) + `"}]}`))
	})
	account := newRunwareTestAccount(server.URL + "/v1")
	audio := true
	seed := int64(42)
	request := VideoCreateRequest{
		TaskID: "video_task", ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate,
		Model: "bytedance:seedance@2.0", Prompt: "a cat on the moon", Seconds: 6, Resolution: "1080p", Ratio: "16:9", AudioEnabled: &audio,
		Seed: &seed, ProviderOptions: map[string]any{"negativePrompt": "blurry"},
	}
	task, err := provider.Create(context.Background(), account, request, nil)
	require.NoError(t, err)
	require.Equal(t, "Bearer rw-secret-key", authorization)
	require.Len(t, captured, 1)
	body := captured[0]
	require.Equal(t, "videoInference", body["taskType"])
	require.Equal(t, "01234567-89ab-cdef-0123-456789abcdef", body["taskUUID"])
	require.Equal(t, "bytedance:seedance@2.0", body["model"])
	require.Equal(t, "a cat on the moon", body["positivePrompt"])
	require.Equal(t, float64(6), body["duration"])
	require.Equal(t, float64(1920), body["width"])
	require.Equal(t, float64(1080), body["height"])
	require.Equal(t, "async", body["deliveryMethod"])
	require.Equal(t, true, body["includeCost"])
	require.Equal(t, float64(42), body["seed"])
	require.Equal(t, "blurry", body["negativePrompt"])
	require.Equal(t, map[string]any{"audio": true}, body["settings"])
	_, hasInputs := body["inputs"]
	require.False(t, hasInputs)

	require.Equal(t, "01234567-89ab-cdef-0123-456789abcdef", task.ProviderTaskID)
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.Equal(t, "bytedance:seedance@2.0", task.Metadata["model"])
	require.Equal(t, 6, task.Metadata["seconds"])
	require.Equal(t, "1080p", task.Metadata["resolution"])
	require.Equal(t, "16:9", task.Metadata["ratio"])
	require.Equal(t, int64(42), task.Metadata["seed"])
}

func TestRunwareCreateInlinesImageAndSendsResolution(t *testing.T) {
	var captured []map[string]any
	provider, server, _ := newRunwareTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &captured))
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	account := newRunwareTestAccount(server.URL + "/v1")
	input := VideoInput{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, MIMEType: "image/png", Size: 3},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil },
	}
	request := VideoCreateRequest{
		ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate,
		Model: "bytedance:seedance@2.0", Prompt: "animate", Seconds: 5, Resolution: "720p", Ratio: "16:9",
	}
	task, err := provider.Create(context.Background(), account, request, []VideoInput{input})
	require.NoError(t, err)
	require.Equal(t, VideoGenerationQueued, task.Status)
	body := captured[0]
	inputs := body["inputs"].(map[string]any)
	frames := inputs["frameImages"].([]any)
	require.Len(t, frames, 1)
	require.Equal(t, "data:image/png;base64,YWJj", frames[0])
	require.Equal(t, "720p", body["resolution"])
	_, hasWidth := body["width"]
	require.False(t, hasWidth, "frame images must not be combined with width/height")

	// 两张图：spool 字节补首帧，content[] 里的 https 尾帧按角色落位。
	captured = nil
	request.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, URL: "https://cdn.example.com/last.png"}}
	_, err = provider.Create(context.Background(), account, request, []VideoInput{input})
	require.NoError(t, err)
	frames = captured[0]["inputs"].(map[string]any)["frameImages"].([]any)
	require.Len(t, frames, 2)
	require.Equal(t, map[string]any{"image": "data:image/png;base64,YWJj", "frame": "first"}, frames[0])
	require.Equal(t, map[string]any{"image": "https://cdn.example.com/last.png", "frame": "last"}, frames[1])
}

func TestRunwareCreateClassifiesErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		kind      string
		code      string
		certainty VideoSubmissionCertainty
		retryable bool
	}{
		{"insufficient balance", http.StatusPaymentRequired, `{"errors":[{"code":"insufficientCredits","message":"top up"}]}`, "upstream", "insufficient_balance", VideoSubmissionRejected, false},
		{"invalid key", http.StatusUnauthorized, `{"errors":[{"code":"invalidApiKey","message":"bad key"}]}`, "authentication", "invalidApiKey", VideoSubmissionRejected, false},
		{"rate limited", http.StatusTooManyRequests, `{"errors":[{"code":"providerRateLimitExceeded","message":"slow down"}]}`, "rate_limit", "providerRateLimitExceeded", VideoSubmissionUnknown, true},
		{"server error without body", http.StatusBadGateway, ``, "upstream", "http_502", VideoSubmissionUnknown, true},
		{"schema rejection in 200", http.StatusOK, `{"errors":[{"code":"invalidParameter","message":"duration out of range","taskUUID":"x"}]}`, "validation", "invalidParameter", VideoSubmissionRejected, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, server, _ := newRunwareTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			account := newRunwareTestAccount(server.URL + "/v1")
			request := VideoCreateRequest{ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate, Model: "bytedance:seedance@2.0", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}
			_, err := provider.Create(context.Background(), account, request, nil)
			require.Error(t, err)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.kind, providerErr.Kind)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, tc.certainty, providerErr.Certainty)
			require.Equal(t, tc.retryable, providerErr.Retryable)
			require.NotContains(t, providerErr.Message, "rw-secret-key")
		})
	}
}

func TestRunwareGetMapsStatuses(t *testing.T) {
	taskID := "01234567-89ab-cdef-0123-456789abcdef"
	ref := ProviderTaskRef{Provider: VideoProviderRunware, AccountID: 7, ProviderTaskID: taskID}
	cases := []struct {
		name     string
		body     string
		status   string
		videoURL string
		progress float64
		cost     float64
		errCode  string
	}{
		{"queued when nothing reported", `{"data":[]}`, VideoGenerationQueued, "", 0, 0, ""},
		{"processing with progress", `{"data":[{"taskType":"videoInference","taskUUID":"` + taskID + `","status":"processing","progress":47}]}`, VideoGenerationInProgress, "", 0.47, 0, ""},
		{"success with cost", `{"data":[{"taskType":"videoInference","taskUUID":"` + taskID + `","status":"success","videoURL":"https://vm.runware.ai/video/x.mp4","cost":0.18}]}`, VideoGenerationCompleted, "https://vm.runware.ai/video/x.mp4", 0, 0.18, ""},
		{"error entry is terminal", `{"data":[],"errors":[{"code":"timeoutProvider","status":"error","message":"provider timed out","taskUUID":"` + taskID + `"}]}`, VideoGenerationFailed, "", 0, 0, "timeoutProvider"},
		{"other task ignored", `{"data":[{"taskUUID":"ffffffff-ffff-4fff-8fff-ffffffffffff","status":"success","videoURL":"https://vm.runware.ai/other.mp4"}]}`, VideoGenerationQueued, "", 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured []map[string]any
			provider, server, _ := newRunwareTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				require.NoError(t, json.Unmarshal(body, &captured))
				_, _ = w.Write([]byte(tc.body))
			})
			account := newRunwareTestAccount(server.URL + "/v1")
			task, err := provider.Get(context.Background(), account, ref)
			require.NoError(t, err)
			require.Equal(t, "getResponse", captured[0]["taskType"])
			require.Equal(t, taskID, captured[0]["taskUUID"])
			require.Equal(t, tc.status, task.Status)
			require.Equal(t, tc.videoURL, task.VideoURL)
			require.Equal(t, runwareDefaultPoll, task.SuggestedPollInterval)
			if tc.progress > 0 {
				require.NotNil(t, task.Progress)
				require.InDelta(t, tc.progress, *task.Progress, 1e-9)
			}
			if tc.cost > 0 {
				require.Equal(t, tc.cost, task.Usage["upstream_cost_usd"])
				require.NotNil(t, task.ContentExpiresAt)
				require.NotNil(t, task.ProviderFinishedAt)
				require.Equal(t, task.ProviderFinishedAt.Add(runwareContentTTL), *task.ContentExpiresAt)
				require.Equal(t, []string{"video"}, task.ContentVariants)
			}
			require.Equal(t, tc.errCode, task.ErrorCode)
		})
	}
}

func TestRunwareSearchAndTerminalOperations(t *testing.T) {
	provider, server, upstream := newRunwareTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"code":"unknownTask","message":"no such task","taskUUID":"01234567-89ab-cdef-0123-456789abcdef"}]}`))
	})
	account := newRunwareTestAccount(server.URL + "/v1")

	observed, err := provider.SearchByClientToken(context.Background(), account, "video_0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Nil(t, observed, "an unknown task must keep the reconciliation window open")

	ref := ProviderTaskRef{Provider: VideoProviderRunware, AccountID: 7, ProviderTaskID: "01234567-89ab-cdef-0123-456789abcdef"}
	before := upstream.calls.Load()
	require.NoError(t, provider.Delete(context.Background(), account, ref), "runware has no delete endpoint; local delete must succeed")
	require.ErrorIs(t, provider.Cancel(context.Background(), account, ref), ErrVideoCapabilityUnsupported)
	require.Equal(t, before, upstream.calls.Load(), "delete and cancel must not call upstream")
	require.False(t, provider.Capabilities().Supports(VideoCapabilityCancel))
}

func TestRunwareArkModelSpec(t *testing.T) {
	provider := newVideoVendorProvider(newRunwareVideoAdapter(), nil, nil)
	spec := provider.ArkModelSpec("bytedance:seedance@2.0")
	// seed 从 provider_options 白名单挪进 Ark 显式字段；negativePrompt 仍是厂商参数。
	require.Equal(t, ArkVideoModelFlags{Seed: true, GenerateAudio: true}, spec.Flags)
	require.Equal(t, []ArkVideoModelOption{{Name: "negativePrompt"}}, spec.ProviderOptions)
	require.False(t, spec.FramesSupported)
	require.Equal(t, ArkVideoContentRole{Max: 1, Accepts: []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI}}, spec.ContentRoles[VideoInputRoleFirstFrame])
	require.Equal(t, []string{string(VideoInputRoleFirstFrame)}, spec.ContentRoles[VideoInputRoleLastFrame].Requires)

	// Seedance 1.0 Pro 没有音频字段，但有 cameraFixed。
	seedance10 := provider.ArkModelSpec("bytedance:2@1")
	require.Equal(t, ArkVideoModelFlags{Seed: true, CameraFixed: true}, seedance10.Flags)

	fallback := provider.ArkModelSpec("unknown-model")
	require.Len(t, fallback.ContentRoles, 2, "the fallback profile allows first and last frame images")
}

func TestBuildVendorURLJoinsBaseAndPath(t *testing.T) {
	require.Equal(t, "https://api.runware.ai/v1", buildVendorURL("https://api.runware.ai/v1", "", nil))
	require.Equal(t, "https://api.runware.ai/v1", buildVendorURL("https://api.runware.ai/v1/", "", nil))
	require.Equal(t, "https://api.wavespeed.ai/api/v3/predictions/x/result", buildVendorURL("https://api.wavespeed.ai/api/v3", "/predictions/x/result", nil))
	require.Equal(t, "https://api.wavespeed.ai/api/v3/models", buildVendorURL("https://api.wavespeed.ai/api/v3/", "models", nil))
	require.Equal(t, "https://queue.fal.run/fal-ai/kling-video/requests/abc/status?logs=1", buildVendorURL("https://queue.fal.run", "fal-ai/kling-video/requests/abc/status", map[string][]string{"logs": {"1"}}))
}
