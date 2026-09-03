package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// APIKeyRateLimitCacheData holds rate limit usage data cached in Redis.
type APIKeyRateLimitCacheData struct {
	Usage5h  float64 `json:"usage_5h"`
	Usage1d  float64 `json:"usage_1d"`
	Usage7d  float64 `json:"usage_7d"`
	Window5h int64   `json:"window_5h"` // unix timestamp, 0 = not started
	Window1d int64   `json:"window_1d"`
	Window7d int64   `json:"window_7d"`
}

// UserPlatformQuotaKey 标识一个 user×platform，用于脏集出入与批量读。
type UserPlatformQuotaKey struct {
	UserID   int64
	Platform string
}

// UserPlatformQuotaCacheEntry Redis hash 反序列化结果。
//
// SchemaVersion 用于向后兼容：
//   - 0（旧 entry，无 SchemaVersion 字段）→ 视为 cache MISS，强制 refresh
//   - 1（当前版本）→ 包含 limits 和 window_start，可免 DB 查询
//
// limit 字段为 nil 表示"无限额"（DB 中对应列为 NULL）。
const UserPlatformQuotaCacheSchemaV1 = int64(1)

type UserPlatformQuotaCacheEntry struct {
	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64
	Version         int64
	SchemaVersion   int64

	// 以下字段仅在 SchemaVersion >= 1 时有效
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64

	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
}

// BillingCache defines cache operations for billing service
type BillingCache interface {
	// Balance operations
	GetUserBalance(ctx context.Context, userID int64) (float64, error)
	SetUserBalance(ctx context.Context, userID int64, balance float64) error
	DeductUserBalance(ctx context.Context, userID int64, amount float64) error
	InvalidateUserBalance(ctx context.Context, userID int64) error

	// Subscription operations
	GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error)
	SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error
	UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error
	InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error

	// API Key rate limit operations
	GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error)
	SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error
	UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error
	InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error

	// user × platform quota 缓存
	GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaCacheEntry, bool, error)
	SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, entry *UserPlatformQuotaCacheEntry, ttl time.Duration) error
	DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error
	// IncrUserPlatformQuotaUsageCache 在缓存命中时累加用量；缓存未命中（key 不存在）静默返回 nil。
	// markDirty=true 时将该 key 的 member 写入 Redis 脏集，供 flusher 批量回写 DB。
	IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error

	// 脏集读写，供 flusher 使用。
	PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error)
	ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error
	BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error)
}

// ModelPricing 模型价格配置（per-token价格，与配置化模型价格目录格式一致）
type ModelPricing struct {
	InputPricePerToken                 float64  // 每token输入价格 (USD)
	InputPriceExplicit                 bool     // 是否由目录或渠道显式配置；用于区分缺失与显式 0
	InputPricePerTokenPriority         float64  // priority service tier 下每token输入价格 (USD)
	ImageInputPricePerToken            float64  // 图片输入 token 价格 (USD)，用于多模态 embedding 等图文不同价场景
	ImageInputPriceExplicit            bool     // 是否显式配置；false 且价格为 0 时回退 InputPricePerToken
	OutputPricePerToken                float64  // 每token输出价格 (USD)
	OutputPriceExplicit                bool     // 是否由目录或渠道显式配置；用于区分缺失与显式 0
	OutputPricePerTokenPriority        float64  // priority service tier 下每token输出价格 (USD)
	CacheCreationPricePerToken         float64  // 缓存创建每token价格 (USD)
	CacheCreationPricePerTokenPriority float64  // priority service tier 下缓存创建每token价格 (USD)
	CacheCreationPriceExplicit         bool     // 是否由渠道/区间定价显式设定（为 true 时即使 == 0 也不回退）
	CacheReadPricePerToken             float64  // 缓存读取每token价格 (USD)
	CacheReadPricePerTokenPriority     float64  // priority service tier 下缓存读取每token价格 (USD)
	FastMultiplier                     *float64 // 渠道显式 Fast/priority 倍率；nil 时沿用模型目录行为
	FlexMultiplier                     *float64 // 渠道显式 Flex 倍率；nil 时沿用默认行为
	CacheCreation5mPrice               float64  // 5分钟缓存创建每token价格 (USD)
	CacheCreation1hPrice               float64  // 1小时缓存创建每token价格 (USD)
	SupportsCacheBreakdown             bool     // 是否支持详细的缓存分类
	LongContextInputThreshold          int      // 超过阈值后按整次会话提升输入价格
	LongContextThresholdInclusive      bool     // 达到阈值即应用（xAI）；默认保持严格大于以兼容既有模型
	LongContextInputMultiplier         float64  // 长上下文整次会话输入倍率
	LongContextOutputMultiplier        float64  // 长上下文整次会话输出倍率
	ImageOutputPricePerToken           float64  // 图片输出 token 价格 (USD)
	ImageOutputPriceExplicit           bool     // 是否由目录或渠道定价显式设定（为 true 时即使 == 0 也不回退）
	// PricePresenceKnown is true only for JSON catalog entries. It lets the
	// billing core distinguish an omitted dimension from an explicit zero
	// without changing legacy in-memory fallback entries.
	PricePresenceKnown                 bool
	CacheReadPriceExplicit             bool
	CacheCreation1hPriceExplicit       bool
	InputPriorityPriceExplicit         bool
	OutputPriorityPriceExplicit        bool
	CacheCreationPriorityPriceExplicit bool
	CacheReadPriorityPriceExplicit     bool
	LongContextPricingExplicit         bool
	OperatorOverride                   bool
	// OfficialTimePricing 标记这份价格是官方报价（价格目录 / 管理端生效价 /
	// 代码内官方兜底表），可以叠加官方峰谷倍率。渠道价、分组价是渠道侧显式定价
	// （渠道另有独立的 TimePricing 配置），必须为 false。
	OfficialTimePricing bool
	// OfficialTimeBaseIsOffPeak 标记上面这份基准价存的是官方空闲价：价格目录对
	// DeepSeek 官方分时 SKU 存的是空闲价（高峰 = 基准 ×2）；false 表示存的是高峰价
	// （代码内官方兜底表，空闲 = 基准 ×0.5）。
	OfficialTimeBaseIsOffPeak bool
}

func normalizeBillingServiceTier(serviceTier string) string {
	return strings.ToLower(strings.TrimSpace(serviceTier))
}

func inputPriorityPriceConfigured(pricing *ModelPricing) bool {
	return pricing != nil &&
		(pricing.InputPriorityPriceExplicit || pricing.InputPricePerTokenPriority > 0)
}

func outputPriorityPriceConfigured(pricing *ModelPricing) bool {
	return pricing != nil &&
		(pricing.OutputPriorityPriceExplicit || pricing.OutputPricePerTokenPriority > 0)
}

func cacheCreationPriorityPriceConfigured(pricing *ModelPricing) bool {
	return pricing != nil &&
		(pricing.CacheCreationPriorityPriceExplicit || pricing.CacheCreationPricePerTokenPriority > 0)
}

func cacheReadPriorityPriceConfigured(pricing *ModelPricing) bool {
	return pricing != nil &&
		(pricing.CacheReadPriorityPriceExplicit || pricing.CacheReadPricePerTokenPriority > 0)
}

func serviceTierCostMultiplier(serviceTier string) float64 {
	switch normalizeBillingServiceTier(serviceTier) {
	case "priority", "fast":
		return 2.0
	case "flex":
		return 0.5
	default:
		return 1.0
	}
}

func configuredServiceTierMultiplier(serviceTier string, pricing *ModelPricing) float64 {
	if pricing != nil {
		switch normalizeBillingServiceTier(serviceTier) {
		case "priority", "fast":
			if pricing.FastMultiplier != nil {
				return *pricing.FastMultiplier
			}
		case "flex":
			if pricing.FlexMultiplier != nil {
				return *pricing.FlexMultiplier
			}
		}
	}
	return serviceTierCostMultiplier(serviceTier)
}

func pricingWithPriorityMultiplier(base *ModelPricing, multiplier float64) *ModelPricing {
	if base == nil {
		return nil
	}
	cloned := *base
	cloned.InputPricePerTokenPriority = cloned.InputPricePerToken * multiplier
	cloned.OutputPricePerTokenPriority = cloned.OutputPricePerToken * multiplier
	cloned.CacheCreationPricePerTokenPriority = cloned.CacheCreationPricePerToken * multiplier
	cloned.CacheReadPricePerTokenPriority = cloned.CacheReadPricePerToken * multiplier
	return &cloned
}

// UsageTokens 使用的token数量
type UsageTokens struct {
	InputTokens           int
	ImageInputTokens      int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ImageOutputTokens     int
}

// CostBreakdown 费用明细
type CostBreakdown struct {
	InputCost                 float64 // 文本输入费用（不含图片输入，图片输入单独记入 ImageInputCost）
	ImageInputCost            float64 // 图片输入 token 费用（如 gpt-image-2 图片编辑）
	OutputCost                float64
	ImageOutputCost           float64
	CacheCreationCost         float64
	CacheReadCost             float64
	TotalCost                 float64
	ActualCost                float64 // 应用倍率后的实际费用
	BillingMode               string  // 计费模式（"token"/"per_request"/"image"），由 CalculateCostUnified 填充
	LongContextBillingApplied bool
}

func applyCostBreakdownMultiplier(cost *CostBreakdown, multiplier float64) {
	if cost == nil || multiplier == 1 {
		return
	}
	cost.InputCost *= multiplier
	cost.ImageInputCost *= multiplier
	cost.OutputCost *= multiplier
	cost.ImageOutputCost *= multiplier
	cost.CacheCreationCost *= multiplier
	cost.CacheReadCost *= multiplier
	cost.TotalCost *= multiplier
	cost.ActualCost *= multiplier
}

// resolvedTokenTimeMultiplier 返回 token 计费的分时倍率。
// 渠道显式分时配置优先；否则仅当生效价确实是官方报价时，才按北京时间选档
// DeepSeek 官方峰谷价。effective 是本次实际用于计算的价格，它自己带着基准档位
// （OfficialTimeBaseIsOffPeak）。
func resolvedTokenTimeMultiplier(
	resolved *ResolvedPricing,
	effective *ModelPricing,
	platform, model string,
	at time.Time,
) float64 {
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		resolved.channelPricing != nil &&
		resolved.channelPricing.TimePricing != nil &&
		len(resolved.channelPricing.TimePricing.Periods) > 0 {
		return resolved.channelPricing.TimePricing.MultiplierAt(at)
	}
	// 渠道价/分组价即使没配 TimePricing 也不叠加官方峰谷：它们是显式定价。
	if resolved != nil && (resolved.Source == PricingSourceChannel || resolved.Source == PricingSourceGroup) {
		return 1
	}
	if !officialTimePricingApplies(effective) {
		return 1
	}
	return deepSeekOfficialTimeMultiplier(platform, model, at, effective.OfficialTimeBaseIsOffPeak)
}

// officialTimeMultiplierForPlatform 供无 ResolvedPricing 的旧路径使用：先确认该
// platform/model 的生效基准价确实来自官方兜底表，再返回峰谷倍率。
func (s *BillingService) officialTimeMultiplierForPlatform(platform, model string, at time.Time) float64 {
	if !usesDeepSeekOfficialTimePricing(platform, model) {
		return 1
	}
	pricing, err := s.GetModelPricingForPlatform(platform, model)
	if err != nil || !officialTimePricingApplies(pricing) {
		return 1
	}
	return deepSeekOfficialTimeMultiplier(platform, model, at, pricing.OfficialTimeBaseIsOffPeak)
}

// ErrModelPricingUnavailable indicates that none of the configured pricing
// sources can price the requested model.
var ErrModelPricingUnavailable = errors.New("pricing not found")

func validateFiniteModelPricing(model string, pricing *ModelPricing) error {
	if pricing == nil {
		return fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
	}
	prices := []struct {
		name  string
		value float64
	}{
		{"input", pricing.InputPricePerToken},
		{"input_priority", pricing.InputPricePerTokenPriority},
		{"image_input", pricing.ImageInputPricePerToken},
		{"output", pricing.OutputPricePerToken},
		{"output_priority", pricing.OutputPricePerTokenPriority},
		{"cache_write", pricing.CacheCreationPricePerToken},
		{"cache_write_priority", pricing.CacheCreationPricePerTokenPriority},
		{"cache_read", pricing.CacheReadPricePerToken},
		{"cache_read_priority", pricing.CacheReadPricePerTokenPriority},
		{"cache_write_5m", pricing.CacheCreation5mPrice},
		{"cache_write_1h", pricing.CacheCreation1hPrice},
		{"image_output", pricing.ImageOutputPricePerToken},
		{"long_context_input_multiplier", pricing.LongContextInputMultiplier},
		{"long_context_output_multiplier", pricing.LongContextOutputMultiplier},
	}
	for _, price := range prices {
		if !isFiniteNonNegativePrice(price.value) {
			return fmt.Errorf(
				"%w: invalid %s price for model %s",
				ErrModelPricingUnavailable,
				price.name,
				model,
			)
		}
	}
	return nil
}

// validateUsedModelPricingDimensions applies presence-aware fail-closed
// validation to dimensions that are optional in the catalog but appeared in
// actual upstream usage. A float64 zero is ambiguous for legacy in-memory
// entries, so this stricter check is limited to parsed catalog entries where
// PricePresenceKnown can distinguish omission from an explicit free price.
func validateUsedModelPricingDimensions(model string, pricing *ModelPricing, tokens UsageTokens) error {
	if pricing == nil || !pricing.PricePresenceKnown {
		return nil
	}

	cacheCreationUsed := tokens.CacheCreationTokens > 0 ||
		tokens.CacheCreation5mTokens > 0 ||
		tokens.CacheCreation1hTokens > 0
	cacheCreationConfigured := pricing.CacheCreationPriceExplicit ||
		pricing.CacheCreationPricePerToken > 0
	if cacheCreationUsed && !cacheCreationConfigured {
		return fmt.Errorf(
			"%w: cache_write usage has no configured price for model %s",
			ErrModelPricingUnavailable,
			model,
		)
	}

	cacheReadConfigured := pricing.CacheReadPriceExplicit ||
		pricing.CacheReadPricePerToken > 0
	if tokens.CacheReadTokens > 0 && !cacheReadConfigured {
		return fmt.Errorf(
			"%w: cache_read usage has no configured price for model %s",
			ErrModelPricingUnavailable,
			model,
		)
	}
	return nil
}

// ---- DeepSeek 官方低谷价（$/token，2026-08-23 起生效）----
// Source: https://api-docs.deepseek.com/quick_start/pricing
// 高峰价 = 2× 低谷价；高峰时段 01:00–04:00 与 06:00–10:00 UTC（仅工作日），
// 北京时间周六/周日全天低谷。时段判定见 deepseekPeakMultiplierAt。
const (
	deepseekFlashOffPeakInputPrice  = 2.2e-7  // $0.22 per MTok (cache miss)
	deepseekFlashOffPeakOutputPrice = 6.6e-7  // $0.66 per MTok
	deepseekFlashOffPeakCacheRead   = 7e-9    // $0.007 per MTok (cache hit)
	deepseekProOffPeakInputPrice    = 6.6e-7  // $0.66 per MTok (cache miss)
	deepseekProOffPeakOutputPrice   = 1.98e-6 // $1.98 per MTok
	deepseekProOffPeakCacheRead     = 2.2e-8  // $0.022 per MTok (cache hit)
)

// isDeepSeekModel 判断模型名是否为 DeepSeek 模型（大小写不敏感）。
// 任意 deepseek- 前缀均视为 DeepSeek 模型；只有已知官方 V4 SKU 会获得
// 代码内兜底价格，未知型号仍按严格缺价策略处理。
func isDeepSeekModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}

func miniMaxUSDPerMillionTokens(usd float64) float64 {
	return usd / 1_000_000
}

