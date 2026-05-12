package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type deepSeekBalanceHealthRunnerSvc interface {
	CheckBatch(ctx context.Context, batchSize int) ([]DeepSeekBalance, error)
}

type DeepSeekBalanceHealthRunner struct {
	svc    deepSeekBalanceHealthRunnerSvc
	cfg    config.GatewayDeepSeekBalanceConfig
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	active atomic.Bool
}

func NewDeepSeekBalanceHealthRunner(svc *DeepSeekBalanceHealthService, cfg config.GatewayDeepSeekBalanceConfig) *DeepSeekBalanceHealthRunner {
	return newDeepSeekBalanceHealthRunner(svc, cfg)
}

func newDeepSeekBalanceHealthRunner(svc deepSeekBalanceHealthRunnerSvc, cfg config.GatewayDeepSeekBalanceConfig) *DeepSeekBalanceHealthRunner {
	return &DeepSeekBalanceHealthRunner{
		svc:  svc,
		cfg:  cfg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (r *DeepSeekBalanceHealthRunner) Start() {
	if r == nil || r.svc == nil || !r.cfg.CheckEnabled {
		return
	}
	if !r.active.CompareAndSwap(false, true) {
		return
	}
	go r.loop()
}

func (r *DeepSeekBalanceHealthRunner) Stop() {
	if r == nil {
		return
	}
	if r.active.Load() {
		r.once.Do(func() { close(r.stop) })
		<-r.done
	}
}

func (r *DeepSeekBalanceHealthRunner) loop() {
	defer close(r.done)
	r.runOnce(context.Background())

	interval := time.Duration(r.cfg.CheckIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.sleepJitter()
			r.runOnce(context.Background())
		}
	}
}

func (r *DeepSeekBalanceHealthRunner) runOnce(ctx context.Context) {
	if r == nil || r.svc == nil || !r.cfg.CheckEnabled {
		return
	}
	if _, err := r.svc.CheckBatch(ctx, r.cfg.BatchSize); err != nil {
		slog.Warn("deepseek balance health run failed", "error", err)
	}
}

func (r *DeepSeekBalanceHealthRunner) sleepJitter() {
	if r == nil || r.cfg.CheckJitterSeconds <= 0 {
		return
	}
	max := time.Duration(r.cfg.CheckJitterSeconds) * time.Second
	if max <= 0 {
		return
	}
	delay := time.Duration(time.Now().UnixNano() % int64(max))
	select {
	case <-time.After(delay):
	case <-r.stop:
	}
}
