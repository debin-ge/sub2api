package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type BenchmarkStandardTaskApplyResult struct {
	CreatedCount  int                  `json:"created_count"`
	ExistingCount int                  `json:"existing_count"`
	EnabledCount  int                  `json:"enabled_count"`
	Tasks         []*ent.BenchmarkTask `json:"tasks"`
}

type BenchmarkStandardRunRequest struct {
	TargetIDs          []int64 `json:"target_ids"`
	TaskCount          int     `json:"task_count"`
	ProcessImmediately bool    `json:"process_immediately"`
	CreatedBy          *int64  `json:"created_by"`
}

func DefaultBenchmarkStandardTasks() []BenchmarkTaskInput {
	return []BenchmarkTaskInput{
		{
			Title:          "Standard Reasoning - Arithmetic",
			Type:           "reasoning",
			Difficulty:     "easy",
			Prompt:         "Return only the final number. Calculate 17 + 23 + 18.",
			VerifierType:   "normalized_match",
			VerifierConfig: map[string]any{"expected": "58"},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      100,
		},
		{
			Title:          "Standard Reasoning - Multiple Choice",
			Type:           "reasoning",
			Difficulty:     "easy",
			Prompt:         "Return only the option letter. Which option is the largest number? A) 19 B) 42 C) 31",
			VerifierType:   "normalized_match",
			VerifierConfig: map[string]any{"expected": "B"},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      110,
		},
		{
			Title:          "Standard Structured Output - JSON Keys",
			Type:           "reasoning",
			Difficulty:     "medium",
			Prompt:         `Answer as a JSON object with keys "answer", "explanation", and "confidence". Question: If all zargs are blens and all blens are crins, are all zargs crins?`,
			VerifierType:   "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"answer", "explanation", "confidence"}},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      120,
		},
		{
			Title:          "Standard Coding - Function Envelope",
			Type:           "coding",
			Difficulty:     "medium",
			Prompt:         `Return a JSON object with keys "language", "code", and "notes". The code should define a function named add that returns the sum of two numbers.`,
			VerifierType:   "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"language", "code", "notes"}},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      130,
		},
		{
			Title:          "Standard Writing - Concise Summary",
			Type:           "writing",
			Difficulty:     "easy",
			Prompt:         `Return a JSON object with keys "summary", "tone", and "constraints_met". Summarize this in one sentence: Model radar compares configured models using a fixed task set and records latency, tokens, and cost separately.`,
			VerifierType:   "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"summary", "tone", "constraints_met"}},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      140,
		},
		{
			Title:          "Standard Instruction Following - Label",
			Type:           "reasoning",
			Difficulty:     "easy",
			Prompt:         `Return only the uppercase label SAFE. Do not include any other words or punctuation.`,
			VerifierType:   "normalized_match",
			VerifierConfig: map[string]any{"expected": "SAFE"},
			Weight:         1,
			PublicPrompt:   true,
			Enabled:        true,
			SortOrder:      150,
		},
	}
}

func BenchmarkStandardTaskTitles() []string {
	tasks := DefaultBenchmarkStandardTasks()
	titles := make([]string, 0, len(tasks))
	for _, task := range tasks {
		titles = append(titles, task.Title)
	}
	return titles
}

func (s *BenchmarkService) EnsureStandardTasks(ctx context.Context) (*BenchmarkStandardTaskApplyResult, error) {
	s.standardTaskMu.Lock()
	defer s.standardTaskMu.Unlock()

	standardTasks := DefaultBenchmarkStandardTasks()
	titles := BenchmarkStandardTaskTitles()
	existing, err := s.repo.ListTasksByTitles(ctx, titles)
	if err != nil {
		return nil, err
	}

	existingByTitle := make(map[string]*ent.BenchmarkTask, len(existing))
	for _, task := range existing {
		if task != nil {
			existingByTitle[task.Title] = task
		}
	}

	createdCount := 0
	existingCount := 0
	orderedTasks := make([]*ent.BenchmarkTask, 0, len(standardTasks))
	for _, input := range standardTasks {
		if task := existingByTitle[input.Title]; task != nil {
			existingCount++
			orderedTasks = append(orderedTasks, task)
			continue
		}
		task, err := s.CreateTask(ctx, input)
		if err != nil {
			return nil, err
		}
		createdCount++
		orderedTasks = append(orderedTasks, task)
	}

	return &BenchmarkStandardTaskApplyResult{
		CreatedCount:  createdCount,
		ExistingCount: existingCount,
		EnabledCount:  benchmarkEnabledTaskCount(orderedTasks),
		Tasks:         orderedTasks,
	}, nil
}

func (s *BenchmarkService) CreateStandardRun(ctx context.Context, input BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
	runtime := s.getBenchmarkRuntime(ctx)
	if !runtime.Enabled {
		return nil, infraBenchmarkDisabled()
	}
	if err := validateBenchmarkRunTaskCount(input.TaskCount); err != nil {
		return nil, err
	}

	if _, err := s.EnsureStandardTasks(ctx); err != nil {
		return nil, err
	}

	selection, err := s.resolveStandardRunSelection(ctx, input.TargetIDs, input.TaskCount)
	if err != nil {
		return nil, err
	}

	return s.createRunFromSelection(ctx, selection, BenchmarkCreateRunRequest{
		TargetIDs:   benchmarkCloneInt64Slice(input.TargetIDs),
		TaskCount:   input.TaskCount,
		TriggerType: BenchmarkTriggerManual,
		CreatedBy:   benchmarkCloneInt64Ptr(input.CreatedBy),
	})
}

func (s *BenchmarkService) resolveStandardRunSelection(ctx context.Context, targetIDs []int64, taskCount int) (*benchmarkRunSelection, error) {
	targets, err := s.resolveRunTargets(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	enabledTasks, err := s.repo.ListEnabledTasksByTitles(ctx, BenchmarkStandardTaskTitles())
	if err != nil {
		return nil, err
	}
	selectedTasks, err := selectBenchmarkTaskEntities(enabledTasks, taskCount, "no enabled standard benchmark tasks available")
	if err != nil {
		return nil, err
	}

	return &benchmarkRunSelection{targets: targets, tasks: selectedTasks}, nil
}

func benchmarkEnabledTaskCount(tasks []*ent.BenchmarkTask) int {
	count := 0
	for _, task := range tasks {
		if task != nil && task.Enabled {
			count++
		}
	}
	return count
}

func infraBenchmarkDisabled() error {
	return infraerrors.Forbidden("BENCHMARK_DISABLED", "benchmark is disabled")
}
