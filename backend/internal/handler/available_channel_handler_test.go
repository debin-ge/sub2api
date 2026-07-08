//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserAvailableChannel_Unauthenticated401(t *testing.T) {
	// 没有 AuthSubject 注入时，handler 应返回 401 且不触达 service 依赖。
	gin.SetMode(gin.TestMode)
	h := &AvailableChannelHandler{} // nil services — 401 路径不会调用它们
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)

	h.List(c)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestFilterUserVisibleGroups_IntersectionOnly(t *testing.T) {
	// 渠道挂在 {g1, g2, g3}，用户只允许 {g1, g3} —— 响应必须仅含 g1/g3。
	groups := []service.AvailableGroupRef{
		{ID: 1, Name: "g1", Platform: "anthropic"},
		{ID: 2, Name: "g2", Platform: "anthropic"},
		{ID: 3, Name: "g3", Platform: "openai"},
	}
	allowed := map[int64]struct{}{1: {}, 3: {}}

	visible := filterUserVisibleGroups(groups, allowed)
	require.Len(t, visible, 2)
	ids := []int64{visible[0].ID, visible[1].ID}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestToUserSupportedModels_FiltersByAllowedPlatforms(t *testing.T) {
	// 用户可访问分组只覆盖 anthropic；anthropic 平台的模型保留，openai 模型被剔除。
	src := []service.SupportedModel{
		{Name: "claude-sonnet-4-6", Platform: "anthropic", Pricing: nil},
		{Name: "gpt-4o", Platform: "openai", Pricing: nil},
	}
	allowed := map[string]struct{}{"anthropic": {}}
	out := toUserSupportedModels(src, allowed)
	require.Len(t, out, 1)
	require.Equal(t, "claude-sonnet-4-6", out[0].Name)
}

func TestToUserSupportedModels_NilAllowedPlatformsKeepsAll(t *testing.T) {
	// 显式传 nil allowedPlatforms 表示不做过滤。
	src := []service.SupportedModel{
		{Name: "a", Platform: "anthropic"},
		{Name: "b", Platform: "openai"},
	}
	require.Len(t, toUserSupportedModels(src, nil), 2)
}

func TestFilterPublicGroups_DropsExclusiveGroups(t *testing.T) {
	channels := []service.AvailableChannel{
		{
			Name:   "active-public",
			Status: service.StatusActive,
			Groups: []service.AvailableGroupRef{
				{ID: 1, Name: "public-ant", Platform: "anthropic", IsExclusive: false},
				{ID: 2, Name: "exclusive-ant", Platform: "anthropic", IsExclusive: true},
			},
			SupportedModels: []service.SupportedModel{
				{Name: "claude-sonnet-4-6", Platform: "anthropic"},
			},
		},
		{
			Name:   "inactive-public",
			Status: service.StatusDisabled,
			Groups: []service.AvailableGroupRef{
				{ID: 3, Name: "public-openai", Platform: "openai", IsExclusive: false},
			},
			SupportedModels: []service.SupportedModel{
				{Name: "gpt-4o", Platform: "openai"},
			},
		},
		{
			Name:   "exclusive-only",
			Status: service.StatusActive,
			Groups: []service.AvailableGroupRef{
				{ID: 4, Name: "exclusive-openai", Platform: "openai", IsExclusive: true},
			},
			SupportedModels: []service.SupportedModel{
				{Name: "gpt-4o", Platform: "openai"},
			},
		},
	}

	out := buildPublicAvailableChannels(context.Background(), nil, nil, channels)
	require.Len(t, out, 1)
	require.Equal(t, "active-public", out[0].Name)
	require.Len(t, out[0].Platforms, 1)
	require.Len(t, out[0].Platforms[0].Groups, 1)
	require.Equal(t, int64(1), out[0].Platforms[0].Groups[0].ID)
	require.False(t, out[0].Platforms[0].Groups[0].IsExclusive)
}

func TestListPublic_IgnoresAvailableChannelsFeatureFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inputPrice := 0.000001
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:       1,
				Name:     "public-channel",
				Status:   service.StatusActive,
				GroupIDs: []int64{1},
				ModelPricing: []service.ChannelModelPricing{{
					Platform:   "openai",
					Models:     []string{"gpt-4o-mini"},
					InputPrice: &inputPrice,
				}},
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:       1,
				Name:     "public-openai",
				Platform: "openai",
			}},
		},
		nil,
		nil,
	)
	h := &AvailableChannelHandler{channelService: channelSvc}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, "public-channel", body.Data[0].Name)
	require.Len(t, body.Data[0].Platforms, 1)
	require.Contains(t, supportedModelNames(body.Data[0].Platforms[0].SupportedModels), "gpt-4o-mini")
}

