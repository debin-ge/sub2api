package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func repairVideoSource() *VideoTask {
	accountID := int64(11)
	providerID := "video_provider_source"
	return &VideoTask{
		ID: 10, PublicID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 42,
		AccountID: &accountID, Provider: VideoProviderOpenAI, ProviderTaskID: &providerID,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured, DeleteState: VideoDeleteNone,
		PublicModel: OpenAIVideoModelSora2Pro, UpstreamModel: OpenAIVideoModelSora2Pro, Version: 4,
		RequestAttributes: map[string]any{"size": "1920x1080", "seconds": 20},
		ResponseMetadata:  map[string]any{"size": "1920x1080", "seconds": "20"},
	}
}

func TestVideoRepairExtensionRejectsCheaperSourceIdentity(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_extension", Status: VideoGenerationQueued}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	source := repairVideoSource()
	tasks.sources[source.PublicID] = source
	request := videoSubmitRequestForTest()
	request.Operation, request.SourceVideoID = VideoOperationExtend, source.PublicID
	_, err := svc.resolveSubmission(context.Background(), request, nil)
	require.ErrorIs(t, err, ErrVideoSourceSpecConflict)
	require.Nil(t, tasks.task)
	require.Zero(t, provider.createCalls)
}

func TestVideoRepairSourceSpecIsSharedByPricingAndProvider(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_extension", Status: VideoGenerationQueued}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing[0].Models = []string{OpenAIVideoModelSora2Pro}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	source := repairVideoSource()
	tasks.sources[source.PublicID] = source
	request := videoSubmitRequestForTest()
	request.Operation, request.SourceVideoID, request.Model, request.Size = VideoOperationExtend, source.PublicID, "", ""
	resolved, err := svc.resolveSubmission(context.Background(), request, nil)
	require.NoError(t, err)
	require.Equal(t, OpenAIVideoModelSora2Pro, resolved.upstreamModel)
	require.Equal(t, "1920x1080", resolved.request.Size)
	require.Equal(t, "1920x1080", resolved.providerRequest.Size)
	require.Equal(t, OpenAIVideoModelSora2Pro, resolved.providerRequest.Model)
	require.Equal(t, 8, resolved.providerRequest.Seconds)
	spec := resolved.executionSpec
	require.Equal(t, source.Version, spec.SourceVersion)
	require.Equal(t, "extension_segment", spec.DurationSemantics)
	require.Equal(t, float64(20), spec.SourceSeconds)
}

func TestVideoRepairEditInheritsSourceDurationInsteadOfClientOverride(t *testing.T) {
	source := repairVideoSource()
	request := VideoCreateRequest{Operation: VideoOperationEdit, Model: source.UpstreamModel}
	resolved, spec, err := resolveVideoExecutionSpec(request, *source.AccountID, source.Provider, source)
	require.NoError(t, err)
	require.Equal(t, 20, resolved.Seconds)
	require.Equal(t, "1920x1080", resolved.Size)
	require.Equal(t, resolved.Seconds, spec.Seconds)
	request.Seconds = 4
	_, _, err = resolveVideoExecutionSpec(request, *source.AccountID, source.Provider, source)
	require.ErrorIs(t, err, ErrVideoSourceSpecConflict)
}

type videoRepairGroupAccountRepo struct {
	AccountRepository
	account *Account
}

func (repo *videoRepairGroupAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return repo.account, nil
}

func (repo *videoRepairGroupAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return nil, nil
}

func TestVideoRepairAffinityRequiresCurrentGroupCandidate(t *testing.T) {
	svc, _, _ := newVideoTaskServiceForTest(&videoProviderStub{}, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.accounts = &videoRepairGroupAccountRepo{account: &Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		GroupIDs: []int64{99}, Credentials: map[string]any{"api_key": "test-only"},
	}}
	_, _, _, err := svc.selectVideoAccount(context.Background(), 42, 7, OpenAIVideoModelSora2, 11, nil, false)
	require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
}

