//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type recoveryUsageLogRepoStub struct {
	// pages 按调用顺序返回，模拟游标翻页。
	pages    [][]UsageLog
	listCall int
	afterIDs []int64
	limits   []int

	listErr error

	marked   []int64
	markCost map[int64]SettlementCost
	markOK   map[int64]bool
	markErr  error
}

func (s *recoveryUsageLogRepoStub) ListPendingSettlement(_ context.Context, afterID int64, limit int) ([]UsageLog, error) {
	s.afterIDs = append(s.afterIDs, afterID)
	s.limits = append(s.limits, limit)
	if s.listErr != nil {
		return nil, s.listErr
	}
	idx := s.listCall
	s.listCall++
	if idx >= len(s.pages) {
		return nil, nil
	}
	return s.pages[idx], nil
}

func (s *recoveryUsageLogRepoStub) MarkSettlementRecovered(_ context.Context, id int64, cost SettlementCost) (bool, error) {
	s.marked = append(s.marked, id)
	if s.markCost == nil {
		s.markCost = make(map[int64]SettlementCost)
	}
	s.markCost[id] = cost
	if s.markErr != nil {
		return false, s.markErr
	}
	if ok, found := s.markOK[id]; found {
		return ok, nil
	}
	return true, nil
}

type recoveryAPIKeyRepoStub struct {
	keys  map[int64]*APIKey
	calls int
	err   error
}

type recoveryAggregationRefresherStub struct {
	ranges [][2]time.Time
	err    error
}

func (s *recoveryAggregationRefresherStub) TriggerRecomputeRange(start, end time.Time) error {
	s.ranges = append(s.ranges, [2]time.Time{start, end})
	return s.err
}

func (s *recoveryAPIKeyRepoStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.keys[id], nil
}

func newRecoveryService(t *testing.T, mode string, repo BillingRecoveryUsageLogRepository, apiKeyRepo BillingRecoveryAPIKeyRepository) *BillingRecoveryService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Pricing.RecoveryMode = mode
	cfg.Pricing.RecoveryBatchSize = 10
	return NewBillingRecoveryService(cfg, repo, apiKeyRepo, NewBillingService(cfg, nil), nil, nil)
}

func pendingTokenLog(id int64, model string) UsageLog {
	unpriced := string(BillingModeToken)
	return UsageLog{
		ID:             id,
		Model:          model,
		RequestedModel: model,
		InputTokens:    1000,
		OutputTokens:   500,
		RateMultiplier: 1,
		BillingMode:    &unpriced,
		BillingState:   BillingStatePricingUnavailable,
	}
}

func pendingImageLog(id int64, model, size string) UsageLog {
	log := pendingTokenLog(id, model)
	mode := string(BillingModeImage)
	log.BillingMode = &mode
	log.ImageCount = 1
	log.ImageSize = &size
	return log
}

func pendingVideoLog(id int64, model, resolution string) UsageLog {
	log := pendingTokenLog(id, model)
	mode := string(BillingModeVideo)
	duration := 1
	log.BillingMode = &mode
	log.VideoCount = 1
	log.VideoResolution = &resolution
	log.VideoDurationSeconds = &duration
	return log
}

func recoveryChannelService(groupID int64, model string, pricing *ChannelModelPricing) *ChannelService {
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformOpenAI
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformOpenAI,
		model:    strings.ToLower(model),
	}] = pricing
	cache.loadedAt = time.Now()
	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	return channelService
}

func recoveryFloatPtr(value float64) *float64 {
	return &value
}

// 一个在静态兜底价目里存在的模型，与一个怎么查都查不到的模型。
const (
	recoveryPricedModel   = "claude-sonnet-4"
	recoveryUnpricedModel = "totally-unknown-model-v99"
)

func TestBillingRecoveryRunOnce_OffModeDoesNothing(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{pendingTokenLog(1, recoveryPricedModel)}}}
	svc := newRecoveryService(t, config.PricingGuardModeOff, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, config.PricingGuardModeOff, report.Mode)
	require.Zero(t, report.Scanned)
	require.Empty(t, repo.afterIDs, "off 档连查都不该查")
}

