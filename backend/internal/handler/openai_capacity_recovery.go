package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

const (
	defaultOpenAICapacityRetryMaxAttempts = 3
	defaultOpenAICapacityRetryBudget      = 15 * time.Second
	defaultOpenAICapacityRetryMaxRounds   = 3
	openAICapacityRoundBackoffInitial     = 500 * time.Millisecond
	openAICapacityRetryHardMaxAttempts    = 3
	openAICapacityRetryHardMaxBudget      = 15 * time.Second
	openAICapacityRetryHardMaxRounds      = 3
)

// OpenAICapacityRecoveryState tracks request-scoped OpenAI capacity recovery.
// It deliberately does not share the generic account-switch budget: a capacity
// shed is not an account health failure and may need another full scheduler
// pass after the current candidate set has been tried.
type OpenAICapacityRecoveryState struct {
	startedAt   time.Time
	deadline    time.Time
	maxAttempts int
	maxRounds   int
	round       int
	retries     map[int64]int
	failed      map[int64]struct{}
}

func NewOpenAICapacityRecoveryState(cfg *config.Config) *OpenAICapacityRecoveryState {
	now := time.Now()
	budget := defaultOpenAICapacityRetryBudget
	if cfg != nil && cfg.Gateway.OpenAICapacityRetryBudgetSeconds > 0 {
		budget = time.Duration(cfg.Gateway.OpenAICapacityRetryBudgetSeconds) * time.Second
	}
	if budget > openAICapacityRetryHardMaxBudget {
		budget = openAICapacityRetryHardMaxBudget
	}
	maxAttempts := defaultOpenAICapacityRetryMaxAttempts
	if cfg != nil && cfg.Gateway.OpenAICapacityRetryMaxAttempts > 0 {
		maxAttempts = cfg.Gateway.OpenAICapacityRetryMaxAttempts
	}
	if maxAttempts > openAICapacityRetryHardMaxAttempts {
		maxAttempts = openAICapacityRetryHardMaxAttempts
	}
	maxRounds := defaultOpenAICapacityRetryMaxRounds
	if cfg != nil && cfg.Gateway.OpenAICapacityRetryMaxRounds > 0 {
		maxRounds = cfg.Gateway.OpenAICapacityRetryMaxRounds
	}
	if maxRounds > openAICapacityRetryHardMaxRounds {
		maxRounds = openAICapacityRetryHardMaxRounds
	}

	return &OpenAICapacityRecoveryState{
		startedAt:   now,
		deadline:    now.Add(budget),
		maxAttempts: maxAttempts,
		maxRounds:   maxRounds,
		round:       1,
		retries:     make(map[int64]int),
		failed:      make(map[int64]struct{}),
	}
}

func (s *OpenAICapacityRecoveryState) Enabled() bool {
	return s != nil && s.maxAttempts > 0 && s.maxRounds > 0 && s.deadline.After(s.startedAt)
}

func (s *OpenAICapacityRecoveryState) IsCapacityFailure(err *service.UpstreamFailoverError) bool {
	return s != nil && err != nil && err.IsOpenAICapacityShed()
}

func (s *OpenAICapacityRecoveryState) Remaining() time.Duration {
	if s == nil {
		return 0
	}
	return time.Until(s.deadline)
}

func (s *OpenAICapacityRecoveryState) Exhausted() bool {
	return s == nil || !s.Enabled() || time.Now().After(s.deadline) || s.round > s.maxRounds
}

// BeginSameAccountRetry reserves one of the request-scoped retries and returns
// the bounded backoff. The caller performs the context-aware sleep before
// re-entering the forwarding loop.
func (s *OpenAICapacityRecoveryState) BeginSameAccountRetry(accountID int64, failoverErr *service.UpstreamFailoverError) (int, time.Duration, bool) {
	if s == nil || s.Exhausted() {
		return 0, 0, false
	}
	count := s.retries[accountID]
	if count >= s.maxAttempts {
		return count, 0, false
	}
	count++
	delay := sameAccountRetryDelayFor(failoverErr, count)
	if remaining := s.Remaining(); remaining <= 0 {
		return count, 0, false
	} else if delay > remaining {
		delay = remaining
	}
	s.retries[accountID] = count
	return count, delay, true
}

func (s *OpenAICapacityRecoveryState) MarkAccountFailed(accountID int64) {
	if s == nil {
		return
	}
	s.failed[accountID] = struct{}{}
}

func (s *OpenAICapacityRecoveryState) FailedAccountIDs() map[int64]struct{} {
	if s == nil {
		return nil
	}
	return s.failed
}

func (s *OpenAICapacityRecoveryState) Round() int {
	if s == nil {
		return 0
	}
	return s.round
}

func (s *OpenAICapacityRecoveryState) MaxAttempts() int {
	if s == nil {
		return 0
	}
	return s.maxAttempts
}

func (s *OpenAICapacityRecoveryState) MaxRounds() int {
	if s == nil {
		return 0
	}
	return s.maxRounds
}

