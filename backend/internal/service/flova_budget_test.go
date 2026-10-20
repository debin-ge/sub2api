package service

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 带预算的任务：Seedance 2.5 1080p 4s → 81 × 4 × 1.1 = 356.4 积分预算，B0 = 2009。
func flovaBudgetedState(resource string) flovaTaskState {
	state, _ := parseFlovaTaskID(flovaSubmittedState("seedance-2.5", "1080p", 4, resource))
	state.BalanceBefore, state.BudgetCredits = 2009, 356.4
	return state
}

func flovaBudgetedRef(state flovaTaskState) ProviderTaskRef {
	ref := flovaTaskRef(state.encode())
	if state.ResumeActionKey != "" || state.ResumedStreamChatID != "" {
		ref.ProviderExecutionState = flovaControlState(state)
	}
	return ref
}

func flovaUserBalance(balance string) string {
	return `{"code":0,"message":"success","data":{"credits_balance":"` + balance + `","subscription_info":{"is_active":true}}}`
}

func TestFlovaVideoProviderCreateRecordsBalanceAndBudget(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.on("/api/agent/v2/upload", func(_ int, req *http.Request, body string) *http.Response {
		mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		part, err := multipart.NewReader(strings.NewReader(body), params["boundary"]).NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", part.FormName())
		data, _ := io.ReadAll(part)
		require.Equal(t, "PNGDATA", string(data))
		return openAIVideoResponseForTest(http.StatusOK, `{"code":0,"message":"ok","data":{"file_name":"ref.png","file_type":"image","file_url":"https://cdn.flova.ai/images/abc.png?sig=1","instant":true}}`, nil)
	})
	stub.json("/api/agent/v2/create", `{"code":0,"message":"success","data":{"project_id":"p1","project_url":"https://www.flova.ai/p/p1"}}`)
	stub.json("/api/agent/v2/chat", `{"code":0,"message":"success","data":{"agent_status":"processing","project_id":"p1","stream_chat_id":"s1","type":"chat_started"}}`)
	provider := newFlovaTestProvider(stub)
	inputs := []VideoInput{{
		VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, FileName: "ref.png", MIMEType: "image/png", Size: 7},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("PNGDATA")), nil },
	}}
	fixed := true
	task, err := provider.Create(context.Background(), flovaVideoTestAccount(), VideoCreateRequest{
		TaskID: "task_abc", Model: FlovaVideoModelSeedance25, Prompt: "A red paper boat", Seconds: 4, Resolution: "1080p", CameraFixed: &fixed,
	}, inputs)
	require.NoError(t, err)
	require.Equal(t, []string{"/api/agent/v2/user", "/api/agent/v2/upload", "/api/agent/v2/create", "/api/agent/v2/chat"}, stub.calls)
	require.Equal(t, "sub2api-task_abc", stub.lastBody("/api/agent/v2/create")["name"])
	content := stub.lastBody("/api/agent/v2/chat")["content"].(string)
	require.Contains(t, content, "visual reference only.\nCamera: keep a fixed, static camera")

	state, err := parseFlovaTaskID(task.ProviderTaskID)
	require.NoError(t, err)
	require.True(t, state.budgeted())
	require.Equal(t, 2009.0, state.BalanceBefore)
	require.InDelta(t, 81*4*1.1, state.BudgetCredits, 1e-9)
	require.Equal(t, VideoSpendingSerializedBalanceDelta, state.spendingPolicy())
	require.Equal(t, VideoGenerationQueued, task.Status)
	require.NotContains(t, task.Metadata, "seconds", "requested parameters are not output measurements")
}

func TestFlovaVideoProviderCreateRejectsBalanceBelowBudgetPlusReserve(t *testing.T) {
	// 预算 356.4 + 低水位 60 = 416.4 > 400：在任何上游副作用之前拒绝。
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserBalance("400.000000"))
	_, err := newFlovaTestProvider(stub).Create(context.Background(), flovaVideoTestAccount(),
		VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance25, Prompt: "x", Seconds: 4, Resolution: "1080p"}, nil)
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, videoInsufficientBalanceCode, typed.Code)
	require.Equal(t, VideoSubmissionRejected, typed.Certainty)
	require.Equal(t, []string{"/api/agent/v2/user"}, stub.calls)
}

