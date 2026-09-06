package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type openAIVideoHTTPStub struct {
	do func(*http.Request, string, int64, int) (*http.Response, error)
}

type openAIVideoCloseTracker struct {
	io.Reader
	closed chan<- struct{}
}

func (r *openAIVideoCloseTracker) Close() error {
	select {
	case r.closed <- struct{}{}:
	default:
	}
	return nil
}

func (s *openAIVideoHTTPStub) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	return s.do(req, proxyURL, accountID, concurrency)
}

func (s *openAIVideoHTTPStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

func openAIVideoTestAccount() *Account {
	return &Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 3,
		Credentials: map[string]any{"api_key": "sk-video-secret", "openai_capabilities": []any{"videos"}},
	}
}

func openAIVideoResponseForTest(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOpenAIVideoProviderCapabilityProbeUsesNonBillingList(t *testing.T) {
	account := openAIVideoTestAccount()
	delete(account.Credentials, "openai_capabilities")
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/v1/videos", req.URL.Path)
		require.Equal(t, "1", req.URL.Query().Get("limit"))
		require.Equal(t, "Bearer sk-video-secret", req.Header.Get("Authorization"))
		return openAIVideoResponseForTest(http.StatusOK, `{"data":[]}`, nil), nil
	}}, nil)

	result, err := provider.ProbeCapability(context.Background(), account, VideoCapabilityCreate)

	require.NoError(t, err)
	require.Equal(t, VideoCapabilityProbeSupported, result.Status)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Empty(t, result.ErrorSummary)
}

func TestOpenAIVideoProviderCapabilityProbeClassifiesSafeFailures(t *testing.T) {
	account := openAIVideoTestAccount()
	delete(account.Credentials, "openai_capabilities")
	tests := []struct {
		name       string
		response   *http.Response
		transport  error
		wantStatus string
		wantError  string
	}{
		{name: "permission", response: openAIVideoResponseForTest(http.StatusForbidden, `{"error":{"message":"sk-secret","code":"forbidden"}}`, nil), wantStatus: VideoCapabilityProbeUnsupported, wantError: "videos_access_denied"},
		{name: "missing endpoint", response: openAIVideoResponseForTest(http.StatusNotFound, `{}`, nil), wantStatus: VideoCapabilityProbeUnsupported, wantError: "videos_endpoint_unavailable"},
		{name: "rate limited", response: openAIVideoResponseForTest(http.StatusTooManyRequests, `{}`, nil), wantStatus: VideoCapabilityProbeUnknown, wantError: "transient_upstream_error"},
		{name: "transport", transport: errors.New("dial included sk-secret"), wantStatus: VideoCapabilityProbeUnknown, wantError: "transport_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				return test.response, test.transport
			}}, nil)
			result, err := provider.ProbeCapability(context.Background(), account, VideoCapabilityCreate)
			require.NoError(t, err)
			require.Equal(t, test.wantStatus, result.Status)
			require.Equal(t, test.wantError, result.ErrorSummary)
			require.NotContains(t, result.ErrorSummary, "sk-secret")
		})
	}
}