func TestVideoRepairFailedSpecificationIsNotBillingEvidence(t *testing.T) {
	observed, err := decodeOpenAIVideoTask(strings.NewReader(`{"id":"video_failed","status":"failed","seconds":"8","error":{"code":"moderation_blocked"}}`), false)
	require.NoError(t, err)
	decision := videoTerminalBillingFor(baseVideoWorkerTask(), VideoGenerationFailed, observed)
	require.Equal(t, VideoBillingReleasePending, decision.state)
	require.Zero(t, *decision.actualCost)
}

func TestVideoRepairFailureAlwaysReleasesRegardlessOfUsage(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		unit  string
		usage map[string]any
	}{
		{"positive seconds", VideoBillingUnitSecond, map[string]any{"billable_seconds": 3}},
		{"zero seconds", VideoBillingUnitSecond, map[string]any{"seconds": 0}},
		{"zero tokens", VideoBillingUnitVideoToken, map[string]any{"output_tokens": 0}},
		{"conflicting seconds", VideoBillingUnitSecond, map[string]any{"billable_seconds": 3, "seconds": 8}},
		{"invalid seconds", VideoBillingUnitSecond, map[string]any{"billable_seconds": -1, "seconds": 8}},
		{"missing request evidence", VideoBillingUnitRequest, nil},
		{"positive request evidence", VideoBillingUnitRequest, map[string]any{"requests": 1}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.BillingUnit = &scenario.unit
			observed := &ProviderVideoTask{Metadata: map[string]any{"seconds": 99}, Usage: sanitizeVideoProviderUsage(scenario.usage)}
			decision := videoTerminalBillingFor(task, VideoGenerationFailed, observed)
			require.Equal(t, VideoBillingReleasePending, decision.state)
			require.Zero(t, *decision.actualUnits)
			require.Zero(t, *decision.actualCost)
		})
	}
}

func TestVideoRepairAccountQuotaUsesFrozenMultiplier(t *testing.T) {
	for _, multiplier := range []float64{0, 0.25, 1, 3} {
		task := baseVideoWorkerTask()
		task.ActualUnits = floatPointer(8)
		task.ProviderCostSnapshot = map[string]any{"account_rate_multiplier": multiplier}
		finished := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
		task.FinishedAt = &finished
		command, usage, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
		require.NoError(t, err)
		require.Equal(t, 8.0, command.ActualCost)
		require.Equal(t, 4*multiplier, command.AccountQuotaCost)
		require.Equal(t, multiplier, *usage.AccountRateMultiplier)
		require.Equal(t, finished, command.OccurredAt)
		require.Equal(t, finished, usage.CreatedAt)
	}
}

func TestVideoRepairInvalidCostSnapshotDoesNotBuildSettlement(t *testing.T) {
	for _, multiplier := range []any{nil, -1, math.NaN(), "not-a-price"} {
		task := baseVideoWorkerTask()
		task.ProviderCostSnapshot = map[string]any{"account_rate_multiplier": multiplier}
		_, _, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
		require.Error(t, err)
	}
	task := baseVideoWorkerTask()
	task.PriceSnapshot["billing_contract_version"] = 2
	_, _, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
	require.Error(t, err)
}

func TestVideoRepairExtensionBillsNewSegmentNotWholeVideo(t *testing.T) {
	task := baseVideoWorkerTask()
	task.Operation = VideoOperationExtend
	task.RequestAttributes["source_output_seconds"] = 20
	units, cost, err := videoActualCost(task, &ProviderVideoTask{Metadata: map[string]any{"seconds": 28}})
	require.NoError(t, err)
	require.Equal(t, 8.0, units)
	require.Equal(t, 8.0, cost)
	delete(task.RequestAttributes, "source_output_seconds")
	_, _, err = videoActualCost(task, &ProviderVideoTask{Metadata: map[string]any{"seconds": 28}})
	require.Error(t, err)
}
