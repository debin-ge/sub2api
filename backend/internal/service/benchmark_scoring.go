package service

import (
	"time"
)

// benchmarkPassThreshold is the normalized score (0-100) at or above which a
// scored task counts as "passed" for the X/N display. Binary verifiers emit
// 100 (pass) or 0 (fail), so 50 cleanly separates them while still allowing
// partial-credit verifiers in the future.
const benchmarkPassThreshold = 50.0

// ComputeBenchmarkTargetScore aggregates one target's results in a run into an
// ability score (weighted pass rate, 0-100) plus per-dimension scores and
// operational metrics. Only scored results count toward the ability score;
// invalid results are tallied separately so failures stay visible.
func ComputeBenchmarkTargetScore(results []BenchmarkScoredResult) BenchmarkTargetScoreResult {
	out := BenchmarkTargetScoreResult{
		DimensionScores:        map[string]float64{},
		InvalidReasonBreakdown: map[string]int{},
		TotalCount:             len(results),
		ComputedAt:             time.Now(),
	}

	var weightedSum, weightSum float64
	var latencySum, latencyCount float64
	var tokenSum, tokenCount float64
	dimWeighted := map[string]float64{}
	dimWeights := map[string]float64{}

	for _, result := range results {
		out.TotalCost += result.EstimatedCost
		if result.Status != BenchmarkResultStatusScored {
			out.InvalidReasonBreakdown[result.Status]++
			continue
		}
		if result.NormalizedScore >= benchmarkPassThreshold {
			out.PassedCount++
		}
		weight := result.Weight
		if weight <= 0 {
			weight = 1
		}
		weightedSum += result.NormalizedScore * weight
		weightSum += weight
		dimWeighted[result.TaskType] += result.NormalizedScore * weight
		dimWeights[result.TaskType] += weight
		if result.LatencyMS > 0 {
			latencySum += float64(result.LatencyMS)
			latencyCount++
		}
		if result.TotalTokens > 0 {
			tokenSum += float64(result.TotalTokens)
			tokenCount++
		}
	}

	if weightSum > 0 {
		out.OverallScore = weightedSum / weightSum
	}
	for typ, sum := range dimWeighted {
		if dimWeights[typ] > 0 {
			out.DimensionScores[typ] = sum / dimWeights[typ]
		}
	}
	if latencyCount > 0 {
		out.AvgLatencyMS = latencySum / latencyCount
	}
	if tokenCount > 0 {
		out.AvgTotalTokens = tokenSum / tokenCount
	}
	return out
}
