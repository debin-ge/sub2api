package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type radarServiceTestRepo struct {
	mu sync.Mutex

	payloads    map[RadarSourceKey][]byte
	payloadErrs map[RadarSourceKey]error
	metas       map[RadarSourceKey]SourceFetchMeta
	metaErr     error
	metrics     RadarMetricsSnapshot
	metricsErr  error
	metaHook    func(context.Context)
	payloadHook func(context.Context, RadarSourceKey)

	getPayloadCalls map[RadarSourceKey]int
	listMetaCalls   int
	writeCalls      int
}

func newRadarServiceTestRepo() *radarServiceTestRepo {
	return &radarServiceTestRepo{
		payloads:        make(map[RadarSourceKey][]byte),
		payloadErrs:     make(map[RadarSourceKey]error),
		metas:           make(map[RadarSourceKey]SourceFetchMeta),
		metrics:         RadarMetricsSnapshot{AggregatorStateValid: true},
		getPayloadCalls: make(map[RadarSourceKey]int),
	}
}

func (r *radarServiceTestRepo) AppendBucketSnapshot(context.Context, BucketSnapshotDTO) error {
	r.recordWrite()
	return errors.New("unexpected write")
}

func (r *radarServiceTestRepo) ListBucketKeys(context.Context) ([]string, error) {
	return nil, errors.New("unexpected quota read")
}

func (r *radarServiceTestRepo) GetLatestBucket(context.Context, string) (*BucketSnapshotDTO, error) {
	return nil, errors.New("unexpected quota read")
}

func (r *radarServiceTestRepo) GetBucketTrend(context.Context, string, time.Time) ([]BucketSnapshotDTO, error) {
	return nil, errors.New("unexpected quota read")
}

func (r *radarServiceTestRepo) SetSourcePayload(context.Context, RadarSourceKey, []byte, time.Duration) error {
	r.recordWrite()
	return errors.New("unexpected write")
}

func (r *radarServiceTestRepo) GetSourcePayload(ctx context.Context, source RadarSourceKey) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.getPayloadCalls[source]++
	err := r.payloadErrs[source]
	payload, ok := r.payloads[source]
	hook := r.payloadHook
	cloned := append([]byte(nil), payload...)
	r.mu.Unlock()
	if hook != nil {
		hook(ctx, source)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRadarCacheMiss
	}
	return cloned, nil
}

func (r *radarServiceTestRepo) CommitSourceSuccess(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
	r.recordWrite()
	return false, errors.New("unexpected write")
}

func (r *radarServiceTestRepo) CommitSourceFailure(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
	r.recordWrite()
	return false, errors.New("unexpected write")
}

func (r *radarServiceTestRepo) SetSourceMeta(context.Context, RadarSourceKey, SourceFetchMeta) error {
	r.recordWrite()
	return errors.New("unexpected write")
}

func (r *radarServiceTestRepo) ListSourceMeta(ctx context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.listMetaCalls++
	err := r.metaErr
	hook := r.metaHook
	result := make(map[RadarSourceKey]SourceFetchMeta, len(r.metas))
	for key, meta := range r.metas {
		result[key] = cloneRadarServiceTestMeta(meta)
	}
	r.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *radarServiceTestRepo) GetRadarAggregatorState(ctx context.Context) (RadarMetricsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return RadarMetricsSnapshot{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.metricsErr != nil {
		return RadarMetricsSnapshot{}, r.metricsErr
	}
	return r.metrics, nil
}

func (r *radarServiceTestRepo) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	r.recordWrite()
	return false, errors.New("unexpected write")
}

func (r *radarServiceTestRepo) ReleaseLock(context.Context, string, string) error {
	r.recordWrite()
	return errors.New("unexpected write")
}

func (r *radarServiceTestRepo) recordWrite() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeCalls++
}

func (r *radarServiceTestRepo) payloadCallCount(source RadarSourceKey) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getPayloadCalls[source]
}

func (r *radarServiceTestRepo) metaCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listMetaCalls
}

func cloneRadarServiceTestMeta(meta SourceFetchMeta) SourceFetchMeta {
	result := meta
	if meta.LastSuccessAt != nil {
		value := *meta.LastSuccessAt
		result.LastSuccessAt = &value
	}
	if meta.NextFireAt != nil {
		value := *meta.NextFireAt
		result.NextFireAt = &value
	}
	if meta.HTTPStatus != nil {
		value := *meta.HTTPStatus
		result.HTTPStatus = &value
	}
	if meta.Error != nil {
		value := *meta.Error
		result.Error = &value
	}
	return result
}

type radarServiceTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *radarServiceTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *radarServiceTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type nilRadarServiceTestRepo struct{}

func (*nilRadarServiceTestRepo) AppendBucketSnapshot(context.Context, BucketSnapshotDTO) error {
	return nil
}
func (*nilRadarServiceTestRepo) ListBucketKeys(context.Context) ([]string, error) { return nil, nil }
func (*nilRadarServiceTestRepo) GetLatestBucket(context.Context, string) (*BucketSnapshotDTO, error) {
	return nil, nil
}
func (*nilRadarServiceTestRepo) GetBucketTrend(context.Context, string, time.Time) ([]BucketSnapshotDTO, error) {
	return nil, nil
}
func (*nilRadarServiceTestRepo) SetSourcePayload(context.Context, RadarSourceKey, []byte, time.Duration) error {
	return nil
}
func (*nilRadarServiceTestRepo) GetSourcePayload(context.Context, RadarSourceKey) ([]byte, error) {
	return nil, nil
}
func (*nilRadarServiceTestRepo) CommitSourceSuccess(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
	return false, nil
}
func (*nilRadarServiceTestRepo) CommitSourceFailure(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
	return false, nil
}
func (*nilRadarServiceTestRepo) SetSourceMeta(context.Context, RadarSourceKey, SourceFetchMeta) error {
	return nil
}
func (*nilRadarServiceTestRepo) ListSourceMeta(context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
	return nil, nil
}
func (*nilRadarServiceTestRepo) GetRadarAggregatorState(context.Context) (RadarMetricsSnapshot, error) {
	return RadarMetricsSnapshot{AggregatorStateValid: true}, nil
}
func (*nilRadarServiceTestRepo) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}
func (*nilRadarServiceTestRepo) ReleaseLock(context.Context, string, string) error { return nil }

func TestNewRadarServiceValidatesDependenciesAndCopiesConfig(t *testing.T) {
	cfg := radarServiceTestConfig()
	repo := newRadarServiceTestRepo()

	service, err := NewRadarService(cfg, repo)
	require.NoError(t, err)
	require.NotNil(t, service)
	require.Implements(t, (*RadarPublicService)(nil), service)

	_, err = NewRadarService(nil, repo)
	require.EqualError(t, err, "radar service requires config")

	_, err = NewRadarService(cfg, nil)
	require.EqualError(t, err, "radar service requires cache repository")

	_, err = NewRadarService(cfg, struct{ RadarCacheRepository }{RadarCacheRepository: repo})
	require.EqualError(t, err, "radar service requires aggregator state reader")

	var typedNil *nilRadarServiceTestRepo
	_, err = NewRadarService(cfg, typedNil)
	require.EqualError(t, err, "radar service requires cache repository")

	invalid := radarServiceTestConfig()
	invalid.Radar.HealthStaleThresholdMinutes = 0
	_, err = NewRadarService(invalid, repo)
	require.EqualError(t, err, "radar service requires valid radar config")

	invalidSlug := radarServiceTestConfig()
	invalidSlug.Radar.ArtificialAnalysisModelSlugs = []string{"safe-model", "../secret"}
	_, err = NewRadarService(invalidSlug, repo)
	require.EqualError(t, err, "radar service requires valid radar config")
	require.NotContains(t, err.Error(), "../secret")

	cfg.Radar.ArtificialAnalysisModelSlugs[0] = "mutated-after-construction"
	require.Equal(t, []string{"model-a", "model-b"}, service.modelSlugs)
}

