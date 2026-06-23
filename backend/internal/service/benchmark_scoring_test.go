package service

import (
	"math"
	"testing"
)

func TestComputeBenchmarkTargetScoreExcludesInvalidResults(t *testing.T) {
	t.Parallel()

	score := ComputeBenchmarkTargetScore([]BenchmarkScoredResult{
		{TaskID: 1, TaskType: "math", Weight: 2, Status: BenchmarkResultStatusScored, NormalizedScore: 80, LatencyMS: 100, TotalTokens: 50, EstimatedCost: 0.01},
		{TaskID: 2, TaskType: "math", Weight: 1, Status: BenchmarkResultStatusTimeout},
		{TaskID: 3, TaskType: "coding", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 100, LatencyMS: 300, TotalTokens: 150, EstimatedCost: 0.03},
	}, BenchmarkConfidenceThresholds{HighCoverage: 0.9, MediumCoverage: 0.7})

	if score.PlannedTasks != 3 || score.ScoredTasks != 2 || score.InvalidTasks != 1 {
		t.Fatalf("basis = %#v", score)
	}
	assertFloatNear(t, score.OverallScore, 86.66666666666667)
	assertFloatNear(t, score.CoverageRate, 0.6666666666666666)
	if score.ConfidenceLevel != BenchmarkConfidenceLow || !score.InsufficientSample {
		t.Fatalf("confidence = %s insufficient=%v", score.ConfidenceLevel, score.InsufficientSample)
	}
}

func TestComputeBenchmarkTargetScoreMetricsAreSeparate(t *testing.T) {
	t.Parallel()

	score := ComputeBenchmarkTargetScore([]BenchmarkScoredResult{
		{TaskID: 1, TaskType: "math", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 50, LatencyMS: 300, TotalTokens: 30, EstimatedCost: 0.02},
		{TaskID: 2, TaskType: "math", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 50, LatencyMS: 100, TotalTokens: 10, EstimatedCost: 0.01},
	}, BenchmarkConfidenceThresholds{HighCoverage: 0.9, MediumCoverage: 0.7})

	assertFloatNear(t, score.OverallScore, 50)
	if score.LatencyP50MS != 300 || score.LatencyP95MS != 300 {
		t.Fatalf("latency p50/p95 = %d/%d", score.LatencyP50MS, score.LatencyP95MS)
	}
	assertFloatNear(t, score.AvgTotalTokens, 20)
	assertFloatNear(t, score.EstimatedCost, 0.03)
}

func TestUpperInclusivePercentileBoundaries(t *testing.T) {
	t.Parallel()

	if got := upperInclusivePercentile(nil, 0.50); got != 0 {
		t.Fatalf("empty percentile = %d", got)
	}
	if got := upperInclusivePercentile([]int{100}, 0.95); got != 100 {
		t.Fatalf("single percentile = %d", got)
	}
	if got := upperInclusivePercentile([]int{100, 300}, 0.50); got != 300 {
		t.Fatalf("two-sample p50 = %d", got)
	}
	if got := upperInclusivePercentile([]int{100, 300}, 0.95); got != 300 {
		t.Fatalf("two-sample p95 = %d", got)
	}
}

func assertFloatNear(t *testing.T, got, want float64) {
	t.Helper()

	const tolerance = 1e-9
	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %v, want %v", got, want)
	}
}
