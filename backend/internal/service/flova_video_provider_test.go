package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func flovaVideoTestAccount() *Account {
	return &Account{
		ID: 31, Platform: PlatformFlova, Type: AccountTypeAPIKey, Concurrency: 2,
		Credentials: map[string]any{"api_key": "flova-secret", "skill_id": "sk_test"},
	}
}

// flovaStubServer 按路径分发的上游桩：记录请求顺序、头与 body，便于断言协议形状。
type flovaStubServer struct {
	mu       sync.Mutex
	calls    []string
	requests map[string][]*http.Request
	bodies   map[string][]string
	handlers map[string]func(call int, req *http.Request, body string) *http.Response
	fail     map[string]error
}

func newFlovaStubServer() *flovaStubServer {
	return &flovaStubServer{requests: map[string][]*http.Request{}, bodies: map[string][]string{}, handlers: map[string]func(int, *http.Request, string) *http.Response{}, fail: map[string]error{}}
}

func (s *flovaStubServer) on(path string, handler func(call int, req *http.Request, body string) *http.Response) {
	s.handlers[path] = handler
}

func (s *flovaStubServer) json(path string, body string) {
	s.on(path, func(int, *http.Request, string) *http.Response {
		return openAIVideoResponseForTest(http.StatusOK, body, nil)
	})
}

func (s *flovaStubServer) upstream() HTTPUpstream {
	return &openAIVideoHTTPStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		path := req.URL.Path
		var body string
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		s.mu.Lock()
		s.calls = append(s.calls, path)
		s.requests[path] = append(s.requests[path], req)
		s.bodies[path] = append(s.bodies[path], body)
		call := len(s.requests[path])
		handler := s.handlers[path]
		failErr := s.fail[path]
		s.mu.Unlock()
		if failErr != nil {
			return nil, failErr
		}
		if handler == nil {
			return openAIVideoResponseForTest(http.StatusNotFound, `{"code":"404","message":"no stub for `+path+`"}`, nil), nil
		}
		return handler(call, req, body), nil
	}}
}

func (s *flovaStubServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests[path])
}

func (s *flovaStubServer) lastBody(path string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	bodies := s.bodies[path]
	if len(bodies) == 0 {
		return nil
	}
	var decoded map[string]any
	_ = json.Unmarshal([]byte(bodies[len(bodies)-1]), &decoded)
	return decoded
}

func (s *flovaStubServer) lastRequest(path string) *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := s.requests[path]
	if len(requests) == 0 {
		return nil
	}
	return requests[len(requests)-1]
}

func newFlovaTestProvider(stub *flovaStubServer) *FlovaVideoProvider {
	provider := NewFlovaVideoProvider(stub.upstream(), nil, config.GatewayFlovaConfig{
		UserAgent: "flova-cli/v0.0.10 (linux; amd64)", ClientVersion: "v0.0.10",
		CreditsReserveDefault: 60, MaxRunMinutesDefault: 60, PriceDriftToleranceDefault: 0.10,
	})
	provider.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	return provider
}

const flovaUserOK = `{"code":0,"message":"success","request_id":"r1","data":{"credits_balance":"2009.000000","email":"u@example.com","subscription_info":{"is_active":true,"lookup_key":"starter_month"},"upgrade_url":"https://www.flova.ai/pricing/","user_id":"u1"}}`

func flovaRunResultJSON(status string, terminal bool, extra string) string {
	return `{"code":0,"message":"success","request_id":"r2","data":{"status":"` + status + `","terminal":` + map[bool]string{true: "true", false: "false"}[terminal] + `,"project_id":"p1","stream_chat_id":"s1"` + extra + `}}`
}

func flovaTaskRef(id string) ProviderTaskRef {
	return ProviderTaskRef{Provider: VideoProviderFlova, AccountID: 31, ProviderTaskID: id}
}

func flovaSubmittedState(model, resolution string, seconds int, resource string) string {
	state := flovaTaskState{ProjectID: "p1", StreamChatID: "s1", ModelKey: model, Resolution: resolution, Seconds: seconds,
		SubmittedAt: time.Date(2026, 10, 7, 11, 50, 0, 0, time.UTC), ResourceID: resource}
	return state.encode()
}

func TestFlovaVideoProviderIdentityAndSupportsAccount(t *testing.T) {
	provider := newFlovaTestProvider(newFlovaStubServer())
	require.Equal(t, VideoProviderFlova, provider.Name())
	require.True(t, provider.SupportsAccount(flovaVideoTestAccount()))

	noSkill := flovaVideoTestAccount()
	delete(noSkill.Credentials, "skill_id")
	require.True(t, provider.SupportsAccount(noSkill), "skill_id is provisioned lazily on first submit")
	require.False(t, provider.SupportsAccount(byteDanceVideoTestAccount()))
	require.False(t, provider.SupportsAccount(&Account{Platform: PlatformFlova, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "x", "skill_id": "y"}}))

	low := flovaVideoTestAccount()
	low.Extra = map[string]any{FlovaExtraCredits: 12.0}
	require.False(t, provider.SupportsAccount(low), "credits snapshot below reserve excludes the account without a network call")
	low.Credentials["min_credits_reserve"] = 10
	require.True(t, provider.SupportsAccount(low))
}

