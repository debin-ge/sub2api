//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_GetAffiliateRegistrationRewardAmount(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
	}{
		{name: "missing", want: 0},
		{name: "configured", raw: "10.123456789", want: 10.12345679},
		{name: "negative", raw: "-1", want: 0},
		{name: "nan", raw: "NaN", want: 0},
		{name: "infinity", raw: "+Inf", want: 0},
		{name: "over range clamps", raw: "1000000000000", want: AffiliateRegistrationRewardAmountMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{}
			if tt.raw != "" {
				values[SettingKeyAffiliateRegistrationRewardAmount] = tt.raw
			}
			svc := NewSettingService(&settingRepoStub{values: values}, &config.Config{})
			require.InDelta(
				t,
				tt.want,
				svc.GetAffiliateRegistrationRewardAmount(context.Background()),
				1e-9,
			)
		})
	}
}

func TestNormalizeAffiliateRegistrationReward(t *testing.T) {
	require.Zero(t, normalizeAffiliateRegistrationReward(math.NaN()))
	require.Zero(t, normalizeAffiliateRegistrationReward(math.Inf(1)))
	require.Zero(t, normalizeAffiliateRegistrationReward(-0.01))
	require.Equal(t, AffiliateRegistrationRewardAmountMax, normalizeAffiliateRegistrationReward(AffiliateRegistrationRewardAmountMax))
	require.Equal(t, AffiliateRegistrationRewardAmountMax, normalizeAffiliateRegistrationReward(AffiliateRegistrationRewardAmountMax+1))
	require.InDelta(t, 1.23456789, normalizeAffiliateRegistrationReward(1.23456789), 1e-9)
}

func TestSettingService_IsInvitationCodeRequired(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled invitation feature is never required", func(t *testing.T) {
		svc := NewSettingService(&settingRepoStub{values: map[string]string{
			SettingKeyInvitationCodeEnabled:  "false",
			SettingKeyInvitationCodeRequired: "true",
		}}, &config.Config{})
		require.False(t, svc.IsInvitationCodeRequired(ctx))
	})

	t.Run("missing setting preserves legacy required behavior", func(t *testing.T) {
		svc := NewSettingService(&settingRepoStub{values: map[string]string{
			SettingKeyInvitationCodeEnabled: "true",
		}}, &config.Config{})
		require.True(t, svc.IsInvitationCodeRequired(ctx))
	})

	t.Run("explicit optional setting is honored", func(t *testing.T) {
		svc := NewSettingService(&settingRepoStub{values: map[string]string{
			SettingKeyInvitationCodeEnabled:  "true",
			SettingKeyInvitationCodeRequired: "false",
		}}, &config.Config{})
		require.False(t, svc.IsInvitationCodeRequired(ctx))
	})
}
