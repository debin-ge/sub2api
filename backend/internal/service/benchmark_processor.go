package service

import (
	"context"
	"errors"
)

const (
	benchmarkProcessorDefaultRunLimit = 5
	benchmarkProcessorMaxRunLimit     = 20
)

type BenchmarkProcessOptions struct {
	RunLimit int
}

type benchmarkRunExecutor interface {
	RunOnce(ctx context.Context, runID int64) error
}

type benchmarkRunScorer interface {
	BuildTargetScores(ctx context.Context, runID int64) error
}

type BenchmarkProcessor struct {
	repo    BenchmarkRepository
	runner  benchmarkRunExecutor
	scorer  benchmarkRunScorer
}

func NewBenchmarkProcessor(repo BenchmarkRepository, runner benchmarkRunExecutor, scorer benchmarkRunScorer) *BenchmarkProcessor {
	return &BenchmarkProcessor{repo: repo, runner: runner, scorer: scorer}
}

func (p *BenchmarkProcessor) ProcessDue(ctx context.Context, options BenchmarkProcessOptions) (int, error) {
	runs, err := p.repo.ClaimRunnableRuns(ctx, benchmarkProcessorRunLimit(options.RunLimit))
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, run := range runs {
		if run == nil {
			continue
		}
		count, err := p.ProcessRun(ctx, run.ID)
		processed += count
		if err != nil {
			return processed, err
		}
	}
	return processed, nil
}

func (p *BenchmarkProcessor) ProcessRun(ctx context.Context, runID int64) (int, error) {
	run, err := p.repo.GetRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	if run == nil {
		return 0, errors.New("benchmark run not found")
	}
	if benchmarkProcessorIsTerminalRunStatus(run.Status) {
		return 0, nil
	}

	if run.Status == BenchmarkRunStatusQueued {
		if err := p.repo.MarkRunStarted(ctx, runID); err != nil {
			return 0, err
		}
	}

	counts, err := p.repo.CountRunResultsByStatus(ctx, runID)
	if err != nil {
		return 0, err
	}

	// Still has pending/running work: execute one batch.
	if counts[BenchmarkResultStatusPending] > 0 || counts[BenchmarkResultStatusRunning] > 0 {
		if err := p.runner.RunOnce(ctx, runID); err != nil {
			p.markRunFailed(ctx, runID, err)
			return 1, err
		}
		return 1, nil
	}

	// All results terminal: aggregate scores and finish.
	if err := p.scorer.BuildTargetScores(ctx, runID); err != nil {
		p.markRunFailed(ctx, runID, err)
		return 1, err
	}
	if err := p.repo.MarkRunFinished(ctx, runID, BenchmarkRunStatusCompleted, nil); err != nil {
		return 1, err
	}
	return 1, nil
}

func (p *BenchmarkProcessor) markRunFailed(ctx context.Context, runID int64, runErr error) {
	errMessage := runErr.Error()
	_ = p.repo.MarkRunFinished(context.WithoutCancel(ctx), runID, BenchmarkRunStatusFailed, &errMessage)
}

func benchmarkProcessorRunLimit(limit int) int {
	if limit <= 0 {
		return benchmarkProcessorDefaultRunLimit
	}
	if limit > benchmarkProcessorMaxRunLimit {
		return benchmarkProcessorMaxRunLimit
	}
	return limit
}

func benchmarkProcessorIsTerminalRunStatus(status string) bool {
	switch status {
	case BenchmarkRunStatusCompleted, BenchmarkRunStatusFailed, BenchmarkRunStatusCanceled:
		return true
	default:
		return false
	}
}
