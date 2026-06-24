package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type BenchmarkRuntime struct {
	Enabled               bool
	PublicEnabled         bool
	GlobalConcurrency     int
	DefaultTimeoutSeconds int
	ConfidenceThresholds  BenchmarkConfidenceThresholds
}

type benchmarkRuntimeProvider interface {
	GetBenchmarkRuntime(ctx context.Context) BenchmarkRuntime
}

type BenchmarkService struct {
	repo            BenchmarkRepository
	runtimeProvider benchmarkRuntimeProvider
}

type BenchmarkProfilePreviewInput struct {
	TargetIDs        []int64
	TaskTypes        []string
	TaskScale        string
	TaskCountLimit   *int
	PerTypeLimit     map[string]int
	DifficultyFilter []string
	TagFilter        []string
	SelectionSeed    *int64
}

type BenchmarkProfilePreview struct {
	TargetCount       int
	TaskCount         int
	ResultCount       int
	TaskTypes         []string
	TaskScale         string
	RankingBasis      string
	EstimatedCost     float64
	SelectedTaskIDs   []int64
	SelectedTargetIDs []int64
}

type BenchmarkCreateRunRequest struct {
	ProfileID   int64
	TriggerType string
	CreatedBy   *int64
	Override    BenchmarkProfilePreviewInput
}

type benchmarkProfileSelection struct {
	profile            *ent.BenchmarkProfile
	targets            []*ent.BenchmarkTarget
	tasks              []*ent.BenchmarkTask
	targetIDs          []int64
	taskIDs            []int64
	taskTypes          []string
	taskScale          string
	taskCountLimit     *int
	perTypeLimit       map[string]int
	difficultyFilter   []string
	tagFilter          []string
	selectionSeed      *int64
	selectionSeedValue int64
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

func benchmarkRuntimeDefaults(enabled, publicEnabled bool) BenchmarkRuntime {
	return BenchmarkRuntime{
		Enabled:               enabled,
		PublicEnabled:         publicEnabled,
		GlobalConcurrency:     BenchmarkGlobalConcurrencyDefault,
		DefaultTimeoutSeconds: BenchmarkDefaultTimeoutSecondsDefault,
		ConfidenceThresholds: BenchmarkConfidenceThresholds{
			MediumCoverage: BenchmarkLowConfidenceThresholdDefault,
			HighCoverage:   BenchmarkHighConfidenceThresholdDefault,
		},
	}
}

func normalizeBenchmarkRuntime(runtime BenchmarkRuntime) BenchmarkRuntime {
	if runtime.GlobalConcurrency <= 0 {
		runtime.GlobalConcurrency = BenchmarkGlobalConcurrencyDefault
	}
	if runtime.DefaultTimeoutSeconds <= 0 {
		runtime.DefaultTimeoutSeconds = BenchmarkDefaultTimeoutSecondsDefault
	}
	if runtime.ConfidenceThresholds.MediumCoverage <= 0 {
		runtime.ConfidenceThresholds.MediumCoverage = BenchmarkLowConfidenceThresholdDefault
	}
	if runtime.ConfidenceThresholds.HighCoverage <= 0 {
		runtime.ConfidenceThresholds.HighCoverage = BenchmarkHighConfidenceThresholdDefault
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

func (s *BenchmarkService) CreateSuite(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	return s.repo.CreateSuite(ctx, input)
}

func (s *BenchmarkService) ListSuites(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	return s.repo.ListSuites(ctx, NormalizeBenchmarkListInput(input))
}

func (s *BenchmarkService) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if strings.TrimSpace(input.ModelName) == "" {
		return nil, errors.New("model name is required")
	}
	if input.ChannelID <= 0 {
		return nil, errors.New("channel id must be positive")
	}
	return s.repo.CreateTarget(ctx, input)
}

func (s *BenchmarkService) ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	return s.repo.ListTargets(ctx, NormalizeBenchmarkListInput(input))
}

func (s *BenchmarkService) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if strings.TrimSpace(input.Type) == "" {
		return nil, errors.New("task type is required")
	}
	if !isValidBenchmarkTaskScale(input.MinScale) {
		return nil, errors.New("unsupported task scale")
	}
	return s.repo.CreateTask(ctx, input)
}

func (s *BenchmarkService) ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	return s.repo.ListTasks(ctx, NormalizeBenchmarkTaskListInput(input))
}

