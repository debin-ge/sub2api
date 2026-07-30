//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestResolveGatewayGroupAuthorizesEveryFallbackTarget(t *testing.T) {
	sourceID := int64(31)
	targetID := int64(32)
	repo := &mockGroupRepoForGateway{groups: map[int64]*Group{
		sourceID: {
			ID: sourceID, Status: StatusActive, ClaudeCodeOnly: true, FallbackGroupID: &targetID, Hydrated: true,
		},
		targetID: {
			ID: targetID, Status: StatusActive, VIPOnly: true, Hydrated: true,
		},
	}}
	profile := &GroupAccessProfile{UserID: 7, VIPAccessState: VIPAccessStatePaymentRequired}
	ctx := WithGroupAccessProfile(context.Background(), profile)

	enforce := &GatewayService{
		groupRepo: repo,
		cfg:       &config.Config{GroupAccessRuntimeMode: config.GroupAccessRuntimeModeEnforce},
	}
	_, _, err := enforce.resolveGatewayGroup(ctx, &sourceID)
	require.Equal(t, 403, infraerrors.Code(err))
	require.Equal(t, "GROUP_VIP_ONLY", infraerrors.Reason(err))

	audit := &GatewayService{
		groupRepo: repo,
		cfg:       &config.Config{GroupAccessRuntimeMode: config.GroupAccessRuntimeModeAuditOnly},
	}
	group, resolvedID, err := audit.resolveGatewayGroup(ctx, &sourceID)
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, targetID, group.ID)
	require.NotNil(t, resolvedID)
	require.Equal(t, targetID, *resolvedID)
}
