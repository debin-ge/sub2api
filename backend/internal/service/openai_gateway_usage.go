package service

// 本文件由 openai_gateway_service.go 纯移动拆分而来：用量记录、计费成本计算与
// Codex 用量快照。仅做代码搬迁，无任何行为变更。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"go.uber.org/zap"
)

// OpenAIRecordUsageInput input for recording usage
type OpenAIRecordUsageInput struct {
	Result             *OpenAIForwardResult
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       *UserSubscription
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string // 请求的 User-Agent
	IPAddress          string // 请求的客户端 IP 地址
	SessionID          string // 客户端显式会话标识（session_id / X-Session-Id 等请求头），仅用于用量行会话关联
	RequestPayloadHash string
	APIKeyService      APIKeyQuotaUpdater
	QuotaPlatform      string // user×platform quota platform resolved by the handler before async billing.
	// BillingKind 是路由在转发前就已确定的结算口径（token/image/video/web_search）。
	// 由入口显式传下来，而不是在这里从 result 的字段反推：上游少回一个 image_count
	// 不该把按张计费的请求变成按 token 计费，进而在没有 token 价时变成免费。
	// 零值表示该调用方尚未迁移，此时沿用旧的反推逻辑，行为不变。
	BillingKind BillingKind
	// PricingAt 是请求级定价时刻（请求开始捕获，与利润门的 D 同源）：高峰因子
	// 按该时刻计算，保证同一请求从准入到扣费不中途变价。零值回退记录时刻
	//（既有行为），供未装配的路径（图片/异步/cyber 等）沿用。
	PricingAt time.Time
	// CyberBlocked 为 true 时把该用量行标记为 cyber（request_type=cyber），计费逻辑不变。
	CyberBlocked bool
	ChannelUsageFields
}

// CyberPolicyUsageInput 是 cyber 拒绝、未走正常 RecordUsage 的请求记录用量的入参。
// 用量按上游真实 token 计费，与 WS cyber 及正常请求口径一致（InputTokens/OutputTokens
// 取自上游 response.failed 报告的 usage，即 mark.UpstreamInTok/OutTok）。
type CyberPolicyUsageInput struct {
	APIKey       *APIKey
	Account      *Account
	Subscription *UserSubscription
	RequestID    string
	Model        string
	Stream       bool
	InputTokens  int
	OutputTokens int
	// 渠道归因与请求级 meta，使 cyber 计费行与正常 RecordUsage 行口径一致
	// （否则 cyber 行 channel_id 等为空，渠道维度统计会遗漏 cyber 命中）。
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	SessionID          string
	RequestPayloadHash string
	APIKeyService      APIKeyQuotaUpdater
	ChannelUsageFields
}

// RecordCyberPolicyUsageLog 为被上游 cyber_policy 拒绝、未走正常 RecordUsage 的请求
// （HTTP forward 返回错误路径）记录用量并按上游真实 token 计费，使其与 WS cyber 路径、
// 与正常请求的计费口径统一（不再是 tokens=0 免费行）。token 取自上游 response.failed
// 报告的 usage（非流式直接拒通常为 0，cost 随之为 0）。复用 RecordUsage 完成成本计算、
// 扣费与用量行写入（request_type=cyber 由 CyberBlocked 置位）。仅 forward 返回错误的
// 路径由 handler 调用，避免与成功路径的正常 RecordUsage 重复。
func (s *OpenAIGatewayService) RecordCyberPolicyUsageLog(ctx context.Context, in CyberPolicyUsageInput) error {
	if s == nil {
		return errors.New("openai gateway service is nil")
	}
	if in.APIKey == nil || in.APIKey.User == nil || in.Account == nil || strings.TrimSpace(in.Model) == "" {
		return errors.New("cyber usage input is incomplete")
	}
	result := &OpenAIForwardResult{
		RequestID: in.RequestID,
		Model:     in.Model,
		Stream:    in.Stream,
		Usage: OpenAIUsage{
			InputTokens:  in.InputTokens,
			OutputTokens: in.OutputTokens,
		},
	}
	return s.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             in.APIKey,
		User:               in.APIKey.User,
		Account:            in.Account,
		Subscription:       in.Subscription,
		InboundEndpoint:    in.InboundEndpoint,
		UpstreamEndpoint:   in.UpstreamEndpoint,
		UserAgent:          in.UserAgent,
		IPAddress:          in.IPAddress,
		SessionID:          in.SessionID,
		RequestPayloadHash: in.RequestPayloadHash,
		APIKeyService:      in.APIKeyService,
		BillingKind:        BillingKindToken,
		ChannelUsageFields: in.ChannelUsageFields,
		CyberBlocked:       true,
	})
}

// ResolveUserGroupRateMultiplier resolves the same cached multiplier used by OpenAI usage billing.
func (s *OpenAIGatewayService) ResolveUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if s == nil {
		return groupDefaultMultiplier
	}
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
	}
	return resolver.Resolve(ctx, userID, groupID, groupDefaultMultiplier)
}

// openAIUsagePricingAt 返回本次用量记录使用的定价时刻：优先请求级 PricingAt
// （与利润门 D 同源同刻），未装配时回退记录时刻（既有行为）。
func openAIUsagePricingAt(input *OpenAIRecordUsageInput) time.Time {
	if input != nil && !input.PricingAt.IsZero() {
		return input.PricingAt
	}
	return timezone.Now()
}

