package main

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cleanupModelCatalogAccountRepoStub struct {
	service.AccountRepository
	listCalls chan struct{}
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
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
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
		nil, // miniMaxRemainsSyncRunner
		nil, // deepSeekBalanceHealthRunner
		nil, // channelMonitorRunner
		modelCatalogRefreshRunner,
		nil, // quotaFlusher
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}
