package handler

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

const (
	// publicRecentCallsWindow 公开模型广场的调用量统计窗口。
	publicRecentCallsWindow = 7 * 24 * time.Hour
	// publicPlazaResponseCacheTTL 匿名广场响应的进程内缓存时长，与响应头
	// Cache-Control: max-age=60 对齐：客户端可接受的陈旧度，服务端同样接受。
	publicPlazaResponseCacheTTL = 60 * time.Second
	// publicRecentCallsCacheTTL 近 7 天调用量聚合（usage_logs 上的 GROUP BY）的缓存时长；
	// 匿名与登录态请求共享，登录态响应本身不缓存，但不必每次都重跑聚合。
	publicRecentCallsCacheTTL = 5 * time.Minute
	// publicPlazaAnonymousCacheKey 匿名响应缓存键。ListPublic 不读取任何查询参数、
	// 语言或用户信息，匿名输出对所有请求相同；若日后引入影响输出的参数，须并入该键。
	publicPlazaAnonymousCacheKey = "anonymous"
)

// publicPlazaCache 缓存匿名模型广场的最终响应体。匿名请求无需认证、输出与用户无关，
// 每次重算都要跑渠道/分组/目录/定价/调用量五套查询，因此用 singleflight 合并并发
// miss，并在 TTL 内直接复用；登录态响应含用户专属分组与倍率，不经此缓存。
type publicPlazaCache struct {
	now     func() time.Time
	sf      singleflight.Group
	mu      sync.Mutex
	entries map[string]publicPlazaCacheEntry
}

type publicPlazaCacheEntry struct {
	out       []userAvailableChannel
	expiresAt time.Time
}

func newPublicPlazaCache(now func() time.Time) *publicPlazaCache {
	if now == nil {
		now = time.Now
	}
	return &publicPlazaCache{now: now, entries: make(map[string]publicPlazaCacheEntry)}
}

func (c *publicPlazaCache) lookup(key string) ([]userAvailableChannel, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expiresAt) {
		return nil, false
	}
	return entry.out, true
}

// get 返回 key 对应的缓存响应；未命中时由 build 生成，并发 miss 只执行一次 build。
// c 为 nil 时不缓存（直接构建），便于直接以字面量构造 handler 的测试沿用旧行为。
func (c *publicPlazaCache) get(key string, build func() ([]userAvailableChannel, error)) ([]userAvailableChannel, error) {
	if c == nil {
		return build()
	}
	if out, ok := c.lookup(key); ok {
		return out, nil
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// 排队等待期间前一次 build 可能已经落地，先复查再重算。
		if out, ok := c.lookup(key); ok {
			return out, nil
		}
		out, err := build()
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.entries[key] = publicPlazaCacheEntry{out: out, expiresAt: c.now().Add(publicPlazaResponseCacheTTL)}
		c.mu.Unlock()
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	out, _ := v.([]userAvailableChannel)
	return out, nil
}

// recentCallCountsCache 缓存近 7 天模型调用量聚合结果。窗口起点按 TTL 粒度对齐，
// 同一粒度桶内所有请求共用一次查询；桶切换后重新聚合。
type recentCallCountsCache struct {
	now func() time.Time
	sf  singleflight.Group

	mu        sync.Mutex
	windowKey int64
	counts    map[string]int64
	expiresAt time.Time
}

func newRecentCallCountsCache(now func() time.Time) *recentCallCountsCache {
	if now == nil {
		now = time.Now
	}
	return &recentCallCountsCache{now: now}
}

// window 返回当前请求应使用的聚合窗口起点及其缓存键。
func (c *recentCallCountsCache) window(now time.Time) (time.Time, int64) {
	since := now.UTC().Add(-publicRecentCallsWindow).Truncate(publicRecentCallsCacheTTL)
	return since, since.Unix()
}

func (c *recentCallCountsCache) lookup(windowKey int64) (map[string]int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil || c.windowKey != windowKey || !c.now().Before(c.expiresAt) {
		return nil, false
	}
	return c.counts, true
}

// get 返回窗口内各模型调用量。c 为 nil 时直接查询（不缓存）。
func (c *recentCallCountsCache) get(ctx context.Context, stats publicModelStatsProvider) (map[string]int64, error) {
	if stats == nil {
		return nil, nil
	}
	if c == nil {
		return stats.GetPublicModelRecentCallCounts(ctx, time.Now().UTC().Add(-publicRecentCallsWindow))
	}
	since, windowKey := c.window(c.now())
	if counts, ok := c.lookup(windowKey); ok {
		return counts, nil
	}
	v, err, _ := c.sf.Do("recent_calls", func() (any, error) {
		if counts, ok := c.lookup(windowKey); ok {
			return counts, nil
		}
		// 与请求上下文解耦：一次聚合服务于所有并发等待者，不应被首个请求方的取消拖垮。
		counts, err := stats.GetPublicModelRecentCallCounts(context.WithoutCancel(ctx), since)
		if err != nil {
			return nil, err
		}
		if counts == nil {
			counts = map[string]int64{}
		}
		c.mu.Lock()
		c.windowKey = windowKey
		c.counts = counts
		c.expiresAt = c.now().Add(publicRecentCallsCacheTTL)
		c.mu.Unlock()
		return counts, nil
	})
	if err != nil {
		return nil, err
	}
	counts, _ := v.(map[string]int64)
	return counts, nil
}

// publicModelStatsProvider 提供公开广场排序所需的近期调用量统计。
type publicModelStatsProvider interface {
	GetPublicModelRecentCallCounts(ctx context.Context, since time.Time) (map[string]int64, error)
}

// billingFallbackProvider 提供硬编码 fallback 定价，用于 pricing catalog 未覆盖时
// 让广场展示对齐真实计费口径。
type billingFallbackProvider interface {
	GetFallbackPricing(model string) *service.ModelPricing
}

type availableChannelAPIKeyService interface {
	GetVisibleGroupCatalog(context.Context, int64) ([]service.GroupCatalogEntry, error)
	GetUserGroupRates(context.Context, int64) (map[int64]float64, error)
}

type availableChannelSettingService interface {
	GetAvailableChannelsRuntime(context.Context) service.AvailableChannelsRuntime
	GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime
}

// availableChannelModelCatalog is the narrow catalog surface consumed by the
// authenticated channel view and the anonymous Model Plaza.
type availableChannelModelCatalog interface {
	ListForGroup(context.Context, int64, string) ([]string, error)
}

// AvailableChannelHandler 处理用户侧「可用渠道」查询。
//
// 用户侧接口委托 ChannelService.ListAvailable，并在返回前做四层过滤：
//  1. 行过滤：只保留状态为 Active 且与当前用户可访问分组有交集的渠道；
//  2. 分组过滤：渠道的 Groups 只保留用户可访问的那些；
//  3. 平台过滤：普通分组只保留自身平台模型；Composite 分组按渠道已配置的具体模型平台
//     展开。这样既防止普通分组跨平台泄漏，也让 Composite 正确展示其多平台能力；
//  4. 字段白名单：仅返回用户需要的字段（省略 BillingModelSource / RestrictModels
//     / 内部 ID / Status 等管理字段）。
type AvailableChannelHandler struct {
	channelService  *service.ChannelService
	apiKeyService   availableChannelAPIKeyService
	settingService  availableChannelSettingService
	modelCatalog    availableChannelModelCatalog
	modelStats      publicModelStatsProvider
	billingFallback billingFallbackProvider
	modelPrices     *service.PricingService

	// now 供缓存 TTL 判定使用，测试可注入假时钟；nil 时为 time.Now。
	now func() time.Time
	// publicCache / recentCalls 为 nil 时不缓存（以字面量构造 handler 的测试沿用旧行为）。
	publicCache *publicPlazaCache
	recentCalls *recentCallCountsCache
}

// NewAvailableChannelHandler 创建用户侧可用渠道 handler。
func NewAvailableChannelHandler(
	channelService *service.ChannelService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	modelCatalogService *service.ModelCatalogService,
	modelStats publicModelStatsProvider,
	billingFallback billingFallbackProvider,
	modelPrices *service.PricingService,
) *AvailableChannelHandler {
	var modelCatalog availableChannelModelCatalog
	if modelCatalogService != nil {
		modelCatalog = modelCatalogService
	}
	h := &AvailableChannelHandler{
		channelService:  channelService,
		apiKeyService:   apiKeyService,
		settingService:  settingService,
		modelCatalog:    modelCatalog,
		modelStats:      modelStats,
		billingFallback: billingFallback,
		modelPrices:     modelPrices,
	}
	h.useClock(time.Now)
	return h
}

// useClock 以给定时钟初始化匿名响应缓存与调用量缓存；测试用它注入假时钟。
func (h *AvailableChannelHandler) useClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	h.now = now
	h.publicCache = newPublicPlazaCache(now)
	h.recentCalls = newRecentCallCountsCache(now)
}