// RecordUsage records usage and deducts balance
func (s *OpenAIGatewayService) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway service is nil", ErrDurableUsageBillingRequired)
	}
	if input == nil {
		return fmt.Errorf("%w: OpenAI usage input is nil", ErrDurableUsageBillingRequired)
	}
	result := input.Result
	if result == nil {
		return fmt.Errorf("%w: OpenAI usage result is nil", ErrDurableUsageBillingRequired)
	}
	if input.APIKey == nil {
		return fmt.Errorf("%w: OpenAI usage API key is nil", ErrDurableUsageBillingRequired)
	}
	if input.User == nil {
		return fmt.Errorf("%w: OpenAI usage user is nil", ErrDurableUsageBillingRequired)
	}
	if input.Account == nil {
		return fmt.Errorf("%w: OpenAI usage account is nil", ErrDurableUsageBillingRequired)
	}
	if s.billingService == nil {
		return fmt.Errorf("%w: OpenAI billing service is nil", ErrDurableUsageBillingRequired)
	}
	if s.rateLimitService != nil && input.Account.Platform == PlatformOpenAI {
		s.rateLimitService.ResetOpenAI403Counter(ctx, input.Account.ID)
	}

	apiKey := input.APIKey
	user := input.User
	account := input.Account
	subscription := input.Subscription
	pricingPlatforms := pricingPlatformCandidates(apiKey, account)
	if !isGrokVideoUsageResult(result, nil) {
		ApplyOpenAIImageBillingResolution(result)
	}
	logServiceTierBillingDowngrade("service.openai_gateway", account, result.RequestID, ApplyOpenAIServiceTierBillingResolution(result))

	// OpenAI input_tokens 是总输入，包含缓存读取和缓存写入明细。
	// 将三类 token 拆成互斥桶，避免缓存写入同时按普通输入和 cache_write 重复计费。
	actualInputTokens := result.Usage.InputTokens - result.Usage.CacheReadInputTokens - result.Usage.CacheCreationInputTokens
	if actualInputTokens < 0 {
		actualInputTokens = 0
	}

	// Calculate cost
	tokens := UsageTokens{
		InputTokens:         actualInputTokens,
		ImageInputTokens:    result.Usage.ImageInputTokens,
		OutputTokens:        result.Usage.OutputTokens,
		CacheCreationTokens: result.Usage.CacheCreationInputTokens,
		CacheReadTokens:     result.Usage.CacheReadInputTokens,
		ImageOutputTokens:   result.Usage.ImageOutputTokens,
	}

	// Get rate multiplier
	multiplier := 1.0
	if s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if apiKey.GroupID != nil && apiKey.Group != nil {
		multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	// token 倍率叠加高峰因子（token 计费含图片 token，图片按次倍率不受影响）。
	// 高峰因子按请求级 PricingAt 现算（与利润门 D 同源同刻，跨峰谷请求不中途
	// 变价）；未装配 PricingAt 的路径回退记录时刻，保持既有行为。不并入上面的
	// Resolve，以免污染 user:group 倍率缓存。
	baseMultiplier := multiplier
	pricingAt := openAIUsagePricingAt(input)
	multiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, pricingAt)
	videoMultiplier := resolveVideoRateMultiplier(apiKey, baseMultiplier)

	var cost *CostBreakdown
	var err error
	billingModel := forwardResultBillingModel(result.Model, result.UpstreamModel)
	if result.BillingModel != "" {
		billingModel = strings.TrimSpace(result.BillingModel)
	}
	// 来源 → 用哪个模型名查价的映射表与准入守卫共用，见 billing_model_selection.go。
	// OpenAI 侧比 Anthropic 侧多两条约束，都保留原语义：
	//   - upstream 来源不在这里覆盖。转发阶段已把最终计费模型写进 result.BillingModel
	//     （图片、WS 轮次等都有各自口径），比事后按 UpstreamModel 重推更准。
	//   - channel_mapped 与请求模型同名说明渠道没真的改名，同样不该盖掉
	//     result.BillingModel。
	// Forward resolves response-driven media to the exact model that produced
	// the asset (notably /responses tools[].model). Once media was produced that
	// identity is authoritative; reapplying the top-level text channel source
	// here can replace it with an unrelated requested alias.
	mediaBillingModelLocked := strings.TrimSpace(result.BillingModel) != "" &&
		(result.ImageCount > 0 || result.VideoCount > 0)
	if !mediaBillingModelLocked {
		if selected, ok := selectBillingModelBySource(
			input.BillingModelSource,
			input.OriginalModel,
			input.ChannelMappedModel,
			result.UpstreamModel,
		); ok && selected != "" && input.BillingModelSource != BillingModelSourceUpstream {
			if input.BillingModelSource != BillingModelSourceChannelMapped || selected != strings.TrimSpace(input.OriginalModel) {
				billingModel = selected
			}
		}
	}
	billingModels := usageBillingModelCandidates(
		billingModel,
		result.BillingModel,
		input.ChannelMappedModel,
		input.OriginalModel,
		result.UpstreamModel,
		result.Model,
	)
	billingModels = s.filterCNProviderBillingModelCandidates(ctx, account, apiKey, billingModels)
	serviceTier := ""
	if result.ServiceTier != nil {
		serviceTier = strings.TrimSpace(*result.ServiceTier)
	}
	billingAccount := account
	if account.IsShadow() {
		billingAccount, err = resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return err
		}
	}
	longContextBillingGate := openAILongContextBillingGate(billingAccount)
	cost, err = s.calculateOpenAIRecordUsageCostForPlatforms(
		ctx,
		result,
		apiKey,
		pricingPlatforms,
		billingModels,
		input.BillingKind,
		multiplier,
		imageMultiplier,
		videoMultiplier,
		baseMultiplier,
		tokens,
		serviceTier,
		longContextBillingGate,
		pricingAt,
	)
	// 定价缺失的 fail-closed 属于准入层（转发前拒绝）；走到这里上游成本已真实
	// 发生，丢弃整条记录会连用量、配额计数和对账线索一起丢掉。改为 fail-loud：
	// 不扣费、标记为待结算、照常落库并告警。非定价错误仍然上抛。
	simpleMode := s.cfg != nil && s.cfg.RunMode == config.RunModeSimple
	billingState := BillingStateSettled
	if err != nil {
		unpriced, fatal := classifyRecordUsageCostError(err)
		if fatal != nil {
			return fatal
		}
		if unpriced {
			billingState = BillingStatePricingUnavailable
			cost = unpricedCostBreakdown(input.BillingKind, result.ImageCount, result.VideoCount)
			if result.ImageBillingPlan != nil && result.ImageBillingPlan.Mode != "" {
				// A token-priced Image API request with missing upstream usage
				// must remain identifiable as token billing in the recovery
				// queue; labeling it "image" could later settle it by count.
				cost.BillingMode = string(result.ImageBillingPlan.Mode)
			}
			logPricingUnavailableUsage("service.openai_gateway", simpleMode, err,
				zap.String("billing_kind", input.BillingKind.String()),
				zap.Strings("billing_models", billingModels),
				zap.String("requested_model", input.OriginalModel),
				zap.String("mapped_model", input.ChannelMappedModel),
				zap.String("upstream_model", result.UpstreamModel),
				zap.String("billing_model_source", input.BillingModelSource),
				zap.String("inbound_endpoint", input.InboundEndpoint),
				zap.Int64("api_key_id", apiKeyIDForLog(apiKey)),
				zap.Int64("account_id", accountIDForLog(account)),
			)
		}
	}
	// response_model：按上游成功响应自报的模型计费（渠道显式开启才生效）。
	// 采纳条件见 responseModelBillingDeclaration + hasIdentifiedOpenAIResponsePricing
	// + responseModelBillingAdoptable。任一条件不满足都静默回落基线，即开启本模式前的
	// 既有行为。响应模型与基线同名时直接跳过：重算必然同价，白跑一次定价解析。
	baselineBillingModel := firstUsageBillingModel(billingModels)
	if responseModel := responseModelBillingDeclaration(
		input.BillingModelSource,
		result.UpstreamResponseModel,
		result.UpstreamResponseModelConflict,
		result.ImageCount > 0 || result.VideoCount > 0 || result.WebSearchCalls > 0 ||
			result.AudioUsage != nil || result.SearchCount > 0,
	); responseModel != "" && !strings.EqualFold(responseModel, baselineBillingModel) {
		if identified, responseChannelPriced := s.hasIdentifiedOpenAIResponsePricingForPlatforms(
			ctx, responseModel, apiKey, pricingPlatforms,
		); identified {
			responseModels := s.filterCNProviderBillingModelCandidates(ctx, account, apiKey, usageBillingModelCandidates(responseModel))
			responseCost, responseErr := s.calculateOpenAIRecordUsageCostForPlatforms(
				ctx, result, apiKey, pricingPlatforms, responseModels, input.BillingKind, multiplier, imageMultiplier,
				videoMultiplier, baseMultiplier, tokens, serviceTier, longContextBillingGate, pricingAt,
			)
			// 基线定价源以 baselineBillingModel 为准：它正是 calculateOpenAIRecordUsageCost
			// 内部做渠道定价判断时使用的模型，且"首候选有渠道价"必然意味着首候选就是实际
			// 定价基准（有渠道价就一定能算出价，循环不会落到后续候选）。
			baselineChannelPriced := s.resolveOpenAIChannelPricingForPlatforms(ctx, baselineBillingModel, apiKey, pricingPlatforms) != nil
			if responseErr == nil && responseModelBillingAdoptable(cost, responseCost, baselineChannelPriced, responseChannelPriced) {
				logResponseModelBillingApplied("service.openai_gateway", account, result.RequestID,
					baselineBillingModel, responseModel, cost, responseCost)
				billingModels = responseModels
				cost = responseCost
			}
		}
	}

	// Determine billing type
	isSubscriptionBilling := subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
	billingType := BillingTypeBalance
	if isSubscriptionBilling {
		billingType = BillingTypeSubscription
	}

	// Create usage log
	durationMs := int(result.Duration.Milliseconds())
	accountRateMultiplier := account.BillingRateMultiplier()
	requestID := resolveUsageBillingRequestID(ctx, result.RequestID)
	if result.OpenAIWSMode {
		if upstreamRequestID := strings.TrimSpace(result.RequestID); upstreamRequestID != "" {
			// A WebSocket connection reuses the server-generated client
			// correlation ID across turns, so the upstream turn ID remains the
			// correct billing key. Keep it in the same explicit namespace as
			// non-WebSocket upstream IDs to prevent values such as "client:*"
			// from colliding with another identity source.
			requestID = "upstream:" + upstreamRequestID
		} else {
			// The client correlation ID is connection-scoped in WS mode. Using
			// it here would collapse every ID-less turn into one billing key,
			// charging at most the first turn. Each completed turn invocation
			// therefore gets its own server-generated identity when the
			// upstream did not provide a stable response ID.
			requestID = "generated:" + generateRequestID()
		}
	}
	// Async Grok video: always use the stable task id for dedup (status + content polls
	// share one bill). Context-local client/local IDs would otherwise create a new row
	// per poll if Redis claim is lost.
	if result.VideoCount > 0 {
		if stable := StableGrokVideoBillingRequestID(firstNonEmpty(
			strings.TrimPrefix(strings.TrimSpace(result.RequestID), "grok-video:"),
			strings.TrimSpace(result.ResponseID),
			strings.TrimPrefix(strings.TrimSpace(requestID), "grok-video:"),
		)); stable != "" {
			requestID = stable
		}
	}
	// WebSocket and async-video identities above intentionally replace the
	// common resolved ID. Mark after all overrides so internal relay rows remain
	// excluded from user-facing business statistics; marking is idempotent.
	requestID = markInternalRelayUsageRequestID(ctx, requestID)

	// 确定 RequestedModel（渠道映射前的原始模型）
	requestedModel := result.Model
	if input.OriginalModel != "" {
		requestedModel = input.OriginalModel
	}
	usageModel := result.Model
	if mediaBillingModelLocked && strings.TrimSpace(billingModel) != "" {
		// Responses media tools may use a model independent of the top-level text
		// model. Persist the settlement identity so pricing-unavailable rows can
		// be recovered after the corresponding media price is restored.
		usageModel = strings.TrimSpace(billingModel)
	}
	sentModel := upstreamSentModel(result.Model, result.UpstreamModel)
	if result.UpstreamResponseModelConflict {
		logger.L().Warn("upstream_response_model_conflict",
			zap.String("platform", account.Platform),
			zap.Int64("account_id", account.ID),
			zap.String("request_id", requestID),
			zap.String("sent_model", sentModel),
			zap.String("selected_response_model", strings.TrimSpace(result.UpstreamResponseModel)),
		)
	}

	usageLog := &UsageLog{
		UserID:                user.ID,
		APIKeyID:              apiKey.ID,
		AccountID:             account.ID,
		RequestID:             requestID,
		Model:                 usageModel,
		RequestedModel:        requestedModel,
		UpstreamModel:         optionalTrimmedStringPtr(result.UpstreamModel),
		UpstreamResponseModel: optionalTrimmedStringPtr(result.UpstreamResponseModel),
		UpstreamModelMismatch: upstreamModelMismatch(sentModel, result.UpstreamResponseModel),
		ServiceTier:           result.ServiceTier,
		ReasoningEffort:       result.ReasoningEffort,
		InboundEndpoint:       optionalTrimmedStringPtr(input.InboundEndpoint),
		UpstreamEndpoint:      optionalTrimmedStringPtr(input.UpstreamEndpoint),
		InputTokens:           actualInputTokens,
		OutputTokens:          result.Usage.OutputTokens,
		CacheCreationTokens:   result.Usage.CacheCreationInputTokens,
		CacheReadTokens:       result.Usage.CacheReadInputTokens,
		ImageInputTokens:      result.Usage.ImageInputTokens,
		ImageOutputTokens:     result.Usage.ImageOutputTokens,
		ImageCount:            result.ImageCount,
		ImageSize:             optionalTrimmedStringPtr(result.ImageSize),
		ImageInputSize:        optionalTrimmedStringPtr(result.ImageInputSize),
		ImageOutputSize:       optionalTrimmedStringPtr(result.ImageOutputSize),
		ImageSizeSource:       optionalTrimmedStringPtr(result.ImageSizeSource),
		ImageSizeBreakdown:    result.ImageSizeBreakdown,
	}
	// 视频用量的判定同样以入口口径优先：videos_* 路由发出去的请求就是视频用量，
	// 不取决于上游有没有回 video_count（回不回都已经产生了成本）。
	isVideoUsage := input.BillingKind == BillingKindVideo ||
		(input.BillingKind.ResponseDrivenMediaOverride() && isGrokVideoUsageResult(result, billingModels))
	if isVideoUsage {
		// 与 calculateOpenAIVideoCost 用同一个归一化：上游漏回 video_count 时按 1 计费，
		// 日志里也必须是 1，否则"扣了 1 个的钱、记了 0 个的量"无法对账。显式未知
		// 分辨率必须原样保留，使补偿结算继续 fail-closed，不能在落库时伪装成 480p。
		usageLog.VideoCount = normalizeVideoBillingCount(result)
		videoResolution := strings.TrimSpace(result.VideoResolution)
		if normalized, err := NormalizeVideoBillingResolutionStrictOrDefault(videoResolution); err == nil {
			videoResolution = normalized
		}
		usageLog.VideoResolution = optionalTrimmedStringPtr(videoResolution)
		videoDurationSeconds := NormalizeVideoBillingDurationSecondsOrDefault(result.VideoDurationSeconds)
		usageLog.VideoDurationSeconds = &videoDurationSeconds
	}
	if cost != nil {
		usageLog.InputCost = cost.InputCost
		usageLog.ImageInputCost = cost.ImageInputCost
		usageLog.OutputCost = cost.OutputCost
		usageLog.ImageOutputCost = cost.ImageOutputCost
		usageLog.CacheCreationCost = cost.CacheCreationCost
		usageLog.CacheReadCost = cost.CacheReadCost
		usageLog.TotalCost = cost.TotalCost
		usageLog.ActualCost = cost.ActualCost
		usageLog.LongContextBillingApplied = cost.LongContextBillingApplied
	}
	if isVideoUsage && (cost == nil || cost.BillingMode != string(BillingModeToken)) {
		usageLog.RateMultiplier = videoMultiplier
	} else if result.ImageCount > 0 && (cost == nil || cost.BillingMode != string(BillingModeToken)) {
		usageLog.RateMultiplier = imageMultiplier
	} else {
		usageLog.RateMultiplier = multiplier
	}
	usageLog.AccountRateMultiplier = &accountRateMultiplier
	usageLog.BillingType = billingType
	usageLog.Stream = result.Stream
	if input.CyberBlocked {
		usageLog.RequestType = RequestTypeCyberBlocked
	}
	usageLog.OpenAIWSMode = result.OpenAIWSMode
	usageLog.BillingState = billingState
	usageLog.DurationMs = &durationMs
	usageLog.FirstTokenMs = result.FirstTokenMs
	usageLog.CreatedAt = time.Now()
	// 设置渠道信息
	usageLog.ChannelID = optionalInt64Ptr(input.ChannelID)
	usageLog.ModelMappingChain = optionalTrimmedStringPtr(input.ModelMappingChain)
	// 设置计费模式
	if cost != nil && cost.BillingMode != "" {
		billingMode := cost.BillingMode
		usageLog.BillingMode = &billingMode
	} else if isVideoUsage {
		billingMode := string(BillingModeVideo)
		usageLog.BillingMode = &billingMode
	} else if result.ImageCount > 0 {
		billingMode := string(BillingModeImage)
		usageLog.BillingMode = &billingMode
	} else {
		billingMode := string(BillingModeToken)
		usageLog.BillingMode = &billingMode
	}
	// 添加 UserAgent
	if input.UserAgent != "" {
		usageLog.UserAgent = &input.UserAgent
	}

	// 添加 IPAddress
	if input.IPAddress != "" {
		usageLog.IPAddress = &input.IPAddress
	}

	// 添加 SessionID（客户端显式会话标识；缺失/无效时保持 nil）
	usageLog.SessionID = optionalTrimmedStringPtr(input.SessionID)

	if apiKey.GroupID != nil {
		usageLog.GroupID = apiKey.GroupID
	}
	if subscription != nil {
		usageLog.SubscriptionID = &subscription.ID
	}

	// 计算账号统计定价费用（使用最终上游模型匹配自定义规则）
	if apiKey.GroupID != nil {
		applyAccountStatsCost(ctx, usageLog, s.channelService, s.billingService,
			account.ID, *apiKey.GroupID, result.UpstreamModel, result.Model,
			tokens, cost.TotalCost,
			account.Platform,
		)
	}

	if simpleMode {
		if err := writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.openai_gateway"); err != nil {
			return err
		}
		logger.LegacyPrintf("service.openai_gateway", "[SIMPLE MODE] Usage recorded (not billed): user=%d, tokens=%d", usageLog.UserID, usageLog.TotalTokens())
		s.deferredService.ScheduleLastUsedUpdate(account.ID)
		return nil
	}

	// Async usage billing runs outside the original request context, so it
	// cannot recover ForcePlatform there. Fall back for internal/test callers.
	quotaPlatform := input.QuotaPlatform
	if quotaPlatform == "" {
		quotaPlatform = PlatformFromAPIKey(apiKey)
	}

	_, usageLogRecorded, billingErr := applyUsageBilling(ctx, requestID, usageLog, &postUsageBillingParams{
		Cost:                  cost,
		User:                  user,
		APIKey:                apiKey,
		Account:               account,
		Subscription:          subscription,
		RequestPayloadHash:    resolveUsageBillingPayloadFingerprint(ctx, input.RequestPayloadHash),
		IsSubscriptionBill:    isSubscriptionBilling,
		AccountRateMultiplier: accountRateMultiplier,
		APIKeyService:         input.APIKeyService,
		Platform:              quotaPlatform,
	}, s.billingDeps(), s.usageBillingRepo)

	if billingErr != nil {
		if shouldWriteUnsettledUsageLog(billingErr, usageLogRecorded) {
			usageLog.ActualCost = 0
			if usageLogErr := writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.openai_gateway"); usageLogErr != nil {
				return errors.Join(billingErr, usageLogErr)
			}
		}
		return billingErr
	}
	if !usageLogRecorded {
		if err := writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.openai_gateway"); err != nil {
			return err
		}
	}

	return nil
}

