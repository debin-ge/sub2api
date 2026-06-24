package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

const benchmarkRunnerDefaultClaimLimit = 20

type BenchmarkRunner struct {
	repo            BenchmarkRepository
	client          BenchmarkGatewayClient
	runtimeProvider benchmarkRuntimeProvider
}

func NewBenchmarkRunner(repo BenchmarkRepository, client BenchmarkGatewayClient) *BenchmarkRunner {
	return &BenchmarkRunner{
		repo:   repo,
		client: client,
	}
}

func (r *BenchmarkRunner) SetBenchmarkRuntimeProvider(provider benchmarkRuntimeProvider) {
	r.runtimeProvider = provider
}

func (r *BenchmarkRunner) SetSettingService(settingService *SettingService) {
	r.SetBenchmarkRuntimeProvider(settingService)
}

func (r *BenchmarkRunner) RunOnce(ctx context.Context, runID int64) error {
	runtime, hasRuntimeProvider := r.getBenchmarkRuntime(ctx)
	run, err := r.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	claimed, err := r.repo.ClaimPendingResults(ctx, runID, benchmarkRunnerClaimLimit(run, runtime, hasRuntimeProvider))
	if err != nil {
		return err
	}

	for i, result := range claimed {
		resultCtx, err := r.repo.GetRunResultContext(ctx, result.ID)
		if err != nil {
			return r.requeueClaimedResults(ctx, claimed[i:], err)
		}
		if err := r.runResultOnce(ctx, resultCtx, runtime, hasRuntimeProvider); err != nil {
			return r.requeueClaimedResults(ctx, claimed[i:], err)
		}
	}

	counts, err := r.repo.CountRunResultsByStatus(ctx, runID)
	if err != nil {
		return err
	}

	nextStatus := BenchmarkRunStatusScoring
	if counts[BenchmarkResultStatusPending] > 0 || counts[BenchmarkResultStatusRunning] > 0 {
		nextStatus = BenchmarkRunStatusRunning
	}
	return r.repo.UpdateRunStatus(ctx, runID, nextStatus, nil)
}

func benchmarkRunnerClaimLimit(run *ent.BenchmarkRun, runtime BenchmarkRuntime, hasRuntimeProvider bool) int {
	if value, ok := benchmarkRunnerRuntimeConfigPositiveInt(run, "max_concurrency"); ok {
		return value
	}
	if value, ok := benchmarkRunnerRuntimeConfigPositiveInt(run, "concurrency"); ok {
		return value
	}
	if hasRuntimeProvider && runtime.GlobalConcurrency > 0 {
		return runtime.GlobalConcurrency
	}
	return benchmarkRunnerDefaultClaimLimit
}

func (r *BenchmarkRunner) getBenchmarkRuntime(ctx context.Context) (BenchmarkRuntime, bool) {
	if r == nil || r.runtimeProvider == nil {
		return BenchmarkRuntime{GlobalConcurrency: benchmarkRunnerDefaultClaimLimit}, false
	}
	return normalizeBenchmarkRuntime(r.runtimeProvider.GetBenchmarkRuntime(ctx)), true
}

func (r *BenchmarkRunner) requeueClaimedResults(ctx context.Context, claimed []*ent.BenchmarkResult, runErr error) error {
	resultIDs := make([]int64, 0, len(claimed))
	for _, result := range claimed {
		if result == nil || result.ID <= 0 {
			continue
		}
		resultIDs = append(resultIDs, result.ID)
	}
	if len(resultIDs) == 0 {
		return runErr
	}
	if err := r.repo.RequeueClaimedResults(benchmarkRunnerCleanupContext(ctx), resultIDs); err != nil {
		return errors.Join(runErr, fmt.Errorf("requeue claimed results: %w", err))
	}
	return runErr
}