// skill_id 可选：未配置时不创建 Skill、不改写账号，/chat 也不带 skill_id。
func TestFlovaVideoProviderCreateWithoutSkillNeverProvisionsOne(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.json("/api/agent/v2/create", `{"code":0,"data":{"project_id":"p1"}}`)
	stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s1"}}`)
	account := flovaVideoTestAccount()
	delete(account.Credentials, "skill_id")
	_, err := newFlovaTestProvider(stub).Create(context.Background(), account, VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"/api/agent/v2/user", "/api/agent/v2/create", "/api/agent/v2/chat"}, stub.calls)
	chat := stub.lastBody("/api/agent/v2/chat")
	require.NotContains(t, chat["extra_data"].(map[string]any), "skill_id")
	require.Contains(t, chat["content"].(string), "Do not ask clarifying questions", "the single-shot rules travel with every run")
	require.NotContains(t, account.Credentials, "skill_id")

	// 管理员配置了 skill_id 时照常带上。
	configured := newFlovaStubServer()
	configured.json("/api/agent/v2/user", flovaUserOK)
	configured.json("/api/agent/v2/create", `{"code":0,"data":{"project_id":"p1"}}`)
	configured.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s1"}}`)
	_, err = newFlovaTestProvider(configured).Create(context.Background(), flovaVideoTestAccount(), VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	require.NoError(t, err)
	require.Equal(t, "sk_test", configured.lastBody("/api/agent/v2/chat")["extra_data"].(map[string]any)["skill_id"])
}

func TestFlovaTaskIDBudgetRoundTripAndValidation(t *testing.T) {
	state := flovaBudgetedState("")
	again, err := parseFlovaTaskID(state.encode())
	require.NoError(t, err)
	require.Equal(t, state.BalanceBefore, again.BalanceBefore)
	require.Equal(t, state.BudgetCredits, again.BudgetCredits)
	require.True(t, validVideoProviderIdentifier(state.encode()))

	legacy := flovaSubmittedState("seedance-2.5", "1080p", 4, "")
	parsed, err := parseFlovaTaskID(legacy)
	require.NoError(t, err)
	require.False(t, parsed.budgeted(), "tasks without b/c keep the blocked policy")

	for _, bad := range []string{legacy + ";b=10", legacy + ";c=10", legacy + ";b=10;c=0", legacy + ";b=-1;c=10", legacy + ";b=NaN;c=10"} {
		_, err := parseFlovaTaskID(bad)
		require.Error(t, err, bad)
	}
}

func TestFlovaVideoProviderAuthorizesInBudgetConfirmationWithoutCallingChat(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK) // 尚未花费
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
	state := flovaBudgetedState("")
	task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(state))
	require.NoError(t, err)
	require.Equal(t, VideoGenerationInProgress, task.Status)
	require.Equal(t, "resume_authorized", task.RawStatus)
	require.NotNil(t, task.Control.ResumeIntent)
	require.Equal(t, "once", task.Control.ResumeIntent.OptionID)
	require.Equal(t, "a1", task.Control.ResumeIntent.ActionID)
	require.Equal(t, task.Control.ResumeIntent.ActionKey, task.Control.State.ResumeActionKey)
	require.Equal(t, VideoSpendingSerializedBalanceDelta, task.Control.State.SpendingPolicy)
	require.Zero(t, stub.count("/api/agent/v2/chat"), "Get only authorizes; the worker resumes after the authorization is persisted")
}

