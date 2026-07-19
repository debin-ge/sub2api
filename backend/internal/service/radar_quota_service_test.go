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

	"github.com/stretchr/testify/require"
)

type radarQuotaTrendCall struct {
	bucket string
	since  time.Time
}

type radarQuotaServiceTestRepo struct {
	mu sync.Mutex

	keys       []string
	listErr    error
	latest     map[string]*BucketSnapshotDTO
	latestErrs map[string]error
	trends     map[string][]BucketSnapshotDTO
	trendErrs  map[string]error

	listCalls   int
	latestCalls []string
	trendCalls  []radarQuotaTrendCall
	writeCalls  int

	latestHook func(context.Context, string)
	trendHook  func(context.Context, string, time.Time)
}

func newRadarQuotaServiceTestRepo() *radarQuotaServiceTestRepo {
	return &radarQuotaServiceTestRepo{
		latest:     make(map[string]*BucketSnapshotDTO),
		latestErrs: make(map[string]error),
		trends:     make(map[string][]BucketSnapshotDTO),
		trendErrs:  make(map[string]error),
	}
}

func (r *radarQuotaServiceTestRepo) AppendBucketSnapshot(context.Context, BucketSnapshotDTO) error {
	r.mu.Lock()
	r.writeCalls++
	r.mu.Unlock()
	return errors.New("unexpected write")
}

func (r *radarQuotaServiceTestRepo) ListBucketKeys(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	return append([]string(nil), r.keys...), r.listErr
}

func (r *radarQuotaServiceTestRepo) GetLatestBucket(ctx context.Context, bucket string) (*BucketSnapshotDTO, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.latestCalls = append(r.latestCalls, bucket)
	snapshot := r.latest[bucket]
	err := r.latestErrs[bucket]
	hook := r.latestHook
	r.mu.Unlock()
	if hook != nil {
		hook(ctx, bucket)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return snapshot, err
}

func (r *radarQuotaServiceTestRepo) GetBucketTrend(ctx context.Context, bucket string, since time.Time) ([]BucketSnapshotDTO, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.trendCalls = append(r.trendCalls, radarQuotaTrendCall{bucket: bucket, since: since})
	snapshots := r.trends[bucket]
	err := r.trendErrs[bucket]
	hook := r.trendHook
	r.mu.Unlock()
	if hook != nil {
		hook(ctx, bucket, since)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return snapshots, err
}

func (r *radarQuotaServiceTestRepo) SetSourcePayload(context.Context, RadarSourceKey, []byte, time.Duration) error {
	return errors.New("unexpected source write")
}

func (r *radarQuotaServiceTestRepo) GetSourcePayload(context.Context, RadarSourceKey) ([]byte, error) {
	return nil, errors.New("unexpected source read")
}

func (r *radarQuotaServiceTestRepo) CommitSourceSuccess(context.Context, RadarSourceKey, []byte, time.Duration, SourceFetchMeta) (bool, error) {
	return false, errors.New("unexpected source write")
}

func (r *radarQuotaServiceTestRepo) CommitSourceFailure(context.Context, RadarSourceKey, SourceFetchMeta) (bool, error) {
	return false, errors.New("unexpected source write")
}

func (r *radarQuotaServiceTestRepo) SetSourceMeta(context.Context, RadarSourceKey, SourceFetchMeta) error {
	return errors.New("unexpected source write")
}

func (r *radarQuotaServiceTestRepo) ListSourceMeta(context.Context) (map[RadarSourceKey]SourceFetchMeta, error) {
	return nil, errors.New("unexpected source read")
}

func (r *radarQuotaServiceTestRepo) GetRadarAggregatorState(ctx context.Context) (RadarMetricsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return RadarMetricsSnapshot{}, err
	}
	return RadarMetricsSnapshot{AggregatorStateValid: true}, nil
}

func (r *radarQuotaServiceTestRepo) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("unexpected write")
}

func (r *radarQuotaServiceTestRepo) ReleaseLock(context.Context, string, string) error {
	return errors.New("unexpected write")
}

func (r *radarQuotaServiceTestRepo) quotaCallCounts() (int, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls, len(r.latestCalls), len(r.trendCalls)
}

