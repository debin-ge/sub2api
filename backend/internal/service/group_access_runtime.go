package service

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type GroupAccessRuntimeEntry string

const (
	GroupAccessRuntimeEntryPrimaryAuth            GroupAccessRuntimeEntry = "primary_auth"
	GroupAccessRuntimeEntryGooglePrimaryAuth      GroupAccessRuntimeEntry = "google_primary_auth"
	GroupAccessRuntimeEntryFallback               GroupAccessRuntimeEntry = "fallback"
	GroupAccessRuntimeEntryInvalidRequestFallback GroupAccessRuntimeEntry = "invalid_request_fallback"
)

type GroupAccessRuntimeOutcome string

const (
	GroupAccessRuntimeOutcomeAllow     GroupAccessRuntimeOutcome = "allow"
	GroupAccessRuntimeOutcomeWouldDeny GroupAccessRuntimeOutcome = "would_deny"
	GroupAccessRuntimeOutcomeDeny      GroupAccessRuntimeOutcome = "deny"
)

type GroupAccessRuntimeMetric struct {
	Mode    string
	Outcome GroupAccessRuntimeOutcome
	Reason  GroupAccessDenyReason
	Entry   GroupAccessRuntimeEntry
	Count   uint64
}

type groupAccessRuntimeMetricKey struct {
	mode    string
	outcome GroupAccessRuntimeOutcome
	reason  GroupAccessDenyReason
	entry   GroupAccessRuntimeEntry
}

type GroupAccessRuntimeDriftMetric struct {
	Reason string
	Entry  GroupAccessRuntimeEntry
	Count  uint64
}

type groupAccessRuntimeDriftMetricKey struct {
	reason string
	entry  GroupAccessRuntimeEntry
}

var groupAccessRuntimeMetrics = struct {
	sync.Mutex
	counts map[groupAccessRuntimeMetricKey]uint64
}{counts: make(map[groupAccessRuntimeMetricKey]uint64)}

var groupAccessRuntimeDriftMetrics = struct {
	sync.Mutex
	counts map[groupAccessRuntimeDriftMetricKey]uint64
}{counts: make(map[groupAccessRuntimeDriftMetricKey]uint64)}

// ApplyGroupAccessRuntimeDecision applies the two-phase rollout semantics to a
// policy decision. An empty mode is accepted only for directly-constructed
// test/internal configs and behaves as AUDIT_ONLY; production config loading
// rejects missing values before services are built.
func ApplyGroupAccessRuntimeDecision(
	modeRaw string,
	entry GroupAccessRuntimeEntry,
	decision GroupAccessDecision,
	userID, groupID int64,
) error {
	mode := modeRaw
	if mode == "" {
		mode = config.GroupAccessRuntimeModeAuditOnly
	}
	parsedMode, err := config.ParseGroupAccessRuntimeMode(mode)
	if err != nil {
		return infraerrors.InternalServer(
			"GROUP_ACCESS_RUNTIME_CONFIG_INVALID",
			"group access runtime mode is invalid",
		)
	}
	if !validGroupAccessRuntimeEntry(entry) {
		return infraerrors.InternalServer(
			"GROUP_ACCESS_RUNTIME_ENTRY_INVALID",
			"group access runtime entry is invalid",
		)
	}

	if decision.Allowed {
		recordGroupAccessRuntimeMetric(parsedMode, GroupAccessRuntimeOutcomeAllow, decision.Reason, entry)
		return nil
	}
	if parsedMode == config.GroupAccessRuntimeModeAuditOnly &&
		isNewGroupAccessRuntimeDenial(entry, decision.Reason) {
		recordGroupAccessRuntimeMetric(parsedMode, GroupAccessRuntimeOutcomeWouldDeny, decision.Reason, entry)
		slog.Warn("group access policy would deny",
			"mode", parsedMode,
			"entry", entry,
			"reason", decision.Reason,
			"user_id", userID,
			"group_id", groupID,
		)
		return nil
	}

	recordGroupAccessRuntimeMetric(parsedMode, GroupAccessRuntimeOutcomeDeny, decision.Reason, entry)
	return decision.Error()
}

func validGroupAccessRuntimeEntry(entry GroupAccessRuntimeEntry) bool {
	switch entry {
	case GroupAccessRuntimeEntryPrimaryAuth,
		GroupAccessRuntimeEntryGooglePrimaryAuth,
		GroupAccessRuntimeEntryFallback,
		GroupAccessRuntimeEntryInvalidRequestFallback:
		return true
	default:
		return false
	}
}