func beginOpenAICapacityNextRound(ctx context.Context, recovery *OpenAICapacityRecoveryState, excluded map[int64]struct{}) bool {
	if recovery == nil {
		return false
	}
	removed, delay, ok := recovery.BeginNextRound(ctx, excluded)
	if !ok {
		return false
	}
	for _, accountID := range removed {
		delete(excluded, accountID)
	}
	logger.FromContext(ctx).Warn("gateway.openai_capacity_next_round",
		zap.Int("round", recovery.Round()),
		zap.Int("round_max", recovery.MaxRounds()),
		zap.Duration("round_delay", delay),
		zap.Duration("remaining_budget", recovery.Remaining()),
	)
	return true
}

// BeginNextRound waits briefly, advances the full-pool round, and returns the
// account IDs that can be removed from the caller's scheduler exclusion map.
// It only succeeds when every current exclusion came from capacity failures.
func (s *OpenAICapacityRecoveryState) BeginNextRound(ctx context.Context, excluded map[int64]struct{}) ([]int64, time.Duration, bool) {
	if s == nil || s.Exhausted() || s.round >= s.maxRounds || len(excluded) == 0 || len(excluded) != len(s.failed) {
		return nil, 0, false
	}
	for accountID := range excluded {
		if _, ok := s.failed[accountID]; !ok {
			return nil, 0, false
		}
	}

	nextRound := s.round + 1
	delay := openAICapacityRoundBackoffInitial
	for i := 2; i < nextRound; i++ {
		delay *= 2
	}
	if remaining := s.Remaining(); remaining <= 0 {
		return nil, 0, false
	} else if delay > remaining {
		delay = remaining
	}
	if !sleepWithContext(ctx, delay) {
		return nil, 0, false
	}

	removed := make([]int64, 0, len(s.failed))
	for accountID := range s.failed {
		removed = append(removed, accountID)
	}
	s.round = nextRound
	s.retries = make(map[int64]int)
	s.failed = make(map[int64]struct{})
	return removed, delay, true
}

func (s *FailoverState) openAICapacityRecovery(cfg *config.Config) *OpenAICapacityRecoveryState {
	if s == nil {
		return nil
	}
	if s.capacityRecovery == nil {
		s.capacityRecovery = NewOpenAICapacityRecoveryState(cfg)
	}
	return s.capacityRecovery
}

func (s *FailoverState) HandleOpenAICapacityError(ctx context.Context, cfg *config.Config, accountID int64, failoverErr *service.UpstreamFailoverError) FailoverAction {
	if s == nil || failoverErr == nil || !failoverErr.IsOpenAICapacityShed() {
		return FailoverExhausted
	}
	recovery := s.openAICapacityRecovery(cfg)
	if retryCount, retryDelay, ok := recovery.BeginSameAccountRetry(accountID, failoverErr); ok {
		logger.FromContext(ctx).Warn("gateway.openai_capacity_same_account_retry",
			zap.Int64("account_id", accountID),
			zap.Int("retry_count", retryCount),
			zap.Int("retry_max", recovery.MaxAttempts()),
			zap.Int("round", recovery.Round()),
			zap.Int("round_max", recovery.MaxRounds()),
			zap.Duration("retry_delay", retryDelay),
			zap.Duration("remaining_budget", recovery.Remaining()),
		)
		if !sleepWithContext(ctx, retryDelay) {
			return FailoverCanceled
		}
		return FailoverContinue
	}
	if recovery.Exhausted() {
		s.LastFailoverErr = failoverErr
		return FailoverExhausted
	}
	recovery.MarkAccountFailed(accountID)
	s.FailedAccountIDs[accountID] = struct{}{}
	s.LastFailoverErr = failoverErr
	logger.FromContext(ctx).Warn("gateway.openai_capacity_reschedule",
		zap.Int64("account_id", accountID),
		zap.Int("round", recovery.Round()),
		zap.Int("round_max", recovery.MaxRounds()),
		zap.Duration("remaining_budget", recovery.Remaining()),
	)
	return FailoverContinue
}

func (s *FailoverState) HandleOpenAICapacitySelectionExhausted(ctx context.Context) FailoverAction {
	if s == nil || s.capacityRecovery == nil || s.LastFailoverErr == nil || !s.LastFailoverErr.IsOpenAICapacityShed() {
		return FailoverExhausted
	}
	recovery := s.capacityRecovery
	removed, delay, ok := recovery.BeginNextRound(ctx, s.FailedAccountIDs)
	if !ok {
		return FailoverExhausted
	}
	for _, accountID := range removed {
		delete(s.FailedAccountIDs, accountID)
	}
	logger.FromContext(ctx).Warn("gateway.openai_capacity_next_round",
		zap.Int("round", recovery.Round()),
		zap.Int("round_max", recovery.MaxRounds()),
		zap.Duration("round_delay", delay),
		zap.Duration("remaining_budget", recovery.Remaining()),
	)
	return FailoverContinue
}
