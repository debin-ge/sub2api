package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoAdmissionUserRepo struct {
	UserRepository
	user *User
}

func (repo *videoAdmissionUserRepo) GetByID(context.Context, int64) (*User, error) {
	return repo.user, nil
}

type videoAdmissionQuotaRepo struct {
	UserPlatformQuotaRepository
	record *UserPlatformQuotaRecord
	err    error
}

func (repo *videoAdmissionQuotaRepo) GetByUserPlatform(context.Context, int64, string) (*UserPlatformQuotaRecord, error) {
	return repo.record, repo.err
}

type videoAdmissionRateLoader struct {
	data *APIKeyRateLimitData
	err  error
}

func (loader *videoAdmissionRateLoader) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	return loader.data, loader.err
}

type videoAdmissionRPMStub struct {
	UserRPMCache
	count int
	err   error
}

func (cache *videoAdmissionRPMStub) IncrementUserGroupRPMOnce(context.Context, int64, int64, string) (int, error) {
	return cache.count, cache.err
}

func (cache *videoAdmissionRPMStub) IncrementUserRPMOnce(context.Context, int64, string) (int, error) {
	return cache.count, cache.err
}

func TestVideoAdmissionEnforcesWindowPlatformAndRPMLimits(t *testing.T) {
	for _, scenario := range []string{"key_window", "platform_window", "group_rpm", "user_rpm", "rate_db_error", "platform_db_error", "redis_error"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now()
			user := &User{ID: 42, Status: StatusActive, Balance: 20}
			group := &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive}
			key := &APIKey{ID: 3, UserID: user.ID, Status: StatusActive, User: user, Group: group, GroupID: &group.ID}
			quota := &videoAdmissionQuotaRepo{}
			rate := &videoAdmissionRateLoader{data: &APIKeyRateLimitData{Usage5h: 1, Window5hStart: &now}}
			rpm := &videoAdmissionRPMStub{count: 2}
			svc := &BillingCacheService{
				cfg: &config.Config{RunMode: config.RunModeStandard}, userRepo: &videoAdmissionUserRepo{user: user},
				userPlatformQuotaRepo: quota, apiKeyRateLimitLoader: rate, userRPMCache: rpm,
			}
			var expected error
			switch scenario {
			case "key_window":
				key.RateLimit5h = 1
				expected = ErrAPIKeyRateLimit5hExceeded
			case "platform_window":
				quota.record = &UserPlatformQuotaRecord{DailyUsageUSD: 1, DailyLimitUSD: floatPointer(1), DailyWindowStart: &now}
				expected = ErrUserPlatformDailyQuotaExhausted
			case "group_rpm":
				group.RPMLimit = 1
				expected = ErrGroupRPMExceeded
			case "user_rpm":
				user.RPMLimit = 1
				expected = ErrUserRPMExceeded
			case "rate_db_error":
				key.RateLimit5h = 1
				rate.err = errors.New("unavailable")
				expected = ErrBillingServiceUnavailable
			case "platform_db_error":
				quota.err = errors.New("unavailable")
				expected = ErrBillingServiceUnavailable
			case "redis_error":
				user.RPMLimit = 1
				rpm.err = errors.New("unavailable")
				expected = ErrBillingServiceUnavailable
			}
			require.ErrorIs(t, svc.CheckVideoAdmission(context.Background(), key, group, PlatformOpenAI, "stable-operation"), expected)
		})
	}
}

func TestVideoAdmissionRejectsBeforeTaskOrProviderAndAllowsReplay(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_provider_test", Status: VideoGenerationQueued}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	admission := svc.admission.(*videoAdmissionStub)
	admission.err = ErrAPIKeyRateLimit5hExceeded
	request := videoSubmitRequestForTest()
	_, err := svc.Submit(context.Background(), request)
	require.ErrorIs(t, err, ErrAPIKeyRateLimit5hExceeded)
	require.Nil(t, tasks.task)
	require.Zero(t, provider.createCalls)
	admission.err = nil
	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, admission.invalidations)
	tasks.preflightExisting = result.Task
	svc.cfg.Gateway.Video.CreationEnabled = false
	admission.err = ErrAPIKeyRateLimit5hExceeded
	checks := admission.checks
	replayed, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, result.Task.PublicID, replayed.Task.PublicID)
	require.Equal(t, checks, admission.checks)
	require.Equal(t, 1, provider.createCalls)
}
