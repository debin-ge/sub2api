//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkprofile"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkpublicsnapshot"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkresult"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkrun"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntarget"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntask"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkscoresnapshot"
	"github.com/Wei-Shaw/sub2api/ent/benchmarksuite"
	"github.com/Wei-Shaw/sub2api/ent/benchmarktarget"
	"github.com/Wei-Shaw/sub2api/ent/benchmarktask"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type benchmarkFixture struct {
	ctx            context.Context
	client         *dbent.Client
	repo           service.BenchmarkRepository
	suite          *dbent.BenchmarkSuite
	target         *dbent.BenchmarkTarget
	task           *dbent.BenchmarkTask
	profile        *dbent.BenchmarkProfile
	createRunInput service.BenchmarkCreateRunInput
	runIDs         []int64
}

func newBenchmarkFixture(t *testing.T, prefix string) *benchmarkFixture {
	t.Helper()
	return newBenchmarkFixtureWith(t, context.Background(), testEntClient(t), prefix)
}

func newBenchmarkFixtureWith(t *testing.T, ctx context.Context, client *dbent.Client, prefix string) *benchmarkFixture {
	t.Helper()

	repo := NewBenchmarkRepository(client)

	suite, err := repo.CreateSuite(ctx, service.BenchmarkSuiteInput{
		Name:          uniqueTestValue(t, prefix+"-suite"),
		Slug:          uniqueTestValue(t, prefix+"-suite"),
		Description:   "integration suite",
		Enabled:       true,
		PublicVisible: true,
		Metadata:      map[string]any{"scope": prefix},
	})
	require.NoError(t, err)

	target, err := repo.CreateTarget(ctx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, prefix+"-model"),
		ChannelID:           101,
		DisplayName:         "Radar Model",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "primary-openai",
		SupportedTaskTypes:  []string{"reasoning", "coding"},
		MaxConcurrency:      2,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           1,
		Metadata:            map[string]any{"tier": "test"},
	})
	require.NoError(t, err)

	task, err := repo.CreateTask(ctx, service.BenchmarkTaskInput{
		SuiteID:        suite.ID,
		Title:          "Benchmark snapshot task",
		Type:           "reasoning",
		Category:       "logic",
		Difficulty:     "easy",
		Tags:           []string{"snapshot"},
		Prompt:         "Original reasoning prompt",
		InputPayload:   map[string]any{"question": "2+2"},
		ExpectedOutput: map[string]any{"answer": "4"},
		VerifierType:   "exact_match",
		VerifierConfig: map[string]any{"field": "answer"},
		Weight:         1.5,
		MinScale:       service.BenchmarkTaskScaleSmall,
		PublicPrompt:   true,
		Enabled:        true,
		Metadata:       map[string]any{"case": prefix},
	})
	require.NoError(t, err)

	profile, err := repo.CreateProfile(ctx, service.BenchmarkProfileInput{
		SuiteID:          suite.ID,
		Name:             prefix + " profile",
		Description:      "profile for benchmark repository integration test",
		TaskScale:        service.BenchmarkTaskScaleSmall,
		SamplingStrategy: "seeded",
		TargetIDs:        []int64{target.ID},
		TaskTypes:        []string{"reasoning"},
		PerTypeLimit:     map[string]int{"reasoning": 1},
		SelectionSeed:    ptrInt64(42),
		RuntimeConfig:    map[string]any{"temperature": 0},
		ScoringConfig:    map[string]any{"normalize": true},
		Metadata:         map[string]any{"profile": prefix},
		Enabled:          true,
	})
	require.NoError(t, err)

	createRunInput := service.BenchmarkCreateRunInput{
		SuiteID:       suite.ID,
		ProfileID:     profile.ID,
		Status:        service.BenchmarkRunStatusQueued,
		TriggerType:   "manual",
		TaskScale:     service.BenchmarkTaskScaleSmall,
		TaskTypes:     []string{"reasoning"},
		SelectionSeed: ptrInt64(42),
		ConfigSnapshot: map[string]any{
			"profile_id": profile.ID,
		},
		Targets: []service.BenchmarkRunTargetInput{
			{
				TargetID:            target.ID,
				ModelName:           target.ModelName,
				ChannelID:           target.ChannelID,
				DisplayNameSnapshot: "Radar Model Snapshot",
				ChannelNameSnapshot: "primary-openai",
				ProviderSnapshot:    "openai",
				TargetOrder:         1,
				ConfigSnapshot:      map[string]any{"max_concurrency": 2},
			},
		},
		Tasks: []service.BenchmarkRunTaskInput{
			{
				TaskID:                 task.ID,
				TaskOrder:              1,
				Type:                   task.Type,
				Category:               stringValue(task.Category),
				Difficulty:             stringValue(task.Difficulty),
				WeightSnapshot:         task.Weight,
				PromptSnapshot:         task.Prompt,
				VerifierTypeSnapshot:   task.VerifierType,
				VerifierConfigSnapshot: task.VerifierConfig,
				TaskSnapshot: map[string]any{
					"title":  task.Title,
					"prompt": task.Prompt,
				},
			},
		},
	}

	run, err := repo.CreateRunWithSnapshots(ctx, createRunInput)
	require.NoError(t, err)

	fixture := &benchmarkFixture{
		ctx:            ctx,
		client:         client,
		repo:           repo,
		suite:          suite,
		target:         target,
		task:           task,
		profile:        profile,
		createRunInput: createRunInput,
		runIDs:         []int64{run.ID},
	}

	t.Cleanup(func() {
		for i := len(fixture.runIDs) - 1; i >= 0; i-- {
			runID := fixture.runIDs[i]
			_, _ = client.BenchmarkPublicSnapshot.Delete().Where(benchmarkpublicsnapshot.RunIDEQ(runID)).Exec(ctx)
			_, _ = client.BenchmarkScoreSnapshot.Delete().Where(benchmarkscoresnapshot.RunIDEQ(runID)).Exec(ctx)
			_, _ = client.BenchmarkResult.Delete().Where(benchmarkresult.RunIDEQ(runID)).Exec(ctx)
			_, _ = client.BenchmarkRunTask.Delete().Where(benchmarkruntask.RunIDEQ(runID)).Exec(ctx)
			_, _ = client.BenchmarkRunTarget.Delete().Where(benchmarkruntarget.RunIDEQ(runID)).Exec(ctx)
			_, _ = client.BenchmarkRun.Delete().Where(benchmarkrun.IDEQ(runID)).Exec(ctx)
		}
		_, _ = client.BenchmarkProfile.Delete().Where(benchmarkprofile.IDEQ(profile.ID)).Exec(ctx)
		_, _ = client.BenchmarkTask.Delete().Where(benchmarktask.IDEQ(task.ID)).Exec(ctx)
		_, _ = client.BenchmarkTarget.Delete().Where(benchmarktarget.IDEQ(target.ID)).Exec(ctx)
		_, _ = client.BenchmarkSuite.Delete().Where(benchmarksuite.IDEQ(suite.ID)).Exec(ctx)
	})

	return fixture
}

