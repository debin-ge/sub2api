package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestBenchmarkSnapshotComputesAbilityOnlyRanking(t *testing.T) {
	t.Parallel()

	const runID int64 = 77

	var savedRunID int64
	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, gotRunID int64) ([]BenchmarkRunScoreInput, error) {
		require.Equal(t, runID, gotRunID)
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(101, 201, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(90),
				latencyMS:       intPtr(120),
				totalTokens:     12,
				estimatedCost:   0.02,
			}),
			benchmarkRunScoreInput(101, 202, "coding", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{
				latencyMS:     intPtr(700),
				totalTokens:   80,
				estimatedCost: 0.30,
			}),
			benchmarkRunScoreInput(102, 201, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(80),
				latencyMS:       intPtr(100),
				totalTokens:     10,
				estimatedCost:   0.01,
			}),
			benchmarkRunScoreInput(102, 202, "coding", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(80),
				latencyMS:       intPtr(110),
				totalTokens:     11,
				estimatedCost:   0.01,
			}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, gotRunID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedRunID = gotRunID
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	svc := NewBenchmarkSnapshotService(repo)

	err := svc.BuildScoreSnapshots(context.Background(), runID)
	require.NoError(t, err)

	require.Equal(t, runID, savedRunID)
	require.Len(t, savedSnapshots, 2)

	top := benchmarkSnapshotByRunTargetID(t, savedSnapshots, 101)
	require.InDelta(t, 90.0, top.OverallScore, 0.000001)
	require.InDelta(t, 0.5, top.CoverageRate, 0.000001)
	require.True(t, top.InsufficientSample)
	require.Equal(t, 1, benchmarkSnapshotRank(t, top.RankingMetadata))

	second := benchmarkSnapshotByRunTargetID(t, savedSnapshots, 102)
	require.InDelta(t, 80.0, second.OverallScore, 0.000001)
	require.InDelta(t, 1.0, second.CoverageRate, 0.000001)
	require.False(t, second.InsufficientSample)
	require.Equal(t, 2, benchmarkSnapshotRank(t, second.RankingMetadata))
}

func TestBenchmarkSnapshotKeepsInsufficientSampleInRanking(t *testing.T) {
	t.Parallel()

	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error) {
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(201, 301, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(100),
			}),
			benchmarkRunScoreInput(201, 302, "coding", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{}),
			benchmarkRunScoreInput(202, 301, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(90),
			}),
			benchmarkRunScoreInput(202, 302, "coding", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(90),
			}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).BuildScoreSnapshots(context.Background(), 88)
	require.NoError(t, err)

	require.Len(t, savedSnapshots, 2)
	top := benchmarkSnapshotByRunTargetID(t, savedSnapshots, 201)
	require.True(t, top.InsufficientSample)
	require.Equal(t, 1, benchmarkSnapshotRank(t, top.RankingMetadata))

	second := benchmarkSnapshotByRunTargetID(t, savedSnapshots, 202)
	require.False(t, second.InsufficientSample)
	require.Equal(t, 2, benchmarkSnapshotRank(t, second.RankingMetadata))
}

func TestBenchmarkSnapshotExcludesInvalidSamplesFromScore(t *testing.T) {
	t.Parallel()

	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error) {
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(301, 401, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(70),
			}),
			benchmarkRunScoreInput(301, 402, "reasoning", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{}),
			benchmarkRunScoreInput(301, 403, "reasoning", 1, BenchmarkResultStatusParseError, benchmarkScoreInputOptions{}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).BuildScoreSnapshots(context.Background(), 99)
	require.NoError(t, err)

	require.Len(t, savedSnapshots, 1)
	snapshot := savedSnapshots[0]
	require.InDelta(t, 70.0, snapshot.OverallScore, 0.000001)
	require.Equal(t, 3, snapshot.PlannedTasks)
	require.Equal(t, 1, snapshot.ScoredTasks)
	require.Equal(t, 2, snapshot.InvalidTasks)
	benchmarkRequireMapFloat(t, snapshot.DimensionScores, "reasoning", 70)
	require.Equal(t, 1, benchmarkSnapshotMapInt(t, snapshot.InvalidReasonBreakdown, BenchmarkResultStatusTimeout))
	require.Equal(t, 1, benchmarkSnapshotMapInt(t, snapshot.InvalidReasonBreakdown, BenchmarkResultStatusParseError))
}

