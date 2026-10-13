package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoModelCatalogGroupStub struct {
	GroupRepository
	group *Group
	err   error
}

func (s *videoModelCatalogGroupStub) GetByID(context.Context, int64) (*Group, error) {
	return s.group, s.err
}

func videoModelCatalogAPIKey(groupID int64) *APIKey {
	id := groupID
	return &APIKey{ID: 7, UserID: 11, GroupID: &id}
}

func videoModelCatalogGroup(platform string) *Group {
	return &Group{ID: 42, Platform: platform, Status: StatusActive}
}

// soraVideoPricing 是一份"运营把 sora 系配全了"的模型价格：分辨率覆盖能力表的每一个
// 尺寸，时长档位与能力表一致。这样别的用例的断言才落在解析逻辑上，而不是价目本身。
func soraVideoPricing() *VideoPricingConfig {
	return &VideoPricingConfig{
		Version: VideoPricingConfigVersion, Enabled: true, Currency: ModelPriceCurrencyUSD,
		Defaults: VideoPricingDefaults{Resolution: "720p"},
		Resolutions: map[string]VideoResolutionSpec{
			"720p":  {Sizes: []string{"720x1280", "1280x720"}},
			"1080p": {Sizes: []string{"1080x1920", "1920x1080"}},
			"1792p": {Sizes: []string{"1024x1792", "1792x1024"}},
		},
		Rules: []VideoPricingRule{{
			Key: "per-second", BillingUnit: VideoBillingUnitSecond, UnitPriceUSD: 0.1,
			Conditions: VideoPricingConditions{Seconds: []int{4, 8, 12, 16, 20}},
		}},
	}
}

func videoPricedCatalogEntry(profile *VideoPricingConfig) *ModelPriceEntry {
	return &ModelPriceEntry{VideoPricing: profile, PricePresenceKnown: true, TokenPricingAbsent: true}
}

// defaultVideoCatalogPricing 模拟"运营已经在模型价格里把两侧的内置模型都配好了"。
// 清单不再从能力表直出，所以不配价就什么都列不出来——多数用例并不想测这件事。
func defaultVideoCatalogPricing() map[string]*ModelPriceEntry {
	catalog := make(map[string]*ModelPriceEntry, 8)
	for _, model := range []string{
		OpenAIVideoModelSora2, "sora-2-2025-10-06", "sora-2-2025-12-08",
		OpenAIVideoModelSora2Pro, "sora-2-pro-2025-10-06",
	} {
		catalog[model] = videoPricedCatalogEntry(soraVideoPricing())
	}
	for _, model := range []string{ByteDanceVideoModelSeedance10Lite, ByteDanceVideoModelSeedance10Pro} {
		catalog[model] = videoPricedCatalogEntry(seedanceVideoPricing())
	}
	return catalog
}

// newVideoModelCatalogService 装配 ListVideoModels 真正读到的四样东西：配置、分组仓储、
// provider 注册表、模型价格。渠道价按需由 withVideoCatalogChannel 追加，其余字段留零值，
// 避免测试悄悄依赖别的协作者。
func newVideoModelCatalogService(group *Group, providers ...VideoProvider) *VideoTaskService {
	return newVideoModelCatalogServiceWithCatalog(group, defaultVideoCatalogPricing(), providers...)
}

func newVideoModelCatalogServiceWithCatalog(
	group *Group, catalog map[string]*ModelPriceEntry, providers ...VideoProvider,
) *VideoTaskService {
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(catalog)
	return &VideoTaskService{
		groups:    &videoModelCatalogGroupStub{group: group},
		providers: NewVideoProviderRegistry(providers...),
		pricing:   NewVideoPricingResolver(nil, pricing),
		cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
			Enabled: true, CreationEnabled: true,
		}}},
	}
}

// withVideoCatalogChannel 给分组挂一个已激活的渠道及其定价行。缓存直接预热，
// 因为 loadCache 只有在 loadedAt 过期时才会去打仓储。
func withVideoCatalogChannel(
	service *VideoTaskService, groupID int64, platform string, rows ...ChannelModelPricing,
) {
	channel := &Channel{ID: 5, Status: StatusActive, ModelPricing: rows}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[groupID] = channel
	cache.groupPlatform[groupID] = platform
	expandPricingToCache(cache, channel, groupID, platform)
	channels := &ChannelService{}
	channels.cache.Store(cache)
	service.channels = channels
}

func videoModelIDs(response *VideoModelsResponse) []string {
	ids := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		ids = append(ids, model.ID)
	}
	return ids
}

func videoModelByID(t *testing.T, response *VideoModelsResponse, id string) VideoModel {
	t.Helper()
	for _, model := range response.Data {
		if model.ID == id {
			return model
		}
	}
	require.FailNowf(t, "model not listed", "id=%s", id)
	return VideoModel{}
}

