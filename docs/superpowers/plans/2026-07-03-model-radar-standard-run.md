# Model Radar Standard Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a one-click standard Model Radar workflow that seeds a built-in standard task set and runs it against enabled benchmark targets by default.

**Architecture:** Keep the existing benchmark entities and manual admin pages. Add backend standard-task orchestration in the service layer, expose two admin endpoints, then make the frontend use those endpoints as the primary path while preserving advanced controls.

**Tech Stack:** Go service/repository/handler with Ent and Gin, Vue 3 Composition API, TypeScript API wrappers, Vitest frontend tests, Go unit tests.

---

## File Structure

- Create `backend/internal/service/benchmark_standard_tasks.go`
  - Owns standard task definitions, standard task title helpers, idempotent task application, and standard run creation.
- Modify `backend/internal/service/benchmark_repository.go`
  - Add repository interface methods needed for title-based standard task lookup.
- Modify `backend/internal/repository/benchmark_repo.go`
  - Add `ListTasksByTitles` and `ListEnabledTasksByTitles` using Ent predicates.
- Modify `backend/internal/service/benchmark_service.go`
  - Reuse existing run creation code by adding a private `createRunFromSelection` helper, so normal and standard runs share snapshot behavior.
- Create `backend/internal/service/benchmark_standard_tasks_test.go`
  - Unit tests for standard task definitions, idempotent apply behavior, standard task filtering, and standard run creation.
- Modify `backend/internal/handler/admin/benchmark_handler.go`
  - Extend the benchmark admin service interface and add handlers for standard task apply and standard run creation.
- Modify `backend/internal/server/routes/benchmark.go`
  - Register `POST /admin/benchmark/tasks/standard/apply` and `POST /admin/benchmark/runs/standard`.
- Modify backend tests:
  - `backend/internal/handler/admin/benchmark_handler_test.go`
  - `backend/internal/server/routes/benchmark_test.go`
- Modify `frontend/src/types/benchmark.ts`
  - Add standard task apply response and standard run request types.
- Modify `frontend/src/api/admin/benchmark.ts`
  - Add `applyStandardTasks` and `createStandardRun`.
- Modify frontend tests:
  - `frontend/src/api/__tests__/benchmark.spec.ts`
  - `frontend/src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts`
- Modify `frontend/src/views/admin/benchmark/BenchmarkRunsView.vue`
  - Replace the primary manual create form with a standard run action and advanced options.
- Modify `frontend/src/views/admin/benchmark/BenchmarkTasksView.vue`
  - Add standard task set apply action and empty-state guidance.
- Modify `frontend/src/i18n/locales/zh.ts` and `frontend/src/i18n/locales/en.ts`
  - Add new labels and remove visible stale suite/profile wording for the current workflow.

---

### Task 1: Backend Standard Task Service Tests

**Files:**
- Create: `backend/internal/service/benchmark_standard_tasks_test.go`
- Implemented in a later step: `backend/internal/service/benchmark_standard_tasks.go`

- [ ] **Step 1: Write failing tests for standard task definitions**

Create `backend/internal/service/benchmark_standard_tasks_test.go` with:

```go
package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
)

type benchmarkStandardRepoStub struct {
	BenchmarkRepository
	tasksByTitle        map[string]*ent.BenchmarkTask
	enabledTargets      []*ent.BenchmarkTarget
	createdTasks        []BenchmarkTaskInput
	createRunInput      *BenchmarkCreateRunInput
	listByTitlesCalls   [][]string
	listEnabledTitleArg []string
}

func newBenchmarkStandardRepoStub() *benchmarkStandardRepoStub {
	return &benchmarkStandardRepoStub{tasksByTitle: map[string]*ent.BenchmarkTask{}}
}

func (s *benchmarkStandardRepoStub) ListTargetsByIDs(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error) {
	out := make([]*ent.BenchmarkTarget, 0, len(ids))
	byID := map[int64]*ent.BenchmarkTarget{}
	for _, target := range s.enabledTargets {
		byID[target.ID] = target
	}
	for _, id := range ids {
		if target := byID[id]; target != nil {
			out = append(out, target)
		}
	}
	return out, nil
}
func (s *benchmarkStandardRepoStub) ListEnabledTargets(ctx context.Context) ([]*ent.BenchmarkTarget, error) {
	return s.enabledTargets, nil
}

func (s *benchmarkStandardRepoStub) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	id := int64(len(s.tasksByTitle) + 1)
	task := &ent.BenchmarkTask{
		ID:             id,
		Title:          input.Title,
		Type:           input.Type,
		Prompt:         input.Prompt,
		VerifierType:   input.VerifierType,
		VerifierConfig: input.VerifierConfig,
		Weight:         input.Weight,
		PublicPrompt:   input.PublicPrompt,
		Enabled:        input.Enabled,
		SortOrder:      input.SortOrder,
	}
	if input.Difficulty != "" {
		task.Difficulty = &input.Difficulty
	}
	s.createdTasks = append(s.createdTasks, input)
	s.tasksByTitle[input.Title] = task
	return task, nil
}

func (s *benchmarkStandardRepoStub) ListTasksByTitles(ctx context.Context, titles []string) ([]*ent.BenchmarkTask, error) {
	s.listByTitlesCalls = append(s.listByTitlesCalls, append([]string(nil), titles...))
	out := make([]*ent.BenchmarkTask, 0, len(titles))
	for _, title := range titles {
		if task := s.tasksByTitle[title]; task != nil {
			out = append(out, task)
		}
	}
	return out, nil
}
func (s *benchmarkStandardRepoStub) ListEnabledTasksByTitles(ctx context.Context, titles []string) ([]*ent.BenchmarkTask, error) {
	s.listEnabledTitleArg = append([]string(nil), titles...)
	out := make([]*ent.BenchmarkTask, 0, len(titles))
	for _, title := range titles {
		if task := s.tasksByTitle[title]; task != nil && task.Enabled {
			out = append(out, task)
		}
	}
	return out, nil
}

func (s *benchmarkStandardRepoStub) CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
	s.createRunInput = &input
	return &ent.BenchmarkRun{
		ID:                 99,
		Status:             input.Status,
		TriggerType:        input.TriggerType,
		TaskCount:          input.TaskCount,
		PlannedTargetCount: input.PlannedTargetCount,
		PlannedTaskCount:   input.PlannedTaskCount,
		PlannedResultCount: input.PlannedResultCount,
	}, nil
}
```

