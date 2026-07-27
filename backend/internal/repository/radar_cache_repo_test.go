package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	testRadarHistoryRetention = 7 * 24 * time.Hour
	testRadarAggregationGap   = 15 * time.Minute
	testRadarSourceRetention  = 7 * 24 * time.Hour
)

func newRadarCacheTestRepository(t *testing.T) (service.RadarCacheRepository, *redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	repo, err := NewRadarCacheRepository(rdb, validRadarCacheTestConfig())
	require.NoError(t, err)
	return repo, rdb, mr
}

func validRadarCacheTestConfig() *config.Config {
	return &config.Config{Radar: config.RadarConfig{
		QuotaAggregatorIntervalMin: 15,
		QuotaHistoryRetentionDays:  7,
		ExternalResponseMaxBytes:   32,
		SourceHardRetentionDays:    7,
	}}
}

func validPublicRadarServiceConfig(minAccounts int) *config.Config {
	return &config.Config{Radar: config.RadarConfig{
		Enabled: true, QuotaAggregatorIntervalMin: 15, QuotaHistoryRetentionDays: 7,
		SampleSizeWarnBelow: 3, PublicMinBucketAccounts: minAccounts,
		InferMinUtilization: 5, InferMaxStdevRatio: 0.3,
		ExternalRequestTimeoutSeconds: 10, ExternalResponseMaxBytes: 1024 * 1024,
		ArtificialAnalysisModelsIntervalMinutes: 360,
		LMArenaIntervalMinutes:                  1440, StatuspageIntervalMinutes: 30, SourceHardRetentionDays: 7,
		QuotaStaleThresholdMinutes: 30, HealthStaleThresholdMinutes: 60,
		ArtificialAnalysisModelsStaleThresholdMinutes: 720,
		LMArenaStaleThresholdMinutes:                  2880,
		LMArenaURL:                                    "https://datasets-server.huggingface.co/filter",
	}}
}

func writeRawRadarSnapshot(t *testing.T, rdb *redis.Client, snapshot service.BucketSnapshotDTO) {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, rdb.ZAdd(context.Background(), radarBucketRedisKey(snapshot.BucketKey), redis.Z{
		Score: float64(snapshot.CapturedAt.UnixMilli()), Member: encoded,
	}).Err())
	require.NoError(t, rdb.SAdd(context.Background(), radarBucketIndexKey, snapshot.BucketKey).Err())
}

func TestRadarCacheAndPublicServiceFailClosedOnInjectedPrivacyMetadata(t *testing.T) {
	ctx := context.Background()
	// 基准时间必须锚定真实时钟：本测试经由 service.NewRadarService 断言趋势数据，
	// 其 7 天窗口由服务内部的 real clock 计算（公开 API 无时钟注入口），
	// 固定日期会在快照滑出窗口后让测试整体过期失败。
	now := time.Now().UTC().Truncate(time.Millisecond)

	t.Run("repository skips malformed stored variants on latest and trend", func(t *testing.T) {
		variants := []struct {
			name   string
			mutate func(*service.BucketSnapshotDTO)
		}{
			{"old calculation version", func(s *service.BucketSnapshotDTO) { s.CalculationVersion = 1 }},
			{"missing threshold", func(s *service.BucketSnapshotDTO) { s.PrivacyThreshold = 0 }},
			{"single account", func(s *service.BucketSnapshotDTO) { s.AccountsCount = 1 }},
			{"window contributors", func(s *service.BucketSnapshotDTO) { s.FiveHour = &service.WindowStatsDTO{ContributorsCount: 1} }},
			{"model contributors", func(s *service.BucketSnapshotDTO) {
				s.SevenDaySonnet = &service.ModelWindowStatsDTO{Model: "claude-sonnet", SampleSize: 1}
			}},
			{"breakdown contributors", func(s *service.BucketSnapshotDTO) {
				s.ModelBreakdown5h = []service.ModelCostBreakdownDTO{{Model: "other", ContributorsCount: 1}}
			}},
		}
		for _, variant := range variants {
			t.Run(variant.name, func(t *testing.T) {
				repo, rdb, _ := newRadarCacheTestRepository(t)
				snapshot := testRadarSnapshot("anthropic/pro", now)
				variant.mutate(&snapshot)
				writeRawRadarSnapshot(t, rdb, snapshot)
				latest, err := repo.GetLatestBucket(ctx, snapshot.BucketKey)
				require.Nil(t, latest)
				require.ErrorIs(t, err, service.ErrRadarCacheMiss)
				trend, err := repo.GetBucketTrend(ctx, snapshot.BucketKey, now.Add(-time.Hour))
				require.NoError(t, err)
				require.Empty(t, trend)
				publicService, err := service.NewRadarService(validPublicRadarServiceConfig(2), repo)
				require.NoError(t, err)
				latestDTO, err := publicService.GetQuotaBucketsLatest(ctx)
				require.NoError(t, err)
				require.Empty(t, latestDTO.Buckets)
				trendDTO, err := publicService.GetQuotaBucketsTrend(ctx, snapshot.BucketKey, 7)
				require.NoError(t, err)
				require.Empty(t, trendDTO.DataPoints)
			})
		}
	})

	t.Run("current threshold filters older snapshots point by point", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { require.NoError(t, rdb.Close()) })
		cfg := validPublicRadarServiceConfig(3)
		repo, err := NewRadarCacheRepository(rdb, cfg)
		require.NoError(t, err)

		old := testRadarSnapshot("anthropic/pro", now.Add(-time.Hour))
		old.AccountsCount = 2
		old.PrivacyThreshold = 2
		compliantTrend := testRadarSnapshot("anthropic/pro", now.Add(-time.Minute))
		compliantTrend.PrivacyThreshold = 3
		other := testRadarSnapshot("openai/pro", now)
		other.PrivacyThreshold = 3
		writeRawRadarSnapshot(t, rdb, old)
		writeRawRadarSnapshot(t, rdb, compliantTrend)
		writeRawRadarSnapshot(t, rdb, other)

		publicService, err := service.NewRadarService(cfg, repo)
		require.NoError(t, err)
		latest, err := publicService.GetQuotaBucketsLatest(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"anthropic/pro", "openai/pro"}, []string{latest.Buckets[0].BucketKey, latest.Buckets[1].BucketKey})
		trend, err := publicService.GetQuotaBucketsTrend(ctx, "anthropic/pro", 7)
		require.NoError(t, err)
		require.Len(t, trend.DataPoints, 1)
		require.Equal(t, compliantTrend.CapturedAt, trend.DataPoints[0].Timestamp)
	})

	t.Run("latest skips a threshold-two bucket while retaining compliant bucket", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { require.NoError(t, rdb.Close()) })
		cfg := validPublicRadarServiceConfig(3)
		repo, err := NewRadarCacheRepository(rdb, cfg)
		require.NoError(t, err)
		old := testRadarSnapshot("anthropic/pro", now)
		old.AccountsCount, old.PrivacyThreshold = 2, 2
		compliant := testRadarSnapshot("openai/pro", now)
		compliant.PrivacyThreshold = 3
		writeRawRadarSnapshot(t, rdb, old)
		writeRawRadarSnapshot(t, rdb, compliant)
		publicService, err := service.NewRadarService(cfg, repo)
		require.NoError(t, err)
		latest, err := publicService.GetQuotaBucketsLatest(ctx)
		require.NoError(t, err)
		require.Len(t, latest.Buckets, 1)
		require.Equal(t, "openai/pro", latest.Buckets[0].BucketKey)
	})
}

func TestNewRadarCacheRepositoryRejectsInvalidDependenciesAndConfig(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	invalidConfig := func(mutate func(*config.RadarConfig)) *config.Config {
		cfg := validRadarCacheTestConfig()
		mutate(&cfg.Radar)
		return cfg
	}
	tests := []struct {
		name string
		rdb  *redis.Client
		cfg  *config.Config
	}{
		{name: "nil redis", rdb: nil, cfg: validRadarCacheTestConfig()},
		{name: "nil config", rdb: rdb, cfg: nil},
		{name: "zero aggregation interval", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.QuotaAggregatorIntervalMin = 0 })},
		{name: "negative aggregation interval", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.QuotaAggregatorIntervalMin = -1 })},
		{name: "zero history retention", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.QuotaHistoryRetentionDays = 0 })},
		{name: "negative history retention", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.QuotaHistoryRetentionDays = -1 })},
		{name: "zero payload limit", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.ExternalResponseMaxBytes = 0 })},
		{name: "negative payload limit", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.ExternalResponseMaxBytes = -1 })},
		{name: "zero source retention", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.SourceHardRetentionDays = 0 })},
		{name: "negative source retention", rdb: rdb, cfg: invalidConfig(func(c *config.RadarConfig) { c.SourceHardRetentionDays = -1 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewRadarCacheRepository(tt.rdb, tt.cfg)
			require.Nil(t, repo)
			require.Error(t, err)
		})
	}
}

