package service

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DeepSeek publishes peak windows in UTC; Beijing time is used only to
	// identify weekends, which are off-peak all day.
	deepSeekOfficialTimezone = "UTC"
	// deepSeekOffPeakMultiplier 是官方空闲价相对高峰价的倍率：空闲 = 高峰 × 1/2。
	deepSeekOffPeakMultiplier = 0.5
	// deepSeekPeakMultiplier 是官方高峰价相对空闲价的倍率：高峰 = 空闲 × 2。
	deepSeekPeakMultiplier   = 1 / deepSeekOffPeakMultiplier
	deepSeekOfficialTimeKind = "deepseek_official"
)

// deepSeekOfficialPeakWindows 是 UTC 左闭右开高峰窗口。
// 官方 2026-08-23 口径：01:00-04:00、06:00-10:00，仅工作日。
var deepSeekOfficialPeakWindows = [][2]int{
	{1 * 60, 4 * 60},
	{6 * 60, 10 * 60},
}

var deepSeekOfficialLocation = func() *time.Location {
	loc, err := time.LoadLocation(deepSeekOfficialTimezone)
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// usesDeepSeekOfficialTimePricing 判断 platform/model 是否属于官方峰谷价 SKU。
// 必须命中官方分时 SKU 名单：deepseek 平台的能力表是 AllowUnknownModels，任意
// 模型 ID 都能透传，仅凭平台放行会把第三方中转的非官方 SKU 也打五折。
// platform 为空时按未知处理（旧路径不传平台），只以模型名判定。
func usesDeepSeekOfficialTimePricing(platform, model string) bool {
	if !isDeepSeekOfficialTimePricedModel(model) {
		return false
	}
	trimmed := strings.TrimSpace(platform)
	return trimmed == "" || strings.EqualFold(trimmed, PlatformDeepSeek)
}

func isDeepSeekOfficialTimePricedModel(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(lower, "deepseek-v4-flash") || strings.Contains(lower, "deepseek-v4-pro")
}

// deepSeekFallbackAllowedForPlatforms limits the code-owned official card to
// DeepSeek routing scopes. A model name alone is not enough evidence to apply
// DeepSeek pricing to an account on another provider platform.
func deepSeekFallbackAllowedForPlatforms(platforms []string) bool {
	for _, platform := range normalizePricingPlatforms(platforms) {
		switch platform {
		case PlatformDeepSeek, PlatformComposite, ModelPriceOverrideWildcardPlatform:
			continue
		default:
			return false
		}
	}
	return true
}

// deepSeekOfficialPeakWindowLabels 由 deepSeekOfficialPeakWindows 生成展示用文案，
// 避免窗口定义与展示文案两处各写一份、改一处漏一处。
func deepSeekOfficialPeakWindowLabels() []string {
	labels := make([]string, 0, len(deepSeekOfficialPeakWindows))
	for _, window := range deepSeekOfficialPeakWindows {
		labels = append(labels, fmt.Sprintf("%02d:%02d-%02d:%02d",
			window[0]/60, window[0]%60, window[1]/60, window[1]%60))
	}
	return labels
}

// officialTimePricingApplies 判断这份基准价能否叠加官方峰谷倍率。
//
// 峰谷是官方 SKU 自带的属性，因此只要基准价是官方报价就成立：价格目录（含管理端
// 手动覆盖后的生效价）存的是官方空闲价，代码内官方兜底表存的是高峰价，具体是哪
// 一档由 ModelPricing.OfficialTimeBaseIsOffPeak 区分。渠道价、分组价是渠道自己的
// 显式定价、自带时段语义（渠道还有独立的 TimePricing 配置），不叠加官方倍率。
func officialTimePricingApplies(pricing *ModelPricing) bool {
	return pricing != nil && pricing.OfficialTimePricing
}

func deepSeekIsPeakAt(at time.Time) bool {
	if at.IsZero() {
		return false
	}
	beijing := at.In(time.FixedZone("Asia/Shanghai", 8*3600))
	if beijing.Weekday() == time.Saturday || beijing.Weekday() == time.Sunday {
		return false
	}
	local := at.In(deepSeekOfficialLocation)
	cur := local.Hour()*60 + local.Minute()
	for _, window := range deepSeekOfficialPeakWindows {
		if cur >= window[0] && cur < window[1] {
			return true
		}
	}
	return false
}

// deepSeekOfficialTimeMultiplier 返回相对「基准价」的 DeepSeek 官方峰谷倍率。
//
// baseIsOffPeak=true：基准价是官方空闲价（价格目录 / 管理端生效价），高峰 ×2、空闲 ×1；
// baseIsOffPeak=false：基准价是高峰价（代码内官方兜底表），高峰 ×1、空闲 ×0.5。
// 非 DeepSeek 官方分时 SKU 返回 1；at 为零时按当前时刻现算（补偿/旧路径与“当前生效价”一致）。
func deepSeekOfficialTimeMultiplier(platform, model string, at time.Time, baseIsOffPeak bool) float64 {
	if !usesDeepSeekOfficialTimePricing(platform, model) {
		return 1
	}
	if at.IsZero() {
		at = time.Now()
	}
	if deepSeekIsPeakAt(at) {
		if baseIsOffPeak {
			return deepSeekPeakMultiplier
		}
		return 1
	}
	if baseIsOffPeak {
		return 1
	}
	return deepSeekOffPeakMultiplier
}

// DeepSeekOfficialPriceTimeSchedule 返回 DeepSeek 官方峰谷展示规则；非 DeepSeek 模型为 nil。
// baseIsOffPeak 含义见 deepSeekOfficialTimeMultiplier：展示侧据此把随行价格换算成
// 高峰价 / 空闲价，与结算侧用的是同一组倍率。
func DeepSeekOfficialPriceTimeSchedule(platform, model string, baseIsOffPeak bool) *ModelPriceTimeSchedule {
	return deepSeekOfficialPriceTimeSchedule(platform, model, baseIsOffPeak)
}

func deepSeekOfficialPriceTimeSchedule(platform, model string, baseIsOffPeak bool) *ModelPriceTimeSchedule {
	if !usesDeepSeekOfficialTimePricing(platform, model) {
		return nil
	}
	schedule := &ModelPriceTimeSchedule{
		Kind:              deepSeekOfficialTimeKind,
		Timezone:          deepSeekOfficialTimezone,
		PeakWindows:       deepSeekOfficialPeakWindowLabels(),
		PeakMultiplier:    1,
		OffPeakMultiplier: deepSeekOffPeakMultiplier,
	}
	if baseIsOffPeak {
		schedule.PeakMultiplier = deepSeekPeakMultiplier
		schedule.OffPeakMultiplier = 1
	}
	return schedule
}
