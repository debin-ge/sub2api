//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuiltinGeminiCacheStoragePrice(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  float64
	}{
		// 已发布型号
		{"gemini-3.8-flash", 0.5e-6},
		{"gemini-3.7-flash", 0.5e-6},
		{"gemini-3.6-flash", 0.5e-6},
		{"gemini-3.8-flash-cyber", 1e-6},
		{"gemini-3.5-flash", 1e-6},
		{"gemini-3.5-flash-lite", 1e-6},
		{"gemini-3.1-flash-lite", 1e-6},
		{"gemini-3.1-pro-preview", 4.5e-6},
		{"gemini-3.1-pro-preview-customtools", 4.5e-6},
		{"gemini-3-flash-preview", 1e-6},
		{"gemini-3-pro-preview", 4.5e-6},
		{"gemini-2.5-pro", 4.5e-6},
		{"gemini-2.5-flash", 1e-6},
		{"gemini-2.5-flash-lite", 1e-6},
		{"gemini-2.0-flash-001", 1e-6},
		{"gemini-2.0-flash-lite-001", 1e-6},
		// 资源名与大小写
		{"models/gemini-3.8-flash", 0.5e-6},
		{"projects/p/locations/global/publishers/google/models/gemini-2.5-pro", 4.5e-6},
		{" Gemini-3.8-Flash ", 0.5e-6},
		// 系列兜底：未发布型号与别名按标准价
		{"gemini-3.9-flash", 1e-6},
		{"gemini-3.9-flash-lite", 1e-6},
		{"gemini-3.8-flash-lite", 1e-6},
		{"gemini-3.9-pro-preview", 4.5e-6},
		{"gemini-flash-latest", 1e-6},
		{"gemini-flash-lite-latest", 1e-6},
		{"gemini-pro-latest", 4.5e-6},
		// 无存储价
		{"gemini-embedding-001", 0},
		{"gemini-unknown-model", 0},
		{"claude-sonnet-5-5", 0},
		{"", 0},
	} {
		require.InDelta(t, tc.want, builtinGeminiCacheStoragePriceAt(tc.model, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), 1e-15, tc.model)
	}
}

func TestCacheStoragePricePerTokenHourPriority(t *testing.T) {
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{
		"gemini-3.8-flash":{"litellm_provider":"gemini","input_cost_per_token":0.00000075,"output_cost_per_token":0.00000375,
			"cache_storage_cost_per_token_per_hour":0.000002},
		"gemini-3.9-flash":{"litellm_provider":"gemini","input_cost_per_token":0.0000015,"output_cost_per_token":0.0000075,
			"cache_storage_cost_per_token_per_hour":0.0000007},
		"gemini-2.5-pro":{"litellm_provider":"gemini","input_cost_per_token":0.00000125,"output_cost_per_token":0.00001}}`))
	require.NoError(t, err)
	pricing.pricingData = data
	billing := NewBillingService(&config.Config{}, pricing)

	require.InDelta(t, 2e-6, billing.cacheStoragePricePerTokenHourAt("gemini-3.8-flash", at), 1e-15, "价格表配置值优先于内置型号价")
	require.InDelta(t, 0.7e-6, billing.cacheStoragePricePerTokenHourAt("gemini-3.9-flash", at), 1e-15, "价格表配置值优先于内置系列兜底价")
	require.InDelta(t, 4.5e-6, billing.cacheStoragePricePerTokenHourAt("gemini-2.5-pro", at), 1e-15, "价格表条目缺存储价时取内置型号价")
	require.InDelta(t, 0.5e-6, billing.cacheStoragePricePerTokenHourAt("gemini-3.7-flash", at), 1e-15, "价格表无条目时取内置型号价")
	require.InDelta(t, 4.5e-6, billing.cacheStoragePricePerTokenHourAt("gemini-3.9-pro", at), 1e-15, "内置型号未命中时取系列兜底价")
	require.Zero(t, billing.cacheStoragePricePerTokenHourAt("customer-alias", at))

	noCatalog := NewBillingService(&config.Config{}, nil)
	require.InDelta(t, 0.5e-6, noCatalog.cacheStoragePricePerTokenHourAt("gemini-3.8-flash", at), 1e-15, "无价格服务时取内置价")
}

func TestCacheStoragePriceIndependentAndPromotionExpiry(t *testing.T) {
	before := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	require.InDelta(t, 0.5e-6, builtinGeminiCacheStoragePriceAt("gemini-3.8-flash", before), 1e-15)
	require.InDelta(t, 1e-6, builtinGeminiCacheStoragePriceAt("gemini-3.8-flash", after), 1e-15)
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{"storage-only":{"cache_storage_cost_per_token_per_hour":0.000002},"gemini-3.8-flash":{"cache_storage_cost_per_token_per_hour":0.000003}}`))
	require.NoError(t, err)
	pricing.pricingData = data
	billing := NewBillingService(nil, pricing)
	require.InDelta(t, 2e-6, billing.cacheStoragePricePerTokenHourAt("storage-only", after), 1e-15)
	require.InDelta(t, 3e-6, billing.cacheStoragePricePerTokenHourAt("gemini-3.8-flash", after), 1e-15)
	recovery := &BillingRecoveryService{billingService: NewBillingService(nil, nil)}
	cost, err := recovery.recomputeCost(context.Background(), &UsageLog{Model: "gemini-3.8-flash", CreatedAt: before, CacheStorageTokenHours: 1000000, RateMultiplier: 1}, "gemini-3.8-flash", nil, nil)
	require.NoError(t, err)
	require.InDelta(t, 0.5, cost.CacheStorageCost, 1e-12, "historical recovery uses the original event time")
}