func TestBillingRecoveryRunOnce_ShadowRecomputesButNeverWrites(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{pendingTokenLog(1, recoveryPricedModel)}}}
	svc := newRecoveryService(t, config.PricingGuardModeShadow, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Scanned)
	require.Equal(t, 1, report.Recovered, "shadow 汇报的是「会补多少笔」")
	require.Greater(t, report.RecoveredCost, 0.0, "金额要真的算出来，否则 shadow 报的数没有参考价值")
	require.Empty(t, repo.marked, "shadow 一行都不能写")
}

func TestBillingRecoveryRunOnce_EnforceWritesRecomputedCost(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{pendingTokenLog(7, recoveryPricedModel)}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.Equal(t, []int64{7}, repo.marked)

	got := repo.markCost[7]
	require.Greater(t, got.TotalCost, 0.0)
	require.Equal(t, string(BillingModeToken), got.BillingMode)
	// 分项之和必须等于总额，否则写回后 usage_logs 自己对不上账。
	sum := got.InputCost + got.ImageInputCost + got.OutputCost +
		got.CacheCreationCost + got.CacheReadCost + got.ImageOutputCost
	require.InDelta(t, got.TotalCost, sum, 1e-12)
	require.InDelta(t, got.TotalCost, report.RecoveredCost, 1e-12, "倍率为 1 时理论应收等于标准总额")
}

func TestBillingRecoveryRunOnce_ZeroMultiplierRemainsFree(t *testing.T) {
	log := pendingTokenLog(8, recoveryPricedModel)
	log.RateMultiplier = 0
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.Greater(t, repo.markCost[8].TotalCost, 0.0, "仍应回填标准成本用于成本核算")
	require.Zero(t, report.RecoveredCost, "显式 0 倍率必须继续表示免费，不能在恢复时改成 1")
}

func TestBillingRecoveryRunOnce_RefreshesAffectedAggregateRange(t *testing.T) {
	first := pendingTokenLog(21, recoveryPricedModel)
	first.CreatedAt = time.Date(2025, 4, 2, 3, 4, 5, 0, time.UTC)
	second := pendingTokenLog(22, recoveryPricedModel)
	second.CreatedAt = first.CreatedAt.Add(2 * time.Hour)
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{second, first}}}
	aggregation := &recoveryAggregationRefresherStub{}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.SetAggregationRefresher(aggregation)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, report.Recovered)
	require.Len(t, aggregation.ranges, 1)
	require.Equal(t, first.CreatedAt, aggregation.ranges[0][0])
	require.Equal(t, second.CreatedAt.Add(time.Nanosecond), aggregation.ranges[0][1])
}

func TestBillingRecoveryRunOnce_DoesNotRefreshAggregatesInShadow(t *testing.T) {
	log := pendingTokenLog(23, recoveryPricedModel)
	log.CreatedAt = time.Now().UTC()
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	aggregation := &recoveryAggregationRefresherStub{}
	svc := newRecoveryService(t, config.PricingGuardModeShadow, repo, nil)
	svc.SetAggregationRefresher(aggregation)

	_, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Empty(t, aggregation.ranges)
}

func TestBillingRecoveryRunOnce_StillUnpricedStaysPending(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{pendingTokenLog(3, recoveryUnpricedModel)}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked, "还没配价的行必须留在 billing_state=1，继续出现在欠账看板上")
}

func TestBillingRecoveryRunOnce_TokenPriceDoesNotRecoverImageOrVideoUsage(t *testing.T) {
	logs := []UsageLog{
		pendingImageLog(31, recoveryPricedModel, ImageBillingSize4K),
		pendingVideoLog(32, recoveryPricedModel, VideoBillingResolution720P),
	}
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{logs}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked, "token 价不能替代图片/视频实际档位价格")
}

