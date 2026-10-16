package service

import (
	"context"
	"encoding/json"
	"io"
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
	require.True(t, provider.SupportsAccount(noSkill), "skill_id is not required to read legacy tasks")
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

func TestFlovaVideoProviderCreateRejectsUnverifiableBudgetBeforeSideEffects(t *testing.T) {
	for _, skillPresent := range []bool{true, false} {
		stub := newFlovaStubServer()
		provider := newFlovaTestProvider(stub)
		account := flovaVideoTestAccount()
		if !skillPresent {
			delete(account.Credentials, "skill_id")
		}
		// Legacy configuration must not opt back into unbounded spending.
		account.Credentials["auto_confirm_high_cost"] = true
		account.Credentials["auto_confirm_credits_max"] = 1000000
		opened := false
		inputs := []VideoInput{{
			VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, FileName: "ref.png", MIMEType: "image/png", Size: 7},
			Open: func(context.Context) (io.ReadCloser, error) {
				opened = true
				return io.NopCloser(strings.NewReader("PNGDATA")), nil
			},
		}}
		task, err := provider.Create(context.Background(), account, VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 5, Resolution: "720p"}, inputs)
		var typed *VideoProviderError
		require.ErrorAs(t, err, &typed)
		require.Equal(t, "budget_unverifiable", typed.Code)
		require.Equal(t, VideoSubmissionRejected, typed.Certainty)
		require.Nil(t, task)
		require.Empty(t, stub.calls)
		require.False(t, opened)
	}
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
	_, err := provider.plan(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: "9:16"}, nil)
	require.NoError(t, err, "defaults fill seconds and resolution")
	audioOff := false
	_, err = provider.plan(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: ArkVideoRatioAdaptive, AudioEnabled: &audioOff}, nil)
	require.NoError(t, err, "adaptive falls back to 16:9 and generate_audio=false is a no-op")

	// camera_fixed 不再拒绝：true 写进指令，false 不加提示。
	fixed, moving := true, false
	plan, err := provider.plan(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: "9:16", CameraFixed: &fixed}, nil)
	require.NoError(t, err)
	require.True(t, plan.cameraFixed)
	plan, err = provider.plan(account, VideoCreateRequest{Model: FlovaVideoModelMiniMaxH3, Prompt: "x", Ratio: "9:16", CameraFixed: &moving}, nil)
	require.NoError(t, err)
	require.False(t, plan.cameraFixed)
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
		require.NotContains(t, task.Metadata, "seconds", "requested duration is not a verified output measurement")
		require.Empty(t, task.Usage)
		require.Equal(t, "res1", task.Metadata["resource_id"])
		require.Equal(t, "Generated one video.", task.Metadata["assistant_text"])
		require.Equal(t, ref.ProviderTaskID, task.ProviderTaskID, "provider identity remains stable")
		require.Equal(t, "res1", task.Control.State.ResourceID, "resource is persisted in private recovery state")
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

func TestFlovaVideoProviderPendingActionsNeverResume(t *testing.T) {
	ref := flovaTaskRef(flovaSubmittedState("seedance-2.5", "1080p", 4, ""))
	for _, credits := range []string{`120`, `-1`, `"NaN"`, `"+Inf"`, `"bad"`, `null`} {
		stub := newFlovaStubServer()
		body := `,"completion_reason":"waiting_user","pending_actions":[{"action_id":"a1","type":"batch_task_limit","options":[{"id":"confirm","effect":"resume"}],"payload":{"credits":` + credits + `},"resume_message_id":"rm1"}]`
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, body))
		account := flovaVideoTestAccount()
		account.Credentials["auto_confirm_high_cost"] = true
		account.Credentials["auto_confirm_credits_max"] = 1000000
		provider := newFlovaTestProvider(stub)
		var key string
		for i := 0; i < 2; i++ {
			task, err := provider.Get(context.Background(), account, ref)
			require.NoError(t, err)
			require.Equal(t, VideoGenerationInProgress, task.Status)
			require.Equal(t, "flova_action_blocked", task.ErrorCode)
			require.Len(t, task.Control.BlockedActions, 1)
			require.True(t, task.Control.State.CostUncertain)
			if i == 0 {
				key = task.Control.BlockedActions[0].ActionKey
			} else {
				require.Equal(t, key, task.Control.BlockedActions[0].ActionKey)
			}
			if credits != `120` {
				require.Nil(t, task.Control.BlockedActions[0].QuotedCredits)
			}
		}
		require.Zero(t, stub.count("/api/agent/v2/chat"))
	}
	t.Run("high cost stays blocked even with a valid quote", func(t *testing.T) {
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
		task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
		require.NoError(t, err)
		require.Equal(t, "flova_action_blocked", task.ErrorCode)
		require.Zero(t, stub.count("/api/agent/v2/chat"))
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
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("processing", false, ""))
	provider := newFlovaTestProvider(stub)
	_, err := provider.RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, "")))
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "content_unavailable", typed.Code)
	require.Zero(t, stub.count("/api/agent/v2/resource_info"))
	require.Zero(t, stub.count("/api/agent/v2/chat"))
}