// featureEnabled 返回 available-channels 开关是否启用。默认关闭（opt-in）。
func (h *AvailableChannelHandler) featureEnabled(c *gin.Context) bool {
	if h.settingService == nil {
		return false
	}
	return h.settingService.GetAvailableChannelsRuntime(c.Request.Context()).Enabled
}

// userAvailableGroup 用户可见的分组概要（白名单字段）。
//
// 前端据此区分专属 vs 公开分组（IsExclusive）、订阅 vs 标准分组（SubscriptionType，
// 订阅视觉加深），并展示默认倍率与高峰倍率规则；用户专属倍率前端走
// /groups/rates，和 API 密钥页面保持一致。
type userAvailableGroup struct {
	ID                   int64                              `json:"id"`
	Name                 string                             `json:"name"`
	Platform             string                             `json:"platform"`
	SubscriptionType     string                             `json:"subscription_type"`
	RateMultiplier       float64                            `json:"rate_multiplier"`
	PeakRateEnabled      bool                               `json:"peak_rate_enabled"`
	PeakStart            string                             `json:"peak_start"`
	PeakEnd              string                             `json:"peak_end"`
	PeakRateMultiplier   float64                            `json:"peak_rate_multiplier"`
	IsExclusive          bool                               `json:"is_exclusive"`
	VIPOnly              bool                               `json:"vip_only"`
	ImageRateIndependent bool                               `json:"image_rate_independent"`
	ImageRateMultiplier  float64                            `json:"image_rate_multiplier"`
	VideoRateIndependent bool                               `json:"video_rate_independent"`
	VideoRateMultiplier  float64                            `json:"video_rate_multiplier"`
	CanBind              *bool                              `json:"can_bind,omitempty"`
	DenyReason           service.GroupAccessDenyReason      `json:"deny_reason,omitempty"`
	SuggestedAction      service.GroupAccessSuggestedAction `json:"suggested_action,omitempty"`
	ImagePrice1K         *float64                           `json:"-"`
	ImagePrice2K         *float64                           `json:"-"`
	ImagePrice4K         *float64                           `json:"-"`
	VideoPrice480P       *float64                           `json:"-"`
	VideoPrice720P       *float64                           `json:"-"`
	VideoPrice1080P      *float64                           `json:"-"`
	VideoModelPrices     map[string]map[string]float64      `json:"-"`
	ModelsListConfig     service.GroupModelsListConfig      `json:"-"`
}

