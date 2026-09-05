//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 本文件验证粘性会话"只升不降"绑定规则与 Layer 1.5 的高优先级迁回逻辑：
//   - 高优先级账号短暂限流时，请求临时落到低优先级兜底账号，但绑定不改写；
//   - 高优先级账号恢复且有容量时，绑定到低优先级账号的会话自动迁回，并标记 StickyMigrated；
//   - 高优先级账号满载时不迁回；
//   - priority_tier_wait 开启时最高优先级层满载在该层排队而不下沉。
//
// 绑定账号状态来自调度快照（Redis 账号 meta），因此测试使用快照后端而不是仓储。

// countingSnapshotCache 在 snapshotHydrationCache 之上统计 GetAccount 调用次数。
type countingSnapshotCache struct {
	snapshotHydrationCache
	getAccountCalls int
}

func (c *countingSnapshotCache) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	c.getAccountCalls++
	return c.snapshotHydrationCache.GetAccount(ctx, accountID)
}

func newStickySnapshot(accounts []Account) (*SchedulerSnapshotService, *countingSnapshotCache) {
	cache := &countingSnapshotCache{}
	cache.accounts = make(map[int64]*Account, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		cache.snapshot = append(cache.snapshot, &acc)
		cache.accounts[acc.ID] = &acc
	}
	return NewSchedulerSnapshotService(cache, nil, nil, nil, nil), cache
}

func TestBindStickySessionPreservingPriority(t *testing.T) {
	ctx := context.Background()
	const hash = "sess-preserve"
	future := time.Now().Add(time.Hour)

	high := Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 3}
	low := Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 99, Status: StatusActive, Schedulable: true, Concurrency: 3}
	highRateLimited := high
	highRateLimited.RateLimitResetAt = &future
	highDisabled := high
	highDisabled.Status = StatusDisabled

	newSvc := func(accounts []Account, bindings map[string]int64) (*GatewayService, *mockGatewayCacheForPlatform, *countingSnapshotCache) {
		gwCache := &mockGatewayCacheForPlatform{sessionBindings: bindings}
		snap, snapCache := newStickySnapshot(accounts)
		return &GatewayService{schedulerSnapshot: snap, cache: gwCache, cfg: testConfig()}, gwCache, snapCache
	}

	t.Run("no binding creates", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{high, low}, nil)
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindCreated, decision)
		require.Equal(t, low.ID, cache.sessionBindings[hash])
		require.False(t, decision.migrated())
	})

	t.Run("same account refreshes", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{high, low}, map[string]int64{hash: high.ID})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, high.ID, &high, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindRefreshed, decision)
		require.Equal(t, high.ID, cache.sessionBindings[hash])
	})

	t.Run("fallback to lower priority does not downgrade binding", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{highRateLimited, low}, map[string]int64{hash: high.ID})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindSkipped, decision)
		require.Equal(t, high.ID, cache.sessionBindings[hash], "binding to the rate-limited high-priority account must survive")
	})

	t.Run("higher priority upgrades binding", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{high, low}, map[string]int64{hash: low.ID})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, high.ID, &high, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindUpgraded, decision)
		require.True(t, decision.migrated())
		require.Equal(t, high.ID, cache.sessionBindings[hash])
	})

	t.Run("known bound account skips lookup", func(t *testing.T) {
		svc, cache, snapCache := newSvc([]Account{high, low}, map[string]int64{hash: high.ID})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, high.ID, &high)
		require.NoError(t, err)
		require.Equal(t, stickyBindSkipped, decision)
		require.Zero(t, snapCache.getAccountCalls, "caller-provided bound account must avoid a snapshot lookup")
		require.Equal(t, high.ID, cache.sessionBindings[hash])
	})

	t.Run("permanently unschedulable bound account is replaced", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{highDisabled, low}, map[string]int64{hash: high.ID})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindReplaced, decision)
		require.True(t, decision.migrated())
		require.Equal(t, low.ID, cache.sessionBindings[hash])
	})

	t.Run("bound account missing from snapshot is replaced", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{low}, map[string]int64{hash: 777})
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindReplaced, decision)
		require.Equal(t, low.ID, cache.sessionBindings[hash])
	})

	t.Run("without snapshot service falls back to eager bind", func(t *testing.T) {
		cache := &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{hash: high.ID}}
		svc := &GatewayService{cache: cache, cfg: testConfig()}
		decision, err := svc.bindStickySessionPreservingPriority(ctx, nil, hash, low.ID, &low, 0, nil)
		require.NoError(t, err)
		require.Equal(t, stickyBindReplaced, decision, "bound account state unknown → legacy overwrite")
		require.Equal(t, low.ID, cache.sessionBindings[hash])
	})

	t.Run("profit-admission path without gate also preserves priority", func(t *testing.T) {
		svc, cache, _ := newSvc([]Account{highRateLimited, low}, map[string]int64{hash: high.ID})
		require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, hash, low.ID))
		require.Equal(t, high.ID, cache.sessionBindings[hash])
		require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, hash, high.ID))
		require.Equal(t, high.ID, cache.sessionBindings[hash])
	})
}

