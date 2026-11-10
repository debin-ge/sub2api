package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 模型价格覆盖的计费方式（billing_mode）。图片按张与按 token 二选一：
//   - image 档 = 出图按张：只有默认按张价 output_cost_per_image 与 1K/2K/4K 档位价
//     intervals。继承自目录的文本 token 价、图片 token 价一律抹掉，所以 image 档
//     模型不能再走 token 计费（对话请求会因缺价被拒，不会再被按 1 张收费）。
//   - token 档 = 按 token：文本 token 价用于对话；出图按 token 时只用两项价——
//     图片输入价 input_cost_per_image_token、图片输出价 output_cost_per_image_token
//     （提示词与缓存都按图片输入价）。token 档不再带按张价：继承的
//     output_cost_per_image / intervals 会被抹掉，payload 里写了也不接受。
//   - video 档：用途是出视频。按 token 计费的视频有专门归宿——video_pricing 里的
//     billing_unit: video_token（见 video_pricing_config.go），所以 video 档抑制继承的
//     token 与图片价。
//
// 275 迁移前的存量行都是 token 档。289 迁移把带按张价的出图行改成了 image 档；
// 迁移规则外的存量行在运行时按档过滤（projectPayloadToBillingMode），被忽略的字段
// 会在后台标出来。无论哪一档，video_pricing 永不置空（见 suppressInheritedDimensions）。
var modelPriceOverrideBillingModes = []BillingMode{BillingModeToken, BillingModeImage, BillingModeVideo}

// NormalizeModelPriceOverrideBillingMode 校验并归一覆盖行的计费方式，空值视为 token。
func NormalizeModelPriceOverrideBillingMode(mode string) (BillingMode, error) {
	normalized := BillingMode(strings.ToLower(strings.TrimSpace(mode)))
	if normalized == "" {
		return BillingModeToken, nil
	}
	for _, allowed := range modelPriceOverrideBillingModes {
		if normalized == allowed {
			return normalized, nil
		}
	}
	return "", infraerrors.BadRequest("INVALID_BILLING_MODE", "billing_mode must be token, image or video").
		WithMetadata(map[string]string{"billing_mode": mode})
}

func modelPriceOverrideBillingModeOrToken(mode BillingMode) BillingMode {
	if mode == "" {
		return BillingModeToken
	}
	return mode
}

// payloadFieldBillingMode 返回 payload 价格字段所属的计费档：按张价归 image 档，
// video_pricing 归 video 档，其余（文本 token 价、图片 token 价）归 token 档。
func payloadFieldBillingMode(field string) BillingMode {
	switch field {
	case videoPayloadField:
		return BillingModeVideo
	case perImagePayloadField, intervalsPayloadField:
		return BillingModeImage
	default:
		return BillingModeToken
	}
}

// payloadFieldAllowedInBillingMode 判断字段能否写在 mode 档里。token 档另外放行
// video_pricing：275 迁移前的存量 token 行可能带视频价，后台编辑这些行时不能报错。
func payloadFieldAllowedInBillingMode(field string, mode BillingMode) bool {
	owner := payloadFieldBillingMode(field)
	return owner == mode || (mode == BillingModeToken && owner == BillingModeVideo)
}

const (
	perImagePayloadField  = "output_cost_per_image"
	intervalsPayloadField = "intervals"
)

const videoPayloadField = "video_pricing"

