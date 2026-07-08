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

// AvailableChannelHandler 处理用户侧「可用渠道」查询。
//
// 用户侧接口委托 ChannelService.ListAvailable，并在返回前做三层过滤：
//  1. 行过滤：只保留状态为 Active 且与当前用户可访问分组有交集的渠道；
//  2. 分组过滤：渠道的 Groups 只保留用户可访问的那些；
//  3. 平台过滤：渠道的 SupportedModels 只保留平台在用户可见 Groups 中出现过的模型，
//     防止"渠道同时挂在 antigravity / anthropic 两个平台的分组上，用户只访问
//     antigravity，却看到 anthropic 模型"这类跨平台信息泄漏；
//  4. 字段白名单：仅返回用户需要的字段（省略 BillingModelSource / RestrictModels
//     / 内部 ID / Status 等管理字段）。
type AvailableChannelHandler struct {
	channelService  *service.ChannelService
	apiKeyService   *service.APIKeyService
	settingService  *service.SettingService
	gatewayModels   gatewayModelsProvider
	modelStats      publicModelStatsProvider
	billingFallback billingFallbackProvider
}

// NewAvailableChannelHandler 创建用户侧可用渠道 handler。
func NewAvailableChannelHandler(
	channelService *service.ChannelService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	gatewayModels *service.GatewayService,
	modelStats publicModelStatsProvider,
	billingFallback billingFallbackProvider,
) *AvailableChannelHandler {
	return &AvailableChannelHandler{
		channelService:  channelService,
		apiKeyService:   apiKeyService,
		settingService:  settingService,
		gatewayModels:   gatewayModels,
		modelStats:      modelStats,
		billingFallback: billingFallback,
	}
}

type gatewayModelsProvider interface {
	GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string
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
	ID                 int64                         `json:"id"`
	Name               string                        `json:"name"`
	Platform           string                        `json:"platform"`
	SubscriptionType   string                        `json:"subscription_type"`
	RateMultiplier     float64                       `json:"rate_multiplier"`
	PeakRateEnabled    bool                          `json:"peak_rate_enabled"`
	PeakStart          string                        `json:"peak_start"`
	PeakEnd            string                        `json:"peak_end"`
	PeakRateMultiplier float64                       `json:"peak_rate_multiplier"`
	IsExclusive        bool                          `json:"is_exclusive"`
	ModelsListConfig   service.GroupModelsListConfig `json:"-"`
}

// userSupportedModelPricing 用户可见的定价字段白名单。
type userSupportedModelPricing struct {
	BillingMode      string                   `json:"billing_mode"`
	InputPrice       *float64                 `json:"input_price"`
	OutputPrice      *float64                 `json:"output_price"`
	CacheWritePrice  *float64                 `json:"cache_write_price"`
	CacheReadPrice   *float64                 `json:"cache_read_price"`
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
	Name             string                     `json:"name"`
	Platform         string                     `json:"platform"`
	Pricing          *userSupportedModelPricing `json:"pricing"`
	RecentCallCount  int64                      `json:"recent_call_count"`
	RecentCallWindow int64                      `json:"recent_call_window_seconds"`
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

	userGroups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	allowedGroupIDs := make(map[int64]struct{}, len(userGroups))
	for i := range userGroups {
		allowedGroupIDs[userGroups[i].ID] = struct{}{}
	}

	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]userAvailableChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterUserVisibleGroups(ch.Groups, allowedGroupIDs)
		if len(visibleGroups) == 0 {
			continue
		}
		sections := buildPlatformSections(ch, visibleGroups)
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

	c.Header("Cache-Control", "public, max-age=300")
	out := buildPublicAvailableChannels(c.Request.Context(), h.channelService, h.gatewayModels, channels)
	applyBillingFallbackToChannels(h.billingFallback, out)
	applyRecentCallCounts(c.Request.Context(), h.modelStats, out)
	response.Success(c, out)
}

