package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// 图片 / 视频 / 网页搜索路由的定价准入守卫。
//
// token 路由的守卫（ValidateSelectedOpenAIModelPricing）此前是唯一一层准入检查，
// 而它挂在调度器的 useUpstreamTokenCost 分支上，于是 /v1/images/*、grok 的
// images_*、videos_*、alpha search、count_tokens 这几条路由整条跳过了守卫。
//
// 这些路由的漏洞形态和 token 路由不同，值得说清楚：
//
//	token 路由：查不到价 → 结算得到 0 → 真·免费。
//	媒体路由：查不到价 → getDefaultImagePrice 兜底到硬编码的 $0.134 → 不是免费，
//	          但那是个**占位数**，不是任何人配的价。一条上游成本 $2 的视频按
//	          $0.134/秒的图片占位价结算，账面上还"正常收费"了，比记成 0 更难发现。
//
// 所以这层守卫要求的是"存在一个真实来源的价格"：分组配置、渠道显式配置、模型价格
// 目录里的 output_cost_per_image、或代码里按具体 SKU 写死的 Grok Imagine 价格。
// 唯独 defaultImageGenerationPrice 这个通用占位值不算 —— 它一旦算数，守卫就永远
// 不会拒绝任何模型，等于没有。
//
// 在线准入始终 enforce；pricing.guard_mode 的 off/shadow 值会在配置校验时被拒绝，
// 直接构造 Config 也不能降级此守卫。

// validateSelectedPricingForBillingKind 按结算口径分派准入守卫。
//
// 这是 I1（转发前必须已确定恰好一种可结算价格来源）与 I3（准入与结算看同一个口径）
// 的交汇点：口径由入口给出，守卫据此决定该查哪张价格表，结算之后也用同一个口径。
func (s *OpenAIGatewayService) validateSelectedPricingForBillingKind(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	requireCompact bool,
	kind BillingKind,
) error {
	// An empty model is never an implicit pricing exemption. Only callers that
	// deliberately select BillingKindNone (for example count_tokens) may skip
	// the guard without a billing identity.
	if kind != BillingKindNone && strings.TrimSpace(requestedModel) == "" {
		return fmt.Errorf("%w: model is empty for billing_kind=%s", ErrModelPricingUnavailable, kind.String())
	}
	switch kind {
	case BillingKindToken, BillingKindUnspecified:
		return s.ValidateSelectedOpenAIModelPricing(ctx, groupID, account, requestedModel, requireCompact)
	case BillingKindImage, BillingKindVideo:
		return s.enforceSelectedOpenAIMediaPricing(ctx, groupID, account, requestedModel, kind)
	case BillingKindAudio:
		// Grok voice billing uses group audio prices or documented defaults;
		// it must not be forced through token-model pricing.
		if s.billingService == nil {
			return fmt.Errorf("%w: OpenAI billing service unavailable for audio", ErrModelPricingUnavailable)
		}
		return nil
	case BillingKindWebSearch, BillingKindNone:
		// web_search：单价要么是分组配置、要么是官方默认，不存在"未知"。
		// none：产品显式列入非计费白名单，不是未知模型的兜底。
		return nil
	default:
		return fmt.Errorf(
			"%w: unsupported billing_kind=%q",
			ErrModelPricingUnavailable,
			kind,
		)
	}
}

