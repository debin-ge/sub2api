package service

import (
	"context"
	"math"
	"strings"
	"time"
)

// PlazaMediaTierPrice 是模型广场展示用的一档媒体单价（未乘分组倍率）。
// 图片档位单位为「美元/张」，视频档位单位为「美元/秒」。
type PlazaMediaTierPrice struct {
	Tier  string
	Price float64
}

// PlazaMediaPricing 汇总某个模型在某个分组下的按张/按秒展示价。
type PlazaMediaPricing struct {
	ImageTiers []PlazaMediaTierPrice
	VideoTiers []PlazaMediaTierPrice
	// VideoPerRequest 是视频模型只配了按次（request）规则时的单次价；有按秒档位时为 nil。
	VideoPerRequest *float64
	// ImageBillingMode 是出图方式（按张与按 token 二选一）：按张时只有 ImageTiers；
	// 按 token 时只有 ImageInputPrice、ImageOutputPrice 两项价（每 token）。
	ImageBillingMode BillingMode
	ImageInputPrice  *float64
	ImageOutputPrice *float64
}

var (
	plazaImageTiers = []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K}
	plazaVideoTiers = []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
)

// ResolvePlazaMediaPricing 按真实结算链路的优先级推导模型广场的按张/按秒展示价，
// 保证用户在广场看到的档位价就是实际扣费用到的单价：
//
//   - 图片：按出图计划（ResolveImageBilling，专用图片端点口径）推导，按张与按 token
//     二选一，与 /v1/images 的结算一致；
//   - 视频：分组按模型族配置 > 分组统一 480p/720p/1080p 价 > 渠道视频档位价 > Grok 内置价，
//     与 calculateOpenAIVideoCost / strictVideoUnitPrice 一致；都没有时再看模型价格里的
//     video_pricing 规则（VideoPricingResolver 的最后一级），按秒/按视频 token 的规则折算
//     成每秒价，只有按次规则时给出单次价。不从图片价推断视频价。
//
// 只有被识别为图片/视频模型时才会产出对应档位，避免同分组的文本模型沾上分组媒体价。
// 任何一档都解析不到价格时该档省略，前端据此只渲染真实可结算的档位。
func ResolvePlazaMediaPricing(
	pricing *PricingService,
	platform string,
	model string,
	channel *ChannelModelPricing,
	group AvailableGroupRef,
) PlazaMediaPricing {
	model = strings.TrimSpace(model)
	if model == "" {
		return PlazaMediaPricing{}
	}
	var catalog *ModelPriceEntry
	if pricing != nil {
		catalog = pricing.LookupModelPricingStrictForPlatforms(plazaPlatforms(platform), model)
	}
	profile := plazaCatalogVideoProfile(catalog)
	if isPlazaVideoModel(model, channel, group) || profile != nil {
		tiers := plazaVideoTierPrices(model, channel, group)
		if len(tiers) > 0 || profile == nil {
			return PlazaMediaPricing{VideoTiers: tiers}
		}
		tiers, perRequest := plazaProfileVideoPrices(model, profile, time.Now().UTC())
		return PlazaMediaPricing{VideoTiers: tiers, VideoPerRequest: perRequest}
	}
	if !isPlazaImageModel(model, channel, catalog) {
		return PlazaMediaPricing{}
	}
	return plazaImagePricing(pricing, platform, model, channel, group)
}

// plazaImagePricing 按出图计划（专用图片端点口径）给出广场展示价，保证展示的方式与
// 档位就是实际结算用的：按张只列出能解析出价格的档位，按 token 只给两项图片价。
func plazaImagePricing(
	pricing *PricingService,
	platform string,
	model string,
	channel *ChannelModelPricing,
	group AvailableGroupRef,
) PlazaMediaPricing {
	resolver := &ModelPricingResolver{billingService: &BillingService{pricingService: pricing}}
	in := ImageBillingInput{
		Model:     model,
		Platforms: plazaPlatforms(platform),
		Group: &Group{
			Platform:     strings.TrimSpace(platform),
			ImagePrice1K: group.ImagePrice1K,
			ImagePrice2K: group.ImagePrice2K,
			ImagePrice4K: group.ImagePrice4K,
		},
		ChannelRow:    channel,
		ImageEndpoint: true,
	}
	out := PlazaMediaPricing{}
	for _, tier := range plazaImageTiers {
		in.SizeTier = tier
		plan, err := resolver.ResolveImageBilling(context.Background(), in)
		if err != nil || plan == nil {
			continue
		}
		switch plan.Mode {
		case BillingModeImage:
			out.ImageBillingMode = BillingModeImage
			out.ImageTiers = append(out.ImageTiers, PlazaMediaTierPrice{Tier: tier, Price: plan.UnitPrice})
		case BillingModeToken:
			input, output := plan.ImageInputPrice, plan.ImageOutputPrice
			return PlazaMediaPricing{ImageBillingMode: BillingModeToken, ImageInputPrice: &input, ImageOutputPrice: &output}
		}
	}
	return out
}