func (s *BenchmarkService) CreateProfile(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	if len(input.TargetIDs) == 0 {
		return nil, errors.New("at least one target is required")
	}
	for _, targetID := range input.TargetIDs {
		if targetID <= 0 {
			return nil, errors.New("target id must be positive")
		}
	}
	if len(input.TaskTypes) == 0 {
		return nil, errors.New("at least one task type is required")
	}
	for _, taskType := range input.TaskTypes {
		if strings.TrimSpace(taskType) == "" {
			return nil, errors.New("task type is required")
		}
	}
	if !isValidBenchmarkTaskScale(input.TaskScale) {
		return nil, errors.New("unsupported task scale")
	}
	return s.repo.CreateProfile(ctx, input)
}

func (s *BenchmarkService) GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
	return s.repo.GetProfile(ctx, id)
}

func (s *BenchmarkService) ListProfiles(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkProfile, int, error) {
	return s.repo.ListProfiles(ctx, NormalizeBenchmarkListInput(input))
}

func (s *BenchmarkService) ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	return s.repo.ListRuns(ctx, NormalizeBenchmarkRunListInput(input))
}

func (s *BenchmarkService) GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
	return s.repo.GetRun(ctx, id)
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

func (s *BenchmarkService) ListScoreSnapshots(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
	return s.repo.ListScoreSnapshots(ctx, runID)
}

func (s *BenchmarkService) PreviewProfile(ctx context.Context, profileID int64, override BenchmarkProfilePreviewInput) (*BenchmarkProfilePreview, error) {
	selection, err := s.selectBenchmarkProfile(ctx, profileID, override)
	if err != nil {
		return nil, err
	}
	return &BenchmarkProfilePreview{
		TargetCount:       len(selection.targets),
		TaskCount:         len(selection.tasks),
		ResultCount:       len(selection.targets) * len(selection.tasks),
		TaskTypes:         benchmarkCloneStringSlice(selection.taskTypes),
		TaskScale:         selection.taskScale,
		RankingBasis:      "ability_score_only",
		EstimatedCost:     0,
		SelectedTaskIDs:   benchmarkCloneInt64Slice(selection.taskIDs),
		SelectedTargetIDs: benchmarkCloneInt64Slice(selection.targetIDs),
	}, nil
}

func (s *BenchmarkService) CreateRun(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	runtime := s.getBenchmarkRuntime(ctx)
	if !runtime.Enabled {
		return nil, infraerrors.Forbidden("BENCHMARK_DISABLED", "benchmark is disabled")
	}

	profile, err := s.repo.GetProfile(ctx, input.ProfileID)
	if err != nil {
		return nil, err
	}
	if !profile.Enabled {
		return nil, errors.New("benchmark profile is disabled")
	}

	selection, err := s.selectBenchmarkProfileFromProfile(ctx, profile, input.Override)
	if err != nil {
		return nil, err
	}
	if len(selection.tasks) == 0 {
		return nil, errors.New("no benchmark tasks selected")
	}

	triggerType := input.TriggerType
	if triggerType == "" {
		triggerType = "manual"
	}

	return s.repo.CreateRunWithSnapshots(ctx, BenchmarkCreateRunInput{
		SuiteID:            selection.profile.SuiteID,
		ProfileID:          selection.profile.ID,
		Status:             BenchmarkRunStatusQueued,
		TriggerType:        triggerType,
		TaskScale:          selection.taskScale,
		TaskTypes:          benchmarkCloneStringSlice(selection.taskTypes),
		SelectionSeed:      benchmarkCloneInt64Ptr(selection.selectionSeed),
		PlannedTargetCount: len(selection.targets),
		PlannedTaskCount:   len(selection.tasks),
		PlannedResultCount: len(selection.targets) * len(selection.tasks),
		ConfigSnapshot:     benchmarkRunConfigSnapshot(selection),
		CreatedBy:          benchmarkCloneInt64Ptr(input.CreatedBy),
		Targets:            benchmarkRunTargetInputs(selection.targets),
		Tasks:              benchmarkRunTaskInputs(selection.tasks),
	})
}

func isValidBenchmarkTaskScale(scale string) bool {
	switch scale {
	case "", BenchmarkTaskScaleSmall, BenchmarkTaskScaleMedium, BenchmarkTaskScaleFull, BenchmarkTaskScaleCustom:
		return true
	default:
		return false
	}
}

