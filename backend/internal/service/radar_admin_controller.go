package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrRadarAdminUnavailable = errors.New("radar admin control unavailable")

type RadarAdminState string

const (
	RadarAdminStateNeverAttempted RadarAdminState = "never_attempted"
	RadarAdminStateHealthy        RadarAdminState = "healthy"
	RadarAdminStateFailed         RadarAdminState = "failed"
)

type RadarAdminSourceStatus struct {
	Key           string               `json:"key"`
	Status        RadarAdminState      `json:"status"`
	Stale         bool                 `json:"stale"`
	LastAttemptAt *time.Time           `json:"last_attempt_at"`
	LastSuccessAt *time.Time           `json:"last_success_at"`
	LastFailureAt *time.Time           `json:"last_failure_at"`
	NextFireAt    *time.Time           `json:"next_fire_at"`
	HTTPStatus    *int                 `json:"http_status"`
	Error         *DataSourceErrorCode `json:"error"`
}

type RadarAdminStatus struct {
	Enabled    bool                     `json:"enabled"`
	Sources    []RadarAdminSourceStatus `json:"sources"`
	Aggregator RadarAdminSourceStatus   `json:"aggregator"`
}

type RadarAdminSettings struct {
	Enabled bool `json:"enabled"`
}

type RadarAdminRefreshStatus string

const (
	RadarAdminRefreshTriggered RadarAdminRefreshStatus = "triggered"
	RadarAdminRefreshCoalesced RadarAdminRefreshStatus = "coalesced"
)

type RadarAdminRefreshResult struct {
	RefreshID string                  `json:"refresh_id"`
	Status    RadarAdminRefreshStatus `json:"status"`
	Tasks     []string                `json:"tasks"`
}

type RadarAdminAuditContext struct {
	AdminUserID int64
	RequestID   string
}

type radarAdminAggregatorStateReader interface {
	GetRadarAggregatorState(context.Context) (RadarMetricsSnapshot, error)
}

type RadarAdminController struct {
	repo                   RadarCacheRepository
	aggregatorRepo         radarAdminAggregatorStateReader
	settings               *SettingService
	logger                 *slog.Logger
	now                    func() time.Time
	healthStaleThreshold   time.Duration
	aaModelsStaleThreshold time.Duration
	lmarenaStaleThreshold  time.Duration
	quotaStaleThreshold    time.Duration

	mu     sync.RWMutex
	runner *RadarRunner
}

func NewRadarAdminController(cfg *config.Config, repo RadarCacheRepository, settings *SettingService) (*RadarAdminController, error) {
	return newRadarAdminController(cfg, repo, settings, slog.Default())
}

func newRadarAdminController(cfg *config.Config, repo RadarCacheRepository, settings *SettingService, logger *slog.Logger) (*RadarAdminController, error) {
	if cfg == nil || isNilRadarCacheRepository(repo) || settings == nil || logger == nil {
		return nil, errors.New("radar admin controller requires dependencies")
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, errors.New("radar admin controller requires valid radar config")
	}
	aggregatorRepo, ok := repo.(radarAdminAggregatorStateReader)
	if !ok || isNilRadarAdminAggregatorReader(aggregatorRepo) {
		return nil, errors.New("radar admin controller requires aggregator state reader")
	}
	return &RadarAdminController{
		repo:                   repo,
		aggregatorRepo:         aggregatorRepo,
		settings:               settings,
		logger:                 logger,
		now:                    time.Now,
		healthStaleThreshold:   time.Duration(cfg.Radar.HealthStaleThresholdMinutes) * time.Minute,
		aaModelsStaleThreshold: time.Duration(cfg.Radar.ArtificialAnalysisModelsStaleThresholdMinutes) * time.Minute,
		lmarenaStaleThreshold:  time.Duration(cfg.Radar.LMArenaStaleThresholdMinutes) * time.Minute,
		quotaStaleThreshold:    time.Duration(cfg.Radar.QuotaStaleThresholdMinutes) * time.Minute,
	}, nil
}