func TestListPublic_UsesAllActivePublicGroupsNotOnlyChannelBindings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inputPrice := 0.000001
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:     1,
				Name:   "unbound-channel",
				Status: service.StatusActive,
				ModelPricing: []service.ChannelModelPricing{{
					Platform:   "openai",
					Models:     []string{"priced-model"},
					InputPrice: &inputPrice,
				}},
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:       1,
				Name:     "public-openai",
				Platform: "openai",
				ModelsListConfig: service.GroupModelsListConfig{
					Enabled: true,
					Models:  []string{"group-only-model"},
				},
			}},
		},
		nil,
		nil,
	)
	h := &AvailableChannelHandler{channelService: channelSvc}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, "unbound-channel", body.Data[0].Name)
	require.Len(t, body.Data[0].Platforms, 1)
	names := supportedModelNames(body.Data[0].Platforms[0].SupportedModels)
	require.Contains(t, names, "group-only-model")
	require.Contains(t, names, "priced-model")
}

func TestListPublic_UsesGatewayModelsWhenChannelAndGroupModelsAreEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inputPrice := 0.000003
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:     1,
				Name:   "public-channel",
				Status: service.StatusActive,
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:          1,
				Name:        "public-openai",
				Platform:    "openai",
				IsExclusive: false,
			}},
		},
		nil,
		newHandlerTestPricingService(map[string]*service.ModelPriceEntry{
			"gpt-5.4": {Mode: "chat", InputCostPerToken: inputPrice},
		}),
	)
	h := &AvailableChannelHandler{
		channelService: channelSvc,
		gatewayModels:  stubGatewayModelsProvider{modelsByPlatform: map[string][]string{"openai": {"gpt-5.4"}}},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Len(t, body.Data[0].Platforms, 1)
	require.Equal(t, []string{"gpt-5.4"}, supportedModelNames(body.Data[0].Platforms[0].SupportedModels))
	model := body.Data[0].Platforms[0].SupportedModels[0]
	require.NotNil(t, model.Pricing)
	require.NotNil(t, model.Pricing.InputPrice)
	require.InDelta(t, inputPrice, *model.Pricing.InputPrice, 1e-12)
}

