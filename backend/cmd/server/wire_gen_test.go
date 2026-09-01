package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type cleanupModelCatalogAccountRepoStub struct {
	service.AccountRepository
	listCalls chan struct{}
}

type cleanupRadarRepositoryStub struct {
	service.RadarCacheRepository
	lockCalls atomic.Int32
}

func (*cleanupRadarRepositoryStub) GetRadarAggregatorState(context.Context) (service.RadarMetricsSnapshot, error) {
	return service.RadarMetricsSnapshot{AggregatorStateValid: true}, nil
}

func cleanupRadarAdminController(t *testing.T, cfg *config.Config, repo service.RadarCacheRepository) *service.RadarAdminController {
	t.Helper()
	controller, err := service.NewRadarAdminController(cfg, repo, &service.SettingService{})
	require.NoError(t, err)
	return controller
}

type cleanupRadarRuntimeGate bool

func (enabled cleanupRadarRuntimeGate) IsRadarEnabled(context.Context) bool {
	return bool(enabled)
}

type cleanupRadarCatalog struct{}

func (*cleanupRadarCatalog) ListPublicPassive(context.Context) (map[string][]string, error) {
	return map[string][]string{service.PlatformOpenAI: {"gpt-5.5"}}, nil
}

func cleanupRadarFetchersConstructor() radarFetchersConstructor {
	return func(cfg *config.Config) ([]service.RadarFetcher, error) {
		return service.NewRadarFetchers(cfg, &cleanupRadarCatalog{})
	}
}

func TestProvideRadarRuntimeSettingReaderUsesApplicationSettingService(t *testing.T) {
	settingService := &service.SettingService{}
	runtimeReader := provideRadarRuntimeSettingReader(settingService)
	require.Same(t, settingService, runtimeReader)
}

func (*cleanupRadarRepositoryStub) AppendBucketSnapshot(context.Context, service.BucketSnapshotDTO) error {
	return nil
}

func (*cleanupRadarRepositoryStub) ReplaceActiveBucketKeys(context.Context, []string) error {
	return nil
}

func (*cleanupRadarRepositoryStub) ListSourceMeta(context.Context) (map[service.RadarSourceKey]service.SourceFetchMeta, error) {
	return map[service.RadarSourceKey]service.SourceFetchMeta{}, nil
}

func (*cleanupRadarRepositoryStub) AdvanceSourceNextFire(_ context.Context, _ service.RadarSourceKey, nextFireAt time.Time) (service.RadarSourceCadence, error) {
	return service.RadarSourceCadence{NextFireAt: nextFireAt, Version: "1"}, nil
}

func (*cleanupRadarRepositoryStub) GetSourceCadence(context.Context, service.RadarSourceKey) (service.RadarSourceCadence, error) {
	return service.RadarSourceCadence{}, service.ErrRadarCacheMiss
}

type cleanupRadarAggregatorDependencies struct{}

func (*cleanupRadarAggregatorDependencies) ListAllWithFilters(
	context.Context,
	string,
	string,
	string,
	string,
	int64,
	string,
) ([]service.Account, error) {
	return nil, nil
}

func (*cleanupRadarAggregatorDependencies) GetRadarUsageSnapshot(context.Context, *service.Account) (*service.UsageInfo, error) {
	return nil, service.ErrRadarUsageSnapshotUnavailable
}

func (*cleanupRadarAggregatorDependencies) GetAccountWindowStatsBatch(
	context.Context,
	[]int64,
	time.Time,
) (map[int64]*usagestats.AccountStats, error) {
	return nil, nil
}

func (*cleanupRadarAggregatorDependencies) GetAccountModelBreakdownBatch(
	context.Context,
	[]int64,
	time.Time,
) (map[int64]map[string]service.ModelCostStats, error) {
	return nil, nil
}

func (*cleanupRadarAggregatorDependencies) GetAccountModelBreakdownByWindowBatch(
	context.Context,
	[]service.RadarQuotaAccountWindow,
) (map[int64]map[string]service.ModelCostStats, error) {
	return nil, nil
}

func (r *cleanupRadarRepositoryStub) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	r.lockCalls.Add(1)
	return true, nil
}

func (*cleanupRadarRepositoryStub) ReleaseLock(context.Context, string, string) error {
	return nil
}

type cleanupRadarBlockingFetcher struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (*cleanupRadarBlockingFetcher) Source() service.RadarSourceKey {
	return service.RadarSourceStatusClaude
}

