package service

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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
	mu                  sync.Mutex
	blockCreate         chan struct{}
	releaseCreate       chan struct{}
	blockedCreateOnce   sync.Once
	listByTitlesCalled  chan struct{}
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
	if s.blockCreate != nil {
		s.blockedCreateOnce.Do(func() {
			s.blockCreate <- struct{}{}
			<-s.releaseCreate
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listByTitlesCalled != nil {
		s.listByTitlesCalled <- struct{}{}
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()

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

func TestBenchmarkStandardTaskDefinitionsAreStableAndSupported(t *testing.T) {
	t.Parallel()

	tasks := DefaultBenchmarkStandardTasks()
	if len(tasks) != 6 {
		t.Fatalf("len(DefaultBenchmarkStandardTasks()) = %d, want 6", len(tasks))
	}

	wantTitles := []string{
		"Standard Reasoning - Arithmetic",
		"Standard Reasoning - Multiple Choice",
		"Standard Structured Output - JSON Keys",
		"Standard Coding - Function Envelope",
		"Standard Writing - Concise Summary",
		"Standard Instruction Following - Label",
	}
	if got := BenchmarkStandardTaskTitles(); !reflect.DeepEqual(got, wantTitles) {
		t.Fatalf("BenchmarkStandardTaskTitles() = %#v, want %#v", got, wantTitles)
	}

	seen := map[string]bool{}
	lastSortOrder := 0
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
			if len(testAnyStringSlice(task.VerifierConfig["required_keys"])) == 0 {
				t.Fatalf("json task %q missing required_keys", task.Title)
			}
		default:
			t.Fatalf("unsupported verifier type %q", task.VerifierType)
		}
		if task.Weight != 1 || !task.PublicPrompt || !task.Enabled {
			t.Fatalf("standard task has wrong defaults: %#v", task)
		}
		if task.SortOrder <= lastSortOrder {
			t.Fatalf("standard task %q sort_order = %d, want greater than %d", task.Title, task.SortOrder, lastSortOrder)
		}
		lastSortOrder = task.SortOrder
	}
}

func TestBenchmarkServiceEnsureStandardTasksCreatesMissingAndIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	svc := NewBenchmarkService(repo)

	first, err := svc.EnsureStandardTasks(context.Background())
	if err != nil {
		t.Fatalf("EnsureStandardTasks first returned error: %v", err)
	}
	if first.CreatedCount != 6 || first.ExistingCount != 0 || first.EnabledCount != 6 || len(first.Tasks) != 6 {
		t.Fatalf("first result = %#v, want 6 created, 0 existing, 6 enabled, 6 tasks", first)
	}
	if len(repo.createdTasks) != 6 {
		t.Fatalf("created tasks = %d, want 6", len(repo.createdTasks))
	}

	existing := repo.tasksByTitle["Standard Reasoning - Arithmetic"]
	existing.Prompt = "admin edited prompt"
	existing.Enabled = false

	second, err := svc.EnsureStandardTasks(context.Background())
	if err != nil {
		t.Fatalf("EnsureStandardTasks second returned error: %v", err)
	}
	if second.CreatedCount != 0 || second.ExistingCount != 6 || second.EnabledCount != 5 || len(second.Tasks) != 6 {
		t.Fatalf("second result = %#v, want 0 created, 6 existing, 5 enabled, 6 tasks", second)
	}
	if len(repo.createdTasks) != 6 {
		t.Fatalf("created tasks after second call = %d, want 6", len(repo.createdTasks))
	}
	if got := repo.tasksByTitle["Standard Reasoning - Arithmetic"].Prompt; got != "admin edited prompt" {
		t.Fatalf("EnsureStandardTasks overwrote existing task prompt: %q", got)
	}
}

func TestBenchmarkServiceEnsureStandardTasksSerializesConcurrentApply(t *testing.T) {
	repo := newBenchmarkStandardRepoStub()
	repo.blockCreate = make(chan struct{}, 1)
	repo.releaseCreate = make(chan struct{})
	repo.listByTitlesCalled = make(chan struct{}, 2)
	svc := NewBenchmarkService(repo)

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.EnsureStandardTasks(context.Background())
		firstDone <- err
	}()

	<-repo.listByTitlesCalled
	<-repo.blockCreate
	secondDone := make(chan error, 1)
	go func() {
		_, err := svc.EnsureStandardTasks(context.Background())
		secondDone <- err
	}()
	select {
	case <-repo.listByTitlesCalled:
	case <-time.After(25 * time.Millisecond):
	}

	close(repo.releaseCreate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first EnsureStandardTasks returned error: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second EnsureStandardTasks returned error: %v", err)
	}

	if len(repo.createdTasks) != len(DefaultBenchmarkStandardTasks()) {
		t.Fatalf("created tasks = %d, want exactly one standard task set", len(repo.createdTasks))
	}
}