func TestOpenAIVideoProviderCreateJSON(t *testing.T) {
	account := openAIVideoTestAccount()
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "https://api.openai.com/v1/videos", req.URL.String())
		require.Equal(t, "Bearer sk-video-secret", req.Header.Get("Authorization"))
		require.Equal(t, "video_local", req.Header.Get("Idempotency-Key"))
		require.Equal(t, int64(11), accountID)
		require.Equal(t, 3, concurrency)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, "sora-2", payload["model"])
		require.Equal(t, "8", payload["seconds"])
		require.Equal(t, map[string]any{"image_url": "https://cdn.example/reference.png"}, payload["input_reference"])
		require.NotContains(t, payload, "task_id")
		return openAIVideoResponseForTest(http.StatusOK, `{
			"id":"video_upstream_1","status":"queued","model":"sora-2",
			"progress":0,"seconds":"8","size":"1280x720","created_at":1788364800
		}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), account, VideoCreateRequest{
		TaskID: "video_local", ClientToken: "video_local", Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		Prompt: "A paper airplane", Seconds: 8, Size: "1280x720",
		InputReference: &ProviderInputReference{ImageURL: "https://cdn.example/reference.png"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "video_upstream_1", task.ProviderTaskID)
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.NotNil(t, task.ProviderCreatedAt)
}

func TestOpenAIVideoProviderCustomBaseURLForwardsReferenceVideos(t *testing.T) {
	account := openAIVideoTestAccount()
	account.Credentials["base_url"] = "https://video-upstream.example/v1"
	referenceURL := "https://media.example.com/reference.mp4?preview=1&auth_key=signed"
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "https://video-upstream.example/v1/videos", req.URL.String())
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, "10", payload["seconds"], "Seedance-compatible upstream requires a string")
		require.Equal(t, []any{referenceURL}, payload["reference_videos"])
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_seedance_1","status":"queued"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), account, VideoCreateRequest{
		ClientToken: "video_local", Operation: VideoOperationGenerate,
		Model: "doubao-seedance-2.0-mini-480p", Prompt: "A sports car", Seconds: 10,
		ReferenceMedia: ProviderVideoReferenceMedia{ReferenceVideos: []string{referenceURL}},
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "video_seedance_1", task.ProviderTaskID)
}

func TestOpenAIVideoProviderCustomBaseURLIgnoresRatioWhenMediaDeterminesFraming(t *testing.T) {
	account := openAIVideoTestAccount()
	account.Credentials["base_url"] = "https://video-upstream.example/v1"
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.NotContains(t, payload, "ratio")
		require.NotContains(t, payload, "aspect_ratio")
		require.Equal(t, "https://media.example.com/first.png", payload["image_url"])
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_seedance_2","status":"queued"}`, nil), nil
	}}, nil)

	_, err := provider.Create(context.Background(), account, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-mini-480p",
		Prompt: "A sports car", Seconds: 10,
		ReferenceMedia: ProviderVideoReferenceMedia{Ratio: "16:9", ImageURL: "https://media.example.com/first.png"},
	}, nil)
	require.NoError(t, err)
}

func TestOpenAIVideoProviderRejectsInvalidSeedance20BeforeUpstream(t *testing.T) {
	account := openAIVideoTestAccount()
	account.Credentials["base_url"] = "https://video-upstream.example/v1"
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("invalid Seedance request must fail before the upstream call")
		return nil, nil
	}}, nil)

	_, err := provider.Create(context.Background(), account, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-mini-480p",
		Prompt: "test", Seconds: 16,
	}, nil)
	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "invalid_seedance_request", providerErr.Code)
}

func TestOpenAIVideoProviderKeepsCompatibleReferencesOffOfficialEndpoint(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("official OpenAI endpoint must reject compatible reference fields before forwarding")
		return nil, nil
	}}, nil)

	_, err := provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		Prompt: "test", Seconds: 8, Size: "1280x720",
		ReferenceMedia: ProviderVideoReferenceMedia{ReferenceVideos: []string{"https://media.example.com/reference.mp4"}},
	}, nil)

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "unsupported_reference_media", providerErr.Code)
}

func TestDecodeBoundedJSONRejectsTrailingDocument(t *testing.T) {
	var decoded map[string]any
	err := decodeBoundedJSON(strings.NewReader(`{"id":"video_1"} {"extra":true}`), 1024, &decoded)
	require.Error(t, err)
}

func TestOpenAIVideoProviderRejectsOptionsItCannotForward(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("unsupported options must fail before the upstream call")
		return nil, nil
	}}, nil)
	base := VideoCreateRequest{Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2, Prompt: "waves", Seconds: 4, Size: "1280x720"}

	audio := base
	audioEnabled := true
	audio.AudioEnabled = &audioEnabled
	_, err := provider.Create(context.Background(), openAIVideoTestAccount(), audio, nil)
	require.Error(t, err)

	tier := base
	tier.ServiceTier = "batch"
	_, err = provider.Create(context.Background(), openAIVideoTestAccount(), tier, nil)
	require.Error(t, err)

	options := base
	options.ProviderOptions = map[string]any{"seed": 7}
	_, err = provider.Create(context.Background(), openAIVideoTestAccount(), options, nil)
	require.Error(t, err)
}

