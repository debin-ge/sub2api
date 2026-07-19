package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/zeromicro/go-zero/core/collection"
)

func TestProvideTimingWheelService_ReturnsError(t *testing.T) {
	original := newTimingWheel
	t.Cleanup(func() { newTimingWheel = original })

	newTimingWheel = func(_ time.Duration, _ int, _ collection.Execute) (*collection.TimingWheel, error) {
		return nil, errors.New("boom")
	}

	svc, err := ProvideTimingWheelService()
	if err == nil {
		t.Fatalf("期望返回 error，但得到 nil")
	}
	if svc != nil {
		t.Fatalf("期望返回 nil svc，但得到非空")
	}
}

func TestProvideTimingWheelService_Success(t *testing.T) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		t.Fatalf("期望 err 为 nil，但得到: %v", err)
	}
	if svc == nil {
		t.Fatalf("期望 svc 非空，但得到 nil")
	}
	svc.Stop()
}

func TestProvideRadarQuotaAggregatorRequiresStaticDependencies(t *testing.T) {
	cfg := &config.Config{}
	cfg.Radar.PublicMinBucketAccounts = 2
	cfg.Radar.InferMinUtilization = 5
	cfg.Radar.InferMaxStdevRatio = 0.3

	var batchReader RadarQuotaBatchReader
	aggregator, err := ProvideRadarQuotaAggregator(nil, nil, batchReader, nil, cfg)
	if err == nil {
		t.Fatal("expected missing dependency error")
	}
	if aggregator != nil {
		t.Fatal("expected nil aggregator")
	}
}