func (*cleanupRadarBlockingFetcher) Interval() time.Duration {
	return time.Minute
}

func (f *cleanupRadarBlockingFetcher) Fetch(ctx context.Context) ([]byte, service.SourceFetchMeta, error) {
	close(f.started)
	<-ctx.Done()
	close(f.canceled)
	<-f.release
	return nil, service.SourceFetchMeta{}, ctx.Err()
}

type cleanupCloseTrackingConn struct {
	net.Conn
	closed    chan struct{}
	closeOnce *sync.Once
}

type applicationCleanupProbe struct {
	factoryCalls atomic.Int32
	cleanupCalls atomic.Int32
	mu           sync.Mutex
	runners      []*service.RadarRunner
}

func (p *applicationCleanupProbe) factory() cleanupFactory {
	return func(runner *service.RadarRunner) func() {
		p.factoryCalls.Add(1)
		p.mu.Lock()
		p.runners = append(p.runners, runner)
		p.mu.Unlock()
		return func() {
			p.cleanupCalls.Add(1)
			if runner != nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = runner.Stop(ctx)
			}
		}
	}
}

func (p *applicationCleanupProbe) runner() *service.RadarRunner {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.runners) == 0 {
		return nil
	}
	return p.runners[len(p.runners)-1]
}

func (c *cleanupCloseTrackingConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func cleanupRadarConfig() *config.Config {
	return &config.Config{Radar: config.RadarConfig{
		Enabled:                                       true,
		QuotaAggregatorIntervalMin:                    15,
		QuotaHistoryRetentionDays:                     7,
		SampleSizeWarnBelow:                           3,
		PublicMinBucketAccounts:                       2,
		InferMinUtilization:                           5,
		InferMaxStdevRatio:                            0.3,
		ExternalRequestTimeoutSeconds:                 10,
		ExternalResponseMaxBytes:                      1024 * 1024,
		ArtificialAnalysisModelsIntervalMinutes:       360,
		LMArenaIntervalMinutes:                        1440,
		StatuspageIntervalMinutes:                     30,
		SourceHardRetentionDays:                       7,
		QuotaStaleThresholdMinutes:                    30,
		HealthStaleThresholdMinutes:                   60,
		ArtificialAnalysisModelsStaleThresholdMinutes: 720,
		LMArenaStaleThresholdMinutes:                  2880,
		LMArenaURL:                                    "https://datasets-server.huggingface.co/filter",
	}}
}

func cleanupRadarAggregator(
	t *testing.T,
	cfg *config.Config,
	repo service.RadarCacheRepository,
) *service.RadarQuotaAggregator {
	t.Helper()
	dependencies := &cleanupRadarAggregatorDependencies{}
	aggregator, err := service.NewRadarQuotaAggregator(
		dependencies,
		dependencies,
		dependencies,
		repo,
		&cfg.Radar,
	)
	require.NoError(t, err)
	return aggregator
}

func cleanupRadarAggregatorFactory(
	t *testing.T,
	cfg *config.Config,
	repo service.RadarCacheRepository,
) radarQuotaAggregatorConstructor {
	t.Helper()
	return func() (*service.RadarQuotaAggregator, error) {
		return cleanupRadarAggregator(t, cfg, repo), nil
	}
}

func cleanupWithRadarTestDependencies(rdb *redis.Client, radarRunner *service.RadarRunner) func() {
	cfg := &config.Config{}
	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)
	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)

	return provideCleanup(
		nil, // entClient
		rdb,
		nil, // opsMetricsCollector
		nil, // opsAggregation
		nil, // opsAlertEvaluator
		nil, // opsCleanup
		nil, // opsScheduledReport
		nil, // opsSystemLogSink
		nil, // opsService
		nil, // opsIngressReject
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		nil, // notificationEmailWorker
		nil, // apiKeyRotation
		nil, // usageBillingOutboxWorker
		nil, // schedulerSnapshot
		tokenRefreshSvc,
		service.NewAccountExpiryService(nil, time.Second),
		nil, // cnProviderBalanceCheck
		nil, // codexVersionSync
		service.NewProxyExpiryService(nil, time.Second),
		service.NewSubscriptionExpiryService(nil, time.Second),
		nil, // vipReconcile
		nil, // vipIncrementalReconcile
		nil, // usageCleanup
		nil, // idempotencyCleanup
		nil, // batchImageCleanup
		nil, // batchImageWorker
		service.NewPricingService(cfg, nil),
		service.NewEmailQueueService(nil, 1),
		service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil),
		nil, // usageRecordWorkerPool
		nil, // subscriptionService
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // billingRecovery
		nil, // miniMaxRemainsSyncRunner
		nil, // deepSeekBalanceHealthRunner
		nil, // channelMonitorRunner
		nil, // channelMonitorV2Aggregator
		nil, // modelCatalogRefreshRunner
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // ollamaCloudUsage
		nil, // auditLog
		nil, // openAIAutoReset
		nil, // promptAudit
		nil, // pluginManager
	)(radarRunner)
}