func TestFlovaVideoProviderRefreshLegacyResourceOnlyReadsOriginalRun(t *testing.T) {
	video := `,"assistant_messages":[{"artifacts":[{"resource_id":"res1","media_type":"video","status":"success","resource_url":"https://cdn.flova.ai/old.mp4"}]}]`
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"completed", flovaRunResultJSON("completed", true, video), true},
		{"different stream", strings.Replace(flovaRunResultJSON("completed", true, video), `"stream_chat_id":"s1"`, `"stream_chat_id":"s2"`, 1), false},
		{"invalid resource", flovaRunResultJSON("completed", true, strings.Replace(video, "res1", "invalid;resource", 1)), false},
		{"pending action", flovaRunResultJSON("running", false, flovaHighCostAction(81, "1080p", flovaHighCostOptions)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newFlovaStubServer()
			stub.json("/api/agent/v2/run_result", tc.body)
			stub.json("/api/agent/v2/resource_info", `{"code":0,"data":{"resource":{"resource_url":"https://cdn.flova.ai/fresh.mp4"}}}`)
			provider := newFlovaTestProvider(stub)
			ref := flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, ""))
			access, err := provider.RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), ref)
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, "https://cdn.flova.ai/fresh.mp4", access.Value)
				require.Equal(t, "res1", stub.lastBody("/api/agent/v2/resource_info")["resource_id"])
			} else {
				require.Error(t, err)
				require.Nil(t, access)
				require.Zero(t, stub.count("/api/agent/v2/resource_info"))
			}
			require.Equal(t, 1, stub.count("/api/agent/v2/run_result"))
			require.Equal(t, "p1", stub.lastBody("/api/agent/v2/run_result")["project_id"])
			require.Equal(t, "s1", stub.lastBody("/api/agent/v2/run_result")["stream_chat_id"])
			require.Zero(t, stub.count("/api/agent/v2/chat"))
		})
	}
}

func TestFlovaVideoProviderNonterminalPendingActionIsAudited(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("running", false, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
	observed, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaTaskRef(flovaSubmittedState("seedance-2.5", "1080p", 4, "")))
	require.NoError(t, err)
	require.Equal(t, "flova_action_blocked", observed.ErrorCode)
	require.Equal(t, VideoGenerationInProgress, observed.Status)
	require.Len(t, observed.Control.BlockedActions, 1)
	require.Zero(t, stub.count("/api/agent/v2/chat"))
}

func TestFlovaVideoProviderRejectsMismatchedPrivateRecoveryState(t *testing.T) {
	stub := newFlovaStubServer()
	ref := flovaTaskRef(flovaSubmittedState("seedance-2.5", "720p", 4, ""))
	state, err := parseFlovaTaskID(ref.ProviderTaskID)
	require.NoError(t, err)
	ref.ProviderExecutionState = flovaControlState(state)
	ref.ProviderExecutionState.StreamChatID = "other"
	_, err = newFlovaTestProvider(stub).RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), ref)
	require.Error(t, err)
	require.Empty(t, stub.calls, "mismatched private state cannot select another upstream run")
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
	require.Equal(t, ArkVideoModelFlags{CameraFixed: true}, spec.Flags, "Flova only realises camera_fixed, via its instruction")
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

func TestFlovaRenderInstructionCameraFixedHint(t *testing.T) {
	plan := &flovaPlan{profile: flovaModelProfileFor(FlovaVideoModelSeedance20Fast), ratio: "9:16", resolution: "720p", seconds: 6}
	plain, plainSHA := flovaRenderInstruction(plan, "a cat", nil)
	require.NotContains(t, plain, "Camera:")

	plan.cameraFixed = true
	fixed, fixedSHA := flovaRenderInstruction(plan, "a cat", nil)
	require.Contains(t, fixed, "Duration: 6 seconds.\nCamera: keep a fixed, static camera for the whole video (no pan, tilt, zoom, dolly or handheld movement).\nPrompt (use verbatim")
	require.NotEqual(t, plainSHA, fixedSHA, "the hint is part of the frozen instruction")
}