func TestFlovaTaskIDRoundTrip(t *testing.T) {
	encoded := flovaSubmittedState("seedance-2.5", "1080p", 8, "")
	state, err := parseFlovaTaskID(encoded)
	require.NoError(t, err)
	require.Equal(t, "p1", state.ProjectID)
	require.Equal(t, "s1", state.StreamChatID)
	require.Equal(t, "flova/seedance-2.5", state.model())
	require.Equal(t, "1080p", state.Resolution)
	require.Equal(t, 8, state.Seconds)
	require.Equal(t, time.Date(2026, 10, 7, 11, 50, 0, 0, time.UTC), state.SubmittedAt)
	require.True(t, validVideoProviderIdentifier(encoded))
	state.ResourceID = "res1"
	again, err := parseFlovaTaskID(state.encode())
	require.NoError(t, err)
	require.Equal(t, "res1", again.ResourceID)
	_, err = parseFlovaTaskID("garbage")
	require.Error(t, err)
}

func TestFlovaVideoProviderCreateThreeSteps(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.on("/api/agent/v2/upload", func(_ int, req *http.Request, body string) *http.Response {
		mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(strings.NewReader(body), params["boundary"])
		part, err := reader.NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", part.FormName())
		require.Equal(t, "ref.png", part.FileName())
		require.Equal(t, "image/png", part.Header.Get("Content-Type"))
		data, _ := io.ReadAll(part)
		require.Equal(t, "PNGDATA", string(data))
		return openAIVideoResponseForTest(http.StatusOK, `{"code":0,"message":"ok","data":{"file_name":"ref.png","file_type":"image","file_url":"https://cdn.flova.ai/images/abc.png?sig=1","instant":true}}`, nil)
	})
	stub.json("/api/agent/v2/create", `{"code":0,"message":"success","data":{"project_id":"p1","project_url":"https://www.flova.ai/p/p1"}}`)
	stub.json("/api/agent/v2/chat", `{"code":0,"message":"success","data":{"agent_status":"processing","project_id":"p1","stream_chat_id":"s1","type":"chat_started"}}`)
	provider := newFlovaTestProvider(stub)
	account := flovaVideoTestAccount()

	inputs := []VideoInput{{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, FileName: "ref.png", MIMEType: "image/png", Size: 7},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("PNGDATA")), nil },
	}}
	task, err := provider.Create(context.Background(), account, VideoCreateRequest{
		TaskID: "task_abc", Model: FlovaVideoModelSeedance25, Prompt: "A red paper boat >>> drifting", Seconds: 5, Resolution: "1080p",
	}, inputs)
	require.NoError(t, err)
	require.Equal(t, []string{"/api/agent/v2/user", "/api/agent/v2/upload", "/api/agent/v2/create", "/api/agent/v2/chat"}, stub.calls)

	// 头：Bearer、UA、X-Request-Id。
	chatReq := stub.lastRequest("/api/agent/v2/chat")
	require.Equal(t, "Bearer flova-secret", chatReq.Header.Get("Authorization"))
	require.Equal(t, "flova-cli/v0.0.10 (linux; amd64)", chatReq.Header.Get("User-Agent"))
	require.Len(t, chatReq.Header.Get("X-Request-Id"), 32)

	create := stub.lastBody("/api/agent/v2/create")
	require.Equal(t, "sub2api-task_abc", create["name"])
	require.EqualValues(t, 0, create["project_type"])

	chat := stub.lastBody("/api/agent/v2/chat")
	require.Equal(t, "p1", chat["project_id"])
	extra := chat["extra_data"].(map[string]any)
	require.Equal(t, "sk_test", extra["skill_id"])
	require.Equal(t, "flova-cli", extra["client"])
	require.Equal(t, "v0.0.10", extra["client_version"])
	files := chat["files"].([]any)
	require.Len(t, files, 1)
	file := files[0].(map[string]any)
	require.Equal(t, "ref.png", file["name"])
	require.Equal(t, "image", file["type"])
	require.Equal(t, "https://cdn.flova.ai/images/abc.png?sig=1", file["url"])
	require.True(t, strings.HasPrefix(file["id"].(string), "file_"))
	content := chat["content"].(string)
	require.True(t, strings.HasPrefix(content, "`{\"resource_name\":\"ref.png\",\"type\":\"uploaded\"}` Generate exactly one video. Model: Seedance 2.5. Aspect ratio: 16:9. Resolution: 1080p. Duration: 5 seconds."), content)
	require.Contains(t, content, "Reference image: use the attached uploaded image")
	require.Contains(t, content, "<<<A red paper boat > > > drifting>>>", "delimiter inside the prompt must be neutralised")

	require.Equal(t, VideoGenerationQueued, task.Status)
	state, err := parseFlovaTaskID(task.ProviderTaskID)
	require.NoError(t, err)
	require.Equal(t, "p1", state.ProjectID)
	require.Equal(t, "s1", state.StreamChatID)
	require.Equal(t, "seedance-2.5", state.ModelKey)
	require.Equal(t, "1080p", state.Resolution)
	require.Equal(t, 5, state.Seconds)
	require.Equal(t, FlovaVideoModelSeedance25, task.Metadata["model"])
	require.Equal(t, "16:9", task.Metadata["ratio"])
	require.Equal(t, 5, task.Metadata["seconds"])
	require.NotEmpty(t, task.Metadata["instruction_sha256"])
}