func (s *BenchmarkService) selectBenchmarkProfile(ctx context.Context, profileID int64, override BenchmarkProfilePreviewInput) (*benchmarkProfileSelection, error) {
	profile, err := s.repo.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return s.selectBenchmarkProfileFromProfile(ctx, profile, override)
}

func (s *BenchmarkService) selectBenchmarkProfileFromProfile(ctx context.Context, profile *ent.BenchmarkProfile, override BenchmarkProfilePreviewInput) (*benchmarkProfileSelection, error) {
	targetIDs := benchmarkCloneInt64Slice(profile.TargetIds)
	if override.TargetIDs != nil {
		targetIDs = benchmarkCloneInt64Slice(override.TargetIDs)
	}
	if len(targetIDs) == 0 {
		return nil, errors.New("at least one target is required")
	}
	for _, targetID := range targetIDs {
		if targetID <= 0 {
			return nil, errors.New("target id must be positive")
		}
	}

	taskTypes := benchmarkCloneStringSlice(profile.TaskTypes)
	if override.TaskTypes != nil {
		taskTypes = benchmarkCloneStringSlice(override.TaskTypes)
	}
	if len(taskTypes) == 0 {
		return nil, errors.New("at least one task type is required")
	}
	for _, taskType := range taskTypes {
		if strings.TrimSpace(taskType) == "" {
			return nil, errors.New("task type is required")
		}
	}

	taskScale := string(profile.TaskScale)
	if override.TaskScale != "" {
		taskScale = override.TaskScale
	}
	if taskScale == "" {
		taskScale = BenchmarkTaskScaleMedium
	}
	if !isValidBenchmarkTaskScale(taskScale) {
		return nil, errors.New("unsupported task scale")
	}

	taskCountLimit := profile.TaskCountLimit
	if override.TaskCountLimit != nil {
		taskCountLimit = override.TaskCountLimit
	}
	perTypeLimit := benchmarkCloneIntMap(profile.PerTypeLimit)
	if override.PerTypeLimit != nil {
		perTypeLimit = benchmarkCloneIntMap(override.PerTypeLimit)
	}
	difficultyFilter := benchmarkCloneStringSlice(profile.DifficultyFilter)
	if override.DifficultyFilter != nil {
		difficultyFilter = benchmarkCloneStringSlice(override.DifficultyFilter)
	}
	tagFilter := benchmarkCloneStringSlice(profile.TagFilter)
	if override.TagFilter != nil {
		tagFilter = benchmarkCloneStringSlice(override.TagFilter)
	}
	selectionSeed := profile.SelectionSeed
	if override.SelectionSeed != nil {
		selectionSeed = override.SelectionSeed
	}
	var selectionSeedValue int64
	if selectionSeed != nil {
		selectionSeedValue = *selectionSeed
	}

	targets, err := s.repo.ListTargetsByIDs(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	enabledTasks, err := s.repo.ListEnabledTasksForSuite(ctx, profile.SuiteID)
	if err != nil {
		return nil, err
	}
	candidates := make([]BenchmarkTaskCandidate, 0, len(enabledTasks))
	for _, task := range enabledTasks {
		candidates = append(candidates, benchmarkTaskCandidateFromEnt(task))
	}

	taskLimit := 0
	if taskCountLimit != nil {
		taskLimit = *taskCountLimit
	}
	selectedCandidates, err := SelectBenchmarkTasks(candidates, BenchmarkSelectionConfig{
		TaskTypes:        taskTypes,
		TaskScale:        taskScale,
		TaskCountLimit:   taskLimit,
		PerTypeLimit:     perTypeLimit,
		DifficultyFilter: difficultyFilter,
		TagFilter:        tagFilter,
		SelectionSeed:    selectionSeedValue,
	})
	if err != nil {
		return nil, err
	}

	tasksByID := make(map[int64]*ent.BenchmarkTask, len(enabledTasks))
	for _, task := range enabledTasks {
		tasksByID[task.ID] = task
	}
	selectedTasks := make([]*ent.BenchmarkTask, 0, len(selectedCandidates))
	selectedTaskIDs := make([]int64, 0, len(selectedCandidates))
	for _, candidate := range selectedCandidates {
		task := tasksByID[candidate.ID]
		if task == nil {
			return nil, errors.New("selected benchmark task not found")
		}
		selectedTasks = append(selectedTasks, task)
		selectedTaskIDs = append(selectedTaskIDs, candidate.ID)
	}

	return &benchmarkProfileSelection{
		profile:            profile,
		targets:            targets,
		tasks:              selectedTasks,
		targetIDs:          benchmarkTargetIDs(targets),
		taskIDs:            selectedTaskIDs,
		taskTypes:          taskTypes,
		taskScale:          taskScale,
		taskCountLimit:     benchmarkCloneIntPtr(taskCountLimit),
		perTypeLimit:       perTypeLimit,
		difficultyFilter:   difficultyFilter,
		tagFilter:          tagFilter,
		selectionSeed:      benchmarkCloneInt64Ptr(selectionSeed),
		selectionSeedValue: selectionSeedValue,
	}, nil
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
			ProviderSnapshot:    stringFromPtr(target.ProviderSnapshot),
			TargetOrder:         i + 1,
			ConfigSnapshot: map[string]any{
				"supported_task_types": benchmarkCloneStringSlice(target.SupportedTaskTypes),
				"max_concurrency":      target.MaxConcurrency,
				"enabled":              target.Enabled,
				"public_visible":       target.PublicVisible,
			},
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
			Category:               stringFromPtr(task.Category),
			Difficulty:             stringFromPtr(task.Difficulty),
			WeightSnapshot:         task.Weight,
			PromptSnapshot:         task.Prompt,
			VerifierTypeSnapshot:   task.VerifierType,
			VerifierConfigSnapshot: benchmarkCloneAnyMap(task.VerifierConfig),
			TaskSnapshot: map[string]any{
				"title":           task.Title,
				"type":            task.Type,
				"category":        stringFromPtr(task.Category),
				"difficulty":      stringFromPtr(task.Difficulty),
				"tags":            benchmarkCloneStringSlice(task.Tags),
				"prompt":          task.Prompt,
				"input_payload":   benchmarkCloneAnyMap(task.InputPayload),
				"expected_output": benchmarkCloneAnyMap(task.ExpectedOutput),
				"verifier_type":   task.VerifierType,
				"verifier_config": benchmarkCloneAnyMap(task.VerifierConfig),
				"weight":          task.Weight,
				"min_scale":       string(task.MinScale),
				"public_prompt":   task.PublicPrompt,
			},
		})
	}
	return inputs
}