Add these tests:

```go
func TestBenchmarkStandardTaskDefinitionsAreStableAndSupported(t *testing.T) {
	t.Parallel()

	tasks := DefaultBenchmarkStandardTasks()
	if len(tasks) != 6 {
		t.Fatalf("len(DefaultBenchmarkStandardTasks()) = %d, want 6", len(tasks))
	}

	seen := map[string]bool{}
	for _, task := range tasks {
		if task.Title == "" || task.Type == "" || task.Prompt == "" {
			t.Fatalf("standard task has empty required field: %#v", task)
		}
		if seen[task.Title] {
			t.Fatalf("duplicate standard task title %q", task.Title)
		}
		seen[task.Title] = true
		switch task.VerifierType {
		case "normalized_match":
			if task.VerifierConfig["expected"] == "" {
				t.Fatalf("normalized task %q missing expected", task.Title)
			}
		case "json_object":
			if len(anyStringSlice(task.VerifierConfig["required_keys"])) == 0 {
				t.Fatalf("json task %q missing required_keys", task.Title)
			}
		default:
			t.Fatalf("unsupported verifier type %q", task.VerifierType)
		}
		if task.Weight != 1 || !task.PublicPrompt || !task.Enabled {
			t.Fatalf("standard task has wrong defaults: %#v", task)
		}
	}
}
```

- [ ] **Step 2: Run the definition test and verify it fails**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkStandardTaskDefinitionsAreStableAndSupported -count=1
```

Expected: FAIL with `undefined: DefaultBenchmarkStandardTasks`.

- [ ] **Step 3: Implement only standard task definitions**

Create `backend/internal/service/benchmark_standard_tasks.go` with:

```go
package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/ent"
)

const benchmarkStandardRunTrigger = BenchmarkTriggerManual