// applyBillingFallbackToChannels 在 pricing catalog 补齐后再跑一遍：
// 用 billing_service 的硬编码 fallback 补 pricing 完全为空或存在 nil 字段的模型。
func applyBillingFallbackToChannels(
	provider billingFallbackProvider,
	channels []userAvailableChannel,
) {
	if provider == nil || len(channels) == 0 {
		return
	}
	for i := range channels {
		for j := range channels[i].Platforms {
			models := channels[i].Platforms[j].SupportedModels
			for k := range models {
				name := strings.TrimSpace(models[k].Name)
				if name == "" {
					continue
				}
				mp := provider.GetFallbackPricing(name)
				if mp == nil {
					continue
				}
				if models[k].Pricing == nil {
					models[k].Pricing = buildUserPricingFromModelPricing(mp)
					continue
				}
				enrichUserPricingFromModelPricing(models[k].Pricing, mp)
			}
			channels[i].Platforms[j].SupportedModels = models
		}
	}
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
	ctx context.Context,
	channelService *service.ChannelService,
	modelsProvider gatewayModelsProvider,
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
		sections := buildPlatformSections(ch, visibleGroups)
		sections = mergePublicGatewayModels(ctx, modelsProvider, sections)
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

func mergePublicGatewayModels(
	ctx context.Context,
	modelsProvider gatewayModelsProvider,
	sections []userChannelPlatformSection,
) []userChannelPlatformSection {
	for i := range sections {
		models := publicModelIDsForPlatform(ctx, modelsProvider, sections[i].Platform)
		sections[i].SupportedModels = mergeNamedSupportedModels(sections[i].SupportedModels, models, sections[i].Platform)
	}
	return sections
}

var plazaRoutingOnlyModelIDs = map[string]struct{}{
	"adaptive":            {},
	"arena-fast":          {},
	"arena-mixed":         {},
	"arena-smart":         {},
	"swe-1-6":             {},
	"swe-1-6-fast":        {},
	"swe-check":           {},
	"deepseek-v4":         {},
	"minimax-m2-5":        {},
	"opencode/big-pickle": {},
}

func filterRoutingOnlyModels(sections []userChannelPlatformSection) []userChannelPlatformSection {
	if len(plazaRoutingOnlyModelIDs) == 0 || len(sections) == 0 {
		return sections
	}
	for i := range sections {
		src := sections[i].SupportedModels
		if len(src) == 0 {
			continue
		}
		kept := src[:0]
		for _, m := range src {
			key := strings.ToLower(strings.TrimSpace(m.Name))
			if _, ok := plazaRoutingOnlyModelIDs[key]; ok {
				continue
			}
			kept = append(kept, m)
		}
		sections[i].SupportedModels = kept
	}
	return sections
}

func publicModelIDsForPlatform(ctx context.Context, modelsProvider gatewayModelsProvider, platform string) []string {
	if modelsProvider != nil {
		if models := modelsProvider.GetAvailableModels(ctx, nil, platform); len(models) > 0 {
			return models
		}
	}
	return defaultModelIDsForPlatform(platform)
}

// buildPlatformSections 把一个渠道按 visibleGroups 的平台集合拆成有序的 section 列表：
// 每个 section 对应一个平台，只包含该平台的 groups 和 supported_models。
// 输出按 platform 字母序稳定排序，便于前端等效比较与回归测试。
func buildPlatformSections(
	ch service.AvailableChannel,
	visibleGroups []userAvailableGroup,
) []userChannelPlatformSection {
	groupsByPlatform := make(map[string][]userAvailableGroup, 4)
	for _, g := range visibleGroups {
		if g.Platform == "" {
			continue
		}
		groupsByPlatform[g.Platform] = append(groupsByPlatform[g.Platform], g)
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
			Platform: platform,
			Groups:   groupsByPlatform[platform],
			SupportedModels: mergeGroupSupportedModels(
				toUserSupportedModels(ch.SupportedModels, platformSet),
				groupsByPlatform[platform],
				platform,
			),
		})
	}
	return sections
}

func mergeGroupSupportedModels(
	channelModels []userSupportedModel,
	groups []userAvailableGroup,
	platform string,
) []userSupportedModel {
	groupModels := make([]string, 0)
	for _, group := range groups {
		if !group.ModelsListConfig.Enabled {
			continue
		}
		groupModels = append(groupModels, group.ModelsListConfig.Models...)
	}
	return mergeNamedSupportedModels(channelModels, groupModels, platform)
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
			sections[sectionIndex].SupportedModels[modelIndexes[i]].Pricing = toUserPricing(model.Pricing)
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

// filterUserVisibleGroups 仅保留用户可访问的分组。
func filterUserVisibleGroups(
	groups []service.AvailableGroupRef,
	allowed map[int64]struct{},
) []userAvailableGroup {
	visible := make([]userAvailableGroup, 0, len(groups))
	for _, g := range groups {
		if _, ok := allowed[g.ID]; !ok {
			continue
		}
		visible = append(visible, userAvailableGroup{
			ID:                 g.ID,
			Name:               g.Name,
			Platform:           g.Platform,
			SubscriptionType:   g.SubscriptionType,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			IsExclusive:        g.IsExclusive,
			ModelsListConfig:   g.ModelsListConfig,
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
			ID:                 g.ID,
			Name:               g.Name,
			Platform:           g.Platform,
			SubscriptionType:   g.SubscriptionType,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			IsExclusive:        g.IsExclusive,
			ModelsListConfig:   g.ModelsListConfig,
		})
	}
	return visible
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
		ImageOutputPrice: p.ImageOutputPrice,
		PerRequestPrice:  p.PerRequestPrice,
		Intervals:        intervals,
	}
}