func TestOpenAIVideoProviderRejectsFileReferenceWithoutVerifiedIsolation(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("shared provider file references must fail before the upstream call")
		return nil, nil
	}}, nil)

	ownerID := int64(42)
	for _, mode := range []string{"shared", "legacy_owner", "unverified_owner"} {
		t.Run(mode, func(t *testing.T) {
			account := openAIVideoTestAccount()
			if mode != "shared" {
				account.VideoOwnerUserID = &ownerID
			}
			if mode == "unverified_owner" {
				account.OwnerUserID = &ownerID
				account.OwnershipMode = AccountOwnershipUserDedicated
				account.IsolationState = AccountIsolationUnverified
				account.ProviderIdentityVersion = 1
			}
			_, err := provider.Create(context.Background(), account, VideoCreateRequest{
				Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
				Prompt: "Animate", Seconds: 8, Size: "1280x720",
				InputReference: &ProviderInputReference{FileID: "file_other_tenant"},
			}, nil)
			require.Error(t, err)
		})
	}
}

func TestOpenAIVideoProviderCreateMultipartStreamsSpoolInput(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1280, 720))))
	content := encoded.Bytes()
	input, err := session.Store(context.Background(), VideoInputRoleReferenceImage, "reference.png", "image/png", bytes.NewReader(content))
	require.NoError(t, err)

	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.LessOrEqual(t, req.ContentLength, int64(0))
		mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(req.Body, params["boundary"])
		fields := map[string]string{}
		var uploaded []byte
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			value, err := io.ReadAll(part)
			require.NoError(t, err)
			if part.FormName() == "input_reference" {
				require.Equal(t, "reference.png", part.FileName())
				require.Equal(t, "image/png", part.Header.Get("Content-Type"))
				uploaded = value
			} else {
				fields[part.FormName()] = string(value)
			}
		}
		require.Equal(t, content, uploaded)
		require.Equal(t, "sora-2-pro", fields["model"])
		require.Equal(t, "8", fields["seconds"])
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_upload_1","status":"in_progress","model":"sora-2-pro"}`, nil), nil
	}}, nil)

	task, err := provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2Pro,
		Prompt: "Animate the image", Seconds: 8, Size: "1280x720",
	}, []VideoInput{input})
	require.NoError(t, err)
	require.Equal(t, VideoGenerationInProgress, task.Status)
}

func TestOpenAIVideoProviderMultipartPreservesExplicitEarlyRejection(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1280, 720))))
	input, err := session.Store(
		context.Background(), VideoInputRoleReferenceImage, "reference.png", "image/png", bytes.NewReader(encoded.Bytes()),
	)
	require.NoError(t, err)
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		// Return without consuming the multipart body, matching an upstream that
		// rejects from headers or early form fields.
		return openAIVideoResponseForTest(http.StatusBadRequest, `{"error":{"code":"invalid_image","message":"image rejected"}}`, nil), nil
	}}, nil)

	_, err = provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		Prompt: "Animate", Seconds: 8, Size: "1280x720",
	}, []VideoInput{input})

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, VideoSubmissionRejected, providerErr.Certainty)
	require.Equal(t, "invalid_image", providerErr.Code)
}