// userSupportedModelPricing 用户可见的定价字段白名单。
type userSupportedModelPricing struct {
	BillingMode                  string                   `json:"billing_mode"`
	Currency                     string                   `json:"currency"`
	Source                       string                   `json:"source"`
	InputPrice                   *float64                 `json:"input_price"`
	OutputPrice                  *float64                 `json:"output_price"`
	CacheWritePrice              *float64                 `json:"cache_write_price"`
	CacheWrite1hPrice            *float64                 `json:"cache_write_1h_price"`
	CacheReadPrice               *float64                 `json:"cache_read_price"`
	MaxReasoningEffortMultiplier *float64                 `json:"max_reasoning_effort_multiplier,omitempty"`
	ImageInputPrice              *float64                 `json:"image_input_price"`
	ImageOutputPrice             *float64                 `json:"image_output_price"`
	PerRequestPrice              *float64                 `json:"per_request_price"`
	Intervals                    []userPricingIntervalDTO `json:"intervals"`
	// ImageTierPrices 图片按张档位价（1K/2K/4K，美元/张）；VideoTierPrices 视频按秒档位价
	// （480p/720p/1080p，美元/秒）。均为未乘倍率的单价，仅模型广场填充，非媒体模型省略。
	ImageTierPrices []userMediaTierPriceDTO `json:"image_tier_prices,omitempty"`
	VideoTierPrices []userMediaTierPriceDTO `json:"video_tier_prices,omitempty"`
}

// userMediaTierPriceDTO 一档媒体单价。
type userMediaTierPriceDTO struct {
	Tier  string  `json:"tier"`
	Price float64 `json:"price"`
}

// userPricingIntervalDTO 定价区间白名单（去掉内部 ID、SortOrder 等前端不渲染的字段）。
type userPricingIntervalDTO struct {
	MinTokens            int      `json:"min_tokens"`
	MaxTokens            *int     `json:"max_tokens"`
	TierLabel            string   `json:"tier_label,omitempty"`
	InputPrice           *float64 `json:"input_price"`
	OutputPrice          *float64 `json:"output_price"`
	CacheWritePrice      *float64 `json:"cache_write_price"`
	CacheWrite1hPrice    *float64 `json:"cache_write_1h_price"`
	CacheReadPrice       *float64 `json:"cache_read_price"`
	InputMultiplier      *float64 `json:"input_multiplier"`
	OutputMultiplier     *float64 `json:"output_multiplier"`
	CacheWriteMultiplier *float64 `json:"cache_write_multiplier"`
	CacheReadMultiplier  *float64 `json:"cache_read_multiplier"`
	PerRequestPrice      *float64 `json:"per_request_price"`
}

// userSupportedModel 用户可见的支持模型条目。
type userSupportedModel struct {
	Name             string                          `json:"name"`
	Platform         string                          `json:"platform"`
	Pricing          *userSupportedModelPricing      `json:"pricing"`
	RecentCallCount  int64                           `json:"recent_call_count"`
	RecentCallWindow int64                           `json:"recent_call_window_seconds"`
	TimeSchedule     *service.ModelPriceTimeSchedule `json:"time_schedule,omitempty"`

	// channelPricing 保留渠道/全局回退的原始定价，供广场在展示价被目录覆盖后
	// 仍能按真实结算链路推导图片/视频档位价；不序列化。
	channelPricing *service.ChannelModelPricing
}

// userChannelPlatformSection 单渠道内某个平台的子视图：用户可见的分组 + 该平台
// 支持的模型。按 platform 聚合后让前端可以把渠道名作为 row-group 一次渲染，
// 后面的平台行按 sections 顺序铺开。
type userChannelPlatformSection struct {
	Platform        string               `json:"platform"`
	Groups          []userAvailableGroup `json:"groups"`
	SupportedModels []userSupportedModel `json:"supported_models"`
}

// userAvailableChannel 用户可见的渠道条目（白名单字段）。
//
// 每个渠道聚合为一条记录，内嵌 platforms 子数组：每个 section 对应一个平台，
// 包含该平台的 groups 和 supported_models。
type userAvailableChannel struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Platforms   []userChannelPlatformSection `json:"platforms"`
}

// List 列出当前用户可见的「可用渠道」。
// GET /api/v1/channels/available
func (h *AvailableChannelHandler) List(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// Feature 未启用时返回空数组（不暴露渠道信息）。检查放在认证之后，
	// 保持与未开关前的 401 行为一致：未登录先 401，登录后再按开关决定。
	if !h.featureEnabled(c) {
		response.Success(c, []userAvailableChannel{})
		return
	}

	groupCatalog, err := h.apiKeyService.GetVisibleGroupCatalog(
		c.Request.Context(),
		subject.UserID,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	catalogByGroupID := make(map[int64]service.GroupCatalogEntry, len(groupCatalog))
	for i := range groupCatalog {
		catalogByGroupID[groupCatalog[i].ID] = groupCatalog[i]
	}

	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]userAvailableChannel, 0, len(channels))
	groupCatalogs := make(map[int64][]string)
	resolvedGroups := make(map[int64]struct{})
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterUserVisibleGroups(ch.Groups, catalogByGroupID)
		if len(visibleGroups) == 0 {
			continue
		}
		if err := h.resolveGroupCatalogs(c.Request.Context(), visibleGroups, groupCatalogs, resolvedGroups); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		sections := buildPlatformSections(ch, visibleGroups)
		sections = mergeGroupCatalogModels(sections, groupCatalogs)
		if len(sections) == 0 {
			continue
		}
		out = append(out, userAvailableChannel{
			Name:        ch.Name,
			Description: ch.Description,
			Platforms:   sections,
		})
	}

	response.Success(c, out)
}