func (r *radarQuotaServiceTestRepo) snapshotTrendCalls() []radarQuotaTrendCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]radarQuotaTrendCall(nil), r.trendCalls...)
}

func TestRadarServiceGetQuotaBucketsLatestSortsFreshnessAndDeepClones(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	limit := 900.0
	stdev := 12.0
	reason := InferenceRejectReasonHighDispersion
	repo := newRadarQuotaServiceTestRepo()
	repo.keys = []string{"openai/pro", "anthropic/zeta", "anthropic/alpha"}
	repo.latest["openai/pro"] = radarQuotaSnapshot("openai/pro", "OpenAI Pro", now.Add(-time.Minute))
	repo.latest["anthropic/zeta"] = radarQuotaSnapshot("anthropic/zeta", "Claude Zeta", now.Add(-3*time.Minute))
	repo.latest["anthropic/alpha"] = radarQuotaSnapshot("anthropic/alpha", "Claude Alpha", now.Add(-2*time.Minute))
	repo.latest["anthropic/alpha"].FiveHour = &WindowStatsDTO{
		AvgUtilization:        42,
		InferredLimitUSD:      &limit,
		InferredStdev:         &stdev,
		InferenceRejectReason: &reason,
		ContributorsCount:     3,
	}
	repo.latest["anthropic/alpha"].SevenDay = &WindowStatsDTO{AvgUtilization: 21, InferredLimitUSD: &limit, ContributorsCount: 3}
	repo.latest["anthropic/alpha"].SevenDaySonnet = &ModelWindowStatsDTO{Model: "claude-sonnet", AvgUtilization: 31, SampleSize: 3}
	repo.latest["anthropic/alpha"].SevenDayFable = &ModelWindowStatsDTO{Model: "claude-fable", AvgUtilization: 11, SampleSize: 3}
	repo.latest["anthropic/alpha"].ModelBreakdown5h = []ModelCostBreakdownDTO{{Model: "other", AvgCost: 4, ContributorsCount: 3}}
	repo.latest["anthropic/alpha"].ModelBreakdown7d = []ModelCostBreakdownDTO{{Model: "other", AvgCost: 7, ContributorsCount: 3}}

	cfg := radarServiceTestConfig()
	cfg.Radar.SampleSizeWarnBelow = 4
	clock := &radarServiceTestClock{now: now}
	service, err := newRadarService(cfg, repo, radarServiceOptions{clock: clock, cacheTTL: time.Minute})
	require.NoError(t, err)
	cfg.Radar.SampleSizeWarnBelow = 99

	got, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"anthropic/alpha", "anthropic/zeta", "openai/pro"}, radarQuotaBucketKeys(got.Buckets))
	require.Equal(t, now.Add(-time.Minute), *got.LastAggregatedAt)
	require.Equal(t, 4, got.SampleSizeWarnBelow)
	require.False(t, got.Stale)
	for _, bucket := range got.Buckets {
		require.False(t, bucket.Stale)
	}

	got.Buckets[0].BucketKey = "caller/mutation"
	*got.Buckets[0].FiveHour.InferredLimitUSD = 0
	*got.Buckets[0].FiveHour.InferredStdev = 0
	*got.Buckets[0].FiveHour.InferenceRejectReason = InferenceRejectReasonInvalidMean
	got.Buckets[0].SevenDay.AvgUtilization = 0
	got.Buckets[0].SevenDaySonnet.Model = "caller"
	got.Buckets[0].SevenDayFable.Model = "caller"
	got.Buckets[0].ModelBreakdown5h[0].Model = "caller"
	got.Buckets[0].ModelBreakdown7d[0].Model = "caller"
	*got.LastAggregatedAt = time.Time{}

	again, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.Equal(t, "anthropic/alpha", again.Buckets[0].BucketKey)
	require.Equal(t, 900.0, *again.Buckets[0].FiveHour.InferredLimitUSD)
	require.Equal(t, 12.0, *again.Buckets[0].FiveHour.InferredStdev)
	require.Equal(t, InferenceRejectReasonHighDispersion, *again.Buckets[0].FiveHour.InferenceRejectReason)
	require.Equal(t, 21.0, again.Buckets[0].SevenDay.AvgUtilization)
	require.Equal(t, "claude-sonnet", again.Buckets[0].SevenDaySonnet.Model)
	require.Equal(t, "claude-fable", again.Buckets[0].SevenDayFable.Model)
	require.Equal(t, "other", again.Buckets[0].ModelBreakdown5h[0].Model)
	require.Equal(t, "other", again.Buckets[0].ModelBreakdown7d[0].Model)
	require.Equal(t, now.Add(-time.Minute), *again.LastAggregatedAt)
	listCalls, latestCalls, trendCalls := repo.quotaCallCounts()
	require.Equal(t, 1, listCalls)
	require.Equal(t, 3, latestCalls)
	require.Zero(t, trendCalls)
	require.Zero(t, repo.writeCalls)
}