func TestFlovaVideoProviderBlocksConfirmationOutsideBudget(t *testing.T) {
	cases := map[string]struct {
		balance string
		action  string
		message string
	}{
		// 81 × 4 = 324 ≤ 356.4，但已花 100：324 + 100 > 356.4。
		"quote plus spend exceeds budget": {"1909.000000", flovaHighCostAction(81, "1080p", flovaHighCostOptions), "exceeds the task budget"},
		"price above tolerance":           {"2009.000000", flovaHighCostAction(95, "1080p", flovaHighCostOptions), "price table expects"},
		"wrong resolution":                {"2009.000000", flovaHighCostAction(81, "720p", flovaHighCostOptions), "differs from the requested"},
		"batch task limit":                {"2009.000000", `,"completion_reason":"waiting_user","pending_actions":[{"action_id":"a1","type":"batch_task_limit","options":[{"id":"confirm","effect":"resume"}],"payload":{"credits":10},"resume_message_id":"rm1"}]`, "never confirmed automatically"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newFlovaStubServer()
			stub.json("/api/agent/v2/user", flovaUserBalance(tc.balance))
			stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, tc.action))
			task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(flovaBudgetedState("")))
			require.NoError(t, err)
			require.Nil(t, task.Control.ResumeIntent)
			require.Empty(t, task.Control.State.ResumeActionKey)
			require.Equal(t, "flova_action_blocked", task.ErrorCode)
			require.Contains(t, task.ErrorMessage, tc.message)
			require.Len(t, task.Control.BlockedActions, 1)
			require.Equal(t, VideoSpendingExceedsTaskBudget, task.Control.BlockedActions[0].Reason)
			require.Zero(t, stub.count("/api/agent/v2/chat"))
		})
	}
}

func TestFlovaVideoProviderAllowsOnlyOneConfirmationPerTask(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
	state := flovaBudgetedState("")
	state.ResumeActionKey = strings.Repeat("ab", 32)
	task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(state))
	require.NoError(t, err)
	require.Nil(t, task.Control.ResumeIntent)
	require.Equal(t, "flova_resume_unconfirmed", task.ErrorCode)
	require.Equal(t, state.ResumeActionKey, task.Control.State.ResumeActionKey)
}

func TestFlovaVideoProviderSubscriptionGateFailsBudgetedTask(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, `,"completion_reason":"waiting_user","pending_actions":[{"action_id":"a1","type":"subscription_required","message":"Upgrade","options":[{"id":"up","effect":"open_url"}]}]`))
	task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(flovaBudgetedState("")))
	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, task.Status)
	require.Equal(t, flovaErrorMembershipRequired, task.ErrorCode)
}

func TestFlovaVideoProviderReportsInRunOverspend(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserBalance("1500.000000")) // 已花 509 > 356.4
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("processing", false, ""))
	task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(flovaBudgetedState("")))
	require.NoError(t, err)
	require.True(t, task.Control.Overspent)
	require.InDelta(t, 509, *task.Control.SpentCredits, 1e-9)

	// 旧任务没有 B0：不读余额，也不判超支。
	legacy := newFlovaStubServer()
	legacy.json("/api/agent/v2/run_result", flovaRunResultJSON("processing", false, ""))
	task, err = newFlovaTestProvider(legacy).Get(context.Background(), flovaVideoTestAccount(), flovaTaskRef(flovaSubmittedState("seedance-2.5", "1080p", 4, "")))
	require.NoError(t, err)
	require.False(t, task.Control.Overspent)
	require.Zero(t, legacy.count("/api/agent/v2/user"))
}