// ValidateSelectedOpenAIMediaPricing 校验非 token 口径路由的可结算价格。
//
// 它只在 image / video 口径上真正查价：
//   - web_search 的单价要么是分组配置，要么是官方公布的 $10/1000 次，永远解析得出，
//     没有"未知"这个状态可拒（分组显式配 0 是管理员在送，不是漏配）。
//   - none 是明确的端点级非计费白名单，不按模型查价。
func (s *OpenAIGatewayService) ValidateSelectedOpenAIMediaPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	kind BillingKind,
) error {
	if kind != BillingKindImage && kind != BillingKindVideo {
		return nil
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return fmt.Errorf("%w: model is empty", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	pricingRequestedModel := requestedModel
	mapping := ChannelMappingResult{MappedModel: requestedModel}
	if identity, ok := resolvedChannelPricingIdentityFromContext(ctx, requestedModel); ok {
		pricingRequestedModel = identity.requestedModel
		mapping = ChannelMappingResult{
			Mapped:             identity.mapped,
			MappedModel:        identity.channelMappedModel,
			BillingModelSource: identity.billingModelSource,
		}
	} else if s.channelService != nil && groupID != nil {
		mapping = s.channelService.ResolveChannelMapping(ctx, *groupID, requestedModel)
	}
	models := resolveOpenAIPricingGuardModels(account, pricingRequestedModel, mapping, "", false, false)

	platforms := pricingPlatformCandidates(openAIPricingGuardAPIKey(ctx, groupID), account)
	if s.hasResolvableOpenAIMediaPricingForPlatforms(ctx, groupID, platforms, models, kind) {
		return nil
	}
	return models.unavailableError(kind, account)
}

// enforceSelectedOpenAIMediaPricing 强制执行媒体守卫。
func (s *OpenAIGatewayService) enforceSelectedOpenAIMediaPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	kind BillingKind,
) error {
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	return s.ValidateSelectedOpenAIMediaPricing(ctx, groupID, account, requestedModel, kind)
}

// ValidateSelectedOpenAIResponsesImagePricing validates the image model declared
// by a /responses image_generation tool. The route itself is token-based, but
// once the tool is available the request can incur image cost under a model
// that is independent from the top-level text model.
func (s *OpenAIGatewayService) ValidateSelectedOpenAIResponsesImagePricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	body []byte,
) error {
	if err := ValidateUniqueOpenAIResponsesBillingFields(body); err != nil {
		return err
	}
	if !IsExplicitImageGenerationIntent(openAIResponsesEndpoint, requestedModel, body) {
		return nil
	}
	if s == nil || account == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}

	mapping := ChannelMappingResult{MappedModel: requestedModel}
	if s.channelService != nil && groupID != nil {
		mapping = s.channelService.ResolveChannelMapping(ctx, *groupID, requestedModel)
	}
	models := resolveOpenAIPricingGuardModels(account, requestedModel, mapping, "", false, false)
	imageCfg, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, models.Billing)
	if err != nil {
		return err
	}
	return s.enforceResolvedOpenAIMediaPricing(
		ctx,
		groupID,
		account,
		requestedModel,
		imageCfg.Model,
		imageCfg.SizeTier,
		BillingKindImage,
	)
}

// enforceResolvedOpenAIMediaPricing applies the media guard to the exact model
// and image tier that settlement will use. It deliberately does not resolve
// another channel mapping: image_generation.tools[].model is already a billing
// identity.
func (s *OpenAIGatewayService) enforceResolvedOpenAIMediaPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	billingModel string,
	imageSizeTier string,
	kind BillingKind,
) error {
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	billingModel = strings.TrimSpace(billingModel)
	models := openAIPricingGuardModels{
		Requested: strings.TrimSpace(requestedModel),
		Channel:   billingModel,
		Billing:   billingModel,
		Upstream:  billingModel,
		Primary:   billingModel,
	}
	if billingModel != "" {
		platforms := pricingPlatformCandidates(openAIPricingGuardAPIKey(ctx, groupID), account)
		var hasPricing bool
		if kind == BillingKindImage {
			hasPricing = s.hasResolvableOpenAIImageTierPricingForPlatforms(
				ctx,
				groupID,
				platforms,
				models,
				imageSizeTier,
			)
		} else {
			hasPricing = s.hasResolvableOpenAIMediaPricingForPlatforms(ctx, groupID, platforms, models, kind)
		}
		if hasPricing {
			return nil
		}
	}
	return models.unavailableError(kind, account)
}

func (s *OpenAIGatewayService) hasResolvableOpenAIMediaPricing(
	ctx context.Context,
	groupID *int64,
	models openAIPricingGuardModels,
	kind BillingKind,
) bool {
	return s.hasResolvableOpenAIMediaPricingForPlatforms(
		ctx, groupID, []string{PlatformFromAPIKey(openAIPricingGuardAPIKey(ctx, groupID))}, models, kind,
	)
}