func TestBenchmarkSnapshotStoresIndependentMetrics(t *testing.T) {
	t.Parallel()

	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error) {
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(401, 501, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(50),
				latencyMS:       intPtr(100),
				totalTokens:     10,
				estimatedCost:   0.02,
			}),
			benchmarkRunScoreInput(401, 502, "coding", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{
				latencyMS:     intPtr(700),
				totalTokens:   90,
				estimatedCost: 0.30,
			}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).BuildScoreSnapshots(context.Background(), 123)
	require.NoError(t, err)

	require.Len(t, savedSnapshots, 1)
	snapshot := savedSnapshots[0]
	require.InDelta(t, 50.0, snapshot.OverallScore, 0.000001)
	require.InDelta(t, 0.5, snapshot.SuccessRate, 0.000001)
	require.NotNil(t, snapshot.LatencyP50MS)
	require.NotNil(t, snapshot.LatencyP95MS)
	require.NotNil(t, snapshot.AvgTotalTokens)
	require.InDelta(t, 700.0, *snapshot.LatencyP50MS, 0.000001)
	require.InDelta(t, 700.0, *snapshot.LatencyP95MS, 0.000001)
	require.InDelta(t, 50.0, *snapshot.AvgTotalTokens, 0.000001)
	require.InDelta(t, 0.32, snapshot.EstimatedCost, 0.000001)
}

func TestBenchmarkSnapshotFallsBackToPromptAndCompletionTokensForAverage(t *testing.T) {
	t.Parallel()

	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error) {
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(451, 551, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore:  float64Ptr(50),
				promptTokens:     30,
				completionTokens: 20,
				totalTokens:      0,
				estimatedCost:    0.02,
			}),
			benchmarkRunScoreInput(451, 552, "coding", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{
				promptTokens:     10,
				completionTokens: 5,
				totalTokens:      0,
				estimatedCost:    0.03,
			}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).BuildScoreSnapshots(context.Background(), 124)
	require.NoError(t, err)

	require.Len(t, savedSnapshots, 1)
	snapshot := savedSnapshots[0]
	require.NotNil(t, snapshot.AvgTotalTokens)
	require.InDelta(t, 32.5, *snapshot.AvgTotalTokens, 0.000001)
}

func TestBenchmarkSnapshotUsesSettingServiceConfidenceThresholds(t *testing.T) {
	t.Parallel()

	var savedSnapshots []BenchmarkScoreSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.listRunScoreInputsFn = func(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error) {
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(601, 701, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(80),
			}),
			benchmarkRunScoreInput(601, 702, "coding", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				normalizedScore: float64Ptr(70),
			}),
			benchmarkRunScoreInput(601, 703, "coding", 1, BenchmarkResultStatusTimeout, benchmarkScoreInputOptions{}),
		}, nil
	}
	repo.saveScoreSnapshotsFn = func(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
		savedSnapshots = append([]BenchmarkScoreSnapshotInput(nil), snapshots...)
		return nil
	}

	svc := NewBenchmarkSnapshotService(repo)
	svc.SetBenchmarkRuntimeProvider(&benchmarkRuntimeProviderStub{
		runtime: BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
			ConfidenceThresholds: BenchmarkConfidenceThresholds{
				MediumCoverage: 0.6,
				HighCoverage:   0.95,
			},
		},
	})

	err := svc.BuildScoreSnapshots(context.Background(), 125)
	require.NoError(t, err)

	require.Len(t, savedSnapshots, 1)
	require.Equal(t, BenchmarkConfidenceMedium, savedSnapshots[0].ConfidenceLevel)
	require.False(t, savedSnapshots[0].InsufficientSample)
	require.InDelta(t, 2.0/3.0, savedSnapshots[0].CoverageRate, 0.000001)
}