func isNilRadarAdminAggregatorReader(reader radarAdminAggregatorStateReader) bool {
	if reader == nil {
		return true
	}
	v := reflect.ValueOf(reader)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// BindRunner connects the late, fallible lifecycle constructor to the controller
// already held by the singleton HTTP handler. It may succeed exactly once.
func (c *RadarAdminController) BindRunner(runner *RadarRunner) error {
	if c == nil || runner == nil {
		return ErrRadarAdminUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runner != nil {
		return errors.New("radar admin controller runner already bound")
	}
	c.runner = runner
	return nil
}

func (c *RadarAdminController) boundRunner() *RadarRunner {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.runner
}

func (c *RadarAdminController) GetStatus(ctx context.Context) (*RadarAdminStatus, error) {
	if c == nil || c.settings == nil || isNilRadarCacheRepository(c.repo) || isNilRadarAdminAggregatorReader(c.aggregatorRepo) {
		return nil, ErrRadarAdminUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	metas, err := c.repo.ListSourceMeta(ctx)
	if err != nil {
		return nil, ErrRadarAdminUnavailable
	}
	aggregator, err := c.aggregatorRepo.GetRadarAggregatorState(ctx)
	if err != nil || !aggregator.AggregatorStateValid {
		return nil, ErrRadarAdminUnavailable
	}

	sources := []RadarSourceKey{}
	if runner := c.boundRunner(); runner != nil {
		sources = runner.configuredSources()
	}
	result := &RadarAdminStatus{
		Enabled: c.settings.IsRadarEnabled(ctx),
		Sources: make([]RadarAdminSourceStatus, 0, len(sources)),
	}
	now := c.now().UTC()
	for _, source := range sources {
		meta, ok := metas[source]
		result.Sources = append(result.Sources, radarAdminMapSource(now, c.sourceStaleThreshold(source), source, meta, ok))
	}
	result.Aggregator = radarAdminMapAggregator(now, c.quotaStaleThreshold, aggregator)
	return result, nil
}

func (c *RadarAdminController) sourceStaleThreshold(source RadarSourceKey) time.Duration {
	switch source {
	case RadarSourceAA:
		return c.aaModelsStaleThreshold
	case RadarSourceLMArena:
		return c.lmarenaStaleThreshold
	case RadarSourceStatusClaude, RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf, RadarSourceStatusDeepSeek, RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxGlobal, RadarSourceStatusMiniMaxChina:
		return c.healthStaleThreshold
	default:
		return 0
	}
}

func radarAdminMapSource(now time.Time, staleThreshold time.Duration, source RadarSourceKey, meta SourceFetchMeta, ok bool) RadarAdminSourceStatus {
	result := RadarAdminSourceStatus{Key: string(source), Status: RadarAdminStateNeverAttempted}
	if !ok {
		return result
	}
	result.LastAttemptAt = radarAdminTimePointer(meta.LastAttemptAt)
	result.LastSuccessAt = radarAdminTimePointerFromPointer(meta.LastSuccessAt)
	result.NextFireAt = radarAdminTimePointerFromPointer(meta.NextFireAt)
	result.HTTPStatus = radarServiceIntPointer(meta.HTTPStatus)
	result.Error = radarServiceSafeErrorPointer(meta.Error)
	result.Stale = radarAdminIsStale(now, staleThreshold, meta.LastSuccessAt)
	if meta.Error != nil || meta.LastSuccessAt == nil {
		result.Status = RadarAdminStateFailed
		result.LastFailureAt = radarAdminTimePointer(meta.LastAttemptAt)
		if result.Error == nil {
			safe := DataSourceErrorCodeInvalidResponse
			result.Error = &safe
		}
		return result
	}
	result.Status = RadarAdminStateHealthy
	return result
}

func radarAdminMapAggregator(now time.Time, staleThreshold time.Duration, snapshot RadarMetricsSnapshot) RadarAdminSourceStatus {
	result := RadarAdminSourceStatus{Key: radarServiceQuotaAggregatorSourceKey, Status: RadarAdminStateNeverAttempted}
	if snapshot.AggregatorLastRunAt.IsZero() {
		return result
	}
	result.LastAttemptAt = radarAdminTimePointer(snapshot.AggregatorLastAttemptAt)
	result.LastSuccessAt = radarAdminTimePointer(snapshot.AggregatorLastSuccessAt)
	result.NextFireAt = radarAdminTimePointer(snapshot.AggregatorNextFireAt)
	result.Stale = radarAdminIsStale(now, staleThreshold, radarAdminTimePointer(snapshot.AggregatorLastSuccessAt))
	if snapshot.AggregatorLastSuccessAt.IsZero() || snapshot.AggregatorLastRunAt.After(snapshot.AggregatorLastSuccessAt) {
		result.Status = RadarAdminStateFailed
		result.LastFailureAt = radarAdminTimePointer(snapshot.AggregatorLastAttemptAt)
		safe := DataSourceErrorCodeAggregation
		result.Error = &safe
		return result
	}
	result.Status = RadarAdminStateHealthy
	return result
}

func radarAdminIsStale(now time.Time, threshold time.Duration, lastSuccessAt *time.Time) bool {
	if threshold <= 0 || lastSuccessAt == nil || lastSuccessAt.IsZero() {
		return false
	}
	return !now.UTC().Before(lastSuccessAt.UTC().Add(threshold))
}

func radarAdminTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func radarAdminTimePointerFromPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return radarAdminTimePointer(*value)
}

func (c *RadarAdminController) SetEnabled(ctx context.Context, enabled bool, audit RadarAdminAuditContext) (*RadarAdminSettings, error) {
	if c == nil || c.settings == nil {
		return nil, ErrRadarAdminUnavailable
	}
	if err := c.settings.SetRadarEnabled(ctx, enabled); err != nil {
		c.auditSettings(audit, enabled, "failed")
		return nil, ErrRadarAdminUnavailable
	}
	effective := c.settings.IsRadarEnabled(ctx)
	c.auditSettings(audit, effective, "succeeded")
	return &RadarAdminSettings{Enabled: effective}, nil
}

func (c *RadarAdminController) TriggerRefresh(audit RadarAdminAuditContext) (*RadarAdminRefreshResult, error) {
	refreshID, err := newRadarAdminRefreshID()
	if err != nil {
		return nil, ErrRadarAdminUnavailable
	}
	runner := c.boundRunner()
	if runner == nil {
		return nil, ErrRadarAdminUnavailable
	}
	triggered, tasks, err := runner.TriggerManualRefresh()
	if err != nil {
		c.auditRefresh(audit, refreshID, tasks, "unavailable")
		return nil, ErrRadarAdminUnavailable
	}
	status := RadarAdminRefreshCoalesced
	if triggered {
		status = RadarAdminRefreshTriggered
	}
	c.auditRefresh(audit, refreshID, tasks, string(status))
	return &RadarAdminRefreshResult{RefreshID: refreshID, Status: status, Tasks: tasks}, nil
}

func newRadarAdminRefreshID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "refresh-" + hex.EncodeToString(random[:]), nil
}

func (c *RadarAdminController) auditSettings(audit RadarAdminAuditContext, enabled bool, status string) {
	c.logger.Info("radar_admin_settings_audit", "admin_user_id", audit.AdminUserID, "enabled", enabled, "status", status, "request_id", safeRadarAdminRequestID(audit.RequestID))
}

func (c *RadarAdminController) auditRefresh(audit RadarAdminAuditContext, refreshID string, tasks []string, status string) {
	c.logger.Info("radar_admin_refresh_audit", "admin_user_id", audit.AdminUserID, "refresh_id", refreshID, "tasks", append([]string(nil), tasks...), "status", status, "request_id", safeRadarAdminRequestID(audit.RequestID))
}

func safeRadarAdminRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return "unknown"
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return "unknown"
		}
	}
	return value
}