func TestFlovaVideoProviderCreateProvisionsSkillWhenMissing(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.json("/api/agent/v2/skill/create", `{"code":0,"data":{"skill_id":"sk_lazy"}}`)
	stub.json("/api/agent/v2/create", `{"code":0,"data":{"project_id":"p1"}}`)
	stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s1"}}`)
	account := flovaVideoTestAccount()
	delete(account.Credentials, "skill_id")
	repo := newFlovaAccountRepoStub(account)
	provider := newFlovaTestProvider(stub)
	provider.accounts = repo
	task, err := provider.Create(context.Background(), account, VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"/api/agent/v2/user", "/api/agent/v2/skill/create", "/api/agent/v2/create", "/api/agent/v2/chat"}, stub.calls)
	require.Equal(t, "sk_lazy", stub.lastBody("/api/agent/v2/chat")["extra_data"].(map[string]any)["skill_id"])
	require.Equal(t, "sk_lazy", repo.updated.Credentials["skill_id"], "skill id is persisted on the account")
	require.Equal(t, "sk_lazy", task.Metadata["skill_id"])
}

func TestFlovaVideoProviderCreateRejectsLowCredits(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", `{"code":0,"message":"success","data":{"credits_balance":"40.000000","subscription_info":{}}}`)
	provider := newFlovaTestProvider(stub)
	_, err := provider.Create(context.Background(), flovaVideoTestAccount(), VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, videoInsufficientBalanceCode, typed.Code)
	require.Equal(t, VideoSubmissionRejected, typed.Certainty)
	require.Zero(t, stub.count("/api/agent/v2/create"), "no project is created when credits are short")
}

func TestFlovaVideoProviderValidation(t *testing.T) {
	provider := newFlovaTestProvider(newFlovaStubServer())
	account := flovaVideoTestAccount()
	cases := []struct {
		name    string
		request VideoCreateRequest
		inputs  []VideoInput
		code    string
	}{
		{"unknown model", VideoCreateRequest{Model: "flova/wan-2.6", Prompt: "x"}, nil, "unsupported_model"},
		{"kling needs image", VideoCreateRequest{Model: FlovaVideoModelKling30Omni, Prompt: "x", Seconds: 5, Resolution: "720p"}, nil, "missing_reference_image"},
		{"unsupported seconds", VideoCreateRequest{Model: FlovaVideoModelVeo31Fast, Prompt: "x", Seconds: 5, Resolution: "720p"}, nil, "unsupported_seconds"},
		{"unpriced resolution", VideoCreateRequest{Model: FlovaVideoModelSeedance20Fast, Prompt: "x", Seconds: 5, Resolution: "1080p"}, nil, "unsupported_size"},
		{"bad resolution", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 5, Resolution: "100p"}, nil, "unsupported_size"},
		{"bad ratio", VideoCreateRequest{Model: FlovaVideoModelVeo31Fast, Prompt: "x", Seconds: 4, Ratio: "1:1"}, nil, "unsupported_size"},
		{"url reference", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 5, Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://x/y.png"}}}, nil, "unsupported_reference_media"},
		{"last frame role", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 5, Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleLastFrame, Input: &VideoInput{}}}}, nil, "unsupported_reference_media"},
		{"openai size", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 5, Size: "1280x720"}, nil, "unsupported_option"},
		{"smart duration", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", SmartDuration: true}, nil, "unsupported_option"},
		{"frames", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Prompt: "x", Frames: 29}, nil, "unsupported_option"},
		{"missing prompt", VideoCreateRequest{Model: FlovaVideoModelSeedance25, Seconds: 5}, nil, "missing_prompt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := provider.ValidateSubmission(account, tc.request, tc.inputs)
			var typed *VideoProviderError
			require.ErrorAs(t, err, &typed)
			require.Equal(t, tc.code, typed.Code)
			require.Equal(t, VideoSubmissionRejected, typed.Certainty)
		})
	}
	require.NoError(t, provider.ValidateSubmission(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: "9:16"}, nil), "defaults fill seconds and resolution")
	audioOff := false
	require.NoError(t, provider.ValidateSubmission(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: ArkVideoRatioAdaptive, AudioEnabled: &audioOff}, nil), "adaptive falls back to 16:9 and generate_audio=false is a no-op")
}

