package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFlovaControlPersistsResourceThroughReconciliation(t *testing.T) {
	id := flovaSubmittedState("seedance-2.5", "720p", 4, "")
	state, err := parseFlovaTaskID(id)
	require.NoError(t, err)
	state.ResourceID = "res1"
	accountID := int64(31)
	task := &VideoTask{ID: 1, PublicID: NewVideoTaskID(), Provider: VideoProviderFlova, AccountID: &accountID,
		ProviderTaskID: &id, GenerationState: VideoGenerationInProgress, BillingState: VideoBillingHeld,
		EstimatedUnits: floatPointer(4), HoldAmount: floatPointer(2)}
	svc, repo, _ := newVideoTaskServiceForTest(&videoProviderStub{}, nil, nil)
	repo.task = task
	observed := &ProviderVideoTask{ProviderTaskID: id, Status: VideoGenerationCompleted,
		Control:  &VideoProviderControlObservation{State: flovaControlState(state)},
		VideoURL: "https://cdn.flova.ai/video.mp4", ContentVariants: []string{"video"}}
	_, err = svc.ReconcileProviderObservation(context.Background(), task, observed, "provider_polled")
	require.NoError(t, err)
	require.Len(t, repo.transitions, 1)
	write := repo.transitions[0]
	require.Equal(t, id, *write.ProviderTaskID)
	require.Equal(t, "res1", write.ProviderExecutionState.ResourceID)
	require.NotContains(t, write.ResponseMetadata, "resource_id", "internal state must not leak through public metadata")
	require.Equal(t, VideoBillingCapturePending, write.BillingState)
	require.Equal(t, 2.0, *write.ActualCost, "unverified legacy output cannot increase the authorized customer quote")

	// Reload the exact reference destined for storage before exercising refresh.
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/resource_info", `{"code":0,"data":{"resource":{"resource_url":"https://cdn.flova.ai/fresh.mp4"}}}`)
	ref := flovaTaskRef(*write.ProviderTaskID)
	ref.ProviderExecutionState = write.ProviderExecutionState
	_, err = newFlovaTestProvider(stub).RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), ref)
	require.NoError(t, err)
	require.Equal(t, "res1", stub.lastBody("/api/agent/v2/resource_info")["resource_id"])
}

func TestFlovaControlRejectsIdentityAndFrozenSpecificationChanges(t *testing.T) {
	id := flovaSubmittedState("seedance-2.5", "720p", 4, "")
	task := &VideoTask{Provider: VideoProviderFlova, ProviderTaskID: &id}
	baseline, err := parseFlovaTaskID(id)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*flovaTaskState){
		"project":            func(s *flovaTaskState) { s.ProjectID = "other" },
		"stream":             func(s *flovaTaskState) { s.StreamChatID = "unverified-resume" },
		"model":              func(s *flovaTaskState) { s.ModelKey = "veo-3.1" },
		"resolution":         func(s *flovaTaskState) { s.Resolution = "1080p" },
		"duration":           func(s *flovaTaskState) { s.Seconds = 8 },
		"submission time":    func(s *flovaTaskState) { s.SubmittedAt = s.SubmittedAt.Add(time.Hour) },
		"resource delimiter": func(s *flovaTaskState) { s.ResourceID = "r;s=100" },
	} {
		t.Run(name, func(t *testing.T) {
			next := baseline
			mutate(&next)
			require.Error(t, ValidateVideoProviderControlTransition(task, next.encode(), flovaControlState(next), nil))
		})
	}
	next := baseline
	next.ResourceID = "res1"
	require.NoError(t, ValidateVideoProviderControlTransition(task, id, flovaControlState(next), nil))
	require.Error(t, ValidateVideoProviderControlTransition(task, next.encode(), flovaControlState(next), nil), "stable identity must not grow")
	state := flovaControlState(next)
	state.CostUncertain = false
	require.Error(t, ValidateVideoProviderControlTransition(task, id, state, nil))

	task.ProviderExecutionState = flovaControlState(next)
	next.ResourceID = "replacement"
	require.Error(t, ValidateVideoProviderControlTransition(task, id, flovaControlState(next), nil), "persisted resource is immutable")
}