func arkVideoModelIDs(response *ArkVideoModelsResponse) []string {
	ids := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		ids = append(ids, model.ID)
	}
	return ids
}

func arkVideoModelByID(t *testing.T, response *ArkVideoModelsResponse, id string) ArkVideoModel {
	t.Helper()
	for _, model := range response.Data {
		if model.ID == id {
			return model
		}
	}
	require.FailNowf(t, "ark model not listed", "id=%s", id)
	return ArkVideoModel{}
}

func byteDanceCatalogProvider() VideoProvider {
	return &videoProviderStub{name: VideoProviderByteDance, capabilities: videoCapabilitiesPointer(DefaultByteDanceVideoCapabilities())}
}

// TestListVideoModelsResolvesCanonicalModelSideTables 钉死别名解析：带日期的模型名
// 是可路由的真实模型，必须各自成条目，且档位要按 canonical 键查表。
// 一旦退化成"按前缀折叠"或"直接用别名查表"，sora-2-pro-2025-10-06 就会消失，
// 或者拿到 sora-2 的四个尺寸而不是 sora-2-pro 的六个。
func TestListVideoModelsResolvesCanonicalModelSideTables(t *testing.T) {
	service := newVideoModelCatalogService(
		videoModelCatalogGroup(PlatformOpenAI),
		&videoProviderStub{},
	)

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Equal(t, "list", response.Object)

	for _, model := range response.Data {
		require.Equal(t, videoModelObject, model.Object)
		require.Equal(t, VideoProviderOpenAI, model.Provider)
	}
	require.Equal(t, []string{
		"sora-2", "sora-2-2025-10-06", "sora-2-2025-12-08", "sora-2-pro", "sora-2-pro-2025-10-06",
	}, videoModelIDs(response))

	dated := videoModelByID(t, response, "sora-2-pro-2025-10-06")
	require.Equal(t, OpenAIVideoModelSora2Pro, dated.CanonicalModel)
	require.Equal(t, []int{4, 8, 12, 16, 20}, dated.Seconds.Values)
	require.Equal(t, 4, dated.Seconds.Default)
	// 顺序不是随手排的：价目的默认分辨率排最前，其余按分辨率名排序。map 迭代序不稳定，
	// 不定序会让同一份配置每次返回不同的画幅列表。
	require.Equal(t, []string{
		"720x1280", "1280x720", "1080x1920", "1920x1080", "1024x1792", "1792x1024",
	}, dated.Sizes.Values)
	require.Equal(t, "720x1280", dated.Sizes.Default)
	require.False(t, dated.IsDefault)

	base := videoModelByID(t, response, "sora-2-2025-10-06")
	require.Equal(t, OpenAIVideoModelSora2, base.CanonicalModel)
	require.Len(t, base.Sizes.Values, 4)
	require.NotContains(t, base.Sizes.Values, "1920x1080")

	require.True(t, videoModelByID(t, response, OpenAIVideoModelSora2).IsDefault)
	require.Equal(t,
		[]string{"spritesheet", "thumbnail", "video"},
		videoModelByID(t, response, OpenAIVideoModelSora2).ContentVariants,
	)
}

// TestListVideoModelsSkipsArkProviders：五家平台只能按 Ark 形态提交，列进 /v1/videos/models
// 只会引导用户撞 404；它们的目录在 ListArkVideoModels。
func TestListVideoModelsSkipsArkProviders(t *testing.T) {
	service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformByteDance), byteDanceCatalogProvider())

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Empty(t, response.Data)

	ark, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Equal(t, "list", ark.Object)
	require.Equal(t, []string{ByteDanceVideoModelSeedance10Lite, ByteDanceVideoModelSeedance10Pro}, arkVideoModelIDs(ark))
}

