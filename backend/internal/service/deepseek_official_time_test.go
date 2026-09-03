package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func beijingTime(hour, minute int) time.Time {
	return time.Date(2026, 8, 19, hour, minute, 0, 0, deepSeekOfficialLocation)
}

func TestDeepSeekIsPeakAtWindows(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "before first peak", at: beijingTime(0, 59), want: false},
		{name: "first peak start", at: beijingTime(1, 0), want: true},
		{name: "first peak middle", at: beijingTime(2, 30), want: true},
		{name: "first peak end exclusive", at: beijingTime(4, 0), want: false},
		{name: "between peaks", at: beijingTime(5, 0), want: false},
		{name: "second peak start", at: beijingTime(6, 0), want: true},
		{name: "second peak middle", at: beijingTime(8, 0), want: true},
		{name: "second peak end exclusive", at: beijingTime(10, 0), want: false},
		{name: "late off-peak", at: beijingTime(22, 0), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, deepSeekIsPeakAt(tt.at))
		})
	}
}

// 基准价是高峰价（代码内官方兜底表）：高峰 ×1、空闲 ×0.5。
func TestDeepSeekOfficialTimeMultiplierPeakBase(t *testing.T) {
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier("", "claude-sonnet-4", beijingTime(10, 0), false))
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformDeepSeek, "deepseek-v4-flash", beijingTime(2, 0), false))
	require.Equal(t, 0.5, deepSeekOfficialTimeMultiplier("", "deepseek-v4-pro", beijingTime(13, 0), false))
	// deepseek 平台的能力表是 AllowUnknownModels：只认平台会把第三方中转的
	// 非官方 SKU 也打五折。必须命中官方分时 SKU 名单。
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformDeepSeek, "custom-sku", beijingTime(20, 0), false))
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier("", "deepseek-chat", beijingTime(13, 0), false))
	// 官方 SKU 跑在别的平台上（第三方中转）同样不打折。
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformOpenAI, "deepseek-v4-pro", beijingTime(13, 0), false))
}

// 基准价是空闲价（价格目录 / 管理端生效价）：高峰 ×2、空闲 ×1。
func TestDeepSeekOfficialTimeMultiplierOffPeakBase(t *testing.T) {
	require.Equal(t, 2.0, deepSeekOfficialTimeMultiplier(PlatformDeepSeek, "deepseek-v4-flash", beijingTime(2, 0), true))
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformDeepSeek, "deepseek-v4-flash", beijingTime(13, 0), true))
	require.Equal(t, 2.0, deepSeekOfficialTimeMultiplier("", "deepseek-v4-pro", beijingTime(7, 0), true))
	// 基准档位不改变 SKU / 平台判定。
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformDeepSeek, "custom-sku", beijingTime(10, 0), true))
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier(PlatformOpenAI, "deepseek-v4-pro", beijingTime(10, 0), true))
}

func TestDeepSeekOfficialPeakWindowLabels(t *testing.T) {
	require.Equal(t, []string{"01:00-04:00", "06:00-10:00"}, deepSeekOfficialPeakWindowLabels())
}

// 没有价格目录时结算落在代码内官方兜底表上：表里存低谷价，高峰翻倍。
func TestOfficialTimePricingAppliesToFallbackTableAsOffPeakBase(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)

	fallback, err := billing.GetModelPricingForPlatform(PlatformDeepSeek, "deepseek-v4-flash")
	require.NoError(t, err)
	require.True(t, officialTimePricingApplies(fallback))
	require.True(t, fallback.OfficialTimeBaseIsOffPeak, "兜底表存的是低谷价")
	require.Equal(t, 1.0, billing.officialTimeMultiplierForPlatform(PlatformDeepSeek, "deepseek-v4-flash", beijingTime(13, 0)))
	require.Equal(t, 2.0, billing.officialTimeMultiplierForPlatform(PlatformDeepSeek, "deepseek-v4-flash", beijingTime(2, 0)))

	// 渠道显式定价后不再是官方兜底价。
	price := 0.001
	channelPriced, err := billing.GetModelPricingWithChannelForPlatform(
		PlatformDeepSeek, "deepseek-v4-flash", &ChannelModelPricing{InputPrice: &price},
	)
	require.NoError(t, err)
	require.False(t, officialTimePricingApplies(channelPriced))
	// 共享指针不能被污染
	require.True(t, officialTimePricingApplies(fallback))

	other, err := billing.GetModelPricingForPlatform(PlatformAnthropic, "claude-sonnet-4")
	require.NoError(t, err)
	require.False(t, officialTimePricingApplies(other))
}