func (f *benchmarkFixture) createRun(t *testing.T) *dbent.BenchmarkRun {
	t.Helper()

	run, err := f.repo.CreateRunWithSnapshots(f.ctx, f.createRunInput)
	require.NoError(t, err)
	f.runIDs = append(f.runIDs, run.ID)
	return run
}

func (f *benchmarkFixture) runTarget(t *testing.T, runID int64) *dbent.BenchmarkRunTarget {
	t.Helper()

	return f.runTargetByTargetID(t, runID, f.target.ID)
}

func (f *benchmarkFixture) runTargetByTargetID(t *testing.T, runID, targetID int64) *dbent.BenchmarkRunTarget {
	t.Helper()

	runTarget, err := f.client.BenchmarkRunTarget.Query().
		Where(
			benchmarkruntarget.RunIDEQ(runID),
			benchmarkruntarget.TargetIDEQ(targetID),
		).
		Only(f.ctx)
	require.NoError(t, err)
	return runTarget
}

func TestBenchmarkRepositoryCreateRunSnapshot(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewBenchmarkRepository(client)

	suite, err := repo.CreateSuite(txCtx, service.BenchmarkSuiteInput{
		Name:          uniqueTestValue(t, "Radar Suite"),
		Slug:          uniqueTestValue(t, "radar-suite"),
		Description:   "integration suite",
		Enabled:       true,
		PublicVisible: true,
		Metadata:      map[string]any{"scope": "integration"},
	})
	require.NoError(t, err)

	target, err := repo.CreateTarget(txCtx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, "radar-model"),
		ChannelID:           101,
		DisplayName:         "Radar Model",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "primary-openai",
		SupportedTaskTypes:  []string{"reasoning", "coding"},
		MaxConcurrency:      2,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           1,
		Metadata:            map[string]any{"tier": "test"},
	})
	require.NoError(t, err)

	taskOne, err := repo.CreateTask(txCtx, service.BenchmarkTaskInput{
		SuiteID:        suite.ID,
		Title:          "Reasoning snapshot",
		Type:           "reasoning",
		Category:       "logic",
		Difficulty:     "easy",
		Tags:           []string{"snapshot"},
		Prompt:         "Original reasoning prompt",
		InputPayload:   map[string]any{"question": "2+2"},
		ExpectedOutput: map[string]any{"answer": "4"},
		VerifierType:   "exact_match",
		VerifierConfig: map[string]any{"field": "answer"},
		Weight:         1.5,
		MinScale:       service.BenchmarkTaskScaleSmall,
		PublicPrompt:   true,
		Enabled:        true,
		Metadata:       map[string]any{"case": "one"},
	})
	require.NoError(t, err)

	taskTwo, err := repo.CreateTask(txCtx, service.BenchmarkTaskInput{
		SuiteID:        suite.ID,
		Title:          "Coding snapshot",
		Type:           "coding",
		Category:       "go",
		Difficulty:     "medium",
		Tags:           []string{"snapshot", "code"},
		Prompt:         "Original coding prompt",
		InputPayload:   map[string]any{"language": "go"},
		ExpectedOutput: map[string]any{"passes": true},
		VerifierType:   "json_contains",
		VerifierConfig: map[string]any{"field": "passes"},
		Weight:         2,
		MinScale:       service.BenchmarkTaskScaleMedium,
		PublicPrompt:   false,
		Enabled:        true,
		Metadata:       map[string]any{"case": "two"},
	})
	require.NoError(t, err)

	profile, err := repo.CreateProfile(txCtx, service.BenchmarkProfileInput{
		SuiteID:          suite.ID,
		Name:             "Integration profile",
		Description:      "profile for snapshot integration test",
		TaskScale:        service.BenchmarkTaskScaleMedium,
		SamplingStrategy: "seeded",
		TargetIDs:        []int64{target.ID},
		TaskTypes:        []string{"reasoning", "coding"},
		PerTypeLimit:     map[string]int{"reasoning": 1, "coding": 1},
		SelectionSeed:    ptrInt64(42),
		RuntimeConfig:    map[string]any{"temperature": 0},
		ScoringConfig:    map[string]any{"normalize": true},
		Metadata:         map[string]any{"profile": "integration"},
		Enabled:          true,
	})
	require.NoError(t, err)

	run, err := repo.CreateRunWithSnapshots(txCtx, service.BenchmarkCreateRunInput{
		SuiteID:       suite.ID,
		ProfileID:     profile.ID,
		Status:        service.BenchmarkRunStatusQueued,
		TriggerType:   "manual",
		TaskScale:     service.BenchmarkTaskScaleMedium,
		TaskTypes:     []string{"reasoning", "coding"},
		SelectionSeed: ptrInt64(42),
		ConfigSnapshot: map[string]any{
			"profile_id": profile.ID,
		},
		Targets: []service.BenchmarkRunTargetInput{
			{
				TargetID:            target.ID,
				ModelName:           target.ModelName,
				ChannelID:           target.ChannelID,
				DisplayNameSnapshot: "Radar Model Snapshot",
				ChannelNameSnapshot: "primary-openai",
				ProviderSnapshot:    "openai",
				TargetOrder:         1,
				ConfigSnapshot:      map[string]any{"max_concurrency": 2},
			},
		},
		Tasks: []service.BenchmarkRunTaskInput{
			{
				TaskID:                 taskOne.ID,
				TaskOrder:              1,
				Type:                   taskOne.Type,
				Category:               stringValue(taskOne.Category),
				Difficulty:             stringValue(taskOne.Difficulty),
				WeightSnapshot:         taskOne.Weight,
				PromptSnapshot:         taskOne.Prompt,
				VerifierTypeSnapshot:   taskOne.VerifierType,
				VerifierConfigSnapshot: taskOne.VerifierConfig,
				TaskSnapshot: map[string]any{
					"title":  taskOne.Title,
					"prompt": taskOne.Prompt,
				},
			},
			{
				TaskID:                 taskTwo.ID,
				TaskOrder:              2,
				Type:                   taskTwo.Type,
				Category:               stringValue(taskTwo.Category),
				Difficulty:             stringValue(taskTwo.Difficulty),
				WeightSnapshot:         taskTwo.Weight,
				PromptSnapshot:         taskTwo.Prompt,
				VerifierTypeSnapshot:   taskTwo.VerifierType,
				VerifierConfigSnapshot: taskTwo.VerifierConfig,
				TaskSnapshot: map[string]any{
					"title":  taskTwo.Title,
					"prompt": taskTwo.Prompt,
				},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, run.PlannedTargetCount)
	require.Equal(t, 2, run.PlannedTaskCount)
	require.Equal(t, 2, run.PlannedResultCount)

	runTargets, err := client.BenchmarkRunTarget.Query().
		Where(benchmarkruntarget.RunIDEQ(run.ID)).
		All(txCtx)
	require.NoError(t, err)
	require.Len(t, runTargets, 1)
	require.Equal(t, target.ID, runTargets[0].TargetID)
	require.NotNil(t, runTargets[0].DisplayNameSnapshot)
	require.Equal(t, "Radar Model Snapshot", *runTargets[0].DisplayNameSnapshot)

	runTasks, err := client.BenchmarkRunTask.Query().
		Where(benchmarkruntask.RunIDEQ(run.ID)).
		Order(dbent.Asc(benchmarkruntask.FieldTaskOrder)).
		All(txCtx)
	require.NoError(t, err)
	require.Len(t, runTasks, 2)
	require.Equal(t, "Original reasoning prompt", runTasks[0].PromptSnapshot)
	require.Equal(t, "Original coding prompt", runTasks[1].PromptSnapshot)

	results, err := client.BenchmarkResult.Query().
		Where(benchmarkresult.RunIDEQ(run.ID)).
		All(txCtx)
	require.NoError(t, err)
	require.Len(t, results, len(runTargets)*len(runTasks))
	for _, result := range results {
		require.Equal(t, service.BenchmarkResultStatusPending, result.Status)
		require.Equal(t, run.ID, result.RunID)
	}

	_, err = client.BenchmarkTask.UpdateOneID(taskOne.ID).
		SetPrompt("Mutated reasoning prompt").
		SetVerifierConfig(map[string]any{"field": "mutated"}).
		Save(txCtx)
	require.NoError(t, err)

	unchangedSnapshot, err := client.BenchmarkRunTask.Query().
		Where(
			benchmarkruntask.RunIDEQ(run.ID),
			benchmarkruntask.TaskIDEQ(taskOne.ID),
		).
		Only(txCtx)
	require.NoError(t, err)
	require.Equal(t, "Original reasoning prompt", unchangedSnapshot.PromptSnapshot)
	require.Equal(t, "answer", unchangedSnapshot.VerifierConfigSnapshot["field"])
	require.Equal(t, "Original reasoning prompt", unchangedSnapshot.TaskSnapshot["prompt"])

	mutatedTask, err := client.BenchmarkTask.Query().
		Where(benchmarktask.IDEQ(taskOne.ID)).
		Only(txCtx)
	require.NoError(t, err)
	require.Equal(t, "Mutated reasoning prompt", mutatedTask.Prompt)
}

func TestBenchmarkRepositorySaveScoreSnapshotsRollsBackOnError(t *testing.T) {
	fixture := newBenchmarkFixture(t, "score-rollback")

	secondaryTarget, err := fixture.repo.CreateTarget(fixture.ctx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, "score-rollback-secondary-model"),
		ChannelID:           102,
		DisplayName:         "Secondary Model",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "secondary-openai",
		SupportedTaskTypes:  []string{"reasoning"},
		MaxConcurrency:      1,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           2,
		Metadata:            map[string]any{"tier": "secondary"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = fixture.client.BenchmarkTarget.Delete().Where(benchmarktarget.IDEQ(secondaryTarget.ID)).Exec(fixture.ctx)
	})

	batchRunInput := fixture.createRunInput
	batchRunInput.Targets = []service.BenchmarkRunTargetInput{
		{
			TargetID:            fixture.target.ID,
			ModelName:           fixture.target.ModelName,
			ChannelID:           fixture.target.ChannelID,
			DisplayNameSnapshot: "Primary Model Snapshot",
			ChannelNameSnapshot: "primary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         1,
			ConfigSnapshot:      map[string]any{"max_concurrency": 2},
		},
		{
			TargetID:            secondaryTarget.ID,
			ModelName:           secondaryTarget.ModelName,
			ChannelID:           secondaryTarget.ChannelID,
			DisplayNameSnapshot: "Secondary Model Snapshot",
			ChannelNameSnapshot: "secondary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         2,
			ConfigSnapshot:      map[string]any{"max_concurrency": 1},
		},
	}

	batchRun, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, batchRunInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, batchRun.ID)

	primaryRunTarget := fixture.runTargetByTargetID(t, batchRun.ID, fixture.target.ID)
	secondaryRunTarget := fixture.runTargetByTargetID(t, batchRun.ID, secondaryTarget.ID)
	foreignRunTarget := fixture.runTarget(t, fixture.runIDs[0])

	originalSnapshots := []service.BenchmarkScoreSnapshotInput{
		{
			RunTargetID:            primaryRunTarget.ID,
			OverallScore:           91.1,
			DimensionScores:        map[string]any{"reasoning": 90.0},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           1.0,
			ConfidenceLevel:        "medium",
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.11,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "primary"},
		},
		{
			RunTargetID:            secondaryRunTarget.ID,
			OverallScore:           82.2,
			DimensionScores:        map[string]any{"reasoning": 80.0},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           1.0,
			ConfidenceLevel:        "medium",
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.08,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "secondary"},
		},
	}
	require.NoError(t, fixture.repo.SaveScoreSnapshots(fixture.ctx, batchRun.ID, originalSnapshots))

	originalRows, err := fixture.client.BenchmarkScoreSnapshot.Query().
		Where(benchmarkscoresnapshot.RunIDEQ(batchRun.ID)).
		Order(dbent.Asc(benchmarkscoresnapshot.FieldRunTargetID)).
		All(fixture.ctx)
	require.NoError(t, err)
	require.Len(t, originalRows, 2)

	originalSnapshotsByRunTarget := make(map[int64]*dbent.BenchmarkScoreSnapshot, len(originalRows))
	for _, snapshot := range originalRows {
		originalSnapshotsByRunTarget[snapshot.RunTargetID] = snapshot
	}

	partialReplacement := []service.BenchmarkScoreSnapshotInput{
		{
			RunTargetID:            primaryRunTarget.ID,
			OverallScore:           99.9,
			DimensionScores:        map[string]any{"reasoning": 99.0},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           1.0,
			ConfidenceLevel:        "high",
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.19,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "primary-replacement"},
		},
		{
			RunTargetID:            foreignRunTarget.ID,
			OverallScore:           12.34,
			DimensionScores:        map[string]any{"reasoning": 12.34},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           1.0,
			ConfidenceLevel:        "low",
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.01,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "foreign"},
		},
	}

	err = fixture.repo.SaveScoreSnapshots(fixture.ctx, batchRun.ID, partialReplacement)
	require.Error(t, err)

	afterSnapshots, err := fixture.client.BenchmarkScoreSnapshot.Query().
		Where(benchmarkscoresnapshot.RunIDEQ(batchRun.ID)).
		Order(dbent.Asc(benchmarkscoresnapshot.FieldRunTargetID)).
		All(fixture.ctx)
	require.NoError(t, err)
	require.Len(t, afterSnapshots, 2)

	afterByRunTarget := make(map[int64]*dbent.BenchmarkScoreSnapshot, len(afterSnapshots))
	for _, snapshot := range afterSnapshots {
		afterByRunTarget[snapshot.RunTargetID] = snapshot
	}

	for runTargetID, original := range originalSnapshotsByRunTarget {
		after := afterByRunTarget[runTargetID]
		require.NotNil(t, after)
		require.Equal(t, original.ID, after.ID)
		require.InDelta(t, original.OverallScore, after.OverallScore, 0.000001)
	}

	require.NotContains(t, afterByRunTarget, foreignRunTarget.ID)
	require.InDelta(t, 91.1, afterByRunTarget[primaryRunTarget.ID].OverallScore, 0.000001)
	require.InDelta(t, 82.2, afterByRunTarget[secondaryRunTarget.ID].OverallScore, 0.000001)

	_, err = fixture.client.BenchmarkScoreSnapshot.Query().
		Where(
			benchmarkscoresnapshot.RunIDEQ(batchRun.ID),
			benchmarkscoresnapshot.OverallScoreEQ(99.9),
		).
		Only(fixture.ctx)
	require.Error(t, err)
}