// TestListArkVideoModelsDescribesArkDimensions：Ark 维度的目录用分辨率档位 + 比例表达画幅，
// 时长允许 -1，frames 走 25+4n，并且把 content[] 角色上限与开关一并交给前端。
func TestListArkVideoModelsDescribesArkDimensions(t *testing.T) {
	// 开关、frames 与角色约束来自 provider 的 ArkModelSpec 自描述，所以这里注册真 provider。
	service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformByteDance), NewByteDanceVideoProvider(&openAIVideoHTTPStub{}, nil))

	response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Len(t, response.Data, 2)
	for _, model := range response.Data {
		require.Equal(t, VideoProviderByteDance, model.Provider, "id=%s", model.ID)
		require.Equal(t, videoModelObject, model.Object, "id=%s", model.ID)
		// 价目没声明 conditions.seconds，档位回落到能力表（Ark 文档：1.0 系列 2 到 12 秒）；
		// 1.0 系列不支持智能时长与 frames，目录照实声明。
		require.Equal(t, byteDanceSeedance10Durations(), model.Durations.Values, "id=%s", model.ID)
		require.Equal(t, 5, model.Durations.Default, "id=%s", model.ID)
		require.False(t, model.Durations.AllowSmart, "id=%s", model.ID)
		require.Nil(t, model.Frames, "id=%s", model.ID)
		require.Equal(t, ArkVideoModelFlags{Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true, ReturnLastFrame: true}, model.Flags)
		require.Empty(t, model.ProviderOptions, "Ark has no vendor-specific provider_options")
	}
	pro := arkVideoModelByID(t, response, ByteDanceVideoModelSeedance10Pro)
	require.True(t, pro.IsDefault)
	require.False(t, arkVideoModelByID(t, response, ByteDanceVideoModelSeedance10Lite).IsDefault)
	// seedanceVideoPricing 只配了 480p / 720p：分辨率按价目收窄，默认档取价目 defaults。
	require.Equal(t, &ArkVideoModelChoices{Values: []string{"480p", "720p"}, Default: "480p"}, pro.Resolutions)
	require.Equal(t, []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", ArkVideoRatioAdaptive}, pro.Ratios.Values)
	require.Equal(t, "16:9", pro.Ratios.Default)
	roles := pro.ContentRoles
	require.Len(t, roles, 5)
	require.Equal(t, ArkVideoContentRole{Max: 1, Accepts: []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI}}, roles[string(VideoInputRoleFirstFrame)])
	require.Equal(t, []string{string(VideoInputRoleFirstFrame)}, roles[string(VideoInputRoleLastFrame)].Requires)
	require.Equal(t, 9, roles[string(VideoInputRoleTaggedReferenceImage)].Max)
	require.Equal(t, ArkVideoContentRole{Max: 3, Accepts: []string{videoReferenceAcceptsHTTPS}}, roles[string(VideoInputRoleReferenceVideo)])
	require.Equal(t, ArkVideoContentRole{Max: 3, Accepts: []string{videoReferenceAcceptsHTTPS}}, roles[string(VideoInputRoleReferenceAudio)])
}

// TestListArkVideoModelsUsesVendorAdapterSpecs：三家聚合平台没有注册 provider 实例时，目录
// 仍能从静态适配器档案推出开关与角色，不会退化成"只有能力表角色"的保守描述。
func TestListArkVideoModelsUsesVendorAdapterSpecs(t *testing.T) {
	const model = "bytedance:seedance@2.0"
	runware := newVideoVendorProvider(newRunwareVideoAdapter(), nil, nil)
	service := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformRunware),
		map[string]*ModelPriceEntry{model: videoPricedCatalogEntry(seedanceVideoPricing())},
		runware,
	)

	response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	entry := arkVideoModelByID(t, response, model)
	require.Equal(t, VideoProviderRunware, entry.Provider)
	require.Equal(t, ArkVideoModelFlags{Seed: true, GenerateAudio: true}, entry.Flags)
	require.Equal(t, []ArkVideoModelOption{{Name: "negativePrompt"}}, entry.ProviderOptions)
	require.Nil(t, entry.Frames, "frames are Ark-native only")
	require.False(t, entry.Durations.AllowSmart)
	// 档案的连续区间在能力表里展开成离散档位。
	require.Equal(t, 4, entry.Durations.Values[0])
	require.Equal(t, 15, entry.Durations.Values[len(entry.Durations.Values)-1])
	require.Equal(t, 5, entry.Durations.Default)
	require.Equal(t, []string{"480p", "720p"}, entry.Resolutions.Values, "narrowed to the priced tiers")
	require.Contains(t, entry.Ratios.Values, ArkVideoRatioAdaptive, "image-capable models accept adaptive")
	require.Equal(t, "16:9", entry.Ratios.Default)
	require.Len(t, entry.ContentRoles, 2)
}

