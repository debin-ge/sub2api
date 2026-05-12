package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type minimaxRemainsSyncRunnerSvcStub struct {
	calls     int
	batchSize int
}

func (s *minimaxRemainsSyncRunnerSvcStub) SyncBatch(ctx context.Context, batchSize int) ([]MiniMaxRemainsSyncResult, error) {
	s.calls++
	s.batchSize = batchSize
	return nil, nil
}

func TestMiniMaxRemainsSyncRunnerRunOnceCallsServiceWhenEnabled(t *testing.T) {
	svc := &minimaxRemainsSyncRunnerSvcStub{}
	runner := newMiniMaxRemainsSyncRunner(svc, config.GatewayMiniMaxRemainsConfig{
		SyncEnabled:         true,
		SyncIntervalSeconds: 300,
		SyncJitterSeconds:   30,
		BatchSize:           50,
		StaleAfterSeconds:   900,
	})

	runner.runOnce(context.Background())

	if svc.calls != 1 {
		t.Fatalf("SyncAll calls = %d, want 1", svc.calls)
	}
	if svc.batchSize != 50 {
		t.Fatalf("batchSize = %d, want 50", svc.batchSize)
	}
}

func TestMiniMaxRemainsSyncRunnerRunOnceSkipsWhenDisabled(t *testing.T) {
	svc := &minimaxRemainsSyncRunnerSvcStub{}
	runner := newMiniMaxRemainsSyncRunner(svc, config.GatewayMiniMaxRemainsConfig{
		SyncEnabled:         false,
		SyncIntervalSeconds: 300,
	})

	runner.runOnce(context.Background())

	if svc.calls != 0 {
		t.Fatalf("SyncAll calls = %d, want 0", svc.calls)
	}
}
