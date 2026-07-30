package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestApplyGroupAccessRuntimeDecisionRollout(t *testing.T) {
	policy := NewGroupAccessPolicy()
	nonVIP := &GroupAccessProfile{UserID: 7, VIPAccessState: VIPAccessStatePaymentRequired}

	t.Run("audit bypasses new primary VIP denial", func(t *testing.T) {
		decision := policy.Evaluate(nonVIP, &Group{ID: 1, Status: StatusActive, VIPOnly: true}, GroupAccessPrimaryAuth)
		require.NoError(t, ApplyGroupAccessRuntimeDecision(
			config.GroupAccessRuntimeModeAuditOnly,
			GroupAccessRuntimeEntryPrimaryAuth,
			decision,
			7,
			1,
		))
	})

	t.Run("audit preserves pre-existing exclusive primary denial", func(t *testing.T) {
		decision := policy.Evaluate(nonVIP, &Group{ID: 2, Status: StatusActive, IsExclusive: true}, GroupAccessPrimaryAuth)
		err := ApplyGroupAccessRuntimeDecision(
			config.GroupAccessRuntimeModeAuditOnly,
			GroupAccessRuntimeEntryPrimaryAuth,
			decision,
			7,
			2,
		)
		require.Equal(t, "GROUP_NOT_ALLOWED", infraerrors.Reason(err))
		require.Equal(t, 403, infraerrors.Code(err))
	})

	t.Run("enforce returns exact primary VIP denial", func(t *testing.T) {
		decision := policy.Evaluate(nonVIP, &Group{ID: 3, Status: StatusActive, VIPOnly: true}, GroupAccessPrimaryAuth)
		err := ApplyGroupAccessRuntimeDecision(
			config.GroupAccessRuntimeModeEnforce,
			GroupAccessRuntimeEntryPrimaryAuth,
			decision,
			7,
			3,
		)
		require.Equal(t, "GROUP_VIP_ONLY", infraerrors.Reason(err))
		require.Equal(t, 403, infraerrors.Code(err))
	})

	t.Run("audit bypasses new fallback structural and profile denials", func(t *testing.T) {
		for _, decision := range []GroupAccessDecision{
			policy.Evaluate(nonVIP, &Group{ID: 4, Status: StatusActive, IsExclusive: true}, GroupAccessFallbackRuntime),
			policy.Evaluate(nonVIP, &Group{ID: 5, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}, GroupAccessFallbackRuntime),
			policy.Evaluate(nil, &Group{ID: 6, Status: StatusActive}, GroupAccessFallbackRuntime),
		} {
			require.NoError(t, ApplyGroupAccessRuntimeDecision(
				config.GroupAccessRuntimeModeAuditOnly,
				GroupAccessRuntimeEntryFallback,
				decision,
				7,
				4,
			))
		}
	})

	t.Run("enforce returns exact fallback errors", func(t *testing.T) {
		for _, tt := range []struct {
			decision GroupAccessDecision
			code     int
			reason   string
		}{
			{policy.Evaluate(nonVIP, &Group{ID: 7, Status: StatusActive, VIPOnly: true}, GroupAccessFallbackRuntime), 403, "GROUP_VIP_ONLY"},
			{policy.Evaluate(nonVIP, &Group{ID: 8, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}, GroupAccessFallbackRuntime), 503, "GROUP_FALLBACK_INVALID_CONFIG"},
			{policy.Evaluate(nil, &Group{ID: 9, Status: StatusActive}, GroupAccessFallbackRuntime), 500, "GROUP_ACCESS_PROFILE_MISSING"},
		} {
			err := ApplyGroupAccessRuntimeDecision(
				config.GroupAccessRuntimeModeEnforce,
				GroupAccessRuntimeEntryFallback,
				tt.decision,
				7,
				7,
			)
			require.Equal(t, tt.code, infraerrors.Code(err))
			require.Equal(t, tt.reason, infraerrors.Reason(err))
		}
	})

	t.Run("unknown runtime mode fails closed", func(t *testing.T) {
		err := ApplyGroupAccessRuntimeDecision(
			"shadow",
			GroupAccessRuntimeEntryPrimaryAuth,
			GroupAccessDecision{Allowed: true},
			7,
			1,
		)
		require.Equal(t, 500, infraerrors.Code(err))
		require.Equal(t, "GROUP_ACCESS_RUNTIME_CONFIG_INVALID", infraerrors.Reason(err))
	})
}

func TestGroupAccessRuntimeMetricsRecordWouldDeny(t *testing.T) {
	before := groupAccessRuntimeMetricCount(
		config.GroupAccessRuntimeModeAuditOnly,
		GroupAccessRuntimeOutcomeWouldDeny,
		GroupAccessDenyVIPOnly,
		GroupAccessRuntimeEntryGooglePrimaryAuth,
	)
	require.NoError(t, ApplyGroupAccessRuntimeDecision(
		config.GroupAccessRuntimeModeAuditOnly,
		GroupAccessRuntimeEntryGooglePrimaryAuth,
		GroupAccessDecision{Reason: GroupAccessDenyVIPOnly},
		7,
		10,
	))
	after := groupAccessRuntimeMetricCount(
		config.GroupAccessRuntimeModeAuditOnly,
		GroupAccessRuntimeOutcomeWouldDeny,
		GroupAccessDenyVIPOnly,
		GroupAccessRuntimeEntryGooglePrimaryAuth,
	)
	require.Equal(t, before+1, after)
}

