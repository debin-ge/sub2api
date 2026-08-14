//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShouldWriteUnsettledUsageLog(t *testing.T) {
	require.False(t, shouldWriteUnsettledUsageLog(
		errors.Join(ErrUsageBillingIntentPending, errors.New("transaction failed")),
		false,
	))
	require.False(t, shouldWriteUnsettledUsageLog(errors.New("ack failed"), true))
	require.True(t, shouldWriteUnsettledUsageLog(errors.New("no durable repository"), false))
}

type strictDurableUsageBillingRepoStub struct {
	DurableUsageBillingRepository

	applyCalls          int
	applyAndRecordCalls int
	ackCalls            int
	result              *UsageBillingApplyResult
	returnNilResult     bool
}

func (s *strictDurableUsageBillingRepoStub) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.applyCalls++
	return s.result, nil
}

func (s *strictDurableUsageBillingRepoStub) ApplyAndRecord(context.Context, *UsageBillingCommand, *UsageLog) (*UsageBillingApplyResult, error) {
	s.applyAndRecordCalls++
	if s.returnNilResult {
		return nil, nil
	}
	if s.result != nil {
		return s.result, nil
	}
	return &UsageBillingApplyResult{
		Applied:          false,
		UsageLogRecorded: true,
		OutboxReceipt: &UsageBillingOutboxReceipt{
			ID:       1,
			WorkerID: "strict-test-worker",
		},
	}, nil
}

func (s *strictDurableUsageBillingRepoStub) AcknowledgeUsageBillingOutbox(context.Context, string, int64) error {
	s.ackCalls++
	return nil
}

func durableEnforcementParams() *postUsageBillingParams {
	return &postUsageBillingParams{
		Cost:    &CostBreakdown{},
		User:    &User{ID: 101},
		APIKey:  &APIKey{ID: 102},
		Account: &Account{ID: 103},
	}
}

func TestApplyUsageBilling_StrictModeRejectsEveryNonDurableInput(t *testing.T) {
	durableRepo := &strictDurableUsageBillingRepoStub{}
	var typedNilRepo *strictDurableUsageBillingRepoStub
	legacyRepo := &openAIRecordUsageBillingRepoStub{}
	usageLog := &UsageLog{RequestID: "usage-req", CreatedAt: time.Now()}
	validParams := durableEnforcementParams()
	validDeps := &billingDeps{}

	tests := []struct {
		name      string
		requestID string
		usageLog  *UsageLog
		params    *postUsageBillingParams
		deps      *billingDeps
		repo      UsageBillingRepository
	}{
		{
			name:      "nil params",
			requestID: "billing-req",
			usageLog:  usageLog,
			deps:      validDeps,
			repo:      durableRepo,
		},
		{
			name:      "nil dependencies",
			requestID: "billing-req",
			usageLog:  usageLog,
			params:    validParams,
			repo:      durableRepo,
		},
		{
			name:      "command unavailable",
			requestID: "billing-req",
			usageLog:  usageLog,
			params: &postUsageBillingParams{
				Cost:    &CostBreakdown{},
				User:    &User{ID: 101},
				Account: &Account{ID: 103},
			},
			deps: validDeps,
			repo: durableRepo,
		},
		{
			name:     "empty request id",
			usageLog: usageLog,
			params:   validParams,
			deps:     validDeps,
			repo:     durableRepo,
		},
		{
			name:      "nil usage log",
			requestID: "billing-req",
			params:    validParams,
			deps:      validDeps,
			repo:      durableRepo,
		},
		{
			name:      "nil repository",
			requestID: "billing-req",
			usageLog:  usageLog,
			params:    validParams,
			deps:      validDeps,
		},
		{
			name:      "typed nil repository",
			requestID: "billing-req",
			usageLog:  usageLog,
			params:    validParams,
			deps:      validDeps,
			repo:      typedNilRepo,
		},
		{
			name:      "non durable repository",
			requestID: "billing-req",
			usageLog:  usageLog,
			params:    validParams,
			deps:      validDeps,
			repo:      legacyRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applied, usageLogRecorded, err := applyUsageBilling(
				context.Background(),
				tt.requestID,
				tt.usageLog,
				tt.params,
				tt.deps,
				tt.repo,
			)

			require.ErrorIs(t, err, ErrDurableUsageBillingRequired)
			require.False(t, applied)
			require.False(t, usageLogRecorded)
		})
	}

	require.Zero(t, durableRepo.applyCalls)
	require.Zero(t, durableRepo.applyAndRecordCalls)
	require.Zero(t, legacyRepo.calls, "strict mode must not fall back to repo.Apply")
}