// ListPublic 列出无需认证即可展示的公开「可用渠道」。
// GET /api/v1/channels/public
func (h *AvailableChannelHandler) ListPublic(c *gin.Context) {
	if h.settingService == nil {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	runtime := h.settingService.GetModelPlazaRuntime(c.Request.Context())
	if !runtime.Enabled {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if runtime.RequireAuth && !authenticated {
		response.Unauthorized(c, "Authentication required")
		return
	}

	c.Header("Vary", "Authorization")
	if !authenticated {
		// 匿名输出与用户无关：走进程内缓存，并发 miss 由 singleflight 合并为一次构建。
		out, err := h.publicCache.get(publicPlazaAnonymousCacheKey, func() ([]userAvailableChannel, error) {
			return h.buildAnonymousPlaza(context.WithoutCancel(c.Request.Context()))
		})
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		response.Success(c, out)
		return
	}

	if h.apiKeyService == nil {
		response.InternalError(c, "Model plaza user catalog is not configured")
		return
	}
	catalog, err := h.apiKeyService.GetVisibleGroupCatalog(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	visibleCatalog := make(map[int64]service.GroupCatalogEntry, len(catalog))
	for i := range catalog {
		visibleCatalog[catalog[i].ID] = catalog[i]
	}
	userRates, err := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
	if err != nil {
		slog.Warn("public plaza: user group rates query failed", "user_id", subject.UserID, "err", err)
		userRates = nil
	}

	out, err := h.buildPlaza(c.Request.Context(), visibleCatalog, userRates)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, out)
}

// buildAnonymousPlaza 构建匿名广场响应（仅公开分组、默认倍率）。
func (h *AvailableChannelHandler) buildAnonymousPlaza(ctx context.Context) ([]userAvailableChannel, error) {
	return h.buildPlaza(ctx, nil, nil)
}

// buildPlaza 构建广场响应：visibleCatalog 为 nil 表示匿名视图，否则按用户可见分组与专属倍率裁剪。
func (h *AvailableChannelHandler) buildPlaza(
	ctx context.Context,
	visibleCatalog map[int64]service.GroupCatalogEntry,
	userRates map[int64]float64,
) ([]userAvailableChannel, error) {
	channels, err := h.channelService.ListPublicAvailable(ctx)
	if err != nil {
		return nil, err
	}

	groupCatalogs := make(map[int64][]string)
	resolvedGroups := make(map[int64]struct{})
	if h.modelCatalog != nil {
		for _, ch := range channels {
			visibleGroups := filterPlazaVisibleGroups(ch.Groups, visibleCatalog, userRates)
			if err := h.resolveGroupCatalogs(ctx, visibleGroups, groupCatalogs, resolvedGroups); err != nil {
				return nil, err
			}
		}
	}

	var out []userAvailableChannel
	if visibleCatalog != nil {
		out = buildPlazaAvailableChannels(h.channelService, groupCatalogs, channels, visibleCatalog, userRates)
	} else {
		out = buildPublicAvailableChannels(h.channelService, groupCatalogs, channels)
	}
	applyPlazaModelPricesToChannels(h.modelPrices, h.billingFallback, out)
	applyPlazaMediaPricesToChannels(h.modelPrices, out)
	h.applyRecentCallCounts(ctx, out)
	return out, nil
}

func (h *AvailableChannelHandler) resolveGroupCatalogs(
	ctx context.Context,
	groups []userAvailableGroup,
	groupCatalogs map[int64][]string,
	resolvedGroups map[int64]struct{},
) error {
	if h.modelCatalog == nil {
		return nil
	}
	for _, group := range groups {
		if _, ok := resolvedGroups[group.ID]; ok {
			continue
		}
		models, err := h.modelCatalog.ListForGroup(ctx, group.ID, group.Platform)
		if err != nil {
			return err
		}
		groupCatalogs[group.ID] = models
		resolvedGroups[group.ID] = struct{}{}
	}
	return nil
}

// applyPlazaModelPricesToChannels 按「管理覆盖 > 第三方目录 > 官方兜底 > 渠道价」
// 选择一整套广场展示价，并附带币种、来源与 DeepSeek 官方峰谷规则。
func applyPlazaModelPricesToChannels(
	pricing *service.PricingService,
	official billingFallbackProvider,
	channels []userAvailableChannel,
) {
	if len(channels) == 0 {
		return
	}
	for i := range channels {
		for j := range channels[i].Platforms {
			platform := channels[i].Platforms[j].Platform
			models := channels[i].Platforms[j].SupportedModels
			for k := range models {
				name := strings.TrimSpace(models[k].Name)
				if name == "" {
					continue
				}
				modelPlatform := strings.TrimSpace(models[k].Platform)
				if modelPlatform == "" {
					modelPlatform = platform
				}
				resolution := service.ResolvePlazaDisplayPrice(pricing, modelPlatform, name, plazaOfficialEntry(official, name))
				if resolution != nil {
					models[k].Pricing = userPricingFromPlazaResolution(resolution)
					models[k].TimeSchedule = resolution.TimeSchedule
				} else if models[k].Pricing != nil {
					// 模型价格、第三方目录和官方兜底都缺失时，保留渠道自配价。
					models[k].Pricing.Currency = service.ModelPriceCurrencyUSD
					models[k].Pricing.Source = service.ModelPriceSourceChannel
					models[k].TimeSchedule = nil
				}
			}
			channels[i].Platforms[j].SupportedModels = models
		}
	}
}

func officialModelPricing(provider billingFallbackProvider, model string) *service.ModelPricing {
	if provider == nil {
		return nil
	}
	return provider.GetFallbackPricing(model)
}

func plazaOfficialEntry(provider billingFallbackProvider, model string) *service.ModelPriceEntry {
	return service.ModelPriceEntryFromOfficial(model, officialModelPricing(provider, model))
}

func plazaTokenPrice(value float64, explicit bool) *float64 {
	if value != 0 || explicit {
		return &value
	}
	return nil
}

func userPricingFromPlazaResolution(resolution *service.PlazaDisplayPriceResolution) *userSupportedModelPricing {
	if resolution == nil || resolution.Pricing == nil {
		return nil
	}
	entry := resolution.Pricing
	billingMode := service.BillingModeToken
	if (entry.OutputCostPerImage != 0 || entry.OutputCostPerImageExplicit) &&
		entry.InputCostPerToken == 0 && !entry.InputPriceExplicit &&
		entry.OutputCostPerToken == 0 && !entry.OutputPriceExplicit {
		billingMode = service.BillingModeImage
	}
	return &userSupportedModelPricing{
		BillingMode:     string(billingMode),
		Currency:        resolution.Currency,
		Source:          resolution.Source,
		InputPrice:      plazaTokenPrice(entry.InputCostPerToken, entry.InputPriceExplicit),
		OutputPrice:     plazaTokenPrice(entry.OutputCostPerToken, entry.OutputPriceExplicit),
		CacheWritePrice: plazaTokenPrice(entry.CacheCreationInputTokenCost, entry.CacheCreationPriceExplicit),
		CacheReadPrice:  plazaTokenPrice(entry.CacheReadInputTokenCost, entry.CacheReadPriceExplicit),
		ImageInputPrice: plazaTokenPrice(entry.InputCostPerImageToken, entry.ImageInputPriceExplicit),
		ImageOutputPrice: plazaTokenPrice(
			entry.OutputCostPerImageToken,
			entry.ImageOutputPriceExplicit,
		),
		PerRequestPrice: plazaTokenPrice(entry.OutputCostPerImage, entry.OutputCostPerImageExplicit),
		Intervals:       []userPricingIntervalDTO{},
	}
}

// applyPlazaMediaPricesToChannels 为广场中的图片/视频模型附上按张/按秒档位价。
// 广场 section 恒为单分组（见 buildPublicGroupSections），档位价按该分组解析。
func applyPlazaMediaPricesToChannels(pricing *service.PricingService, channels []userAvailableChannel) {
	for i := range channels {
		for j := range channels[i].Platforms {
			section := &channels[i].Platforms[j]
			if len(section.Groups) != 1 {
				continue
			}
			groupRef := availableGroupRefForImagePricing(section.Groups[0])
			for k := range section.SupportedModels {
				model := &section.SupportedModels[k]
				platform := strings.TrimSpace(model.Platform)
				if platform == "" {
					platform = section.Platform
				}
				media := service.ResolvePlazaMediaPricing(pricing, platform, model.Name, model.channelPricing, groupRef)
				if len(media.ImageTiers) == 0 && len(media.VideoTiers) == 0 && media.VideoPerRequest == nil {
					continue
				}
				if model.Pricing == nil {
					model.Pricing = &userSupportedModelPricing{
						BillingMode: string(service.BillingModeImage),
						Currency:    service.ModelPriceCurrencyUSD,
						Source:      service.ModelPriceSourceChannel,
						Intervals:   []userPricingIntervalDTO{},
					}
				}
				model.Pricing.ImageTierPrices = toUserMediaTierPrices(media.ImageTiers)
				model.Pricing.VideoTierPrices = toUserMediaTierPrices(media.VideoTiers)
				if len(media.VideoTiers) > 0 {
					model.Pricing.BillingMode = string(service.BillingModeVideo)
				} else if media.VideoPerRequest != nil {
					// 视频价目只有按次规则：没有每秒价可展示，按单次价展示。
					model.Pricing.BillingMode = string(service.BillingModePerRequest)
					model.Pricing.PerRequestPrice = media.VideoPerRequest
					model.Pricing.Currency = service.ModelPriceCurrencyUSD // video_pricing 恒为 USD
				}
			}
		}
	}
}

func toUserMediaTierPrices(src []service.PlazaMediaTierPrice) []userMediaTierPriceDTO {
	if len(src) == 0 {
		return nil
	}
	out := make([]userMediaTierPriceDTO, 0, len(src))
	for _, tier := range src {
		out = append(out, userMediaTierPriceDTO{Tier: tier.Tier, Price: tier.Price})
	}
	return out
}

// applyRecentCallCounts 为广场模型附上近 7 天调用量；聚合结果经 recentCalls 缓存复用。
func (h *AvailableChannelHandler) applyRecentCallCounts(ctx context.Context, channels []userAvailableChannel) {
	if h.modelStats == nil || len(channels) == 0 {
		return
	}
	counts, err := h.recentCalls.get(ctx, h.modelStats)
	if err != nil {
		slog.Warn("public plaza: recent call counts query failed", "err", err)
		return
	}
	windowSeconds := int64(publicRecentCallsWindow / time.Second)
	for i := range channels {
		for j := range channels[i].Platforms {
			models := channels[i].Platforms[j].SupportedModels
			for k := range models {
				name := strings.TrimSpace(models[k].Name)
				if name == "" {
					continue
				}
				if count, ok := counts[name]; ok {
					models[k].RecentCallCount = count
				}
				models[k].RecentCallWindow = windowSeconds
			}
			channels[i].Platforms[j].SupportedModels = models
		}
	}
}

func buildPublicAvailableChannels(
	channelService *service.ChannelService,
	groupCatalogs map[int64][]string,
	channels []service.AvailableChannel,
) []userAvailableChannel {
	return buildPlazaAvailableChannels(channelService, groupCatalogs, channels, nil, nil)
}

func buildPlazaAvailableChannels(
	channelService *service.ChannelService,
	groupCatalogs map[int64][]string,
	channels []service.AvailableChannel,
	visibleCatalog map[int64]service.GroupCatalogEntry,
	userRates map[int64]float64,
) []userAvailableChannel {
	out := make([]userAvailableChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterPlazaVisibleGroups(ch.Groups, visibleCatalog, userRates)
		if len(visibleGroups) == 0 {
			continue
		}
		sections := buildPublicGroupSections(ch, visibleGroups, groupCatalogs)
		sections = filterRoutingOnlyModels(sections)
		applyPricingFallbackToSections(channelService, sections)
		sections = filterSectionsWithModels(sections)
		if len(sections) == 0 {
			continue
		}
		out = append(out, userAvailableChannel{
			Name:        ch.Name,
			Description: ch.Description,
			Platforms:   sections,
		})
	}
	return out
}

func filterPlazaVisibleGroups(
	groups []service.AvailableGroupRef,
	visibleCatalog map[int64]service.GroupCatalogEntry,
	userRates map[int64]float64,
) []userAvailableGroup {
	if visibleCatalog == nil {
		return filterPublicGroups(groups)
	}
	visible := filterUserVisibleGroups(groups, visibleCatalog)
	for i := range visible {
		if rate, ok := userRates[visible[i].ID]; ok {
			visible[i].RateMultiplier = rate
		}
	}
	return visible
}

// buildPublicGroupSections emits one section per public group. Keeping the
// group and its authoritative model catalog together prevents a low-rate group
// from contributing to a model that the group cannot actually route.
func buildPublicGroupSections(
	ch service.AvailableChannel,
	visibleGroups []userAvailableGroup,
	groupCatalogs map[int64][]string,
) []userChannelPlatformSection {
	groups := append([]userAvailableGroup(nil), visibleGroups...)
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Platform != groups[j].Platform {
			return groups[i].Platform < groups[j].Platform
		}
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})

	sections := make([]userChannelPlatformSection, 0, len(groups))
	for _, group := range groups {
		if strings.TrimSpace(group.Platform) == "" {
			continue
		}
		platformSet := map[string]struct{}{group.Platform: {}}
		models := toUserSupportedModelsForPublicGroup(ch.SupportedModels, platformSet, group)
		if catalog, resolved := groupCatalogs[group.ID]; resolved {
			models = selectCatalogSupportedModels(models, catalog, group.Platform)
		}
		sections = append(sections, userChannelPlatformSection{
			Platform:        group.Platform,
			Groups:          []userAvailableGroup{group},
			SupportedModels: models,
		})
	}
	return sections
}