func TestBenchmarkServiceCreateStandardRunEnsuresTasksAndCreatesSnapshots(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{
		{ID: 11, ModelName: "model-a", ChannelID: 101, Enabled: true, SortOrder: 2},
		{ID: 22, ModelName: "model-b", ChannelID: 202, Enabled: true, SortOrder: 1},
	}
	svc := NewBenchmarkService(repo)

	run, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{
		TaskCount: 2,
		CreatedBy: ptrInt64(7),
	})
	if err != nil {
		t.Fatalf("CreateStandardRun returned error: %v", err)
	}
	if run.ID != 99 {
		t.Fatalf("run.ID = %d, want 99", run.ID)
	}
	if repo.createRunInput == nil {
		t.Fatal("CreateStandardRun did not create run snapshots")
	}
	input := repo.createRunInput
	if input.Status != BenchmarkRunStatusQueued || input.TriggerType != BenchmarkTriggerManual {
		t.Fatalf("run status/trigger = %q/%q, want queued/manual", input.Status, input.TriggerType)
	}
	if input.TaskCount != 2 || input.PlannedTargetCount != 2 || input.PlannedTaskCount != 2 || input.PlannedResultCount != 4 {
		t.Fatalf("run plan = task_count:%d targets:%d tasks:%d results:%d, want 2/2/2/4",
			input.TaskCount, input.PlannedTargetCount, input.PlannedTaskCount, input.PlannedResultCount)
	}
	if input.CreatedBy == nil || *input.CreatedBy != 7 {
		t.Fatalf("CreatedBy = %#v, want 7", input.CreatedBy)
	}
	if got := benchmarkRunTaskIDsFromInput(input.Tasks); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("snapshot task ids = %#v, want first two standard tasks", got)
	}
	if !reflect.DeepEqual(repo.listEnabledTitleArg, BenchmarkStandardTaskTitles()) {
		t.Fatalf("enabled title lookup = %#v, want standard titles", repo.listEnabledTitleArg)
	}
}

func TestBenchmarkServiceCreateStandardRunUsesSelectedTargetsAndAllStandardTasksByDefault(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{
		{ID: 11, ModelName: "model-a", ChannelID: 101, Enabled: true, SortOrder: 1},
		{ID: 22, ModelName: "model-b", ChannelID: 202, Enabled: true, SortOrder: 2},
	}
	svc := NewBenchmarkService(repo)

	_, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{TargetIDs: []int64{22}})
	if err != nil {
		t.Fatalf("CreateStandardRun returned error: %v", err)
	}
	if got := benchmarkRunTargetIDsFromInput(repo.createRunInput.Targets); !reflect.DeepEqual(got, []int64{22}) {
		t.Fatalf("snapshot target ids = %#v, want selected target only", got)
	}
	if len(repo.createRunInput.Tasks) != 6 {
		t.Fatalf("snapshot tasks = %d, want all 6 standard tasks", len(repo.createRunInput.Tasks))
	}
}

