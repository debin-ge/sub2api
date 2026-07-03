package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

// BenchmarkRunnerService is the background daemon: every minute it triggers due
// schedules and advances any runnable runs. It is the bridge between the cron
// schedules the admin configures and actual execution.
type BenchmarkRunnerService struct {
	scheduleSvc *BenchmarkScheduleService
	processor   *BenchmarkProcessor
	cfg         *config.Config

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
	running   sync.Mutex
}

func NewBenchmarkRunnerService(scheduleSvc *BenchmarkScheduleService, processor *BenchmarkProcessor, cfg *config.Config) *BenchmarkRunnerService {
	return &BenchmarkRunnerService{
		scheduleSvc: scheduleSvc,
		processor:   processor,
		cfg:         cfg,
	}
}

func (s *BenchmarkRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}
		c := cron.New(cron.WithParser(benchmarkScheduleCronParser), cron.WithLocation(loc))
		if _, err := c.AddFunc("* * * * *", func() { s.tick() }); err != nil {
			logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] started (tick=every minute)")
	})
}

func (s *BenchmarkRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] cron stop timed out")
			}
		}
	})
}

func (s *BenchmarkRunnerService) tick() {
	// Skip if a previous tick is still working (runs can take a while).
	if !s.running.TryLock() {
		return
	}
	defer s.running.Unlock()

	// Delay 15s so execution lands mid-minute, away from other :00 jobs.
	time.Sleep(15 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	now := time.Now()
	if s.scheduleSvc != nil {
		if triggered, err := s.scheduleSvc.TriggerDue(ctx, now); err != nil {
			logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] TriggerDue error: %v", err)
		} else if triggered > 0 {
			logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] triggered %d due schedules", triggered)
		}
	}

	if s.processor != nil {
		// ProcessDue advances each runnable run by one batch; loop until no run
		// makes progress so runs drain to completion within this tick.
		for i := 0; i < 2000; i++ {
			processed, err := s.processor.ProcessDue(ctx, BenchmarkProcessOptions{})
			if err != nil {
				logger.LegacyPrintf("service.benchmark_runner", "[BenchmarkRunner] ProcessDue error: %v", err)
				break
			}
			if processed == 0 {
				break
			}
		}
	}
}
