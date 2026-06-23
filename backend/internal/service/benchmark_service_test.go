package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type benchmarkServiceRepoStub struct {
	t               *testing.T
	createSuiteFn   func(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	listSuitesFn    func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	createTargetFn  func(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	listTargetsFn   func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	createTaskFn    func(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	listTasksFn     func(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	createProfileFn func(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	getProfileFn    func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
}

func newBenchmarkServiceRepoStub(t *testing.T) *benchmarkServiceRepoStub {
	t.Helper()
	return &benchmarkServiceRepoStub{t: t}
}

func (s *benchmarkServiceRepoStub) CreateSuite(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	if s.createSuiteFn != nil {
		return s.createSuiteFn(ctx, input)
	}
	s.t.Fatalf("unexpected CreateSuite call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListSuites(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	if s.listSuitesFn != nil {
		return s.listSuitesFn(ctx, input)
	}
	s.t.Fatalf("unexpected ListSuites call")
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if s.createTargetFn != nil {
		return s.createTargetFn(ctx, input)
	}
	s.t.Fatalf("unexpected CreateTarget call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	if s.listTargetsFn != nil {
		return s.listTargetsFn(ctx, input)
	}
	s.t.Fatalf("unexpected ListTargets call")
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if s.createTaskFn != nil {
		return s.createTaskFn(ctx, input)
	}
	s.t.Fatalf("unexpected CreateTask call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	if s.listTasksFn != nil {
		return s.listTasksFn(ctx, input)
	}
	s.t.Fatalf("unexpected ListTasks call")
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateProfile(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	if s.createProfileFn != nil {
		return s.createProfileFn(ctx, input)
	}
	s.t.Fatalf("unexpected CreateProfile call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
	if s.getProfileFn != nil {
		return s.getProfileFn(ctx, id)
	}
	s.t.Fatalf("unexpected GetProfile call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
	s.t.Fatalf("unexpected CreateRunWithSnapshots call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	s.t.Fatalf("unexpected ListRuns call")
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	s.t.Fatalf("unexpected ListRunResults call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error {
	s.t.Fatalf("unexpected UpdateResult call")
	return nil
}

func (s *benchmarkServiceRepoStub) SaveScoreSnapshots(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
	s.t.Fatalf("unexpected SaveScoreSnapshots call")
	return nil
}

func (s *benchmarkServiceRepoStub) PublishPublicSnapshot(ctx context.Context, input BenchmarkPublicSnapshotInput) error {
	s.t.Fatalf("unexpected PublishPublicSnapshot call")
	return nil
}

func (s *benchmarkServiceRepoStub) GetLatestPublicSnapshot(ctx context.Context) (*ent.BenchmarkPublicSnapshot, error) {
	s.t.Fatalf("unexpected GetLatestPublicSnapshot call")
	return nil, nil
}

func TestBenchmarkServiceCreateSuiteDelegatesInput(t *testing.T) {
	t.Parallel()

	var gotInput BenchmarkSuiteInput
	want := &ent.BenchmarkSuite{ID: 11}
	repo := newBenchmarkServiceRepoStub(t)
	repo.createSuiteFn = func(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
		gotInput = input
		return want, nil
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.CreateSuite(context.Background(), BenchmarkSuiteInput{
		Name:             "suite",
		Slug:             "suite-1",
		Description:      "desc",
		Enabled:          true,
		PublicVisible:    true,
		DefaultProfileID: int64Ptr(77),
		Metadata:         map[string]any{"source": "unit"},
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, BenchmarkSuiteInput{
		Name:             "suite",
		Slug:             "suite-1",
		Description:      "desc",
		Enabled:          true,
		PublicVisible:    true,
		DefaultProfileID: int64Ptr(77),
		Metadata:         map[string]any{"source": "unit"},
	}, gotInput)
}

func TestBenchmarkServiceListSuitesDelegatesInput(t *testing.T) {
	t.Parallel()

	want := []*ent.BenchmarkSuite{{ID: 1}, {ID: 2}}
	repo := newBenchmarkServiceRepoStub(t)
	repo.listSuitesFn = func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
		require.Equal(t, BenchmarkListInput{Page: 3, PageSize: 50}, input)
		return want, 123, nil
	}

	svc := NewBenchmarkService(repo)
	got, total, err := svc.ListSuites(context.Background(), BenchmarkListInput{Page: 3, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 123, total)
}

func TestBenchmarkServiceCreateTargetRejectsEmptyModelName(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "",
		ChannelID: 1,
	})
	require.EqualError(t, err, "model name is required")
}

func TestBenchmarkServiceCreateTargetRejectsWhitespaceModelName(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: " \t ",
		ChannelID: 1,
	})
	require.EqualError(t, err, "model name is required")
}

func TestBenchmarkServiceCreateTargetRejectsInvalidChannelID(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		ChannelID: 0,
	})
	require.EqualError(t, err, "channel id must be positive")
}

func TestBenchmarkServiceCreateTargetDelegatesInput(t *testing.T) {
	t.Parallel()

	var gotInput BenchmarkTargetInput
	want := &ent.BenchmarkTarget{ID: 21}
	repo := newBenchmarkServiceRepoStub(t)
	repo.createTargetFn = func(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
		gotInput = input
		return want, nil
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName:           "gpt-4.1",
		ChannelID:           7,
		DisplayName:         "GPT 4.1",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "main",
		SupportedTaskTypes:  []string{"reasoning"},
		MaxConcurrency:      3,
		PerRunBudget:        float64Ptr(2.5),
		DailyBudget:         float64Ptr(20),
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           9,
		Metadata:            map[string]any{"region": "us"},
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, BenchmarkTargetInput{
		ModelName:           "gpt-4.1",
		ChannelID:           7,
		DisplayName:         "GPT 4.1",
		ProviderSnapshot:    "openai",
		ChannelNameSnapshot: "main",
		SupportedTaskTypes:  []string{"reasoning"},
		MaxConcurrency:      3,
		PerRunBudget:        float64Ptr(2.5),
		DailyBudget:         float64Ptr(20),
		Enabled:             true,
		PublicVisible:       true,
		SortOrder:           9,
		Metadata:            map[string]any{"region": "us"},
	}, gotInput)
}

func TestBenchmarkServiceListTargetsDelegatesInput(t *testing.T) {
	t.Parallel()

	want := []*ent.BenchmarkTarget{{ID: 1}}
	repo := newBenchmarkServiceRepoStub(t)
	repo.listTargetsFn = func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
		require.Equal(t, BenchmarkListInput{Page: 4, PageSize: 25}, input)
		return want, 9, nil
	}

	svc := NewBenchmarkService(repo)
	got, total, err := svc.ListTargets(context.Background(), BenchmarkListInput{Page: 4, PageSize: 25})
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 9, total)
}

func TestBenchmarkServiceCreateTaskRejectsEmptyType(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID: 1,
		Title:   "task",
		Type:    "",
	})
	require.EqualError(t, err, "task type is required")
}

func TestBenchmarkServiceCreateTaskRejectsWhitespaceType(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID: 1,
		Title:   "task",
		Type:    " \n\t",
	})
	require.EqualError(t, err, "task type is required")
}