func TestRadarServiceGetServiceHealthLocalizesFailureAndDeepClonesCache(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceStatusOpenAI] = radarServiceFailedMeta(now)
	clock := &radarServiceTestClock{now: now}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

	got, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, []ServiceKey{
		ServiceKeyClaudeAPI,
		ServiceKeyClaudeCode,
		ServiceKeyCodexWeb,
		ServiceKeyOpenAIAPI,
	}, radarServiceHealthKeys(got))
	require.Equal(t, ServiceStatusPartialOutage, got[0].Status)
	require.False(t, got[0].Stale)
	require.NotNil(t, got[0].LastIncident)
	require.Equal(t, "Elevated latency", got[0].LastIncident.Name)
	require.NotNil(t, got[0].LastIncident.ResolvedAt)
	require.Len(t, got[0].History30d, 30)
	require.NotEmpty(t, got[0].History30d[len(got[0].History30d)-1].Incidents)
	require.Equal(t, ServiceStatusOperational, got[1].Status)
	require.False(t, got[1].Stale)
	require.Equal(t, now, *got[0].LastUpdatedAt, "card freshness must use the latest successful collection, not component updated_at")
	require.Equal(t, now, *got[1].LastUpdatedAt)
	for _, card := range got[2:] {
		require.Equal(t, ServiceStatusUnknown, card.Status)
		require.True(t, card.Stale)
	}
	require.Equal(t, openAIStatuspagePublicURL, got[2].SourceURL)

	got[0].Name = "caller mutation"
	got[0].LastUpdatedAt = nil
	got[0].LastIncident.Name = "caller mutation"
	*got[0].LastIncident.ResolvedAt = time.Time{}
	got[0].History30d[len(got[0].History30d)-1].Incidents[0].Name = "caller history mutation"

	again, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Claude API", again[0].Name)
	require.NotNil(t, again[0].LastUpdatedAt)
	require.Equal(t, "Elevated latency", again[0].LastIncident.Name)
	require.False(t, again[0].LastIncident.ResolvedAt.IsZero())
	require.Equal(t, "Elevated latency", again[0].History30d[len(again[0].History30d)-1].Incidents[0].Name)
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusClaude))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusOpenAI))
	require.Equal(t, 1, repo.metaCallCount())
	require.Zero(t, repo.writeCalls)
}

func TestRadarServiceGetServiceHealthAlwaysReturnsFourUnknownCards(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	service := mustNewRadarServiceForTest(
		t,
		radarServiceTestConfig(),
		repo,
		&radarServiceTestClock{now: now},
	)

	got, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 4)
	for _, card := range got {
		require.Equal(t, ServiceStatusUnknown, card.Status)
		require.Equal(t, StatusIndicatorUnknown, card.StatusIndicator)
		require.True(t, card.Stale)
	}
	again, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Len(t, again, 4)
	require.Equal(t, 1, repo.metaCallCount())
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusClaude))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusOpenAI))
}

func TestRadarServiceGetServiceHealthIncludesOfficialPlatformsAndUsesMiniMaxChinaOnly(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
	repo.payloads[RadarSourceStatusWindsurf] = radarServicePlatformStatusPayload(now, "Windsurf", "Cascade", "operational")
	repo.payloads[RadarSourceStatusDeepSeek] = radarServicePlatformStatusPayload(now, "DeepSeek", "API 服务 (API Service)", "degraded_performance")
	repo.payloads[RadarSourceStatusKimi] = radarServicePlatformStatusPayload(now, "Kimi", "Open API", "partial_outage")
	repo.payloads[RadarSourceStatusMiniMaxGlobal] = radarServicePlatformStatusPayload(now, "MiniMax Global", "Large Language Models (LLM)", "major_outage")
	repo.payloads[RadarSourceStatusMiniMaxChina] = radarServicePlatformStatusPayload(now, "MiniMax China", "大语言模型LLM", "operational")
	for _, source := range statuspageRadarSources() {
		repo.metas[source] = radarServiceSuccessfulMeta(now)
	}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, []ServiceKey{
		ServiceKeyClaudeAPI, ServiceKeyClaudeCode, ServiceKeyCodexWeb, ServiceKeyOpenAIAPI,
		ServiceKeyWindsurf, ServiceKeyDeepSeek, ServiceKeyKimi, ServiceKeyMiniMax,
	}, radarServiceHealthKeys(got))
	require.Equal(t, ServiceStatusOperational, got[4].Status)
	require.Equal(t, ServiceStatusDegradedPerformance, got[5].Status)
	require.Equal(t, ServiceStatusPartialOutage, got[6].Status)
	require.Equal(t, ServiceStatusOperational, got[7].Status)
	require.Zero(t, repo.payloadCallCount(RadarSourceStatusMiniMaxGlobal), "the international source must not be read when the China source exists")
	for _, card := range got {
		require.False(t, card.Stale)
	}
}

func TestRadarServiceGetServiceHealthMissingComponentIsUnknownButSourceFresh(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayloadWithoutCode(now)
	repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, ServiceStatusPartialOutage, got[0].Status)
	require.False(t, got[0].Stale)
	require.Equal(t, ServiceStatusUnknown, got[1].Status)
	require.False(t, got[1].Stale)
}

func TestRadarServiceGetServiceHealthStaleness(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	tests := []struct {
		name       string
		payload    []byte
		meta       *SourceFetchMeta
		metaErr    error
		wantStale  bool
		wantStatus ServiceStatus
	}{
		{name: "fresh", payload: radarServiceClaudeStatusPayload(now), meta: radarServiceMetaPointer(radarServiceSuccessfulMeta(now)), wantStatus: ServiceStatusPartialOutage},
		{name: "missing meta", payload: radarServiceClaudeStatusPayload(now), wantStale: true, wantStatus: ServiceStatusPartialOutage},
		{name: "last success nil", payload: radarServiceClaudeStatusPayload(now), meta: radarServiceMetaPointer(SourceFetchMeta{LastAttemptAt: now}), wantStale: true, wantStatus: ServiceStatusPartialOutage},
		{name: "older than threshold", payload: radarServiceClaudeStatusPayload(now), meta: radarServiceMetaPointer(radarServiceSuccessfulMeta(now.Add(-61 * time.Minute))), wantStale: true, wantStatus: ServiceStatusPartialOutage},
		{name: "latest fetch failed", payload: radarServiceClaudeStatusPayload(now), meta: radarServiceMetaPointer(radarServiceFailedMeta(now)), wantStale: true, wantStatus: ServiceStatusPartialOutage},
		{name: "payload missing", meta: radarServiceMetaPointer(radarServiceSuccessfulMeta(now)), wantStale: true, wantStatus: ServiceStatusUnknown},
		{name: "payload corrupt", payload: []byte(`{"page":"secret raw payload"}`), meta: radarServiceMetaPointer(radarServiceSuccessfulMeta(now)), wantStale: true, wantStatus: ServiceStatusUnknown},
		{name: "meta read failed", payload: radarServiceClaudeStatusPayload(now), metaErr: errors.New("redis secret.internal"), wantStale: true, wantStatus: ServiceStatusPartialOutage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			if test.payload != nil {
				repo.payloads[RadarSourceStatusClaude] = test.payload
			}
			if test.meta != nil {
				repo.metas[RadarSourceStatusClaude] = *test.meta
			}
			repo.metaErr = test.metaErr
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			got, err := service.GetServiceHealth(context.Background())
			require.NoError(t, err)
			require.Equal(t, test.wantStatus, got[0].Status)
			require.Equal(t, test.wantStale, got[0].Stale)
		})
	}
}

func TestRadarServiceGetServiceHealthPartialFailuresAreNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	for _, fault := range []string{"meta operational", "payload operational", "corrupt payload"} {
		t.Run(fault, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
			repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
			repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
			repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
			switch fault {
			case "meta operational":
				repo.metaErr = errors.New("redis metadata secret")
			case "payload operational":
				repo.payloadErrs[RadarSourceStatusClaude] = errors.New("redis payload secret")
			case "corrupt payload":
				repo.payloads[RadarSourceStatusClaude] = []byte(`{"page":"corrupt secret"}`)
			}
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			first, err := service.GetServiceHealth(context.Background())
			require.NoError(t, err)
			require.Equal(t, ServiceStatusOperational, first[2].Status)
			require.Equal(t, ServiceStatusDegradedPerformance, first[3].Status)
			if fault == "meta operational" {
				require.Equal(t, ServiceStatusPartialOutage, first[0].Status)
				require.True(t, first[0].Stale)
			} else {
				require.Equal(t, ServiceStatusUnknown, first[0].Status)
				require.True(t, first[0].Stale)
			}

			repo.mu.Lock()
			repo.metaErr = nil
			repo.payloadErrs[RadarSourceStatusClaude] = nil
			repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
			repo.mu.Unlock()

			recovered, err := service.GetServiceHealth(context.Background())
			require.NoError(t, err)
			require.Equal(t, ServiceStatusPartialOutage, recovered[0].Status)
			require.False(t, recovered[0].Stale)
			require.Equal(t, ServiceStatusOperational, recovered[2].Status)
			require.False(t, recovered[2].Stale)
			require.Equal(t, 2, repo.metaCallCount())
			require.Equal(t, 2, repo.payloadCallCount(RadarSourceStatusClaude))
			require.Equal(t, 2, repo.payloadCallCount(RadarSourceStatusOpenAI))
		})
	}
}

