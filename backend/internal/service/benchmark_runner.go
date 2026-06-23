package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

const benchmarkRunnerDefaultClaimLimit = 20

type BenchmarkRunner struct {
	repo   BenchmarkRepository
	client BenchmarkGatewayClient
}

func NewBenchmarkRunner(repo BenchmarkRepository, client BenchmarkGatewayClient) *BenchmarkRunner {
	return &BenchmarkRunner{
		repo:   repo,
		client: client,
	}
}

func (r *BenchmarkRunner) RunOnce(ctx context.Context, runID int64) error {
	claimed, err := r.repo.ClaimPendingResults(ctx, runID, benchmarkRunnerDefaultClaimLimit)
	if err != nil {
		return err
	}

	for _, result := range claimed {
		resultCtx, err := r.repo.GetRunResultContext(ctx, result.ID)
		if err != nil {
			return err
		}
		if err := r.runResultOnce(ctx, resultCtx); err != nil {
			return err
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

func (r *BenchmarkRunner) runResultOnce(ctx context.Context, resultCtx *BenchmarkRunResultContext) error {
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
		Timeout:      benchmarkRunnerTimeout(resultCtx.Run),
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
				ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
				ErrorMessage:         &errMessage,
				StartedAt:            &startedAt,
				FinishedAt:           &finishedAt,
			},
		))
	}

	switch verifierResult.Status {
	case BenchmarkResultStatusScored:
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			BenchmarkResultUpdateInput{
				Status:            benchmarkRunnerStringPtr(BenchmarkResultStatusScored),
				RequestID:         &requestID,
				Score:             &verifierResult.Score,
				MaxScore:          &verifierResult.MaxScore,
				NormalizedScore:   &verifierResult.NormalizedScore,
				EvaluatorType:     benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
				EvaluatorOutput:   benchmarkCloneAnyMap(verifierResult.Output),
				ClearErrorCode:    true,
				ClearErrorMessage: true,
				StartedAt:         &startedAt,
				FinishedAt:        &finishedAt,
			},
		))
	case BenchmarkResultStatusParseError:
		errMessage := benchmarkRunnerVerifierErrorMessage(verifierResult, "benchmark verifier parse error")
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			BenchmarkResultUpdateInput{
				Status:               benchmarkRunnerStringPtr(BenchmarkResultStatusParseError),
				RequestID:            &requestID,
				ClearScore:           true,
				ClearMaxScore:        true,
				ClearNormalizedScore: true,
				EvaluatorType:        benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
				EvaluatorOutput:      benchmarkCloneAnyMap(verifierResult.Output),
				ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusParseError),
				ErrorMessage:         &errMessage,
				StartedAt:            &startedAt,
				FinishedAt:           &finishedAt,
			},
		))
	default:
		errMessage := fmt.Sprintf("unexpected benchmark verifier status: %s", verifierResult.Status)
		return r.repo.UpdateResult(ctx, resultCtx.Result.ID, benchmarkRunnerResponseUpdateInput(
			resp,
			BenchmarkResultUpdateInput{
				Status:               benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
				RequestID:            &requestID,
				ClearScore:           true,
				ClearMaxScore:        true,
				ClearNormalizedScore: true,
				EvaluatorType:        benchmarkRunnerStringPtr(resultCtx.Task.VerifierTypeSnapshot),
				EvaluatorOutput:      benchmarkCloneAnyMap(verifierResult.Output),
				ErrorCode:            benchmarkRunnerStringPtr(BenchmarkResultStatusVerifierError),
				ErrorMessage:         &errMessage,
				StartedAt:            &startedAt,
				FinishedAt:           &finishedAt,
			},
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

func benchmarkRunnerTimeout(run *ent.BenchmarkRun) time.Duration {
	if run == nil {
		return 0
	}
	runtimeConfig, ok := run.ConfigSnapshot["runtime_config"].(map[string]any)
	if !ok {
		return 0
	}
	switch value := runtimeConfig["timeout"].(type) {
	case int:
		if value > 0 {
			return time.Duration(value) * time.Second
		}
	case int64:
		if value > 0 {
			return time.Duration(value) * time.Second
		}
	case float64:
		if value > 0 {
			return time.Duration(value * float64(time.Second))
		}
	}
	return 0
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