func TestRadarServiceGetQuotaBucketsLatestEmptyMissAndSafeErrors(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)

	t.Run("empty", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

		got, err := service.GetQuotaBucketsLatest(context.Background())
		require.NoError(t, err)
		require.NotNil(t, got.Buckets)
		require.Empty(t, got.Buckets)
		require.Nil(t, got.LastAggregatedAt)
		require.True(t, got.Stale)
	})

	t.Run("list to latest miss race is skipped", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/gone", "openai/pro"}
		repo.latestErrs["anthropic/gone"] = ErrRadarCacheMiss
		repo.latest["openai/pro"] = radarQuotaSnapshot("openai/pro", "OpenAI Pro", now)
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

		got, err := service.GetQuotaBucketsLatest(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"openai/pro"}, radarQuotaBucketKeys(got.Buckets))
	})

	for _, tt := range []struct {
		name  string
		setup func(*radarQuotaServiceTestRepo)
	}{
		{name: "list operational error", setup: func(repo *radarQuotaServiceTestRepo) { repo.listErr = errors.New("redis secret detail") }},
		{name: "unsafe indexed key", setup: func(repo *radarQuotaServiceTestRepo) { repo.listErr = ErrInvalidRadarCacheKey }},
		{name: "corrupt latest", setup: func(repo *radarQuotaServiceTestRepo) {
			repo.keys = []string{"anthropic/pro"}
			repo.latestErrs["anthropic/pro"] = errors.New("stored secret payload is corrupt")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			tt.setup(repo)
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

			got, err := service.GetQuotaBucketsLatest(context.Background())
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrRadarUnavailable)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestRadarServiceQuotaReadDefenseSkipsInvalidSnapshotsLocally(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 0, 0, 0, time.UTC)

	t.Run("latest retains valid buckets and malformed-only is empty", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/bad", "openai/pro"}
		invalid := radarQuotaSnapshot("anthropic/bad", "bad", now)
		invalid.PrivacyThreshold = 0
		repo.latest["anthropic/bad"] = invalid
		repo.latest["openai/pro"] = radarQuotaSnapshot("openai/pro", "OpenAI Pro", now)
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)
		got, err := service.GetQuotaBucketsLatest(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"openai/pro"}, radarQuotaBucketKeys(got.Buckets))

		repoOnlyBad := newRadarQuotaServiceTestRepo()
		repoOnlyBad.keys = []string{"anthropic/bad"}
		repoOnlyBad.latest["anthropic/bad"] = invalid
		serviceOnlyBad := mustNewRadarQuotaServiceForTest(t, repoOnlyBad, &radarServiceTestClock{now: now}, time.Minute)
		empty, err := serviceOnlyBad.GetQuotaBucketsLatest(context.Background())
		require.NoError(t, err)
		require.Empty(t, empty.Buckets)
	})

	t.Run("latest skips bucket mismatch", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro", "openai/pro"}
		repo.latest["anthropic/pro"] = radarQuotaSnapshot("openai/pro", "mismatch", now)
		repo.latest["openai/pro"] = radarQuotaSnapshot("openai/pro", "OpenAI Pro", now)
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)
		got, err := service.GetQuotaBucketsLatest(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"openai/pro"}, radarQuotaBucketKeys(got.Buckets))
	})

	t.Run("trend skips invalid threshold mismatch and retains good point", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro"}
		invalid := radarQuotaSnapshot("anthropic/pro", "bad", now.Add(-3*time.Minute))
		invalid.PrivacyThreshold = 0
		mismatch := radarQuotaSnapshot("openai/pro", "mismatch", now.Add(-2*time.Minute))
		good := radarQuotaSnapshot("anthropic/pro", "good", now.Add(-time.Minute))
		repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*invalid, *mismatch, *good}
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)
		got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
		require.NoError(t, err)
		require.Len(t, got.DataPoints, 1)
		require.Equal(t, good.CapturedAt, got.DataPoints[0].Timestamp)
	})
}

