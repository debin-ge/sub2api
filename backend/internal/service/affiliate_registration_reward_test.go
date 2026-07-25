//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAffiliateService_BindInviterAndGrantRegistrationReward_UsesSettingsSnapshot(t *testing.T) {
	repo := &affiliateRepoStub{codeOwners: map[string]int64{"AFF123": 7}}
	settingService := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                  "true",
		SettingKeyAffiliateRegistrationRewardAmount: "10.123456789",
		SettingKeyAffiliateRebatePerInviteeCap:      "25.5",
		SettingKeyAffiliateRebateFreezeHours:        "24",
	}}, &config.Config{})
	svc := NewAffiliateService(repo, settingService, nil, nil)

	result, err := svc.BindInviterAndGrantRegistrationReward(
		context.Background(),
		42,
		" aff123 ",
	)

	require.NoError(t, err)
	require.True(t, result.Bound)
	require.True(t, result.RewardApplied)
	require.InDelta(t, 10.12345679, result.RewardAmount, 1e-9)
	require.Equal(t, 1, len(repo.registrationRewardCalls))
	require.Equal(t, int64(42), repo.registrationRewardCalls[0].userID)
	require.Equal(t, "AFF123", repo.registrationRewardCalls[0].code)
	require.InDelta(t, 10.12345679, repo.registrationRewardCalls[0].reward, 1e-9)
	require.InDelta(t, 25.5, repo.registrationRewardCalls[0].perUserCap, 1e-9)
	require.Equal(t, 24, repo.registrationRewardCalls[0].freezeHours)
}

func TestAffiliateService_BindInviterAndGrantRegistrationReward_ZeroStillBinds(t *testing.T) {
	repo := &affiliateRepoStub{codeOwners: map[string]int64{"AFF123": 7}}
	settingService := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                  "true",
		SettingKeyAffiliateRegistrationRewardAmount: "0",
	}}, &config.Config{})
	svc := NewAffiliateService(repo, settingService, nil, nil)

	result, err := svc.BindInviterAndGrantRegistrationReward(context.Background(), 42, "AFF123")

	require.NoError(t, err)
	require.True(t, result.Bound)
	require.False(t, result.RewardApplied)
	require.Len(t, repo.registrationRewardCalls, 1)
	require.Zero(t, repo.registrationRewardCalls[0].reward)
}

func TestAffiliateService_EffectiveRegistrationRewardAmount_RespectsPerInviteeCap(t *testing.T) {
	settingService := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                  "true",
		SettingKeyAffiliateRegistrationRewardAmount: "10",
		SettingKeyAffiliateRebatePerInviteeCap:      "6.5",
	}}, &config.Config{})
	svc := NewAffiliateService(&affiliateRepoStub{}, settingService, nil, nil)

	require.InDelta(t, 6.5, svc.effectiveRegistrationRewardAmount(context.Background()), 1e-9)
}

func TestAffiliateService_RegistrationRewardDisabledAndDatabaseFailure(t *testing.T) {
	repo := &affiliateRepoStub{
		codeOwners:            map[string]int64{"AFF123": 7},
		registrationRewardErr: errors.New("database unavailable"),
	}
	disabled := NewAffiliateService(repo, NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "false",
	}}, &config.Config{}), nil, nil)

	result, err := disabled.BindInviterAndGrantRegistrationReward(context.Background(), 42, "AFF123")
	require.NoError(t, err)
	require.False(t, result.Bound)
	require.Empty(t, repo.registrationRewardCalls)
	require.ErrorIs(t, disabled.ValidateAffiliateRegistrationCode(context.Background(), "AFF123"), ErrAffiliateDisabled)

	enabled := NewAffiliateService(repo, NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                  "true",
		SettingKeyAffiliateRegistrationRewardAmount: "10",
	}}, &config.Config{}), nil, nil)
	_, err = enabled.BindInviterAndGrantRegistrationReward(context.Background(), 42, "AFF123")
	require.Error(t, err)
	require.Equal(t, "SERVICE_UNAVAILABLE", infraerrors.Reason(err))
}

func TestAffiliateService_AccrueInviteRebate_RegistrationRewardConsumesPerInviteeCap(t *testing.T) {
	inviterID := int64(7)
	cappedAmount := 2.0
	repo := &paymentFulfillmentAffiliateRepoStub{
		inviteeSummary: &AffiliateSummary{
			UserID:    42,
			InviterID: &inviterID,
			CreatedAt: time.Now(),
		},
		inviterSummary: &AffiliateSummary{
			UserID:  inviterID,
			AffCode: "AFF123",
		},
		accrueAppliedAmount: &cappedAmount,
	}
	settingService := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:             "true",
		SettingKeyAffiliateRebateRate:          "20",
		SettingKeyAffiliateRebatePerInviteeCap: "10",
	}}, &config.Config{})
	svc := NewAffiliateService(repo, settingService, nil, nil)

	// 20% of 20 requests a rebate of 4. The repository atomically applies only
	// the remaining 2 after including the existing registration reward.
	rebate, err := svc.AccrueInviteRebate(context.Background(), 42, 20)

	require.NoError(t, err)
	require.InDelta(t, 2, rebate, 1e-9)
	require.Len(t, repo.accrueCalls, 1)
	require.InDelta(t, 4, repo.accrueCalls[0].amount, 1e-9)
	require.InDelta(t, 10, repo.accrueCalls[0].perInviteeCap, 1e-9)
}

func TestAuthService_BindOAuthAffiliateWithoutCodeRemainsLazy(t *testing.T) {
	repo := &affiliateRepoStub{}
	authService := &AuthService{
		affiliateService: NewAffiliateService(repo, nil, nil, nil),
	}

	require.NoError(t, authService.bindOAuthAffiliate(context.Background(), 42, ""))
	require.Empty(t, repo.ensureUserIDs)
	require.Empty(t, repo.registrationRewardCalls)
}