func DefaultBenchmarkStandardTasks() []BenchmarkTaskInput {
	return []BenchmarkTaskInput{
		{
			Title: "Standard Reasoning - Arithmetic",
			Type: "reasoning", Difficulty: "easy",
			Prompt: "Return only the final number. Calculate 17 + 23 + 18.",
			VerifierType: "normalized_match",
			VerifierConfig: map[string]any{"expected": "58"},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 100,
		},
		{
			Title: "Standard Reasoning - Multiple Choice",
			Type: "reasoning", Difficulty: "easy",
			Prompt: "Return only the option letter. Which option is the largest number? A) 19 B) 42 C) 31",
			VerifierType: "normalized_match",
			VerifierConfig: map[string]any{"expected": "B"},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 110,
		},
		{
			Title: "Standard Structured Output - JSON Keys",
			Type: "reasoning", Difficulty: "medium",
			Prompt: `Answer as a JSON object with keys "answer", "explanation", and "confidence". Question: If all zargs are blens and all blens are crins, are all zargs crins?`,
			VerifierType: "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"answer", "explanation", "confidence"}},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 120,
		},
		{
			Title: "Standard Coding - Function Envelope",
			Type: "coding", Difficulty: "medium",
			Prompt: `Return a JSON object with keys "language", "code", and "notes". The code should define a function named add that returns the sum of two numbers.`,
			VerifierType: "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"language", "code", "notes"}},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 130,
		},
		{
			Title: "Standard Writing - Concise Summary",
			Type: "writing", Difficulty: "easy",
			Prompt: `Return a JSON object with keys "summary", "tone", and "constraints_met". Summarize this in one sentence: Model radar compares configured models using a fixed task set and records latency, tokens, and cost separately.`,
			VerifierType: "json_object",
			VerifierConfig: map[string]any{"required_keys": []any{"summary", "tone", "constraints_met"}},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 140,
		},
		{
			Title: "Standard Instruction Following - Label",
			Type: "reasoning", Difficulty: "easy",
			Prompt: `Return only the uppercase label SAFE.`,
			VerifierType: "normalized_match",
			VerifierConfig: map[string]any{"expected": "SAFE"},
			Weight: 1, PublicPrompt: true, Enabled: true, SortOrder: 150,
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

type BenchmarkStandardTaskApplyResult struct {
	CreatedCount  int                  `json:"created_count"`
	ExistingCount int                  `json:"existing_count"`
	EnabledCount  int                  `json:"enabled_count"`
	Tasks         []*ent.BenchmarkTask `json:"tasks"`
}

type BenchmarkStandardRunRequest struct {
	TargetIDs []int64
	TaskCount int
	ProcessImmediately bool
	CreatedBy *int64
}

func (s *BenchmarkService) EnsureStandardTasks(ctx context.Context) (*BenchmarkStandardTaskApplyResult, error) {
	return nil, nil
}
```

- [ ] **Step 4: Run the definition test and verify it passes**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkStandardTaskDefinitionsAreStableAndSupported -count=1
```

Expected: PASS.

---

### Task 2: Repository Support for Title-Based Task Lookup

**Files:**
- Modify: `backend/internal/service/benchmark_repository.go`
- Modify: `backend/internal/repository/benchmark_repo.go`
- Test: existing backend compile tests

- [ ] **Step 1: Extend repository interface**

Modify `BenchmarkRepository` in `backend/internal/service/benchmark_repository.go` to add:

```go
	ListTasksByTitles(ctx context.Context, titles []string) ([]*ent.BenchmarkTask, error)
	ListEnabledTasksByTitles(ctx context.Context, titles []string) ([]*ent.BenchmarkTask, error)
```

Place these after `ListEnabledTasks`.

- [ ] **Step 2: Run service tests to see stub compile failures**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkStandardTaskDefinitionsAreStableAndSupported -count=1
```

Expected: FAIL until all test stubs and repository implementations satisfy the expanded interface.

- [ ] **Step 3: Implement repository methods**

In `backend/internal/repository/benchmark_repo.go`, add after `ListEnabledTasks`:

```go
func (r *benchmarkRepository) ListTasksByTitles(ctx context.Context, titles []string) ([]*dbent.BenchmarkTask, error) {
	if len(titles) == 0 {
		return []*dbent.BenchmarkTask{}, nil
	}
	return r.listTasksByTitles(ctx, titles, false)
}

func (r *benchmarkRepository) ListEnabledTasksByTitles(ctx context.Context, titles []string) ([]*dbent.BenchmarkTask, error) {
	if len(titles) == 0 {
		return []*dbent.BenchmarkTask{}, nil
	}
	return r.listTasksByTitles(ctx, titles, true)
}

func (r *benchmarkRepository) listTasksByTitles(ctx context.Context, titles []string, enabledOnly bool) ([]*dbent.BenchmarkTask, error) {
	orderedTitles := make([]string, 0, len(titles))
	seen := make(map[string]struct{}, len(titles))
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		if _, ok := seen[title]; ok {
			continue
		}
		seen[title] = struct{}{}
		orderedTitles = append(orderedTitles, title)
	}
	if len(orderedTitles) == 0 {
		return []*dbent.BenchmarkTask{}, nil
	}

	query := clientFromContext(ctx, r.client).BenchmarkTask.Query().
		Where(benchmarktask.TitleIn(orderedTitles...))
	if enabledOnly {
		query = query.Where(benchmarktask.EnabledEQ(true))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	byTitle := make(map[string]*dbent.BenchmarkTask, len(rows))
	for _, task := range rows {
		byTitle[task.Title] = task
	}
	ordered := make([]*dbent.BenchmarkTask, 0, len(rows))
	for _, title := range orderedTitles {
		if task := byTitle[title]; task != nil {
			ordered = append(ordered, task)
		}
	}
	return ordered, nil
}
```

`strings` is already imported in this file; keep the existing import.

- [ ] **Step 4: Run the service test again**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkStandardTaskDefinitionsAreStableAndSupported -count=1
```

Expected: PASS.

---

### Task 3: Idempotent Standard Task Application

**Files:**
- Modify: `backend/internal/service/benchmark_standard_tasks_test.go`
- Modify: `backend/internal/service/benchmark_standard_tasks.go`

- [ ] **Step 1: Add failing idempotence test**

Append to `backend/internal/service/benchmark_standard_tasks_test.go`:

```go
func TestBenchmarkServiceEnsureStandardTasksCreatesMissingTasksAndIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	svc := NewBenchmarkService(repo)

	first, err := svc.EnsureStandardTasks(context.Background())
	if err != nil {
		t.Fatalf("EnsureStandardTasks first error = %v", err)
	}
	if first.CreatedCount != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("first.CreatedCount = %d", first.CreatedCount)
	}
	if first.ExistingCount != 0 || first.EnabledCount != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("first result = %#v", first)
	}

	second, err := svc.EnsureStandardTasks(context.Background())
	if err != nil {
		t.Fatalf("EnsureStandardTasks second error = %v", err)
	}
	if second.CreatedCount != 0 {
		t.Fatalf("second.CreatedCount = %d, want 0", second.CreatedCount)
	}
	if second.ExistingCount != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("second.ExistingCount = %d", second.ExistingCount)
	}
	if len(repo.createdTasks) != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("createdTasks length = %d", len(repo.createdTasks))
	}
}
```

- [ ] **Step 2: Run the new test and verify it fails**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkServiceEnsureStandardTasksCreatesMissingTasksAndIsIdempotent -count=1
```

Expected: FAIL because `EnsureStandardTasks` returns nil result.

- [ ] **Step 3: Implement `EnsureStandardTasks`**

Replace the temporary implementation in `backend/internal/service/benchmark_standard_tasks.go`:

```go
func (s *BenchmarkService) EnsureStandardTasks(ctx context.Context) (*BenchmarkStandardTaskApplyResult, error) {
	definitions := DefaultBenchmarkStandardTasks()
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

	result := &BenchmarkStandardTaskApplyResult{
		ExistingCount: len(existingByTitle),
		Tasks: make([]*ent.BenchmarkTask, 0, len(definitions)),
	}
	for _, definition := range definitions {
		if task := existingByTitle[definition.Title]; task != nil {
			if task.Enabled {
				result.EnabledCount++
			}
			result.Tasks = append(result.Tasks, task)
			continue
		}
		created, err := s.CreateTask(ctx, definition)
		if err != nil {
			return nil, err
		}
		result.CreatedCount++
		if created.Enabled {
			result.EnabledCount++
		}
		result.Tasks = append(result.Tasks, created)
	}
	return result, nil
}
```

- [ ] **Step 4: Run standard service tests**

Run:

```bash
cd backend
go test ./internal/service -run 'TestBenchmark(StandardTaskDefinitions|ServiceEnsureStandardTasks)' -count=1
```

Expected: PASS.

---

### Task 4: Standard Run Creation in Service Layer

**Files:**
- Modify: `backend/internal/service/benchmark_standard_tasks_test.go`
- Modify: `backend/internal/service/benchmark_service.go`
- Modify: `backend/internal/service/benchmark_standard_tasks.go`

- [ ] **Step 1: Add failing standard run tests**

Append to `backend/internal/service/benchmark_standard_tasks_test.go`:

```go
func TestBenchmarkServiceCreateStandardRunEnsuresTasksAndUsesOnlyStandardTasks(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{
		{ID: 7, ModelName: "gpt-5.2", ChannelID: 3, Enabled: true, SortOrder: 1},
	}
	svc := NewBenchmarkService(repo)

	run, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{})
	if err != nil {
		t.Fatalf("CreateStandardRun error = %v", err)
	}
	if run.ID != 99 {
		t.Fatalf("run.ID = %d", run.ID)
	}
	if repo.createRunInput == nil {
		t.Fatal("CreateRunWithSnapshots was not called")
	}
	if repo.createRunInput.TriggerType != BenchmarkTriggerManual {
		t.Fatalf("TriggerType = %q", repo.createRunInput.TriggerType)
	}
	if repo.createRunInput.PlannedTargetCount != 1 {
		t.Fatalf("PlannedTargetCount = %d", repo.createRunInput.PlannedTargetCount)
	}
	if repo.createRunInput.PlannedTaskCount != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("PlannedTaskCount = %d", repo.createRunInput.PlannedTaskCount)
	}
	if !reflect.DeepEqual(repo.listEnabledTitleArg, BenchmarkStandardTaskTitles()) {
		t.Fatalf("enabled titles = %#v", repo.listEnabledTitleArg)
	}
}

func TestBenchmarkServiceCreateStandardRunReturnsNoStandardTasksWhenAllDisabled(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{{ID: 7, ModelName: "gpt-5.2", ChannelID: 3, Enabled: true}}
	for _, definition := range DefaultBenchmarkStandardTasks() {
		task := &ent.BenchmarkTask{ID: int64(len(repo.tasksByTitle) + 1), Title: definition.Title, Type: definition.Type, Enabled: false, SortOrder: definition.SortOrder}
		repo.tasksByTitle[definition.Title] = task
	}
	svc := NewBenchmarkService(repo)

	_, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{})
	if err == nil || err.Error() != "no enabled standard benchmark tasks available" {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run standard run tests and verify they fail**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmarkServiceCreateStandardRun -count=1
```

Expected: FAIL with `svc.CreateStandardRun undefined`.

- [ ] **Step 3: Extract shared run snapshot helper**

In `backend/internal/service/benchmark_service.go`, replace the body of `CreateRun` after selection resolution with:

```go
	triggerType := input.TriggerType
	if triggerType == "" {
		triggerType = BenchmarkTriggerManual
	}

	return s.createRunFromSelection(ctx, selection, BenchmarkCreateRunInput{
		Status:      BenchmarkRunStatusQueued,
		TriggerType: triggerType,
		ScheduleID:  input.ScheduleID,
		TaskCount:   input.TaskCount,
		CreatedBy:   input.CreatedBy,
	})
```

Add below `CreateRun`:

```go
func (s *BenchmarkService) createRunFromSelection(ctx context.Context, selection *benchmarkRunSelection, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
	input.Status = BenchmarkRunStatusQueued
	input.PlannedTargetCount = len(selection.targets)
	input.PlannedTaskCount = len(selection.tasks)
	input.PlannedResultCount = len(selection.targets) * len(selection.tasks)
	input.Targets = benchmarkRunTargetInputs(selection.targets)
	input.Tasks = benchmarkRunTaskInputs(selection.tasks)
	return s.repo.CreateRunWithSnapshots(ctx, input)
}
```

- [ ] **Step 4: Implement `CreateStandardRun`**

Add to `backend/internal/service/benchmark_standard_tasks.go`:

```go
func (s *BenchmarkService) CreateStandardRun(ctx context.Context, input BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
	runtime := s.getBenchmarkRuntime(ctx)
	if !runtime.Enabled {
		return nil, infraerrors.Forbidden("BENCHMARK_DISABLED", "benchmark is disabled")
	}
	if _, err := s.EnsureStandardTasks(ctx); err != nil {
		return nil, err
	}

	targets, err := s.resolveStandardRunTargets(ctx, input.TargetIDs)
	if err != nil {
		return nil, err
	}
	tasks, err := s.resolveStandardRunTasks(ctx, input.TaskCount)
	if err != nil {
		return nil, err
	}

	return s.createRunFromSelection(ctx, &benchmarkRunSelection{targets: targets, tasks: tasks}, BenchmarkCreateRunInput{
		Status:      BenchmarkRunStatusQueued,
		TriggerType: benchmarkStandardRunTrigger,
		TaskCount:   input.TaskCount,
		CreatedBy:   input.CreatedBy,
	})
}

func (s *BenchmarkService) resolveStandardRunTargets(ctx context.Context, targetIDs []int64) ([]*ent.BenchmarkTarget, error) {
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

func (s *BenchmarkService) resolveStandardRunTasks(ctx context.Context, taskCount int) ([]*ent.BenchmarkTask, error) {
	enabledTasks, err := s.repo.ListEnabledTasksByTitles(ctx, BenchmarkStandardTaskTitles())
	if err != nil {
		return nil, err
	}
	candidates := make([]BenchmarkTaskCandidate, 0, len(enabledTasks))
	for _, task := range enabledTasks {
		candidates = append(candidates, BenchmarkTaskCandidate{ID: task.ID, Type: task.Type, SortOrder: task.SortOrder, Enabled: task.Enabled})
	}
	selectedCandidates := SelectBenchmarkTasks(candidates, taskCount)
	if len(selectedCandidates) == 0 {
		return nil, errors.New("no enabled standard benchmark tasks available")
	}
	tasksByID := make(map[int64]*ent.BenchmarkTask, len(enabledTasks))
	for _, task := range enabledTasks {
		tasksByID[task.ID] = task
	}
	selectedTasks := make([]*ent.BenchmarkTask, 0, len(selectedCandidates))
	for _, candidate := range selectedCandidates {
		if task := tasksByID[candidate.ID]; task != nil {
			selectedTasks = append(selectedTasks, task)
		}
	}
	return selectedTasks, nil
}
```

Update imports in `benchmark_standard_tasks.go` to include:

```go
import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)
```

- [ ] **Step 5: Run service standard tests**

Run:

```bash
cd backend
go test ./internal/service -run TestBenchmark -count=1
```

Expected: PASS for the standard task tests and existing benchmark service tests.

---

### Task 5: Backend Handler and Routes

**Files:**
- Modify: `backend/internal/handler/admin/benchmark_handler.go`
- Modify: `backend/internal/server/routes/benchmark.go`
- Modify: `backend/internal/handler/admin/benchmark_handler_test.go`
- Modify: `backend/internal/server/routes/benchmark_test.go`

- [ ] **Step 1: Add failing handler tests**

In `backend/internal/handler/admin/benchmark_handler_test.go`, extend `benchmarkAdminServiceStub` with:

```go
	ensureStandardTasksFn func(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error)
	createStandardRunFn func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error)
```

Add methods:

```go
func (s *benchmarkAdminServiceStub) EnsureStandardTasks(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error) {
	return s.ensureStandardTasksFn(ctx)
}

func (s *benchmarkAdminServiceStub) CreateStandardRun(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
	return s.createStandardRunFn(ctx, input)
}
```

Register test routes in `newBenchmarkTestRouter`:

```go
	router.POST("/api/v1/admin/benchmark/tasks/standard/apply", handler.ApplyStandardTasks)
	router.POST("/api/v1/admin/benchmark/runs/standard", handler.CreateStandardRun)
```

Add tests:

```go
func TestBenchmarkHandlerApplyStandardTasks(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		ensureStandardTasksFn: func(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error) {
			return &service.BenchmarkStandardTaskApplyResult{
				CreatedCount: 2,
				ExistingCount: 4,
				EnabledCount: 6,
				Tasks: []*ent.BenchmarkTask{{ID: 1, Title: "Standard Reasoning - Arithmetic"}},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks/standard/apply", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"created_count":2`)
	require.Contains(t, rec.Body.String(), `"existing_count":4`)
	require.Contains(t, rec.Body.String(), `"enabled_count":6`)
}

func TestBenchmarkHandlerCreateStandardRunDefaultsToImmediateProcessing(t *testing.T) {
	processed := make(chan int64, 1)
	svc := &benchmarkAdminServiceStub{
		createStandardRunFn: func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
			require.True(t, input.ProcessImmediately)
			require.Empty(t, input.TargetIDs)
			require.Zero(t, input.TaskCount)
			return &ent.BenchmarkRun{ID: 41, Status: service.BenchmarkRunStatusQueued, TriggerType: service.BenchmarkTriggerManual}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			return 0, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}, processor: processor})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/standard", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case runID := <-processed:
		require.Equal(t, int64(41), runID)
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}
}
```

- [ ] **Step 2: Run handler tests and verify failure**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'TestBenchmarkHandler(ApplyStandardTasks|CreateStandardRunDefaults)' -count=1
```