// deepseekPeakMultiplierAt 返回指定时刻的 DeepSeek 官方峰谷定价因子。
// 官方口径（2026-08-23 起生效）：高峰价 = 2× 低谷价；高峰时段为
// 01:00–04:00 与 06:00–10:00 UTC（半开区间），仅工作日；
// 周末（北京时间周六/周日）全天低谷。北京时间用固定 +8 偏移（无夏令时）。
func deepseekPeakMultiplierAt(now time.Time) float64 {
	beijing := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	switch beijing.Weekday() {
	case time.Saturday, time.Sunday:
		return 1.0
	}
	switch h := now.UTC().Hour(); {
	case h >= 1 && h < 4, h >= 6 && h < 10:
		return 2.0
	}
	return 1.0
}

// BillingService 计费服务
type BillingService struct {
	cfg            *config.Config
	pricingService *PricingService
	fallbackPrices map[string]*ModelPricing // 硬编码回退价格

	// fallbackWarnSeen 记录已打过 fallback 警告日志的(已小写化)模型名,
	// 让 "[Billing] Using fallback pricing" 每个模型每进程最多打一条,
	// 避免热路径上每请求刷屏(issue #3394)。零值即可用,无需在构造函数初始化。
	fallbackWarnSeen sync.Map
}

// NewBillingService 创建计费服务实例
func NewBillingService(cfg *config.Config, pricingService *PricingService) *BillingService {
	s := &BillingService{
		cfg:            cfg,
		pricingService: pricingService,
		fallbackPrices: make(map[string]*ModelPricing),
	}

	// 初始化硬编码回退价格（当动态价格不可用时使用）
	s.initFallbackPricing()

	return s
}