// TestListVideoModelsListsOnlyWhatIsPriced 是本组测试的主张：清单由运营在模型价格 /
// 渠道价 / 分组价里配过的东西决定，而不是内置能力表。内置能力表描述的是"这个 build
// 认得哪些模型的请求约束"，跟"这个分组买得到它吗"是两回事——直出能力表会让用户写完
// prompt 才撞上 ErrVideoPricingMissing。
//
// 三个来源与 VideoPricingResolver.Resolve 的优先级链一一对应，任何一条单独成立都足以
// 让模型进选择器。
func TestListVideoModelsListsOnlyWhatIsPriced(t *testing.T) {
	t.Run("nothing priced yields an empty list", func(t *testing.T) {
		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformOpenAI), nil, &videoProviderStub{},
		)
		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Empty(t, response.Data, "能力表不再是清单来源，空列表才是诚实的答案")
	})

	t.Run("group pricing alone lists the model", func(t *testing.T) {
		group := videoModelCatalogGroup(PlatformOpenAI)
		group.ModelPricing = []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{OpenAIVideoModelSora2}, BillingMode: BillingModeVideo,
		}}
		service := newVideoModelCatalogServiceWithCatalog(group, nil, &videoProviderStub{})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Equal(t, []string{OpenAIVideoModelSora2}, videoModelIDs(response))
	})

	t.Run("a non-video group pricing row does not", func(t *testing.T) {
		group := videoModelCatalogGroup(PlatformOpenAI)
		group.ModelPricing = []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{OpenAIVideoModelSora2}, BillingMode: BillingModeToken,
		}}
		service := newVideoModelCatalogServiceWithCatalog(group, nil, &videoProviderStub{})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Empty(t, response.Data)
	})

	t.Run("a group pricing row for another platform does not", func(t *testing.T) {
		group := videoModelCatalogGroup(PlatformOpenAI)
		group.ModelPricing = []ChannelModelPricing{{
			Platform: PlatformByteDance, Models: []string{OpenAIVideoModelSora2}, BillingMode: BillingModeVideo,
		}}
		service := newVideoModelCatalogServiceWithCatalog(group, nil, &videoProviderStub{})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Empty(t, response.Data)
	})

	t.Run("group pricing wildcards are dropped", func(t *testing.T) {
		// findVideoGroupPricing 只做精确匹配，列出来提交时也匹配不到分组价。
		group := videoModelCatalogGroup(PlatformOpenAI)
		group.ModelPricing = []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{"sora-2*"}, BillingMode: BillingModeVideo,
		}}
		service := newVideoModelCatalogServiceWithCatalog(group, nil, &videoProviderStub{})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Empty(t, response.Data)
	})

	t.Run("channel pricing alone lists the model", func(t *testing.T) {
		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformOpenAI), nil, &videoProviderStub{},
		)
		withVideoCatalogChannel(service, 42, PlatformOpenAI, ChannelModelPricing{
			Platform: PlatformOpenAI, Models: []string{"sora-2-2025-12-08"}, BillingMode: BillingModeVideo,
		})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Equal(t, []string{"sora-2-2025-12-08"}, videoModelIDs(response))
	})

	t.Run("channel wildcards expand against the known universe", func(t *testing.T) {
		// 缓存里存的是前缀而不是模型名（expandPricingToCache 把 "foo*" 拆成 prefix="foo"），
		// 只有调用方知道该拿哪个候选域去撞它。
		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformOpenAI), nil, &videoProviderStub{},
		)
		withVideoCatalogChannel(service, 42, PlatformOpenAI, ChannelModelPricing{
			Platform: PlatformOpenAI, Models: []string{"sora-2-pro*"}, BillingMode: BillingModeVideo,
		})

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Equal(t, []string{"sora-2-pro", "sora-2-pro-2025-10-06"}, videoModelIDs(response))
	})
}

// TestListVideoModelsDoesNotLeakAnotherPlatformsModels：模型价格的基础目录是平台无关的，
// 一条 sora-2 的 video_pricing 对火山方舟分组同样可见。没有归属过滤，Ark 的用户会在选择器
// 里看到 sora-2，选中即必挂。
func TestListVideoModelsDoesNotLeakAnotherPlatformsModels(t *testing.T) {
	service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformByteDance), byteDanceCatalogProvider())

	response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Equal(t, []string{
		ByteDanceVideoModelSeedance10Lite, ByteDanceVideoModelSeedance10Pro,
	}, arkVideoModelIDs(response))

	// 反过来，OpenAI 分组的 Ark 目录是空的：sora 不按 Ark 形态提交。
	openAI := newVideoModelCatalogService(videoModelCatalogGroup(PlatformOpenAI), &videoProviderStub{})
	response, err = openAI.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Empty(t, response.Data)
}

// TestListVideoModelsKeepsUnclaimedCustomModels：两侧能力表都不认识的模型名要放行。
// 那是自定义兼容上游——ValidateVideoCreateCapabilities 对未知模型不约束档位，提交得了，
// 只是内置目录覆盖不到，只能靠价目声明。把它一并过滤掉等于把一条活路堵死。
func TestListVideoModelsKeepsUnclaimedCustomModels(t *testing.T) {
	profile := soraVideoPricing()
	profile.Rules[0].Conditions.Seconds = []int{6}
	service := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformByteDance),
		map[string]*ModelPriceEntry{"custom-video-compat": videoPricedCatalogEntry(profile)},
		byteDanceCatalogProvider(),
	)

	response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Equal(t, []string{"custom-video-compat"}, arkVideoModelIDs(response))
	require.Equal(t, []int{6}, response.Data[0].Durations.Values)
	require.False(t, response.Data[0].Durations.AllowSmart, "an unknown model is not declared smart-duration capable")
	require.Nil(t, response.Data[0].Resolutions, "the capability snapshot knows no tiers for it")
	require.Equal(t, VideoProviderByteDance, response.Data[0].Provider)
}