func TestOpenAIVideoProviderCreateCharacterMultipart(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	content := append([]byte("\x00\x00\x00\x18ftypmp42"), bytes.Repeat([]byte("character-video"), 128)...)
	input, err := session.Store(
		context.Background(), VideoInputRoleCharacterClip, "character.mp4", "video/mp4", bytes.NewReader(content),
	)
	require.NoError(t, err)
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "/v1/videos/characters", req.URL.Path)
		require.Equal(t, "video_character_local", req.Header.Get("Idempotency-Key"))
		mediaType, params, parseErr := mime.ParseMediaType(req.Header.Get("Content-Type"))
		require.NoError(t, parseErr)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(req.Body, params["boundary"])
		fields := make(map[string]string)
		var uploaded []byte
		for {
			part, partErr := reader.NextPart()
			if partErr == io.EOF {
				break
			}
			require.NoError(t, partErr)
			value, readErr := io.ReadAll(part)
			require.NoError(t, readErr)
			if part.FormName() == "video" {
				uploaded = value
			} else {
				fields[part.FormName()] = string(value)
			}
		}
		require.Equal(t, "Mossy", fields["name"])
		require.Equal(t, content, uploaded)
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"char_upstream","name":"Mossy","expires_at":1788368400}`, nil), nil
	}}, nil)

	resource, err := provider.CreateCharacter(context.Background(), openAIVideoTestAccount(), VideoCharacterRequest{
		TaskID: "video_character_local", ClientToken: "video_character_local", Name: "Mossy",
	}, input)

	require.NoError(t, err)
	require.Equal(t, "char_upstream", resource.ProviderResourceID)
	require.Equal(t, "Mossy", resource.Metadata["name"])
	require.NotNil(t, resource.ExpiresAt)
}

func TestOpenAIVideoProviderEditUploadedVideoMultipart(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	content := append([]byte("\x00\x00\x00\x18ftypmp42"), bytes.Repeat([]byte("source-video"), 128)...)
	input, err := session.Store(
		context.Background(), VideoInputRoleSourceVideo, "source.mp4", "video/mp4", bytes.NewReader(content),
	)
	require.NoError(t, err)
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "/v1/videos/edits", req.URL.Path)
		mediaType, params, parseErr := mime.ParseMediaType(req.Header.Get("Content-Type"))
		require.NoError(t, parseErr)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(req.Body, params["boundary"])
		fields := make(map[string]string)
		var uploaded []byte
		for {
			part, partErr := reader.NextPart()
			if partErr == io.EOF {
				break
			}
			require.NoError(t, partErr)
			value, readErr := io.ReadAll(part)
			require.NoError(t, readErr)
			if part.FormName() == "video" {
				uploaded = value
			} else {
				fields[part.FormName()] = string(value)
			}
		}
		require.Equal(t, OpenAIVideoModelSora2Pro, fields["model"])
		require.Equal(t, "Shift the palette", fields["prompt"])
		require.Len(t, fields, 2)
		require.Equal(t, content, uploaded)
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_edited_upload","status":"queued"}`, nil), nil
	}}, nil)

	task, err := provider.Edit(context.Background(), openAIVideoTestAccount(), VideoEditRequest{
		VideoCreateRequest: VideoCreateRequest{
			ClientToken: "video_edit_local", Model: OpenAIVideoModelSora2Pro,
			Prompt: "Shift the palette",
		},
	}, []VideoInput{input})

	require.NoError(t, err)
	require.Equal(t, "video_edited_upload", task.ProviderTaskID)
}

func TestOpenAIVideoProviderRejectsCharacterExtensions(t *testing.T) {
	input := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleCharacterClip, MIMEType: "video/mp4", Size: 1024,
	}}
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("unsupported character options must fail before the upstream call")
		return nil, nil
	}}, nil)

	_, err := provider.CreateCharacter(context.Background(), openAIVideoTestAccount(), VideoCharacterRequest{
		Name: "Mossy", ProviderOptions: map[string]any{"seed": 7},
	}, input)

	require.Error(t, err)
}

func TestOpenAIVideoProviderDerivedOperations(t *testing.T) {
	account := openAIVideoTestAccount()
	t.Run("extension uses source id and no internal fields", func(t *testing.T) {
		provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			require.Equal(t, "/v1/videos/extensions", req.URL.Path)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, map[string]any{"id": "video_source"}, payload["video"])
			require.Equal(t, "8", payload["seconds"])
			require.NotContains(t, payload, "model")
			return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_extended","status":"queued"}`, nil), nil
		}}, nil)
		task, err := provider.Extend(context.Background(), account, VideoExtendRequest{
			VideoCreateRequest: VideoCreateRequest{Model: OpenAIVideoModelSora2, Prompt: "Continue", Seconds: 8},
			SourceTask:         ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_source"},
		})
		require.NoError(t, err)
		require.Equal(t, "video_extended", task.ProviderTaskID)
	})

	t.Run("custom extension uses integer seconds", func(t *testing.T) {
		custom := *account
		custom.Credentials = map[string]any{
			"api_key": "sk-video-secret", "base_url": "https://video-upstream.example/v1",
			"openai_capabilities": []any{"videos"},
		}
		provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			var payload map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, float64(8), payload["seconds"])
			return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_extended","status":"queued"}`, nil), nil
		}}, nil)
		_, err := provider.Extend(context.Background(), &custom, VideoExtendRequest{
			VideoCreateRequest: VideoCreateRequest{Model: OpenAIVideoModelSora2, Prompt: "Continue", Seconds: 8},
			SourceTask:         ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_source"},
		})
		require.NoError(t, err)
	})

	t.Run("edit existing source", func(t *testing.T) {
		provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			require.Equal(t, "/v1/videos/edits", req.URL.Path)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, map[string]any{"id": "video_source"}, payload["video"])
			return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_edited","status":"queued"}`, nil), nil
		}}, nil)
		task, err := provider.Edit(context.Background(), account, VideoEditRequest{
			VideoCreateRequest: VideoCreateRequest{Model: OpenAIVideoModelSora2, Prompt: "Change palette", Seconds: 8},
			SourceTask:         &ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_source"},
		}, nil)
		require.NoError(t, err)
		require.Equal(t, "video_edited", task.ProviderTaskID)
	})
}

