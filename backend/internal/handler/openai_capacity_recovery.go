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
	// defaultOpenAICapacityRetryMaxAttempts 同账号重试次数上限。广度优先策略下
	// 第 1 轮不做同账号重试（只快速换号扫池），该上限从第 2 轮起才生效。
	defaultOpenAICapacityRetryMaxAttempts = 3
	// defaultOpenAICapacityRetryBudget 为 0：默认不设墙钟上限。降载是请求级信号，
	// 网关一直重试到客户端自己断开，尽量不把 overloaded 文案吐给用户。
	// 运维可用 openai_capacity_retry_budget_seconds 设一个可选的墙钟上限。
	defaultOpenAICapacityRetryBudget = time.Duration(0)
	// defaultOpenAICapacityRetryMaxRounds 为 0：默认不限整池轮次。
	defaultOpenAICapacityRetryMaxRounds = 0
	openAICapacityRoundBackoffInitial   = 500 * time.Millisecond
	// openAICapacityRoundBackoffMax 限制整池轮次退避的封顶值，保证卡住的请求
	// 在稳态下约每 8s 重扫一遍账号池，而不是退避到分钟级。
	openAICapacityRoundBackoffMax      = 8 * time.Second
	openAICapacityRetryHardMaxAttempts = 3
	// openAICapacityTotalAttemptHardMax 是无限预算下唯一的兜底熔断：单个请求内
	// 允许的降载上游尝试总数。客户端一直连着、上游又瞬时返回降载时，它保证请求
	// 最终仍会终止，而不是永久空转。
	openAICapacityTotalAttemptHardMax = 200
)

// 降载恢复耗尽的原因枚举，用于 openai.capacity_recovery_exhausted 日志的 reason 字段。
const (
	openAICapacityExhaustedReasonDisabled        = "recovery_disabled"
	openAICapacityExhaustedReasonTotalAttemptCap = "total_attempt_cap"
	openAICapacityExhaustedReasonBudget          = "budget"
	openAICapacityExhaustedReasonRoundCap        = "round_cap"
	openAICapacityExhaustedReasonNoMoreAccounts  = "no_more_accounts"
	openAICapacityExhaustedReasonClientOutput    = "client_output_started"
)

// OpenAICapacityRetryDecision 是一次降载失败的处置结论。
type OpenAICapacityRetryDecision int

const (
	// OpenAICapacityRetrySameAccount 退避已完成，用同一账号重入转发。
	OpenAICapacityRetrySameAccount OpenAICapacityRetryDecision = iota
	// OpenAICapacityRetryNextAccount 账号已记入本轮失败集，调用方应把它加入
	// 自己的排除集合后重新选号。
	OpenAICapacityRetryNextAccount
	// OpenAICapacityRetryExhausted 请求级重试预算耗尽，只能把错误返回给客户端。
	OpenAICapacityRetryExhausted
	// OpenAICapacityRetryCanceled 客户端已断开，直接结束。
	OpenAICapacityRetryCanceled
)

// OpenAICapacityRecoveryState tracks request-scoped OpenAI capacity recovery.
// It deliberately does not share the generic account-switch budget: a capacity
// shed is not an account health failure and may need another full scheduler
// pass after the current candidate set has been tried.
//
// 调度策略为广度优先：第 1 轮零睡眠地把整池账号快速探一遍，选号耗尽后退避
// 进入下一轮，从第 2 轮起才在单账号上做有界退避重试。
type OpenAICapacityRecoveryState struct {
	startedAt   time.Time
	deadline    time.Time
	hasDeadline bool
	disabled    bool
	maxAttempts int
	maxRounds   int
	round       int
	attempts    int
	retries     map[int64]int
	failed      map[int64]struct{}
	tried       map[int64]struct{}
}