// TestListVideoModelsHonorsPricedProviderConditions：价目自己用 conditions.providers
// 指名道姓时以价目为准，这是运营给自定义模型标注归属的唯一手段。
func TestListVideoModelsHonorsPricedProviderConditions(t *testing.T) {
	catalog := func() map[string]*ModelPriceEntry {
		profile := soraVideoPricing()
		profile.Rules[0].Conditions.Seconds = []int{6}
		profile.Rules[0].Conditions.Providers = []string{VideoProviderOpenAI}
		return map[string]*ModelPriceEntry{"custom-video-compat": videoPricedCatalogEntry(profile)}
	}

	byteDance := newVideoModelCatalogServiceWithCatalog(videoModelCatalogGroup(PlatformByteDance), catalog(), byteDanceCatalogProvider())
	arkResponse, err := byteDance.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Empty(t, arkResponse.Data)

	openAI := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformOpenAI), catalog(), &videoProviderStub{},
	)
	response, err := openAI.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.Equal(t, []string{"custom-video-compat"}, videoModelIDs(response))
}

// TestListVideoModelsNarrowsDurationsAndSizesToThePricedProfile：运营只卖 4/8 秒的
// 720p，就不该让用户选到 20 秒的 1920x1080 再在提交时撞 ErrVideoPricingRuleMissing。
func TestListVideoModelsNarrowsDurationsAndSizesToThePricedProfile(t *testing.T) {
	profile := soraVideoPricing()
	profile.Rules[0].Conditions.Seconds = []int{8, 4}
	profile.Resolutions = map[string]VideoResolutionSpec{"720p": {Sizes: []string{"720x1280", "1280x720"}}}
	service := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformOpenAI),
		map[string]*ModelPriceEntry{OpenAIVideoModelSora2: videoPricedCatalogEntry(profile)},
		&videoProviderStub{},
	)

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	model := videoModelByID(t, response, OpenAIVideoModelSora2)
	require.Equal(t, []int{4, 8}, model.Seconds.Values)
	require.Equal(t, 4, model.Seconds.Default)
	require.Equal(t, []string{"720x1280", "1280x720"}, model.Sizes.Values)
	require.Equal(t, "720x1280", model.Sizes.Default)
}

// TestListVideoModelsFallsBackWhenThePricedSizesAreForeign：价目配的是另一套档位（只有
// 480p，而 sora 只认 720x1280 那几档）时，交集为空。此时以能力表为准——交出空列表会让
// 画幅控件整段消失，而能力表的档位至少是提交时会被接受的那些。
func TestListVideoModelsFallsBackWhenThePricedSizesAreForeign(t *testing.T) {
	profile := soraVideoPricing()
	profile.Defaults.Resolution = "480p"
	profile.Resolutions = map[string]VideoResolutionSpec{"480p": {Sizes: []string{"864x480", "480x864"}}}
	service := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformOpenAI),
		map[string]*ModelPriceEntry{OpenAIVideoModelSora2: videoPricedCatalogEntry(profile)},
		&videoProviderStub{},
	)

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	model := videoModelByID(t, response, OpenAIVideoModelSora2)
	require.Equal(t, []string{"720x1280", "1280x720", "1024x1792", "1792x1024"}, model.Sizes.Values)
	require.Equal(t, "720x1280", model.Sizes.Default)
}

