package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 周期任务对 coding plan 账号的额度探测行为（runOnce 集成路径）：
//   - kimi coding 账号（含已被阈值停调的）→ 额度探测被调用；
//   - 智谱 coding 账号 → 额度探测被调用（智谱不进 kimi/deepseek 余额循环）；
//   - payg 账号不经过额度探测（走余额路径，本测试不放 payg 账号避免真实网络）；
//   - 非激活账号完全跳过。

// fakeCNQuotaProber 需要并发安全：runOnce 以 cnQuotaProbeConcurrency 并发调用 QueryUsage。
type fakeCNQuotaProber struct {
	mu     sync.Mutex
	probed []int64
}

func (f *fakeCNQuotaProber) QueryUsage(ctx context.Context, accountID int64) (*CNProviderQuotaProbeResult, error) {
	f.mu.Lock()
	f.probed = append(f.probed, accountID)
	f.mu.Unlock()
	return &CNProviderQuotaProbeResult{Success: true, Persisted: true}, nil
}

type fakeCNCheckRepo struct {
	AccountRepository
	byPlatform       map[string][]Account
	clearedAccountID int64
	extraUpdates     map[string]any
}

func (r *fakeCNCheckRepo) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return r.byPlatform[platform], nil
}

func (r *fakeCNCheckRepo) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearedAccountID = id
	return nil
}

func (r *fakeCNCheckRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.extraUpdates = updates
	return nil
}

func TestCNProviderBalanceCheckRunOnceProbesCodingPlanQuota(t *testing.T) {
	kimiActive := Account{ID: 1, Platform: PlatformKimi, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"account_mode": "coding", "api_key": "sk-kimi-active"}}
	// 已被阈值停调的 coding 账号也要刷新快照（决定是否续停）。
	kimiPaused := Account{ID: 2, Platform: PlatformKimi, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: false,
		Credentials: map[string]any{"account_mode": "coding", "api_key": "sk-kimi-paused"}}
	// 非激活账号跳过。
	kimiInactive := Account{ID: 3, Platform: PlatformKimi, Type: AccountTypeAPIKey, Status: StatusDisabled,
		Credentials: map[string]any{"account_mode": "coding", "api_key": "sk-kimi-inactive"}}
	zhipuCoding := Account{ID: 4, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"account_mode": "coding", "api_key": "sk-zhipu"}}

	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{
		PlatformKimi:  {kimiActive, kimiPaused, kimiInactive},
		PlatformZhipu: {zhipuCoding},
	}}
	prober := &fakeCNQuotaProber{}
	svc := &CNProviderBalanceCheckService{
		accountRepo:  repo,
		quotaService: prober,
		cfg:          &config.Config{},
	}

	svc.runOnce()

	require.ElementsMatch(t, []int64{1, 2, 4}, prober.probed)
}

// runOnceZhipuQuota 在 quotaService 缺失时安全跳过（Start 门控不启动的老部署路径）。
func TestCNProviderBalanceCheckRunOnceWithoutQuotaService(t *testing.T) {
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{
		PlatformZhipu: {{ID: 4, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Status: StatusActive,
			Credentials: map[string]any{"account_mode": "coding", "api_key": "sk-zhipu"}}},
	}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}
	require.NotPanics(t, func() { svc.runOnce() })
}

func TestCNProviderBalanceCheckSkipsThirdPartyDeepSeekAndClearsFalsePause(t *testing.T) {
	until := time.Now().Add(15 * time.Minute)
	account := Account{
		ID:          5,
		Platform:    PlatformDeepseek,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"account_mode": AccountModePayG,
			"api_key":      "sk-third-party",
			"base_url":     "https://relay.example/v1",
		},
		Extra: map[string]any{
			"deepseek_balance":          0.0,
			"deepseek_balance_currency": "CNY",
			"deepseek_balance_low":      true,
		},
		TempUnschedulableUntil:  &until,
		TempUnschedulableReason: "cn_balance_low: 余额 0 CNY 低于阈值 0.50",
	}
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{
		PlatformDeepseek: {account},
	}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}

	require.NotPanics(t, func() { svc.runOnce() })
	require.Equal(t, account.ID, repo.clearedAccountID)
	require.Contains(t, repo.extraUpdates, "deepseek_balance")
	require.Nil(t, repo.extraUpdates["deepseek_balance"])
	require.Equal(t, false, repo.extraUpdates["deepseek_balance_low"])
}

