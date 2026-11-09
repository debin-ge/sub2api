package service

import (
	"context"
	"fmt"
	"strings"
)

// 出图计费：按张与按 token 二选一（docs/图片计费二选一改造方案.md §3）。
//
// 一次出图请求从上往下找第一个声明了出图方式的层，方式就此锁定，不跨层混用（D5）：
//
//	① 分组模型定价卡（groups.model_pricing）
//	② 分组 1K/2K/4K 图片价；分组「按实际出图张数计费」开关（只作用于专用图片端点）
//	③ 渠道模型定价
//	④ 模型价格（覆盖行或目录，见 ModelPriceEntry.ImageBillingMode）
//	⑤ 内置价（Grok imagine，只有按张）
//
// 取价只在同方式的层里找，声明另一种方式的层直接跳过：
//   - 按张：从声明层往下，每层先看档位价、再看该层默认价，最后看 ⑤。④ 的默认价按
//     2K ×1.5、4K ×2 推算（沿用现状）。都没有就拒绝。
//   - 按 token：图片输入价、图片输出价分别从声明层往下找，只认按 token 的层；任一项
//     找不到就拒绝，不再回退文本价。
const (
	ImageBillingDeclaredByGroupCard       = "group_card"
	ImageBillingDeclaredByGroupImagePrice = "group_image_price"
	ImageBillingDeclaredByGroupImageCount = "group_image_count_billing"
	ImageBillingDeclaredByChannel         = "channel"
	ImageBillingDeclaredByModelOverride   = "model_price_override"
	ImageBillingDeclaredByModelCatalog    = "model_price"
	ImageBillingDeclaredByBuiltin         = "builtin"
)

// ImageBillingInput 是一次出图请求的计费解析输入。
type ImageBillingInput struct {
	Model     string
	Platforms []string
	GroupID   *int64
	// Group 提供分组模型定价卡、分组图片价与按张开关。调用方先用
	// apiKeyWithFreshGroupMediaPricing 之类的方法补全分组媒体价字段。
	Group *Group
	// SizeTier 是 1K/2K/4K，空或 auto 按 2K。
	SizeTier string
	// ImageEndpoint 表示专用图片端点；分组「按实际出图张数计费」开关只作用于它。
	ImageEndpoint bool
	// PerImageOnly 表示入口只能按张结算（Grok imagine）；声明成按 token 时拒绝。
	PerImageOnly bool
	// ForceMode 只给补账用：按用量记录里的方式重算，配置里的方式不一致时拒绝。
	ForceMode BillingMode
	// CrossModel 表示计费名和实际转发的模型不同（组合分组的公开别名、模型映射）。
	// 这时只认按张，或由显式配置（分组卡、渠道、覆盖行）声明的按 token：token 用量
	// 属于实际出图的模型，不能拿别名的目录 token 价去乘（方案 §3.2 第 3 条）。
	CrossModel bool
}

// ImageBillingPlan 锁定出图请求的计费方式与价格快照，转发前解析、结算时直接使用。
type ImageBillingPlan struct {
	// Mode 是 BillingModeImage（按张）或 BillingModeToken（按 token）。
	Mode     BillingMode
	Model    string
	SizeTier string
	// DeclaredBy 是声明出图方式的层，取值见 ImageBillingDeclaredBy*。
	DeclaredBy string
	// Source 是单价（按张）或图片输出价（按 token）的取价来源，取值见 PricingSource*。
	Source string
	// UnitPrice 是按张单价（Mode=image）。
	UnitPrice float64
	// ImageInputPrice、ImageOutputPrice 是按 token 的两项价（Mode=token）。
	ImageInputPrice  float64
	ImageOutputPrice float64

	// channelPricing 是参与解析的分组卡或渠道行，结算时取它的推理等级与服务档倍率。
	channelPricing *ChannelModelPricing
}

// OpenAIImageBillingPlan 是 ImageBillingPlan 的旧名，/v1/images 与 Responses 的转发结果沿用它。
type OpenAIImageBillingPlan = ImageBillingPlan

// explicitTokenDeclaration 判断按 token 的计划是否由显式配置声明。
func (p *ImageBillingPlan) explicitTokenDeclaration() bool {
	if p == nil {
		return false
	}
	switch p.DeclaredBy {
	case ImageBillingDeclaredByGroupCard, ImageBillingDeclaredByChannel, ImageBillingDeclaredByModelOverride:
		return true
	default:
		return false
	}
}