func TestRadarServiceMemoryCacheExpiresAtTTLBoundary(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	clock := &radarServiceTestClock{now: now}
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
	repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

	_, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	clock.Advance(59 * time.Second)
	_, err = service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.metaCallCount())

	clock.Advance(time.Second)
	_, err = service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, repo.metaCallCount())
}

func TestRadarServiceSingleflightCoalescesColdAndExpiredLoads(t *testing.T) {
	const callers = 24
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"cold", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			clock := &radarServiceTestClock{now: now}
			repo := newRadarServiceTestRepo()
			repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
			repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
			repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
			repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
			joined := make(chan struct{}, callers+1)
			service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
				clock:    clock,
				cacheTTL: time.Minute,
				afterFlightJoin: func(radarServiceCacheKey) {
					joined <- struct{}{}
				},
			})
			require.NoError(t, err)

			if scenario == "expired" {
				_, err := service.GetServiceHealth(context.Background())
				require.NoError(t, err)
				<-joined
				clock.Advance(time.Minute)
			}
			metaCallsBefore := repo.metaCallCount()
			claudeCallsBefore := repo.payloadCallCount(RadarSourceStatusClaude)
			openAICallsBefore := repo.payloadCallCount(RadarSourceStatusOpenAI)

			loaderStarted := make(chan struct{})
			releaseLoader := make(chan struct{})
			var blockOnce sync.Once
			repo.mu.Lock()
			repo.payloadHook = func(_ context.Context, source RadarSourceKey) {
				if source != RadarSourceStatusClaude {
					return
				}
				blockOnce.Do(func() {
					close(loaderStarted)
					<-releaseLoader
				})
			}
			repo.mu.Unlock()

			results := make([][]ServiceHealthDTO, callers)
			errorsByCaller := make([]error, callers)
			start := make(chan struct{})
			var wait sync.WaitGroup
			for index := 0; index < callers; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					<-start
					results[index], errorsByCaller[index] = service.GetServiceHealth(context.Background())
				}(index)
			}
			close(start)
			for index := 0; index < callers; index++ {
				<-joined
			}
			<-loaderStarted
			close(releaseLoader)
			wait.Wait()

			for index := range errorsByCaller {
				require.NoError(t, errorsByCaller[index])
				require.Len(t, results[index], 4)
			}
			require.Equal(t, 1, repo.metaCallCount()-metaCallsBefore)
			require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusClaude)-claudeCallsBefore)
			require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusOpenAI)-openAICallsBefore)

			results[0][0].Name = "caller mutation"
			results[0][0].LastIncident.Name = "caller mutation"
			results[0][0].History30d[len(results[0][0].History30d)-1].Incidents[0].Name = "caller history mutation"
			require.Equal(t, "Claude API", results[1][0].Name)
			require.Equal(t, "Elevated latency", results[1][0].LastIncident.Name)
			require.Equal(t, "Elevated latency", results[1][0].History30d[len(results[1][0].History30d)-1].Incidents[0].Name)
		})
	}
}

func TestRadarServiceLeaderCancellationDoesNotCancelSharedLoad(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	clock := &radarServiceTestClock{now: now}
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
	repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
	joined := make(chan struct{}, 3)
	loadCompleted := make(chan struct{})
	releaseCompletedLoad := make(chan struct{})
	var blockOnce sync.Once
	service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
		clock:    clock,
		cacheTTL: time.Minute,
		afterFlightJoin: func(radarServiceCacheKey) {
			joined <- struct{}{}
		},
		afterLoad: func(radarServiceCacheKey) {
			blockOnce.Do(func() {
				close(loadCompleted)
				<-releaseCompletedLoad
			})
		},
	})
	require.NoError(t, err)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := service.GetServiceHealth(leaderCtx)
		leaderErr <- err
	}()
	<-loadCompleted
	<-joined

	type healthResult struct {
		cards []ServiceHealthDTO
		err   error
	}
	waiterResult := make(chan healthResult, 1)
	go func() {
		cards, err := service.GetServiceHealth(context.Background())
		waiterResult <- healthResult{cards: cards, err: err}
	}()
	<-joined
	cancelLeader()
	close(releaseCompletedLoad)
	require.ErrorIs(t, <-leaderErr, context.Canceled)
	waiter := <-waiterResult
	require.NoError(t, waiter.err)
	require.Len(t, waiter.cards, 4)

	waiter.cards[0].Name = "caller mutation"
	waiter.cards[0].LastIncident.Name = "caller mutation"
	cached, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Claude API", cached[0].Name)
	require.Equal(t, "Elevated latency", cached[0].LastIncident.Name)
	require.Equal(t, 1, repo.metaCallCount())
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusClaude))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusOpenAI))
}

func TestRadarServiceNonLeaderCancellationIsIndependent(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	clock := &radarServiceTestClock{now: now}
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
	repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
	joined := make(chan struct{}, 3)
	loadCompleted := make(chan struct{})
	releaseCompletedLoad := make(chan struct{})
	var blockOnce sync.Once
	service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
		clock:    clock,
		cacheTTL: time.Minute,
		afterFlightJoin: func(radarServiceCacheKey) {
			joined <- struct{}{}
		},
		afterLoad: func(radarServiceCacheKey) {
			blockOnce.Do(func() {
				close(loadCompleted)
				<-releaseCompletedLoad
			})
		},
	})
	require.NoError(t, err)

	leaderResult := make(chan error, 1)
	go func() {
		_, err := service.GetServiceHealth(context.Background())
		leaderResult <- err
	}()
	<-loadCompleted
	<-joined

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterResult := make(chan error, 1)
	go func() {
		_, err := service.GetServiceHealth(waiterCtx)
		waiterResult <- err
	}()
	<-joined
	cancelWaiter()
	require.ErrorIs(t, <-waiterResult, context.Canceled)
	close(releaseCompletedLoad)
	require.NoError(t, <-leaderResult)

	cached, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Len(t, cached, 4)
	require.Equal(t, 1, repo.metaCallCount())
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusClaude))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceStatusOpenAI))
}

func TestRadarServiceAllCanceledCallersLeaveOnlyBoundedSharedLoad(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	loadStarted := make(chan struct{})
	loadStopped := make(chan error, 1)
	var startOnce sync.Once
	repo.metaHook = func(ctx context.Context) {
		startOnce.Do(func() { close(loadStarted) })
		<-ctx.Done()
		loadStopped <- ctx.Err()
	}
	joined := make(chan struct{}, 2)
	service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
		clock:       &radarServiceTestClock{now: now},
		cacheTTL:    time.Minute,
		loadTimeout: 50 * time.Millisecond,
		afterFlightJoin: func(radarServiceCacheKey) {
			joined <- struct{}{}
		},
	})
	require.NoError(t, err)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	waiterErr := make(chan error, 1)
	go func() {
		_, err := service.GetServiceHealth(leaderCtx)
		leaderErr <- err
	}()
	<-loadStarted
	<-joined
	go func() {
		_, err := service.GetServiceHealth(waiterCtx)
		waiterErr <- err
	}()
	<-joined
	cancelLeader()
	cancelWaiter()
	require.ErrorIs(t, <-leaderErr, context.Canceled)
	require.ErrorIs(t, <-waiterErr, context.Canceled)

	select {
	case loadErr := <-loadStopped:
		require.ErrorIs(t, loadErr, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("bounded shared load did not stop")
	}
}

func TestRadarServiceSharedRepositoryDeadlinePropagates(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.metaHook = func(ctx context.Context) {
		<-ctx.Done()
	}
	service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
		clock:       &radarServiceTestClock{now: now},
		cacheTTL:    time.Minute,
		loadTimeout: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	_, err = service.GetServiceHealth(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, repo.metaCallCount())
}

