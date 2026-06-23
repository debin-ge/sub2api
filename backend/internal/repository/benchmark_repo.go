package repository

import (
	"context"
	"fmt"
	"sort"

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
)

const (
	defaultBenchmarkPage     = 1
	defaultBenchmarkPageSize = 20
	maxBenchmarkPageSize     = 100
)

type benchmarkRepository struct {
	client *dbent.Client
}

func NewBenchmarkRepository(client *dbent.Client) service.BenchmarkRepository {
	return &benchmarkRepository{client: client}
}

func (r *benchmarkRepository) CreateSuite(ctx context.Context, input service.BenchmarkSuiteInput) (*dbent.BenchmarkSuite, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkSuite.Create().
		SetName(input.Name).
		SetSlug(input.Slug).
		SetEnabled(input.Enabled).
		SetPublicVisible(input.PublicVisible).
		SetNillableDefaultProfileID(input.DefaultProfileID).
		SetMetadata(benchmarkMap(input.Metadata))
	if input.Description != "" {
		builder.SetDescription(input.Description)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) ListSuites(ctx context.Context, input service.BenchmarkListInput) ([]*dbent.BenchmarkSuite, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkSuite.Query()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := benchmarkLimitOffset(input)
	rows, err := query.
		Order(dbent.Asc(benchmarksuite.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) GetSuite(ctx context.Context, id int64) (*dbent.BenchmarkSuite, error) {
	return clientFromContext(ctx, r.client).BenchmarkSuite.Get(ctx, id)
}

func (r *benchmarkRepository) CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*dbent.BenchmarkTarget, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkTarget.Create().
		SetModelName(input.ModelName).
		SetChannelID(input.ChannelID).
		SetSupportedTaskTypes(benchmarkStringSlice(input.SupportedTaskTypes)).
		SetEnabled(input.Enabled).
		SetPublicVisible(input.PublicVisible).
		SetSortOrder(input.SortOrder).
		SetNillablePerRunBudget(input.PerRunBudget).
		SetNillableDailyBudget(input.DailyBudget).
		SetMetadata(benchmarkMap(input.Metadata))
	if input.DisplayName != "" {
		builder.SetDisplayName(input.DisplayName)
	}
	if input.ProviderSnapshot != "" {
		builder.SetProviderSnapshot(input.ProviderSnapshot)
	}
	if input.ChannelNameSnapshot != "" {
		builder.SetChannelNameSnapshot(input.ChannelNameSnapshot)
	}
	if input.MaxConcurrency > 0 {
		builder.SetMaxConcurrency(input.MaxConcurrency)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) ListTargets(ctx context.Context, input service.BenchmarkListInput) ([]*dbent.BenchmarkTarget, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkTarget.Query()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := benchmarkLimitOffset(input)
	rows, err := query.
		Order(dbent.Asc(benchmarktarget.FieldSortOrder), dbent.Asc(benchmarktarget.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) GetTarget(ctx context.Context, id int64) (*dbent.BenchmarkTarget, error) {
	return clientFromContext(ctx, r.client).BenchmarkTarget.Get(ctx, id)
}

func (r *benchmarkRepository) ListTargetsByIDs(ctx context.Context, ids []int64) ([]*dbent.BenchmarkTarget, error) {
	orderedIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		orderedIDs = append(orderedIDs, id)
	}
	if len(orderedIDs) == 0 {
		return []*dbent.BenchmarkTarget{}, nil
	}

	rows, err := clientFromContext(ctx, r.client).BenchmarkTarget.Query().
		Where(benchmarktarget.IDIn(orderedIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	byID := make(map[int64]*dbent.BenchmarkTarget, len(rows))
	for _, target := range rows {
		byID[target.ID] = target
	}
	ordered := make([]*dbent.BenchmarkTarget, 0, len(orderedIDs))
	missingIDs := make([]int64, 0)
	for _, id := range orderedIDs {
		target, ok := byID[id]
		if !ok {
			missingIDs = append(missingIDs, id)
			continue
		}
		ordered = append(ordered, target)
	}
	if len(missingIDs) > 0 {
		return nil, fmt.Errorf("benchmark targets missing: %v", missingIDs)
	}
	return ordered, nil
}

func (r *benchmarkRepository) CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*dbent.BenchmarkTask, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkTask.Create().
		SetSuiteID(input.SuiteID).
		SetTitle(input.Title).
		SetType(input.Type).
		SetTags(benchmarkStringSlice(input.Tags)).
		SetPrompt(input.Prompt).
		SetInputPayload(benchmarkMap(input.InputPayload)).
		SetExpectedOutput(benchmarkMap(input.ExpectedOutput)).
		SetVerifierType(input.VerifierType).
		SetVerifierConfig(benchmarkMap(input.VerifierConfig)).
		SetMinScale(benchmarktask.MinScale(benchmarkValueOrDefault(input.MinScale, service.BenchmarkTaskScaleSmall))).
		SetPublicPrompt(input.PublicPrompt).
		SetEnabled(input.Enabled).
		SetMetadata(benchmarkMap(input.Metadata))
	if input.Category != "" {
		builder.SetCategory(input.Category)
	}
	if input.Difficulty != "" {
		builder.SetDifficulty(input.Difficulty)
	}
	if input.Weight > 0 {
		builder.SetWeight(input.Weight)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*dbent.BenchmarkTask, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkTask.Query()
	if input.SuiteID > 0 {
		query = query.Where(benchmarktask.SuiteIDEQ(input.SuiteID))
	}
	if len(input.TaskTypes) > 0 {
		query = query.Where(benchmarktask.TypeIn(input.TaskTypes...))
	}
	if input.Enabled != nil {
		query = query.Where(benchmarktask.EnabledEQ(*input.Enabled))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := benchmarkLimitOffset(input.BenchmarkListInput)
	rows, err := query.
		Order(dbent.Asc(benchmarktask.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) ListEnabledTasksForSuite(ctx context.Context, suiteID int64) ([]*dbent.BenchmarkTask, error) {
	return clientFromContext(ctx, r.client).BenchmarkTask.Query().
		Where(
			benchmarktask.SuiteIDEQ(suiteID),
			benchmarktask.EnabledEQ(true),
		).
		Order(dbent.Asc(benchmarktask.FieldID)).
		All(ctx)
}

func (r *benchmarkRepository) CreateProfile(ctx context.Context, input service.BenchmarkProfileInput) (*dbent.BenchmarkProfile, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkProfile.Create().
		SetSuiteID(input.SuiteID).
		SetName(input.Name).
		SetTargetIds(benchmarkInt64Slice(input.TargetIDs)).
		SetTaskTypes(benchmarkStringSlice(input.TaskTypes)).
		SetTaskScale(benchmarkprofile.TaskScale(benchmarkValueOrDefault(input.TaskScale, service.BenchmarkTaskScaleMedium))).
		SetNillableTaskCountLimit(input.TaskCountLimit).
		SetPerTypeLimit(benchmarkIntMap(input.PerTypeLimit)).
		SetDifficultyFilter(benchmarkStringSlice(input.DifficultyFilter)).
		SetTagFilter(benchmarkStringSlice(input.TagFilter)).
		SetSamplingStrategy(benchmarkValueOrDefault(input.SamplingStrategy, "seeded")).
		SetNillableSelectionSeed(input.SelectionSeed).
		SetRuntimeConfig(benchmarkMap(input.RuntimeConfig)).
		SetScoringConfig(benchmarkMap(input.ScoringConfig)).
		SetEnabled(input.Enabled).
		SetMetadata(benchmarkMap(input.Metadata))
	if input.Description != "" {
		builder.SetDescription(input.Description)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) GetProfile(ctx context.Context, id int64) (*dbent.BenchmarkProfile, error) {
	return clientFromContext(ctx, r.client).BenchmarkProfile.Get(ctx, id)
}

func (r *benchmarkRepository) CreateRunWithSnapshots(ctx context.Context, input service.BenchmarkCreateRunInput) (*dbent.BenchmarkRun, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.createRunWithSnapshots(ctx, tx.Client(), input)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	run, err := r.createRunWithSnapshots(txCtx, tx.Client(), input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}

func (r *benchmarkRepository) GetRun(ctx context.Context, id int64) (*dbent.BenchmarkRun, error) {
	return clientFromContext(ctx, r.client).BenchmarkRun.Get(ctx, id)
}

func (r *benchmarkRepository) ListRuns(ctx context.Context, input service.BenchmarkRunListInput) ([]*dbent.BenchmarkRun, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkRun.Query()
	if input.SuiteID > 0 {
		query = query.Where(benchmarkrun.SuiteIDEQ(input.SuiteID))
	}
	if input.ProfileID > 0 {
		query = query.Where(benchmarkrun.ProfileIDEQ(input.ProfileID))
	}
	if len(input.Status) > 0 {
		query = query.Where(benchmarkrun.StatusIn(input.Status...))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := benchmarkLimitOffset(input.BenchmarkListInput)
	rows, err := query.
		Order(dbent.Desc(benchmarkrun.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) ListRunTargets(ctx context.Context, runID int64) ([]*dbent.BenchmarkRunTarget, error) {
	return clientFromContext(ctx, r.client).BenchmarkRunTarget.Query().
		Where(benchmarkruntarget.RunIDEQ(runID)).
		Order(dbent.Asc(benchmarkruntarget.FieldTargetOrder), dbent.Asc(benchmarkruntarget.FieldID)).
		All(ctx)
}

func (r *benchmarkRepository) ListRunTasks(ctx context.Context, runID int64) ([]*dbent.BenchmarkRunTask, error) {
	return clientFromContext(ctx, r.client).BenchmarkRunTask.Query().
		Where(benchmarkruntask.RunIDEQ(runID)).
		Order(dbent.Asc(benchmarkruntask.FieldTaskOrder), dbent.Asc(benchmarkruntask.FieldID)).
		All(ctx)
}

func (r *benchmarkRepository) ListRunResults(ctx context.Context, runID int64) ([]*dbent.BenchmarkResult, error) {
	return clientFromContext(ctx, r.client).BenchmarkResult.Query().
		Where(benchmarkresult.RunIDEQ(runID)).
		Order(dbent.Asc(benchmarkresult.FieldID)).
		All(ctx)
}

func (r *benchmarkRepository) ListRunScoreInputs(ctx context.Context, runID int64) ([]service.BenchmarkRunScoreInput, error) {
	rows, err := clientFromContext(ctx, r.client).BenchmarkResult.Query().
		Where(benchmarkresult.RunIDEQ(runID)).
		WithRunTarget().
		WithRunTask().
		All(ctx)
	if err != nil {
		return nil, err
	}

	type runScoreItem struct {
		target *dbent.BenchmarkRunTarget
		task   *dbent.BenchmarkRunTask
		result *dbent.BenchmarkResult
	}

	items := make([]runScoreItem, 0, len(rows))
	for _, result := range rows {
		target, err := result.Edges.RunTargetOrErr()
		if err != nil {
			return nil, err
		}
		task, err := result.Edges.RunTaskOrErr()
		if err != nil {
			return nil, err
		}
		items = append(items, runScoreItem{
			target: target,
			task:   task,
			result: result,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].target.TargetOrder != items[j].target.TargetOrder {
			return items[i].target.TargetOrder < items[j].target.TargetOrder
		}
		if items[i].task.TaskOrder != items[j].task.TaskOrder {
			return items[i].task.TaskOrder < items[j].task.TaskOrder
		}
		return items[i].result.ID < items[j].result.ID
	})

	scoreInputs := make([]service.BenchmarkRunScoreInput, 0, len(items))
	for _, item := range items {
		scoreInputs = append(scoreInputs, service.BenchmarkRunScoreInput{
			RunTarget: item.target,
			RunTask:   item.task,
			Result:    item.result,
		})
	}
	return scoreInputs, nil
}

func (r *benchmarkRepository) ListScoreSnapshots(ctx context.Context, runID int64) ([]*dbent.BenchmarkScoreSnapshot, error) {
	return clientFromContext(ctx, r.client).BenchmarkScoreSnapshot.Query().
		Where(benchmarkscoresnapshot.RunIDEQ(runID)).
		Order(
			dbent.Desc(benchmarkscoresnapshot.FieldOverallScore),
			dbent.Desc(benchmarkscoresnapshot.FieldCoverageRate),
			dbent.Asc(benchmarkscoresnapshot.FieldRunTargetID),
		).
		All(ctx)
}

func (r *benchmarkRepository) ClaimPendingResults(ctx context.Context, runID int64, limit int) ([]*dbent.BenchmarkResult, error) {
	if limit <= 0 {
		return []*dbent.BenchmarkResult{}, nil
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.claimPendingResults(ctx, tx.Client(), runID, limit)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	results, err := r.claimPendingResults(txCtx, tx.Client(), runID, limit)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *benchmarkRepository) RequeueClaimedResults(ctx context.Context, resultIDs []int64) error {
	if len(resultIDs) == 0 {
		return nil
	}
	err := clientFromContext(ctx, r.client).BenchmarkResult.Update().
		Where(
			benchmarkresult.IDIn(resultIDs...),
			benchmarkresult.StatusEQ(service.BenchmarkResultStatusRunning),
		).
		SetStatus(service.BenchmarkResultStatusPending).
		Exec(ctx)
	return err
}

func (r *benchmarkRepository) UpdateRunStatus(ctx context.Context, runID int64, status string, errorMessage *string) error {
	builder := clientFromContext(ctx, r.client).BenchmarkRun.UpdateOneID(runID).SetStatus(status)
	if errorMessage == nil {
		builder.ClearErrorMessage()
	} else {
		builder.SetErrorMessage(*errorMessage)
	}
	return builder.Exec(ctx)
}

func (r *benchmarkRepository) CountRunResultsByStatus(ctx context.Context, runID int64) (map[string]int, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(
		ctx,
		`SELECT status, COUNT(*) FROM benchmark_results WHERE run_id = $1 GROUP BY status`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (r *benchmarkRepository) GetRunResultContext(ctx context.Context, resultID int64) (*service.BenchmarkRunResultContext, error) {
	result, err := clientFromContext(ctx, r.client).BenchmarkResult.Query().
		Where(benchmarkresult.IDEQ(resultID)).
		WithRun().
		WithRunTarget().
		WithRunTask().
		Only(ctx)
	if err != nil {
		return nil, err
	}

	run, err := result.Edges.RunOrErr()
	if err != nil {
		return nil, err
	}
	target, err := result.Edges.RunTargetOrErr()
	if err != nil {
		return nil, err
	}
	task, err := result.Edges.RunTaskOrErr()
	if err != nil {
		return nil, err
	}

	return &service.BenchmarkRunResultContext{
		Result: result,
		Run:    run,
		Target: target,
		Task:   task,
	}, nil
}

func (r *benchmarkRepository) UpdateResult(ctx context.Context, id int64, input service.BenchmarkResultUpdateInput) error {
	builder := clientFromContext(ctx, r.client).BenchmarkResult.UpdateOneID(id)
	if input.Status != nil {
		builder.SetStatus(*input.Status)
	}
	if input.ClearRequestID {
		builder.ClearRequestID()
	} else if input.RequestID != nil {
		builder.SetRequestID(*input.RequestID)
	}
	if input.ClearScore {
		builder.ClearScore()
	} else if input.Score != nil {
		builder.SetScore(*input.Score)
	}
	if input.ClearMaxScore {
		builder.ClearMaxScore()
	} else if input.MaxScore != nil {
		builder.SetMaxScore(*input.MaxScore)
	}
	if input.ClearNormalizedScore {
		builder.ClearNormalizedScore()
	} else if input.NormalizedScore != nil {
		builder.SetNormalizedScore(*input.NormalizedScore)
	}
	if input.ClearEvaluatorType {
		builder.ClearEvaluatorType()
	} else if input.EvaluatorType != nil {
		builder.SetEvaluatorType(*input.EvaluatorType)
	}
	if input.ClearEvaluatorOutput {
		builder.SetEvaluatorOutput(map[string]any{})
	} else if input.EvaluatorOutput != nil {
		builder.SetEvaluatorOutput(input.EvaluatorOutput)
	}
	if input.ClearLatencyMS {
		builder.ClearLatencyMs()
	} else if input.LatencyMS != nil {
		builder.SetLatencyMs(*input.LatencyMS)
	}
	if input.PromptTokens != nil {
		builder.SetPromptTokens(*input.PromptTokens)
	}
	if input.CompletionTokens != nil {
		builder.SetCompletionTokens(*input.CompletionTokens)
	}
	if input.TotalTokens != nil {
		builder.SetTotalTokens(*input.TotalTokens)
	}
	if input.EstimatedCost != nil {
		builder.SetEstimatedCost(*input.EstimatedCost)
	}
	if input.RawResponse != nil {
		builder.SetRawResponse(input.RawResponse)
	}
	if input.ClearErrorCode {
		builder.ClearErrorCode()
	} else if input.ErrorCode != nil {
		builder.SetErrorCode(*input.ErrorCode)
	}
	if input.ClearErrorMessage {
		builder.ClearErrorMessage()
	} else if input.ErrorMessage != nil {
		builder.SetErrorMessage(*input.ErrorMessage)
	}
	if input.AttemptCount != nil {
		builder.SetAttemptCount(*input.AttemptCount)
	}
	if input.ClearStartedAt {
		builder.ClearStartedAt()
	} else if input.StartedAt != nil {
		builder.SetStartedAt(*input.StartedAt)
	}
	if input.ClearFinishedAt {
		builder.ClearFinishedAt()
	} else if input.FinishedAt != nil {
		builder.SetFinishedAt(*input.FinishedAt)
	}
	return builder.Exec(ctx)
}

func (r *benchmarkRepository) SaveScoreSnapshots(ctx context.Context, runID int64, snapshots []service.BenchmarkScoreSnapshotInput) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.saveScoreSnapshots(ctx, tx.Client(), runID, snapshots)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := r.saveScoreSnapshots(txCtx, tx.Client(), runID, snapshots); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *benchmarkRepository) PublishPublicSnapshot(ctx context.Context, input service.BenchmarkPublicSnapshotInput) error {
	builder := clientFromContext(ctx, r.client).BenchmarkPublicSnapshot.Create().
		SetRunID(input.RunID).
		SetSuiteID(input.SuiteID).
		SetProfileID(input.ProfileID).
		SetSnapshot(benchmarkMap(input.Snapshot)).
		SetNillablePublishedAt(input.PublishedAt)
	return builder.Exec(ctx)
}

func (r *benchmarkRepository) GetLatestPublicSnapshot(ctx context.Context) (*dbent.BenchmarkPublicSnapshot, error) {
	snapshot, err := clientFromContext(ctx, r.client).BenchmarkPublicSnapshot.Query().
		Order(dbent.Desc(benchmarkpublicsnapshot.FieldPublishedAt), dbent.Desc(benchmarkpublicsnapshot.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *benchmarkRepository) createRunWithSnapshots(ctx context.Context, client *dbent.Client, input service.BenchmarkCreateRunInput) (*dbent.BenchmarkRun, error) {
	plannedTargetCount := input.PlannedTargetCount
	if plannedTargetCount == 0 {
		plannedTargetCount = len(input.Targets)
	}
	plannedTaskCount := input.PlannedTaskCount
	if plannedTaskCount == 0 {
		plannedTaskCount = len(input.Tasks)
	}
	plannedResultCount := input.PlannedResultCount
	if plannedResultCount == 0 {
		plannedResultCount = len(input.Targets) * len(input.Tasks)
	}

	run, err := client.BenchmarkRun.Create().
		SetSuiteID(input.SuiteID).
		SetProfileID(input.ProfileID).
		SetStatus(benchmarkValueOrDefault(input.Status, service.BenchmarkRunStatusPending)).
		SetTriggerType(benchmarkValueOrDefault(input.TriggerType, "manual")).
		SetTaskScale(benchmarkrun.TaskScale(benchmarkValueOrDefault(input.TaskScale, service.BenchmarkTaskScaleMedium))).
		SetTaskTypes(benchmarkStringSlice(input.TaskTypes)).
		SetNillableSelectionSeed(input.SelectionSeed).
		SetPlannedTargetCount(plannedTargetCount).
		SetPlannedTaskCount(plannedTaskCount).
		SetPlannedResultCount(plannedResultCount).
		SetNillableStartedAt(input.StartedAt).
		SetNillableFinishedAt(input.FinishedAt).
		SetConfigSnapshot(benchmarkMap(input.ConfigSnapshot)).
		SetNillableErrorMessage(input.ErrorMessage).
		SetNillableCreatedBy(input.CreatedBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	runTargets := make([]*dbent.BenchmarkRunTarget, 0, len(input.Targets))
	for _, targetInput := range input.Targets {
		builder := client.BenchmarkRunTarget.Create().
			SetRunID(run.ID).
			SetTargetID(targetInput.TargetID).
			SetModelName(targetInput.ModelName).
			SetChannelID(targetInput.ChannelID).
			SetTargetOrder(targetInput.TargetOrder).
			SetConfigSnapshot(benchmarkMap(targetInput.ConfigSnapshot))
		if targetInput.DisplayNameSnapshot != "" {
			builder.SetDisplayNameSnapshot(targetInput.DisplayNameSnapshot)
		}
		if targetInput.ChannelNameSnapshot != "" {
			builder.SetChannelNameSnapshot(targetInput.ChannelNameSnapshot)
		}
		if targetInput.ProviderSnapshot != "" {
			builder.SetProviderSnapshot(targetInput.ProviderSnapshot)
		}
		runTarget, err := builder.Save(ctx)
		if err != nil {
			return nil, err
		}
		runTargets = append(runTargets, runTarget)
	}

	runTasks := make([]*dbent.BenchmarkRunTask, 0, len(input.Tasks))
	for _, taskInput := range input.Tasks {
		builder := client.BenchmarkRunTask.Create().
			SetRunID(run.ID).
			SetTaskID(taskInput.TaskID).
			SetTaskOrder(taskInput.TaskOrder).
			SetType(taskInput.Type).
			SetPromptSnapshot(taskInput.PromptSnapshot).
			SetVerifierTypeSnapshot(taskInput.VerifierTypeSnapshot).
			SetVerifierConfigSnapshot(benchmarkMap(taskInput.VerifierConfigSnapshot)).
			SetTaskSnapshot(benchmarkMap(taskInput.TaskSnapshot))
		if taskInput.Category != "" {
			builder.SetCategory(taskInput.Category)
		}
		if taskInput.Difficulty != "" {
			builder.SetDifficulty(taskInput.Difficulty)
		}
		if taskInput.WeightSnapshot > 0 {
			builder.SetWeightSnapshot(taskInput.WeightSnapshot)
		}
		runTask, err := builder.Save(ctx)
		if err != nil {
			return nil, err
		}
		runTasks = append(runTasks, runTask)
	}

	for _, runTask := range runTasks {
		for _, runTarget := range runTargets {
			if _, err := client.BenchmarkResult.Create().
				SetRunID(run.ID).
				SetRunTaskID(runTask.ID).
				SetRunTargetID(runTarget.ID).
				SetStatus(service.BenchmarkResultStatusPending).
				Save(ctx); err != nil {
				return nil, err
			}
		}
	}

	return run, nil
}

func (r *benchmarkRepository) saveScoreSnapshots(ctx context.Context, client *dbent.Client, runID int64, snapshots []service.BenchmarkScoreSnapshotInput) error {
	if len(snapshots) > 0 {
		uniqueRunTargetIDs := make(map[int64]struct{}, len(snapshots))
		for _, snapshot := range snapshots {
			uniqueRunTargetIDs[snapshot.RunTargetID] = struct{}{}
		}

		runTargetIDs := make([]int64, 0, len(uniqueRunTargetIDs))
		for runTargetID := range uniqueRunTargetIDs {
			runTargetIDs = append(runTargetIDs, runTargetID)
		}

		matchedCount, err := client.BenchmarkRunTarget.Query().
			Where(
				benchmarkruntarget.RunIDEQ(runID),
				benchmarkruntarget.IDIn(runTargetIDs...),
			).
			Count(ctx)
		if err != nil {
			return err
		}
		if matchedCount != len(uniqueRunTargetIDs) {
			return fmt.Errorf(
				"benchmark run %d has %d/%d owned score snapshot targets",
				runID,
				matchedCount,
				len(uniqueRunTargetIDs),
			)
		}
	}

	if _, err := client.BenchmarkScoreSnapshot.Delete().
		Where(benchmarkscoresnapshot.RunIDEQ(runID)).
		Exec(ctx); err != nil {
		return err
	}

	for _, snapshot := range snapshots {
		builder := client.BenchmarkScoreSnapshot.Create().
			SetRunID(runID).
			SetRunTargetID(snapshot.RunTargetID).
			SetOverallScore(snapshot.OverallScore).
			SetDimensionScores(benchmarkMap(snapshot.DimensionScores)).
			SetPlannedTasks(snapshot.PlannedTasks).
			SetScoredTasks(snapshot.ScoredTasks).
			SetInvalidTasks(snapshot.InvalidTasks).
			SetCoverageRate(snapshot.CoverageRate).
			SetConfidenceLevel(benchmarkscoresnapshot.ConfidenceLevel(benchmarkValueOrDefault(snapshot.ConfidenceLevel, service.BenchmarkConfidenceLow))).
			SetInsufficientSample(snapshot.InsufficientSample).
			SetSuccessRate(snapshot.SuccessRate).
			SetNillableLatencyP50Ms(snapshot.LatencyP50MS).
			SetNillableLatencyP95Ms(snapshot.LatencyP95MS).
			SetNillableAvgTotalTokens(snapshot.AvgTotalTokens).
			SetEstimatedCost(snapshot.EstimatedCost).
			SetInvalidReasonBreakdown(benchmarkMap(snapshot.InvalidReasonBreakdown)).
			SetRankingMetadata(benchmarkMap(snapshot.RankingMetadata))
		if _, err := builder.Save(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (r *benchmarkRepository) claimPendingResults(ctx context.Context, client *dbent.Client, runID int64, limit int) ([]*dbent.BenchmarkResult, error) {
	rows, err := client.QueryContext(
		ctx,
		`
WITH claimed AS (
	SELECT id
	FROM benchmark_results
	WHERE run_id = $1 AND status = $2
	ORDER BY id ASC
	LIMIT $3
	FOR UPDATE SKIP LOCKED
),
updated AS (
	UPDATE benchmark_results AS br
	SET status = $4,
	    attempt_count = br.attempt_count + 1,
	    updated_at = NOW()
	FROM claimed
	WHERE br.id = claimed.id
	RETURNING br.id
)
SELECT id FROM updated ORDER BY id ASC
`,
		runID,
		service.BenchmarkResultStatusPending,
		limit,
		service.BenchmarkResultStatusRunning,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claimedIDs := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		claimedIDs = append(claimedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(claimedIDs) == 0 {
		return []*dbent.BenchmarkResult{}, nil
	}

	return client.BenchmarkResult.Query().
		Where(benchmarkresult.IDIn(claimedIDs...)).
		Order(dbent.Asc(benchmarkresult.FieldID)).
		All(ctx)
}

func benchmarkLimitOffset(input service.BenchmarkListInput) (int, int) {
	page := input.Page
	if page <= 0 {
		page = defaultBenchmarkPage
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = defaultBenchmarkPageSize
	}
	if pageSize > maxBenchmarkPageSize {
		pageSize = maxBenchmarkPageSize
	}
	return pageSize, (page - 1) * pageSize
}

func benchmarkMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}

func benchmarkIntMap(input map[string]int) map[string]int {
	if input == nil {
		return map[string]int{}
	}
	return input
}

func benchmarkStringSlice(input []string) []string {
	if input == nil {
		return []string{}
	}
	return input
}

func benchmarkInt64Slice(input []int64) []int64 {
	if input == nil {
		return []int64{}
	}
	return input
}

func benchmarkValueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
