package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// VideoSpendingBlockedUnverified 是 e8453248d 起的旧任务策略：花费不可验证，任何确认都阻断。
	VideoSpendingBlockedUnverified = "blocked_unverified_budget"
	// VideoSpendingSerializedBalanceDelta 是带预算的新任务策略：单账号串行，预算内的确认自动放行，
	// 终态后按账号余额差结清供应商成本（docs/Flova 成本约束设计.md）。
	VideoSpendingSerializedBalanceDelta = "serialized_balance_delta"
	// VideoSpendingExceedsTaskBudget 记录报价超过任务预算而被阻断的确认。
	VideoSpendingExceedsTaskBudget = "exceeds_task_budget"
)

// VideoProviderExecutionState is private recovery state, distinct from public
// response metadata and the immutable pricing snapshot. Legacy Flova runs have
// no verifiable spend history, so observing them cannot clear CostUncertain.
type VideoProviderExecutionState struct {
	Version        int    `json:"version"`
	Provider       string `json:"provider"`
	ProjectID      string `json:"project_id"`
	StreamChatID   string `json:"stream_chat_id"`
	ResourceID     string `json:"resource_id,omitempty"`
	VideoArtifacts int    `json:"video_artifacts,omitempty"`
	ImageArtifacts int    `json:"image_artifacts,omitempty"`
	// ResumeActionKey 是预算内被授权自动确认的动作；授权落库后 worker 只调用一次 resume，
	// 之后不论结果都不会再确认任何动作（每任务最多一次，绝不重放）。
	ResumeActionKey string `json:"resume_action_key,omitempty"`
	// ResumedStreamChatID 是 resume 后上游开出的新 run；只写一次，原 StreamChatID 仍是身份。
	ResumedStreamChatID string `json:"resumed_stream_chat_id,omitempty"`
	SpendingPolicy      string `json:"spending_policy"`
	CostUncertain       bool   `json:"cost_uncertain"`
}

// VideoProviderBlockedAction records an observation, never an authorization to
// spend. No raw messages, URLs, or unbounded upstream payloads enter the ledger.
type VideoProviderBlockedAction struct {
	ActionKey     string   `json:"action_key"`
	ActionType    string   `json:"action_type"`
	Reason        string   `json:"reason"`
	QuotedCredits *float64 `json:"quoted_credits,omitempty"`
}

type VideoProviderControlObservation struct {
	State          *VideoProviderExecutionState
	BlockedActions []VideoProviderBlockedAction
	// ResumeIntent 不落库：State.ResumeActionKey 首次落库后，worker 凭它调用一次
	// VideoProviderResumer.ResumeAuthorized。
	ResumeIntent *VideoProviderResumeIntent
	// SpentCredits 是运行中按账号余额估算的已花费积分（B0 − 当前余额）；Overspent 表示已超出
	// 任务预算，worker 据此立即熔断账号。二者不落库。
	SpentCredits *float64
	Overspent    bool
}

// VideoProviderResumeIntent 是一次已授权、尚未执行的确认动作。
type VideoProviderResumeIntent struct {
	ActionKey       string
	ActionID        string
	ResumeMessageID string
	OptionID        string
}

// VideoProviderResumer 由支持预算内自动确认的 provider 实现。调用方必须保证每个任务只调用一次：
// 在 ResumeActionKey 首次落库的那次观测之后立即调用，失败或结果未知都不得重试。
type VideoProviderResumer interface {
	ResumeAuthorized(ctx context.Context, account *Account, ref ProviderTaskRef, intent VideoProviderResumeIntent) (string, error)
}

func flovaControlState(state flovaTaskState) *VideoProviderExecutionState {
	return &VideoProviderExecutionState{
		Version: 1, Provider: VideoProviderFlova, ProjectID: state.ProjectID,
		StreamChatID: state.StreamChatID, ResourceID: state.ResourceID,
		ResumeActionKey: state.ResumeActionKey, ResumedStreamChatID: state.ResumedStreamChatID,
		SpendingPolicy: state.spendingPolicy(), CostUncertain: true,
	}
}