func TestBillingRecoveryRunOnce_GroupMediaPricingUsesActualTier(t *testing.T) {
	groupID := int64(901)
	apiKeyID := int64(902)
	price1K := 0.11
	price4K := 0.44
	price480P := 0.08
	price720P := 0.14
	keyRepo := &recoveryAPIKeyRepoStub{keys: map[int64]*APIKey{
		apiKeyID: {
			ID:      apiKeyID,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				ImagePrice1K:   &price1K,
				ImagePrice4K:   &price4K,
				VideoPrice480P: &price480P,
				VideoPrice720P: &price720P,
			},
		},
	}}
	image := pendingImageLog(33, recoveryUnpricedModel, ImageBillingSize4K)
	image.APIKeyID, image.GroupID = apiKeyID, &groupID
	video := pendingVideoLog(34, recoveryUnpricedModel, VideoBillingResolution720P)
	video.APIKeyID, video.GroupID = apiKeyID, &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image, video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, report.Recovered)
	require.InDelta(t, price4K, repo.markCost[33].TotalCost, 1e-12)
	require.InDelta(t, price720P, repo.markCost[34].TotalCost, 1e-12)
}

func TestBillingRecoveryRunOnce_GroupMediaOtherTierDoesNotRecover(t *testing.T) {
	groupID := int64(903)
	apiKeyID := int64(904)
	price1K := 0.11
	price480P := 0.08
	keyRepo := &recoveryAPIKeyRepoStub{keys: map[int64]*APIKey{
		apiKeyID: {
			ID:      apiKeyID,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				ImagePrice1K:   &price1K,
				VideoPrice480P: &price480P,
			},
		},
	}}
	image := pendingImageLog(35, recoveryUnpricedModel, ImageBillingSize4K)
	image.APIKeyID, image.GroupID = apiKeyID, &groupID
	video := pendingVideoLog(36, recoveryUnpricedModel, VideoBillingResolution1080P)
	video.APIKeyID, video.GroupID = apiKeyID, &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image, video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked)
}

func TestBillingRecoveryRunOnce_UnknownVideoResolutionDoesNotBorrowDefaults(t *testing.T) {
	groupID := int64(907)
	apiKeyID := int64(908)
	price480P := 0.08
	defaultChannelPrice := 0.12
	keyRepo := &recoveryAPIKeyRepoStub{keys: map[int64]*APIKey{
		apiKeyID: {
			ID:      apiKeyID,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				VideoPrice480P: &price480P,
			},
		},
	}}
	channelService := recoveryChannelService(groupID, recoveryUnpricedModel, &ChannelModelPricing{
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: &defaultChannelPrice,
		Intervals: []PricingInterval{{
			TierLabel:       VideoBillingResolution480P,
			PerRequestPrice: &price480P,
		}},
	})
	video := pendingVideoLog(42, recoveryUnpricedModel, "4k")
	video.APIKeyID, video.GroupID = apiKeyID, &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked,
		"explicit unknown resolution must not borrow group 480p or channel default pricing")
}

func TestBillingRecoveryRunOnce_ChannelMediaPricingUsesActualTier(t *testing.T) {
	groupID := int64(905)
	model := recoveryUnpricedModel
	price4K := 0.51
	channelService := recoveryChannelService(groupID, model, &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals: []PricingInterval{
			{TierLabel: ImageBillingSize2K, PerRequestPrice: recoveryFloatPtr(0.21)},
			{TierLabel: ImageBillingSize4K, PerRequestPrice: &price4K},
		},
	})
	image := pendingImageLog(37, model, ImageBillingSize4K)
	image.GroupID = &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.InDelta(t, price4K, repo.markCost[37].TotalCost, 1e-12)
}

func TestBillingRecoveryRunOnce_ChannelMediaOtherTierDoesNotRecover(t *testing.T) {
	groupID := int64(906)
	model := recoveryUnpricedModel
	channelService := recoveryChannelService(groupID, model, &ChannelModelPricing{
		BillingMode: BillingModePerRequest,
		Intervals: []PricingInterval{
			{TierLabel: VideoBillingResolution480P, PerRequestPrice: recoveryFloatPtr(0.08)},
		},
	})
	video := pendingVideoLog(38, model, VideoBillingResolution1080P)
	video.GroupID = &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
}

