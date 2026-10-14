package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// FlovaCreditsSyncService 周期性拉取 Flova 账号积分（POST /user）写入 accounts.extra，
// 低于账号的 min_credits_reserve 时临时移出调度；余额恢复后只清除本服务写入的停调。
//
// 积分快照供账户管理与低余额调度过滤使用；新建任务仍由 ValidateSubmission 拒绝，
// 余额快照不能替代任务级硬预算。
const (
	flovaCreditsLowReasonPrefix   = "flova credits below reserve"
	flovaCreditsPauseCooldown     = 30 * time.Minute
	flovaCreditsSyncBudget        = 2 * time.Minute
	flovaEmptyStreakPauseReason   = "flova empty completions"
	flovaEmptyStreakPauseCooldown = 15 * time.Minute
	flovaEmptyStreakPauseAfter    = 3
)

type FlovaCreditsSyncService struct {
	accountRepo AccountRepository
	provider    *FlovaVideoProvider
	interval    time.Duration
	now         func() time.Time
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewFlovaCreditsSyncService(accountRepo AccountRepository, provider *FlovaVideoProvider, interval time.Duration) *FlovaCreditsSyncService {
	return &FlovaCreditsSyncService{
		accountRepo: accountRepo, provider: provider, interval: interval,
		now: time.Now, stopCh: make(chan struct{}),
	}
}

// Start 启动周期同步；interval <= 0 时不启动（配置关闭）。
func (s *FlovaCreditsSyncService) Start() {
	if s == nil || s.accountRepo == nil || s.provider == nil || s.interval <= 0 {
		return
	}
	log.Printf("[FlovaCredits] started (interval=%s)", s.interval)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *FlovaCreditsSyncService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *FlovaCreditsSyncService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), flovaCreditsSyncBudget)
	defer cancel()
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformFlova)
	if err != nil {
		log.Printf("[FlovaCredits] list accounts failed: %v", err)
		return
	}
	for i := range accounts {
		account := &accounts[i]
		if !account.IsActive() || account.Type != AccountTypeAPIKey {
			continue
		}
		if _, err := s.SyncAccount(ctx, account); err != nil {
			log.Printf("[FlovaCredits] sync account %d failed: %v", account.ID, err)
		}
	}
}

// SyncAccountByID 按账号 ID 同步一次（管理端手动刷新用）。
func (s *FlovaCreditsSyncService) SyncAccountByID(ctx context.Context, accountID int64) (*FlovaUserInfo, error) {
	if s == nil || s.accountRepo == nil {
		return nil, errors.New("flova credits sync is not available")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformFlova {
		return nil, errors.New("account is not a Flova account")
	}
	return s.SyncAccount(ctx, account)
}

// SyncAccount 拉一次 /user，写快照，并按低水位暂停 / 恢复调度。
func (s *FlovaCreditsSyncService) SyncAccount(ctx context.Context, account *Account) (*FlovaUserInfo, error) {
	if s == nil || s.provider == nil || s.accountRepo == nil || account == nil {
		return nil, errors.New("flova credits sync is not available")
	}
	user, err := s.provider.FetchUser(ctx, account)
	if err != nil {
		var typed *VideoProviderError
		if errors.As(err, &typed) && typed != nil && typed.Kind == "authentication" {
			_ = s.accountRepo.SetError(context.WithoutCancel(ctx), account.ID, "Flova rejected the API key: "+typed.Message)
		}
		return nil, err
	}
	now := s.now().UTC()
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, flovaCreditsExtraUpdates(user, now)); err != nil {
		return user, err
	}
	reserve := s.provider.minCreditsReserve(account)
	if user.CreditsBalance < reserve {
		if account.IsSchedulable() {
			reason := flovaCreditsLowReasonPrefix + ": " + formatFlovaCredits(user.CreditsBalance) + " < " + formatFlovaCredits(reserve)
			if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, now.Add(flovaCreditsPauseCooldown), reason); err != nil {
				return user, err
			}
			log.Printf("[FlovaCredits] paused account %d: credits=%s reserve=%s", account.ID, formatFlovaCredits(user.CreditsBalance), formatFlovaCredits(reserve))
		}
		return user, nil
	}
	if account.TempUnschedulableUntil != nil && strings.HasPrefix(account.TempUnschedulableReason, flovaCreditsLowReasonPrefix) {
		if err := s.accountRepo.ClearTempUnschedulable(ctx, account.ID); err != nil {
			return user, err
		}
		log.Printf("[FlovaCredits] reactivated account %d: credits=%s", account.ID, formatFlovaCredits(user.CreditsBalance))
	}
	return user, nil
}

func flovaCreditsExtraUpdates(user *FlovaUserInfo, now time.Time) map[string]any {
	updates := map[string]any{
		FlovaExtraCredits:          user.CreditsBalance,
		FlovaExtraCreditsCheckedAt: now.UTC().Format(time.RFC3339),
	}
	if len(user.SubscriptionInfo) > 0 {
		updates[FlovaExtraSubscription] = user.SubscriptionInfo
	} else {
		updates[FlovaExtraSubscription] = map[string]any{}
	}
	return updates
}

func formatFlovaCredits(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmtFloat(v), "0"), ".")
}
