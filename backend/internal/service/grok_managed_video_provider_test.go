package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type grokManagedHTTPStub struct {
	before func()
	HTTPUpstream
	calls    int
	request  *http.Request
	body     []byte
	status   int
	response string
}

func (h *grokManagedHTTPStub) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if h.before != nil {
		h.before()
	}
	h.calls++
	h.request = r
	if r.Body != nil {
		h.body, _ = io.ReadAll(r.Body)
	}
	status := h.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(h.response))}, nil
}
func grokManagedTestProvider() (*GrokVideoProvider, *Account, *grokManagedHTTPStub) {
	h := &grokManagedHTTPStub{response: `{"request_id":"upstream-1"}`}
	return NewGrokVideoProvider(h, nil, nil), &Account{ID: 12, Platform: PlatformGrok, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-key"}}, h
}
func grokVerifiedSourceForTest(seconds float64) *GrokVerifiedVideoSource {
	raw := []byte("immutable-video")
	sum := sha256.Sum256(raw)
	return &GrokVerifiedVideoSource{verified: true, Seconds: seconds, Width: 1920, Height: 1080, Input: VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleSourceVideo, MIMEType: "video/mp4", Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}}
}
func TestGrokManagedProviderReferenceGeneration(t *testing.T) {
	p, a, h := grokManagedTestProvider()
	r := p.ApplyRequestDefaults(VideoCreateRequest{Model: "grok-imagine-video-1.5", RequestedModel: "business-alias", Operation: VideoOperationGenerate, Prompt: "A scene", Ratio: "9:16", Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleTaggedReferenceImage, URL: "https://example.com/ref.png"}}, Grok: &GrokVideoOptions{VoiceIDs: []string{"eve"}, Keyframes: []GrokVideoKeyframe{{TimestampSeconds: 3, Image: VideoContentItem{Type: VideoContentTypeImage, URL: "https://example.com/frame.png"}}}}})
	result, err := p.Create(context.Background(), a, r, nil)
	require.NoError(t, err)
	require.Equal(t, VideoGenerationQueued, result.Status)
	require.Equal(t, "upstream-1", result.ProviderTaskID)
	require.Equal(t, "/v1/videos/generations", h.request.URL.Path)
	var body map[string]any
	require.NoError(t, json.Unmarshal(h.body, &body))
	require.Equal(t, float64(8), body["duration"])
	require.Equal(t, true, body["generate_audio"])
	require.Equal(t, "9:16", body["aspect_ratio"])
	require.NotContains(t, body, "content")
	require.NotContains(t, body, "client_token")
	require.NotContains(t, body, "watermark")
	require.Len(t, body["reference_images"], 1)
	require.Len(t, body["keyframes"], 1)
	require.Len(t, body["reference_audios"], 1)
}
func TestGrokManagedProviderInvalidRequestsNeverSubmit(t *testing.T) {
	cases := []struct {
		name   string
		change func(*VideoCreateRequest)
	}{
		{"unknown model", func(r *VideoCreateRequest) { r.Model = "unknown" }},
		{"unsupported watermark", func(r *VideoCreateRequest) { b := false; r.Watermark = &b }},
		{"lite references", func(r *VideoCreateRequest) {
			r.Model = "grok-imagine-video-1.5-lite"
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleTaggedReferenceImage, URL: "https://example.com/ref.png"}}
		}},
		{"unsupported reference resolution", func(r *VideoCreateRequest) {
			r.Resolution = "1080p"
			r.Content = []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleTaggedReferenceImage, URL: "https://example.com/ref.png"}}
		}},
		{"duration too long", func(r *VideoCreateRequest) { r.Seconds = 16 }},
		{"unverified source", func(r *VideoCreateRequest) {
			r.Model = "grok-imagine-video"
			r.Operation = VideoOperationEdit
			r.Grok = &GrokVideoOptions{Source: grokVerifiedSourceForTest(8)}
			r.Grok.Source.verified = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, a, h := grokManagedTestProvider()
			r := VideoCreateRequest{Model: "grok-imagine-video-1.5", Operation: VideoOperationGenerate, Prompt: "test", Seconds: 8, Resolution: "480p"}
			tc.change(&r)
			_, err := p.Create(context.Background(), a, r, nil)
			require.Error(t, err)
			require.Zero(t, h.calls)
		})
	}
}
func TestGrokManagedProviderDerivedUsesFrozenBytes(t *testing.T) {
	for _, op := range []string{VideoOperationEdit, VideoOperationExtend} {
		t.Run(op, func(t *testing.T) {
			p, a, h := grokManagedTestProvider()
			r := VideoCreateRequest{Model: "grok-imagine-video", Operation: op, Prompt: "change scene", Grok: &GrokVideoOptions{Source: grokVerifiedSourceForTest(8.25)}}
			if op == VideoOperationExtend {
				r.Seconds = 6
			}
			resolved, spec, err := resolveGrokVideoExecutionSpec(r, a.ID, nil)
			require.NoError(t, err)
			if op == VideoOperationEdit {
				_, err = p.Edit(context.Background(), a, VideoEditRequest{VideoCreateRequest: resolved}, nil)
				require.Equal(t, 8.25, spec.BillableSeconds)
			} else {
				_, err = p.Extend(context.Background(), a, VideoExtendRequest{VideoCreateRequest: resolved})
				require.Equal(t, 14.25, spec.TotalSeconds)
			}
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(h.body, &body))
			require.NotContains(t, body, "resolution")
			require.NotContains(t, body, "aspect_ratio")
			require.Contains(t, body["video"].(map[string]any)["url"], "data:video/mp4;base64,")
			if op == VideoOperationEdit {
				require.NotContains(t, body, "duration")
			} else {
				require.Equal(t, float64(6), body["duration"])
			}
		})
	}
	p, a, h := grokManagedTestProvider()
	r := VideoCreateRequest{Model: "grok-imagine-video", Operation: VideoOperationEdit, Prompt: "edit", Grok: &GrokVideoOptions{Source: grokVerifiedSourceForTest(8)}}
	r.Grok.Source.Input.SHA256 = "wrong"
	_, err := p.Edit(context.Background(), a, VideoEditRequest{VideoCreateRequest: r}, nil)
	require.Error(t, err)
	require.Zero(t, h.calls)
}
func TestGrokManagedProviderSubmissionCertainty(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500, 502, 408, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			p, a, h := grokManagedTestProvider()
			h.status = status
			r := p.ApplyRequestDefaults(VideoCreateRequest{Model: "grok-imagine-video", Prompt: "test"})
			_, err := p.Create(context.Background(), a, r, nil)
			var pe *VideoProviderError
			require.True(t, errors.As(err, &pe))
			want := VideoSubmissionRejected
			if status >= 500 || status == 408 || status == 302 {
				want = VideoSubmissionUnknown
			}
			require.Equal(t, want, pe.Certainty)
			require.Equal(t, 1, h.calls)
		})
	}
}
func TestGrokManagedProviderStatusPreservesExtendedDuration(t *testing.T) {
	result, err := decodeGrokManagedTask([]byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/video.mp4","duration":18.25},"usage":{"cost_in_usd_ticks":6900000000}}`), "task")
	require.NoError(t, err)
	require.Equal(t, 18.25, result.Metadata["seconds"])
	require.Equal(t, VideoGenerationCompleted, result.Status)
	_, err = decodeGrokManagedTask([]byte(`{"status":"done"}`), "task")
	require.Error(t, err)
	result, err = decodeGrokManagedTask([]byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/video.mp4","respect_moderation":false}}`), "task")
	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, result.Status)
	require.Empty(t, result.VideoURL)
}

func TestGrokManagedProviderHonorsCatalogForMappedModel(t *testing.T) {
	provider, account, upstream := grokManagedTestProvider()
	document := DefaultVideoCapabilityCatalogDocument()
	caps := document.Providers[VideoProviderGrok]
	caps.SupportedSeconds["grok-imagine-video-1.5"] = []int{8}
	document.Providers[VideoProviderGrok] = caps
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	catalog := NewVideoCapabilityCatalog(&videoCapabilitySettingStub{value: string(raw)})
	require.NoError(t, catalog.Refresh(context.Background()))
	provider.catalog = catalog
	request := provider.ApplyRequestDefaults(VideoCreateRequest{RequestedModel: "business-alias", Model: "grok-imagine-video-1.5", Operation: VideoOperationGenerate, Prompt: "test", Seconds: 4})
	_, err = provider.Create(context.Background(), account, request, nil)
	require.Error(t, err)
	require.Zero(t, upstream.calls)
	request.Seconds = 8
	_, err = provider.Create(context.Background(), account, request, nil)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
}

func TestGrokManagedContentUsesBoundAccountHeaders(t *testing.T) {
	p, account, upstream := grokManagedTestProvider()
	account.Credentials[credKeyHeaderOverrideEnabled] = true
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"user-agent": "grok-test-proxy/1", "x-custom": "configured-header"}
	contentURL, err := buildGrokMediaURL(account, nil, GrokMediaEndpointVideoContent, "upstream-1")
	require.NoError(t, err)
	response, err := p.OpenContent(context.Background(), account, ProviderContentRequest{TaskRef: ProviderTaskRef{Provider: VideoProviderGrok, AccountID: account.ID, ProviderTaskID: "upstream-1"}, UpstreamURL: contentURL, Method: "GET", Range: "bytes=0-99"})
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, "Bearer test-key", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "grok-test-proxy/1", upstream.request.Header.Get("User-Agent"))
	require.Equal(t, "configured-header", getHeaderRaw(upstream.request.Header, "x-custom"))
	require.Equal(t, "bytes=0-99", upstream.request.Header.Get("Range"))
}