func TestDeepSeekOfficialTimeMultiplierRespectsUTCInstant(t *testing.T) {
	// 2026-08-19 01:00 UTC = 09:00 北京，高峰。
	peakUTC := time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier("", "deepseek-v4-flash", peakUTC, false))
	require.Equal(t, 2.0, deepSeekOfficialTimeMultiplier("", "deepseek-v4-flash", peakUTC, true))
	// 2026-08-19 05:00 UTC = 13:00 北京，空闲。
	offPeakUTC := time.Date(2026, 8, 19, 5, 0, 0, 0, time.UTC)
	require.Equal(t, 0.5, deepSeekOfficialTimeMultiplier("", "deepseek-v4-flash", offPeakUTC, false))
	require.Equal(t, 1.0, deepSeekOfficialTimeMultiplier("", "deepseek-v4-flash", offPeakUTC, true))
}

func TestDeepSeekOfficialPriceTimeSchedule(t *testing.T) {
	require.Nil(t, deepSeekOfficialPriceTimeSchedule(PlatformAnthropic, "claude-sonnet-4", true))
	require.Nil(t, deepSeekOfficialPriceTimeSchedule(PlatformAnthropic, "claude-sonnet-4", false))

	// 基准价是空闲价（目录 / 管理端生效价）：高峰要 ×2 才是官方高峰价。
	offPeakBase := deepSeekOfficialPriceTimeSchedule(PlatformDeepSeek, "deepseek-v4-flash", true)
	require.NotNil(t, offPeakBase)
	require.Equal(t, "deepseek_official", offPeakBase.Kind)
	require.Equal(t, "UTC", offPeakBase.Timezone)
	require.Equal(t, []string{"01:00-04:00", "06:00-10:00"}, offPeakBase.PeakWindows)
	require.Equal(t, 2.0, offPeakBase.PeakMultiplier)
	require.Equal(t, 1.0, offPeakBase.OffPeakMultiplier)

	// 基准价是高峰价（代码内官方兜底表）：空闲 ×0.5。
	peakBase := deepSeekOfficialPriceTimeSchedule(PlatformDeepSeek, "deepseek-v4-flash", false)
	require.NotNil(t, peakBase)
	require.Equal(t, 1.0, peakBase.PeakMultiplier)
	require.Equal(t, 0.5, peakBase.OffPeakMultiplier)
}

func TestCalculateCostUnified_DeepSeekOffPeakIsHalfOfPeak(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{
		InputTokens:     1_000_000,
		OutputTokens:    1_000_000,
		CacheReadTokens: 1_000_000,
	}
	resolved := &ResolvedPricing{
		Mode: BillingModeToken,
		BasePricing: &ModelPricing{
			InputPricePerToken:        deepseekFlashOffPeakInputPrice,
			OutputPricePerToken:       deepseekFlashOffPeakOutputPrice,
			CacheReadPricePerToken:    deepseekFlashOffPeakCacheRead,
			OfficialTimePricing:       true,
			OfficialTimeBaseIsOffPeak: true,
		},
		Source:                    PricingSourceFallback,
		longContextPricingEnabled: true,
	}

	peak, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "deepseek-v4-flash",
		Tokens:    tokens,
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: beijingTime(2, 0),
	})
	require.NoError(t, err)
	require.InDelta(t, 2*(deepseekFlashOffPeakInputPrice+deepseekFlashOffPeakOutputPrice+deepseekFlashOffPeakCacheRead)*1_000_000, peak.TotalCost, 1e-10)

	offPeak, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "deepseek-v4-flash",
		Tokens:    tokens,
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: beijingTime(13, 0),
	})
	require.NoError(t, err)
	require.InDelta(t, peak.TotalCost*0.5, offPeak.TotalCost, 1e-10)
	require.InDelta(t, peak.ActualCost*0.5, offPeak.ActualCost, 1e-10)
}

