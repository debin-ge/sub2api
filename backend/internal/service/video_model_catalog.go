package service

import (
	"context"
	"sort"
	"strings"
)

const videoModelObject = "video.model"

const (
	// videoModelParameterInBody 表示参数直接写在创建请求体上；
	// videoModelParameterInProviderOptions 表示必须包在 provider_options 里。
	// 两者不可互换：OpenAI 拒收任何 provider_options，而 Ark 的 resolution/ratio
	// 只认 provider_options，所以每个参数必须自报归属，前端才知道往哪儿放。
	videoModelParameterInBody            = "body"
	videoModelParameterInProviderOptions = "provider_options"

	videoReferenceAcceptsHTTPS   = "https"
	videoReferenceAcceptsDataURI = "data_uri"
)

// byteDanceVideoResolutionVocabulary / byteDanceVideoRatioVocabulary 是 Ark 认得的
// 全部取值，顺序即前端的渲染顺序——字典序会把 1080p 排到 480p 前面。
// 内容必须与 byteDanceSupportedResolution / byteDanceSupportedRatio 一致，同包测试
// 会把这里的每个取值回喂给那两个判定函数以防漂移。
var (
	byteDanceVideoResolutionVocabulary = []string{"480p", "720p", "1080p", "2k", "4k"}
	byteDanceVideoRatioVocabulary      = []string{"adaptive", "16:9", "9:16", "4:3", "3:4", "1:1", "21:9"}
)

// VideoModelSeconds 是某个模型可选的生成时长。
//
// 两种形态互斥：Values 非空表示只能从这几个离散档位里挑（Sora 的 4/8/12/16/20，
// 或运营在价目里逐条列出的档位）；Min/Max 非空表示区间内任意整数秒都可以，前端
// 应当渲染成滑杆而不是几个孤零零的按钮。
type VideoModelSeconds struct {
	Values  []int `json:"values,omitempty"`
	Min     int   `json:"min,omitempty"`
	Max     int   `json:"max,omitempty"`
	Default int   `json:"default,omitempty"`
}

// VideoModelSizes 是某个模型可选的画面尺寸。ByteDance 不通过 size 表达画幅，
// 因此这一段可能整体缺席——缺席本身就是"改用 ratio/resolution"的信号。
type VideoModelSizes struct {
	Values  []string `json:"values"`
	Default string   `json:"default,omitempty"`
}

// VideoModelParameter 描述一个可选的生成参数及其互斥关系。
type VideoModelParameter struct {
	Name   string   `json:"name"`
	In     string   `json:"in"`
	Values []string `json:"values,omitempty"`
	// ConflictsWith 里的字段与本参数同时出现会被上游直接拒绝（硬 400）。
	ConflictsWith []string `json:"conflicts_with,omitempty"`
	// IgnoredWhenPresent 里的字段一旦出现，本参数会被静默忽略而不是报错——
	// 画幅由素材本身决定。前端应当置灰并说明，而不是等一个不会来的错误。
	IgnoredWhenPresent []string `json:"ignored_when_present,omitempty"`
}