func isNewGroupAccessRuntimeDenial(entry GroupAccessRuntimeEntry, reason GroupAccessDenyReason) bool {
	switch entry {
	case GroupAccessRuntimeEntryPrimaryAuth, GroupAccessRuntimeEntryGooglePrimaryAuth:
		// Exclusive primary-group enforcement predates VIP and must never be
		// bypassed by AUDIT_ONLY.
		return reason == GroupAccessDenyVIPOnly || reason == GroupAccessDenyProfileMissing
	case GroupAccessRuntimeEntryFallback, GroupAccessRuntimeEntryInvalidRequestFallback:
		switch reason {
		case GroupAccessDenyGroupNotAllowed,
			GroupAccessDenyVIPOnly,
			GroupAccessDenyFallbackSubscription,
			GroupAccessDenyFallbackInvalidConfig,
			GroupAccessDenyProfileMissing:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func recordGroupAccessRuntimeMetric(
	mode string,
	outcome GroupAccessRuntimeOutcome,
	reason GroupAccessDenyReason,
	entry GroupAccessRuntimeEntry,
) {
	key := groupAccessRuntimeMetricKey{mode: mode, outcome: outcome, reason: reason, entry: entry}
	groupAccessRuntimeMetrics.Lock()
	groupAccessRuntimeMetrics.counts[key]++
	groupAccessRuntimeMetrics.Unlock()
}

// GroupAccessRuntimeMetricsSnapshot exposes bounded counters to the ops
// collector without coupling the hot path to a global Prometheus registry.
func GroupAccessRuntimeMetricsSnapshot() []GroupAccessRuntimeMetric {
	groupAccessRuntimeMetrics.Lock()
	out := make([]GroupAccessRuntimeMetric, 0, len(groupAccessRuntimeMetrics.counts))
	for key, count := range groupAccessRuntimeMetrics.counts {
		out = append(out, GroupAccessRuntimeMetric{
			Mode: key.mode, Outcome: key.outcome, Reason: key.reason, Entry: key.entry, Count: count,
		})
	}
	groupAccessRuntimeMetrics.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mode != out[j].Mode {
			return out[i].Mode < out[j].Mode
		}
		if out[i].Entry != out[j].Entry {
			return out[i].Entry < out[j].Entry
		}
		if out[i].Outcome != out[j].Outcome {
			return out[i].Outcome < out[j].Outcome
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func GroupAccessRuntimeDriftMetricsSnapshot() []GroupAccessRuntimeDriftMetric {
	groupAccessRuntimeDriftMetrics.Lock()
	out := make([]GroupAccessRuntimeDriftMetric, 0, len(groupAccessRuntimeDriftMetrics.counts))
	for key, count := range groupAccessRuntimeDriftMetrics.counts {
		out = append(out, GroupAccessRuntimeDriftMetric{
			Reason: key.reason,
			Entry:  key.entry,
			Count:  count,
		})
	}
	groupAccessRuntimeDriftMetrics.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entry != out[j].Entry {
			return out[i].Entry < out[j].Entry
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

var runtimeFallbackGroupAccessPolicy = NewGroupAccessPolicy()

// ResolveAuthorizedFallback evaluates the candidate before callers clone an
// API key, run billing, or allocate a target account.
func (s *GatewayService) ResolveAuthorizedFallback(
	ctx context.Context,
	source *Group,
	target *Group,
	entry GroupAccessRuntimeEntry,
) error {
	profile, _ := GroupAccessProfileFromContext(ctx)
	decision := runtimeFallbackGroupAccessPolicy.Evaluate(profile, target, GroupAccessFallbackRuntime)
	mode := ""
	if s != nil && s.cfg != nil {
		mode = s.cfg.GroupAccessRuntimeMode
	}
	userID := int64(0)
	if profile != nil {
		userID = profile.UserID
	} else if value, ok := ctx.Value(ctxkey.UserID).(int64); ok {
		userID = value
	}
	groupID := int64(0)
	if target != nil {
		groupID = target.ID
	}
	if err := ApplyGroupAccessRuntimeDecision(mode, entry, decision, userID, groupID); err != nil {
		return err
	}
	if decision.Allowed {
		if reason := authorizedFallbackLegacyDriftReason(source, target); reason != "" {
			recordAuthorizedFallbackLegacyDrift(reason, entry, source, target)
		}
	}
	return nil
}

func authorizedFallbackLegacyDriftReason(source, target *Group) string {
	if source == nil || target == nil {
		return ""
	}
	switch {
	case !source.IsExclusive && target.IsExclusive:
		return GroupFallbackReasonExclusiveEscalation
	case !source.VIPOnly && target.VIPOnly:
		return GroupFallbackReasonVIPEscalation
	default:
		return ""
	}
}

func recordAuthorizedFallbackLegacyDrift(
	reason string,
	entry GroupAccessRuntimeEntry,
	source, target *Group,
) {
	key := groupAccessRuntimeDriftMetricKey{reason: reason, entry: entry}
	groupAccessRuntimeDriftMetrics.Lock()
	groupAccessRuntimeDriftMetrics.counts[key]++
	groupAccessRuntimeDriftMetrics.Unlock()
	slog.Warn("authorized legacy group fallback drift",
		"reason", reason,
		"entry", entry,
		"source_group_id", source.ID,
		"target_group_id", target.ID,
	)
}