// selectCatalogSupportedModels treats the resolved group catalog as the
// authority while retaining channel pricing for matching model names.
func selectCatalogSupportedModels(
	channelModels []userSupportedModel,
	catalog []string,
	platform string,
) []userSupportedModel {
	byName := make(map[string]userSupportedModel, len(channelModels))
	for _, model := range channelModels {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			continue
		}
		byName[strings.ToLower(name)] = model
	}

	out := make([]userSupportedModel, 0, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	for _, rawName := range catalog {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if model, exists := byName[key]; exists {
			model.Name = name
			if model.Platform == "" {
				model.Platform = platform
			}
			out = append(out, model)
			continue
		}
		out = append(out, userSupportedModel{Name: name, Platform: platform})
	}
	return out
}

func mergeGroupCatalogModels(
	sections []userChannelPlatformSection,
	groupCatalogs map[int64][]string,
) []userChannelPlatformSection {
	for i := range sections {
		models := make([]string, 0)
		for _, group := range sections[i].Groups {
			models = append(models, groupCatalogs[group.ID]...)
		}
		sections[i].SupportedModels = mergeNamedSupportedModels(
			sections[i].SupportedModels,
			models,
			sections[i].Platform,
		)
	}
	return sections
}

func filterRoutingOnlyModels(sections []userChannelPlatformSection) []userChannelPlatformSection {
	if len(sections) == 0 {
		return sections
	}
	for i := range sections {
		src := sections[i].SupportedModels
		if len(src) == 0 {
			continue
		}
		kept := src[:0]
		for _, m := range src {
			if service.IsPublicCatalogRoutingOnlyModelID(m.Name) {
				continue
			}
			kept = append(kept, m)
		}
		sections[i].SupportedModels = kept
	}
	return sections
}