func TestListPublic_UsesBillingFallbackWhenCatalogMisses(t *testing.T) {
	// 场景：模型在 pricing catalog 里没有条目，但 billing_service 硬编码 fallback 有价，
	// 广场应把 billing fallback 的价填到公开响应里，跟真实计费口径对齐。
	gin.SetMode(gin.TestMode)
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:     1,
				Name:   "public-channel",
				Status: service.StatusActive,
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:          1,
				Name:        "public-glm",
				Platform:    "glm",
				IsExclusive: false,
			}},
		},
		nil,
		nil, // pricing catalog 完全不覆盖
	)
	h := &AvailableChannelHandler{
		channelService: channelSvc,
		gatewayModels:  stubGatewayModelsProvider{modelsByPlatform: map[string][]string{"glm": {"GLM-4.7"}}},
		billingFallback: stubBillingFallbackProvider{
			data: map[string]*service.ModelPricing{
				"glm-4.7": {
					InputPricePerToken:         6e-6,
					OutputPricePerToken:        24e-6,
					CacheReadPricePerToken:     1.3e-6,
					CacheCreationPricePerToken: 0,
					SupportsCacheBreakdown:     false,
				},
			},
		},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Len(t, body.Data[0].Platforms, 1)
	require.Equal(t, []string{"GLM-4.7"}, supportedModelNames(body.Data[0].Platforms[0].SupportedModels))

	model := body.Data[0].Platforms[0].SupportedModels[0]
	require.NotNil(t, model.Pricing, "billing fallback should have filled pricing")
	require.NotNil(t, model.Pricing.InputPrice)
	require.InDelta(t, 6e-6, *model.Pricing.InputPrice, 1e-12)
	require.NotNil(t, model.Pricing.OutputPrice)
	require.InDelta(t, 24e-6, *model.Pricing.OutputPrice, 1e-12)
	require.NotNil(t, model.Pricing.CacheReadPrice)
	require.InDelta(t, 1.3e-6, *model.Pricing.CacheReadPrice, 1e-12)
	// CacheCreationPricePerToken == 0 应保持 nil（用户看到空字段，不显示 ¥0）
	require.Nil(t, model.Pricing.CacheWritePrice)
}

func TestListPublic_BillingFallbackEnrichesPartialCatalog(t *testing.T) {
	// 场景：catalog 只填了 input/output，billing fallback 有 cache_read；
	// 补齐后 catalog 已有值不动，nil 字段用 fallback 填。
	gin.SetMode(gin.TestMode)
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:     1,
				Name:   "public-channel",
				Status: service.StatusActive,
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:          1,
				Name:        "public-openai",
				Platform:    "openai",
				IsExclusive: false,
			}},
		},
		nil,
		newHandlerTestPricingService(map[string]*service.ModelPriceEntry{
			"gpt-5.4": {
				Mode:               "chat",
				InputCostPerToken:  2.5e-6,
				OutputCostPerToken: 15e-6,
			},
		}),
	)
	h := &AvailableChannelHandler{
		channelService: channelSvc,
		gatewayModels:  stubGatewayModelsProvider{modelsByPlatform: map[string][]string{"openai": {"gpt-5.4"}}},
		billingFallback: stubBillingFallbackProvider{
			data: map[string]*service.ModelPricing{
				"gpt-5.4": {
					InputPricePerToken:     99e-6, // 应被 catalog 的 2.5e-6 保护
					OutputPricePerToken:    99e-6, // 同上
					CacheReadPricePerToken: 0.25e-6,
				},
			},
		},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	model := body.Data[0].Platforms[0].SupportedModels[0]
	require.NotNil(t, model.Pricing)
	require.InDelta(t, 2.5e-6, *model.Pricing.InputPrice, 1e-12, "catalog 值应被保留")
	require.InDelta(t, 15e-6, *model.Pricing.OutputPrice, 1e-12, "catalog 值应被保留")
	require.NotNil(t, model.Pricing.CacheReadPrice, "cache_read 应从 fallback 补")
	require.InDelta(t, 0.25e-6, *model.Pricing.CacheReadPrice, 1e-12)
}

func TestListPublic_HidesRoutingOnlyModels(t *testing.T) {
	// Windsurf 的 arena-* / swe-* / adaptive 是运行时路由标识，不应出现在广场。
	gin.SetMode(gin.TestMode)
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{
			channels: []service.Channel{{
				ID:     1,
				Name:   "public-channel",
				Status: service.StatusActive,
			}},
		},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:          1,
				Name:        "public-windsurf",
				Platform:    "windsurf",
				IsExclusive: false,
			}},
		},
		nil,
		nil,
	)
	h := &AvailableChannelHandler{
		channelService: channelSvc,
		gatewayModels: stubGatewayModelsProvider{modelsByPlatform: map[string][]string{
			"windsurf": {
				"adaptive",
				"arena-fast",
				"swe-check",
				"deepseek-v4",
				"minimax-m2-5",
				"claude-sonnet-4-6",
			},
		}},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Len(t, body.Data[0].Platforms, 1)
	names := supportedModelNames(body.Data[0].Platforms[0].SupportedModels)
	require.Equal(t, []string{"claude-sonnet-4-6"}, names, "路由型模型应被过滤，只留真实模型")
}