func plazaPlatforms(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

func isPlazaVideoModel(model string, channel *ChannelModelPricing, group AvailableGroupRef) bool {
	if channel != nil && channel.BillingMode == BillingModeVideo {
		return true
	}
	if isGrokVideoBillingModel(model) || CanonicalGrokImagineVideoPriceFamily(model) != "" {
		return true
	}
	return LookupVideoModelPrice(group.VideoModelPrices, model, VideoBillingResolution480P) != nil ||
		LookupVideoModelPrice(group.VideoModelPrices, model, VideoBillingResolution720P) != nil ||
		LookupVideoModelPrice(group.VideoModelPrices, model, VideoBillingResolution1080P) != nil
}

func isPlazaImageModel(model string, channel *ChannelModelPricing, catalog *ModelPriceEntry) bool {
	if channel != nil && channel.BillingMode == BillingModeImage {
		return true
	}
	if _, ok := getDefaultGrokImagineImagePrice(model, ImageBillingSize1K); ok {
		return true
	}
	if catalog == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(catalog.Mode), "image_generation") {
		return true
	}
	return len(catalog.Intervals) > 0 ||
		(catalog.OutputCostPerImage > 0 || catalog.OutputCostPerImageExplicit) ||
		(catalog.OutputCostPerImageToken > 0 || catalog.ImageOutputPriceExplicit)
}

func plazaVideoTierPrices(model string, channel *ChannelModelPricing, group AvailableGroupRef) []PlazaMediaTierPrice {
	flat := map[string]*float64{
		VideoBillingResolution480P:  group.VideoPrice480P,
		VideoBillingResolution720P:  group.VideoPrice720P,
		VideoBillingResolution1080P: group.VideoPrice1080P,
	}
	out := make([]PlazaMediaTierPrice, 0, len(plazaVideoTiers))
	for _, tier := range plazaVideoTiers {
		if price := LookupVideoModelPrice(group.VideoModelPrices, model, tier); validConfiguredPrice(price) {
			out = append(out, PlazaMediaTierPrice{Tier: tier, Price: *price})
			continue
		}
		if price := flat[tier]; validConfiguredPrice(price) {
			out = append(out, PlazaMediaTierPrice{Tier: tier, Price: *price})
			continue
		}
		if channel != nil && channel.BillingMode == BillingModeVideo {
			if price := channelMediaTierPrice(channel, tier); validConfiguredPrice(price) {
				out = append(out, PlazaMediaTierPrice{Tier: tier, Price: *price})
				continue
			}
		}
		if price, ok := getDefaultGrokImagineVideoPrice(model, tier); ok {
			out = append(out, PlazaMediaTierPrice{Tier: tier, Price: price})
		}
	}
	return out
}

// channelMediaTierPrice 取渠道同档位价，缺省回落到渠道按次价（与 AvailableImageDisplayPricing 一致）。
func channelMediaTierPrice(channel *ChannelModelPricing, tier string) *float64 {
	for i := range channel.Intervals {
		if strings.EqualFold(strings.TrimSpace(channel.Intervals[i].TierLabel), tier) &&
			channel.Intervals[i].PerRequestPrice != nil {
			return channel.Intervals[i].PerRequestPrice
		}
	}
	return channel.PerRequestPrice
}

// plazaCatalogVideoProfile 取模型价格里生效且校验通过的 video_pricing，判定与
// VideoPricingResolver 的 catalogVideoPricingActive 一致。
func plazaCatalogVideoProfile(catalog *ModelPriceEntry) *VideoPricingConfig {
	if !catalogVideoPricingActive(catalog) || ValidateVideoPricingConfig(catalog.VideoPricing) != nil {
		return nil
	}
	return catalog.VideoPricing
}

