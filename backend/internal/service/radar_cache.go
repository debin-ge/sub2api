package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var (
	// ErrRadarCacheMiss reports that a requested radar value is not present.
	ErrRadarCacheMiss = errors.New("radar cache miss")
	// ErrInvalidRadarCacheKey reports an input that cannot safely form part of a
	// Redis key. Callers may use errors.Is to classify wrapped validation errors.
	ErrInvalidRadarCacheKey = errors.New("invalid radar cache key")
)

// RadarSourceKey is the canonical internal identifier for an external radar
// data source. Dynamic Artificial Analysis performance sources use
// "aa_perf:<model-slug>" and must be created with RadarAAPerformanceSource.
type RadarSourceKey string

const (
	RadarSourceAA           RadarSourceKey = "aa"
	RadarSourceLMArena      RadarSourceKey = "lmarena"
	RadarSourceStatusClaude RadarSourceKey = "status_claude"
	RadarSourceStatusOpenAI RadarSourceKey = "status_openai"
)

// RadarAAPerformanceSource returns the canonical source key for one Artificial
// Analysis model performance feed.
func RadarAAPerformanceSource(modelSlug string) (RadarSourceKey, error) {
	if !isSafeRadarModelSlug(modelSlug) {
		return "", fmt.Errorf("%w: invalid model slug", ErrInvalidRadarCacheKey)
	}
	return RadarSourceKey("aa_perf:" + modelSlug), nil
}

func isSafeRadarModelSlug(value string) bool {
	return config.IsValidRadarModelSlug(value)
}

// ParseRadarBucketKey validates and splits the only bucket-key shape that may
// reach Radar Redis storage. Keeping this parser in the service contract lets
// HTTP and repository boundaries enforce the same canonical allowlist.
func ParseRadarBucketKey(bucketKey string) (string, string, error) {
	platform, planTier, found := strings.Cut(bucketKey, "/")
	if !found || !isCanonicalRadarBucketPlatform(platform) || !isSafeRadarPlanTier(planTier) {
		return "", "", fmt.Errorf("%w: invalid bucket key", ErrInvalidRadarCacheKey)
	}
	return platform, planTier, nil
}

func isCanonicalRadarBucketPlatform(platform string) bool {
	switch platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformAntigravity:
		return true
	default:
		return false
	}
}

func isSafeRadarPlanTier(value string) bool {
	if len(value) == 0 || len(value) > 64 || !isRadarBucketLowerAlphaNumeric(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !isRadarBucketLowerAlphaNumeric(value[index]) && value[index] != '.' && value[index] != '_' && value[index] != '-' {
			return false
		}
	}
	return true
}

func isRadarBucketLowerAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

// SourceFetchMeta is the internal scheduling/freshness state for one source.
// Error deliberately contains only a safe classification and no raw error text.
type SourceFetchMeta struct {
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	NextFireAt    *time.Time `json:"next_fire_at"`
	// CadenceVersion is empty only for legacy metadata or an unassigned
	// scheduled candidate whose Redis Advance failed. A winning commit
	// atomically allocates the next per-source sequence in that case.
	CadenceVersion string               `json:"cadence_version,omitempty"`
	HTTPStatus     *int                 `json:"http_status"`
	Error          *DataSourceErrorCode `json:"error,omitempty"`
}

type RadarSourceCadence struct {
	NextFireAt time.Time
	Version    string
}

// RadarCacheRepository stores quota snapshots, external source payloads and
// source scheduling metadata in Redis.
type RadarCacheRepository interface {
	// AppendBucketSnapshot treats a snapshot older than the retention window
	// anchored at the newest stored capture as a successful no-op. Accepted
	// older points do not refresh TTL; equal-score corrections do.
	AppendBucketSnapshot(ctx context.Context, snapshot BucketSnapshotDTO) error
	ListBucketKeys(ctx context.Context) ([]string, error)
	GetLatestBucket(ctx context.Context, bucketKey string) (*BucketSnapshotDTO, error)
	GetBucketTrend(ctx context.Context, bucketKey string, since time.Time) ([]BucketSnapshotDTO, error)
	// SetSourcePayload is a low-level payload-only operation for repair and
	// testing. Production successful fetches must use CommitSourceSuccess.
	SetSourcePayload(ctx context.Context, source RadarSourceKey, payload []byte, retention time.Duration) error
	GetSourcePayload(ctx context.Context, source RadarSourceKey) ([]byte, error)
	// CommitSourceSuccess atomically publishes a payload and its matching meta;
	// it returns false without mutation when a newer attempt is already stored.
	CommitSourceSuccess(ctx context.Context, source RadarSourceKey, payload []byte, retention time.Duration, meta SourceFetchMeta) (bool, error)
	// CommitSourceFailure atomically advances failure metadata without touching
	// payload. When metadata already exists, its last_success_at is preserved
	// exactly; an older failure is a successful no-op. At an equal millisecond
	// version, an already stored success takes precedence over a later failure,
	// while CommitSourceSuccess may still replace an equal-version failure.
	CommitSourceFailure(ctx context.Context, source RadarSourceKey, meta SourceFetchMeta) (bool, error)
	// SetSourceMeta advances metadata monotonically and ignores older attempts.
	// It must not be paired with SetSourcePayload for successful fetches;
	// production success paths must use CommitSourceSuccess.
	SetSourceMeta(ctx context.Context, source RadarSourceKey, meta SourceFetchMeta) error
	ListSourceMeta(ctx context.Context) (map[RadarSourceKey]SourceFetchMeta, error)
	// TryLock uses ttl only as crash safety. Bounded job runners must choose a
	// ttl longer than their worst-case runtime and defer ReleaseLock; lock renewal
	// is intentionally outside the v1 contract.
	TryLock(ctx context.Context, task, owner string, ttl time.Duration) (bool, error)
	// ReleaseLock deletes a lock only while owner still matches its stored value.
	ReleaseLock(ctx context.Context, task, owner string) error
}

// RadarSourceCadenceRepository owns the Redis-authoritative per-source
// sequence. Advance atomically allocates the next version; callers must never
// manufacture versions from process-local clocks.
type RadarSourceCadenceRepository interface {
	AdvanceSourceNextFire(ctx context.Context, source RadarSourceKey, nextFireAt time.Time) (RadarSourceCadence, error)
	GetSourceCadence(ctx context.Context, source RadarSourceKey) (RadarSourceCadence, error)
}
