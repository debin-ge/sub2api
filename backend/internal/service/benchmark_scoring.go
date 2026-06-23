package service

import (
	"math"
	"sort"
	"time"
)

func ComputeBenchmarkTargetScore(results []BenchmarkScoredResult, thresholds BenchmarkConfidenceThresholds) BenchmarkTargetScore {
	if thresholds.HighCoverage <= 0 {
		thresholds.HighCoverage = 0.9
	}
	if thresholds.MediumCoverage <= 0 {
		thresholds.MediumCoverage = 0.7
	}

	out := BenchmarkTargetScore{
		PlannedTasks:    len(results),
		DimensionScores: map[string]float64{},
		ComputedAt:      time.Now(),
	}
	var weightedSum float64
	var weightSum float64
	var tokenSum int
	var latencies []int
	dimWeighted := map[string]float64{}
	dimWeights := map[string]float64{}

	for _, result := range results {
		if result.Status != BenchmarkResultStatusScored {
			out.InvalidTasks++
			continue
		}
		out.ScoredTasks++
		weight := result.Weight
		if weight <= 0 {
			weight = 1
		}
		weightedSum += result.NormalizedScore * weight
		weightSum += weight
		dimWeighted[result.TaskType] += result.NormalizedScore * weight
		dimWeights[result.TaskType] += weight
		if result.LatencyMS > 0 {
			latencies = append(latencies, result.LatencyMS)
		}
		tokenSum += result.TotalTokens
		out.EstimatedCost += result.EstimatedCost
	}

	if weightSum > 0 {
		out.OverallScore = weightedSum / weightSum
	}
	for typ, sum := range dimWeighted {
		if dimWeights[typ] > 0 {
			out.DimensionScores[typ] = sum / dimWeights[typ]
		}
	}
	if out.PlannedTasks > 0 {
		out.CoverageRate = float64(out.ScoredTasks) / float64(out.PlannedTasks)
		out.SuccessRate = out.CoverageRate
	}
	if out.ScoredTasks > 0 {
		out.AvgTotalTokens = float64(tokenSum) / float64(out.ScoredTasks)
	}
	if len(latencies) > 0 {
		sort.Ints(latencies)
		out.LatencyP50MS = percentileNearestRank(latencies, 0.50)
		out.LatencyP95MS = percentileNearestRank(latencies, 0.95)
	}
	switch {
	case out.CoverageRate >= thresholds.HighCoverage:
		out.ConfidenceLevel = BenchmarkConfidenceHigh
	case out.CoverageRate >= thresholds.MediumCoverage:
		out.ConfidenceLevel = BenchmarkConfidenceMedium
	default:
		out.ConfidenceLevel = BenchmarkConfidenceLow
		out.InsufficientSample = true
	}
	return out
}

func percentileNearestRank(sorted []int, percentile float64) int {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(percentile*float64(len(sorted)+1))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}