func TestRadarServiceFreshnessDeadlineExpiresCacheBeforeTTL(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)

	t.Run("health", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
		repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
		lastSuccess := now.Add(-60*time.Minute + time.Second)
		repo.metas[RadarSourceStatusClaude] = radarServiceSuccessfulMeta(lastSuccess)
		repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(lastSuccess)
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetServiceHealth(context.Background())
		require.NoError(t, err)
		require.False(t, fresh[0].Stale)
		clock.Advance(time.Second)
		stale, err := service.GetServiceHealth(context.Background())
		require.NoError(t, err)
		require.True(t, stale[0].Stale)
		require.Equal(t, 2, repo.metaCallCount())
	})

	t.Run("degradation latest", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
		repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
		repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now.Add(-12*time.Hour + time.Second))
		repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now.Add(-48*time.Hour + time.Second))
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetDegradationLatest(context.Background())
		require.NoError(t, err)
		require.False(t, fresh.Stale)
		clock.Advance(time.Second)
		stale, err := service.GetDegradationLatest(context.Background())
		require.NoError(t, err)
		require.True(t, stale.Stale)
		require.Equal(t, 2, repo.metaCallCount())
	})

	t.Run("degradation trend", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		source, err := RadarAAPerformanceSource("model-a")
		require.NoError(t, err)
		repo.payloads[source] = radarServiceAAPerformancePayload("model-a")
		repo.metas[source] = radarServiceSuccessfulMeta(now.Add(-48*time.Hour + time.Second))
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
		require.NoError(t, err)
		require.False(t, fresh.Stale)
		clock.Advance(time.Second)
		stale, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
		require.NoError(t, err)
		require.True(t, stale.Stale)
		require.Equal(t, 2, repo.metaCallCount())
	})

	t.Run("lmarena", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
		repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now.Add(-48*time.Hour + time.Second))
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetLMArena(context.Background())
		require.NoError(t, err)
		require.False(t, fresh.Stale)
		clock.Advance(time.Second)
		stale, err := service.GetLMArena(context.Background())
		require.NoError(t, err)
		require.True(t, stale.Stale)
		require.Equal(t, 2, repo.metaCallCount())
	})

	t.Run("data sources", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now.Add(-48*time.Hour + time.Second))
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetDataSources(context.Background())
		require.NoError(t, err)
		require.False(t, fresh[3].Stale)
		require.True(t, fresh[3].IsHealthy)
		clock.Advance(time.Second)
		stale, err := service.GetDataSources(context.Background())
		require.NoError(t, err)
		require.True(t, stale[3].Stale)
		require.False(t, stale[3].IsHealthy)
		require.Equal(t, 2, repo.metaCallCount())
	})

	t.Run("quota aggregator source", func(t *testing.T) {
		clock := &radarServiceTestClock{now: now}
		repo := newRadarServiceTestRepo()
		lastSuccess := now.Add(-30*time.Minute + time.Second)
		repo.metrics = RadarMetricsSnapshot{
			AggregatorLastAttemptAt: lastSuccess,
			AggregatorLastRunAt:     lastSuccess,
			AggregatorLastSuccessAt: lastSuccess,
			AggregatorStateValid:    true,
		}
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, clock)

		fresh, err := service.GetDataSources(context.Background())
		require.NoError(t, err)
		require.False(t, fresh[len(fresh)-1].Stale)
		require.True(t, fresh[len(fresh)-1].IsHealthy)
		clock.Advance(time.Second)
		stale, err := service.GetDataSources(context.Background())
		require.NoError(t, err)
		require.True(t, stale[len(stale)-1].Stale)
		require.False(t, stale[len(stale)-1].IsHealthy)
	})
}

func TestRadarServiceContextCancellationStopsReads(t *testing.T) {
	repo := newRadarServiceTestRepo()
	service := mustNewRadarServiceForTest(
		t,
		radarServiceTestConfig(),
		repo,
		&radarServiceTestClock{now: time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.GetServiceHealth(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.GetDegradationLatest(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.GetDegradationTrend(ctx, "model-a", DegradationMetricIntelligenceIndex, 7)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.GetLMArena(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.GetDataSources(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, repo.metaCallCount())
	require.Zero(t, repo.payloadCallCount(RadarSourceStatusClaude))
}

func TestRadarServiceGetDegradationLatestCombinesSourcesAndDeepClones(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
	repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 6)
	repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	require.False(t, got.Stale)
	require.Equal(t, []string{"model-a", "model-b"}, radarServiceModelSlugs(got.Models))
	require.Len(t, got.LMArenaTop5, 5)
	require.Equal(t, 1, got.LMArenaTop5[0].Rank)
	require.NotNil(t, got.Models)
	require.NotNil(t, got.LMArenaTop5)
	require.Equal(t, []string{"aa", "lmarena"}, radarServiceSortedMapKeys(got.SourcesLastUpdated))
	require.NotNil(t, got.SourcesLastUpdated["aa"])
	require.NotNil(t, got.SourcesLastUpdated["lmarena"])
	require.True(t, got.TrendAvailable)

	*got.Models[0].IntelligenceIndex = 0
	*got.Models[0].LastUpdatedAt = time.Time{}
	*got.LMArenaTop5[0].Vendor = "caller mutation"
	*got.LMArenaTop5[0].Elo = 0
	*got.SourcesLastUpdated["aa"] = time.Time{}
	got.Models = append(got.Models, DegradationModelDTO{Slug: "caller"})
	got.SourcesLastUpdated["extra"] = &now

	again, err := service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	require.Len(t, again.Models, 2)
	require.NotZero(t, *again.Models[0].IntelligenceIndex)
	require.False(t, again.Models[0].LastUpdatedAt.IsZero())
	require.NotEqual(t, "caller mutation", *again.LMArenaTop5[0].Vendor)
	require.NotZero(t, *again.LMArenaTop5[0].Elo)
	require.False(t, again.SourcesLastUpdated["aa"].IsZero())
	require.NotContains(t, again.SourcesLastUpdated, "extra")
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceAA))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceLMArena))
	require.Equal(t, 1, repo.metaCallCount())
}

func TestRadarServiceGetDegradationLatestAutomaticallySelectsOverviewWithoutTrendFetchers(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	cfg := radarServiceTestConfig()
	cfg.Radar.ArtificialAnalysisModelSlugs = nil
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
	repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 1)
	repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, cfg, repo, &radarServiceTestClock{now: now})

	got, err := service.GetDegradationLatest(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b", "model-c"}, radarServiceModelSlugs(got.Models))
	require.False(t, got.TrendAvailable, "automatic overview models have no statically scheduled performance fetchers")
}

func TestRadarServiceGetDegradationLatestLocallyDegradesEachSource(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		aaPayload     []byte
		arenaPayload  []byte
		wantModels    int
		wantArenaTop5 int
	}{
		{name: "aa only", aaPayload: radarServiceAAModelsPayload(now), wantModels: 2},
		{name: "lmarena only", arenaPayload: radarServiceLMArenaPayload(now, 6), wantArenaTop5: 5},
		{name: "both missing"},
		{name: "aa corrupt", aaPayload: []byte(`{"data":"raw-secret"}`), arenaPayload: radarServiceLMArenaPayload(now, 6), wantArenaTop5: 5},
		{name: "lmarena corrupt", aaPayload: radarServiceAAModelsPayload(now), arenaPayload: []byte(`{"meta":"raw-secret"}`), wantModels: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			if test.aaPayload != nil {
				repo.payloads[RadarSourceAA] = test.aaPayload
			}
			if test.arenaPayload != nil {
				repo.payloads[RadarSourceLMArena] = test.arenaPayload
			}
			repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
			repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			got, err := service.GetDegradationLatest(context.Background())
			require.NoError(t, err)
			require.NotNil(t, got.Models)
			require.NotNil(t, got.LMArenaTop5)
			require.Len(t, got.Models, test.wantModels)
			require.Len(t, got.LMArenaTop5, test.wantArenaTop5)
			require.True(t, got.Stale)
			require.Contains(t, got.SourcesLastUpdated, "aa")
			require.Contains(t, got.SourcesLastUpdated, "lmarena")
			if test.name == "both missing" {
				again, err := service.GetDegradationLatest(context.Background())
				require.NoError(t, err)
				require.NotNil(t, again.Models)
				require.NotNil(t, again.LMArenaTop5)
				require.Equal(t, 1, repo.metaCallCount())
				require.Equal(t, 1, repo.payloadCallCount(RadarSourceAA))
				require.Equal(t, 1, repo.payloadCallCount(RadarSourceLMArena))
			}
		})
	}
}

func TestRadarServiceGetDegradationLatestRetainsPayloadAfterLatestFetchFailure(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
	repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
	repo.metas[RadarSourceAA] = radarServiceFailedMeta(now)
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Models, 2)
	require.Len(t, got.LMArenaTop5, 2)
	require.True(t, got.Stale)
	require.Equal(t, repo.metas[RadarSourceAA].LastSuccessAt.UTC(), got.SourcesLastUpdated["aa"].UTC())
}

