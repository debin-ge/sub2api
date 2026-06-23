package service

import "testing"

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
	if score.OverallScore != 86.66666666666667 {
		t.Fatalf("overall score = %v", score.OverallScore)
	}
	if score.CoverageRate != 0.6666666666666666 {
		t.Fatalf("coverage = %v", score.CoverageRate)
	}
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

	if score.OverallScore != 50 {
		t.Fatalf("overall score = %v", score.OverallScore)
	}
	if score.LatencyP50MS != 300 || score.LatencyP95MS != 300 {
		t.Fatalf("latency p50/p95 = %d/%d", score.LatencyP50MS, score.LatencyP95MS)
	}
	if score.AvgTotalTokens != 20 {
		t.Fatalf("avg tokens = %v", score.AvgTotalTokens)
	}
	if score.EstimatedCost != 0.03 {
		t.Fatalf("cost = %v", score.EstimatedCost)
	}
}
