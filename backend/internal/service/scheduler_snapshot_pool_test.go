//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 本文件验证调度快照"存配置池、读取时判断瞬时态"的语义：
//   - 桶内处于限流/过载冷却中的账号在读取时被过滤；
//   - 冷却到期后无需任何事件即可重新可见；
//   - 仓储实现 schedulerPoolAccountLister 时，桶重建写入完整配置池。

func TestFilterSchedulableSnapshotAccounts(t *testing.T) {
	future := time.Now().Add(time.Minute)
	past := time.Now().Add(-time.Minute)
	accounts := []Account{
		{ID: 1, Status: StatusActive, Schedulable: true},
		{ID: 2, Status: StatusActive, Schedulable: true, RateLimitResetAt: &future},
		{ID: 3, Status: StatusActive, Schedulable: true, RateLimitResetAt: &past},
		{ID: 4, Status: StatusActive, Schedulable: true, OverloadUntil: &future},
		{ID: 5, Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &future},
		{ID: 6, Status: StatusDisabled, Schedulable: true},
	}

	filtered := filterSchedulableSnapshotAccounts(accounts)
	ids := make([]int64, 0, len(filtered))
	for _, acc := range filtered {
		ids = append(ids, acc.ID)
	}
	require.Equal(t, []int64{1, 3}, ids)
	require.Len(t, accounts, 6, "input slice must not be mutated")
}

func TestSchedulerSnapshotListSchedulableAccounts_FiltersTransientStateAtReadTime(t *testing.T) {
	groupID := int64(7)
	cooling := time.Now().Add(5 * time.Second)
	recovered := time.Now().Add(-time.Second)
	cache := &snapshotHydrationCache{
		snapshot: []*Account{
			{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1, RateLimitResetAt: &cooling},
			{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 99},
			{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1, RateLimitResetAt: &recovered},
		},
	}
	svc := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)

	accounts, useMixed, err := svc.ListSchedulableAccounts(context.Background(), &groupID, PlatformAnthropic, false)
	require.NoError(t, err)
	require.True(t, useMixed)
	ids := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		ids = append(ids, acc.ID)
	}
	require.Equal(t, []int64{2, 3}, ids, "cooling account hidden, recovered account visible without any rebuild")
}

// poolListerRepo 实现 schedulerPoolAccountLister，返回包含冷却中账号的完整配置池。
type poolListerRepo struct {
	mockAccountRepoForPlatform
	poolCalls int
}

func (r *poolListerRepo) ListSchedulablePoolByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]Account, error) {
	return r.pool(platforms), nil
}

func (r *poolListerRepo) ListSchedulablePoolByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.pool(platforms), nil
}

func (r *poolListerRepo) ListSchedulablePoolUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.pool(platforms), nil
}

func (r *poolListerRepo) pool(platforms []string) []Account {
	r.poolCalls++
	set := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		set[p] = true
	}
	var out []Account
	for _, acc := range r.accounts {
		if set[acc.Platform] && acc.Status == StatusActive && acc.Schedulable {
			out = append(out, acc)
		}
	}
	return out
}

// snapshotMissCache 强制走 DB 回源，并记录发布到桶里的账号。
type snapshotMissCache struct {
	snapshotHydrationCache
	published []Account
}

func (c *snapshotMissCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return nil, false, nil
}

func (c *snapshotMissCache) SetSnapshot(_ context.Context, _ SchedulerBucket, _ SchedulerBucketWriteToken, accounts []Account) error {
	c.published = append([]Account(nil), accounts...)
	return nil
}

func TestSchedulerSnapshotLoadAccountsFromDB_UsesPoolListerAndFiltersOnRead(t *testing.T) {
	groupID := int64(9)
	cooling := time.Now().Add(5 * time.Second)
	repo := &poolListerRepo{}
	repo.accounts = []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1, RateLimitResetAt: &cooling},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 99},
		{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusDisabled, Schedulable: true, Priority: 1},
	}
	cache := &snapshotMissCache{}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)

	accounts, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, PlatformAnthropic, true)
	require.NoError(t, err)
	require.Equal(t, 1, repo.poolCalls, "snapshot must load the configured pool via the pool lister")

	publishedIDs := make([]int64, 0, len(cache.published))
	for _, acc := range cache.published {
		publishedIDs = append(publishedIDs, acc.ID)
	}
	require.Equal(t, []int64{1, 2}, publishedIDs, "bucket keeps the cooling account so it reappears when the cooldown expires")

	returnedIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		returnedIDs = append(returnedIDs, acc.ID)
	}
	require.Equal(t, []int64{2}, returnedIDs, "callers still only see currently schedulable accounts")
}