func TestFlovaVideoProviderCreateChatTransportFailureIsUnknown(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.json("/api/agent/v2/create", `{"code":0,"data":{"project_id":"p1"}}`)
	stub.fail["/api/agent/v2/chat"] = errors.New("timeout")
	provider := newFlovaTestProvider(stub)
	_, err := provider.Create(context.Background(), flovaVideoTestAccount(), VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, VideoSubmissionUnknown, typed.Certainty)

	stub2 := newFlovaStubServer()
	stub2.json("/api/agent/v2/user", flovaUserOK)
	stub2.fail["/api/agent/v2/create"] = errors.New("timeout")
	_, err = newFlovaTestProvider(stub2).Create(context.Background(), flovaVideoTestAccount(), VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	require.ErrorAs(t, err, &typed)
	require.Equal(t, VideoSubmissionRejected, typed.Certainty, "nothing is spent before /chat")
}

func TestFlovaVideoProviderGetStateMachine(t *testing.T) {
	ref := flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, ""))
	video := `,"assistant_messages":[{"message_id":"m1","contents":[{"type":"text","text":"intent"}],"artifacts":[]},{"message_id":"m2","contents":[{"type":"text","text":"Generated one video."}],"artifacts":[{"resource_id":"res1","media_type":"video","artifact_role":"video_asset","status":"success","resource_url":"https://cdn.flova.ai/v/res1.mp4?Expires=1791966647&Signature=abc"}]}],"pending_actions":[]`
	cases := []struct {
		name     string
		body     string
		status   string
		code     string
		rawCheck string
	}{
		{"queued", flovaRunResultJSON("in_queue", false, ""), VideoGenerationQueued, "", "in_queue"},
		{"processing", flovaRunResultJSON("processing", false, ""), VideoGenerationInProgress, "", "processing"},
		{"completed with video", flovaRunResultJSON("completed", true, video), VideoGenerationCompleted, "", "completed"},
		{"completed without video", flovaRunResultJSON("completed", true, `,"assistant_messages":[{"message_id":"m1","contents":[{"type":"text","text":"No video was generated. Kling requires a reference image."}],"artifacts":[]}],"pending_actions":[]`), VideoGenerationFailed, flovaErrorNoVideoArtifact, "completed"},
		{"upstream failed", flovaRunResultJSON("failed", true, `,"assistant_messages":[]`), VideoGenerationFailed, "failed", "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newFlovaStubServer()
			stub.json("/api/agent/v2/run_result", tc.body)
			provider := newFlovaTestProvider(stub)
			task, err := provider.Get(context.Background(), flovaVideoTestAccount(), ref)
			require.NoError(t, err)
			require.Equal(t, tc.status, task.Status)
			require.Equal(t, tc.rawCheck, task.RawStatus)
			if tc.code != "" {
				require.Equal(t, tc.code, task.ErrorCode)
			}
			body := stub.lastBody("/api/agent/v2/run_result")
			require.Equal(t, "p1", body["project_id"])
			require.Equal(t, "s1", body["stream_chat_id"])
		})
	}

	t.Run("completed details", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, video))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, "https://cdn.flova.ai/v/res1.mp4?Expires=1791966647&Signature=abc", task.VideoURL)
		require.Equal(t, time.Unix(1791966647, 0).UTC(), *task.ContentExpiresAt)
		require.Equal(t, "signed_url", task.Access.Kind)
		require.Equal(t, "video_content", task.Access.Scope)
		require.Equal(t, 4, task.Metadata["seconds"])
		require.Equal(t, "res1", task.Metadata["resource_id"])
		require.Equal(t, "Generated one video.", task.Metadata["assistant_text"])
		state, err := parseFlovaTaskID(task.ProviderTaskID)
		require.NoError(t, err)
		require.Equal(t, "res1", state.ResourceID, "resource id is carried in the task id for later refreshes")
	})

	t.Run("no video keeps the assistant explanation", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, `,"assistant_messages":[{"message_id":"m1","contents":[{"type":"text","text":"No video was generated. Kling requires a reference image."}],"artifacts":[]}],"pending_actions":[]`))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Contains(t, task.ErrorMessage, "Kling requires a reference image")
	})

	t.Run("two videos keep the first", func(t *testing.T) {
		two := `,"assistant_messages":[{"message_id":"m2","contents":[],"artifacts":[{"resource_id":"res1","media_type":"video","status":"success","resource_url":"https://cdn.flova.ai/v/1.mp4"},{"resource_id":"res2","media_type":"video","status":"success","resource_url":"https://cdn.flova.ai/v/2.mp4"}]}],"pending_actions":[]`
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, two))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationCompleted, task.Status)
		require.Equal(t, "https://cdn.flova.ai/v/1.mp4", task.VideoURL)
		require.Equal(t, 1, task.Metadata["extra_artifacts"])
	})

	t.Run("run_result_not_ready keeps polling", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", `{"code":"run_result_not_ready","message":"Run is still processing","request_id":"r","data":null}`)
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status)
		require.Equal(t, "run_result_not_ready", task.RawStatus)
		require.Equal(t, flovaDefaultPollInterval, task.SuggestedPollInterval)
	})

	t.Run("unknown business code during polling is retryable", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", `{"code":"export_timeout","message":"wait","data":null}`)
		_, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		var typed *VideoProviderError
		require.ErrorAs(t, err, &typed)
		require.True(t, typed.Retryable, "unknown codes must not terminate a running task")

		stub.json("/api/agent/v2/run_result", `{"code":"timeline_not_ready","message":"wait","data":null}`)
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status, "*_not_ready codes mean the run is still going")

		stub.json("/api/agent/v2/run_result", `{"code":"insufficient_credits","message":"buy more","data":null}`)
		_, err = newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.ErrorAs(t, err, &typed)
		require.False(t, typed.Retryable, "credit exhaustion stays terminal")
	})

	t.Run("not ready still honours the run timeout", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", `{"code":"run_result_not_ready","message":"Run is still processing","data":null}`)
		provider := newFlovaTestProvider(stub)
		provider.now = func() time.Time { return time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC) }
		task, err := provider.Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, flovaErrorRunTimeout, task.ErrorCode)
	})

	t.Run("agent busy backs off", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", `{"code":"agent_busy","message":"another run is active","data":null}`)
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status)
		require.Equal(t, flovaBusyPollInterval, task.SuggestedPollInterval)
	})

	t.Run("run timeout", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("processing", false, ""))
		provider := newFlovaTestProvider(stub)
		provider.now = func() time.Time { return time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC) }
		task, err := provider.Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationFailed, task.Status)
		require.Equal(t, flovaErrorRunTimeout, task.ErrorCode)
	})

	t.Run("http 401 surfaces authentication", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.on("/api/agent/v2/run_result", func(int, *http.Request, string) *http.Response {
			return openAIVideoResponseForTest(http.StatusUnauthorized, `{"code":"401","message":"Invalid or expired API token"}`, nil)
		})
		_, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		var typed *VideoProviderError
		require.ErrorAs(t, err, &typed)
		require.Equal(t, "authentication", typed.Kind)
		require.NotContains(t, typed.Message, "flova-secret")
	})
}

