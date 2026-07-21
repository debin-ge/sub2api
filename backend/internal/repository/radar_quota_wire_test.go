package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestNewUsageLogRepositoryProvidesBroadAndRadarBatchContracts(t *testing.T) {
	repository := NewUsageLogRepository(nil, nil)
	// 编译期契约断言：仓储实现必须同时满足两个 service 接口。
	var _ service.UsageLogRepository = repository
	var _ service.RadarQuotaBatchReader = repository
}