// imageBillingLayer 是解析过程中的一层候选：声明的方式与该层能提供的价格。
type imageBillingLayer struct {
	declaredBy string
	source     string
	mode       BillingMode
	row        *ChannelModelPricing
	// 按张：该层的档位价与默认价。
	perImage func(tier string) (float64, bool)
	// 按 token：该层的图片输入价与图片输出价（nil 表示该层没配）。
	imageInput  *float64
	imageOutput *float64
	// invalid 表示显式配置里有非法价格（负数、NaN）。按 token 的层遇到它直接拒绝，
	// 不能悄悄跳过、改用下层的价。
	invalid bool
}

// ResolveImageBilling 按方案 §3.2 解析一次出图请求的计费方式与价格。
// 失败时返回包装了 ErrModelPricingUnavailable 的错误，消息里带声明层、方式与缺的维度。
func (r *ModelPricingResolver) ResolveImageBilling(ctx context.Context, in ImageBillingInput) (*ImageBillingPlan, error) {
	model := strings.TrimSpace(in.Model)
	if r == nil || r.billingService == nil {
		return nil, fmt.Errorf("%w for image model %q: pricing resolver unavailable", ErrModelPricingUnavailable, model)
	}
	if model == "" {
		return nil, fmt.Errorf("%w: image billing model is empty", ErrModelPricingUnavailable)
	}
	tier, err := NormalizeImageBillingTierStrictOrDefault(in.SizeTier)
	if err != nil {
		return nil, err
	}
	layers := r.imageBillingLayers(ctx, in, model)

	declared := -1
	for i := range layers {
		if layers[i].mode != "" {
			declared = i
			break
		}
	}
	if declared < 0 {
		return nil, fmt.Errorf("%w for image model %q tier %s: no layer declares an image billing mode",
			ErrModelPricingUnavailable, model, tier)
	}
	decl := layers[declared]
	if in.PerImageOnly && decl.mode != BillingModeImage {
		return nil, fmt.Errorf("%w for image model %q: declared_by=%s billing_mode=%s, this endpoint only bills per image",
			ErrModelPricingUnavailable, model, decl.declaredBy, decl.mode)
	}
	if in.ForceMode != "" && in.ForceMode != decl.mode {
		return nil, fmt.Errorf("%w for image model %q: declared_by=%s billing_mode=%s does not match recorded billing_mode=%s",
			ErrModelPricingUnavailable, model, decl.declaredBy, decl.mode, in.ForceMode)
	}

	plan := &ImageBillingPlan{
		Mode:       decl.mode,
		Model:      model,
		SizeTier:   tier,
		DeclaredBy: decl.declaredBy,
	}
	switch decl.mode {
	case BillingModeImage:
		if !resolvePerImagePrice(plan, layers[declared:], tier) {
			return nil, fmt.Errorf("%w for image model %q tier %s: declared_by=%s billing_mode=image, no per-image price for this tier",
				ErrModelPricingUnavailable, model, tier, decl.declaredBy)
		}
	case BillingModeToken:
		if missing := resolveImageTokenPrices(plan, layers[declared:]); missing != "" {
			return nil, fmt.Errorf("%w for image model %q: declared_by=%s billing_mode=token, missing %s",
				ErrModelPricingUnavailable, model, decl.declaredBy, missing)
		}
		if in.CrossModel && !plan.explicitTokenDeclaration() {
			return nil, fmt.Errorf("%w for image model %q: declared_by=%s billing_mode=token, catalog token prices do not apply to another upstream model",
				ErrModelPricingUnavailable, model, decl.declaredBy)
		}
	}
	return plan, nil
}

func resolvePerImagePrice(plan *ImageBillingPlan, layers []imageBillingLayer, tier string) bool {
	for _, layer := range layers {
		if layer.mode != BillingModeImage || layer.perImage == nil {
			continue
		}
		if price, ok := layer.perImage(tier); ok {
			plan.UnitPrice = price
			plan.Source = layer.source
			plan.channelPricing = layer.row
			return true
		}
	}
	return false
}

