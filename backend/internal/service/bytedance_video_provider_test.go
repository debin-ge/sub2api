package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func byteDanceCompatibleVideoTestAccount() *Account {
	account := byteDanceVideoTestAccount()
	account.Credentials["protocol_mode"] = byteDanceProtocolOpenAICompat
	account.Credentials["base_url"] = "https://relay.example.com/v1"
	return account
}

func TestByteDanceVideoProviderIdentityAndAccountIsolation(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{}, nil)
	require.Equal(t, VideoProviderByteDance, provider.Name())
	require.True(t, provider.SupportsAccount(byteDanceVideoTestAccount()))
	require.True(t, provider.SupportsAccount(byteDanceCompatibleVideoTestAccount()))
	require.False(t, provider.SupportsAccount(openAIVideoTestAccount()))
	require.False(t, provider.SupportsAccount(&Account{Platform: PlatformByteDance, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "secret"}}))
	require.False(t, provider.SupportsAccount(&Account{Platform: PlatformByteDance, Type: AccountTypeAPIKey}))

	// An unrecognised protocol must fail account support outright. Falling back
	// to a default protocol would speak the wrong wire format to the upstream.
	unknown := byteDanceVideoTestAccount()
	unknown.Credentials["protocol_mode"] = "grpc"
	require.False(t, provider.SupportsAccount(unknown))

	// A relay account without its own host has nowhere valid to send an
	// OpenAI-shaped body: Ark's default base only speaks the native protocol.
	hostless := byteDanceVideoTestAccount()
	hostless.Credentials["protocol_mode"] = byteDanceProtocolOpenAICompat
	require.False(t, provider.SupportsAccount(hostless))

	registry := NewVideoProviderRegistry(NewOpenAIVideoProvider(&openAIVideoHTTPStub{}, nil), provider)
	resolved, ok := registry.Get(VideoProviderByteDance)
	require.True(t, ok)
	require.Same(t, provider, resolved)
}

func TestByteDanceVideoProviderNativeCreateUsesArkTaskProtocol(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, accountID int64, concurrency int) (*http.Response, error) {
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks", req.URL.String())
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "Bearer ark-video-secret", req.Header.Get("Authorization"))
		require.Equal(t, int64(21), accountID)
		require.Equal(t, 3, concurrency)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, ByteDanceVideoModelSeedance10Pro, payload["model"])
		// Ark takes the generation parameters as top-level request fields.
		require.Equal(t, "720p", payload["resolution"])
		require.Equal(t, "16:9", payload["ratio"])
		require.EqualValues(t, 10, payload["duration"])
		require.NotContains(t, payload, "size")
		require.NotContains(t, payload, "seconds")
		require.NotContains(t, payload, "prompt")

		content, ok := payload["content"].([]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		first, ok := content[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "text", first["type"])
		require.Equal(t, "A tracking shot", first["text"])

		// A create answers with the identifier alone.
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"cgt-20260907-abcdef"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), byteDanceVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
		Prompt: "A tracking shot", Seconds: 10, Resolution: "720p", Ratio: "16:9",
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "cgt-20260907-abcdef", task.ProviderTaskID)
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.Equal(t, "720p", task.Metadata["resolution"])
	require.Equal(t, "16:9", task.Metadata["ratio"])
	require.Equal(t, 10, task.Metadata["seconds"])
}

// A private Ark-compatible deployment overrides the base URL. Unlike the OpenAI
// joiner, the native builder carries no "/v1" semantics.
func TestByteDanceVideoProviderNativeHonoursCustomBaseURL(t *testing.T) {
	account := byteDanceVideoTestAccount()
	account.Credentials["base_url"] = "https://ark.internal.example/api/v3"
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "https://ark.internal.example/api/v3/contents/generations/tasks", req.URL.String())
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"cgt-private-1"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), account, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
		Prompt: "A sports car", Seconds: 5,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "cgt-private-1", task.ProviderTaskID)
}