func testRadarSnapshot(bucketKey string, capturedAt time.Time) service.BucketSnapshotDTO {
	platform, planTier, _ := strings.Cut(bucketKey, "/")
	return service.BucketSnapshotDTO{
		CalculationVersion: 2,
		BucketKey:          bucketKey,
		Platform:           platform,
		PlanTier:           planTier,
		DisplayName:        bucketKey,
		AccountsCount:      3,
		PrivacyThreshold:   2,
		ModelBreakdown5h:   []service.ModelCostBreakdownDTO{},
		ModelBreakdown7d:   []service.ModelCostBreakdownDTO{},
		CapturedAt:         capturedAt,
	}
}

func TestRadarCacheRepositoryAppendLatestAndTrend(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	base := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)

	snapshots := []service.BucketSnapshotDTO{
		testRadarSnapshot("anthropic/pro", base.Add(2*time.Hour)),
		testRadarSnapshot("anthropic/pro", base),
		testRadarSnapshot("anthropic/pro", base.Add(time.Hour)),
	}
	for _, snapshot := range snapshots {
		require.NoError(t, repo.AppendBucketSnapshot(ctx, snapshot))
	}

	latest, err := repo.GetLatestBucket(ctx, "anthropic/pro")
	require.NoError(t, err)
	require.Equal(t, base.Add(2*time.Hour), latest.CapturedAt)

	trend, err := repo.GetBucketTrend(ctx, "anthropic/pro", base.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []time.Time{base.Add(time.Hour), base.Add(2 * time.Hour)}, []time.Time{
		trend[0].CapturedAt,
		trend[1].CapturedAt,
	})

	// Redis scores have millisecond precision. A sub-millisecond since value must
	// still exclude a decoded point that is before the requested instant.
	trend, err = repo.GetBucketTrend(ctx, "anthropic/pro", base.Add(time.Hour+time.Nanosecond))
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, base.Add(2*time.Hour), trend[0].CapturedAt)

	missing, err := repo.GetLatestBucket(ctx, "openai/free")
	require.Nil(t, missing)
	require.ErrorIs(t, err, service.ErrRadarCacheMiss)

	empty, err := repo.GetBucketTrend(ctx, "openai/free", base)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestRadarCacheRepositoryAppendIsIdempotentAtExactScore(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	capturedAt := time.Date(2026, time.July, 2, 9, 30, 0, 123456000, time.UTC)

	first := testRadarSnapshot("openai/team", capturedAt)
	first.AccountsCount = 2
	second := first
	second.AccountsCount = 9

	require.NoError(t, repo.AppendBucketSnapshot(ctx, first))
	require.NoError(t, repo.AppendBucketSnapshot(ctx, second))

	count, err := rdb.ZCard(ctx, "radar:quota:bucket:openai/team").Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	latest, err := repo.GetLatestBucket(ctx, "openai/team")
	require.NoError(t, err)
	require.Equal(t, 9, latest.AccountsCount)
}

func TestRadarCacheRepositoryAppendTrimsHistoryAndSetsTTL(t *testing.T) {
	repo, _, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)
	key := "radar:quota:bucket:antigravity/enterprise"

	for _, capturedAt := range []time.Time{
		now.Add(-testRadarHistoryRetention - time.Millisecond),
		now.Add(-testRadarHistoryRetention),
		now,
	} {
		require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot("antigravity/enterprise", capturedAt)))
	}

	trend, err := repo.GetBucketTrend(ctx, "antigravity/enterprise", time.Time{})
	require.NoError(t, err)
	require.Len(t, trend, 2)
	require.Equal(t, now.Add(-testRadarHistoryRetention), trend[0].CapturedAt)
	require.Equal(t, now, trend[1].CapturedAt)
	require.Equal(t, testRadarHistoryRetention+2*testRadarAggregationGap, mr.TTL(key))
}

func TestRadarCacheRepositoryOutOfOrderAppendPreservesTTLUntilEqualCorrection(t *testing.T) {
	repo, _, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	newestAt := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	key := "radar:quota:bucket:anthropic/pro"
	initialTTL := testRadarHistoryRetention + 2*testRadarAggregationGap

	newest := testRadarSnapshot("anthropic/pro", newestAt)
	newest.AccountsCount = 10
	require.NoError(t, repo.AppendBucketSnapshot(ctx, newest))
	require.Equal(t, initialTTL, mr.TTL(key))

	mr.FastForward(30 * time.Minute)
	remainingTTL := initialTTL - 30*time.Minute

	tooOld := testRadarSnapshot("anthropic/pro", newestAt.Add(-testRadarHistoryRetention-time.Millisecond))
	require.NoError(t, repo.AppendBucketSnapshot(ctx, tooOld), "stale out-of-order writes are successful no-ops")
	require.Equal(t, remainingTTL, mr.TTL(key), "discarded writes must not extend retention")

	insideWindow := testRadarSnapshot("anthropic/pro", newestAt.Add(-testRadarHistoryRetention+time.Hour))
	require.NoError(t, repo.AppendBucketSnapshot(ctx, insideWindow))
	require.Equal(t, remainingTTL, mr.TTL(key), "accepted older writes must not extend retention")

	equalRetry := newest
	equalRetry.AccountsCount = 11
	require.NoError(t, repo.AppendBucketSnapshot(ctx, equalRetry))
	require.Equal(t, initialTTL, mr.TTL(key), "equal-score corrections must reset full retention")

	trend, err := repo.GetBucketTrend(ctx, "anthropic/pro", time.Time{})
	require.NoError(t, err)
	require.Len(t, trend, 2)
	require.Equal(t, insideWindow.CapturedAt, trend[0].CapturedAt)
	require.Equal(t, newestAt, trend[1].CapturedAt)
	require.Equal(t, 11, trend[1].AccountsCount)
}

func TestRadarCacheRepositoryListBucketKeysSortsAndCleansExpiredEntries(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	capturedAt := time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC)
	for _, bucketKey := range []string{"openai/team", "anthropic/pro", "antigravity/free"} {
		require.NoError(t, repo.AppendBucketSnapshot(ctx, testRadarSnapshot(bucketKey, capturedAt)))
	}

	// Simulate a bucket zset expiring while its durable index entry remains.
	require.NoError(t, rdb.Del(ctx, "radar:quota:bucket:openai/team").Err())

	keys, err := repo.ListBucketKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"anthropic/pro", "antigravity/free"}, keys)

	indexed, err := rdb.SMembers(ctx, "radar:quota:buckets").Result()
	require.NoError(t, err)
	require.ElementsMatch(t, keys, indexed)
}

func TestRadarCacheRepositoryIndexCleanupRechecksExistenceAtomically(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	impl := repo.(*radarCacheRepository)
	ctx := context.Background()
	recreatedBucket := "anthropic/pro"
	stillMissingBucket := "openai/team"
	require.NoError(t, rdb.SAdd(ctx, "radar:quota:buckets", recreatedBucket, stillMissingBucket).Err())

	// This is the initial pipelined observation made by ListBucketKeys: both
	// zsets are absent and therefore become cleanup candidates.
	exists, err := rdb.Exists(ctx,
		"radar:quota:bucket:"+recreatedBucket,
		"radar:quota:bucket:"+stillMissingBucket,
	).Result()
	require.NoError(t, err)
	require.Zero(t, exists)

	// Recreate one bucket after observation but before cleanup. The cleanup Lua
	// script must recheck EXISTS and preserve this index entry.
	require.NoError(t, rdb.ZAdd(ctx, "radar:quota:bucket:"+recreatedBucket, redis.Z{
		Score:  1000,
		Member: `{"recreated":true}`,
	}).Err())
	require.NoError(t, impl.cleanupMissingBucketIndexEntries(ctx, []string{recreatedBucket, stillMissingBucket}))

	present, err := rdb.SIsMember(ctx, "radar:quota:buckets", recreatedBucket).Result()
	require.NoError(t, err)
	require.True(t, present)
	present, err = rdb.SIsMember(ctx, "radar:quota:buckets", stillMissingBucket).Result()
	require.NoError(t, err)
	require.False(t, present)
}

