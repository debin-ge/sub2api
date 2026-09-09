package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestByteDancePlatformContracts(t *testing.T) {
	require.Equal(t, "bytedance", PlatformByteDance)
	require.Equal(t, "bytedance", VideoProviderByteDance)
	require.True(t, IsAllowedQuotaPlatform(PlatformByteDance))
	require.False(t, IsCNProvider(PlatformByteDance))
	require.True(t, isConcreteRequestPlatform(PlatformByteDance))
	require.Equal(t, PlatformByteDance, NormalizeGroupPlatform(PlatformByteDance))
	require.Contains(t, CatalogOverridePlatforms(), PlatformByteDance)
	require.Contains(t, DefaultModelCatalogIDs(PlatformByteDance), ByteDanceVideoModelSeedance10Pro)
	require.NotContains(t, AllowedSchedulingThresholdPlatforms, PlatformByteDance)
}

func TestDefaultByteDanceVideoCapabilities(t *testing.T) {
	caps := DefaultByteDanceVideoCapabilities()
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, caps.DefaultModel)
	require.True(t, caps.Supports(VideoCapabilityCreate))
	require.True(t, caps.Supports(VideoCapabilityInputReference))
	// Ark exposes asynchronous generation only; DELETE cancels a running task.
	require.True(t, caps.Supports(VideoCapabilityCancel))
	require.False(t, caps.Supports(VideoCapabilityEdits))
	require.False(t, caps.Supports(VideoCapabilityExtensions))
	require.False(t, caps.Supports(VideoCapabilityCharacters))
	require.False(t, caps.Supports(VideoCapabilityWebhook))
	// Ark derives the frame size from resolution plus ratio, so it declares no
	// "WxH" sizes at all.
	require.Empty(t, caps.DefaultSizes)
	require.Empty(t, caps.SupportedSizes)
	require.NoError(t, ValidateVideoCapabilityCatalog(DefaultVideoCapabilityCatalogDocument()))
}