func (s *cleanupModelCatalogAccountRepoStub) ListSchedulable(context.Context) ([]service.Account, error) {
	select {
	case s.listCalls <- struct{}{}:
	default:
	}
	return nil, nil
}

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestGeneratedWireDefersRadarQuotaAggregatorConstructionToLifecycleSeam(t *testing.T) {
	generated, err := os.ReadFile("wire_gen.go")
	require.NoError(t, err)
	generatedSource := string(generated)
	require.Equal(t, 1, strings.Count(generatedSource, ":= provideRadarQuotaAggregatorConstructor("))
	injector, _, found := strings.Cut(generatedSource, "\n// wire.go:")
	require.True(t, found)
	require.NotContains(t, injector, "service.ProvideRadarQuotaAggregator(")
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	codexVersionSyncSvc := service.NewOpenAICodexVersionSyncService(nil, nil, nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)
	modelCatalogCfg := config.ModelCatalogConfig{
		RefreshIntervalSeconds: 300,
		RequestTimeoutSeconds:  10,
		StaleTTLSeconds:        86400,
		FailureBackoffSeconds:  60,
		MaxConcurrency:         1,
	}
	modelCatalogListCalls := make(chan struct{}, 1)
	modelCatalogSvc := service.NewModelCatalogService(
		&cleanupModelCatalogAccountRepoStub{listCalls: modelCatalogListCalls},
		nil,
		nil,
		nil,
		modelCatalogCfg,
	)
	modelCatalogRefreshRunner := service.NewModelCatalogRefreshRunner(modelCatalogSvc, modelCatalogCfg)
	modelCatalogRefreshRunner.Start()
	select {
	case <-modelCatalogListCalls:
	case <-time.After(time.Second):
		t.Fatal("model catalog runner did not start")
	}

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		nil, // notificationEmailWorker
		nil, // apiKeyRotation
		nil, // usageBillingOutboxWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		nil, // cnProviderBalanceCheck
		codexVersionSyncSvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		nil, // vipReconcile
		nil, // vipIncrementalReconcile
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // billingRecovery
		nil, // miniMaxRemainsSyncRunner
		nil, // deepSeekBalanceHealthRunner
		nil, // channelMonitorRunner
		nil, // channelMonitorV2Aggregator
		nil, // modelCatalogRefreshRunner
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // ollamaCloudUsage
		nil, // auditLog
		nil, // openAIAutoReset
		nil, // promptAudit
		nil, // pluginManager
	)(nil)

	require.NotPanics(t, func() {
		cleanup()
	})
}

