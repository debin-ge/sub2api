package service

import (
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func graphGroup(id int64, name string) Group {
	return Group{
		ID:               id,
		Name:             name,
		Platform:         PlatformAnthropic,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
	}
}

func groupGraphPtr[T any](value T) *T { return &value }

func TestValidateGroupAccessGraphSubscriptionVIPOnly(t *testing.T) {
	group := graphGroup(1, "subscription")
	group.SubscriptionType = SubscriptionTypeSubscription
	group.VIPOnly = true

	err := ValidateGroupAccessGraph([]Group{group})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonSubscriptionVIPOnly, infraerrors.Reason(err))
}

func TestValidateGroupAccessGraphRejectsClaudeCodeOnlyOnUnsupportedPlatform(t *testing.T) {
	group := graphGroup(1, "openai-claude-code-only")
	group.Platform = PlatformOpenAI
	group.ClaudeCodeOnly = true

	err := ValidateGroupClaudeCodeOnlyConfiguration(&group)
	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonClaudeCodePlatform, infraerrors.Reason(err))

	err = ValidateGroupAccessGraph([]Group{group})
	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonClaudeCodePlatform, infraerrors.Reason(err))

	group.Platform = PlatformAntigravity
	require.NoError(t, ValidateGroupClaudeCodeOnlyConfiguration(&group))
	require.NoError(t, ValidateGroupAccessGraph([]Group{group}))
}

func TestValidateGroupAccessGraphStableEdgePriority(t *testing.T) {
	source := graphGroup(1, "public")
	target := graphGroup(2, "subscription-exclusive-vip")
	target.SubscriptionType = SubscriptionTypeSubscription
	target.IsExclusive = true
	target.VIPOnly = true
	source.FallbackGroupID = groupGraphPtr(target.ID)

	err := ValidateGroupAccessGraph([]Group{source, target})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonSubscriptionTarget, infraerrors.Reason(err))
	appErr := infraerrors.FromError(err)
	require.Equal(t, "1->2", appErr.Metadata["path"])
	require.Equal(t, string(GroupFallbackDefault), appErr.Metadata["fallback_kind"])
}

func TestValidateGroupAccessGraphExclusivePrecedesVIP(t *testing.T) {
	source := graphGroup(1, "public")
	target := graphGroup(2, "exclusive-vip")
	target.IsExclusive = true
	target.VIPOnly = true
	source.FallbackGroupIDOnInvalidRequest = groupGraphPtr(target.ID)

	err := ValidateGroupAccessGraph([]Group{source, target})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonExclusiveEscalation, infraerrors.Reason(err))
}

func TestValidateGroupAccessGraphCoversBothFallbackKinds(t *testing.T) {
	subscriptionA := graphGroup(2, "subscription-a")
	subscriptionA.SubscriptionType = SubscriptionTypeSubscription
	subscriptionB := graphGroup(3, "subscription-b")
	subscriptionB.SubscriptionType = SubscriptionTypeSubscription
	source := graphGroup(1, "source")
	source.FallbackGroupID = groupGraphPtr(subscriptionA.ID)
	source.FallbackGroupIDOnInvalidRequest = groupGraphPtr(subscriptionB.ID)

	violations := ScanGroupAccessGraph([]Group{source, subscriptionA, subscriptionB})

	var kinds []GroupFallbackKind
	for _, violation := range violations {
		if violation.Reason == GroupFallbackReasonSubscriptionTarget {
			kinds = append(kinds, violation.Kind)
		}
	}
	require.ElementsMatch(t, []GroupFallbackKind{GroupFallbackDefault, GroupFallbackInvalidRequest}, kinds)
}

func TestValidateGroupAccessGraphDetectsReverseCandidateChanges(t *testing.T) {
	public := graphGroup(1, "public")
	target := graphGroup(2, "target")
	public.FallbackGroupID = groupGraphPtr(target.ID)
	before := []Group{public, target}

	target.VIPOnly = true
	err := ValidateGroupAccessGraphMutation(before, []Group{public, target})
	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonVIPEscalation, infraerrors.Reason(err))

	target.VIPOnly = false
	target.SubscriptionType = SubscriptionTypeSubscription
	err = ValidateGroupAccessGraphMutation(before, []Group{public, target})
	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonSubscriptionTarget, infraerrors.Reason(err))
}

