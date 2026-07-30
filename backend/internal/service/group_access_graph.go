package service

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// GroupFallbackKind identifies the two independently configured fallback
// routes. Both kinds participate in the same access graph invariants.
type GroupFallbackKind string

const (
	GroupFallbackDefault        GroupFallbackKind = "default"
	GroupFallbackInvalidRequest GroupFallbackKind = "invalid_request"
)

const (
	GroupFallbackReasonSubscriptionVIPOnly = "GROUP_SUBSCRIPTION_VIP_ONLY_FORBIDDEN"
	GroupFallbackReasonSubscriptionTarget  = "GROUP_FALLBACK_SUBSCRIPTION_FORBIDDEN"
	GroupFallbackReasonExclusiveEscalation = "GROUP_FALLBACK_EXCLUSIVE_ESCALATION"
	GroupFallbackReasonVIPEscalation       = "GROUP_FALLBACK_VIP_ESCALATION"
	GroupFallbackReasonTargetNotFound      = "GROUP_FALLBACK_TARGET_NOT_FOUND"
	GroupFallbackReasonCycle               = "GROUP_FALLBACK_CYCLE"
	GroupFallbackReasonInvalidPlatform     = "GROUP_FALLBACK_INVALID_PLATFORM"
	GroupFallbackReasonInvalidSingleHop    = "GROUP_FALLBACK_INVALID_REQUEST_SINGLE_HOP"
	GroupFallbackReasonClaudeCodeOnly      = "GROUP_FALLBACK_CLAUDE_CODE_ONLY"
	GroupFallbackReasonClaudeCodePlatform  = "GROUP_CLAUDE_CODE_ONLY_PLATFORM_INVALID"
)

// GroupAccessGraphViolation is also the machine-readable row returned by the
// read-only scanner. Path is intentionally bounded by the number of groups in
// the graph and is emitted in deterministic source-to-target order.
type GroupAccessGraphViolation struct {
	Reason     string            `json:"reason"`
	OriginID   int64             `json:"origin_id,omitempty"`
	OriginName string            `json:"origin_name,omitempty"`
	SourceID   int64             `json:"source_id,omitempty"`
	SourceName string            `json:"source_name,omitempty"`
	TargetID   int64             `json:"target_id,omitempty"`
	TargetName string            `json:"target_name,omitempty"`
	Kind       GroupFallbackKind `json:"fallback_kind,omitempty"`
	Path       []int64           `json:"path,omitempty"`
}

type GroupAccessGraphScanReport struct {
	ScannedAt  time.Time                   `json:"scanned_at"`
	GroupCount int                         `json:"group_count"`
	Violations []GroupAccessGraphViolation `json:"violations"`
}

func (v GroupAccessGraphViolation) identity() string {
	path := make([]string, 0, len(v.Path))
	for _, id := range v.Path {
		path = append(path, strconv.FormatInt(id, 10))
	}
	return strings.Join([]string{
		v.Reason,
		strconv.FormatInt(v.OriginID, 10),
		strconv.FormatInt(v.SourceID, 10),
		strconv.FormatInt(v.TargetID, 10),
		string(v.Kind),
		strings.Join(path, ","),
	}, "|")
}

// mutationIdentity deliberately excludes Path. Removing or shortening an
// upstream route may change the diagnostic path to a pre-existing bad edge;
// that repair must not be rejected as though it introduced a new violation.
func (v GroupAccessGraphViolation) mutationIdentity() string {
	return strings.Join([]string{
		v.Reason,
		strconv.FormatInt(v.OriginID, 10),
		strconv.FormatInt(v.SourceID, 10),
		strconv.FormatInt(v.TargetID, 10),
		string(v.Kind),
	}, "|")
}