func TestCNProviderBalanceCheckKeepsReactiveThirdPartyBalancePause(t *testing.T) {
	until := time.Now().Add(15 * time.Minute)
	account := Account{
		ID:          6,
		Platform:    PlatformDeepseek,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"account_mode": AccountModePayG,
			"api_key":      "sk-third-party",
			"base_url":     "https://relay.example/v1",
		},
		TempUnschedulableUntil:  &until,
		TempUnschedulableReason: "cn_balance_low: insufficient balance",
	}
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{
		PlatformDeepseek: {account},
	}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}

	svc.runOnce()
	require.Zero(t, repo.clearedAccountID)
}

func TestCNProviderBalanceCheckSkipsThirdPartyKimiAndClearsFalsePause(t *testing.T) {
	until := time.Now().Add(15 * time.Minute)
	account := Account{
		ID:          7,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"account_mode": AccountModePayG,
			"api_key":      "sk-third-party",
			"base_url":     "https://relay.example/v1",
		},
		Extra: map[string]any{
			"kimi_balance":          0.0,
			"kimi_balance_currency": "CNY",
			"kimi_balance_low":      true,
		},
		TempUnschedulableUntil:  &until,
		TempUnschedulableReason: "cn_balance_low: 余额 0 CNY 低于阈值 0.50",
	}
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{PlatformKimi: {account}}}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}

	svc.runOnce()
	require.Equal(t, account.ID, repo.clearedAccountID)
	require.Contains(t, repo.extraUpdates, "kimi_balance")
	require.Nil(t, repo.extraUpdates["kimi_balance"])
	require.Equal(t, false, repo.extraUpdates["kimi_balance_low"])
}

func TestCNProviderBalanceCheckSkipsThirdPartyCodingAndClearsThresholdPause(t *testing.T) {
	until := time.Now().Add(2 * time.Hour)
	reason := BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
		Platform: PlatformKimi, Window: "5h", Scope: PlatformKimi,
		ThresholdPercent: 80, UsedPercent: 95, Until: until, Now: time.Now(),
	})
	account := Account{
		ID:          8,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"account_mode": AccountModeCoding,
			"api_key":      "sk-third-party",
			"base_url":     "https://relay.example/kimi",
		},
		Extra: map[string]any{
			"kimi_5h_used_percent":  95.0,
			"kimi_5h_reset_at":      until.Format(time.RFC3339),
			"kimi_usage_updated_at": time.Now().UTC().Format(time.RFC3339),
		},
		TempUnschedulableUntil:  &until,
		TempUnschedulableReason: reason,
	}
	repo := &fakeCNCheckRepo{byPlatform: map[string][]Account{PlatformKimi: {account}}}
	prober := &fakeCNQuotaProber{}
	svc := &CNProviderBalanceCheckService{accountRepo: repo, quotaService: prober, cfg: &config.Config{}}

	svc.runOnce()
	require.Empty(t, prober.probed)
	require.Equal(t, account.ID, repo.clearedAccountID)
	require.Contains(t, repo.extraUpdates, "kimi_5h_used_percent")
	require.Nil(t, repo.extraUpdates["kimi_5h_used_percent"])
}

// 双币种（deepseek CNY+USD）停调判定：任一币种达标即不停调，全部低于阈值才停；
// 无明细时退回主币种（兼容旧结果）。
func TestAllCNBalancesBelowThreshold(t *testing.T) {
	dualLow := &CNProviderBalanceResult{
		Balance:  1.0,
		Currency: "CNY",
		Balances: []CNProviderBalanceEntry{
			{Currency: "CNY", Balance: 1.0},
			{Currency: "USD", Balance: 0.5},
		},
	}
	require.True(t, allCNBalancesBelowThreshold(dualLow, 5.0))

	dualMixed := &CNProviderBalanceResult{
		Balance:  1.0,
		Currency: "CNY",
		Balances: []CNProviderBalanceEntry{
			{Currency: "CNY", Balance: 1.0},
			{Currency: "USD", Balance: 20.0},
		},
	}
	require.False(t, allCNBalancesBelowThreshold(dualMixed, 5.0))

	// 无明细：按主币种判定（旧行为）。
	singleLow := &CNProviderBalanceResult{Balance: 1.0, Currency: "CNY"}
	require.True(t, allCNBalancesBelowThreshold(singleLow, 5.0))
	singleOK := &CNProviderBalanceResult{Balance: 10.0, Currency: "CNY"}
	require.False(t, allCNBalancesBelowThreshold(singleOK, 5.0))
}
