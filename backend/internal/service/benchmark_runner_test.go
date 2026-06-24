package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type benchmarkRunnerRepoStub struct {
	*benchmarkServiceRepoStub

	claimPendingResultsFn   func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error)
	getRunResultContextFn   func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error)
	requeueClaimedResultsFn func(ctx context.Context, resultIDs []int64) error
	updateResultFn          func(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error
	countRunResultsFn       func(ctx context.Context, runID int64) (map[string]int, error)
	updateRunStatusFn       func(ctx context.Context, runID int64, status string, errorMessage *string) error
	claimPendingResultsCall struct {
		runID int64
		limit int
	}
	updateCalls    []benchmarkRunnerUpdateCall
	requeueCalls   [][]int64
	runStatusCalls []benchmarkRunnerRunStatusCall
}

type benchmarkRunnerUpdateCall struct {
	id    int64
	input BenchmarkResultUpdateInput
}

type benchmarkRunnerRunStatusCall struct {
	runID        int64
	status       string
	errorMessage *string
}

func newBenchmarkRunnerRepoStub(t *testing.T) *benchmarkRunnerRepoStub {
	t.Helper()
	return &benchmarkRunnerRepoStub{
		benchmarkServiceRepoStub: newBenchmarkServiceRepoStub(t),
	}
}

func (s *benchmarkRunnerRepoStub) ClaimPendingResults(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
	s.claimPendingResultsCall.runID = runID
	s.claimPendingResultsCall.limit = limit
	if s.claimPendingResultsFn != nil {
		return s.claimPendingResultsFn(ctx, runID, limit)
	}
	s.t.Fatalf("unexpected ClaimPendingResults call")
	return nil, nil
}

func (s *benchmarkRunnerRepoStub) GetRunResultContext(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
	if s.getRunResultContextFn != nil {
		return s.getRunResultContextFn(ctx, resultID)
	}
	s.t.Fatalf("unexpected GetRunResultContext call")
	return nil, nil
}

func (s *benchmarkRunnerRepoStub) GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
	if s.getRunFn != nil {
		return s.getRunFn(ctx, id)
	}
	return &ent.BenchmarkRun{ID: id}, nil
}

func (s *benchmarkRunnerRepoStub) RequeueClaimedResults(ctx context.Context, resultIDs []int64) error {
	cloned := append([]int64(nil), resultIDs...)
	s.requeueCalls = append(s.requeueCalls, cloned)
	if s.requeueClaimedResultsFn != nil {
		return s.requeueClaimedResultsFn(ctx, cloned)
	}
	return nil
}

func (s *benchmarkRunnerRepoStub) UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error {
	s.updateCalls = append(s.updateCalls, benchmarkRunnerUpdateCall{id: id, input: input})
	if s.updateResultFn != nil {
		return s.updateResultFn(ctx, id, input)
	}
	return nil
}

func (s *benchmarkRunnerRepoStub) CountRunResultsByStatus(ctx context.Context, runID int64) (map[string]int, error) {
	if s.countRunResultsFn != nil {
		return s.countRunResultsFn(ctx, runID)
	}
	s.t.Fatalf("unexpected CountRunResultsByStatus call")
	return nil, nil
}

func (s *benchmarkRunnerRepoStub) UpdateRunStatus(ctx context.Context, runID int64, status string, errorMessage *string) error {
	s.runStatusCalls = append(s.runStatusCalls, benchmarkRunnerRunStatusCall{
		runID:        runID,
		status:       status,
		errorMessage: errorMessage,
	})
	if s.updateRunStatusFn != nil {
		return s.updateRunStatusFn(ctx, runID, status, errorMessage)
	}
	return nil
}

type benchmarkRunnerClientStub struct {
	executeFn func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error)
	requests  []BenchmarkGatewayRequest
}

func (s *benchmarkRunnerClientStub) Execute(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
	s.requests = append(s.requests, req)
	if s.executeFn != nil {
		return s.executeFn(ctx, req)
	}
	return nil, nil
}