// validatePayloadFieldsInBillingMode 检查 payload 的价格字段是否属于声明的计费档：
// image 档只接受按张价；token 档不接受按张价；video 档只接受 video_pricing。
func validatePayloadFieldsInBillingMode(payload *ModelPriceOverridePayload, mode BillingMode) error {
	fields := make([]string, 0, 8)
	for field := range payloadToMap(payload) {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if !isPayloadPriceField(field) || payloadFieldAllowedInBillingMode(field, mode) {
			continue
		}
		return infraerrors.BadRequest("FIELD_NOT_IN_BILLING_MODE",
			fmt.Sprintf("%s does not belong to billing mode %s", field, mode)).
			WithMetadata(map[string]string{"field": field, "billing_mode": string(mode)})
	}
	if len(payload.Intervals) > 0 {
		if mode != BillingModeImage {
			return infraerrors.BadRequest("FIELD_NOT_IN_BILLING_MODE", fmt.Sprintf("intervals does not belong to billing mode %s", mode)).
				WithMetadata(map[string]string{"field": "intervals", "billing_mode": string(mode)})
		}
		seen := make(map[string]struct{}, len(payload.Intervals))
		for i, interval := range payload.Intervals {
			label := strings.ToUpper(strings.TrimSpace(interval.TierLabel))
			switch label {
			case ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K:
			default:
				return infraerrors.BadRequest("INVALID_IMAGE_INTERVAL", fmt.Sprintf("intervals[%d].tier_label must be 1K, 2K or 4K", i))
			}
			if _, exists := seen[label]; exists {
				return infraerrors.BadRequest("INVALID_IMAGE_INTERVAL", fmt.Sprintf("duplicate image tier %s", label))
			}
			seen[label] = struct{}{}
			if interval.PerRequestPrice == nil || math.IsNaN(*interval.PerRequestPrice) || math.IsInf(*interval.PerRequestPrice, 0) || *interval.PerRequestPrice < 0 {
				return infraerrors.BadRequest("INVALID_IMAGE_INTERVAL", fmt.Sprintf("intervals[%d].per_request_price must be finite and non-negative", i))
			}
			if interval.MinTokens != 0 || interval.MaxTokens != nil || interval.InputPrice != nil || interval.OutputPrice != nil || interval.CacheWritePrice != nil || interval.CacheWrite1hPrice != nil || interval.CacheReadPrice != nil || interval.InputMultiplier != nil || interval.OutputMultiplier != nil || interval.CacheWriteMultiplier != nil || interval.CacheReadMultiplier != nil || interval.Conditions != nil || interval.BillingUnit != nil {
				return infraerrors.BadRequest("INVALID_IMAGE_INTERVAL", fmt.Sprintf("intervals[%d] may only contain tier_label and per_request_price", i))
			}
		}
	}
	return nil
}

// isPayloadPriceField 区分价格字段与 supports_* / litellm_provider / mode 这类元数据，
// 后者不归属任何计费档。
func isPayloadPriceField(field string) bool {
	switch field {
	case "supports_service_tier", "supports_prompt_caching", "litellm_provider", "mode":
		return false
	default:
		return true
	}
}

// suppressInheritedDimensions 抹掉从目录继承、但不属于 mode 的价格。
//   - image 档：抹掉文本 token 价与图片 token 价，只留按张价。
//   - token 档：抹掉按张价（output_cost_per_image、intervals）。
//   - video 档：抹掉 token 与图片价，视频按 token 计费时用的是 video_pricing 里的
//     video_token 单价。
//
// video_pricing 在任何一档都原样保留：它同时承载 estimator / resolution 等技术档案，
// 渠道/分组级视频定价在使用前会检查 Enabled，改动它会让那些定价丢失分辨率归一化
// （命中更便宜的兜底规则）或直接报 ESTIMATOR_MISSING。image 档对目录视频价的屏蔽
// 改由 ModelPriceEntry.BillingMode + catalogVideoPricingActive 完成。
func suppressInheritedDimensions(raw *RawModelPriceEntry, mode BillingMode) {
	if raw == nil {
		return
	}
	switch mode {
	case BillingModeImage:
		clearRawTokenPrices(raw)
		clearRawImageTokenPrices(raw)
	case BillingModeVideo:
		clearRawTokenPrices(raw)
		clearRawImageTokenPrices(raw)
		raw.OutputCostPerImage = nil
	default:
		raw.OutputCostPerImage = nil
		raw.Intervals = nil
	}
}

