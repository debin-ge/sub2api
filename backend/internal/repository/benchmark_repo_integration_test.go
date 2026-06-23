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
	extraTargetIDs []int64
}

func cleanupBenchmarkFixture(ctx context.Context, client *dbent.Client, fixture *benchmarkFixture) {
	for i := len(fixture.runIDs) - 1; i >= 0; i-- {
		runID := fixture.runIDs[i]
		_, _ = client.BenchmarkPublicSnapshot.Delete().Where(benchmarkpublicsnapshot.RunIDEQ(runID)).Exec(ctx)
		_, _ = client.BenchmarkScoreSnapshot.Delete().Where(benchmarkscoresnapshot.RunIDEQ(runID)).Exec(ctx)
		_, _ = client.BenchmarkResult.Delete().Where(benchmarkresult.RunIDEQ(runID)).Exec(ctx)
		_, _ = client.BenchmarkRunTask.Delete().Where(benchmarkruntask.RunIDEQ(runID)).Exec(ctx)
		_, _ = client.BenchmarkRunTarget.Delete().Where(benchmarkruntarget.RunIDEQ(runID)).Exec(ctx)
		_, _ = client.BenchmarkRun.Delete().Where(benchmarkrun.IDEQ(runID)).Exec(ctx)
	}

	for i := len(fixture.extraTargetIDs) - 1; i >= 0; i-- {
		targetID := fixture.extraTargetIDs[i]
		_, _ = client.BenchmarkTarget.Delete().Where(benchmarktarget.IDEQ(targetID)).Exec(ctx)
	}

	if fixture.profile != nil {
		_, _ = client.BenchmarkProfile.Delete().Where(benchmarkprofile.IDEQ(fixture.profile.ID)).Exec(ctx)
	}
	if fixture.task != nil {
		_, _ = client.BenchmarkTask.Delete().Where(benchmarktask.IDEQ(fixture.task.ID)).Exec(ctx)
	}
	if fixture.target != nil {
		_, _ = client.BenchmarkTarget.Delete().Where(benchmarktarget.IDEQ(fixture.target.ID)).Exec(ctx)
	}
	if fixture.suite != nil {
		_, _ = client.BenchmarkSuite.Delete().Where(benchmarksuite.IDEQ(fixture.suite.ID)).Exec(ctx)
	}
}

func newBenchmarkFixture(t *testing.T, prefix string) *benchmarkFixture {
	t.Helper()
	return newBenchmarkFixtureWith(t, context.Background(), testEntClient(t), prefix)
}