func TestBenchmarkRunnerScoresSuccessfulResult(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		require.Equal(t, ctxValue.Result.ID, resultID)
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusPending: 1,
			BenchmarkResultStatusScored:  1,
		}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{
				RequestID:        "req-success-1",
				Content:          "Paris",
				RawResponse:      map[string]any{"answer": "Paris"},
				LatencyMS:        321,
				PromptTokens:     12,
				CompletionTokens: 6,
				TotalTokens:      18,
				EstimatedCost:    0.0123,
			}, nil
		},
	}

	runner := NewBenchmarkRunner(repo, client)
	err := runner.RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)

	require.Equal(t, ctxValue.Run.ID, repo.claimPendingResultsCall.runID)
	require.Equal(t, benchmarkRunnerDefaultClaimLimit, repo.claimPendingResultsCall.limit)
	require.Len(t, client.requests, 1)
	require.Equal(t, ctxValue.Run.ID, client.requests[0].RunID)
	require.Equal(t, ctxValue.Target.ID, client.requests[0].RunTargetID)
	require.Equal(t, ctxValue.Task.ID, client.requests[0].RunTaskID)
	require.Equal(t, ctxValue.Result.AttemptCount, client.requests[0].Attempt)
	require.Equal(t, ctxValue.Target.ModelName, client.requests[0].ModelName)
	require.Equal(t, ctxValue.Target.ChannelID, client.requests[0].ChannelID)
	require.Equal(t, ctxValue.Task.PromptSnapshot, client.requests[0].Prompt)
	require.Equal(t, map[string]any{"question": "capital of France"}, client.requests[0].InputPayload)
	require.Equal(t, 30*time.Second, client.requests[0].Timeout)

	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusScored, *update.Status)
	require.NotNil(t, update.RequestID)
	require.Equal(t, "req-success-1", *update.RequestID)
	require.NotNil(t, update.Score)
	require.InDelta(t, 1, *update.Score, 0.000001)
	require.NotNil(t, update.MaxScore)
	require.InDelta(t, 1, *update.MaxScore, 0.000001)
	require.NotNil(t, update.NormalizedScore)
	require.InDelta(t, 100, *update.NormalizedScore, 0.000001)
	require.NotNil(t, update.EvaluatorType)
	require.Equal(t, "exact_match", *update.EvaluatorType)
	require.Equal(t, map[string]any{"answer": "Paris"}, update.RawResponse)
	require.NotNil(t, update.LatencyMS)
	require.Equal(t, 321, *update.LatencyMS)
	require.NotNil(t, update.PromptTokens)
	require.Equal(t, 12, *update.PromptTokens)
	require.NotNil(t, update.CompletionTokens)
	require.Equal(t, 6, *update.CompletionTokens)
	require.NotNil(t, update.TotalTokens)
	require.Equal(t, 18, *update.TotalTokens)
	require.NotNil(t, update.EstimatedCost)
	require.InDelta(t, 0.0123, *update.EstimatedCost, 0.000001)
	require.NotNil(t, update.StartedAt)
	require.NotNil(t, update.FinishedAt)
	require.False(t, update.StartedAt.After(*update.FinishedAt))
	require.True(t, update.ClearErrorCode)
	require.True(t, update.ClearErrorMessage)

	require.Len(t, repo.runStatusCalls, 1)
	require.Equal(t, BenchmarkRunStatusRunning, repo.runStatusCalls[0].status)
	require.Nil(t, repo.runStatusCalls[0].errorMessage)
}

func TestBenchmarkRunnerMarksTimeoutInvalid(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusTimeout, *update.Status)
	require.True(t, update.ClearScore)
	require.True(t, update.ClearMaxScore)
	require.True(t, update.ClearNormalizedScore)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusTimeout, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, context.DeadlineExceeded.Error())
}

func TestBenchmarkRunnerMarksGatewayErrorInvalid(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return nil, errors.New("upstream 502")
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusChannelError, *update.Status)
	require.True(t, update.ClearScore)
	require.True(t, update.ClearMaxScore)
	require.True(t, update.ClearNormalizedScore)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusChannelError, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, "upstream 502")
}