func validFlovaActionKey(value string) bool {
	if value == "" {
		return true
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validFlovaStreamIdentifier(value string) bool {
	return validVideoProviderIdentifier(value) && !strings.ContainsAny(value, ":|;=")
}

func validFlovaControlState(state *VideoProviderExecutionState) bool {
	return state != nil && state.Version == 1 && state.Provider == VideoProviderFlova &&
		(state.SpendingPolicy == VideoSpendingBlockedUnverified ||
			(state.SpendingPolicy == VideoSpendingSerializedBalanceDelta && (state.ResumedStreamChatID == "" || validFlovaStreamIdentifier(state.ResumedStreamChatID)))) &&
		(state.SpendingPolicy == VideoSpendingSerializedBalanceDelta || (state.ResumedStreamChatID == "" && state.ResumeActionKey == "")) &&
		(state.ResumedStreamChatID == "" || state.ResumeActionKey != "") && validFlovaActionKey(state.ResumeActionKey) && state.CostUncertain &&
		state.VideoArtifacts >= 0 && state.VideoArtifacts <= 1000000 &&
		state.ImageArtifacts >= 0 && state.ImageArtifacts <= 1000000 &&
		(state.ResourceID == "" || (validVideoProviderIdentifier(state.ResourceID) && !strings.ContainsAny(state.ResourceID, ":|;=")))
}

// The provider identifier remains stable. Resource IDs written by older
// versions are readable, but new resource IDs live only in private JSON state
// so an otherwise valid 255-byte provider identifier never needs to grow.
func flovaStateForRef(ref ProviderTaskRef) (flovaTaskState, error) {
	if !validVideoProviderIdentifier(ref.ProviderTaskID) {
		return flovaTaskState{}, errors.New("flova task identity is invalid")
	}
	state, err := parseFlovaTaskID(ref.ProviderTaskID)
	if err != nil {
		return state, err
	}
	if state.ResourceID != "" && (!validVideoProviderIdentifier(state.ResourceID) || strings.ContainsAny(state.ResourceID, ":|;=")) {
		return flovaTaskState{}, errors.New("flova legacy resource identity is invalid")
	}
	// Migration 278 uses {} for legacy rows; JSON decoding produces a non-nil
	// zero struct. Only this exact empty value is equivalent to absent state.
	if control := ref.ProviderExecutionState; control != nil && *control != (VideoProviderExecutionState{}) {
		if !validFlovaControlState(control) || control.ProjectID != state.ProjectID || control.StreamChatID != state.StreamChatID ||
			(state.ResourceID != "" && state.ResourceID != control.ResourceID) || control.SpendingPolicy != state.spendingPolicy() {
			return flovaTaskState{}, errors.New("flova recovery state does not match its task identity")
		}
		state.ResourceID = control.ResourceID
		state.ResumeActionKey = control.ResumeActionKey
		state.ResumedStreamChatID = control.ResumedStreamChatID
	}
	return state, nil
}

func flovaBlockedAction(state flovaTaskState, action flovaPendingAction) VideoProviderBlockedAction {
	identity, _ := json.Marshal([]string{state.ProjectID, state.activeStream(), action.ActionID, action.ResumeMessageID, action.Type})
	hash := sha256.Sum256(identity)
	kind := strings.ToLower(strings.TrimSpace(action.Type))
	switch kind {
	case flovaActionHighCostConfirm, flovaActionBatchTaskLimit, flovaActionSubscriptionRequired:
	default:
		kind = "unknown"
	}
	reason := VideoSpendingBlockedUnverified
	if state.budgeted() {
		// 有预算的任务只会因报价超预算（或动作本身不可放行）被阻断。
		reason = VideoSpendingExceedsTaskBudget
	}
	blocked := VideoProviderBlockedAction{ActionKey: hex.EncodeToString(hash[:]), ActionType: kind, Reason: reason}
	// A quote is audit evidence only. Missing, negative, non-finite, or conflicting
	// aliases must remain unknown rather than silently becoming a zero-cost action.
	var quoted *float64
	for _, key := range []string{"credits", "estimated_credits"} {
		if _, exists := action.Payload[key]; !exists {
			continue
		}
		value, ok := numericMapValue(action.Payload, key)
		if !ok || !finiteNonNegative(value) || value > 1e12 || (quoted != nil && *quoted != value) {
			return blocked
		}
		quoted = &value
	}
	blocked.QuotedCredits = quoted
	return blocked
}

// ValidateVideoProviderControlTransition is also enforced by the repository so
// callers cannot replace a task identity by bypassing observation reconciliation.
// Only budgeted tasks may resume, and only once: the resumed stream is recorded
// beside the original identity and can never be replaced afterwards.
func ValidateVideoProviderControlTransition(task *VideoTask, id string, state *VideoProviderExecutionState, actions []VideoProviderBlockedAction) error {
	invalid := errors.New("invalid video provider control transition")
	if task == nil || task.Provider != VideoProviderFlova || task.ProviderTaskID == nil || state == nil ||
		!validFlovaControlState(state) || !validVideoProviderIdentifier(id) || id != *task.ProviderTaskID || len(actions) > 16 {
		return invalid
	}
	previous, err := flovaStateForRef(ProviderTaskRef{ProviderTaskID: *task.ProviderTaskID, ProviderExecutionState: task.ProviderExecutionState})
	if err != nil {
		return invalid
	}
	if state.ProjectID != previous.ProjectID || state.StreamChatID != previous.StreamChatID ||
		(previous.ResourceID != "" && state.ResourceID != previous.ResourceID) || state.SpendingPolicy != previous.spendingPolicy() ||
		(previous.ResumeActionKey != "" && state.ResumeActionKey != previous.ResumeActionKey) ||
		(previous.ResumedStreamChatID != "" && state.ResumedStreamChatID != previous.ResumedStreamChatID) ||
		// resume 的新 run 只能在授权已落库之后、由 resume 本身写入，不能与授权同一次落库。
		(state.ResumedStreamChatID != "" && previous.ResumeActionKey == "") ||
		state.ResumedStreamChatID == state.StreamChatID {
		return invalid
	}
	for _, action := range actions {
		decoded, err := hex.DecodeString(action.ActionKey)
		if err != nil || len(decoded) != sha256.Size || (action.Reason != VideoSpendingBlockedUnverified && action.Reason != VideoSpendingExceedsTaskBudget) ||
			(action.QuotedCredits != nil && (!finiteNonNegative(*action.QuotedCredits) || *action.QuotedCredits > 1e12)) {
			return invalid
		}
		switch action.ActionType {
		case flovaActionHighCostConfirm, flovaActionBatchTaskLimit, flovaActionSubscriptionRequired, "unknown":
		default:
			return invalid
		}
	}
	return nil
}

func videoProviderControlTransition(task *VideoTask, observed *ProviderVideoTask, transition *VideoTaskTransition) error {
	if observed.Control == nil {
		return nil
	}
	id := observed.ProviderTaskID
	if err := ValidateVideoProviderControlTransition(task, id, observed.Control.State, observed.Control.BlockedActions); err != nil {
		return err
	}
	transition.ProviderTaskID = &id
	transition.ProviderExecutionState = observed.Control.State
	transition.BlockedProviderActions = observed.Control.BlockedActions
	return nil
}