func (v GroupAccessGraphViolation) Error() error {
	message := "group fallback configuration is invalid"
	switch v.Reason {
	case GroupFallbackReasonSubscriptionVIPOnly:
		message = fmt.Sprintf("subscription group %q cannot be VIP-only", v.SourceName)
	case GroupFallbackReasonSubscriptionTarget:
		message = fmt.Sprintf("group %q cannot use subscription group %q as a fallback target", v.SourceName, v.TargetName)
	case GroupFallbackReasonExclusiveEscalation:
		message = fmt.Sprintf("public group %q cannot fall back to exclusive group %q", v.SourceName, v.TargetName)
	case GroupFallbackReasonVIPEscalation:
		message = fmt.Sprintf("non-VIP group %q cannot fall back to VIP-only group %q", v.SourceName, v.TargetName)
	case GroupFallbackReasonTargetNotFound:
		message = fmt.Sprintf("fallback target %d referenced by group %q does not exist", v.TargetID, v.SourceName)
	case GroupFallbackReasonCycle:
		message = fmt.Sprintf("fallback group cycle detected from group %q", v.SourceName)
	case GroupFallbackReasonInvalidPlatform:
		message = fmt.Sprintf("invalid-request fallback from group %q to group %q violates platform constraints", v.SourceName, v.TargetName)
	case GroupFallbackReasonInvalidSingleHop:
		message = fmt.Sprintf("invalid-request fallback target %q cannot configure another invalid-request fallback", v.TargetName)
	case GroupFallbackReasonClaudeCodeOnly:
		message = fmt.Sprintf("fallback target %q cannot have claude_code_only enabled", v.TargetName)
	case GroupFallbackReasonClaudeCodePlatform:
		message = fmt.Sprintf(
			"group %q can enable claude_code_only only on Anthropic or Antigravity",
			v.SourceName,
		)
	}
	metadata := map[string]string{
		"source_id":     strconv.FormatInt(v.SourceID, 10),
		"source_name":   v.SourceName,
		"target_id":     strconv.FormatInt(v.TargetID, 10),
		"target_name":   v.TargetName,
		"fallback_kind": string(v.Kind),
	}
	if v.OriginID > 0 {
		metadata["origin_id"] = strconv.FormatInt(v.OriginID, 10)
		metadata["origin_name"] = v.OriginName
	}
	if len(v.Path) > 0 {
		path := make([]string, 0, len(v.Path))
		for _, id := range v.Path {
			path = append(path, strconv.FormatInt(id, 10))
		}
		metadata["path"] = strings.Join(path, "->")
	}
	return infraerrors.New(http.StatusBadRequest, v.Reason, message).WithMetadata(metadata)
}

type groupFallbackEdge struct {
	source *Group
	target *Group
	id     int64
	kind   GroupFallbackKind
}

// ScanGroupAccessGraph returns every structural violation without mutating the
// supplied groups. Results are stable, with the public error precedence first:
// subscription targets, exclusive escalation, then VIP escalation.
func ScanGroupAccessGraph(groups []Group) []GroupAccessGraphViolation {
	byID := make(map[int64]*Group, len(groups))
	groupRefs := make([]*Group, 0, len(groups))
	for i := range groups {
		groupRefs = append(groupRefs, &groups[i])
		if groups[i].ID != 0 {
			byID[groups[i].ID] = &groups[i]
		}
	}
	sort.SliceStable(groupRefs, func(i, j int) bool {
		if groupRefs[i].ID == groupRefs[j].ID {
			return groupRefs[i].Name < groupRefs[j].Name
		}
		return groupRefs[i].ID < groupRefs[j].ID
	})

	violations := make([]GroupAccessGraphViolation, 0)
	for _, g := range groupRefs {
		if g.IsSubscriptionType() && g.VIPOnly {
			violations = append(violations, GroupAccessGraphViolation{
				Reason:     GroupFallbackReasonSubscriptionVIPOnly,
				SourceID:   g.ID,
				SourceName: g.Name,
				Path:       []int64{g.ID},
			})
		}
		if g.ClaudeCodeOnly &&
			g.Platform != PlatformAnthropic &&
			g.Platform != PlatformAntigravity {
			violations = append(violations, GroupAccessGraphViolation{
				Reason:     GroupFallbackReasonClaudeCodePlatform,
				SourceID:   g.ID,
				SourceName: g.Name,
				Path:       []int64{g.ID},
			})
		}
	}

	edges := make([]groupFallbackEdge, 0, len(groups)*2)
	for _, source := range groupRefs {
		for _, configured := range []struct {
			id   *int64
			kind GroupFallbackKind
		}{
			{id: source.FallbackGroupID, kind: GroupFallbackDefault},
			{id: source.FallbackGroupIDOnInvalidRequest, kind: GroupFallbackInvalidRequest},
		} {
			if configured.id == nil || *configured.id <= 0 {
				continue
			}
			edges = append(edges, groupFallbackEdge{
				source: source,
				target: byID[*configured.id],
				id:     *configured.id,
				kind:   configured.kind,
			})
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].source.ID != edges[j].source.ID {
			return edges[i].source.ID < edges[j].source.ID
		}
		if edges[i].kind != edges[j].kind {
			return edges[i].kind < edges[j].kind
		}
		return edges[i].id < edges[j].id
	})

	// Missing targets are reported before dereferencing any target attributes.
	for _, edge := range edges {
		if edge.target == nil {
			violations = append(violations, violationForEdge(GroupFallbackReasonTargetNotFound, edge))
		}
	}
	violations = append(violations, scanGroupFallbackReachability(groupRefs, edges)...)

	// Preserve the pre-existing platform and single-hop restrictions for the
	// invalid-request route, but evaluate them against the final graph.
	for _, edge := range edges {
		if edge.target == nil || edge.kind != GroupFallbackInvalidRequest {
			continue
		}
		if (edge.source.Platform != PlatformAnthropic && edge.source.Platform != PlatformAntigravity) ||
			edge.source.IsSubscriptionType() ||
			edge.target.Platform != PlatformAnthropic {
			violations = append(violations, violationForEdge(GroupFallbackReasonInvalidPlatform, edge))
		}
		if edge.target.FallbackGroupIDOnInvalidRequest != nil && *edge.target.FallbackGroupIDOnInvalidRequest > 0 {
			violations = append(violations, violationForEdge(GroupFallbackReasonInvalidSingleHop, edge))
		}
	}
	for _, edge := range edges {
		if edge.target != nil && edge.kind == GroupFallbackDefault && edge.target.ClaudeCodeOnly {
			violations = append(violations, violationForEdge(GroupFallbackReasonClaudeCodeOnly, edge))
		}
	}

	// The two edge kinds share one access graph. Detecting cycles on the union
	// prevents a cross-kind path from bypassing the legacy per-field checks.
	violations = append(violations, scanGroupFallbackCycles(groupRefs, edges)...)

	sort.SliceStable(violations, func(i, j int) bool {
		left, right := groupAccessViolationPriority(violations[i].Reason), groupAccessViolationPriority(violations[j].Reason)
		if left != right {
			return left < right
		}
		if violations[i].SourceID != violations[j].SourceID {
			return violations[i].SourceID < violations[j].SourceID
		}
		if violations[i].TargetID != violations[j].TargetID {
			return violations[i].TargetID < violations[j].TargetID
		}
		return violations[i].Kind < violations[j].Kind
	})
	return dedupeGroupAccessViolations(violations)
}

