package service

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// 记账层对成本计算错误的分类是"不扣费但留痕"与"重试"之间的分水岭：
// 分错方向就会把一次数据库抖动伪造成一条 $0 的已结算记录，永久丢掉这笔钱。
func TestClassifyRecordUsageCostError(t *testing.T) {
	t.Run("nil_is_neither", func(t *testing.T) {
		unpriced, fatal := classifyRecordUsageCostError(nil)
		require.False(t, unpriced)
		require.NoError(t, fatal)
	})

	t.Run("pricing_unavailable_is_recorded_not_retried", func(t *testing.T) {
		err := fmt.Errorf("no pricing available for model: %s: %w", "mystery-model", ErrModelPricingUnavailable)
		unpriced, fatal := classifyRecordUsageCostError(err)
		require.True(t, unpriced)
		require.NoError(t, fatal)
	})

	t.Run("dependency_failure_propagates", func(t *testing.T) {
		// DB 抖动不是"这个模型没价格"，重试是有意义的，绝不能标成 pricing_unavailable
		// 然后按 $0 落库 —— 那等于把一次可恢复的故障变成永久漏账。
		dbErr := fmt.Errorf("query channel pricing: %w", sql.ErrConnDone)
		unpriced, fatal := classifyRecordUsageCostError(dbErr)
		require.False(t, unpriced)
		require.ErrorIs(t, fatal, sql.ErrConnDone)
	})

	t.Run("unwraps_through_nesting", func(t *testing.T) {
		err := fmt.Errorf("calculate image cost: %w",
			fmt.Errorf("%w: invalid per-request price for model %s", ErrModelPricingUnavailable, "img-x"))
		unpriced, fatal := classifyRecordUsageCostError(err)
		require.True(t, unpriced)
		require.NoError(t, fatal)
	})

	t.Run("sentinel_lookalike_is_not_matched", func(t *testing.T) {
		unpriced, fatal := classifyRecordUsageCostError(errors.New("model pricing unavailable"))
		require.False(t, unpriced)
		require.Error(t, fatal)
	})
}

// 未定价记录仍要带上正确的计费模式，否则管理端无法按"哪类流量在漏计"分面排查。
func TestUnpricedCostBreakdown(t *testing.T) {
	t.Run("token_by_default", func(t *testing.T) {
		cost := unpricedCostBreakdown(BillingKindUnspecified, 0, 0)
		require.Equal(t, string(BillingModeToken), cost.BillingMode)
		require.Zero(t, cost.TotalCost)
		require.Zero(t, cost.ActualCost)
	})

	t.Run("image", func(t *testing.T) {
		require.Equal(t, string(BillingModeImage), unpricedCostBreakdown(BillingKindUnspecified, 3, 0).BillingMode)
	})

	t.Run("video_wins_over_image", func(t *testing.T) {
		// 视频用量常同时带图片计数（首帧等），按视频归类才对得上计费口径。
		require.Equal(t, string(BillingModeVideo), unpricedCostBreakdown(BillingKindUnspecified, 1, 1).BillingMode)
	})

	t.Run("explicit_kind_survives_missing_upstream_counts", func(t *testing.T) {
		// 这正是漏计最难查的一类：videos_* 路由没拿到 video_count，若按计数反推
		// 就会被标成 token，管理端再也看不出"视频在漏计"。
		require.Equal(t, string(BillingModeVideo), unpricedCostBreakdown(BillingKindVideo, 0, 0).BillingMode)
		require.Equal(t, string(BillingModeImage), unpricedCostBreakdown(BillingKindImage, 0, 0).BillingMode)
	})

	t.Run("token_kind_still_follows_upstream_counts", func(t *testing.T) {
		// token 只是基线：对话里真产出了图片，未定价记录也该归到图片口径。
		require.Equal(t, string(BillingModeImage), unpricedCostBreakdown(BillingKindToken, 2, 0).BillingMode)
	})
}

// BillingKind 的两个方向必须区分：媒体口径由入口独占，token 口径接受上游升级。
// 搞反任一方向都会让"图片路由降级成 token→无 token 价→免费"这条路重新打开。
func TestBillingKindResponseDrivenMediaOverride(t *testing.T) {
	for _, kind := range []BillingKind{BillingKindToken, BillingKindUnspecified} {
		require.True(t, kind.ResponseDrivenMediaOverride(), "kind %s should stay response-driven", kind)
	}
	for _, kind := range []BillingKind{BillingKindImage, BillingKindVideo, BillingKindWebSearch, BillingKindNone} {
		require.False(t, kind.ResponseDrivenMediaOverride(), "kind %s must be authoritative", kind)
	}
}

func TestBillingKindRequiresPricing(t *testing.T) {
	for _, kind := range []BillingKind{BillingKindToken, BillingKindImage, BillingKindVideo, BillingKindWebSearch} {
		require.True(t, kind.RequiresPricing(), "kind %s must be priced before forwarding", kind)
	}
	// count_tokens 本地算完就返回，没有上游成本可查价；未指定则由调用方按旧逻辑决定。
	require.False(t, BillingKindNone.RequiresPricing())
	require.True(t, BillingKindUnspecified.RequiresPricing())
}

func TestParseBillingKind(t *testing.T) {
	require.Equal(t, BillingKindWebSearch, ParseBillingKind("  Web_Search "))
	require.Equal(t, BillingKindUnspecified, ParseBillingKind("images"))
	require.Equal(t, "unspecified", BillingKindUnspecified.String())
}
