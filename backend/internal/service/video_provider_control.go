package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const VideoSpendingBlockedUnverified = "blocked_unverified_budget"

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
	SpendingPolicy string `json:"spending_policy"`
	CostUncertain  bool   `json:"cost_uncertain"`
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
}

func flovaControlState(state flovaTaskState) *VideoProviderExecutionState {
	return &VideoProviderExecutionState{
		Version: 1, Provider: VideoProviderFlova, ProjectID: state.ProjectID,
		StreamChatID: state.StreamChatID, ResourceID: state.ResourceID,
		SpendingPolicy: VideoSpendingBlockedUnverified, CostUncertain: true,
	}
}

func validFlovaControlState(state *VideoProviderExecutionState) bool {
	return state != nil && state.Version == 1 && state.Provider == VideoProviderFlova &&
		state.SpendingPolicy == VideoSpendingBlockedUnverified && state.CostUncertain &&
		state.VideoArtifacts >= 0 && state.VideoArtifacts <= 1000000 &&
		state.ImageArtifacts >= 0 && state.ImageArtifacts <= 1000000 &&
		(state.ResourceID == "" || (validVideoProviderIdentifier(state.ResourceID) && !strings.ContainsAny(state.ResourceID, ":|;=")))
}

// The provider identifier remains stable. Resource IDs written by older
// versions are readable, but new resource IDs live only in private JSON state
// so an otherwise valid 255-byte provider identifier never needs to grow.
func flovaStateForRef(ref ProviderTaskRef) (flovaTaskState, error) {
	if !validVideoProviderIdentifier(ref.ProviderTaskID) {
		return flovaTaskState{}, errors.New("Flova task identity is invalid")
	}
	state, err := parseFlovaTaskID(ref.ProviderTaskID)
	if err != nil {
		return state, err
	}
	if state.ResourceID != "" && (!validVideoProviderIdentifier(state.ResourceID) || strings.ContainsAny(state.ResourceID, ":|;=")) {
		return flovaTaskState{}, errors.New("Flova legacy resource identity is invalid")
	}
	// Migration 278 uses {} for legacy rows; JSON decoding produces a non-nil
	// zero struct. Only this exact empty value is equivalent to absent state.
	if control := ref.ProviderExecutionState; control != nil && *control != (VideoProviderExecutionState{}) {
		if !validFlovaControlState(control) || control.ProjectID != state.ProjectID || control.StreamChatID != state.StreamChatID ||
			(state.ResourceID != "" && state.ResourceID != control.ResourceID) {
			return flovaTaskState{}, errors.New("Flova recovery state does not match its task identity")
		}
		state.ResourceID = control.ResourceID
	}
	return state, nil
}

func flovaBlockedAction(state flovaTaskState, action flovaPendingAction) VideoProviderBlockedAction {
	identity, _ := json.Marshal([]string{state.ProjectID, state.StreamChatID, action.ActionID, action.ResumeMessageID, action.Type})
	hash := sha256.Sum256(identity)
	kind := strings.ToLower(strings.TrimSpace(action.Type))
	switch kind {
	case flovaActionHighCostConfirm, flovaActionBatchTaskLimit, flovaActionSubscriptionRequired:
	default:
		kind = "unknown"
	}
	blocked := VideoProviderBlockedAction{ActionKey: hex.EncodeToString(hash[:]), ActionType: kind, Reason: VideoSpendingBlockedUnverified}
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
// Automatic resume is disabled; a read-only observation must retain its stream.
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
		(previous.ResourceID != "" && state.ResourceID != previous.ResourceID) {
		return invalid
	}
	for _, action := range actions {
		decoded, err := hex.DecodeString(action.ActionKey)
		if err != nil || len(decoded) != sha256.Size || action.Reason != VideoSpendingBlockedUnverified ||
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