func TestRadarServiceGetDegradationLatestPartialFailuresAreNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	for _, fault := range []string{"meta operational", "payload operational", "corrupt payload"} {
		t.Run(fault, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
			repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
			repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
			repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
			switch fault {
			case "meta operational":
				repo.metaErr = errors.New("redis metadata secret")
			case "payload operational":
				repo.payloadErrs[RadarSourceAA] = errors.New("redis payload secret")
			case "corrupt payload":
				repo.payloads[RadarSourceAA] = []byte(`{"data":"corrupt secret"}`)
			}
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			first, err := service.GetDegradationLatest(context.Background())
			require.NoError(t, err)
			require.Len(t, first.LMArenaTop5, 2)
			require.True(t, first.Stale)
			if fault == "meta operational" {
				require.Len(t, first.Models, 2)
			} else {
				require.Empty(t, first.Models)
			}

			repo.mu.Lock()
			repo.metaErr = nil
			repo.payloadErrs[RadarSourceAA] = nil
			repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
			repo.mu.Unlock()

			recovered, err := service.GetDegradationLatest(context.Background())
			require.NoError(t, err)
			require.Len(t, recovered.Models, 2)
			require.Len(t, recovered.LMArenaTop5, 2)
			require.False(t, recovered.Stale)
			require.Equal(t, 2, repo.metaCallCount())
			require.Equal(t, 2, repo.payloadCallCount(RadarSourceAA))
			require.Equal(t, 2, repo.payloadCallCount(RadarSourceLMArena))
		})
	}
}

func TestRadarServiceGetDegradationTrendMapsAllMetricsAndCalendarWindow(t *testing.T) {
	now := time.Date(2026, 7, 13, 23, 45, 0, 0, time.UTC)
	tests := []struct {
		metric    DegradationMetric
		wantDates []string
		want      []float64
	}{
		{metric: DegradationMetricIntelligenceIndex, wantDates: []string{"2026-07-11", "2026-07-12", "2026-07-13"}, want: []float64{81, 82, 83}},
		{metric: DegradationMetricCodingIndex, wantDates: []string{"2026-07-11", "2026-07-13"}, want: []float64{71, 73}},
		{metric: DegradationMetricAgenticIndex, wantDates: []string{"2026-07-11", "2026-07-12", "2026-07-13"}, want: []float64{61, 62, 63}},
	}

	for _, test := range tests {
		t.Run(string(test.metric), func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			source, err := RadarAAPerformanceSource("model-a")
			require.NoError(t, err)
			repo.payloads[source] = radarServiceAAPerformancePayload("model-a")
			repo.metas[source] = radarServiceSuccessfulMeta(now)
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			got, err := service.GetDegradationTrend(context.Background(), " model-a ", test.metric, 3)
			require.NoError(t, err)
			require.Equal(t, "model-a", got.ModelSlug)
			require.Equal(t, test.metric, got.Metric)
			require.Equal(t, 3, got.Days)
			require.False(t, got.Stale)
			require.Equal(t, test.wantDates, radarServiceMetricDates(got.DataPoints))
			require.Equal(t, test.want, radarServiceMetricValues(got.DataPoints))
		})
	}
}

func TestRadarServiceGetDegradationTrendMissIsEmptyAndStale(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	service := mustNewRadarServiceForTest(
		t,
		radarServiceTestConfig(),
		newRadarServiceTestRepo(),
		&radarServiceTestClock{now: now},
	)

	got, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 90)
	require.NoError(t, err)
	require.Equal(t, "model-a", got.ModelSlug)
	require.Equal(t, DegradationMetricIntelligenceIndex, got.Metric)
	require.Equal(t, 90, got.Days)
	require.NotNil(t, got.DataPoints)
	require.Empty(t, got.DataPoints)
	require.True(t, got.Stale)
}

func TestRadarServiceGetDegradationTrendValidatesBeforeRepositoryRead(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		model  string
		metric DegradationMetric
		days   int
	}{
		{name: "unconfigured model", model: "unconfigured-super-secret", metric: DegradationMetricIntelligenceIndex, days: 7},
		{name: "unsafe model", model: "../model-a", metric: DegradationMetricIntelligenceIndex, days: 7},
		{name: "invalid metric", model: "model-a", metric: DegradationMetric("raw-secret-metric"), days: 7},
		{name: "zero days", model: "model-a", metric: DegradationMetricIntelligenceIndex, days: 0},
		{name: "too many days", model: "model-a", metric: DegradationMetricIntelligenceIndex, days: 91},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			_, err := service.GetDegradationTrend(context.Background(), test.model, test.metric, test.days)
			require.ErrorIs(t, err, ErrInvalidRadarQuery)
			require.Equal(t, ErrInvalidRadarQuery.Error(), err.Error())
			require.NotContains(t, err.Error(), "secret")
			require.Zero(t, repo.metaCallCount())
		})
	}
}

func TestRadarServiceGetDegradationTrendOperationalAndCorruptErrorsAreSafeAndNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	source, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	tests := []struct {
		name       string
		payload    []byte
		payloadErr error
		metaErr    error
	}{
		{name: "payload operational", payloadErr: errors.New("redis://user:password@secret.internal/key/model-a")},
		{name: "metadata operational", payload: radarServiceAAPerformancePayload("model-a"), metaErr: errors.New("redis raw secret")},
		{name: "corrupt payload", payload: []byte(`{"model_slug":"model-a","data_points":"raw-secret"}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			if test.payload != nil {
				repo.payloads[source] = test.payload
			}
			repo.payloadErrs[source] = test.payloadErr
			repo.metaErr = test.metaErr
			repo.metas[source] = radarServiceSuccessfulMeta(now)
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			_, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
			require.ErrorIs(t, err, ErrRadarUnavailable)
			require.Equal(t, ErrRadarUnavailable.Error(), err.Error())
			require.NotContains(t, err.Error(), "model-a")
			require.NotContains(t, err.Error(), "secret")

			repo.mu.Lock()
			repo.payloadErrs[source] = nil
			repo.metaErr = nil
			repo.payloads[source] = radarServiceAAPerformancePayload("model-a")
			repo.mu.Unlock()
			got, retryErr := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
			require.NoError(t, retryErr)
			require.NotEmpty(t, got.DataPoints)
		})
	}
}

func TestRadarServiceCacheKeyIncludesEveryTrendParameter(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	sourceA, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	sourceB, err := RadarAAPerformanceSource("model-b")
	require.NoError(t, err)
	repo.payloads[sourceA] = radarServiceAAPerformancePayload("model-a")
	repo.payloads[sourceB] = radarServiceAAPerformancePayload("model-b")
	repo.metas[sourceA] = radarServiceSuccessfulMeta(now)
	repo.metas[sourceB] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	queries := []struct {
		model  string
		metric DegradationMetric
		days   int
	}{
		{model: "model-a", metric: DegradationMetricIntelligenceIndex, days: 7},
		{model: " model-a ", metric: DegradationMetricIntelligenceIndex, days: 7},
		{model: "model-a", metric: DegradationMetricCodingIndex, days: 7},
		{model: "model-a", metric: DegradationMetricIntelligenceIndex, days: 8},
		{model: "model-b", metric: DegradationMetricIntelligenceIndex, days: 7},
	}
	for _, query := range queries {
		_, err := service.GetDegradationTrend(context.Background(), query.model, query.metric, query.days)
		require.NoError(t, err)
	}
	require.Equal(t, 3, repo.payloadCallCount(sourceA))
	require.Equal(t, 1, repo.payloadCallCount(sourceB))
	require.Equal(t, 4, repo.metaCallCount())
}

func TestRadarServiceGetLMArenaMissSuccessStaleAndDeepClone(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)

	t.Run("miss", func(t *testing.T) {
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), newRadarServiceTestRepo(), &radarServiceTestClock{now: now})
		got, err := service.GetLMArena(context.Background())
		require.NoError(t, err)
		require.NotNil(t, got.Leaderboard)
		require.Empty(t, got.Leaderboard)
		require.Nil(t, got.TotalVotes)
		require.Nil(t, got.LastUpdatedAt)
		require.True(t, got.Stale)
	})

	t.Run("success and latest failure stale", func(t *testing.T) {
		repo := newRadarServiceTestRepo()
		repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 3)
		repo.metas[RadarSourceLMArena] = radarServiceFailedMeta(now)
		service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

		got, err := service.GetLMArena(context.Background())
		require.NoError(t, err)
		require.Len(t, got.Leaderboard, 3)
		require.NotNil(t, got.TotalVotes)
		require.NotNil(t, got.LastUpdatedAt)
		require.True(t, got.Stale)
		*got.Leaderboard[0].Vendor = "caller mutation"
		*got.Leaderboard[0].Votes = 0
		*got.TotalVotes = 0
		*got.LastUpdatedAt = time.Time{}

		again, err := service.GetLMArena(context.Background())
		require.NoError(t, err)
		require.NotEqual(t, "caller mutation", *again.Leaderboard[0].Vendor)
		require.NotZero(t, *again.Leaderboard[0].Votes)
		require.NotZero(t, *again.TotalVotes)
		require.False(t, again.LastUpdatedAt.IsZero())
	})
}

func TestRadarServiceGetLMArenaOperationalAndCorruptErrorsAreSafeAndNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		payload    []byte
		payloadErr error
		metaErr    error
	}{
		{name: "payload operational", payloadErr: errors.New("redis secret key")},
		{name: "metadata operational", payload: radarServiceLMArenaPayload(now, 2), metaErr: errors.New("redis secret key")},
		{name: "corrupt", payload: []byte(`{"meta":"raw-secret"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRadarServiceTestRepo()
			repo.payloads[RadarSourceLMArena] = test.payload
			repo.payloadErrs[RadarSourceLMArena] = test.payloadErr
			repo.metaErr = test.metaErr
			repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
			service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

			_, err := service.GetLMArena(context.Background())
			require.ErrorIs(t, err, ErrRadarUnavailable)
			require.Equal(t, ErrRadarUnavailable.Error(), err.Error())
			require.NotContains(t, err.Error(), "secret")

			repo.mu.Lock()
			repo.payloadErrs[RadarSourceLMArena] = nil
			repo.metaErr = nil
			repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
			repo.mu.Unlock()
			got, retryErr := service.GetLMArena(context.Background())
			require.NoError(t, retryErr)
			require.Len(t, got.Leaderboard, 2)
		})
	}
}