// hasIdentifiedOpenAIResponsePricingForPlatforms 判断上游自报的响应模型是否可以作为计费基准，
// 并回传它是否解析到了渠道级定价（供 responseModelBillingAdoptable 的跨定价源守卫使用，
// 避免为此再解析一次）。
// 只接受管理员为该模型显式配置的渠道定价，或价格表中能被确定性识别的条目；
// 刻意不接受按子串猜出来的系列兜底价，否则上游随便编一个含 "haiku" 的名字就能把
// 计费拉到最便宜的系列价上。详见 responseModelBillingDeclaration。
func (s *OpenAIGatewayService) hasIdentifiedOpenAIResponsePricingForPlatforms(
	ctx context.Context,
	model string,
	apiKey *APIKey,
	platforms []string,
) (identified bool, channelPriced bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return false, false
	}
	if s.resolveOpenAIChannelPricing(ctx, model, apiKey) != nil {
		return true, true
	}
	return s.billingService.HasIdentifiedTokenPricingForPlatforms(platforms, model), false
}

// openAILongContextBillingGate returns the per-account long-context opt-in.
// The flag is an OpenAI-only account setting, so other platforms (Grok) return
// nil — "no per-account gate" — and are governed by the group toggle alone.
// Returning a hardcoded false for them would veto the official model ladders
// (e.g. the Grok >=200k 2x card) that no account setting can ever re-enable.
func openAILongContextBillingGate(account *Account) *bool {
	if account == nil || !account.IsOpenAI() {
		return nil
	}
	enabled := account.IsOpenAILongContextBillingEnabled()
	return &enabled
}

