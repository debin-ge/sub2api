package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// SettlementCost 是补偿结算写回用量记录的费用字段集合。
//
// 它比 CostBreakdown 窄：补偿只改价格，不改用量。tokens、模型名、时间戳都是请求当时
// 的事实，重算价格没有理由动它们。
// 字段要覆盖 total_cost 的每一个加数（见 CalculateCost 里 TotalCost 的求和式），
// 少写一个，写回后 usage_logs 上的分项之和就对不上总额，对账时会当成数据损坏。
type SettlementCost struct {
	InputCost         float64
	ImageInputCost    float64
	OutputCost        float64
	CacheCreationCost float64
	CacheReadCost     float64
	ImageOutputCost   float64
	TotalCost         float64
	AccountStatsCost  *float64
	BillingMode       string
}

func settlementCostFromBreakdown(cost *CostBreakdown) SettlementCost {
	if cost == nil {
		return SettlementCost{BillingMode: string(BillingModeToken)}
	}
	mode := strings.TrimSpace(cost.BillingMode)
	if mode == "" {
		mode = string(BillingModeToken)
	}
	return SettlementCost{
		InputCost:         cost.InputCost,
		ImageInputCost:    cost.ImageInputCost,
		OutputCost:        cost.OutputCost,
		CacheCreationCost: cost.CacheCreationCost,
		CacheReadCost:     cost.CacheReadCost,
		ImageOutputCost:   cost.ImageOutputCost,
		TotalCost:         cost.TotalCost,
		BillingMode:       mode,
	}
}

// BillingRecoveryUsageLogRepository 是补偿任务需要的最小用量仓储接口。
type BillingRecoveryUsageLogRepository interface {
	ListPendingSettlement(ctx context.Context, afterID int64, limit int) ([]UsageLog, error)
	MarkSettlementRecovered(ctx context.Context, id int64, cost SettlementCost) (bool, error)
}

// BillingRecoveryAPIKeyRepository 只用于取回记录所属 Key 的分组媒体价配置。
// 窄接口而不是直接吃 APIKeyRepository：那是个二十多个方法的接口，补偿只需要其中一个。
type BillingRecoveryAPIKeyRepository interface {
	GetByID(ctx context.Context, id int64) (*APIKey, error)
}

// BillingRecoveryAggregationRefresher 让价格回填触发刷新受影响的历史仪表盘桶。
// TriggerRecomputeRange 本身负责与正在运行的聚合作业协调和重试。
type BillingRecoveryAggregationRefresher interface {
	TriggerRecomputeRange(start, end time.Time) error
}