// buildPlatformSections 把一个渠道按 visibleGroups 的平台集合拆成有序的 section 列表：
// 每个 section 对应一个具体平台，只包含该平台的 groups 和 supported_models。
//
// Composite 分组可访问渠道中所有已配置的具体平台，因此会被展开到每个有支持模型的
// 平台 section。普通分组仍严格留在自身平台，避免跨平台模型信息泄漏。Composite 渠道
// 尚未配置任何模型时保留 composite section，以便前端继续展示该分组和“未配置模型”状态。
// 输出按 platform 字母序稳定排序，便于前端等效比较与回归测试。
func buildPlatformSections(
	ch service.AvailableChannel,
	visibleGroups []userAvailableGroup,
) []userChannelPlatformSection {
	groupsByPlatform := make(map[string][]userAvailableGroup, 4)
	compositeGroups := make([]userAvailableGroup, 0, 1)
	for _, g := range visibleGroups {
		if g.Platform == "" {
			continue
		}
		if g.Platform == service.PlatformComposite {
			compositeGroups = append(compositeGroups, g)
			continue
		}
		groupsByPlatform[g.Platform] = append(groupsByPlatform[g.Platform], g)
	}

	if len(compositeGroups) > 0 {
		modelPlatforms := make(map[string]struct{}, len(ch.SupportedModels))
		for i := range ch.SupportedModels {
			if platform := ch.SupportedModels[i].Platform; platform != "" {
				modelPlatforms[platform] = struct{}{}
			}
		}
		if len(modelPlatforms) == 0 {
			groupsByPlatform[service.PlatformComposite] = append(
				groupsByPlatform[service.PlatformComposite],
				compositeGroups...,
			)
		} else {
			for platform := range modelPlatforms {
				groupsByPlatform[platform] = append(groupsByPlatform[platform], compositeGroups...)
			}
		}
	}
	if len(groupsByPlatform) == 0 {
		return nil
	}

	platforms := make([]string, 0, len(groupsByPlatform))
	for p := range groupsByPlatform {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)

	sections := make([]userChannelPlatformSection, 0, len(platforms))
	for _, platform := range platforms {
		platformSet := map[string]struct{}{platform: {}}
		sections = append(sections, userChannelPlatformSection{
			Platform:        platform,
			Groups:          groupsByPlatform[platform],
			SupportedModels: toUserSupportedModels(ch.SupportedModels, platformSet),
		})
	}
	return sections
}