func TestRadarServiceQuotaFreshnessBoundaryInvalidTimesAndCacheDeadline(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)

	for _, tt := range []struct {
		name       string
		capturedAt time.Time
	}{
		{name: "exact stale boundary", capturedAt: now.Add(-30 * time.Minute)},
		{name: "future timestamp", capturedAt: now.Add(time.Second)},
		{name: "zero timestamp", capturedAt: time.Time{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			repo.keys = []string{"anthropic/pro"}
			repo.latest["anthropic/pro"] = radarQuotaSnapshot("anthropic/pro", "Claude Pro", tt.capturedAt)
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, 5*time.Minute)

			got, err := service.GetQuotaBucketsLatest(context.Background())
			require.NoError(t, err)
			require.True(t, got.Stale)
			require.True(t, got.Buckets[0].Stale)
		})
	}

	repo := newRadarQuotaServiceTestRepo()
	repo.keys = []string{"anthropic/pro"}
	repo.latest["anthropic/pro"] = radarQuotaSnapshot("anthropic/pro", "Claude Pro", now.Add(-29*time.Minute))
	clock := &radarServiceTestClock{now: now}
	service := mustNewRadarQuotaServiceForTest(t, repo, clock, 5*time.Minute)

	fresh, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.False(t, fresh.Stale)
	clock.Advance(59 * time.Second)
	fresh, err = service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.False(t, fresh.Stale)
	listCalls, _, _ := repo.quotaCallCounts()
	require.Equal(t, 1, listCalls)

	clock.Advance(time.Second)
	stale, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.True(t, stale.Stale)
	listCalls, _, _ = repo.quotaCallCounts()
	require.Equal(t, 2, listCalls, "the memory cache must expire at the freshness deadline")
}

func TestRadarServiceGetQuotaBucketsTrendValidatesBeforeRepositoryRead(t *testing.T) {
	invalidBuckets := []string{
		"", "anthropic", "anthropic/", "/pro", "Anthropic/pro", "google/pro",
		"anthropic/PRO", "anthropic/pro tier", "anthropic/pro/../../lock",
		"anthropic/" + strings.Repeat("a", 65),
	}
	for _, bucket := range invalidBuckets {
		t.Run("bucket "+bucket, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: time.Now()}, time.Minute)

			got, err := service.GetQuotaBucketsTrend(context.Background(), bucket, 7)
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrInvalidRadarQuery)
			_, _, trendCalls := repo.quotaCallCounts()
			require.Zero(t, trendCalls)
		})
	}

	for _, days := range []int{-1, 0, 8, 90} {
		t.Run("days", func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: time.Now()}, time.Minute)

			got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", days)
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrInvalidRadarQuery)
			_, _, trendCalls := repo.quotaCallCounts()
			require.Zero(t, trendCalls)
		})
	}
}

func TestRadarServiceQuotaTrendUnknownBucketsDoNotGrowCacheOrReadTrends(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarQuotaServiceTestRepo()
	service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

	const bucketCount = 256
	for pass := range 2 {
		for index := range bucketCount {
			bucket := fmt.Sprintf("anthropic/tier%d", index)
			days := index%7 + 1
			got, err := service.GetQuotaBucketsTrend(context.Background(), bucket, days)
			require.NoError(t, err, "pass=%d bucket=%s", pass, bucket)
			require.Equal(t, bucket, got.BucketKey)
			require.Equal(t, days, got.Days)
			require.NotNil(t, got.DataPoints)
			require.Empty(t, got.DataPoints)
			require.True(t, got.Stale)
		}
	}

	listCalls, _, trendCalls := repo.quotaCallCounts()
	require.Equal(t, bucketCount*2, listCalls, "unknown buckets must be revalidated instead of cached")
	require.Zero(t, trendCalls, "unknown buckets must not select arbitrary Redis trend keys")
	service.cacheMu.RLock()
	cacheSize := len(service.cache)
	service.cacheMu.RUnlock()
	require.Zero(t, cacheSize, "attacker-controlled bucket/day combinations must not grow the cache")
}

