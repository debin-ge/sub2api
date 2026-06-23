package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/benchmarkprofile"
	"github.com/Wei-Shaw/sub2api/ent/benchmarktask"
	"github.com/stretchr/testify/require"
)

type benchmarkServiceRepoStub struct {
	t                          *testing.T
	createSuiteFn              func(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	listSuitesFn               func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	getSuiteFn                 func(ctx context.Context, id int64) (*ent.BenchmarkSuite, error)
	createTargetFn             func(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	listTargetsFn              func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	getTargetFn                func(ctx context.Context, id int64) (*ent.BenchmarkTarget, error)
	listTargetsByIDsFn         func(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error)
	createTaskFn               func(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	listTasksFn                func(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	listEnabledTasksForSuiteFn func(ctx context.Context, suiteID int64) ([]*ent.BenchmarkTask, error)
	createProfileFn            func(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	getProfileFn               func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
	createRunWithSnapshotsFn   func(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error)
	getRunFn                   func(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	listRunTargetsFn           func(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error)
	listRunTasksFn             func(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error)
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

func (s *benchmarkServiceRepoStub) GetSuite(ctx context.Context, id int64) (*ent.BenchmarkSuite, error) {
	if s.getSuiteFn != nil {
		return s.getSuiteFn(ctx, id)
	}
	s.t.Fatalf("unexpected GetSuite call")
	return nil, nil
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

func (s *benchmarkServiceRepoStub) GetTarget(ctx context.Context, id int64) (*ent.BenchmarkTarget, error) {
	if s.getTargetFn != nil {
		return s.getTargetFn(ctx, id)
	}
	s.t.Fatalf("unexpected GetTarget call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListTargetsByIDs(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error) {
	if s.listTargetsByIDsFn != nil {
		return s.listTargetsByIDsFn(ctx, ids)
	}
	s.t.Fatalf("unexpected ListTargetsByIDs call")
	return nil, nil
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

func (s *benchmarkServiceRepoStub) ListEnabledTasksForSuite(ctx context.Context, suiteID int64) ([]*ent.BenchmarkTask, error) {
	if s.listEnabledTasksForSuiteFn != nil {
		return s.listEnabledTasksForSuiteFn(ctx, suiteID)
	}
	s.t.Fatalf("unexpected ListEnabledTasksForSuite call")
	return nil, nil
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
	if s.createRunWithSnapshotsFn != nil {
		return s.createRunWithSnapshotsFn(ctx, input)
	}
	s.t.Fatalf("unexpected CreateRunWithSnapshots call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
	if s.getRunFn != nil {
		return s.getRunFn(ctx, id)
	}
	s.t.Fatalf("unexpected GetRun call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	s.t.Fatalf("unexpected ListRuns call")
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) ListRunTargets(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error) {
	if s.listRunTargetsFn != nil {
		return s.listRunTargetsFn(ctx, runID)
	}
	s.t.Fatalf("unexpected ListRunTargets call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListRunTasks(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error) {
	if s.listRunTasksFn != nil {
		return s.listRunTasksFn(ctx, runID)
	}
	s.t.Fatalf("unexpected ListRunTasks call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	s.t.Fatalf("unexpected ListRunResults call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error {
	s.t.Fatalf("unexpected UpdateResult call")
	return nil
}

func (s *benchmarkServiceRepoStub) ClaimPendingResults(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error) {
	s.t.Fatalf("unexpected ClaimPendingResults call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) UpdateRunStatus(ctx context.Context, runID int64, status string, errorMessage *string) error {
	s.t.Fatalf("unexpected UpdateRunStatus call")
	return nil
}

func (s *benchmarkServiceRepoStub) CountRunResultsByStatus(ctx context.Context, runID int64) (map[string]int, error) {
	s.t.Fatalf("unexpected CountRunResultsByStatus call")
	return nil, nil
}

func (s *benchmarkServiceRepoStub) GetRunResultContext(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error) {
	s.t.Fatalf("unexpected GetRunResultContext call")
	return nil, nil
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

func TestBenchmarkServicePreviewProfileUsesTaskTypesAndScale(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	svc := NewBenchmarkService(repo)

	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{})
	require.NoError(t, err)
	require.Equal(t, 2, preview.TargetCount)
	require.Equal(t, 2, preview.TaskCount)
	require.Equal(t, 4, preview.ResultCount)
	require.Equal(t, "ability_score_only", preview.RankingBasis)
	require.Equal(t, []string{"reasoning"}, preview.TaskTypes)
	require.Equal(t, BenchmarkTaskScaleSmall, preview.TaskScale)
	require.Equal(t, []int64{101, 102}, preview.SelectedTargetIDs)
	require.Equal(t, []int64{201, 204}, preview.SelectedTaskIDs)
}

func TestBenchmarkServicePreviewProfileAppliesPerTypeLimit(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	svc := NewBenchmarkService(repo)

	limit := 3
	seed := int64(13)
	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{
		TaskTypes:        []string{"reasoning", "coding"},
		TaskScale:        BenchmarkTaskScaleCustom,
		TaskCountLimit:   &limit,
		PerTypeLimit:     map[string]int{"reasoning": 1, "coding": 1},
		DifficultyFilter: []string{"easy", "medium"},
		TagFilter:        []string{"public", "code"},
		SelectionSeed:    &seed,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"reasoning", "coding"}, preview.TaskTypes)
	require.Equal(t, BenchmarkTaskScaleCustom, preview.TaskScale)
	require.Equal(t, 2, preview.TaskCount)
	require.Equal(t, 4, preview.ResultCount)
	require.Len(t, preview.SelectedTaskIDs, 2)
	require.Contains(t, preview.SelectedTaskIDs, int64(203))
	require.True(t, preview.SelectedTaskIDs[0] == 201 || preview.SelectedTaskIDs[0] == 204 || preview.SelectedTaskIDs[1] == 201 || preview.SelectedTaskIDs[1] == 204)
}

func TestBenchmarkServicePreviewProfileCanClearFiltersAndPerTypeLimit(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	profile.PerTypeLimit = map[string]int{"reasoning": 1}
	profile.DifficultyFilter = []string{"easy"}
	profile.TagFilter = []string{"public"}

	svc := NewBenchmarkService(repo)
	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{
		TaskScale:        BenchmarkTaskScaleCustom,
		PerTypeLimit:     map[string]int{},
		DifficultyFilter: []string{},
		TagFilter:        []string{},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{201, 202, 204}, preview.SelectedTaskIDs)
	require.Equal(t, 3, preview.TaskCount)
	require.Equal(t, 6, preview.ResultCount)
}

func TestBenchmarkServicePreviewProfileReturnsResultMatrixSize(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	svc := NewBenchmarkService(repo)

	targetIDs := []int64{102}
	limit := 3
	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{
		TargetIDs:      targetIDs,
		TaskTypes:      []string{"reasoning", "coding"},
		TaskScale:      BenchmarkTaskScaleCustom,
		TaskCountLimit: &limit,
		DifficultyFilter: []string{
			"easy",
			"medium",
			"hard",
		},
		TagFilter: []string{"public", "code"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, preview.TargetCount)
	require.Equal(t, 3, preview.TaskCount)
	require.Equal(t, 3, preview.ResultCount)
	require.Equal(t, []int64{102}, preview.SelectedTargetIDs)
	require.Len(t, preview.SelectedTaskIDs, 3)
}

func TestBenchmarkServicePreviewProfileRejectsMissingProfileTargets(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	repo.listTargetsByIDsFn = func(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error) {
		require.Equal(t, []int64{101, 102}, ids)
		return nil, errors.New("benchmark targets missing: [102]")
	}

	svc := NewBenchmarkService(repo)
	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{})
	require.Nil(t, preview)
	require.EqualError(t, err, "benchmark targets missing: [102]")
}

func TestBenchmarkServicePreviewProfileRejectsExplicitEmptyTargetAndTaskTypeOverrides(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	svc := NewBenchmarkService(repo)

	preview, err := svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{
		TargetIDs: []int64{},
	})
	require.Nil(t, preview)
	require.EqualError(t, err, "at least one target is required")

	preview, err = svc.PreviewProfile(context.Background(), profile.ID, BenchmarkProfilePreviewInput{
		TaskTypes: []string{},
	})
	require.Nil(t, preview)
	require.EqualError(t, err, "at least one task type is required")
}

func TestBenchmarkServiceCreateRunMaterializesTargetAndTaskSnapshots(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	createdBy := int64(9001)
	var gotInput BenchmarkCreateRunInput
	repo.createRunWithSnapshotsFn = func(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
		gotInput = input
		return &ent.BenchmarkRun{
			ID:                 501,
			SuiteID:            input.SuiteID,
			ProfileID:          input.ProfileID,
			Status:             input.Status,
			TriggerType:        input.TriggerType,
			TaskTypes:          input.TaskTypes,
			SelectionSeed:      input.SelectionSeed,
			PlannedTargetCount: input.PlannedTargetCount,
			PlannedTaskCount:   input.PlannedTaskCount,
			PlannedResultCount: input.PlannedResultCount,
			ConfigSnapshot:     input.ConfigSnapshot,
			CreatedBy:          input.CreatedBy,
		}, nil
	}

	svc := NewBenchmarkService(repo)
	run, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{
		ProfileID: profile.ID,
		CreatedBy: &createdBy,
	})
	require.NoError(t, err)
	require.Equal(t, int64(501), run.ID)
	require.Equal(t, BenchmarkRunStatusQueued, run.Status)
	require.Equal(t, "manual", run.TriggerType)

	require.Equal(t, profile.SuiteID, gotInput.SuiteID)
	require.Equal(t, profile.ID, gotInput.ProfileID)
	require.Equal(t, BenchmarkRunStatusQueued, gotInput.Status)
	require.Equal(t, "manual", gotInput.TriggerType)
	require.Equal(t, BenchmarkTaskScaleSmall, gotInput.TaskScale)
	require.Equal(t, []string{"reasoning"}, gotInput.TaskTypes)
	require.Equal(t, int64Ptr(7), gotInput.SelectionSeed)
	require.Equal(t, 2, gotInput.PlannedTargetCount)
	require.Equal(t, 2, gotInput.PlannedTaskCount)
	require.Equal(t, 4, gotInput.PlannedResultCount)
	require.Equal(t, &createdBy, gotInput.CreatedBy)

	require.Len(t, gotInput.Targets, 2)
	require.Equal(t, BenchmarkRunTargetInput{
		TargetID:            101,
		ModelName:           "gpt-4.1",
		ChannelID:           11,
		DisplayNameSnapshot: "GPT 4.1",
		ChannelNameSnapshot: "primary-openai",
		ProviderSnapshot:    "openai",
		TargetOrder:         1,
		ConfigSnapshot: map[string]any{
			"supported_task_types": []string{"reasoning", "coding"},
			"max_concurrency":      3,
			"enabled":              true,
			"public_visible":       true,
		},
	}, gotInput.Targets[0])
	require.Equal(t, BenchmarkRunTargetInput{
		TargetID:            102,
		ModelName:           "claude-sonnet-4",
		ChannelID:           12,
		DisplayNameSnapshot: "Claude Sonnet 4",
		ChannelNameSnapshot: "primary-anthropic",
		ProviderSnapshot:    "anthropic",
		TargetOrder:         2,
		ConfigSnapshot: map[string]any{
			"supported_task_types": []string{"reasoning"},
			"max_concurrency":      2,
			"enabled":              true,
			"public_visible":       true,
		},
	}, gotInput.Targets[1])

	require.Len(t, gotInput.Tasks, 2)
	require.Equal(t, int64(201), gotInput.Tasks[0].TaskID)
	require.Equal(t, 1, gotInput.Tasks[0].TaskOrder)
	require.Equal(t, "reasoning prompt 1", gotInput.Tasks[0].PromptSnapshot)
	require.Equal(t, "exact_match", gotInput.Tasks[0].VerifierTypeSnapshot)
	require.Equal(t, map[string]any{"field": "answer"}, gotInput.Tasks[0].VerifierConfigSnapshot)
	require.InDelta(t, 1.25, gotInput.Tasks[0].WeightSnapshot, 0.000001)
	require.Equal(t, "reasoning prompt 1", gotInput.Tasks[0].TaskSnapshot["prompt"])
	require.Equal(t, []string{"public"}, gotInput.Tasks[0].TaskSnapshot["tags"])
	require.Equal(t, int64(204), gotInput.Tasks[1].TaskID)
	require.Equal(t, 2, gotInput.Tasks[1].TaskOrder)
	require.Equal(t, "reasoning prompt 3", gotInput.Tasks[1].PromptSnapshot)
	require.Equal(t, "exact_match", gotInput.Tasks[1].VerifierTypeSnapshot)
	require.Equal(t, map[string]any{"field": "answer"}, gotInput.Tasks[1].VerifierConfigSnapshot)
	require.InDelta(t, 1.75, gotInput.Tasks[1].WeightSnapshot, 0.000001)

	require.Equal(t, profile.ID, gotInput.ConfigSnapshot["profile_id"])
	require.Equal(t, BenchmarkTaskScaleSmall, gotInput.ConfigSnapshot["task_scale"])
	require.Equal(t, []string{"reasoning"}, gotInput.ConfigSnapshot["task_types"])
	require.Equal(t, int64(7), gotInput.ConfigSnapshot["selection_seed"])
	require.Equal(t, 5, gotInput.ConfigSnapshot["task_count_limit"])
	require.Equal(t, map[string]int{}, gotInput.ConfigSnapshot["per_type_limit"])
	require.Equal(t, "balanced", gotInput.ConfigSnapshot["sampling_strategy"])
	require.Equal(t, "ability_score_only", gotInput.ConfigSnapshot["ranking_basis"])
	require.Equal(t, map[string]any{"timeout": 30}, gotInput.ConfigSnapshot["runtime_config"])
	require.Equal(t, map[string]any{"mode": "strict"}, gotInput.ConfigSnapshot["scoring_config"])

	runtimeSnapshot, ok := gotInput.ConfigSnapshot["runtime_config"].(map[string]any)
	require.True(t, ok)
	profile.RuntimeConfig["timeout"] = 15
	require.Equal(t, 30, runtimeSnapshot["timeout"])
	runtimeSnapshot["timeout"] = 99
	require.Equal(t, 15, profile.RuntimeConfig["timeout"])
}

func TestBenchmarkServiceCreateRunRejectsDisabledProfile(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	disabled := *profile
	disabled.Enabled = false
	repo.getProfileFn = func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
		require.Equal(t, profile.ID, id)
		return &disabled, nil
	}

	svc := NewBenchmarkService(repo)
	run, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{ProfileID: profile.ID})
	require.Nil(t, run)
	require.EqualError(t, err, "benchmark profile is disabled")
}

func TestBenchmarkServiceCreateRunRejectsNoSelectedTasks(t *testing.T) {
	t.Parallel()

	repo, profile := newBenchmarkPreviewRepoStub(t)
	svc := NewBenchmarkService(repo)

	run, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{
		ProfileID: profile.ID,
		Override: BenchmarkProfilePreviewInput{
			DifficultyFilter: []string{"impossible"},
		},
	})
	require.Nil(t, run)
	require.EqualError(t, err, "no benchmark tasks selected")
}

func newBenchmarkPreviewRepoStub(t *testing.T) (*benchmarkServiceRepoStub, *ent.BenchmarkProfile) {
	t.Helper()

	profile := &ent.BenchmarkProfile{
		ID:               77,
		SuiteID:          9,
		TargetIds:        []int64{101, 102},
		TaskTypes:        []string{"reasoning"},
		TaskScale:        benchmarkprofile.TaskScale(BenchmarkTaskScaleSmall),
		TaskCountLimit:   intPtr(5),
		PerTypeLimit:     map[string]int{},
		DifficultyFilter: []string{"easy"},
		TagFilter:        []string{"public"},
		SamplingStrategy: "balanced",
		SelectionSeed:    int64Ptr(7),
		RuntimeConfig:    map[string]any{"timeout": 30},
		ScoringConfig:    map[string]any{"mode": "strict"},
		Enabled:          true,
	}
	targets := []*ent.BenchmarkTarget{
		{
			ID:                  101,
			ModelName:           "gpt-4.1",
			ChannelID:           11,
			DisplayName:         benchmarkStringPtr("GPT 4.1"),
			ProviderSnapshot:    benchmarkStringPtr("openai"),
			ChannelNameSnapshot: benchmarkStringPtr("primary-openai"),
			SupportedTaskTypes:  []string{"reasoning", "coding"},
			MaxConcurrency:      3,
			Enabled:             true,
			PublicVisible:       true,
		},
		{
			ID:                  102,
			ModelName:           "claude-sonnet-4",
			ChannelID:           12,
			DisplayName:         benchmarkStringPtr("Claude Sonnet 4"),
			ProviderSnapshot:    benchmarkStringPtr("anthropic"),
			ChannelNameSnapshot: benchmarkStringPtr("primary-anthropic"),
			SupportedTaskTypes:  []string{"reasoning"},
			MaxConcurrency:      2,
			Enabled:             true,
			PublicVisible:       true,
		},
	}
	tasks := []*ent.BenchmarkTask{
		{
			ID:           201,
			SuiteID:      9,
			Title:        "reasoning easy public",
			Type:         "reasoning",
			Category:     benchmarkStringPtr("logic"),
			Difficulty:   benchmarkStringPtr("easy"),
			Tags:         []string{"public"},
			Prompt:       "reasoning prompt 1",
			InputPayload: map[string]any{"question": "1+1"},
			ExpectedOutput: map[string]any{
				"answer": "2",
			},
			VerifierType: "exact_match",
			VerifierConfig: map[string]any{
				"field": "answer",
			},
			Weight:   1.25,
			MinScale: benchmarktask.MinScale(BenchmarkTaskScaleSmall),
			Enabled:  true,
		},
		{
			ID:           202,
			SuiteID:      9,
			Title:        "reasoning hard public",
			Type:         "reasoning",
			Category:     benchmarkStringPtr("logic"),
			Difficulty:   benchmarkStringPtr("hard"),
			Tags:         []string{"public"},
			Prompt:       "reasoning prompt 2",
			InputPayload: map[string]any{"question": "hard"},
			ExpectedOutput: map[string]any{
				"answer": "hard",
			},
			VerifierType: "exact_match",
			VerifierConfig: map[string]any{
				"field": "answer",
			},
			Weight:   1.5,
			MinScale: benchmarktask.MinScale(BenchmarkTaskScaleMedium),
			Enabled:  true,
		},
		{
			ID:           203,
			SuiteID:      9,
			Title:        "coding medium code",
			Type:         "coding",
			Category:     benchmarkStringPtr("go"),
			Difficulty:   benchmarkStringPtr("medium"),
			Tags:         []string{"code"},
			Prompt:       "coding prompt",
			InputPayload: map[string]any{"language": "go"},
			ExpectedOutput: map[string]any{
				"passes": true,
			},
			VerifierType: "json_contains",
			VerifierConfig: map[string]any{
				"field": "passes",
			},
			Weight:   2,
			MinScale: benchmarktask.MinScale(BenchmarkTaskScaleMedium),
			Enabled:  true,
		},
		{
			ID:           204,
			SuiteID:      9,
			Title:        "reasoning easy public full",
			Type:         "reasoning",
			Category:     benchmarkStringPtr("logic"),
			Difficulty:   benchmarkStringPtr("easy"),
			Tags:         []string{"public"},
			Prompt:       "reasoning prompt 3",
			InputPayload: map[string]any{"question": "2+2"},
			ExpectedOutput: map[string]any{
				"answer": "4",
			},
			VerifierType: "exact_match",
			VerifierConfig: map[string]any{
				"field": "answer",
			},
			Weight:   1.75,
			MinScale: benchmarktask.MinScale(BenchmarkTaskScaleSmall),
			Enabled:  true,
		},
		{
			ID:           205,
			SuiteID:      9,
			Title:        "disabled reasoning",
			Type:         "reasoning",
			Category:     benchmarkStringPtr("logic"),
			Difficulty:   benchmarkStringPtr("easy"),
			Tags:         []string{"public"},
			Prompt:       "disabled prompt",
			VerifierType: "exact_match",
			Weight:       1,
			MinScale:     benchmarktask.MinScale(BenchmarkTaskScaleSmall),
			Enabled:      false,
		},
	}

	repo := newBenchmarkServiceRepoStub(t)
	repo.getProfileFn = func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
		require.Equal(t, profile.ID, id)
		return profile, nil
	}
	repo.listTargetsByIDsFn = func(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error) {
		byID := make(map[int64]*ent.BenchmarkTarget, len(targets))
		for _, target := range targets {
			byID[target.ID] = target
		}
		out := make([]*ent.BenchmarkTarget, 0, len(ids))
		for _, id := range ids {
			target, ok := byID[id]
			if !ok {
				return nil, errors.New("missing target")
			}
			out = append(out, target)
		}
		return out, nil
	}
	repo.listEnabledTasksForSuiteFn = func(ctx context.Context, suiteID int64) ([]*ent.BenchmarkTask, error) {
		require.Equal(t, profile.SuiteID, suiteID)
		return tasks, nil
	}
	return repo, profile
}

func benchmarkStringPtr(v string) *string {
	return &v
}