func newBenchmarkFixtureWith(t *testing.T, ctx context.Context, client *dbent.Client, prefix string) *benchmarkFixture {
	t.Helper()

	repo := NewBenchmarkRepository(client)
	fixture := &benchmarkFixture{
		ctx:    ctx,
		client: client,
		repo:   repo,
	}

	t.Cleanup(func() {
		cleanupBenchmarkFixture(ctx, client, fixture)
	})

	suite, err := repo.CreateSuite(ctx, service.BenchmarkSuiteInput{
		Name:          uniqueTestValue(t, prefix+"-suite"),
		Slug:          uniqueTestValue(t, prefix+"-suite"),
		Description:   "integration suite",
		Enabled:       true,
		PublicVisible: true,
		Metadata:      map[string]any{"scope": prefix},
	})
	require.NoError(t, err)
	fixture.suite = suite

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
	fixture.target = target

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
	fixture.task = task

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
	fixture.profile = profile

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
	fixture.createRunInput = createRunInput
	fixture.runIDs = []int64{run.ID}

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

func TestBenchmarkRepositoryListTargetsByIDsPreservesDedupedInputOrderAndReportsMissingIDs(t *testing.T) {
	fixture := newBenchmarkFixture(t, "tid")

	secondaryTarget, err := fixture.repo.CreateTarget(fixture.ctx, service.BenchmarkTargetInput{
		ModelName:          uniqueTestValue(t, "tid-secondary"),
		ChannelID:          102,
		DisplayName:        "Secondary Radar Model",
		SupportedTaskTypes: []string{"reasoning"},
		MaxConcurrency:     1,
		Enabled:            true,
		PublicVisible:      true,
		SortOrder:          2,
	})
	require.NoError(t, err)
	fixture.extraTargetIDs = append(fixture.extraTargetIDs, secondaryTarget.ID)

	targets, err := fixture.repo.ListTargetsByIDs(fixture.ctx, []int64{
		secondaryTarget.ID,
		fixture.target.ID,
		secondaryTarget.ID,
		fixture.target.ID,
	})
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.Equal(t, []int64{secondaryTarget.ID, fixture.target.ID}, []int64{targets[0].ID, targets[1].ID})

	missingTargets, err := fixture.repo.ListTargetsByIDs(fixture.ctx, []int64{
		secondaryTarget.ID,
		-987654321,
		secondaryTarget.ID,
	})
	require.Nil(t, missingTargets)
	require.EqualError(t, err, "benchmark targets missing: [-987654321]")
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

func TestBenchmarkServiceCreateRunWithRepositoryMaterializesQueuedRun(t *testing.T) {
	fixture := newBenchmarkFixture(t, "service-create-run")
	svc := service.NewBenchmarkService(fixture.repo)

	run, err := svc.CreateRun(fixture.ctx, service.BenchmarkCreateRunRequest{
		ProfileID:   fixture.profile.ID,
		TriggerType: "scheduled",
	})
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, run.ID)

	storedRun, err := fixture.repo.GetRun(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, service.BenchmarkRunStatusQueued, storedRun.Status)
	require.Equal(t, fixture.suite.ID, storedRun.SuiteID)
	require.Equal(t, fixture.profile.ID, storedRun.ProfileID)

	runTargets, err := fixture.repo.ListRunTargets(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, runTargets, 1)
	require.Equal(t, fixture.target.ID, runTargets[0].TargetID)
	require.Equal(t, fixture.target.ModelName, runTargets[0].ModelName)
	require.NotEmpty(t, runTargets[0].ConfigSnapshot)

	runTasks, err := fixture.repo.ListRunTasks(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, runTasks, 1)
	require.Equal(t, fixture.task.ID, runTasks[0].TaskID)
	require.Equal(t, fixture.task.Prompt, runTasks[0].PromptSnapshot)
	require.NotEmpty(t, runTasks[0].TaskSnapshot)

	results, err := fixture.repo.ListRunResults(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, results, len(runTargets)*len(runTasks))
	for _, result := range results {
		require.Equal(t, service.BenchmarkResultStatusPending, result.Status)
		require.Equal(t, run.ID, result.RunID)
	}

	configSnapshot := storedRun.ConfigSnapshot
	require.Equal(t, float64(fixture.profile.ID), configSnapshot["profile_id"])
	require.Equal(t, service.BenchmarkTaskScaleSmall, configSnapshot["task_scale"])
	require.Equal(t, []any{"reasoning"}, configSnapshot["task_types"])
	require.Equal(t, float64(42), configSnapshot["selection_seed"])
	require.Contains(t, configSnapshot, "task_count_limit")
	require.Contains(t, configSnapshot, "per_type_limit")
	require.Contains(t, configSnapshot, "difficulty_filter")
	require.Contains(t, configSnapshot, "tag_filter")
	require.Equal(t, "seeded", configSnapshot["sampling_strategy"])
	require.Equal(t, "ability_score_only", configSnapshot["ranking_basis"])
	runtimeConfig, ok := configSnapshot["runtime_config"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, runtimeConfig, "temperature")
	scoringConfig, ok := configSnapshot["scoring_config"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, scoringConfig, "normalize")
}

func TestBenchmarkRepositoryReadRunSnapshots(t *testing.T) {
	fixture := newBenchmarkFixture(t, "read-run-snapshots")
	runID := fixture.runIDs[0]

	run, err := fixture.repo.GetRun(fixture.ctx, runID)
	require.NoError(t, err)
	require.Equal(t, runID, run.ID)
	require.Equal(t, service.BenchmarkRunStatusQueued, run.Status)
	require.Equal(t, fixture.suite.ID, run.SuiteID)
	require.Equal(t, fixture.profile.ID, run.ProfileID)

	runTargets, err := fixture.repo.ListRunTargets(fixture.ctx, runID)
	require.NoError(t, err)
	require.Len(t, runTargets, 1)
	require.Equal(t, fixture.target.ID, runTargets[0].TargetID)
	require.Equal(t, fixture.target.ModelName, runTargets[0].ModelName)
	require.Equal(t, fixture.target.ChannelID, runTargets[0].ChannelID)
	require.NotNil(t, runTargets[0].DisplayNameSnapshot)
	require.Equal(t, "Radar Model Snapshot", *runTargets[0].DisplayNameSnapshot)
	require.NotNil(t, runTargets[0].ChannelNameSnapshot)
	require.Equal(t, "primary-openai", *runTargets[0].ChannelNameSnapshot)
	require.NotNil(t, runTargets[0].ProviderSnapshot)
	require.Equal(t, "openai", *runTargets[0].ProviderSnapshot)

	runTasks, err := fixture.repo.ListRunTasks(fixture.ctx, runID)
	require.NoError(t, err)
	require.Len(t, runTasks, 1)
	require.Equal(t, fixture.task.ID, runTasks[0].TaskID)
	require.Equal(t, fixture.task.Type, runTasks[0].Type)
	require.Equal(t, "Original reasoning prompt", runTasks[0].PromptSnapshot)
	require.Equal(t, fixture.task.VerifierType, runTasks[0].VerifierTypeSnapshot)
	require.Equal(t, "answer", runTasks[0].VerifierConfigSnapshot["field"])
	require.InDelta(t, fixture.task.Weight, runTasks[0].WeightSnapshot, 0.000001)

	results, err := fixture.repo.ListRunResults(fixture.ctx, runID)
	require.NoError(t, err)
	require.Len(t, results, len(runTargets)*len(runTasks))
	require.Equal(t, service.BenchmarkResultStatusPending, results[0].Status)
	require.Equal(t, runTargets[0].ID, results[0].RunTargetID)
	require.Equal(t, runTasks[0].ID, results[0].RunTaskID)
}

func TestBenchmarkRepositoryListRunScoreInputs(t *testing.T) {
	fixture := newBenchmarkFixture(t, "run-score-inputs")

	secondaryTarget, err := fixture.repo.CreateTarget(fixture.ctx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, "run-score-inputs-secondary-model"),
		ChannelID:           102,
		DisplayName:         "Secondary Radar Model",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "secondary-openai",
		SupportedTaskTypes:  []string{"reasoning", "coding"},
		MaxConcurrency:      1,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           2,
		Metadata:            map[string]any{"tier": "secondary"},
	})
	require.NoError(t, err)
	fixture.extraTargetIDs = append(fixture.extraTargetIDs, secondaryTarget.ID)

	secondaryTask, err := fixture.repo.CreateTask(fixture.ctx, service.BenchmarkTaskInput{
		SuiteID:        fixture.suite.ID,
		Title:          "Coding snapshot task",
		Type:           "coding",
		Category:       "code",
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
		Metadata:       map[string]any{"case": "secondary"},
	})
	require.NoError(t, err)

	runInput := fixture.createRunInput
	runInput.Targets = []service.BenchmarkRunTargetInput{
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
	runInput.Tasks = []service.BenchmarkRunTaskInput{
		{
			TaskID:                 fixture.task.ID,
			TaskOrder:              1,
			Type:                   fixture.task.Type,
			Category:               stringValue(fixture.task.Category),
			Difficulty:             stringValue(fixture.task.Difficulty),
			WeightSnapshot:         fixture.task.Weight,
			PromptSnapshot:         fixture.task.Prompt,
			VerifierTypeSnapshot:   fixture.task.VerifierType,
			VerifierConfigSnapshot: fixture.task.VerifierConfig,
			TaskSnapshot: map[string]any{
				"title":  fixture.task.Title,
				"prompt": fixture.task.Prompt,
			},
		},
		{
			TaskID:                 secondaryTask.ID,
			TaskOrder:              2,
			Type:                   secondaryTask.Type,
			Category:               stringValue(secondaryTask.Category),
			Difficulty:             stringValue(secondaryTask.Difficulty),
			WeightSnapshot:         secondaryTask.Weight,
			PromptSnapshot:         secondaryTask.Prompt,
			VerifierTypeSnapshot:   secondaryTask.VerifierType,
			VerifierConfigSnapshot: secondaryTask.VerifierConfig,
			TaskSnapshot: map[string]any{
				"title":  secondaryTask.Title,
				"prompt": secondaryTask.Prompt,
			},
		},
	}

	run, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, runInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, run.ID)

	_, err = fixture.client.BenchmarkTarget.UpdateOneID(fixture.target.ID).
		SetModelName("mutated-primary-model").
		SetDisplayName("Mutated Primary Model").
		Save(fixture.ctx)
	require.NoError(t, err)
	_, err = fixture.client.BenchmarkTarget.UpdateOneID(secondaryTarget.ID).
		SetModelName("mutated-secondary-model").
		SetDisplayName("Mutated Secondary Model").
		Save(fixture.ctx)
	require.NoError(t, err)
	_, err = fixture.client.BenchmarkTask.UpdateOneID(fixture.task.ID).
		SetPrompt("Mutated reasoning prompt").
		Save(fixture.ctx)
	require.NoError(t, err)
	_, err = fixture.client.BenchmarkTask.UpdateOneID(secondaryTask.ID).
		SetPrompt("Mutated coding prompt").
		Save(fixture.ctx)
	require.NoError(t, err)

	scoreInputs, err := fixture.repo.ListRunScoreInputs(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, scoreInputs, 4)

	require.NotNil(t, scoreInputs[0].RunTarget)
	require.NotNil(t, scoreInputs[0].RunTask)
	require.NotNil(t, scoreInputs[0].Result)
	require.Equal(t, fixture.target.ID, scoreInputs[0].RunTarget.TargetID)
	require.NotNil(t, scoreInputs[0].RunTarget.DisplayNameSnapshot)
	require.Equal(t, "Primary Model Snapshot", *scoreInputs[0].RunTarget.DisplayNameSnapshot)
	require.Equal(t, fixture.task.ID, scoreInputs[0].RunTask.TaskID)
	require.Equal(t, "Original reasoning prompt", scoreInputs[0].RunTask.PromptSnapshot)
	require.Equal(t, scoreInputs[0].RunTarget.ID, scoreInputs[0].Result.RunTargetID)
	require.Equal(t, scoreInputs[0].RunTask.ID, scoreInputs[0].Result.RunTaskID)

	require.Equal(t, fixture.target.ID, scoreInputs[1].RunTarget.TargetID)
	require.NotNil(t, scoreInputs[1].RunTask)
	require.Equal(t, secondaryTask.ID, scoreInputs[1].RunTask.TaskID)
	require.NotNil(t, scoreInputs[1].RunTask.PromptSnapshot)
	require.Equal(t, "Original coding prompt", scoreInputs[1].RunTask.PromptSnapshot)

	require.Equal(t, secondaryTarget.ID, scoreInputs[2].RunTarget.TargetID)
	require.Equal(t, fixture.task.ID, scoreInputs[2].RunTask.TaskID)
	require.NotNil(t, scoreInputs[3].RunTarget)
	require.Equal(t, secondaryTarget.ID, scoreInputs[3].RunTarget.TargetID)
	require.Equal(t, secondaryTask.ID, scoreInputs[3].RunTask.TaskID)

	for _, scoreInput := range scoreInputs {
		require.NotNil(t, scoreInput.RunTarget)
		require.NotNil(t, scoreInput.RunTask)
		require.NotNil(t, scoreInput.Result)
		require.Equal(t, run.ID, scoreInput.Result.RunID)
	}
}