func TestRadarServiceGetQuotaBucketsTrendMapsSortsUsesUTCAnchorAndDeepClones(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.July, 13, 16, 0, 0, 0, location)
	utcNow := now.UTC()
	limit5h := 500.0
	limit7d := 700.0
	repo := newRadarQuotaServiceTestRepo()
	repo.keys = []string{"anthropic/pro"}
	newer := radarQuotaSnapshot("anthropic/pro", "Claude Pro", utcNow.Add(-time.Minute))
	newer.FiveHour = &WindowStatsDTO{AvgUtilization: 40, AvgCost: 5, InferredLimitUSD: &limit5h, SampleSize: 4, ContributorsCount: 3}
	newer.SevenDay = &WindowStatsDTO{AvgUtilization: 60, AvgCost: 7, InferredLimitUSD: &limit7d, SampleSize: 6, ContributorsCount: 3}
	older := radarQuotaSnapshot("anthropic/pro", "Claude Pro", utcNow.Add(-time.Hour))
	older.FiveHour = &WindowStatsDTO{AvgUtilization: 20, AvgCost: 2, SampleSize: 2, ContributorsCount: 3}
	older.SevenDay = &WindowStatsDTO{AvgUtilization: 30, AvgCost: 3, SampleSize: 3, ContributorsCount: 3}
	repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*newer, *older}
	service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

	got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
	require.NoError(t, err)
	require.Equal(t, "anthropic/pro", got.BucketKey)
	require.Equal(t, 7, got.Days)
	require.False(t, got.Stale)
	require.Equal(t, []time.Time{older.CapturedAt, newer.CapturedAt}, radarQuotaTrendTimes(got.DataPoints))
	require.Equal(t, 40.0, got.DataPoints[1].FiveHour.AvgUtilization)
	require.Equal(t, 5.0, got.DataPoints[1].FiveHour.AvgCost)
	require.Equal(t, 500.0, *got.DataPoints[1].FiveHour.InferredLimitUSD)
	require.Equal(t, 4, got.DataPoints[1].FiveHour.SampleSize)
	require.Equal(t, 60.0, got.DataPoints[1].SevenDay.AvgUtilization)
	require.Equal(t, 7.0, got.DataPoints[1].SevenDay.AvgCost)
	require.Equal(t, 700.0, *got.DataPoints[1].SevenDay.InferredLimitUSD)
	require.Equal(t, 6, got.DataPoints[1].SevenDay.SampleSize)
	calls := repo.snapshotTrendCalls()
	require.Equal(t, []radarQuotaTrendCall{{
		bucket: "anthropic/pro",
		since:  utcNow.AddDate(0, 0, -7),
	}}, calls)

	got.DataPoints[0].Timestamp = time.Time{}
	got.DataPoints[1].FiveHour.AvgCost = 0
	*got.DataPoints[1].FiveHour.InferredLimitUSD = 0
	*got.DataPoints[1].SevenDay.InferredLimitUSD = 0
	again, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
	require.NoError(t, err)
	require.Equal(t, older.CapturedAt, again.DataPoints[0].Timestamp)
	require.Equal(t, 5.0, again.DataPoints[1].FiveHour.AvgCost)
	require.Equal(t, 500.0, *again.DataPoints[1].FiveHour.InferredLimitUSD)
	require.Equal(t, 700.0, *again.DataPoints[1].SevenDay.InferredLimitUSD)
	require.Len(t, repo.snapshotTrendCalls(), 1)
}

