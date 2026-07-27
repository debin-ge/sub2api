package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	radarServiceDefaultCacheTTL    = time.Minute
	radarServiceDefaultLoadTimeout = 5 * time.Second
)

const (
	radarServiceArtificialAnalysisPublicURL = "https://artificialanalysis.ai"
	radarServiceLMArenaPublicURL            = "https://huggingface.co/datasets/lmarena-ai/leaderboard-dataset"
	radarServiceQuotaAggregatorSourceKey    = "quota_aggregator"
	radarServiceQuotaAggregatorSourceName   = "Sub2API Aggregated Usage"
)

var (
	// ErrInvalidRadarQuery classifies a rejected public Radar query without
	// echoing untrusted query values.
	ErrInvalidRadarQuery = errors.New("invalid radar query")
	// ErrRadarUnavailable is the safe public classification for an operational
	// or stored-data failure that cannot be locally degraded.
	ErrRadarUnavailable = errors.New("radar data unavailable")
)

// RadarPublicService is the read-only public Model Radar contract.
type RadarPublicService interface {
	GetServiceHealth(ctx context.Context) ([]ServiceHealthDTO, error)
	GetQuotaBucketsLatest(ctx context.Context) (*QuotaRadarLatestDTO, error)
	GetQuotaBucketsTrend(ctx context.Context, bucketKey string, days int) (*QuotaTrendDTO, error)
	GetDegradationLatest(ctx context.Context) (*DegradationLatestDTO, error)
	GetLMArena(ctx context.Context) (*LMArenaDTO, error)
	GetDataSources(ctx context.Context) ([]DataSourceMetaDTO, error)
}

type radarServiceClock interface {
	Now() time.Time
}

type realRadarServiceClock struct{}

func (realRadarServiceClock) Now() time.Time { return time.Now() }

type radarServiceOptions struct {
	clock           radarServiceClock
	cacheTTL        time.Duration
	loadTimeout     time.Duration
	afterFlightJoin func(radarServiceCacheKey)
	afterLoad       func(radarServiceCacheKey)
	catalog         RadarPublicModelCatalog
}

type radarServiceCacheKey struct {
	method string
	bucket string
	days   int
}

type radarServiceCacheEntry struct {
	expiresAt time.Time
	value     any
}

// RadarService serves cached public Radar data. It never fetches an external
// source and never calls a mutating repository method.
type RadarService struct {
	repo                  RadarCacheRepository
	aggregatorStateReader radarAggregatorStateReader
	catalog               RadarPublicModelCatalog

	aaConfigured            bool
	sampleSizeWarnBelow     int
	publicMinBucketAccounts int
	quotaStaleThreshold     time.Duration
	healthStaleThreshold    time.Duration
	aaModelsStaleThreshold  time.Duration
	lmarenaStaleThreshold   time.Duration
	aaModelsInterval        time.Duration
	lmarenaInterval         time.Duration
	statuspageInterval      time.Duration
	quotaAggregatorInterval time.Duration

	clock       radarServiceClock
	cacheTTL    time.Duration
	loadTimeout time.Duration
	cacheMu     sync.RWMutex
	cache       map[radarServiceCacheKey]*radarServiceCacheEntry
	flights     singleflight.Group

	afterFlightJoin func(radarServiceCacheKey)
	afterLoad       func(radarServiceCacheKey)
}

type radarAggregatorStateReader interface {
	GetRadarAggregatorState(context.Context) (RadarMetricsSnapshot, error)
}

var _ RadarPublicService = (*RadarService)(nil)

// NewRadarService constructs the read-only public Model Radar service.
func NewRadarService(cfg *config.Config, repo RadarCacheRepository) (*RadarService, error) {
	return newRadarService(cfg, repo, radarServiceOptions{})
}

// ProvideRadarService is the production provider. Keeping NewRadarService's
// two-argument form avoids forcing repository-only callers to construct the
// public catalog dependency.
func ProvideRadarService(cfg *config.Config, repo RadarCacheRepository, catalog *ModelCatalogService) (*RadarService, error) {
	return newRadarService(cfg, repo, radarServiceOptions{catalog: catalog})
}

func newRadarService(
	cfg *config.Config,
	repo RadarCacheRepository,
	options radarServiceOptions,
) (*RadarService, error) {
	if cfg == nil {
		return nil, errors.New("radar service requires config")
	}
	if isNilRadarCacheRepository(repo) {
		return nil, errors.New("radar service requires cache repository")
	}
	aggregatorStateReader, ok := repo.(radarAggregatorStateReader)
	if !ok {
		return nil, errors.New("radar service requires aggregator state reader")
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, errors.New("radar service requires valid radar config")
	}
	if options.clock == nil {
		options.clock = realRadarServiceClock{}
	}
	if options.cacheTTL == 0 {
		options.cacheTTL = radarServiceDefaultCacheTTL
	}
	if options.cacheTTL <= 0 {
		return nil, errors.New("radar service cache ttl must be positive")
	}
	if options.loadTimeout == 0 {
		options.loadTimeout = radarServiceDefaultLoadTimeout
	}
	if options.loadTimeout <= 0 {
		return nil, errors.New("radar service load timeout must be positive")
	}

	return &RadarService{
		repo:                    repo,
		aggregatorStateReader:   aggregatorStateReader,
		catalog:                 options.catalog,
		aaConfigured:            strings.TrimSpace(cfg.Radar.ArtificialAnalysisAPIKey) != "",
		sampleSizeWarnBelow:     cfg.Radar.SampleSizeWarnBelow,
		publicMinBucketAccounts: cfg.Radar.PublicMinBucketAccounts,
		quotaStaleThreshold:     time.Duration(cfg.Radar.QuotaStaleThresholdMinutes) * time.Minute,
		healthStaleThreshold:    time.Duration(cfg.Radar.HealthStaleThresholdMinutes) * time.Minute,
		aaModelsStaleThreshold:  time.Duration(cfg.Radar.ArtificialAnalysisModelsStaleThresholdMinutes) * time.Minute,
		lmarenaStaleThreshold:   time.Duration(cfg.Radar.LMArenaStaleThresholdMinutes) * time.Minute,
		aaModelsInterval:        time.Duration(cfg.Radar.ArtificialAnalysisModelsIntervalMinutes) * time.Minute,
		lmarenaInterval:         time.Duration(cfg.Radar.LMArenaIntervalMinutes) * time.Minute,
		statuspageInterval:      time.Duration(cfg.Radar.StatuspageIntervalMinutes) * time.Minute,
		quotaAggregatorInterval: time.Duration(cfg.Radar.QuotaAggregatorIntervalMin) * time.Minute,
		clock:                   options.clock,
		cacheTTL:                options.cacheTTL,
		loadTimeout:             options.loadTimeout,
		cache:                   make(map[radarServiceCacheKey]*radarServiceCacheEntry),
		afterFlightJoin:         options.afterFlightJoin,
		afterLoad:               options.afterLoad,
	}, nil
}