func TestRadarServiceGetDataSourcesStableSafeStatesAndDeepClone(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repo := newRadarServiceTestRepo()
	sourceA, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
	repo.metas[sourceA] = radarServiceFailedMeta(now)
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now.Add(-49 * time.Hour))
	repo.metas[RadarSourceStatusOpenAI] = radarServiceSuccessfulMeta(now)
	unknownCode := DataSourceErrorCode("raw-secret-error")
	meta := repo.metas[sourceA]
	meta.Error = &unknownCode
	repo.metas[sourceA] = meta
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{
		"aa",
		"aa_perf:model-a",
		"aa_perf:model-b",
		"lmarena",
		"status_claude",
		"status_openai",
		"status_windsurf",
		"status_deepseek",
		"status_kimi",
		"status_minimax_china",
		"quota_aggregator",
	}, radarServiceSourceKeys(got))
	require.Equal(t, []string{
		"https://artificialanalysis.ai",
		"https://artificialanalysis.ai",
		"https://artificialanalysis.ai",
		"https://huggingface.co/datasets/lmarena-ai/leaderboard-dataset",
		"https://status.claude.com",
		"https://status.openai.com",
		"https://status.windsurf.com",
		"https://status.deepseek.com",
		"https://status.moonshot.cn",
		"https://status.minimaxi.com",
		"",
	}, radarServiceSourceURLs(got))
	require.Equal(t, []string{"6h", "24h", "24h", "24h", "30m", "30m", "30m", "30m", "30m", "30m", "15m"}, radarServiceSourceIntervals(got))

	require.Equal(t, DataSourceStateHealthy, got[0].State)
	require.True(t, got[0].IsHealthy)
	require.False(t, got[0].Stale)
	require.Equal(t, DataSourceStateFailed, got[1].State)
	require.False(t, got[1].IsHealthy)
	require.True(t, got[1].Stale)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *got[1].Error)
	require.Equal(t, DataSourceStateNeverAttempted, got[2].State)
	require.True(t, got[2].Stale)
	require.Equal(t, DataSourceStateHealthy, got[3].State)
	require.True(t, got[3].Stale)
	require.False(t, got[3].IsHealthy)
	require.Equal(t, DataSourceStateNeverAttempted, got[4].State)
	require.Equal(t, DataSourceStateHealthy, got[5].State)
	require.True(t, got[5].IsHealthy)
	require.Equal(t, "Sub2API Aggregated Usage", got[len(got)-1].Name)
	require.Equal(t, DataSourceStateNeverAttempted, got[len(got)-1].State)
	require.Nil(t, got[6].HTTPStatus)

	for _, source := range got {
		require.NotContains(t, source.URL, "fetch.secret")
		require.NotContains(t, source.URL, "never-public")
		require.NotContains(t, source.Name, "super-secret-aa-key")
	}
	require.Equal(t, time.UTC, got[0].LastAttemptAt.Location())
	require.Equal(t, time.UTC, got[0].LastSuccessAt.Location())

	*got[0].LastAttemptAt = time.Time{}
	*got[0].LastSuccessAt = time.Time{}
	*got[0].NextFireAt = time.Time{}
	*got[0].HTTPStatus = 0
	*got[1].Error = DataSourceErrorCodeUnauthorized
	again, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.False(t, again[0].LastAttemptAt.IsZero())
	require.False(t, again[0].LastSuccessAt.IsZero())
	require.False(t, again[0].NextFireAt.IsZero())
	require.NotZero(t, *again[0].HTTPStatus)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *again[1].Error)
	require.Zero(t, repo.writeCalls)
}

func TestRadarServiceGetDataSourcesAAWithoutKeyIsNotConfigured(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	cfg := radarServiceTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = ""
	repo := newRadarServiceTestRepo()
	performanceA, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	performanceB, err := RadarAAPerformanceSource("model-b")
	require.NoError(t, err)
	repo.metas[RadarSourceAA] = radarServiceFailedMeta(now)
	repo.metas[performanceA] = radarServiceSuccessfulMeta(now)
	repo.metas[performanceB] = radarServiceFailedMeta(now)
	service := mustNewRadarServiceForTest(t, cfg, repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	for _, source := range got[:3] {
		require.Equal(t, DataSourceStateNotConfigured, source.State)
		require.False(t, source.IsHealthy)
		require.False(t, source.Stale)
		require.Nil(t, source.LastAttemptAt)
		require.Nil(t, source.LastSuccessAt)
		require.Nil(t, source.NextFireAt)
		require.Nil(t, source.HTTPStatus)
		require.Nil(t, source.Error)
	}
	for _, source := range got[3:] {
		require.Equal(t, DataSourceStateNeverAttempted, source.State)
	}
	got[0].State = DataSourceStateHealthy
	got[0].Stale = true
	got[0].LastAttemptAt = &now
	again, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Equal(t, DataSourceStateNotConfigured, again[0].State)
	require.False(t, again[0].Stale)
	require.Nil(t, again[0].LastAttemptAt)
}

func TestRadarServiceDegradationLatestIgnoresAAWhenSourceIsNotConfigured(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	cfg := radarServiceTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = "   "
	repo := newRadarServiceTestRepo()
	performanceSource, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
	repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 2)
	repo.payloads[performanceSource] = radarServiceAAPerformancePayload("model-a")
	repo.metas[RadarSourceAA] = radarServiceSuccessfulMeta(now)
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
	repo.metas[performanceSource] = radarServiceSuccessfulMeta(now)
	service := mustNewRadarServiceForTest(t, cfg, repo, &radarServiceTestClock{now: now})

	latest, err := service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	require.NotNil(t, latest.Models)
	require.Empty(t, latest.Models)
	require.NotEmpty(t, latest.LMArenaTop5)
	require.Nil(t, latest.SourcesLastUpdated["aa"])
	require.NotNil(t, latest.SourcesLastUpdated["lmarena"])
	require.False(t, latest.Stale)
	require.Zero(t, repo.payloadCallCount(RadarSourceAA))
	require.Equal(t, 1, repo.payloadCallCount(RadarSourceLMArena))
	trend, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
	require.NoError(t, err)
	require.NotEmpty(t, trend.DataPoints)
	require.True(t, trend.Stale)
}