func TestBillingRecoveryRunOnce_StrictCatalogVideoUsesActualResolution(t *testing.T) {
	video := pendingVideoLog(39, "grok-imagine-video-1.5", VideoBillingResolution1080P)
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.InDelta(t, defaultGrokImagineVideo15Price1080P, repo.markCost[39].TotalCost, 1e-12)
}

func TestBillingRecoveryRunOnce_UnknownCatalogVideoPrefixStaysPending(t *testing.T) {
	video := pendingVideoLog(40, "grok-imagine-video-future", VideoBillingResolution720P)
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{video}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
}

// 严格口径的核心断言：宽松查价链会把任意含 claude 的未知模型兜到 claude-sonnet-4 上
// （getFallbackPricing），补偿绝不能靠这个"补"出一个价来。
func TestBillingRecoveryRunOnce_RejectsInferredCrossModelPrice(t *testing.T) {
	inferred := "claude-model-that-does-not-exist-v99"
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, &recoveryUsageLogRepoStub{}, nil)

	_, looseErr := svc.billingService.GetModelPricing(inferred)
	require.NoError(t, looseErr, "前提：宽松口径确实能给这个模型推断出价格")

	require.False(t, svc.hasStrictTokenPricing(context.Background(), inferred, nil),
		"严格口径必须拒绝跨模型推断出来的价格")
}

func TestBillingRecoveryRecomputeTokenCostDoesNotReintroduceFamilyFallback(t *testing.T) {
	const inferred = "claude-opus-business"
	log := pendingTokenLog(41, inferred)
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, &recoveryUsageLogRepoStub{}, nil)

	_, looseErr := svc.billingService.GetModelPricing(inferred)
	require.NoError(t, looseErr, "前提：旧的宽松重算能够借到家族价格")

	cost, err := svc.recomputeCost(context.Background(), &log, inferred, nil)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, cost)
}

func TestRecoveryUsageTokens_PreservesImageInputDimension(t *testing.T) {
	log := &UsageLog{
		InputTokens:       371,
		ImageInputTokens:  352,
		OutputTokens:      439,
		ImageOutputTokens: 439,
	}

	tokens := recoveryUsageTokens(log)

	require.Equal(t, 371, tokens.InputTokens)
	require.Equal(t, 352, tokens.ImageInputTokens)
	require.Equal(t, 439, tokens.OutputTokens)
	require.Equal(t, 439, tokens.ImageOutputTokens)
}

func TestBillingRecoveryRunOnce_PrefersUpstreamModelCandidate(t *testing.T) {
	upstream := recoveryPricedModel
	log := pendingTokenLog(11, recoveryUnpricedModel)
	log.UpstreamModel = &upstream
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered, "上游模型是权威 SKU，排在候选首位")
	require.Equal(t, []int64{11}, repo.marked)
}

func TestBillingRecoveryRunOnce_UnknownUpstreamTokenDoesNotFallBackToGlobalAliasPrice(t *testing.T) {
	upstream := recoveryUnpricedModel
	log := pendingTokenLog(12, recoveryPricedModel)
	log.UpstreamModel = &upstream
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked,
		"已知请求别名的全局价格不能冒充未知实际上游 SKU 的价格")
}

func TestBillingRecoveryRunOnce_UnknownUpstreamTokenAllowsExplicitChannelAliasPrice(t *testing.T) {
	const alias = "explicitly-priced-request-alias"
	groupID := int64(907)
	upstream := recoveryUnpricedModel
	inputPrice := 0.001
	outputPrice := 0.002
	cacheWritePrice := 0.003
	cacheReadPrice := 0.0005
	channelService := recoveryChannelService(groupID, alias, &ChannelModelPricing{
		BillingMode:     BillingModeToken,
		InputPrice:      &inputPrice,
		OutputPrice:     &outputPrice,
		CacheWritePrice: &cacheWritePrice,
		CacheReadPrice:  &cacheReadPrice,
	})
	log := pendingTokenLog(13, recoveryPricedModel)
	log.RequestedModel = alias
	log.UpstreamModel = &upstream
	log.GroupID = &groupID
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.channelService = channelService
	svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.Zero(t, report.StillUnpriced)
	require.Equal(t, []int64{13}, repo.marked)
	require.Greater(t, repo.markCost[13].TotalCost, 0.0)
}