Expected: FAIL because handlers are undefined.

- [ ] **Step 3: Implement handler interface and methods**

In `backend/internal/handler/admin/benchmark_handler.go`, extend `benchmarkAdminService`:

```go
	EnsureStandardTasks(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error)
	CreateStandardRun(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error)
```

Add request DTO:

```go
type benchmarkStandardRunCreateRequest struct {
	TargetIDs []int64 `json:"target_ids"`
	TaskCount int `json:"task_count"`
	ProcessImmediately *bool `json:"process_immediately"`
	CreatedBy *int64 `json:"created_by"`
}
```

Add methods:

```go
func (h *BenchmarkHandler) ApplyStandardTasks(c *gin.Context) {
	result, err := h.benchmarkService.EnsureStandardTasks(c.Request.Context())
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *BenchmarkHandler) CreateStandardRun(c *gin.Context) {
	var req benchmarkStandardRunCreateRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}
	processImmediately := true
	if req.ProcessImmediately != nil {
		processImmediately = *req.ProcessImmediately
	}
	var processor benchmarkAdminProcessor
	if processImmediately {
		processor = h.processor
		if processor == nil {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_PROCESSOR_UNAVAILABLE", "benchmark processor unavailable"))
			return
		}
	}
	run, err := h.benchmarkService.CreateStandardRun(c.Request.Context(), service.BenchmarkStandardRunRequest{
		TargetIDs: req.TargetIDs,
		TaskCount: req.TaskCount,
		ProcessImmediately: processImmediately,
		CreatedBy: req.CreatedBy,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	if processImmediately {
		runID := run.ID
		go func(processor benchmarkAdminProcessor, runID int64) {
			log := logger.L().With(zap.String("component", "handler.admin.benchmark"), zap.Int64("run_id", runID))
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Error("benchmark.process_immediately_panic_recovered", zap.Any("panic", recovered))
				}
			}()
			if err := processBenchmarkRunUntilDone(context.Background(), processor, runID); err != nil {
				log.Error("benchmark.process_immediately_failed", zap.Error(err))
			}
		}(processor, runID)
	}
	response.Success(c, run)
}
```