func clearRawTokenPrices(raw *RawModelPriceEntry) {
	raw.InputCostPerToken = nil
	raw.OutputCostPerToken = nil
	raw.InputCostPerTokenPriority = nil
	raw.OutputCostPerTokenPriority = nil
	raw.CacheCreationInputTokenCost = nil
	raw.CacheCreationInputTokenCostPriority = nil
	raw.CacheCreationInputTokenCostAbove1hr = nil
	raw.CacheReadInputTokenCost = nil
	raw.CacheReadInputTokenCostPriority = nil
	raw.LongContextInputTokenThreshold = nil
	raw.LongContextInputCostMultiplier = nil
	raw.LongContextOutputCostMultiplier = nil
}

func clearRawImageTokenPrices(raw *RawModelPriceEntry) {
	raw.OutputCostPerImageToken = nil
	raw.InputCostPerImageToken = nil
	raw.CacheReadInputImageTokenCost = nil
}

// projectPayloadToBillingMode 在运行时按档过滤覆盖行的 payload：不属于 mode 的价格
// 字段（289 迁移规则外的存量行）直接忽略，不改库里的数据。ignored 按字段名排序，
// 供后台标记「被忽略字段」。
func projectPayloadToBillingMode(payload *ModelPriceOverridePayload, mode BillingMode) (ModelPriceOverridePayload, []string) {
	if payload == nil {
		return ModelPriceOverridePayload{}, nil
	}
	projected := cloneModelPriceOverridePayload(*payload)
	mode = modelPriceOverrideBillingModeOrToken(mode)
	var ignored []string
	drop := func(field string, clear func()) {
		if !payloadFieldAllowedInBillingMode(field, mode) {
			ignored = append(ignored, field)
			clear()
		}
	}
	if projected.InputCostPerToken != nil {
		drop("input_cost_per_token", func() { projected.InputCostPerToken = nil })
	}
	if projected.OutputCostPerToken != nil {
		drop("output_cost_per_token", func() { projected.OutputCostPerToken = nil })
	}
	if projected.CacheCreationInputTokenCost != nil {
		drop("cache_creation_input_token_cost", func() { projected.CacheCreationInputTokenCost = nil })
	}
	if projected.CacheCreationInputTokenCostAbove1hr != nil {
		drop("cache_creation_input_token_cost_above_1hr", func() { projected.CacheCreationInputTokenCostAbove1hr = nil })
	}
	if projected.CacheReadInputTokenCost != nil {
		drop("cache_read_input_token_cost", func() { projected.CacheReadInputTokenCost = nil })
	}
	if projected.InputCostPerTokenPriority != nil {
		drop("input_cost_per_token_priority", func() { projected.InputCostPerTokenPriority = nil })
	}
	if projected.OutputCostPerTokenPriority != nil {
		drop("output_cost_per_token_priority", func() { projected.OutputCostPerTokenPriority = nil })
	}
	if projected.CacheCreationInputTokenCostPriority != nil {
		drop("cache_creation_input_token_cost_priority", func() { projected.CacheCreationInputTokenCostPriority = nil })
	}
	if projected.CacheReadInputTokenCostPriority != nil {
		drop("cache_read_input_token_cost_priority", func() { projected.CacheReadInputTokenCostPriority = nil })
	}
	if projected.LongContextInputTokenThreshold != nil {
		drop("long_context_input_token_threshold", func() { projected.LongContextInputTokenThreshold = nil })
	}
	if projected.LongContextInputCostMultiplier != nil {
		drop("long_context_input_cost_multiplier", func() { projected.LongContextInputCostMultiplier = nil })
	}
	if projected.LongContextOutputCostMultiplier != nil {
		drop("long_context_output_cost_multiplier", func() { projected.LongContextOutputCostMultiplier = nil })
	}
	if projected.OutputCostPerImage != nil {
		drop(perImagePayloadField, func() { projected.OutputCostPerImage = nil })
	}
	if projected.OutputCostPerImageToken != nil {
		drop("output_cost_per_image_token", func() { projected.OutputCostPerImageToken = nil })
	}
	if projected.InputCostPerImageToken != nil {
		drop("input_cost_per_image_token", func() { projected.InputCostPerImageToken = nil })
	}
	if len(projected.Intervals) > 0 {
		drop(intervalsPayloadField, func() { projected.Intervals = nil })
	}
	if projected.VideoPricing != nil {
		drop(videoPayloadField, func() { projected.VideoPricing = nil })
	}
	sort.Strings(ignored)
	return projected, ignored
}