func TestBenchmarkSnapshotPublicSnapshotRedactsSensitiveFields(t *testing.T) {
	t.Parallel()

	const runID int64 = 456

	var published BenchmarkPublicSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.getRunFn = func(ctx context.Context, gotRunID int64) (*ent.BenchmarkRun, error) {
		require.Equal(t, runID, gotRunID)
		startedAt := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
		finishedAt := startedAt.Add(2 * time.Minute)
		return &ent.BenchmarkRun{
			ID:                 runID,
			SuiteID:            10,
			ProfileID:          20,
			Status:             BenchmarkRunStatusCompleted,
			TaskScale:          "medium",
			TaskTypes:          []string{"reasoning", "coding"},
			PlannedTargetCount: 1,
			PlannedTaskCount:   2,
			PlannedResultCount: 2,
			StartedAt:          &startedAt,
			FinishedAt:         &finishedAt,
			ConfigSnapshot: map[string]any{
				"internal_note": "do-not-publish",
			},
		}, nil
	}
	repo.listScoreSnapshotsFn = func(ctx context.Context, gotRunID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
		require.Equal(t, runID, gotRunID)
		return []*ent.BenchmarkScoreSnapshot{
			{
				RunID:              runID,
				RunTargetID:        601,
				OverallScore:       88,
				DimensionScores:    map[string]any{"reasoning": 90.0},
				PlannedTasks:       2,
				ScoredTasks:        1,
				InvalidTasks:       1,
				CoverageRate:       0.5,
				ConfidenceLevel:    BenchmarkConfidenceLow,
				InsufficientSample: true,
				SuccessRate:        0.5,
				LatencyP50Ms:       float64Ptr(700),
				LatencyP95Ms:       float64Ptr(700),
				AvgTotalTokens:     float64Ptr(50),
				EstimatedCost:      0.32,
				RankingMetadata:    map[string]any{"rank": 1, "internal_note": "secret-rank"},
				InvalidReasonBreakdown: map[string]any{
					BenchmarkResultStatusTimeout: 1,
				},
			},
		}, nil
	}
	repo.listRunScoreInputsFn = func(ctx context.Context, gotRunID int64) ([]BenchmarkRunScoreInput, error) {
		require.Equal(t, runID, gotRunID)
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(601, 701, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				targetID:        901,
				modelName:       "gpt-4.1",
				displayName:     "GPT 4.1 Public",
				channelName:     "openai-prod",
				provider:        "openai",
				targetConfig:    map[string]any{"secret_target_setting": true},
				promptSnapshot:  "top-secret prompt",
				verifierConfig:  map[string]any{"threshold": 0.9},
				taskSnapshot:    map[string]any{"private": "task"},
				rawResponse:     map[string]any{"secret": "response"},
				errorMessage:    benchmarkSnapshotStringPtr("secret-error"),
				normalizedScore: float64Ptr(88),
				latencyMS:       intPtr(120),
				totalTokens:     12,
				estimatedCost:   0.02,
			}),
		}, nil
	}
	repo.publishPublicSnapshotFn = func(ctx context.Context, input BenchmarkPublicSnapshotInput) error {
		published = input
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).PublishPublicSnapshot(context.Background(), runID)
	require.NoError(t, err)

	require.Equal(t, runID, published.RunID)
	require.Equal(t, int64(10), published.SuiteID)
	require.Equal(t, int64(20), published.ProfileID)
	require.NotNil(t, published.PublishedAt)

	payloadBytes, err := json.Marshal(published.Snapshot)
	require.NoError(t, err)
	payload := string(payloadBytes)
	require.NotContains(t, payload, "prompt_snapshot")
	require.NotContains(t, payload, "verifier_config_snapshot")
	require.NotContains(t, payload, "raw_response")
	require.NotContains(t, payload, "error_message")
	require.NotContains(t, payload, "internal_note")
	require.NotContains(t, payload, "secret_target_setting")

	var radar BenchmarkPublicRadar
	require.NoError(t, json.Unmarshal(payloadBytes, &radar))
	require.Equal(t, "ability_score_only", radar.RankingBasis)
	require.NotNil(t, radar.LatestRun)
	require.Equal(t, runID, radar.LatestRun.ID)
	require.Len(t, radar.Targets, 1)
	require.Equal(t, "gpt-4.1", radar.Targets[0].Model)
	require.Equal(t, int64(0), radar.Targets[0].ChannelID)
	require.Equal(t, "openai-prod", radar.Targets[0].ChannelName)
	require.Equal(t, "GPT 4.1 Public", radar.Targets[0].DisplayName)
	require.Equal(t, 1, radar.Targets[0].Rank)
	require.InDelta(t, 88.0, radar.Targets[0].OverallScore, 0.000001)
	require.InDelta(t, 90.0, radar.Targets[0].Dimensions["reasoning"], 0.000001)
	require.Equal(t, 2, radar.Targets[0].ScoreBasis.PlannedTasks)
	require.Equal(t, 1, radar.Targets[0].ScoreBasis.ScoredTasks)
	require.Equal(t, 1, radar.Targets[0].ScoreBasis.InvalidTasks)
	require.InDelta(t, 0.5, radar.Targets[0].ScoreBasis.CoverageRate, 0.000001)
	require.Equal(t, BenchmarkConfidenceLow, radar.Targets[0].ScoreBasis.ConfidenceLevel)
	require.True(t, radar.Targets[0].ScoreBasis.InsufficientSample)
	require.InDelta(t, 0.5, radar.Targets[0].Metrics.SuccessRate, 0.000001)
	require.NotNil(t, radar.Targets[0].Metrics.LatencyP50MS)
	require.InDelta(t, 700.0, *radar.Targets[0].Metrics.LatencyP50MS, 0.000001)
}