func (s *OpenAIGatewayService) calculateOpenAIRecordUsageCost(
	ctx context.Context,
	result *OpenAIForwardResult,
	apiKey *APIKey,
	billingModels []string,
	kind BillingKind,
	multiplier float64,
	imageMultiplier float64,
	videoMultiplier float64,
	webSearchMultiplier float64,
	tokens UsageTokens,
	serviceTier string,
	longContextBillingGate *bool,
	pricingAt time.Time,
) (*CostBreakdown, error) {
	return s.calculateOpenAIRecordUsageCostForPlatforms(
		ctx,
		result,
		apiKey,
		[]string{PlatformFromAPIKey(apiKey)},
		billingModels,
		kind,
		multiplier,
		imageMultiplier,
		videoMultiplier,
		webSearchMultiplier,
		tokens,
		serviceTier,
		longContextBillingGate,
		pricingAt,
	)
}

func (s *OpenAIGatewayService) calculateOpenAIRecordUsageCostForPlatforms(
	ctx context.Context,
	result *OpenAIForwardResult,
	apiKey *APIKey,
	platforms []string,
	billingModels []string,
	kind BillingKind,
	multiplier float64,
	imageMultiplier float64,
	videoMultiplier float64,
	webSearchMultiplier float64,
	tokens UsageTokens,
	serviceTier string,
	longContextBillingGate *bool,
	pricingAt time.Time,
) (*CostBreakdown, error) {
	billingModel := firstUsageBillingModel(billingModels)

	// 媒体口径优先取入口显式传下来的 kind。旧逻辑完全从上游返回值反推口径，
	// 这让"上游少回一个字段"等价于"换一张价格表"——videos_* 路由一旦没拿到
	// video_count 就会掉进 token 分支，而 grok-imagine-video 没有 token 价，
	// 于是一条已经生成并交付的视频被记成 $0。显式 kind 下不再看这些字段是否出现，
	// 只保留"渠道显式配置了 token 价"这个管理员覆盖（那是真实意图，不是上游噪声）。
	//
	// 反过来 token 只是基线口径，仍然接受上游产出的媒体升级结算方式，
	// 见 BillingKind.ResponseDrivenMediaOverride。
	switch kind {
	case BillingKindNone:
		// 端点级非计费白名单；它不能被用作未知模型的兜底。
		return &CostBreakdown{BillingMode: string(BillingModeToken)}, nil
	case BillingKindWebSearch:
		return s.billingService.CalculateWebSearchCost(webSearchCallsFromResult(result), webSearchPricePerCallFromAPIKey(apiKey), webSearchMultiplier), nil
	case BillingKindAudio:
		if result != nil && result.AudioUsage != nil {
			cfg := groupAudioPriceConfigFromAPIKey(apiKey)
			return s.billingService.CalculateAudioCost(result.AudioUsage.Mode, result.AudioUsage.DurationOrUnits, cfg, webSearchMultiplier), nil
		}
		return &CostBreakdown{}, nil
	case BillingKindVideo:
		if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved == nil || resolved.Mode != BillingModeToken {
			return s.calculateOpenAIVideoCost(ctx, billingModel, apiKey, result, videoMultiplier)
		}
	case BillingKindImage:
		if result != nil && result.ImageBillingPlan != nil {
			return s.calculateOpenAIImageCostFromPlan(
				ctx,
				result,
				result.ImageBillingPlan,
				apiKey,
				tokens,
				multiplier,
				imageMultiplier,
				serviceTier,
				longContextBillingGate == nil || *longContextBillingGate,
			)
		}
		if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved == nil || resolved.Mode != BillingModeToken {
			return s.calculateOpenAIImageCostForPlatforms(ctx, billingModel, apiKey, platforms, result, imageMultiplier)
		}
	case BillingKindToken, BillingKindUnspecified:
		// Continue below. Media upgrades observed on legacy callers are handled
		// by the response-driven checks before token settlement.
	}

	if result != nil && result.AudioUsage != nil {
		if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved != nil &&
			(resolved.Mode == BillingModePerRequest) {
			gid := apiKey.Group.ID
			return s.billingService.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
				Platforms:  platforms,
				UsageUnits: result.AudioUsage.DurationOrUnits, SizeTier: result.AudioUsage.Mode,
				RateMultiplier: webSearchMultiplier, Resolver: s.resolver, Resolved: resolved,
			})
		}
		cfg := groupAudioPriceConfigFromAPIKey(apiKey)
		return s.billingService.CalculateAudioCost(result.AudioUsage.Mode, result.AudioUsage.DurationOrUnits, cfg, webSearchMultiplier), nil
	}

	if kind != BillingKindImage && result != nil && result.ImageCount > 0 {
		// 渠道定价为 token 计费时走 token 路径，否则走图片计费
		if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved == nil || resolved.Mode != BillingModeToken {
			return s.calculateOpenAIImageCostForPlatforms(ctx, billingModel, apiKey, platforms, result, imageMultiplier)
		}
	}
	if kind == BillingKindToken || kind == BillingKindUnspecified {
		// BillingKindToken（基线）与 BillingKindUnspecified（尚未迁移的调用方）：
		// 保留原有的反推顺序。
		if result != nil && result.WebSearchCalls > 0 {
			// Codex alpha/search 网页搜索按次计费：上游不返回 usage/token 字段，单价只取
			// 分组覆盖价（nil 时默认 0.01 = 官方 $10/1000 次），不参与渠道级模型定价。
			// 倍率与 image/video 按次口径一致：使用不含高峰因子的基础倍率
			//（用户专属 > 分组 rate_multiplier > 系统默认），与分组表单的价格预览承诺一致。
			return s.billingService.CalculateWebSearchCost(result.WebSearchCalls, webSearchPricePerCallFromAPIKey(apiKey), webSearchMultiplier), nil
		}
		if isGrokVideoUsageResult(result, billingModels) {
			if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved == nil || resolved.Mode != BillingModeToken {
				return s.calculateOpenAIVideoCost(ctx, billingModel, apiKey, result, videoMultiplier)
			}
		}
		if result != nil && result.ImageCount > 0 {
			// 渠道定价为 token 计费时走 token 路径，否则走图片计费
			if resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms); resolved == nil || resolved.Mode != BillingModeToken {
				return s.calculateOpenAIImageCostForPlatforms(ctx, billingModel, apiKey, platforms, result, imageMultiplier)
			}
		}
	}

	// Token path (optional search surcharge is additive — never replaces token cost).
	var tokenCost *CostBreakdown
	var lastErr error
	if len(billingModels) > 0 && billingModel != "" {
		for _, candidate := range billingModels {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cost, err := s.calculateOpenAIRecordUsageTokenCost(
				ctx,
				apiKey,
				platforms,
				candidate,
				multiplier,
				pricingAt,
				tokens,
				serviceTier,
				longContextBillingGate,
			)
			if err == nil {
				tokenCost = cost
				break
			}
			lastErr = err
		}
	}
	// Search surcharge is additive. Never let a zero/default search cost mask a
	// real token-pricing failure for requests that attempted token billing.
	searchCost := (*CostBreakdown)(nil)
	if result != nil && result.SearchCount > 0 {
		price := groupSearchPricePer1kFromAPIKey(apiKey)
		if price != nil && *price == 0 {
			logger.L().Info("openai_usage.search_price_per_1k_explicit_free",
				zap.Int("search_count", result.SearchCount),
				zap.String("model", billingModel),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
			)
		}
		searchCost = s.billingService.CalculateSearchCost(result.SearchCount, price, webSearchMultiplier)
	}

	tokenBillingAttempted := len(billingModels) > 0 && billingModel != ""
	if tokenCost == nil {
		if tokenBillingAttempted {
			if lastErr == nil {
				lastErr = fmt.Errorf("%w: no non-empty billing model candidates", ErrModelPricingUnavailable)
			}
			return nil, fmt.Errorf("calculate OpenAI usage cost failed for billing models %s: %w", strings.Join(billingModels, ","), lastErr)
		}
		// Search-only (no model / pure tool path): allow search billing alone.
		if searchCost != nil {
			return searchCost, nil
		}
		// 空候选按「无价可循」处理并携带 ErrModelPricingUnavailable：上层据此走
		// 零成本+告警落账，而不是丢弃整条 usage 记录。CN 账号的 claude-* 候选被
		// filterCNProviderBillingModelCandidates 全数过滤后即落到这里。
		if lastErr == nil {
			lastErr = fmt.Errorf("%w: openai usage billing model is empty", ErrModelPricingUnavailable)
		}
		return nil, fmt.Errorf("calculate OpenAI usage cost failed for billing models %s: %w", strings.Join(billingModels, ","), lastErr)
	}
	if searchCost == nil || (searchCost.TotalCost == 0 && searchCost.ActualCost == 0) {
		return tokenCost, nil
	}
	// Additive: tokens + search surcharge.
	tokenCost.TotalCost += searchCost.TotalCost
	tokenCost.ActualCost += searchCost.ActualCost
	return tokenCost, nil
}

