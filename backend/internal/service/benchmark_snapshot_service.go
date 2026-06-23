package service

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

type BenchmarkSnapshotService struct {
	repo BenchmarkRepository
}

func NewBenchmarkSnapshotService(repo BenchmarkRepository) *BenchmarkSnapshotService {
	return &BenchmarkSnapshotService{repo: repo}
}

type BenchmarkPublicRadar struct {
	RankingBasis string                  `json:"ranking_basis"`
	LatestRun    *BenchmarkPublicRun     `json:"latest_run,omitempty"`
	Targets      []BenchmarkPublicTarget `json:"targets"`
}

type BenchmarkPublicRun struct {
	ID          int64      `json:"id"`
	SuiteID     int64      `json:"suite_id"`
	ProfileID   int64      `json:"profile_id"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type BenchmarkPublicTarget struct {
	Rank         int                       `json:"rank"`
	Model        string                    `json:"model"`
	ChannelID    int64                     `json:"channel_id"`
	ChannelName  string                    `json:"channel_name,omitempty"`
	DisplayName  string                    `json:"display_name,omitempty"`
	OverallScore float64                   `json:"overall_score"`
	Dimensions   map[string]float64        `json:"dimensions,omitempty"`
	ScoreBasis   BenchmarkPublicScoreBasis `json:"score_basis"`
	Metrics      BenchmarkPublicMetrics    `json:"metrics"`
}

type BenchmarkPublicScoreBasis struct {
	PlannedTasks       int     `json:"planned_tasks"`
	ScoredTasks        int     `json:"scored_tasks"`
	InvalidTasks       int     `json:"invalid_tasks"`
	CoverageRate       float64 `json:"coverage_rate"`
	ConfidenceLevel    string  `json:"confidence_level"`
	InsufficientSample bool    `json:"insufficient_sample"`
}

type BenchmarkPublicMetrics struct {
	SuccessRate    float64  `json:"success_rate"`
	LatencyP50MS   *float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95MS   *float64 `json:"latency_p95_ms,omitempty"`
	AvgTotalTokens *float64 `json:"avg_total_tokens,omitempty"`
	EstimatedCost  float64  `json:"estimated_cost"`
}

func (s *BenchmarkSnapshotService) BuildScoreSnapshots(ctx context.Context, runID int64) error {
	inputs, err := s.repo.ListRunScoreInputs(ctx, runID)
	if err != nil {
		return err
	}

	grouped := make(map[int64][]BenchmarkRunScoreInput)
	for _, input := range inputs {
		if input.RunTarget == nil || input.RunTask == nil || input.Result == nil {
			continue
		}
		grouped[input.RunTarget.ID] = append(grouped[input.RunTarget.ID], input)
	}

	snapshots := make([]BenchmarkScoreSnapshotInput, 0, len(grouped))
	for runTargetID, group := range grouped {
		abilityResults := make([]BenchmarkScoredResult, 0, len(group))
		invalidReasonBreakdown := make(map[string]int)
		latencies := make([]int, 0, len(group))
		var tokenSum int
		var tokenSamples int
		var successCount int
		var estimatedCost float64

		for _, input := range group {
			result := input.Result
			runTask := input.RunTask

			var normalizedScore float64
			if result.NormalizedScore != nil {
				normalizedScore = *result.NormalizedScore
			}

			var latencyMS int
			if result.LatencyMs != nil {
				latencyMS = *result.LatencyMs
				if latencyMS > 0 {
					latencies = append(latencies, latencyMS)
				}
			}

			abilityResults = append(abilityResults, BenchmarkScoredResult{
				TaskID:           runTask.TaskID,
				TaskType:         runTask.Type,
				Weight:           runTask.WeightSnapshot,
				Status:           result.Status,
				NormalizedScore:  normalizedScore,
				LatencyMS:        latencyMS,
				PromptTokens:     result.PromptTokens,
				CompletionTokens: result.CompletionTokens,
				TotalTokens:      result.TotalTokens,
				EstimatedCost:    result.EstimatedCost,
			})

			if result.Status == BenchmarkResultStatusScored {
				successCount++
			} else {
				invalidReasonBreakdown[result.Status]++
			}

			tokenCount := result.TotalTokens
			if tokenCount <= 0 && result.PromptTokens+result.CompletionTokens > 0 {
				tokenCount = result.PromptTokens + result.CompletionTokens
			}
			if tokenCount > 0 {
				tokenSum += tokenCount
				tokenSamples++
			}
			estimatedCost += result.EstimatedCost
		}

		score := ComputeBenchmarkTargetScore(abilityResults, BenchmarkConfidenceThresholds{})
		if len(group) > 0 {
			score.SuccessRate = float64(successCount) / float64(len(group))
		}
		if len(latencies) > 0 {
			sort.Ints(latencies)
			p50 := float64(upperInclusivePercentile(latencies, 0.50))
			p95 := float64(upperInclusivePercentile(latencies, 0.95))
			score.LatencyP50MS = int(p50)
			score.LatencyP95MS = int(p95)
		}
		if tokenSamples > 0 {
			score.AvgTotalTokens = float64(tokenSum) / float64(tokenSamples)
		} else {
			score.AvgTotalTokens = 0
		}
		score.EstimatedCost = estimatedCost

		snapshot := BenchmarkScoreSnapshotInput{
			RunTargetID:            runTargetID,
			OverallScore:           score.OverallScore,
			DimensionScores:        benchmarkFloatMapToAny(score.DimensionScores),
			PlannedTasks:           score.PlannedTasks,
			ScoredTasks:            score.ScoredTasks,
			InvalidTasks:           score.InvalidTasks,
			CoverageRate:           score.CoverageRate,
			ConfidenceLevel:        score.ConfidenceLevel,
			InsufficientSample:     score.InsufficientSample,
			SuccessRate:            score.SuccessRate,
			EstimatedCost:          score.EstimatedCost,
			InvalidReasonBreakdown: benchmarkIntMapToAny(invalidReasonBreakdown),
			RankingMetadata:        map[string]any{},
		}
		if len(latencies) > 0 {
			p50 := float64(score.LatencyP50MS)
			p95 := float64(score.LatencyP95MS)
			snapshot.LatencyP50MS = &p50
			snapshot.LatencyP95MS = &p95
		}
		if tokenSamples > 0 {
			avgTokens := score.AvgTotalTokens
			snapshot.AvgTotalTokens = &avgTokens
		}
		snapshots = append(snapshots, snapshot)
	}

	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].OverallScore != snapshots[j].OverallScore {
			return snapshots[i].OverallScore > snapshots[j].OverallScore
		}
		if snapshots[i].CoverageRate != snapshots[j].CoverageRate {
			return snapshots[i].CoverageRate > snapshots[j].CoverageRate
		}
		return snapshots[i].RunTargetID < snapshots[j].RunTargetID
	})
	for i := range snapshots {
		snapshots[i].RankingMetadata = map[string]any{
			"rank": i + 1,
		}
	}

	return s.repo.SaveScoreSnapshots(ctx, runID, snapshots)
}

func (s *BenchmarkSnapshotService) PublishPublicSnapshot(ctx context.Context, runID int64) error {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	scoreSnapshots, err := s.repo.ListScoreSnapshots(ctx, runID)
	if err != nil {
		return err
	}

	scoreInputs, err := s.repo.ListRunScoreInputs(ctx, runID)
	if err != nil {
		return err
	}

	runTargets := make(map[int64]*ent.BenchmarkRunTarget)
	for _, input := range scoreInputs {
		if input.RunTarget == nil {
			continue
		}
		if _, exists := runTargets[input.RunTarget.ID]; exists {
			continue
		}
		runTargets[input.RunTarget.ID] = input.RunTarget
	}

	radar := BenchmarkPublicRadar{
		RankingBasis: benchmarkRankingBasis(run.ConfigSnapshot),
		LatestRun: &BenchmarkPublicRun{
			ID:          run.ID,
			SuiteID:     run.SuiteID,
			ProfileID:   run.ProfileID,
			Status:      run.Status,
			CompletedAt: run.FinishedAt,
		},
		Targets: make([]BenchmarkPublicTarget, 0, len(scoreSnapshots)),
	}

	for index, snapshot := range scoreSnapshots {
		runTarget := runTargets[snapshot.RunTargetID]
		target := BenchmarkPublicTarget{
			Rank:         index + 1,
			OverallScore: snapshot.OverallScore,
			Dimensions:   benchmarkAnyMapToFloatMap(snapshot.DimensionScores),
			ScoreBasis: BenchmarkPublicScoreBasis{
				PlannedTasks:       snapshot.PlannedTasks,
				ScoredTasks:        snapshot.ScoredTasks,
				InvalidTasks:       snapshot.InvalidTasks,
				CoverageRate:       snapshot.CoverageRate,
				ConfidenceLevel:    string(snapshot.ConfidenceLevel),
				InsufficientSample: snapshot.InsufficientSample,
			},
			Metrics: BenchmarkPublicMetrics{
				SuccessRate:    snapshot.SuccessRate,
				LatencyP50MS:   benchmarkCloneFloat64Ptr(snapshot.LatencyP50Ms),
				LatencyP95MS:   benchmarkCloneFloat64Ptr(snapshot.LatencyP95Ms),
				AvgTotalTokens: benchmarkCloneFloat64Ptr(snapshot.AvgTotalTokens),
				EstimatedCost:  snapshot.EstimatedCost,
			},
		}
		if runTarget != nil {
			target.Model = runTarget.ModelName
			target.ChannelID = runTarget.ChannelID
			target.ChannelName = stringFromPtr(runTarget.ChannelNameSnapshot)
			target.DisplayName = stringFromPtr(runTarget.DisplayNameSnapshot)
		}
		if rank, ok := benchmarkIntFromAny(snapshot.RankingMetadata["rank"]); ok {
			target.Rank = rank
		}
		radar.Targets = append(radar.Targets, target)
	}
	sort.SliceStable(radar.Targets, func(i, j int) bool {
		return radar.Targets[i].Rank < radar.Targets[j].Rank
	})

	payload, err := benchmarkPublicRadarPayload(radar)
	if err != nil {
		return err
	}

	publishedAt := time.Now().UTC()
	return s.repo.PublishPublicSnapshot(ctx, BenchmarkPublicSnapshotInput{
		RunID:       run.ID,
		SuiteID:     run.SuiteID,
		ProfileID:   run.ProfileID,
		Snapshot:    payload,
		PublishedAt: &publishedAt,
	})
}

func (s *BenchmarkSnapshotService) GetPublicRadar(ctx context.Context) (*BenchmarkPublicRadar, error) {
	snapshot, err := s.repo.GetLatestPublicSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}

	payload, err := json.Marshal(snapshot.Snapshot)
	if err != nil {
		return nil, err
	}

	var radar BenchmarkPublicRadar
	if err := json.Unmarshal(payload, &radar); err != nil {
		return nil, err
	}
	return &radar, nil
}

func benchmarkRankingBasis(config map[string]any) string {
	if config == nil {
		return "ability_score_only"
	}
	value, ok := config["ranking_basis"].(string)
	if !ok || value == "" {
		return "ability_score_only"
	}
	return value
}

func benchmarkPublicRadarPayload(radar BenchmarkPublicRadar) (map[string]any, error) {
	payload, err := json.Marshal(radar)
	if err != nil {
		return nil, err
	}

	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func benchmarkFloatMapToAny(values map[string]float64) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func benchmarkIntMapToAny(values map[string]int) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func benchmarkAnyMapToFloatMap(values map[string]any) map[string]float64 {
	if values == nil {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case float64:
			out[key] = typed
		case int:
			out[key] = float64(typed)
		case int64:
			out[key] = float64(typed)
		}
	}
	return out
}

func benchmarkIntFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func benchmarkCloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