func TestBenchmarkRunnerPreservesGatewayMetricsOnInvalidError(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := newBenchmarkGatewayClient(func(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
		return &BenchmarkGatewayResponse{
			RequestID:        "provider-partial-err",
			Content:          "partial body",
			RawResponse:      map[string]any{"provider_status": 502, "body": "partial"},
			LatencyMS:        456,
			PromptTokens:     9,
			CompletionTokens: 4,
			TotalTokens:      13,
			EstimatedCost:    0.0099,
		}, errors.New("upstream 502")
	})

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusChannelError, *update.Status)
	require.True(t, update.ClearScore)
	require.True(t, update.ClearMaxScore)
	require.True(t, update.ClearNormalizedScore)
	require.Equal(t, map[string]any{"provider_status": 502, "body": "partial"}, update.RawResponse)
	require.NotNil(t, update.RequestID)
	require.Equal(t, "provider-partial-err", *update.RequestID)
	require.NotNil(t, update.LatencyMS)
	require.Equal(t, 456, *update.LatencyMS)
	require.NotNil(t, update.PromptTokens)
	require.Equal(t, 9, *update.PromptTokens)
	require.NotNil(t, update.CompletionTokens)
	require.Equal(t, 4, *update.CompletionTokens)
	require.NotNil(t, update.TotalTokens)
	require.Equal(t, 13, *update.TotalTokens)
	require.NotNil(t, update.EstimatedCost)
	require.InDelta(t, 0.0099, *update.EstimatedCost, 0.000001)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusChannelError, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, "upstream 502")
}

func TestBenchmarkRunnerRequeuesCurrentAndRemainingClaimedResultsOnInfrastructureError(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	firstCtx := benchmarkRunnerTestResultContextWithIDs(101, 201, 401, 601)
	secondCtx := benchmarkRunnerTestResultContextWithIDs(101, 202, 402, 602)
	thirdCtx := benchmarkRunnerTestResultContextWithIDs(101, 203, 403, 603)
	contextByResultID := map[int64]*BenchmarkRunResultContext{
		firstCtx.Result.ID:  firstCtx,
		secondCtx.Result.ID: secondCtx,
		thirdCtx.Result.ID:  thirdCtx,
	}

	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{firstCtx.Result, secondCtx.Result, thirdCtx.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return contextByResultID[resultID], nil
	}
	repo.updateResultFn = func(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error {
		if id == secondCtx.Result.ID {
			return errors.New("write failed")
		}
		return nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{
				RequestID:   "req-success",
				Content:     "Paris",
				RawResponse: map[string]any{"answer": "Paris"},
			}, nil
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), firstCtx.Run.ID)
	require.EqualError(t, err, "write failed")
	require.Len(t, repo.updateCalls, 2)
	require.Len(t, repo.requeueCalls, 1)
	require.Equal(t, []int64{secondCtx.Result.ID, thirdCtx.Result.ID}, repo.requeueCalls[0])
	require.Empty(t, repo.runStatusCalls)
}

func TestBenchmarkRunnerRequeuesWithUsableContextAfterCancellation(t *testing.T) {
	t.Parallel()

	type contextKey string

	repo := newBenchmarkRunnerRepoStub(t)
	firstCtx := benchmarkRunnerTestResultContextWithIDs(101, 201, 401, 601)
	secondCtx := benchmarkRunnerTestResultContextWithIDs(101, 202, 402, 602)
	contextByResultID := map[int64]*BenchmarkRunResultContext{
		firstCtx.Result.ID:  firstCtx,
		secondCtx.Result.ID: secondCtx,
	}

	baseCtx := context.WithValue(context.Background(), contextKey("trace_id"), "trace-123")
	runCtx, cancel := context.WithTimeout(baseCtx, time.Minute)
	defer cancel()

	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{firstCtx.Result, secondCtx.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		if resultID == secondCtx.Result.ID {
			cancel()
			return nil, context.Canceled
		}
		return contextByResultID[resultID], nil
	}
	repo.requeueClaimedResultsFn = func(ctx context.Context, resultIDs []int64) error {
		if err := ctx.Err(); err != nil {
			return errors.New("requeue received canceled ctx")
		}
		if _, ok := ctx.Deadline(); ok {
			return errors.New("requeue received deadline")
		}
		if got := ctx.Value(contextKey("trace_id")); got != "trace-123" {
			return errors.New("requeue lost context values")
		}
		return nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{
				RequestID:   "req-success",
				Content:     "Paris",
				RawResponse: map[string]any{"answer": "Paris"},
			}, nil
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(runCtx, firstCtx.Run.ID)
	require.EqualError(t, err, context.Canceled.Error())
	require.Len(t, repo.requeueCalls, 1)
	require.Equal(t, []int64{secondCtx.Result.ID}, repo.requeueCalls[0])
}

func TestBenchmarkRunnerMarksRateLimitedInvalid(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return nil, newBenchmarkRunnerRateLimitError("too many requests")
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusRateLimited, *update.Status)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusRateLimited, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, "too many requests")
}