func TestBillingRecoveryRunOnce_MediaMayUsePersistedModelWhenTopLevelUpstreamIsUnpriced(t *testing.T) {
	upstreamTextModel := recoveryUnpricedModel
	image := pendingImageLog(14, "grok-imagine-image-quality", ImageBillingSize2K)
	image.UpstreamModel = &upstreamTextModel
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered,
		"Responses 顶层 upstream 是文本模型时，应允许使用已持久化的媒体计费模型")
	require.Zero(t, report.StillUnpriced)
	require.Equal(t, []int64{14}, repo.marked)
}

func TestBillingRecoveryRunOnce_Grok4KDoesNotBorrowHardcoded2KPrice(t *testing.T) {
	image := pendingImageLog(140, "grok-imagine-image-quality", ImageBillingSize4K)
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.StillUnpriced)
	require.Zero(t, report.Recovered)
	require.Empty(t, repo.marked)
}

func TestBillingRecoveryRunOnce_MediaPrefersPersistedBillingModelOverPricedTopLevelModel(t *testing.T) {
	const (
		mediaBillingModel = "responses-tool-image-model"
		topLevelTextModel = "responses-top-level-model-with-image-price"
	)
	image := pendingImageLog(15, mediaBillingModel, ImageBillingSize1K)
	image.UpstreamModel = optionalTrimmedStringPtr(topLevelTextModel)
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{image}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	pricingService := NewPricingService(svc.cfg, nil)
	pricingService.pricingData[mediaBillingModel] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.25,
	}
	pricingService.pricingData[topLevelTextModel] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.01,
	}
	svc.billingService.pricingService = pricingService

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.Zero(t, report.StillUnpriced)
	require.InDelta(t, 0.25, repo.markCost[15].TotalCost, 1e-12,
		"Responses 顶层文本模型即便也有媒体价，也不能覆盖已持久化的工具计费模型")
}

func TestBillingRecoveryRunOnce_ConcurrentWinnerIsNotDoubleCounted(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{
		pages:  [][]UsageLog{{pendingTokenLog(5, recoveryPricedModel)}},
		markOK: map[int64]bool{5: false}, // 另一个实例先补上了：影响 0 行
	}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Zero(t, report.Recovered)
	require.Zero(t, report.RecoveredCost, "没改到的行不能计入本轮补回金额")
	require.Zero(t, report.Failed, "被别人抢先不是错误")
}

func TestBillingRecoveryRunOnce_WriteErrorCountsAsFailed(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{
		pages:   [][]UsageLog{{pendingTokenLog(9, recoveryPricedModel)}},
		markErr: errors.New("db down"),
	}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err, "单行写失败不该中断整轮")
	require.Equal(t, 1, report.Failed)
	require.Zero(t, report.Recovered)
}

func TestBillingRecoveryRunOnce_ListErrorPropagates(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{listErr: errors.New("boom")}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)

	_, err := svc.RunOnce(context.Background())

	require.Error(t, err)
}

// 游标要跨轮次前进，否则一批永远补不上的行会占满每一轮的 batch，把后面能补的行饿死。
func TestBillingRecoveryRunOnce_CursorAdvancesPastStuckRows(t *testing.T) {
	stuck := []UsageLog{
		pendingTokenLog(1, recoveryUnpricedModel),
		pendingTokenLog(2, recoveryUnpricedModel),
	}
	next := []UsageLog{pendingTokenLog(3, recoveryPricedModel)}
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{stuck, next}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.cfg.Pricing.RecoveryBatchSize = 2 // 第一页刚好取满，说明后面还有

	first, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, first.StillUnpriced)

	second, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, second.Recovered)
	require.Equal(t, []int64{0, 2}, repo.afterIDs, "第二轮要从第一轮的末尾之后继续")
}