func TestCacheStorageExplicitZeroPreservesFreePricingAndRecovery(t *testing.T) {
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{"gemini-2.5-pro":{"cache_storage_cost_per_token_per_hour":0},"free-alias":{"cache_storage_cost_per_token_per_hour":0},"invalid-storage":{"cache_storage_cost_per_token_per_hour":-1},"gemini-3.8-flash":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`))
	require.NoError(t, err)
	// Presence survives catalog conversions and unrelated token-price overrides.
	require.True(t, buildModelPriceEntry("gemini-2.5-pro", rawOf(data["gemini-2.5-pro"])).CacheStoragePriceExplicit)
	require.False(t, buildModelPriceEntry("gemini-3.8-flash", rawOf(data["gemini-3.8-flash"])).CacheStoragePriceExplicit)
	require.True(t, buildOverrideModelPriceEntry("gemini-2.5-pro", data["gemini-2.5-pro"], &ModelPriceOverride{}).CacheStoragePriceExplicit)
	pricing.pricingData = data
	billing := NewBillingService(nil, pricing)
	gateway := &GatewayService{billingService: billing}
	key := &APIKey{Group: &Group{Platform: PlatformGemini}}
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, model := range []string{"gemini-2.5-pro", "free-alias"} {
		result := &ForwardResult{Model: model, UpstreamModel: "gemini-3.8-flash", CacheStorageTokenHours: 1000000}
		price, known := gateway.cacheStoragePrice(result, model, at)
		require.True(t, known)
		require.Zero(t, price, "free rate must not fall back to a paid model")
		require.True(t, gateway.CacheStoragePriced(context.Background(), &RecordUsageInput{APIKey: key, Result: result, PricingAt: at}))
		cost, err := gateway.calculateRecordUsageCost(context.Background(), result, key, model, 2, 2, at)
		require.NoError(t, err)
		require.Zero(t, cost.TotalCost)
		require.Zero(t, cost.ActualCost)
		recovery := &BillingRecoveryService{billingService: billing}
		cost, err = recovery.recomputeCost(context.Background(), &UsageLog{Model: model, CreatedAt: at, CacheStorageTokenHours: 1000000, RateMultiplier: 2}, model, key, nil)
		require.NoError(t, err)
		require.Zero(t, cost.TotalCost)
	}
	invalid := &ForwardResult{Model: "invalid-storage", UpstreamModel: "gemini-3.8-flash", CacheStorageTokenHours: 1}
	require.False(t, gateway.CacheStoragePriced(context.Background(), &RecordUsageInput{APIKey: key, Result: invalid, PricingAt: at}))
	_, err = gateway.calculateRecordUsageCost(context.Background(), invalid, key, invalid.Model, 1, 1, at)
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "invalid explicit rate must not fall back to a paid model")
	_, known := billing.lookupCacheStoragePricePerTokenHourAt("missing-alias", at)
	require.False(t, known, "missing remains different from explicitly free")
	price, known := billing.lookupCacheStoragePricePerTokenHourAt("gemini-3.8-flash", at)
	require.True(t, known)
	require.InDelta(t, 0.5e-6, price, 1e-15, "missing field still uses built-in storage pricing")
}
