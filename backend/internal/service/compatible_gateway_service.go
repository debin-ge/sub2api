package service

import "github.com/Wei-Shaw/sub2api/internal/config"

// CompatibleGatewayService is the shared multi-protocol gateway service.
//
// The implementation still lives in the historical OpenAIGatewayService type
// for source compatibility with the existing test and helper surface. New
// production wiring should use the CompatibleGateway name so the service is
// not mistaken for an OpenAI-only upstream implementation.
type CompatibleGatewayService = OpenAIGatewayService

// NewCompatibleGatewayService is the canonical constructor for the shared
// multi-protocol gateway. It delegates to the legacy constructor while the
// implementation is being migrated incrementally.
func NewCompatibleGatewayService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	grokTokenProvider *GrokTokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
) *CompatibleGatewayService {
	return NewOpenAIGatewayService(
		accountRepo, usageLogRepo, usageBillingRepo, userRepo, userSubRepo,
		userGroupRateRepo, cache, cfg, schedulerSnapshot, concurrencyService,
		billingService, rateLimitService, billingCacheService, httpUpstream,
		deferredService, openAITokenProvider, grokTokenProvider, resolver,
		channelService, balanceNotifyService, settingService, userPlatformQuotaRepo,
	)
}