func TestOpenAIVideoProviderErrorCertaintyAndSecretRedaction(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		certainty VideoSubmissionCertainty
		retryable bool
	}{
		{name: "validation rejection", status: 400, certainty: VideoSubmissionRejected},
		{name: "rate limit is unknown", status: 429, certainty: VideoSubmissionUnknown, retryable: true},
		{name: "server failure is unknown", status: 503, certainty: VideoSubmissionUnknown, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				return openAIVideoResponseForTest(test.status, `{"error":{"code":"upstream","message":"failure sk-video-secret"}}`, nil), nil
			}}, nil)
			_, err := provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
				Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
				Prompt: "test", Seconds: 8, Size: "1280x720",
			}, nil)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, test.certainty, providerErr.Certainty)
			require.Equal(t, test.retryable, providerErr.Retryable)
			require.NotContains(t, providerErr.Message, "sk-video-secret")
		})
	}
}

func TestOpenAIVideoProviderSubmissionErrorRedactsPromptAndURLQuery(t *testing.T) {
	prompt := "private prompt text"
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusBadRequest, `{"error":{"code":"invalid","message":"private prompt text at https://cdn.example/input.png?token=secret"}}`, nil), nil
	}}, nil)

	_, err := provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		Prompt: prompt, Seconds: 8, Size: "1280x720",
	}, nil)

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.NotContains(t, providerErr.Message, prompt)
	require.NotContains(t, providerErr.Message, "token=secret")
	require.Contains(t, providerErr.Message, "https://cdn.example/input.png")
}

func TestDecodeOpenAIVideoTaskPreservesSanitizedFailureDetails(t *testing.T) {
	task, err := decodeOpenAIVideoTask(strings.NewReader(`{
			"id":"video_failed","status":"failed","error":{"code":"content_policy","message":"Video rejected by the upstream content policy"}
		}`), false)

	require.NoError(t, err)
	require.Equal(t, "content_policy", task.ErrorCode)
	require.Equal(t, "Video rejected by the upstream content policy", task.ErrorMessage)
}

func TestDecodeOpenAIVideoTaskRedactsUnsafeFailureDetails(t *testing.T) {
	task, err := decodeOpenAIVideoTask(strings.NewReader(`{
			"id":"video_failed","status":"failed","error":{"code":"secret=value","message":"Bearer provider-secret"}
		}`), false)

	require.NoError(t, err)
	require.Equal(t, "upstream_error", task.ErrorCode)
	require.Equal(t, "OpenAI video generation failed", task.ErrorMessage)
}

func TestDecodeOpenAIVideoTaskRejectsOversizedIdentifier(t *testing.T) {
	_, err := decodeOpenAIVideoTask(strings.NewReader(`{"id":"`+strings.Repeat("x", 256)+`","status":"queued"}`), true)

	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, VideoSubmissionUnknown, providerErr.Certainty)
}

