//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 本文件验证 sticky_escape_on_full：粘性账号并发槽已满时，
//   - 存在其他有空闲容量的账号（不论优先级）→ 本次请求直接调度到空闲账号，绑定不改写；
//   - 关闭开关 / 其他账号也满载 → 维持原行为，在粘性账号上排队；
//   - 逃逸后其他账号也没抢到槽位 → 回到粘性账号排队（粘性等待参数），而不是去排其他满载账号；
//   - priority_tier_wait 开启时不逃逸到更低优先级层。

func newStickyEscapeService(accounts []Account, bindings map[string]int64, cache *mockConcurrencyCache) (*GatewayService, *mockGatewayCacheForPlatform) {
	svc, gwCache := newLoadAwareStickyService(accounts, bindings, nil)
	svc.cfg.Gateway.Scheduling.StickyEscapeOnFull = true
	svc.concurrencyService = NewConcurrencyService(cache)
	return svc, gwCache
}

func TestSelectAccountWithLoadAwareness_StickyEscapeOnFull(t *testing.T) {
	ctx := context.Background()
	const hash = "sess-escape"
	const model = "claude-3-5-sonnet-20241022"

	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 1},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 99, Status: StatusActive, Schedulable: true, Concurrency: 1},
	}
	stickyFull := map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
	}

	t.Run("escapes to idle lower-priority account without rewriting binding", func(t *testing.T) {
		svc, cache := newStickyEscapeService(accounts, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap:        stickyFull,
			acquireResults: map[int64]bool{1: false},
		})

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, int64(2), result.Account.ID, "saturated sticky account must not block an idle account")
		require.True(t, result.Acquired)
		require.Nil(t, result.WaitPlan)
		require.False(t, result.StickyMigrated)
		require.Equal(t, int64(1), cache.sessionBindings[hash], "binding must stay on the higher-priority sticky account")
	})

	t.Run("disabled keeps queueing on sticky account", func(t *testing.T) {
		svc, _ := newStickyEscapeService(accounts, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap:        stickyFull,
			acquireResults: map[int64]bool{1: false},
		})
		svc.cfg.Gateway.Scheduling.StickyEscapeOnFull = false

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.Account.ID)
		require.False(t, result.Acquired)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, svc.cfg.Gateway.Scheduling.StickySessionWaitTimeout, result.WaitPlan.Timeout)
	})

	t.Run("no idle alternative keeps queueing on sticky account", func(t *testing.T) {
		svc, _ := newStickyEscapeService(accounts, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 1, LoadRate: 100},
			},
			acquireResults: map[int64]bool{1: false, 2: false},
		})

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.Account.ID)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.WaitPlan.AccountID)
		require.Equal(t, svc.cfg.Gateway.Scheduling.StickySessionWaitTimeout, result.WaitPlan.Timeout)
		require.Equal(t, svc.cfg.Gateway.Scheduling.StickySessionMaxWaiting, result.WaitPlan.MaxWaiting)
	})

	t.Run("alternative lost the race falls back to sticky wait plan", func(t *testing.T) {
		// 负载缓存显示账号 2 空闲，但抢槽时已被占满：应回到粘性账号排队而不是排账号 2。
		accounts := []Account{
			{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 50, Status: StatusActive, Schedulable: true, Concurrency: 1},
			{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 1},
		}
		svc, _ := newStickyEscapeService(accounts, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap:        stickyFull,
			acquireResults: map[int64]bool{1: false, 2: false},
		})
		svc.cfg.Gateway.Scheduling.StickyPreferHigherPriority = false

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.Account.ID, "must queue on the sticky account, not on the higher-priority sibling")
		require.False(t, result.Acquired)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.WaitPlan.AccountID)
		require.Equal(t, svc.cfg.Gateway.Scheduling.StickySessionWaitTimeout, result.WaitPlan.Timeout)
	})

	t.Run("priority tier wait forbids escaping to a lower tier", func(t *testing.T) {
		svc, _ := newStickyEscapeService(accounts, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap:        stickyFull,
			acquireResults: map[int64]bool{1: false},
		})
		svc.cfg.Gateway.Scheduling.PriorityTierWait = true

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.Account.ID)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, svc.cfg.Gateway.Scheduling.StickySessionWaitTimeout, result.WaitPlan.Timeout)
	})

	t.Run("priority tier wait still escapes within the same tier", func(t *testing.T) {
		sameTier := []Account{
			{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 1},
			{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 1},
		}
		svc, cache := newStickyEscapeService(sameTier, map[string]int64{hash: 1}, &mockConcurrencyCache{
			loadMap:        stickyFull,
			acquireResults: map[int64]bool{1: false},
		})
		svc.cfg.Gateway.Scheduling.PriorityTierWait = true

		result, err := svc.SelectAccountWithLoadAwareness(ctx, nil, hash, model, nil, "", 0)
		require.NoError(t, err)
		require.Equal(t, int64(3), result.Account.ID)
		require.True(t, result.Acquired)
		require.Equal(t, int64(1), cache.sessionBindings[hash], "same-priority escape must not rewrite the binding")
	})
}