func TestBenchmarkRepositoryListScoreSnapshotsUsesRankingOrder(t *testing.T) {
	fixture := newBenchmarkFixture(t, "score-snapshot-order")

	secondaryTarget, err := fixture.repo.CreateTarget(fixture.ctx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, "score-snapshot-order-secondary-model"),
		ChannelID:           103,
		DisplayName:         "Secondary Snapshot Model",
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
	fixture.extraTargetIDs = append(fixture.extraTargetIDs, secondaryTarget.ID)

	runOneInput := fixture.createRunInput
	runOneInput.Targets = []service.BenchmarkRunTargetInput{
		{
			TargetID:            fixture.target.ID,
			ModelName:           fixture.target.ModelName,
			ChannelID:           fixture.target.ChannelID,
			DisplayNameSnapshot: "Primary Snapshot Model",
			ChannelNameSnapshot: "primary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         1,
			ConfigSnapshot:      map[string]any{"max_concurrency": 2},
		},
		{
			TargetID:            secondaryTarget.ID,
			ModelName:           secondaryTarget.ModelName,
			ChannelID:           secondaryTarget.ChannelID,
			DisplayNameSnapshot: "Secondary Snapshot Model",
			ChannelNameSnapshot: "secondary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         2,
			ConfigSnapshot:      map[string]any{"max_concurrency": 1},
		},
	}
	runOneInput.Tasks = []service.BenchmarkRunTaskInput{
		{
			TaskID:                 fixture.task.ID,
			TaskOrder:              1,
			Type:                   fixture.task.Type,
			Category:               stringValue(fixture.task.Category),
			Difficulty:             stringValue(fixture.task.Difficulty),
			WeightSnapshot:         fixture.task.Weight,
			PromptSnapshot:         fixture.task.Prompt,
			VerifierTypeSnapshot:   fixture.task.VerifierType,
			VerifierConfigSnapshot: fixture.task.VerifierConfig,
			TaskSnapshot: map[string]any{
				"title":  fixture.task.Title,
				"prompt": fixture.task.Prompt,
			},
		},
	}
	runOne, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, runOneInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, runOne.ID)

	secondRunInput := fixture.createRunInput
	secondRunInput.Targets = []service.BenchmarkRunTargetInput{
		{
			TargetID:            secondaryTarget.ID,
			ModelName:           secondaryTarget.ModelName,
			ChannelID:           secondaryTarget.ChannelID,
			DisplayNameSnapshot: "Secondary Snapshot Model",
			ChannelNameSnapshot: "secondary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         1,
			ConfigSnapshot:      map[string]any{"max_concurrency": 1},
		},
	}
	secondRun, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, secondRunInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, secondRun.ID)

	primaryRunTarget := fixture.runTargetByTargetID(t, runOne.ID, fixture.target.ID)
	secondaryRunTarget, err := fixture.client.BenchmarkRunTarget.Query().
		Where(
			benchmarkruntarget.RunIDEQ(runOne.ID),
			benchmarkruntarget.TargetIDEQ(secondaryTarget.ID),
		).
		Only(fixture.ctx)
	require.NoError(t, err)
	secondRunTarget, err := fixture.client.BenchmarkRunTarget.Query().
		Where(
			benchmarkruntarget.RunIDEQ(secondRun.ID),
			benchmarkruntarget.TargetIDEQ(secondaryTarget.ID),
		).
		Only(fixture.ctx)
	require.NoError(t, err)

	require.NoError(t, fixture.repo.SaveScoreSnapshots(fixture.ctx, secondRun.ID, []service.BenchmarkScoreSnapshotInput{
		{
			RunTargetID:            secondRunTarget.ID,
			OverallScore:           99.0,
			DimensionScores:        map[string]any{"rank": "second-run"},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           0.99,
			ConfidenceLevel:        service.BenchmarkConfidenceHigh,
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.2,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "second-run"},
		},
	}))

	require.NoError(t, fixture.repo.SaveScoreSnapshots(fixture.ctx, runOne.ID, []service.BenchmarkScoreSnapshotInput{
		{
			RunTargetID:            primaryRunTarget.ID,
			OverallScore:           91.0,
			DimensionScores:        map[string]any{"rank": "primary"},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           0.90,
			ConfidenceLevel:        service.BenchmarkConfidenceHigh,
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.09,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "primary"},
		},
		{
			RunTargetID:            secondaryRunTarget.ID,
			OverallScore:           91.0,
			DimensionScores:        map[string]any{"rank": "secondary"},
			PlannedTasks:           1,
			ScoredTasks:            1,
			InvalidTasks:           0,
			CoverageRate:           0.95,
			ConfidenceLevel:        service.BenchmarkConfidenceHigh,
			InsufficientSample:     false,
			SuccessRate:            1.0,
			EstimatedCost:          0.10,
			InvalidReasonBreakdown: map[string]any{},
			RankingMetadata:        map[string]any{"slot": "secondary"},
		},
	}))

	snapshots, err := fixture.repo.ListScoreSnapshots(fixture.ctx, runOne.ID)
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, secondaryRunTarget.ID, snapshots[0].RunTargetID)
	require.InDelta(t, 91.0, snapshots[0].OverallScore, 0.000001)
	require.InDelta(t, 0.95, snapshots[0].CoverageRate, 0.000001)
	require.Equal(t, primaryRunTarget.ID, snapshots[1].RunTargetID)
	require.InDelta(t, 91.0, snapshots[1].OverallScore, 0.000001)
	require.InDelta(t, 0.90, snapshots[1].CoverageRate, 0.000001)
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
	fixture.extraTargetIDs = append(fixture.extraTargetIDs, secondaryTarget.ID)

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

	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(fixture.ctx, tx)

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

	err = fixture.repo.SaveScoreSnapshots(txCtx, batchRun.ID, partialReplacement)
	require.Error(t, err)

	afterSnapshots, err := tx.Client().BenchmarkScoreSnapshot.Query().
		Where(benchmarkscoresnapshot.RunIDEQ(batchRun.ID)).
		Order(dbent.Asc(benchmarkscoresnapshot.FieldRunTargetID)).
		All(txCtx)
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

	_, err = tx.Client().BenchmarkScoreSnapshot.Query().
		Where(
			benchmarkscoresnapshot.RunIDEQ(batchRun.ID),
			benchmarkscoresnapshot.OverallScoreEQ(99.9),
		).
		Only(txCtx)
	require.Error(t, err)
}