func TestListPublic_RendersModelsFromGroupsAndAccountsWhenNoChannelsExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{},
		&publicGroupRepoStub{
			groups: []service.Group{{
				ID:          1,
				Name:        "public-openai",
				Platform:    "openai",
				IsExclusive: false,
			}},
		},
		nil,
		nil,
	)
	h := &AvailableChannelHandler{
		channelService: channelSvc,
		gatewayModels:  stubGatewayModelsProvider{modelsByPlatform: map[string][]string{"openai": {"gpt-5.4"}}},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/public", nil)

	h.ListPublic(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []userAvailableChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, service.PublicCatalogChannelName, body.Data[0].Name)
	require.Len(t, body.Data[0].Platforms, 1)
	require.Equal(t, []string{"gpt-5.4"}, supportedModelNames(body.Data[0].Platforms[0].SupportedModels))
}

func TestPublicAvailableChannel_FieldWhitelist(t *testing.T) {
	channels := []service.AvailableChannel{
		{
			ID:                 99,
			Name:               "public-channel",
			Description:        "safe description",
			Status:             service.StatusActive,
			BillingModelSource: "admin_override",
			RestrictModels:     true,
			Groups: []service.AvailableGroupRef{
				{ID: 1, Name: "public-ant", Platform: "anthropic", IsExclusive: false},
			},
			SupportedModels: []service.SupportedModel{
				{
					Name:     "claude-sonnet-4-6",
					Platform: "anthropic",
					Pricing: &service.ChannelModelPricing{
						BillingMode: service.BillingModeToken,
					},
				},
			},
		},
	}

	raw, err := json.Marshal(buildPublicAvailableChannels(context.Background(), nil, nil, channels))
	require.NoError(t, err)
	body := string(raw)
	for _, forbidden := range []string{
		"api_key",
		"base_url",
		"priority",
		"weight",
		"status",
		"billing_model_source",
		"restrict_models",
		"models_list_config",
		"admin_override",
	} {
		require.NotContains(t, body, forbidden)
	}
	require.Contains(t, body, `"name":"public-channel"`)
	require.Contains(t, body, `"platforms"`)
	require.Contains(t, body, `"supported_models"`)
}

func TestUserAvailableChannel_FieldWhitelist(t *testing.T) {
	// 通过序列化 userAvailableChannel 结构体验证响应形状：
	// 只有 name / description / platforms；不含管理端字段。
	row := userAvailableChannel{
		Name:        "ch",
		Description: "d",
		Platforms: []userChannelPlatformSection{
			{
				Platform:        "anthropic",
				Groups:          []userAvailableGroup{{ID: 1, Name: "g1", Platform: "anthropic"}},
				SupportedModels: []userSupportedModel{},
			},
		},
	}
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{"id", "status", "billing_model_source", "restrict_models"} {
		_, exists := decoded[key]
		require.Falsef(t, exists, "user DTO must not expose %q", key)
	}
	for _, key := range []string{"name", "description", "platforms"} {
		_, exists := decoded[key]
		require.Truef(t, exists, "user DTO must expose %q", key)
	}

	// 验证 section 的字段（platform / groups / supported_models）。
	rawSection, err := json.Marshal(row.Platforms[0])
	require.NoError(t, err)
	var sectionDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawSection, &sectionDecoded))
	for _, key := range []string{"platform", "groups", "supported_models"} {
		_, exists := sectionDecoded[key]
		require.Truef(t, exists, "platform section must expose %q", key)
	}

	// Group DTO 暴露区分专属/公开、订阅类型、默认倍率和高峰倍率规则所需的字段，
	// 前端据此渲染 GroupBadge 并与 API 密钥页保持一致的视觉。
	rawGroup, err := json.Marshal(row.Platforms[0].Groups[0])
	require.NoError(t, err)
	var groupDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawGroup, &groupDecoded))
	for _, key := range []string{"id", "name", "platform", "subscription_type", "rate_multiplier", "peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier", "is_exclusive"} {
		_, exists := groupDecoded[key]
		require.Truef(t, exists, "group DTO must expose %q", key)
	}

	// pricing interval 白名单：不应暴露 id / sort_order。
	pricing := toUserPricing(&service.ChannelModelPricing{
		BillingMode: service.BillingModeToken,
		Intervals: []service.PricingInterval{
			{ID: 7, MinTokens: 0, MaxTokens: nil, SortOrder: 3},
		},
	})
	require.NotNil(t, pricing)
	require.Len(t, pricing.Intervals, 1)
	rawIv, err := json.Marshal(pricing.Intervals[0])
	require.NoError(t, err)
	var ivDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawIv, &ivDecoded))
	for _, key := range []string{"id", "pricing_id", "sort_order"} {
		_, exists := ivDecoded[key]
		require.Falsef(t, exists, "user pricing interval must not expose %q", key)
	}
}

