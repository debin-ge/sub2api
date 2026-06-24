package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkresult"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntarget"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntask"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type benchmarkSmokeSettingRepoStub struct {
	values map[string]string
}

func (s *benchmarkSmokeSettingRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	return &service.Setting{Key: key, Value: s.values[key]}, nil
}

func (s *benchmarkSmokeSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s *benchmarkSmokeSettingRepoStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *benchmarkSmokeSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}

func (s *benchmarkSmokeSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func (s *benchmarkSmokeSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *benchmarkSmokeSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func newBenchmarkSmokeClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestBenchmarkSmokeFixture(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newBenchmarkSmokeClient(t)
	repo := repository.NewBenchmarkRepository(client)

	settings := service.NewSettingService(&benchmarkSmokeSettingRepoStub{
		values: map[string]string{
			service.SettingKeyBenchmarkEnabled:               "true",
			service.SettingKeyBenchmarkPublicEnabled:         "true",
			service.SettingKeyBenchmarkHomeEnabled:           "true",
			service.SettingKeyBenchmarkDefaultSuiteID:        "0",
			service.SettingKeyBenchmarkGlobalConcurrency:     "4",
			service.SettingKeyBenchmarkDefaultTimeoutSeconds: "120",
			service.SettingKeyBenchmarkLowConfidenceThreshold:"0.70",
			service.SettingKeyBenchmarkHighConfidenceThreshold:"0.90",
			service.SettingKeyBenchmarkScheduleEnabled:       "true",
		},
	}, &config.Config{})

	benchmarkSvc := service.NewBenchmarkService(repo)
	benchmarkSvc.SetSettingService(settings)

	snapshotSvc := service.NewBenchmarkSnapshotService(repo)
	snapshotSvc.SetSettingService(settings)

	suite, err := benchmarkSvc.CreateSuite(ctx, service.BenchmarkSuiteInput{
		Name:          "Smoke Radar Suite",
		Slug:          fmt.Sprintf("smoke-radar-%d", time.Now().UnixNano()),
		Description:   "Smoke fixture suite",
		Enabled:       true,
		PublicVisible: true,
	})
	require.NoError(t, err)

	targetOne, err := benchmarkSvc.CreateTarget(ctx, service.BenchmarkTargetInput{
		ModelName:           "gpt-4.1",
		ChannelID:           101,
		DisplayName:         "GPT-4.1 · Relay A",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "Relay A",
		SupportedTaskTypes:  []string{"reasoning", "coding"},
		MaxConcurrency:      2,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           1,
	})
	require.NoError(t, err)

	targetTwo, err := benchmarkSvc.CreateTarget(ctx, service.BenchmarkTargetInput{
		ModelName:           "claude-sonnet-4",
		ChannelID:           202,
		DisplayName:         "Claude Sonnet 4 · Relay B",
		ProviderSnapshot:    "anthropic",
		ChannelNameSnapshot: "Relay B",
		SupportedTaskTypes:  []string{"reasoning", "coding"},
		MaxConcurrency:      2,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           2,
	})
	require.NoError(t, err)

	reasoningTask, err := benchmarkSvc.CreateTask(ctx, service.BenchmarkTaskInput{
		SuiteID:      suite.ID,
		Title:        "Smoke reasoning task",
		Type:         "reasoning",
		Prompt:       "Answer the reasoning question.",
		VerifierType: "exact_match",
		Weight:       1,
		MinScale:     service.BenchmarkTaskScaleSmall,
		PublicPrompt: true,
		Enabled:      true,
	})
	require.NoError(t, err)

	codingTask, err := benchmarkSvc.CreateTask(ctx, service.BenchmarkTaskInput{
		SuiteID:      suite.ID,
		Title:        "Smoke coding task",
		Type:         "coding",
		Prompt:       "Return valid JSON for the coding task.",
		VerifierType: "exact_match",
		Weight:       1,
		MinScale:     service.BenchmarkTaskScaleSmall,
		PublicPrompt: false,
		Enabled:      true,
	})
	require.NoError(t, err)

	_, err = benchmarkSvc.CreateTask(ctx, service.BenchmarkTaskInput{
		SuiteID:      suite.ID,
		Title:        "Smoke writing task",
		Type:         "writing",
		Prompt:       "Write a short summary.",
		VerifierType: "exact_match",
		Weight:       1,
		MinScale:     service.BenchmarkTaskScaleMedium,
		PublicPrompt: true,
		Enabled:      true,
	})
	require.NoError(t, err)

	profile, err := benchmarkSvc.CreateProfile(ctx, service.BenchmarkProfileInput{
		SuiteID:          suite.ID,
		Name:             "Smoke medium profile",
		Description:      "Fixture profile for smoke validation",
		TaskScale:        service.BenchmarkTaskScaleMedium,
		SamplingStrategy: "random",
		TargetIDs:        []int64{targetOne.ID, targetTwo.ID},
		TaskTypes:        []string{"reasoning", "coding"},
		Enabled:          true,
	})
	require.NoError(t, err)

	preview, err := benchmarkSvc.PreviewProfile(ctx, profile.ID, service.BenchmarkProfilePreviewInput{})
	require.NoError(t, err)
	require.Equal(t, 2, preview.TargetCount)
	require.Equal(t, 2, preview.TaskCount)
	require.Equal(t, 4, preview.ResultCount)

	run, err := benchmarkSvc.CreateRun(ctx, service.BenchmarkCreateRunRequest{
		ProfileID:   profile.ID,
		TriggerType: "manual",
	})
	require.NoError(t, err)
	require.Equal(t, 2, run.PlannedTargetCount)
	require.Equal(t, 2, run.PlannedTaskCount)
	require.Equal(t, 4, run.PlannedResultCount)

	runTargets, err := client.BenchmarkRunTarget.Query().
		Where(benchmarkruntarget.RunIDEQ(run.ID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, runTargets, 2)

	runTasks, err := client.BenchmarkRunTask.Query().
		Where(benchmarkruntask.RunIDEQ(run.ID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, runTasks, 2)

	runTargetByTargetID := make(map[int64]*dbent.BenchmarkRunTarget, len(runTargets))
	for _, runTarget := range runTargets {
		runTargetByTargetID[runTarget.TargetID] = runTarget
	}
	runTaskByTaskID := make(map[int64]*dbent.BenchmarkRunTask, len(runTasks))
	for _, runTask := range runTasks {
		runTaskByTaskID[runTask.TaskID] = runTask
	}

	scoreTargetOne := 95.0
	scoreTargetTwoReasoning := 88.0
	scoreTargetTwoCoding := 82.0
	parseError := "invalid json payload"

	updateResult := func(targetID, taskID int64, status string, normalizedScore *float64, latencyMs int, totalTokens int, estimatedCost float64, errorMessage *string) {
		t.Helper()

		result, err := client.BenchmarkResult.Query().
			Where(
				benchmarkresult.RunIDEQ(run.ID),
				benchmarkresult.RunTargetIDEQ(runTargetByTargetID[targetID].ID),
				benchmarkresult.RunTaskIDEQ(runTaskByTaskID[taskID].ID),
			).
			Only(ctx)
		require.NoError(t, err)

		update := client.BenchmarkResult.UpdateOneID(result.ID).
			SetStatus(status).
			SetLatencyMs(latencyMs).
			SetPromptTokens(totalTokens / 2).
			SetCompletionTokens(totalTokens - (totalTokens / 2)).
			SetTotalTokens(totalTokens).
			SetEstimatedCost(estimatedCost)

		if normalizedScore != nil {
			update.SetNormalizedScore(*normalizedScore)
		} else {
			update.ClearNormalizedScore()
		}
		if errorMessage != nil {
			update.SetErrorMessage(*errorMessage)
		} else {
			update.ClearErrorMessage()
		}

		_, err = update.Save(ctx)
		require.NoError(t, err)
	}

	updateResult(targetOne.ID, reasoningTask.ID, service.BenchmarkResultStatusScored, &scoreTargetOne, 820, 140, 0.021, nil)
	updateResult(targetOne.ID, codingTask.ID, service.BenchmarkResultStatusParseError, nil, 1800, 120, 0.031, &parseError)
	updateResult(targetTwo.ID, reasoningTask.ID, service.BenchmarkResultStatusScored, &scoreTargetTwoReasoning, 900, 150, 0.022, nil)
	updateResult(targetTwo.ID, codingTask.ID, service.BenchmarkResultStatusScored, &scoreTargetTwoCoding, 980, 160, 0.024, nil)

	finishedAt := time.Now().UTC()
	_, err = client.BenchmarkRun.UpdateOneID(run.ID).
		SetStatus(service.BenchmarkRunStatusCompleted).
		SetFinishedAt(finishedAt).
		Save(ctx)
	require.NoError(t, err)

	err = snapshotSvc.BuildScoreSnapshots(ctx, run.ID)
	require.NoError(t, err)

	snapshots, err := benchmarkSvc.ListScoreSnapshots(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, runTargetByTargetID[targetOne.ID].ID, snapshots[0].RunTargetID)
	require.InDelta(t, 95.0, snapshots[0].OverallScore, 0.000001)
	require.InDelta(t, 0.5, snapshots[0].CoverageRate, 0.000001)
	require.True(t, snapshots[0].InsufficientSample)
	require.Equal(t, 1, snapshots[0].InvalidTasks)
	require.Equal(t, runTargetByTargetID[targetTwo.ID].ID, snapshots[1].RunTargetID)
	require.InDelta(t, 85.0, snapshots[1].OverallScore, 0.000001)
	require.InDelta(t, 1.0, snapshots[1].CoverageRate, 0.000001)
	require.False(t, snapshots[1].InsufficientSample)

	err = snapshotSvc.PublishPublicSnapshot(ctx, run.ID)
	require.NoError(t, err)

	radar, err := snapshotSvc.GetPublicRadar(ctx)
	require.NoError(t, err)
	require.NotNil(t, radar)
	require.Equal(t, "ability_score_only", radar.RankingBasis)
	require.NotNil(t, radar.PublishedAt)
	require.NotNil(t, radar.LatestRun)
	require.Equal(t, run.ID, radar.LatestRun.ID)
	require.Len(t, radar.Targets, 2)
	require.Equal(t, "GPT-4.1 · Relay A", radar.Targets[0].DisplayName)
	require.True(t, radar.Targets[0].ScoreBasis.InsufficientSample)
	require.InDelta(t, 95.0, radar.Targets[0].OverallScore, 0.000001)
	require.InDelta(t, 0.5, radar.Targets[0].Metrics.SuccessRate, 0.000001)
	require.InDelta(t, 85.0, radar.Targets[1].OverallScore, 0.000001)
	require.False(t, radar.Targets[1].ScoreBasis.InsufficientSample)
}