func TestGatewayServiceResolveAuthorizedFallback(t *testing.T) {
	nonVIP := &GroupAccessProfile{UserID: 7, VIPAccessState: VIPAccessStatePaymentRequired}
	ctx := WithGroupAccessProfile(context.Background(), nonVIP)
	vipGroup := &Group{ID: 20, Status: StatusActive, VIPOnly: true}

	audit := &GatewayService{cfg: &config.Config{GroupAccessRuntimeMode: config.GroupAccessRuntimeModeAuditOnly}}
	require.NoError(t, audit.ResolveAuthorizedFallback(ctx, nil, vipGroup, GroupAccessRuntimeEntryFallback))

	enforce := &GatewayService{cfg: &config.Config{GroupAccessRuntimeMode: config.GroupAccessRuntimeModeEnforce}}
	err := enforce.ResolveAuthorizedFallback(ctx, nil, vipGroup, GroupAccessRuntimeEntryFallback)
	require.Equal(t, 403, infraerrors.Code(err))
	require.Equal(t, "GROUP_VIP_ONLY", infraerrors.Reason(err))

	err = enforce.ResolveAuthorizedFallback(
		ctx,
		nil,
		&Group{ID: 21, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		GroupAccessRuntimeEntryInvalidRequestFallback,
	)
	require.Equal(t, 503, infraerrors.Code(err))
	require.Equal(t, "GROUP_FALLBACK_INVALID_CONFIG", infraerrors.Reason(err))

	ctxWithoutProfile := context.WithValue(context.Background(), ctxkey.UserID, int64(7))
	err = enforce.ResolveAuthorizedFallback(
		ctxWithoutProfile,
		nil,
		&Group{ID: 22, Status: StatusActive},
		GroupAccessRuntimeEntryFallback,
	)
	require.Equal(t, 500, infraerrors.Code(err))
	require.Equal(t, "GROUP_ACCESS_PROFILE_MISSING", infraerrors.Reason(err))
}

func TestGatewayServiceAuthorizedLegacyFallbackRecordsDriftWithoutDenying(t *testing.T) {
	enforce := &GatewayService{cfg: &config.Config{GroupAccessRuntimeMode: config.GroupAccessRuntimeModeEnforce}}
	source := &Group{
		ID:               30,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
	}

	t.Run("vip escalation", func(t *testing.T) {
		entry := GroupAccessRuntimeEntryFallback
		reason := GroupFallbackReasonVIPEscalation
		before := groupAccessRuntimeDriftMetricCount(reason, entry)
		ctx := WithGroupAccessProfile(context.Background(), &GroupAccessProfile{
			UserID: 7,
			IsVIP:  true,
		})

		err := enforce.ResolveAuthorizedFallback(
			ctx,
			source,
			&Group{ID: 31, Status: StatusActive, VIPOnly: true},
			entry,
		)

		require.NoError(t, err)
		require.Equal(t, before+1, groupAccessRuntimeDriftMetricCount(reason, entry))
	})

	t.Run("exclusive escalation", func(t *testing.T) {
		entry := GroupAccessRuntimeEntryInvalidRequestFallback
		reason := GroupFallbackReasonExclusiveEscalation
		before := groupAccessRuntimeDriftMetricCount(reason, entry)
		ctx := WithGroupAccessProfile(context.Background(), &GroupAccessProfile{
			UserID:        7,
			AllowedGroups: []int64{32},
		})

		err := enforce.ResolveAuthorizedFallback(
			ctx,
			source,
			&Group{ID: 32, Status: StatusActive, IsExclusive: true},
			entry,
		)

		require.NoError(t, err)
		require.Equal(t, before+1, groupAccessRuntimeDriftMetricCount(reason, entry))
	})
}

func groupAccessRuntimeMetricCount(
	mode string,
	outcome GroupAccessRuntimeOutcome,
	reason GroupAccessDenyReason,
	entry GroupAccessRuntimeEntry,
) uint64 {
	for _, metric := range GroupAccessRuntimeMetricsSnapshot() {
		if metric.Mode == mode && metric.Outcome == outcome && metric.Reason == reason && metric.Entry == entry {
			return metric.Count
		}
	}
	return 0
}

func groupAccessRuntimeDriftMetricCount(
	reason string,
	entry GroupAccessRuntimeEntry,
) uint64 {
	for _, metric := range GroupAccessRuntimeDriftMetricsSnapshot() {
		if metric.Reason == reason && metric.Entry == entry {
			return metric.Count
		}
	}
	return 0
}
