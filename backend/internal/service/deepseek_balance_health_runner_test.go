package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type deepSeekBalanceHealthRunnerSvcStub struct {
	calls     int
	batchSize int
}

func (s *deepSeekBalanceHealthRunnerSvcStub) CheckBatch(ctx context.Context, batchSize int) ([]DeepSeekBalance, error) {
	s.calls++
	s.batchSize = batchSize
	return nil, nil
}

func TestDeepSeekBalanceHealthRunnerRunOnceCallsServiceWhenEnabled(t *testing.T) {
	svc := &deepSeekBalanceHealthRunnerSvcStub{}
	runner := newDeepSeekBalanceHealthRunner(svc, config.GatewayDeepSeekBalanceConfig{
		CheckEnabled:         true,
		CheckIntervalSeconds: 300,
		CheckJitterSeconds:   30,
		BatchSize:            50,
		StaleAfterSeconds:    900,
	})

	runner.runOnce(context.Background())

	if svc.calls != 1 {
		t.Fatalf("CheckBatch calls = %d, want 1", svc.calls)
	}
	if svc.batchSize != 50 {
		t.Fatalf("batchSize = %d, want 50", svc.batchSize)
	}
}

func TestDeepSeekBalanceHealthRunnerRunOnceSkipsWhenDisabled(t *testing.T) {
	svc := &deepSeekBalanceHealthRunnerSvcStub{}
	runner := newDeepSeekBalanceHealthRunner(svc, config.GatewayDeepSeekBalanceConfig{
		CheckEnabled:         false,
		CheckIntervalSeconds: 300,
	})

	runner.runOnce(context.Background())

	if svc.calls != 0 {
		t.Fatalf("CheckBatch calls = %d, want 0", svc.calls)
	}
}