func TestRadarCacheRepositorySourcePayloadMappingsTTLAndMiss(t *testing.T) {
	repo, rdb, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	performanceSource, err := service.RadarAAPerformanceSource("claude-4.1_opus")
	require.NoError(t, err)

	tests := []struct {
		name   string
		source service.RadarSourceKey
		key    string
		value  []byte
	}{
		{name: "artificial analysis", source: service.RadarSourceAA, key: "radar:degradation:aa", value: []byte(`[]`)},
		{name: "AA performance", source: performanceSource, key: "radar:degradation:aa:perf:claude-4.1_opus", value: []byte(`{}`)},
		{name: "LMArena", source: service.RadarSourceLMArena, key: "radar:degradation:lmarena", value: []byte(`{"rows":[]}`)},
		{name: "Claude status", source: service.RadarSourceStatusClaude, key: "radar:health:claude", value: []byte(`{"ok":true}`)},
		{name: "OpenAI status", source: service.RadarSourceStatusOpenAI, key: "radar:health:openai", value: []byte(`{"ok":true}`)},
		{name: "Windsurf status", source: service.RadarSourceStatusWindsurf, key: "radar:health:windsurf", value: []byte(`{"ok":true}`)},
		{name: "DeepSeek status", source: service.RadarSourceStatusDeepSeek, key: "radar:health:deepseek", value: []byte(`{"ok":true}`)},
		{name: "Kimi status", source: service.RadarSourceStatusKimi, key: "radar:health:kimi", value: []byte(`{"ok":true}`)},
		{name: "MiniMax global status", source: service.RadarSourceStatusMiniMaxGlobal, key: "radar:health:minimax:global", value: []byte(`{"ok":true}`)},
		{name: "MiniMax China status", source: service.RadarSourceStatusMiniMaxChina, key: "radar:health:minimax:china", value: []byte(`{"ok":true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, repo.SetSourcePayload(ctx, tt.source, tt.value, 2*time.Hour))

			stored, err := rdb.Get(ctx, tt.key).Bytes()
			require.NoError(t, err)
			require.Equal(t, tt.value, stored)
			require.Equal(t, 2*time.Hour, mr.TTL(tt.key))

			got, err := repo.GetSourcePayload(ctx, tt.source)
			require.NoError(t, err)
			require.Equal(t, tt.value, got)
		})
	}

	got, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
	require.NoError(t, err)
	require.Equal(t, []byte(`[]`), got, "an empty JSON collection must remain representable")

	missingSource, err := service.RadarAAPerformanceSource("missing-model")
	require.NoError(t, err)
	missing, err := repo.GetSourcePayload(ctx, missingSource)
	require.Nil(t, missing)
	require.ErrorIs(t, err, service.ErrRadarCacheMiss)
}

func TestRadarCacheRepositorySourcePayloadOverwriteRefreshesTTL(t *testing.T) {
	repo, _, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	key := "radar:degradation:aa"

	require.NoError(t, repo.SetSourcePayload(ctx, service.RadarSourceAA, []byte(`{"version":1}`), 2*time.Hour))
	mr.FastForward(90 * time.Minute)
	require.Equal(t, 30*time.Minute, mr.TTL(key))

	require.NoError(t, repo.SetSourcePayload(ctx, service.RadarSourceAA, []byte(`{"version":2}`), 3*time.Hour))
	got, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"version":2}`), got)
	require.Equal(t, 3*time.Hour, mr.TTL(key))
}

func TestRadarCacheRepositoryCommitSourceSuccessPublishesMatchingPayloadAndMeta(t *testing.T) {
	repo, rdb, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	attemptedAt := time.Date(2026, time.July, 11, 8, 0, 0, 123000000, time.UTC)
	succeededAt := attemptedAt
	status := 200
	meta := service.SourceFetchMeta{
		LastAttemptAt: attemptedAt,
		LastSuccessAt: &succeededAt,
		HTTPStatus:    &status,
	}
	payload := []byte(`{"models":[]}`)

	applied, err := repo.CommitSourceSuccess(ctx, service.RadarSourceAA, payload, 4*time.Hour, meta)
	require.NoError(t, err)
	require.True(t, applied)

	gotPayload, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload)
	allMeta, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, meta, allMeta[service.RadarSourceAA])
	require.Equal(t, 4*time.Hour, mr.TTL("radar:degradation:aa"))
	version, err := rdb.HGet(ctx, "radar:meta:source_versions", string(service.RadarSourceAA)).Int64()
	require.NoError(t, err)
	require.Equal(t, attemptedAt.UnixMilli(), version)
}

func TestRadarSourceCadenceAtomicallyOverridesStaleManualSuccessAndFailure(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	cadenceRepo, ok := repo.(service.RadarSourceCadenceRepository)
	require.True(t, ok)
	ctx := context.Background()
	source := service.RadarSourceLMArena
	attempt := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	oldDeadline := attempt.Add(time.Hour)
	advancedDeadline := oldDeadline.Add(time.Hour)
	oldCadence, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, oldDeadline)
	require.NoError(t, err)
	advancedCadence, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, advancedDeadline)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		meta := service.SourceFetchMeta{LastAttemptAt: attempt, LastSuccessAt: &attempt, NextFireAt: &oldDeadline, CadenceVersion: oldCadence.Version}
		applied, err := repo.CommitSourceSuccess(ctx, source, []byte(`{"leaderboard":[]}`), time.Hour, meta)
		require.NoError(t, err)
		require.True(t, applied)
		stored, err := repo.ListSourceMeta(ctx)
		require.NoError(t, err)
		require.Equal(t, advancedDeadline, *stored[source].NextFireAt)
		require.Equal(t, advancedCadence.Version, stored[source].CadenceVersion)
	})

	t.Run("failure", func(t *testing.T) {
		failedAt := attempt.Add(time.Minute)
		failure := service.DataSourceErrorCodeNetworkError
		meta := service.SourceFetchMeta{LastAttemptAt: failedAt, NextFireAt: &oldDeadline, CadenceVersion: oldCadence.Version, Error: &failure}
		applied, err := repo.CommitSourceFailure(ctx, source, meta)
		require.NoError(t, err)
		require.True(t, applied)
		stored, err := repo.ListSourceMeta(ctx)
		require.NoError(t, err)
		require.Equal(t, advancedDeadline, *stored[source].NextFireAt)
		require.Equal(t, advancedCadence.Version, stored[source].CadenceVersion)
	})
}

