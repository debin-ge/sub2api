//go:build wireinject
// +build wireinject

package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

type Application struct {
	Server      *http.Server
	PromptAudit *securityaudit.PromptService
	Cleanup     func()
}

type cleanupFactory func(radarRunner *service.RadarRunner) func()

type radarFetchersConstructor func(cfg *config.Config) ([]service.RadarFetcher, error)

type radarQuotaAggregatorConstructor func() (*service.RadarQuotaAggregator, error)

type radarRunnerConstructor func(
	cfg *config.Config,
	repo service.RadarCacheRepository,
	fetchers []service.RadarFetcher,
	quotaAggregator *service.RadarQuotaAggregator,
	runtimeGate service.RadarRuntimeSettingReader,
) (*service.RadarRunner, error)

func initializeApplication(buildInfo handler.BuildInfo) (*Application, error) {
	wire.Build(
		// Infrastructure layer ProviderSets
		config.ProviderSet,

		// Business layer ProviderSets
		repository.ProviderSet,
		service.ProviderSet,
		securityaudit.ProviderSet,
		payment.ProviderSet,
		middleware.ProviderSet,
		handler.ProviderSet,

		// Server layer ProviderSet
		server.ProviderSet,

		// Privacy client factory for OpenAI training opt-out
		providePrivacyClientFactory,

		// BuildInfo provider
		provideServiceBuildInfo,

		// Cleanup function provider
		provideCleanup,
		provideRadarQuotaAggregatorConstructor,
		provideRadarFetchersConstructor,
		provideRadarRuntimeSettingReader,

		// Final application provider. Radar remains the final fallible constructor
		// so failures can roll back all previously started dependencies.
		provideApplication,
	)
	return nil, nil
}

func providePrivacyClientFactory() service.PrivacyClientFactory {
	return repository.CreatePrivacyReqClient
}

func provideServiceBuildInfo(buildInfo handler.BuildInfo) service.BuildInfo {
	return service.BuildInfo{
		Version:   buildInfo.Version,
		BuildType: buildInfo.BuildType,
	}
}

func provideRadarQuotaAggregatorConstructor(
	accountRepo service.AccountRepository,
	usageReader service.RadarUsageSnapshotReader,
	batchReader service.RadarQuotaBatchReader,
	cacheRepo service.RadarCacheRepository,
	cfg *config.Config,
) radarQuotaAggregatorConstructor {
	return func() (*service.RadarQuotaAggregator, error) {
		return service.ProvideRadarQuotaAggregator(accountRepo, usageReader, batchReader, cacheRepo, cfg)
	}
}

func provideRadarRuntimeSettingReader(settingService *service.SettingService) service.RadarRuntimeSettingReader {
	return settingService
}

func provideRadarFetchersConstructor(modelCatalog *service.ModelCatalogService) radarFetchersConstructor {
	return func(cfg *config.Config) ([]service.RadarFetcher, error) {
		return service.NewRadarFetchers(cfg, modelCatalog)
	}
}

func provideApplication(
	httpServer *http.Server,
	promptAudit *securityaudit.PromptService,
	cfg *config.Config,
	radarRepo service.RadarCacheRepository,
	runtimeGate service.RadarRuntimeSettingReader,
	radarAdminController *service.RadarAdminController,
	newCleanup cleanupFactory,
	newQuotaAggregator radarQuotaAggregatorConstructor,
	newFetchers radarFetchersConstructor,
) (*Application, error) {
	return provideApplicationWithRadarConstructors(
		httpServer,
		promptAudit,
		cfg,
		radarRepo,
		runtimeGate,
		radarAdminController,
		newCleanup,
		newQuotaAggregator,
		newFetchers,
		service.NewRadarRunner,
	)
}

func provideApplicationWithRadarConstructors(
	httpServer *http.Server,
	promptAudit *securityaudit.PromptService,
	cfg *config.Config,
	radarRepo service.RadarCacheRepository,
	runtimeGate service.RadarRuntimeSettingReader,
	radarAdminController *service.RadarAdminController,
	newCleanup cleanupFactory,
	newQuotaAggregator radarQuotaAggregatorConstructor,
	newFetchers radarFetchersConstructor,
	newRunner radarRunnerConstructor,
) (*Application, error) {
	quotaAggregator, err := newQuotaAggregator()
	if err != nil {
		newCleanup(nil)()
		return nil, err
	}

	fetchers, err := newFetchers(cfg)
	if err != nil {
		newCleanup(nil)()
		return nil, err
	}

	radarRunner, err := newRunner(cfg, radarRepo, fetchers, quotaAggregator, runtimeGate)
	if err != nil {
		newCleanup(nil)()
		return nil, err
	}

	cleanup := newCleanup(radarRunner)
	if err := radarAdminController.BindRunner(radarRunner); err != nil {
		cleanup()
		return nil, err
	}
	radarRunner.Start()
	return &Application{Server: httpServer, PromptAudit: promptAudit, Cleanup: cleanup}, nil
}