func (r *BenchmarkRunner) runResultOnce(ctx context.Context, resultCtx *BenchmarkRunResultContext, runtime BenchmarkRuntime, hasRuntimeProvider bool) error {
	if resultCtx == nil || resultCtx.Result == nil || resultCtx.Run == nil || resultCtx.Target == nil || resultCtx.Task == nil {
		return errors.New("benchmark run result context is incomplete")
	}

	startedAt := time.Now().UTC()
	req := BenchmarkGatewayRequest{
		RunID:        resultCtx.Run.ID,
		RunTargetID:  resultCtx.Target.ID,
		RunTaskID:    resultCtx.Task.ID,
		Attempt:      resultCtx.Result.AttemptCount,
		ModelName:    resultCtx.Target.ModelName,
		ChannelID:    resultCtx.Target.ChannelID,
		Prompt:       resultCtx.Task.PromptSnapshot,
		InputPayload: benchmarkRunnerInputPayload(resultCtx.Task),
		Timeout:      benchmarkRunnerTimeout(resultCtx.Run, runtime, hasRuntimeProvider),
	}

	resp, err := r.client.Execute(ctx, req)
	finishedAt := time.Now().UTC()
	if err != nil {
		status := benchmarkRunnerGatewayErrorStatus(err)
		errMessage := err.Error()
		requestID := deterministicBenchmarkGatewayRequestID(req)
		if resp != nil && strings.TrimSpace(resp.RequestID) != "" {
			requestID = resp.RequestID
		}
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			BenchmarkResultUpdateInput{
				Status:               benchmarkRunnerStringPtr(status),
				RequestID:            benchmarkRunnerStringPtr(requestID),
				ClearScore:           true,
				ClearMaxScore:        true,
				ClearNormalizedScore: true,
				ClearEvaluatorType:   true,
				ClearEvaluatorOutput: true,
				ErrorCode:            benchmarkRunnerStringPtr(status),
				ErrorMessage:         &errMessage,
				StartedAt:            &startedAt,
				FinishedAt:           &finishedAt,
			},
		))
	}

	if resp == nil {
		resp = &BenchmarkGatewayResponse{}
	}
	requestID := strings.TrimSpace(resp.RequestID)
	if requestID == "" {
		requestID = deterministicBenchmarkGatewayRequestID(req)
	}

	verifierResult, verifyErr := VerifyBenchmarkResponse(
		resultCtx.Task.VerifierTypeSnapshot,
		benchmarkCloneAnyMap(resultCtx.Task.VerifierConfigSnapshot),
		resp.Content,
	)
	if verifyErr != nil {
		errMessage := verifyErr.Error()
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			BenchmarkResultUpdateInput{
				Status:               benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
				RequestID:            &requestID,
				ClearScore:           true,
				ClearMaxScore:        true,
				ClearNormalizedScore: true,
				EvaluatorType:        benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
				ClearEvaluatorOutput: true,
				ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
				ErrorMessage:         &errMessage,
				StartedAt:            &startedAt,
				FinishedAt:           &finishedAt,
			},
		))
	}

	switch verifierResult.Status {
	case BenchmarkResultStatusScored:
		scoredUpdate := BenchmarkResultUpdateInput{
			Status:            benchmarkRunnerStringPtr(BenchmarkResultStatusScored),
			RequestID:         &requestID,
			Score:             &verifierResult.Score,
			MaxScore:          &verifierResult.MaxScore,
			NormalizedScore:   &verifierResult.NormalizedScore,
			EvaluatorType:     benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
			ClearErrorCode:    true,
			ClearErrorMessage: true,
			StartedAt:         &startedAt,
			FinishedAt:        &finishedAt,
		}
		benchmarkRunnerSetEvaluatorOutput(&scoredUpdate, verifierResult.Output)
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			scoredUpdate,
		))
	case BenchmarkResultStatusParseError:
		errMessage := benchmarkRunnerVerifierErrorMessage(verifierResult, "benchmark verifier parse error")
		parseUpdate := BenchmarkResultUpdateInput{
			Status:               benchmarkRunnerStringPtr(BenchmarkResultStatusParseError),
			RequestID:            &requestID,
			ClearScore:           true,
			ClearMaxScore:        true,
			ClearNormalizedScore: true,
			EvaluatorType:        benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
			ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusParseError),
			ErrorMessage:         &errMessage,
			StartedAt:            &startedAt,
			FinishedAt:           &finishedAt,
		}
		benchmarkRunnerSetEvaluatorOutput(&parseUpdate, verifierResult.Output)
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			parseUpdate,
		))
	default:
		errMessage := fmt.Sprintf("unexpected benchmark verifier status: %s", verifierResult.Status)
		unexpectedUpdate := BenchmarkResultUpdateInput{
			Status:               benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
			RequestID:            &requestID,
			ClearScore:           true,
			ClearMaxScore:        true,
			ClearNormalizedScore: true,
			EvaluatorType:        benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
			ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
			ErrorMessage:         &errMessage,
			StartedAt:            &startedAt,
			FinishedAt:           &finishedAt,
		}
		benchmarkRunnerSetEvaluatorOutput(&unexpectedUpdate, verifierResult.Output)
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			unexpectedUpdate,
		))
	}
}

type benchmarkRunnerRateLimitError struct {
	message string
}

func (e benchmarkRunnerRateLimitError) Error() string {
	if strings.TrimSpace(e.message) == "" {
		return "benchmark gateway rate limited"
	}
	return e.message
}

func newBenchmarkRunnerRateLimitError(message string) error {
	return benchmarkRunnerRateLimitError{message: message}
}