func TestRadarSourceCadenceVersionSelectionAcrossCommitPaths(t *testing.T) {
	type commitPath struct {
		name  string
		apply func(context.Context, service.RadarCacheRepository, service.RadarSourceKey, service.SourceFetchMeta) (bool, error)
	}
	paths := []commitPath{
		{
			name: "success",
			apply: func(ctx context.Context, repo service.RadarCacheRepository, source service.RadarSourceKey, meta service.SourceFetchMeta) (bool, error) {
				meta.LastSuccessAt = &meta.LastAttemptAt
				return repo.CommitSourceSuccess(ctx, source, []byte(`{"ok":true}`), time.Hour, meta)
			},
		},
		{
			name: "failure",
			apply: func(ctx context.Context, repo service.RadarCacheRepository, source service.RadarSourceKey, meta service.SourceFetchMeta) (bool, error) {
				failure := service.DataSourceErrorCodeNetworkError
				meta.Error = &failure
				return repo.CommitSourceFailure(ctx, source, meta)
			},
		},
		{
			name: "set meta",
			apply: func(ctx context.Context, repo service.RadarCacheRepository, source service.RadarSourceKey, meta service.SourceFetchMeta) (bool, error) {
				err := repo.SetSourceMeta(ctx, source, meta)
				return err == nil, err
			},
		},
	}

	base := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	deadlineEarly := base.Add(30 * time.Minute)
	deadlineLate := base.Add(2 * time.Hour)
	legacyMeta := service.SourceFetchMeta{LastAttemptAt: base}
	tests := []struct {
		name             string
		persisted        *service.RadarSourceCadence
		corruptPersisted func(context.Context, *redis.Client, service.RadarSourceKey)
		candidate        service.SourceFetchMeta
		want             *service.RadarSourceCadence
		wantErr          bool
	}{
		{
			name:      "persisted newer version wins",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "1"},
			want:      &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
		},
		{
			name:      "candidate newer version repairs hashes",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineEarly, Version: "1"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineLate, CadenceVersion: "2"},
			want:      &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
		},
		{
			name:      "newer version may move deadline earlier",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "1"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "2"},
			want:      &service.RadarSourceCadence{NextFireAt: deadlineEarly, Version: "2"},
		},
		{
			name:      "unassigned candidate atomically recovers after persisted sequence",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly},
			want:      &service.RadarSourceCadence{NextFireAt: deadlineEarly, Version: "3"},
		},
		{
			name:      "equal version and equal deadline is idempotent",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineLate, CadenceVersion: "2"},
			want:      &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
		},
		{
			name:      "equal version and conflicting deadline fails closed",
			persisted: &service.RadarSourceCadence{NextFireAt: deadlineLate, Version: "2"},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "2"},
			wantErr:   true,
		},
		{
			name: "persisted deadline without version fails closed",
			corruptPersisted: func(ctx context.Context, rdb *redis.Client, source service.RadarSourceKey) {
				require.NoError(t, rdb.HSet(ctx, radarSourceCadenceKey, string(source), deadlineLate.Format(time.RFC3339Nano)).Err())
			},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "2"},
			wantErr:   true,
		},
		{
			name: "persisted version without deadline fails closed",
			corruptPersisted: func(ctx context.Context, rdb *redis.Client, source service.RadarSourceKey) {
				require.NoError(t, rdb.HSet(ctx, radarSourceCadenceVersionKey, string(source), "2").Err())
			},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "3"},
			wantErr:   true,
		},
		{
			name: "invalid persisted version fails closed",
			corruptPersisted: func(ctx context.Context, rdb *redis.Client, source service.RadarSourceKey) {
				require.NoError(t, rdb.HSet(ctx, radarSourceCadenceKey, string(source), deadlineLate.Format(time.RFC3339Nano)).Err())
				require.NoError(t, rdb.HSet(ctx, radarSourceCadenceVersionKey, string(source), "not-a-version").Err())
			},
			candidate: service.SourceFetchMeta{LastAttemptAt: base, NextFireAt: &deadlineEarly, CadenceVersion: "3"},
			wantErr:   true,
		},
		{
			name:      "legacy double missing remains compatible",
			candidate: legacyMeta,
			want:      nil,
		},
	}

	for _, path := range paths {
		path := path
		t.Run(path.name, func(t *testing.T) {
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					repo, rdb, _ := newRadarCacheTestRepository(t)
					ctx := context.Background()
					source := service.RadarSourceStatusClaude
					if tt.persisted != nil {
						cadenceRepo := repo.(service.RadarSourceCadenceRepository)
						version, parseErr := strconv.Atoi(tt.persisted.Version)
						require.NoError(t, parseErr)
						for i := 0; i < version; i++ {
							stored, advanceErr := cadenceRepo.AdvanceSourceNextFire(ctx, source, tt.persisted.NextFireAt)
							require.NoError(t, advanceErr)
							require.Equal(t, strconv.Itoa(i+1), stored.Version)
						}
					}
					if tt.corruptPersisted != nil {
						tt.corruptPersisted(ctx, rdb, source)
					}
					beforeDeadline, err := rdb.HGetAll(ctx, radarSourceCadenceKey).Result()
					require.NoError(t, err)
					beforeVersion, err := rdb.HGetAll(ctx, radarSourceCadenceVersionKey).Result()
					require.NoError(t, err)

					applied, err := path.apply(ctx, repo, source, tt.candidate)
					if tt.wantErr {
						require.Error(t, err)
						require.False(t, applied)
						afterDeadline, readErr := rdb.HGetAll(ctx, radarSourceCadenceKey).Result()
						require.NoError(t, readErr)
						afterVersion, readErr := rdb.HGetAll(ctx, radarSourceCadenceVersionKey).Result()
						require.NoError(t, readErr)
						require.Equal(t, beforeDeadline, afterDeadline)
						require.Equal(t, beforeVersion, afterVersion)
						require.False(t, rdb.HExists(ctx, radarSourceMetaKey, string(source)).Val())
						return
					}

					require.NoError(t, err)
					require.True(t, applied)
					stored, err := repo.ListSourceMeta(ctx)
					require.NoError(t, err)
					if tt.want == nil {
						require.Nil(t, stored[source].NextFireAt)
						require.Empty(t, stored[source].CadenceVersion)
						require.False(t, rdb.HExists(ctx, radarSourceCadenceKey, string(source)).Val())
						require.False(t, rdb.HExists(ctx, radarSourceCadenceVersionKey, string(source)).Val())
						return
					}
					require.Equal(t, tt.want.NextFireAt, *stored[source].NextFireAt)
					require.Equal(t, tt.want.Version, stored[source].CadenceVersion)
					require.Equal(t, tt.want.NextFireAt.Format(time.RFC3339Nano), rdb.HGet(ctx, radarSourceCadenceKey, string(source)).Val())
					require.Equal(t, tt.want.Version, rdb.HGet(ctx, radarSourceCadenceVersionKey, string(source)).Val())
				})
			}
		})
	}
}

func TestRadarSourceCadenceCommitRepairsTransientAdvanceFailure(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	hook := &radarRedisCommandHook{}
	rdb.AddHook(hook)
	ctx := context.Background()
	source := service.RadarSourceLMArena
	base := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	firstDeadline := base.Add(time.Hour)
	secondDeadline := base.Add(2 * time.Hour)
	cadenceRepo := repo.(service.RadarSourceCadenceRepository)
	first, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, firstDeadline)
	require.NoError(t, err)

	hook.failNext("evalsha")
	_, err = cadenceRepo.AdvanceSourceNextFire(ctx, source, secondDeadline)
	require.Error(t, err)
	interveningDeadline := base.Add(3 * time.Hour)
	intervening, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, interveningDeadline)
	require.NoError(t, err)
	require.Equal(t, "2", intervening.Version)
	meta := service.SourceFetchMeta{
		LastAttemptAt: base,
		LastSuccessAt: &base,
		NextFireAt:    &secondDeadline,
	}
	applied, err := repo.CommitSourceSuccess(ctx, source, []byte(`{"leaderboard":[]}`), time.Hour, meta)
	require.NoError(t, err)
	require.True(t, applied)

	stored, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, secondDeadline, *stored[source].NextFireAt)
	require.Equal(t, "3", stored[source].CadenceVersion)
	require.Equal(t, secondDeadline.Format(time.RFC3339Nano), rdb.HGet(ctx, radarSourceCadenceKey, string(source)).Val())
	require.Equal(t, "3", rdb.HGet(ctx, radarSourceCadenceVersionKey, string(source)).Val())
	require.Equal(t, "1", first.Version)
}

func TestRadarSourceCadenceAdvanceUsesVersionAndFailsClosedOnCorruption(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	source := service.RadarSourceStatusOpenAI
	base := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	lateDeadline := base.Add(2 * time.Hour)
	earlyDeadline := base.Add(30 * time.Minute)
	cadenceRepo := repo.(service.RadarSourceCadenceRepository)
	first, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, lateDeadline)
	require.NoError(t, err)
	require.Equal(t, "1", first.Version)
	second, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, earlyDeadline)
	require.NoError(t, err)
	require.Equal(t, "2", second.Version)
	require.Equal(t, earlyDeadline, second.NextFireAt, "a later sequence may intentionally move the deadline earlier")
	authoritative, err := cadenceRepo.GetSourceCadence(ctx, source)
	require.NoError(t, err)
	require.Equal(t, second, authoritative)

	require.NoError(t, rdb.HDel(ctx, radarSourceCadenceVersionKey, string(source)).Err())
	_, err = cadenceRepo.AdvanceSourceNextFire(ctx, source, base.Add(3*time.Hour))
	require.Error(t, err)
	require.Equal(t, earlyDeadline.Format(time.RFC3339Nano), rdb.HGet(ctx, radarSourceCadenceKey, string(source)).Val())
	require.False(t, rdb.HExists(ctx, radarSourceCadenceVersionKey, string(source)).Val())

	overflowRepo, overflowRDB, _ := newRadarCacheTestRepository(t)
	overflowCadenceRepo := overflowRepo.(service.RadarSourceCadenceRepository)
	require.NoError(t, overflowRDB.HSet(ctx, radarSourceCadenceKey, string(source), lateDeadline.Format(time.RFC3339Nano)).Err())
	require.NoError(t, overflowRDB.HSet(ctx, radarSourceCadenceVersionKey, string(source), strconv.FormatInt(radarMaxExactCadenceVersion, 10)).Err())
	_, err = overflowCadenceRepo.AdvanceSourceNextFire(ctx, source, base.Add(4*time.Hour))
	require.Error(t, err, "sequence overflow must fail closed")
	require.Equal(t, strconv.FormatInt(radarMaxExactCadenceVersion, 10), overflowRDB.HGet(ctx, radarSourceCadenceVersionKey, string(source)).Val())
}

