package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/ent"
)

type BenchmarkRuntime struct {
	Enabled               bool
	PublicEnabled         bool
	GlobalConcurrency     int
	DefaultTimeoutSeconds int
}

type benchmarkRuntimeProvider interface {
	GetBenchmarkRuntime(ctx context.Context) BenchmarkRuntime
}

// benchmarkChannelResolver fills the channel display name snapshot from the
// channel record so admins never type it by hand.
type benchmarkChannelResolver interface {
	GetByID(ctx context.Context, id int64) (*Channel, error)
}

type BenchmarkService struct {
	repo            BenchmarkRepository
	runtimeProvider benchmarkRuntimeProvider
	channelResolver benchmarkChannelResolver
	standardTaskMu  sync.Mutex
}

func NewBenchmarkService(repo BenchmarkRepository) *BenchmarkService {
	return &BenchmarkService{repo: repo}
}

func (s *BenchmarkService) SetBenchmarkRuntimeProvider(provider benchmarkRuntimeProvider) {
	s.runtimeProvider = provider
}

func (s *BenchmarkService) SetSettingService(settingService *SettingService) {
	s.SetBenchmarkRuntimeProvider(settingService)
}

func (s *BenchmarkService) SetChannelResolver(resolver benchmarkChannelResolver) {
	s.channelResolver = resolver
}

// BenchmarkCreateRunRequest is the input for both manual and scheduled runs.
// TargetIDs empty means "all enabled targets"; TaskCount 0 means "all enabled
// tasks", else the first N by sort order.
type BenchmarkCreateRunRequest struct {
	TargetIDs   []int64
	TaskCount   int
	TriggerType string
	ScheduleID  *int64
	CreatedBy   *int64
}

type BenchmarkRunPreview struct {
	TargetCount  int     `json:"target_count"`
	TaskCount    int     `json:"task_count"`
	ResultCount  int     `json:"result_count"`
	RankingBasis string  `json:"ranking_basis"`
	TargetIDs    []int64 `json:"target_ids"`
	TaskIDs      []int64 `json:"task_ids"`
}

type benchmarkRunSelection struct {
	targets []*ent.BenchmarkTarget
	tasks   []*ent.BenchmarkTask
}

func benchmarkRuntimeDefaults(enabled, publicEnabled bool) BenchmarkRuntime {
	return BenchmarkRuntime{
		Enabled:               enabled,
		PublicEnabled:         publicEnabled,
		GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
		DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
	}
}

func normalizeBenchmarkRuntime(runtime BenchmarkRuntime) BenchmarkRuntime {
	if runtime.GlobalConcurrency <= 0 {
		runtime.GlobalConcurrency = BenchmarkGlobalConcurrencyDefault
	}
	if runtime.DefaultTimeoutSeconds <= 0 {
		runtime.DefaultTimeoutSeconds = BenchmarkDefaultTimeoutSecondsDefault
	}
	return runtime
}

func (s *BenchmarkService) getBenchmarkRuntime(ctx context.Context) BenchmarkRuntime {
	if s == nil || s.runtimeProvider == nil {
		return benchmarkRuntimeDefaults(true, true)
	}
	return normalizeBenchmarkRuntime(s.runtimeProvider.GetBenchmarkRuntime(ctx))
}

func NormalizeBenchmarkListInput(input BenchmarkListInput) BenchmarkListInput {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	} else if input.PageSize > 100 {
		input.PageSize = 100
	}
	return input
}

func NormalizeBenchmarkTaskListInput(input BenchmarkTaskListInput) BenchmarkTaskListInput {
	input.BenchmarkListInput = NormalizeBenchmarkListInput(input.BenchmarkListInput)
	return input
}

func NormalizeBenchmarkRunListInput(input BenchmarkRunListInput) BenchmarkRunListInput {
	input.BenchmarkListInput = NormalizeBenchmarkListInput(input.BenchmarkListInput)
	return input
}

// ---- Targets ----

func (s *BenchmarkService) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if err := validateBenchmarkTargetInput(input); err != nil {
		return nil, err
	}
	input = s.fillTargetChannelName(ctx, input)
	return s.repo.CreateTarget(ctx, input)
}

func validateBenchmarkTargetInput(input BenchmarkTargetInput) error {
	if strings.TrimSpace(input.ModelName) == "" {
		return errors.New("model name is required")
	}
	if input.ChannelID <= 0 {
		return errors.New("channel id must be positive")
	}
	return nil
}

func (s *BenchmarkService) fillTargetChannelName(ctx context.Context, input BenchmarkTargetInput) BenchmarkTargetInput {
	if strings.TrimSpace(input.ChannelNameSnapshot) != "" || s.channelResolver == nil {
		return input
	}
	if channel, err := s.channelResolver.GetByID(ctx, input.ChannelID); err == nil && channel != nil && strings.TrimSpace(channel.Name) != "" {
		input.ChannelNameSnapshot = channel.Name
	}
	return input
}

func (s *BenchmarkService) ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	return s.repo.ListTargets(ctx, NormalizeBenchmarkListInput(input))
}

