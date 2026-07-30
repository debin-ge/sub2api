package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// metadataUsageBillingRepoStub gives handler metadata tests the same durable
// ApplyAndRecord contract used in production without reintroducing the legacy
// billing fallback they are not intended to exercise.
type metadataUsageBillingRepoStub struct {
	service.DurableUsageBillingRepository

	usageLogRepo service.UsageLogRepository
}

func (s *metadataUsageBillingRepoStub) ApplyAndRecord(
	ctx context.Context,
	_ *service.UsageBillingCommand,
	usageLog *service.UsageLog,
) (*service.UsageBillingApplyResult, error) {
	inserted, err := s.usageLogRepo.Create(ctx, usageLog)
	if err != nil {
		return nil, err
	}
	return &service.UsageBillingApplyResult{
		Applied:          false,
		UsageLogRecorded: inserted,
	}, nil
}