func TestByteDanceVideoProviderNativeGetMapsStatusAndContentExpiry(t *testing.T) {
	finished := time.Unix(1757239200, 0).UTC()
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/cgt-20260907-abcdef", req.URL.String())
		return openAIVideoResponseForTest(http.StatusOK, `{
			"id":"cgt-20260907-abcdef","model":"doubao-seedance-1-0-pro-250528",
			"status":"succeeded","created_at":1757239200,"updated_at":`+
			`1757239200,"content":{"video_url":"https://tos.example.com/v.mp4?sig=x"},
			"usage":{"completion_tokens":1234}
		}`, nil), nil
	}}, nil)

	account := byteDanceVideoTestAccount()
	ref := ProviderTaskRef{Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "cgt-20260907-abcdef"}
	task, err := provider.Get(context.Background(), account, ref)

	require.NoError(t, err)
	require.Equal(t, VideoGenerationCompleted, task.Status)
	require.Equal(t, "succeeded", task.RawStatus)
	require.Equal(t, "https://tos.example.com/v.mp4?sig=x", task.VideoURL)
	require.Equal(t, []string{"video"}, task.ContentVariants)
	require.NotNil(t, task.ContentExpiresAt)
	require.Equal(t, finished.Add(byteDanceContentURLTTL), task.ContentExpiresAt.UTC())
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, task.Metadata["model"])
}

func TestNormalizeByteDanceVideoStatus(t *testing.T) {
	for raw, want := range map[string]string{
		"queued":    VideoGenerationQueued,
		"running":   VideoGenerationInProgress,
		"succeeded": VideoGenerationCompleted,
		"failed":    VideoGenerationFailed,
		"cancelled": VideoGenerationCancelled,
		"canceled":  VideoGenerationCancelled,
		"SUCCEEDED": VideoGenerationCompleted,
		"weird":     "",
		"":          "",
	} {
		require.Equal(t, want, normalizeByteDanceVideoStatus(raw), raw)
	}
}