func TestProvideCleanupStopsRadarBeforeClosingRedis(t *testing.T) {
	miniRedis := miniredis.RunT(t)
	redisClosed := make(chan struct{})
	var redisCloseOnce sync.Once
	rdb := redis.NewClient(&redis.Options{
		Addr: miniRedis.Addr(),
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &cleanupCloseTrackingConn{
				Conn:      conn,
				closed:    redisClosed,
				closeOnce: &redisCloseOnce,
			}, nil
		},
	})
	require.NoError(t, rdb.Ping(context.Background()).Err())

	fetcher := &cleanupRadarBlockingFetcher{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
	radarRunner, err := service.NewRadarRunner(
		cleanupRadarConfig(),
		&cleanupRadarRepositoryStub{},
		[]service.RadarFetcher{fetcher},
		cleanupRadarAggregator(t, cleanupRadarConfig(), &cleanupRadarRepositoryStub{}),
		cleanupRadarRuntimeGate(true),
	)
	require.NoError(t, err)
	radarRunner.Start()

	select {
	case <-fetcher.started:
	case <-time.After(time.Second):
		t.Fatal("Radar runner did not start")
	}

	cleanup := cleanupWithRadarTestDependencies(rdb, radarRunner)
	cleanupDone := make(chan struct{})
	go func() {
		cleanup()
		close(cleanupDone)
	}()

	select {
	case <-fetcher.canceled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop Radar runner")
	}

	select {
	case <-redisClosed:
		t.Fatal("Redis closed before Radar runner Stop returned")
	case <-time.After(100 * time.Millisecond):
	}

	close(fetcher.release)
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish after Radar runner stopped")
	}
	select {
	case <-redisClosed:
	case <-time.After(time.Second):
		t.Fatal("Redis did not close after Radar runner stopped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, radarRunner.Stop(ctx), "cleanup must leave Radar runner stopped")
}

func TestProvideApplicationRollsBackWhenRunnerBudgetIsIncompatible(t *testing.T) {
	cfg := cleanupRadarConfig()
	cfg.Radar.ExternalRequestTimeoutSeconds = 20
	cfg.Radar.LMArenaIntervalMinutes = 1
	cfg.Radar.StatuspageIntervalMinutes = 1
	require.NoError(t, cfg.Radar.Validate(), "fixture must reach runner budget validation")
	repo := &cleanupRadarRepositoryStub{}
	probe := &applicationCleanupProbe{}

	app, err := provideApplication(&http.Server{}, nil, nil, cfg, repo, cleanupRadarRuntimeGate(true), cleanupRadarAdminController(t, cfg, repo), probe.factory(), cleanupRadarAggregatorFactory(t, cfg, repo), cleanupRadarFetchersConstructor())

	require.Error(t, err)
	require.Nil(t, app)
	require.Equal(t, int32(1), probe.factoryCalls.Load())
	require.Equal(t, int32(1), probe.cleanupCalls.Load())
	require.Nil(t, probe.runner())
	require.Zero(t, repo.lockCalls.Load(), "failed runner construction must not start work")
}

func TestProvideApplicationRollsBackAggregatorConstructionFailure(t *testing.T) {
	cfg := cleanupRadarConfig()
	repo := &cleanupRadarRepositoryStub{}
	probe := &applicationCleanupProbe{}
	var fetcherConstructorCalls atomic.Int32
	var runnerConstructorCalls atomic.Int32

	app, err := provideApplicationWithRadarConstructors(
		&http.Server{}, nil, nil,

		cfg,
		repo,
		cleanupRadarRuntimeGate(true),
		cleanupRadarAdminController(t, cfg, repo),
		probe.factory(),
		func() (*service.RadarQuotaAggregator, error) {
			return nil, errors.New("quota aggregator construction failed")
		},
		func(*config.Config) ([]service.RadarFetcher, error) {
			fetcherConstructorCalls.Add(1)
			return nil, nil
		},
		func(
			*config.Config,
			service.RadarCacheRepository,
			[]service.RadarFetcher,
			*service.RadarQuotaAggregator,
			service.RadarRuntimeSettingReader,
		) (*service.RadarRunner, error) {
			runnerConstructorCalls.Add(1)
			return nil, nil
		})

	require.Error(t, err)
	require.Nil(t, app)
	require.Equal(t, int32(1), probe.factoryCalls.Load())
	require.Equal(t, int32(1), probe.cleanupCalls.Load())
	require.Nil(t, probe.runner())
	require.Zero(t, fetcherConstructorCalls.Load())
	require.Zero(t, runnerConstructorCalls.Load())
	require.Zero(t, repo.lockCalls.Load())
}

func TestProvideApplicationRollsBackFetcherFailureWhileDisabled(t *testing.T) {
	cfg := cleanupRadarConfig()
	cfg.Radar.Enabled = false
	cfg.Update.ProxyURL = "http://proxy-user:proxy-secret@%"
	require.NoError(t, cfg.Radar.Validate(), "disabled fixture must reach HTTP client construction")
	repo := &cleanupRadarRepositoryStub{}
	probe := &applicationCleanupProbe{}

	app, err := provideApplication(&http.Server{}, nil, nil, cfg, repo, cleanupRadarRuntimeGate(false), cleanupRadarAdminController(t, cfg, repo), probe.factory(), cleanupRadarAggregatorFactory(t, cfg, repo), cleanupRadarFetchersConstructor())

	require.Error(t, err)
	require.Nil(t, app)
	require.Equal(t, int32(1), probe.factoryCalls.Load())
	require.Equal(t, int32(1), probe.cleanupCalls.Load())
	require.Nil(t, probe.runner())
	require.Zero(t, repo.lockCalls.Load())
	require.NotContains(t, err.Error(), "proxy-secret")
}

func TestProvideApplicationDisabledSucceedsWithoutScheduling(t *testing.T) {
	cfg := cleanupRadarConfig()
	cfg.Radar.Enabled = false
	require.NoError(t, cfg.Radar.Validate())
	repo := &cleanupRadarRepositoryStub{}
	probe := &applicationCleanupProbe{}
	httpServer := &http.Server{}

	app, err := provideApplication(httpServer, nil, nil, cfg, repo, cleanupRadarRuntimeGate(false), cleanupRadarAdminController(t, cfg, repo), probe.factory(), cleanupRadarAggregatorFactory(t, cfg, repo), cleanupRadarFetchersConstructor())

	require.NoError(t, err)
	require.NotNil(t, app)
	require.Same(t, httpServer, app.Server)
	require.NotNil(t, app.Cleanup)
	require.NotNil(t, probe.runner())
	require.Equal(t, int32(1), probe.factoryCalls.Load())
	require.Zero(t, probe.cleanupCalls.Load())
	require.Zero(t, repo.lockCalls.Load())

	app.Cleanup()
	require.Equal(t, int32(1), probe.cleanupCalls.Load())
}

func TestProvideApplicationStartsEnabledRunnerAfterAllFallibleConstruction(t *testing.T) {
	cfg := cleanupRadarConfig()
	require.NoError(t, cfg.Radar.Validate())
	repo := &cleanupRadarRepositoryStub{}
	probe := &applicationCleanupProbe{}
	fetcher := &cleanupRadarBlockingFetcher{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
	httpServer := &http.Server{}
	aggregator := cleanupRadarAggregator(t, cfg, repo)
	var aggregatorConstructorCalls atomic.Int32
	var runnerConstructorCalls atomic.Int32

	app, err := provideApplicationWithRadarConstructors(
		httpServer, nil, nil,

		cfg,
		repo,
		cleanupRadarRuntimeGate(true),
		cleanupRadarAdminController(t, cfg, repo),
		probe.factory(),
		func() (*service.RadarQuotaAggregator, error) {
			aggregatorConstructorCalls.Add(1)
			return aggregator, nil
		},
		func(*config.Config) ([]service.RadarFetcher, error) {
			return []service.RadarFetcher{fetcher}, nil
		},
		func(
			gotCfg *config.Config,
			gotRepo service.RadarCacheRepository,
			fetchers []service.RadarFetcher,
			gotAggregator *service.RadarQuotaAggregator,
			gotRuntimeGate service.RadarRuntimeSettingReader,
		) (*service.RadarRunner, error) {
			runnerConstructorCalls.Add(1)
			require.Same(t, aggregator, gotAggregator)
			require.Equal(t, cleanupRadarRuntimeGate(true), gotRuntimeGate)
			return service.NewRadarRunner(gotCfg, gotRepo, fetchers, gotAggregator, gotRuntimeGate)
		})

	require.NoError(t, err)
	require.NotNil(t, app)
	require.Same(t, httpServer, app.Server)

	select {
	case <-fetcher.started:
	case <-time.After(time.Second):
		t.Fatal("application did not start Radar runner")
	}
	require.Equal(t, int32(1), probe.factoryCalls.Load())
	require.Zero(t, probe.cleanupCalls.Load())
	require.NotNil(t, probe.runner())
	require.Equal(t, int32(1), aggregatorConstructorCalls.Load())
	require.Equal(t, int32(1), runnerConstructorCalls.Load())

	close(fetcher.release)
	app.Cleanup()
	require.Equal(t, int32(1), probe.cleanupCalls.Load())
	select {
	case <-fetcher.canceled:
	case <-time.After(time.Second):
		t.Fatal("application cleanup did not stop Radar runner")
	}
}

func TestValidatedConfigFrontloadsRadarRepositoryServiceAndHandlerConditions(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("GROUP_ACCESS_RUNTIME_MODE", config.GroupAccessRuntimeModeAuditOnly)

	cfg, err := config.Load()
	require.NoError(t, err)

	miniRedis := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	radarRepo, err := repository.NewRadarCacheRepository(rdb, cfg)
	require.NoError(t, err)
	radarService, err := service.NewRadarService(cfg, radarRepo)
	require.NoError(t, err)
	radarHandler, err := handler.NewRadarHandler(cfg, radarService)
	require.NoError(t, err)
	require.NotNil(t, radarHandler)

}