func benchmarkRunConfigSnapshot(selection *benchmarkProfileSelection) map[string]any {
	var taskCountLimit any
	if selection.taskCountLimit != nil {
		taskCountLimit = *selection.taskCountLimit
	}
	var selectionSeed any
	if selection.selectionSeed != nil {
		selectionSeed = *selection.selectionSeed
	}
	return map[string]any{
		"profile_id":        selection.profile.ID,
		"task_scale":        selection.taskScale,
		"task_types":        benchmarkCloneStringSlice(selection.taskTypes),
		"selection_seed":    selectionSeed,
		"task_count_limit":  taskCountLimit,
		"per_type_limit":    benchmarkCloneIntMap(selection.perTypeLimit),
		"difficulty_filter": benchmarkCloneStringSlice(selection.difficultyFilter),
		"tag_filter":        benchmarkCloneStringSlice(selection.tagFilter),
		"sampling_strategy": selection.profile.SamplingStrategy,
		"runtime_config":    benchmarkCloneAnyMap(selection.profile.RuntimeConfig),
		"scoring_config":    benchmarkCloneAnyMap(selection.profile.ScoringConfig),
		"ranking_basis":     "ability_score_only",
	}
}

func benchmarkTaskCandidateFromEnt(task *ent.BenchmarkTask) BenchmarkTaskCandidate {
	return BenchmarkTaskCandidate{
		ID:         task.ID,
		Type:       task.Type,
		Category:   stringFromPtr(task.Category),
		Difficulty: stringFromPtr(task.Difficulty),
		Tags:       benchmarkCloneStringSlice(task.Tags),
		Weight:     task.Weight,
		MinScale:   string(task.MinScale),
		Enabled:    task.Enabled,
	}
}

func benchmarkTargetIDs(targets []*ent.BenchmarkTarget) []int64 {
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
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

func benchmarkCloneIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	out := make(map[string]int, len(input))
	for key, value := range input {
		out[key] = value
	}
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

func benchmarkCloneIntPtr(input *int) *int {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}

func benchmarkCloneInt64Ptr(input *int64) *int64 {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}
