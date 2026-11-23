package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 本文件验证 OpenAI 调度在账号并发满载时不放着空闲账号不用：
//   - 旧版负载感知调度（默认路径）的粘性满载逃逸（sticky_escape_on_full）；
//   - 高级调度器 TopK 全部满载时继续尝试 TopK 之外的空闲账号。

func newOpenAIStickyEscapeTestService(accounts []Account, binding int64, cc stubConcurrencyCache) (*OpenAIGatewayService, *stubGatewayCache) {
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"openai:sticky-escape": binding}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 30 * time.Second
	cfg.Gateway.Scheduling.FallbackMaxWaiting = 100
	cfg.Gateway.Scheduling.StickyEscapeOnFull = true
	return &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cc),
	}, cache
}

func TestOpenAISelectAccountWithLoadAwareness_StickyEscapeOnFull(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1)
	const sessionHash = "sticky-escape"
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}},
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 50, GroupIDs: []int64{groupID}},
	}
	stickyFull := map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
		2: {AccountID: 2, LoadRate: 0},
	}

	t.Run("escapes to idle lower-priority account without rewriting binding", func(t *testing.T) {
		svc, cache := newOpenAIStickyEscapeTestService(accounts, 1, stubConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap:        stickyFull,
		})

		selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, sessionHash, "gpt-4", nil)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, int64(2), selection.Account.ID, "saturated sticky account must not block an idle account")
		require.True(t, selection.Acquired)
		require.Nil(t, selection.WaitPlan)
		require.Equal(t, int64(1), cache.sessionBindings["openai:"+sessionHash], "escape must not migrate the durable binding")
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	})

	t.Run("disabled keeps queueing on sticky account", func(t *testing.T) {
		svc, _ := newOpenAIStickyEscapeTestService(accounts, 1, stubConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap:        stickyFull,
		})
		svc.cfg.Gateway.Scheduling.StickyEscapeOnFull = false

		selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, sessionHash, "gpt-4", nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), selection.Account.ID)
		require.False(t, selection.Acquired)
		require.NotNil(t, selection.WaitPlan)
		require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
	})

	t.Run("no idle alternative falls back to sticky wait plan", func(t *testing.T) {
		// 账号 2 优先级更高：未修复前 Layer 3 会在账号 2 上排队，修复后应回到粘性账号排队。
		accounts := []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 50, GroupIDs: []int64{groupID}},
			{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}},
		}
		svc, _ := newOpenAIStickyEscapeTestService(accounts, 1, stubConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false},
			loadMap:        stickyFull,
		})

		selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, sessionHash, "gpt-4", nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), selection.Account.ID)
		require.False(t, selection.Acquired)
		require.NotNil(t, selection.WaitPlan)
		require.Equal(t, int64(1), selection.WaitPlan.AccountID)
		require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
		require.Equal(t, 3, selection.WaitPlan.MaxWaiting)
	})

	t.Run("priority tier wait forbids escaping to a lower tier", func(t *testing.T) {
		svc, _ := newOpenAIStickyEscapeTestService(accounts, 1, stubConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap:        stickyFull,
		})
		svc.cfg.Gateway.Scheduling.PriorityTierWait = true

		selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, sessionHash, "gpt-4", nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), selection.Account.ID)
		require.NotNil(t, selection.WaitPlan)
		require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
	})
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_TriesIdleAccountsOutsideTopK(t *testing.T) {
	ctx := context.Background()
	groupID := int64(12)
	accounts := []Account{
		{ID: 4001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 4002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 4003, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10},
	}

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 2
	// 优先级权重压过负载权重：两个满载的高优先级账号占满 TopK，空闲的低优先级账号排在 TopK 之外。
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1.0
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 0.1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 0
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 0
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 0

	svc := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:            &schedulerTestGatewayCache{},
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				4001: {AccountID: 4001, CurrentConcurrency: 1, LoadRate: 100},
				4002: {AccountID: 4002, CurrentConcurrency: 1, LoadRate: 100},
				4003: {AccountID: 4003, LoadRate: 0},
			},
			acquireResults: map[int64]bool{4001: false, 4002: false, 4003: true},
		}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(4003), selection.Account.ID, "idle account outside TopK must be used instead of queueing on a saturated one")
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, 2, decision.TopK)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}
