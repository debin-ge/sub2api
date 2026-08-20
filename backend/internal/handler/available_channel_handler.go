package handler

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// publicRecentCallsWindow 公开模型广场的调用量统计窗口。
const publicRecentCallsWindow = 7 * 24 * time.Hour

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
}

type availableChannelSettingService interface {
	GetAvailableChannelsRuntime(context.Context) service.AvailableChannelsRuntime
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
	return &AvailableChannelHandler{
		channelService:  channelService,
		apiKeyService:   apiKeyService,
		settingService:  settingService,
		modelCatalog:    modelCatalog,
		modelStats:      modelStats,
		billingFallback: billingFallback,
		modelPrices:     modelPrices,
	}
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
	CanBind              *bool                              `json:"can_bind,omitempty"`
	DenyReason           service.GroupAccessDenyReason      `json:"deny_reason,omitempty"`
	SuggestedAction      service.GroupAccessSuggestedAction `json:"suggested_action,omitempty"`
	ImagePrice1K         *float64                           `json:"-"`
	ImagePrice2K         *float64                           `json:"-"`
	ImagePrice4K         *float64                           `json:"-"`
	ModelsListConfig     service.GroupModelsListConfig      `json:"-"`
}

// userSupportedModelPricing 用户可见的定价字段白名单。
type userSupportedModelPricing struct {
	BillingMode      string                   `json:"billing_mode"`
	InputPrice       *float64                 `json:"input_price"`
	OutputPrice      *float64                 `json:"output_price"`
	CacheWritePrice  *float64                 `json:"cache_write_price"`
	CacheReadPrice   *float64                 `json:"cache_read_price"`
	ImageInputPrice  *float64                 `json:"image_input_price"`
	ImageOutputPrice *float64                 `json:"image_output_price"`
	PerRequestPrice  *float64                 `json:"per_request_price"`
	Intervals        []userPricingIntervalDTO `json:"intervals"`
}

// userPricingIntervalDTO 定价区间白名单（去掉内部 ID、SortOrder 等前端不渲染的字段）。
type userPricingIntervalDTO struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
}