// A task id echoed back by the upstream must match the one that was polled, or
// a mismatched response could settle the wrong task.
func TestByteDanceVideoProviderNativeGetRejectsIdentityMismatch(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"cgt-someone-else","status":"succeeded"}`, nil), nil
	}}, nil)
	account := byteDanceVideoTestAccount()
	_, err := provider.Get(context.Background(), account, ProviderTaskRef{
		Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "cgt-20260907-abcdef",
	})
	require.Error(t, err)
}

func TestByteDanceVideoProviderNativeCancelUsesTaskEndpoint(t *testing.T) {
	calls := 0
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodDelete, req.Method)
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/cgt-1", req.URL.String())
		return openAIVideoResponseForTest(http.StatusOK, `{}`, nil), nil
	}}, nil)
	account := byteDanceVideoTestAccount()
	ref := ProviderTaskRef{Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "cgt-1"}

	require.NoError(t, provider.Cancel(context.Background(), account, ref))
	require.NoError(t, provider.Delete(context.Background(), account, ref))
	require.Equal(t, 2, calls)
}

func TestByteDanceVideoProviderRejectsInvalidRequestsBeforeUpstream(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("invalid ByteDance request must not reach the upstream")
		return nil, nil
	}}, nil)

	valid := func() VideoCreateRequest {
		return VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
			Prompt: "A tracking shot", Seconds: 10,
		}
	}
	seed := int64(-2)
	tests := map[string]struct {
		mutate func(*VideoCreateRequest)
		code   string
	}{
		// Ark derives the frame size from resolution plus ratio; the OpenAI-only
		// fields have no honest mapping and are refused outright.
		"size":         {func(r *VideoCreateRequest) { r.Size = "1280x720" }, "invalid_seedance_request"},
		"quality":      {func(r *VideoCreateRequest) { r.Quality = "high" }, "invalid_seedance_request"},
		"aspect ratio": {func(r *VideoCreateRequest) { r.AspectRatio = "16:9" }, "invalid_seedance_request"},
		"uppercase model": {func(r *VideoCreateRequest) {
			r.Model = "Doubao-Seedance-1-0-Pro-250528"
		}, "invalid_seedance_request"},
		"edit operation": {func(r *VideoCreateRequest) { r.Operation = VideoOperationEdit }, "unsupported_operation"},
		"empty prompt":   {func(r *VideoCreateRequest) { r.Prompt = "  " }, "prompt_required"},
		// Every Ark parameter has an explicit field, so provider_options has
		// nothing to map onto and is refused rather than silently dropped.
		"provider options": {func(r *VideoCreateRequest) {
			r.ProviderOptions = map[string]any{"service_tier": "priority"}
		}, "unsupported_option"},
		"unsupported resolution": {func(r *VideoCreateRequest) { r.Resolution = "8k" }, "invalid_seedance_request"},
		"unsupported ratio":      {func(r *VideoCreateRequest) { r.Ratio = "5:2" }, "invalid_seedance_request"},
		"frames with duration":   {func(r *VideoCreateRequest) { r.Frames = 29 }, "invalid_seedance_request"},
		"frames off the 25+4n grid": {func(r *VideoCreateRequest) {
			r.Seconds, r.Frames = 0, 30
		}, "invalid_seedance_request"},
		"seed below -1": {func(r *VideoCreateRequest) { r.Seed = &seed }, "invalid_seedance_request"},
		"execution expiry out of range": {func(r *VideoCreateRequest) {
			r.ExecutionExpiresAfter = 60
		}, "invalid_seedance_request"},
		"unknown service tier": {func(r *VideoCreateRequest) { r.ServiceTier = "priority" }, "invalid_seedance_request"},
		"characters": {func(r *VideoCreateRequest) {
			r.Characters = []ProviderResourceRef{{Provider: VideoProviderByteDance, ProviderResourceID: "cameo_1"}}
		}, "invalid_seedance_request"},
		// content[] role limits come from the capability snapshot.
		"two first frames": {func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{
				{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://media.example.com/a.png"},
				{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://media.example.com/b.png"},
			}
		}, "unsupported_capability"},
		"unknown content role": {func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleMask, URL: "https://media.example.com/mask.png"}}
		}, "unsupported_capability"},
		"adaptive ratio without first frame": {func(r *VideoCreateRequest) { r.Ratio = ArkVideoRatioAdaptive }, "unsupported_capability"},
		// Reference video and audio must be https URLs: Ark has no inline form.
		"inline reference video": {func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, Input: &VideoInput{
				VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceVideo, MIMEType: "video/mp4", Size: 3},
			}}}
		}, "unsupported_reference_media"},
		"invalid reference url": {func(r *VideoCreateRequest) {
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "ftp://media.example.com/first.png"}}
		}, "unsupported_reference_media"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			request := valid()
			test.mutate(&request)
			_, err := provider.Create(context.Background(), byteDanceVideoTestAccount(), request, nil)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, test.code, providerErr.Code)
			require.Equal(t, http.StatusBadRequest, providerErr.StatusCode)
		})
	}
}

func TestByteDanceVideoProviderOpenAICompatibleModeUsesVideosEndpoint(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "https://relay.example.com/v1/videos", req.URL.String())
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, ByteDanceVideoModelSeedance10Pro, payload["model"])
		require.Equal(t, "A tracking shot", payload["prompt"])
		require.EqualValues(t, 10, payload["seconds"])
		require.Equal(t, "720p", payload["resolution"])
		require.NotContains(t, payload, "content")
		require.NotContains(t, payload, "size")
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_relay_1","status":"queued"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
		Prompt: "A tracking shot", Seconds: 10, Resolution: "720p",
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "video_relay_1", task.ProviderTaskID)
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.Equal(t, "720p", task.Metadata["resolution"])
}

func TestByteDanceVideoProviderOpenAICompatibleCreateRejectsHTTPVideoURL(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusOK,
			`{"id":"video_relay_bad_url","status":"completed","video_url":"http://cdn.example.test/video.mp4"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
		Prompt: "A tracking shot", Seconds: 10,
	}, nil)

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Nil(t, task)
	require.Equal(t, "invalid_video_url", providerErr.Code)
	require.Equal(t, VideoSubmissionUnknown, providerErr.Certainty)
	require.False(t, providerErr.Retryable)
}

func TestByteDanceVideoProviderLegacySeedance20UsesStringSeconds(t *testing.T) {
	referenceURL := "https://media.example.com/reference.mp4?preview=1&auth_key=signed"
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, "doubao-seedance-2.0-mini-480p", payload["model"])
		require.Equal(t, "8", payload["seconds"])
		require.Equal(t, []any{referenceURL}, payload["reference_videos"])
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_seedance_20","status":"queued"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-mini-480p",
		Prompt: "A couple walking on grass", Seconds: 8,
		Content: []VideoContentItem{{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: referenceURL}},
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "video_seedance_20", task.ProviderTaskID)
}

