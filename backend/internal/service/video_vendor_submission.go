package service

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	// videoUpstreamQuoteTimeout 限制提交前向厂商询价的耗时；询价只是核对，失败不阻塞提交。
	videoUpstreamQuoteTimeout = 5 * time.Second
	// videoUpstreamQuoteTolerance 是"厂商报价高于本地配置单价"的标记阈值（5%）。
	videoUpstreamQuoteTolerance = 0.05
	// videoInsufficientBalanceCode 是三家适配器统一使用的余额不足错误码。
	videoInsufficientBalanceCode = "insufficient_balance"
	// videoBalancePauseCooldown 是上游报余额不足后账号暂停调度的时长；充值后到期自动恢复。
	videoBalancePauseCooldown = 30 * time.Minute
)

// VideoUpstreamCostEstimator 由能在提交前向厂商询价的 provider 实现（WaveSpeed 的
// POST /model/price）。返回 ok=false 表示本次请求无法询价（例如素材尚未上传）。
type VideoUpstreamCostEstimator interface {
	EstimateUpstreamCost(ctx context.Context, account *Account, request VideoCreateRequest) (float64, bool, error)
}

// recordUpstreamQuote 把厂商报价写进 ProviderCostSnapshot，并标记它是否高于本地配置的
// 基础价（unit_price × maximum_units，未乘客户倍率）。只记录，不改变预扣与结算。
func (s *VideoTaskService) recordUpstreamQuote(ctx context.Context, resolved *resolvedVideoSubmission, snapshot map[string]any) {
	if s == nil || resolved == nil || resolved.provider == nil || snapshot == nil {
		return
	}
	estimator, ok := resolved.provider.(VideoUpstreamCostEstimator)
	if !ok {
		return
	}
	quoteCtx, cancel := context.WithTimeout(ctx, videoUpstreamQuoteTimeout)
	defer cancel()
	upstream, ok, err := estimator.EstimateUpstreamCost(quoteCtx, resolved.account, resolved.providerRequest)
	if err != nil {
		snapshot["upstream_quote_error"] = boundedProviderMessage(err.Error(), "upstream quote failed")
		return
	}
	if !ok || upstream < 0 {
		return
	}
	snapshot["upstream_quote_usd"] = upstream
	if resolved.quote == nil {
		return
	}
	base := resolved.quote.UnitPrice * resolved.quote.MaximumUnits
	if base <= 0 {
		return
	}
	snapshot["configured_base_usd"] = base
	if upstream > base*(1+videoUpstreamQuoteTolerance) {
		snapshot["upstream_quote_exceeds_price"] = true
	}
}

// pauseAccountOnInsufficientBalance 在上游明确报余额不足时把账号临时移出调度，避免
// 后续请求继续落到同一个账号上反复失败。到期自动恢复，充值后无需人工干预。
func (s *VideoTaskService) pauseAccountOnInsufficientBalance(ctx context.Context, task *VideoTask, providerErr error) {
	if s == nil || s.accounts == nil || task == nil || task.AccountID == nil || *task.AccountID <= 0 {
		return
	}
	var typed *VideoProviderError
	if !errors.As(providerErr, &typed) || typed == nil || !strings.EqualFold(strings.TrimSpace(typed.Code), videoInsufficientBalanceCode) {
		return
	}
	until := s.now().UTC().Add(videoBalancePauseCooldown)
	reason := "video provider " + strings.TrimSpace(task.Provider) + " reported insufficient balance"
	_ = s.accounts.SetTempUnschedulable(context.WithoutCancel(ctx), *task.AccountID, until, reason)
}
