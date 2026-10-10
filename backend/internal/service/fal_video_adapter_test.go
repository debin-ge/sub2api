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

func newFalTestAccount(baseURL string) *Account {
	return &Account{
		ID: 13, Platform: PlatformFal, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"api_key": "fal-secret-key", "base_url": baseURL},
	}
}

func newFalTestProvider(t *testing.T, handler http.HandlerFunc) (*VideoVendorProvider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	provider := newVideoVendorProvider(newFalVideoAdapter(), &runwareTestUpstream{client: server.Client()}, nil)
	return provider, server
}

const falTestTaskID = "fal-ai/bytedance/requests/764cabcf-b745-4b3e-ae38-1200304cf45b"

func TestFalTaskIDEncodesOwnerAliasOnly(t *testing.T) {
	appID, ok := falAppID("fal-ai/bytedance/seedance/v1/lite/text-to-video")
	require.True(t, ok)
	require.Equal(t, "fal-ai/bytedance", appID)
	appID, ok = falAppID("bytedance/seedance-2.0/text-to-video")
	require.True(t, ok)
	require.Equal(t, "bytedance/seedance-2.0", appID)
	_, ok = falAppID("single")
	require.False(t, ok)

	taskID := falTaskID("fal-ai/bytedance", "764cabcf")
	require.Equal(t, "fal-ai/bytedance/requests/764cabcf", taskID)
	require.True(t, validVideoProviderIdentifier(taskID))
	app, requestID, ok := falSplitTaskID(taskID)
	require.True(t, ok)
	require.Equal(t, "fal-ai/bytedance", app)
	require.Equal(t, "764cabcf", requestID)
	for _, bad := range []string{"", "fal-ai/requests/x", "fal-ai/bytedance/requests/", "fal-ai/bytedance/requests/a/b", "no-requests-segment"} {
		_, _, ok := falSplitTaskID(bad)
		require.False(t, ok, bad)
	}
}

func TestFalAdapterRegisteredAsManagedPlatform(t *testing.T) {
	provider, ok := managedVideoProviderForPlatform(PlatformFal)
	require.True(t, ok)
	require.Equal(t, VideoProviderFal, provider)
	capabilities, ok := builtinVideoCapabilitiesForProvider(VideoProviderFal)
	require.True(t, ok)
	require.NoError(t, validateVideoCapabilities(VideoProviderFal, capabilities))
	require.True(t, capabilities.Supports(VideoCapabilityCancel))
	require.True(t, capabilities.SupportedModels["bytedance/seedance-2.0/text-to-video"])
	require.Equal(t, []int{4, 6, 8}, capabilities.SupportedSeconds["fal-ai/veo3.1"])
	require.NotContains(t, capabilities.SupportedSizes["bytedance/seedance-2.0/text-to-video"], "1920x1080", "seedance 2.0 on fal is 480p/720p only")
	require.NoError(t, ValidateVideoCapabilityCatalog(DefaultVideoCapabilityCatalogDocument()))
	require.Len(t, videoVendorDefaultModelIDs(PlatformFal), 22)
}