func TestDecodeOpenAIVideoTaskBoundsUnknownRawStatus(t *testing.T) {
	task, err := decodeOpenAIVideoTask(strings.NewReader(`{"id":"video_safe","status":"`+strings.Repeat("x", 65)+`"}`), false)

	require.NoError(t, err)
	require.Equal(t, "unknown", task.RawStatus)
	require.Equal(t, VideoGenerationInProgress, task.Status)
}

func TestOpenAIVideoProviderPreservesCompatibleUpstreamErrorShapes(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    string
		wantMessage string
	}{
		{name: "OpenAI envelope", body: `{"error":{"code":"invalid_value","message":"seconds is invalid"}}`, wantCode: "invalid_value", wantMessage: "seconds is invalid"},
		{name: "root fields", body: `{"code":"InvalidParameter","message":"size is unsupported"}`, wantCode: "InvalidParameter", wantMessage: "size is unsupported"},
		{name: "string error", body: `{"error":"model is unavailable"}`, wantMessage: "model is unavailable"},
		{name: "FastAPI detail", body: `{"detail":[{"loc":["body","seconds"],"msg":"Input should be a valid string","type":"string_type"}]}`, wantMessage: "body.seconds: Input should be a valid string"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				return openAIVideoResponseForTest(http.StatusBadRequest, test.body, nil), nil
			}}, nil)
			_, err := provider.Create(context.Background(), openAIVideoTestAccount(), VideoCreateRequest{
				Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
				Prompt: "test", Seconds: 8, Size: "1280x720",
			}, nil)
			var providerErr *VideoProviderError
			require.ErrorAs(t, err, &providerErr)
			require.Equal(t, test.wantCode, providerErr.Code)
			require.Equal(t, test.wantMessage, providerErr.Message)
		})
	}
}

func TestOpenAIVideoProviderGetAndContentRange(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/content") {
			require.Equal(t, "bytes=10-20", req.Header.Get("Range"))
			require.Equal(t, "thumbnail", req.URL.Query().Get("variant"))
			return openAIVideoResponseForTest(http.StatusPartialContent, "binary-video", http.Header{
				"Content-Type":  []string{"image/webp"},
				"Content-Range": []string{"bytes 10-20/100"},
			}), nil
		}
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_done","status":"completed","progress":100,"video_url":"https://cdn.example.test/v1/videos/video_done/content?token=signed","usage":{"video_tokens":123}}`, nil), nil
	}}, nil)
	account := openAIVideoTestAccount()
	task, err := provider.Get(context.Background(), account, ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_done"})
	require.NoError(t, err)
	require.Equal(t, VideoGenerationCompleted, task.Status)
	require.Equal(t, "https://cdn.example.test/v1/videos/video_done/content?token=signed", task.VideoURL)
	require.ElementsMatch(t, []string{"video", "thumbnail", "spritesheet"}, task.ContentVariants)

	content, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef: ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_done"},
		Variant: "thumbnail", Method: http.MethodGet, Range: "bytes=10-20",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, content.StatusCode)
	data, err := io.ReadAll(content.Body)
	require.NoError(t, err)
	require.NoError(t, content.Body.Close())
	require.Equal(t, []byte("binary-video"), data)
}

func TestOpenAIVideoProviderContentUsesCompleteVideoURLWithoutLeakingCredentials(t *testing.T) {
	account := openAIVideoTestAccount()
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		t.Fatal("external video_url must use the pinned content transport")
		return nil, nil
	}}, nil)
	address := netip.MustParseAddr("93.184.216.34")
	provider.resolver = videoCallbackResolverStub{addresses: []netip.Addr{address}}
	providerURL := "https://cdn.example.test/files/video.mp4?token=signed&disposition=inline"
	provider.redirect = func(_ context.Context, request *http.Request, _ *Account, addresses []netip.Addr) (*http.Response, error) {
		require.Equal(t, providerURL, request.URL.String())
		require.Equal(t, []netip.Addr{address}, addresses)
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		require.Equal(t, "bytes=0-3", request.Header.Get("Range"))
		return openAIVideoResponseForTest(http.StatusPartialContent, "data", http.Header{"Content-Type": {"video/mp4"}}), nil
	}

	content, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef:     ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_done"},
		UpstreamURL: providerURL, Variant: "video", Method: http.MethodGet, Range: "bytes=0-3",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, content.StatusCode)
	require.NoError(t, content.Body.Close())
}