func TestBenchmarkRepositoryGetLatestPublicSnapshotOrdersByPublishedAtThenID(t *testing.T) {
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(context.Background(), tx)
	fixture := newBenchmarkFixtureWith(t, txCtx, tx.Client(), "public-snapshot-order")

	_, err := fixture.client.BenchmarkPublicSnapshot.Delete().Exec(fixture.ctx)
	require.NoError(t, err)
	remaining, err := fixture.client.BenchmarkPublicSnapshot.Query().Count(fixture.ctx)
	require.NoError(t, err)
	require.Zero(t, remaining)

	firstPublishedAt := time.Date(2024, 6, 23, 12, 0, 0, 0, time.UTC)
	secondPublishedAt := firstPublishedAt.Add(time.Minute)
	tiePublishedAt := secondPublishedAt.Add(time.Minute)

	require.NoError(t, fixture.repo.PublishPublicSnapshot(fixture.ctx, service.BenchmarkPublicSnapshotInput{
		RunID:       fixture.runIDs[0],
		SuiteID:     fixture.suite.ID,
		ProfileID:   fixture.profile.ID,
		Snapshot:    map[string]any{"phase": "first"},
		PublishedAt: &firstPublishedAt,
	}))
	require.NoError(t, fixture.repo.PublishPublicSnapshot(fixture.ctx, service.BenchmarkPublicSnapshotInput{
		RunID:       fixture.runIDs[0],
		SuiteID:     fixture.suite.ID,
		ProfileID:   fixture.profile.ID,
		Snapshot:    map[string]any{"phase": "second"},
		PublishedAt: &secondPublishedAt,
	}))

	latest, err := fixture.repo.GetLatestPublicSnapshot(fixture.ctx)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, "second", latest.Snapshot["phase"])
	require.True(t, secondPublishedAt.Equal(latest.PublishedAt))

	require.NoError(t, fixture.repo.PublishPublicSnapshot(fixture.ctx, service.BenchmarkPublicSnapshotInput{
		RunID:       fixture.runIDs[0],
		SuiteID:     fixture.suite.ID,
		ProfileID:   fixture.profile.ID,
		Snapshot:    map[string]any{"phase": "tie-a"},
		PublishedAt: &tiePublishedAt,
	}))
	require.NoError(t, fixture.repo.PublishPublicSnapshot(fixture.ctx, service.BenchmarkPublicSnapshotInput{
		RunID:       fixture.runIDs[0],
		SuiteID:     fixture.suite.ID,
		ProfileID:   fixture.profile.ID,
		Snapshot:    map[string]any{"phase": "tie-b"},
		PublishedAt: &tiePublishedAt,
	}))

	latestTie, err := fixture.repo.GetLatestPublicSnapshot(fixture.ctx)
	require.NoError(t, err)
	require.NotNil(t, latestTie)
	require.True(t, tiePublishedAt.Equal(latestTie.PublishedAt))

	tieSnapshots, err := fixture.client.BenchmarkPublicSnapshot.Query().
		Where(benchmarkpublicsnapshot.PublishedAtEQ(tiePublishedAt)).
		Order(dbent.Asc(benchmarkpublicsnapshot.FieldID)).
		All(fixture.ctx)
	require.NoError(t, err)
	require.Len(t, tieSnapshots, 2)
	require.Equal(t, tieSnapshots[1].ID, latestTie.ID)
	require.Equal(t, "tie-b", latestTie.Snapshot["phase"])
	require.Greater(t, latestTie.ID, tieSnapshots[0].ID)
}