func TestRadarSourceCadenceConcurrentAdvanceAllocatesUniqueMonotonicSequence(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	cadenceRepo := repo.(service.RadarSourceCadenceRepository)
	ctx := context.Background()
	source := service.RadarSourceStatusClaude
	base := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	const advances = 64
	type result struct {
		deadline time.Time
		cadence  service.RadarSourceCadence
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, advances)
	for i := 0; i < advances; i++ {
		deadline := base.Add(time.Duration(i) * time.Nanosecond)
		go func() {
			<-start
			cadence, err := cadenceRepo.AdvanceSourceNextFire(ctx, source, deadline)
			results <- result{deadline: deadline, cadence: cadence, err: err}
		}()
	}
	close(start)
	seen := make(map[int64]time.Time, advances)
	for i := 0; i < advances; i++ {
		got := <-results
		require.NoError(t, got.err)
		version, err := strconv.ParseInt(got.cadence.Version, 10, 64)
		require.NoError(t, err)
		require.NotContains(t, seen, version)
		seen[version] = got.deadline.UTC()
		require.Equal(t, got.deadline.UTC(), got.cadence.NextFireAt)
	}
	for version := int64(1); version <= advances; version++ {
		require.Contains(t, seen, version)
	}
	authoritative, err := cadenceRepo.GetSourceCadence(ctx, source)
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(advances), authoritative.Version)
	require.Equal(t, seen[advances], authoritative.NextFireAt)
}