func TestOpenAIVideoProviderHeadFallsBackToHeaderOnlyGet(t *testing.T) {
	methods := make([]string, 0, 2)
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodHead {
			return openAIVideoResponseForTest(http.StatusMethodNotAllowed, `{"error":{"message":"method not allowed"}}`, nil), nil
		}
		return openAIVideoResponseForTest(http.StatusOK, "binary-video", http.Header{"Content-Type": []string{"video/mp4"}}), nil
	}}, nil)
	account := openAIVideoTestAccount()

	content, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef: ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_done"},
		Variant: "video", Method: http.MethodHead,
	})

	require.NoError(t, err)
	require.Equal(t, []string{http.MethodHead, http.MethodGet}, methods)
	require.Equal(t, "video/mp4", content.Header.Get("Content-Type"))
	require.NoError(t, content.Body.Close())
}

func TestOpenAIVideoProviderContentFollowsValidatedRedirectWithoutCredentials(t *testing.T) {
	account := openAIVideoTestAccount()
	requests := make([]*http.Request, 0, 2)
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header = request.Header.Clone()
		requests = append(requests, clone)
		return openAIVideoResponseForTest(http.StatusFound, "", http.Header{"Location": []string{"https://cdn.example.test/signed/video.mp4?token=temporary"}}), nil
	}}, nil)
	resolvedAddress := netip.MustParseAddr("93.184.216.34")
	provider.resolver = videoCallbackResolverStub{addresses: []netip.Addr{resolvedAddress}}
	provider.redirect = func(_ context.Context, request *http.Request, _ *Account, addresses []netip.Addr) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header = request.Header.Clone()
		requests = append(requests, clone)
		require.Equal(t, []netip.Addr{resolvedAddress}, addresses)
		return openAIVideoResponseForTest(http.StatusPartialContent, "data", http.Header{"Content-Type": []string{"video/mp4"}}), nil
	}

	content, err := provider.OpenContent(context.Background(), account, ProviderContentRequest{
		TaskRef: ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_done"},
		Variant: "video", Method: http.MethodGet, Range: "bytes=0-3", IfRange: `"etag"`,
	})

	require.NoError(t, err)
	require.Len(t, requests, 2)
	require.Equal(t, "Bearer sk-video-secret", requests[0].Header.Get("Authorization"))
	require.Equal(t, "https://cdn.example.test/signed/video.mp4?token=temporary", requests[1].URL.String())
	require.Empty(t, requests[1].Header.Get("Authorization"))
	require.Empty(t, requests[1].Header.Get("Cookie"))
	require.Equal(t, "bytes=0-3", requests[1].Header.Get("Range"))
	require.Equal(t, `"etag"`, requests[1].Header.Get("If-Range"))
	require.NoError(t, content.Body.Close())
}

func TestPinnedVideoContentDialerConnectsToResolvedAddressNotHostname(t *testing.T) {
	var dialed []string
	dialer := &pinnedVideoContentDialer{
		targetHostname: "cdn.example.test",
		addresses:      []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			return nil, errors.New("stop before network access")
		},
	}

	connection, err := dialer.DialTLSContext(context.Background(), "tcp", "cdn.example.test:443")

	require.Error(t, err)
	require.Nil(t, connection)
	require.Equal(t, []string{"93.184.216.34:443"}, dialed)
}

func TestValidateVideoContentRedirectReturnsOnlyValidatedAddresses(t *testing.T) {
	address := netip.MustParseAddr("93.184.216.34")
	target, addresses, err := validateVideoContentRedirect(
		context.Background(),
		&url.URL{Scheme: "https", Host: "api.openai.com", Path: "/v1/videos/video_1/content"},
		"https://cdn.example.test/video.mp4",
		videoCallbackResolverStub{addresses: []netip.Addr{address}},
	)

	require.NoError(t, err)
	require.Equal(t, "https://cdn.example.test/video.mp4", target.String())
	require.Equal(t, []netip.Addr{address}, addresses)
}