func TestBuildPlatformSections_GroupsByPlatform(t *testing.T) {
	// 一个渠道横跨 anthropic / openai / 空平台：应该生成 2 个 section，
	// 按 platform 字母序排序，各自 groups 和 supported_models 只含同平台条目。
	ch := service.AvailableChannel{
		Name: "ch",
		SupportedModels: []service.SupportedModel{
			{Name: "claude-sonnet-4-6", Platform: "anthropic"},
			{Name: "gpt-4o", Platform: "openai"},
		},
	}
	visible := []userAvailableGroup{
		{ID: 1, Name: "g-openai", Platform: "openai"},
		{ID: 2, Name: "g-ant", Platform: "anthropic"},
		{ID: 3, Name: "g-empty", Platform: ""},
	}
	sections := buildPlatformSections(ch, visible)
	require.Len(t, sections, 2)
	require.Equal(t, "anthropic", sections[0].Platform)
	require.Equal(t, "openai", sections[1].Platform)
	require.Len(t, sections[0].Groups, 1)
	require.Equal(t, int64(2), sections[0].Groups[0].ID)
	require.Len(t, sections[0].SupportedModels, 1)
	require.Equal(t, "claude-sonnet-4-6", sections[0].SupportedModels[0].Name)
}

func TestBuildPlatformSections_IncludesGroupModelsListConfig(t *testing.T) {
	ch := service.AvailableChannel{
		Name: "ch",
		SupportedModels: []service.SupportedModel{
			{Name: "channel-model", Platform: "openai"},
		},
	}
	visible := []userAvailableGroup{
		{
			ID:       1,
			Name:     "g-openai",
			Platform: "openai",
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"group-only-model", "channel-model", "  "},
			},
		},
		{
			ID:       2,
			Name:     "g-anthropic",
			Platform: "anthropic",
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"claude-from-group"},
			},
		},
	}

	sections := buildPlatformSections(ch, visible)

	require.Len(t, sections, 2)
	require.Equal(t, "anthropic", sections[0].Platform)
	require.Equal(t, []string{"claude-from-group"}, supportedModelNames(sections[0].SupportedModels))
	require.Equal(t, "openai", sections[1].Platform)
	require.Equal(t, []string{"channel-model", "group-only-model"}, supportedModelNames(sections[1].SupportedModels))
}

func TestApplyPricingFallbackToSections_FillsGroupOnlyModelPricing(t *testing.T) {
	inputPrice := 0.000004
	channelSvc := service.NewChannelService(
		&publicChannelRepoStub{},
		&publicGroupRepoStub{},
		nil,
		newHandlerTestPricingService(map[string]*service.ModelPriceEntry{
			"group-only-model": {Mode: "chat", InputCostPerToken: inputPrice},
		}),
	)
	sections := []userChannelPlatformSection{{
		Platform: "openai",
		SupportedModels: []userSupportedModel{{
			Name:     "group-only-model",
			Platform: "openai",
		}},
	}}

	applyPricingFallbackToSections(channelSvc, sections)

	model := sections[0].SupportedModels[0]
	require.NotNil(t, model.Pricing)
	require.NotNil(t, model.Pricing.InputPrice)
	require.InDelta(t, inputPrice, *model.Pricing.InputPrice, 1e-12)
}

