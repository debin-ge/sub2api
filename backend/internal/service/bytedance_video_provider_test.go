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
		Prompt: "A tracking shot", Seconds: 10,
		ProviderOptions: map[string]any{"resolution": "720p", "ratio": "16:9"},
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
	tests := map[string]struct {
		mutate func(*VideoCreateRequest)
		code   string
	}{
		"size": {func(r *VideoCreateRequest) { r.Size = "1280x720" }, "invalid_seedance_request"},
		"uppercase model": {func(r *VideoCreateRequest) {
			r.Model = "Doubao-Seedance-1-0-Pro-250528"
		}, "invalid_seedance_request"},
		"edit operation": {func(r *VideoCreateRequest) { r.Operation = VideoOperationEdit }, "unsupported_operation"},
		"empty prompt":   {func(r *VideoCreateRequest) { r.Prompt = "  " }, "prompt_required"},
		// Ark honours "--flags" inside the prompt, so a prompt carrying them
		// would override the resolution and duration that were quoted and held.
		"prompt flag": {func(r *VideoCreateRequest) {
			r.Prompt = "A tracking shot --resolution 4k"
		}, "unsupported_prompt_flag"},
		"leading prompt flag": {func(r *VideoCreateRequest) { r.Prompt = "--duration 12" }, "unsupported_prompt_flag"},
		"unknown provider option": {func(r *VideoCreateRequest) {
			r.ProviderOptions = map[string]any{"service_tier": "priority"}
		}, "unsupported_option"},
		"unsupported resolution": {func(r *VideoCreateRequest) {
			r.ProviderOptions = map[string]any{"resolution": "8k"}
		}, "unsupported_option"},
		"unsupported ratio": {func(r *VideoCreateRequest) {
			r.ProviderOptions = map[string]any{"ratio": "5:2"}
		}, "unsupported_option"},
		"non-string provider option": {func(r *VideoCreateRequest) {
			r.ProviderOptions = map[string]any{"resolution": 720}
		}, "unsupported_option"},
		"characters": {func(r *VideoCreateRequest) {
			r.Characters = []ProviderResourceRef{{Provider: VideoProviderByteDance, ProviderResourceID: "cameo_1"}}
		}, "unsupported_characters"},
		"first frame reference": {func(r *VideoCreateRequest) {
			r.ReferenceMedia.FirstImageURL = "https://media.example.com/first.png"
		}, "unsupported_reference_media"},
		"video reference": {func(r *VideoCreateRequest) {
			r.ReferenceMedia.ReferenceVideos = []string{"https://media.example.com/clip.mp4"}
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
		Prompt: "A tracking shot", Seconds: 10,
		ProviderOptions: map[string]any{"resolution": "720p"},
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
		ReferenceMedia: ProviderVideoReferenceMedia{ReferenceVideos: []string{referenceURL}},
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
		ReferenceMedia: ProviderVideoReferenceMedia{ReferenceVideos: []string{"http://127.0.0.1/reference.mp4"}},
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