func (s *OpenAIGatewayService) hasResolvableOpenAIMediaPricingForPlatforms(
	ctx context.Context,
	groupID *int64,
	platforms []string,
	models openAIPricingGuardModels,
	kind BillingKind,
) bool {
	// 顺序是按代价排的：先把内存里就能判定的来源问一遍，最后才允许回源查分组。
	// 准入在请求热路径上，而结算时的 apiKeyWithFreshGroupMediaPricing 会真的打一次
	// 库；把它放在最前面等于给每个图片请求都加一次同步查询。

	// 分组级媒体价（先只看请求上下文里已有的分组）。调度接口还没有尺寸参数，
	// 因此只有所有可能输出档位均有价格时才能在转发前证明“本次一定可结算”。
	guardKey := openAIPricingGuardAPIKey(ctx, groupID)
	if groupHasConfiguredMediaPrice(guardKey, kind) {
		return true
	}
	// Primary 是结算阶段真正优先使用的模型。不能因为其它候选模型有价就放行，
	// 因为媒体结算不会像 token 结算一样继续尝试后续候选。
	billingModel := strings.TrimSpace(models.Primary)
	if billingModel == "" {
		return false
	}
	// 渠道显式配置的按次/按张价格覆盖一切，管理员配了就是配了。渠道定价走内存缓存。
	if s.hasCompleteOpenAIChannelMediaPricing(ctx, groupID, billingModel, kind) {
		return true
	}
	// /v1/images/* may be billed from exact image token dimensions. This is
	// only a scheduler-level candidate check; the Image API forwarder resolves
	// and locks the exact plan after account mapping. Native Responses image
	// tools retain their existing exact per-image guard.
	if kind == BillingKindImage &&
		s.hasResolvableOpenAIImageTokenPricingForPlatforms(ctx, groupID, platforms, billingModel) {
		return true
	}
	// 模型价格目录 / 按 SKU 写死的默认价。
	if s.catalogHasMediaPricingForPlatforms(platforms, billingModel, kind) {
		return true
	}
	// 走到这里才回源：上下文里的分组可能是没带媒体价字段的精简快照，
	// 在真正要判"未定价"之前必须确认一次，否则会误拒配过价的分组。
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, guardKey); refreshed != guardKey {
		return groupHasConfiguredMediaPrice(refreshed, kind)
	}
	return false
}

// hasResolvableOpenAIImageTierPricingForPlatforms mirrors calculateOpenAIImageCost
// for the exact tier already resolved from the Responses request. Requiring every
// possible tier here would reject a request whose selected tier has a valid group
// or channel price even though post-settlement can charge it exactly.
func (s *OpenAIGatewayService) hasResolvableOpenAIImageTierPricingForPlatforms(
	ctx context.Context,
	groupID *int64,
	platforms []string,
	models openAIPricingGuardModels,
	imageSizeTier string,
) bool {
	tier, ok := ClassifyImageBillingTier(imageSizeTier)
	if !ok {
		return false
	}

	guardKey := openAIPricingGuardAPIKey(ctx, groupID)
	if groupHasConfiguredImageTierPrice(guardKey, tier) {
		return true
	}
	billingModel := strings.TrimSpace(models.Primary)
	if billingModel == "" {
		return false
	}
	if s.hasExactOpenAIChannelImagePricing(ctx, groupID, billingModel, tier) {
		return true
	}
	if s.catalogHasExactImagePricingForPlatforms(platforms, billingModel, tier) {
		return true
	}
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, guardKey); refreshed != guardKey {
		return groupHasConfiguredImageTierPrice(refreshed, tier)
	}
	return false
}

func groupHasConfiguredImageTierPrice(apiKey *APIKey, imageSizeTier string) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	tier, ok := ClassifyImageBillingTier(imageSizeTier)
	if !ok {
		return false
	}
	return validConfiguredPrice(apiKey.Group.GetImagePrice(tier))
}

func groupHasConfiguredMediaPrice(apiKey *APIKey, kind BillingKind) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	var prices []*float64
	if kind == BillingKindVideo {
		prices = []*float64{apiKey.Group.VideoPrice480P, apiKey.Group.VideoPrice720P, apiKey.Group.VideoPrice1080P}
	} else {
		prices = []*float64{apiKey.Group.ImagePrice1K, apiKey.Group.ImagePrice2K, apiKey.Group.ImagePrice4K}
	}
	for _, price := range prices {
		// 与 getImageUnitPrice / getVideoUnitPrice 用同一个有效性判定：NaN / 负数
		// 在结算侧会被跳过并回落到占位价，准入侧就不能把它当成"配过价了"。
		if price == nil || !isFiniteNonNegativePrice(*price) {
			return false
		}
	}
	return true
}