func supportedModelNames(models []userSupportedModel) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	return names
}

type publicChannelRepoStub struct {
	channels []service.Channel
}

func (s *publicChannelRepoStub) Create(context.Context, *service.Channel) error { return nil }
func (s *publicChannelRepoStub) GetByID(context.Context, int64) (*service.Channel, error) {
	return nil, nil
}
func (s *publicChannelRepoStub) Update(context.Context, *service.Channel) error { return nil }
func (s *publicChannelRepoStub) Delete(context.Context, int64) error            { return nil }
func (s *publicChannelRepoStub) List(context.Context, pagination.PaginationParams, string, string) ([]service.Channel, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *publicChannelRepoStub) ListAll(context.Context) ([]service.Channel, error) {
	return s.channels, nil
}
func (s *publicChannelRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (s *publicChannelRepoStub) ExistsByNameExcluding(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (s *publicChannelRepoStub) GetGroupIDs(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (s *publicChannelRepoStub) SetGroupIDs(context.Context, int64, []int64) error {
	return nil
}
func (s *publicChannelRepoStub) GetChannelIDByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *publicChannelRepoStub) GetGroupsInOtherChannels(context.Context, int64, []int64) ([]int64, error) {
	return nil, nil
}
func (s *publicChannelRepoStub) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return nil, nil
}
func (s *publicChannelRepoStub) ListModelPricing(context.Context, int64) ([]service.ChannelModelPricing, error) {
	return nil, nil
}
func (s *publicChannelRepoStub) CreateModelPricing(context.Context, *service.ChannelModelPricing) error {
	return nil
}
func (s *publicChannelRepoStub) UpdateModelPricing(context.Context, *service.ChannelModelPricing) error {
	return nil
}
func (s *publicChannelRepoStub) DeleteModelPricing(context.Context, int64) error {
	return nil
}
func (s *publicChannelRepoStub) ReplaceModelPricing(context.Context, int64, []service.ChannelModelPricing) error {
	return nil
}

type publicGroupRepoStub struct {
	groups []service.Group
}

func (s *publicGroupRepoStub) Create(context.Context, *service.Group) error { return nil }
func (s *publicGroupRepoStub) GetByID(context.Context, int64) (*service.Group, error) {
	return nil, nil
}
func (s *publicGroupRepoStub) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return nil, nil
}
func (s *publicGroupRepoStub) Update(context.Context, *service.Group) error { return nil }
func (s *publicGroupRepoStub) Delete(context.Context, int64) error          { return nil }
func (s *publicGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (s *publicGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *publicGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *publicGroupRepoStub) ListActive(context.Context) ([]service.Group, error) {
	return s.groups, nil
}
func (s *publicGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (s *publicGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (s *publicGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *publicGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *publicGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (s *publicGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (s *publicGroupRepoStub) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

type stubGatewayModelsProvider struct {
	modelsByPlatform map[string][]string
}

func (s stubGatewayModelsProvider) GetAvailableModels(_ context.Context, _ *int64, platform string) []string {
	return s.modelsByPlatform[platform]
}

func newHandlerTestPricingService(data map[string]*service.ModelPriceEntry) *service.PricingService {
	return service.NewPricingServiceForTest(data)
}

// stubBillingFallbackProvider 用于测试 handler 层 billing fallback 二次查询。
type stubBillingFallbackProvider struct {
	data map[string]*service.ModelPricing
}

func (s stubBillingFallbackProvider) GetFallbackPricing(model string) *service.ModelPricing {
	if s.data == nil {
		return nil
	}
	return s.data[strings.ToLower(strings.TrimSpace(model))]
}