func resolveImageTokenPrices(plan *ImageBillingPlan, layers []imageBillingLayer) string {
	var input, output *float64
	for _, layer := range layers {
		if layer.mode != BillingModeToken {
			continue
		}
		if layer.invalid {
			return fmt.Sprintf("a valid image token price (invalid explicit price at declared_by=%s)", layer.declaredBy)
		}
		if plan.channelPricing == nil && layer.row != nil {
			plan.channelPricing = layer.row
		}
		if input == nil && validConfiguredPrice(layer.imageInput) {
			input = layer.imageInput
		}
		if output == nil && validConfiguredPrice(layer.imageOutput) {
			output = layer.imageOutput
			plan.Source = layer.source
		}
	}
	switch {
	case input == nil:
		return "image input price"
	case output == nil:
		return "image output price"
	}
	plan.ImageInputPrice = *input
	plan.ImageOutputPrice = *output
	return ""
}

// imageBillingLayers 依次列出 ①–⑤ 各层。mode 为空的层没有声明出图方式。
func (r *ModelPricingResolver) imageBillingLayers(ctx context.Context, in ImageBillingInput, model string) []imageBillingLayer {
	layers := make([]imageBillingLayer, 0, 6)

	// ① 分组模型定价卡。
	if card := matchGroupModelPricing(in.Group, model); card != nil {
		layers = append(layers, channelRowImageBillingLayer(card, ImageBillingDeclaredByGroupCard, PricingSourceGroup))
	}

	// ② 分组图片价：配了任意一档即声明按张；按张开关只作用于专用图片端点，价格向下层找。
	if group := in.Group; group != nil {
		if validConfiguredPrice(group.ImagePrice1K) || validConfiguredPrice(group.ImagePrice2K) || validConfiguredPrice(group.ImagePrice4K) {
			layers = append(layers, imageBillingLayer{
				declaredBy: ImageBillingDeclaredByGroupImagePrice,
				source:     PricingSourceGroup,
				mode:       BillingModeImage,
				perImage: func(tier string) (float64, bool) {
					price := group.GetImagePrice(tier)
					if !validConfiguredPrice(price) {
						return 0, false
					}
					return *price, true
				},
			})
		}
		if in.ImageEndpoint && group.ImageCountBilling {
			layers = append(layers, imageBillingLayer{
				declaredBy: ImageBillingDeclaredByGroupImageCount,
				source:     PricingSourceGroup,
				mode:       BillingModeImage,
			})
		}
	}

	// ③ 渠道模型定价。
	groupID := in.GroupID
	if groupID == nil && in.Group != nil && in.Group.ID > 0 {
		id := in.Group.ID
		groupID = &id
	}
	if groupID != nil && r.channelService != nil {
		if row := r.lookupChannelPricingNormalized(ctx, *groupID, model); row != nil {
			layers = append(layers, channelRowImageBillingLayer(row, ImageBillingDeclaredByChannel, PricingSourceChannel))
		}
	}

	// ④ 模型价格（覆盖行或目录）。
	if pricing := r.billingService.pricingService; pricing != nil {
		platforms := normalizePricingPlatforms(append(append([]string(nil), in.Platforms...), groupPlatform(in.Group)))
		if entry := pricing.LookupModelPricingStrictForPlatforms(platforms, model); entry != nil {
			layers = append(layers, modelPriceImageBillingLayer(entry))
		}
	}

	// ⑤ 内置价（Grok imagine）。
	if _, ok := getDefaultGrokImagineImagePrice(model, ImageBillingSize1K); ok {
		layers = append(layers, imageBillingLayer{
			declaredBy: ImageBillingDeclaredByBuiltin,
			source:     PricingSourceModelPrice,
			mode:       BillingModeImage,
			perImage: func(tier string) (float64, bool) {
				return getDefaultGrokImagineImagePrice(model, tier)
			},
		})
	}
	return layers
}

func groupPlatform(group *Group) string {
	if group == nil {
		return ""
	}
	return group.Platform
}

// channelRowImageBillingLayer 把分组卡或渠道行转成一层：image / per_request 档声明按张，
// token 档（含空）声明按 token；video 档不声明出图方式。
func channelRowImageBillingLayer(row *ChannelModelPricing, declaredBy, source string) imageBillingLayer {
	layer := imageBillingLayer{declaredBy: declaredBy, source: source, row: row}
	switch row.BillingMode {
	case BillingModeImage, BillingModePerRequest:
		layer.mode = BillingModeImage
		layer.perImage = func(tier string) (float64, bool) {
			return channelRowPerImagePrice(row, tier)
		}
	case BillingModeToken, "":
		layer.mode = BillingModeToken
		layer.imageInput = row.ImageInputPrice
		layer.imageOutput = row.ImageOutputPrice
		layer.invalid = channelTokenPricingHasInvalidPrice(row) || hasInvalidTokenPrice(row.ImageInputPrice, row.ImageOutputPrice)
	}
	return layer
}

