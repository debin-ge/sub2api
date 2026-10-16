package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/observability"
)

func fmtFloat(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// disableAccountOnAuthFailure：任何视频 provider 在提交阶段报 authentication（401）时把账号
// 置为 error。视频路径不经过 RateLimitService.HandleUpstreamError，没有这一刀的话失效的
// Key 会被反复选中、反复失败（方案 D13，对所有视频 provider 生效）。
func (s *VideoTaskService) disableAccountOnAuthFailure(ctx context.Context, task *VideoTask, providerErr error) {
	if s == nil || s.accounts == nil || task == nil || task.AccountID == nil || *task.AccountID <= 0 {
		return
	}
	var typed *VideoProviderError
	if !errors.As(providerErr, &typed) || typed == nil || typed.Kind != "authentication" {
		return
	}
	message := "video provider " + strings.TrimSpace(task.Provider) + " rejected credentials"
	if detail := strings.TrimSpace(typed.Message); detail != "" {
		message += ": " + detail
	}
	if err := s.accounts.SetError(context.WithoutCancel(ctx), *task.AccountID, message); err != nil {
		log.Printf("[Video] mark account %d error failed: %v", *task.AccountID, err)
	}
}

// trackFlovaObservation 统计 Flova 账号的「空完成」连击：Agent 回合结束却没有视频
// 多半是余额或档位异常，连续 N 次就临时移出调度并清零；成功一次即清零。
func (w *VideoTaskWorker) trackFlovaObservation(ctx context.Context, account *Account, task *VideoTask, observed *ProviderVideoTask) {
	if w == nil || w.accounts == nil || account == nil || task == nil || observed == nil || task.Provider != VideoProviderFlova {
		return
	}
	current := 0
	if v, ok := flovaExtraFloat(account, FlovaExtraEmptyStreak); ok {
		current = int(v)
	}
	ctx = context.WithoutCancel(ctx)
	if observed.Control != nil && observed.Control.Overspent {
		spent := 0.0
		if observed.Control.SpentCredits != nil {
			spent = *observed.Control.SpentCredits
		}
		w.tripFlovaOverspend(ctx, account, task.PublicID, spent, "in-run balance")
	}
	switch observed.Status {
	case VideoGenerationCompleted:
		if current > 0 {
			_ = w.accounts.UpdateExtra(ctx, account.ID, map[string]any{FlovaExtraEmptyStreak: 0})
		}
	case VideoGenerationFailed:
		if observed.ErrorCode != flovaErrorNoVideoArtifact {
			return
		}
		next := current + 1
		if next >= flovaEmptyStreakPauseAfter && !flovaOverspendTripped(account, w.now()) {
			until := w.now().Add(flovaEmptyStreakPauseCooldown)
			if err := w.accounts.SetTempUnschedulable(ctx, account.ID, until, flovaEmptyStreakPauseReason); err != nil {
				log.Printf("[Video] pause flova account %d after empty completions failed: %v", account.ID, err)
			} else {
				log.Printf("[Video] paused flova account %d after %d empty completions", account.ID, next)
			}
			next = 0
		}
		_ = w.accounts.UpdateExtra(ctx, account.ID, map[string]any{FlovaExtraEmptyStreak: next})
	}
}

// flovaOverspendPauseReasonPrefix 标记超支熔断。它比积分低水位与空完成暂停都长，并且只能由
// 人工解除：积分同步只清理自己的低水位原因，空完成暂停不会覆盖它。
const (
	flovaOverspendPauseReasonPrefix = "flova overspend"
	flovaOverspendPauseCooldown     = 24 * time.Hour
)

func flovaOverspendTripped(account *Account, now time.Time) bool {
	return account != nil && account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) &&
		strings.HasPrefix(account.TempUnschedulableReason, flovaOverspendPauseReasonPrefix)
}

// tripFlovaOverspend 在任务花费超出预算时把账号移出调度（D2：平台承担超支，用户账单不变）。
func (w *VideoTaskWorker) tripFlovaOverspend(ctx context.Context, account *Account, taskPublicID string, spent float64, source string) {
	if w == nil || w.accounts == nil || account == nil || flovaOverspendTripped(account, w.now()) {
		return
	}
	until := w.now().Add(flovaOverspendPauseCooldown)
	reason := fmt.Sprintf("%s: task %s spent %s credits beyond its budget (%s)", flovaOverspendPauseReasonPrefix, taskPublicID, fmtFloat(spent), source)
	if err := w.accounts.SetTempUnschedulable(context.WithoutCancel(ctx), account.ID, until, reason); err != nil {
		log.Printf("[Video] trip flova overspend breaker for account %d failed: %v", account.ID, err)
		return
	}
	account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, reason
	observability.DefaultVideoMetrics().RecordFlovaOutcome("overspend")
	log.Printf("[Video] flova account %d paused: %s", account.ID, reason)
}

func (w *VideoTaskWorker) now() time.Time {
	return time.Now().UTC()
}