func provideCleanup(
	entClient *ent.Client,
	rdb *redis.Client,
	opsMetricsCollector *service.OpsMetricsCollector,
	opsAggregation *service.OpsAggregationService,
	opsAlertEvaluator *service.OpsAlertEvaluatorService,
	opsCleanup *service.OpsCleanupService,
	opsScheduledReport *service.OpsScheduledReportService,
	opsSystemLogSink *service.OpsSystemLogSink,
	opsService *service.OpsService,
	opsIngressReject *service.OpsIngressRejectAggregator,
	apiKeyService *service.APIKeyService,
	authCacheInvalidationWorker *service.AuthCacheInvalidationWorker,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	accountExpiry *service.AccountExpiryService,
	proxyExpiry *service.ProxyExpiryService,
	subscriptionExpiry *service.SubscriptionExpiryService,
	usageCleanup *service.UsageCleanupService,
	idempotencyCleanup *service.IdempotencyCleanupService,
	batchImageCleanup *service.BatchImageCleanupService,
	batchImageWorker *service.BatchImageWorkerRuntime,
	pricing *service.PricingService,
	emailQueue *service.EmailQueueService,
	billingCache *service.BillingCacheService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	subscriptionService *service.SubscriptionService,
	oauth *service.OAuthService,
	openaiOAuth *service.OpenAIOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	antigravityOAuth *service.AntigravityOAuthService,
	grokOAuth *service.GrokOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	scheduledTestRunner *service.ScheduledTestRunnerService,
	backupSvc *service.BackupService,
	paymentOrderExpiry *service.PaymentOrderExpiryService,
	miniMaxRemainsSyncRunner *service.MiniMaxRemainsSyncRunner,
	deepSeekBalanceHealthRunner *service.DeepSeekBalanceHealthRunner,
	channelMonitorRunner *service.ChannelMonitorRunner,
	modelCatalogRefreshRunner *service.ModelCatalogRefreshRunner,
	quotaFlusher *service.UserPlatformQuotaUsageFlusher,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	auditLog *service.AuditLogService,
	promptAudit *securityaudit.PromptService,
) cleanupFactory {
	return func(radarRunner *service.RadarRunner) func() {
		return provideFinalCleanup(
			entClient,
			rdb,
			opsMetricsCollector,
			opsAggregation,
			opsAlertEvaluator,
			opsCleanup,
			opsScheduledReport,
			opsSystemLogSink,
			opsService,
			opsIngressReject,
			apiKeyService,
			authCacheInvalidationWorker,
			schedulerSnapshot,
			tokenRefresh,
			accountExpiry,
			proxyExpiry,
			subscriptionExpiry,
			usageCleanup,
			idempotencyCleanup,
			batchImageCleanup,
			batchImageWorker,
			pricing,
			emailQueue,
			billingCache,
			usageRecordWorkerPool,
			subscriptionService,
			oauth,
			openaiOAuth,
			geminiOAuth,
			antigravityOAuth,
			grokOAuth,
			openAIGateway,
			scheduledTestRunner,
			backupSvc,
			paymentOrderExpiry,
			miniMaxRemainsSyncRunner,
			deepSeekBalanceHealthRunner,
			channelMonitorRunner,
			modelCatalogRefreshRunner,
			quotaFlusher,
			upstreamBillingProbe,
			auditLog,
			promptAudit,
			radarRunner,
		)
	}
}