// BillingRecoveryService 把 billing_state=1（定价缺失待结算）的用量在价格补齐后重新
// 计算价格，并置为 billing_state=2（价格已恢复）。
//
// # 它不动用户余额，这是有意的
//
// 待结算的行意味着上游成本已经发生、但当时按 $0 落库。把价格补回记录里是纯粹的账目
// 修正：不修，usage_logs 会永久低估成本，所有基于它的统计、对账、成本看板都是错的。
// 这一步没有争议。
//
// 但"几天后从用户余额里把这笔钱扣走"是另一回事：那是面向客户的、不可撤销的资金动作。
// 用户当时拿到的是一个显示为免费的请求，余额可能早已花在别处；事后静默补扣会把余额
// 打成负数，而这笔支出在用户那边没有任何预期。这个决定属于运营，不属于一个后台扫描
// 任务的默认行为。
//
// 因此补偿任务只回填标准费用/上游成本字段，并把记录标记为 pricing recovered。
// actual_cost 仍是请求发生时真正扣掉的金额（此处保持 $0），绝不能把“后来知道应该收多少”
// 冒充成“已经从用户扣了多少”。要不要追收必须走独立、可审计的资金流程。
//
// # 灰度
//
// recovery_mode 保留历史修账所需的三档控制，默认 shadow；在线准入的
// pricing.guard_mode / pricing.strict_model_match_mode 固定为 enforce：
//   - off     不扫描。
//   - shadow  扫描并汇报"会重算多少笔、金额多大"，不写库。
//   - enforce 回填费用并置为 pricing recovered；不执行追扣。
//
// # 只认严格价
//
// 判定"现在有价了"用的是严格口径（模型自己在价目录里有条目，或分组渠道显式配了价），
// 而不是结算侧那条会跨模型推断的宽松链——见 pricing_strict_match.go。用宽松口径会把
// 一个仍然没配价的模型按别的模型的价格“恢复”掉，那不是修复，是把同一个 bug 换个
// 地方重犯一遍，而且这次还错误盖上了“价格已恢复”的章。查不到严格价的行原样留在 billing_state=1，
// 下一轮继续被扫到、继续出现在欠账看板上。
type BillingRecoveryService struct {
	cfg            *config.Config
	usageLogRepo   BillingRecoveryUsageLogRepository
	apiKeyRepo     BillingRecoveryAPIKeyRepository
	billingService *BillingService
	channelService *ChannelService
	resolver       *ModelPricingResolver
	aggregation    BillingRecoveryAggregationRefresher

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// cursor 跨轮次前进，见 advanceCursor 的注释。
	cursorMu sync.Mutex
	cursor   int64

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

// SetAggregationRefresher 注入历史聚合刷新器。单测或未启用预聚合的部署可留空。
func (s *BillingRecoveryService) SetAggregationRefresher(refresher BillingRecoveryAggregationRefresher) {
	if s == nil {
		return
	}
	s.aggregation = refresher
}

func NewBillingRecoveryService(
	cfg *config.Config,
	usageLogRepo BillingRecoveryUsageLogRepository,
	apiKeyRepo BillingRecoveryAPIKeyRepository,
	billingService *BillingService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
) *BillingRecoveryService {
	return &BillingRecoveryService{
		cfg:            cfg,
		usageLogRepo:   usageLogRepo,
		apiKeyRepo:     apiKeyRepo,
		billingService: billingService,
		channelService: channelService,
		resolver:       resolver,
		interval:       billingRecoveryInterval(cfg),
		stopCh:         make(chan struct{}),
		instanceID:     uuid.NewString(),
	}
}

// SetLeaderLock 注入选主锁。多实例下补偿只该由一个实例跑：写回本身是幂等的
// （MarkSettlementRecovered 的 WHERE 带状态判断），但 N 个实例同时全量扫待结算行
// 是纯粹的浪费，shadow 档下还会把同一批欠账在日志里报 N 遍。两者都为 nil 时不选主
// （单实例 / 测试行为）。
func (s *BillingRecoveryService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

const (
	defaultBillingRecoveryInterval  = time.Hour
	defaultBillingRecoveryBatchSize = 500

	billingRecoveryLeaderLockKey = "billing:settlement:recovery:leader"
	// 锁的 TTL 要盖住一整轮扫描（runOnceLogged 给的 5 分钟超时），否则锁会在跑到
	// 一半时过期，让第二个实例插进来重复扫。
	billingRecoveryLeaderLockTTL = 10 * time.Minute
)

func billingRecoveryInterval(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Pricing.RecoveryIntervalMinutes <= 0 {
		return defaultBillingRecoveryInterval
	}
	return time.Duration(cfg.Pricing.RecoveryIntervalMinutes) * time.Minute
}

func billingRecoveryBatchSize(cfg *config.Config) int {
	if cfg == nil || cfg.Pricing.RecoveryBatchSize <= 0 {
		return defaultBillingRecoveryBatchSize
	}
	return cfg.Pricing.RecoveryBatchSize
}

func (s *BillingRecoveryService) mode() string {
	if s == nil || s.cfg == nil {
		return config.PricingGuardModeShadow
	}
	return config.NormalizePricingRecoveryMode(s.cfg.Pricing.RecoveryMode)
}

// Start 起一个后台扫描循环。off 档下直接不起协程。
func (s *BillingRecoveryService) Start() {
	if s == nil || s.usageLogRepo == nil || s.billingService == nil {
		return
	}
	if s.mode() == config.PricingGuardModeOff || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		// 先跑一轮再进循环：默认间隔是一小时，等一小时才知道 shadow 档下有没有欠账
		// 太迟了——开关刚打开时正是最想看结果的时候。
		s.runOnceLogged()
		for {
			select {
			case <-ticker.C:
				s.runOnceLogged()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *BillingRecoveryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

// BillingRecoveryReport 汇总一轮补偿的结果。
type BillingRecoveryReport struct {
	Mode string
	// Scanned 本轮读到的待结算行数。
	Scanned int
	// Recovered 已恢复价格（enforce）或本会恢复价格（shadow）的行数。
	Recovered int
	// RecoveredCost 是这些行按请求时倍率重算出的理论应收合计，仅供估算潜在损失；
	// 它不代表实际追扣，持久化的 actual_cost 仍保持真实扣除额 $0。
	RecoveredCost float64
	// StillUnpriced 至今仍查不到严格价的行数——真正需要管理员去配价的量。
	StillUnpriced int
	// Failed 重算或写回出错的行数。
	Failed int
	// LastID 本轮处理到的最大 id，供下一轮做游标。
	LastID int64
}

func (s *BillingRecoveryService) runOnceLogged() {
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, billingRecoveryLeaderLockKey, s.instanceID, billingRecoveryLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	report, err := s.RunOnce(ctx)
	if err != nil {
		logger.L().Error("billing.recovery_failed",
			zap.String("component", "service.billing_recovery"),
			zap.String("mode", report.Mode),
			zap.Int("scanned", report.Scanned),
			zap.Error(err),
		)
		return
	}
	if report.Scanned == 0 {
		return
	}
	logger.L().Warn("billing.recovery_completed",
		zap.String("component", "service.billing_recovery"),
		zap.String("mode", report.Mode),
		zap.Int("scanned", report.Scanned),
		zap.Int("recovered", report.Recovered),
		zap.Float64("repriced_estimated_charge", report.RecoveredCost),
		zap.Int("still_unpriced", report.StillUnpriced),
		zap.Int("failed", report.Failed),
	)
}

// RunOnce 扫描一批待结算记录并（按档位）重算/写回。
//
// 一轮只处理 recovery_batch_size 行：补偿是低优先级的账目修正，没必要为了"一次补完"
// 去跟在线流量抢数据库。剩下的行留在库里，下一轮接着补——它们已经是被记录下来的欠账，
// 晚补不会变得更糟。
func (s *BillingRecoveryService) RunOnce(ctx context.Context) (BillingRecoveryReport, error) {
	report := BillingRecoveryReport{Mode: s.mode()}
	if s == nil || s.usageLogRepo == nil || s.billingService == nil {
		return report, nil
	}
	if report.Mode == config.PricingGuardModeOff {
		return report, nil
	}

	batchSize := billingRecoveryBatchSize(s.cfg)
	cursor := s.loadCursor()
	logs, err := s.usageLogRepo.ListPendingSettlement(ctx, cursor, batchSize)
	if err != nil {
		return report, err
	}
	report.Scanned = len(logs)
	if len(logs) == 0 {
		// 游标已经走到集合末尾，下一轮从头再来。
		s.storeCursor(0)
		return report, nil
	}
	defer func() { s.advanceCursor(report.LastID, len(logs) < batchSize) }()

	// 同一批里同一个 API Key 往往重复出现（一个模型配错价会连累它的全部流量），
	// 分组媒体价配置按 Key 缓存一次即可。
	keyCache := make(map[int64]*APIKey)
	var recomputeStart, recomputeEnd time.Time

	for i := range logs {
		log := &logs[i]
		report.LastID = log.ID

		apiKey := s.apiKeyForLog(ctx, keyCache, log)
		model, ok := s.recoverableBillingModel(ctx, log, apiKey)
		if !ok {
			report.StillUnpriced++
			continue
		}
		cost, err := s.recomputeCost(ctx, log, model, apiKey)
		if err != nil {
			report.Failed++
			logger.L().Warn("billing.recovery_recompute_failed",
				zap.String("component", "service.billing_recovery"),
				zap.Int64("usage_log_id", log.ID),
				zap.String("billing_model", model),
				zap.Error(err),
			)
			continue
		}

		settlement := settlementCostFromBreakdown(cost)
		settlement.AccountStatsCost = s.recomputeAccountStatsCost(ctx, log, model, cost.TotalCost)
		if report.Mode == config.PricingGuardModeEnforce {
			updated, err := s.usageLogRepo.MarkSettlementRecovered(ctx, log.ID, settlement)
			if err != nil {
				report.Failed++
				logger.L().Warn("billing.recovery_write_failed",
					zap.String("component", "service.billing_recovery"),
					zap.Int64("usage_log_id", log.ID),
					zap.Error(err),
				)
				continue
			}
			if !updated {
				// 另一轮/另一个实例先恢复了价格。不是错误，也不该重复计入本轮金额。
				continue
			}
			recomputeStart, recomputeEnd = extendRecoveryRecomputeRange(recomputeStart, recomputeEnd, log.CreatedAt)
		}
		report.Recovered++
		report.RecoveredCost += cost.ActualCost
	}
	if report.Mode == config.PricingGuardModeEnforce && s.aggregation != nil && recomputeEnd.After(recomputeStart) {
		if err := s.aggregation.TriggerRecomputeRange(recomputeStart, recomputeEnd); err != nil {
			logger.L().Warn("billing.recovery_aggregation_refresh_failed",
				zap.String("component", "service.billing_recovery"),
				zap.Time("start", recomputeStart),
				zap.Time("end", recomputeEnd),
				zap.Error(err),
			)
		}
	}
	return report, nil
}

func (s *BillingRecoveryService) recomputeAccountStatsCost(
	ctx context.Context,
	log *UsageLog,
	billingModel string,
	totalCost float64,
) *float64 {
	if log == nil || log.GroupID == nil {
		return nil
	}
	requestCount := 1
	if log.ImageCount > 0 {
		requestCount = log.ImageCount
	} else if log.VideoCount > 0 {
		requestCount = log.VideoCount
	}
	return resolveAccountStatsCost(
		ctx,
		s.channelService,
		s.billingService,
		log.AccountID,
		*log.GroupID,
		billingModel,
		recoveryUsageTokens(log),
		requestCount,
		totalCost,
		stringOrEmpty(log.ServiceTier),
	)
}

func extendRecoveryRecomputeRange(start, end, createdAt time.Time) (time.Time, time.Time) {
	if createdAt.IsZero() {
		return start, end
	}
	createdAt = createdAt.UTC()
	if start.IsZero() || createdAt.Before(start) {
		start = createdAt
	}
	// 聚合范围是 [start, end)，所以至少向后扩一纳秒以包含 createdAt 本身。
	candidateEnd := createdAt.Add(time.Nanosecond)
	if end.IsZero() || candidateEnd.After(end) {
		end = candidateEnd
	}
	return start, end
}

func (s *BillingRecoveryService) loadCursor() int64 {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.cursor
}

func (s *BillingRecoveryService) storeCursor(id int64) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.cursor = id
}

// advanceCursor 让扫描跨轮次向前走，走到头再回绕。
//
// 每轮都从 id=0 重扫会被"永远补不上的行"卡死：一个模型如果管理员始终没配价，它那批
// 记录会一直留在 billing_state=1，占满每一轮的 batch，后面已经能补的行永远轮不到。
// 记住游标就绕开了这一点——扫过的行这轮不再看，下一轮从它之后继续。
//
// 回绕的条件是"这一页没取满"，也就是当前游标之后已经没有更多待结算行了。此时必须归零，
// 否则游标会永久停在表尾，之后新产生的（id 更大的）待结算行确实还能扫到，但当初被跳过
// 的那些旧行再也不会被复查——而它们正是最可能已经补上价的。
func (s *BillingRecoveryService) advanceCursor(lastID int64, reachedEnd bool) {
	if reachedEnd || lastID <= 0 {
		s.storeCursor(0)
		return
	}
	s.storeCursor(lastID)
}

// recoverableBillingModel 找出这条记录现在能按哪个模型名结算。
//
// token 与媒体必须分开处理：
//   - token 行的非空 UpstreamModel 是已经发给供应商的权威 SKU。它仍然未知时，不能仅仅
//     因为 Model/RequestedModel 在全局目录里有价，就拿别名价格给实际上游成本盖上
//     "已恢复"的章。只有分组对别名配置了完整、显式的渠道价格，才有足够证据回落。
//   - 媒体行的 UpstreamModel 可能是 Responses 顶层文本模型，而实际计费 SKU 是图片工具
//     模型。新写入的待恢复媒体行会把精确结算身份持久化在 Model，因此媒体必须先查
//     Model，再把 UpstreamModel/RequestedModel 作为历史记录的兼容回退。否则只要顶层
//     文本模型碰巧也有图片目录价，就会按错误 SKU “恢复”成功。
//
// UpstreamModel 为空的历史 token 行没有更权威的事实来源，继续按 Model →
// RequestedModel 查严格渠道/全局价。
//
// 返回 false 表示没有足够证据安全恢复——这行继续留在欠账里，等待管理员补价或修复
// 模型身份元数据。
func (s *BillingRecoveryService) recoverableBillingModel(ctx context.Context, log *UsageLog, apiKey *APIKey) (string, bool) {
	if log == nil {
		return "", false
	}

	kind, _, _ := recoveryBillingKindAndTier(log)
	upstreamModel := stringOrEmpty(log.UpstreamModel)
	if kind == BillingKindToken && upstreamModel != "" {
		if s.hasStrictPricingForUsage(ctx, upstreamModel, log, apiKey) {
			return upstreamModel, true
		}

		// A requested/display alias is not evidence of what the provider charged.
		// A complete channel price is an explicit billing policy and is the sole
		// safe exception when the actual upstream SKU remains unknown.
		for _, candidate := range []string{log.Model, log.RequestedModel} {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || strings.EqualFold(candidate, upstreamModel) {
				continue
			}
			if s.hasExplicitChannelTokenPricing(ctx, candidate, log.GroupID) {
				return candidate, true
			}
		}
		return "", false
	}

	candidates := make([]string, 0, 3)
	if kind == BillingKindImage || kind == BillingKindVideo {
		candidates = append(candidates, log.Model)
		if upstreamModel != "" {
			candidates = append(candidates, upstreamModel)
		}
		candidates = append(candidates, log.RequestedModel)
	} else {
		if upstreamModel != "" {
			candidates = append(candidates, upstreamModel)
		}
		candidates = append(candidates, log.Model, log.RequestedModel)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if s.hasStrictPricingForUsage(ctx, candidate, log, apiKey) {
			return candidate, true
		}
	}
	return "", false
}

func (s *BillingRecoveryService) hasStrictPricingForUsage(
	ctx context.Context,
	model string,
	log *UsageLog,
	apiKey *APIKey,
) bool {
	kind, tier, tierValid := recoveryBillingKindAndTier(log)
	var groupID *int64
	if log != nil {
		groupID = log.GroupID
	}
	if log != nil && log.ImageCount > 0 && billingModeIsToken(log.BillingMode) {
		// A delivered token-billed image without image-output usage cannot be
		// reconstructed from pricing alone. Keep it pending instead of
		// "recovering" it as a settled zero-cost row.
		if log.ImageOutputTokens <= 0 || log.OutputTokens < log.ImageOutputTokens {
			return false
		}
		requireImageInput := strings.Contains(
			strings.ToLower(stringOrEmpty(log.InboundEndpoint)),
			"/images/edits",
		)
		if requireImageInput && log.ImageInputTokens <= 0 {
			return false
		}
		return s.hasStrictImageTokenPricing(ctx, model, groupID, requireImageInput)
	}
	switch kind {
	case BillingKindImage, BillingKindVideo:
		if !tierValid {
			return false
		}
		return recoveryGroupMediaTierConfigured(apiKey, kind, tier) ||
			s.recoveryChannelMediaTierConfigured(ctx, model, groupID, tier) ||
			s.recoveryCatalogMediaTierConfigured(model, kind, tier)
	default:
		return s.hasStrictTokenPricing(ctx, model, groupID)
	}
}

func (s *BillingRecoveryService) hasStrictImageTokenPricing(
	ctx context.Context,
	model string,
	groupID *int64,
	requireImageInput bool,
) bool {
	if s == nil || s.billingService == nil {
		return false
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	_, err := resolver.ResolveStrictImageToken(ctx, PricingInput{
		Model:   model,
		GroupID: groupID,
	}, requireImageInput)
	return err == nil
}

func recoveryBillingKindAndTier(log *UsageLog) (BillingKind, string, bool) {
	if log == nil {
		return BillingKindToken, "", true
	}
	if log.VideoCount > 0 {
		tier, err := NormalizeVideoBillingResolutionStrictOrDefault(stringOrEmpty(log.VideoResolution))
		return BillingKindVideo, tier, err == nil
	}
	if log.ImageCount > 0 && !billingModeIsToken(log.BillingMode) {
		tier, err := NormalizeImageBillingTierStrictOrDefault(stringOrEmpty(log.ImageSize))
		return BillingKindImage, tier, err == nil
	}
	return BillingKindToken, "", true
}

func recoveryGroupMediaTierConfigured(apiKey *APIKey, kind BillingKind, tier string) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	var price *float64
	if kind == BillingKindVideo {
		price = apiKey.Group.GetVideoPrice(tier)
	} else {
		price = apiKey.Group.GetImagePrice(tier)
	}
	return validConfiguredPrice(price)
}

func (s *BillingRecoveryService) recoveryChannelMediaTierConfigured(
	ctx context.Context,
	model string,
	groupID *int64,
	tier string,
) bool {
	resolved := s.resolveRecoveryChannelMediaPricing(ctx, model, groupID)
	if resolved == nil {
		return false
	}
	price, found := s.resolver.LookupRequestTierPrice(resolved, tier)
	if !found && resolved.DefaultPerRequestPriceSet {
		price = resolved.DefaultPerRequestPrice
		found = true
	}
	return found && isFiniteNonNegativePrice(price)
}

func (s *BillingRecoveryService) resolveRecoveryChannelMediaPricing(
	ctx context.Context,
	model string,
	groupID *int64,
) *ResolvedPricing {
	if s == nil || s.resolver == nil || groupID == nil {
		return nil
	}
	resolved := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	if resolved == nil || resolved.Source != PricingSourceChannel {
		return nil
	}
	if resolved.Mode != BillingModePerRequest && resolved.Mode != BillingModeImage {
		return nil
	}
	return resolved
}

func (s *BillingRecoveryService) recoveryCatalogMediaTierConfigured(model string, kind BillingKind, tier string) bool {
	if s == nil || s.billingService == nil {
		return false
	}
	switch kind {
	case BillingKindImage:
		_, ok := s.billingService.strictImageUnitPrice(model, tier, nil)
		return ok
	case BillingKindVideo:
		_, ok := s.billingService.strictVideoUnitPrice(model, tier, nil)
		return ok
	default:
		return false
	}
}

// hasStrictTokenPricing 与 GatewayService.hasResolvableTokenPricing 使用同一严格口径：
// 补偿和实时后扣都不能拿跨模型推断出来的价格去给一行盖“已结算”的章。
func (s *BillingRecoveryService) hasStrictTokenPricing(ctx context.Context, model string, groupID *int64) bool {
	if s.channelService != nil && groupID != nil {
		if pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model); pricing != nil {
			if channelTokenPricingHasInvalidPrice(pricing) {
				return false
			}
			if explicitChannelTokenPricingConfigured(pricing) {
				return true
			}
		}
	}
	_, err := s.billingService.GetModelPricingStrict(model)
	return err == nil
}

// hasExplicitChannelTokenPricing is deliberately narrower than
// hasStrictTokenPricing: it never falls through to the global catalog. It is
// used only when a persisted upstream token SKU is present but still unknown;
// in that case a priced display/request alias is safe only when the group has
// explicitly and completely defined how that alias is billed.
func (s *BillingRecoveryService) hasExplicitChannelTokenPricing(
	ctx context.Context,
	model string,
	groupID *int64,
) bool {
	if s == nil || s.channelService == nil || groupID == nil {
		return false
	}
	pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model)
	if pricing == nil || channelTokenPricingHasInvalidPrice(pricing) {
		return false
	}
	return explicitChannelTokenPricingConfigured(pricing)
}

func explicitChannelTokenPricingConfigured(pricing *ChannelModelPricing) bool {
	if pricing == nil {
		return false
	}
	switch pricing.BillingMode {
	case BillingModePerRequest, BillingModeImage:
		return validConfiguredPrice(pricing.PerRequestPrice)
	default:
		return channelTokenPricingConfigured(pricing)
	}
}

func (s *BillingRecoveryService) apiKeyForLog(ctx context.Context, cache map[int64]*APIKey, log *UsageLog) *APIKey {
	if s.apiKeyRepo == nil || log.APIKeyID == 0 {
		return nil
	}
	if cached, ok := cache[log.APIKeyID]; ok {
		return cached
	}
	// 取不到就按 nil 走：分组媒体价缺失会让媒体行退回模型/系统默认价，
	// 而不是让整行补偿失败。缓存 nil 以免同一个已删除的 Key 每行都查一次库。
	apiKey, err := s.apiKeyRepo.GetByID(ctx, log.APIKeyID)
	if err != nil {
		apiKey = nil
	}
	cache[log.APIKeyID] = apiKey
	return apiKey
}

// recomputeCost 按记录里持久化的用量重算费用。
//
// 用的是 usage_logs 自己存下来的 tokens / 倍率 / 图片尺寸 / 视频时长，不是重新去问上游：
// 那些数字是请求当时的事实，补偿唯一要换的是价格。
// recoveryPricingGroup 返回补偿结算用的分组。定价解析器要靠 Group 拿到平台，
// 才能命中平台级手动覆盖价；只传 GroupID 会让补偿路径和网关路径算出不同的价。
func recoveryPricingGroup(log *UsageLog, apiKey *APIKey) *Group {
	if log != nil && log.Group != nil {
		return log.Group
	}
	if apiKey != nil {
		return apiKey.Group
	}
	return nil
}

func (s *BillingRecoveryService) recomputeCost(
	ctx context.Context,
	log *UsageLog,
	billingModel string,
	apiKey *APIKey,
) (*CostBreakdown, error) {
	multiplier := log.RateMultiplier
	if multiplier < 0 {
		multiplier = 0
	}

	switch {
	case log.VideoCount > 0:
		resolution := stringOrEmpty(log.VideoResolution)
		duration := 0
		if log.VideoDurationSeconds != nil {
			duration = *log.VideoDurationSeconds
		}
		normalizedResolution, err := NormalizeVideoBillingResolutionStrictOrDefault(resolution)
		if err != nil {
			return nil, err
		}
		resolution = normalizedResolution
		if recoveryGroupMediaTierConfigured(apiKey, BillingKindVideo, resolution) {
			return s.billingService.CalculateVideoCostStrict(
				billingModel, resolution, log.VideoCount, duration, videoPriceConfigFromAPIKey(apiKey), multiplier,
			)
		}
		if resolved := s.resolveRecoveryChannelMediaPricing(ctx, billingModel, log.GroupID); resolved != nil {
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx:            ctx,
				Model:          billingModel,
				GroupID:        log.GroupID,
				Group:          recoveryPricingGroup(log, apiKey),
				RequestCount:   log.VideoCount,
				SizeTier:       resolution,
				RateMultiplier: multiplier,
				PricingAt:      log.CreatedAt,
				Resolver:       s.resolver,
				Resolved:       resolved,
			})
			if err == nil {
				cost.BillingMode = string(BillingModeVideo)
				return cost, nil
			}
		}
		return s.billingService.CalculateVideoCostStrict(
			billingModel, resolution, log.VideoCount, duration, nil, multiplier,
		)
	case log.ImageCount > 0 && !billingModeIsToken(log.BillingMode):
		sizeTier, err := NormalizeImageBillingTierStrictOrDefault(stringOrEmpty(log.ImageSize))
		if err != nil {
			return nil, err
		}
		if recoveryGroupMediaTierConfigured(apiKey, BillingKindImage, sizeTier) {
			return s.billingService.CalculateImageCostStrict(
				billingModel, sizeTier, log.ImageCount, imagePriceConfigFromAPIKey(apiKey), multiplier,
			)
		}
		if resolved := s.resolveRecoveryChannelMediaPricing(ctx, billingModel, log.GroupID); resolved != nil {
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx:            ctx,
				Model:          billingModel,
				GroupID:        log.GroupID,
				Group:          recoveryPricingGroup(log, apiKey),
				RequestCount:   log.ImageCount,
				SizeTier:       sizeTier,
				RateMultiplier: multiplier,
				PricingAt:      log.CreatedAt,
				Resolver:       s.resolver,
				Resolved:       resolved,
			})
			if err == nil {
				return cost, nil
			}
		}
		return s.billingService.CalculateImageCostStrict(
			billingModel, sizeTier, log.ImageCount, nil, multiplier,
		)
	}

	tokens := recoveryUsageTokens(log)
	if log.ImageCount > 0 && billingModeIsToken(log.BillingMode) {
		requireImageInput := strings.Contains(
			strings.ToLower(stringOrEmpty(log.InboundEndpoint)),
			"/images/edits",
		)
		if log.ImageOutputTokens <= 0 || log.OutputTokens < log.ImageOutputTokens ||
			(requireImageInput && log.ImageInputTokens <= 0) {
			return nil, fmt.Errorf(
				"%w: persisted image token usage is incomplete",
				ErrModelPricingUnavailable,
			)
		}
		resolver := s.resolver
		if resolver == nil {
			resolver = NewModelPricingResolver(s.channelService, s.billingService)
		}
		resolved, err := resolver.ResolveStrictImageToken(ctx, PricingInput{
			Model:   billingModel,
			GroupID: log.GroupID,
			Group:   recoveryPricingGroup(log, apiKey),
		}, requireImageInput)
		if err != nil {
			return nil, err
		}
		longContextBillingEnabled := true
		return s.billingService.CalculateCostUnified(CostInput{
			Ctx:                       ctx,
			Model:                     billingModel,
			GroupID:                   log.GroupID,
			Group:                     recoveryPricingGroup(log, apiKey),
			Tokens:                    tokens,
			RequestCount:              1,
			RateMultiplier:            multiplier,
			PricingAt:                 log.CreatedAt,
			ServiceTier:               stringOrEmpty(log.ServiceTier),
			Resolver:                  resolver,
			Resolved:                  resolved,
			LongContextBillingEnabled: &longContextBillingEnabled,
		})
	}
	if s.resolver != nil && log.GroupID != nil {
		resolved, err := s.resolver.ResolveStrictToken(ctx, PricingInput{
			Model:   billingModel,
			GroupID: log.GroupID,
			Group:   recoveryPricingGroup(log, apiKey),
		})
		if err != nil {
			return nil, err
		}
		longContextBillingEnabled := true
		return s.billingService.CalculateCostUnified(CostInput{
			Ctx:                       ctx,
			Model:                     billingModel,
			GroupID:                   log.GroupID,
			Group:                     recoveryPricingGroup(log, apiKey),
			Tokens:                    tokens,
			RequestCount:              1,
			SizeTier:                  stringOrEmpty(log.BillingTier),
			RateMultiplier:            multiplier,
			PricingAt:                 log.CreatedAt,
			ServiceTier:               stringOrEmpty(log.ServiceTier),
			Resolver:                  s.resolver,
			Resolved:                  resolved,
			LongContextBillingEnabled: &longContextBillingEnabled,
		})
	}
	pricing, err := s.billingService.GetModelPricingStrict(billingModel)
	if err != nil {
		return nil, err
	}
	cost, err := s.billingService.computeTokenBreakdownValidated(
		billingModel,
		pricing,
		tokens,
		multiplier,
		stringOrEmpty(log.ServiceTier),
		true,
	)
	if err != nil {
		return nil, err
	}
	cost.BillingMode = string(BillingModeToken)
	return cost, nil
}

func recoveryUsageTokens(log *UsageLog) UsageTokens {
	if log == nil {
		return UsageTokens{}
	}
	return UsageTokens{
		InputTokens:           log.InputTokens,
		ImageInputTokens:      log.ImageInputTokens,
		OutputTokens:          log.OutputTokens,
		CacheCreationTokens:   log.CacheCreationTokens,
		CacheReadTokens:       log.CacheReadTokens,
		CacheCreation5mTokens: log.CacheCreation5mTokens,
		CacheCreation1hTokens: log.CacheCreation1hTokens,
		ImageOutputTokens:     log.ImageOutputTokens,
	}
}

// billingModeIsToken 判断记录当时是按 token 记的。
//
// 未定价的行是 unpricedCostBreakdown 落的：它按入口声明的 BillingKind 标注 billing_mode，
// 所以这个字段在待结算行上是可信的。空值按 token 处理，与 resolveBillingMode 的默认一致。
func billingModeIsToken(mode *string) bool {
	if mode == nil {
		return true
	}
	trimmed := strings.TrimSpace(*mode)
	return trimmed == "" || trimmed == string(BillingModeToken)
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