func TestBenchmarkSnapshotPublicSnapshotTargetsFollowRankOrder(t *testing.T) {
	t.Parallel()

	const runID int64 = 457

	var published BenchmarkPublicSnapshotInput

	repo := newBenchmarkServiceRepoStub(t)
	repo.getRunFn = func(ctx context.Context, gotRunID int64) (*ent.BenchmarkRun, error) {
		require.Equal(t, runID, gotRunID)
		completedAt := time.Date(2026, 6, 23, 10, 3, 0, 0, time.UTC)
		return &ent.BenchmarkRun{
			ID:         runID,
			SuiteID:    10,
			ProfileID:  20,
			Status:     BenchmarkRunStatusCompleted,
			FinishedAt: &completedAt,
			ConfigSnapshot: map[string]any{
				"ranking_basis": "ability_score_only",
			},
		}, nil
	}
	repo.listScoreSnapshotsFn = func(ctx context.Context, gotRunID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
		require.Equal(t, runID, gotRunID)
		return []*ent.BenchmarkScoreSnapshot{
			{
				RunID:           runID,
				RunTargetID:     702,
				OverallScore:    80,
				DimensionScores: map[string]any{"reasoning": 80.0},
				RankingMetadata: map[string]any{"rank": 2},
			},
			{
				RunID:           runID,
				RunTargetID:     701,
				OverallScore:    90,
				DimensionScores: map[string]any{"reasoning": 90.0},
				RankingMetadata: map[string]any{"rank": 1},
			},
		}, nil
	}
	repo.listRunScoreInputsFn = func(ctx context.Context, gotRunID int64) ([]BenchmarkRunScoreInput, error) {
		require.Equal(t, runID, gotRunID)
		return []BenchmarkRunScoreInput{
			benchmarkRunScoreInput(701, 801, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				modelName:       "rank-one-model",
				displayName:     "Rank One",
				channelName:     "alpha",
				channelID:       11,
				normalizedScore: float64Ptr(90),
			}),
			benchmarkRunScoreInput(702, 802, "reasoning", 1, BenchmarkResultStatusScored, benchmarkScoreInputOptions{
				modelName:       "rank-two-model",
				displayName:     "Rank Two",
				channelName:     "beta",
				channelID:       22,
				normalizedScore: float64Ptr(80),
			}),
		}, nil
	}
	repo.publishPublicSnapshotFn = func(ctx context.Context, input BenchmarkPublicSnapshotInput) error {
		published = input
		return nil
	}

	err := NewBenchmarkSnapshotService(repo).PublishPublicSnapshot(context.Background(), runID)
	require.NoError(t, err)

	payloadBytes, err := json.Marshal(published.Snapshot)
	require.NoError(t, err)

	var radar BenchmarkPublicRadar
	require.NoError(t, json.Unmarshal(payloadBytes, &radar))
	require.Len(t, radar.Targets, 2)
	require.Equal(t, 1, radar.Targets[0].Rank)
	require.Equal(t, "Rank One", radar.Targets[0].DisplayName)
	require.Equal(t, int64(11), radar.Targets[0].ChannelID)
	require.Equal(t, 2, radar.Targets[1].Rank)
	require.Equal(t, "Rank Two", radar.Targets[1].DisplayName)
	require.Equal(t, int64(22), radar.Targets[1].ChannelID)
}