// VideoModelReferenceInput 描述一个以 URL（或 data URI）提交的参考素材槽位。
type VideoModelReferenceInput struct {
	// Field 是创建请求体里的 JSON 键名，不是内部的 input role。二者的映射是
	// 非对称的（同一个 video 字段在 edit 下是 source_video、在 character_create
	// 下是 character_clip），发 role 会逼前端硬编码一张翻译表。
	Field   string   `json:"field"`
	Kind    string   `json:"kind"`
	Max     int      `json:"max"`
	Accepts []string `json:"accepts"`
	// MaxValueBytes 是单条引用**字符串**的长度上限，不是被引用文件的大小上限。
	MaxValueBytes int      `json:"max_value_bytes,omitempty"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
	// RequiresAny 列出的字段至少要有一个非空，本槽位才能使用。
	RequiresAny []string `json:"requires_any,omitempty"`
}

// VideoModel 是一个可提交的视频模型及其请求约束。
type VideoModel struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	// Provider 让 composite 分组的调用方知道某个模型会落到哪一侧，
	// 进而决定渲染哪一套参数控件。
	Provider string `json:"provider"`
	// CanonicalModel 是能力表的查表键。带日期的别名（sora-2-pro-2025-10-06）
	// 与其规范名共享档位，此处直接给出解析结果，前端不必实现前缀折叠。
	CanonicalModel  string                     `json:"canonical_model"`
	IsDefault       bool                       `json:"is_default,omitempty"`
	Operations      []string                   `json:"operations,omitempty"`
	ContentVariants []string                   `json:"content_variants,omitempty"`
	Seconds         *VideoModelSeconds         `json:"seconds,omitempty"`
	Sizes           *VideoModelSizes           `json:"sizes,omitempty"`
	Parameters      []VideoModelParameter      `json:"parameters,omitempty"`
	ReferenceInputs []VideoModelReferenceInput `json:"reference_inputs,omitempty"`
}

// VideoModelsResponse 是 GET /v1/videos/models 的响应。
type VideoModelsResponse struct {
	Object string       `json:"object"`
	Data   []VideoModel `json:"data"`
}

// videoModelCatalogScope 是一个 provider 在本次枚举中的全部上下文。
type videoModelCatalogScope struct {
	provider     string
	platform     string
	capabilities VideoCapabilities
	// foreign 是别的 managed provider 声明为自己的模型名。模型价格的基础目录是
	// 平台无关的，没有这一层过滤，火山方舟的分组会在选择器里看到 sora-2。
	foreign map[string]struct{}
	models  []string
}

// ListVideoModels 按 API Key 所属分组列出可提交的视频模型。
//
// 清单由运营配置的价格决定，而不是内置能力表。能力表描述的是"这个 build 认得哪些
// 模型的请求约束"，跟"这个分组买得到它吗"是两回事：直出能力表会把没配价的模型也列
// 出来，用户写完 prompt 才撞上 ErrVideoPricingMissing。
//
// 清单的三个来源与 VideoPricingResolver.Resolve 的优先级链一一对应：
//
//  1. group.ModelPricing 里 billing_mode=video 的行（只取精确名，findVideoGroupPricing
//     不做通配符匹配，列出来提交时也匹配不到分组价）；
//  2. 分组所属渠道的 billing_mode=video 定价行（含前缀通配符）；
//  3. 模型价格里 video_pricing.enabled 为真的条目（含平台覆盖）。
//
// 而时长/尺寸/分辨率这些技术档位只从第 3 条的 VideoPricingConfig 取：渠道价与分组价
// 的结构里只有价格数字，没有 resolutions/rules——resolveChannelStylePricing 同样是拿
// 全局 profile 去归一化属性的。价目没覆盖到的部分回落到能力表。
//
// 注意：列出 ≠ 一定可调度。真正的路由白名单是账号 model_mapping，SupportedModels 与
// 价目都只是描述性的。
func (s *VideoTaskService) ListVideoModels(ctx context.Context, apiKey *APIKey) (*VideoModelsResponse, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.Video.Enabled {
		return nil, ErrVideoDisabled
	}
	// 这个端点只为创建表单服务。创建关掉时早报错，好过让用户写完 prompt 再挨 403。
	if !s.cfg.Gateway.Video.CreationEnabled {
		return nil, ErrVideoCreationDisabled
	}
	if apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 || s.groups == nil {
		return nil, ErrVideoPricingMissing
	}
	group, err := s.groups.GetByID(ctx, *apiKey.GroupID)
	if err != nil || group == nil {
		return nil, ErrVideoPricingMissing
	}
	if group.IsSubscriptionType() {
		return nil, ErrVideoSubscriptionUnsupported
	}
	if group.Status != StatusActive {
		return nil, ErrVideoNoAccountAvailable
	}
	providerNames, ok := videoModelProvidersForPlatform(group.Platform)
	if !ok {
		return nil, ErrVideoDisabled
	}

	scopes := make([]videoModelCatalogScope, 0, len(providerNames))
	for _, providerName := range providerNames {
		provider, ok := s.providers.Get(providerName)
		if !ok || provider == nil {
			continue
		}
		// 走 provider.Capabilities() 而不是裸目录：provider 侧会在目录不可读或
		// 缓存尚冷时回落到内置能力，返回的正是提交时校验会拿到的同一个值。
		scope := videoModelCatalogScope{
			provider:     providerName,
			platform:     videoPricingPlatformForProvider(providerName),
			capabilities: provider.Capabilities(),
			foreign:      s.videoModelsClaimedElsewhere(providerName),
		}
		scope.models = s.pricedVideoModelNames(ctx, group, scope.platform, scope.capabilities)
		scopes = append(scopes, scope)
	}
	owners := assignVideoModelOwners(scopes)

	data := make([]VideoModel, 0, 8)
	for _, scope := range scopes {
		for _, model := range scope.models {
			if owners[model] != scope.provider {
				continue
			}
			if entry := s.videoModelEntry(scope, model); entry != nil {
				data = append(data, *entry)
			}
		}
	}
	return &VideoModelsResponse{Object: "list", Data: data}, nil
}

// videoModelProvidersForPlatform 把分组平台展开成要枚举的 provider。
// composite 分组取并集：用户无从预知请求会落到哪一侧，返回空或报错会让页面直接不可用。
func videoModelProvidersForPlatform(platform string) ([]string, bool) {
	if strings.TrimSpace(platform) == PlatformComposite {
		return []string{VideoProviderOpenAI, VideoProviderByteDance}, true
	}
	provider, ok := managedVideoProviderForPlatform(platform)
	if !ok {
		return nil, false
	}
	return []string{provider}, true
}

// videoPricingPlatformForProvider 是 managedVideoProviderForPlatform 的反向映射。
// 二者的取值目前逐字相同，但定价查表用的是平台维度，写死等号会在其中一侧改名时
// 静默串味。
func videoPricingPlatformForProvider(provider string) string {
	switch provider {
	case VideoProviderByteDance:
		return PlatformByteDance
	case VideoProviderOpenAI:
		return PlatformOpenAI
	default:
		return provider
	}
}

// assignVideoModelOwners 决定每个模型 id 由哪个 provider 输出。
//
// 只有 composite 分组会同时枚举两侧的价格，而同一个模型 id 在响应里只能出现一次，
// 否则前端的选择器会拿到重复的 key。先让能力表认领自己认得的模型，剩下认不出的
// （自定义兼容模型）再按固定顺序落给第一个枚举到它的 provider。
func assignVideoModelOwners(scopes []videoModelCatalogScope) map[string]string {
	owners := make(map[string]string, 8)
	for _, claimed := range []bool{true, false} {
		for _, scope := range scopes {
			for _, model := range scope.models {
				if _, taken := owners[model]; taken {
					continue
				}
				if scope.capabilities.SupportedModels[model] != claimed {
					continue
				}
				owners[model] = scope.provider
			}
		}
	}
	return owners
}

// pricedVideoModelNames 汇总某个平台上"运营配过价"的模型名，结果按字典序去重。
func (s *VideoTaskService) pricedVideoModelNames(
	ctx context.Context, group *Group, platform string, capabilities VideoCapabilities,
) []string {
	seen := make(map[string]struct{}, 8)
	add := func(model string) {
		name := normalizePricingModelKey(model)
		// 通配符在这里丢掉：分组价只做精确匹配，渠道价的通配符走下面的展开。
		if name == "" || strings.HasSuffix(name, "*") {
			return
		}
		seen[name] = struct{}{}
	}

	for i := range group.ModelPricing {
		pricing := &group.ModelPricing[i]
		if pricing.BillingMode != BillingModeVideo {
			continue
		}
		// 空 platform 表示"不限平台"，与 findVideoGroupPricing 的判定保持一致。
		if declared := strings.TrimSpace(pricing.Platform); declared != "" && !strings.EqualFold(declared, platform) {
			continue
		}
		for _, model := range pricing.Models {
			add(model)
		}
	}

	// 模型价格先算：它既是第 3 个来源，也是渠道通配符的展开域之一。
	catalog := s.videoPricedCatalogModels(platform)
	for _, model := range catalog {
		add(model)
	}

	names, prefixes := s.videoChannelPricedModels(ctx, group.ID, platform)
	for _, model := range names {
		add(model)
	}
	if len(prefixes) > 0 {
		// 缓存里存的是前缀而不是模型名，只能拿候选域去撞。候选域取"能力表认得的"
		// 与"模型价格配过的"之并——前者保证 doubao-* 这类写法能命中内置模型，
		// 后者保证运营自己加的模型也能被通配到。
		universe := make([]string, 0, len(catalog)+len(capabilities.SupportedModels))
		universe = append(universe, catalog...)
		for model, supported := range capabilities.SupportedModels {
			if supported {
				universe = append(universe, normalizePricingModelKey(model))
			}
		}
		for _, prefix := range prefixes {
			for _, candidate := range universe {
				if strings.HasPrefix(candidate, prefix) {
					add(candidate)
				}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for model := range seen {
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

func (s *VideoTaskService) videoChannelPricedModels(ctx context.Context, groupID int64, platform string) ([]string, []string) {
	if s.channels == nil {
		return nil, nil
	}
	return s.channels.ListVideoPricedModelsForGroup(ctx, groupID, platform)
}

func (s *VideoTaskService) videoPricedCatalogModels(platform string) []string {
	if s.pricing == nil {
		return nil
	}
	return s.pricing.pricing.ListVideoPricedModels(platform)
}

// videoTechnicalProfile 取该模型的技术档位来源。
//
// 只认全局模型价格里的 VideoPricingConfig：渠道价与分组价（ChannelModelPricing）只
// 携带价格数字，没有 resolutions/rules 可读——resolveChannelStylePricing 同样是拿全局
// profile 来归一化属性的。校验不过的 profile 视同没有，与那里的判定保持一致。
func (s *VideoTaskService) videoTechnicalProfile(platform, model string) *VideoPricingConfig {
	if s.pricing == nil || s.pricing.pricing == nil {
		return nil
	}
	entry := s.pricing.pricing.LookupModelPricingStrictForPlatform(platform, model)
	if entry == nil || entry.VideoPricing == nil || !entry.VideoPricing.Enabled {
		return nil
	}
	if err := ValidateVideoPricingConfig(entry.VideoPricing); err != nil {
		return nil
	}
	return entry.VideoPricing
}

func (s *VideoTaskService) videoModelEntry(scope videoModelCatalogScope, model string) *VideoModel {
	// 一个价目/能力键一个条目，不按 canonical 折叠：带日期的别名是可路由的真实
	// 模型名，折叠会把它们藏掉。
	canonical := canonicalOpenAIVideoModel(model)
	profile := s.videoTechnicalProfile(scope.platform, model)
	if videoModelBelongsElsewhere(scope, model, profile) {
		return nil
	}

	return &VideoModel{
		ID: model, Object: videoModelObject, Provider: scope.provider,
		CanonicalModel:  canonical,
		IsDefault:       model == normalizePricingModelKey(scope.capabilities.DefaultModel),
		Operations:      videoCapabilityNames(scope.capabilities.Operations),
		ContentVariants: videoContentVariantNames(scope.capabilities.ContentVariants),
		Seconds:         videoModelSeconds(scope.provider, scope.capabilities, canonical, profile),
		Sizes:           videoModelSizes(scope.capabilities, canonical, profile),
		Parameters:      videoModelParametersForProvider(scope.provider, profile),
		ReferenceInputs: videoModelReferenceInputsForProvider(scope.provider),
	}
}

// videoModelsClaimedElsewhere 列出「被另一个 managed provider 声明为自己的」模型名。
//
// 模型价格的基础目录是平台无关的：一条 sora-2 的 video_pricing 对 ByteDance 分组同样
// 可见。没有这层过滤，火山方舟的用户会在选择器里看到 sora-2，选中即必挂。
//
// 内置快照是底，注册表是叠加：本次枚举只会装配用得上的 provider，所以对方常常根本
// 不在注册表里——那时仍要认得它的模型名；而运行时目录又可能给对方加了内置快照没有的
// 新模型，所以注册表里有就再叠一层。
func (s *VideoTaskService) videoModelsClaimedElsewhere(provider string) map[string]struct{} {
	claimed := make(map[string]struct{}, 8)
	absorb := func(capabilities VideoCapabilities) {
		for model, supported := range capabilities.SupportedModels {
			if supported {
				claimed[normalizePricingModelKey(model)] = struct{}{}
			}
		}
	}
	for _, other := range []struct {
		name    string
		builtin func() VideoCapabilities
	}{
		{VideoProviderOpenAI, DefaultOpenAIVideoCapabilities},
		{VideoProviderByteDance, DefaultByteDanceVideoCapabilities},
	} {
		if other.name == provider {
			continue
		}
		absorb(other.builtin())
		if s == nil || s.providers == nil {
			continue
		}
		if live, ok := s.providers.Get(other.name); ok && live != nil {
			absorb(live.Capabilities())
		}
	}
	return claimed
}

// videoModelBelongsElsewhere 判断一个配过价的模型该不该由本 provider 输出。
//
// 判定顺序是从强到弱：自家能力表认得的一定留下；价目自己用 conditions.providers
// 指名道姓时以价目为准；再不然就看有没有别的 provider 认领。两边都不认识的名字要
// 放行——那是自定义兼容上游，正是能力表覆盖不到、只能靠价目声明的那类模型。
func videoModelBelongsElsewhere(scope videoModelCatalogScope, model string, profile *VideoPricingConfig) bool {
	if scope.capabilities.SupportedModels[model] {
		return false
	}
	if declared := videoPricedProviders(profile); len(declared) > 0 {
		for _, name := range declared {
			if strings.EqualFold(name, scope.provider) {
				return false
			}
		}
		return true
	}
	_, claimed := scope.foreign[model]
	return claimed
}

func videoPricedProviders(profile *VideoPricingConfig) []string {
	if profile == nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, rule := range profile.Rules {
		for _, provider := range rule.Conditions.Providers {
			if name := strings.TrimSpace(provider); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// 自定义兼容模型的自由时长区间。这两个数是 UI 边界而非硬约束——不在
// SupportedModels 里的模型，ValidateVideoCreateCapabilities 对它的时长压根不校验
// （video_catalog.go:182-188），真正会拒绝的是上游和价目。
//
// 上界按上游分家：Ark 的 seedance 2.x 收 4~15 秒任意整数，Sora 一族跨度到 20。
const (
	videoFreeSecondsMin          = 4
	videoByteDanceFreeSecondsMax = 15
	videoOpenAIFreeSecondsMax    = 20
)

// videoModelSeconds 决定时长选择器的形态，四级回落。
//
//  1. 价目逐条列出的 conditions.seconds —— 运营主动收窄过，以他为准（离散档位）；
//  2. 能力表里这个模型自己的档位 —— 提交时的硬校验就是拿它比的（离散档位）；
//  3. 本 provider 的自由区间 —— 自定义兼容模型走到这里，只知道它属于哪一家上游，
//     给一个区间让用户在里面随便挑；
//  4. 连这家上游的时长都一无所知（能力表里一条 SupportedSeconds 都没有）就交 nil，
//     意思是「这一档我们说不准」，由前端渲染自由输入。
//
// 第 3 级以前是"本 provider 已知模型档位的并集"，那是错的：doubao-seedance-2.0-mini-480p
// 会因此被冒充成 5/10 两档，而它实际收 4~15 秒任意整数——拿另一个模型的离散档位充当
// 这个模型的能力，比说"说不准"更糟，因为用户没有办法发现这是猜的。
//
// 关键是任何一级都不再把模型整个丢掉。以前这里只有前两级、且落空即 return nil，
// 于是运营在模型价格里给 doubao-seedance-2.0-mini-480p 配好了价，却因为没有逐条列
// conditions.seconds（按秒计价的规则本就不必列——不写即「任意时长都卖」），模型在
// 选择器里凭空消失，页面只剩一句"没有可用的视频模型"。时长说不准不是不能卖的理由。
func videoModelSeconds(
	provider string, capabilities VideoCapabilities, canonical string, profile *VideoPricingConfig,
) *VideoModelSeconds {
	values := videoPricedSeconds(profile)
	if len(values) == 0 {
		values = append([]int(nil), capabilities.SupportedSeconds[canonical]...)
	}
	if len(values) > 0 {
		return &VideoModelSeconds{
			Values:  values,
			Default: videoDefaultSeconds(values, capabilities.DefaultSeconds[canonical]),
		}
	}
	if len(capabilities.SupportedSeconds) == 0 {
		return nil
	}
	minSeconds, maxSeconds := videoFreeSecondsRange(provider)
	return &VideoModelSeconds{
		Min:     minSeconds,
		Max:     maxSeconds,
		Default: videoRangeDefaultSeconds(minSeconds, maxSeconds, capabilities.DefaultSeconds[canonical]),
	}
}

// videoFreeSecondsRange 给出某家上游的自由时长区间。
func videoFreeSecondsRange(provider string) (int, int) {
	if provider == VideoProviderByteDance {
		return videoFreeSecondsMin, videoByteDanceFreeSecondsMax
	}
	return videoFreeSecondsMin, videoOpenAIFreeSecondsMax
}

// videoRangeDefaultSeconds 把能力表的默认时长夹进区间；区间外或没声明就落在下界。
func videoRangeDefaultSeconds(minSeconds, maxSeconds, preferred int) int {
	if preferred >= minSeconds && preferred <= maxSeconds {
		return preferred
	}
	return minSeconds
}

// videoPricedSeconds 取价目里逐条列出的时长档位。
// 计价规则的 conditions.seconds 是运营主动收窄的结果：一旦列了，不在其中的时长提交时
// 就会撞 ErrVideoPricingRuleMissing，不该出现在选择器里。但一条都不列不等于一档都不卖，
// 恰恰相反——那是「任意时长都按这条规则计价」。
func videoPricedSeconds(profile *VideoPricingConfig) []int {
	if profile == nil {
		return nil
	}
	seen := make(map[int]struct{}, 4)
	for _, rule := range profile.Rules {
		for _, value := range rule.Conditions.Seconds {
			if value > 0 {
				seen[value] = struct{}{}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func videoDefaultSeconds(values []int, preferred int) int {
	for _, value := range values {
		if value == preferred {
			return preferred
		}
	}
	return values[0]
}

// videoModelSizes 把价目里声明的分辨率翻译成可选尺寸。
//
// 返回 nil 有两种含义，前端都按"这个 provider 不用 size 表达画幅"处理：能力表里
// 一个 SupportedSizes 都没有（ByteDance 走 provider_options 的 resolution/ratio），
// 或者两边都没给出任何尺寸。
func videoModelSizes(capabilities VideoCapabilities, canonical string, profile *VideoPricingConfig) *VideoModelSizes {
	if len(capabilities.SupportedSizes) == 0 {
		return nil
	}
	capabilitySizes := capabilities.SupportedSizes[canonical]
	values := videoPricedSizes(profile)
	switch {
	case len(values) == 0:
		values = append([]string(nil), capabilitySizes...)
	case len(capabilitySizes) > 0:
		// 能力表对已知模型的 size 是硬校验（ValidateVideoCreateCapabilities），
		// 透出它不认的尺寸等于让用户必挨 400，所以要收窄到交集。
		if narrowed := intersectVideoSizes(values, capabilitySizes); len(narrowed) > 0 {
			values = narrowed
		} else {
			// 交集为空说明价目用的是另一套档位（比如只配了 480p，而 sora 只认 720x1280
			// 那几档）。此时以能力表为准：交出空列表会让画幅控件整段消失。
			values = append([]string(nil), capabilitySizes...)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return &VideoModelSizes{Values: values, Default: videoDefaultSize(values, profile, capabilities.DefaultSizes[canonical])}
}

// videoPricedSizes 展平价目的 resolutions。默认分辨率排最前，其余按分辨率名排序，
// 组内保持声明顺序——map 迭代序不稳定，不排会让响应每次都不一样。
func videoPricedSizes(profile *VideoPricingConfig) []string {
	if profile == nil || len(profile.Resolutions) == 0 {
		return nil
	}
	names := make([]string, 0, len(profile.Resolutions))
	for name := range profile.Resolutions {
		names = append(names, name)
	}
	sort.Strings(names)
	if def := strings.TrimSpace(profile.Defaults.Resolution); def != "" {
		ordered := make([]string, 0, len(names))
		for _, name := range names {
			if strings.EqualFold(name, def) {
				ordered = append(ordered, name)
			}
		}
		for _, name := range names {
			if !strings.EqualFold(name, def) {
				ordered = append(ordered, name)
			}
		}
		names = ordered
	}
	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)
	for _, name := range names {
		for _, size := range profile.Resolutions[name].Sizes {
			// 校验器允许 864X480 这种写法，能力表一律小写，比较前必须归一。
			normalized := strings.ToLower(strings.TrimSpace(size))
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

func intersectVideoSizes(values, allowed []string) []string {
	permitted := make(map[string]struct{}, len(allowed))
	for _, size := range allowed {
		permitted[strings.ToLower(strings.TrimSpace(size))] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, size := range values {
		if _, ok := permitted[size]; ok {
			out = append(out, size)
		}
	}
	return out
}

// videoDefaultSize 优先用价目默认分辨率的第一个尺寸——运营在模型价格里指定的默认档
// 才是他期望被买到的那一档；价目没说话时才回落到能力表的默认值。
func videoDefaultSize(values []string, profile *VideoPricingConfig, capabilityDefault string) string {
	contains := func(candidate string) bool {
		for _, value := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	if profile != nil {
		for name, spec := range profile.Resolutions {
			if !strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(profile.Defaults.Resolution)) {
				continue
			}
			for _, size := range spec.Sizes {
				if normalized := strings.ToLower(strings.TrimSpace(size)); contains(normalized) {
					return normalized
				}
			}
		}
	}
	if normalized := strings.ToLower(strings.TrimSpace(capabilityDefault)); normalized != "" && contains(normalized) {
		return normalized
	}
	return values[0]
}

func videoCapabilityNames(operations map[VideoCapability]bool) []string {
	names := make([]string, 0, len(operations))
	for capability, enabled := range operations {
		name := strings.TrimSpace(string(capability))
		if !enabled || name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func videoContentVariantNames(variants map[string]bool) []string {
	names := make([]string, 0, len(variants))
	for variant, enabled := range variants {
		name := strings.TrimSpace(variant)
		if !enabled || name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func videoModelParametersForProvider(providerName string, profile *VideoPricingConfig) []VideoModelParameter {
	switch providerName {
	case VideoProviderByteDance:
		return byteDanceVideoModelParameters(profile)
	case VideoProviderOpenAI:
		return openAIVideoModelParameters()
	default:
		return nil
	}
}

func videoModelReferenceInputsForProvider(providerName string) []VideoModelReferenceInput {
	switch providerName {
	case VideoProviderByteDance:
		return byteDanceVideoReferenceInputs()
	case VideoProviderOpenAI:
		return openAIVideoReferenceInputs()
	default:
		return nil
	}
}

// openAIVideoCompatibleRatioValues 从校验器自己的词表推导取值，两者因此不会漂移。
func openAIVideoCompatibleRatioValues() []string {
	values := make([]string, 0, len(openAIVideoCompatibleRatios))
	for ratio := range openAIVideoCompatibleRatios {
		values = append(values, ratio)
	}
	sort.Strings(values)
	return values
}

func openAIVideoModelParameters() []VideoModelParameter {
	// 画幅提示与 size 同时出现会被直接拒（openai_video_provider.go 的
	// unsupported_reference_media），所以前端必须把二者做成单选而非两个独立控件。
	framingIgnoredWhenPresent := []string{"image_url", "first_image_url", "last_image_url", "reference_videos"}
	return []VideoModelParameter{
		{
			Name: "ratio", In: videoModelParameterInBody, Values: openAIVideoCompatibleRatioValues(),
			ConflictsWith: []string{"aspect_ratio", "size"}, IgnoredWhenPresent: framingIgnoredWhenPresent,
		},
		{
			Name: "aspect_ratio", In: videoModelParameterInBody, Values: openAIVideoCompatibleRatioValues(),
			ConflictsWith: []string{"ratio", "size"}, IgnoredWhenPresent: framingIgnoredWhenPresent,
		},
	}
}

func byteDanceVideoModelParameters(profile *VideoPricingConfig) []VideoModelParameter {
	// Ark 用独立的 resolution/ratio 表达画幅，且只认 provider_options。
	return []VideoModelParameter{
		{
			Name: "resolution", In: videoModelParameterInProviderOptions,
			Values: byteDanceVideoResolutionValues(profile),
		},
		{
			Name: "ratio", In: videoModelParameterInProviderOptions,
			Values: append([]string(nil), byteDanceVideoRatioVocabulary...),
		},
	}
}

// byteDanceVideoResolutionValues 用价目里声明的分辨率收窄可选项：运营只配了
// 480p/720p，就不该让用户选到 1080p 再在提交时撞 ErrVideoPricingRuleMissing。
//
// 但只保留 Ark 认得的名字——价目的分辨率名是自由字符串，透出一个 Ark 不认的名字
// 换来的是硬 400（byteDanceGenerationOptions 对未知 resolution 直接报错）。过滤后
// 为空说明价目用的是另一套命名，此时退回完整词表：收窄到空只会让画幅控件失灵。
func byteDanceVideoResolutionValues(profile *VideoPricingConfig) []string {
	if profile == nil || len(profile.Resolutions) == 0 {
		return append([]string(nil), byteDanceVideoResolutionVocabulary...)
	}
	values := make([]string, 0, len(byteDanceVideoResolutionVocabulary))
	for _, candidate := range byteDanceVideoResolutionVocabulary {
		for name := range profile.Resolutions {
			if strings.EqualFold(strings.TrimSpace(name), candidate) {
				values = append(values, candidate)
				break
			}
		}
	}
	if len(values) == 0 {
		return append([]string(nil), byteDanceVideoResolutionVocabulary...)
	}
	return values
}

// openAIVideoReferenceInputs 描述 OpenAI 兼容通道用 URL 提交参考素材时的槽位。
//
// 份数上限直接取 openai_video_compatible_references.go 的常量——那才是 URL 通道的
// 真实约束。切勿改用 VideoCapabilities.MaxInputsByOperation：那描述的是多段二进制
// 上传的份数（generate 下为 1），拿它驱动 URL 槽位会把 reference_images 误卡到 1，
// 让合法请求被前端拦下且无从诊断。
//
// image_url 排在最前：它是"拿一张图当视频的起始画面"这件事在协议上唯一通用的说法。
// 它曾被当成 reference_images 的重复项删掉，那是错的——火山方舟原生模式只认
// image_url 与 reference_images 两个字段（byteDanceReferenceImages），而后者进请求时
// 带的是 reference_image 角色、表达的是"风格/主体参考"，做不了首帧；首尾帧字段在原生
// 模式下更是直接被拒。删掉 image_url 等于让原生模式账号（账号未配协议凭证时的默认模式）
// 彻底失去图生视频入口。它与 first_image_url/last_image_url 互斥，同现会被
// openAICompatibleVideoReferenceFields 直接判错，所以三者两两声明冲突。
func openAIVideoReferenceInputs() []VideoModelReferenceInput {
	return []VideoModelReferenceInput{
		{
			Field: "image_url", Kind: "image", Max: 1,
			Accepts:       []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			ConflictsWith: []string{"first_image_url", "last_image_url"},
		},
		{
			Field: "first_image_url", Kind: "image", Max: 1,
			Accepts:       []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			ConflictsWith: []string{"image_url", "reference_images", "reference_videos"},
		},
		{
			Field: "last_image_url", Kind: "image", Max: 1,
			Accepts:       []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			ConflictsWith: []string{"image_url", "reference_images", "reference_videos"},
		},
		{
			Field: "reference_images", Kind: "image", Max: openAIVideoMaxReferenceImages,
			Accepts:       []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			ConflictsWith: []string{"first_image_url", "last_image_url"},
		},
		{
			// 参考视频不接受 data URI：校验器对 video 一类关掉了 data 分支。
			Field: "reference_videos", Kind: "video", Max: openAIVideoMaxReferenceVideos,
			Accepts:       []string{videoReferenceAcceptsHTTPS},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			ConflictsWith: []string{"first_image_url", "last_image_url"},
		},
		{
			// 参考音频不能单独使用，必须至少搭配一项图片或视频参考（本平台 API 文档 §4.3）。
			// 这条前置三处同口径：接口文档这么承诺，网关侧的 openAICompatibleVideoReferenceFields
			// 照此拦截，Playground 据此把槽位禁掉并说明原因。少了任何一处，直连 API 的客户端
			// 和 Playground 用户就会拿到两套规则。
			Field: "reference_audios", Kind: "audio", Max: openAIVideoMaxReferenceAudios,
			Accepts:       []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI},
			MaxValueBytes: openAIVideoMaxReferenceBytes,
			RequiresAny: []string{
				"image_url", "first_image_url", "last_image_url", "reference_images", "reference_videos",
			},
		},
	}
}

// byteDanceVideoReferenceInputs 给出与 OpenAI 兼容通道相同的槽位，只是一律不接受
// data URI（原生模式走 normalizeProviderVideoURL，它只认 http(s)）。
//
// 这里刻意不再取"原生模式 ∩ 兼容模式"的交集。协议模式是账号级的、在这一层不可见，
// 取交集意味着兼容模式的账号在 Playground 里永远看不到首尾帧、参考视频与参考音频，
// 而那正是用户报上来的问题。两种错法的代价并不对称：多报换来的是原生模式账号提交时
// 一个明确的 400（byteDanceReferenceImages 对首尾帧/视频/音频直接报错），错误会原样
// 贴在卡片上；少报换来的是能力被静默藏掉，用户既发现不了也诊断不了。
func byteDanceVideoReferenceInputs() []VideoModelReferenceInput {
	slots := openAIVideoReferenceInputs()
	for i := range slots {
		slots[i].Accepts = []string{videoReferenceAcceptsHTTPS}
	}
	return slots
}

// ListVideoPricedModelsForGroup 枚举某分组在指定平台上 billing_mode=video 的渠道定价，
// 分别返回精确模型名与通配符前缀。
//
// 通配符必须分开返回：缓存里存的是前缀而不是模型名（expandPricingToCache 把 "foo*"
// 拆成 prefix="foo"），只有调用方知道该拿哪个候选域去展开它。
//
// 这个方法与 PricingService.ListVideoPricedModels 都只为 ListVideoModels 存在，
// 因此与它同文件——放回各自的宿主文件只会让两个上千行的热点文件再多一个入口。
func (s *ChannelService) ListVideoPricedModelsForGroup(
	ctx context.Context, groupID int64, platform string,
) ([]string, []string) {
	if s == nil {
		return nil, nil
	}
	lk, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil || lk == nil {
		return nil, nil
	}
	platforms := matchingPlatforms(platform)
	names := make([]string, 0, 4)
	for key, pricing := range lk.cache.pricingByGroupModel {
		if key.groupID != groupID || pricing == nil || pricing.BillingMode != BillingModeVideo {
			continue
		}
		for _, candidate := range platforms {
			if key.platform == candidate {
				names = append(names, key.model)
				break
			}
		}
	}
	prefixes := make([]string, 0, 2)
	for _, candidate := range platforms {
		for _, wildcard := range lk.cache.wildcardByGroupPlatform[channelGroupPlatformKey{groupID: groupID, platform: candidate}] {
			if wildcard == nil || wildcard.pricing == nil || wildcard.pricing.BillingMode != BillingModeVideo {
				continue
			}
			prefixes = append(prefixes, wildcard.prefix)
		}
	}
	sort.Strings(names)
	sort.Strings(prefixes)
	return names, prefixes
}

// ListVideoPricedModels 枚举模型价格里对该平台生效、且 video_pricing.enabled 为真的
// 模型名。
//
// 平台覆盖行整体替换同名基础行——与 effectiveEntryLocked 的"先 overlay 后 base"是同
// 一个优先级。所以一个基础行开了视频、平台覆盖把它关掉时，这里必须跟着不列出来。
func (s *PricingService) ListVideoPricedModels(platform string) []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	overlay := s.platformOverrides[normalizeOverridePlatform(platform)]
	seen := make(map[string]struct{}, 8)
	consider := func(name string, entry *ModelPriceEntry) {
		if override, ok := overlay[name]; ok {
			entry = override
		}
		if entry == nil || entry.VideoPricing == nil || !entry.VideoPricing.Enabled {
			return
		}
		if normalized := normalizePricingModelKey(name); normalized != "" {
			seen[normalized] = struct{}{}
		}
	}
	for name, entry := range s.pricingData {
		consider(name, entry)
	}
	for name, entry := range overlay {
		if _, ok := s.pricingData[name]; !ok {
			consider(name, entry)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