func (s *OpenAIGatewayService) calculateOpenAIImageCostFromPlan(
	ctx context.Context,
	result *OpenAIForwardResult,
	plan *OpenAIImageBillingPlan,
	apiKey *APIKey,
	tokens UsageTokens,
	tokenMultiplier float64,
	imageMultiplier float64,
	serviceTier string,
	longContextBillingEnabled bool,
) (*CostBreakdown, error) {
	if plan == nil || plan.Resolved == nil || strings.TrimSpace(plan.Model) == "" {
		return nil, fmt.Errorf("%w: OpenAI image billing plan is incomplete", ErrModelPricingUnavailable)
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}

	switch plan.Mode {
	case BillingModeToken:
		if err := validateOpenAIImageTokenUsage(plan, result, tokens); err != nil {
			return nil, err
		}
		var groupID *int64
		if apiKey != nil {
			groupID = apiKey.GroupID
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:                       ctx,
			Model:                     plan.Model,
			GroupID:                   groupID,
			Tokens:                    tokens,
			RequestCount:              1,
			RateMultiplier:            tokenMultiplier,
			ServiceTier:               serviceTier,
			Resolver:                  resolver,
			Resolved:                  plan.Resolved,
			LongContextBillingEnabled: &longContextBillingEnabled,
		})
		if err != nil {
			return nil, err
		}
		cost.BillingMode = string(BillingModeToken)
		return cost, nil
	case BillingModeImage, BillingModePerRequest:
		if result == nil || result.ImageCount <= 0 {
			return nil, fmt.Errorf("%w: billable image count is missing", ErrModelPricingUnavailable)
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          plan.Model,
			RequestCount:   result.ImageCount,
			SizeTier:       plan.SizeTier,
			RateMultiplier: imageMultiplier,
			Resolver:       resolver,
			Resolved:       plan.Resolved,
		})
		if err != nil {
			return nil, err
		}
		cost.BillingMode = string(BillingModeImage)
		return cost, nil
	default:
		return nil, fmt.Errorf(
			"%w: unsupported OpenAI image billing mode %q",
			ErrModelPricingUnavailable,
			plan.Mode,
		)
	}
}