// userSupportedModel 用户可见的支持模型条目。
type userSupportedModel struct {
	Name             string                          `json:"name"`
	Platform         string                          `json:"platform"`
	Pricing          *userSupportedModelPricing      `json:"pricing"`
	RecentCallCount  int64                           `json:"recent_call_count"`
	RecentCallWindow int64                           `json:"recent_call_window_seconds"`
	TimeSchedule     *service.ModelPriceTimeSchedule `json:"time_schedule,omitempty"`
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
	channels, err := h.channelService.ListPublicAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	groupCatalogs := make(map[int64][]string)
	resolvedGroups := make(map[int64]struct{})
	if h.modelCatalog != nil {
		for _, ch := range channels {
			if err := h.resolveGroupCatalogs(
				c.Request.Context(),
				filterPublicGroups(ch.Groups),
				groupCatalogs,
				resolvedGroups,
			); err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
	}

	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	out := buildPublicAvailableChannels(h.channelService, groupCatalogs, channels)
	applyPlazaModelPricesToChannels(h.modelPrices, h.billingFallback, out)
	applyRecentCallCounts(c.Request.Context(), h.modelStats, out)
	response.Success(c, out)
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

// applyPlazaModelPricesToChannels 用管理端模型价格页的生效价覆盖广场展示价
// （目录 + 手动覆盖 + 官方兜底），并附带 DeepSeek 官方峰谷规则。
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
				officialEntry := plazaOfficialEntry(official, name)
				if pricing != nil {
					entry, schedule := service.ResolvePlazaDisplayPrice(pricing, modelPlatform, name, officialEntry)
					if entry != nil {
						models[k].Pricing = overlayUserPricingFromModelPrice(models[k].Pricing, entry)
					}
					models[k].TimeSchedule = schedule
				} else {
					// 没有价格目录时展示价只能来自官方兜底表，表里存的是高峰价。
					models[k].TimeSchedule = service.DeepSeekOfficialPriceTimeSchedule(modelPlatform, name, false)
				}
				if mp := officialModelPricing(official, name); mp != nil {
					if models[k].Pricing == nil {
						models[k].Pricing = buildUserPricingFromModelPricing(mp)
					} else {
						enrichUserPricingFromModelPricing(models[k].Pricing, mp)
					}
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

func overlayUserPricingFromModelPrice(existing *userSupportedModelPricing, entry *service.ModelPriceEntry) *userSupportedModelPricing {
	if entry == nil || !plazaModelPriceUsable(entry) {
		return existing
	}
	if existing == nil {
		existing = &userSupportedModelPricing{
			BillingMode: string(service.BillingModeToken),
			Intervals:   []userPricingIntervalDTO{},
		}
	}
	if v := plazaTokenPrice(entry.InputCostPerToken, entry.InputPriceExplicit); v != nil {
		existing.InputPrice = v
	}
	if v := plazaTokenPrice(entry.OutputCostPerToken, entry.OutputPriceExplicit); v != nil {
		existing.OutputPrice = v
	}
	if v := plazaTokenPrice(entry.CacheCreationInputTokenCost, entry.CacheCreationPriceExplicit); v != nil {
		existing.CacheWritePrice = v
	}
	if v := plazaTokenPrice(entry.CacheReadInputTokenCost, entry.CacheReadPriceExplicit); v != nil {
		existing.CacheReadPrice = v
	}
	if v := plazaTokenPrice(entry.OutputCostPerImageToken, entry.ImageOutputPriceExplicit); v != nil {
		existing.ImageOutputPrice = v
	}
	if v := plazaTokenPrice(entry.InputCostPerImageToken, entry.ImageInputPriceExplicit); v != nil {
		existing.ImageInputPrice = v
	}
	return existing
}

func plazaModelPriceUsable(entry *service.ModelPriceEntry) bool {
	if entry == nil {
		return false
	}
	return entry.InputCostPerToken != 0 || entry.InputPriceExplicit ||
		entry.OutputCostPerToken != 0 || entry.OutputPriceExplicit ||
		entry.CacheCreationInputTokenCost != 0 || entry.CacheCreationPriceExplicit ||
		entry.CacheReadInputTokenCost != 0 || entry.CacheReadPriceExplicit ||
		entry.OutputCostPerImageToken != 0 || entry.ImageOutputPriceExplicit ||
		entry.InputCostPerImageToken != 0 || entry.ImageInputPriceExplicit ||
		entry.OutputCostPerImage != 0 || entry.OutputCostPerImageExplicit
}

func plazaTokenPrice(value float64, explicit bool) *float64 {
	if value != 0 || explicit {
		return nonZeroFloatPtr(value)
	}
	return nil
}

func buildUserPricingFromModelPricing(mp *service.ModelPricing) *userSupportedModelPricing {
	if mp == nil {
		return nil
	}
	return &userSupportedModelPricing{
		BillingMode:      string(service.BillingModeToken),
		InputPrice:       nonZeroFloatPtr(mp.InputPricePerToken),
		OutputPrice:      nonZeroFloatPtr(mp.OutputPricePerToken),
		CacheWritePrice:  nonZeroFloatPtr(mp.CacheCreationPricePerToken),
		CacheReadPrice:   nonZeroFloatPtr(mp.CacheReadPricePerToken),
		ImageOutputPrice: nonZeroFloatPtr(mp.ImageOutputPricePerToken),
		Intervals:        []userPricingIntervalDTO{},
	}
}

func enrichUserPricingFromModelPricing(existing *userSupportedModelPricing, mp *service.ModelPricing) {
	if existing == nil || mp == nil {
		return
	}
	if existing.InputPrice == nil {
		existing.InputPrice = nonZeroFloatPtr(mp.InputPricePerToken)
	}
	if existing.OutputPrice == nil {
		existing.OutputPrice = nonZeroFloatPtr(mp.OutputPricePerToken)
	}
	if existing.CacheWritePrice == nil {
		existing.CacheWritePrice = nonZeroFloatPtr(mp.CacheCreationPricePerToken)
	}
	if existing.CacheReadPrice == nil {
		existing.CacheReadPrice = nonZeroFloatPtr(mp.CacheReadPricePerToken)
	}
	if existing.ImageOutputPrice == nil {
		existing.ImageOutputPrice = nonZeroFloatPtr(mp.ImageOutputPricePerToken)
	}
}

func nonZeroFloatPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func applyRecentCallCounts(
	ctx context.Context,
	stats publicModelStatsProvider,
	channels []userAvailableChannel,
) {
	if stats == nil || len(channels) == 0 {
		return
	}
	since := time.Now().UTC().Add(-publicRecentCallsWindow)
	counts, err := stats.GetPublicModelRecentCallCounts(ctx, since)
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
	out := make([]userAvailableChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterPublicGroups(ch.Groups)
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
			CanBind:              &canBind,
			DenyReason:           entry.DenyReason,
			SuggestedAction:      entry.SuggestedAction,
			ImagePrice1K:         g.ImagePrice1K,
			ImagePrice2K:         g.ImagePrice2K,
			ImagePrice4K:         g.ImagePrice4K,
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
			ImagePrice1K:         g.ImagePrice1K,
			ImagePrice2K:         g.ImagePrice2K,
			ImagePrice4K:         g.ImagePrice4K,
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
			Name:     model.Name,
			Platform: model.Platform,
			Pricing:  toUserPricing(service.AvailableImageDisplayPricing(model.Pricing, groupRef)),
		})
	}
	return out
}

func availableGroupRefForImagePricing(group userAvailableGroup) service.AvailableGroupRef {
	return service.AvailableGroupRef{
		ImagePrice1K: group.ImagePrice1K,
		ImagePrice2K: group.ImagePrice2K,
		ImagePrice4K: group.ImagePrice4K,
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

// toUserPricing 将 service 层定价转换为用户 DTO；入参为 nil 时返回 nil。
func toUserPricing(p *service.ChannelModelPricing) *userSupportedModelPricing {
	if p == nil {
		return nil
	}
	intervals := make([]userPricingIntervalDTO, 0, len(p.Intervals))
	for _, iv := range p.Intervals {
		intervals = append(intervals, userPricingIntervalDTO{
			MinTokens:       iv.MinTokens,
			MaxTokens:       iv.MaxTokens,
			TierLabel:       iv.TierLabel,
			InputPrice:      iv.InputPrice,
			OutputPrice:     iv.OutputPrice,
			CacheWritePrice: iv.CacheWritePrice,
			CacheReadPrice:  iv.CacheReadPrice,
			PerRequestPrice: iv.PerRequestPrice,
		})
	}
	billingMode := string(p.BillingMode)
	if billingMode == "" {
		billingMode = string(service.BillingModeToken)
	}
	return &userSupportedModelPricing{
		BillingMode:      billingMode,
		InputPrice:       p.InputPrice,
		OutputPrice:      p.OutputPrice,
		CacheWritePrice:  p.CacheWritePrice,
		CacheReadPrice:   p.CacheReadPrice,
		ImageInputPrice:  p.ImageInputPrice,
		ImageOutputPrice: p.ImageOutputPrice,
		PerRequestPrice:  p.PerRequestPrice,
		Intervals:        intervals,
	}
}