func TestBenchmarkServiceCreateStandardRunRejectsNegativeTaskCount(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{{ID: 11, ModelName: "model-a", ChannelID: 101, Enabled: true}}
	svc := NewBenchmarkService(repo)

	_, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{TaskCount: -1})
	if err == nil || err.Error() != "task count must not be negative" {
		t.Fatalf("CreateStandardRun error = %v, want task count must not be negative", err)
	}
	if len(repo.createdTasks) != 0 {
		t.Fatalf("created standard tasks despite invalid task_count: %d", len(repo.createdTasks))
	}
	if repo.createRunInput != nil {
		t.Fatal("CreateStandardRun created a run despite invalid task_count")
	}
}

func TestBenchmarkServiceCreateRunRejectsNegativeTaskCount(t *testing.T) {
	t.Parallel()

	svc := NewBenchmarkService(newBenchmarkStandardRepoStub())

	_, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{TaskCount: -1})
	if err == nil || err.Error() != "task count must not be negative" {
		t.Fatalf("CreateRun error = %v, want task count must not be negative", err)
	}
}

func TestBenchmarkServicePreviewRunRejectsNegativeTaskCount(t *testing.T) {
	t.Parallel()

	svc := NewBenchmarkService(newBenchmarkStandardRepoStub())

	_, err := svc.PreviewRun(context.Background(), nil, -1)
	if err == nil || err.Error() != "task count must not be negative" {
		t.Fatalf("PreviewRun error = %v, want task count must not be negative", err)
	}
}

func TestBenchmarkServiceCreateStandardRunReturnsClearErrorWhenStandardTasksDisabled(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{{ID: 11, ModelName: "model-a", ChannelID: 101, Enabled: true}}
	for _, task := range DefaultBenchmarkStandardTasks() {
		created, err := repo.CreateTask(context.Background(), task)
		if err != nil {
			t.Fatalf("CreateTask returned error: %v", err)
		}
		created.Enabled = false
	}
	svc := NewBenchmarkService(repo)

	_, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{})
	if err == nil || err.Error() != "no enabled standard benchmark tasks available" {
		t.Fatalf("CreateStandardRun error = %v, want no enabled standard benchmark tasks available", err)
	}
	if repo.createRunInput != nil {
		t.Fatal("CreateStandardRun created a run despite disabled standard tasks")
	}
}

func TestBenchmarkServiceCreateStandardRunHonorsRuntimeDisabled(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkStandardRepoStub()
	repo.enabledTargets = []*ent.BenchmarkTarget{{ID: 11, ModelName: "model-a", ChannelID: 101, Enabled: true}}
	svc := NewBenchmarkService(repo)
	svc.SetBenchmarkRuntimeProvider(benchmarkRuntimeProviderFunc(func(ctx context.Context) BenchmarkRuntime {
		return benchmarkRuntimeDefaults(false, false)
	}))

	_, err := svc.CreateStandardRun(context.Background(), BenchmarkStandardRunRequest{})
	if err == nil {
		t.Fatal("CreateStandardRun returned nil error, want benchmark disabled error")
	}
	if !strings.Contains(err.Error(), "benchmark is disabled") {
		t.Fatalf("CreateStandardRun error = %v, want benchmark is disabled", err)
	}
	if len(repo.createdTasks) != 0 {
		t.Fatalf("created standard tasks despite disabled runtime: %d", len(repo.createdTasks))
	}
}

type benchmarkRuntimeProviderFunc func(context.Context) BenchmarkRuntime

func (f benchmarkRuntimeProviderFunc) GetBenchmarkRuntime(ctx context.Context) BenchmarkRuntime {
	return f(ctx)
}

func testAnyStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

func benchmarkRunTaskIDsFromInput(tasks []BenchmarkRunTaskInput) []int64 {
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.TaskID)
	}
	return ids
}

func benchmarkRunTargetIDsFromInput(targets []BenchmarkRunTargetInput) []int64 {
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.TargetID)
	}
	return ids
}

func ptrInt64(value int64) *int64 {
	return &value
}