func validateOpenAIImageTokenUsage(
	plan *OpenAIImageBillingPlan,
	result *OpenAIForwardResult,
	tokens UsageTokens,
) error {
	if result == nil || result.ImageCount <= 0 {
		return fmt.Errorf("%w: image token usage has no billable image", ErrModelPricingUnavailable)
	}
	if tokens.ImageOutputTokens <= 0 || tokens.OutputTokens < tokens.ImageOutputTokens {
		return fmt.Errorf(
			"%w: image output token usage is missing or inconsistent",
			ErrModelPricingUnavailable,
		)
	}
	if tokens.ImageInputTokens < 0 || tokens.InputTokens < tokens.ImageInputTokens {
		return fmt.Errorf(
			"%w: image input token usage is inconsistent",
			ErrModelPricingUnavailable,
		)
	}
	if plan.RequireImageInput && tokens.ImageInputTokens <= 0 {
		return fmt.Errorf("%w: image edit input token usage is missing", ErrModelPricingUnavailable)
	}
	pricing := plan.Resolved.BasePricing
	if pricing == nil {
		return fmt.Errorf("%w: image token pricing snapshot is missing", ErrModelPricingUnavailable)
	}
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	outputPriceConfigured := pricing.OutputPriceExplicit || pricing.OutputPricePerToken > 0
	if textOutputTokens > 0 && !outputPriceConfigured {
		return fmt.Errorf("%w: text output usage has no configured price", ErrModelPricingUnavailable)
	}
	return nil
}

func webSearchCallsFromResult(result *OpenAIForwardResult) int {
	if result == nil {
		return 0
	}
	return result.WebSearchCalls
}

// normalizeVideoBillingCount 是视频计费与视频用量记录共用的归一化。
//
// 上游偶尔不回 video_count（尤其是 extensions 这类续写端点）。按 0 计费等于免费，
// 所以计费侧一直是「缺失就按 1 算」。此前 isVideoUsage 由 result.VideoCount > 0
// 反推，二者永远一致；改成由入口显式给出 BillingKindVideo 之后就会分叉 ——
// 扣了 1 个的钱、记了 0 个的量，账对不上。这里让两边走同一个函数。
func normalizeVideoBillingCount(result *OpenAIForwardResult) int {
	if result == nil || result.VideoCount <= 0 {
		return 1
	}
	return result.VideoCount
}

func isGrokVideoBillingModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "grok-imagine-video")
}

func isGrokVideoUsageResult(result *OpenAIForwardResult, billingModels []string) bool {
	if result == nil || result.VideoCount <= 0 {
		return false
	}
	// VideoCount alone is authoritative for async video completion billing.
	// Prefer model-family match when present; never drop video mode on rename/mapping.
	candidates := append([]string{}, billingModels...)
	candidates = append(candidates, result.BillingModel, result.Model, result.UpstreamModel)
	for _, candidate := range candidates {
		if isGrokVideoBillingModel(candidate) {
			return true
		}
	}
	return true
}

func (s *OpenAIGatewayService) calculateOpenAIRecordUsageTokenCost(
	ctx context.Context,
	apiKey *APIKey,
	platforms []string,
	billingModel string,
	multiplier float64,
	pricingAt time.Time,
	tokens UsageTokens,
	serviceTier string,
	longContextBillingGate *bool,
) (*CostBreakdown, error) {
	if s.resolver != nil && apiKey.Group != nil {
		gid := apiKey.Group.ID
		resolved, err := s.resolver.ResolveStrictToken(ctx, PricingInput{
			Model:     billingModel,
			Platforms: platforms,
			GroupID:   &gid,
			Group:     apiKey.Group,
		})
		if err != nil {
			return nil, err
		}
		return s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			Platforms: platforms,
			Tokens:    tokens, RequestCount: 1, RateMultiplier: multiplier, PricingAt: pricingAt,
			ServiceTier: serviceTier, Resolver: s.resolver, Resolved: resolved,
			LongContextBillingEnabled: longContextBillingGate,
		})
	}
	pricing, err := s.billingService.GetModelPricingStrictForPlatforms(platforms, billingModel)
	if err != nil {
		return nil, err
	}
	longContextBillingEnabled := apiKey == nil || apiKey.Group == nil || apiKey.Group.LongContextPricingEnabled
	if longContextBillingGate != nil && *longContextBillingGate {
		longContextBillingEnabled = true
	}
	cost, err := s.billingService.computeTokenBreakdownValidated(
		billingModel,
		pricing,
		tokens,
		multiplier,
		serviceTier,
		longContextBillingEnabled,
	)
	if err != nil {
		return nil, err
	}
	// 峰谷倍率只作用在官方报价上；基准价是空闲档还是高峰档由 pricing 自己带着。
	if officialTimePricingApplies(pricing) {
		applyCostBreakdownMultiplier(cost, deepSeekOfficialTimeMultiplier(
			basePricingPlatform(platforms), billingModel, pricingAt, pricing.OfficialTimeBaseIsOffPeak))
	}
	cost.BillingMode = string(BillingModeToken)
	return cost, nil
}

func (s *OpenAIGatewayService) calculateOpenAIImageCost(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	result *OpenAIForwardResult,
	multiplier float64,
) (*CostBreakdown, error) {
	return s.calculateOpenAIImageCostForPlatforms(
		ctx, billingModel, apiKey, []string{PlatformFromAPIKey(apiKey)}, result, multiplier,
	)
}