func violationForEdge(reason string, edge groupFallbackEdge) GroupAccessGraphViolation {
	targetName := ""
	if edge.target != nil {
		targetName = edge.target.Name
	}
	return GroupAccessGraphViolation{
		Reason:     reason,
		SourceID:   edge.source.ID,
		SourceName: edge.source.Name,
		TargetID:   edge.id,
		TargetName: targetName,
		Kind:       edge.kind,
		Path:       []int64{edge.source.ID, edge.id},
	}
}

func scanGroupFallbackReachability(groups []*Group, edges []groupFallbackEdge) []GroupAccessGraphViolation {
	adjacency := make(map[int64][]groupFallbackEdge, len(groups))
	for _, edge := range edges {
		if edge.target != nil {
			adjacency[edge.source.ID] = append(adjacency[edge.source.ID], edge)
		}
	}
	for id := range adjacency {
		sort.SliceStable(adjacency[id], func(i, j int) bool {
			if adjacency[id][i].id != adjacency[id][j].id {
				return adjacency[id][i].id < adjacency[id][j].id
			}
			return adjacency[id][i].kind < adjacency[id][j].kind
		})
	}

	type reachableNode struct {
		group *Group
		path  []int64
	}
	violations := make([]GroupAccessGraphViolation, 0)
	for _, origin := range groups {
		visited := map[int64]struct{}{origin.ID: {}}
		queue := []reachableNode{{group: origin, path: []int64{origin.ID}}}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, edge := range adjacency[current.group.ID] {
				path := append(append([]int64(nil), current.path...), edge.id)
				reason := ""
				switch {
				case edge.target.IsSubscriptionType():
					reason = GroupFallbackReasonSubscriptionTarget
				case !origin.IsExclusive && edge.target.IsExclusive:
					reason = GroupFallbackReasonExclusiveEscalation
				case !origin.VIPOnly && edge.target.VIPOnly:
					reason = GroupFallbackReasonVIPEscalation
				}
				if reason != "" {
					violations = append(violations, GroupAccessGraphViolation{
						Reason:     reason,
						OriginID:   origin.ID,
						OriginName: origin.Name,
						SourceID:   edge.source.ID,
						SourceName: edge.source.Name,
						TargetID:   edge.id,
						TargetName: edge.target.Name,
						Kind:       edge.kind,
						Path:       path,
					})
				}

				// A subscription target is terminal by definition. Cycles are
				// handled separately, while visited bounds every path to O(V).
				if edge.target.IsSubscriptionType() {
					continue
				}
				if _, seen := visited[edge.id]; seen {
					continue
				}
				visited[edge.id] = struct{}{}
				queue = append(queue, reachableNode{group: edge.target, path: path})
			}
		}
	}
	return violations
}