func TestFlovaVideoProviderResumeAuthorized(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s2"}}`)
	state := flovaBudgetedState("")
	state.ResumeActionKey = strings.Repeat("cd", 32)
	intent := VideoProviderResumeIntent{ActionKey: state.ResumeActionKey, ActionID: "a1", ResumeMessageID: "rm1", OptionID: "once"}
	provider := newFlovaTestProvider(stub)

	stream, err := provider.ResumeAuthorized(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(state), intent)
	require.NoError(t, err)
	require.Equal(t, "s2", stream)
	body := stub.lastBody("/api/agent/v2/chat")
	require.Equal(t, true, body["is_resume"])
	require.Equal(t, "a1", body["action_id"])
	require.Equal(t, "once", body["option_id"])
	require.Equal(t, "rm1", body["resume_message_id"])

	// 授权不匹配、已 resume 过、旧任务：都不调用上游。
	other := intent
	other.ActionKey = strings.Repeat("ef", 32)
	_, err = provider.ResumeAuthorized(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(state), other)
	require.Error(t, err)
	resumed := state
	resumed.ResumedStreamChatID = "s2"
	_, err = provider.ResumeAuthorized(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(resumed), intent)
	require.Error(t, err)
	require.Equal(t, 1, stub.count("/api/agent/v2/chat"))
}

func TestFlovaVideoProviderPollsResumedStream(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	stub.on("/api/agent/v2/run_result", func(_ int, _ *http.Request, body string) *http.Response {
		require.Contains(t, body, `"stream_chat_id":"s2"`)
		return openAIVideoResponseForTest(http.StatusOK, `{"code":0,"data":{"status":"processing","terminal":false,"project_id":"p1","stream_chat_id":"s2"}}`, nil)
	})
	state := flovaBudgetedState("")
	state.ResumeActionKey, state.ResumedStreamChatID = strings.Repeat("ab", 32), "s2"
	task, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), flovaBudgetedRef(state))
	require.NoError(t, err)
	require.Equal(t, VideoGenerationInProgress, task.Status)
	require.Equal(t, "s2", task.Control.State.ResumedStreamChatID)
	require.Equal(t, flovaBudgetedRef(state).ProviderTaskID, task.ProviderTaskID, "the provider identity never changes")
}

func TestValidateFlovaControlTransitionResumeRules(t *testing.T) {
	state := flovaBudgetedState("")
	id := state.encode()
	task := &VideoTask{Provider: VideoProviderFlova, ProviderTaskID: &id}
	key := strings.Repeat("ab", 32)

	authorize := flovaControlState(state)
	authorize.ResumeActionKey = key
	require.NoError(t, ValidateVideoProviderControlTransition(task, id, authorize, nil))

	both := *authorize
	both.ResumedStreamChatID = "s2"
	require.Error(t, ValidateVideoProviderControlTransition(task, id, &both, nil), "the resumed stream cannot be written with the authorization")

	task.ProviderExecutionState = authorize
	require.NoError(t, ValidateVideoProviderControlTransition(task, id, &both, nil))
	changed := *authorize
	changed.ResumeActionKey = strings.Repeat("cd", 32)
	require.Error(t, ValidateVideoProviderControlTransition(task, id, &changed, nil), "an authorization is immutable")

	task.ProviderExecutionState = &both
	replaced := both
	replaced.ResumedStreamChatID = "s3"
	require.Error(t, ValidateVideoProviderControlTransition(task, id, &replaced, nil), "the resumed stream is written once")

	legacyID := flovaSubmittedState("seedance-2.5", "1080p", 4, "")
	legacyTask := &VideoTask{Provider: VideoProviderFlova, ProviderTaskID: &legacyID}
	legacyState, _ := parseFlovaTaskID(legacyID)
	legacyAuthorize := flovaControlState(legacyState)
	legacyAuthorize.ResumeActionKey = key
	require.Error(t, ValidateVideoProviderControlTransition(legacyTask, legacyID, legacyAuthorize, nil), "legacy tasks can never resume")
}