func TestValidateGroupAccessGraphMutationAllowsLegacyRepairOnly(t *testing.T) {
	source := graphGroup(1, "legacy-source")
	vipTarget := graphGroup(2, "vip-target")
	vipTarget.VIPOnly = true
	source.FallbackGroupID = groupGraphPtr(vipTarget.ID)
	before := []Group{source, vipTarget}

	renamed := source
	renamed.Name = "renamed"
	require.NoError(t, ValidateGroupAccessGraphMutation(before, []Group{renamed, vipTarget}))

	exclusiveTarget := graphGroup(3, "exclusive")
	exclusiveTarget.IsExclusive = true
	renamed.FallbackGroupIDOnInvalidRequest = groupGraphPtr(exclusiveTarget.ID)
	err := ValidateGroupAccessGraphMutation(before, []Group{renamed, vipTarget, exclusiveTarget})
	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonExclusiveEscalation, infraerrors.Reason(err))

	renamed.FallbackGroupID = nil
	renamed.FallbackGroupIDOnInvalidRequest = nil
	require.NoError(t, ValidateGroupAccessGraphMutation(before, []Group{renamed, vipTarget}))
}

func TestValidateGroupAccessGraphMutationRejectsExpandedLegacyReachability(t *testing.T) {
	legacySource := graphGroup(2, "legacy-source")
	vipTarget := graphGroup(3, "vip-target")
	vipTarget.VIPOnly = true
	legacySource.FallbackGroupID = groupGraphPtr(vipTarget.ID)
	before := []Group{legacySource, vipTarget}

	newSource := graphGroup(1, "new-source")
	newSource.FallbackGroupID = groupGraphPtr(legacySource.ID)
	err := ValidateGroupAccessGraphMutation(before, []Group{newSource, legacySource, vipTarget})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonVIPEscalation, infraerrors.Reason(err))
	require.Equal(t, "1->2->3", infraerrors.FromError(err).Metadata["path"])
}

func TestGroupAccessGraphReachabilityReportsConfiguredEdgeAndOrigin(t *testing.T) {
	origin := graphGroup(1, "origin")
	edgeSource := graphGroup(2, "edge-source")
	vipTarget := graphGroup(3, "vip-target")
	vipTarget.VIPOnly = true
	origin.FallbackGroupID = groupGraphPtr(edgeSource.ID)
	edgeSource.FallbackGroupID = groupGraphPtr(vipTarget.ID)

	violations := ScanGroupAccessGraph([]Group{origin, edgeSource, vipTarget})

	var got *GroupAccessGraphViolation
	for i := range violations {
		if violations[i].Reason == GroupFallbackReasonVIPEscalation &&
			violations[i].OriginID == origin.ID {
			got = &violations[i]
			break
		}
	}
	require.NotNil(t, got)
	require.Equal(t, edgeSource.ID, got.SourceID)
	require.Equal(t, edgeSource.Name, got.SourceName)
	require.Equal(t, origin.ID, got.OriginID)
	require.Equal(t, origin.Name, got.OriginName)
	require.Equal(t, []int64{origin.ID, edgeSource.ID, vipTarget.ID}, got.Path)
}

func TestGroupAccessGraphMutationIdentityIgnoresDiagnosticPath(t *testing.T) {
	first := GroupAccessGraphViolation{
		Reason:   GroupFallbackReasonVIPEscalation,
		OriginID: 1,
		SourceID: 2,
		TargetID: 3,
		Kind:     GroupFallbackDefault,
		Path:     []int64{1, 2, 3},
	}
	second := first
	second.Path = []int64{1, 4, 2, 3}

	require.Equal(t, first.mutationIdentity(), second.mutationIdentity())
	require.NotEqual(t, first.identity(), second.identity())
}

func TestValidateGroupAccessGraphDetectsCrossKindCycle(t *testing.T) {
	first := graphGroup(1, "first")
	second := graphGroup(2, "second")
	first.FallbackGroupID = groupGraphPtr(second.ID)
	second.FallbackGroupIDOnInvalidRequest = groupGraphPtr(first.ID)

	err := ValidateGroupAccessGraph([]Group{first, second})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonCycle, infraerrors.Reason(err))
}

func TestValidateGroupAccessGraphPreservesInvalidRequestConstraints(t *testing.T) {
	source := graphGroup(1, "openai")
	source.Platform = PlatformOpenAI
	target := graphGroup(2, "anthropic")
	source.FallbackGroupIDOnInvalidRequest = groupGraphPtr(target.ID)

	err := ValidateGroupAccessGraph([]Group{source, target})

	require.Error(t, err)
	require.Equal(t, GroupFallbackReasonInvalidPlatform, infraerrors.Reason(err))
}

func TestGroupAccessGraphErrorsRemainComparable(t *testing.T) {
	source := graphGroup(1, "source")
	target := graphGroup(2, "subscription")
	target.SubscriptionType = SubscriptionTypeSubscription
	source.FallbackGroupID = groupGraphPtr(target.ID)

	err := ValidateGroupAccessGraph([]Group{source, target})

	expected := infraerrors.BadRequest(GroupFallbackReasonSubscriptionTarget, "different message")
	require.True(t, errors.Is(err, expected))
}