func (s *OpenAIGatewayService) hasExactOpenAIChannelImagePricing(
	ctx context.Context,
	groupID *int64,
	model string,
	imageSizeTier string,
) bool {
	model = strings.TrimSpace(model)
	tier, ok := ClassifyImageBillingTier(imageSizeTier)
	if !ok || model == "" || groupID == nil || s == nil || s.channelService == nil {
		return false
	}
	pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model)
	if pricing == nil ||
		(pricing.BillingMode != BillingModePerRequest && pricing.BillingMode != BillingModeImage) {
		return false
	}
	if validConfiguredPrice(pricing.PerRequestPrice) {
		return true
	}
	for _, interval := range pricing.Intervals {
		if strings.EqualFold(strings.TrimSpace(interval.TierLabel), tier) &&
			validConfiguredPrice(interval.PerRequestPrice) {
			return true
		}
	}
	return false
}

// hasCompleteOpenAIChannelMediaPricing checks the exact settlement model and
// requires either a valid default price or every possible media tier.
func (s *OpenAIGatewayService) hasCompleteOpenAIChannelMediaPricing(
	ctx context.Context,
	groupID *int64,
	model string,
	kind BillingKind,
) bool {
	model = strings.TrimSpace(model)
	if model == "" || groupID == nil || s == nil || s.channelService == nil {
		return false
	}
	pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model)
	if pricing == nil {
		return false
	}
	mode := pricing.BillingMode
	if mode != BillingModePerRequest && mode != BillingModeImage {
		// Media settlement only consumes channel per_request/image prices.
		// A complete token price cannot prove that an image/video request is
		// billable; settlement would ignore it and fall through to the strict
		// media catalog.
		return false
	}
	if validConfiguredPrice(pricing.PerRequestPrice) {
		return true
	}
	requiredTiers := []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K}
	if kind == BillingKindVideo {
		requiredTiers = []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
	}
	for _, required := range requiredTiers {
		found := false
		for _, interval := range pricing.Intervals {
			if strings.EqualFold(strings.TrimSpace(interval.TierLabel), required) &&
				validConfiguredPrice(interval.PerRequestPrice) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *OpenAIGatewayService) catalogHasExactImagePricingForPlatforms(platforms []string, model string, imageSizeTier string) bool {
	model = strings.TrimSpace(model)
	tier, ok := ClassifyImageBillingTier(imageSizeTier)
	if !ok || model == "" || s == nil || s.billingService == nil {
		return false
	}
	_, ok = s.billingService.strictImageUnitPriceForPlatforms(platforms, model, tier, nil)
	return ok
}

// catalogHasMediaPricingForPlatforms 判断价格目录里是否有该模型的真实媒体单价。
//
// 注意这里刻意**不**调用 getDefaultImagePrice：那个函数在查不到任何价格时会返回
// defaultImageGenerationPrice（$0.134）这个通用占位值，用它判断"有没有价"会恒真。
func (s *OpenAIGatewayService) catalogHasMediaPricingForPlatforms(platforms []string, model string, kind BillingKind) bool {
	model = strings.TrimSpace(model)
	if model == "" || s == nil || s.billingService == nil {
		return false
	}
	switch kind {
	case BillingKindVideo:
		for _, tier := range []string{
			VideoBillingResolution480P,
			VideoBillingResolution720P,
			VideoBillingResolution1080P,
		} {
			if _, ok := s.billingService.strictVideoUnitPrice(model, tier, nil); !ok {
				return false
			}
		}
		return true
	case BillingKindImage:
		// Scheduler selection does not carry the requested image tier. Treat this
		// as a model-level candidate check: every generation path performs a
		// second enforceResolvedOpenAIMediaPricing check with the exact tier
		// before the upstream call. Requiring all tiers here would make a model
		// with real 1K/2K prices unusable merely because its 4K tier is unpriced.
		for _, tier := range []string{
			ImageBillingSize1K,
			ImageBillingSize2K,
			ImageBillingSize4K,
		} {
			if _, ok := s.billingService.strictImageUnitPriceForPlatforms(platforms, model, tier, nil); ok {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// openAIPricingGuardAPIKey 拼一个只用于查价的最小 APIKey。
// 媒体价格挂在分组上，而准入发生在调度阶段，此时手里只有 groupID。
func openAIPricingGuardAPIKey(ctx context.Context, groupID *int64) *APIKey {
	if groupID == nil || *groupID <= 0 {
		return nil
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if !IsGroupContextValid(group) || group.ID != *groupID {
		// 只留分组身份，媒体价字段交给 apiKeyWithFreshGroupMediaPricing 去补。
		group = &Group{ID: *groupID}
	}
	return &APIKey{GroupID: groupID, Group: group}
}
