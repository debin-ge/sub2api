package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type ModelCatalogRefreshPlatformSummary struct {
	Scanned   int
	Succeeded int
	Failed    int
}

type ModelCatalogRefreshSummary struct {
	ByPlatform map[string]ModelCatalogRefreshPlatformSummary
	Duration   time.Duration
}

// RefreshAll refreshes every schedulable account that supports live discovery.
func (s *ModelCatalogService) RefreshAll(ctx context.Context) (summary ModelCatalogRefreshSummary) {
	summary = ModelCatalogRefreshSummary{ByPlatform: make(map[string]ModelCatalogRefreshPlatformSummary)}
	startedAt := time.Now()
	if s != nil {
		startedAt = s.currentTime()
	}
	defer func() {
		finishedAt := time.Now()
		if s != nil {
			finishedAt = s.currentTime()
		}
		summary.Duration = finishedAt.Sub(startedAt)
	}()
	if s == nil || s.accountRepo == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	accounts, err := s.accountRepo.ListSchedulable(ctx)
	if err != nil {
		slog.Warn("model_catalog_refresh_scan_failed", "error", err)
		return summary
	}
	eligible := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if account.Status != StatusActive || !account.Schedulable || !accountRequiresLiveCatalog(&account) {
			continue
		}
		eligible = append(eligible, account)
		platformSummary := summary.ByPlatform[account.Platform]
		platformSummary.Scanned++
		summary.ByPlatform[account.Platform] = platformSummary
	}
	if len(eligible) == 0 {
		return summary
	}

	type refreshResult struct {
		account Account
		err     error
	}
	jobs := make(chan Account)
	results := make(chan refreshResult, len(eligible))
	workerCount := s.maxConcurrency()
	if workerCount > len(eligible) {
		workerCount = len(eligible)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for account := range jobs {
				_, refreshErr := s.RefreshAccount(ctx, &account)
				results <- refreshResult{account: account, err: refreshErr}
			}
		}()
	}
	for i := range eligible {
		jobs <- eligible[i]
	}
	close(jobs)
	workers.Wait()
	close(results)

	for result := range results {
		platformSummary := summary.ByPlatform[result.account.Platform]
		if result.err == nil {
			platformSummary.Succeeded++
		} else {
			platformSummary.Failed++
			slog.Warn(
				"model_catalog_account_refresh_failed",
				"account_id", result.account.ID,
				"platform", result.account.Platform,
				"error_kind", modelCatalogRefreshErrorKind(result.err),
			)
		}
		summary.ByPlatform[result.account.Platform] = platformSummary
	}
	return summary
}

func (s *ModelCatalogService) maxConcurrency() int {
	if s != nil && s.cfg.MaxConcurrency > 0 {
		return s.cfg.MaxConcurrency
	}
	return 5
}

func modelCatalogRefreshErrorKind(err error) string {
	var syncErr *UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		return string(syncErr.Kind)
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, errModelCatalogRefreshBackoff) {
		return "backoff"
	}
	return "unknown"
}

type ModelCatalogRefreshRunner struct {
	service *ModelCatalogService
	cfg     config.ModelCatalogConfig
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	active  atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewModelCatalogRefreshRunner(service *ModelCatalogService, cfg config.ModelCatalogConfig) *ModelCatalogRefreshRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &ModelCatalogRefreshRunner{
		service: service,
		cfg:     cfg,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (r *ModelCatalogRefreshRunner) Start() {
	if r == nil || r.service == nil {
		return
	}
	if !r.active.CompareAndSwap(false, true) {
		return
	}
	go r.loop()
}

func (r *ModelCatalogRefreshRunner) Stop() {
	if r == nil || !r.active.Load() {
		return
	}
	r.once.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		close(r.stop)
	})
	<-r.done
}

func (r *ModelCatalogRefreshRunner) loop() {
	defer close(r.done)
	r.runOnce(r.ctx)

	interval := time.Duration(r.cfg.RefreshIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(r.ctx)
		}
	}
}

func (r *ModelCatalogRefreshRunner) runOnce(ctx context.Context) {
	if r == nil || r.service == nil {
		return
	}
	summary := r.service.RefreshAll(ctx)
	slog.Info("model_catalog_refresh_pass_completed", "by_platform", summary.ByPlatform, "duration", summary.Duration)
}