func flovaHighCostAction(creditsPerSecond float64, resolution string, options string) string {
	return `,"completion_reason":"waiting_user","assistant_messages":[{"message_id":"m1","contents":[{"type":"text","text":"Confirm before credits are spent."}],"artifacts":[]}],"pending_actions":[{"action_id":"a1","type":"high_cost_model_confirm","blocking":true,"message":"Seedance 2.5 1080p requires confirmation before credits are spent.","payload":{"model_name":"Seedance 2.5","estimated_credits":` + jsonFloat(creditsPerSecond*4) + `,"items":[{"model_name":"Seedance 2.5","resolution":"` + resolution + `","credits_per_second":` + jsonFloat(creditsPerSecond) + `,"estimated_credits":` + jsonFloat(creditsPerSecond*4) + `,"video_count":1}]},"options":` + options + `,"resume_message_id":"rm1"}]`
}

func jsonFloat(v float64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

const flovaHighCostOptions = `[{"id":"once","action":"approve","effect":"resume","scope":"run"},{"id":"project","action":"approve","effect":"resume","scope":"project"},{"id":"lower_cost_720p","action":"select_alternative","effect":"resume"}]`

func TestFlovaVideoProviderHighCostConfirm(t *testing.T) {
	ref := flovaTaskRef(flovaSubmittedState("seedance-2.5", "1080p", 4, ""))

	t.Run("within tolerance resumes once", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
		stub.json("/api/agent/v2/chat", `{"code":0,"data":{"agent_status":"processing","project_id":"p1","stream_chat_id":"s2","type":"chat_started"}}`)
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status)
		resume := stub.lastBody("/api/agent/v2/chat")
		require.Equal(t, true, resume["is_resume"])
		require.Equal(t, "once", resume["option_id"])
		require.Equal(t, "a1", resume["action_id"])
		require.Equal(t, "rm1", resume["resume_message_id"])
		state, err := parseFlovaTaskID(task.ProviderTaskID)
		require.NoError(t, err)
		require.Equal(t, "s2", state.StreamChatID, "new stream_chat_id is written back")
		require.Equal(t, "s1", task.Metadata["previous_stream_chat_id"])
		require.EqualValues(t, 324, task.Metadata["auto_confirmed_credits"])
	})

	t.Run("price drift fails without resuming", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(120, "1080p", flovaHighCostOptions)))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationFailed, task.Status)
		require.Equal(t, flovaErrorPriceDrift, task.ErrorCode)
		require.Zero(t, stub.count("/api/agent/v2/chat"))
	})

	t.Run("resolution mismatch fails", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(26, "720p", flovaHighCostOptions)))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, flovaErrorPriceDrift, task.ErrorCode)
	})

	t.Run("disabled auto confirm fails", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
		account := flovaVideoTestAccount()
		account.Credentials["auto_confirm_high_cost"] = false
		task, err := newFlovaTestProvider(stub).Get(context.Background(), account, ref)
		require.NoError(t, err)
		require.Equal(t, flovaErrorPriceDrift, task.ErrorCode)
		require.Zero(t, stub.count("/api/agent/v2/chat"))
	})

	t.Run("subscription required is membership_required", func(t *testing.T) {
		body := `,"completion_reason":"waiting_user","assistant_messages":[],"pending_actions":[{"action_id":"a2","type":"subscription_required","blocking":true,"message":"The selected model or parameters require a subscription change.","action_url":"https://www.flova.ai/pricing/","options":[{"id":"open_subscription","effect":"open_url"},{"id":"completed","effect":"resume"}],"payload":{"media_type":"video","source":"model_permission_required"},"resume_message_id":"rm2"}]`
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, body))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationFailed, task.Status)
		require.Equal(t, flovaErrorMembershipRequired, task.ErrorCode)
		require.Contains(t, task.ErrorMessage, "subscription change")
		require.Zero(t, stub.count("/api/agent/v2/chat"))
	})

	t.Run("batch task limit honours the credits cap", func(t *testing.T) {
		body := `,"completion_reason":"waiting_user","assistant_messages":[],"pending_actions":[{"action_id":"a3","type":"batch_task_limit","blocking":true,"message":"This batch task requires confirmation before credits are spent.","options":[{"id":"confirm","effect":"resume"},{"id":"cancel","effect":"none"}],"payload":{"credits":120},"resume_message_id":"rm3"}]`
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, body))
		stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s3"}}`)
		provider := newFlovaTestProvider(stub)
		task, err := provider.Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, flovaErrorUserActionRequired, task.ErrorCode, "default cap is 0")

		account := flovaVideoTestAccount()
		account.Credentials["auto_confirm_credits_max"] = 200
		task, err = provider.Get(context.Background(), account, ref)
		require.NoError(t, err)
		require.Equal(t, VideoGenerationInProgress, task.Status)
		require.Equal(t, "confirm", stub.lastBody("/api/agent/v2/chat")["option_id"])
	})
}

