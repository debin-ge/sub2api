package service

// CatalogImagePriceWarnings 找出「渠道覆盖了文本 token 价、却没填图片输出价」的模型，
// 且目录里该模型带真实的图片 token 价。这类模型的图片输出会继续按目录图片价计费，
// 不会跟随渠道的文本折扣——运营者多半没意识到这一点，所以保存时提示一次。
// 只读、不阻断保存；pricingService 为 nil 时返回空。
func (s *ChannelService) CatalogImagePriceWarnings(pricing []ChannelModelPricing) []ModelPriceWarning {
	if s == nil || s.pricingService == nil {
		return nil
	}
	var warnings []ModelPriceWarning
	for i := range pricing {
		entry := &pricing[i]
		if entry.BillingMode != BillingModeToken && entry.BillingMode != "" {
			continue
		}
		if entry.ImageOutputPrice != nil {
			continue
		}
		if entry.InputPrice == nil && entry.OutputPrice == nil {
			continue
		}
		for _, model := range entry.Models {
			catalog := s.pricingService.GetModelPricingForPlatform(entry.Platform, model)
			if catalog == nil || !catalog.ImageOutputPriceExplicit || catalog.OutputCostPerImageToken <= 0 {
				continue
			}
			warnings = append(warnings, ModelPriceWarning{
				Code:  "IMAGE_PRICE_INHERITS_CATALOG",
				Field: "image_output_price",
				Model: model,
			})
		}
	}
	return warnings
}