func (s *OpenAIGatewayService) calculateOpenAIImageCostForPlatforms(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	platforms []string,
	result *OpenAIForwardResult,
	multiplier float64,
) (*CostBreakdown, error) {
	sizeTier, err := NormalizeImageBillingTierStrictOrDefault(result.ImageSize)
	if err != nil {
		return nil, err
	}
	resolved := s.resolveOpenAIChannelPricingForPlatforms(ctx, billingModel, apiKey, platforms)
	if resolved != nil && resolved.Source == PricingSourceGroup &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			Platforms:    platforms,
			RequestCount: result.ImageCount, SizeTier: sizeTier,
			RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
		})
		if err == nil {
			return cost, nil
		}
	}
	groupConfig := imagePriceConfigFromAPIKey(apiKey)
	if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
		return s.billingService.CalculateImageCostStrictForPlatforms(
			platforms, billingModel, sizeTier, result.ImageCount, groupConfig, multiplier,
		)
	}
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != apiKey {
		apiKey = refreshed
		groupConfig = imagePriceConfigFromAPIKey(apiKey)
		if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
			return s.billingService.CalculateImageCostStrictForPlatforms(
				platforms, billingModel, sizeTier, result.ImageCount, groupConfig, multiplier,
			)
		}
	}
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			Group:          apiKey.Group,
			Platforms:      platforms,
			RequestCount:   result.ImageCount,
			SizeTier:       sizeTier,
			RateMultiplier: multiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
		if err == nil {
			return cost, nil
		}
		logger.LegacyPrintf("service.openai_gateway", "Calculate image channel cost failed, checking strict catalog price: %v", err)
	}

	return s.billingService.CalculateImageCostStrictForPlatforms(
		platforms, billingModel, sizeTier, result.ImageCount, groupConfig, multiplier,
	)
}

func (s *OpenAIGatewayService) calculateOpenAIVideoCost(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	result *OpenAIForwardResult,
	multiplier float64,
) (*CostBreakdown, error) {
	videoCount := normalizeVideoBillingCount(result)
	resolution, err := NormalizeVideoBillingResolutionStrictOrDefault(result.VideoResolution)
	if err != nil {
		return nil, err
	}
	durationSeconds := NormalizeVideoBillingDurationSecondsOrDefault(result.VideoDurationSeconds)
	resolved := s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)
	if resolved != nil && resolved.Source == PricingSourceGroup && resolved.Mode == BillingModeVideo {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			UsageUnits: float64(videoCount * durationSeconds), SizeTier: resolution,
			RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
		})
		if err == nil {
			return cost, nil
		}
	}
	groupConfig := videoPriceConfigFromAPIKey(apiKey)
	if apiKeyHasConfiguredVideoPrice(apiKey, billingModel, resolution) {
		return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
	}
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != apiKey {
		apiKey = refreshed
		groupConfig = videoPriceConfigFromAPIKey(apiKey)
		if apiKeyHasConfiguredVideoPrice(apiKey, billingModel, resolution) {
			return s.billingService.CalculateVideoCost(billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier), nil
		}
	}
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo) {
		// 渠道 per_request/image 定价保持"按请求次数"口径（价格由管理员按次配置），不乘视频时长。
		gid := apiKey.Group.ID
		units := float64(videoCount)
		if resolved.Mode == BillingModeVideo {
			units = float64(videoCount * durationSeconds)
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			Group:          apiKey.Group,
			RequestCount:   videoCount,
			UsageUnits:     units,
			SizeTier:       resolution,
			RateMultiplier: multiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
		if err == nil {
			cost.BillingMode = string(BillingModeVideo)
			return cost, nil
		}
		logger.LegacyPrintf("service.openai_gateway", "Calculate video channel cost failed, checking strict catalog price: %v", err)
	}

	return s.billingService.CalculateVideoCostStrict(
		billingModel, resolution, videoCount, durationSeconds, groupConfig, multiplier,
	)
}

func (s *OpenAIGatewayService) apiKeyWithFreshGroupMediaPricing(ctx context.Context, apiKey *APIKey) *APIKey {
	if apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
		return apiKey
	}
	if !groupMediaPricingLooksIncomplete(apiKey.Group) {
		return apiKey
	}
	if s == nil || s.channelService == nil || s.channelService.groupRepo == nil {
		return apiKey
	}
	group, err := s.channelService.groupRepo.GetByIDLite(ctx, *apiKey.GroupID)
	if err != nil || group == nil {
		return apiKey
	}
	clone := *apiKey
	clone.Group = group
	return &clone
}

// groupMediaPricingLooksIncomplete 判断分组对象是否可能缺失媒体/搜索/语音计费字段
// （例如由不含这些字段的旧快照或手工构造的上下文对象生成）。image/video 独立倍率在
// 数据库中的默认值均为 1.0；正常加载的分组不可能两个倍率同时为 0 且未开启独立倍率、
// 全部媒体/搜索/语音价为 nil——只有这种情况才回源查库，避免对未配置覆盖价的分组每条
// 用量都多打一次 DB 查询。
//
// 注意：apiKeyAuthSnapshotVersion 升级会强制刷新存量快照；本函数是热路径上的二次兜底，
// 不能仅凭 legacy video_price_* 判定完整而跳过 VideoModelPrices/search/audio 的回源。
func groupMediaPricingLooksIncomplete(group *Group) bool {
	if group == nil {
		return true
	}
	if group.ImageRateIndependent || group.VideoRateIndependent {
		return false
	}
	if group.ImageRateMultiplier != 0 || group.VideoRateMultiplier != 0 {
		return false
	}
	// Any first-class pricing field present means the projection is not a blank shell.
	if len(group.VideoModelPrices) > 0 {
		return false
	}
	if len(group.ModelPricing) > 0 || group.LongContextPricingEnabled {
		return false
	}
	if group.SearchPricePer1k != nil ||
		group.AudioRealtimePricePerMin != nil ||
		group.AudioTTSPricePerMillionChars != nil ||
		group.AudioSTTPricePerHour != nil ||
		group.WebSearchPricePerCall != nil {
		return false
	}
	return group.ImagePrice1K == nil && group.ImagePrice2K == nil && group.ImagePrice4K == nil &&
		group.VideoPrice480P == nil && group.VideoPrice720P == nil && group.VideoPrice1080P == nil
}

// filterCNProviderBillingModelCandidates 过滤国产供应商（kimi/zhipu/deepseek）
// 账号的计费候选模型名：claude-* 候选仅在运营者显式配置了分组/渠道定价时保留。
//
// 背景：候选链的兜底候选含客户端请求的原始模型名。CN 上游的 Anthropic 兼容端点
// 接受 claude-* 模型名但从不真正服务 Claude 模型；若放行，目录里的 Claude 价卡
// 与 getFallbackPricing 的 "claude"→Sonnet 统一兜底会把 CN 流量按 Claude 原价
// （数倍～数十倍）静默误计，且 usage 日志显示的正是 claude-* 名，无从察觉。
// 候选全部落空时走既有的零成本+告警路径（openai_usage.pricing_missing_record_
// zero_cost），与定价层「未知型号不回退以避免误计价」的既有设计意图一致；
// 运营者的修复手段是配置账号级 model_mapping（映射到已定价的 CN 模型）或
// 分组/渠道显式定价。
func (s *OpenAIGatewayService) filterCNProviderBillingModelCandidates(ctx context.Context, account *Account, apiKey *APIKey, candidates []string) []string {
	if account == nil || !account.IsCNProvider() {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "claude") &&
			s.resolveOpenAIChannelPricing(ctx, trimmed, apiKey) == nil {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (s *OpenAIGatewayService) resolveOpenAIChannelPricing(ctx context.Context, billingModel string, apiKey *APIKey) *ResolvedPricing {
	return s.resolveOpenAIChannelPricingForPlatforms(
		ctx, billingModel, apiKey, []string{PlatformFromAPIKey(apiKey)},
	)
}

func (s *OpenAIGatewayService) resolveOpenAIChannelPricingForPlatforms(
	ctx context.Context,
	billingModel string,
	apiKey *APIKey,
	platforms []string,
) *ResolvedPricing {
	if s.resolver == nil || apiKey == nil || apiKey.Group == nil {
		return nil
	}
	gid := apiKey.Group.ID
	resolved := s.resolver.Resolve(ctx, PricingInput{
		Model: billingModel, Platforms: platforms, GroupID: &gid, Group: apiKey.Group,
	})
	if resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel {
		return resolved
	}
	return nil
}

// ParseCodexRateLimitHeaders extracts Codex usage limits from response headers.
// Exported for use in ratelimit_service when handling OpenAI 429 responses.
func ParseCodexRateLimitHeaders(headers http.Header) *OpenAICodexUsageSnapshot {
	snapshot := &OpenAICodexUsageSnapshot{}
	hasData := false

	// Helper to parse float64 from header
	parseFloat := func(key string) *float64 {
		if v := headers.Get(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return &f
			}
		}
		return nil
	}

	// Helper to parse int from header
	parseInt := func(key string) *int {
		if v := headers.Get(key); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				return &i
			}
		}
		return nil
	}

	// Primary (weekly) limits
	if v := parseFloat("x-codex-primary-used-percent"); v != nil {
		snapshot.PrimaryUsedPercent = v
		hasData = true
	}
	if v := parseInt("x-codex-primary-reset-after-seconds"); v != nil {
		snapshot.PrimaryResetAfterSeconds = v
		hasData = true
	}
	if v := parseInt("x-codex-primary-window-minutes"); v != nil {
		snapshot.PrimaryWindowMinutes = v
		hasData = true
	}

	// Secondary (5h) limits
	if v := parseFloat("x-codex-secondary-used-percent"); v != nil {
		snapshot.SecondaryUsedPercent = v
		hasData = true
	}
	if v := parseInt("x-codex-secondary-reset-after-seconds"); v != nil {
		snapshot.SecondaryResetAfterSeconds = v
		hasData = true
	}
	if v := parseInt("x-codex-secondary-window-minutes"); v != nil {
		snapshot.SecondaryWindowMinutes = v
		hasData = true
	}

	// Overflow ratio
	if v := parseFloat("x-codex-primary-over-secondary-limit-percent"); v != nil {
		snapshot.PrimaryOverSecondaryPercent = v
		hasData = true
	}

	if !hasData {
		return nil
	}

	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return snapshot
}