func TestRadarCacheRepositoryCommitSourceSuccessRejectsOlderCompletionWithoutRefreshingTTL(t *testing.T) {
	repo, _, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	newAttempt := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	oldAttempt := newAttempt.Add(-time.Hour)
	newStatus := 200
	oldStatus := 200
	newMeta := service.SourceFetchMeta{LastAttemptAt: newAttempt, LastSuccessAt: &newAttempt, HTTPStatus: &newStatus}
	oldMeta := service.SourceFetchMeta{LastAttemptAt: oldAttempt, LastSuccessAt: &oldAttempt, HTTPStatus: &oldStatus}

	applied, err := repo.CommitSourceSuccess(ctx, service.RadarSourceLMArena, []byte(`{"new":true}`), 4*time.Hour, newMeta)
	require.NoError(t, err)
	require.True(t, applied)
	mr.FastForward(time.Hour)

	applied, err = repo.CommitSourceSuccess(ctx, service.RadarSourceLMArena, []byte(`{"old":true}`), 6*time.Hour, oldMeta)
	require.NoError(t, err)
	require.False(t, applied)

	payload, err := repo.GetSourcePayload(ctx, service.RadarSourceLMArena)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"new":true}`), payload)
	allMeta, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, newMeta, allMeta[service.RadarSourceLMArena])
	require.Equal(t, 3*time.Hour, mr.TTL("radar:degradation:lmarena"), "older completion must not refresh payload TTL")
}

func TestRadarCacheRepositoryCommitSourceFailurePreservesSuccessAndPayloadAtomically(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	succeededAt := time.Date(2026, time.July, 11, 8, 0, 0, 0, time.UTC)
	successStatus := 200
	successMeta := service.SourceFetchMeta{
		LastAttemptAt: succeededAt,
		LastSuccessAt: &succeededAt,
		HTTPStatus:    &successStatus,
	}
	payload := []byte(`{"models":[{"slug":"safe"}]}`)
	applied, err := repo.CommitSourceSuccess(ctx, service.RadarSourceAA, payload, 4*time.Hour, successMeta)
	require.NoError(t, err)
	require.True(t, applied)

	failedAt := succeededAt.Add(time.Hour)
	poisonSuccess := succeededAt.Add(24 * time.Hour)
	nextFireAt := failedAt.Add(6 * time.Hour)
	failureStatus := 503
	failureCode := service.DataSourceErrorCodeUpstreamError
	failureMeta := service.SourceFetchMeta{
		LastAttemptAt:  failedAt,
		LastSuccessAt:  &poisonSuccess,
		NextFireAt:     &nextFireAt,
		CadenceVersion: "1",
		HTTPStatus:     &failureStatus,
		Error:          &failureCode,
	}
	applied, err = repo.CommitSourceFailure(ctx, service.RadarSourceAA, failureMeta)
	require.NoError(t, err)
	require.True(t, applied)

	gotPayload, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload, "failure commit must never mutate the last successful payload")
	allMeta, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	stored := allMeta[service.RadarSourceAA]
	require.Equal(t, failedAt, stored.LastAttemptAt)
	require.NotNil(t, stored.LastSuccessAt)
	require.Equal(t, succeededAt, *stored.LastSuccessAt, "failure must preserve stored success rather than caller input")
	require.Equal(t, &nextFireAt, stored.NextFireAt)
	require.Equal(t, &failureStatus, stored.HTTPStatus)
	require.Equal(t, &failureCode, stored.Error)
}

func TestRadarCacheRepositoryCommitSourceFailureColdStartAndOlderNoOp(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	newAttempt := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	oldAttempt := newAttempt.Add(-time.Hour)
	newCode := service.DataSourceErrorCodeRateLimited
	oldCode := service.DataSourceErrorCodeNetworkError
	newMeta := service.SourceFetchMeta{LastAttemptAt: newAttempt, Error: &newCode}
	oldMeta := service.SourceFetchMeta{LastAttemptAt: oldAttempt, Error: &oldCode}

	applied, err := repo.CommitSourceFailure(ctx, service.RadarSourceStatusOpenAI, newMeta)
	require.NoError(t, err)
	require.True(t, applied)
	allMeta, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Nil(t, allMeta[service.RadarSourceStatusOpenAI].LastSuccessAt)

	applied, err = repo.CommitSourceFailure(ctx, service.RadarSourceStatusOpenAI, oldMeta)
	require.NoError(t, err)
	require.False(t, applied)
	allMeta, err = repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, newMeta, allMeta[service.RadarSourceStatusOpenAI])
}

func TestRadarCacheRepositoryCommitSourceFailureConcurrentOrderingKeepsNewest(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	base := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	oldCode := service.DataSourceErrorCodeNetworkError
	newCode := service.DataSourceErrorCodeUpstreamError
	oldMeta := service.SourceFetchMeta{LastAttemptAt: base, Error: &oldCode}
	newMeta := service.SourceFetchMeta{LastAttemptAt: base.Add(time.Hour), Error: &newCode}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, meta := range []service.SourceFetchMeta{oldMeta, newMeta} {
		meta := meta
		go func() {
			<-start
			_, err := repo.CommitSourceFailure(ctx, service.RadarSourceLMArena, meta)
			errs <- err
		}()
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	allMeta, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, newMeta, allMeta[service.RadarSourceLMArena])
}

func TestRadarCacheRepositoryCommitSourceFailureRejectsCorruptMetaWithoutMutationOrLeak(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	const secret = "TOP_SECRET_CORRUPT_META"
	source := service.RadarSourceAA
	payload := []byte(`{"old":true}`)
	require.NoError(t, repo.SetSourcePayload(ctx, source, payload, time.Hour))
	require.NoError(t, rdb.HSet(ctx, "radar:meta:sources", string(source), `{"last_attempt_at":"`+secret+`"}`).Err())
	attemptedAt := time.Date(2026, time.July, 11, 11, 0, 0, 0, time.UTC)
	require.NoError(t, rdb.HSet(ctx, "radar:meta:source_versions", string(source), attemptedAt.Add(time.Hour).UnixMilli()).Err())
	failureCode := service.DataSourceErrorCodeNetworkError

	applied, err := repo.CommitSourceFailure(ctx, source, service.SourceFetchMeta{
		LastAttemptAt: attemptedAt,
		Error:         &failureCode,
	})
	require.False(t, applied)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	gotPayload, err := repo.GetSourcePayload(ctx, source)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload)
	rawMeta, err := rdb.HGet(ctx, "radar:meta:sources", string(source)).Result()
	require.NoError(t, err)
	require.Contains(t, rawMeta, secret)
}

func TestRadarCacheRepositoryCommitSourceFailureRejectsInvalidRFC3339WithoutAnyMutation(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	source := service.RadarSourceStatusOpenAI
	payload := []byte(`{"old":true}`)
	require.NoError(t, repo.SetSourcePayload(ctx, source, payload, time.Hour))
	candidateVersion := time.Date(2026, time.July, 11, 11, 0, 0, 0, time.UTC).UnixMilli()

	invalidTimes := []struct {
		name  string
		value string
	}{
		{name: "non time suffix", value: "2026-07-11Tnot-a-time"},
		{name: "month zero", value: "2026-00-11T10:20:30Z"},
		{name: "month thirteen", value: "2026-13-11T10:20:30Z"},
		{name: "day zero", value: "2026-07-00T10:20:30Z"},
		{name: "non leap february", value: "2026-02-29T10:20:30Z"},
		{name: "april day thirty one", value: "2026-04-31T10:20:30Z"},
		{name: "hour twenty four", value: "2026-07-11T24:20:30Z"},
		{name: "minute sixty", value: "2026-07-11T10:60:30Z"},
		{name: "second sixty", value: "2026-07-11T10:20:60Z"},
		{name: "empty fraction", value: "2026-07-11T10:20:30.Z"},
		{name: "fraction too long", value: "2026-07-11T10:20:30.1234567890Z"},
		{name: "fraction non digit", value: "2026-07-11T10:20:30.12xZ"},
		{name: "lowercase zone", value: "2026-07-11T10:20:30z"},
		{name: "offset hour out of range", value: "2026-07-11T10:20:30+24:00"},
		{name: "offset minute out of range", value: "2026-07-11T10:20:30+08:60"},
		{name: "offset missing colon", value: "2026-07-11T10:20:30+0800"},
		{name: "trailing garbage", value: "2026-07-11T10:20:30Zgarbage"},
	}

	for _, tt := range invalidTimes {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, rdb.HDel(ctx, radarSourceMetaKey, string(source)).Err())
			require.NoError(t, rdb.HDel(ctx, radarSourceVersionKey, string(source)).Err())
			candidate, err := json.Marshal(map[string]any{
				"last_attempt_at": tt.value,
				"last_success_at": nil,
				"next_fire_at":    nil,
				"http_status":     nil,
				"error":           service.DataSourceErrorCodeNetworkError,
			})
			require.NoError(t, err)

			result, err := radarCommitSourceFailureScript.Run(
				ctx,
				rdb,
				[]string{radarSourceMetaKey, radarSourceVersionKey, radarSourceCadenceKey, radarSourceCadenceVersionKey},
				string(source),
				candidate,
				candidateVersion,
			).Int64()
			require.NoError(t, err)
			require.Equal(t, int64(-1), result)
			metaExists, err := rdb.HExists(ctx, radarSourceMetaKey, string(source)).Result()
			require.NoError(t, err)
			require.False(t, metaExists)
			versionExists, err := rdb.HExists(ctx, radarSourceVersionKey, string(source)).Result()
			require.NoError(t, err)
			require.False(t, versionExists)
			gotPayload, err := repo.GetSourcePayload(ctx, source)
			require.NoError(t, err)
			require.Equal(t, payload, gotPayload)
		})
	}
}

func TestRadarCacheRepositoryCommitSourceFailureRejectsInvalidCurrentTimesWithoutAnyMutation(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	source := service.RadarSourceAA
	payload := []byte(`{"old":true}`)
	require.NoError(t, repo.SetSourcePayload(ctx, source, payload, time.Hour))
	validAttempt := "2026-07-11T10:20:30.123456789+08:00"
	validSuccess := "2024-02-29T23:59:59Z"
	candidateAt := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	failureCode := service.DataSourceErrorCodeNetworkError

	tests := []struct {
		name           string
		lastAttemptRaw string
		lastSuccessRaw string
	}{
		{name: "bad current attempt", lastAttemptRaw: "2026-07-11Tnot-a-time", lastSuccessRaw: validSuccess},
		{name: "bad current success", lastAttemptRaw: validAttempt, lastSuccessRaw: "2026-02-29T10:20:30Ztrailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := json.Marshal(map[string]any{
				"last_attempt_at": tt.lastAttemptRaw,
				"last_success_at": tt.lastSuccessRaw,
				"next_fire_at":    nil,
				"http_status":     200,
			})
			require.NoError(t, err)
			storedVersion := candidateAt.Add(time.Hour).UnixMilli()
			require.NoError(t, rdb.HSet(ctx, radarSourceMetaKey, string(source), stored).Err())
			require.NoError(t, rdb.HSet(ctx, radarSourceVersionKey, string(source), storedVersion).Err())

			applied, err := repo.CommitSourceFailure(ctx, source, service.SourceFetchMeta{
				LastAttemptAt: candidateAt,
				Error:         &failureCode,
			})
			require.False(t, applied)
			require.Error(t, err)
			rawAfter, err := rdb.HGet(ctx, radarSourceMetaKey, string(source)).Bytes()
			require.NoError(t, err)
			require.Equal(t, stored, rawAfter)
			versionAfter, err := rdb.HGet(ctx, radarSourceVersionKey, string(source)).Int64()
			require.NoError(t, err)
			require.Equal(t, storedVersion, versionAfter)
			gotPayload, err := repo.GetSourcePayload(ctx, source)
			require.NoError(t, err)
			require.Equal(t, payload, gotPayload)
		})
	}
}

func TestRadarCacheRepositoryCommitSourceFailureAcceptsGoRFC3339NanoBoundaries(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	location := time.FixedZone("boundary", -int((23*time.Hour+59*time.Minute)/time.Second))
	attemptedAt := time.Date(2024, time.February, 29, 23, 59, 59, 123456789, location)
	failureCode := service.DataSourceErrorCodeNetworkError
	applied, err := repo.CommitSourceFailure(ctx, service.RadarSourceStatusClaude, service.SourceFetchMeta{
		LastAttemptAt: attemptedAt,
		Error:         &failureCode,
	})
	require.NoError(t, err)
	require.True(t, applied)
}

func TestRadarCacheRepositoryEqualVersionSuccessTakesPrecedenceOverFailure(t *testing.T) {
	t.Run("success then failure keeps success", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		ctx := context.Background()
		attemptedAt := time.Date(2026, time.July, 11, 8, 0, 0, 123456789, time.UTC)
		status := 200
		successMeta := service.SourceFetchMeta{LastAttemptAt: attemptedAt, LastSuccessAt: &attemptedAt, HTTPStatus: &status}
		payload := []byte(`{"success":true}`)
		applied, err := repo.CommitSourceSuccess(ctx, service.RadarSourceAA, payload, time.Hour, successMeta)
		require.NoError(t, err)
		require.True(t, applied)
		failureCode := service.DataSourceErrorCodeNetworkError
		failureMeta := service.SourceFetchMeta{LastAttemptAt: attemptedAt, Error: &failureCode}

		applied, err = repo.CommitSourceFailure(ctx, service.RadarSourceAA, failureMeta)
		require.NoError(t, err)
		require.False(t, applied)
		allMeta, err := repo.ListSourceMeta(ctx)
		require.NoError(t, err)
		require.Equal(t, successMeta, allMeta[service.RadarSourceAA])
		gotPayload, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
		require.NoError(t, err)
		require.Equal(t, payload, gotPayload)
		version, err := rdb.HGet(ctx, radarSourceVersionKey, string(service.RadarSourceAA)).Int64()
		require.NoError(t, err)
		require.Equal(t, attemptedAt.UnixMilli(), version)
	})

	t.Run("failure then success upgrades to success", func(t *testing.T) {
		repo, _, _ := newRadarCacheTestRepository(t)
		ctx := context.Background()
		attemptedAt := time.Date(2026, time.July, 11, 8, 0, 0, 123456789, time.UTC)
		failureCode := service.DataSourceErrorCodeNetworkError
		failureMeta := service.SourceFetchMeta{LastAttemptAt: attemptedAt, Error: &failureCode}
		applied, err := repo.CommitSourceFailure(ctx, service.RadarSourceLMArena, failureMeta)
		require.NoError(t, err)
		require.True(t, applied)
		status := 200
		successMeta := service.SourceFetchMeta{LastAttemptAt: attemptedAt, LastSuccessAt: &attemptedAt, HTTPStatus: &status}
		payload := []byte(`{"success":true}`)

		applied, err = repo.CommitSourceSuccess(ctx, service.RadarSourceLMArena, payload, time.Hour, successMeta)
		require.NoError(t, err)
		require.True(t, applied)
		allMeta, err := repo.ListSourceMeta(ctx)
		require.NoError(t, err)
		require.Equal(t, successMeta, allMeta[service.RadarSourceLMArena])
		gotPayload, err := repo.GetSourcePayload(ctx, service.RadarSourceLMArena)
		require.NoError(t, err)
		require.Equal(t, payload, gotPayload)
	})
}

func TestRadarCacheRepositorySourcePayloadValidationPreservesPreviousValue(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	require.NoError(t, repo.SetSourcePayload(ctx, service.RadarSourceAA, []byte(`{"old":true}`), time.Hour))

	tests := []struct {
		name      string
		source    service.RadarSourceKey
		payload   []byte
		retention time.Duration
	}{
		{name: "nil payload", source: service.RadarSourceAA, payload: nil, retention: time.Hour},
		{name: "too large", source: service.RadarSourceAA, payload: []byte(strings.Repeat("x", 33)), retention: time.Hour},
		{name: "zero retention", source: service.RadarSourceAA, payload: []byte(`{}`), retention: 0},
		{name: "negative retention", source: service.RadarSourceAA, payload: []byte(`{}`), retention: -time.Second},
		{name: "retention above hard limit", source: service.RadarSourceAA, payload: []byte(`{}`), retention: testRadarSourceRetention + time.Nanosecond},
		{name: "unknown source", source: service.RadarSourceKey("aa:injected"), payload: []byte(`{}`), retention: time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.SetSourcePayload(ctx, tt.source, tt.payload, tt.retention)
			require.Error(t, err)
		})
	}

	got, err := repo.GetSourcePayload(ctx, service.RadarSourceAA)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"old":true}`), got)
}