// 价格目录（含管理端手动覆盖后的生效价）对官方分时 SKU 存的是空闲价：
// 空闲时段按目录价原样结算，高峰时段翻倍。
func TestCalculateCostUnified_DeepSeekCatalogBaseIsOffPeak(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	resolved := &ResolvedPricing{
		Mode: BillingModeToken,
		BasePricing: &ModelPricing{
			InputPricePerToken:        0.14e-6,
			OutputPricePerToken:       0.28e-6,
			OfficialTimePricing:       true,
			OfficialTimeBaseIsOffPeak: true,
		},
		Source:                    PricingSourceModelPrice,
		longContextPricingEnabled: true,
	}
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000}

	offPeak, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "deepseek-v4-flash",
		Platform:  PlatformDeepSeek,
		Tokens:    tokens,
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: beijingTime(13, 0),
	})
	require.NoError(t, err)
	require.InDelta(t, (deepseekFlashOffPeakInputPrice+deepseekFlashOffPeakOutputPrice)*1_000_000, offPeak.TotalCost, 1e-10)

	peak, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "deepseek-v4-flash",
		Platform:  PlatformDeepSeek,
		Tokens:    tokens,
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: beijingTime(2, 0),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeak.TotalCost*2, peak.TotalCost, 1e-10)
	require.InDelta(t, offPeak.ActualCost*2, peak.ActualCost, 1e-10)
}

// 官方峰谷是官方报价表自带的属性。分组价 / 渠道价 / 管理员手动覆盖价都是显式定价，
// 叠加官方倍率会让设定价在一天 17 小时（71%）的空闲时段被打对折。
func TestCalculateCostUnified_DeepSeekTimeDoesNotApplyToExplicitPricing(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)

	cases := []struct {
		name   string
		source string
	}{
		{name: "group pricing", source: PricingSourceGroup},
		{name: "channel pricing without time periods", source: PricingSourceChannel},
		{name: "admin model price override", source: PricingSourceModelPrice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := &ResolvedPricing{
				Mode:                      BillingModeToken,
				BasePricing:               &ModelPricing{InputPricePerToken: 0.001, OperatorOverride: tc.source == PricingSourceModelPrice},
				Source:                    tc.source,
				longContextPricingEnabled: true,
			}
			if tc.source == PricingSourceChannel {
				resolved.channelPricing = &ChannelModelPricing{BillingMode: BillingModeToken}
			}

			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx:       context.Background(),
				Model:     "deepseek-v4-pro",
				Platform:  PlatformDeepSeek,
				Tokens:    UsageTokens{InputTokens: 1000},
				Resolver:  &ModelPricingResolver{},
				Resolved:  resolved,
				PricingAt: beijingTime(13, 0),
			})
			require.NoError(t, err)
			require.InDelta(t, 1.0, cost.TotalCost, 1e-12)
		})
	}
}

func TestCalculateCostUnified_ChannelTimePricingOverridesDeepSeekOfficial(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	resolved := &ResolvedPricing{
		Mode:        BillingModeToken,
		BasePricing: &ModelPricing{InputPricePerToken: 0.001},
		Source:      PricingSourceChannel,
		channelPricing: &ChannelModelPricing{
			BillingMode: BillingModeToken,
			TimePricing: &ChannelTimePricing{
				Timezone: "Asia/Shanghai",
				Periods: []ChannelTimePricingPeriod{{
					StartTime:  "09:00",
					EndTime:    "12:00",
					Multiplier: 2,
				}},
			},
		},
		longContextPricingEnabled: true,
	}

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:      context.Background(),
		Model:    "deepseek-v4-flash",
		Tokens:   UsageTokens{InputTokens: 1000},
		Resolver: &ModelPricingResolver{},
		Resolved: resolved,
		// 10:00 Asia/Shanghai is 02:00 UTC; the channel period is evaluated
		// in its declared Asia/Shanghai timezone.
		PricingAt: time.Date(2026, 8, 19, 10, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)),
	})
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12)
}