func TestBenchmarkSnapshotGetPublicRadarDecodesLatestSnapshot(t *testing.T) {
	t.Parallel()

	radar := BenchmarkPublicRadar{
		RankingBasis: "ability_score_only",
		LatestRun: &BenchmarkPublicRun{
			ID:          999,
			SuiteID:     10,
			ProfileID:   20,
			Status:      BenchmarkRunStatusCompleted,
			CompletedAt: benchmarkSnapshotTimePtr(time.Date(2026, 6, 23, 10, 2, 0, 0, time.UTC)),
		},
		Targets: []BenchmarkPublicTarget{
			{
				Model:        "gpt-4.1",
				ChannelID:    7,
				ChannelName:  "openai-prod",
				DisplayName:  "GPT 4.1",
				Rank:         1,
				OverallScore: 91.5,
				Dimensions: map[string]float64{
					"reasoning": 91.5,
				},
				ScoreBasis: BenchmarkPublicScoreBasis{
					PlannedTasks:       2,
					ScoredTasks:        2,
					InvalidTasks:       0,
					CoverageRate:       1,
					ConfidenceLevel:    BenchmarkConfidenceHigh,
					InsufficientSample: false,
				},
				Metrics: BenchmarkPublicMetrics{
					SuccessRate:   1,
					EstimatedCost: 0.03,
				},
			},
		},
	}

	repo := newBenchmarkServiceRepoStub(t)
	publishedAt := time.Date(2026, 6, 24, 9, 15, 0, 0, time.UTC)
	repo.getLatestPublicSnapshotFn = func(ctx context.Context) (*ent.BenchmarkPublicSnapshot, error) {
		return &ent.BenchmarkPublicSnapshot{
			RunID:       999,
			SuiteID:     10,
			ProfileID:   20,
			Snapshot:    benchmarkSnapshotStructMap(t, radar),
			PublishedAt: publishedAt,
		}, nil
	}

	got, err := NewBenchmarkSnapshotService(repo).GetPublicRadar(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, radar.RankingBasis, got.RankingBasis)
	require.NotNil(t, got.LatestRun)
	require.Equal(t, radar.LatestRun.ID, got.LatestRun.ID)
	require.NotNil(t, got.PublishedAt)
	require.Equal(t, publishedAt, *got.PublishedAt)
	require.Equal(t, radar.Targets[0].DisplayName, got.Targets[0].DisplayName)
	require.InDelta(t, radar.Targets[0].OverallScore, got.Targets[0].OverallScore, 0.000001)
	require.InDelta(t, radar.Targets[0].Metrics.EstimatedCost, got.Targets[0].Metrics.EstimatedCost, 0.000001)
}

type benchmarkScoreInputOptions struct {
	targetID         int64
	channelID        int64
	modelName        string
	displayName      string
	channelName      string
	provider         string
	targetConfig     map[string]any
	promptSnapshot   string
	verifierConfig   map[string]any
	taskSnapshot     map[string]any
	rawResponse      map[string]any
	errorMessage     *string
	normalizedScore  *float64
	latencyMS        *int
	promptTokens     int
	completionTokens int
	totalTokens      int
	estimatedCost    float64
}

