package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// imageBillingResolver 返回出图计费用的解析器；未注入时现建一个，与其他入口同一口径。
func (s *OpenAIGatewayService) imageBillingResolver() *ModelPricingResolver {
	if s == nil {
		return nil
	}
	if s.resolver != nil {
		return s.resolver
	}
	return NewModelPricingResolver(s.channelService, s.billingService)
}

func (s *OpenAIGatewayService) resolveOpenAIImageBillingPlan(
	ctx context.Context,
	apiKey *APIKey,
	groupID *int64,
	model string,
	imageSizeTier string,
) (*OpenAIImageBillingPlan, error) {
	return s.resolveOpenAIImageBillingPlanForPlatforms(
		ctx,
		apiKey,
		groupID,
		[]string{PlatformFromAPIKey(apiKey)},
		model,
		imageSizeTier,
	)
}

// resolveOpenAIImageBillingPlanForPlatforms 在 /v1/images（含经 Responses 实现的
// Image API）转发前锁定计费方式与价格快照。它是专用图片端点，分组「按实际出图张数
// 计费」开关在这里生效；分组模型定价卡同样生效。
func (s *OpenAIGatewayService) resolveOpenAIImageBillingPlanForPlatforms(
	ctx context.Context,
	apiKey *APIKey,
	groupID *int64,
	platforms []string,
	model string,
	imageSizeTier string,
) (*OpenAIImageBillingPlan, error) {
	return s.resolveOpenAIImagePlan(ctx, apiKey, groupID, platforms, model, imageSizeTier, true)
}

// resolveOpenAIImagePlan 补全分组媒体价字段后调用 ResolveImageBilling。imageEndpoint
// 为 false 时（Responses 生图工具）分组按张开关不生效。
func (s *OpenAIGatewayService) resolveOpenAIImagePlan(
	ctx context.Context,
	apiKey *APIKey,
	groupID *int64,
	platforms []string,
	model string,
	imageSizeTier string,
	imageEndpoint bool,
) (*ImageBillingPlan, error) {
	if s == nil || s.billingService == nil {
		return nil, fmt.Errorf("%w: OpenAI image billing service unavailable", ErrModelPricingUnavailable)
	}
	if apiKey == nil {
		apiKey = openAIPricingGuardAPIKey(ctx, groupID)
	}
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != nil {
		apiKey = refreshed
	}
	if groupID == nil && apiKey != nil {
		groupID = apiKey.GroupID
	}
	var group *Group
	if apiKey != nil {
		group = apiKey.Group
	}
	return s.imageBillingResolver().ResolveImageBilling(ctx, ImageBillingInput{
		Model:         model,
		Platforms:     platforms,
		GroupID:       groupID,
		Group:         group,
		SizeTier:      imageSizeTier,
		ImageEndpoint: imageEndpoint,
	})
}

func wrapOpenAIImageBillingPlanError(
	err error,
	account *Account,
	upstreamModel string,
	imageSizeTier string,
) error {
	if err == nil || !errors.Is(err, ErrModelPricingUnavailable) {
		return err
	}
	platform := PlatformOpenAI
	if account != nil && strings.TrimSpace(account.Platform) != "" {
		platform = account.Platform
	}
	return fmt.Errorf(
		"%w for billing_kind=%s platform=%s upstream_model=%q media_size_tier=%q: %v",
		ErrModelPricingUnavailable,
		BillingKindImage.String(),
		platform,
		strings.TrimSpace(upstreamModel),
		strings.TrimSpace(imageSizeTier),
		err,
	)
}