func TestBenchmarkServiceCreateTaskRejectsInvalidMinScale(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID:      1,
		Title:        "task",
		Type:         "reasoning",
		MinScale:     "giant",
		PublicPrompt: true,
	})
	require.EqualError(t, err, "unsupported task scale")
}

func TestBenchmarkServiceCreateTaskDelegatesInput(t *testing.T) {
	t.Parallel()

	var gotInput BenchmarkTaskInput
	want := &ent.BenchmarkTask{ID: 31}
	repo := newBenchmarkServiceRepoStub(t)
	repo.createTaskFn = func(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
		gotInput = input
		return want, nil
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID:        5,
		Title:          "task",
		Type:           "reasoning",
		Category:       "general",
		Difficulty:     "hard",
		Tags:           []string{"alpha", "beta"},
		Prompt:         "prompt",
		InputPayload:   map[string]any{"x": 1},
		ExpectedOutput: map[string]any{"y": 2},
		VerifierType:   "json",
		VerifierConfig: map[string]any{"strict": true},
		Weight:         2.5,
		MinScale:       BenchmarkTaskScaleMedium,
		PublicPrompt:   true,
		Enabled:        true,
		Metadata:       map[string]any{"team": "evals"},
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, BenchmarkTaskInput{
		SuiteID:        5,
		Title:          "task",
		Type:           "reasoning",
		Category:       "general",
		Difficulty:     "hard",
		Tags:           []string{"alpha", "beta"},
		Prompt:         "prompt",
		InputPayload:   map[string]any{"x": 1},
		ExpectedOutput: map[string]any{"y": 2},
		VerifierType:   "json",
		VerifierConfig: map[string]any{"strict": true},
		Weight:         2.5,
		MinScale:       BenchmarkTaskScaleMedium,
		PublicPrompt:   true,
		Enabled:        true,
		Metadata:       map[string]any{"team": "evals"},
	}, gotInput)
}