func TestBenchmarkRepositoryCreateRunWithSnapshotsRollsBackOnDuplicateRunTaskError(t *testing.T) {
	fixture := newBenchmarkFixture(t, "run-rb")

	beforeRuns, err := fixture.client.BenchmarkRun.Query().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)

	beforeRunTargets, err := fixture.client.BenchmarkRunTarget.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)

	beforeRunTasks, err := fixture.client.BenchmarkRunTask.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)

	beforeResults, err := fixture.client.BenchmarkResult.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)

	input := fixture.createRunInput
	input.Tasks = append(input.Tasks, service.BenchmarkRunTaskInput{
		TaskID:                 fixture.createRunInput.Tasks[0].TaskID,
		TaskOrder:              fixture.createRunInput.Tasks[0].TaskOrder + 1,
		Type:                   fixture.createRunInput.Tasks[0].Type,
		Category:               fixture.createRunInput.Tasks[0].Category,
		Difficulty:             fixture.createRunInput.Tasks[0].Difficulty,
		WeightSnapshot:         fixture.createRunInput.Tasks[0].WeightSnapshot,
		PromptSnapshot:         fixture.createRunInput.Tasks[0].PromptSnapshot,
		VerifierTypeSnapshot:   fixture.createRunInput.Tasks[0].VerifierTypeSnapshot,
		VerifierConfigSnapshot: fixture.createRunInput.Tasks[0].VerifierConfigSnapshot,
		TaskSnapshot:           fixture.createRunInput.Tasks[0].TaskSnapshot,
	})

	run, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, input)
	require.Error(t, err)
	require.Nil(t, run)

	afterRuns, err := fixture.client.BenchmarkRun.Query().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, beforeRuns, afterRuns)

	afterRunTargets, err := fixture.client.BenchmarkRunTarget.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, beforeRunTargets, afterRunTargets)

	afterRunTasks, err := fixture.client.BenchmarkRunTask.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, beforeRunTasks, afterRunTasks)

	afterResults, err := fixture.client.BenchmarkResult.Query().
		QueryRun().
		Where(
			benchmarkrun.SuiteIDEQ(fixture.suite.ID),
			benchmarkrun.ProfileIDEQ(fixture.profile.ID),
		).
		Count(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, beforeResults, afterResults)
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

	_, err := tx.Client().BenchmarkPublicSnapshot.Delete().Exec(txCtx)
	require.NoError(t, err)

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
	evaluatorOutput := map[string]any{"error": "old-parse-error"}
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
		EvaluatorOutput: evaluatorOutput,
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
	require.Equal(t, evaluatorOutput, updated.EvaluatorOutput)
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
	sameCallEvaluatorOutput := map[string]any{"error": "new-error"}
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
		EvaluatorOutput:      sameCallEvaluatorOutput,
		ClearEvaluatorOutput: true,
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
	require.Empty(t, sameCallCleared.EvaluatorOutput)
	require.Nil(t, sameCallCleared.LatencyMs)
	require.Nil(t, sameCallCleared.ErrorCode)
	require.Nil(t, sameCallCleared.ErrorMessage)
	require.Nil(t, sameCallCleared.StartedAt)
	require.Nil(t, sameCallCleared.FinishedAt)

	// Case 3: a pure clear call should remain a no-op on already-null fields.
	err = repo.UpdateResult(txCtx, result.ID, service.BenchmarkResultUpdateInput{
		ClearRequestID:       true,
		ClearScore:           true,
		ClearMaxScore:        true,
		ClearNormalizedScore: true,
		ClearEvaluatorType:   true,
		ClearEvaluatorOutput: true,
		ClearLatencyMS:       true,
		ClearErrorCode:       true,
		ClearErrorMessage:    true,
		ClearStartedAt:       true,
		ClearFinishedAt:      true,
	})
	require.NoError(t, err)

	clearedAgain, err := client.BenchmarkResult.Query().
		Where(benchmarkresult.IDEQ(result.ID)).
		Only(txCtx)
	require.NoError(t, err)
	require.Nil(t, clearedAgain.RequestID)
	require.Nil(t, clearedAgain.Score)
	require.Nil(t, clearedAgain.MaxScore)
	require.Nil(t, clearedAgain.NormalizedScore)
	require.Nil(t, clearedAgain.EvaluatorType)
	require.Empty(t, clearedAgain.EvaluatorOutput)
	require.Nil(t, clearedAgain.LatencyMs)
	require.Nil(t, clearedAgain.ErrorCode)
	require.Nil(t, clearedAgain.ErrorMessage)
	require.Nil(t, clearedAgain.StartedAt)
	require.Nil(t, clearedAgain.FinishedAt)
}