func TestApplyUsageBilling_StrictModeUsesApplyAndRecord(t *testing.T) {
	repo := &strictDurableUsageBillingRepoStub{
		result: &UsageBillingApplyResult{
			Applied:          false,
			UsageLogRecorded: true,
			OutboxReceipt: &UsageBillingOutboxReceipt{
				ID:       1,
				WorkerID: "strict-test-worker",
			},
		},
	}

	applied, usageLogRecorded, err := applyUsageBilling(
		context.Background(),
		"billing-req",
		&UsageLog{RequestID: "usage-req", CreatedAt: time.Now()},
		durableEnforcementParams(),
		&billingDeps{},
		repo,
	)

	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, usageLogRecorded)
	require.Equal(t, 1, repo.applyAndRecordCalls)
	require.Zero(t, repo.applyCalls)
	require.Equal(t, 1, repo.ackCalls)
}

func TestApplyUsageBilling_StrictModeRejectsNilDurableResult(t *testing.T) {
	repo := &strictDurableUsageBillingRepoStub{returnNilResult: true}

	applied, usageLogRecorded, err := applyUsageBilling(
		context.Background(),
		"billing-req",
		&UsageLog{RequestID: "usage-req", CreatedAt: time.Now()},
		durableEnforcementParams(),
		&billingDeps{},
		repo,
	)

	require.ErrorIs(t, err, ErrDurableUsageBillingRequired)
	require.False(t, applied)
	require.False(t, usageLogRecorded)
	require.Equal(t, 1, repo.applyAndRecordCalls)
	require.Zero(t, repo.applyCalls)
}

func TestApplyUsageBilling_StrictModeRejectsInvalidDurableSuccessResult(t *testing.T) {
	tests := []struct {
		name   string
		result *UsageBillingApplyResult
	}{
		{
			name: "usage log not recorded",
			result: &UsageBillingApplyResult{
				Applied: true,
				OutboxReceipt: &UsageBillingOutboxReceipt{
					ID:       1,
					WorkerID: "strict-test-worker",
				},
			},
		},
		{
			name: "missing receipt",
			result: &UsageBillingApplyResult{
				UsageLogRecorded: true,
			},
		},
		{
			name: "invalid receipt id",
			result: &UsageBillingApplyResult{
				UsageLogRecorded: true,
				OutboxReceipt: &UsageBillingOutboxReceipt{
					WorkerID: "strict-test-worker",
				},
			},
		},
		{
			name: "invalid receipt worker",
			result: &UsageBillingApplyResult{
				UsageLogRecorded: true,
				OutboxReceipt: &UsageBillingOutboxReceipt{
					ID: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &strictDurableUsageBillingRepoStub{result: tt.result}

			applied, usageLogRecorded, err := applyUsageBilling(
				context.Background(),
				"billing-req",
				&UsageLog{RequestID: "usage-req", CreatedAt: time.Now()},
				durableEnforcementParams(),
				&billingDeps{},
				repo,
			)

			require.ErrorIs(t, err, ErrDurableUsageBillingRequired)
			require.False(t, applied)
			require.False(t, usageLogRecorded)
			require.Equal(t, 1, repo.applyAndRecordCalls)
			require.Zero(t, repo.applyCalls)
			require.Zero(t, repo.ackCalls)
		})
	}
}

func TestApplyUsageBilling_LegacyPathRequiresExplicitTestOptIn(t *testing.T) {
	userRepo := &openAIRecordUsageUserRepoStub{}
	params := durableEnforcementParams()
	params.Cost = &CostBreakdown{ActualCost: 1, TotalCost: 1}

	applied, usageLogRecorded, err := applyUsageBilling(
		context.Background(),
		"billing-req",
		&UsageLog{RequestID: "usage-req", CreatedAt: time.Now()},
		params,
		&billingDeps{
			userRepo:                        userRepo,
			allowLegacyUsageBillingForTests: true,
		},
		nil,
	)

	require.NoError(t, err)
	require.True(t, applied)
	require.False(t, usageLogRecorded)
	require.Equal(t, 1, userRepo.deductCalls)
	require.Equal(t, 1.0, userRepo.lastAmount)
}

func TestGatewayServiceRecordUsage_PropagatesMissingDurableRepository(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(
		usageRepo,
		userRepo,
		&openAIRecordUsageSubRepoStub{},
	)
	svc.allowLegacyUsageBillingForTests = false

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway-durable-required",
			Model:     "claude-sonnet-4",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 201},
		User:    &User{ID: 202},
		Account: &Account{ID: 203},
	})

	require.ErrorIs(t, err, ErrDurableUsageBillingRequired)
	// A durable settlement failure must still leave an unsettled usage row for
	// reconciliation; it must never look like a successful charge.
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.Zero(t, userRepo.deductCalls)
}

func TestOpenAIGatewayServiceRecordUsage_PropagatesNonDurableRepository(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		userRepo,
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	svc.allowLegacyUsageBillingForTests = false

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "openai-durable-required",
			Model:     "gpt-5.1",
			Usage: OpenAIUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 301},
		User:    &User{ID: 302},
		Account: &Account{ID: 303},
	})

	require.ErrorIs(t, err, ErrDurableUsageBillingRequired)
	require.Zero(t, billingRepo.calls, "strict mode must not call legacy repo.Apply")
	// Preserve the request as an unsettled usage row even though the strict
	// repository contract rejected the billing path.
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.Zero(t, userRepo.deductCalls)
}