// TestListVideoModelsKeepsPricedModelsWithoutDeclaredSeconds 是用户报障的回归测试：
// 运营在模型价格里给 doubao-seedance-2.0-mini-480p 配好了价，选择器却一片空白，页面只
// 剩一句「当前密钥所属分组没有可用的视频模型」。
//
// 病根是按秒/按 token 计价的规则本就不必逐条列 conditions.seconds——不写即「任意时长都按
// 这条规则计价」——而这里以前落空即把整个模型丢掉。时长说不准不是不能卖的理由：回落到
// 本家上游的自由区间，模型必须还在列表里。
//
// 回落必须是区间而不是"同族已知模型档位的并集"：并集会把 seedance 2.0 冒充成 5/10 两档，
// 而它实际收 4~15 秒任意整数。拿另一个模型的离散档位充当这个模型的能力，比说"说不准"
// 更糟——用户没有任何办法发现这是猜的。
func TestListVideoModelsKeepsPricedModelsWithoutDeclaredSeconds(t *testing.T) {
	const arkCustomModel = "doubao-seedance-2.0-mini-480p"

	t.Run("bytedance custom model falls back to the ark free range", func(t *testing.T) {
		profile := seedanceVideoPricing()
		// 夹具本身就得是"一条 seconds 都没列"，否则这个用例测的是别的东西。
		require.Empty(t, videoPricedSeconds(profile))

		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformByteDance),
			map[string]*ModelPriceEntry{arkCustomModel: videoPricedCatalogEntry(profile)},
			byteDanceCatalogProvider(),
		)

		response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		require.Equal(t, []string{arkCustomModel}, arkVideoModelIDs(response))
		model := arkVideoModelByID(t, response, arkCustomModel)
		require.Empty(t, model.Durations.Values)
		require.Equal(t, 4, model.Durations.Min)
		require.Equal(t, 15, model.Durations.Max)
		require.Equal(t, 4, model.Durations.Default)
	})

	t.Run("declared ladders stay discrete", func(t *testing.T) {
		service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformByteDance), byteDanceCatalogProvider())

		response, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		model := arkVideoModelByID(t, response, ByteDanceVideoModelSeedance10Pro)
		require.Equal(t, byteDanceSeedance10Durations(), model.Durations.Values)
		require.Equal(t, 5, model.Durations.Default)
		require.Zero(t, model.Durations.Min)
		require.Zero(t, model.Durations.Max)
	})

	t.Run("openai custom model falls back to the sora free range", func(t *testing.T) {
		profile := soraVideoPricing()
		profile.Rules[0].Conditions.Seconds = nil
		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformOpenAI),
			map[string]*ModelPriceEntry{"mystery-video": videoPricedCatalogEntry(profile)},
			&videoProviderStub{},
		)

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		model := videoModelByID(t, response, "mystery-video")
		require.Empty(t, model.Seconds.Values)
		require.Equal(t, 4, model.Seconds.Min)
		require.Equal(t, 20, model.Seconds.Max)
	})

	t.Run("silent everywhere still lists the model", func(t *testing.T) {
		profile := soraVideoPricing()
		profile.Rules[0].Conditions.Seconds = nil
		service := newVideoModelCatalogServiceWithCatalog(
			videoModelCatalogGroup(PlatformOpenAI),
			map[string]*ModelPriceEntry{"mystery-video": videoPricedCatalogEntry(profile)},
			&videoProviderStub{capabilities: &VideoCapabilities{}},
		)

		response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
		require.NoError(t, err)
		model := videoModelByID(t, response, "mystery-video")
		// nil 的意思是"这一档我们说不准"，前端据此渲染自由输入；不是丢弃模型的理由。
		require.Nil(t, model.Seconds)
	})
}

// TestListVideoModelsCompositeReturnsUnionInFixedOrder：composite 分组的用户
// 无从预知请求落到哪一侧，返回空或报错会让页面直接不可用。同一个模型 id 只能出现一次，
// 否则前端选择器会拿到重复的 key。
func TestListVideoModelsCompositeReturnsUnionInFixedOrder(t *testing.T) {
	service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformComposite), &videoProviderStub{}, byteDanceCatalogProvider())

	// OpenAI 形态的目录只剩 sora 系；Ark 形态的目录只有火山方舟。两张表互不混用。
	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	for _, model := range response.Data {
		require.Equal(t, VideoProviderOpenAI, model.Provider, "id=%s", model.ID)
	}
	require.Equal(t, []string{
		"sora-2", "sora-2-2025-10-06", "sora-2-2025-12-08", "sora-2-pro", "sora-2-pro-2025-10-06",
	}, videoModelIDs(response))

	ark, err := service.ListArkVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	for _, model := range ark.Data {
		require.Equal(t, VideoProviderByteDance, model.Provider, "id=%s", model.ID)
	}
	require.Equal(t, []string{ByteDanceVideoModelSeedance10Lite, ByteDanceVideoModelSeedance10Pro}, arkVideoModelIDs(ark))
}

func TestListVideoModelsRejectsIneligibleCallers(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*VideoTaskService, **APIKey)
		wantErr error
	}{
		{
			name:    "video disabled",
			mutate:  func(s *VideoTaskService, _ **APIKey) { s.cfg.Gateway.Video.Enabled = false },
			wantErr: ErrVideoDisabled,
		},
		{
			// 创建关掉时早报错，好过让用户写完 prompt 再挨 403。
			name:    "creation disabled",
			mutate:  func(s *VideoTaskService, _ **APIKey) { s.cfg.Gateway.Video.CreationEnabled = false },
			wantErr: ErrVideoCreationDisabled,
		},
		{
			name:    "api key without group",
			mutate:  func(_ *VideoTaskService, key **APIKey) { *key = &APIKey{ID: 7, UserID: 11} },
			wantErr: ErrVideoPricingMissing,
		},
		{
			name: "group lookup fails",
			mutate: func(s *VideoTaskService, _ **APIKey) {
				s.groups = &videoModelCatalogGroupStub{err: errors.New("boom")}
			},
			wantErr: ErrVideoPricingMissing,
		},
		{
			name: "subscription group",
			mutate: func(s *VideoTaskService, _ **APIKey) {
				group := videoModelCatalogGroup(PlatformOpenAI)
				group.SubscriptionType = SubscriptionTypeSubscription
				s.groups = &videoModelCatalogGroupStub{group: group}
			},
			wantErr: ErrVideoSubscriptionUnsupported,
		},
		{
			name: "inactive group",
			mutate: func(s *VideoTaskService, _ **APIKey) {
				group := videoModelCatalogGroup(PlatformOpenAI)
				group.Status = StatusDisabled
				s.groups = &videoModelCatalogGroupStub{group: group}
			},
			wantErr: ErrVideoNoAccountAvailable,
		},
		{
			name: "platform without managed video provider",
			mutate: func(s *VideoTaskService, _ **APIKey) {
				s.groups = &videoModelCatalogGroupStub{group: videoModelCatalogGroup(PlatformAnthropic)}
			},
			wantErr: ErrVideoDisabled,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformOpenAI), &videoProviderStub{})
			apiKey := videoModelCatalogAPIKey(42)
			testCase.mutate(service, &apiKey)

			response, err := service.ListVideoModels(context.Background(), apiKey)
			require.ErrorIs(t, err, testCase.wantErr)
			require.Nil(t, response)
		})
	}
}