func provideFinalCleanup(
	entClient *ent.Client,
	rdb *redis.Client,
	opsMetricsCollector *service.OpsMetricsCollector,
	opsAggregation *service.OpsAggregationService,
	opsAlertEvaluator *service.OpsAlertEvaluatorService,
	opsCleanup *service.OpsCleanupService,
	opsScheduledReport *service.OpsScheduledReportService,
	opsSystemLogSink *service.OpsSystemLogSink,
	opsService *service.OpsService,
	opsIngressReject *service.OpsIngressRejectAggregator,
	apiKeyService *service.APIKeyService,
	authCacheInvalidationWorker *service.AuthCacheInvalidationWorker,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	accountExpiry *service.AccountExpiryService,
	proxyExpiry *service.ProxyExpiryService,
	subscriptionExpiry *service.SubscriptionExpiryService,
	usageCleanup *service.UsageCleanupService,
	idempotencyCleanup *service.IdempotencyCleanupService,
	batchImageCleanup *service.BatchImageCleanupService,
	batchImageWorker *service.BatchImageWorkerRuntime,
	pricing *service.PricingService,
	emailQueue *service.EmailQueueService,
	billingCache *service.BillingCacheService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	subscriptionService *service.SubscriptionService,
	oauth *service.OAuthService,
	openaiOAuth *service.OpenAIOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	antigravityOAuth *service.AntigravityOAuthService,
	grokOAuth *service.GrokOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	scheduledTestRunner *service.ScheduledTestRunnerService,
	backupSvc *service.BackupService,
	paymentOrderExpiry *service.PaymentOrderExpiryService,
	miniMaxRemainsSyncRunner *service.MiniMaxRemainsSyncRunner,
	deepSeekBalanceHealthRunner *service.DeepSeekBalanceHealthRunner,
	channelMonitorRunner *service.ChannelMonitorRunner,
	modelCatalogRefreshRunner *service.ModelCatalogRefreshRunner,
	quotaFlusher *service.UserPlatformQuotaUsageFlusher,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	auditLog *service.AuditLogService,
	promptAudit *securityaudit.PromptService,
	radarRunner *service.RadarRunner,
) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		type cleanupStep struct {
			name string
			fn   func() error
		}

		// 应用层清理步骤可并行执行，基础设施资源（Redis/Ent）最后按顺序关闭。
		parallelSteps := []cleanupStep{
			{"OpsIngressRejectAggregator", func() error {
				if opsIngressReject != nil {
					opsIngressReject.Stop()
				}
				return nil
			}},
			{"AuthCacheInvalidationWorker", func() error {
				if authCacheInvalidationWorker != nil {
					authCacheInvalidationWorker.Stop()
				}
				return nil
			}},
			{"AuthCacheInvalidationSubscriber", func() error {
				if apiKeyService != nil {
					apiKeyService.StopAuthCacheInvalidationSubscriber()
				}
				return nil
			}},
			{"OpsRuntimeSettingsRefresh", func() error {
				if opsService != nil {
					opsService.StopRuntimeSettingsRefresh()
				}
				return nil
			}},
			{"PromptAuditService", func() error {
				if promptAudit != nil {
					return promptAudit.Shutdown(ctx)
				}
				return nil
			}},
			{"OpsScheduledReportService", func() error {
				if opsScheduledReport != nil {
					opsScheduledReport.Stop()
				}
				return nil
			}},
			{"OpsCleanupService", func() error {
				if opsCleanup != nil {
					opsCleanup.Stop()
				}
				return nil
			}},
			{"OpsSystemLogSink", func() error {
				if opsSystemLogSink != nil {
					opsSystemLogSink.Stop()
				}
				return nil
			}},
			{"AuditLogService", func() error {
				if auditLog != nil {
					auditLog.Stop()
				}
				return nil
			}},
			{"OpsAlertEvaluatorService", func() error {
				if opsAlertEvaluator != nil {
					opsAlertEvaluator.Stop()
				}
				return nil
			}},
			{"OpsAggregationService", func() error {
				if opsAggregation != nil {
					opsAggregation.Stop()
				}
				return nil
			}},
			{"OpsMetricsCollector", func() error {
				if opsMetricsCollector != nil {
					opsMetricsCollector.Stop()
				}
				return nil
			}},
			{"SchedulerSnapshotService", func() error {
				if schedulerSnapshot != nil {
					schedulerSnapshot.Stop()
				}
				return nil
			}},
			{"UsageCleanupService", func() error {
				if usageCleanup != nil {
					usageCleanup.Stop()
				}
				return nil
			}},
			{"IdempotencyCleanupService", func() error {
				if idempotencyCleanup != nil {
					idempotencyCleanup.Stop()
				}
				return nil
			}},
			{"BatchImageCleanupService", func() error {
				if batchImageCleanup != nil {
					batchImageCleanup.Stop()
				}
				return nil
			}},
			{"BatchImageWorkerRuntime", func() error {
				if batchImageWorker != nil {
					batchImageWorker.Stop()
				}
				return nil
			}},
			{"TokenRefreshService", func() error {
				tokenRefresh.Stop()
				return nil
			}},
			{"AccountExpiryService", func() error {
				accountExpiry.Stop()
				return nil
			}},
			{"ProxyExpiryService", func() error {
				proxyExpiry.Stop()
				return nil
			}},
			{"SubscriptionExpiryService", func() error {
				subscriptionExpiry.Stop()
				return nil
			}},
			{"SubscriptionService", func() error {
				if subscriptionService != nil {
					subscriptionService.Stop()
				}
				return nil
			}},
			{"PricingService", func() error {
				pricing.Stop()
				return nil
			}},
			{"EmailQueueService", func() error {
				emailQueue.Stop()
				return nil
			}},
			{"BillingCacheService", func() error {
				billingCache.Stop()
				return nil
			}},
			{"UsageRecordWorkerPool", func() error {
				if usageRecordWorkerPool != nil {
					usageRecordWorkerPool.Stop()
				}
				return nil
			}},
			{"OAuthService", func() error {
				oauth.Stop()
				return nil
			}},
			{"OpenAIOAuthService", func() error {
				openaiOAuth.Stop()
				return nil
			}},
			{"GeminiOAuthService", func() error {
				geminiOAuth.Stop()
				return nil
			}},
			{"AntigravityOAuthService", func() error {
				antigravityOAuth.Stop()
				return nil
			}},
			{"GrokOAuthService", func() error {
				if grokOAuth != nil {
					grokOAuth.Stop()
				}
				return nil
			}},
			{"OpenAIWSPool", func() error {
				if openAIGateway != nil {
					openAIGateway.CloseOpenAIWSPool()
				}
				return nil
			}},
			{"ScheduledTestRunnerService", func() error {
				if scheduledTestRunner != nil {
					scheduledTestRunner.Stop()
				}
				return nil
			}},
			{"BackupService", func() error {
				if backupSvc != nil {
					backupSvc.Stop()
				}
				return nil
			}},
			{"PaymentOrderExpiryService", func() error {
				if paymentOrderExpiry != nil {
					paymentOrderExpiry.Stop()
				}
				return nil
			}},
			{"MiniMaxRemainsSyncRunner", func() error {
				if miniMaxRemainsSyncRunner != nil {
					miniMaxRemainsSyncRunner.Stop()
				}
				return nil
			}},
			{"DeepSeekBalanceHealthRunner", func() error {
				if deepSeekBalanceHealthRunner != nil {
					deepSeekBalanceHealthRunner.Stop()
				}
				return nil
			}},
			{"ChannelMonitorRunner", func() error {
				if channelMonitorRunner != nil {
					channelMonitorRunner.Stop()
				}
				return nil
			}},
			{"ModelCatalogRefreshRunner", func() error {
				if modelCatalogRefreshRunner != nil {
					modelCatalogRefreshRunner.Stop()
				}
				return nil
			}},
			{"UserPlatformQuotaUsageFlusher", func() error {
				if quotaFlusher != nil {
					quotaFlusher.Stop()
				}
				return nil
			}},
			{"RadarRunner", func() error {
				if radarRunner == nil {
					return nil
				}
				return radarRunner.Stop(ctx)
			}},
			{"UpstreamBillingProbeService", func() error {
				if upstreamBillingProbe != nil {
					upstreamBillingProbe.Stop()
				}
				return nil
			}},
		}

		infraSteps := []cleanupStep{
			{"Redis", func() error {
				if rdb == nil {
					return nil
				}
				return rdb.Close()
			}},
			{"Ent", func() error {
				if entClient == nil {
					return nil
				}
				return entClient.Close()
			}},
		}

		runParallel := func(steps []cleanupStep) {
			var wg sync.WaitGroup
			for i := range steps {
				step := steps[i]
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := step.fn(); err != nil {
						log.Printf("[Cleanup] %s failed: %v", step.name, err)
						return
					}
					log.Printf("[Cleanup] %s succeeded", step.name)
				}()
			}
			wg.Wait()
		}

		runSequential := func(steps []cleanupStep) {
			for i := range steps {
				step := steps[i]
				if err := step.fn(); err != nil {
					log.Printf("[Cleanup] %s failed: %v", step.name, err)
					continue
				}
				log.Printf("[Cleanup] %s succeeded", step.name)
			}
		}

		runParallel(parallelSteps)
		runSequential(infraSteps)

		// Check if context timed out
		select {
		case <-ctx.Done():
			log.Printf("[Cleanup] Warning: cleanup timed out after 10 seconds")
		default:
			log.Printf("[Cleanup] All cleanup steps completed")
		}
	}
}
