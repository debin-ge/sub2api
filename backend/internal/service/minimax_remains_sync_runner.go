package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type miniMaxRemainsSyncRunnerSvc interface {
	SyncBatch(ctx context.Context, batchSize int) ([]MiniMaxRemainsSyncResult, error)
}

type MiniMaxRemainsSyncRunner struct {
	svc    miniMaxRemainsSyncRunnerSvc
	cfg    config.GatewayMiniMaxRemainsConfig
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	active atomic.Bool
}

func NewMiniMaxRemainsSyncRunner(svc *MiniMaxRemainsSyncService, cfg config.GatewayMiniMaxRemainsConfig) *MiniMaxRemainsSyncRunner {
	return newMiniMaxRemainsSyncRunner(svc, cfg)
}

func newMiniMaxRemainsSyncRunner(svc miniMaxRemainsSyncRunnerSvc, cfg config.GatewayMiniMaxRemainsConfig) *MiniMaxRemainsSyncRunner {
	return &MiniMaxRemainsSyncRunner{
		svc:  svc,
		cfg:  cfg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (r *MiniMaxRemainsSyncRunner) Start() {
	if r == nil || r.svc == nil || !r.cfg.SyncEnabled {
		return
	}
	if !r.active.CompareAndSwap(false, true) {
		return
	}
	go r.loop()
}

func (r *MiniMaxRemainsSyncRunner) Stop() {
	if r == nil {
		return
	}
	if r.active.Load() {
		r.once.Do(func() { close(r.stop) })
		<-r.done
	}
}

func (r *MiniMaxRemainsSyncRunner) loop() {
	defer close(r.done)
	r.runOnce(context.Background())

	interval := time.Duration(r.cfg.SyncIntervalSeconds) * time.Second
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

func (r *MiniMaxRemainsSyncRunner) runOnce(ctx context.Context) {
	if r == nil || r.svc == nil || !r.cfg.SyncEnabled {
		return
	}
	if _, err := r.svc.SyncBatch(ctx, r.cfg.BatchSize); err != nil {
		slog.Warn("minimax remains sync run failed", "error", err)
	}
}

func (r *MiniMaxRemainsSyncRunner) sleepJitter() {
	if r == nil || r.cfg.SyncJitterSeconds <= 0 {
		return
	}
	max := time.Duration(r.cfg.SyncJitterSeconds) * time.Second
	if max <= 0 {
		return
	}
	delay := time.Duration(time.Now().UnixNano() % int64(max))
	select {
	case <-time.After(delay):
	case <-r.stop:
	}
}