// 走到集合末尾后必须归零：否则游标永久停在表尾，之前跳过的旧行再也不会被复查，
// 而它们恰恰是最可能已经补上价的。
func TestBillingRecoveryRunOnce_CursorWrapsAtEndOfSet(t *testing.T) {
	short := []UsageLog{pendingTokenLog(4, recoveryUnpricedModel)}
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{short, short}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.cfg.Pricing.RecoveryBatchSize = 10 // 一页没取满 = 已到末尾

	_, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	_, err = svc.RunOnce(context.Background())
	require.NoError(t, err)

	require.Equal(t, []int64{0, 0}, repo.afterIDs)
}

func TestBillingRecoveryRunOnce_EmptyResultResetsCursor(t *testing.T) {
	full := []UsageLog{
		pendingTokenLog(1, recoveryUnpricedModel),
		pendingTokenLog(2, recoveryUnpricedModel),
	}
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{full, {}, full}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, nil)
	svc.cfg.Pricing.RecoveryBatchSize = 2

	for range 3 {
		_, err := svc.RunOnce(context.Background())
		require.NoError(t, err)
	}

	require.Equal(t, []int64{0, 2, 0}, repo.afterIDs, "扫空之后要回到表头")
}

// 同一批里同一个 Key 反复出现是常态（一个模型配错价会连累它的全部流量）。
func TestBillingRecoveryRunOnce_APIKeyLookupIsCachedPerRound(t *testing.T) {
	first := pendingTokenLog(1, recoveryPricedModel)
	first.APIKeyID = 42
	second := pendingTokenLog(2, recoveryPricedModel)
	second.APIKeyID = 42
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{first, second}}}
	keyRepo := &recoveryAPIKeyRepoStub{keys: map[int64]*APIKey{42: {ID: 42}}}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)

	_, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, keyRepo.calls)
}

// Key 已被删除不该让整行补偿失败：媒体价退回模型/系统默认价即可，token 行根本不看它。
func TestBillingRecoveryRunOnce_MissingAPIKeyDoesNotFailRow(t *testing.T) {
	log := pendingTokenLog(1, recoveryPricedModel)
	log.APIKeyID = 99
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{log}}}
	keyRepo := &recoveryAPIKeyRepoStub{err: ErrAPIKeyNotFound}
	svc := newRecoveryService(t, config.PricingGuardModeEnforce, repo, keyRepo)

	report, err := svc.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, report.Recovered)
	require.Zero(t, report.Failed)
}

func TestBillingRecoveryStart_OffModeStartsNoLoop(t *testing.T) {
	repo := &recoveryUsageLogRepoStub{pages: [][]UsageLog{{pendingTokenLog(1, recoveryPricedModel)}}}
	svc := newRecoveryService(t, config.PricingGuardModeOff, repo, nil)

	svc.Start()
	svc.Stop()

	require.Empty(t, repo.afterIDs)
}

func TestBillingRecoveryModeDefaultsToShadow(t *testing.T) {
	svc := newRecoveryService(t, "nonsense-mode", &recoveryUsageLogRepoStub{}, nil)
	require.Equal(t, config.PricingGuardModeShadow, svc.mode())

	var nilCfg *BillingRecoveryService
	require.Equal(t, config.PricingGuardModeShadow, nilCfg.mode())
}

func TestBillingRecoveryIntervalAndBatchDefaults(t *testing.T) {
	require.Equal(t, defaultBillingRecoveryInterval, billingRecoveryInterval(nil))
	require.Equal(t, defaultBillingRecoveryBatchSize, billingRecoveryBatchSize(nil))

	cfg := &config.Config{}
	cfg.Pricing.RecoveryIntervalMinutes = 0
	cfg.Pricing.RecoveryBatchSize = -1
	require.Equal(t, defaultBillingRecoveryInterval, billingRecoveryInterval(cfg))
	require.Equal(t, defaultBillingRecoveryBatchSize, billingRecoveryBatchSize(cfg))
}