func TestRadarServiceGetQuotaBucketsTrendEmptyErrorsAndInvalidTimes(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)

	t.Run("empty", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro"}
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

		for range 2 {
			got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
			require.NoError(t, err)
			require.NotNil(t, got.DataPoints)
			require.Empty(t, got.DataPoints)
			require.True(t, got.Stale)
		}
		listCalls, _, trendCalls := repo.quotaCallCounts()
		require.Equal(t, 2, listCalls, "an indexed bucket with no trend must not be cached")
		require.Equal(t, 2, trendCalls, "an indexed bucket with no trend must be re-read")
		require.Zero(t, radarQuotaServiceCacheSize(service))
	})

	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "operational", err: errors.New("redis secret detail")},
		{name: "corrupt", err: ErrInvalidRadarCacheKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			repo.keys = []string{"anthropic/pro"}
			repo.trendErrs["anthropic/pro"] = tt.err
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

			got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrRadarUnavailable)
			require.NotContains(t, err.Error(), "secret")
		})
	}

	for _, tt := range []struct {
		name       string
		capturedAt time.Time
	}{
		{name: "stale boundary", capturedAt: now.Add(-30 * time.Minute)},
		{name: "future", capturedAt: now.Add(time.Second)},
		{name: "zero", capturedAt: time.Time{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			repo.keys = []string{"anthropic/pro"}
			repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("anthropic/pro", "Claude Pro", tt.capturedAt)}
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

			got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
			require.NoError(t, err)
			require.True(t, got.Stale)
		})
	}
}

func TestRadarServiceQuotaTrendListErrorsAndContextAreSafe(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		listErr error
		wantErr error
	}{
		{name: "operational", listErr: errors.New("redis secret detail"), wantErr: ErrRadarUnavailable},
		{name: "corrupt index", listErr: ErrInvalidRadarCacheKey, wantErr: ErrRadarUnavailable},
		{name: "canceled", listErr: context.Canceled, wantErr: context.Canceled},
		{name: "deadline", listErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRadarQuotaServiceTestRepo()
			repo.listErr = tt.listErr
			service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

			got, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
			require.Nil(t, got)
			require.ErrorIs(t, err, tt.wantErr)
			require.NotContains(t, err.Error(), "secret")
			listCalls, _, trendCalls := repo.quotaCallCounts()
			require.Equal(t, 1, listCalls)
			require.Zero(t, trendCalls)
			require.Zero(t, radarQuotaServiceCacheSize(service))
		})
	}

	t.Run("caller already canceled", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := service.GetQuotaBucketsTrend(ctx, "anthropic/pro", 7)
		require.Nil(t, got)
		require.ErrorIs(t, err, context.Canceled)
		listCalls, _, trendCalls := repo.quotaCallCounts()
		require.Zero(t, listCalls)
		require.Zero(t, trendCalls)
	})
}

func TestRadarServiceQuotaTrendCacheKeyIsolationFreshnessDeadlineAndSingleflight(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)

	t.Run("cache key includes bucket and days", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro", "openai/pro"}
		repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("anthropic/pro", "Claude Pro", now)}
		repo.trends["openai/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("openai/pro", "OpenAI Pro", now)}
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)
		queries := []struct {
			bucket string
			days   int
		}{
			{bucket: "anthropic/pro", days: 1},
			{bucket: "anthropic/pro", days: 7},
			{bucket: "openai/pro", days: 1},
		}
		for _, query := range queries {
			for range 2 {
				_, err := service.GetQuotaBucketsTrend(context.Background(), query.bucket, query.days)
				require.NoError(t, err)
			}
		}
		calls := repo.snapshotTrendCalls()
		require.Len(t, calls, 3)
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].bucket != calls[j].bucket {
				return calls[i].bucket < calls[j].bucket
			}
			return calls[i].since.After(calls[j].since)
		})
		require.Equal(t, "anthropic/pro", calls[0].bucket)
		require.Equal(t, now.AddDate(0, 0, -1), calls[0].since)
		require.Equal(t, now.AddDate(0, 0, -7), calls[1].since)
		require.Equal(t, "openai/pro", calls[2].bucket)
		listCalls, _, _ := repo.quotaCallCounts()
		require.Equal(t, 3, listCalls)
	})

	t.Run("freshness deadline", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro"}
		repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("anthropic/pro", "Claude Pro", now.Add(-29*time.Minute))}
		clock := &radarServiceTestClock{now: now}
		service := mustNewRadarQuotaServiceForTest(t, repo, clock, 5*time.Minute)

		fresh, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
		require.NoError(t, err)
		require.False(t, fresh.Stale)
		clock.Advance(time.Minute)
		stale, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
		require.NoError(t, err)
		require.True(t, stale.Stale)
		require.Len(t, repo.snapshotTrendCalls(), 2)
	})

	t.Run("singleflight", func(t *testing.T) {
		repo := newRadarQuotaServiceTestRepo()
		repo.keys = []string{"anthropic/pro"}
		repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("anthropic/pro", "Claude Pro", now)}
		started := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		repo.trendHook = func(context.Context, string, time.Time) {
			once.Do(func() { close(started) })
			<-release
		}
		service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

		const callers = 12
		start := make(chan struct{})
		errCh := make(chan error, callers)
		var wait sync.WaitGroup
		for range callers {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
				errCh <- err
			}()
		}
		close(start)
		<-started
		close(release)
		wait.Wait()
		close(errCh)
		for err := range errCh {
			require.NoError(t, err)
		}
		require.Len(t, repo.snapshotTrendCalls(), 1)
		listCalls, _, _ := repo.quotaCallCounts()
		require.Equal(t, 1, listCalls)
	})
}