func flovaResumeWorkerFixture(t *testing.T, stub *flovaStubServer) (*VideoTaskWorker, *videoTaskRepoStub, *FlovaVideoProvider, *VideoTask, *ProviderVideoTask) {
	t.Helper()
	svc, tasks, _ := newVideoTaskServiceForTest(&videoProviderStub{}, videoGroupForTest(SubscriptionTypeStandard), nil)
	provider := newFlovaTestProvider(stub)
	state := flovaBudgetedState("")
	id := state.encode()
	accountID := int64(31)
	before := &VideoTask{ID: 9, PublicID: "video_flova", Provider: VideoProviderFlova, AccountID: &accountID, ProviderTaskID: &id,
		GenerationState: VideoGenerationInProgress, BillingState: VideoBillingHeld, ProviderExecutionState: flovaControlState(state)}
	persisted := *before
	authorized := flovaControlState(state)
	authorized.ResumeActionKey = strings.Repeat("ab", 32)
	persisted.ProviderExecutionState = authorized
	tasks.task = &persisted
	observed := &ProviderVideoTask{ProviderTaskID: id, Status: VideoGenerationInProgress, Control: &VideoProviderControlObservation{
		State:        authorized,
		ResumeIntent: &VideoProviderResumeIntent{ActionKey: authorized.ResumeActionKey, ActionID: "a1", ResumeMessageID: "rm1", OptionID: "once"},
	}}
	worker := &VideoTaskWorker{tasks: tasks, service: svc, cfg: svc.cfg}
	return worker, tasks, provider, before, observed
}

func TestVideoTaskWorkerResumesAuthorizedActionExactlyOnce(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s2"}}`)
	worker, tasks, provider, before, observed := flovaResumeWorkerFixture(t, stub)
	account := flovaVideoTestAccount()

	require.NoError(t, worker.resumeAuthorizedAction(context.Background(), before, tasks.task, account, provider, observed))
	require.Equal(t, 1, stub.count("/api/agent/v2/chat"))
	require.Equal(t, "s2", tasks.task.ProviderExecutionState.ResumedStreamChatID)

	// 授权已落库（before 已带 key）或已 resume：再观测到同一意图也不会再调用。
	afterAuthorization := *tasks.task
	require.NoError(t, worker.resumeAuthorizedAction(context.Background(), &afterAuthorization, tasks.task, account, provider, observed))
	require.Equal(t, 1, stub.count("/api/agent/v2/chat"))
}

func TestVideoTaskWorkerDoesNotRetryFailedResume(t *testing.T) {
	stub := newFlovaStubServer()
	stub.fail["/api/agent/v2/chat"] = io.ErrUnexpectedEOF
	worker, tasks, provider, before, observed := flovaResumeWorkerFixture(t, stub)
	account := flovaVideoTestAccount()

	require.NoError(t, worker.resumeAuthorizedAction(context.Background(), before, tasks.task, account, provider, observed))
	require.Equal(t, 1, stub.count("/api/agent/v2/chat"))
	require.Empty(t, tasks.task.ProviderExecutionState.ResumedStreamChatID)
	require.NotEmpty(t, tasks.events, "the failed attempt is recorded")

	// 下一轮轮询：授权已在库里，结果未知也不重放。
	persisted := *tasks.task
	require.NoError(t, worker.resumeAuthorizedAction(context.Background(), &persisted, tasks.task, account, provider, observed))
	require.Equal(t, 1, stub.count("/api/agent/v2/chat"))
}

func TestVideoTaskWorkerTripsOverspendBreaker(t *testing.T) {
	account := flovaSchedulableAccount()
	account.Extra = map[string]any{FlovaExtraEmptyStreak: 2.0}
	repo := newFlovaAccountRepoStub(account)
	worker := &VideoTaskWorker{accounts: repo}
	spent := 509.0
	task := &VideoTask{PublicID: "video_flova", Provider: VideoProviderFlova}
	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{
		Status: VideoGenerationInProgress, Control: &VideoProviderControlObservation{Overspent: true, SpentCredits: &spent},
	})
	require.Equal(t, account.ID, repo.pausedID)
	require.True(t, strings.HasPrefix(repo.pausedWhy, flovaOverspendPauseReasonPrefix))
	require.WithinDuration(t, worker.now().Add(flovaOverspendPauseCooldown), repo.pausedUntil, time.Minute)

	// 熔断期间空完成暂停（15 分钟）不会覆盖它。
	repo.pausedID = 0
	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{Status: VideoGenerationFailed, ErrorCode: flovaErrorNoVideoArtifact})
	require.Zero(t, repo.pausedID)
}