In `writeBenchmarkError`, add `"no enabled standard benchmark tasks available"` to validation errors.

- [ ] **Step 4: Register routes**

Modify `backend/internal/server/routes/benchmark.go`:

```go
		benchmark.GET("/tasks", h.Admin.Benchmark.ListTasks)
		benchmark.POST("/tasks/standard/apply", h.Admin.Benchmark.ApplyStandardTasks)
		benchmark.POST("/tasks", h.Admin.Benchmark.CreateTask)
```

and:

```go
		benchmark.POST("/runs/preview", h.Admin.Benchmark.PreviewRun)
		benchmark.POST("/runs/standard", h.Admin.Benchmark.CreateStandardRun)
		benchmark.POST("/runs", h.Admin.Benchmark.CreateRun)
```

Add route-test assertions in `backend/internal/server/routes/benchmark_test.go`:

```go
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/tasks/standard/apply")
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/runs/standard")
```

- [ ] **Step 5: Run backend handler and route tests**

Run:

```bash
cd backend
go test ./internal/handler/admin ./internal/server/routes -run 'Benchmark|TestBenchmark' -count=1
```

Expected: PASS for benchmark handler and route tests.

---

### Task 6: Frontend API Types and Wrappers

**Files:**
- Modify: `frontend/src/types/benchmark.ts`
- Modify: `frontend/src/api/admin/benchmark.ts`
- Modify: `frontend/src/api/__tests__/benchmark.spec.ts`