// rawHasBillingModePricing 判断合并后的条目在 mode 档是否有任何价格。
// image 档只认按张价；token 档认文本 token 价或图片 token 价。
func rawHasBillingModePricing(raw *RawModelPriceEntry, mode BillingMode) bool {
	if raw == nil {
		return false
	}
	switch mode {
	case BillingModeImage:
		return len(raw.Intervals) > 0 || raw.OutputCostPerImage != nil
	case BillingModeVideo:
		// 显式关闭的视频价不算有价：否则 video 档会抹掉 token 价、却没有可用的视频价。
		return raw.VideoPricing != nil && raw.VideoPricing.Enabled
	default:
		return raw.InputCostPerToken != nil || raw.OutputCostPerToken != nil ||
			raw.InputCostPerImageToken != nil || raw.OutputCostPerImageToken != nil
	}
}

// rawRequiresImageTokenPair 判断 token 档条目是否是「按 token 出图」的模型：目录
// mode=image_generation，或 payload 自己写了图片输出价。这类条目的图片输入价、
// 图片输出价必须成对。只看图的对话模型（只有图片输入价）不受限制。
func rawRequiresImageTokenPair(raw *RawModelPriceEntry, payload *ModelPriceOverridePayload) bool {
	if raw != nil && strings.EqualFold(strings.TrimSpace(raw.Mode), imageGenerationCatalogMode) {
		return true
	}
	return payload != nil && payload.OutputCostPerImageToken != nil
}

const imageGenerationCatalogMode = "image_generation"

// deriveImageBillingMode 推导条目的出图方式（方案 §6.3）：
//   - 覆盖行：image 档 → 按张；token 档且有图片输出价 → 按 token；其余为空。
//   - 目录：mode 不是 image_generation → 空（gemini-3.1-pro-* 上的按张价不算数）；
//     有按张价 → 按张（两种价都有时按张，D8）；只有图片输出 token 价 → 按 token。
func deriveImageBillingMode(entry *ModelPriceEntry, overrideMode BillingMode, fromOverride bool) BillingMode {
	if entry == nil {
		return ""
	}
	if fromOverride {
		switch modelPriceOverrideBillingModeOrToken(overrideMode) {
		case BillingModeImage:
			return BillingModeImage
		case BillingModeToken:
			if entryHasImageOutputTokenPrice(entry) {
				return BillingModeToken
			}
		}
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(entry.Mode), imageGenerationCatalogMode) {
		return ""
	}
	if entryHasPerImagePrice(entry) {
		return BillingModeImage
	}
	if entryHasImageOutputTokenPrice(entry) {
		return BillingModeToken
	}
	return ""
}

func entryHasPerImagePrice(entry *ModelPriceEntry) bool {
	if entry == nil {
		return false
	}
	for _, interval := range entry.Intervals {
		if validConfiguredPrice(interval.PerRequestPrice) {
			return true
		}
	}
	return isFiniteNonNegativePrice(entry.OutputCostPerImage) &&
		(entry.OutputCostPerImage > 0 || entry.OutputCostPerImageExplicit)
}

func entryHasImageOutputTokenPrice(entry *ModelPriceEntry) bool {
	return entry != nil && isFiniteNonNegativePrice(entry.OutputCostPerImageToken) &&
		(entry.ImageOutputPriceExplicit || (!entry.PricePresenceKnown && entry.OutputCostPerImageToken > 0))
}

// catalogVideoPricingActive 判断条目自身的视频价能否作为最终报价来源（目录兜底）。
// image 档覆盖声明模型用途是出图，目录视频价不再生效；VideoPricing 仍作为技术档案
// 供渠道/分组视频定价使用，见 suppressInheritedDimensions。
func catalogVideoPricingActive(entry *ModelPriceEntry) bool {
	return entry != nil && entry.VideoPricing != nil && entry.VideoPricing.Enabled &&
		entry.BillingMode != BillingModeImage
}