func TestBenchmarkRunnerMarksParseErrorInvalid(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	ctxValue.Task.VerifierTypeSnapshot = "json_object"
	ctxValue.Task.VerifierConfigSnapshot = map[string]any{"required_keys": []any{"answer"}}
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{
				RequestID:   "req-parse-1",
				Content:     "not-json",
				RawResponse: map[string]any{"content": "not-json"},
			}, nil
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusParseError, *update.Status)
	require.True(t, update.ClearScore)
	require.True(t, update.ClearMaxScore)
	require.True(t, update.ClearNormalizedScore)
	require.NotNil(t, update.EvaluatorType)
	require.Equal(t, "json_object", *update.EvaluatorType)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusParseError, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, "invalid character")
	require.Equal(t, map[string]any{"content": "not-json"}, update.RawResponse)
	require.Equal(t, map[string]any{"error": *update.ErrorMessage}, update.EvaluatorOutput)
}

func TestBenchmarkRunnerClearsPreviousEvaluatorOutputOnRetrySuccess(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	parseCtx := benchmarkRunnerTestResultContext()
	parseCtx.Task.VerifierTypeSnapshot = "json_object"
	parseCtx.Task.VerifierConfigSnapshot = map[string]any{"required_keys": []any{"answer"}}

	successCtx := benchmarkRunnerTestResultContext()
	successCtx.Result.ID = parseCtx.Result.ID
	successCtx.Result.RunID = parseCtx.Result.RunID
	successCtx.Result.RunTaskID = parseCtx.Result.RunTaskID
	successCtx.Result.RunTargetID = parseCtx.Result.RunTargetID
	successCtx.Result.AttemptCount = 2

	runPhase := 0
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		runPhase++
		switch runPhase {
		case 1:
			return []*ent.BenchmarkResult{parseCtx.Result}, nil
		case 2:
			return []*ent.BenchmarkResult{successCtx.Result}, nil
		default:
			return nil, nil
		}
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		if runPhase == 1 {
			return parseCtx, nil
		}
		return successCtx, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			if runPhase == 1 {
				return &BenchmarkGatewayResponse{
					RequestID:   "req-parse",
					Content:     "not-json",
					RawResponse: map[string]any{"content": "not-json"},
				}, nil
			}
			return &BenchmarkGatewayResponse{
				RequestID:   "req-success",
				Content:     "Paris",
				RawResponse: map[string]any{"answer": "Paris"},
			}, nil
		},
	}

	runner := NewBenchmarkRunner(repo, client)
	require.NoError(t, runner.RunOnce(context.Background(), parseCtx.Run.ID))
	require.NoError(t, runner.RunOnce(context.Background(), successCtx.Run.ID))
	require.Len(t, repo.updateCalls, 2)

	parseUpdate := repo.updateCalls[0].input
	require.Equal(t, map[string]any{"error": *parseUpdate.ErrorMessage}, parseUpdate.EvaluatorOutput)

	successUpdate := repo.updateCalls[1].input
	require.NotNil(t, successUpdate.Status)
	require.Equal(t, BenchmarkResultStatusScored, *successUpdate.Status)
	require.True(t, successUpdate.ClearEvaluatorOutput)
	require.Nil(t, successUpdate.EvaluatorOutput)
}

