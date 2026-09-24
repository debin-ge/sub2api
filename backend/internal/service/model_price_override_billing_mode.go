package service

import (
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 模型价格覆盖的计费方式（billing_mode）：声明该模型的**用途**，而不是价格单位。
//
// 图片与视频都可能按 token 计费，所以“image 档”不等于“只认 output_cost_per_image”：
// 字段按用途分档，价格单位却跨用途复用——
//   - image 档：用途是出图。价格可以来自图片字段（按张 / 按图片 token），也可以直接
//     复用 token 字段（出图按普通 token 计价）。目录里的 gpt-image-1 正是两者都有
//     （input_cost_per_token 5e-06 与 output_cost_per_image_token 4e-05）。因此
//     image 档不抑制 token 价，只屏蔽视频档。
//   - video 档：用途是出视频。按 token 计费的视频也有专门归宿——video_pricing 里的
//     billing_unit: video_token（见 video_pricing_config.go），所以 video 档抑制继承的
//     token 与图片价。
//   - token 档：默认档，也是 275 迁移前所有存量行的语义：不抑制任何目录价、不限制
//     payload 字段。图片 token 本就按 token 档计费（见 BillingModeImage 的注释），
//     所以 token 档不能抹掉目录图片价，否则 gpt-image 之类模型的存量覆盖会静默退化到文本价。
//
// 无论哪一档，video_pricing 永不置空（见 suppressInheritedDimensions）。
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

// payloadFieldBillingMode 返回 payload 价格字段所属的计费档。图片模型既可能按张/按图片
// token 计价，也可能按普通 token 计价（见文件头注释），所以 image 档与 token 档共享
// 除 video_pricing 以外的全部价格字段，只有 video_pricing 是 video 档专属。
func payloadFieldBillingMode(field string) BillingMode {
	if field == videoPayloadField {
		return BillingModeVideo
	}
	return BillingModeToken
}

const videoPayloadField = "video_pricing"

// validatePayloadFieldsInBillingMode 检查 payload 的价格字段是否属于声明的计费档。
// token / image 档接受除 video_pricing 以外的全部价格字段（图片按 token 计费是正常配置）；
// video 档只接受 video_pricing，写入 token / 图片价格会被抑制，属于配置错误。
func validatePayloadFieldsInBillingMode(payload *ModelPriceOverridePayload, mode BillingMode) error {
	if mode == BillingModeToken {
		return nil
	}
	for field := range payloadToMap(payload) {
		if !isPayloadPriceField(field) {
			continue
		}
		if (payloadFieldBillingMode(field) == BillingModeVideo) != (mode == BillingModeVideo) {
			return infraerrors.BadRequest("FIELD_NOT_IN_BILLING_MODE",
				fmt.Sprintf("%s does not belong to billing mode %s", field, mode)).
				WithMetadata(map[string]string{"field": field, "billing_mode": string(mode)})
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
// token 档不抑制任何东西（存量语义）。image 档保留 token 与图片价——图片模型按普通
// token 计价是正常配置。video 档抑制 token 与图片价：视频按 token 计费时用的是
// video_pricing 里的 video_token 单价，不需要目录的文本/图片价。
//
// video_pricing 在任何一档都原样保留：它同时承载 estimator / resolution 等技术档案，
// 渠道/分组级视频定价在使用前会检查 Enabled，改动它会让那些定价丢失分辨率归一化
// （命中更便宜的兜底规则）或直接报 ESTIMATOR_MISSING。image 档对目录视频价的屏蔽
// 改由 ModelPriceEntry.BillingMode + catalogVideoPricingActive 完成。
func suppressInheritedDimensions(raw *RawModelPriceEntry, mode BillingMode) {
	if raw == nil || mode != BillingModeVideo {
		return
	}
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
	raw.OutputCostPerImage = nil
	raw.OutputCostPerImageToken = nil
	raw.InputCostPerImageToken = nil
	raw.CacheReadInputImageTokenCost = nil
}

// rawHasBillingModePricing 判断合并后的条目在 mode 档是否有任何价格。
// image 档同时接受 token 价与图片价：图片按 token 计费时价格就写在 token 字段上，
// 若只认图片字段，这类模型会被误判成 EMPTY_PRICING。
func rawHasBillingModePricing(raw *RawModelPriceEntry, mode BillingMode) bool {
	if raw == nil {
		return false
	}
	switch mode {
	case BillingModeImage:
		return raw.OutputCostPerImage != nil || raw.OutputCostPerImageToken != nil ||
			raw.InputCostPerImageToken != nil || raw.InputCostPerToken != nil || raw.OutputCostPerToken != nil
	case BillingModeVideo:
		// 显式关闭的视频价不算有价：否则 video 档会抹掉 token 价、却没有可用的视频价。
		return raw.VideoPricing != nil && raw.VideoPricing.Enabled
	default:
		return raw.InputCostPerToken != nil || raw.OutputCostPerToken != nil
	}
}

// catalogVideoPricingActive 判断条目自身的视频价能否作为最终报价来源（目录兜底）。
// image 档覆盖声明模型用途是出图，目录视频价不再生效；VideoPricing 仍作为技术档案
// 供渠道/分组视频定价使用，见 suppressInheritedDimensions。
func catalogVideoPricingActive(entry *ModelPriceEntry) bool {
	return entry != nil && entry.VideoPricing != nil && entry.VideoPricing.Enabled &&
		entry.BillingMode != BillingModeImage
}