- [ ] **Step 1: Add failing API tests**

Append to `frontend/src/api/__tests__/benchmark.spec.ts`:

```ts
  it('applies standard benchmark tasks', async () => {
    const response = { created_count: 2, existing_count: 4, enabled_count: 6, tasks: [] }
    post.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.applyStandardTasks()

    expect(post).toHaveBeenCalledWith('/admin/benchmark/tasks/standard/apply')
    expect(result).toEqual(response)
  })

  it('creates a standard run with optional advanced payload', async () => {
    const payload = { target_ids: [7], task_count: 3, process_immediately: false }
    const response = { id: 88, status: 'queued', trigger_type: 'manual' }
    post.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.createStandardRun(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/runs/standard', payload)
    expect(result).toEqual(response)
  })
```

- [ ] **Step 2: Run API tests and verify failure**

Run:

```bash
cd frontend
npm test -- src/api/__tests__/benchmark.spec.ts --runInBand
```

If this project uses Vitest directly, run:

```bash
cd frontend
npx vitest run src/api/__tests__/benchmark.spec.ts
```

Expected: FAIL with `applyStandardTasks is not a function`.

- [ ] **Step 3: Add types**

In `frontend/src/types/benchmark.ts`, add near action responses:

```ts
export interface BenchmarkStandardTaskApplyResponse {
  created_count: number
  existing_count: number
  enabled_count: number
  tasks: BenchmarkTask[]
}

export interface CreateBenchmarkStandardRunRequest {
  target_ids?: number[]
  task_count?: number
  process_immediately?: boolean
  created_by?: number | null
}
```

- [ ] **Step 4: Add API functions**

In `frontend/src/api/admin/benchmark.ts`, import the new types and add:

```ts
export async function applyStandardTasks(): Promise<BenchmarkStandardTaskApplyResponse> {
  const { data } = await apiClient.post<BenchmarkStandardTaskApplyResponse>(
    '/admin/benchmark/tasks/standard/apply',
  )
  return data
}

export async function createStandardRun(
  payload?: CreateBenchmarkStandardRunRequest,
): Promise<BenchmarkRun> {
  const { data } = await apiClient.post<BenchmarkRun>(
    '/admin/benchmark/runs/standard',
    payload,
  )
  return data
}
```

Add both functions to `adminBenchmarkAPI`.

- [ ] **Step 5: Run API tests**

Run:

```bash
cd frontend
npx vitest run src/api/__tests__/benchmark.spec.ts
```

Expected: PASS.

---

### Task 7: Runs Page Standard Action

**Files:**
- Modify: `frontend/src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts`
- Modify: `frontend/src/views/admin/benchmark/BenchmarkRunsView.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`

- [ ] **Step 1: Update failing view test**

In `BenchmarkViews.smoke.spec.ts`, add `createStandardRun` to the hoisted mocks and mocked admin API:

```ts
const { listTargets, createTarget, listTasks, createTask, listRuns, createRun, createStandardRun, listSchedules, createSchedule } = vi.hoisted(() => ({
  listTargets: vi.fn(),
  createTarget: vi.fn(),
  listTasks: vi.fn(),
  createTask: vi.fn(),
  listRuns: vi.fn(),
  createRun: vi.fn(),
  createStandardRun: vi.fn(),
  listSchedules: vi.fn(),
  createSchedule: vi.fn(),
}))
```

and:

```ts
      createStandardRun,
```

Replace the `BenchmarkRunsView` test with:

```ts
describe('BenchmarkRunsView', () => {
  it('creates a standard run by default and uses advanced target options when selected', async () => {
    const { default: View } = await import('../BenchmarkRunsView.vue')
    listTargets.mockResolvedValue({ ...emptyPage, items: [{ id: 7, model_name: 'gpt-4.1', channel_id: 3, display_name: 'GPT', enabled: true }], total: 1 })
    createStandardRun.mockResolvedValue({ id: 9, status: 'queued' })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="create-standard-run-button"]').trigger('click')
    await flushPromises()

    expect(createStandardRun).toHaveBeenCalledWith(undefined)

    await wrapper.find('[data-test="standard-run-advanced-toggle"]').trigger('click')
    await wrapper.find('[data-test="run-task-count"]').setValue('5')
    await wrapper.find('[data-test="run-target-7"]').setValue(true)
    await wrapper.find('[data-test="run-process-immediately"]').setValue(false)
    await wrapper.find('[data-test="create-standard-run-button"]').trigger('click')
    await flushPromises()

    expect(createStandardRun).toHaveBeenLastCalledWith({
      target_ids: [7],
      task_count: 5,
      process_immediately: false,
    })
  })
})
```

- [ ] **Step 2: Run view test and verify failure**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts
```

Expected: FAIL because the new data-test selectors and API call do not exist.

- [ ] **Step 3: Implement runs page standard action**

In `BenchmarkRunsView.vue`:

- Rename `creating` to `creatingStandardRun`.
- Keep `targetIds`, `taskCount`, and `processImmediately`.
- Add `const showAdvanced = ref(false)`.
- Set `const taskCount = ref<number | null>(null)` and `const processImmediately = ref(true)`.
- Replace the visible form heading/copy with i18n keys under `benchmark.admin.runs.standardTitle`, `standardDescription`, and `advancedOptions`.
- Add a button with `data-test="standard-run-advanced-toggle"` to toggle advanced options.
- Wrap the target/task/process controls in `v-if="showAdvanced"`.

Replace `createRun` with:

```ts
async function createStandardRun() {
  formError.value = ''
  const normalizedTaskCount = Number(taskCount.value || 0)
  if (normalizedTaskCount < 0) {
    formError.value = t('benchmark.admin.runs.validation.taskCountLimit')
    return
  }

  const payload = showAdvanced.value
    ? {
        ...(targetIds.value.length > 0 ? { target_ids: [...targetIds.value] } : {}),
        ...(normalizedTaskCount > 0 ? { task_count: normalizedTaskCount } : {}),
        process_immediately: processImmediately.value,
      }
    : undefined

  creatingStandardRun.value = true
  try {
    const created = await adminAPI.benchmark.createStandardRun(payload)
    runs.value = [created, ...runs.value.filter((run) => run.id !== created.id)]
    pagination.total += 1
    appStore.showSuccess(t('benchmark.admin.runs.createSuccess', { id: created.id }))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.runs.createError'))
  } finally {
    creatingStandardRun.value = false
  }
}
```

Keep the old `createRun` API unused in this view; the API wrapper remains for advanced/manual callers.

- [ ] **Step 4: Add i18n keys**

In `zh.ts` under `benchmark.admin.runs`, add:

```ts
standardTitle: '使用标准设定运行',
standardDescription: '默认测试所有已启用目标，自动应用标准任务集，并立即处理本次运行。',
advancedOptions: '高级选项',
hideAdvancedOptions: '收起高级选项',
```

In `en.ts` under `benchmark.admin.runs`, add:

```ts
standardTitle: 'Run with Standard Settings',
standardDescription: 'Tests all enabled targets by default, applies the standard task set, and processes the run immediately.',
advancedOptions: 'Advanced Options',
hideAdvancedOptions: 'Hide Advanced Options',
```

Update existing labels:

- zh `createRun`: `运行标准雷达测试`
- en `createRun`: `Run Standard Radar Test`
- zh `fields.taskCount`: `标准任务数`
- en `fields.taskCount`: `Standard Task Count`

- [ ] **Step 5: Run runs view test**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts
```

Expected: PASS for the runs view case.

---

### Task 8: Tasks Page Standard Apply Action

**Files:**
- Modify: `frontend/src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts`
- Modify: `frontend/src/views/admin/benchmark/BenchmarkTasksView.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`

- [ ] **Step 1: Add failing task apply test**

In `BenchmarkViews.smoke.spec.ts`, add `applyStandardTasks` to hoisted mocks and mocked admin API:

```ts
applyStandardTasks: vi.fn(),
```

Add test:

```ts
describe('BenchmarkTasksView standard task set', () => {
  it('applies the standard task set and reloads tasks', async () => {
    const { default: View } = await import('../BenchmarkTasksView.vue')
    applyStandardTasks.mockResolvedValue({ created_count: 6, existing_count: 0, enabled_count: 6, tasks: [] })
    const wrapper = mount(View, { global: { stubs } })
    await flushPromises()

    await wrapper.find('[data-test="apply-standard-tasks-button"]').trigger('click')
    await flushPromises()

    expect(applyStandardTasks).toHaveBeenCalledTimes(1)
    expect(listTasks).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts
```

Expected: FAIL because `apply-standard-tasks-button` does not exist.

- [ ] **Step 3: Implement button and action**

In `BenchmarkTasksView.vue` header actions, replace the single refresh button with a flex group containing:

```vue
<button type="button" data-test="apply-standard-tasks-button" class="btn btn-primary inline-flex items-center gap-2" :disabled="applyingStandardTasks" @click="applyStandardTasks">
  <Icon name="play" size="sm" />
  {{ t('benchmark.admin.tasks.applyStandard') }}
</button>
```

Add state:

```ts
const applyingStandardTasks = ref(false)
```

Add method:

```ts
async function applyStandardTasks() {
  applyingStandardTasks.value = true
  try {
    const result = await adminAPI.benchmark.applyStandardTasks()
    appStore.showSuccess(t('benchmark.admin.tasks.applyStandardSuccess', {
      created: result.created_count,
      existing: result.existing_count,
      enabled: result.enabled_count,
    }))
    await load()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('benchmark.admin.tasks.applyStandardError'))
  } finally {
    applyingStandardTasks.value = false
  }
}
```