func TestRadarCacheRepositorySourceMetaRoundTripEmptyAndCorrupt(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()

	empty, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)

	lastAttempt := time.Date(2026, time.July, 4, 8, 0, 0, 0, time.UTC)
	lastSuccess := lastAttempt.Add(-time.Minute)
	nextFire := lastAttempt.Add(30 * time.Minute)
	status := 429
	code := service.DataSourceErrorCodeRateLimited
	meta := service.SourceFetchMeta{
		LastAttemptAt:  lastAttempt,
		LastSuccessAt:  &lastSuccess,
		NextFireAt:     &nextFire,
		CadenceVersion: "1",
		HTTPStatus:     &status,
		Error:          &code,
	}
	require.NoError(t, repo.SetSourceMeta(ctx, service.RadarSourceStatusOpenAI, meta))

	all, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, map[service.RadarSourceKey]service.SourceFetchMeta{
		service.RadarSourceStatusOpenAI: meta,
	}, all)

	raw, err := rdb.HGet(ctx, "radar:meta:sources", string(service.RadarSourceStatusOpenAI)).Result()
	require.NoError(t, err)
	require.NotContains(t, raw, "error_text")
	require.NotContains(t, raw, "raw_error")

	require.NoError(t, rdb.HSet(ctx, "radar:meta:sources", string(service.RadarSourceAA), `{not-json`).Err())
	all, err = repo.ListSourceMeta(ctx)
	require.Nil(t, all)
	require.Error(t, err)
	require.NotContains(t, err.Error(), `{not-json`)
}

func TestRadarCacheRepositorySourceMetaValidation(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()

	err := repo.SetSourceMeta(ctx, service.RadarSourceAA, service.SourceFetchMeta{})
	require.Error(t, err, "last_attempt_at is required")

	err = repo.SetSourceMeta(ctx, service.RadarSourceKey("aa_perf:../../escape"), service.SourceFetchMeta{
		LastAttemptAt: time.Now().UTC(),
	})
	require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)

	validMeta, err := json.Marshal(service.SourceFetchMeta{LastAttemptAt: time.Now().UTC()})
	require.NoError(t, err)
	require.NoError(t, rdb.HSet(ctx, "radar:meta:sources", "status_openai:injected", validMeta).Err())
	all, err := repo.ListSourceMeta(ctx)
	require.Nil(t, all)
	require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
}

func TestRadarCacheRepositorySourceMetaCannotRegressLastAttempt(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	newAttempt := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	oldAttempt := newAttempt.Add(-time.Hour)
	newStatus := 200
	oldStatus := 503
	newMeta := service.SourceFetchMeta{LastAttemptAt: newAttempt, LastSuccessAt: &newAttempt, HTTPStatus: &newStatus}
	oldError := service.DataSourceErrorCodeUpstreamError
	oldFailureMeta := service.SourceFetchMeta{LastAttemptAt: oldAttempt, HTTPStatus: &oldStatus, Error: &oldError}

	require.NoError(t, repo.SetSourceMeta(ctx, service.RadarSourceStatusOpenAI, newMeta))
	require.NoError(t, repo.SetSourceMeta(ctx, service.RadarSourceStatusOpenAI, oldFailureMeta), "older metadata is a successful no-op")

	all, err := repo.ListSourceMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, newMeta, all[service.RadarSourceStatusOpenAI])
	version, err := rdb.HGet(ctx, "radar:meta:source_versions", string(service.RadarSourceStatusOpenAI)).Int64()
	require.NoError(t, err)
	require.Equal(t, newAttempt.UnixMilli(), version)
}

func TestRadarCacheRepositorySourceMetaRejectsUnknownErrorCodeWithoutStoring(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	unknownCode := service.DataSourceErrorCode("TOP_SECRET_UNKNOWN_FAILURE")

	err := repo.SetSourceMeta(ctx, service.RadarSourceAA, service.SourceFetchMeta{
		LastAttemptAt: time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC),
		Error:         &unknownCode,
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), string(unknownCode))

	stored, err := rdb.HExists(ctx, "radar:meta:sources", string(service.RadarSourceAA)).Result()
	require.NoError(t, err)
	require.False(t, stored)
	versionStored, err := rdb.HExists(ctx, "radar:meta:source_versions", string(service.RadarSourceAA)).Result()
	require.NoError(t, err)
	require.False(t, versionStored)
}

func TestRadarCacheRepositoryInvalidSourceMetaTimestampDoesNotLeakStoredValue(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	const secret = "TOP_SECRET_TOKEN"
	const stored = `{"last_attempt_at":"TOP_SECRET_TOKEN"}`
	require.NoError(t, rdb.HSet(ctx, "radar:meta:sources", string(service.RadarSourceAA), stored).Err())

	all, err := repo.ListSourceMeta(ctx)
	require.Nil(t, all)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
}