func benchmarkRunnerGatewayErrorStatus(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return BenchmarkResultStatusTimeout
	case benchmarkRunnerIsRateLimitError(err):
		return BenchmarkResultStatusRateLimited
	default:
		return BenchmarkResultStatusChannelError
	}
}

func benchmarkRunnerIsRateLimitError(err error) bool {
	var target benchmarkRunnerRateLimitError
	return errors.As(err, &target)
}

func benchmarkRunnerTimeout(run *ent.BenchmarkRun, runtime BenchmarkRuntime, hasRuntimeProvider bool) time.Duration {
	if value, ok := benchmarkRunnerRuntimeConfigPositiveDuration(run, "timeout"); ok {
		return value
	}
	if hasRuntimeProvider && runtime.DefaultTimeoutSeconds > 0 {
		return time.Duration(runtime.DefaultTimeoutSeconds) * time.Second
	}
	return 0
}

func benchmarkRunnerRuntimeConfigPositiveInt(run *ent.BenchmarkRun, key string) (int, bool) {
	if run == nil || len(run.ConfigSnapshot) == 0 {
		return 0, false
	}
	runtimeConfig, ok := run.ConfigSnapshot["runtime_config"].(map[string]any)
	if !ok {
		return 0, false
	}
	switch value := runtimeConfig[key].(type) {
	case int:
		if value > 0 {
			return value, true
		}
	case int64:
		if value > 0 {
			return int(value), true
		}
	case float64:
		if value > 0 && value == math.Trunc(value) {
			return int(value), true
		}
	}
	return 0, false
}

func benchmarkRunnerRuntimeConfigPositiveDuration(run *ent.BenchmarkRun, key string) (time.Duration, bool) {
	if run == nil || len(run.ConfigSnapshot) == 0 {
		return 0, false
	}
	runtimeConfig, ok := run.ConfigSnapshot["runtime_config"].(map[string]any)
	if !ok {
		return 0, false
	}
	switch value := runtimeConfig[key].(type) {
	case int:
		if value > 0 {
			return time.Duration(value) * time.Second, true
		}
	case int64:
		if value > 0 {
			return time.Duration(value) * time.Second, true
		}
	case float64:
		if value > 0 {
			return time.Duration(value * float64(time.Second)), true
		}
	}
	return 0, false
}

func benchmarkRunnerInputPayload(task *ent.BenchmarkRunTask) map[string]any {
	if task == nil || task.TaskSnapshot == nil {
		return nil
	}
	payload, ok := task.TaskSnapshot["input_payload"].(map[string]any)
	if !ok {
		return nil
	}
	return benchmarkCloneAnyMap(payload)
}

func benchmarkRunnerResponseUpdateInput(resp *BenchmarkGatewayResponse, input BenchmarkResultUpdateInput) BenchmarkResultUpdateInput {
	if resp == nil {
		resp = &BenchmarkGatewayResponse{}
	}
	if resp.RawResponse != nil {
		input.RawResponse = benchmarkCloneAnyMap(resp.RawResponse)
	} else if input.RawResponse == nil {
		input.RawResponse = map[string]any{}
	}
	if resp.LatencyMS > 0 {
		input.LatencyMS = benchmarkRunnerIntPtr(resp.LatencyMS)
	} else {
		input.ClearLatencyMS = true
	}
	input.PromptTokens = benchmarkRunnerIntPtr(resp.PromptTokens)
	input.CompletionTokens = benchmarkRunnerIntPtr(resp.CompletionTokens)
	input.TotalTokens = benchmarkRunnerIntPtr(resp.TotalTokens)
	input.EstimatedCost = benchmarkRunnerFloat64Ptr(resp.EstimatedCost)
	return input
}

func benchmarkRunnerCleanupContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func benchmarkRunnerSetEvaluatorOutput(input *BenchmarkResultUpdateInput, output map[string]any) {
	if input == nil {
		return
	}
	if len(output) == 0 {
		input.ClearEvaluatorOutput = true
		input.EvaluatorOutput = nil
		return
	}
	input.ClearEvaluatorOutput = false
	input.EvaluatorOutput = benchmarkCloneAnyMap(output)
}

func benchmarkRunnerVerifierErrorMessage(result BenchmarkVerifierResult, fallback string) string {
	if message, ok := result.Output["error"].(string); ok && strings.TrimSpace(message) != "" {
		return message
	}
	return fallback
}

func benchmarkRunnerStringPtr(value string) *string {
	return &value
}

func benchmarkRunnerIntPtr(value int) *int {
	return &value
}

func benchmarkRunnerFloat64Ptr(value float64) *float64 {
	return &value
}