func TestBenchmarkRepositoryGetLatestPublicSnapshotEmpty(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewBenchmarkRepository(tx.Client())

	snapshot, err := repo.GetLatestPublicSnapshot(txCtx)
	require.NoError(t, err)
	require.Nil(t, snapshot)
}

func TestBenchmarkRepositoryUpdateResultClearsNullableFields(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewBenchmarkRepository(client)

	suite, err := repo.CreateSuite(txCtx, service.BenchmarkSuiteInput{
		Name:          uniqueTestValue(t, "clear-suite"),
		Slug:          uniqueTestValue(t, "clear-suite"),
		Enabled:       true,
		PublicVisible: true,
	})
	require.NoError(t, err)

	target, err := repo.CreateTarget(txCtx, service.BenchmarkTargetInput{
		ModelName:          uniqueTestValue(t, "clear-model"),
		ChannelID:          202,
		SupportedTaskTypes: []string{"reasoning"},
		Enabled:            true,
		PublicVisible:      true,
		SortOrder:          1,
	})
	require.NoError(t, err)

	task, err := repo.CreateTask(txCtx, service.BenchmarkTaskInput{
		SuiteID:      suite.ID,
		Title:        "Clear fields task",
		Type:         "reasoning",
		Prompt:       "clear fields prompt",
		VerifierType: "exact_match",
		Enabled:      true,
	})
	require.NoError(t, err)

	profile, err := repo.CreateProfile(txCtx, service.BenchmarkProfileInput{
		SuiteID:          suite.ID,
		Name:             "Clear fields profile",
		TargetIDs:        []int64{target.ID},
		TaskTypes:        []string{"reasoning"},
		TaskScale:        service.BenchmarkTaskScaleSmall,
		SamplingStrategy: "seeded",
		Enabled:          true,
	})
	require.NoError(t, err)

	run, err := repo.CreateRunWithSnapshots(txCtx, service.BenchmarkCreateRunInput{
		SuiteID:     suite.ID,
		ProfileID:   profile.ID,
		Status:      service.BenchmarkRunStatusQueued,
		TriggerType: "manual",
		TaskScale:   service.BenchmarkTaskScaleSmall,
		TaskTypes:   []string{"reasoning"},
		Targets: []service.BenchmarkRunTargetInput{
			{
				TargetID:    target.ID,
				ModelName:   target.ModelName,
				ChannelID:   target.ChannelID,
				TargetOrder: 1,
			},
		},
		Tasks: []service.BenchmarkRunTaskInput{
			{
				TaskID:               task.ID,
				TaskOrder:            1,
				Type:                 task.Type,
				PromptSnapshot:       task.Prompt,
				VerifierTypeSnapshot: task.VerifierType,
			},
		},
	})
	require.NoError(t, err)

	result, err := client.BenchmarkResult.Query().
		Where(benchmarkresult.RunIDEQ(run.ID)).
		Only(txCtx)
	require.NoError(t, err)

	requestID := "req-clear-1"
	score := 98.5
	maxScore := 100.0
	normalizedScore := 0.985
	evaluatorType := "manual"
	latencyMS := 1234
	errorCode := "E42"
	errorMessage := "initial error"
	startedAt := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	finishedAt := time.Date(2026, 6, 23, 10, 1, 0, 0, time.UTC)

	err = repo.UpdateResult(txCtx, result.ID, service.BenchmarkResultUpdateInput{
		RequestID:       &requestID,
		Score:           &score,
		MaxScore:        &maxScore,
		NormalizedScore: &normalizedScore,
		EvaluatorType:   &evaluatorType,
		LatencyMS:       &latencyMS,
		ErrorCode:       &errorCode,
		ErrorMessage:    &errorMessage,
		StartedAt:       &startedAt,
		FinishedAt:      &finishedAt,
	})
	require.NoError(t, err)

	updated, err := client.BenchmarkResult.Query().
		Where(benchmarkresult.IDEQ(result.ID)).
		Only(txCtx)
	require.NoError(t, err)
	require.NotNil(t, updated.RequestID)
	require.Equal(t, requestID, *updated.RequestID)
	require.NotNil(t, updated.Score)
	require.InDelta(t, score, *updated.Score, 0.000001)
	require.NotNil(t, updated.MaxScore)
	require.InDelta(t, maxScore, *updated.MaxScore, 0.000001)
	require.NotNil(t, updated.NormalizedScore)
	require.InDelta(t, normalizedScore, *updated.NormalizedScore, 0.000001)
	require.NotNil(t, updated.EvaluatorType)
	require.Equal(t, evaluatorType, *updated.EvaluatorType)
	require.NotNil(t, updated.LatencyMs)
	require.Equal(t, latencyMS, *updated.LatencyMs)
	require.NotNil(t, updated.ErrorCode)
	require.Equal(t, errorCode, *updated.ErrorCode)
	require.NotNil(t, updated.ErrorMessage)
	require.Equal(t, errorMessage, *updated.ErrorMessage)
	require.NotNil(t, updated.StartedAt)
	require.True(t, startedAt.Equal(*updated.StartedAt))
	require.NotNil(t, updated.FinishedAt)
	require.True(t, finishedAt.Equal(*updated.FinishedAt))

	// Case 2: while the row is populated, a conflicting update must still clear.
	sameCallRequestID := "req-same-call"
	sameCallScore := 77.7
	sameCallMaxScore := 88.8
	sameCallNormalizedScore := 0.875
	sameCallEvaluatorType := "automatic"
	sameCallLatencyMS := 4321
	sameCallErrorCode := "E99"
	sameCallErrorMessage := "same-call error"
	sameCallStartedAt := time.Date(2026, 6, 23, 11, 0, 0, 0, time.UTC)
	sameCallFinishedAt := time.Date(2026, 6, 23, 11, 1, 0, 0, time.UTC)

	err = repo.UpdateResult(txCtx, result.ID, service.BenchmarkResultUpdateInput{
		RequestID:            &sameCallRequestID,
		ClearRequestID:       true,
		Score:                &sameCallScore,
		ClearScore:           true,
		MaxScore:             &sameCallMaxScore,
		ClearMaxScore:        true,
		NormalizedScore:      &sameCallNormalizedScore,
		ClearNormalizedScore: true,
		EvaluatorType:        &sameCallEvaluatorType,
		ClearEvaluatorType:   true,
		LatencyMS:            &sameCallLatencyMS,
		ClearLatencyMS:       true,
		ErrorCode:            &sameCallErrorCode,
		ClearErrorCode:       true,
		ErrorMessage:         &sameCallErrorMessage,
		ClearErrorMessage:    true,
		StartedAt:            &sameCallStartedAt,
		ClearStartedAt:       true,
		FinishedAt:           &sameCallFinishedAt,
		ClearFinishedAt:      true,
	})
	require.NoError(t, err)

	sameCallCleared, err := client.BenchmarkResult.Query().
		Where(benchmarkresult.IDEQ(result.ID)).
		Only(txCtx)
	require.NoError(t, err)
	require.Nil(t, sameCallCleared.RequestID)
	require.Nil(t, sameCallCleared.Score)
	require.Nil(t, sameCallCleared.MaxScore)
	require.Nil(t, sameCallCleared.NormalizedScore)
	require.Nil(t, sameCallCleared.EvaluatorType)
	require.Nil(t, sameCallCleared.LatencyMs)
	require.Nil(t, sameCallCleared.ErrorCode)
	require.Nil(t, sameCallCleared.ErrorMessage)
	require.Nil(t, sameCallCleared.StartedAt)
	require.Nil(t, sameCallCleared.FinishedAt)
}

func ptrInt64(v int64) *int64 {
	return &v
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