func TestByteDanceVideoProviderOpenAICompatibleRejectsInvalidReferenceMedia(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("invalid compatible reference media must not reach the upstream")
		return nil, nil
	}}, nil)

	_, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-mini-480p",
		Prompt: "A couple walking on grass", Seconds: 8,
		Content: []VideoContentItem{{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: "http://127.0.0.1/reference.mp4"}},
	}, nil)

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "invalid_reference_media", providerErr.Code)
	require.Equal(t, http.StatusBadRequest, providerErr.StatusCode)
}

func TestByteDanceVideoProviderRetriesTransportFailureBeforeRequestWrite(t *testing.T) {
	calls := 0
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		require.NotEmpty(t, req.Header.Get("Idempotency-Key"))
		if calls == 1 {
			return nil, errors.New("dial timeout before request write")
		}
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_retry","status":"queued"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		ClientToken: "video_retry_token", Operation: VideoOperationGenerate,
		Model: "doubao-seedance-2.0-mini-480p", Prompt: "A tracking shot", Seconds: 8,
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "video_retry", task.ProviderTaskID)
	require.Equal(t, 2, calls)
}

func TestByteDanceVideoProviderDoesNotRetryAfterRequestWrite(t *testing.T) {
	calls := 0
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		trace := httptrace.ContextClientTrace(req.Context())
		require.NotNil(t, trace)
		require.NotNil(t, trace.WroteRequest)
		trace.WroteRequest(httptrace.WroteRequestInfo{})
		return nil, errors.New("connection lost after request write")
	}}, nil)

	_, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), VideoCreateRequest{
		ClientToken: "video_no_retry_token", Operation: VideoOperationGenerate,
		Model: "doubao-seedance-2.0-mini-480p", Prompt: "A tracking shot", Seconds: 8,
	}, nil)

	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestByteDanceVideoProviderOpenAICompatibleGetAndDeleteUseVideosEndpoint(t *testing.T) {
	paths := []string{}
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if req.Method == http.MethodDelete {
			return openAIVideoResponseForTest(http.StatusNoContent, "", nil), nil
		}
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_relay_1","status":"completed"}`, nil), nil
	}}, nil)
	account := byteDanceCompatibleVideoTestAccount()
	ref := ProviderTaskRef{Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "video_relay_1"}

	task, err := provider.Get(context.Background(), account, ref)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationCompleted, task.Status)
	require.NoError(t, provider.Delete(context.Background(), account, ref))
	require.Equal(t, []string{
		"GET /v1/videos/video_relay_1",
		"DELETE /v1/videos/video_relay_1",
	}, paths)
}

func TestByteDanceVideoProviderOpenAICompatibleGetRejectsHTTPVideoURL(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusOK,
			`{"id":"video_relay_1","status":"completed","video_url":"http://cdn.example.test/video.mp4"}`, nil), nil
	}}, nil)
	account := byteDanceCompatibleVideoTestAccount()

	task, err := provider.Get(context.Background(), account, ProviderTaskRef{
		Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "video_relay_1",
	})

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Nil(t, task)
	require.Equal(t, "invalid_video_url", providerErr.Code)
	require.False(t, providerErr.Retryable)
}

// Ark never serves bytes from the API host, so content is only reachable
// through the stored object-storage URL.
func TestByteDanceVideoProviderOpenContentRequiresStoredUpstreamURL(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("ByteDance content must never be fetched from the API host")
		return nil, nil
	}}, nil)
	account := byteDanceVideoTestAccount()
	ref := ProviderTaskRef{Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "cgt-1"}

	_, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{TaskRef: ref})
	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "content_unavailable", providerErr.Code)
	require.Equal(t, http.StatusConflict, providerErr.StatusCode)

	_, err = provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef: ref, UpstreamURL: "http://tos.example.com/v.mp4",
	})
	require.Error(t, err)
}

// The object-storage URL is already signed. Attaching the Ark API key to it
// would leak the upstream credential to a third-party host.
func TestByteDanceVideoProviderOpenContentSendsNoUpstreamCredential(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("ByteDance content must not go through the API upstream client")
		return nil, nil
	}}, nil)
	provider.resolver = videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	provider.redirect = func(_ context.Context, request *http.Request, _ *Account, _ []netip.Addr) (*http.Response, error) {
		require.Empty(t, request.Header.Get("Authorization"))
		require.Equal(t, "sub2api-video-content/1", request.Header.Get("User-Agent"))
		require.Equal(t, "bytes=0-1023", request.Header.Get("Range"))
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("video")),
		}, nil
	}

	account := byteDanceVideoTestAccount()
	content, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef:     ProviderTaskRef{Provider: VideoProviderByteDance, AccountID: account.ID, ProviderTaskID: "cgt-1"},
		UpstreamURL: "https://tos.example.com/v.mp4?sig=x",
		Range:       "bytes=0-1023",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, content.StatusCode)
	require.NoError(t, content.Body.Close())
}