func codexSnapshotBaseTime(snapshot *OpenAICodexUsageSnapshot, fallback time.Time) time.Time {
	if snapshot == nil {
		return fallback.UTC()
	}
	if snapshot.UpdatedAt == "" {
		return fallback.UTC()
	}
	base, err := time.Parse(time.RFC3339, snapshot.UpdatedAt)
	if err != nil {
		return fallback.UTC()
	}
	return base.UTC()
}

func codexResetAtRFC3339(base time.Time, resetAfterSeconds *int) *string {
	if resetAfterSeconds == nil {
		return nil
	}
	sec := *resetAfterSeconds
	if sec < 0 {
		sec = 0
	}
	resetAt := base.UTC().Add(time.Duration(sec) * time.Second).Format(time.RFC3339)
	return &resetAt
}

func buildCodexUsageExtraUpdates(snapshot *OpenAICodexUsageSnapshot, fallbackNow time.Time) map[string]any {
	if snapshot == nil {
		return nil
	}

	baseTime := codexSnapshotBaseTime(snapshot, fallbackNow)
	updates := make(map[string]any)

	// 保存原始 primary/secondary 字段，便于排查问题
	if snapshot.PrimaryUsedPercent != nil {
		updates["codex_primary_used_percent"] = *snapshot.PrimaryUsedPercent
	}
	if snapshot.PrimaryResetAfterSeconds != nil {
		updates["codex_primary_reset_after_seconds"] = *snapshot.PrimaryResetAfterSeconds
	}
	if snapshot.PrimaryWindowMinutes != nil {
		updates["codex_primary_window_minutes"] = *snapshot.PrimaryWindowMinutes
	}
	if snapshot.SecondaryUsedPercent != nil {
		updates["codex_secondary_used_percent"] = *snapshot.SecondaryUsedPercent
	}
	if snapshot.SecondaryResetAfterSeconds != nil {
		updates["codex_secondary_reset_after_seconds"] = *snapshot.SecondaryResetAfterSeconds
	}
	if snapshot.SecondaryWindowMinutes != nil {
		updates["codex_secondary_window_minutes"] = *snapshot.SecondaryWindowMinutes
	}
	if snapshot.PrimaryOverSecondaryPercent != nil {
		updates["codex_primary_over_secondary_percent"] = *snapshot.PrimaryOverSecondaryPercent
	}
	updates["codex_usage_updated_at"] = baseTime.Format(time.RFC3339)

	// 归一化到 5h/7d 规范字段
	if normalized := snapshot.Normalize(); normalized != nil {
		if normalized.Used5hPercent != nil || normalized.Used7dPercent != nil {
			updates[openAICodex5hAvailableExtraKey] = normalized.Used5hPercent != nil
			updates[openAICodex7dAvailableExtraKey] = normalized.Used7dPercent != nil
		}
		if normalized.Used5hPercent != nil {
			updates["codex_5h_used_percent"] = *normalized.Used5hPercent
		}
		if normalized.Reset5hSeconds != nil {
			updates["codex_5h_reset_after_seconds"] = *normalized.Reset5hSeconds
		}
		if normalized.Window5hMinutes != nil {
			updates["codex_5h_window_minutes"] = *normalized.Window5hMinutes
		}
		if normalized.Used7dPercent != nil {
			updates["codex_7d_used_percent"] = *normalized.Used7dPercent
		}
		if normalized.Reset7dSeconds != nil {
			updates["codex_7d_reset_after_seconds"] = *normalized.Reset7dSeconds
		}
		if normalized.Window7dMinutes != nil {
			updates["codex_7d_window_minutes"] = *normalized.Window7dMinutes
		}
		if reset5hAt := codexResetAtRFC3339(baseTime, normalized.Reset5hSeconds); reset5hAt != nil {
			updates["codex_5h_reset_at"] = *reset5hAt
		}
		if reset7dAt := codexResetAtRFC3339(baseTime, normalized.Reset7dSeconds); reset7dAt != nil {
			updates["codex_7d_reset_at"] = *reset7dAt
		}
	}

	return updates
}

// updateCodexUsageSnapshot saves the Codex usage snapshot to account's Extra field
// updateCodexUsageSnapshot 把 /responses 的 x-codex-* 全局头快照写入账号 codex_* Extra。
// ⚠️ 调用方必须排除 spark 影子账号(account.IsShadow()):影子的 codex_* 仅由 QueryUsage
// (/wham/usage bengalfox 道)更新,不能被全局头口径污染(外审第7轮 P1)。本函数仅持 accountID,
// 无法在此自检影子,故守卫前置到各调用点。
func (s *OpenAIGatewayService) updateCodexUsageSnapshot(ctx context.Context, accountID int64, snapshot *OpenAICodexUsageSnapshot) {
	if snapshot == nil {
		return
	}
	if s == nil || s.accountRepo == nil {
		return
	}

	now := time.Now()
	updates := buildCodexUsageExtraUpdates(snapshot, now)
	if len(updates) == 0 {
		return
	}
	if !s.getCodexSnapshotThrottle().Allow(accountID, now) {
		return
	}

	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.accountRepo.UpdateExtra(updateCtx, accountID, updates); err == nil {
			notifyOpenAIAutoReset(accountID)
		}
	}()
}

func (s *OpenAIGatewayService) UpdateCodexUsageSnapshotFromHeaders(ctx context.Context, accountID int64, headers http.Header) {
	if accountID <= 0 || headers == nil {
		return
	}
	if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
		s.updateCodexUsageSnapshot(ctx, accountID, snapshot)
	}
}