func scanGroupFallbackCycles(groups []*Group, edges []groupFallbackEdge) []GroupAccessGraphViolation {
	adjacency := make(map[int64][]groupFallbackEdge, len(groups))
	for _, edge := range edges {
		if edge.target != nil {
			adjacency[edge.source.ID] = append(adjacency[edge.source.ID], edge)
		}
	}
	for id := range adjacency {
		sort.SliceStable(adjacency[id], func(i, j int) bool {
			if adjacency[id][i].id != adjacency[id][j].id {
				return adjacency[id][i].id < adjacency[id][j].id
			}
			return adjacency[id][i].kind < adjacency[id][j].kind
		})
	}

	state := make(map[int64]uint8, len(groups))
	stack := make([]int64, 0, len(groups))
	stackIndex := make(map[int64]int, len(groups))
	violations := make([]GroupAccessGraphViolation, 0)
	var visit func(*Group)
	visit = func(current *Group) {
		state[current.ID] = 1
		stackIndex[current.ID] = len(stack)
		stack = append(stack, current.ID)
		for _, edge := range adjacency[current.ID] {
			switch state[edge.id] {
			case 0:
				visit(edge.target)
			case 1:
				start := stackIndex[edge.id]
				path := append([]int64(nil), stack[start:]...)
				path = append(path, edge.id)
				violations = append(violations, GroupAccessGraphViolation{
					Reason:     GroupFallbackReasonCycle,
					SourceID:   edge.source.ID,
					SourceName: edge.source.Name,
					TargetID:   edge.id,
					TargetName: edge.target.Name,
					Kind:       edge.kind,
					Path:       path,
				})
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, current.ID)
		state[current.ID] = 2
	}
	for _, g := range groups {
		if state[g.ID] == 0 {
			visit(g)
		}
	}
	return violations
}

func groupAccessViolationPriority(reason string) int {
	switch reason {
	case GroupFallbackReasonTargetNotFound:
		return 0
	case GroupFallbackReasonSubscriptionTarget:
		return 1
	case GroupFallbackReasonSubscriptionVIPOnly:
		return 2
	case GroupFallbackReasonExclusiveEscalation:
		return 3
	case GroupFallbackReasonVIPEscalation:
		return 4
	case GroupFallbackReasonInvalidPlatform:
		return 5
	case GroupFallbackReasonInvalidSingleHop:
		return 6
	case GroupFallbackReasonClaudeCodeOnly:
		return 7
	case GroupFallbackReasonClaudeCodePlatform:
		return 8
	case GroupFallbackReasonCycle:
		return 9
	default:
		return 100
	}
}

func dedupeGroupAccessViolations(in []GroupAccessGraphViolation) []GroupAccessGraphViolation {
	out := make([]GroupAccessGraphViolation, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, violation := range in {
		key := violation.identity()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, violation)
	}
	return out
}

// ValidateGroupAccessGraph rejects the first violation using the documented
// stable priority. It is useful for imports and read-only validation tools.
func ValidateGroupAccessGraph(groups []Group) error {
	violations := ScanGroupAccessGraph(groups)
	if len(violations) == 0 {
		return nil
	}
	return violations[0].Error()
}

// ValidateGroupVIPOnlyConfiguration is the fast final-candidate guard used by
// service-layer write entry points. The repository repeats this invariant as
// part of the complete graph transaction.
func ValidateGroupVIPOnlyConfiguration(group *Group) error {
	if group == nil || !group.IsSubscriptionType() || !group.VIPOnly {
		return nil
	}
	return GroupAccessGraphViolation{
		Reason:     GroupFallbackReasonSubscriptionVIPOnly,
		SourceID:   group.ID,
		SourceName: group.Name,
		Path:       []int64{group.ID},
	}.Error()
}

func ValidateGroupClaudeCodeOnlyConfiguration(group *Group) error {
	if group == nil ||
		!group.ClaudeCodeOnly ||
		group.Platform == PlatformAnthropic ||
		group.Platform == PlatformAntigravity {
		return nil
	}
	return GroupAccessGraphViolation{
		Reason:     GroupFallbackReasonClaudeCodePlatform,
		SourceID:   group.ID,
		SourceName: group.Name,
		Path:       []int64{group.ID},
	}.Error()
}

// ValidateGroupAccessGraphMutation permits a legacy violation to remain while
// it is being repaired, but rejects every newly introduced or expanded
// violation. This is the Phase-A write-side behavior from the rollout plan.
func ValidateGroupAccessGraphMutation(before, after []Group) error {
	beforeSet := make(map[string]struct{})
	for _, violation := range ScanGroupAccessGraph(before) {
		beforeSet[violation.mutationIdentity()] = struct{}{}
	}
	for _, violation := range ScanGroupAccessGraph(after) {
		if _, existed := beforeSet[violation.mutationIdentity()]; existed {
			continue
		}
		return violation.Error()
	}
	return nil
}