func (s *BenchmarkService) GetTarget(ctx context.Context, id int64) (*ent.BenchmarkTarget, error) {
	return s.repo.GetTarget(ctx, id)
}

func (s *BenchmarkService) UpdateTarget(ctx context.Context, id int64, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if err := validateBenchmarkTargetInput(input); err != nil {
		return nil, err
	}
	input = s.fillTargetChannelName(ctx, input)
	return s.repo.UpdateTarget(ctx, id, input)
}

func (s *BenchmarkService) DeleteTarget(ctx context.Context, id int64) error {
	return s.repo.DeleteTarget(ctx, id)
}

// ---- Tasks ----

func (s *BenchmarkService) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if err := validateBenchmarkTaskInput(input); err != nil {
		return nil, err
	}
	return s.repo.CreateTask(ctx, input)
}

func validateBenchmarkTaskInput(input BenchmarkTaskInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("task title is required")
	}
	if strings.TrimSpace(input.Type) == "" {
		return errors.New("task type is required")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return errors.New("task prompt is required")
	}
	if strings.TrimSpace(input.VerifierType) == "" {
		return errors.New("verifier type is required")
	}
	return nil
}

func (s *BenchmarkService) ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	return s.repo.ListTasks(ctx, NormalizeBenchmarkTaskListInput(input))
}

func (s *BenchmarkService) GetTask(ctx context.Context, id int64) (*ent.BenchmarkTask, error) {
	return s.repo.GetTask(ctx, id)
}

func (s *BenchmarkService) UpdateTask(ctx context.Context, id int64, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if err := validateBenchmarkTaskInput(input); err != nil {
		return nil, err
	}
	return s.repo.UpdateTask(ctx, id, input)
}

func (s *BenchmarkService) DeleteTask(ctx context.Context, id int64) error {
	return s.repo.DeleteTask(ctx, id)
}

// ---- Runs ----

func (s *BenchmarkService) ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	return s.repo.ListRuns(ctx, NormalizeBenchmarkRunListInput(input))
}

func (s *BenchmarkService) GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
	return s.repo.GetRun(ctx, id)
}

func (s *BenchmarkService) CancelRun(ctx context.Context, runID int64, reason string) error {
	return s.repo.CancelRun(ctx, runID, reason)
}

func (s *BenchmarkService) ListRunTargets(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error) {
	return s.repo.ListRunTargets(ctx, runID)
}

func (s *BenchmarkService) ListRunTasks(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error) {
	return s.repo.ListRunTasks(ctx, runID)
}

func (s *BenchmarkService) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	return s.repo.ListRunResults(ctx, runID)
}

func (s *BenchmarkService) ListTargetScores(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error) {
	return s.repo.ListTargetScores(ctx, runID)
}

// PreviewRun reports the matrix size for a manual run config without creating it.
func (s *BenchmarkService) PreviewRun(ctx context.Context, targetIDs []int64, taskCount int) (*BenchmarkRunPreview, error) {
	selection, err := s.resolveRunSelection(ctx, targetIDs, taskCount)
	if err != nil {
		return nil, err
	}
	return &BenchmarkRunPreview{
		TargetCount:  len(selection.targets),
		TaskCount:    len(selection.tasks),
		ResultCount:  len(selection.targets) * len(selection.tasks),
		RankingBasis: benchmarkRankingBasisValue,
		TargetIDs:    benchmarkTargetIDs(selection.targets),
		TaskIDs:      benchmarkTaskIDs(selection.tasks),
	}, nil
}

func (s *BenchmarkService) CreateRun(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	runtime := s.getBenchmarkRuntime(ctx)
	if !runtime.Enabled {
		return nil, infraBenchmarkDisabled()
	}

	selection, err := s.resolveRunSelection(ctx, input.TargetIDs, input.TaskCount)
	if err != nil {
		return nil, err
	}

	return s.createRunFromSelection(ctx, selection, input)
}

func (s *BenchmarkService) createRunFromSelection(ctx context.Context, selection *benchmarkRunSelection, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	triggerType := input.TriggerType
	if triggerType == "" {
		triggerType = BenchmarkTriggerManual
	}

	return s.repo.CreateRunWithSnapshots(ctx, BenchmarkCreateRunInput{
		Status:             BenchmarkRunStatusQueued,
		TriggerType:        triggerType,
		ScheduleID:         input.ScheduleID,
		TaskCount:          input.TaskCount,
		PlannedTargetCount: len(selection.targets),
		PlannedTaskCount:   len(selection.tasks),
		PlannedResultCount: len(selection.targets) * len(selection.tasks),
		CreatedBy:          input.CreatedBy,
		Targets:            benchmarkRunTargetInputs(selection.targets),
		Tasks:              benchmarkRunTaskInputs(selection.tasks),
	})
}