func mergeNamedSupportedModels(
	channelModels []userSupportedModel,
	modelNames []string,
	platform string,
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(channelModels))
	seen := make(map[string]struct{}, len(channelModels))
	add := func(model userSupportedModel) {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		model.Name = name
		if model.Platform == "" {
			model.Platform = platform
		}
		out = append(out, model)
	}

	for _, model := range channelModels {
		add(model)
	}
	for _, name := range modelNames {
		add(userSupportedModel{Name: name, Platform: platform})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func applyPricingFallbackToSections(channelService *service.ChannelService, sections []userChannelPlatformSection) {
	if channelService == nil {
		return
	}
	for sectionIndex := range sections {
		modelsNeedingPricing := make([]service.SupportedModel, 0)
		modelIndexes := make([]int, 0)
		for modelIndex := range sections[sectionIndex].SupportedModels {
			model := sections[sectionIndex].SupportedModels[modelIndex]
			if model.Pricing != nil {
				continue
			}
			platform := model.Platform
			if platform == "" {
				platform = sections[sectionIndex].Platform
			}
			modelsNeedingPricing = append(modelsNeedingPricing, service.SupportedModel{
				Name:     model.Name,
				Platform: platform,
			})
			modelIndexes = append(modelIndexes, modelIndex)
		}
		if len(modelsNeedingPricing) == 0 {
			continue
		}
		channelService.FillGlobalPricingFallback(modelsNeedingPricing)
		for i, model := range modelsNeedingPricing {
			if model.Pricing == nil {
				continue
			}
			pricing := model.Pricing
			if len(sections[sectionIndex].Groups) == 1 {
				pricing = service.AvailableImageDisplayPricing(
					pricing,
					availableGroupRefForImagePricing(sections[sectionIndex].Groups[0]),
				)
			}
			sections[sectionIndex].SupportedModels[modelIndexes[i]].Pricing = toUserPricing(pricing)
			sections[sectionIndex].SupportedModels[modelIndexes[i]].channelPricing = model.Pricing
		}
	}
}

func filterSectionsWithModels(sections []userChannelPlatformSection) []userChannelPlatformSection {
	out := make([]userChannelPlatformSection, 0, len(sections))
	for _, section := range sections {
		if len(section.Groups) == 0 || len(section.SupportedModels) == 0 {
			continue
		}
		out = append(out, section)
	}
	return out
}

// filterUserVisibleGroups keeps every server-visible group, including a
// public VIP group that the current user cannot yet bind. Binding decisions
// come from the authoritative catalog and are never reconstructed here.
func filterUserVisibleGroups(
	groups []service.AvailableGroupRef,
	catalog map[int64]service.GroupCatalogEntry,
) []userAvailableGroup {
	visible := make([]userAvailableGroup, 0, len(groups))
	for _, g := range groups {
		entry, ok := catalog[g.ID]
		if !ok {
			continue
		}
		canBind := entry.CanBind
		visible = append(visible, userAvailableGroup{
			ID:                   g.ID,
			Name:                 g.Name,
			Platform:             g.Platform,
			SubscriptionType:     g.SubscriptionType,
			RateMultiplier:       g.RateMultiplier,
			PeakRateEnabled:      g.PeakRateEnabled,
			PeakStart:            g.PeakStart,
			PeakEnd:              g.PeakEnd,
			PeakRateMultiplier:   g.PeakRateMultiplier,
			IsExclusive:          g.IsExclusive,
			VIPOnly:              entry.VIPOnly,
			ImageRateIndependent: g.ImageRateIndependent,
			ImageRateMultiplier:  g.ImageRateMultiplier,
			VideoRateIndependent: g.VideoRateIndependent,
			VideoRateMultiplier:  g.VideoRateMultiplier,
			CanBind:              &canBind,
			DenyReason:           entry.DenyReason,
			SuggestedAction:      entry.SuggestedAction,
			ImagePrice1K:         g.ImagePrice1K,
			ImagePrice2K:         g.ImagePrice2K,
			ImagePrice4K:         g.ImagePrice4K,
			VideoPrice480P:       g.VideoPrice480P,
			VideoPrice720P:       g.VideoPrice720P,
			VideoPrice1080P:      g.VideoPrice1080P,
			VideoModelPrices:     g.VideoModelPrices,
			ModelsListConfig:     g.ModelsListConfig,
		})
	}
	return visible
}

// filterPublicGroups 仅保留公开分组。
func filterPublicGroups(groups []service.AvailableGroupRef) []userAvailableGroup {
	visible := make([]userAvailableGroup, 0, len(groups))
	for _, g := range groups {
		if g.IsExclusive {
			continue
		}
		visible = append(visible, userAvailableGroup{
			ID:                   g.ID,
			Name:                 g.Name,
			Platform:             g.Platform,
			SubscriptionType:     g.SubscriptionType,
			RateMultiplier:       g.RateMultiplier,
			PeakRateEnabled:      g.PeakRateEnabled,
			PeakStart:            g.PeakStart,
			PeakEnd:              g.PeakEnd,
			PeakRateMultiplier:   g.PeakRateMultiplier,
			IsExclusive:          g.IsExclusive,
			VIPOnly:              g.VIPOnly,
			ImageRateIndependent: g.ImageRateIndependent,
			ImageRateMultiplier:  g.ImageRateMultiplier,
			VideoRateIndependent: g.VideoRateIndependent,
			VideoRateMultiplier:  g.VideoRateMultiplier,
			ImagePrice1K:         g.ImagePrice1K,
			ImagePrice2K:         g.ImagePrice2K,
			ImagePrice4K:         g.ImagePrice4K,
			VideoPrice480P:       g.VideoPrice480P,
			VideoPrice720P:       g.VideoPrice720P,
			VideoPrice1080P:      g.VideoPrice1080P,
			VideoModelPrices:     g.VideoModelPrices,
			ModelsListConfig:     g.ModelsListConfig,
		})
	}
	return visible
}

func toUserSupportedModelsForPublicGroup(
	src []service.SupportedModel,
	allowedPlatforms map[string]struct{},
	group userAvailableGroup,
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(src))
	groupRef := availableGroupRefForImagePricing(group)
	for i := range src {
		model := src[i]
		if allowedPlatforms != nil {
			if _, ok := allowedPlatforms[model.Platform]; !ok {
				continue
			}
		}
		out = append(out, userSupportedModel{
			Name:           model.Name,
			Platform:       model.Platform,
			Pricing:        toUserPricing(service.AvailableImageDisplayPricing(model.Pricing, groupRef)),
			channelPricing: model.Pricing,
		})
	}
	return out
}