func TestRadarServiceCachedValueEvictsExpiredEntries(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	repo := newRadarQuotaServiceTestRepo()
	repo.keys = []string{"anthropic/pro"}
	repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*radarQuotaSnapshot("anthropic/pro", "Claude Pro", now)}
	clock := &radarServiceTestClock{now: now}
	service := mustNewRadarQuotaServiceForTest(t, repo, clock, time.Minute)

	_, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
	require.NoError(t, err)
	require.Equal(t, 1, radarQuotaServiceCacheSize(service))

	clock.Advance(time.Minute)
	key := radarServiceCacheKey{method: "quota_buckets_trend", bucket: "anthropic/pro", days: 7}
	_, ok := service.cachedValue(key, clock.Now())
	require.False(t, ok)
	require.Zero(t, radarQuotaServiceCacheSize(service), "expired entries must not accumulate indefinitely")
}

func TestRadarServiceExpiredCacheEvictionKeepsConcurrentReplacement(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	service := mustNewRadarQuotaServiceForTest(t, newRadarQuotaServiceTestRepo(), &radarServiceTestClock{now: now}, time.Minute)
	key := radarServiceCacheKey{method: "quota_buckets_trend", bucket: "anthropic/pro", days: 7}
	expired := &radarServiceCacheEntry{expiresAt: now.Add(-time.Second), value: "expired"}
	replacement := &radarServiceCacheEntry{expiresAt: now.Add(time.Minute), value: "replacement"}

	service.cacheMu.Lock()
	service.cache[key] = replacement
	service.cacheMu.Unlock()
	service.deleteCacheEntryIfCurrent(key, expired)

	got, ok := service.cachedValue(key, now)
	require.True(t, ok)
	require.Equal(t, "replacement", got)
	require.Equal(t, 1, radarQuotaServiceCacheSize(service))
}