func TestFlovaVideoProviderClassifyError(t *testing.T) {
	provider := newFlovaTestProvider(newFlovaStubServer())
	account := flovaVideoTestAccount()
	cases := []struct {
		name      string
		status    int
		body      string
		kind      string
		code      string
		retryable bool
	}{
		{"401", http.StatusUnauthorized, `{"code":"401","message":"Invalid or expired API token"}`, "authentication", "unauthorized", false},
		{"403", http.StatusForbidden, `{}`, "permission", "forbidden", false},
		{"429", http.StatusTooManyRequests, `{}`, "rate_limit", "rate_limited", true},
		{"500", http.StatusInternalServerError, `oops`, "transport", "upstream_unavailable", true},
		{"insufficient credits", http.StatusOK, `{"code":"insufficient_credits","message":"buy more","data":{"upgrade_url":"https://www.flova.ai/pricing/"}}`, "upstream", videoInsufficientBalanceCode, false},
		{"membership required", http.StatusOK, `{"code":"membership_required","message":"subscribe"}`, "upstream", videoInsufficientBalanceCode, false},
		{"agent busy", http.StatusOK, `{"code":"agent_busy","message":"busy"}`, "conflict", "agent_busy", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := openAIVideoResponseForTest(tc.status, tc.body, http.Header{"Retry-After": []string{"7"}})
			result, err := provider.readResult(response)
			require.NoError(t, err)
			typed := provider.classifyError(result, account, true)
			require.Equal(t, tc.kind, typed.Kind)
			require.Equal(t, tc.code, typed.Code)
			require.Equal(t, tc.retryable, typed.Retryable)
			if tc.retryable {
				require.Equal(t, VideoSubmissionUnknown, typed.Certainty)
				require.Equal(t, 7*time.Second, typed.RetryAfter)
			} else {
				require.Equal(t, VideoSubmissionRejected, typed.Certainty)
			}
		})
	}
}

func TestFlovaSignedURLExpiry(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	require.Equal(t, time.Unix(1791966647, 0).UTC(), flovaSignedURLExpiry("https://cdn.flova.ai/v.mp4?Expires=1791966647&Signature=x", now))
	require.Equal(t, time.Date(2026, 10, 7, 7, 6, 6, 0, time.UTC).Add(604800*time.Second), flovaSignedURLExpiry("https://cdn-water.flova.ai/v.mp4?X-Goog-Date=20261007T070606Z&X-Goog-Expires=604800&X-Goog-Signature=x", now))
	require.Equal(t, now.Add(flovaDefaultContentTTL), flovaSignedURLExpiry("https://cdn.flova.ai/v.mp4", now))
	require.Equal(t, now.Add(flovaDefaultContentTTL), flovaSignedURLExpiry("::not a url", now))
}