func availableGroupRefForImagePricing(group userAvailableGroup) service.AvailableGroupRef {
	return service.AvailableGroupRef{
		ImagePrice1K:         group.ImagePrice1K,
		ImagePrice2K:         group.ImagePrice2K,
		ImagePrice4K:         group.ImagePrice4K,
		VideoRateIndependent: group.VideoRateIndependent,
		VideoRateMultiplier:  group.VideoRateMultiplier,
		VideoPrice480P:       group.VideoPrice480P,
		VideoPrice720P:       group.VideoPrice720P,
		VideoPrice1080P:      group.VideoPrice1080P,
		VideoModelPrices:     group.VideoModelPrices,
	}
}

// toUserSupportedModels 将 service 层支持模型转换为用户 DTO（字段白名单）。
// 仅保留平台在 allowedPlatforms 中的条目，防止跨平台模型信息泄漏。
// allowedPlatforms 为 nil 时不做平台过滤（保留全部，供测试或明确无过滤场景使用）。
func toUserSupportedModels(
	src []service.SupportedModel,
	allowedPlatforms map[string]struct{},
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(src))
	for i := range src {
		m := src[i]
		if allowedPlatforms != nil {
			if _, ok := allowedPlatforms[m.Platform]; !ok {
				continue
			}
		}
		out = append(out, userSupportedModel{
			Name:     m.Name,
			Platform: m.Platform,
			Pricing:  toUserPricing(m.Pricing),
		})
	}
	return out
}

// toUserPricingIntervals 将定价区间转换为用户 DTO 白名单形态；nil 入参返回 nil（JSON omitempty 可省略）。
func toUserPricingIntervals(src []service.PricingInterval) []userPricingIntervalDTO {
	if src == nil {
		return nil
	}
	intervals := make([]userPricingIntervalDTO, 0, len(src))
	for _, iv := range src {
		intervals = append(intervals, userPricingIntervalDTO{
			MinTokens:            iv.MinTokens,
			MaxTokens:            iv.MaxTokens,
			TierLabel:            iv.TierLabel,
			InputPrice:           iv.InputPrice,
			OutputPrice:          iv.OutputPrice,
			CacheWritePrice:      iv.CacheWritePrice,
			CacheWrite1hPrice:    iv.CacheWrite1hPrice,
			CacheReadPrice:       iv.CacheReadPrice,
			InputMultiplier:      iv.InputMultiplier,
			OutputMultiplier:     iv.OutputMultiplier,
			CacheWriteMultiplier: iv.CacheWriteMultiplier,
			CacheReadMultiplier:  iv.CacheReadMultiplier,
			PerRequestPrice:      iv.PerRequestPrice,
		})
	}
	return intervals
}

// toUserPricing 将 service 层定价转换为用户 DTO；入参为 nil 时返回 nil。
func toUserPricing(p *service.ChannelModelPricing) *userSupportedModelPricing {
	if p == nil {
		return nil
	}
	intervals := toUserPricingIntervals(p.Intervals)
	if intervals == nil {
		// 用户侧定价的 intervals 固定输出数组（空配置为 []），保持既有契约。
		intervals = []userPricingIntervalDTO{}
	}
	billingMode := string(p.BillingMode)
	if billingMode == "" {
		billingMode = string(service.BillingModeToken)
	}
	return &userSupportedModelPricing{
		BillingMode:                  billingMode,
		Currency:                     service.ModelPriceCurrencyUSD,
		Source:                       service.ModelPriceSourceChannel,
		InputPrice:                   p.InputPrice,
		OutputPrice:                  p.OutputPrice,
		CacheWritePrice:              p.CacheWritePrice,
		CacheWrite1hPrice:            p.CacheWrite1hPrice,
		CacheReadPrice:               p.CacheReadPrice,
		MaxReasoningEffortMultiplier: p.MaxReasoningEffortMultiplier,
		ImageInputPrice:              p.ImageInputPrice,
		ImageOutputPrice:             p.ImageOutputPrice,
		PerRequestPrice:              p.PerRequestPrice,
		Intervals:                    intervals,
	}
}