func NewOpenAICapacityRecoveryState(cfg *config.Config) *OpenAICapacityRecoveryState {
	now := time.Now()
	budget := defaultOpenAICapacityRetryBudget
	maxAttempts := defaultOpenAICapacityRetryMaxAttempts
	maxRounds := defaultOpenAICapacityRetryMaxRounds
	disabled := false
	if cfg != nil {
		if cfg.Gateway.OpenAICapacityRetryBudgetSeconds > 0 {
			budget = time.Duration(cfg.Gateway.OpenAICapacityRetryBudgetSeconds) * time.Second
		}
		if cfg.Gateway.OpenAICapacityRetryMaxAttempts > 0 {
			maxAttempts = cfg.Gateway.OpenAICapacityRetryMaxAttempts
		}
		switch {
		case cfg.Gateway.OpenAICapacityRetryMaxRounds > 0:
			maxRounds = cfg.Gateway.OpenAICapacityRetryMaxRounds
		case cfg.Gateway.OpenAICapacityRetryMaxRounds < 0:
			// 负值是运维杀手锏：整套降载恢复关闭，退回普通 failover 行为。
			disabled = true
		}
	}
	if maxAttempts > openAICapacityRetryHardMaxAttempts {
		maxAttempts = openAICapacityRetryHardMaxAttempts
	}

	state := &OpenAICapacityRecoveryState{
		startedAt:   now,
		maxAttempts: maxAttempts,
		maxRounds:   maxRounds,
		disabled:    disabled,
		round:       1,
		retries:     make(map[int64]int),
		failed:      make(map[int64]struct{}),
		tried:       make(map[int64]struct{}),
	}
	if budget > 0 {
		state.deadline = now.Add(budget)
		state.hasDeadline = true
	}
	return state
}

func (s *OpenAICapacityRecoveryState) Enabled() bool {
	return s != nil && !s.disabled
}

func (s *OpenAICapacityRecoveryState) IsCapacityFailure(err *service.UpstreamFailoverError) bool {
	return s != nil && err != nil && err.IsOpenAICapacityShed()
}

// Remaining 返回剩余墙钟预算；未设预算时返回 -1 表示不限。
func (s *OpenAICapacityRecoveryState) Remaining() time.Duration {
	if s == nil {
		return 0
	}
	if !s.hasDeadline {
		return -1
	}
	return time.Until(s.deadline)
}

func (s *OpenAICapacityRecoveryState) Elapsed() time.Duration {
	if s == nil {
		return 0
	}
	return time.Since(s.startedAt)
}

func (s *OpenAICapacityRecoveryState) Exhausted() bool {
	if s == nil || !s.Enabled() {
		return true
	}
	if s.attempts >= openAICapacityTotalAttemptHardMax {
		return true
	}
	if s.hasDeadline && time.Now().After(s.deadline) {
		return true
	}
	return s.maxRounds > 0 && s.round > s.maxRounds
}

// ExhaustedReason 说明这次请求为什么不能再重试，用于耗尽日志。
// 所有硬上限都没触发时归因为 no_more_accounts：账号池已无可换的候选。
func (s *OpenAICapacityRecoveryState) ExhaustedReason() string {
	if s == nil || !s.Enabled() {
		return openAICapacityExhaustedReasonDisabled
	}
	if s.attempts >= openAICapacityTotalAttemptHardMax {
		return openAICapacityExhaustedReasonTotalAttemptCap
	}
	if s.hasDeadline && time.Now().After(s.deadline) {
		return openAICapacityExhaustedReasonBudget
	}
	if s.maxRounds > 0 && s.round > s.maxRounds {
		return openAICapacityExhaustedReasonRoundCap
	}
	return openAICapacityExhaustedReasonNoMoreAccounts
}

// sameAccountAttemptsAllowedThisRound 实现广度优先：第 1 轮不允许同账号重试，
// 先零睡眠地把整池账号探一遍；第 2 轮起才启用同账号退避重试。
func (s *OpenAICapacityRecoveryState) sameAccountAttemptsAllowedThisRound() int {
	if s == nil || s.round <= 1 {
		return 0
	}
	return s.maxAttempts
}