// GetServiceHealth returns the historical canonical cards plus platform health
// cards backed by official sources. A broken source degrades only the services
// it owns; cancellation and deadlines remain observable.
func (s *RadarService) GetServiceHealth(ctx context.Context) ([]ServiceHealthDTO, error) {
	key := radarServiceCacheKey{method: "service_health"}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, _ time.Time) (any, bool, *time.Time, error) {
		cacheable := true
		metas, err := s.repo.ListSourceMeta(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			metas = nil
			cacheable = false
		}

		sources := statuspageRadarSources()
		groups := make([][]ServiceHealthDTO, 0, len(sources))
		usable := make(map[RadarSourceKey]bool, len(sources))
		for _, source := range sources {
			group, sourceUsable, sourceCacheable, readErr := s.readStatuspageCards(loadCtx, source)
			if readErr != nil {
				return nil, false, nil, readErr
			}
			groups = append(groups, group)
			usable[source] = sourceUsable
			cacheable = cacheable && sourceCacheable
		}

		now := s.clock.Now().UTC()
		cards := MergeStatuspageServiceHealth(groups...)
		staleBySource := make(map[RadarSourceKey]bool, len(sources))
		var earliestDeadline *time.Time
		for _, source := range sources {
			meta, metaOK := metas[source]
			stale, deadline := radarSourceFreshness(now, s.healthStaleThreshold, meta, metaOK, usable[source])
			staleBySource[source] = stale
			earliestDeadline = radarServiceEarliestDeadline(earliestDeadline, deadline)
		}
		for index := range cards {
			cardSources := statuspageSourcesForServiceKey(cards[index].ServiceKey)
			for _, source := range cardSources {
				cards[index].Stale = cards[index].Stale || staleBySource[source]
			}
			// Component updated_at means "the component last changed state" and
			// can legitimately remain unchanged for months. The card's public
			// "Updated" field describes Radar source freshness, so use the most
			// recent successful collection time instead.
			cards[index].LastUpdatedAt = latestRadarSourceSuccess(metas, cardSources)
		}
		return cards,
			cacheable,
			earliestDeadline,
			nil
	}, func(value any) any {
		v, ok := value.([]ServiceHealthDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceHealth(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.([]ServiceHealthDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

func (s *RadarService) readStatuspageCards(
	ctx context.Context,
	source RadarSourceKey,
) ([]ServiceHealthDTO, bool, bool, error) {
	payload, err := s.repo.GetSourcePayload(ctx, source)
	if contextErr := radarServiceContextError(ctx, err); contextErr != nil {
		return nil, false, false, contextErr
	}
	if errors.Is(err, ErrRadarCacheMiss) {
		return nil, false, true, nil
	}
	if err != nil {
		return nil, false, false, nil
	}
	summary, err := DecodeStatuspageSummary(payload)
	if err != nil {
		return nil, false, false, nil
	}
	cards, err := MapStatuspageServiceHealth(source, summary)
	if err != nil {
		return nil, false, false, nil
	}
	return cards, true, true, nil
}

// GetQuotaBucketsLatest returns the latest cached snapshot for every indexed
// quota bucket. A bucket that expires after the index read is skipped as a
// benign race; all other repository failures use the safe public class.
func (s *RadarService) GetQuotaBucketsLatest(ctx context.Context) (*QuotaRadarLatestDTO, error) {
	key := radarServiceCacheKey{method: "quota_buckets_latest"}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, now time.Time) (any, bool, *time.Time, error) {
		bucketKeys, err := s.repo.ListBucketKeys(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}

		now = now.UTC()
		result := &QuotaRadarLatestDTO{
			Buckets:             make([]BucketSnapshotDTO, 0, len(bucketKeys)),
			SampleSizeWarnBelow: s.sampleSizeWarnBelow,
			Stale:               true,
		}
		deadlines := make([]*time.Time, 0, len(bucketKeys))
		for _, bucketKey := range bucketKeys {
			platform, planTier, err := ParseRadarBucketKey(bucketKey)
			if err != nil {
				return nil, false, nil, ErrRadarUnavailable
			}
			if !isSupportedRadarQuotaPlanTier(platform, planTier) {
				continue
			}
			snapshot, err := s.repo.GetLatestBucket(loadCtx, bucketKey)
			if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
				return nil, false, nil, contextErr
			}
			if errors.Is(err, ErrRadarCacheMiss) {
				continue
			}
			if err != nil {
				return nil, false, nil, ErrRadarUnavailable
			}
			if snapshot == nil || snapshot.BucketKey != bucketKey ||
				snapshot.Platform != platform || snapshot.PlanTier != planTier ||
				ValidateRadarBucketSnapshot(*snapshot) != nil {
				continue
			}
			if snapshot.PrivacyThreshold < s.publicMinBucketAccounts {
				continue
			}

			mapped := *snapshot
			mapped.DisplayName = radarQuotaDisplayName(platform, planTier)
			mapped.CapturedAt = snapshot.CapturedAt.UTC()
			var deadline *time.Time
			mapped.Stale, deadline = radarQuotaFreshness(now, s.quotaStaleThreshold, mapped.CapturedAt)
			deadlines = append(deadlines, deadline)
			result.Buckets = append(result.Buckets, mapped)
			if result.LastAggregatedAt == nil || mapped.CapturedAt.After(*result.LastAggregatedAt) {
				result.LastAggregatedAt = radarServiceTimePointer(mapped.CapturedAt)
			}
		}

		sort.SliceStable(result.Buckets, func(i, j int) bool {
			left, right := result.Buckets[i], result.Buckets[j]
			if left.Platform != right.Platform {
				return left.Platform < right.Platform
			}
			if left.DisplayName != right.DisplayName {
				return left.DisplayName < right.DisplayName
			}
			return left.BucketKey < right.BucketKey
		})
		if len(result.Buckets) > 0 {
			result.Stale = false
			for _, bucket := range result.Buckets {
				if bucket.Stale {
					result.Stale = true
					break
				}
			}
		}
		return result, true, radarServiceEarliestDeadline(deadlines...), nil
	}, func(value any) any {
		v, ok := value.(*QuotaRadarLatestDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceQuotaLatest(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*QuotaRadarLatestDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

// GetQuotaBucketsTrend returns one validated bucket's cached history. The
// service validates canonical syntax before repository access and requires an
// active index membership before selecting the bucket's Redis trend key.
func (s *RadarService) GetQuotaBucketsTrend(ctx context.Context, bucketKey string, days int) (*QuotaTrendDTO, error) {
	platform, planTier, err := ParseRadarBucketKey(bucketKey)
	if err != nil || !isSupportedRadarQuotaPlanTier(platform, planTier) || days < 1 || days > 7 {
		return nil, ErrInvalidRadarQuery
	}

	key := radarServiceCacheKey{method: "quota_buckets_trend", bucket: bucketKey, days: days}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, now time.Time) (any, bool, *time.Time, error) {
		result := &QuotaTrendDTO{
			BucketKey:  bucketKey,
			Days:       days,
			DataPoints: make([]QuotaTrendPointDTO, 0),
			Stale:      true,
		}
		bucketKeys, err := s.repo.ListBucketKeys(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}
		indexed := false
		for _, indexedBucketKey := range bucketKeys {
			if indexedBucketKey == bucketKey {
				indexed = true
				break
			}
		}
		if !indexed {
			return result, false, nil, nil
		}

		anchor := now.UTC()
		since := anchor.AddDate(0, 0, -days)
		snapshots, err := s.repo.GetBucketTrend(loadCtx, bucketKey, since)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if errors.Is(err, ErrRadarCacheMiss) {
			snapshots = nil
		} else if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}

		result.DataPoints = make([]QuotaTrendPointDTO, 0, len(snapshots))
		var latestCapturedAt *time.Time
		for index := range snapshots {
			snapshot := snapshots[index]
			if snapshot.BucketKey != bucketKey || snapshot.Platform != platform || snapshot.PlanTier != planTier ||
				ValidateRadarBucketSnapshot(snapshot) != nil {
				continue
			}
			if snapshot.PrivacyThreshold < s.publicMinBucketAccounts {
				continue
			}
			timestamp := snapshot.CapturedAt.UTC()
			result.DataPoints = append(result.DataPoints, QuotaTrendPointDTO{
				Timestamp: timestamp,
				FiveHour:  radarQuotaTrendWindow(snapshot.FiveHour),
				SevenDay:  radarQuotaTrendWindow(snapshot.SevenDay),
			})
			if latestCapturedAt == nil || timestamp.After(*latestCapturedAt) {
				latestCapturedAt = radarServiceTimePointer(timestamp)
			}
		}
		sort.SliceStable(result.DataPoints, func(i, j int) bool {
			return result.DataPoints[i].Timestamp.Before(result.DataPoints[j].Timestamp)
		})

		var freshnessDeadline *time.Time
		if latestCapturedAt != nil {
			result.Stale, freshnessDeadline = radarQuotaFreshness(anchor, s.quotaStaleThreshold, *latestCapturedAt)
		}
		return result, len(result.DataPoints) > 0, freshnessDeadline, nil
	}, func(value any) any {
		v, ok := value.(*QuotaTrendDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceQuotaTrend(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*QuotaTrendDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

func radarQuotaFreshness(now time.Time, threshold time.Duration, capturedAt time.Time) (bool, *time.Time) {
	now = now.UTC()
	capturedAt = capturedAt.UTC()
	if capturedAt.IsZero() || capturedAt.After(now) {
		return true, nil
	}
	deadline := capturedAt.Add(threshold).UTC()
	if !now.Before(deadline) {
		return true, nil
	}
	return false, &deadline
}

func radarQuotaTrendWindow(window *WindowStatsDTO) *QuotaTrendWindowDTO {
	if window == nil {
		return nil
	}
	return &QuotaTrendWindowDTO{
		AvgUtilization:      window.AvgUtilization,
		AvgCost:             window.AvgCost,
		InferredLimitUSD:    cloneRadarFloat(window.InferredLimitUSD),
		SampleSize:          window.SampleSize,
		InferenceConfidence: window.InferenceConfidence,
	}
}

// GetDegradationLatest returns the latest configured Artificial Analysis
// models and the LMArena top five.
func (s *RadarService) GetDegradationLatest(ctx context.Context) (*DegradationLatestDTO, error) {
	key := radarServiceCacheKey{method: "degradation_latest"}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, _ time.Time) (any, bool, *time.Time, error) {
		cacheable := true
		metas, err := s.repo.ListSourceMeta(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			metas = nil
			cacheable = false
		}

		availableModels := make([]DegradationModelDTO, 0)
		var intelligenceIndexVersion *float64
		aaUsable := false
		aaCacheable := true
		var aaMeta SourceFetchMeta
		var aaMetaOK bool
		if s.aaConfigured {
			availableModels, intelligenceIndexVersion, aaUsable, aaCacheable, err = s.readArtificialAnalysisModels(loadCtx)
			if err != nil {
				return nil, false, nil, err
			}
			aaMeta, aaMetaOK = metas[RadarSourceAA]
		}
		arena, arenaUsable, arenaCacheable, err := s.readLMArenaLocally(loadCtx)
		if err != nil {
			return nil, false, nil, err
		}
		topCount := len(arena.Leaderboard)
		if topCount > 5 {
			topCount = 5
		}
		topFive := cloneRadarServiceLMArenaEntries(arena.Leaderboard[:topCount])

		now := s.clock.Now().UTC()
		arenaMeta, arenaMetaOK := metas[RadarSourceLMArena]
		defaultCount := min(len(availableModels), artificialAnalysisAutoModelLimit)
		defaultModels := cloneRadarServiceDegradationModels(availableModels[:defaultCount])
		defaultSlugs := make([]string, defaultCount)
		for i := range defaultModels {
			defaultSlugs[i] = defaultModels[i].Slug
		}
		result := &DegradationLatestDTO{
			Models:                   defaultModels,
			AvailableModels:          cloneRadarServiceDegradationModels(availableModels),
			DefaultModelSlugs:        defaultSlugs,
			IntelligenceIndexVersion: cloneRadarFloat(intelligenceIndexVersion),
			LMArenaTop5:              topFive,
			SourcesLastUpdated: map[string]*time.Time{
				"aa":      nil,
				"lmarena": radarServiceMetaLastSuccess(arenaMeta, arenaMetaOK),
			},
		}
		arenaStale, arenaDeadline := radarSourceFreshness(now, s.lmarenaStaleThreshold, arenaMeta, arenaMetaOK, arenaUsable)
		result.Stale = arenaStale
		var aaDeadline *time.Time
		if s.aaConfigured {
			result.SourcesLastUpdated["aa"] = radarServiceMetaLastSuccess(aaMeta, aaMetaOK)
			aaStale, deadline := radarSourceFreshness(now, s.aaModelsStaleThreshold, aaMeta, aaMetaOK, aaUsable)
			aaDeadline = deadline
			result.Stale = result.Stale || aaStale
		}
		return result,
			cacheable && aaCacheable && arenaCacheable,
			radarServiceEarliestDeadline(aaDeadline, arenaDeadline),
			nil
	}, func(value any) any {
		v, ok := value.(*DegradationLatestDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceDegradationLatest(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*DegradationLatestDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

// GetLMArena returns the complete cached public leaderboard.
func (s *RadarService) GetLMArena(ctx context.Context) (*LMArenaDTO, error) {
	key := radarServiceCacheKey{method: "lmarena"}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, _ time.Time) (any, bool, *time.Time, error) {
		metas, err := s.repo.ListSourceMeta(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}
		meta, metaOK := metas[RadarSourceLMArena]

		payload, err := s.repo.GetSourcePayload(loadCtx, RadarSourceLMArena)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if errors.Is(err, ErrRadarCacheMiss) {
			return &LMArenaDTO{
				Leaderboard: make([]LMArenaEntryDTO, 0),
				Stale:       true,
			}, true, nil, nil
		}
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}

		snapshot, err := DecodeLMArena(payload)
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}
		mapped, err := MapLMArena(snapshot)
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}
		now := s.clock.Now().UTC()
		stale, freshnessDeadline := radarSourceFreshness(now, s.lmarenaStaleThreshold, meta, metaOK, true)
		mapped.Stale = stale
		return &mapped, true, freshnessDeadline, nil
	}, func(value any) any {
		v, ok := value.(*LMArenaDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceLMArena(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*LMArenaDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

// GetDataSources returns safe public metadata for configured sources.
func (s *RadarService) GetDataSources(ctx context.Context) ([]DataSourceMetaDTO, error) {
	key := radarServiceCacheKey{method: "data_sources"}
	value, err := s.cached(ctx, key, func(loadCtx context.Context, _ time.Time) (any, bool, *time.Time, error) {
		metas, err := s.repo.ListSourceMeta(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}
		if err != nil {
			return nil, false, nil, ErrRadarUnavailable
		}
		aggregatorState, err := s.aggregatorStateReader.GetRadarAggregatorState(loadCtx)
		if contextErr := radarServiceContextError(loadCtx, err); contextErr != nil {
			return nil, false, nil, contextErr
		}

		now := s.clock.Now().UTC()
		specs := s.dataSourceSpecs()
		result := make([]DataSourceMetaDTO, 0, len(specs)+1)
		deadlines := make([]*time.Time, 0, len(specs)+1)
		for _, spec := range specs {
			meta, ok := metas[spec.source]
			mapped, deadline := radarServiceMapDataSource(now, spec, meta, ok)
			result = append(result, mapped)
			deadlines = append(deadlines, deadline)
		}
		cacheable := true
		aggregator := radarServiceFailedQuotaAggregator(s.quotaAggregatorInterval)
		var deadline *time.Time
		if err == nil && aggregatorState.AggregatorStateValid {
			mapped, mappedDeadline, mapErr := radarServiceMapQuotaAggregator(
				now,
				s.quotaAggregatorInterval,
				s.quotaStaleThreshold,
				aggregatorState,
			)
			if mapErr == nil {
				aggregator = mapped
				deadline = mappedDeadline
			} else {
				cacheable = false
			}
		} else {
			cacheable = false
		}
		result = append(result, aggregator)
		deadlines = append(deadlines, deadline)
		return result, cacheable, radarServiceEarliestDeadline(deadlines...), nil
	}, func(value any) any {
		v, ok := value.([]DataSourceMetaDTO)
		if !ok {
			return value
		}
		return cloneRadarServiceDataSources(v)
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.([]DataSourceMetaDTO)
	if !ok {
		return nil, ErrRadarUnavailable
	}
	return result, nil
}

func (s *RadarService) readArtificialAnalysisModels(ctx context.Context) ([]DegradationModelDTO, *float64, bool, bool, error) {
	payload, err := s.repo.GetSourcePayload(ctx, RadarSourceAA)
	if contextErr := radarServiceContextError(ctx, err); contextErr != nil {
		return nil, nil, false, false, contextErr
	}
	if errors.Is(err, ErrRadarCacheMiss) {
		return make([]DegradationModelDTO, 0), nil, false, true, nil
	}
	if err != nil {
		return make([]DegradationModelDTO, 0), nil, false, false, nil
	}
	models, version, err := DecodeArtificialAnalysisSnapshot(payload)
	if err != nil {
		return make([]DegradationModelDTO, 0), nil, false, false, nil
	}
	var mapped []DegradationModelDTO
	if !isNilRadarPublicModelCatalog(s.catalog) {
		byPlatform, catalogErr := s.catalog.ListPublicPassive(ctx)
		if contextErr := radarServiceContextError(ctx, catalogErr); contextErr != nil {
			return nil, nil, false, false, contextErr
		}
		if catalogErr != nil {
			return make([]DegradationModelDTO, 0), version, false, false, nil
		}
		mapped, err = MatchArtificialAnalysisCatalog(models, byPlatform)
	} else {
		mapped, err = MapArtificialAnalysisModels(models)
	}
	if err != nil {
		return make([]DegradationModelDTO, 0), version, false, false, nil
	}
	return mapped, version, true, true, nil
}

func (s *RadarService) readLMArenaLocally(ctx context.Context) (LMArenaDTO, bool, bool, error) {
	payload, err := s.repo.GetSourcePayload(ctx, RadarSourceLMArena)
	if contextErr := radarServiceContextError(ctx, err); contextErr != nil {
		return LMArenaDTO{}, false, false, contextErr
	}
	if errors.Is(err, ErrRadarCacheMiss) {
		return LMArenaDTO{Leaderboard: make([]LMArenaEntryDTO, 0), Stale: true}, false, true, nil
	}
	if err != nil {
		return LMArenaDTO{Leaderboard: make([]LMArenaEntryDTO, 0), Stale: true}, false, false, nil
	}
	snapshot, err := DecodeLMArena(payload)
	if err != nil {
		return LMArenaDTO{Leaderboard: make([]LMArenaEntryDTO, 0), Stale: true}, false, false, nil
	}
	mapped, err := MapLMArena(snapshot)
	if err != nil {
		return LMArenaDTO{Leaderboard: make([]LMArenaEntryDTO, 0), Stale: true}, false, false, nil
	}
	return mapped, true, true, nil
}

type radarServiceDataSourceSpec struct {
	source     RadarSourceKey
	name       string
	url        string
	interval   time.Duration
	threshold  time.Duration
	configured bool
}

func (s *RadarService) dataSourceSpecs() []radarServiceDataSourceSpec {
	result := make([]radarServiceDataSourceSpec, 0, 9)
	result = append(result, radarServiceDataSourceSpec{
		source:     RadarSourceAA,
		name:       "Artificial Analysis",
		url:        radarServiceArtificialAnalysisPublicURL,
		interval:   s.aaModelsInterval,
		threshold:  s.aaModelsStaleThreshold,
		configured: s.aaConfigured,
	})
	result = append(result,
		radarServiceDataSourceSpec{
			source:     RadarSourceLMArena,
			name:       "LMArena",
			url:        radarServiceLMArenaPublicURL,
			interval:   s.lmarenaInterval,
			threshold:  s.lmarenaStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusClaude,
			name:       "Claude Status",
			url:        claudeStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusOpenAI,
			name:       "OpenAI Status",
			url:        openAIStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusWindsurf,
			name:       "Windsurf Status",
			url:        windsurfStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusDeepSeek,
			name:       "DeepSeek Status",
			url:        deepSeekStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusKimi,
			name:       "Kimi Status",
			url:        kimiStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
		radarServiceDataSourceSpec{
			source:     RadarSourceStatusMiniMaxChina,
			name:       "MiniMax China Status",
			url:        miniMaxChinaStatuspagePublicURL,
			interval:   s.statuspageInterval,
			threshold:  s.healthStaleThreshold,
			configured: true,
		},
	)
	return result
}

func radarServiceMapDataSource(
	now time.Time,
	spec radarServiceDataSourceSpec,
	meta SourceFetchMeta,
	metaOK bool,
) (DataSourceMetaDTO, *time.Time) {
	result := DataSourceMetaDTO{
		Key:      string(spec.source),
		Name:     spec.name,
		URL:      spec.url,
		Interval: radarServiceIntervalString(spec.interval),
		Stale:    true,
	}
	if !spec.configured {
		result.State = DataSourceStateNotConfigured
		result.Stale = false
		return result, nil
	}
	if metaOK {
		if !meta.LastAttemptAt.IsZero() {
			result.LastAttemptAt = radarServiceTimePointer(meta.LastAttemptAt)
		}
		result.LastSuccessAt = radarServiceTimePointerFromPointer(meta.LastSuccessAt)
		result.NextFireAt = radarServiceTimePointerFromPointer(meta.NextFireAt)
		result.HTTPStatus = radarServiceIntPointer(meta.HTTPStatus)
		result.Error = radarServiceSafeErrorPointer(meta.Error)
	}

	var freshnessDeadline *time.Time
	switch {
	case !metaOK:
		result.State = DataSourceStateNeverAttempted
	case meta.Error != nil || meta.LastSuccessAt == nil:
		result.State = DataSourceStateFailed
	default:
		result.State = DataSourceStateHealthy
		result.Stale, freshnessDeadline = radarSourceFreshness(now, spec.threshold, meta, true, true)
	}
	result.IsHealthy = result.State == DataSourceStateHealthy && !result.Stale
	return result, freshnessDeadline
}

// radarServiceMapQuotaAggregator maps the internal shared aggregation ledger
// without pretending it is an HTTP-backed external SourceFetchMeta. The ledger
// stores the scheduler's run-start version, so next_fire_at is derived from the
// actual cadence anchor rather than an invented upstream schedule.
func radarServiceMapQuotaAggregator(
	now time.Time,
	interval time.Duration,
	staleThreshold time.Duration,
	snapshot RadarMetricsSnapshot,
) (DataSourceMetaDTO, *time.Time, error) {
	result := DataSourceMetaDTO{
		Key:      radarServiceQuotaAggregatorSourceKey,
		Name:     radarServiceQuotaAggregatorSourceName,
		URL:      "",
		Interval: radarServiceIntervalString(interval),
		State:    DataSourceStateNeverAttempted,
		Stale:    true,
	}
	lastAttempt := snapshot.AggregatorLastAttemptAt.UTC()
	lastRun := snapshot.AggregatorLastRunAt.UTC()
	lastSuccess := snapshot.AggregatorLastSuccessAt.UTC()
	if lastRun.IsZero() {
		if !lastAttempt.IsZero() || !lastSuccess.IsZero() {
			return DataSourceMetaDTO{}, nil, ErrRadarUnavailable
		}
		return result, nil, nil
	}
	if lastAttempt.IsZero() || lastAttempt.After(lastRun) || interval <= 0 || staleThreshold <= 0 || (!lastSuccess.IsZero() && lastSuccess.After(lastRun)) {
		return DataSourceMetaDTO{}, nil, ErrRadarUnavailable
	}

	result.LastAttemptAt = radarServiceTimePointer(lastAttempt)
	nextFire := snapshot.AggregatorNextFireAt.UTC()
	if nextFire.IsZero() {
		// Backward compatibility for ledgers written before next_fire_at became
		// explicit. Every new scheduled or manual run persists the real deadline.
		nextFire = lastAttempt.Add(interval).UTC()
	}
	result.NextFireAt = &nextFire
	if !lastSuccess.IsZero() {
		result.LastSuccessAt = radarServiceTimePointer(lastSuccess)
	}

	var freshnessDeadline *time.Time
	if !lastSuccess.IsZero() && !lastSuccess.After(now) {
		deadline := lastSuccess.Add(staleThreshold).UTC()
		result.Stale = !now.Before(deadline)
		if !result.Stale {
			freshnessDeadline = &deadline
		}
	}
	if lastSuccess.IsZero() || lastRun.After(lastSuccess) {
		failure := DataSourceErrorCodeAggregation
		result.Error = &failure
		result.State = DataSourceStateFailed
		return result, freshnessDeadline, nil
	}
	result.State = DataSourceStateHealthy
	result.IsHealthy = !result.Stale
	return result, freshnessDeadline, nil
}

func radarServiceFailedQuotaAggregator(interval time.Duration) DataSourceMetaDTO {
	failure := DataSourceErrorCodeAggregation
	return DataSourceMetaDTO{
		Key:       radarServiceQuotaAggregatorSourceKey,
		Name:      radarServiceQuotaAggregatorSourceName,
		URL:       "",
		Interval:  radarServiceIntervalString(interval),
		Error:     &failure,
		State:     DataSourceStateFailed,
		IsHealthy: false,
		Stale:     true,
	}
}

func radarServiceIntervalString(interval time.Duration) string {
	if interval > 0 && interval%time.Hour == 0 {
		return fmt.Sprintf("%dh", interval/time.Hour)
	}
	if interval > 0 && interval%time.Minute == 0 {
		return fmt.Sprintf("%dm", interval/time.Minute)
	}
	return interval.String()
}

func radarServiceSafeErrorPointer(value *DataSourceErrorCode) *DataSourceErrorCode {
	if value == nil {
		return nil
	}
	var safe DataSourceErrorCode
	switch *value {
	case DataSourceErrorCodeNetworkError,
		DataSourceErrorCodeUnauthorized,
		DataSourceErrorCodeRateLimited,
		DataSourceErrorCodeInvalidResponse,
		DataSourceErrorCodeUpstreamError,
		DataSourceErrorCodeAggregation:
		safe = *value
	default:
		safe = DataSourceErrorCodeInvalidResponse
	}
	return &safe
}

func (s *RadarService) cached(
	ctx context.Context,
	key radarServiceCacheKey,
	load func(ctx context.Context, now time.Time) (value any, cacheable bool, freshnessDeadline *time.Time, err error),
	clone func(value any) any,
) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := s.clock.Now().UTC()
	if cached, ok := s.cachedValue(key, now); ok {
		result := clone(cached)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	resultChannel := s.flights.DoChan(key.singleflightKey(), func() (any, error) {
		// Caller cancellation only stops that caller's wait. The shared repository
		// load retains request values so another waiter can still receive and cache
		// the result. If all waiters leave, the load may finish and populate cache,
		// but its independent timeout keeps that completion bounded.
		loadCtx, cancelLoad := context.WithTimeout(context.WithoutCancel(ctx), s.loadTimeout)
		defer cancelLoad()
		loadNow := s.clock.Now().UTC()
		if cached, ok := s.cachedValue(key, loadNow); ok {
			if err := loadCtx.Err(); err != nil {
				return nil, err
			}
			return cached, nil
		}

		loaded, cacheable, freshnessDeadline, err := load(loadCtx, loadNow)
		if s.afterLoad != nil {
			s.afterLoad(key)
		}
		if err != nil {
			return nil, err
		}
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}

		stored := clone(loaded)
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}
		if !cacheable {
			return stored, nil
		}
		writeNow := s.clock.Now().UTC()
		expiresAt := writeNow.Add(s.cacheTTL)
		if freshnessDeadline != nil && freshnessDeadline.Before(expiresAt) {
			expiresAt = freshnessDeadline.UTC()
		}
		if !writeNow.Before(expiresAt) {
			return stored, nil
		}
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}
		entry := &radarServiceCacheEntry{
			expiresAt: expiresAt,
			value:     stored,
		}
		s.cacheMu.Lock()
		s.cache[key] = entry
		s.cacheMu.Unlock()
		if err := loadCtx.Err(); err != nil {
			s.deleteCacheEntryIfCurrent(key, entry)
			return nil, err
		}
		return stored, nil
	})
	if s.afterFlightJoin != nil {
		s.afterFlightJoin(key)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Err != nil {
			return nil, result.Err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return clone(result.Val), nil
	}
}

func (s *RadarService) cachedValue(key radarServiceCacheKey, now time.Time) (any, bool) {
	s.cacheMu.RLock()
	entry, ok := s.cache[key]
	s.cacheMu.RUnlock()
	if !ok {
		return nil, false
	}
	if entry != nil && now.Before(entry.expiresAt) {
		return entry.value, true
	}
	s.deleteCacheEntryIfCurrent(key, entry)
	return nil, false
}

func (s *RadarService) deleteCacheEntryIfCurrent(key radarServiceCacheKey, observed *radarServiceCacheEntry) {
	s.cacheMu.Lock()
	// Pointer identity keeps a stale reader from deleting a newer replacement.
	if current, ok := s.cache[key]; ok && current == observed {
		delete(s.cache, key)
	}
	s.cacheMu.Unlock()
}

func (key radarServiceCacheKey) singleflightKey() string {
	return fmt.Sprintf("%s\x00%s\x00%d", key.method, key.bucket, key.days)
}

func radarServiceContextError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func radarSourceFreshness(
	now time.Time,
	threshold time.Duration,
	meta SourceFetchMeta,
	metaOK bool,
	payloadUsable bool,
) (bool, *time.Time) {
	if !payloadUsable || !metaOK || meta.LastSuccessAt == nil || meta.Error != nil {
		return true, nil
	}
	lastSuccess := meta.LastSuccessAt.UTC()
	if lastSuccess.IsZero() || lastSuccess.After(now) {
		return true, nil
	}
	deadline := lastSuccess.Add(threshold).UTC()
	if !now.Before(deadline) {
		return true, nil
	}
	return false, &deadline
}

func latestRadarSourceSuccess(metas map[RadarSourceKey]SourceFetchMeta, sources []RadarSourceKey) *time.Time {
	var latest *time.Time
	for _, source := range sources {
		meta, ok := metas[source]
		if !ok || meta.LastSuccessAt == nil || meta.LastSuccessAt.IsZero() {
			continue
		}
		candidate := meta.LastSuccessAt.UTC()
		if latest == nil || candidate.After(*latest) {
			latest = &candidate
		}
	}
	return latest
}

func radarServiceEarliestDeadline(deadlines ...*time.Time) *time.Time {
	var earliest *time.Time
	for _, deadline := range deadlines {
		if deadline == nil {
			continue
		}
		candidate := deadline.UTC()
		if earliest == nil || candidate.Before(*earliest) {
			earliest = &candidate
		}
	}
	return earliest
}

func cloneRadarServiceHealth(input []ServiceHealthDTO) []ServiceHealthDTO {
	result := make([]ServiceHealthDTO, len(input))
	for index := range input {
		result[index] = cloneServiceHealthDTO(input[index])
	}
	return result
}

func cloneRadarServiceQuotaLatest(input *QuotaRadarLatestDTO) *QuotaRadarLatestDTO {
	if input == nil {
		return nil
	}
	result := &QuotaRadarLatestDTO{
		Buckets:             make([]BucketSnapshotDTO, len(input.Buckets)),
		LastAggregatedAt:    radarServiceTimePointerFromPointer(input.LastAggregatedAt),
		SampleSizeWarnBelow: input.SampleSizeWarnBelow,
		Stale:               input.Stale,
	}
	for index := range input.Buckets {
		result.Buckets[index] = cloneRadarServiceBucketSnapshot(input.Buckets[index])
	}
	return result
}

func cloneRadarServiceBucketSnapshot(input BucketSnapshotDTO) BucketSnapshotDTO {
	result := input
	result.FiveHour = cloneRadarServiceWindowStats(input.FiveHour)
	result.SevenDay = cloneRadarServiceWindowStats(input.SevenDay)
	result.SevenDaySonnet = cloneRadarServiceModelWindowStats(input.SevenDaySonnet)
	result.SevenDayFable = cloneRadarServiceModelWindowStats(input.SevenDayFable)
	result.ModelBreakdown5h = cloneRadarServiceModelBreakdown(input.ModelBreakdown5h)
	result.ModelBreakdown7d = cloneRadarServiceModelBreakdown(input.ModelBreakdown7d)
	return result
}

func cloneRadarServiceWindowStats(input *WindowStatsDTO) *WindowStatsDTO {
	if input == nil {
		return nil
	}
	result := *input
	result.InferredLimitUSD = cloneRadarFloat(input.InferredLimitUSD)
	result.InferredStdev = cloneRadarFloat(input.InferredStdev)
	if input.InferenceRejectReason != nil {
		value := *input.InferenceRejectReason
		result.InferenceRejectReason = &value
	}
	return &result
}

func cloneRadarServiceModelWindowStats(input *ModelWindowStatsDTO) *ModelWindowStatsDTO {
	if input == nil {
		return nil
	}
	result := *input
	return &result
}

func cloneRadarServiceModelBreakdown(input []ModelCostBreakdownDTO) []ModelCostBreakdownDTO {
	return append(make([]ModelCostBreakdownDTO, 0, len(input)), input...)
}

func cloneRadarServiceQuotaTrend(input *QuotaTrendDTO) *QuotaTrendDTO {
	if input == nil {
		return nil
	}
	result := &QuotaTrendDTO{
		BucketKey:  input.BucketKey,
		Days:       input.Days,
		DataPoints: make([]QuotaTrendPointDTO, len(input.DataPoints)),
		Stale:      input.Stale,
	}
	for index := range input.DataPoints {
		result.DataPoints[index] = input.DataPoints[index]
		result.DataPoints[index].FiveHour = cloneRadarServiceQuotaTrendWindow(input.DataPoints[index].FiveHour)
		result.DataPoints[index].SevenDay = cloneRadarServiceQuotaTrendWindow(input.DataPoints[index].SevenDay)
	}
	return result
}

func cloneRadarServiceQuotaTrendWindow(input *QuotaTrendWindowDTO) *QuotaTrendWindowDTO {
	if input == nil {
		return nil
	}
	result := *input
	result.InferredLimitUSD = cloneRadarFloat(input.InferredLimitUSD)
	return &result
}

func cloneRadarServiceDegradationLatest(input *DegradationLatestDTO) *DegradationLatestDTO {
	if input == nil {
		return nil
	}
	return &DegradationLatestDTO{
		Models:                   cloneRadarServiceDegradationModels(input.Models),
		AvailableModels:          cloneRadarServiceDegradationModels(input.AvailableModels),
		DefaultModelSlugs:        append([]string(nil), input.DefaultModelSlugs...),
		IntelligenceIndexVersion: cloneRadarFloat(input.IntelligenceIndexVersion),
		LMArenaTop5:              cloneRadarServiceLMArenaEntries(input.LMArenaTop5),
		SourcesLastUpdated:       cloneRadarServiceTimeMap(input.SourcesLastUpdated),
		Stale:                    input.Stale,
	}
}

func cloneRadarServiceDegradationModels(input []DegradationModelDTO) []DegradationModelDTO {
	result := make([]DegradationModelDTO, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].IntelligenceIndex = cloneRadarFloat(input[index].IntelligenceIndex)
		result[index].CodingIndex = cloneRadarFloat(input[index].CodingIndex)
		result[index].AgenticIndex = cloneRadarFloat(input[index].AgenticIndex)
		result[index].PriceInputPer1M = cloneRadarFloat(input[index].PriceInputPer1M)
		result[index].PriceOutputPer1M = cloneRadarFloat(input[index].PriceOutputPer1M)
		result[index].LastUpdatedAt = radarServiceTimePointerFromPointer(input[index].LastUpdatedAt)
		result[index].CatalogMatches = append([]DegradationCatalogMatchDTO(nil), input[index].CatalogMatches...)
	}
	return result
}

func cloneRadarServiceLMArenaEntries(input []LMArenaEntryDTO) []LMArenaEntryDTO {
	result := make([]LMArenaEntryDTO, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].Vendor = radarServiceStringPointer(input[index].Vendor)
		result[index].Elo = cloneRadarFloat(input[index].Elo)
		result[index].CILower = cloneRadarFloat(input[index].CILower)
		result[index].CIUpper = cloneRadarFloat(input[index].CIUpper)
		result[index].Votes = cloneRadarInt64(input[index].Votes)
	}
	return result
}

func cloneRadarServiceLMArena(input *LMArenaDTO) *LMArenaDTO {
	if input == nil {
		return nil
	}
	return &LMArenaDTO{
		Leaderboard:   cloneRadarServiceLMArenaEntries(input.Leaderboard),
		TotalVotes:    cloneRadarInt64(input.TotalVotes),
		LastUpdatedAt: radarServiceTimePointerFromPointer(input.LastUpdatedAt),
		FetchedAt:     radarServiceTimePointerFromPointer(input.FetchedAt),
		Stale:         input.Stale,
	}
}

func cloneRadarServiceDataSources(input []DataSourceMetaDTO) []DataSourceMetaDTO {
	result := make([]DataSourceMetaDTO, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].LastAttemptAt = radarServiceTimePointerFromPointer(input[index].LastAttemptAt)
		result[index].LastSuccessAt = radarServiceTimePointerFromPointer(input[index].LastSuccessAt)
		result[index].NextFireAt = radarServiceTimePointerFromPointer(input[index].NextFireAt)
		result[index].HTTPStatus = radarServiceIntPointer(input[index].HTTPStatus)
		result[index].Error = radarServiceSafeErrorPointer(input[index].Error)
	}
	return result
}

func cloneRadarServiceTimeMap(input map[string]*time.Time) map[string]*time.Time {
	result := make(map[string]*time.Time, len(input))
	for key, value := range input {
		result[key] = radarServiceTimePointerFromPointer(value)
	}
	return result
}

func radarServiceMetaLastSuccess(meta SourceFetchMeta, ok bool) *time.Time {
	if !ok {
		return nil
	}
	return radarServiceTimePointerFromPointer(meta.LastSuccessAt)
}

func radarServiceTimePointer(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}

func radarServiceTimePointerFromPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return radarServiceTimePointer(*value)
}

func radarServiceIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func radarServiceStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