func TestFlovaVideoProviderOpenContentUsesUAAndRefreshesOn403(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/resource_info", `{"code":0,"data":{"project_id":"p1","type":"video","resource":{"resource_id":"res1","media_type":"video","status":"success","duration_seconds":4.06,"resource_url":"https://cdn.flova.ai/v/res1.mp4?Expires=1791966647&Signature=new"}}}`)
	provider := newFlovaTestProvider(stub)
	provider.resolver = videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	var seen []string
	provider.redirect = func(_ context.Context, request *http.Request, _ *Account, _ []netip.Addr) (*http.Response, error) {
		seen = append(seen, request.URL.String())
		require.Equal(t, "flova-cli/v0.0.10 (linux; amd64)", request.Header.Get("User-Agent"))
		require.Equal(t, "bytes=0-1", request.Header.Get("Range"))
		if strings.Contains(request.URL.RawQuery, "Signature=old") {
			return openAIVideoResponseForTest(http.StatusForbidden, "error code: 1010", nil), nil
		}
		return openAIVideoResponseForTest(http.StatusPartialContent, "mp", http.Header{"Content-Type": []string{"video/mp4"}}), nil
	}
	content, err := provider.OpenContent(context.Background(), flovaVideoTestAccount(), ProviderContentRequest{
		TaskRef: flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, "res1")), UpstreamURL: "https://cdn.flova.ai/v/res1.mp4?Expires=1&Signature=old", Range: "bytes=0-1",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, content.StatusCode)
	require.Len(t, seen, 2)
	require.Contains(t, seen[1], "Signature=new")
	require.Equal(t, 1, stub.count("/api/agent/v2/resource_info"))
	body := stub.lastBody("/api/agent/v2/resource_info")
	require.Equal(t, "p1", body["project_id"])
	require.Equal(t, "res1", body["resource_id"])
	_ = content.Body.Close()
}

func TestFlovaVideoProviderRefreshTaskAccessRequiresResource(t *testing.T) {
	provider := newFlovaTestProvider(newFlovaStubServer())
	_, err := provider.RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, "")))
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "content_unavailable", typed.Code)
}

func TestFlovaVideoProviderSkillAndUser(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.on("/api/agent/v2/skill/create", func(_ int, _ *http.Request, body string) *http.Response {
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &payload))
		require.Equal(t, flovaSkillName, payload["skill_name"])
		sections := payload["skill_content"].([]any)
		require.Len(t, sections, 2)
		first := sections[0].(map[string]any)
		require.Equal(t, "Role", first["section"])
		require.NotNil(t, first["config"])
		return openAIVideoResponseForTest(http.StatusOK, `{"code":0,"data":{"skill_id":"sk_new"}}`, nil)
	})
	stub.json("/api/agent/v2/skill/get", `{"code":0,"data":{"skill_id":"sk_new","skill_name":"x"}}`)
	provider := newFlovaTestProvider(stub)
	account := flovaVideoTestAccount()
	user, err := provider.FetchUser(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 2009.0, user.CreditsBalance)
	require.Equal(t, "starter_month", user.SubscriptionInfo["lookup_key"])
	skillID, err := provider.CreateSkill(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "sk_new", skillID)
	require.NoError(t, provider.GetSkill(context.Background(), account, "sk_new"))
}