func TestOpenAIVideoProviderContentRejectsPrivateRedirect(t *testing.T) {
	calls := 0
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		return openAIVideoResponseForTest(http.StatusFound, "", http.Header{"Location": []string{"https://127.0.0.1/internal"}}), nil
	}}, nil)

	content, err := provider.OpenContent(context.Background(), openAIVideoTestAccount(), ProviderContentRequest{
		TaskRef: ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: 11, ProviderTaskID: "video_done"},
		Variant: "video", Method: http.MethodGet,
	})

	require.Error(t, err)
	require.Nil(t, content)
	require.Equal(t, 1, calls)
}

func TestOpenAIVideoProviderContentAppliesResponseHeaderTimeout(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}}, nil)

	started := time.Now()
	content, err := provider.OpenContent(context.Background(), openAIVideoTestAccount(), ProviderContentRequest{
		TaskRef: ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: 11, ProviderTaskID: "video_done"},
		Variant: "video", Method: http.MethodGet, ResponseHeaderTimeout: 20 * time.Millisecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, content)
	require.Less(t, time.Since(started), time.Second)
}

func TestVideoContentHeaderTimeoutClosesLateResponseBody(t *testing.T) {
	release := make(chan struct{})
	closed := make(chan struct{}, 1)

	response, err := executeVideoContentWithHeaderTimeout(context.Background(), 20*time.Millisecond, func(context.Context) (*http.Response, error) {
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: &openAIVideoCloseTracker{
			Reader: strings.NewReader("late response"), closed: closed,
		}}, nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, response)

	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("late response body was not closed")
	}
}

func TestOpenAIVideoProviderVerifyWebhook(t *testing.T) {
	secretBytes := []byte("test-webhook-secret-32-bytes-value")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(secretBytes)
	account := openAIVideoTestAccount()
	account.Credentials[openAIVideoWebhookSecretKey] = secret
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_123","type":"video.completed","created_at":1788350400,"data":{"id":"video_upstream_1"}}`)
	id := "msg_123"
	timestamp := "1788350400"
	mac := hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(id + "." + timestamp + "." + string(body)))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{}, nil)
	provider.now = func() time.Time { return now }

	event, err := provider.VerifyWebhook(context.Background(), account, ProviderWebhookRequest{
		Headers: http.Header{
			"Webhook-Id":        []string{id},
			"Webhook-Timestamp": []string{timestamp},
			"Webhook-Signature": []string{"v1," + signature},
		},
		Body: body,
	})
	require.NoError(t, err)
	require.Equal(t, "evt_123", event.ProviderEventID)
	require.Equal(t, "video_upstream_1", event.ProviderTaskID)
	require.Equal(t, VideoGenerationCompleted, event.Status)

	account.Credentials[openAIVideoWebhookSecretKey] = "whsec_" + base64.StdEncoding.EncodeToString([]byte("replacement-webhook-secret-value"))
	account.Credentials[openAIVideoWebhookPreviousSecretKey] = secret
	event, err = provider.VerifyWebhook(context.Background(), account, ProviderWebhookRequest{
		Headers: http.Header{
			"Webhook-Id": []string{id}, "Webhook-Timestamp": []string{timestamp},
			"Webhook-Signature": []string{"v1," + signature},
		},
		Body: body,
	})
	require.NoError(t, err)
	require.Equal(t, "evt_123", event.ProviderEventID)

	_, err = provider.VerifyWebhook(context.Background(), account, ProviderWebhookRequest{
		Headers: http.Header{"Webhook-Id": []string{id}, "Webhook-Timestamp": []string{timestamp}, "Webhook-Signature": []string{"v1,bad"}},
		Body:    body,
	})
	require.Error(t, err)

	duplicateBody := []byte(`{"id":"evt_123","type":"video.completed","type":"video.failed","data":{"id":"video_upstream_1"}}`)
	mac = hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(id + "." + timestamp + "." + string(duplicateBody)))
	duplicateSignature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	_, err = provider.VerifyWebhook(context.Background(), account, ProviderWebhookRequest{
		Headers: http.Header{
			"Webhook-Id": []string{id}, "Webhook-Timestamp": []string{timestamp},
			"Webhook-Signature": []string{"v1," + duplicateSignature},
		},
		Body: duplicateBody,
	})
	require.Error(t, err)
}