func TestFlovaControlLongLegacyIDRetainsStableIdentity(t *testing.T) {
	state, err := parseFlovaTaskID(flovaSubmittedState("seedance-2.5", "720p", 4, ""))
	require.NoError(t, err)
	state.ProjectID = strings.Repeat("p", 200)
	id := state.encode()
	require.LessOrEqual(t, len(id), 255)
	resourceID := strings.Repeat("r", 100)
	completed := flovaRunResultJSON("completed", true, `,"assistant_messages":[{"artifacts":[{"resource_id":"`+resourceID+`","media_type":"video","status":"success","resource_url":"https://cdn.flova.ai/v.mp4"}]}]`)
	completed = strings.Replace(completed, `"project_id":"p1"`, `"project_id":"`+state.ProjectID+`"`, 1)
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/run_result", completed)
	ref := flovaTaskRef(id)
	ref.ProviderExecutionState = &VideoProviderExecutionState{} // legacy migration default {}
	observed, err := newFlovaTestProvider(stub).Get(context.Background(), flovaVideoTestAccount(), ref)
	require.NoError(t, err)
	require.Equal(t, id, observed.ProviderTaskID)
	require.Equal(t, resourceID, observed.Control.State.ResourceID)
	state.ResourceID = resourceID
	require.Greater(t, len(state.encode()), 255, "regression: resource must not be appended to identifier")
	task := &VideoTask{Provider: VideoProviderFlova, ProviderTaskID: &id, ProviderExecutionState: ref.ProviderExecutionState}
	require.NoError(t, ValidateVideoProviderControlTransition(task, observed.ProviderTaskID, observed.Control.State, nil))

	// Emulate a JSON database round-trip; refresh uses private state directly.
	raw, err := json.Marshal(observed.Control.State)
	require.NoError(t, err)
	var persisted VideoProviderExecutionState
	require.NoError(t, json.Unmarshal(raw, &persisted))
	ref.ProviderExecutionState = &persisted
	stub.json("/api/agent/v2/resource_info", `{"code":0,"data":{"resource":{"resource_url":"https://cdn.flova.ai/fresh.mp4"}}}`)
	_, err = newFlovaTestProvider(stub).RefreshTaskAccess(context.Background(), flovaVideoTestAccount(), ref)
	require.NoError(t, err)
	require.Equal(t, 1, stub.count("/api/agent/v2/run_result"), "persisted resource needs no recovery poll")
	require.Equal(t, resourceID, stub.lastBody("/api/agent/v2/resource_info")["resource_id"])
}

func TestFlovaBlockedActionQuoteCannotBecomeAuthorization(t *testing.T) {
	state, _ := parseFlovaTaskID(flovaSubmittedState("seedance-2.5", "720p", 4, ""))
	for _, payload := range []map[string]any{
		{}, {"credits": -1}, {"credits": math.NaN()}, {"credits": "NaN"},
		{"credits": math.Inf(1)}, {"credits": "invalid"},
		{"credits": 10, "estimated_credits": 11},
	} {
		action := flovaBlockedAction(state, flovaPendingAction{ActionID: "a", Type: flovaActionBatchTaskLimit, Payload: payload})
		require.Nil(t, action.QuotedCredits)
		require.Equal(t, VideoSpendingBlockedUnverified, action.Reason)
	}
}

func TestFlovaPrivateControlStateNeverSerializesOnTask(t *testing.T) {
	task := &VideoTask{ProviderExecutionState: &VideoProviderExecutionState{ProjectID: "private-project", ResourceID: "private-resource"}}
	raw, err := json.Marshal(task)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-project")
	require.NotContains(t, string(raw), "private-resource")
}

func TestFlovaExpiredPendingActionDoesNotSpendAndRetainsAudit(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/run_result", flovaRunResultJSON("completed", true, flovaHighCostAction(81, "1080p", flovaHighCostOptions)))
	provider := newFlovaTestProvider(stub)
	provider.now = func() time.Time { return time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC) }
	task, err := provider.Get(context.Background(), flovaVideoTestAccount(), flovaTaskRef(flovaSubmittedState("seedance-2.5", "1080p", 4, "")))
	require.NoError(t, err)
	require.Equal(t, flovaErrorRunTimeout, task.ErrorCode)
	require.Len(t, task.Control.BlockedActions, 1)
	require.True(t, task.Control.State.CostUncertain)
	require.Zero(t, stub.count("/api/agent/v2/chat"))
}