func newLoadAwareStickyService(accounts []Account, bindings map[string]int64, loadMap map[int64]*AccountLoadInfo) (*GatewayService, *mockGatewayCacheForPlatform) {
	gwCache := &mockGatewayCacheForPlatform{sessionBindings: bindings}
	snap, _ := newStickySnapshot(accounts)
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.StickyPreferHigherPriority = true
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 30 * time.Second
	cfg.Gateway.Scheduling.FallbackMaxWaiting = 100
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	svc := &GatewayService{
		schedulerSnapshot:  snap,
		cache:              gwCache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{loadMap: loadMap}),
	}
	return svc, gwCache
}

func TestSelectAccountWithLoadAwareness_TransientRateLimitDoesNotDowngradeSticky(t *testing.T) {
	ctx := context.Background()
	const hash = "sess-transient"
	const model = "claude-3-5-sonnet-20241022"
	future := time.Now().Add(5 * time.Second)

	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 3, RateLimitResetAt: &future},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 99, Status: StatusActive, Schedulable: true, Concurrency: 3},
	}
	svc, cache := newLoadAwareStickyService(accounts, map[string]int64{hash: 1}, nil)

	result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(2), result.Account.ID, "request should temporarily fall back to the API key account")
	require.False(t, result.StickyMigrated)
	require.Equal(t, int64(1), cache.sessionBindings[hash], "binding must stay on the rate-limited OAuth account")
	require.Zero(t, cache.deletedSessions[hash], "a transient rate limit must not clear the binding")
}

func TestSelectAccountWithLoadAwareness_StickyMigratesBackToHigherPriority(t *testing.T) {
	ctx := context.Background()
	const hash = "sess-migrate"
	const model = "claude-3-5-sonnet-20241022"

	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 3},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 99, Status: StatusActive, Schedulable: true, Concurrency: 3},
	}

	t.Run("enabled migrates and flags cache billing", func(t *testing.T) {
		svc, cache := newLoadAwareStickyService(accounts, map[string]int64{hash: 2}, nil)

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, int64(1), result.Account.ID, "healthy higher-priority account wins over sticky fallback")
		require.True(t, result.StickyMigrated, "scheduler-driven switch must be flagged for cache billing")
		require.Equal(t, int64(1), cache.sessionBindings[hash], "binding upgraded to the higher-priority account")
	})

	t.Run("disabled honors sticky", func(t *testing.T) {
		svc, cache := newLoadAwareStickyService(accounts, map[string]int64{hash: 2}, nil)
		svc.cfg.Gateway.Scheduling.StickyPreferHigherPriority = false

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Account.ID)
		require.False(t, result.StickyMigrated)
		require.Equal(t, int64(2), cache.sessionBindings[hash])
	})

	t.Run("saturated higher priority does not migrate", func(t *testing.T) {
		loadMap := map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 3, LoadRate: 100},
		}
		svc, cache := newLoadAwareStickyService(accounts, map[string]int64{hash: 2}, loadMap)

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Account.ID, "no capacity on the higher-priority account: keep the sticky account")
		require.False(t, result.StickyMigrated)
		require.Equal(t, int64(2), cache.sessionBindings[hash])
	})
}

func TestSelectAccountWithLoadAwareness_PriorityTierWait(t *testing.T) {
	ctx := context.Background()
	const model = "claude-3-5-sonnet-20241022"

	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 3},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 99, Status: StatusActive, Schedulable: true, Concurrency: 3},
	}
	loadMap := map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 3, LoadRate: 100},
	}

	t.Run("enabled waits on saturated top tier", func(t *testing.T) {
		svc, _ := newLoadAwareStickyService(accounts, nil, loadMap)
		svc.cfg.Gateway.Scheduling.PriorityTierWait = true

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, int64(1), result.Account.ID, "should queue on the saturated highest-priority account")
		require.False(t, result.Acquired)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.WaitPlan.AccountID)
		require.Equal(t, svc.cfg.Gateway.Scheduling.FallbackWaitTimeout, result.WaitPlan.Timeout)
		require.Equal(t, svc.cfg.Gateway.Scheduling.FallbackMaxWaiting, result.WaitPlan.MaxWaiting)
	})

	t.Run("enabled but queue full falls through", func(t *testing.T) {
		svc, _ := newLoadAwareStickyService(accounts, nil, loadMap)
		svc.cfg.Gateway.Scheduling.PriorityTierWait = true
		svc.cfg.Gateway.Scheduling.FallbackMaxWaiting = 1
		svc.concurrencyService = NewConcurrencyService(&mockConcurrencyCache{loadMap: loadMap, waitCounts: map[int64]int{1: 1}})

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Account.ID)
		require.True(t, result.Acquired)
	})

	t.Run("disabled falls through immediately", func(t *testing.T) {
		svc, _ := newLoadAwareStickyService(accounts, nil, loadMap)
		svc.cfg.Gateway.Scheduling.PriorityTierWait = false

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Account.ID)
		require.True(t, result.Acquired)
	})
}