func TestFalCreateSubmitsToQueueAndEncodesTaskID(t *testing.T) {
	var captured map[string]any
	var path, authorization, lifecycle string
	provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		lifecycle = r.Header.Get(falLifecycleHeader)
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &captured))
		_, _ = w.Write([]byte(`{"request_id":"764cabcf-b745-4b3e-ae38-1200304cf45b","response_url":"https://queue.fal.run/fal-ai/bytedance/requests/764cabcf-b745-4b3e-ae38-1200304cf45b","status_url":"https://queue.fal.run/fal-ai/bytedance/requests/764cabcf-b745-4b3e-ae38-1200304cf45b/status","cancel_url":"https://queue.fal.run/fal-ai/bytedance/requests/764cabcf-b745-4b3e-ae38-1200304cf45b/cancel","queue_position":3}`))
	})
	account := newFalTestAccount(server.URL)
	audio, cameraFixed := true, true
	seed := int64(7)
	request := VideoCreateRequest{
		ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate,
		Model: "fal-ai/bytedance/seedance/v1.5/pro/text-to-video", Prompt: "a fox in snow", Seconds: 6, Resolution: "1080p", Ratio: "9:16",
		AudioEnabled: &audio, CameraFixed: &cameraFixed, Seed: &seed,
	}
	task, err := provider.Create(context.Background(), account, request, nil)
	require.NoError(t, err)
	require.Equal(t, "/fal-ai/bytedance/seedance/v1.5/pro/text-to-video", path, "submission uses the full endpoint id")
	require.Equal(t, "Key fal-secret-key", authorization)
	require.Equal(t, falObjectLifecycleValue, lifecycle)
	require.Equal(t, "a fox in snow", captured["prompt"])
	require.Equal(t, "6", captured["duration"], "seedance durations are string-typed")
	require.Equal(t, "9:16", captured["aspect_ratio"])
	require.Equal(t, "1080p", captured["resolution"])
	require.Equal(t, true, captured["generate_audio"])
	require.Equal(t, true, captured["camera_fixed"])
	require.Equal(t, float64(7), captured["seed"])

	require.Equal(t, falTestTaskID, task.ProviderTaskID, "task id keeps only owner/alias plus the request id")
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.Equal(t, 6, task.Metadata["seconds"])
}

func TestFalCreateEncodesVendorSpecificFields(t *testing.T) {
	adapter := newFalVideoAdapter()
	profiles := adapter.Profiles()

	// Veo：时长带 s 后缀。
	veo := profiles["fal-ai/veo3.1"]
	upstream, err := adapter.BuildCreate(VideoCreateRequest{Model: veo.PublicModel, Prompt: "p", Seconds: 8, Resolution: "4k", Ratio: "16:9"}, veo, nil, "")
	require.NoError(t, err)
	var body map[string]any
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, "8s", body["duration"])
	require.Equal(t, "16:9", body["aspect_ratio"])
	require.Equal(t, "4k", body["resolution"])

	// Kling 图生视频：start_image_url / end_image_url，不传比例，data URI 直接内联。
	kling := profiles["fal-ai/kling-video/v3/pro/image-to-video"]
	inputs := []videoVendorInput{{MIMEType: "image/png", Data: []byte("abc")}, {URL: "https://cdn.example.com/last.png"}}
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: kling.PublicModel, Prompt: "p", Seconds: 5, Resolution: "1080p", Ratio: "16:9"}, kling, inputs, "")
	require.NoError(t, err)
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, "data:image/png;base64,YWJj", body["start_image_url"])
	require.Equal(t, "https://cdn.example.com/last.png", body["end_image_url"])
	require.Equal(t, "5", body["duration"])
	_, hasAspect := body["aspect_ratio"]
	require.False(t, hasAspect)
	_, hasResolution := body["resolution"]
	require.False(t, hasResolution)

	// Sora：时长是整数，只接受 4 / 8 / 12 / 16 / 20。
	sora := profiles["fal-ai/sora-2/text-to-video/pro"]
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: sora.PublicModel, Prompt: "p", Seconds: 12, Resolution: "1080p", Ratio: "16:9"}, sora, nil, "")
	require.NoError(t, err)
	body = nil
	require.NoError(t, json.Unmarshal(upstream.Body, &body))
	require.Equal(t, float64(12), body["duration"])
	require.Equal(t, "/fal-ai/sora-2/text-to-video/pro", upstream.Path)
	require.False(t, sora.supportsSeconds(10))

	// Seedance 2.0 在 fal 只有 480p / 720p。
	provider := newVideoVendorProvider(adapter, nil, nil)
	account := newFalTestAccount("")
	err = provider.ValidateSubmission(account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Resolution: "1080p", Ratio: "16:9"}, nil)
	require.Error(t, err)
	require.NoError(t, provider.ValidateSubmission(account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "21:9"}, nil))
}

func TestFalCreateClassifiesErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		kind      string
		code      string
		certainty VideoSubmissionCertainty
		retryable bool
	}{
		{"pydantic validation", http.StatusUnprocessableEntity, `{"detail":[{"loc":["body","duration"],"msg":"value is not a valid enumeration member","type":"type_error.enum"}]}`, "validation", "invalid_request", VideoSubmissionRejected, false},
		{"content policy", http.StatusUnprocessableEntity, `{"detail":"Your request was rejected by the content policy","error_type":"content_policy_violation"}`, "validation", "content_moderation_rejected", VideoSubmissionRejected, false},
		{"unauthorized", http.StatusUnauthorized, `{"detail":"Invalid key"}`, "authentication", "unauthorized", VideoSubmissionRejected, false},
		{"forbidden balance", http.StatusForbidden, `{"detail":"Insufficient balance","error_type":"balance_exhausted"}`, "upstream", "insufficient_balance", VideoSubmissionRejected, false},
		{"rate limited", http.StatusTooManyRequests, `{"detail":"Too many requests"}`, "rate_limit", "rate_limited", VideoSubmissionUnknown, true},
		{"server error", http.StatusServiceUnavailable, `{"detail":"Runner unavailable","error_type":"runner_unavailable"}`, "upstream", "runner_unavailable", VideoSubmissionUnknown, true},
		{"empty body", http.StatusBadGateway, ``, "upstream", "http_502", VideoSubmissionUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			account := newFalTestAccount(server.URL)
			request := VideoCreateRequest{ClientToken: "video_0123456789abcdef0123456789abcdef", Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}
			_, err := provider.Create(context.Background(), account, request, nil)
			require.Error(t, err)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.kind, providerErr.Kind)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, tc.certainty, providerErr.Certainty)
			require.Equal(t, tc.retryable, providerErr.Retryable)
			require.NotContains(t, providerErr.Message, "fal-secret-key")
		})
	}
}