// TestListVideoModelsFallsBackToBuiltinCapabilities：价目只决定"卖不卖"，档位它没说的
// 部分回落到 provider.Capabilities()——只有它会在运行时目录不可读时给出内置快照，
// 那也正是提交校验会拿到的值。直接读裸目录会在目录冷/坏时返回空列表。
func TestListVideoModelsFallsBackToBuiltinCapabilities(t *testing.T) {
	profile := soraVideoPricing()
	profile.Rules[0].Conditions.Seconds = nil
	provider := NewOpenAIVideoProvider(nil, nil)
	provider.catalog = NewVideoCapabilityCatalog(&videoCapabilitySettingStub{err: errors.New("settings unavailable")})
	service := newVideoModelCatalogServiceWithCatalog(
		videoModelCatalogGroup(PlatformOpenAI),
		map[string]*ModelPriceEntry{OpenAIVideoModelSora2: videoPricedCatalogEntry(profile)},
		provider,
	)

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)
	require.NotEmpty(t, response.Data)
	require.Equal(t, []int{4, 8, 12, 16, 20}, videoModelByID(t, response, OpenAIVideoModelSora2).Seconds.Values)
}

// TestListVideoModelsReferenceSlotsMatchCompatibleReferenceLimits 是这组测试里
// 最值钱的一条：槽位上限必须等于 URL 通道自己的常量。
//
// 切勿改用 VideoCapabilities.MaxInputsByOperation——那描述的是多段二进制上传的份数
// （generate 下为 1），拿它驱动 URL 槽位会把 reference_images 悄悄卡到 1，让合法请求
// 被前端拦下且无从诊断。这条断言让常量与目录一起变，而不是让 UI 静静地误导用户。
func TestListVideoModelsReferenceSlotsMatchCompatibleReferenceLimits(t *testing.T) {
	service := newVideoModelCatalogService(videoModelCatalogGroup(PlatformOpenAI), &videoProviderStub{})

	response, err := service.ListVideoModels(context.Background(), videoModelCatalogAPIKey(42))
	require.NoError(t, err)

	slots := make(map[string]VideoModelReferenceInput)
	for _, slot := range videoModelByID(t, response, OpenAIVideoModelSora2).ReferenceInputs {
		slots[slot.Field] = slot
		require.Equal(t, openAIVideoMaxReferenceBytes, slot.MaxValueBytes, "field=%s", slot.Field)
		require.NotEmpty(t, slot.Accepts, "field=%s", slot.Field)
	}
	require.Equal(t, openAIVideoMaxReferenceImages, slots["reference_images"].Max)
	require.Equal(t, openAIVideoMaxReferenceVideos, slots["reference_videos"].Max)
	require.Equal(t, openAIVideoMaxReferenceAudios, slots["reference_audios"].Max)
	require.Equal(t, 1, slots["first_image_url"].Max)
	require.Equal(t, 1, slots["last_image_url"].Max)

	// image_url 必须在表里：它是"单图当起始画面"唯一通用的说法，也是火山方舟原生模式
	// 仅有的图生视频入口。reference_images 顶不上——那个字段带 reference_image 角色，
	// 表达的是风格/主体参考而非首帧。
	require.Equal(t, 1, slots["image_url"].Max)
	require.ElementsMatch(t,
		[]string{"first_image_url", "last_image_url"},
		slots["image_url"].ConflictsWith,
	)

	// 校验器对 video 一类关掉了 data 分支，别的槽位都接受 data URI。
	require.Equal(t, []string{videoReferenceAcceptsHTTPS}, slots["reference_videos"].Accepts)
	require.Contains(t, slots["reference_images"].Accepts, videoReferenceAcceptsDataURI)

	// 参考音频不能单独使用（API 文档 §4.3）：目录声明前置，Playground 据此禁用槽位；
	// 它本身不与任何槽位互斥，图 / 视频参考都能与它共存。
	require.ElementsMatch(t, []string{
		"image_url", "first_image_url", "last_image_url", "reference_images", "reference_videos",
	}, slots["reference_audios"].RequiresAny)
	require.Empty(t, slots["reference_audios"].ConflictsWith)

	// 首尾帧与 reference_images/videos 互斥。
	require.ElementsMatch(t,
		[]string{"image_url", "reference_images", "reference_videos"},
		slots["first_image_url"].ConflictsWith,
	)
}