func TestVideoTaskServiceSubmitsByteDanceWithIndependentPricingIdentity(t *testing.T) {
	provider := &videoProviderStub{name: VideoProviderByteDance, result: &ProviderVideoTask{
		ProviderTaskID: "video_bytedance_service", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	unit := VideoBillingUnitSecond
	price := 0.25
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformByteDance
	group.ModelPricing = []ChannelModelPricing{{
		Platform: PlatformByteDance, Models: []string{ByteDanceVideoModelSeedance10Pro}, BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{ID: 8, PerRequestPrice: &price, BillingUnit: &unit}},
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Platform = PlatformByteDance
	accounts.accounts[0].Credentials = map[string]any{
		"api_key": "ark-secret",
		"model_mapping": map[string]any{
			ByteDanceVideoModelSeedance10Pro: ByteDanceVideoModelSeedance10Pro,
		},
	}
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	request.Model = ByteDanceVideoModelSeedance10Pro
	request.Size = ""
	request.Seconds = 8
	// Ark takes the output resolution as a request parameter; it is never
	// encoded in the model identifier, so it can only arrive this way.
	request.ProviderOptions = map[string]any{"resolution": "720p"}

	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, VideoProviderByteDance, result.Task.Provider)
	require.Equal(t, PlatformByteDance, tasks.create.PriceSnapshot["pricing_platform"])
	require.Equal(t, VideoProviderByteDance, tasks.create.RequestAttributes["provider"])
	require.Equal(t, "720p", tasks.create.RequestAttributes["resolution"])
	require.Empty(t, tasks.create.RequestAttributes["size"])
	executionSpec, ok := tasks.create.RequestAttributes["execution_spec"].(ResolvedVideoExecutionSpec)
	require.True(t, ok)
	require.Empty(t, executionSpec.Size)
	require.Equal(t, "720p", executionSpec.Resolution)
	require.InDelta(t, 4.0, tasks.create.HoldAmount, 1e-9)
}

// A ByteDance submission that names no resolution must not invent one: the
// quote, hold and settlement all have to describe what was actually requested.
func TestVideoTaskServiceByteDanceOmitsResolutionWhenUnrequested(t *testing.T) {
	provider := &videoProviderStub{name: VideoProviderByteDance, result: &ProviderVideoTask{
		ProviderTaskID: "video_bytedance_default", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	unit := VideoBillingUnitSecond
	price := 0.25
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformByteDance
	group.ModelPricing = []ChannelModelPricing{{
		Platform: PlatformByteDance, Models: []string{ByteDanceVideoModelSeedance10Pro}, BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{ID: 8, PerRequestPrice: &price, BillingUnit: &unit}},
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Platform = PlatformByteDance
	accounts.accounts[0].Credentials = map[string]any{
		"api_key": "ark-secret",
		"model_mapping": map[string]any{
			ByteDanceVideoModelSeedance10Pro: ByteDanceVideoModelSeedance10Pro,
		},
	}
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	request.Model = ByteDanceVideoModelSeedance10Pro
	request.Size = ""
	request.Seconds = 8

	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Empty(t, tasks.create.RequestAttributes["resolution"])
	executionSpec, ok := tasks.create.RequestAttributes["execution_spec"].(ResolvedVideoExecutionSpec)
	require.True(t, ok)
	require.Empty(t, executionSpec.Resolution)
	require.Empty(t, executionSpec.Size)
}

// A ByteDance model with no configured price must not generate for free: the
// quote is resolved before the hold, and a missing rule has to fail the
// submission rather than fall through to a zero price.
func TestVideoTaskServiceByteDanceFailsClosedWithoutPricing(t *testing.T) {
	provider := &videoProviderStub{name: VideoProviderByteDance, result: &ProviderVideoTask{
		ProviderTaskID: "video_bytedance_unpriced", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformByteDance
	group.ModelPricing = nil
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Platform = PlatformByteDance
	accounts.accounts[0].Credentials = map[string]any{
		"api_key": "ark-secret",
		"model_mapping": map[string]any{
			ByteDanceVideoModelSeedance10Pro: ByteDanceVideoModelSeedance10Pro,
		},
	}
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	request.Model = ByteDanceVideoModelSeedance10Pro
	request.Size = ""
	request.Seconds = 8

	_, err := svc.Submit(context.Background(), request)

	require.Error(t, err)
	require.Zero(t, provider.createCalls)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, tasks.create.HoldAmount)
}

func TestVideoTaskServiceRejectsCrossProviderSourceBeforeAccountSelection(t *testing.T) {
	provider := &videoProviderStub{name: VideoProviderByteDance, result: &ProviderVideoTask{
		ProviderTaskID: "video_bytedance_edit", Status: VideoGenerationQueued,
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformByteDance
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	svc.pricing = NewVideoPricingResolver(nil, NewPricingService(&config.Config{}, nil))
	sourceID, providerID, accountID := NewVideoTaskID(), "video_openai_source", int64(11)
	tasks.sources[sourceID] = &VideoTask{
		ID: 9, PublicID: sourceID, UserID: 42, Provider: VideoProviderOpenAI,
		ProviderTaskID: &providerID, AccountID: &accountID,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		RequestedModel: OpenAIVideoModelSora2, PublicModel: OpenAIVideoModelSora2, UpstreamModel: OpenAIVideoModelSora2,
	}
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	request.Operation = VideoOperationEdit
	request.SourceVideoID = sourceID
	request.Model = ByteDanceVideoModelSeedance10Pro
	request.Size = ""
	request.IdempotencyKey = "cross-provider"

	_, err := svc.Submit(context.Background(), request)
	require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
	require.Zero(t, provider.createCalls)
	require.Empty(t, tasks.create.PublicID)
}

// Ark offers generation only, so a completed ByteDance task can never be the
// source of a derived edit or extension. The refusal has to happen while the
// execution specification is resolved, before any hold is placed.
func TestVideoTaskServiceRejectsDerivedRequestFromByteDanceSource(t *testing.T) {
	provider := &videoProviderStub{name: VideoProviderByteDance, result: &ProviderVideoTask{
		ProviderTaskID: "video_bytedance_edit", Status: VideoGenerationQueued,
	}}
	unit := VideoBillingUnitSecond
	price := 0.25
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformComposite
	group.ModelPricing = []ChannelModelPricing{{
		Platform: PlatformByteDance, Models: []string{ByteDanceVideoModelSeedance10Pro}, BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{ID: 8, PerRequestPrice: &price, BillingUnit: &unit}},
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	svc.composite = nil
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Platform = PlatformByteDance
	accounts.accounts[0].Credentials = map[string]any{
		"api_key": "ark-secret",
		"model_mapping": map[string]any{
			ByteDanceVideoModelSeedance10Pro: ByteDanceVideoModelSeedance10Pro,
		},
	}
	sourceID, providerID, accountID := NewVideoTaskID(), "video_bytedance_source", int64(11)
	tasks.sources[sourceID] = &VideoTask{
		ID: 9, PublicID: sourceID, UserID: 42, Provider: VideoProviderByteDance,
		ProviderTaskID: &providerID, AccountID: &accountID,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		DeleteState: VideoDeleteNone, RequestedModel: "video-alias",
		PublicModel: ByteDanceVideoModelSeedance10Pro, UpstreamModel: ByteDanceVideoModelSeedance10Pro,
		RequestAttributes: map[string]any{"seconds": 8, "resolution": "720p"},
	}
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	request.Operation = VideoOperationEdit
	request.SourceVideoID = sourceID
	request.Model = ""
	request.Size = ""
	request.IdempotencyKey = "bytedance-source-derived"

	_, err := svc.resolveSubmission(
		WithResolvedTargetPlatform(context.Background(), PlatformByteDance), request, nil,
	)

	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	require.Zero(t, provider.createCalls)
	require.Empty(t, tasks.create.PublicID)
}

func TestByteDanceVideoWorkerSettlementUsesTaskProviderPlatform(t *testing.T) {
	seconds := 8.0
	unit := VideoBillingUnitSecond
	task := &VideoTask{
		Provider: VideoProviderByteDance, UpstreamModel: ByteDanceVideoModelSeedance10Pro,
		BillingUnit: &unit, PriceSnapshot: map[string]any{
			"unit_price": 0.25, "customer_multiplier": 1.0, "seconds": seconds, "resolution": "720p",
		},
		RequestAttributes: map[string]any{"seconds": seconds, "resolution": "720p"},
		CreatedAt:         time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
	}
	settlement, usage, err := buildVideoUsageSettlement(task, "video:capture:bytedance", seconds)
	require.NoError(t, err)
	require.Equal(t, PlatformByteDance, settlement.Platform)
	require.Equal(t, "720p", *usage.VideoResolution)
}