func TestFalGetPollsStatusThenResult(t *testing.T) {
	ref := ProviderTaskRef{Provider: VideoProviderFal, AccountID: 13, ProviderTaskID: falTestTaskID}
	statusPath := "/" + falTestTaskID + "/status"
	resultPath := "/" + falTestTaskID

	t.Run("queued", func(t *testing.T) {
		var paths []string
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
			_, _ = w.Write([]byte(`{"status":"IN_QUEUE","queue_position":2}`))
		})
		task, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.NoError(t, err)
		require.Equal(t, []string{statusPath + "?logs=0"}, paths)
		require.Equal(t, VideoGenerationQueued, task.Status)
		require.Equal(t, falDefaultPoll, task.SuggestedPollInterval)
	})

	t.Run("in progress", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"IN_PROGRESS","logs":[]}`))
		})
		task, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status)
	})

	t.Run("completed fetches result and billable units", func(t *testing.T) {
		var paths []string
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			switch r.URL.Path {
			case statusPath:
				_, _ = w.Write([]byte(`{"status":"COMPLETED","logs":[],"metrics":{"inference_time":42.5}}`))
			case resultPath:
				w.Header().Set(falBillableUnitsHeader, "6")
				_, _ = w.Write([]byte(`{"video":{"url":"https://v3.fal.media/files/out.mp4","content_type":"video/mp4"},"seed":1094575694}`))
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		})
		task, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.NoError(t, err)
		require.Equal(t, []string{statusPath, resultPath}, paths)
		require.Equal(t, VideoGenerationCompleted, task.Status)
		require.Equal(t, "https://v3.fal.media/files/out.mp4", task.VideoURL)
		require.Equal(t, float64(6), task.Usage["billable_units"])
		require.NotNil(t, task.ContentExpiresAt)
		require.Equal(t, task.ProviderFinishedAt.Add(falContentTTL), *task.ContentExpiresAt)
	})

	t.Run("completed with error is terminal without fetching result", func(t *testing.T) {
		var paths []string
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			_, _ = w.Write([]byte(`{"status":"COMPLETED","error":"Invalid status code: 422","error_type":"content_policy_violation"}`))
		})
		task, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.NoError(t, err)
		require.Equal(t, []string{statusPath}, paths)
		require.Equal(t, VideoGenerationFailed, task.Status)
		require.Equal(t, "content_policy_violation", task.ErrorCode)
		require.Contains(t, task.ErrorMessage, "422")
	})

	t.Run("result without video url fails", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/status") {
				_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
				return
			}
			_, _ = w.Write([]byte(`{"images":[{"url":"https://v3.fal.media/x.png"}]}`))
		})
		task, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationFailed, task.Status)
		require.Equal(t, "missing_output", task.ErrorCode)
	})

	t.Run("result endpoint 422 is a terminal poll failure", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/status") {
				_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
				return
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":"generation failed"}`))
		})
		_, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.Error(t, err)
		var providerErr *VideoProviderError
		require.ErrorAs(t, err, &providerErr)
		require.False(t, providerErr.Retryable)
		require.Equal(t, "validation", providerErr.Kind)
	})

	t.Run("unknown status keeps polling", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"MYSTERY"}`))
		})
		_, err := provider.Get(context.Background(), newFalTestAccount(server.URL), ref)
		require.Error(t, err)
		var providerErr *VideoProviderError
		require.ErrorAs(t, err, &providerErr)
		require.True(t, providerErr.Retryable)
	})
}

func TestFalCancelDeleteSearchAndProbe(t *testing.T) {
	ref := ProviderTaskRef{Provider: VideoProviderFal, AccountID: 13, ProviderTaskID: falTestTaskID}
	cancelPath := "/" + falTestTaskID + "/cancel"

	t.Run("cancel requested", func(t *testing.T) {
		var method, path string
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"CANCELLATION_REQUESTED"}`))
		})
		require.NoError(t, provider.Cancel(context.Background(), newFalTestAccount(server.URL), ref))
		require.Equal(t, http.MethodPut, method)
		require.Equal(t, cancelPath, path)
	})

	t.Run("delete tolerates already completed", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":"ALREADY_COMPLETED"}`))
		})
		require.NoError(t, provider.Delete(context.Background(), newFalTestAccount(server.URL), ref))
	})

	t.Run("delete tolerates not found", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"NOT_FOUND"}`))
		})
		require.NoError(t, provider.Delete(context.Background(), newFalTestAccount(server.URL), ref))
	})

	t.Run("other 400 is an error", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"bad request"}`))
		})
		require.Error(t, provider.Delete(context.Background(), newFalTestAccount(server.URL), ref))
	})

	t.Run("search unsupported and probe shape", func(t *testing.T) {
		provider, server := newFalTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("search must not call upstream: %s", r.URL.Path)
		})
		observed, err := provider.SearchByClientToken(context.Background(), newFalTestAccount(server.URL), "video_0123456789abcdef0123456789abcdef")
		require.NoError(t, err)
		require.Nil(t, observed)
		probe, ok := newFalVideoAdapter().BuildProbe()
		require.True(t, ok)
		require.Equal(t, http.MethodGet, probe.Method)
		require.Equal(t, "/fal-ai/kling-video/requests/"+falProbeRequestID+"/status", probe.Path)
	})
}

func TestFalErrorMessageFlattensShapes(t *testing.T) {
	require.Equal(t, "", falErrorMessage(nil))
	require.Equal(t, "plain", falErrorMessage(" plain "))
	require.Equal(t, "a; b", falErrorMessage([]any{map[string]any{"msg": "a"}, map[string]any{"message": "b"}, ""}))
	require.Equal(t, "nested", falErrorMessage(map[string]any{"detail": "nested"}))
}