// plazaDefaultProbeSeconds 是规则没有列出时长时用来折算每秒价的探测时长。
const plazaDefaultProbeSeconds = 5

// plazaProfileVideoPrices 用结算同款的 ResolveVideoPricingConfig 对每个标准分辨率试算，
// 把报价折算成每秒价（倍率取 1，由前端按分组倍率换算）。
//
// 规则可能按上游、操作类型、输入类型、时长收窄，真实请求总会带上其中某个取值，所以
// 这里把规则里出现过的取值都试一遍，取最低价作为「起步价」；任何组合都报不出价的档位
// 省略。按次规则无法折成每秒价，单独返回最低单次价，仅在没有任何按秒档位时使用。
func plazaProfileVideoPrices(model string, profile *VideoPricingConfig, at time.Time) ([]PlazaMediaTierPrice, *float64) {
	type probeTier struct {
		label      string
		resolution string
	}
	var probes []probeTier
	if len(profile.Resolutions) > 0 {
		for _, tier := range plazaVideoTiers {
			if name, _, ok := videoResolutionSpec(profile, tier); ok {
				probes = append(probes, probeTier{label: tier, resolution: name})
			}
		}
	} else {
		// 规则不分辨率：同一价格适用于该模型的任意输出，挂到能确定的档位上。
		for _, tier := range plazaProfileImpliedTiers(model, profile) {
			probes = append(probes, probeTier{label: tier})
		}
	}

	providers := plazaVideoConditionValues(profile, func(c VideoPricingConditions) []string { return c.Providers })
	operations := plazaVideoConditionValues(profile, func(c VideoPricingConditions) []string { return c.Operations })
	inputTypes := plazaVideoConditionValues(profile, func(c VideoPricingConditions) []string { return c.InputTypes })
	seconds := videoPricedSeconds(profile)
	if len(seconds) == 0 {
		seconds = []int{plazaDefaultProbeSeconds}
	}

	var perRequest *float64
	out := make([]PlazaMediaTierPrice, 0, len(probes))
	for _, probe := range probes {
		best := math.Inf(1)
		for _, provider := range providers {
			for _, operation := range operations {
				for _, inputType := range inputTypes {
					for _, sec := range seconds {
						quote, err := ResolveVideoPricingConfig(profile, VideoPricingAttributes{
							Provider: provider, Model: model, Operation: operation,
							Resolution: probe.resolution, Seconds: sec, InputType: inputType, At: at,
						})
						if err != nil || !isFiniteNonNegativePrice(quote.EstimatedCost) {
							continue
						}
						if quote.BillingUnit == VideoBillingUnitRequest {
							if perRequest == nil || quote.EstimatedCost < *perRequest {
								value := quote.EstimatedCost
								perRequest = &value
							}
							continue
						}
						best = math.Min(best, quote.EstimatedCost/float64(sec))
					}
				}
			}
		}
		if !math.IsInf(best, 1) {
			out = append(out, PlazaMediaTierPrice{Tier: probe.label, Price: best})
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	return nil, perRequest
}

// plazaProfileImpliedTiers 为不分辨率的价目找展示档位：默认分辨率 > 模型名里的分辨率 > 全部标准档。
func plazaProfileImpliedTiers(model string, profile *VideoPricingConfig) []string {
	for _, candidate := range []string{profile.Defaults.Resolution, model} {
		candidate = strings.ToLower(candidate)
		for _, tier := range plazaVideoTiers {
			if strings.Contains(candidate, strings.ToLower(tier)) {
				return []string{tier}
			}
		}
	}
	return plazaVideoTiers
}

// plazaVideoConditionValues 收集规则某个条件维度出现过的取值，外加空值（匹配不设该条件的规则）。
func plazaVideoConditionValues(profile *VideoPricingConfig, pick func(VideoPricingConditions) []string) []string {
	const maxValues = 8
	out := []string{""}
	seen := map[string]struct{}{"": {}}
	for _, rule := range profile.Rules {
		for _, value := range pick(rule.Conditions) {
			key := strings.ToLower(strings.TrimSpace(value))
			if _, ok := seen[key]; ok || len(out) >= maxValues {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}