type flovaCostAutoRepo struct {
	costObservationRepo
	balances    []*float64
	changed     []bool
	reasons     []string
	nexts       []*time.Time
	resolutions []string
	amounts     []float64
	currency    string
	evidence    string
	key         string
}

func (r *flovaCostAutoRepo) RecordVideoProviderCostBalance(_ context.Context, _ *VideoProviderCost, _ string, balance *float64, changed bool, reason string, next *time.Time) error {
	r.balances, r.changed, r.reasons, r.nexts = append(r.balances, balance), append(r.changed, changed), append(r.reasons, reason), append(r.nexts, next)
	return nil
}

func (r *flovaCostAutoRepo) AutoResolveVideoProviderCost(_ context.Context, _ *VideoProviderCost, _, state string, amount float64, currency, evidence, key string) error {
	r.resolutions, r.amounts = append(r.resolutions, state), append(r.amounts, amount)
	r.currency, r.evidence, r.key = currency, evidence, key
	return nil
}

func flovaCostItem(createdAgo time.Duration, observed *float64, observedAgo time.Duration) *VideoProviderCost {
	identity := int64(0)
	now := time.Now().UTC()
	item := &VideoProviderCost{ID: 1, Version: 1, TaskPublicID: "video_flova", AccountID: 31, Provider: VideoProviderFlova,
		Reason: "flova_spend_unverified", ProviderTaskID: flovaBudgetedState("").encode(), AccountIdentityVersion: &identity,
		CreatedAt: now.Add(-createdAgo), ObservedBalance: observed}
	if observed != nil {
		at := now.Add(-observedAgo)
		item.ObservedBalanceAt = &at
	}
	return item
}

func runFlovaCostReconcile(t *testing.T, item *VideoProviderCost, balance string) (*flovaCostAutoRepo, *flovaAccountRepoStub, *flovaStubServer) {
	t.Helper()
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserBalance(balance))
	repo := &flovaCostAutoRepo{costObservationRepo: costObservationRepo{item: item}}
	account := flovaSchedulableAccount()
	accounts := newFlovaAccountRepoStub(account)
	worker := &VideoTaskWorker{tasks: repo, accounts: accounts, workerID: "cost-worker",
		providers: NewVideoProviderRegistry(newFlovaTestProvider(stub)), cfg: &config.Config{}}
	require.NoError(t, worker.reconcileProviderCosts(context.Background(), 4))
	return repo, accounts, stub
}

func TestFlovaBalanceDeltaWaitsForSettlementWindow(t *testing.T) {
	repo, _, stub := runFlovaCostReconcile(t, flovaCostItem(time.Minute, nil, 0), "1700")
	require.Equal(t, []string{"awaiting_settlement"}, repo.reasons)
	require.NotNil(t, repo.nexts[0])
	require.Zero(t, stub.count("/api/agent/v2/user"), "no balance read before the window")
	require.Empty(t, repo.resolutions)
}

func TestFlovaBalanceDeltaRequiresTwoEqualReadings(t *testing.T) {
	repo, _, _ := runFlovaCostReconcile(t, flovaCostItem(11*time.Minute, nil, 0), "1700")
	require.Equal(t, []string{"awaiting_stable_balance"}, repo.reasons)
	require.True(t, repo.changed[0])
	require.Equal(t, 1700.0, *repo.balances[0])
	require.Empty(t, repo.resolutions)

	// 同一读数但间隔不足 2 分钟：继续等，不重置观测时间。
	previous := 1700.0
	repo, _, _ = runFlovaCostReconcile(t, flovaCostItem(12*time.Minute, &previous, 30*time.Second), "1700")
	require.Equal(t, []string{"awaiting_stable_balance"}, repo.reasons)
	require.False(t, repo.changed[0])
	require.Empty(t, repo.resolutions)

	// 读数变了：以新读数重新计时。
	repo, _, _ = runFlovaCostReconcile(t, flovaCostItem(15*time.Minute, &previous, 3*time.Minute), "1690")
	require.True(t, repo.changed[0])
	require.Empty(t, repo.resolutions)
}