func TestBenchmarkRunnerMarksVerifierHardErrorInvalid(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	ctxValue.Task.VerifierTypeSnapshot = "unsupported_verifier"
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{BenchmarkResultStatusPending: 1}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{
				RequestID:   "req-verifier-1",
				Content:     "Paris",
				RawResponse: map[string]any{"answer": "Paris"},
			}, nil
		},
	}

	err := NewBenchmarkRunner(repo, client).RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, repo.updateCalls, 1)
	update := repo.updateCalls[0].input
	require.NotNil(t, update.Status)
	require.Equal(t, BenchmarkResultStatusVerifierError, *update.Status)
	require.True(t, update.ClearScore)
	require.True(t, update.ClearMaxScore)
	require.True(t, update.ClearNormalizedScore)
	require.NotNil(t, update.ErrorCode)
	require.Equal(t, BenchmarkResultStatusVerifierError, *update.ErrorCode)
	require.NotNil(t, update.ErrorMessage)
	require.Contains(t, *update.ErrorMessage, "unsupported benchmark verifier type")
}

func TestBenchmarkRunnerMovesRunToScoringWhenTerminal(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return nil, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored:  1,
			BenchmarkResultStatusTimeout: 1,
		}, nil
	}

	err := NewBenchmarkRunner(repo, &benchmarkRunnerClientStub{}).RunOnce(context.Background(), 123)
	require.NoError(t, err)
	require.Len(t, repo.runStatusCalls, 1)
	require.Equal(t, int64(123), repo.runStatusCalls[0].runID)
	require.Equal(t, BenchmarkRunStatusScoring, repo.runStatusCalls[0].status)
	require.Nil(t, repo.runStatusCalls[0].errorMessage)
}

func TestBenchmarkRunnerUsesSettingServiceDefaultTimeoutWhenRunConfigMissing(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	ctxValue.Run.ConfigSnapshot = map[string]any{}
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored: 1,
		}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{RequestID: "req-timeout-default", Content: "Paris"}, nil
		},
	}

	runner := NewBenchmarkRunner(repo, client)
	runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: 45,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
				HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
			},
		},
	})

	err := runner.RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, client.requests, 1)
	require.Equal(t, 45*time.Second, client.requests[0].Timeout)
}

func TestBenchmarkRunnerPrefersRunRuntimeTimeoutOverSettingDefault(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored: 1,
		}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{RequestID: "req-timeout-explicit", Content: "Paris"}, nil
		},
	}

	runner := NewBenchmarkRunner(repo, client)
	runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: 45,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
				HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
			},
		},
	})

	err := runner.RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, client.requests, 1)
	require.Equal(t, 30*time.Second, client.requests[0].Timeout)
}

func TestBenchmarkRunnerSupportsFloatingPointRuntimeTimeoutOverride(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	ctxValue := benchmarkRunnerTestResultContext()
	ctxValue.Run.ConfigSnapshot = map[string]any{
		"runtime_config": map[string]any{"timeout": 30.5},
	}
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return []*ent.BenchmarkResult{ctxValue.Result}, nil
	}
	repo.getRunResultContextFn = func(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
		return ctxValue, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored: 1,
		}, nil
	}

	client := &benchmarkRunnerClientStub{
		executeFn: func(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
			return &BenchmarkGatewayResponse{RequestID: "req-timeout-float", Content: "Paris"}, nil
		},
	}

	runner := NewBenchmarkRunner(repo, client)
	runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: 45,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
				HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
			},
		},
	})

	err := runner.RunOnce(context.Background(), ctxValue.Run.ID)
	require.NoError(t, err)
	require.Len(t, client.requests, 1)
	require.Equal(t, 30500*time.Millisecond, client.requests[0].Timeout)
}

func TestBenchmarkRunnerUsesSettingServiceGlobalConcurrencyForClaimLimit(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return nil, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored: 1,
		}, nil
	}

	runner := NewBenchmarkRunner(repo, &benchmarkRunnerClientStub{})
	runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     7,
			DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
				HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
			},
		},
	})

	err := runner.RunOnce(context.Background(), 123)
	require.NoError(t, err)
	require.Equal(t, 7, repo.claimPendingResultsCall.limit)
}