// BeginSameAccountRetry reserves one of the request-scoped retries and returns
// the bounded backoff. The caller performs the context-aware sleep before
// re-entering the forwarding loop.
func (s *OpenAICapacityRecoveryState) BeginSameAccountRetry(accountID int64, failoverErr *service.UpstreamFailoverError) (int, time.Duration, bool) {
	if s == nil || s.Exhausted() {
		return 0, 0, false
	}
	allowed := s.sameAccountAttemptsAllowedThisRound()
	count := s.retries[accountID]
	if allowed <= 0 || count >= allowed {
		return count, 0, false
	}
	count++
	delay := sameAccountRetryDelayFor(failoverErr, count)
	if s.hasDeadline {
		remaining := s.Remaining()
		if remaining <= 0 {
			return count, 0, false
		}
		if delay > remaining {
			delay = remaining
		}
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

// Attempts 返回本请求已发生的降载上游尝试总数。
func (s *OpenAICapacityRecoveryState) Attempts() int {
	if s == nil {
		return 0
	}
	return s.attempts
}

// AccountsTried 返回本请求跨轮次实际试过的账号数。
func (s *OpenAICapacityRecoveryState) AccountsTried() int {
	if s == nil {
		return 0
	}
	return len(s.tried)
}

func (s *OpenAICapacityRecoveryState) noteAttempt(accountID int64) {
	if s == nil {
		return
	}
	s.attempts++
	if s.tried == nil {
		s.tried = make(map[int64]struct{})
	}
	s.tried[accountID] = struct{}{}
}

// Decide 处置一次降载失败：按轮次决定「快速换号」还是「同账号退避重试」，
// 需要退避时在内部完成睡眠（客户端断开会立即打断）。调用方只负责把账号加入
// 自己的排除集合，并按返回值 continue / return。
func (s *OpenAICapacityRecoveryState) Decide(
	ctx context.Context,
	accountID int64,
	failoverErr *service.UpstreamFailoverError,
) OpenAICapacityRetryDecision {
	if s == nil || failoverErr == nil || !failoverErr.IsOpenAICapacityShed() {
		return OpenAICapacityRetryExhausted
	}
	s.noteAttempt(accountID)
	if retryCount, retryDelay, ok := s.BeginSameAccountRetry(accountID, failoverErr); ok {
		logger.FromContext(ctx).Warn("gateway.openai_capacity_same_account_retry",
			zap.Int64("account_id", accountID),
			zap.Int("retry_count", retryCount),
			zap.Int("retry_max", s.MaxAttempts()),
			zap.Int("round", s.Round()),
			zap.Int("round_max", s.MaxRounds()),
			zap.Duration("retry_delay", retryDelay),
			zap.Int("attempts_total", s.Attempts()),
			zap.Duration("elapsed", s.Elapsed()),
		)
		if !sleepWithContext(ctx, retryDelay) {
			return OpenAICapacityRetryCanceled
		}
		return OpenAICapacityRetrySameAccount
	}
	if s.Exhausted() {
		return OpenAICapacityRetryExhausted
	}
	s.MarkAccountFailed(accountID)
	logger.FromContext(ctx).Warn("gateway.openai_capacity_reschedule",
		zap.Int64("account_id", accountID),
		zap.Int("round", s.Round()),
		zap.Int("round_max", s.MaxRounds()),
		zap.Int("attempts_total", s.Attempts()),
		zap.Int("accounts_tried", s.AccountsTried()),
		zap.Duration("elapsed", s.Elapsed()),
	)
	return OpenAICapacityRetryNextAccount
}

// openAICapacityRoundBackoffFor 返回进入指定轮次前的退避时长：
// 第 2 轮 500ms，此后翻倍，封顶 openAICapacityRoundBackoffMax。
func openAICapacityRoundBackoffFor(round int) time.Duration {
	if round <= 1 {
		return 0
	}
	delay := openAICapacityRoundBackoffInitial
	for i := 2; i < round; i++ {
		if delay >= openAICapacityRoundBackoffMax/2 {
			return openAICapacityRoundBackoffMax
		}
		delay *= 2
	}
	if delay > openAICapacityRoundBackoffMax {
		return openAICapacityRoundBackoffMax
	}
	return delay
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
	logOpenAICapacityNextRound(ctx, recovery, removed, delay)
	return true
}

func logOpenAICapacityNextRound(
	ctx context.Context,
	recovery *OpenAICapacityRecoveryState,
	removed []int64,
	delay time.Duration,
) {
	logger.FromContext(ctx).Warn("gateway.openai_capacity_next_round",
		zap.Int("round", recovery.Round()),
		zap.Int("round_max", recovery.MaxRounds()),
		zap.Duration("round_delay", delay),
		zap.Int("released_accounts", len(removed)),
		zap.Int("attempts_total", recovery.Attempts()),
		zap.Duration("elapsed", recovery.Elapsed()),
	)
}

// logOpenAICapacityRecoveryExhausted 是降载专用的耗尽埋点：只有它出现，才说明
// 用户真的收到了 overloaded 文案。reason 为空时由状态机自行归因。
func logOpenAICapacityRecoveryExhausted(
	ctx context.Context,
	recovery *OpenAICapacityRecoveryState,
	accountID int64,
	failoverErr *service.UpstreamFailoverError,
	reason string,
) {
	if recovery == nil || failoverErr == nil || !failoverErr.IsOpenAICapacityShed() {
		return
	}
	if reason == "" {
		reason = recovery.ExhaustedReason()
	}
	logger.FromContext(ctx).Warn("openai.capacity_recovery_exhausted",
		zap.String("reason", reason),
		zap.Int64("account_id", accountID),
		zap.Int("round", recovery.Round()),
		zap.Int("round_max", recovery.MaxRounds()),
		zap.Int("attempts_total", recovery.Attempts()),
		zap.Int("accounts_tried", recovery.AccountsTried()),
		zap.Duration("elapsed", recovery.Elapsed()),
		zap.Int("upstream_status", failoverErr.StatusCode),
		zap.String("upstream_message", failoverErr.ClientMessage),
	)
}

// BeginNextRound waits briefly, advances the full-pool round, and returns the
// account IDs that can be removed from the caller's scheduler exclusion map.
//
// 只释放本请求因降载而排除的账号：限流、封禁、能力不匹配等非降载排除原样保留，
// 因此「一次降载 + 一次普通失败」的混合请求同样能进入下一轮。
func (s *OpenAICapacityRecoveryState) BeginNextRound(ctx context.Context, excluded map[int64]struct{}) ([]int64, time.Duration, bool) {
	if s == nil || s.Exhausted() || len(excluded) == 0 || len(s.failed) == 0 {
		return nil, 0, false
	}
	if s.maxRounds > 0 && s.round >= s.maxRounds {
		return nil, 0, false
	}
	released := make([]int64, 0, len(s.failed))
	for accountID := range excluded {
		if _, ok := s.failed[accountID]; ok {
			released = append(released, accountID)
		}
	}
	if len(released) == 0 {
		return nil, 0, false
	}

	nextRound := s.round + 1
	delay := openAICapacityRoundBackoffFor(nextRound)
	if s.hasDeadline {
		remaining := s.Remaining()
		if remaining <= 0 {
			return nil, 0, false
		}
		if delay > remaining {
			delay = remaining
		}
	}
	if !sleepWithContext(ctx, delay) {
		return nil, 0, false
	}

	for _, accountID := range released {
		delete(s.retries, accountID)
		delete(s.failed, accountID)
	}
	s.round = nextRound
	return released, delay, true
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
	switch recovery.Decide(ctx, accountID, failoverErr) {
	case OpenAICapacityRetrySameAccount:
		return FailoverContinue
	case OpenAICapacityRetryNextAccount:
		s.FailedAccountIDs[accountID] = struct{}{}
		s.LastFailoverErr = failoverErr
		return FailoverContinue
	case OpenAICapacityRetryCanceled:
		return FailoverCanceled
	default:
		s.LastFailoverErr = failoverErr
		logOpenAICapacityRecoveryExhausted(ctx, recovery, accountID, failoverErr, "")
		return FailoverExhausted
	}
}

func (s *FailoverState) HandleOpenAICapacitySelectionExhausted(ctx context.Context) FailoverAction {
	if s == nil || s.capacityRecovery == nil || s.LastFailoverErr == nil || !s.LastFailoverErr.IsOpenAICapacityShed() {
		return FailoverExhausted
	}
	recovery := s.capacityRecovery
	removed, delay, ok := recovery.BeginNextRound(ctx, s.FailedAccountIDs)
	if !ok {
		logOpenAICapacityRecoveryExhausted(ctx, recovery, 0, s.LastFailoverErr, "")
		return FailoverExhausted
	}
	for _, accountID := range removed {
		delete(s.FailedAccountIDs, accountID)
	}
	logOpenAICapacityNextRound(ctx, recovery, removed, delay)
	return FailoverContinue
}