func TestFlovaBalanceDeltaRetainsUnverifiedSettlement(t *testing.T) {
	previous := 1700.0
	repo, accounts, _ := runFlovaCostReconcile(t, flovaCostItem(15*time.Minute, &previous, 3*time.Minute), "1700")
	require.Empty(t, repo.resolutions)
	require.Equal(t, []string{"final_settlement_unverified"}, repo.reasons)
	require.Nil(t, repo.nexts[0], "manual final-settlement evidence is required")
	require.Zero(t, accounts.pausedID, "309 is within the 356.4 budget")
	require.Zero(t, repo.observed, "the generic read-only lookup is not used")

	unchanged := 2009.0
	repo, _, _ = runFlovaCostReconcile(t, flovaCostItem(15*time.Minute, &unchanged, 3*time.Minute), "2009")
	require.Empty(t, repo.resolutions, "a stable zero delta is not proof of no charge")
	require.Equal(t, []string{"final_settlement_unverified"}, repo.reasons)
	require.Nil(t, repo.nexts[0])

	// A deduction can arrive after stable observations. It must never retroactively
	// make an earlier task eligible for automated cost closure.
	repo, _, _ = runFlovaCostReconcile(t, flovaCostItem(30*time.Minute, &unchanged, 18*time.Minute), "1700")
	require.Empty(t, repo.resolutions)
	require.Equal(t, []string{"awaiting_stable_balance"}, repo.reasons)
}

func TestFlovaBalanceDeltaOverspendTripsBreaker(t *testing.T) {
	previous := 1500.0
	repo, accounts, _ := runFlovaCostReconcile(t, flovaCostItem(15*time.Minute, &previous, 3*time.Minute), "1500")
	require.Empty(t, repo.resolutions)
	require.Equal(t, []string{"final_settlement_unverified"}, repo.reasons)
	require.Equal(t, int64(31), accounts.pausedID)
	require.True(t, strings.HasPrefix(accounts.pausedWhy, flovaOverspendPauseReasonPrefix))
}

func TestFlovaBalanceDeltaStopsWhenUnverifiable(t *testing.T) {
	// 余额上升：差额无法归因。
	previous := 2100.0
	repo, _, _ := runFlovaCostReconcile(t, flovaCostItem(15*time.Minute, &previous, 3*time.Minute), "2100")
	require.Equal(t, []string{"balance_increased"}, repo.reasons)
	require.Nil(t, repo.nexts[0], "left for an operator")
	require.Empty(t, repo.resolutions)

	// 超过最长等待仍不稳定。
	repo, _, _ = runFlovaCostReconcile(t, flovaCostItem(3*time.Hour, &previous, time.Minute), "1900")
	require.Equal(t, []string{"balance_unstable"}, repo.reasons)
	require.Nil(t, repo.nexts[0])

	// 账号身份变化。
	item := flovaCostItem(15*time.Minute, nil, 0)
	other := int64(7)
	item.AccountIdentityVersion = &other
	repo, _, stub := runFlovaCostReconcile(t, item, "1700")
	require.Equal(t, []string{"account_identity_unverified"}, repo.reasons)
	require.Zero(t, stub.count("/api/agent/v2/user"))
}

func TestFlovaBalanceDeltaLeavesLegacyTasksToTheReadOnlyPath(t *testing.T) {
	item := flovaCostItem(15*time.Minute, nil, 0)
	item.ProviderTaskID = flovaSubmittedState("seedance-2.5", "1080p", 4, "")
	repo, _, _ := runFlovaCostReconcile(t, item, "1700")
	require.Empty(t, repo.reasons)
	require.Empty(t, repo.resolutions)
	require.Equal(t, 1, repo.observed, "legacy tasks keep the evidence-only lookup")
}