func TestRadarCacheRepositoryLockAcquireContentionAndExpiry(t *testing.T) {
	repo, _, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()

	acquired, err := repo.TryLock(ctx, "quota:aggregate", "instance-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = repo.TryLock(ctx, "quota:aggregate", "instance-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	mr.FastForward(time.Minute + time.Millisecond)
	acquired, err = repo.TryLock(ctx, "quota:aggregate", "instance-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestRadarCacheRepositoryReleaseLockRequiresCurrentOwner(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	const task = "quota:aggregate"

	acquired, err := repo.TryLock(ctx, task, "instance-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, repo.ReleaseLock(ctx, task, "instance-b"))
	acquired, err = repo.TryLock(ctx, task, "instance-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "a non-owner must not release the lock")

	require.NoError(t, repo.ReleaseLock(ctx, task, "instance-a"))
	acquired, err = repo.TryLock(ctx, task, "instance-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired, "the correct owner must release the lock")
}

func TestRadarCacheRepositoryStaleOwnerCannotReleaseReacquiredLock(t *testing.T) {
	repo, rdb, mr := newRadarCacheTestRepository(t)
	ctx := context.Background()
	const task = "source:refresh"

	acquired, err := repo.TryLock(ctx, task, "instance-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	mr.FastForward(time.Minute + time.Millisecond)
	acquired, err = repo.TryLock(ctx, task, "instance-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, repo.ReleaseLock(ctx, task, "instance-a"))
	owner, err := rdb.Get(ctx, "radar:lock:"+task).Result()
	require.NoError(t, err)
	require.Equal(t, "instance-b", owner)
}

func TestRadarCacheRepositoryRejectsUnsafeKeys(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	capturedAt := time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC)

	invalidBuckets := []string{
		"anthropic",
		"anthropic/",
		"/pro",
		"Anthropic/pro",
		"google/pro",
		"anthropic/pro/../../lock",
		"anthropic/PRO",
		"anthropic/pro tier",
		"anthropic/" + strings.Repeat("a", 65),
		"anthropic/pro\nradar:lock:quota",
	}
	for _, bucketKey := range invalidBuckets {
		t.Run("bucket "+bucketKey, func(t *testing.T) {
			snapshot := testRadarSnapshot(bucketKey, capturedAt)
			require.ErrorIs(t, repo.AppendBucketSnapshot(ctx, snapshot), service.ErrInvalidRadarCacheKey)
			_, err := repo.GetLatestBucket(ctx, bucketKey)
			require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
			_, err = repo.GetBucketTrend(ctx, bucketKey, capturedAt)
			require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
		})
	}

	mismatch := testRadarSnapshot("openai/pro", capturedAt)
	mismatch.PlanTier = "team"
	require.ErrorIs(t, repo.AppendBucketSnapshot(ctx, mismatch), service.ErrInvalidRadarCacheKey)

	zeroCapturedAt := testRadarSnapshot("openai/pro", time.Time{})
	require.Error(t, repo.AppendBucketSnapshot(ctx, zeroCapturedAt))

	invalidModels := []string{
		"",
		"../claude",
		"claude:4",
		"claude/4",
		"claude 4",
		strings.Repeat("a", 129),
	}
	for _, modelSlug := range invalidModels {
		_, err := service.RadarAAPerformanceSource(modelSlug)
		require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
	}
	for _, validModel := range []string{strings.Repeat("a", 128), "QwQ-32B-Preview"} {
		source, err := service.RadarAAPerformanceSource(validModel)
		require.NoError(t, err)
		require.Equal(t, service.RadarSourceKey("aa_perf:"+validModel), source)
	}

	invalidTasks := []string{
		"",
		"Quota",
		"quota/task",
		"../quota",
		"quota task",
		"quota.task",
		"aa_perf:../../escape",
		strings.Repeat("a", 129),
	}
	for _, task := range invalidTasks {
		acquired, err := repo.TryLock(ctx, task, "owner", time.Minute)
		require.False(t, acquired)
		require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
		require.ErrorIs(t, repo.ReleaseLock(ctx, task, "owner"), service.ErrInvalidRadarCacheKey)
	}
	validPerformanceTasks := []string{
		"aa_perf:claude-4.1_opus",
		"aa_perf:" + strings.Repeat("a", 128),
	}
	for _, task := range validPerformanceTasks {
		acquired, err := repo.TryLock(ctx, task, "owner", time.Minute)
		require.NoError(t, err)
		require.True(t, acquired)
		require.NoError(t, repo.ReleaseLock(ctx, task, "owner"))
	}
	for _, input := range []struct {
		owner string
		ttl   time.Duration
	}{
		{owner: "", ttl: time.Minute},
		{owner: "   ", ttl: time.Minute},
		{owner: "owner", ttl: 0},
		{owner: "owner", ttl: -time.Second},
	} {
		acquired, err := repo.TryLock(ctx, "quota", input.owner, input.ttl)
		require.False(t, acquired)
		require.Error(t, err)
	}
	for _, owner := range []string{"", "   "} {
		require.Error(t, repo.ReleaseLock(ctx, "quota", owner))
	}
}

func TestRadarCacheRepositoryListRejectsUnsafeIndexedBucketWithoutKeyConcatenation(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	require.NoError(t, rdb.SAdd(ctx, "radar:quota:buckets", "../../radar:lock:quota").Err())

	keys, err := repo.ListBucketKeys(ctx)
	require.Nil(t, keys)
	require.ErrorIs(t, err, service.ErrInvalidRadarCacheKey)
}

func TestRadarCacheRepositoryMalformedSnapshotJSONFailsWithoutLeakingPayload(t *testing.T) {
	repo, rdb, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	const corruptPayload = `{private-upstream-data:not-json`
	key := "radar:quota:bucket:anthropic/pro"
	require.NoError(t, rdb.ZAdd(ctx, key, redis.Z{Score: 1000, Member: corruptPayload}).Err())

	latest, err := repo.GetLatestBucket(ctx, "anthropic/pro")
	require.Nil(t, latest)
	require.ErrorIs(t, err, service.ErrRadarCacheMiss)
	require.NotContains(t, err.Error(), corruptPayload)

	trend, err := repo.GetBucketTrend(ctx, "anthropic/pro", time.UnixMilli(0))
	require.NoError(t, err)
	require.Empty(t, trend)
}

func TestRadarCacheRepositoryInvalidSnapshotTimestampDoesNotLeakStoredValue(t *testing.T) {
	const secret = "TOP_SECRET_TOKEN"
	const stored = `{"bucket_key":"anthropic/pro","platform":"anthropic","plan_tier":"pro","captured_at":"TOP_SECRET_TOKEN"}`

	t.Run("latest", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		ctx := context.Background()
		require.NoError(t, rdb.ZAdd(ctx, "radar:quota:bucket:anthropic/pro", redis.Z{
			Score:  1000,
			Member: stored,
		}).Err())

		latest, err := repo.GetLatestBucket(ctx, "anthropic/pro")
		require.Nil(t, latest)
		require.ErrorIs(t, err, service.ErrRadarCacheMiss)
		require.NotContains(t, err.Error(), secret)
	})

	t.Run("trend", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		ctx := context.Background()
		require.NoError(t, rdb.ZAdd(ctx, "radar:quota:bucket:anthropic/pro", redis.Z{
			Score:  1000,
			Member: stored,
		}).Err())

		trend, err := repo.GetBucketTrend(ctx, "anthropic/pro", time.UnixMilli(0))
		require.NoError(t, err)
		require.Empty(t, trend)
	})
}

func TestRadarCacheRepositoryBoundedLatestFallbackAndTrendRecovery(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 14, 8, 0, 0, 0, time.UTC)

	t.Run("normal valid top uses one bounded command", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		hook := &radarRedisCommandHook{}
		rdb.AddHook(hook)
		writeRawRadarSnapshot(t, rdb, testRadarSnapshot("anthropic/pro", base))
		hook.reset()
		latest, err := repo.GetLatestBucket(ctx, "anthropic/pro")
		require.NoError(t, err)
		require.Equal(t, base, latest.CapturedAt)
		require.Equal(t, []string{"zrevrange"}, hook.snapshot())
	})

	t.Run("invalid newest falls back to nearest valid in two commands", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		hook := &radarRedisCommandHook{}
		rdb.AddHook(hook)
		valid := testRadarSnapshot("anthropic/pro", base)
		writeRawRadarSnapshot(t, rdb, valid)
		require.NoError(t, rdb.ZAdd(ctx, radarBucketRedisKey(valid.BucketKey), redis.Z{Score: float64(base.Add(time.Minute).UnixMilli()), Member: `{malicious`}).Err())
		hook.reset()
		latest, err := repo.GetLatestBucket(ctx, valid.BucketKey)
		require.NoError(t, err)
		require.Equal(t, valid.CapturedAt, latest.CapturedAt)
		require.Equal(t, []string{"zrevrange", "zrevrange"}, hook.snapshot())
	})

	t.Run("fallback scans at most 128 total points", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		hook := &radarRedisCommandHook{}
		rdb.AddHook(hook)
		valid := testRadarSnapshot("anthropic/pro", base)
		writeRawRadarSnapshot(t, rdb, valid)
		invalid := make([]redis.Z, 129)
		for index := range invalid {
			invalid[index] = redis.Z{Score: float64(base.Add(time.Duration(index+1) * time.Minute).UnixMilli()), Member: `{invalid-` + strconv.Itoa(index)}
		}
		require.NoError(t, rdb.ZAdd(ctx, radarBucketRedisKey(valid.BucketKey), invalid...).Err())
		hook.reset()
		latest, err := repo.GetLatestBucket(ctx, valid.BucketKey)
		require.Nil(t, latest)
		require.ErrorIs(t, err, service.ErrRadarCacheMiss)
		require.Equal(t, []string{"zrevrange", "zrevrange"}, hook.snapshot())
	})

	t.Run("trend skips old invalid and retains new valid", func(t *testing.T) {
		repo, rdb, _ := newRadarCacheTestRepository(t)
		valid := testRadarSnapshot("anthropic/pro", base.Add(time.Minute))
		writeRawRadarSnapshot(t, rdb, valid)
		require.NoError(t, rdb.ZAdd(ctx, radarBucketRedisKey(valid.BucketKey), redis.Z{Score: float64(base.UnixMilli()), Member: `{legacy`}).Err())
		trend, err := repo.GetBucketTrend(ctx, valid.BucketKey, base.Add(-time.Hour))
		require.NoError(t, err)
		require.Equal(t, []service.BucketSnapshotDTO{valid}, trend)
	})
}

func TestRadarCacheRepositoryPreservesNilAndEmptyCollections(t *testing.T) {
	repo, _, _ := newRadarCacheTestRepository(t)
	ctx := context.Background()
	capturedAt := time.Date(2026, time.July, 6, 0, 0, 0, 0, time.UTC)

	emptyCollections := testRadarSnapshot("anthropic/pro", capturedAt)
	emptyCollections.ModelBreakdown5h = []service.ModelCostBreakdownDTO{}
	emptyCollections.ModelBreakdown7d = []service.ModelCostBreakdownDTO{}
	nilCollections := testRadarSnapshot("openai/pro", capturedAt)
	nilCollections.ModelBreakdown5h = nil
	nilCollections.ModelBreakdown7d = nil

	require.NoError(t, repo.AppendBucketSnapshot(ctx, emptyCollections))
	require.NoError(t, repo.AppendBucketSnapshot(ctx, nilCollections))

	gotEmpty, err := repo.GetLatestBucket(ctx, "anthropic/pro")
	require.NoError(t, err)
	require.NotNil(t, gotEmpty.ModelBreakdown5h)
	require.NotNil(t, gotEmpty.ModelBreakdown7d)
	require.Empty(t, gotEmpty.ModelBreakdown5h)
	require.Empty(t, gotEmpty.ModelBreakdown7d)

	gotNil, err := repo.GetLatestBucket(ctx, "openai/pro")
	require.NoError(t, err)
	require.Nil(t, gotNil.ModelBreakdown5h)
	require.Nil(t, gotNil.ModelBreakdown7d)
}