func TestByteDanceVideoWebhookIsExplicitlyUnsupported(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("unsupported ByteDance webhook must not call the upstream")
		return nil, nil
	}}, nil)
	account := byteDanceVideoTestAccount()
	service := NewVideoWebhookService(
		&videoTaskRepoStub{},
		&videoAccountRepoStub{accounts: []Account{*account}},
		NewVideoProviderRegistry(provider),
		nil,
		nil,
	)

	result, err := service.Handle(context.Background(), VideoProviderByteDance, account.ID, ProviderWebhookRequest{})

	require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
	require.Nil(t, result)
	require.False(t, provider.Capabilities().Supports(VideoCapabilityWebhook))
}

func byteDanceSpoolImageForTest(role VideoInputRole, mimeType, sha string) *VideoInput {
	return &VideoInput{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: role, MIMEType: mimeType, Size: 3, SHA256: sha},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil },
	}
}

func TestByteDanceContentItemsEncodeArkRoles(t *testing.T) {
	spool := byteDanceSpoolImageForTest(VideoInputRoleFirstFrame, "image/png", "aaa")
	request := VideoCreateRequest{Prompt: "A tracking shot", Content: []VideoContentItem{
		{Type: VideoContentTypeText, Text: "A tracking shot"},
		{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, Input: spool},
		{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, URL: "https://media.example.com/last.png"},
		{Type: VideoContentTypeImage, Role: VideoInputRoleTaggedReferenceImage, URL: "https://media.example.com/ref.png"},
		{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: "https://media.example.com/clip.mp4?sig=1"},
		{Type: VideoContentTypeAudio, Role: VideoInputRoleReferenceAudio, URL: "https://media.example.com/voice.mp3"},
	}}

	items, err := byteDanceContentItems(context.Background(), request, nil, true)
	require.NoError(t, err)
	require.Equal(t, []byteDanceNativeContentItem{
		{Type: "text", Text: "A tracking shot"},
		{Type: "image_url", Role: "first_frame", ImageURL: &byteDanceNativeMediaURL{URL: "data:image/png;base64,YWJj"}},
		{Type: "image_url", Role: "last_frame", ImageURL: &byteDanceNativeMediaURL{URL: "https://media.example.com/last.png"}},
		{Type: "image_url", Role: "reference_image", ImageURL: &byteDanceNativeMediaURL{URL: "https://media.example.com/ref.png"}},
		{Type: "video_url", Role: "reference_video", VideoURL: &byteDanceNativeMediaURL{URL: "https://media.example.com/clip.mp4?sig=1"}},
		{Type: "audio_url", Role: "reference_audio", AudioURL: &byteDanceNativeMediaURL{URL: "https://media.example.com/voice.mp3"}},
	}, items)

	// 只校验不读字节：data URI 留空。
	items, err = byteDanceContentItems(context.Background(), request, nil, false)
	require.NoError(t, err)
	require.Equal(t, "", items[1].ImageURL.URL)
	// 已挂到 content 上的 spool 输入不重复编码。
	items, err = byteDanceContentItems(context.Background(), request, []VideoInput{*spool}, false)
	require.NoError(t, err)
	require.Len(t, items, 6)

	// 没有 text 项时用 Prompt 补一条放最前；未挂到 content 的 spool 输入（旧式 input_reference）
	// 与通用 input_reference 引用都按首帧处理。
	loose := byteDanceSpoolImageForTest(VideoInputRoleReferenceImage, "image/jpeg", "bbb")
	legacy := VideoCreateRequest{Prompt: "legacy", Content: []VideoContentItem{
		{Type: VideoContentTypeImage, Role: VideoInputRoleReferenceImage, URL: "https://media.example.com/first.png"},
	}}
	items, err = byteDanceContentItems(context.Background(), legacy, []VideoInput{*loose}, true)
	require.NoError(t, err)
	require.Equal(t, []byteDanceNativeContentItem{
		{Type: "text", Text: "legacy"},
		{Type: "image_url", Role: "first_frame", ImageURL: &byteDanceNativeMediaURL{URL: "https://media.example.com/first.png"}},
		{Type: "image_url", Role: "first_frame", ImageURL: &byteDanceNativeMediaURL{URL: "data:image/jpeg;base64,YWJj"}},
	}, items)

	oversized := byteDanceSpoolImageForTest(VideoInputRoleFirstFrame, "image/png", "ccc")
	oversized.Size = byteDanceMaxInlineImageBytes + 1
	rejections := map[string]struct {
		request VideoCreateRequest
		inputs  []VideoInput
		code    string
	}{
		"inline reference video": {VideoCreateRequest{Prompt: "p", Content: []VideoContentItem{{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, Input: byteDanceSpoolImageForTest(VideoInputRoleReferenceVideo, "video/mp4", "ddd")}}}, nil, "unsupported_reference_media"},
		"ftp reference url":      {VideoCreateRequest{Prompt: "p", Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "ftp://media.example.com/first.png"}}}, nil, "unsupported_reference_media"},
		"unknown role":           {VideoCreateRequest{Prompt: "p", Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleMask, URL: "https://media.example.com/mask.png"}}}, nil, "unsupported_reference_media"},
		"oversized inline image": {VideoCreateRequest{Prompt: "p", Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, Input: oversized}}}, nil, "input_too_large"},
		"loose video input":      {VideoCreateRequest{Prompt: "p"}, []VideoInput{*byteDanceSpoolImageForTest(VideoInputRoleSourceVideo, "video/mp4", "eee")}, "unsupported_input"},
	}
	for name, tc := range rejections {
		t.Run(name, func(t *testing.T) {
			_, err := byteDanceContentItems(context.Background(), tc.request, tc.inputs, false)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, VideoSubmissionRejected, providerErr.Certainty)
		})
	}
}