func TestRadarServiceGetDataSourcesOperationalErrorIsSafeAndNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.metaErr = errors.New("redis://user:password@secret.internal/radar:meta:sources")
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	_, err := service.GetDataSources(context.Background())
	require.ErrorIs(t, err, ErrRadarUnavailable)
	require.Equal(t, ErrRadarUnavailable.Error(), err.Error())
	require.NotContains(t, err.Error(), "secret")

	repo.mu.Lock()
	repo.metaErr = nil
	repo.mu.Unlock()
	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 11)
	require.Equal(t, 2, repo.metaCallCount())
}

func TestRadarServiceGetDataSourcesMapsQuotaAggregatorSharedState(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-10 * time.Minute)
	realScheduledNextFire := now.Add(2 * time.Minute)
	repo := newRadarServiceTestRepo()
	repo.metrics = RadarMetricsSnapshot{
		AggregatorLastAttemptAt: lastSuccess,
		AggregatorLastRunAt:     lastSuccess,
		AggregatorLastSuccessAt: lastSuccess,
		AggregatorNextFireAt:    realScheduledNextFire,
		AggregatorStateValid:    true,
	}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	aggregator := got[len(got)-1]
	require.Equal(t, "quota_aggregator", aggregator.Key)
	require.Empty(t, aggregator.URL)
	require.Equal(t, "15m", aggregator.Interval)
	require.Equal(t, lastSuccess, *aggregator.LastAttemptAt)
	require.Equal(t, lastSuccess, *aggregator.LastSuccessAt)
	require.Equal(t, realScheduledNextFire, *aggregator.NextFireAt, "manual attempts must not shift the real scheduler deadline")
	require.Nil(t, aggregator.HTTPStatus)
	require.Nil(t, aggregator.Error)
	require.Equal(t, DataSourceStateHealthy, aggregator.State)
	require.True(t, aggregator.IsHealthy)
	require.False(t, aggregator.Stale)
}

func TestRadarServiceGetDataSourcesMapsQuotaAggregatorFailureWithoutLeakingDetails(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-20 * time.Minute)
	lastAttempt := now.Add(-time.Minute)
	repo := newRadarServiceTestRepo()
	repo.metrics = RadarMetricsSnapshot{
		AggregatorLastAttemptAt: lastAttempt,
		AggregatorLastRunAt:     lastAttempt,
		AggregatorLastSuccessAt: lastSuccess,
		AggregatorStateValid:    true,
	}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	aggregator := got[len(got)-1]
	require.Equal(t, DataSourceStateFailed, aggregator.State)
	require.False(t, aggregator.IsHealthy)
	require.False(t, aggregator.Stale, "a failed attempt must not make the last successful snapshot stale early")
	require.Equal(t, DataSourceErrorCode("aggregation_error"), *aggregator.Error)
	require.Nil(t, aggregator.HTTPStatus)
	require.NotContains(t, fmt.Sprint(aggregator), "account")
	require.NotContains(t, fmt.Sprint(aggregator), "redis")
}

func TestRadarServiceGetDataSourcesQuotaAggregatorStateErrorIsSafeAndNotCached(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.metricsErr = errors.New("redis://user:password@secret.internal/radar:metrics:aggregator")
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 11)
	aggregator := got[len(got)-1]
	require.Equal(t, DataSourceStateFailed, aggregator.State)
	require.False(t, aggregator.IsHealthy)
	require.True(t, aggregator.Stale)
	require.Equal(t, DataSourceErrorCodeAggregation, *aggregator.Error)
	require.Nil(t, aggregator.LastAttemptAt)
	require.Nil(t, aggregator.LastSuccessAt)
	require.Nil(t, aggregator.NextFireAt)
	require.NotContains(t, fmt.Sprint(got), "secret")

	repo.mu.Lock()
	repo.metricsErr = nil
	repo.metrics = RadarMetricsSnapshot{
		AggregatorLastAttemptAt: now.Add(-time.Minute),
		AggregatorLastRunAt:     now,
		AggregatorLastSuccessAt: now,
		AggregatorStateValid:    true,
	}
	repo.mu.Unlock()
	got, err = service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Equal(t, DataSourceStateHealthy, got[len(got)-1].State)
	require.True(t, got[len(got)-1].IsHealthy)
	require.Equal(t, 2, repo.metaCallCount(), "failed aggregator state must not be cached")
}

func TestRadarServiceGetDataSourcesInvalidQuotaAggregatorStateDegradesOnlyItsRow(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.metas[RadarSourceLMArena] = radarServiceSuccessfulMeta(now)
	repo.metrics = RadarMetricsSnapshot{
		AggregatorLastAttemptAt: now,
		AggregatorLastRunAt:     now.Add(-time.Minute),
		AggregatorStateValid:    true,
	}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	got, err := service.GetDataSources(context.Background())
	require.NoError(t, err)
	require.Equal(t, DataSourceStateHealthy, got[3].State)
	require.Equal(t, DataSourceStateFailed, got[len(got)-1].State)
	require.True(t, got[len(got)-1].Stale)
	require.Equal(t, DataSourceErrorCodeAggregation, *got[len(got)-1].Error)
}