func benchmarkRunScoreInput(runTargetID, runTaskID int64, taskType string, weight float64, status string, opts benchmarkScoreInputOptions) BenchmarkRunScoreInput {
	modelName := opts.modelName
	if modelName == "" {
		modelName = "model"
	}

	targetID := opts.targetID
	if targetID == 0 {
		targetID = runTargetID + 1000
	}

	runTarget := &ent.BenchmarkRunTarget{
		ID:             runTargetID,
		TargetID:       targetID,
		ChannelID:      opts.channelID,
		ModelName:      modelName,
		TargetOrder:    int(runTargetID),
		ConfigSnapshot: benchmarkCloneAnyMap(opts.targetConfig),
	}
	if opts.displayName != "" {
		runTarget.DisplayNameSnapshot = benchmarkSnapshotStringPtr(opts.displayName)
	}
	if opts.channelName != "" {
		runTarget.ChannelNameSnapshot = benchmarkSnapshotStringPtr(opts.channelName)
	}
	if opts.provider != "" {
		runTarget.ProviderSnapshot = benchmarkSnapshotStringPtr(opts.provider)
	}

	runTask := &ent.BenchmarkRunTask{
		ID:                     runTaskID,
		TaskID:                 runTaskID + 2000,
		TaskOrder:              int(runTaskID),
		Type:                   taskType,
		WeightSnapshot:         weight,
		PromptSnapshot:         opts.promptSnapshot,
		VerifierTypeSnapshot:   "json",
		VerifierConfigSnapshot: benchmarkCloneAnyMap(opts.verifierConfig),
		TaskSnapshot:           benchmarkCloneAnyMap(opts.taskSnapshot),
	}

	return BenchmarkRunScoreInput{
		RunTarget: runTarget,
		RunTask:   runTask,
		Result: &ent.BenchmarkResult{
			RunTargetID:      runTargetID,
			RunTaskID:        runTaskID,
			Status:           status,
			NormalizedScore:  opts.normalizedScore,
			LatencyMs:        opts.latencyMS,
			PromptTokens:     opts.promptTokens,
			CompletionTokens: opts.completionTokens,
			TotalTokens:      opts.totalTokens,
			EstimatedCost:    opts.estimatedCost,
			RawResponse:      benchmarkCloneAnyMap(opts.rawResponse),
			ErrorMessage:     opts.errorMessage,
		},
	}
}

func benchmarkSnapshotByRunTargetID(t *testing.T, snapshots []BenchmarkScoreSnapshotInput, runTargetID int64) BenchmarkScoreSnapshotInput {
	t.Helper()

	for _, snapshot := range snapshots {
		if snapshot.RunTargetID == runTargetID {
			return snapshot
		}
	}
	t.Fatalf("snapshot for run target %d not found", runTargetID)
	return BenchmarkScoreSnapshotInput{}
}

func benchmarkSnapshotRank(t *testing.T, metadata map[string]any) int {
	t.Helper()

	return benchmarkSnapshotMapInt(t, metadata, "rank")
}

func benchmarkSnapshotMapInt(t *testing.T, values map[string]any, key string) int {
	t.Helper()

	value, ok := values[key]
	require.True(t, ok, "missing key %q", key)
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		t.Fatalf("unexpected numeric type %T for key %q", value, key)
		return 0
	}
}

func benchmarkRequireMapFloat(t *testing.T, values map[string]any, key string, want float64) {
	t.Helper()

	value, ok := values[key]
	require.True(t, ok, "missing key %q", key)
	switch typed := value.(type) {
	case float64:
		require.InDelta(t, want, typed, 0.000001)
	case int:
		require.InDelta(t, want, float64(typed), 0.000001)
	default:
		t.Fatalf("unexpected numeric type %T for key %q", value, key)
	}
}

func benchmarkSnapshotStructMap(t *testing.T, value any) map[string]any {
	t.Helper()

	payload, err := json.Marshal(value)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(payload, &out))
	return out
}

func benchmarkSnapshotStringPtr(value string) *string {
	return &value
}

func benchmarkSnapshotTimePtr(value time.Time) *time.Time {
	return &value
}