func TestByteDanceVideoProviderNativeCreateEncodesArkFields(t *testing.T) {
	var payload map[string]any
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		payload = nil
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"cgt-ark-1"}`, nil), nil
	}}, nil)
	on, off := true, false
	seed := int64(7)
	request := VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "A tracking shot",
		SmartDuration: true, Resolution: "1080p", Ratio: ArkVideoRatioAdaptive,
		Seed: &seed, CameraFixed: &on, Watermark: &off, AudioEnabled: &on, ReturnLastFrame: &on,
		ServiceTier: "flex", ExecutionExpiresAfter: 7200,
		Content: []VideoContentItem{
			{Type: VideoContentTypeText, Text: "A tracking shot"},
			{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, Input: byteDanceSpoolImageForTest(VideoInputRoleFirstFrame, "image/png", "aaa")},
			{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: "https://media.example.com/clip.mp4"},
		},
	}

	task, err := provider.Create(context.Background(), byteDanceVideoTestAccount(), request, nil)
	require.NoError(t, err)
	require.Equal(t, "cgt-ark-1", task.ProviderTaskID)
	// 智能时长编码成 duration=-1，不送 frames。
	require.EqualValues(t, -1, payload["duration"])
	require.NotContains(t, payload, "frames")
	require.Equal(t, "1080p", payload["resolution"])
	require.Equal(t, ArkVideoRatioAdaptive, payload["ratio"])
	require.EqualValues(t, 7, payload["seed"])
	require.Equal(t, true, payload["camera_fixed"])
	require.Equal(t, false, payload["watermark"])
	require.Equal(t, true, payload["generate_audio"])
	require.Equal(t, true, payload["return_last_frame"])
	require.Equal(t, "flex", payload["service_tier"])
	require.EqualValues(t, 7200, payload["execution_expires_after"])
	for _, absent := range []string{"prompt", "seconds", "size", "provider_options"} {
		require.NotContains(t, payload, absent)
	}
	content, ok := payload["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 3)
	require.Equal(t, map[string]any{"type": "text", "text": "A tracking shot"}, content[0])
	require.Equal(t, map[string]any{"type": "image_url", "role": "first_frame", "image_url": map[string]any{"url": "data:image/png;base64,YWJj"}}, content[1])
	require.Equal(t, map[string]any{"type": "video_url", "role": "reference_video", "video_url": map[string]any{"url": "https://media.example.com/clip.mp4"}}, content[2])
	// 元数据：adaptive 比例与智能时长不记请求值，等上游回报；分辨率与种子记下。
	require.Equal(t, "1080p", task.Metadata["resolution"])
	require.NotContains(t, task.Metadata, "ratio")
	require.NotContains(t, task.Metadata, "seconds")
	require.Equal(t, int64(7), task.Metadata["seed"])
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, task.Metadata["model"])

	// frames 请求：送 frames，不送 duration。
	frames := VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "A tracking shot", Frames: 121, Resolution: "720p", Ratio: "16:9"}
	task, err = provider.Create(context.Background(), byteDanceVideoTestAccount(), frames, nil)
	require.NoError(t, err)
	require.EqualValues(t, 121, payload["frames"])
	require.NotContains(t, payload, "duration")
	require.Equal(t, "16:9", task.Metadata["ratio"])
	require.NotContains(t, task.Metadata, "seconds")

	// 随机种子（-1）不进元数据：它不是可复现的值。
	random := int64(-1)
	explicit := VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "A tracking shot", Seconds: 5, Seed: &random}
	task, err = provider.Create(context.Background(), byteDanceVideoTestAccount(), explicit, nil)
	require.NoError(t, err)
	require.EqualValues(t, 5, payload["duration"])
	require.EqualValues(t, -1, payload["seed"])
	require.NotContains(t, task.Metadata, "seed")
	require.Equal(t, 5, task.Metadata["seconds"])
}

func TestDecodeByteDanceNativeTaskRecordsArkObservations(t *testing.T) {
	task, err := decodeByteDanceNativeTask(strings.NewReader(`{
		"id":"cgt-1","model":"doubao-seedance-1-0-pro-250528","status":"succeeded","created_at":1757239200,"updated_at":1757239260,
		"resolution":"1080P","ratio":"9:16","duration":12,"frames":289,"framespersecond":24,"seed":99,"generate_audio":true,
		"service_tier":"flex","revised_prompt":"revised",
		"content":{"video_url":"https://tos.example.com/v.mp4?sig=x","last_frame_url":"https://tos.example.com/last.png?sig=y"},
		"usage":{"completion_tokens":1234,"total_tokens":1234}
	}`), false)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationCompleted, task.Status)
	require.Equal(t, map[string]any{
		"model": ByteDanceVideoModelSeedance10Pro, "resolution": "1080p", "ratio": "9:16", "seconds": int64(12), "frames": int64(289),
		"framespersecond": int64(24), "seed": int64(99), "generate_audio": true, "service_tier": "flex", "revised_prompt": "revised",
	}, task.Metadata)
	require.Equal(t, "https://tos.example.com/v.mp4?sig=x", task.VideoURL)
	require.Equal(t, "https://tos.example.com/last.png?sig=y", task.LastFrameURL)
	require.Equal(t, []string{"video", "last_frame"}, task.ContentVariants)
	require.Equal(t, time.Unix(1757239260, 0).UTC(), task.ProviderFinishedAt.UTC())
	require.Equal(t, time.Unix(1757239260, 0).Add(byteDanceContentURLTTL).UTC(), task.ContentExpiresAt.UTC())
	require.EqualValues(t, 1234, task.Usage["completion_tokens"])

	// 尾帧链接必须是 https；没有尾帧时变体只有 video。
	_, err = decodeByteDanceNativeTask(strings.NewReader(`{"id":"cgt-1","status":"succeeded","content":{"video_url":"https://tos.example.com/v.mp4","last_frame_url":"http://tos.example.com/last.png"}}`), false)
	require.Error(t, err)
	task, err = decodeByteDanceNativeTask(strings.NewReader(`{"id":"cgt-1","status":"succeeded","updated_at":1757239260,"content":{"video_url":"https://tos.example.com/v.mp4"}}`), false)
	require.NoError(t, err)
	require.Equal(t, []string{"video"}, task.ContentVariants)
	require.Empty(t, task.LastFrameURL)

	// 失败 / 取消 / 过期：状态词映射，错误码留下，没有秒数。
	task, err = decodeByteDanceNativeTask(strings.NewReader(`{"id":"cgt-2","status":"failed","error":{"code":"ModelNotOpen","message":"not enabled"}}`), false)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, task.Status)
	require.Equal(t, "ModelNotOpen", task.ErrorCode)
	require.Equal(t, "not enabled", task.ErrorMessage)
	require.NotContains(t, task.Metadata, "seconds")
	task, err = decodeByteDanceNativeTask(strings.NewReader(`{"id":"cgt-3","status":"expired"}`), false)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationExpired, task.Status)
	// 零值观测不记录（duration=0 不是时长）。
	task, err = decodeByteDanceNativeTask(strings.NewReader(`{"id":"cgt-4","status":"running","duration":0,"frames":0,"seed":0}`), false)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationInProgress, task.Status)
	require.NotContains(t, task.Metadata, "seconds")
	require.NotContains(t, task.Metadata, "frames")
	require.Equal(t, int64(0), task.Metadata["seed"], "seed 0 is a real seed")
}

func TestByteDanceVideoProviderOpenAICompatibleRejectsArkOnlyFields(t *testing.T) {
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("Ark-only fields must not reach the compatible relay")
		return nil, nil
	}}, nil)
	base := func() VideoCreateRequest {
		return VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "A tracking shot", Seconds: 10, Resolution: "720p"}
	}
	on := true
	seed := int64(7)
	cases := map[string]struct {
		mutate func(*VideoCreateRequest)
		inputs []VideoInput
		code   string
	}{
		"smart duration":    {func(r *VideoCreateRequest) { r.Seconds, r.SmartDuration = 0, true }, nil, "unsupported_option"},
		"frames":            {func(r *VideoCreateRequest) { r.Seconds, r.Frames = 0, 29 }, nil, "unsupported_option"},
		"seed":              {func(r *VideoCreateRequest) { r.Seed = &seed }, nil, "unsupported_option"},
		"camera_fixed":      {func(r *VideoCreateRequest) { r.CameraFixed = &on }, nil, "unsupported_option"},
		"watermark":         {func(r *VideoCreateRequest) { r.Watermark = &on }, nil, "unsupported_option"},
		"generate_audio":    {func(r *VideoCreateRequest) { r.AudioEnabled = &on }, nil, "unsupported_option"},
		"return_last_frame": {func(r *VideoCreateRequest) { r.ReturnLastFrame = &on }, nil, "unsupported_option"},
		"service_tier":      {func(r *VideoCreateRequest) { r.ServiceTier = "flex" }, nil, "unsupported_option"},
		// execution_expires_after 不再在此拒绝：中继模式下由本网关 worker 执行排队超时。
		// 中继只认 https 引用：spool 字节没有可上传的通道。
		"uploaded input": {func(r *VideoCreateRequest) {}, []VideoInput{*byteDanceSpoolImageForTest(VideoInputRoleFirstFrame, "image/png", "aaa")}, "unsupported_input"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			request := base()
			tc.mutate(&request)
			_, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), request, tc.inputs)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, tc.code, providerErr.Code)
			require.Equal(t, http.StatusBadRequest, providerErr.StatusCode)
		})
	}
}

func TestByteDanceVideoProviderOpenAICompatibleMapsContentToReferenceFields(t *testing.T) {
	var payload map[string]any
	provider := NewByteDanceVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		payload = nil
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_relay_2","status":"queued"}`, nil), nil
	}}, nil)
	request := VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "A tracking shot", Seconds: 10,
		Resolution: "720p", Ratio: "9:16",
		Content: []VideoContentItem{
			{Type: VideoContentTypeText, Text: "A tracking shot"},
			{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://media.example.com/first.png"},
			{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, URL: "https://media.example.com/last.png"},
		},
	}
	task, err := provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), request, nil)
	require.NoError(t, err)
	require.Equal(t, "video_relay_2", task.ProviderTaskID)
	require.Equal(t, "A tracking shot", payload["prompt"])
	require.EqualValues(t, 10, payload["seconds"])
	require.Equal(t, "720p", payload["resolution"])
	require.Equal(t, "9:16", payload["ratio"])
	require.Equal(t, "https://media.example.com/first.png", payload["first_image_url"])
	require.Equal(t, "https://media.example.com/last.png", payload["last_image_url"])
	require.NotContains(t, payload, "content")
	require.NotContains(t, payload, "duration")
	require.Equal(t, "9:16", task.Metadata["ratio"])

	// adaptive 跟随首帧，中继没有对应字段，不送 ratio。
	request.Ratio = ArkVideoRatioAdaptive
	request.Content = request.Content[:2]
	_, err = provider.Create(context.Background(), byteDanceCompatibleVideoTestAccount(), request, nil)
	require.NoError(t, err)
	require.NotContains(t, payload, "ratio")
	require.Equal(t, "https://media.example.com/first.png", payload["first_image_url"])
}