func (s *BenchmarkService) resolveRunSelection(ctx context.Context, targetIDs []int64, taskCount int) (*benchmarkRunSelection, error) {
	targets, err := s.resolveRunTargets(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	enabledTasks, err := s.repo.ListEnabledTasks(ctx)
	if err != nil {
		return nil, err
	}
	selectedTasks, err := selectBenchmarkTaskEntities(enabledTasks, taskCount, "no enabled benchmark tasks available")
	if err != nil {
		return nil, err
	}

	return &benchmarkRunSelection{targets: targets, tasks: selectedTasks}, nil
}

func (s *BenchmarkService) resolveRunTargets(ctx context.Context, targetIDs []int64) ([]*ent.BenchmarkTarget, error) {
	var targets []*ent.BenchmarkTarget
	var err error
	if len(targetIDs) > 0 {
		targets, err = s.repo.ListTargetsByIDs(ctx, targetIDs)
	} else {
		targets, err = s.repo.ListEnabledTargets(ctx)
	}
	if err != nil {
		return nil, err
	}
	targets = benchmarkEnabledTargets(targets)
	if len(targets) == 0 {
		return nil, errors.New("no enabled benchmark targets selected")
	}
	return targets, nil
}

func selectBenchmarkTaskEntities(enabledTasks []*ent.BenchmarkTask, taskCount int, emptyMessage string) ([]*ent.BenchmarkTask, error) {
	candidates := make([]BenchmarkTaskCandidate, 0, len(enabledTasks))
	for _, task := range enabledTasks {
		if task == nil {
			continue
		}
		candidates = append(candidates, BenchmarkTaskCandidate{
			ID:        task.ID,
			Type:      task.Type,
			SortOrder: task.SortOrder,
			Enabled:   task.Enabled,
		})
	}
	selectedCandidates := SelectBenchmarkTasks(candidates, taskCount)
	if len(selectedCandidates) == 0 {
		return nil, errors.New(emptyMessage)
	}

	tasksByID := make(map[int64]*ent.BenchmarkTask, len(enabledTasks))
	for _, task := range enabledTasks {
		if task == nil {
			continue
		}
		tasksByID[task.ID] = task
	}
	selectedTasks := make([]*ent.BenchmarkTask, 0, len(selectedCandidates))
	for _, candidate := range selectedCandidates {
		task := tasksByID[candidate.ID]
		if task == nil {
			return nil, errors.New("selected benchmark task not found")
		}
		selectedTasks = append(selectedTasks, task)
	}

	return selectedTasks, nil
}

func benchmarkEnabledTargets(targets []*ent.BenchmarkTarget) []*ent.BenchmarkTarget {
	out := make([]*ent.BenchmarkTarget, 0, len(targets))
	for _, target := range targets {
		if target != nil && target.Enabled {
			out = append(out, target)
		}
	}
	return out
}

func benchmarkRunTargetInputs(targets []*ent.BenchmarkTarget) []BenchmarkRunTargetInput {
	inputs := make([]BenchmarkRunTargetInput, 0, len(targets))
	for i, target := range targets {
		inputs = append(inputs, BenchmarkRunTargetInput{
			TargetID:            target.ID,
			ModelName:           target.ModelName,
			ChannelID:           target.ChannelID,
			DisplayNameSnapshot: stringFromPtr(target.DisplayName),
			ChannelNameSnapshot: stringFromPtr(target.ChannelNameSnapshot),
			TargetOrder:         i + 1,
		})
	}
	return inputs
}

func benchmarkRunTaskInputs(tasks []*ent.BenchmarkTask) []BenchmarkRunTaskInput {
	inputs := make([]BenchmarkRunTaskInput, 0, len(tasks))
	for i, task := range tasks {
		inputs = append(inputs, BenchmarkRunTaskInput{
			TaskID:                 task.ID,
			TaskOrder:              i + 1,
			Type:                   task.Type,
			Difficulty:             stringFromPtr(task.Difficulty),
			WeightSnapshot:         task.Weight,
			PromptSnapshot:         task.Prompt,
			VerifierTypeSnapshot:   task.VerifierType,
			VerifierConfigSnapshot: benchmarkCloneAnyMap(task.VerifierConfig),
			TaskSnapshot: map[string]any{
				"title":           task.Title,
				"type":            task.Type,
				"difficulty":      stringFromPtr(task.Difficulty),
				"prompt":          task.Prompt,
				"input_payload":   benchmarkCloneAnyMap(task.InputPayload),
				"expected_output": benchmarkCloneAnyMap(task.ExpectedOutput),
				"verifier_type":   task.VerifierType,
				"verifier_config": benchmarkCloneAnyMap(task.VerifierConfig),
				"weight":          task.Weight,
				"public_prompt":   task.PublicPrompt,
			},
		})
	}
	return inputs
}

func benchmarkTargetIDs(targets []*ent.BenchmarkTarget) []int64 {
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return ids
}

func benchmarkTaskIDs(tasks []*ent.BenchmarkTask) []int64 {
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func benchmarkCloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}

func benchmarkCloneInt64Slice(input []int64) []int64 {
	if input == nil {
		return nil
	}
	out := make([]int64, len(input))
	copy(out, input)
	return out
}

func benchmarkCloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func benchmarkCloneInt64Ptr(input *int64) *int64 {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}