func TestBenchmarkRepositoryRequeueClaimedResults(t *testing.T) {
	fixture := newBenchmarkFixture(t, "requeue-claimed")

	secondTask, err := fixture.repo.CreateTask(fixture.ctx, service.BenchmarkTaskInput{
		SuiteID:        fixture.suite.ID,
		Title:          "Requeue second task",
		Type:           "reasoning",
		Category:       "logic",
		Difficulty:     "medium",
		Prompt:         "Second prompt",
		InputPayload:   map[string]any{"question": "3+3"},
		ExpectedOutput: map[string]any{"answer": "6"},
		VerifierType:   "exact_match",
		VerifierConfig: map[string]any{"expected": "6"},
		Weight:         1,
		MinScale:       service.BenchmarkTaskScaleSmall,
		Enabled:        true,
	})
	require.NoError(t, err)

	runInput := fixture.createRunInput
	runInput.Tasks = append(runInput.Tasks, service.BenchmarkRunTaskInput{
		TaskID:                 secondTask.ID,
		TaskOrder:              2,
		Type:                   secondTask.Type,
		Category:               stringValue(secondTask.Category),
		Difficulty:             stringValue(secondTask.Difficulty),
		WeightSnapshot:         secondTask.Weight,
		PromptSnapshot:         secondTask.Prompt,
		VerifierTypeSnapshot:   secondTask.VerifierType,
		VerifierConfigSnapshot: secondTask.VerifierConfig,
		TaskSnapshot: map[string]any{
			"title":         secondTask.Title,
			"prompt":        secondTask.Prompt,
			"input_payload": secondTask.InputPayload,
		},
	})

	run, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, runInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, run.ID)

	claimed, err := fixture.repo.ClaimPendingResults(fixture.ctx, run.ID, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	scoredStatus := service.BenchmarkResultStatusScored
	require.NoError(t, fixture.repo.UpdateResult(fixture.ctx, claimed[0].ID, service.BenchmarkResultUpdateInput{
		Status: &scoredStatus,
	}))

	err = fixture.repo.RequeueClaimedResults(fixture.ctx, []int64{claimed[0].ID, claimed[1].ID})
	require.NoError(t, err)

	results, err := fixture.repo.ListRunResults(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byID := make(map[int64]*dbent.BenchmarkResult, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}

	require.Equal(t, service.BenchmarkResultStatusScored, byID[claimed[0].ID].Status)
	require.Equal(t, service.BenchmarkResultStatusPending, byID[claimed[1].ID].Status)
}

func TestBenchmarkRepositoryClaimPendingResults(t *testing.T) {
	fixture := newBenchmarkFixture(t, "claim-pending")

	secondTask, err := fixture.repo.CreateTask(fixture.ctx, service.BenchmarkTaskInput{
		SuiteID:        fixture.suite.ID,
		Title:          "Second claim task",
		Type:           "reasoning",
		Category:       "logic",
		Difficulty:     "medium",
		Prompt:         "Second prompt",
		InputPayload:   map[string]any{"question": "3+3"},
		ExpectedOutput: map[string]any{"answer": "6"},
		VerifierType:   "exact_match",
		VerifierConfig: map[string]any{"expected": "6"},
		Weight:         1,
		MinScale:       service.BenchmarkTaskScaleSmall,
		Enabled:        true,
	})
	require.NoError(t, err)

	secondTarget, err := fixture.repo.CreateTarget(fixture.ctx, service.BenchmarkTargetInput{
		ModelName:           uniqueTestValue(t, "claim-pending-secondary-model"),
		ChannelID:           102,
		DisplayName:         "Secondary claim model",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "secondary-openai",
		SupportedTaskTypes:  []string{"reasoning"},
		MaxConcurrency:      1,
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           2,
	})
	require.NoError(t, err)
	fixture.extraTargetIDs = append(fixture.extraTargetIDs, secondTarget.ID)

	runInput := fixture.createRunInput
	runInput.Targets = []service.BenchmarkRunTargetInput{
		{
			TargetID:            fixture.target.ID,
			ModelName:           fixture.target.ModelName,
			ChannelID:           fixture.target.ChannelID,
			DisplayNameSnapshot: "Primary snapshot",
			ChannelNameSnapshot: "primary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         1,
			ConfigSnapshot:      map[string]any{"max_concurrency": 2},
		},
		{
			TargetID:            secondTarget.ID,
			ModelName:           secondTarget.ModelName,
			ChannelID:           secondTarget.ChannelID,
			DisplayNameSnapshot: "Secondary snapshot",
			ChannelNameSnapshot: "secondary-openai",
			ProviderSnapshot:    "openai",
			TargetOrder:         2,
			ConfigSnapshot:      map[string]any{"max_concurrency": 1},
		},
	}
	runInput.Tasks = append(runInput.Tasks, service.BenchmarkRunTaskInput{
		TaskID:                 secondTask.ID,
		TaskOrder:              2,
		Type:                   secondTask.Type,
		Category:               stringValue(secondTask.Category),
		Difficulty:             stringValue(secondTask.Difficulty),
		WeightSnapshot:         secondTask.Weight,
		PromptSnapshot:         secondTask.Prompt,
		VerifierTypeSnapshot:   secondTask.VerifierType,
		VerifierConfigSnapshot: secondTask.VerifierConfig,
		TaskSnapshot: map[string]any{
			"title":         secondTask.Title,
			"prompt":        secondTask.Prompt,
			"input_payload": secondTask.InputPayload,
		},
	})

	run, err := fixture.repo.CreateRunWithSnapshots(fixture.ctx, runInput)
	require.NoError(t, err)
	fixture.runIDs = append(fixture.runIDs, run.ID)

	results, err := fixture.repo.ListRunResults(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, results, 4)

	rateLimitedStatus := service.BenchmarkResultStatusRateLimited
	require.NoError(t, fixture.repo.UpdateResult(fixture.ctx, results[1].ID, service.BenchmarkResultUpdateInput{
		Status: &rateLimitedStatus,
	}))

	runningStatus := service.BenchmarkResultStatusRunning
	require.NoError(t, fixture.repo.UpdateResult(fixture.ctx, results[3].ID, service.BenchmarkResultUpdateInput{
		Status: &runningStatus,
	}))

	claimed, err := fixture.repo.ClaimPendingResults(fixture.ctx, run.ID, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, []int64{results[0].ID, results[2].ID}, []int64{claimed[0].ID, claimed[1].ID})

	for _, result := range claimed {
		require.Equal(t, service.BenchmarkResultStatusRunning, result.Status)
		require.Equal(t, 1, result.AttemptCount)
	}

	stored, err := fixture.repo.ListRunResults(fixture.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, stored, 4)

	byID := make(map[int64]*dbent.BenchmarkResult, len(stored))
	for _, result := range stored {
		byID[result.ID] = result
	}

	require.Equal(t, service.BenchmarkResultStatusRunning, byID[results[0].ID].Status)
	require.Equal(t, 1, byID[results[0].ID].AttemptCount)
	require.Equal(t, service.BenchmarkResultStatusRateLimited, byID[results[1].ID].Status)
	require.Equal(t, 0, byID[results[1].ID].AttemptCount)
	require.Equal(t, service.BenchmarkResultStatusRunning, byID[results[2].ID].Status)
	require.Equal(t, 1, byID[results[2].ID].AttemptCount)
	require.Equal(t, service.BenchmarkResultStatusRunning, byID[results[3].ID].Status)
	require.Equal(t, 0, byID[results[3].ID].AttemptCount)
}

func TestBenchmarkRepositoryRunResultContext(t *testing.T) {
	fixture := newBenchmarkFixture(t, "run-result-context")
	runID := fixture.runIDs[0]

	results, err := fixture.repo.ListRunResults(fixture.ctx, runID)
	require.NoError(t, err)
	require.Len(t, results, 1)

	_, err = fixture.client.BenchmarkTarget.UpdateOneID(fixture.target.ID).
		SetModelName("mutated-model-name").
		SetChannelID(999).
		Save(fixture.ctx)
	require.NoError(t, err)

	_, err = fixture.client.BenchmarkTask.UpdateOneID(fixture.task.ID).
		SetPrompt("mutated prompt").
		SetVerifierConfig(map[string]any{"field": "mutated"}).
		Save(fixture.ctx)
	require.NoError(t, err)

	resultCtx, err := fixture.repo.GetRunResultContext(fixture.ctx, results[0].ID)
	require.NoError(t, err)
	require.NotNil(t, resultCtx)
	require.Equal(t, results[0].ID, resultCtx.Result.ID)
	require.Equal(t, runID, resultCtx.Run.ID)
	require.Equal(t, fixture.runTarget(t, runID).ID, resultCtx.Target.ID)
	require.Equal(t, fixture.target.ID, resultCtx.Target.TargetID)
	require.Equal(t, fixture.target.ModelName, resultCtx.Target.ModelName)
	require.Equal(t, fixture.target.ChannelID, resultCtx.Target.ChannelID)
	require.NotNil(t, resultCtx.Target.DisplayNameSnapshot)
	require.Equal(t, "Radar Model Snapshot", *resultCtx.Target.DisplayNameSnapshot)
	require.Equal(t, fixture.task.ID, resultCtx.Task.TaskID)
	require.Equal(t, "Original reasoning prompt", resultCtx.Task.PromptSnapshot)
	require.Equal(t, fixture.task.VerifierType, resultCtx.Task.VerifierTypeSnapshot)
	require.Equal(t, "answer", resultCtx.Task.VerifierConfigSnapshot["field"])
	require.Equal(t, "Original reasoning prompt", resultCtx.Task.TaskSnapshot["prompt"])
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