// TestArkVideoModelResolutionsFollowThePricedProfile：运营只配了 480p/720p，就不该让用户
// 选到 1080p 再在提交时撞 ErrVideoPricingRuleMissing；但价目的分辨率名是自由字符串，透出
// 一个 Ark 不认的名字换来的是硬 400，所以只保留能力表词表里的名字。
func TestArkVideoModelResolutionsFollowThePricedProfile(t *testing.T) {
	caps := DefaultByteDanceVideoCapabilities()
	const model = ByteDanceVideoModelSeedance10Pro
	require.Equal(t, &ArkVideoModelChoices{Values: []string{"480p", "720p"}, Default: "480p"}, arkVideoModelResolutions(caps, model, seedanceVideoPricing()))

	// 顺序跟能力表词表走，不跟 map 迭代序或字典序走——字典序会把 1080p 排到 480p 前面。
	// 价目没给默认档时沿用能力表默认档（720p 不在交集里则退回第一个）。
	require.Equal(t, &ArkVideoModelChoices{Values: []string{"480p", "1080p"}, Default: "480p"}, arkVideoModelResolutions(caps, model, &VideoPricingConfig{
		Resolutions: map[string]VideoResolutionSpec{"1080p": {}, "480p": {}},
	}))
	require.Equal(t, &ArkVideoModelChoices{Values: []string{"720p", "1080p"}, Default: "720p"}, arkVideoModelResolutions(caps, model, &VideoPricingConfig{
		Resolutions: map[string]VideoResolutionSpec{"1080p": {}, "720p": {}},
	}))

	// 价目没说话，或者用的是另一套命名：退回完整词表。收窄到空只会让画幅控件失灵。
	full := &ArkVideoModelChoices{Values: []string{"480p", "720p", "1080p"}, Default: "720p"}
	require.Equal(t, full, arkVideoModelResolutions(caps, model, nil))
	require.Equal(t, full, arkVideoModelResolutions(caps, model, &VideoPricingConfig{
		Resolutions: map[string]VideoResolutionSpec{"cinema": {}},
	}))
	// 价目的默认档不在词表里时不采纳。
	require.Equal(t, full, arkVideoModelResolutions(caps, model, &VideoPricingConfig{Defaults: VideoPricingDefaults{Resolution: "cinema"}}))

	// 能力表对该模型没有分辨率维度：目录不编造。
	require.Nil(t, arkVideoModelResolutions(caps, "unknown-model", seedanceVideoPricing()))
	require.Nil(t, arkVideoModelResolutions(DefaultOpenAIVideoCapabilities(), OpenAIVideoModelSora2, soraVideoPricing()))
}

// TestListVideoModelsParameterValuesMatchProviderVocabulary 把输出的取值逐个回喂给
// 各自的校验函数/词表，防止这份静态表与真正的判定逻辑漂移。
func TestListVideoModelsParameterValuesMatchProviderVocabulary(t *testing.T) {
	// Ark 维度的目录取值必须落在 Ark 词表内，否则提交时会被 validateByteDanceArkRequest 拒掉。
	caps := DefaultByteDanceVideoCapabilities()
	for model := range caps.SupportedModels {
		for _, value := range caps.SupportedResolutions[model] {
			require.True(t, IsArkVideoResolution(value), "resolution=%s", value)
		}
		for _, value := range caps.SupportedRatios[model] {
			require.True(t, IsArkVideoRatio(value), "ratio=%s", value)
		}
	}

	for _, parameter := range openAIVideoModelParameters() {
		require.Equal(t, videoModelParameterInBody, parameter.In, "name=%s", parameter.Name)
		require.Len(t, parameter.Values, len(openAIVideoCompatibleRatios))
		for _, value := range parameter.Values {
			require.Contains(t, openAIVideoCompatibleRatios, value, "ratio=%s", value)
		}
		// 画幅提示与 size 同时出现会被上游直接拒，前端必须做成单选。
		require.Contains(t, parameter.ConflictsWith, "size", "name=%s", parameter.Name)
		// 有素材时画幅由素材决定，是静默忽略而不是报错——控件要置灰并说明。
		require.Contains(t, parameter.IgnoredWhenPresent, "image_url", "name=%s", parameter.Name)
	}
}
