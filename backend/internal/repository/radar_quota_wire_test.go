package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestNewUsageLogRepositoryProvidesBroadAndRadarBatchContracts(t *testing.T) {
	repository := NewUsageLogRepository(nil, nil)
	var broad service.UsageLogRepository = repository
	var narrow service.RadarQuotaBatchReader = repository
	if broad == nil || narrow == nil {
		t.Fatal("usage log repository must provide both static contracts")
	}
}
