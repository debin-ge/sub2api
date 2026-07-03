package service

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

func TestComputeBenchmarkTargetScoreWeightedPassRate(t *testing.T) {
	results := []BenchmarkScoredResult{
		{TaskType: "reasoning", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 100, LatencyMS: 800, TotalTokens: 100, EstimatedCost: 0.01},
		{TaskType: "coding", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 0, LatencyMS: 1000, TotalTokens: 200, EstimatedCost: 0.02},
		// invalid result: excluded from ability score, counted in breakdown + cost
		{TaskType: "coding", Weight: 1, Status: BenchmarkResultStatusParseError, NormalizedScore: 0, EstimatedCost: 0.03},
	}

	score := ComputeBenchmarkTargetScore(results)

	if !approxEqual(score.OverallScore, 50) {
		t.Fatalf("expected overall 50, got %v", score.OverallScore)
	}
	if score.PassedCount != 1 {
		t.Fatalf("expected passed=1 (only normalized 100 counts as pass via >0), got %d", score.PassedCount)
	}
	if score.TotalCount != 3 {
		t.Fatalf("expected total=3, got %d", score.TotalCount)
	}
	if score.InvalidReasonBreakdown[BenchmarkResultStatusParseError] != 1 {
		t.Fatalf("expected 1 parse_error in breakdown, got %v", score.InvalidReasonBreakdown)
	}
	if !approxEqual(score.TotalCost, 0.06) {
		t.Fatalf("expected total cost 0.06, got %v", score.TotalCost)
	}
	if !approxEqual(score.DimensionScores["reasoning"], 100) {
		t.Fatalf("expected reasoning dim 100, got %v", score.DimensionScores["reasoning"])
	}
	if !approxEqual(score.DimensionScores["coding"], 0) {
		t.Fatalf("expected coding dim 0, got %v", score.DimensionScores["coding"])
	}
	if !approxEqual(score.AvgLatencyMS, 900) {
		t.Fatalf("expected avg latency 900, got %v", score.AvgLatencyMS)
	}
}

func TestComputeBenchmarkTargetScorePassedCount(t *testing.T) {
	results := []BenchmarkScoredResult{
		{TaskType: "a", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 100},
		{TaskType: "a", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 100},
		{TaskType: "a", Weight: 1, Status: BenchmarkResultStatusScored, NormalizedScore: 0},
	}
	score := ComputeBenchmarkTargetScore(results)
	// passed = results with score > 0; 2 of 3
	if score.PassedCount != 2 {
		t.Fatalf("expected passed 2, got %d", score.PassedCount)
	}
	if !approxEqual(score.OverallScore, 200.0/3.0) {
		t.Fatalf("expected overall 66.67, got %v", score.OverallScore)
	}
}

func TestComputeBenchmarkTargetScoreNoScored(t *testing.T) {
	results := []BenchmarkScoredResult{
		{TaskType: "a", Weight: 1, Status: BenchmarkResultStatusTimeout},
	}
	score := ComputeBenchmarkTargetScore(results)
	if score.OverallScore != 0 {
		t.Fatalf("expected 0 overall when no scored, got %v", score.OverallScore)
	}
	if score.PassedCount != 0 || score.TotalCount != 1 {
		t.Fatalf("expected passed 0 total 1, got %d/%d", score.PassedCount, score.TotalCount)
	}
}
