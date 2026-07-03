package service

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const benchmarkRankingBasisValue = "ability_score_only"

type BenchmarkSnapshotService struct {
	repo BenchmarkRepository
}

func NewBenchmarkSnapshotService(repo BenchmarkRepository) *BenchmarkSnapshotService {
	return &BenchmarkSnapshotService{repo: repo}
}

type BenchmarkPublicRadar struct {
	RankingBasis string                  `json:"ranking_basis"`
	PublishedAt  *time.Time              `json:"published_at,omitempty"`
	LatestRun    *BenchmarkPublicRun     `json:"latest_run,omitempty"`
	Targets      []BenchmarkPublicTarget `json:"targets"`
	Trends       []BenchmarkPublicTrend  `json:"trends,omitempty"`
}

type BenchmarkPublicRun struct {
	ID          int64      `json:"id"`
	Status      string     `json:"status"`
	TaskCount   int        `json:"task_count"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type BenchmarkPublicTarget struct {
	Rank         int                    `json:"rank"`
	Model        string                 `json:"model"`
	ChannelID    int64                  `json:"channel_id"`
	ChannelName  string                 `json:"channel_name,omitempty"`
	DisplayName  string                 `json:"display_name,omitempty"`
	OverallScore float64                `json:"overall_score"`
	PassedCount  int                    `json:"passed_count"`
	TotalCount   int                    `json:"total_count"`
	Dimensions   map[string]float64     `json:"dimensions,omitempty"`
	Metrics      BenchmarkPublicMetrics `json:"metrics"`
}

type BenchmarkPublicMetrics struct {
	AvgLatencyMS   *float64 `json:"avg_latency_ms,omitempty"`
	AvgTotalTokens *float64 `json:"avg_total_tokens,omitempty"`
	TotalCost      float64  `json:"total_cost"`
}

type BenchmarkPublicTrend struct {
	Model       string                      `json:"model"`
	ChannelID   int64                        `json:"channel_id"`
	ChannelName string                       `json:"channel_name,omitempty"`
	DisplayName string                       `json:"display_name,omitempty"`
	Points      []BenchmarkPublicTrendPoint `json:"points"`
}

type BenchmarkPublicTrendPoint struct {
	RunID        int64     `json:"run_id"`
	FinishedAt   time.Time `json:"finished_at"`
	OverallScore float64   `json:"overall_score"`
	PassedCount  int       `json:"passed_count"`
	TotalCount   int       `json:"total_count"`
	AvgLatencyMS *float64  `json:"avg_latency_ms,omitempty"`
	TotalCost    float64   `json:"total_cost"`
}

// BuildTargetScores aggregates a finished run's results into per-target scores
// (one trend data point each).
func (s *BenchmarkSnapshotService) BuildTargetScores(ctx context.Context, runID int64) error {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	finishedAt := time.Now().UTC()
	if run != nil && run.FinishedAt != nil {
		finishedAt = *run.FinishedAt
	}

	inputs, err := s.repo.ListRunScoreInputs(ctx, runID)
	if err != nil {
		return err
	}

	grouped := make(map[int64][]BenchmarkRunScoreInput)
	order := make([]int64, 0)
	targetByID := make(map[int64]*ent.BenchmarkRunTarget)
	for _, input := range inputs {
		if input.RunTarget == nil || input.RunTask == nil || input.Result == nil {
			continue
		}
		id := input.RunTarget.ID
		if _, ok := grouped[id]; !ok {
			order = append(order, id)
			targetByID[id] = input.RunTarget
		}
		grouped[id] = append(grouped[id], input)
	}

	scores := make([]BenchmarkTargetScoreInput, 0, len(grouped))
	for _, runTargetID := range order {
		group := grouped[runTargetID]
		runTarget := targetByID[runTargetID]

		abilityResults := make([]BenchmarkScoredResult, 0, len(group))
		for _, input := range group {
			var normalized float64
			if input.Result.NormalizedScore != nil {
				normalized = *input.Result.NormalizedScore
			}
			latency := 0
			if input.Result.LatencyMs != nil {
				latency = *input.Result.LatencyMs
			}
			abilityResults = append(abilityResults, BenchmarkScoredResult{
				TaskID:          input.RunTask.TaskID,
				TaskType:        input.RunTask.Type,
				Weight:          input.RunTask.WeightSnapshot,
				Status:          input.Result.Status,
				NormalizedScore: normalized,
				LatencyMS:       latency,
				TotalTokens:     input.Result.TotalTokens,
				EstimatedCost:   input.Result.EstimatedCost,
			})
		}

		score := ComputeBenchmarkTargetScore(abilityResults)
		input := BenchmarkTargetScoreInput{
			RunTargetID:            runTargetID,
			ModelName:              runTarget.ModelName,
			ChannelID:              runTarget.ChannelID,
			OverallScore:           score.OverallScore,
			PassedCount:            score.PassedCount,
			TotalCount:             score.TotalCount,
			DimensionScores:        benchmarkFloatMapToAny(score.DimensionScores),
			TotalCost:              score.TotalCost,
			InvalidReasonBreakdown: benchmarkIntMapToAny(score.InvalidReasonBreakdown),
			FinishedAt:             finishedAt,
		}
		if score.AvgLatencyMS > 0 {
			avg := score.AvgLatencyMS
			input.AvgLatencyMS = &avg
		}
		if score.AvgTotalTokens > 0 {
			avg := score.AvgTotalTokens
			input.AvgTotalTokens = &avg
		}
		scores = append(scores, input)
	}

	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].OverallScore != scores[j].OverallScore {
			return scores[i].OverallScore > scores[j].OverallScore
		}
		return scores[i].RunTargetID < scores[j].RunTargetID
	})

	return s.repo.SaveTargetScores(ctx, runID, scores)
}

func (s *BenchmarkSnapshotService) PublishPublicSnapshot(ctx context.Context, runID int64) error {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != BenchmarkRunStatusCompleted {
		return infraerrors.Conflict("BENCHMARK_RUN_NOT_COMPLETED", "benchmark run must be completed before publishing")
	}

	scores, err := s.repo.ListTargetScores(ctx, runID)
	if err != nil {
		return err
	}
	if len(scores) == 0 {
		return infraerrors.Conflict("BENCHMARK_SCORE_SNAPSHOT_MISSING", "benchmark run has no target scores")
	}

	runTargets, err := s.repo.ListRunTargets(ctx, runID)
	if err != nil {
		return err
	}
	targetMeta := make(map[int64]*ent.BenchmarkRunTarget, len(runTargets))
	for _, rt := range runTargets {
		targetMeta[rt.ID] = rt
	}

	radar := BenchmarkPublicRadar{
		RankingBasis: benchmarkRankingBasisValue,
		LatestRun: &BenchmarkPublicRun{
			ID:          run.ID,
			Status:      run.Status,
			TaskCount:   run.PlannedTaskCount,
			CompletedAt: run.FinishedAt,
		},
		Targets: make([]BenchmarkPublicTarget, 0, len(scores)),
	}

	for i, score := range scores {
		target := BenchmarkPublicTarget{
			Rank:         i + 1,
			Model:        score.ModelName,
			ChannelID:    score.ChannelID,
			OverallScore: score.OverallScore,
			PassedCount:  score.PassedCount,
			TotalCount:   score.TotalCount,
			Dimensions:   benchmarkAnyMapToFloatMap(score.DimensionScores),
			Metrics: BenchmarkPublicMetrics{
				AvgLatencyMS:   benchmarkCloneFloat64Ptr(score.AvgLatencyMs),
				AvgTotalTokens: benchmarkCloneFloat64Ptr(score.AvgTotalTokens),
				TotalCost:      score.TotalCost,
			},
		}
		if meta := targetMeta[score.RunTargetID]; meta != nil {
			target.ChannelName = stringFromPtr(meta.ChannelNameSnapshot)
			target.DisplayName = stringFromPtr(meta.DisplayNameSnapshot)
		}
		radar.Targets = append(radar.Targets, target)
	}

	publishedAt := time.Now().UTC()
	radar.PublishedAt = &publishedAt

	payload, err := benchmarkPublicRadarPayload(radar)
	if err != nil {
		return err
	}

	return s.repo.PublishPublicSnapshot(ctx, BenchmarkPublicSnapshotInput{
		RunID:       run.ID,
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
	if radar.PublishedAt == nil && !snapshot.PublishedAt.IsZero() {
		publishedAt := snapshot.PublishedAt
		radar.PublishedAt = &publishedAt
	}
	return &radar, nil
}

// GetTrends returns per-target score history grouped by model+channel, oldest
// first. days bounds the window; limit caps the rows scanned.
func (s *BenchmarkSnapshotService) GetTrends(ctx context.Context, days int, limit int) ([]BenchmarkPublicTrend, error) {
	if days <= 0 {
		days = 30
	}
	if limit <= 0 {
		limit = 2000
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	scores, err := s.repo.ListTrendScores(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	return buildBenchmarkTrends(scores), nil
}

func buildBenchmarkTrends(scores []*ent.BenchmarkTargetScore) []BenchmarkPublicTrend {
	type key struct {
		model     string
		channelID int64
	}
	grouped := make(map[key]*BenchmarkPublicTrend)
	order := make([]key, 0)
	for _, score := range scores {
		k := key{model: score.ModelName, channelID: score.ChannelID}
		trend, ok := grouped[k]
		if !ok {
			trend = &BenchmarkPublicTrend{Model: score.ModelName, ChannelID: score.ChannelID}
			grouped[k] = trend
			order = append(order, k)
		}
		point := BenchmarkPublicTrendPoint{
			RunID:        score.RunID,
			FinishedAt:   score.FinishedAt,
			OverallScore: score.OverallScore,
			PassedCount:  score.PassedCount,
			TotalCount:   score.TotalCount,
			AvgLatencyMS: benchmarkCloneFloat64Ptr(score.AvgLatencyMs),
			TotalCost:    score.TotalCost,
		}
		trend.Points = append(trend.Points, point)
	}
	out := make([]BenchmarkPublicTrend, 0, len(order))
	for _, k := range order {
		trend := grouped[k]
		sort.SliceStable(trend.Points, func(i, j int) bool {
			return trend.Points[i].FinishedAt.Before(trend.Points[j].FinishedAt)
		})
		out = append(out, *trend)
	}
	return out
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

func benchmarkCloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