// initFallbackPricing 初始化硬编码回退价格（当动态价格不可用时使用）
// 价格单位：USD per token（与配置化模型价格目录格式一致）
func (s *BillingService) initFallbackPricing() {
	// Claude 4.5 Opus
	s.fallbackPrices["claude-opus-4.5"] = &ModelPricing{
		InputPricePerToken:         5e-6,    // $5 per MTok
		OutputPricePerToken:        25e-6,   // $25 per MTok
		CacheCreationPricePerToken: 6.25e-6, // $6.25 per MTok
		CacheReadPricePerToken:     0.5e-6,  // $0.50 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 4 Sonnet
	s.fallbackPrices["claude-sonnet-4"] = &ModelPricing{
		InputPricePerToken:         3e-6,    // $3 per MTok
		OutputPricePerToken:        15e-6,   // $15 per MTok
		CacheCreationPricePerToken: 3.75e-6, // $3.75 per MTok
		CacheReadPricePerToken:     0.3e-6,  // $0.30 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3.5 Sonnet
	s.fallbackPrices["claude-3-5-sonnet"] = &ModelPricing{
		InputPricePerToken:         3e-6,    // $3 per MTok
		OutputPricePerToken:        15e-6,   // $15 per MTok
		CacheCreationPricePerToken: 3.75e-6, // $3.75 per MTok
		CacheReadPricePerToken:     0.3e-6,  // $0.30 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3.5 Haiku
	s.fallbackPrices["claude-3-5-haiku"] = &ModelPricing{
		InputPricePerToken:         1e-6,    // $1 per MTok
		OutputPricePerToken:        5e-6,    // $5 per MTok
		CacheCreationPricePerToken: 1.25e-6, // $1.25 per MTok
		CacheReadPricePerToken:     0.1e-6,  // $0.10 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3 Opus
	s.fallbackPrices["claude-3-opus"] = &ModelPricing{
		InputPricePerToken:         15e-6,    // $15 per MTok
		OutputPricePerToken:        75e-6,    // $75 per MTok
		CacheCreationPricePerToken: 18.75e-6, // $18.75 per MTok
		CacheReadPricePerToken:     1.5e-6,   // $1.50 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3 Haiku
	s.fallbackPrices["claude-3-haiku"] = &ModelPricing{
		InputPricePerToken:         0.25e-6, // $0.25 per MTok
		OutputPricePerToken:        1.25e-6, // $1.25 per MTok
		CacheCreationPricePerToken: 0.3e-6,  // $0.30 per MTok
		CacheReadPricePerToken:     0.03e-6, // $0.03 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 4.6 Opus (与4.5同价)
	s.fallbackPrices["claude-opus-4.6"] = s.fallbackPrices["claude-opus-4.5"]

	// Claude 4.7 Opus (暂与4.6同价，待官方定价更新)
	s.fallbackPrices["claude-opus-4.7"] = s.fallbackPrices["claude-opus-4.6"]

	// Claude 4.8 Opus / Claude Opus 5（标准 $5/$25，Fast $10/$50 per MTok）。
	// 缺少这两条时 getFallbackPricing 会掉到 claude-3-opus（$15/$75），造成 3 倍超收。
	s.fallbackPrices["claude-opus-4.8"] = pricingWithPriorityMultiplier(s.fallbackPrices["claude-opus-4.7"], 2)
	s.fallbackPrices["claude-opus-5"] = pricingWithPriorityMultiplier(s.fallbackPrices["claude-opus-4.8"], 2)

	// Claude Fable 5.x uses the same input/output and cache-write prices, while
	// Fable 5.1 reduces cache reads from $1 to $0.25 per MTok.
	s.fallbackPrices["claude-fable-5"] = &ModelPricing{
		InputPricePerToken:         10e-6,
		OutputPricePerToken:        50e-6,
		CacheCreationPricePerToken: 12.5e-6,
		CacheCreation5mPrice:       12.5e-6,
		CacheCreation1hPrice:       20e-6,
		CacheReadPricePerToken:     1e-6,
		SupportsCacheBreakdown:     true,
	}
	s.fallbackPrices["claude-fable-5-1"] = &ModelPricing{
		InputPricePerToken:         10e-6,
		OutputPricePerToken:        50e-6,
		CacheCreationPricePerToken: 12.5e-6,
		CacheCreation5mPrice:       12.5e-6,
		CacheCreation1hPrice:       20e-6,
		CacheReadPricePerToken:     0.25e-6,
		SupportsCacheBreakdown:     true,
	}

	// Gemini 3.1 Pro
	s.fallbackPrices["gemini-3.1-pro"] = &ModelPricing{
		InputPricePerToken:         2e-6,   // $2 per MTok
		OutputPricePerToken:        12e-6,  // $12 per MTok
		CacheCreationPricePerToken: 2e-6,   // $2 per MTok
		CacheReadPricePerToken:     0.2e-6, // $0.20 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Gemini 3.6 Flash (Google AI pricing: $1.50 input / $7.50 output /
	// $0.15 cached input per MTok). Antigravity's -high/-low/-medium/-tiered
	// aliases are matched below so unavailable remote pricing never records
	// token-bearing requests at $0.
	s.fallbackPrices["gemini-3.6-flash"] = &ModelPricing{
		InputPricePerToken:     1.5e-6,
		OutputPricePerToken:    7.5e-6,
		CacheReadPricePerToken: 0.15e-6,
		SupportsCacheBreakdown: false,
	}

	// OpenAI GPT-5.4（业务指定价格）
	s.fallbackPrices["gpt-5.4"] = &ModelPricing{
		InputPricePerToken:             2.5e-6,  // $2.5 per MTok
		InputPricePerTokenPriority:     5e-6,    // $5 per MTok
		OutputPricePerToken:            15e-6,   // $15 per MTok
		OutputPricePerTokenPriority:    30e-6,   // $30 per MTok
		CacheCreationPricePerToken:     2.5e-6,  // $2.5 per MTok
		CacheReadPricePerToken:         0.25e-6, // $0.25 per MTok
		CacheReadPricePerTokenPriority: 0.5e-6,  // $0.5 per MTok
		SupportsCacheBreakdown:         false,
	}
	// OpenAI GPT-5.5 官方价格；Fast 为标准价 2.5 倍。
	// Source: https://platform.openai.com/docs/pricing
	s.fallbackPrices["gpt-5.5"] = pricingWithPriorityMultiplier(&ModelPricing{
		InputPricePerToken:  5e-6,
		OutputPricePerToken: 30e-6,
		// 官方未列独立 cache-write 价；内部出现 cache creation token 时按输入价兜底。
		CacheCreationPricePerToken: 5e-6,
		CacheReadPricePerToken:     0.5e-6,
		SupportsCacheBreakdown:     false,
	}, 2.5)
	// GPT-5.5 Pro 当前不提供 Fast；保留标准、Flex 和长上下文 fallback 价格。
	s.fallbackPrices["gpt-5.5-pro"] = &ModelPricing{
		InputPricePerToken:  30e-6,
		OutputPricePerToken: 180e-6,
		// 官方未列独立 cached-input/cache-write 价；内部出现对应 token 时按输入价兜底。
		CacheCreationPricePerToken: 30e-6,
		CacheReadPricePerToken:     30e-6,
		SupportsCacheBreakdown:     false,
	}

	// OpenAI GPT-5.6 官方价格（USD/token）。缓存写入为输入价的 1.25 倍。
	s.fallbackPrices["gpt-5.6-sol"] = &ModelPricing{
		InputPricePerToken:                 5e-6,
		InputPricePerTokenPriority:         10e-6,
		OutputPricePerToken:                30e-6,
		OutputPricePerTokenPriority:        60e-6,
		CacheCreationPricePerToken:         6.25e-6,
		CacheCreationPricePerTokenPriority: 12.5e-6,
		CacheReadPricePerToken:             0.5e-6,
		CacheReadPricePerTokenPriority:     1e-6,
	}
	s.fallbackPrices["gpt-5.6-terra"] = &ModelPricing{
		InputPricePerToken:                 2e-6,
		InputPricePerTokenPriority:         4e-6,
		OutputPricePerToken:                12e-6,
		OutputPricePerTokenPriority:        24e-6,
		CacheCreationPricePerToken:         2.5e-6,
		CacheCreationPricePerTokenPriority: 5e-6,
		CacheReadPricePerToken:             0.2e-6,
		CacheReadPricePerTokenPriority:     0.4e-6,
	}
	s.fallbackPrices["gpt-5.6-luna"] = &ModelPricing{
		InputPricePerToken:                 0.2e-6,
		InputPricePerTokenPriority:         0.4e-6,
		OutputPricePerToken:                1.2e-6,
		OutputPricePerTokenPriority:        2.4e-6,
		CacheCreationPricePerToken:         0.25e-6,
		CacheCreationPricePerTokenPriority: 0.5e-6,
		CacheReadPricePerToken:             0.02e-6,
		CacheReadPricePerTokenPriority:     0.04e-6,
	}

	s.fallbackPrices["gpt-5.4-mini"] = &ModelPricing{
		InputPricePerToken:     7.5e-7,
		OutputPricePerToken:    4.5e-6,
		CacheReadPricePerToken: 7.5e-8,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["gpt-5.4-nano"] = &ModelPricing{
		InputPricePerToken:     2e-7,
		OutputPricePerToken:    1.25e-6,
		CacheReadPricePerToken: 2e-8,
		SupportsCacheBreakdown: false,
	}
	// OpenAI GPT-5.2（本地兜底）
	s.fallbackPrices["gpt-5.2"] = &ModelPricing{
		InputPricePerToken:             1.75e-6,
		InputPricePerTokenPriority:     3.5e-6,
		OutputPricePerToken:            14e-6,
		OutputPricePerTokenPriority:    28e-6,
		CacheCreationPricePerToken:     1.75e-6,
		CacheReadPricePerToken:         0.175e-6,
		CacheReadPricePerTokenPriority: 0.35e-6,
		SupportsCacheBreakdown:         false,
	}
	// Codex 族兜底统一按 GPT-5.3 Codex 价格计费
	s.fallbackPrices["gpt-5.3-codex"] = &ModelPricing{
		InputPricePerToken:             1.5e-6, // $1.5 per MTok
		InputPricePerTokenPriority:     3e-6,   // $3 per MTok
		OutputPricePerToken:            12e-6,  // $12 per MTok
		OutputPricePerTokenPriority:    24e-6,  // $24 per MTok
		CacheCreationPricePerToken:     1.5e-6, // $1.5 per MTok
		CacheReadPricePerToken:         0.15e-6,
		CacheReadPricePerTokenPriority: 0.3e-6,
		SupportsCacheBreakdown:         false,
	}

	// ============================================================
	// 国产 LLM 兜底定价（数据源：各家官方定价页/USD 口径）
	// 顺序：DeepSeek → 智谱 GLM → 月之暗面 Kimi → MiniMax
	// 覆盖逻辑见同文件 getFallbackPricing()
	// ============================================================

	// ---- DeepSeek 系列 ----
	// Source: https://api-docs.deepseek.com/quick_start/pricing
	// 官方口径（2026-08-23 起生效）：现行模型为 deepseek-v4-flash /
	// deepseek-v4-pro / deepseek-v4-flash-vision-exp；deepseek-chat /
	// deepseek-reasoner 已停止服务；未知 deepseek-* 不做通配兜底。
	// 以下均为官方低谷价；高峰价 = 2× 低谷价（高峰时段 01:00–04:00
	// 与 06:00–10:00 UTC，仅工作日；北京时间周六/周日全天低谷），见 deepseekPeakMultiplierAt。
	s.fallbackPrices["deepseek-v4-pro"] = &ModelPricing{
		InputPricePerToken:        deepseekProOffPeakInputPrice,
		OutputPricePerToken:       deepseekProOffPeakOutputPrice,
		CacheReadPricePerToken:    deepseekProOffPeakCacheRead,
		SupportsCacheBreakdown:    false,
		OfficialTimePricing:       true,
		OfficialTimeBaseIsOffPeak: true,
	}
	s.fallbackPrices["deepseek-v4-flash"] = &ModelPricing{
		InputPricePerToken:        deepseekFlashOffPeakInputPrice,
		OutputPricePerToken:       deepseekFlashOffPeakOutputPrice,
		CacheReadPricePerToken:    deepseekFlashOffPeakCacheRead,
		SupportsCacheBreakdown:    false,
		OfficialTimePricing:       true,
		OfficialTimeBaseIsOffPeak: true,
	}
	s.fallbackPrices["deepseek-v4-flash-vision-exp"] = &ModelPricing{
		InputPricePerToken:        deepseekFlashOffPeakInputPrice,
		OutputPricePerToken:       deepseekFlashOffPeakOutputPrice,
		CacheReadPricePerToken:    deepseekFlashOffPeakCacheRead,
		SupportsCacheBreakdown:    false,
		OfficialTimePricing:       true,
		OfficialTimeBaseIsOffPeak: true,
	}

	// ---- 智谱 GLM（Z.AI）----
	// Source: https://docs.z.ai/guides/overview/pricing (USD per 1M tokens)
	// CacheReadPricePerToken 即「缓存命中」价；官方未公开缓存写入价，按 0 处理。
	//
	// 【合并守则】本分支统一采用 z.ai 国际版 USD 口径，与既有 Claude/GPT 对齐；
	// 不要改回「中国区 CNY ÷ 核算汇率」口径。GLM-4.5 国内价 ¥0.8/¥2 换算后约
	// $0.11/$0.28，与国际版 $0.6/$2.2 相差数倍，混用会让同一张价表自相矛盾
	// （v0.1.166 合并曾因重复键误回退到 CNY 口径，GLM 计费被下调 25~75%）。
	// GLM-5.2 与 GLM-5.1 在 z.ai 上同价。
	s.fallbackPrices["glm-5.2"] = &ModelPricing{
		InputPricePerToken:     1.4e-6, // $1.40 per MTok
		OutputPricePerToken:    4.4e-6, // $4.40 per MTok
		CacheReadPricePerToken: 0.26e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5.1"] = &ModelPricing{
		InputPricePerToken:     1.4e-6, // $1.40 per MTok
		OutputPricePerToken:    4.4e-6, // $4.40 per MTok
		CacheReadPricePerToken: 0.26e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5"] = &ModelPricing{
		InputPricePerToken:     1e-6, // $1.00 per MTok
		OutputPricePerToken:    3.2e-6,
		CacheReadPricePerToken: 0.2e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5-turbo"] = &ModelPricing{
		InputPricePerToken:     1.2e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.24e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7-flashx"] = &ModelPricing{
		InputPricePerToken:     0.07e-6, // $0.07 per MTok
		OutputPricePerToken:    0.4e-6,
		CacheReadPricePerToken: 0.01e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.6"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-x"] = &ModelPricing{
		InputPricePerToken:     2.2e-6, // $2.20 per MTok
		OutputPricePerToken:    8.9e-6,
		CacheReadPricePerToken: 0.45e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-air"] = &ModelPricing{
		InputPricePerToken:     0.2e-6, // $0.20 per MTok
		OutputPricePerToken:    1.1e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-airx"] = &ModelPricing{
		InputPricePerToken:     1.1e-6,
		OutputPricePerToken:    4.5e-6,
		CacheReadPricePerToken: 0.22e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4-32b-0414-128k"] = &ModelPricing{
		InputPricePerToken:     0.1e-6, // $0.10 per MTok
		OutputPricePerToken:    0.1e-6,
		SupportsCacheBreakdown: false,
	}
	// GLM-4.5-Flash / GLM-4.7-Flash 在 z.ai 上为 Free，保留 zero-cost entry 防止未知 alias 误计费。
	s.fallbackPrices["glm-4.5-flash"] = &ModelPricing{
		InputPricePerToken:     0,
		OutputPricePerToken:    0,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7-flash"] = &ModelPricing{
		InputPricePerToken:     0,
		OutputPricePerToken:    0,
		SupportsCacheBreakdown: false,
	}

	// ---- 月之暗面 Kimi（K 系列）----
	// Source: https://platform.moonshot.cn/docs/pricing/overview (元/百万 tokens 口径)
	//       交叉验证：https://www.tmtpost.com/7961404.html (USD 口径)
	// Moonshot V1 (¥2/¥5/¥10 多 tier) 公开页未直接标注 USD 价，本分支不覆盖，避免误计价。
	// K2-0905 / K2-0711 官方页面未保留定价，不覆盖。
	// Kimi K3 国际站 USD 价目：https://platform.kimi.ai/docs/pricing/chat-k3.md
	// Kimi Code bare aliases（k3 / k3-256k）官方无按 token 价目；复用 API Platform
	// kimi-k3 档位作代理计费 fallback（同 kimi-for-coding 对 K2.6 的处理口径）。
	s.fallbackPrices["kimi-k3"] = &ModelPricing{
		InputPricePerToken:     3e-6,    // $3.00 per MTok (cache miss)
		OutputPricePerToken:    15e-6,   // $15.00 per MTok
		CacheReadPricePerToken: 0.30e-6, // $0.30 per MTok (cache hit)
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2.6"] = &ModelPricing{
		InputPricePerToken:     0.95e-6, // $0.95 per MTok (cache miss)
		OutputPricePerToken:    4e-6,    // $4.00 per MTok
		CacheReadPricePerToken: 0.15e-6, // $0.15 per MTok (cache hit, ¥1.10)
		SupportsCacheBreakdown: false,
	}
	// kimi-for-coding 走 Kimi Coding endpoint，按当前 K2.6 coding 档位兜底计费。
	s.fallbackPrices["kimi-for-coding"] = &ModelPricing{
		InputPricePerToken:         0.95e-6,
		OutputPricePerToken:        4e-6,
		CacheCreationPricePerToken: 0.95e-6,
		CacheReadPricePerToken:     0.16e-6,
		SupportsCacheBreakdown:     false,
	}
	s.fallbackPrices["kimi-k2.5"] = &ModelPricing{
		InputPricePerToken:     0.60e-6, // $0.60 per MTok
		OutputPricePerToken:    3e-6,    // $3.00 per MTok
		CacheReadPricePerToken: 0.098e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2-thinking"] = &ModelPricing{
		InputPricePerToken:     0.56e-6, // ¥4/百万 ≈ $0.56
		OutputPricePerToken:    2.24e-6, // ¥16/百万
		CacheReadPricePerToken: 0.14e-6, // ¥1/百万
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2"] = &ModelPricing{
		InputPricePerToken:     0.56e-6, // ¥4/百万
		OutputPricePerToken:    2.24e-6, // ¥16/百万
		CacheReadPricePerToken: 0.14e-6, // ¥1/百万
		SupportsCacheBreakdown: false,
	}

	// ---- MiniMax M 系列 ----
	// Source: https://platform.minimax.io/docs/guides/pricing-paygo
	// 注意：MiniMax M3 在 >512K context 时价格翻倍，本兜底采用 ≤512K 标准 tier（保守口径，对用户有利）。
	// 如需支持长上下文 multiplier，可后续参考 GPT-5.4 模式扩展 LongContextXxx 字段。
	s.fallbackPrices["minimax-m3"] = &ModelPricing{
		InputPricePerToken:     0.60e-6, // $0.60 per MTok (≤512K standard tier, 含 50% 永久折扣前原价 $1.20)
		OutputPricePerToken:    2.40e-6,
		CacheReadPricePerToken: 0.12e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.7"] = &ModelPricing{
		InputPricePerToken:         miniMaxUSDPerMillionTokens(0.30),
		OutputPricePerToken:        miniMaxUSDPerMillionTokens(1.20),
		CacheCreationPricePerToken: miniMaxUSDPerMillionTokens(0.375),
		CacheReadPricePerToken:     miniMaxUSDPerMillionTokens(0.06),
		SupportsCacheBreakdown:     false,
	}
	s.fallbackPrices["minimax-m2.7-highspeed"] = &ModelPricing{
		InputPricePerToken:         miniMaxUSDPerMillionTokens(0.30),
		OutputPricePerToken:        miniMaxUSDPerMillionTokens(2.40),
		CacheCreationPricePerToken: miniMaxUSDPerMillionTokens(0.375),
		CacheReadPricePerToken:     miniMaxUSDPerMillionTokens(0.06),
		SupportsCacheBreakdown:     false,
	}
	s.fallbackPrices["minimax-m2.5"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.1"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}

	// ---- 火山方舟 豆包 Embedding（多模态向量化）----
	// doubao-embedding-vision 图文向量化：上游 usage 回传 prompt_tokens_details.{text_tokens,image_tokens}，
	// 按量付费官方价 文本 ¥0.7/MTok、图片 ¥1.8/MTok；汇率口径 ÷7.14（与本表其他国产模型一致，¥1≈$0.14）。
	// embedding 无 output，OutputPricePerToken 置 0。
	s.fallbackPrices["doubao-embedding-vision"] = &ModelPricing{
		InputPricePerToken:      0.098e-6, // ¥0.7/MTok ≈ $0.098（文本输入）
		ImageInputPricePerToken: 0.252e-6, // ¥1.8/MTok ≈ $0.252（图片输入）
		OutputPricePerToken:     0,
		SupportsCacheBreakdown:  false,
	}

	// xAI Grok 4.5: $2 input / $0.30 cached input / $6 output below 200k;
	// long-context rates are $4 / $0.60 / $12 (>=200k prompt tokens).
	s.fallbackPrices["grok-4.5"] = &ModelPricing{
		InputPricePerToken:            2e-6,
		OutputPricePerToken:           6e-6,
		CacheReadPricePerToken:        0.3e-6,
		SupportsCacheBreakdown:        false,
		LongContextInputThreshold:     200000,
		LongContextThresholdInclusive: true,
		LongContextInputMultiplier:    2,
		LongContextOutputMultiplier:   2,
	}

	// xAI Grok 4.6: $2 input / $0.50 cached input / $6 output below 200k;
	// long-context rates are $4 / $1 / $12 (>=200k prompt tokens).
	s.fallbackPrices["grok-4.6"] = &ModelPricing{
		InputPricePerToken:            2e-6,
		OutputPricePerToken:           6e-6,
		CacheReadPricePerToken:        0.5e-6,
		SupportsCacheBreakdown:        false,
		LongContextInputThreshold:     200000,
		LongContextThresholdInclusive: true,
		LongContextInputMultiplier:    2,
		LongContextOutputMultiplier:   2,
	}

	// xAI Grok 4.3: $1.25 input / $0.20 cached / $2.50 output below 200k;
	// long-context rates are $2.50 / $0.40 / $5.
	s.fallbackPrices["grok-4.3"] = &ModelPricing{
		InputPricePerToken:            1.25e-6,
		OutputPricePerToken:           2.5e-6,
		CacheReadPricePerToken:        0.2e-6,
		SupportsCacheBreakdown:        false,
		LongContextInputThreshold:     200000,
		LongContextThresholdInclusive: true,
		LongContextInputMultiplier:    2,
		LongContextOutputMultiplier:   2,
	}
	// Grok 4.20 variants share the official $1.25 / $0.20 / $2.50 card
	// (and $2.50 / $0.40 / $5 long-context rates) with Grok 4.3.
	s.fallbackPrices["grok-4.20"] = &ModelPricing{
		InputPricePerToken:            1.25e-6,
		OutputPricePerToken:           2.5e-6,
		CacheReadPricePerToken:        0.2e-6,
		SupportsCacheBreakdown:        false,
		LongContextInputThreshold:     200000,
		LongContextThresholdInclusive: true,
		LongContextInputMultiplier:    2,
		LongContextOutputMultiplier:   2,
	}

	// Keep legacy Grok 3 Mini requests on their own historical xAI price card;
	// otherwise the generic Grok fallback bills them as Grok 4.5.
	s.fallbackPrices["grok-3-mini"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    0.50e-6,
		CacheReadPricePerToken: 0.075e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["grok-3-mini-fast"] = &ModelPricing{
		InputPricePerToken:     0.60e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.15e-6,
		SupportsCacheBreakdown: false,
	}
	// xAI Grok Build 0.1 (official docs: $1 input / $0.20 cached input /
	// $2 output per MTok). Composer is available only through Grok Build and
	// has no standalone public API rate card, so its aliases use this coding
	// model rate instead of silently billing at zero.
	s.fallbackPrices["grok-build-0.1"] = &ModelPricing{
		InputPricePerToken:            1e-6,
		OutputPricePerToken:           2e-6,
		CacheReadPricePerToken:        0.2e-6,
		SupportsCacheBreakdown:        false,
		LongContextInputThreshold:     200000,
		LongContextThresholdInclusive: true,
		LongContextInputMultiplier:    2,
		LongContextOutputMultiplier:   2,
	}
}

// GetFallbackPricing 公开访问硬编码回退价（getFallbackPricing 的 public wrapper）。
// 用于模型广场等展示场景，在配置化 pricing catalog 命中失败时对齐真实计费口径。
//
// 注意：返回值中 DeepSeek 系列的单价是官方高峰 CNY 报价通过固定内部核算汇率换算得到的
// USD 数字（见 initFallbackPricing 中 deepSeekCNYPerMillionTokens）；空闲价为高峰一半。
// GLM 采用 z.ai 国际版 USD 报价，Kimi/MiniMax 采用官方 USD 报价。调用方应以「参考单价」
// 而非精确市场价处理。若模型没有硬编码 fallback，返回 nil。
func (s *BillingService) GetFallbackPricing(model string) *ModelPricing {
	return s.getFallbackPricing(model)
}

// matchesExactModelAlias only accepts the canonical model ID and the
// explicitly supported Google-style "models/" spelling. In particular, it
// must not use HasSuffix: a future or third-party provider namespace is a
// distinct SKU and must never inherit a hard-coded free price.
func matchesExactModelAlias(model, canonical string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	canonical = strings.ToLower(strings.TrimSpace(canonical))
	return model == canonical || model == "models/"+canonical
}

// matchesModelFamily 判断 model 属于 family 这一档，且不是它的更高版本。
//
// 与裸 strings.Contains 的差别是两个边界检查：
//   - family 前面不能紧邻字母或数字，避免无关 ID 误命中；
//   - family 后面不能紧跟版本延续字符（数字、".数字"、"-数字"）。
//
// 第二条是关键。glm-5.2 上线时曾被 strings.Contains(model, "glm-5") 套上低一档的
// glm-5 价，少收约 29% 且没有任何告警——宽匹配会把每一个未登记的同族新 SKU 静默
// 降档计价。加上边界后，未知的 glm-5.3 / kimi-k2-0905 一类会落空并返回
// ErrModelPricingUnavailable，暴露出来总比按错价扣费好。
//
// 后缀是词而非数字时仍然命中（glm-5-turbo 属于 glm-5 档），这类变体在本表里都有
// 各自更靠前的分支，靠"最具体优先"的排列顺序兜住。
func matchesModelFamily(model, family string) bool {
	for idx := 0; idx+len(family) <= len(model); idx++ {
		if model[idx:idx+len(family)] != family {
			continue
		}
		if idx > 0 && isModelIDWordByte(model[idx-1]) {
			continue
		}
		if modelVersionContinues(model[idx+len(family):]) {
			continue
		}
		return true
	}
	return false
}

func isModelIDWordByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// modelVersionContinues 判断紧跟 family 之后的内容是否在延续版本号，
// 即 "5" → "5.2" / "5-1" / "52" 这三种写法。
func modelVersionContinues(rest string) bool {
	if rest == "" {
		return false
	}
	if rest[0] >= '0' && rest[0] <= '9' {
		return true
	}
	return (rest[0] == '.' || rest[0] == '-') && len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9'
}

// getFallbackPricingStrict 只查兜底表里这个模型自己的条目。
//
// 它对应 getFallbackPricing 的前两步——精确命中与 GLM 的 SKU 归一化——之后的分支
// 全是关键字猜测：含 opus/sonnet/haiku 的按系列套价，剩下任何含 claude 的一律套
// claude-sonnet-4。兜底表里逐个 SKU 写死的价格是开发者的真实决定，算一个来源；
// 关键字分支猜出来的不算。
func (s *BillingService) getFallbackPricingStrict(model string) *ModelPricing {
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if pricing := s.fallbackPrices[modelLower]; pricing != nil {
		return pricing
	}
	strictAliasModel := modelLower
	// Apply only the same-SKU spellings admitted by the strict catalog lookup.
	// This keeps explicit namespaces such as openai/gpt-5.4 and models/gemini-*
	// working when the dynamic catalog is unavailable, without accepting an
	// arbitrary provider/model suffix.
	if _, ok := explicitPricingAliasTarget(modelLower); ok {
		if normalized := normalizeModelNameForPricing(modelLower); normalized != modelLower {
			strictAliasModel = normalized
			if pricing := s.fallbackPrices[normalized]; pricing != nil {
				return pricing
			}
		}
	}
	// Kimi Code documents these two bare IDs as aliases of the kimi-k3 billing
	// tier. Exact matching is intentional: kimi-k30 and bracketed context
	// selectors remain distinct, unpriced SKUs.
	switch modelLower {
	case "k3", "k3-256k":
		return s.fallbackPrices["kimi-k3"]
	}
	if normalized := normalizeGLMBillingModelStrict(strictAliasModel); normalized != "" {
		return s.fallbackPrices[normalized]
	}
	return nil
}

var glmBillingCanonicalIDs = buildGLMBillingCanonicalIDs()

func buildGLMBillingCanonicalIDs() []string {
	caps, ok := GetProviderGatewayCapabilities(PlatformZhipu)
	if !ok {
		return nil
	}
	supported := caps.SupportedModelIDs
	ids := make([]string, 0, len(supported))
	for _, id := range supported {
		if trimmed := strings.ToLower(strings.TrimSpace(id)); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) > len(ids[j])
		}
		return ids[i] < ids[j]
	})
	return ids
}

func normalizeGLMBillingModel(model string) string {
	normalized := strings.ToLower(NormalizeGLMModel(model))
	for _, canonical := range glmBillingCanonicalIDs {
		if normalized == canonical || strings.HasPrefix(normalized, canonical+"-") {
			return canonical
		}
	}
	return ""
}

func normalizeGLMBillingModelStrict(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	for _, canonical := range glmBillingCanonicalIDs {
		if normalized == canonical {
			return canonical
		}
		if suffix, ok := strings.CutPrefix(normalized, canonical+"-"); ok && isGLMReleaseSnapshotSuffix(suffix) {
			return canonical
		}
	}
	return ""
}

func isGLMReleaseSnapshotSuffix(suffix string) bool {
	switch len(suffix) {
	case 4, 6, 8:
	default:
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// getFallbackPricing 根据模型系列获取回退价格
func (s *BillingService) getFallbackPricing(model string) *ModelPricing {
	modelLower := strings.ToLower(model)
	if pricing := s.fallbackPrices[modelLower]; pricing != nil {
		return pricing
	}
	if normalized := normalizeGLMBillingModel(modelLower); normalized != "" {
		return s.fallbackPrices[normalized]
	}

	// 按模型系列匹配
	if strings.Contains(modelLower, "fable-5-1") || strings.Contains(modelLower, "fable-5.1") ||
		strings.Contains(modelLower, "fable5.1") || strings.Contains(modelLower, "fable51") {
		return s.fallbackPrices["claude-fable-5-1"]
	}
	if strings.Contains(modelLower, "fable-5") || strings.Contains(modelLower, "fable5") {
		return s.fallbackPrices["claude-fable-5"]
	}
	if strings.Contains(modelLower, "opus") {
		// "opus-5" 必须先判：不能用裸 "5" 匹配，否则 claude-opus-4-5 会被误判。
		if strings.Contains(modelLower, "opus-5") || strings.Contains(modelLower, "opus5") {
			return s.fallbackPrices["claude-opus-5"]
		}
		if strings.Contains(modelLower, "4.8") || strings.Contains(modelLower, "4-8") {
			return s.fallbackPrices["claude-opus-4.8"]
		}
		if strings.Contains(modelLower, "4.7") || strings.Contains(modelLower, "4-7") {
			return s.fallbackPrices["claude-opus-4.7"]
		}
		if strings.Contains(modelLower, "4.6") || strings.Contains(modelLower, "4-6") {
			return s.fallbackPrices["claude-opus-4.6"]
		}
		if strings.Contains(modelLower, "4.5") || strings.Contains(modelLower, "4-5") {
			return s.fallbackPrices["claude-opus-4.5"]
		}
		return s.fallbackPrices["claude-3-opus"]
	}
	if strings.Contains(modelLower, "sonnet") {
		if strings.Contains(modelLower, "4") && !strings.Contains(modelLower, "3") {
			return s.fallbackPrices["claude-sonnet-4"]
		}
		return s.fallbackPrices["claude-3-5-sonnet"]
	}
	if strings.Contains(modelLower, "haiku") {
		if strings.Contains(modelLower, "3-5") || strings.Contains(modelLower, "3.5") {
			return s.fallbackPrices["claude-3-5-haiku"]
		}
		return s.fallbackPrices["claude-3-haiku"]
	}
	// Claude 未知型号统一回退到 Sonnet，避免计费中断。
	if strings.Contains(modelLower, "claude") {
		return s.fallbackPrices["claude-sonnet-4"]
	}
	if strings.Contains(modelLower, "gemini-3.1-pro") || strings.Contains(modelLower, "gemini-3-1-pro") {
		return s.fallbackPrices["gemini-3.1-pro"]
	}
	if strings.Contains(modelLower, "gemini-3.6-flash") || strings.Contains(modelLower, "gemini-3-6-flash") {
		return s.fallbackPrices["gemini-3.6-flash"]
	}

	// DeepSeek V4 系列：仅匹配已知 V4 Pro/Flash。兼容别名应先由账号映射解析
	// 成真实上游模型再计费；这里不直接猜测，避免自定义映射被错误套价。
	// "deepseek-v4-flash-vision-exp" 含 "deepseek-v4-flash" 子串，显式分支置于 flash 之前，语义清晰。
	if strings.Contains(modelLower, "deepseek-v4-flash-vision-exp") {
		return s.fallbackPrices["deepseek-v4-flash-vision-exp"]
	}
	if strings.Contains(modelLower, "deepseek-v4-flash") {
		return s.fallbackPrices["deepseek-v4-flash"]
	}
	if strings.Contains(modelLower, "deepseek-v4-pro") {
		return s.fallbackPrices["deepseek-v4-pro"]
	}
	// ---- 国产 LLM 兜底匹配 ----
	// 匹配策略：长 key 优先（具体模型 → 系列 / 厂商），未知型号不回退以避免误计价。
	// 与 DeepSeek 一样采用"白名单"语义：未在本表命中的国产模型 alias 一律不返回兜底价。

	// 智谱 GLM（z.ai 公开 SKU：glm-5.2 / glm-5.1 / glm-5 / glm-5-turbo / glm-4.7 / glm-4.6 / glm-4.5 等）
	// 匹配顺序：先判别最高 tier，再依次降级。
	// 注意：glm-5.2 / glm-5.1 必须排在 glm-5 之前——"glm-5.2" 含子串 "glm-5"，
	// 顺序颠倒会让它误命中低一档的 glm-5 价（$1.0/$3.2 而非 $1.4/$4.4）。
	//
	// 同时接受短横线写法：Windsurf 的官方模型表用 glm-5-1 而不是 glm-5.1
	// （见 windsurfOfficialModelIDs），只认点号会让它掉到 glm-5 档、同样少收 29%。
	// Kimi / MiniMax 的分支早就两种写法都写了，这里是补齐 GLM 漏掉的那一半。
	if strings.Contains(modelLower, "glm-5.2") || strings.Contains(modelLower, "glm-5-2") {
		return s.fallbackPrices["glm-5.2"]
	}
	if strings.Contains(modelLower, "glm-5.1") || strings.Contains(modelLower, "glm-5-1") {
		return s.fallbackPrices["glm-5.1"]
	}
	if strings.Contains(modelLower, "glm-5-turbo") || strings.Contains(modelLower, "glm-5turbo") {
		return s.fallbackPrices["glm-5-turbo"]
	}
	if matchesModelFamily(modelLower, "glm-5") {
		return s.fallbackPrices["glm-5"]
	}
	if matchesExactModelAlias(modelLower, "glm-4.7-flashx") {
		return s.fallbackPrices["glm-4.7-flashx"]
	}
	if matchesExactModelAlias(modelLower, "glm-4.7-flash") {
		return s.fallbackPrices["glm-4.7-flash"]
	}
	if matchesModelFamily(modelLower, "glm-4.7") {
		return s.fallbackPrices["glm-4.7"]
	}
	if matchesModelFamily(modelLower, "glm-4.6") {
		return s.fallbackPrices["glm-4.6"]
	}
	if matchesExactModelAlias(modelLower, "glm-4.5-flash") {
		return s.fallbackPrices["glm-4.5-flash"]
	}
	if strings.Contains(modelLower, "glm-4.5-x") || strings.Contains(modelLower, "glm-4.5x") {
		return s.fallbackPrices["glm-4.5-x"]
	}
	if strings.Contains(modelLower, "glm-4.5-airx") || strings.Contains(modelLower, "glm-4.5airx") {
		return s.fallbackPrices["glm-4.5-airx"]
	}
	if strings.Contains(modelLower, "glm-4.5-air") || strings.Contains(modelLower, "glm-4.5air") {
		return s.fallbackPrices["glm-4.5-air"]
	}
	if matchesModelFamily(modelLower, "glm-4.5") {
		return s.fallbackPrices["glm-4.5"]
	}
	if strings.Contains(modelLower, "glm-4-32b") {
		return s.fallbackPrices["glm-4-32b-0414-128k"]
	}

	// 月之暗面 Kimi（kimi-k3 / k3 / k3-256k / kimi-k2.6 / kimi-for-coding / kimi-k2.5 / kimi-k2-thinking / kimi-k2）
	// K2-0905 / K2-0711 官方未保留定价，不进入 fallback。
	// K3 规则置于 K2 前：API Platform 仅官方 kimi-k3（及 / 路径后缀）；
	// Code bare aliases 仅精确 k3 / k3-256k 或 /k3|/k3-256k 后缀，避免 kimi-k30 等未知型号误命中。
	// 注意：kimi-k3[1m] 是 Claude Code 上下文选择语法，不是 Kimi API 模型 ID，不进入 fallback。
	if strings.Contains(modelLower, "kimi-for-coding") {
		return s.fallbackPrices["kimi-for-coding"]
	}
	if modelLower == "kimi-k3" || strings.HasSuffix(modelLower, "/kimi-k3") ||
		modelLower == "k3" || modelLower == "k3-256k" ||
		strings.HasSuffix(modelLower, "/k3") || strings.HasSuffix(modelLower, "/k3-256k") {
		return s.fallbackPrices["kimi-k3"]
	}
	if strings.Contains(modelLower, "kimi-k2.6") || strings.Contains(modelLower, "kimi-k2-6") {
		return s.fallbackPrices["kimi-k2.6"]
	}
	if strings.Contains(modelLower, "kimi-k2.5") || strings.Contains(modelLower, "kimi-k2-5") {
		return s.fallbackPrices["kimi-k2.5"]
	}
	if strings.Contains(modelLower, "kimi-k2-thinking") || strings.Contains(modelLower, "kimi-k2-thinking-") {
		return s.fallbackPrices["kimi-k2-thinking"]
	}
	// 基础档 K2 用 matchesModelFamily 而非 Contains：注释里说的"K2-0905 / K2-0711
	// 官方未保留定价，不进入 fallback"过去并没有真的生效——裸 Contains 会把
	// kimi-k2-0905 也算成 kimi-k2。同理未来的 kimi-k2.7 也不该套 K2 基础价。
	if matchesModelFamily(modelLower, "kimi-k2") || matchesModelFamily(modelLower, "kimi/k2") {
		return s.fallbackPrices["kimi-k2"]
	}

	// MiniMax M 系列（M3 / M2.7 / M2.5 / M2.1 / M2；含 highspeed 变体）
	if matchesModelFamily(modelLower, "minimax-m3") {
		return s.fallbackPrices["minimax-m3"]
	}
	if strings.Contains(modelLower, "minimax-m2.7-highspeed") || strings.Contains(modelLower, "minimax-m2-7-highspeed") {
		return s.fallbackPrices["minimax-m2.7-highspeed"]
	}
	if strings.Contains(modelLower, "minimax-m2.7") || strings.Contains(modelLower, "minimax-m2-7") {
		return s.fallbackPrices["minimax-m2.7"]
	}
	if strings.Contains(modelLower, "minimax-m2.5") || strings.Contains(modelLower, "minimax-m2-5") {
		return s.fallbackPrices["minimax-m2.5"]
	}
	if strings.Contains(modelLower, "minimax-m2.1") || strings.Contains(modelLower, "minimax-m2-1") {
		return s.fallbackPrices["minimax-m2.1"]
	}
	if matchesModelFamily(modelLower, "minimax-m2") || matchesModelFamily(modelLower, "minimax-m-2") {
		return s.fallbackPrices["minimax-m2"]
	}

	// 火山方舟 豆包 Embedding（多模态向量化）。
	// most-specific-first：放在未来任何 doubao-embedding / doubao 宽匹配之前。
	// 覆盖带版本后缀的别名（如 doubao-embedding-vision-251215）。
	if strings.Contains(modelLower, "doubao-embedding-vision") {
		return s.fallbackPrices["doubao-embedding-vision"]
	}

	// OpenAI（GPT-5 / Codex 族）：仅匹配已知型号，避免未知 OpenAI 型号误计价。
	if normalized := normalizeKnownOpenAICodexModel(modelLower); normalized != "" {
		switch normalized {
		case "gpt-5.6-sol":
			return s.fallbackPrices["gpt-5.6-sol"]
		case "gpt-5.6-terra":
			return s.fallbackPrices["gpt-5.6-terra"]
		case "gpt-5.6-luna":
			return s.fallbackPrices["gpt-5.6-luna"]
		case "gpt-5.5-pro":
			return s.fallbackPrices["gpt-5.5-pro"]
		case "gpt-5.5":
			return s.fallbackPrices["gpt-5.5"]
		case "gpt-5.4-mini":
			return s.fallbackPrices["gpt-5.4-mini"]
		case "gpt-5.4-nano":
			return s.fallbackPrices["gpt-5.4-nano"]
		case "gpt-5.4":
			return s.fallbackPrices["gpt-5.4"]
		case "gpt-5.2":
			return s.fallbackPrices["gpt-5.2"]
		case "gpt-5.3-codex", "gpt-5.3-codex-spark":
			return s.fallbackPrices["gpt-5.3-codex"]
		}
	}

	switch modelLower {
	case "grok", "grok-latest", "grok-4.6", "grok-4.6-latest":
		return s.fallbackPrices["grok-4.6"]
	case "grok-4.5", "grok-4.5-latest":
		return s.fallbackPrices["grok-4.5"]
	case "grok-3-mini":
		return s.fallbackPrices["grok-3-mini"]
	case "grok-3-mini-fast":
		return s.fallbackPrices["grok-3-mini-fast"]
	case "grok-4.3":
		return s.fallbackPrices["grok-4.3"]
	case "grok-4.20-0309-reasoning",
		"grok-4.20-0309-non-reasoning",
		"grok-4.20-multi-agent-0309",
		"grok-4.20-reasoning",
		"grok-4.20-non-reasoning":
		return s.fallbackPrices["grok-4.20"]
	case "grok-build", "grok-build-latest", "grok-build-0.1", "grok-composer", "grok-composer-2.5-fast", "composer-2.5":
		return s.fallbackPrices["grok-build-0.1"]
	}

	// Unknown Grok text IDs (grok-5, dated snapshots, provider-prefixed) inherit
	// the current default text card so a new model cannot ship unbilled.
	if pricing := s.grokUnknownTextFamilyFallback(modelLower); pricing != nil {
		return pricing
	}

	return nil
}

func (s *BillingService) grokUnknownTextFamilyFallback(model string) *ModelPricing {
	if s == nil || !isGrokUnknownTextFamilyModel(model) {
		return nil
	}
	return s.fallbackPrices["grok-4.6"]
}

func isGrokUnknownTextFamilyModel(model string) bool {
	native := strings.ToLower(strings.TrimSpace(xai.StripGrokProviderPrefix(model)))
	if isGrokMediaFamilyModel(native) {
		return false
	}
	switch {
	case native == "grok", native == "grok-latest":
		return true
	case strings.HasPrefix(native, "grok-build"),
		strings.HasPrefix(native, "grok-composer"),
		strings.HasPrefix(native, "composer-"):
		return true
	case len(native) > 5 && strings.HasPrefix(native, "grok-"):
		rest := native[len("grok-"):]
		return rest[0] >= '0' && rest[0] <= '9'
	default:
		return false
	}
}

// isGrokMediaFamilyModel matches ids that are billed per image/video/audio unit
// rather than per token, so version-numbered media ids (grok-2-image-1212,
// grok-5-video) cannot slip into the unknown-text fallback and pick up a token
// card. "vision" is deliberately absent: multimodal chat models are token billed.
func isGrokMediaFamilyModel(native string) bool {
	for _, marker := range []string{"imagine", "image", "video", "audio", "speech", "tts", "transcribe", "realtime"} {
		if strings.Contains(native, marker) {
			return true
		}
	}
	return false
}

// HasIdentifiedTokenPricing 判断模型能否在价格表中被"确定性识别"出 token 价格。
//
// 与 GetModelPricing 的关键区别：本函数拒绝按子串猜系列的兜底。GetModelPricing 会
// 让任意含 "haiku"/"opus"/"claude" 的名字（哪怕是不存在的型号）落到 getFallbackPricing
// 的系列兜底价上，因此凡是模型名来自外部、且"能查到价"会直接影响计费金额的场景
// （如按上游响应自报模型计费），都必须用本函数而不是 GetModelPricing 做准入判断。
func (s *BillingService) HasIdentifiedTokenPricing(model string) bool {
	return s.HasIdentifiedTokenPricingForPlatform("", model)
}

func (s *BillingService) HasIdentifiedTokenPricingForPlatform(platform, model string) bool {
	return s.HasIdentifiedTokenPricingForPlatforms([]string{platform}, model)
}

func (s *BillingService) HasIdentifiedTokenPricingForPlatforms(platforms []string, model string) bool {
	if s == nil {
		return false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if s.pricingService != nil {
		// 仅有图片价的条目不能用于 token 计费，口径与 GetModelPricing 保持一致。
		if pricing := s.pricingService.GetIdentifiedModelPricingForPlatforms(platforms, model); pricing != nil && !pricing.TokenPricingAbsent {
			return true
		}
	}
	pricing, ok := s.fallbackPrices[model]
	return ok && pricing != nil
}

// GetModelPricing 获取模型价格配置
func (s *BillingService) GetModelPricing(model string) (*ModelPricing, error) {
	return s.GetModelPricingForPlatform("", model)
}

func (s *BillingService) GetModelPricingForPlatform(platform, model string) (*ModelPricing, error) {
	return s.GetModelPricingForPlatforms([]string{platform}, model)
}

func (s *BillingService) GetModelPricingForPlatforms(platforms []string, model string) (*ModelPricing, error) {
	return s.getModelPricingForPlatforms(platforms, model, true)
}

// GetModelPricingStrict 只在模型自身配过价时返回价格，不接受跨模型推断出来的价格。
//
// 供准入守卫、实时后扣与补偿结算共同使用。GetModelPricing 那条链的最后两级
// （PricingService 的 family/OpenAI 兜底、以及这里 getFallbackPricing 的关键字分支）
// 会把别的模型的价格套到未知型号上，用它判断"配没配价"接近恒真——任何含 claude
// 的模型名都能拿到 claude-sonnet-4 的价，任何 gpt- 开头的都能兜到 DefaultTestModel。
//
// 参见 PricingService.LookupModelPricingStrict 对两种口径差异的完整说明。
func (s *BillingService) GetModelPricingStrict(model string) (*ModelPricing, error) {
	return s.GetModelPricingStrictForPlatform("", model)
}

func (s *BillingService) GetModelPricingStrictForPlatform(platform, model string) (*ModelPricing, error) {
	return s.GetModelPricingStrictForPlatforms([]string{platform}, model)
}

func (s *BillingService) GetModelPricingStrictForPlatforms(platforms []string, model string) (*ModelPricing, error) {
	return s.getModelPricingForPlatforms(platforms, model, false)
}

// GetImageTokenPricingStrict resolves the exact catalog entry for an Image API
// model without requiring a complete ordinary text input/output token pair.
//
// GPT Image models can expose a dedicated image-output token price while
// omitting the ordinary text-output price. That entry is incomplete for a
// chat/token route, but it is complete for /v1/images/* as long as the image
// dimensions used by that endpoint are explicitly priced.
func (s *BillingService) GetImageTokenPricingStrict(model string, requireImageInput bool) (*ModelPricing, error) {
	return s.GetImageTokenPricingStrictForPlatform("", model, requireImageInput)
}

func (s *BillingService) GetImageTokenPricingStrictForPlatform(platform, model string, requireImageInput bool) (*ModelPricing, error) {
	return s.GetImageTokenPricingStrictForPlatforms([]string{platform}, model, requireImageInput)
}

func (s *BillingService) GetImageTokenPricingStrictForPlatforms(platforms []string, model string, requireImageInput bool) (*ModelPricing, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || s == nil || s.pricingService == nil {
		return nil, fmt.Errorf("%w for image model: %s", ErrModelPricingUnavailable, model)
	}
	entry := s.pricingService.LookupModelPricingStrictForPlatforms(platforms, model)
	inputConfigured := entry != nil && (entry.InputPriceExplicit ||
		(!entry.PricePresenceKnown && entry.InputCostPerToken > 0))
	imageOutputConfigured := entry != nil && (entry.ImageOutputPriceExplicit ||
		(!entry.PricePresenceKnown && entry.OutputCostPerImageToken > 0))
	imageInputConfigured := entry != nil && (entry.ImageInputPriceExplicit ||
		(!entry.PricePresenceKnown && entry.InputCostPerImageToken > 0))
	if entry == nil ||
		!inputConfigured ||
		!imageOutputConfigured ||
		(requireImageInput && !imageInputConfigured) {
		return nil, fmt.Errorf("%w for image token model: %s", ErrModelPricingUnavailable, model)
	}
	return s.modelPricingFromCatalogEntry(model, entry)
}

func (s *BillingService) modelPricingFromCatalogEntry(model string, catalogEntry *ModelPriceEntry) (*ModelPricing, error) {
	if catalogEntry == nil {
		return nil, fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
	}
	// Presence-aware catalog entries preserve an explicitly configured 1h tier
	// even when it is zero or below the 5m price. Legacy in-memory entries
	// retain the historical positive-and-higher condition.
	price5m := catalogEntry.CacheCreationInputTokenCost
	price1h := catalogEntry.CacheCreationInputTokenCostAbove1hr
	enableBreakdown := catalogEntry.CacheCreationAbove1hrPriceExplicit &&
		price1h != price5m
	if !catalogEntry.PricePresenceKnown {
		enableBreakdown = price1h > 0 && price1h > price5m
	}
	pricing := s.applyModelSpecificPricingPolicy(model, &ModelPricing{
		InputPricePerToken:                 catalogEntry.InputCostPerToken,
		InputPriceExplicit:                 catalogEntry.InputPriceExplicit,
		InputPricePerTokenPriority:         catalogEntry.InputCostPerTokenPriority,
		OutputPricePerToken:                catalogEntry.OutputCostPerToken,
		OutputPriceExplicit:                catalogEntry.OutputPriceExplicit,
		OutputPricePerTokenPriority:        catalogEntry.OutputCostPerTokenPriority,
		CacheCreationPricePerToken:         catalogEntry.CacheCreationInputTokenCost,
		CacheCreationPricePerTokenPriority: catalogEntry.CacheCreationInputTokenCostPriority,
		CacheCreationPriceExplicit:         catalogEntry.CacheCreationPriceExplicit,
		CacheReadPricePerToken:             catalogEntry.CacheReadInputTokenCost,
		CacheReadPricePerTokenPriority:     catalogEntry.CacheReadInputTokenCostPriority,
		CacheReadPriceExplicit:             catalogEntry.CacheReadPriceExplicit,
		CacheCreation5mPrice:               price5m,
		CacheCreation1hPrice:               price1h,
		CacheCreation1hPriceExplicit:       catalogEntry.CacheCreationAbove1hrPriceExplicit,
		SupportsCacheBreakdown:             enableBreakdown,
		LongContextInputThreshold:          catalogEntry.LongContextInputTokenThreshold,
		LongContextInputMultiplier:         catalogEntry.LongContextInputCostMultiplier,
		LongContextOutputMultiplier:        catalogEntry.LongContextOutputCostMultiplier,
		LongContextPricingExplicit:         catalogEntry.LongContextPricingExplicit,
		LongContextThresholdInclusive:      catalogEntry.LongContextThresholdInclusive,
		OperatorOverride:                   catalogEntry.OperatorOverride,
		ImageInputPricePerToken:            catalogEntry.InputCostPerImageToken,
		ImageInputPriceExplicit:            catalogEntry.ImageInputPriceExplicit,
		ImageOutputPricePerToken:           catalogEntry.OutputCostPerImageToken,
		ImageOutputPriceExplicit:           catalogEntry.ImageOutputPriceExplicit,
		PricePresenceKnown:                 catalogEntry.PricePresenceKnown,
		InputPriorityPriceExplicit:         catalogEntry.InputPriorityPriceExplicit,
		OutputPriorityPriceExplicit:        catalogEntry.OutputPriorityPriceExplicit,
		CacheCreationPriorityPriceExplicit: catalogEntry.CacheCreationPriorityPriceExplicit,
		CacheReadPriorityPriceExplicit:     catalogEntry.CacheReadPriorityPriceExplicit,
	})
	if err := validateFiniteModelPricing(model, pricing); err != nil {
		return nil, err
	}
	return pricing, nil
}

func (s *BillingService) getModelPricingForPlatforms(platforms []string, model string, allowInference bool) (*ModelPricing, error) {
	// 标准化模型名称（转小写）
	model = strings.ToLower(model)
	pricingPlatform := basePricingPlatform(platforms)

	// 1. 优先从动态价格服务获取
	if s.pricingService != nil {
		var catalogEntry *ModelPriceEntry
		var matchedPlatform string
		if allowInference {
			catalogEntry, matchedPlatform = s.pricingService.lookupModelPricingForPlatforms(platforms, model, true)
		} else {
			catalogEntry, matchedPlatform = s.pricingService.lookupModelPricingForPlatforms(platforms, model, false)
		}
		// input/output token 价不完整的条目（如 LiteLLM 的 imagen 类模型）不能用于
		// token 计费：直接返回会把缺失的一侧按 $0 计费。跳过后走 fallback，
		// 无 fallback 则 fail-closed（ErrModelPricingUnavailable）。
		// 图片计费路径（getDefaultImagePrice / getImageUnitPrice）直接读
		// PricingService，不受影响。
		if catalogEntry != nil && catalogEntry.TokenPricingAbsent {
			catalogEntry = nil
		}
		if catalogEntry != nil {
			pricing, err := s.modelPricingFromCatalogEntry(model, catalogEntry)
			if err != nil {
				return nil, err
			}
			// 价格目录（含管理端手动覆盖后的生效价）对 DeepSeek 官方分时 SKU 存的是
			// 官方空闲价，高峰按北京时间翻倍。第三方中转（platform 非 deepseek）
			// 不是官方计费口径，usesDeepSeekOfficialTimePricing 已把它挡在外面。
			if !pricing.OperatorOverride && usesDeepSeekOfficialTimePricing(matchedPlatform, model) {
				pricing.OfficialTimePricing = true
				pricing.OfficialTimeBaseIsOffPeak = true
			}
			return pricing, nil
		}
	}

	// 2. 使用硬编码回退价格
	if isDeepSeekModel(model) && !deepSeekFallbackAllowedForPlatforms(platforms) {
		return nil, fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
	}
	var fallback *ModelPricing
	if allowInference {
		fallback = s.getFallbackPricing(model)
	} else {
		fallback = s.getFallbackPricingStrict(model)
	}
	if fallback != nil {
		// 按模型名去重:每个模型每进程最多打一条 warn,避免热路径每请求刷屏（issue #3394）。
		// model 在函数入口已 ToLower,故 GLM-5.2 / glm-5.2 视为同一条目。
		if _, seen := s.fallbackWarnSeen.LoadOrStore(model, struct{}{}); !seen {
			log.Printf("[Billing] Using fallback pricing for model: %s", model)
		}
		pricing := s.applyModelSpecificPricingPolicy(model, fallback)
		if usesDeepSeekOfficialTimePricing(pricingPlatform, model) {
			pricing.OfficialTimePricing = true
			pricing.OfficialTimeBaseIsOffPeak = true
		}
		if err := validateFiniteModelPricing(model, pricing); err != nil {
			return nil, err
		}
		return pricing, nil
	}

	return nil, fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
}

// GetModelPricingWithChannel 获取模型定价，渠道配置的价格覆盖默认值。
// 渠道未配置图片输出价格时，图片 token 回退到渠道文本输出价；只有显式 0 才免费。
func (s *BillingService) GetModelPricingWithChannel(model string, channelPricing *ChannelModelPricing) (*ModelPricing, error) {
	return s.GetModelPricingWithChannelForPlatform("", model, channelPricing)
}

func (s *BillingService) GetModelPricingWithChannelForPlatform(platform, model string, channelPricing *ChannelModelPricing) (*ModelPricing, error) {
	pricing, err := s.GetModelPricingForPlatform(platform, model)
	if err != nil {
		return nil, err
	}
	if channelPricing == nil {
		return pricing, nil
	}
	// 防止修改 fallbackPrices 中的共享指针
	cloned := *pricing
	pricing = &cloned
	// 只有绝对价格覆盖才会脱离官方价卡。仅配置 Fast/Flex 或区间倍率时，
	// 官方峰谷价仍先确定基础价，各业务倍率随后各应用一次。
	if channelHasAbsoluteTokenPrice(channelPricing) {
		pricing.OfficialTimePricing = false
		pricing.OfficialTimeBaseIsOffPeak = false
	}
	applyChannelTokenPriceOverrides(pricing, channelPricing)
	pricing.FastMultiplier = channelPricing.FastMultiplier
	pricing.FlexMultiplier = channelPricing.FlexMultiplier
	applyChannelImagePrices(channelPricing, pricing)
	return pricing, nil
}

func channelHasAbsoluteTokenPrice(pricing *ChannelModelPricing) bool {
	return pricing != nil && (pricing.InputPrice != nil || pricing.OutputPrice != nil ||
		pricing.CacheWritePrice != nil || pricing.CacheReadPrice != nil ||
		pricing.ImageInputPrice != nil || pricing.ImageOutputPrice != nil)
}

// channelTierOverridePrice applies a Standard-tier override while preserving
// an explicit model-catalog Fast/Priority ratio. If the catalog has no tier
// price, generic service-tier defaults remain responsible for the fallback.
func channelTierOverridePrice(baseStandard, baseTier, channelStandard float64) float64 {
	if baseStandard > 0 && baseTier > 0 {
		return channelStandard * (baseTier / baseStandard)
	}
	return 0
}

func applyChannelTokenPriceOverrides(pricing *ModelPricing, channelPricing *ChannelModelPricing) {
	if pricing == nil || channelPricing == nil {
		return
	}
	if channelPricing.InputPrice != nil {
		priorityConfigured := inputPriorityPriceConfigured(pricing)
		priority := channelTierOverridePrice(pricing.InputPricePerToken, pricing.InputPricePerTokenPriority, *channelPricing.InputPrice)
		pricing.InputPricePerToken = *channelPricing.InputPrice
		pricing.InputPriceExplicit = true
		pricing.InputPricePerTokenPriority = priority
		// An explicit zero Standard price must also suppress generic Priority
		// fallback; otherwise a free channel override can be charged at the model
		// default tier rate even though the operator explicitly set zero.
		pricing.InputPriorityPriceExplicit = priorityConfigured || *channelPricing.InputPrice == 0
	}
	if channelPricing.OutputPrice != nil {
		priorityConfigured := outputPriorityPriceConfigured(pricing)
		priority := channelTierOverridePrice(pricing.OutputPricePerToken, pricing.OutputPricePerTokenPriority, *channelPricing.OutputPrice)
		pricing.OutputPricePerToken = *channelPricing.OutputPrice
		pricing.OutputPriceExplicit = true
		pricing.OutputPricePerTokenPriority = priority
		pricing.OutputPriorityPriceExplicit = priorityConfigured || *channelPricing.OutputPrice == 0
	}
	if channelPricing.CacheWritePrice != nil {
		priorityConfigured := cacheCreationPriorityPriceConfigured(pricing)
		priority := channelTierOverridePrice(pricing.CacheCreationPricePerToken, pricing.CacheCreationPricePerTokenPriority, *channelPricing.CacheWritePrice)
		pricing.CacheCreationPricePerToken = *channelPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = priority
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreationPriorityPriceExplicit = priorityConfigured || *channelPricing.CacheWritePrice == 0
		pricing.CacheCreation5mPrice = *channelPricing.CacheWritePrice
		if channelPricing.CacheWrite1hPrice == nil {
			// Preserve the pre-split behavior for existing configurations: a lone
			// cache_write_price continues to override both TTL tiers.
			pricing.CacheCreation1hPrice = *channelPricing.CacheWritePrice
			pricing.CacheCreation1hPriceExplicit = true
		}
	}
	if channelPricing.CacheWrite1hPrice != nil {
		pricing.CacheCreation1hPrice = *channelPricing.CacheWrite1hPrice
		pricing.CacheCreation1hPriceExplicit = true
		pricing.SupportsCacheBreakdown = true
	}
	if channelPricing.CacheReadPrice != nil {
		priorityConfigured := cacheReadPriorityPriceConfigured(pricing)
		priority := channelTierOverridePrice(pricing.CacheReadPricePerToken, pricing.CacheReadPricePerTokenPriority, *channelPricing.CacheReadPrice)
		pricing.CacheReadPricePerToken = *channelPricing.CacheReadPrice
		pricing.CacheReadPriceExplicit = true
		pricing.CacheReadPricePerTokenPriority = priority
		pricing.CacheReadPriorityPriceExplicit = priorityConfigured || *channelPricing.CacheReadPrice == 0
	}
}

// --- 统一计费入口 ---

// CostInput 统一计费输入
type CostInput struct {
	Ctx                       context.Context
	Model                     string
	Platform                  string
	Platforms                 []string
	GroupID                   *int64 // 用于渠道定价查找
	Group                     *Group
	Tokens                    UsageTokens
	RequestCount              int     // 按次计费时使用
	UsageUnits                float64 // 音频等连续计量单位（分钟/小时/百万字符）
	SizeTier                  string  // 按次/图片模式的层级标签（"1K","2K","4K","HD" 等）
	RateMultiplier            float64
	PricingAt                 time.Time             // 分时定价时刻：渠道 TimePricing 与 DeepSeek 官方峰谷价共用
	ServiceTier               string                // "priority","flex","" 等
	Resolver                  *ModelPricingResolver // 定价解析器
	Resolved                  *ResolvedPricing      // 可选：预解析的定价结果（避免重复 Resolve 调用）
	LongContextBillingEnabled *bool
}

// CalculateCostUnified 统一计费入口，支持三种计费模式。
// 使用 ModelPricingResolver 解析定价，然后根据 BillingMode 分发计算。
func (s *BillingService) CalculateCostUnified(input CostInput) (*CostBreakdown, error) {
	pricingPlatform := strings.TrimSpace(input.Platform)
	if pricingPlatform == "" {
		pricingPlatform = basePricingPlatform(input.Platforms)
	}
	if input.Resolver == nil {
		// 无 Resolver，回退到旧路径
		applyLongContextBilling := true
		if input.LongContextBillingEnabled != nil {
			applyLongContextBilling = *input.LongContextBillingEnabled
		}
		breakdown, err := s.calculateCostInternalForPlatform(
			pricingPlatform,
			input.Model,
			input.Tokens,
			input.RateMultiplier,
			input.ServiceTier,
			nil,
			applyLongContextBilling,
		)
		if err == nil {
			applyCostBreakdownMultiplier(breakdown, s.officialTimeMultiplierForPlatform(pricingPlatform, input.Model, input.PricingAt))
		}
		return breakdown, err
	}

	// 优先使用预解析结果，避免重复 Resolve 调用
	resolved := input.Resolved
	if resolved == nil {
		resolved = input.Resolver.Resolve(input.Ctx, PricingInput{
			Model:     input.Model,
			Platform:  input.Platform,
			Platforms: input.Platforms,
			GroupID:   input.GroupID,
			Group:     input.Group,
		})
	}

	// 保存时强制 > 0；若仍有负数泄漏（缓存/迁移残留），按 0 处理避免按 1x 误扣。
	if input.RateMultiplier < 0 {
		input.RateMultiplier = 0
	}

	var breakdown *CostBreakdown
	var err error
	switch resolved.Mode {
	case BillingModePerRequest, BillingModeImage, BillingModeVideo:
		breakdown, err = s.calculatePerRequestCost(resolved, input)
	default: // BillingModeToken
		breakdown, err = s.calculateTokenCost(resolved, input)
	}
	if err == nil && breakdown != nil {
		breakdown.BillingMode = string(resolved.Mode)
		if breakdown.BillingMode == "" {
			breakdown.BillingMode = string(BillingModeToken)
		}
	}
	return breakdown, err
}

// calculateTokenCost 按 token 区间计费
func (s *BillingService) calculateTokenCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	totalContext := input.Tokens.InputTokens + input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens

	// 分组开关是统一入口；账号 API 开关保留为额外开启能力，但 false 不否决分组配置。
	contextTierPricingEnabled := resolved.longContextPricingEnabled
	if input.LongContextBillingEnabled != nil && *input.LongContextBillingEnabled {
		contextTierPricingEnabled = true
	}

	pricingContext := totalContext
	if !contextTierPricingEnabled {
		// 渠道可能显式配置了第一档，也可能只配置高上下文档。用 1 token
		// 选择最低档；未命中时自然回退到渠道基础价。
		pricingContext = 1
	}
	pricing := input.Resolver.GetIntervalPricing(resolved, pricingContext)
	if pricing == nil {
		return nil, fmt.Errorf("no pricing available for model: %s: %w", input.Model, ErrModelPricingUnavailable)
	}

	// 默认价卡（Source=LiteLLM）应用 DeepSeek 官方价强制覆盖（幂等，GetModelPricing
	// 内部已强制过）；分组/渠道自定义定价保留运营者配置，不强制覆盖官方价。
	pricing = s.applyModelSpecificPricingPolicyEx(input.Model, pricing, resolved.Source == PricingSourceLiteLLM)

	// 官方长上下文阶梯仅在无区间定价时应用（区间定价已包含上下文分层）。
	applyLongCtx := len(resolved.Intervals) == 0 && contextTierPricingEnabled

	breakdown, err := s.computeTokenBreakdownValidated(
		input.Model,
		pricing,
		input.Tokens,
		input.RateMultiplier,
		input.ServiceTier,
		applyLongCtx,
	)
	if err != nil {
		return nil, err
	}
	pricingPlatform := strings.TrimSpace(input.Platform)
	if pricingPlatform == "" {
		pricingPlatform = basePricingPlatform(input.Platforms)
	}
	applyCostBreakdownMultiplier(breakdown, resolvedTokenTimeMultiplier(resolved, pricing, pricingPlatform, input.Model, input.PricingAt))
	return breakdown, nil
}

func (s *BillingService) computeTokenBreakdownValidated(
	model string,
	pricing *ModelPricing,
	tokens UsageTokens,
	rateMultiplier float64,
	serviceTier string,
	applyLongCtx bool,
) (*CostBreakdown, error) {
	if err := validateFiniteModelPricing(model, pricing); err != nil {
		return nil, err
	}
	if err := validateUsedModelPricingDimensions(model, pricing, tokens); err != nil {
		return nil, err
	}
	return s.computeTokenBreakdown(pricing, tokens, rateMultiplier, serviceTier, applyLongCtx), nil
}

// computeTokenBreakdown 是 token 计费的核心逻辑，由 calculateTokenCost 和 calculateCostInternal 共用。
// applyLongCtx 控制是否检查长上下文定价（区间定价已自含上下文分层，不需要额外应用）。
func (s *BillingService) computeTokenBreakdown(
	pricing *ModelPricing, tokens UsageTokens,
	rateMultiplier float64, serviceTier string,
	applyLongCtx bool,
) *CostBreakdown {
	// 保存时强制 > 0；若仍有负数泄漏，按 0 处理避免按 1x 误扣。
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}

	inputPrice := pricing.InputPricePerToken
	outputPrice := pricing.OutputPricePerToken
	cacheReadPrice := pricing.CacheReadPricePerToken
	cacheCreationPrice := pricing.CacheCreationPricePerToken
	cacheCreationMultiplier := 1.0
	inputTierMultiplier := 1.0
	var imageInputTierMultiplier float64
	outputTierMultiplier := 1.0
	var imageOutputTierMultiplier float64
	cacheCreationTierMultiplier := 1.0
	cacheReadTierMultiplier := 1.0

	serviceTier = normalizeBillingServiceTier(serviceTier)
	if (serviceTier == "priority" || serviceTier == "fast") && pricing.FastMultiplier == nil {
		if inputPriorityPriceConfigured(pricing) {
			inputPrice = pricing.InputPricePerTokenPriority
		} else {
			inputTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
		if outputPriorityPriceConfigured(pricing) {
			outputPrice = pricing.OutputPricePerTokenPriority
		} else {
			outputTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
		if cacheReadPriorityPriceConfigured(pricing) {
			cacheReadPrice = pricing.CacheReadPricePerTokenPriority
		} else {
			cacheReadTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
		if cacheCreationPriorityPriceConfigured(pricing) {
			cacheCreationPrice = pricing.CacheCreationPricePerTokenPriority
			if pricing.SupportsCacheBreakdown {
				switch {
				case pricing.CacheCreationPricePerToken > 0:
					// The catalog has one priority cache-write price but separate
					// standard 5m/1h prices. Apply the declared priority/base
					// ratio to both cache durations instead of silently using
					// the standard tier for breakdown usage.
					cacheCreationTierMultiplier =
						cacheCreationPrice / pricing.CacheCreationPricePerToken
				case cacheCreationPrice == 0:
					// Explicit zero remains an intentional free priority tier.
					cacheCreationTierMultiplier = 0
				default:
					// Legacy in-memory entries cannot express presence. Preserve
					// the documented whole-tier multiplier rather than settling
					// a positive priority price as standard-price zero.
					cacheCreationTierMultiplier = serviceTierCostMultiplier(serviceTier)
				}
			}
		} else {
			cacheCreationTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
		// The catalog has no separate priority fields for image-token prices.
		// An omitted image price falls back to the already tier-adjusted text
		// price, so it must reuse that text dimension's multiplier. A dedicated
		// image price has not been tier-adjusted and uses the explicit 2x policy.
		imageInputTierMultiplier = inputTierMultiplier
		if pricing.ImageInputPriceExplicit {
			imageInputTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
		imageOutputTierMultiplier = outputTierMultiplier
		if pricing.ImageOutputPriceExplicit {
			imageOutputTierMultiplier = serviceTierCostMultiplier(serviceTier)
		}
	} else {
		tierMultiplier := configuredServiceTierMultiplier(serviceTier, pricing)
		inputTierMultiplier = tierMultiplier
		imageInputTierMultiplier = tierMultiplier
		outputTierMultiplier = tierMultiplier
		imageOutputTierMultiplier = tierMultiplier
		cacheCreationTierMultiplier = tierMultiplier
		cacheReadTierMultiplier = tierMultiplier
	}

	longContextPricingEligible := applyLongCtx && s.shouldApplySessionLongContextPricing(tokens, pricing)
	var baselineCost *CostBreakdown
	if longContextPricingEligible {
		baselineCost = s.computeTokenBreakdown(pricing, tokens, rateMultiplier, serviceTier, false)
		// A missing multiplier means 1, never zero.
		longCtxInputMultiplier := longContextMultiplierOrOne(pricing.LongContextInputMultiplier)
		longCtxOutputMultiplier := longContextMultiplierOrOne(pricing.LongContextOutputMultiplier)
		inputPrice *= longCtxInputMultiplier
		outputPrice *= longCtxOutputMultiplier
		// Dedicated image-token prices do not flow through inputPrice/outputPrice.
		// Apply the corresponding long-context multiplier exactly once here.
		// Omitted image prices fall back to the already-adjusted text price below
		// and therefore must not receive this extra multiplier.
		if pricing.ImageInputPriceExplicit {
			imageInputTierMultiplier *= longCtxInputMultiplier
		}
		if pricing.ImageOutputPriceExplicit {
			imageOutputTierMultiplier *= longCtxOutputMultiplier
		}
		// 缓存读取本质上是输入侧的复用，应与 input 一同应用长上下文倍率；
		// 否则 cache hit 越多，少计的费用越多（见 #2293）。
		cacheReadPrice *= longCtxInputMultiplier
		// 缓存创建（cache_write）也是输入侧操作，三档价格（标准 / 5m / 1h）
		// 都通过 computeCacheCreationCost 直接读取 pricing.*，不会经过这里
		// 的倍率修改，因此显式向下传一个倍率，避免长上下文场景下被漏乘。
		cacheCreationMultiplier = longCtxInputMultiplier
	}

	bd := &CostBreakdown{}
	// 分离图片输入 token 与文本输入 token（多模态 embedding、图片编辑等图文不同价场景）。
	// InputCost 仅计文本输入，图片输入费用单独记入 ImageInputCost，便于对账；总额不变。
	// ImageInputTokens 为 0 时（绝大多数 chat/vision 流量）走原始单价路径，行为不变。
	if tokens.ImageInputTokens > 0 {
		imageInputTokens := tokens.ImageInputTokens
		textInputTokens := tokens.InputTokens - imageInputTokens
		if textInputTokens < 0 {
			textInputTokens = 0
			imageInputTokens = tokens.InputTokens
		}
		imageInputPrice := pricing.ImageInputPricePerToken
		if imageInputPrice == 0 && !pricing.ImageInputPriceExplicit {
			// 未配置图片输入档时回退到文本 input 价（已含 priority / 长上下文调整）
			imageInputPrice = inputPrice
		}
		bd.InputCost = float64(textInputTokens) * inputPrice
		bd.ImageInputCost = float64(imageInputTokens) * imageInputPrice
	} else {
		bd.InputCost = float64(tokens.InputTokens) * inputPrice
	}

	// 分离图片输出 token 与文本输出 token
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	if textOutputTokens < 0 {
		textOutputTokens = 0
	}
	bd.OutputCost = float64(textOutputTokens) * outputPrice

	// 图片输出 token 费用（独立费率）
	if tokens.ImageOutputTokens > 0 {
		imgPrice := pricing.ImageOutputPricePerToken
		if imgPrice == 0 && !pricing.ImageOutputPriceExplicit {
			imgPrice = outputPrice
		}
		bd.ImageOutputCost = float64(tokens.ImageOutputTokens) * imgPrice
	}

	// 缓存创建费用
	bd.CacheCreationCost = s.computeCacheCreationCost(pricing, tokens, cacheCreationPrice, cacheCreationMultiplier)

	bd.CacheReadCost = float64(tokens.CacheReadTokens) * cacheReadPrice

	bd.InputCost *= inputTierMultiplier
	bd.ImageInputCost *= imageInputTierMultiplier
	bd.OutputCost *= outputTierMultiplier
	bd.ImageOutputCost *= imageOutputTierMultiplier
	bd.CacheCreationCost *= cacheCreationTierMultiplier
	bd.CacheReadCost *= cacheReadTierMultiplier

	bd.TotalCost = bd.InputCost + bd.ImageInputCost + bd.OutputCost + bd.ImageOutputCost +
		bd.CacheCreationCost + bd.CacheReadCost
	bd.ActualCost = bd.TotalCost * rateMultiplier
	bd.LongContextBillingApplied = baselineCost != nil && bd.ActualCost > baselineCost.ActualCost

	return bd
}

// computeCacheCreationCost 计算缓存创建费用（支持 5m/1h 分类或标准计费）。
// multiplier 用于长上下文等场景下的整体价格缩放（普通调用传 1.0 即可）。
func (s *BillingService) computeCacheCreationCost(pricing *ModelPricing, tokens UsageTokens, price, multiplier float64) float64 {
	totalTokens := tokens.CacheCreationTokens
	reportedClassifiedTokens := tokens.CacheCreation5mTokens + tokens.CacheCreation1hTokens
	if totalTokens <= 0 && reportedClassifiedTokens > 0 {
		// Some providers return only the ephemeral duration breakdown.
		totalTokens = reportedClassifiedTokens
	}

	if pricing.SupportsCacheBreakdown && (pricing.CacheCreation5mPrice > 0 || pricing.CacheCreation1hPrice > 0) {
		cacheCreation5mTokens, cacheCreation1hTokens := normalizeCacheCreationBreakdown(tokens)
		if cacheCreation5mTokens == 0 && cacheCreation1hTokens == 0 && tokens.CacheCreationTokens > 0 {
			// API 未返回 ephemeral 明细，回退到全部按 5m 单价计费
			return float64(totalTokens) * pricing.CacheCreation5mPrice * multiplier
		}
		// If the aggregate exceeds the normalized detail, conservatively price
		// the unclassified remainder at the 5m/base cache-write tier. Never
		// subtract when a provider reports detail greater than its aggregate.
		unclassifiedTokens := totalTokens - cacheCreation5mTokens - cacheCreation1hTokens
		if unclassifiedTokens < 0 {
			unclassifiedTokens = 0
		}
		return float64(cacheCreation5mTokens+unclassifiedTokens)*pricing.CacheCreation5mPrice*multiplier +
			float64(cacheCreation1hTokens)*pricing.CacheCreation1hPrice*multiplier
	}
	return float64(totalTokens) * price * multiplier
}

// normalizeCacheCreationBreakdown caps contradictory 5m/1h details at an explicitly
// positive aggregate while retaining their reported ratio as closely as integer tokens allow.
func normalizeCacheCreationBreakdown(tokens UsageTokens) (int, int) {
	cacheCreation5mTokens := tokens.CacheCreation5mTokens
	cacheCreation1hTokens := tokens.CacheCreation1hTokens
	aggregate := tokens.CacheCreationTokens
	if cacheCreation5mTokens < 0 {
		cacheCreation5mTokens = 0
	}
	if cacheCreation1hTokens < 0 {
		cacheCreation1hTokens = 0
	}
	if aggregate <= 0 || (cacheCreation5mTokens <= aggregate && cacheCreation1hTokens <= aggregate-cacheCreation5mTokens) {
		return cacheCreation5mTokens, cacheCreation1hTokens
	}

	detailTotal := float64(cacheCreation5mTokens) + float64(cacheCreation1hTokens)
	normalized5mTokens := math.Round(float64(aggregate) * float64(cacheCreation5mTokens) / detailTotal)
	if normalized5mTokens >= float64(aggregate) {
		cacheCreation5mTokens = aggregate
	} else {
		cacheCreation5mTokens = int(normalized5mTokens)
	}
	return cacheCreation5mTokens, aggregate - cacheCreation5mTokens
}

// calculatePerRequestCost 按次/图片计费
func (s *BillingService) calculatePerRequestCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	units := input.UsageUnits
	if units <= 0 {
		count := input.RequestCount
		if count <= 0 {
			count = 1
		}
		units = float64(count)
	}

	var (
		unitPrice float64
		found     bool
	)
	if input.SizeTier != "" {
		unitPrice, found = input.Resolver.LookupRequestTierPrice(resolved, input.SizeTier)
	} else {
		totalContext := input.Tokens.InputTokens + input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens
		unitPrice, found = input.Resolver.LookupRequestTierPriceByContext(resolved, totalContext)
	}

	// 仅在层级未命中时回退默认价。显式 0 是有效免费配置，不能继续 fallback。
	if !found && resolved.DefaultPerRequestPriceSet {
		unitPrice = resolved.DefaultPerRequestPrice
		found = true
	}
	if !found {
		return nil, fmt.Errorf(
			"%w for model=%s billing_mode=%s size_tier=%q",
			ErrModelPricingUnavailable,
			input.Model,
			resolved.Mode,
			input.SizeTier,
		)
	}
	if !isFiniteNonNegativePrice(unitPrice) {
		return nil, fmt.Errorf(
			"%w: invalid per-request price for model=%s billing_mode=%s size_tier=%q",
			ErrModelPricingUnavailable,
			input.Model,
			resolved.Mode,
			input.SizeTier,
		)
	}

	totalCost := unitPrice * units
	actualCost := totalCost * input.RateMultiplier

	return &CostBreakdown{
		TotalCost:  totalCost,
		ActualCost: actualCost,
	}, nil
}

// CalculateCost 计算使用费用
func (s *BillingService) CalculateCost(model string, tokens UsageTokens, rateMultiplier float64) (*CostBreakdown, error) {
	return s.calculateCostInternal(model, tokens, rateMultiplier, "", nil)
}

func (s *BillingService) CalculateCostWithServiceTier(model string, tokens UsageTokens, rateMultiplier float64, serviceTier string) (*CostBreakdown, error) {
	return s.calculateCostInternal(model, tokens, rateMultiplier, serviceTier, nil)
}

func (s *BillingService) CalculateCostWithServiceTierForPlatform(platform, model string, tokens UsageTokens, rateMultiplier float64, serviceTier string) (*CostBreakdown, error) {
	return s.calculateCostInternalForPlatform(platform, model, tokens, rateMultiplier, serviceTier, nil, true)
}

func (s *BillingService) calculateCostInternal(model string, tokens UsageTokens, rateMultiplier float64, serviceTier string, channelPricing *ChannelModelPricing) (*CostBreakdown, error) {
	return s.calculateCostInternalForPlatform("", model, tokens, rateMultiplier, serviceTier, channelPricing, true)
}

func (s *BillingService) calculateCostInternalForPlatform(
	platform string,
	model string,
	tokens UsageTokens,
	rateMultiplier float64,
	serviceTier string,
	channelPricing *ChannelModelPricing,
	longContextBillingEnabled bool,
) (*CostBreakdown, error) {
	var pricing *ModelPricing
	var err error
	if channelPricing != nil {
		pricing, err = s.GetModelPricingWithChannelForPlatform(platform, model, channelPricing)
	} else {
		pricing, err = s.GetModelPricingForPlatform(platform, model)
	}
	if err != nil {
		return nil, err
	}
	return s.computeTokenBreakdownValidated(
		model,
		pricing,
		tokens,
		rateMultiplier,
		serviceTier,
		longContextBillingEnabled,
	)
}

// applyModelSpecificPricingPolicy 对目录数据做模型特定修正：DeepSeek 官方价
// 强制覆盖；GPT-5.6 缺 cache_write 价时按官方规则补 1.25 倍输入价；Fast/priority
// 档按业务倍率改写（本地/远程目录的 priority 价可能沿用官方旧口径）。长上下文
// 阶梯不在此处补齐：一律由目录数据（above_XXXk 折算或显式 long_context_* 字段）
// 驱动。默认强制 DeepSeek 官方价——该路径仅被默认价卡（GetModelPricing 内部）
// 调用；分组/渠道自定义定价路径用带参数的 applyModelSpecificPricingPolicyEx
// 关闭强制，保留运营者配置。
func (s *BillingService) applyModelSpecificPricingPolicy(model string, pricing *ModelPricing) *ModelPricing {
	return s.applyModelSpecificPricingPolicyEx(model, pricing, true)
}

// applyModelSpecificPricingPolicyEx 与 applyModelSpecificPricingPolicy 相同，
// 但由调用方控制是否强制 DeepSeek 官方价（forceDeepSeekRates）。
// calculateTokenCost 对分组/渠道自定义定价（Source 非 LiteLLM）传 false：
// 强制覆盖会把运营者配置的售价盖回官方价，违反自定义定价语义。
func (s *BillingService) applyModelSpecificPricingPolicyEx(model string, pricing *ModelPricing, forceDeepSeekRates bool) *ModelPricing {
	if pricing == nil {
		return nil
	}
	// DeepSeek 模型：无论 JSON/远端价格表给什么价，一律强制官方低谷价
	// （2026-08-23 起生效）。这是覆盖远端旧价的关键——远端仓库不可改，生产会先
	// 拉到旧价，必须在此兜底修正；克隆后再覆盖，避免污染共享 fallbackPrices 指针。
	// 档位判定只接受已知 V4 Pro/Flash SKU；未知 deepseek-* 不进行通配改价，
	// 由精确目录/运营配置决定，缺价时继续 fail closed。
	if forceDeepSeekRates && !pricing.OperatorOverride && isDeepSeekModel(model) {
		cloned := *pricing
		normalized := strings.ToLower(strings.TrimSpace(model))
		switch {
		case strings.Contains(normalized, "deepseek-v4-pro"):
			cloned.InputPricePerToken = deepseekProOffPeakInputPrice
			cloned.OutputPricePerToken = deepseekProOffPeakOutputPrice
			cloned.CacheReadPricePerToken = deepseekProOffPeakCacheRead
		case strings.Contains(normalized, "deepseek-v4-flash"):
			cloned.InputPricePerToken = deepseekFlashOffPeakInputPrice
			cloned.OutputPricePerToken = deepseekFlashOffPeakOutputPrice
			cloned.CacheReadPricePerToken = deepseekFlashOffPeakCacheRead
		default:
			return pricing
		}
		cloned.OfficialTimePricing = true
		cloned.OfficialTimeBaseIsOffPeak = true
		return &cloned
	}
	normalized := normalizeKnownOpenAICodexModel(model)
	isGPT56 := isOpenAIGPT56Model(normalized)
	needsCacheCreationPolicy := isGPT56 && !pricing.CacheCreationPriceExplicit && (pricing.CacheCreationPricePerToken <= 0 ||
		(pricing.InputPricePerTokenPriority > 0 && pricing.CacheCreationPricePerTokenPriority <= 0))
	fastRatio := openAIModelFastPricingRatio(normalized)
	if !needsCacheCreationPolicy && fastRatio <= 0 {
		return pricing
	}
	cloned := *pricing
	if isGPT56 && !cloned.CacheCreationPriceExplicit {
		if cloned.CacheCreationPricePerToken <= 0 {
			cloned.CacheCreationPricePerToken = cloned.InputPricePerToken * 1.25
		}
		if cloned.CacheCreationPricePerTokenPriority <= 0 {
			cloned.CacheCreationPricePerTokenPriority = cloned.InputPricePerTokenPriority * 1.25
		}
	}
	if fastRatio > 0 {
		enforceOpenAIFastPricingRatio(&cloned, fastRatio)
	}
	return &cloned
}

// openAIModelFastPricingRatio 返回业务口径下 OpenAI GPT-5.x 模型 Fast/priority
// 的标准价倍率：gpt-5.6 系列与 gpt-5.4 为 2x，gpt-5.5 为 2.5x。未定义 Fast
// 档的模型（如 gpt-5.5-pro、gpt-5.4-mini/nano）返回 0。
func openAIModelFastPricingRatio(normalized string) float64 {
	switch normalized {
	case "gpt-5.4", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna":
		return 2.0
	case "gpt-5.5":
		return 2.5
	default:
		return 0
	}
}

// enforceOpenAIFastPricingRatio 把 priority 档价格改写为「标准价 × ratio」。
// 本地/远程 LiteLLM 目录可能只带官方旧口径（如 gpt-5.5 priority 仍标 2x），
// 直接采用会导致 Fast 模式少计费；这里按业务倍率兜底修正，且对已正确的
// fallback 条目（2x/2.5x）是幂等的。computeTokenBreakdown 在 priority 价格
// 存在时走显式档位价、不再叠加通用 tier 倍率，因此不会重复乘价。
func enforceOpenAIFastPricingRatio(pricing *ModelPricing, ratio float64) {
	if pricing == nil || ratio <= 0 {
		return
	}
	pricing.InputPricePerTokenPriority = pricing.InputPricePerToken * ratio
	pricing.OutputPricePerTokenPriority = pricing.OutputPricePerToken * ratio
	if pricing.CacheReadPricePerToken > 0 {
		pricing.CacheReadPricePerTokenPriority = pricing.CacheReadPricePerToken * ratio
	}
	if pricing.CacheCreationPricePerToken > 0 {
		pricing.CacheCreationPricePerTokenPriority = pricing.CacheCreationPricePerToken * ratio
	}
}

// longContextMultiplierOrOne 把未配置（≤0）的长上下文倍率归一为 1。
func longContextMultiplierOrOne(m float64) float64 {
	if m <= 0 {
		return 1
	}
	return m
}

func (s *BillingService) shouldApplySessionLongContextPricing(tokens UsageTokens, pricing *ModelPricing) bool {
	if pricing == nil || pricing.LongContextInputThreshold <= 0 {
		return false
	}
	if pricing.LongContextInputMultiplier <= 1 && pricing.LongContextOutputMultiplier <= 1 {
		return false
	}
	totalInputTokens := tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
	if pricing.LongContextThresholdInclusive {
		return totalInputTokens >= pricing.LongContextInputThreshold
	}
	return totalInputTokens > pricing.LongContextInputThreshold
}

// CalculateCostWithConfig 使用配置中的默认倍率计算费用
func (s *BillingService) CalculateCostWithConfig(model string, tokens UsageTokens) (*CostBreakdown, error) {
	multiplier := s.cfg.Default.RateMultiplier
	if multiplier <= 0 {
		multiplier = 1.0
	}
	return s.CalculateCost(model, tokens, multiplier)
}

// ListSupportedModels 列出所有支持的模型（现在总是返回true，因为有模糊匹配）
func (s *BillingService) ListSupportedModels() []string {
	models := make([]string, 0)
	// 返回回退价格支持的模型系列
	for model := range s.fallbackPrices {
		models = append(models, model)
	}
	return models
}

// IsModelSupported 检查模型是否支持（现在总是返回true，因为有模糊匹配回退）
func (s *BillingService) IsModelSupported(model string) bool {
	// 所有Claude模型都有回退价格支持
	modelLower := strings.ToLower(model)
	return strings.Contains(modelLower, "claude") ||
		strings.Contains(modelLower, "opus") ||
		strings.Contains(modelLower, "sonnet") ||
		strings.Contains(modelLower, "haiku") ||
		normalizeGLMBillingModel(modelLower) != ""
}

// GetEstimatedCost 估算费用（用于前端展示）
func (s *BillingService) GetEstimatedCost(model string, estimatedInputTokens, estimatedOutputTokens int) (float64, error) {
	tokens := UsageTokens{
		InputTokens:  estimatedInputTokens,
		OutputTokens: estimatedOutputTokens,
	}

	breakdown, err := s.CalculateCostWithConfig(model, tokens)
	if err != nil {
		return 0, err
	}

	return breakdown.ActualCost, nil
}

// GetPricingServiceStatus 获取价格服务状态
func (s *BillingService) GetPricingServiceStatus() map[string]any {
	if s.pricingService != nil {
		return s.pricingService.GetStatus()
	}
	return map[string]any{
		"model_count":  len(s.fallbackPrices),
		"last_updated": "using fallback",
		"local_hash":   "N/A",
	}
}

// ForceUpdatePricing 强制更新价格数据
func (s *BillingService) ForceUpdatePricing() error {
	if s.pricingService != nil {
		return s.pricingService.ForceUpdate()
	}
	return fmt.Errorf("pricing service not initialized")
}

// ImagePriceConfig 图片计费配置
type ImagePriceConfig struct {
	Price1K *float64 // 1K 尺寸价格（nil 表示使用默认值）
	Price2K *float64 // 2K 尺寸价格（nil 表示使用默认值）
	Price4K *float64 // 4K 尺寸价格（nil 表示使用默认值）
}

// VideoPriceConfig 视频生成计费配置。所有价格均为**每秒**单价（USD/s），与 xAI 官方计费口径一致。
type VideoPriceConfig struct {
	Price480P  *float64 // 480p 每秒价格（nil 表示使用默认值）
	Price720P  *float64 // 720p 每秒价格（nil 表示使用默认值）
	Price1080P *float64 // 1080p 每秒价格（nil 表示使用默认值）
	// ModelPrices is optional per-model-family override: family → resolution → USD/s.
	// When set for a model, it wins over Price* flat columns for that model only.
	ModelPrices map[string]map[string]float64
}

const (
	defaultImageGenerationPrice = 0.134

	defaultGrokImagineImagePrice1K        = 0.02
	defaultGrokImagineImagePrice2K        = 0.02
	defaultGrokImagineImageQualityPrice1K = 0.05
	defaultGrokImagineImageQualityPrice2K = 0.07
	defaultGrokImagineImage20Price1K      = 0.06 // default quality is Medium
	defaultGrokImagineImage20Price2K      = 0.08

	// 视频默认价为 xAI 官方**每秒**输出价格（USD/s），总价 = 每秒价 × 时长（秒）。
	defaultGrokImagineVideoPrice480P    = 0.05
	defaultGrokImagineVideoPrice720P    = 0.07
	defaultGrokImagineVideo15Price480P  = 0.08
	defaultGrokImagineVideo15Price720P  = 0.14
	defaultGrokImagineVideo15Price1080P = 0.25

	// Codex alpha/search 网页搜索单次默认价：OpenAI 官方 web search 定价 $10/1000 次。
	defaultWebSearchPricePerCall = 0.01

	// xAI server-side web/X search and code execution are $5/1000 calls.
	defaultSearchPricePer1k = 5.0

	// Generic realtime defaults to think-fast-1.0; think-fast-2.0 can be
	// configured independently through per-model group/channel pricing.
	defaultAudioRealtimePricePerMin     = 0.05
	defaultAudioTTSPricePerMillionChars = 15.0
	defaultAudioSTTPricePerHour         = 0.10
)

// CalculateWebSearchCost 计算 Codex alpha/search 网页搜索按次费用。
// callCount: 搜索调用次数（每次请求为 1）
// groupPrice: 分组配置的单次价格（nil 表示使用默认价 0.01；0 表示免费）
// rateMultiplier: 分组费率倍数
func (s *BillingService) CalculateWebSearchCost(callCount int, groupPrice *float64, rateMultiplier float64) *CostBreakdown {
	if callCount <= 0 {
		return &CostBreakdown{}
	}
	unitPrice := defaultWebSearchPricePerCall
	if groupPrice != nil && isFiniteNonNegativePrice(*groupPrice) {
		unitPrice = *groupPrice
	}
	totalCost := unitPrice * float64(callCount)

	// 应用倍率（保存时强制 > 0；负数按 0 处理避免按 1x 误扣）
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// CalculateSearchCost bills search/tool invocations (e.g. web_search) per 1k calls.
// groupPricePer1k: nil → defaultSearchPricePer1k; explicit 0 → free; >0 → that rate.
func (s *BillingService) CalculateSearchCost(numCalls int, groupPricePer1k *float64, rateMultiplier float64) *CostBreakdown {
	if numCalls <= 0 {
		return &CostBreakdown{}
	}
	pricePer1k := defaultSearchPricePer1k
	if groupPricePer1k != nil {
		if *groupPricePer1k < 0 {
			return &CostBreakdown{}
		}
		pricePer1k = *groupPricePer1k
	}
	if pricePer1k == 0 {
		return &CostBreakdown{}
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	unit := pricePer1k / 1000.0
	total := unit * float64(numCalls)
	return &CostBreakdown{
		TotalCost:   total,
		ActualCost:  total * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

type audioPriceConfig struct {
	RealtimePerMin *float64
	TTSPerMChars   *float64
	STTPerHour     *float64
}

// CalculateAudioCost supports realtime (per min), tts (per M chars), stt (per hr).
// Missing group prices use defaults; explicit 0 means free for that mode.
func (s *BillingService) CalculateAudioCost(mode string, durationOrUnits float64, groupConfig *audioPriceConfig, rateMultiplier float64) *CostBreakdown {
	if durationOrUnits <= 0 {
		return &CostBreakdown{}
	}
	var unitPrice float64
	switch strings.ToLower(mode) {
	case "realtime":
		unitPrice = defaultAudioRealtimePricePerMin
		if groupConfig != nil && groupConfig.RealtimePerMin != nil {
			unitPrice = *groupConfig.RealtimePerMin
		}
	case "tts":
		unitPrice = defaultAudioTTSPricePerMillionChars
		if groupConfig != nil && groupConfig.TTSPerMChars != nil {
			unitPrice = *groupConfig.TTSPerMChars
		}
	case "stt":
		unitPrice = defaultAudioSTTPricePerHour
		if groupConfig != nil && groupConfig.STTPerHour != nil {
			unitPrice = *groupConfig.STTPerHour
		}
	default:
		return &CostBreakdown{}
	}
	if unitPrice <= 0 {
		return &CostBreakdown{}
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	total := unitPrice * durationOrUnits
	return &CostBreakdown{
		TotalCost:   total,
		ActualCost:  total * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// CalculateImageCost 计算图片生成费用
// model: 请求的模型名称（用于获取配置化模型价格目录默认价格）
// imageSize: 图片尺寸 "1K", "2K", "4K"
// imageCount: 生成的图片数量
// groupConfig: 分组配置的价格（可能为 nil，表示使用默认值）
// rateMultiplier: 费率倍数
func (s *BillingService) CalculateImageCost(model string, imageSize string, imageCount int, groupConfig *ImagePriceConfig, rateMultiplier float64) *CostBreakdown {
	if imageCount <= 0 {
		return &CostBreakdown{}
	}
	imageSize = NormalizeImageBillingTierOrDefault(imageSize)

	// 获取单价
	unitPrice := s.getImageUnitPrice(model, imageSize, groupConfig)

	// 计算总费用
	totalCost := unitPrice * float64(imageCount)

	// 应用倍率（保存时强制 > 0；负数按 0 处理避免按 1x 误扣）
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	actualCost := totalCost * rateMultiplier

	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  actualCost,
		BillingMode: string(BillingModeImage),
	}
}

// CalculateImageCostStrict calculates image cost only when the actual output
// tier has a real price source. Unlike CalculateImageCost it never falls back
// to defaultImageGenerationPrice, which is a generic safety placeholder rather
// than a price for an arbitrary model.
func (s *BillingService) CalculateImageCostStrict(
	model string,
	imageSize string,
	imageCount int,
	groupConfig *ImagePriceConfig,
	rateMultiplier float64,
) (*CostBreakdown, error) {
	return s.CalculateImageCostStrictForPlatforms(nil, model, imageSize, imageCount, groupConfig, rateMultiplier)
}

func (s *BillingService) CalculateImageCostStrictForPlatforms(
	platforms []string,
	model string,
	imageSize string,
	imageCount int,
	groupConfig *ImagePriceConfig,
	rateMultiplier float64,
) (*CostBreakdown, error) {
	if imageCount <= 0 {
		return &CostBreakdown{}, nil
	}
	var err error
	imageSize, err = NormalizeImageBillingTierStrictOrDefault(imageSize)
	if err != nil {
		return nil, err
	}
	unitPrice, ok := s.strictImageUnitPriceForPlatforms(platforms, model, imageSize, groupConfig)
	if !ok {
		return nil, fmt.Errorf(
			"%w for image model %q tier %q",
			ErrModelPricingUnavailable,
			strings.TrimSpace(model),
			imageSize,
		)
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	totalCost := unitPrice * float64(imageCount)
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModeImage),
	}, nil
}

// CalculateVideoCost 计算视频生成费用（按秒计费，与 xAI 口径一致）。
// model: 请求的模型名称（用于获取默认价格）
// resolution: 视频分辨率 "480p", "720p", "1080p"
// videoCount: 生成的视频数量
// durationSeconds: 单个视频时长（秒），<=0 时按上游默认时长计
// groupConfig: 分组配置的每秒价格（可能为 nil，表示使用默认值）
// rateMultiplier: 费率倍数
func (s *BillingService) CalculateVideoCost(model string, resolution string, videoCount int, durationSeconds int, groupConfig *VideoPriceConfig, rateMultiplier float64) *CostBreakdown {
	if videoCount <= 0 {
		return &CostBreakdown{}
	}
	resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)

	perSecondPrice := s.getVideoUnitPrice(model, resolution, groupConfig)
	totalCost := perSecondPrice * float64(durationSeconds) * float64(videoCount)

	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	actualCost := totalCost * rateMultiplier

	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  actualCost,
		BillingMode: string(BillingModeVideo),
	}
}

// CalculateVideoCostStrict is the fail-loud settlement counterpart of the
// media admission guard. It requires a real price for the actual resolution
// and never turns an unknown video SKU into the generic image placeholder.
func (s *BillingService) CalculateVideoCostStrict(
	model string,
	resolution string,
	videoCount int,
	durationSeconds int,
	groupConfig *VideoPriceConfig,
	rateMultiplier float64,
) (*CostBreakdown, error) {
	if videoCount <= 0 {
		return &CostBreakdown{}, nil
	}
	normalizedResolution, err := NormalizeVideoBillingResolutionStrictOrDefault(resolution)
	if err != nil {
		return nil, err
	}
	resolution = normalizedResolution
	durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)
	perSecondPrice, ok := s.strictVideoUnitPrice(model, resolution, groupConfig)
	if !ok {
		return nil, fmt.Errorf(
			"%w for video model %q tier %q",
			ErrModelPricingUnavailable,
			strings.TrimSpace(model),
			resolution,
		)
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	totalCost := perSecondPrice * float64(durationSeconds) * float64(videoCount)
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModeVideo),
	}, nil
}

// strictImageUnitPriceForPlatforms resolves only explicit group pricing, exact hard-coded
// SKUs, or an exact catalog entry. The bool distinguishes an explicitly
// configured zero price from a missing price.
func (s *BillingService) strictImageUnitPriceForPlatforms(
	platforms []string,
	model string,
	imageSize string,
	groupConfig *ImagePriceConfig,
) (float64, bool) {
	var err error
	imageSize, err = NormalizeImageBillingTierStrictOrDefault(imageSize)
	if err != nil {
		return 0, false
	}
	if groupConfig != nil {
		var configured *float64
		switch imageSize {
		case ImageBillingSize1K:
			configured = groupConfig.Price1K
		case ImageBillingSize2K:
			configured = groupConfig.Price2K
		case ImageBillingSize4K:
			configured = groupConfig.Price4K
		}
		if validConfiguredPrice(configured) {
			return *configured, true
		}
	}

	if price, ok := getDefaultGrokImagineImagePrice(model, imageSize); ok {
		return price, true
	}
	basePrice, ok := s.strictCatalogMediaBasePriceForPlatforms(platforms, model)
	if !ok {
		return 0, false
	}
	switch imageSize {
	case ImageBillingSize2K:
		return basePrice * 1.5, true
	case ImageBillingSize4K:
		return basePrice * 2, true
	default:
		return basePrice, true
	}
}

func (s *BillingService) strictVideoUnitPrice(
	model string,
	resolution string,
	groupConfig *VideoPriceConfig,
) (float64, bool) {
	normalizedResolution, err := NormalizeVideoBillingResolutionStrictOrDefault(resolution)
	if err != nil {
		return 0, false
	}
	resolution = normalizedResolution
	if groupConfig != nil {
		var configured *float64
		switch resolution {
		case VideoBillingResolution480P:
			configured = groupConfig.Price480P
		case VideoBillingResolution720P:
			configured = groupConfig.Price720P
		case VideoBillingResolution1080P:
			configured = groupConfig.Price1080P
		}
		if validConfiguredPrice(configured) {
			return *configured, true
		}
	}

	if price, ok := getDefaultGrokImagineVideoPrice(model, resolution); ok {
		return price, true
	}
	// The bundled catalog exposes output_cost_per_image but no video-per-second
	// field. Treating an image price as a video price is an inference, not a
	// real source. Video therefore requires an explicit group/channel tier or
	// an exact hard-coded video SKU.
	return 0, false
}

func (s *BillingService) strictCatalogMediaBasePriceForPlatforms(platforms []string, model string) (float64, bool) {
	if s == nil || s.pricingService == nil {
		return 0, false
	}
	pricing := s.pricingService.LookupModelPricingStrictForPlatforms(platforms, strings.TrimSpace(model))
	if pricing == nil || !isFiniteNonNegativePrice(pricing.OutputCostPerImage) ||
		(pricing.OutputCostPerImage == 0 && !pricing.OutputCostPerImageExplicit) {
		return 0, false
	}
	return pricing.OutputCostPerImage, true
}

// getImageUnitPrice 获取图片单价
func (s *BillingService) getImageUnitPrice(model string, imageSize string, groupConfig *ImagePriceConfig) float64 {
	// 优先使用分组配置的价格
	if groupConfig != nil {
		switch imageSize {
		case "1K":
			if groupConfig.Price1K != nil && isFiniteNonNegativePrice(*groupConfig.Price1K) {
				return *groupConfig.Price1K
			}
		case "2K":
			if groupConfig.Price2K != nil && isFiniteNonNegativePrice(*groupConfig.Price2K) {
				return *groupConfig.Price2K
			}
		case "4K":
			if groupConfig.Price4K != nil && isFiniteNonNegativePrice(*groupConfig.Price4K) {
				return *groupConfig.Price4K
			}
		}
	}

	// 回退到配置化模型价格目录默认价格
	return s.getDefaultImagePrice(model, imageSize)
}

func (s *BillingService) getVideoUnitPrice(model string, resolution string, groupConfig *VideoPriceConfig) float64 {
	// Order: (a) per-model map (b) flat group video_price_* (c) model-aware code defaults.
	if groupConfig != nil {
		if price := LookupVideoModelPrice(groupConfig.ModelPrices, model, resolution); price != nil {
			return *price
		}
		switch NormalizeVideoBillingResolutionOrDefault(resolution) {
		case VideoBillingResolution480P:
			if groupConfig.Price480P != nil && isFiniteNonNegativePrice(*groupConfig.Price480P) {
				return *groupConfig.Price480P
			}
		case VideoBillingResolution720P:
			if groupConfig.Price720P != nil && isFiniteNonNegativePrice(*groupConfig.Price720P) {
				return *groupConfig.Price720P
			}
		case VideoBillingResolution1080P:
			if groupConfig.Price1080P != nil && isFiniteNonNegativePrice(*groupConfig.Price1080P) {
				return *groupConfig.Price1080P
			}
		}
	}

	return s.getDefaultVideoPrice(model, resolution)
}

// getDefaultImagePrice 获取配置化模型价格目录默认图片价格。
func (s *BillingService) getDefaultImagePrice(model string, imageSize string) float64 {
	if price, ok := getDefaultGrokImagineImagePrice(model, imageSize); ok {
		return price
	}

	basePrice := 0.0
	priceConfigured := false

	// 从 PricingService 获取 output_cost_per_image
	if s.pricingService != nil {
		pricing := s.pricingService.GetModelPricing(model)
		if pricing != nil && isFiniteNonNegativePrice(pricing.OutputCostPerImage) &&
			(pricing.OutputCostPerImage > 0 || pricing.OutputCostPerImageExplicit) {
			basePrice = pricing.OutputCostPerImage
			priceConfigured = true
		}
	}

	// 如果没有找到价格，使用硬编码默认值（$0.134，来自 gemini-3-pro-image-preview）
	if !priceConfigured {
		basePrice = defaultImageGenerationPrice
	}

	// 2K 尺寸 1.5 倍，4K 尺寸翻倍
	if imageSize == "2K" {
		return basePrice * 1.5
	}
	if imageSize == "4K" {
		return basePrice * 2
	}

	return basePrice
}

func (s *BillingService) getDefaultVideoPrice(model string, resolution string) float64 {
	if price, ok := getDefaultGrokImagineVideoPrice(model, resolution); ok {
		return price
	}

	// The bundled LiteLLM schema does not expose an output video generation price.
	// Keep the historical model default as the fallback (interpreted as a per-second
	// rate; today only Grok models reach video billing, so this path is a safety net),
	// while letting group-level video prices override it independently from image prices.
	return s.getDefaultImagePrice(model, ImageBillingSize2K)
}

func getDefaultGrokImagineImagePrice(model string, imageSize string) (float64, bool) {
	tier, err := NormalizeImageBillingTierStrictOrDefault(imageSize)
	if err != nil {
		return 0, false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	switch model {
	case "grok-imagine-image-2.0":
		return getGrokImagineImageTierPrice(
			imageSize,
			defaultGrokImagineImage20Price1K,
			defaultGrokImagineImage20Price2K,
		), true
	case "grok-imagine-image-quality":
		switch tier {
		case ImageBillingSize1K:
			return defaultGrokImagineImageQualityPrice1K, true
		case ImageBillingSize2K:
			return defaultGrokImagineImageQualityPrice2K, true
		}
	case "grok-imagine", "grok-imagine-image", "grok-imagine-edit":
		switch tier {
		case ImageBillingSize1K:
			return defaultGrokImagineImagePrice1K, true
		case ImageBillingSize2K:
			return defaultGrokImagineImagePrice2K, true
		}
	}
	return 0, false
}

func getGrokImagineImageTierPrice(imageSize string, price1K float64, price2K float64) float64 {
	switch NormalizeImageBillingTierOrDefault(imageSize) {
	case ImageBillingSize1K:
		return price1K
	case ImageBillingSize2K, ImageBillingSize4K:
		return price2K
	default:
		return price2K
	}
}

func getDefaultGrokImagineVideoPrice(model string, resolution string) (float64, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	switch model {
	case "grok-imagine-video-1.5":
		switch NormalizeVideoBillingResolutionOrDefault(resolution) {
		case VideoBillingResolution480P:
			return defaultGrokImagineVideo15Price480P, true
		case VideoBillingResolution720P:
			return defaultGrokImagineVideo15Price720P, true
		case VideoBillingResolution1080P:
			return defaultGrokImagineVideo15Price1080P, true
		default:
			return defaultGrokImagineVideo15Price480P, true
		}
	case "grok-imagine-video":
		switch NormalizeVideoBillingResolutionOrDefault(resolution) {
		case VideoBillingResolution480P:
			return defaultGrokImagineVideoPrice480P, true
		case VideoBillingResolution720P, VideoBillingResolution1080P:
			return defaultGrokImagineVideoPrice720P, true
		default:
			return defaultGrokImagineVideoPrice480P, true
		}
	default:
		return 0, false
	}
}