func TestBenchmarkRunnerPrefersRunRuntimeConcurrencyOverSettingDefault(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkRunnerRepoStub(t)
	repo.getRunFn = func(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
		require.Equal(t, int64(123), id)
		return &ent.BenchmarkRun{
			ID:     id,
			Status: BenchmarkRunStatusRunning,
			ConfigSnapshot: map[string]any{
				"runtime_config": map[string]any{
					"max_concurrency": 3,
				},
			},
		}, nil
	}
	repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
		return nil, nil
	}
	repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
		return map[string]int{
			BenchmarkResultStatusScored: 1,
		}, nil
	}

	runner := NewBenchmarkRunner(repo, &benchmarkRunnerClientStub{})
	runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     7,
			DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
				HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
			},
		},
	})

	err := runner.RunOnce(context.Background(), 123)
	require.NoError(t, err)
	require.Equal(t, 3, repo.claimPendingResultsCall.limit)
}

func TestBenchmarkRunnerIgnoresNonIntegerRuntimeConcurrencyOverride(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		override any
	}{
		{name: "fraction less than one", override: 0.5},
		{name: "fraction greater than one", override: 3.7},
		{name: "negative integer", override: -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := newBenchmarkRunnerRepoStub(t)
			repo.getRunFn = func(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
				return &ent.BenchmarkRun{
					ID:     id,
					Status: BenchmarkRunStatusRunning,
					ConfigSnapshot: map[string]any{
						"runtime_config": map[string]any{
							"max_concurrency": tc.override,
						},
					},
				}, nil
			}
			repo.claimPendingResultsFn = func(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
				return nil, nil
			}
			repo.countRunResultsFn = func(ctx context.Context, runID int64) (map[string]int, error) {
				return map[string]int{
					BenchmarkResultStatusScored: 1,
				}, nil
			}

			runner := NewBenchmarkRunner(repo, &benchmarkRunnerClientStub{})
			runner.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
				runtime: BenchmarkRuntime{
					Enabled:               true,
					PublicEnabled:         true,
					GlobalConcurrency:     7,
					DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
					ConfidenceThresholds: BenchmarkConfidenceThresholds{
						MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
						HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
					},
				},
			})

			err := runner.RunOnce(context.Background(), 123)
			require.NoError(t, err)
			require.Equal(t, 7, repo.claimPendingResultsCall.limit)
		})
	}
}

func benchmarkRunnerTestResultContext() *BenchmarkRunResultContext {
	return benchmarkRunnerTestResultContextWithIDs(101, 201, 401, 601)
}

func benchmarkRunnerTestResultContextWithIDs(runID, targetID, taskID, resultID int64) *BenchmarkRunResultContext {
	run := &ent.BenchmarkRun{
		ID:     runID,
		Status: BenchmarkRunStatusRunning,
		ConfigSnapshot: map[string]any{
			"runtime_config": map[string]any{"timeout": 30},
		},
	}
	target := &ent.BenchmarkRunTarget{
		ID:        targetID,
		RunID:     run.ID,
		TargetID:  targetID + 100,
		ModelName: "gpt-test",
		ChannelID: 77,
	}
	task := &ent.BenchmarkRunTask{
		ID:                     taskID,
		RunID:                  run.ID,
		TaskID:                 taskID + 100,
		PromptSnapshot:         "What is the capital of France?",
		VerifierTypeSnapshot:   "exact_match",
		VerifierConfigSnapshot: map[string]any{"expected": "Paris"},
		TaskSnapshot: map[string]any{
			"input_payload": map[string]any{
				"question": "capital of France",
			},
		},
	}
	result := &ent.BenchmarkResult{
		ID:           resultID,
		RunID:        run.ID,
		RunTaskID:    task.ID,
		RunTargetID:  target.ID,
		Status:       BenchmarkResultStatusRunning,
		AttemptCount: 1,
	}
	return &BenchmarkRunResultContext{
		Result: result,
		Run:    run,
		Target: target,
		Task:   task,
	}
}