- [ ] **Step 4: Add i18n keys**

In `zh.ts` under `benchmark.admin.tasks`, add:

```ts
applyStandard: '应用标准任务集',
applyStandardSuccess: '标准任务集已应用：新增 {created} 个，已有 {existing} 个，启用 {enabled} 个',
applyStandardError: '应用标准任务集失败',
emptyDescription: '可以先应用标准任务集，也可以手动创建自定义任务。',
```

In `en.ts` under `benchmark.admin.tasks`, add:

```ts
applyStandard: 'Apply Standard Task Set',
applyStandardSuccess: 'Standard task set applied: {created} created, {existing} existing, {enabled} enabled',
applyStandardError: 'Failed to apply standard task set',
emptyDescription: 'Apply the standard task set first, or create a custom task manually.',
```

- [ ] **Step 5: Run task view tests**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts
```

Expected: PASS.

---

### Task 9: Copy Cleanup and Localization Source Test

**Files:**
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/src/views/admin/benchmark/__tests__/BenchmarkLocalizationSource.spec.ts`

- [ ] **Step 1: Add source-level assertions for stale current workflow terms**

In `BenchmarkLocalizationSource.spec.ts`, add:

```ts
  it('does not expose stale profile or suite copy in current benchmark admin pages', () => {
    const currentSources = [
      readFileSync(resolve(__dirname, '../BenchmarkDashboardView.vue'), 'utf8'),
      readFileSync(resolve(__dirname, '../BenchmarkTargetsView.vue'), 'utf8'),
      readFileSync(resolve(__dirname, '../BenchmarkTasksView.vue'), 'utf8'),
      readFileSync(resolve(__dirname, '../BenchmarkRunsView.vue'), 'utf8'),
      readFileSync(resolve(__dirname, '../BenchmarkSchedulesView.vue'), 'utf8'),
    ].join('\n')

    expect(currentSources).not.toContain('profile_id')
    expect(currentSources).not.toContain('suite_id')
    expect(currentSources).not.toContain('BenchmarkProfile')
    expect(currentSources).not.toContain('BenchmarkSuite')
  })
```

- [ ] **Step 2: Run localization source test**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkLocalizationSource.spec.ts
```

Expected: PASS if stale current workflow terms are gone from the Vue sources. If it fails, update only the currently used benchmark admin views, not unrelated historical docs or types.

- [ ] **Step 3: Update visible locale strings**

In both `zh.ts` and `en.ts`, update currently visible benchmark admin strings:

- dashboard `description`: mention standard run overview
- dashboard `topDescription`: replace `score snapshot` with `completed run`
- tasks `description`: mention standard task set and advanced custom tasks
- runs `description`: mention standard and advanced runs
- schedules `description`: mention recurring benchmark runs, without profile wording
- runDetail `emptyScoreDescription`: replace `score snapshots` with `scores`

- [ ] **Step 4: Run localization tests**

Run:

```bash
cd frontend
npx vitest run src/views/admin/benchmark/__tests__/BenchmarkLocalizationSource.spec.ts
```

Expected: PASS.

---

### Task 10: Full Verification

**Files:**
- No code edits

- [ ] **Step 1: Run backend benchmark-focused tests**

Run:

```bash
cd backend
go test ./internal/service ./internal/repository ./internal/handler/admin ./internal/server/routes -run 'Benchmark|TestBenchmark' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run frontend benchmark-focused tests**

Run:

```bash
cd frontend
npx vitest run src/api/__tests__/benchmark.spec.ts src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts src/views/admin/benchmark/__tests__/BenchmarkLocalizationSource.spec.ts
```

Expected: PASS.

- [ ] **Step 3: Run formatting**

Run:

```bash
cd backend
gofmt -w internal/service/benchmark_standard_tasks.go internal/service/benchmark_standard_tasks_test.go internal/service/benchmark_service.go internal/service/benchmark_repository.go internal/repository/benchmark_repo.go internal/handler/admin/benchmark_handler.go internal/handler/admin/benchmark_handler_test.go internal/server/routes/benchmark.go internal/server/routes/benchmark_test.go
```

Expected: no output.

- [ ] **Step 4: Inspect git diff for unrelated changes**

Run:

```bash
git diff --stat
git status --short
```

Expected: only benchmark standard-run files and locale/API/view tests changed, plus the pre-existing untracked `frontend/package-lock.json` remains untracked and unstaged.

- [ ] **Step 5: Commit implementation**

Run:

```bash
git add backend/internal/service/benchmark_standard_tasks.go backend/internal/service/benchmark_standard_tasks_test.go backend/internal/service/benchmark_service.go backend/internal/service/benchmark_repository.go backend/internal/repository/benchmark_repo.go backend/internal/handler/admin/benchmark_handler.go backend/internal/handler/admin/benchmark_handler_test.go backend/internal/server/routes/benchmark.go backend/internal/server/routes/benchmark_test.go frontend/src/types/benchmark.ts frontend/src/api/admin/benchmark.ts frontend/src/api/__tests__/benchmark.spec.ts frontend/src/views/admin/benchmark/BenchmarkRunsView.vue frontend/src/views/admin/benchmark/BenchmarkTasksView.vue frontend/src/views/admin/benchmark/__tests__/BenchmarkViews.smoke.spec.ts frontend/src/views/admin/benchmark/__tests__/BenchmarkLocalizationSource.spec.ts frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
git commit -m "feat(benchmark): add standard radar run"
```

Expected: commit succeeds and does not include `frontend/package-lock.json`.