func TestBenchmarkServiceListTasksDelegatesInput(t *testing.T) {
	t.Parallel()

	want := []*ent.BenchmarkTask{{ID: 1}, {ID: 2}}
	repo := newBenchmarkServiceRepoStub(t)
	repo.listTasksFn = func(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
		require.Equal(t, BenchmarkTaskListInput{
			BenchmarkListInput: BenchmarkListInput{Page: 2, PageSize: 10},
			SuiteID:            17,
			TaskTypes:          []string{"reasoning"},
			Enabled:            boolPtr(true),
		}, input)
		return want, 8, nil
	}

	svc := NewBenchmarkService(repo)
	got, total, err := svc.ListTasks(context.Background(), BenchmarkTaskListInput{
		BenchmarkListInput: BenchmarkListInput{Page: 2, PageSize: 10},
		SuiteID:            17,
		TaskTypes:          []string{"reasoning"},
		Enabled:            boolPtr(true),
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 8, total)
}

func TestBenchmarkServiceCreateProfileRejectsEmptyTargets(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TaskTypes: []string{"reasoning"},
	})
	require.EqualError(t, err, "at least one target is required")
}

func TestBenchmarkServiceCreateProfileRejectsNonPositiveTargetID(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	for _, targetID := range []int64{0, -7} {
		_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
			SuiteID:   1,
			Name:      "profile",
			TargetIDs: []int64{1, targetID},
			TaskTypes: []string{"reasoning"},
		})
		require.EqualError(t, err, "target id must be positive")
	}
}

func TestBenchmarkServiceCreateProfileRejectsEmptyTaskTypes(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TargetIDs: []int64{1},
	})
	require.EqualError(t, err, "at least one task type is required")
}

func TestBenchmarkServiceCreateProfileRejectsWhitespaceTaskType(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TargetIDs: []int64{1},
		TaskTypes: []string{"reasoning", "  \n\t"},
	})
	require.EqualError(t, err, "task type is required")
}

func TestBenchmarkServiceCreateProfileRejectsInvalidTaskScale(t *testing.T) {
	svc := NewBenchmarkService(newBenchmarkServiceRepoStub(t))

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TargetIDs: []int64{1},
		TaskTypes: []string{"reasoning"},
		TaskScale: "giant",
	})
	require.EqualError(t, err, "unsupported task scale")
}

func TestBenchmarkServiceGetProfileDelegatesInput(t *testing.T) {
	t.Parallel()

	want := &ent.BenchmarkProfile{ID: 123}
	repo := newBenchmarkServiceRepoStub(t)
	repo.getProfileFn = func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
		require.Equal(t, int64(99), id)
		return want, nil
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.GetProfile(context.Background(), 99)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestBenchmarkServiceCreateProfileAcceptsValidInput(t *testing.T) {
	t.Parallel()

	var gotInput BenchmarkProfileInput
	want := &ent.BenchmarkProfile{ID: 123}
	repo := newBenchmarkServiceRepoStub(t)
	repo.createProfileFn = func(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
		gotInput = input
		return want, nil
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:          42,
		Name:             "profile",
		Description:      "desc",
		TargetIDs:        []int64{7, 8},
		TaskTypes:        []string{"reasoning", "coding"},
		TaskScale:        "",
		TaskCountLimit:   intPtr(10),
		PerTypeLimit:     map[string]int{"reasoning": 1},
		DifficultyFilter: []string{"hard"},
		TagFilter:        []string{"public"},
		SamplingStrategy: "balanced",
		SelectionSeed:    int64Ptr(123),
		RuntimeConfig:    map[string]any{"timeout": 30},
		ScoringConfig:    map[string]any{"mode": "strict"},
		Metadata:         map[string]any{"owner": "bench"},
		Enabled:          true,
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, BenchmarkProfileInput{
		SuiteID:          42,
		Name:             "profile",
		Description:      "desc",
		TargetIDs:        []int64{7, 8},
		TaskTypes:        []string{"reasoning", "coding"},
		TaskScale:        "",
		TaskCountLimit:   intPtr(10),
		PerTypeLimit:     map[string]int{"reasoning": 1},
		DifficultyFilter: []string{"hard"},
		TagFilter:        []string{"public"},
		SamplingStrategy: "balanced",
		SelectionSeed:    int64Ptr(123),
		RuntimeConfig:    map[string]any{"timeout": 30},
		ScoringConfig:    map[string]any{"mode": "strict"},
		Metadata:         map[string]any{"owner": "bench"},
		Enabled:          true,
	}, gotInput)
}
