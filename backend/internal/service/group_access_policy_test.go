package service

import (
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestGroupAccessPolicyDecisionMatrix(t *testing.T) {
	policy := NewGroupAccessPolicy()
	vipGroup := &Group{ID: 42, Status: StatusActive, VIPOnly: true}

	t.Run("public VIP group remains visible with payment action", func(t *testing.T) {
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			VIPAccessState: VIPAccessStatePaymentRequired,
		}, vipGroup, GroupAccessVisibility)
		require.True(t, decision.Visible)
		require.False(t, decision.Allowed)
		require.Equal(t, GroupAccessDenyVIPOnly, decision.Reason)
		require.Equal(t, GroupAccessActionPayment, decision.SuggestedAction)
		require.Equal(t, "GROUP_VIP_ONLY", infraerrors.Reason(decision.Error()))
	})

	t.Run("force off never receives payment action", func(t *testing.T) {
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			VIPAccessState: VIPAccessStateRestricted,
		}, vipGroup, GroupAccessVisibility)
		require.Equal(t, GroupAccessActionContactSupport, decision.SuggestedAction)
	})

	t.Run("activation pending never receives payment action", func(t *testing.T) {
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			VIPAccessState: VIPAccessStateActivationPending,
		}, vipGroup, GroupAccessVisibility)
		require.Equal(t, GroupAccessActionNone, decision.SuggestedAction)
	})

	t.Run("activation timeout receives support action", func(t *testing.T) {
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			VIPAccessState: VIPAccessStateActivationFailed,
		}, vipGroup, GroupAccessVisibility)
		require.Equal(t, GroupAccessActionContactSupport, decision.SuggestedAction)
	})

	t.Run("exclusive denial takes priority and hides group", func(t *testing.T) {
		group := *vipGroup
		group.IsExclusive = true
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			VIPAccessState: VIPAccessStatePaymentRequired,
		}, &group, GroupAccessVisibility)
		require.False(t, decision.Visible)
		require.Equal(t, GroupAccessDenyGroupNotAllowed, decision.Reason)
		require.Empty(t, decision.SuggestedAction)
	})

	t.Run("VIP can bind", func(t *testing.T) {
		decision := policy.Evaluate(&GroupAccessProfile{
			UserID:         7,
			IsVIP:          true,
			VIPAccessState: VIPAccessStateActive,
		}, vipGroup, GroupAccessBinding)
		require.True(t, decision.Visible)
		require.True(t, decision.Allowed)
		require.NoError(t, decision.Error())
	})
}

func TestGroupAccessPolicySubscriptionSemantics(t *testing.T) {
	policy := NewGroupAccessPolicy()
	group := &Group{
		ID:               9,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeSubscription,
	}
	profile := &GroupAccessProfile{UserID: 1}

	primary := policy.Evaluate(profile, group, GroupAccessPrimaryAuth)
	require.True(t, primary.Allowed, "original subscription auth stays endpoint-aware")

	binding := policy.Evaluate(profile, group, GroupAccessBinding)
	require.False(t, binding.Allowed)
	require.Equal(t, GroupAccessDenySubscriptionRequired, binding.Reason)
	require.Equal(t, "SUBSCRIPTION_REQUIRED", infraerrors.Reason(binding.Error()))
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(binding.Error()))

	profile.ActiveSubscriptionGroupIDs = []int64{9}
	binding = policy.Evaluate(profile, group, GroupAccessBinding)
	require.True(t, binding.Allowed)

	fallback := policy.Evaluate(profile, group, GroupAccessFallbackRuntime)
	require.False(t, fallback.Allowed)
	require.Equal(t, GroupAccessDenyFallbackInvalidConfig, fallback.Reason)
	require.Equal(t, "GROUP_FALLBACK_INVALID_CONFIG", infraerrors.Reason(fallback.Error()))
}

func TestGroupAccessPolicyUsesLegacyInactiveContract(t *testing.T) {
	decision := NewGroupAccessPolicy().Evaluate(
		&GroupAccessProfile{UserID: 1},
		&Group{ID: 9, Status: StatusDisabled},
		GroupAccessBinding,
	)

	require.False(t, decision.Allowed)
	require.Equal(t, GroupAccessDenyGroupInactive, decision.Reason)
	require.Equal(t, "GROUP_NOT_ACTIVE", infraerrors.Reason(decision.Error()))
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(decision.Error()))
}

func TestGroupAccessPolicyFailsClosedWithoutProfile(t *testing.T) {
	decision := NewGroupAccessPolicy().Evaluate(nil, &Group{ID: 1, Status: StatusActive}, GroupAccessPrimaryAuth)
	require.False(t, decision.Allowed)
	require.Equal(t, GroupAccessDenyProfileMissing, decision.Reason)
	require.Equal(t, "GROUP_ACCESS_PROFILE_MISSING", infraerrors.Reason(decision.Error()))
}