func TestRadarServiceQuotaCallersCanConcurrentlyMutateIndependentClones(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	limit := 500.0
	repo := newRadarQuotaServiceTestRepo()
	repo.keys = []string{"anthropic/pro"}
	snapshot := radarQuotaSnapshot("anthropic/pro", "Claude Pro", now)
	snapshot.FiveHour = &WindowStatsDTO{AvgCost: 5, InferredLimitUSD: &limit, ContributorsCount: 3}
	snapshot.SevenDay = &WindowStatsDTO{AvgCost: 7, InferredLimitUSD: &limit, ContributorsCount: 3}
	snapshot.ModelBreakdown5h = []ModelCostBreakdownDTO{{Model: "other", AvgCost: 5, ContributorsCount: 3}}
	repo.latest["anthropic/pro"] = snapshot
	repo.trends["anthropic/pro"] = []BucketSnapshotDTO{*snapshot}
	service := mustNewRadarQuotaServiceForTest(t, repo, &radarServiceTestClock{now: now}, time.Minute)

	_, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	_, err = service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
	require.NoError(t, err)

	const callers = 24
	errCh := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			latest, err := service.GetQuotaBucketsLatest(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			latest.Buckets[0].FiveHour.AvgCost = 0
			*latest.Buckets[0].FiveHour.InferredLimitUSD = 0
			latest.Buckets[0].ModelBreakdown5h[0].Model = "caller"
			*latest.LastAggregatedAt = time.Time{}

			trend, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
			if err != nil {
				errCh <- err
				return
			}
			trend.DataPoints[0].FiveHour.AvgCost = 0
			*trend.DataPoints[0].FiveHour.InferredLimitUSD = 0
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	latest, err := service.GetQuotaBucketsLatest(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5.0, latest.Buckets[0].FiveHour.AvgCost)
	require.Equal(t, 500.0, *latest.Buckets[0].FiveHour.InferredLimitUSD)
	require.Equal(t, "other", latest.Buckets[0].ModelBreakdown5h[0].Model)
	require.Equal(t, now, *latest.LastAggregatedAt)
	trend, err := service.GetQuotaBucketsTrend(context.Background(), "anthropic/pro", 7)
	require.NoError(t, err)
	require.Equal(t, 5.0, trend.DataPoints[0].FiveHour.AvgCost)
	require.Equal(t, 500.0, *trend.DataPoints[0].FiveHour.InferredLimitUSD)
}

func TestParseRadarBucketKeyCanonicalContract(t *testing.T) {
	for _, tt := range []struct {
		bucket   string
		platform string
		tier     string
	}{
		{bucket: "anthropic/max_20x", platform: "anthropic", tier: "max_20x"},
		{bucket: "openai/pro-v1.2", platform: "openai", tier: "pro-v1.2"},
		{bucket: "antigravity/" + strings.Repeat("a", 64), platform: "antigravity", tier: strings.Repeat("a", 64)},
	} {
		platform, tier, err := ParseRadarBucketKey(tt.bucket)
		require.NoError(t, err)
		require.Equal(t, tt.platform, platform)
		require.Equal(t, tt.tier, tier)
	}

	for _, bucket := range []string{
		"", "anthropic", "anthropic/", "/pro", "Anthropic/pro", "google/pro",
		"anthropic/_pro", "anthropic/PRO", "anthropic/pro/escape", "anthropic/pro tier",
		"anthropic/" + strings.Repeat("a", 65),
	} {
		platform, tier, err := ParseRadarBucketKey(bucket)
		require.Empty(t, platform)
		require.Empty(t, tier)
		require.ErrorIs(t, err, ErrInvalidRadarCacheKey)
		if bucket != "" {
			require.NotContains(t, err.Error(), bucket)
		}
	}
}

func mustNewRadarQuotaServiceForTest(
	t *testing.T,
	repo RadarCacheRepository,
	clock radarServiceClock,
	cacheTTL time.Duration,
) *RadarService {
	t.Helper()
	service, err := newRadarService(radarServiceTestConfig(), repo, radarServiceOptions{
		clock:    clock,
		cacheTTL: cacheTTL,
	})
	require.NoError(t, err)
	return service
}

func radarQuotaServiceCacheSize(service *RadarService) int {
	service.cacheMu.RLock()
	defer service.cacheMu.RUnlock()
	return len(service.cache)
}

func radarQuotaSnapshot(bucket, displayName string, capturedAt time.Time) *BucketSnapshotDTO {
	platform, tier, _ := strings.Cut(bucket, "/")
	return &BucketSnapshotDTO{
		BucketKey:        bucket,
		Platform:         platform,
		PlanTier:         tier,
		DisplayName:      displayName,
		AccountsCount:    3,
		PrivacyThreshold: 2,
		FiveHour:         &WindowStatsDTO{ContributorsCount: 3},
		SevenDay:         &WindowStatsDTO{ContributorsCount: 3},
		ModelBreakdown5h: make([]ModelCostBreakdownDTO, 0),
		ModelBreakdown7d: make([]ModelCostBreakdownDTO, 0),
		CapturedAt:       capturedAt,
	}
}

func radarQuotaBucketKeys(buckets []BucketSnapshotDTO) []string {
	result := make([]string, len(buckets))
	for index := range buckets {
		result[index] = buckets[index].BucketKey
	}
	return result
}

func radarQuotaTrendTimes(points []QuotaTrendPointDTO) []time.Time {
	result := make([]time.Time, len(points))
	for index := range points {
		result[index] = points[index].Timestamp
	}
	return result
}
