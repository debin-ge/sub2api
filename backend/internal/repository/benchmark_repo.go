package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkpublicsnapshot"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkresult"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkrun"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntarget"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkruntask"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkschedule"
	"github.com/Wei-Shaw/sub2api/ent/benchmarktarget"
	"github.com/Wei-Shaw/sub2api/ent/benchmarktargetscore"
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

// ---- Targets ----

func (r *benchmarkRepository) CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*dbent.BenchmarkTarget, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkTarget.Create().
		SetModelName(input.ModelName).
		SetChannelID(input.ChannelID).
		SetEnabled(input.Enabled).
		SetPublicVisible(input.PublicVisible).
		SetSortOrder(input.SortOrder)
	if input.DisplayName != "" {
		builder.SetDisplayName(input.DisplayName)
	}
	if input.ChannelNameSnapshot != "" {
		builder.SetChannelNameSnapshot(input.ChannelNameSnapshot)
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

func (r *benchmarkRepository) UpdateTarget(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*dbent.BenchmarkTarget, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkTarget.UpdateOneID(id).
		SetModelName(input.ModelName).
		SetChannelID(input.ChannelID).
		SetEnabled(input.Enabled).
		SetPublicVisible(input.PublicVisible).
		SetSortOrder(input.SortOrder)
	if input.DisplayName == "" {
		builder.ClearDisplayName()
	} else {
		builder.SetDisplayName(input.DisplayName)
	}
	if input.ChannelNameSnapshot == "" {
		builder.ClearChannelNameSnapshot()
	} else {
		builder.SetChannelNameSnapshot(input.ChannelNameSnapshot)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) DeleteTarget(ctx context.Context, id int64) error {
	return clientFromContext(ctx, r.client).BenchmarkTarget.DeleteOneID(id).Exec(ctx)
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

func (r *benchmarkRepository) ListEnabledTargets(ctx context.Context) ([]*dbent.BenchmarkTarget, error) {
	return clientFromContext(ctx, r.client).BenchmarkTarget.Query().
		Where(benchmarktarget.EnabledEQ(true)).
		Order(dbent.Asc(benchmarktarget.FieldSortOrder), dbent.Asc(benchmarktarget.FieldID)).
		All(ctx)
}

// ---- Tasks ----

func (r *benchmarkRepository) CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*dbent.BenchmarkTask, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkTask.Create().
		SetTitle(input.Title).
		SetType(input.Type).
		SetPrompt(input.Prompt).
		SetInputPayload(benchmarkMap(input.InputPayload)).
		SetExpectedOutput(benchmarkMap(input.ExpectedOutput)).
		SetVerifierType(input.VerifierType).
		SetVerifierConfig(benchmarkMap(input.VerifierConfig)).
		SetPublicPrompt(input.PublicPrompt).
		SetEnabled(input.Enabled).
		SetSortOrder(input.SortOrder)
	if input.Difficulty != "" {
		builder.SetDifficulty(input.Difficulty)
	}
	if input.Weight > 0 {
		builder.SetWeight(input.Weight)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) GetTask(ctx context.Context, id int64) (*dbent.BenchmarkTask, error) {
	return clientFromContext(ctx, r.client).BenchmarkTask.Get(ctx, id)
}

func (r *benchmarkRepository) UpdateTask(ctx context.Context, id int64, input service.BenchmarkTaskInput) (*dbent.BenchmarkTask, error) {
	weight := input.Weight
	if weight <= 0 {
		weight = 1
	}
	builder := clientFromContext(ctx, r.client).BenchmarkTask.UpdateOneID(id).
		SetTitle(input.Title).
		SetType(input.Type).
		SetPrompt(input.Prompt).
		SetInputPayload(benchmarkMap(input.InputPayload)).
		SetExpectedOutput(benchmarkMap(input.ExpectedOutput)).
		SetVerifierType(input.VerifierType).
		SetVerifierConfig(benchmarkMap(input.VerifierConfig)).
		SetWeight(weight).
		SetPublicPrompt(input.PublicPrompt).
		SetEnabled(input.Enabled).
		SetSortOrder(input.SortOrder)
	if input.Difficulty == "" {
		builder.ClearDifficulty()
	} else {
		builder.SetDifficulty(input.Difficulty)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) DeleteTask(ctx context.Context, id int64) error {
	return clientFromContext(ctx, r.client).BenchmarkTask.DeleteOneID(id).Exec(ctx)
}

func (r *benchmarkRepository) ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*dbent.BenchmarkTask, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkTask.Query()
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
		Order(dbent.Asc(benchmarktask.FieldSortOrder), dbent.Asc(benchmarktask.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) ListEnabledTasks(ctx context.Context) ([]*dbent.BenchmarkTask, error) {
	return clientFromContext(ctx, r.client).BenchmarkTask.Query().
		Where(benchmarktask.EnabledEQ(true)).
		Order(dbent.Asc(benchmarktask.FieldSortOrder), dbent.Asc(benchmarktask.FieldID)).
		All(ctx)
}

// ---- Schedules ----

func (r *benchmarkRepository) ListSchedules(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*dbent.BenchmarkSchedule, int, error) {
	client := clientFromContext(ctx, r.client)
	query := client.BenchmarkSchedule.Query()
	if input.Enabled != nil {
		query = query.Where(benchmarkschedule.EnabledEQ(*input.Enabled))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := benchmarkLimitOffset(input.BenchmarkListInput)
	rows, err := query.
		Order(dbent.Asc(benchmarkschedule.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *benchmarkRepository) CreateSchedule(ctx context.Context, input service.BenchmarkScheduleInput) (*dbent.BenchmarkSchedule, error) {
	return clientFromContext(ctx, r.client).BenchmarkSchedule.Create().
		SetName(input.Name).
		SetCronExpr(input.CronExpr).
		SetEnabled(input.Enabled).
		SetTargetIds(benchmarkInt64Slice(input.TargetIDs)).
		SetTaskCount(input.TaskCount).
		SetNillableNextRunAt(input.NextRunAt).
		Save(ctx)
}

func (r *benchmarkRepository) GetSchedule(ctx context.Context, id int64) (*dbent.BenchmarkSchedule, error) {
	return clientFromContext(ctx, r.client).BenchmarkSchedule.Get(ctx, id)
}

func (r *benchmarkRepository) UpdateSchedule(ctx context.Context, id int64, input service.BenchmarkScheduleInput) (*dbent.BenchmarkSchedule, error) {
	builder := clientFromContext(ctx, r.client).BenchmarkSchedule.UpdateOneID(id).
		SetName(input.Name).
		SetCronExpr(input.CronExpr).
		SetEnabled(input.Enabled).
		SetTargetIds(benchmarkInt64Slice(input.TargetIDs)).
		SetTaskCount(input.TaskCount)
	if input.NextRunAt == nil {
		builder.ClearNextRunAt()
	} else {
		builder.SetNextRunAt(*input.NextRunAt)
	}
	return builder.Save(ctx)
}

func (r *benchmarkRepository) DeleteSchedule(ctx context.Context, id int64) error {
	return clientFromContext(ctx, r.client).BenchmarkSchedule.DeleteOneID(id).Exec(ctx)
}

func (r *benchmarkRepository) ListDueSchedules(ctx context.Context, now time.Time) ([]*dbent.BenchmarkSchedule, error) {
	return clientFromContext(ctx, r.client).BenchmarkSchedule.Query().
		Where(
			benchmarkschedule.EnabledEQ(true),
			benchmarkschedule.NextRunAtLTE(now),
		).
		Order(dbent.Asc(benchmarkschedule.FieldNextRunAt), dbent.Asc(benchmarkschedule.FieldID)).
		All(ctx)
}

func (r *benchmarkRepository) UpdateScheduleAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	return clientFromContext(ctx, r.client).BenchmarkSchedule.UpdateOneID(id).
		SetLastRunAt(lastRunAt).
		SetNextRunAt(nextRunAt).
		Exec(ctx)
}

// ---- Runs ----

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

func (r *benchmarkRepository) CancelRun(ctx context.Context, runID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "canceled"
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.cancelRun(ctx, tx.Client(), runID, reason)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := r.cancelRun(txCtx, tx.Client(), runID, reason); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *benchmarkRepository) ClaimRunnableRuns(ctx context.Context, limit int) ([]*dbent.BenchmarkRun, error) {
	limit = benchmarkRunnableRunLimit(limit)
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.claimRunnableRuns(ctx, tx.Client(), limit)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	runs, err := r.claimRunnableRuns(txCtx, tx.Client(), limit)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *benchmarkRepository) MarkRunStarted(ctx context.Context, runID int64) error {
	result, err := clientFromContext(ctx, r.client).ExecContext(
		ctx,
		`
UPDATE benchmark_runs
SET status = $2,
    started_at = COALESCE(started_at, NOW()),
    error_message = NULL,
    updated_at = NOW()
WHERE id = $1 AND status IN ($3, $4)
`,
		runID,
		service.BenchmarkRunStatusRunning,
		service.BenchmarkRunStatusQueued,
		service.BenchmarkRunStatusRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("benchmark run %d cannot be marked started from current status", runID)
	}
	return nil
}

func (r *benchmarkRepository) MarkRunFinished(ctx context.Context, runID int64, status string, errorMessage *string) error {
	if !benchmarkIsTerminalRunStatus(status) {
		return fmt.Errorf("benchmark run finish status must be terminal: %s", status)
	}
	var message any
	if errorMessage != nil {
		message = *errorMessage
	}
	result, err := clientFromContext(ctx, r.client).ExecContext(
		ctx,
		`
UPDATE benchmark_runs
SET status = $2,
    finished_at = NOW(),
    error_message = $3,
    updated_at = NOW()
WHERE id = $1 AND status = $4
`,
		runID,
		status,
		message,
		service.BenchmarkRunStatusRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("benchmark run %d cannot be marked finished from current status", runID)
	}
	return nil
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
		WithRunTarget().
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
		items = append(items, runScoreItem{target: target, task: task, result: result})
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
	return clientFromContext(ctx, r.client).BenchmarkResult.Update().
		Where(
			benchmarkresult.IDIn(resultIDs...),
			benchmarkresult.StatusEQ(service.BenchmarkResultStatusRunning),
		).
		SetStatus(service.BenchmarkResultStatusPending).
		Exec(ctx)
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
	return counts, rows.Err()
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

// ---- Scores & trends ----

func (r *benchmarkRepository) SaveTargetScores(ctx context.Context, runID int64, scores []service.BenchmarkTargetScoreInput) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.saveTargetScores(ctx, tx.Client(), runID, scores)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := r.saveTargetScores(txCtx, tx.Client(), runID, scores); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *benchmarkRepository) ListTargetScores(ctx context.Context, runID int64) ([]*dbent.BenchmarkTargetScore, error) {
	return clientFromContext(ctx, r.client).BenchmarkTargetScore.Query().
		Where(benchmarktargetscore.RunIDEQ(runID)).
		Order(
			dbent.Desc(benchmarktargetscore.FieldOverallScore),
			dbent.Asc(benchmarktargetscore.FieldRunTargetID),
		).
		All(ctx)
}

func (r *benchmarkRepository) ListTrendScores(ctx context.Context, since time.Time, limit int) ([]*dbent.BenchmarkTargetScore, error) {
	if limit <= 0 {
		limit = 2000
	}
	return clientFromContext(ctx, r.client).BenchmarkTargetScore.Query().
		Where(benchmarktargetscore.FinishedAtGTE(since)).
		Order(
			dbent.Asc(benchmarktargetscore.FieldModelName),
			dbent.Asc(benchmarktargetscore.FieldChannelID),
			dbent.Asc(benchmarktargetscore.FieldFinishedAt),
		).
		Limit(limit).
		All(ctx)
}

// ---- Public snapshots ----

func (r *benchmarkRepository) PublishPublicSnapshot(ctx context.Context, input service.BenchmarkPublicSnapshotInput) error {
	return clientFromContext(ctx, r.client).BenchmarkPublicSnapshot.Create().
		SetRunID(input.RunID).
		SetSnapshot(benchmarkMap(input.Snapshot)).
		SetNillablePublishedAt(input.PublishedAt).
		Exec(ctx)
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

// ---- internal helpers ----

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
		SetStatus(benchmarkValueOrDefault(input.Status, service.BenchmarkRunStatusQueued)).
		SetTriggerType(benchmarkValueOrDefault(input.TriggerType, service.BenchmarkTriggerManual)).
		SetNillableScheduleID(input.ScheduleID).
		SetTaskCount(input.TaskCount).
		SetPlannedTargetCount(plannedTargetCount).
		SetPlannedTaskCount(plannedTaskCount).
		SetPlannedResultCount(plannedResultCount).
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
			SetTargetOrder(targetInput.TargetOrder)
		if targetInput.DisplayNameSnapshot != "" {
			builder.SetDisplayNameSnapshot(targetInput.DisplayNameSnapshot)
		}
		if targetInput.ChannelNameSnapshot != "" {
			builder.SetChannelNameSnapshot(targetInput.ChannelNameSnapshot)
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

func (r *benchmarkRepository) saveTargetScores(ctx context.Context, client *dbent.Client, runID int64, scores []service.BenchmarkTargetScoreInput) error {
	if len(scores) > 0 {
		uniqueRunTargetIDs := make(map[int64]struct{}, len(scores))
		for _, score := range scores {
			uniqueRunTargetIDs[score.RunTargetID] = struct{}{}
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
			return fmt.Errorf("benchmark run %d has %d/%d owned score targets", runID, matchedCount, len(uniqueRunTargetIDs))
		}
	}

	if _, err := client.BenchmarkTargetScore.Delete().
		Where(benchmarktargetscore.RunIDEQ(runID)).
		Exec(ctx); err != nil {
		return err
	}

	for _, score := range scores {
		builder := client.BenchmarkTargetScore.Create().
			SetRunID(runID).
			SetRunTargetID(score.RunTargetID).
			SetModelName(score.ModelName).
			SetChannelID(score.ChannelID).
			SetOverallScore(score.OverallScore).
			SetPassedCount(score.PassedCount).
			SetTotalCount(score.TotalCount).
			SetDimensionScores(benchmarkMap(score.DimensionScores)).
			SetNillableAvgLatencyMs(score.AvgLatencyMS).
			SetNillableAvgTotalTokens(score.AvgTotalTokens).
			SetTotalCost(score.TotalCost).
			SetInvalidReasonBreakdown(benchmarkMap(score.InvalidReasonBreakdown)).
			SetFinishedAt(score.FinishedAt)
		if _, err := builder.Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *benchmarkRepository) cancelRun(ctx context.Context, client *dbent.Client, runID int64, reason string) error {
	result, err := client.ExecContext(
		ctx,
		`
UPDATE benchmark_runs
SET status = $2,
    finished_at = NOW(),
    error_message = $3,
    updated_at = NOW()
WHERE id = $1 AND status IN ($4, $5)
`,
		runID,
		service.BenchmarkRunStatusCanceled,
		reason,
		service.BenchmarkRunStatusQueued,
		service.BenchmarkRunStatusRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("benchmark run %d cannot be canceled from current status", runID)
	}

	return client.BenchmarkResult.Update().
		Where(
			benchmarkresult.RunIDEQ(runID),
			benchmarkresult.StatusIn(
				service.BenchmarkResultStatusPending,
				service.BenchmarkResultStatusRunning,
			),
		).
		SetStatus(service.BenchmarkResultStatusSkipped).
		SetErrorCode(service.BenchmarkResultStatusSkipped).
		SetErrorMessage(reason).
		SetFinishedAt(time.Now()).
		Exec(ctx)
}

func (r *benchmarkRepository) claimRunnableRuns(ctx context.Context, client *dbent.Client, limit int) ([]*dbent.BenchmarkRun, error) {
	rows, err := client.QueryContext(
		ctx,
		`
WITH claimable AS (
	SELECT id
	FROM benchmark_runs
	WHERE status = $2
	ORDER BY id ASC
	LIMIT $3
	FOR UPDATE SKIP LOCKED
),
claimed AS (
	UPDATE benchmark_runs AS br
	SET status = $1,
	    started_at = COALESCE(started_at, NOW()),
	    error_message = NULL,
	    updated_at = NOW()
	FROM claimable
	WHERE br.id = claimable.id
	RETURNING br.id
),
recoverable AS (
	SELECT id
	FROM benchmark_runs
	WHERE status = $1
	  AND id NOT IN (SELECT id FROM claimed)
	ORDER BY id ASC
	LIMIT GREATEST($3 - (SELECT COUNT(*) FROM claimed), 0)
	FOR UPDATE SKIP LOCKED
)
SELECT id FROM recoverable
UNION
SELECT id FROM claimed
ORDER BY id ASC
`,
		service.BenchmarkRunStatusRunning,
		service.BenchmarkRunStatusQueued,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runIDs := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		runIDs = append(runIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(runIDs) == 0 {
		return []*dbent.BenchmarkRun{}, nil
	}

	return client.BenchmarkRun.Query().
		Where(benchmarkrun.IDIn(runIDs...)).
		Order(dbent.Asc(benchmarkrun.FieldID)).
		All(ctx)
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

func benchmarkRunnableRunLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 20 {
		return 20
	}
	return limit
}

func benchmarkIsTerminalRunStatus(status string) bool {
	switch status {
	case service.BenchmarkRunStatusCompleted,
		service.BenchmarkRunStatusFailed,
		service.BenchmarkRunStatusCanceled:
		return true
	default:
		return false
	}
}

func benchmarkMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
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