func TestFlovaArkModelSpecAndCatalog(t *testing.T) {
	provider := newFlovaTestProvider(newFlovaStubServer())
	spec := provider.ArkModelSpec(FlovaVideoModelVeo31Fast)
	require.Equal(t, ArkVideoModelFlags{}, spec.Flags, "Flova exposes no Ark switches")
	require.Equal(t, map[VideoInputRole]ArkVideoContentRole{
		VideoInputRoleFirstFrame: {Max: 1, Accepts: []string{videoReferenceAcceptsDataURI}},
	}, spec.ContentRoles, "reference images must be bytes: Flova has no URL ingestion")
	require.Empty(t, spec.ProviderOptions)

	capabilities := DefaultFlovaVideoCapabilities()
	require.Equal(t, FlovaVideoModelSeedance25, capabilities.DefaultModel)
	require.Equal(t, "720p", capabilities.DefaultResolutions[FlovaVideoModelSeedance25])
	require.Equal(t, "720p", capabilities.DefaultResolutions[FlovaVideoModelMiniMaxH3], "MiniMax H3's 768p is the 720p tier in the Ark vocabulary")
	require.Equal(t, []string{"720p"}, capabilities.SupportedResolutions[FlovaVideoModelMiniMaxH3])
	require.Equal(t, "16:9", capabilities.DefaultRatios[FlovaVideoModelSeedance25])
	require.Equal(t, []string{"720p", "1080p"}, capabilities.SupportedResolutions[FlovaVideoModelSeedance25])
	require.Equal(t, []string{"720p"}, capabilities.SupportedResolutions[FlovaVideoModelSeedance20Fast])
	require.Contains(t, capabilities.SupportedRatios[FlovaVideoModelSeedance25], "21:9")
	require.Equal(t, []string{"16:9", "9:16"}, capabilities.SupportedRatios[FlovaVideoModelVeo31Fast])
	require.Empty(t, capabilities.SupportedSizes)
	require.Equal(t, 27, len(capabilities.SupportedSeconds[FlovaVideoModelSeedance25]))
	require.Equal(t, map[VideoInputRole]int{VideoInputRoleFirstFrame: 1}, capabilities.MaxContentByRole)
	require.Equal(t, flovaVideoModelIDs(), DefaultModelCatalogIDs(PlatformFlova))
	require.Equal(t, "1080p", flovaRequestResolution(VideoCreateRequest{Model: FlovaVideoModelSeedance25, Resolution: "1080p"}))
	require.Equal(t, "720p", flovaRequestResolution(VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3}))
	require.Equal(t, "720p", flovaRequestResolution(VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Resolution: "720p"}))
	credits768, ok := flovaCreditsPerSecond(FlovaVideoModelMiniMaxH3, "768p")
	require.True(t, ok)
	require.Equal(t, 12.0, credits768, "the credit table keeps Flova's native tier name")
	require.Equal(t, "", flovaRequestResolution(VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Resolution: "1080p"}), "an unsupported tier resolves to nothing rather than a guess")
	require.Equal(t, "", flovaRequestResolution(VideoCreateRequest{Model: "flova/none"}))
	credits, ok := flovaCreditsPerSecond(FlovaVideoModelSeedance20, "1080p")
	require.True(t, ok)
	require.Equal(t, 60.0, credits)
}

func TestFlovaResolveRendition(t *testing.T) {
	seedance := flovaModelProfileFor(FlovaVideoModelSeedance25)
	cases := []struct {
		name, ratio, resolution, wantRatio, wantResolution string
		wantErr                                            bool
	}{
		{name: "defaults", wantRatio: "16:9", wantResolution: "720p"},
		{name: "explicit", ratio: "9:16", resolution: "1080p", wantRatio: "9:16", wantResolution: "1080p"},
		{name: "adaptive follows the image so Flova gets 16:9", ratio: ArkVideoRatioAdaptive, wantRatio: "16:9", wantResolution: "720p"},
		{name: "case insensitive resolution", resolution: "1080P", wantRatio: "16:9", wantResolution: "1080p"},
		{name: "unknown resolution", resolution: "4k", wantErr: true},
		{name: "unknown ratio", ratio: "5:2", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ratio, resolution, err := flovaResolveRendition(seedance, tc.ratio, tc.resolution)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRatio, ratio)
			require.Equal(t, tc.wantResolution, resolution)
		})
	}
	// 没列 Ratios 的模型只接受 16:9 / 9:16。
	_, _, err := flovaResolveRendition(flovaModelProfileFor(FlovaVideoModelVeo31Fast), "1:1", "")
	require.Error(t, err)
	// Ark 词表的 720p 换回 MiniMax H3 的原生 768p，指令与积分表都认原生名。
	_, resolution, err := flovaResolveRendition(flovaModelProfileFor(FlovaVideoModelMiniMaxH3), "", "720p")
	require.NoError(t, err)
	require.Equal(t, "768p", resolution)
	_, _, err = flovaResolveRendition(nil, "", "")
	require.Error(t, err)
}

func TestFlovaRenderInstructionGolden(t *testing.T) {
	plan := &flovaPlan{profile: flovaModelProfileFor(FlovaVideoModelSeedance20Fast), ratio: "9:16", resolution: "720p", seconds: 6}
	content, sha := flovaRenderInstruction(plan, "  Line one\r\nLine `two` <<<x>>>  ", nil)
	require.Equal(t, "Generate exactly one video. Model: Seedance 2.0 Fast. Aspect ratio: 9:16. Resolution: 720p. Duration: 6 seconds.\n"+
		"Prompt (use verbatim, do not rewrite): <<<Line one\nLine 'two' < < <x> > >>>>\n"+
		"Rules for this round: produce exactly one video asset and nothing else. Do not write a script, storyboard, voiceover, music, timeline or export. "+
		"Do not generate images unless strictly required as the first frame of this single video. "+
		"Use the model, aspect ratio, resolution and duration exactly as given; if the model is unavailable to this account, stop and report the reason instead of substituting another model.", content)
	require.Len(t, sha, 64)
	_, sha2 := flovaRenderInstruction(plan, "different", nil)
	require.NotEqual(t, sha, sha2)
}