// channelRowPerImagePrice 取按张档位价：先看档位标签，再看不带标签的上下文档的最低档
// （渠道按次价只配了上下文分档时图片请求也要能命中），最后看该行默认按次价。
func channelRowPerImagePrice(row *ChannelModelPricing, tier string) (float64, bool) {
	if row == nil {
		return 0, false
	}
	for _, interval := range row.Intervals {
		if strings.EqualFold(strings.TrimSpace(interval.TierLabel), tier) && validConfiguredPrice(interval.PerRequestPrice) {
			return *interval.PerRequestPrice, true
		}
	}
	for _, interval := range row.Intervals {
		if strings.TrimSpace(interval.TierLabel) != "" || !validConfiguredPrice(interval.PerRequestPrice) {
			continue
		}
		if interval.MinTokens < 1 && (interval.MaxTokens == nil || *interval.MaxTokens >= 1) {
			return *interval.PerRequestPrice, true
		}
	}
	if validConfiguredPrice(row.PerRequestPrice) {
		return *row.PerRequestPrice, true
	}
	return 0, false
}

func modelPriceImageBillingLayer(entry *ModelPriceEntry) imageBillingLayer {
	declaredBy := ImageBillingDeclaredByModelCatalog
	if entry.BillingMode != "" {
		declaredBy = ImageBillingDeclaredByModelOverride
	}
	layer := imageBillingLayer{declaredBy: declaredBy, source: PricingSourceModelPrice}
	switch imageBillingModeOfEntry(entry) {
	case BillingModeImage:
		layer.mode = BillingModeImage
		layer.perImage = func(tier string) (float64, bool) {
			return entryPerImageTierPrice(entry, tier)
		}
	case BillingModeToken:
		layer.mode = BillingModeToken
		if entryHasImageInputTokenPrice(entry) {
			input := entry.InputCostPerImageToken
			layer.imageInput = &input
		}
		if entryHasImageOutputTokenPrice(entry) {
			output := entry.OutputCostPerImageToken
			layer.imageOutput = &output
		}
	}
	return layer
}

// imageBillingModeOfEntry 返回条目声明的出图方式。JSON 目录与覆盖行构建的条目在构建时
// 已推导好；代码内直接构造的旧式条目（PricePresenceKnown=false、没标 mode）没有走
// 构建流程，按价格是否存在现推。
func imageBillingModeOfEntry(entry *ModelPriceEntry) BillingMode {
	if entry == nil {
		return ""
	}
	if entry.ImageBillingMode != "" || entry.PricePresenceKnown || entry.BillingMode != "" {
		return entry.ImageBillingMode
	}
	mode := strings.TrimSpace(entry.Mode)
	if mode != "" && !strings.EqualFold(mode, imageGenerationCatalogMode) {
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

// entryPerImageTierPrice 取模型价格的按张价：先看档位价，再按默认价推算（2K ×1.5、4K ×2）。
func entryPerImageTierPrice(entry *ModelPriceEntry, tier string) (float64, bool) {
	if entry == nil {
		return 0, false
	}
	for _, interval := range entry.Intervals {
		if strings.EqualFold(strings.TrimSpace(interval.TierLabel), tier) && validConfiguredPrice(interval.PerRequestPrice) {
			return *interval.PerRequestPrice, true
		}
	}
	if !isFiniteNonNegativePrice(entry.OutputCostPerImage) ||
		(entry.OutputCostPerImage == 0 && !entry.OutputCostPerImageExplicit) {
		return 0, false
	}
	switch tier {
	case ImageBillingSize2K:
		return entry.OutputCostPerImage * 1.5, true
	case ImageBillingSize4K:
		return entry.OutputCostPerImage * 2, true
	default:
		return entry.OutputCostPerImage, true
	}
}

func entryHasImageInputTokenPrice(entry *ModelPriceEntry) bool {
	return entry != nil && isFiniteNonNegativePrice(entry.InputCostPerImageToken) &&
		(entry.ImageInputPriceExplicit || (!entry.PricePresenceKnown && entry.InputCostPerImageToken > 0))
}