func TestRadarServiceGetDataSourcesQuotaAggregatorContextErrorRemainsTerminal(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	repo.metricsErr = context.DeadlineExceeded
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})

	_, err := service.GetDataSources(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRadarServiceConcurrentCachedResultsAreIndependent(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarServiceTestRepo()
	source, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	repo.payloads[RadarSourceStatusClaude] = radarServiceClaudeStatusPayload(now)
	repo.payloads[RadarSourceStatusOpenAI] = radarServiceOpenAIStatusPayload(now)
	repo.payloads[RadarSourceAA] = radarServiceAAModelsPayload(now)
	repo.payloads[RadarSourceLMArena] = radarServiceLMArenaPayload(now, 6)
	repo.payloads[source] = radarServiceAAPerformancePayload("model-a")
	for _, key := range []RadarSourceKey{
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
		RadarSourceAA,
		RadarSourceLMArena,
		source,
	} {
		repo.metas[key] = radarServiceSuccessfulMeta(now)
	}
	service := mustNewRadarServiceForTest(t, radarServiceTestConfig(), repo, &radarServiceTestClock{now: now})
	_, err = service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	_, err = service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	_, err = service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
	require.NoError(t, err)
	_, err = service.GetLMArena(context.Background())
	require.NoError(t, err)
	_, err = service.GetDataSources(context.Background())
	require.NoError(t, err)

	const goroutines = 24
	const iterations = 20
	errCh := make(chan error, goroutines)
	var wait sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				health, err := service.GetServiceHealth(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				health[0].Name = "mutated"
				health[0].LastIncident.Name = "mutated"
				health[0].History30d[len(health[0].History30d)-1].Incidents[0].Name = "mutated"

				latest, err := service.GetDegradationLatest(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				*latest.Models[0].IntelligenceIndex = 0
				latest.SourcesLastUpdated["aa"] = nil

				trend, err := service.GetDegradationTrend(context.Background(), "model-a", DegradationMetricIntelligenceIndex, 7)
				if err != nil {
					errCh <- err
					return
				}
				trend.DataPoints[0].Value = 0

				arena, err := service.GetLMArena(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				*arena.Leaderboard[0].Elo = 0

				sources, err := service.GetDataSources(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				*sources[0].LastSuccessAt = time.Time{}
			}
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	health, err := service.GetServiceHealth(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Claude API", health[0].Name)
	require.Equal(t, "Elevated latency", health[0].LastIncident.Name)
	require.Equal(t, "Elevated latency", health[0].History30d[len(health[0].History30d)-1].Incidents[0].Name)
	latest, err := service.GetDegradationLatest(context.Background())
	require.NoError(t, err)
	require.NotZero(t, *latest.Models[0].IntelligenceIndex)
	require.NotNil(t, latest.SourcesLastUpdated["aa"])
}

func mustNewRadarServiceForTest(
	t *testing.T,
	cfg *config.Config,
	repo RadarCacheRepository,
	clock radarServiceClock,
) *RadarService {
	t.Helper()
	service, err := newRadarService(cfg, repo, radarServiceOptions{
		clock:    clock,
		cacheTTL: time.Minute,
	})
	require.NoError(t, err)
	return service
}

func radarServiceTestConfig() *config.Config {
	return &config.Config{Radar: config.RadarConfig{
		Enabled:                                            true,
		QuotaAggregatorIntervalMin:                         15,
		QuotaHistoryRetentionDays:                          7,
		SampleSizeWarnBelow:                                3,
		PublicMinBucketAccounts:                            2,
		InferMinUtilization:                                5,
		InferMaxStdevRatio:                                 0.3,
		ArtificialAnalysisAPIKey:                           "super-secret-aa-key",
		ArtificialAnalysisModelSlugs:                       []string{"model-a", "model-b"},
		ExternalRequestTimeoutSeconds:                      10,
		ExternalResponseMaxBytes:                           10 * 1024 * 1024,
		ArtificialAnalysisModelsIntervalMinutes:            6 * 60,
		ArtificialAnalysisPerformanceIntervalMinutes:       24 * 60,
		LMArenaIntervalMinutes:                             24 * 60,
		StatuspageIntervalMinutes:                          30,
		SourceHardRetentionDays:                            7,
		QuotaStaleThresholdMinutes:                         30,
		HealthStaleThresholdMinutes:                        60,
		ArtificialAnalysisModelsStaleThresholdMinutes:      12 * 60,
		ArtificialAnalysisPerformanceStaleThresholdMinutes: 48 * 60,
		LMArenaStaleThresholdMinutes:                       48 * 60,
		LMArenaURL:                                         "https://datasets-server.huggingface.co/filter",
	}}
}

func radarServiceSuccessfulMeta(at time.Time) SourceFetchMeta {
	utc := at.UTC()
	status := 200
	next := utc.Add(time.Hour)
	return SourceFetchMeta{
		LastAttemptAt: utc,
		LastSuccessAt: &utc,
		NextFireAt:    &next,
		HTTPStatus:    &status,
	}
}

func radarServiceFailedMeta(at time.Time) SourceFetchMeta {
	meta := radarServiceSuccessfulMeta(at.Add(-time.Hour))
	meta.LastAttemptAt = at.UTC()
	code := DataSourceErrorCodeNetworkError
	status := 503
	meta.Error = &code
	meta.HTTPStatus = &status
	return meta
}

func radarServiceMetaPointer(meta SourceFetchMeta) *SourceFetchMeta {
	return &meta
}

func radarServiceHealthKeys(cards []ServiceHealthDTO) []ServiceKey {
	result := make([]ServiceKey, len(cards))
	for index := range cards {
		result[index] = cards[index].ServiceKey
	}
	return result
}

func radarServiceModelSlugs(models []DegradationModelDTO) []string {
	result := make([]string, len(models))
	for index := range models {
		result[index] = models[index].Slug
	}
	return result
}

func radarServiceSortedMapKeys(values map[string]*time.Time) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func radarServiceMetricDates(points []MetricPointDTO) []string {
	result := make([]string, len(points))
	for index := range points {
		result[index] = points[index].Date
	}
	return result
}

func radarServiceMetricValues(points []MetricPointDTO) []float64 {
	result := make([]float64, len(points))
	for index := range points {
		result[index] = points[index].Value
	}
	return result
}

func radarServiceSourceKeys(sources []DataSourceMetaDTO) []string {
	result := make([]string, len(sources))
	for index := range sources {
		result[index] = sources[index].Key
	}
	return result
}

func radarServiceSourceURLs(sources []DataSourceMetaDTO) []string {
	result := make([]string, len(sources))
	for index := range sources {
		result[index] = sources[index].URL
	}
	return result
}

func radarServiceSourceIntervals(sources []DataSourceMetaDTO) []string {
	result := make([]string, len(sources))
	for index := range sources {
		result[index] = sources[index].Interval
	}
	return result
}

func radarServiceAAModelsPayload(now time.Time) []byte {
	updated := now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(`{
		"data":[
			{"slug":"model-b","name":"Model B","creator":"Vendor B","released_at":"2026-01-02","intelligence_index":88,"coding_index":78,"agentic_index":68,"price_input_per_1m":2,"price_output_per_1m":8,"last_updated_at":%q},
			{"slug":"model-c","name":"Model C","creator":"Vendor C","released_at":"2026-01-03","intelligence_index":87,"coding_index":77,"agentic_index":67,"price_input_per_1m":3,"price_output_per_1m":9,"last_updated_at":%q},
			{"slug":"model-a","name":"Model A","creator":"Vendor A","released_at":"2026-01-01","intelligence_index":89,"coding_index":79,"agentic_index":69,"price_input_per_1m":1,"price_output_per_1m":7,"last_updated_at":%q}
		]
	}`, updated, updated, updated))
}

func radarServiceAAPerformancePayload(model string) []byte {
	return []byte(fmt.Sprintf(`{
		"model_slug":%q,
		"window":"90d",
		"interval":"daily",
		"data_points":[
			{"date":"2026-07-14","intelligence_index":84,"coding_index":74,"agentic_index":64},
			{"date":"2026-07-13","intelligence_index":83,"coding_index":73,"agentic_index":63},
			{"date":"2026-07-10","intelligence_index":80,"coding_index":70,"agentic_index":60},
			{"date":"2026-07-12","intelligence_index":82,"coding_index":null,"agentic_index":62},
			{"date":"2026-07-11","intelligence_index":81,"coding_index":71,"agentic_index":61}
		]
	}`, model))
}

func radarServiceLMArenaPayload(now time.Time, count int) []byte {
	models := make([]string, 0, count)
	for rank := count; rank >= 1; rank-- {
		models = append(models, fmt.Sprintf(
			`{"rank":%d,"model":"Arena Model %d","vendor":"Vendor %d","score":%d,"ci":2.5,"votes":%d}`,
			rank,
			rank,
			rank,
			1300-rank,
			1000+rank,
		))
	}
	return []byte(fmt.Sprintf(`{
		"meta":{"fetched_at":%q,"last_updated":%q,"model_count":%d},
		"models":[%s]
	}`,
		now.UTC().Format(time.RFC3339Nano),
		now.UTC().Format(lmarenaLastUpdatedLayout),
		count,
		strings.Join(models, ","),
	))
}

func radarServiceClaudeStatusPayload(now time.Time) []byte {
	created := now.AddDate(-1, 0, 0).UTC().Format(time.RFC3339Nano)
	updated := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano)
	incidentCreated := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	incidentResolved := now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(`{
		"page":{"id":"claude-page","name":"Claude Status","url":"https://untrusted.example","updated_at":%q},
		"status":{"indicator":"major","description":"Partial outage"},
		"components":[
			{"id":"claude-api","name":"Claude API","status":"partial_outage","created_at":%q,"updated_at":%q,"group":false},
			{"id":"claude-code","name":"Claude Code","status":"operational","created_at":%q,"updated_at":%q,"group":false}
		],
		"incidents":[{"id":"incident-1","name":"Elevated latency","status":"resolved","impact":"major","created_at":%q,"resolved_at":%q,"components":[{"id":"claude-api","name":"Claude API"}]}]
	}`, updated, created, updated, created, updated, incidentCreated, incidentResolved))
}

func radarServiceClaudeStatusPayloadWithoutCode(now time.Time) []byte {
	created := now.AddDate(-1, 0, 0).UTC().Format(time.RFC3339Nano)
	updated := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(`{
		"page":{"id":"claude-page","name":"Claude Status","url":"https://untrusted.example","updated_at":%q},
		"status":{"indicator":"major","description":"Partial outage"},
		"components":[
			{"id":"claude-api","name":"Claude API","status":"partial_outage","created_at":%q,"updated_at":%q,"group":false}
		],
		"incidents":[]
	}`, updated, created, updated))
}

func radarServiceOpenAIStatusPayload(now time.Time) []byte {
	created := now.AddDate(-1, 0, 0).UTC().Format(time.RFC3339Nano)
	updated := now.Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(`{
		"page":{"id":"openai-page","name":"OpenAI Status","url":"https://untrusted.example","updated_at":%q},
		"status":{"indicator":"none","description":"All operational"},
		"components":[
			{"id":"codex-web","name":"Codex Web","status":"operational","created_at":%q,"updated_at":%q,"group":false},
			{"id":"openai-api","name":"API","status":"degraded_performance","created_at":%q,"updated_at":%q,"group":false}
		],
		"incidents":[]
	}`, updated, created, updated, created, updated))
}

func radarServicePlatformStatusPayload(now time.Time, page, component, status string) []byte {
	created := now.AddDate(-1, 0, 0).UTC().Format(time.RFC3339Nano)
	updated := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(`{
		"page":{"id":"platform-page","name":%q,"url":"https://untrusted.example","updated_at":%q},
		"status":{"indicator":"none","description":"status"},
		"components":[{"id":"platform-component","name":%q,"status":%q,"created_at":%q,"updated_at":%q,"group":false}],
		"incidents":[]
	}`, page, updated, component, status, created, updated))
}
