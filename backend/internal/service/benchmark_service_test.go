package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type benchmarkServiceRepoStub struct {
	createSuiteFn   func(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	listSuitesFn    func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	createTargetFn  func(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	listTargetsFn   func(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	createTaskFn    func(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	listTasksFn     func(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	createProfileFn func(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	getProfileFn    func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
}

func (s *benchmarkServiceRepoStub) CreateSuite(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	if s.createSuiteFn != nil {
		return s.createSuiteFn(ctx, input)
	}
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListSuites(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	if s.listSuitesFn != nil {
		return s.listSuitesFn(ctx, input)
	}
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if s.createTargetFn != nil {
		return s.createTargetFn(ctx, input)
	}
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	if s.listTargetsFn != nil {
		return s.listTargetsFn(ctx, input)
	}
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if s.createTaskFn != nil {
		return s.createTaskFn(ctx, input)
	}
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	if s.listTasksFn != nil {
		return s.listTasksFn(ctx, input)
	}
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) CreateProfile(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	if s.createProfileFn != nil {
		return s.createProfileFn(ctx, input)
	}
	return nil, nil
}

func (s *benchmarkServiceRepoStub) GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
	if s.getProfileFn != nil {
		return s.getProfileFn(ctx, id)
	}
	return nil, nil
}

func (s *benchmarkServiceRepoStub) CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
	return nil, nil
}

func (s *benchmarkServiceRepoStub) ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	return nil, 0, nil
}

func (s *benchmarkServiceRepoStub) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	return nil, nil
}

func (s *benchmarkServiceRepoStub) UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error {
	return nil
}

func (s *benchmarkServiceRepoStub) SaveScoreSnapshots(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error {
	return nil
}

func (s *benchmarkServiceRepoStub) PublishPublicSnapshot(ctx context.Context, input BenchmarkPublicSnapshotInput) error {
	return nil
}

func (s *benchmarkServiceRepoStub) GetLatestPublicSnapshot(ctx context.Context) (*ent.BenchmarkPublicSnapshot, error) {
	return nil, nil
}

func TestBenchmarkServiceCreateTargetRejectsEmptyModelName(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "",
		ChannelID: 1,
	})
	require.EqualError(t, err, "model name is required")
}

func TestBenchmarkServiceCreateTargetRejectsInvalidChannelID(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		ChannelID: 0,
	})
	require.EqualError(t, err, "channel id must be positive")
}

func TestBenchmarkServiceCreateTaskRejectsEmptyType(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID: 1,
		Title:   "task",
		Type:    "",
	})
	require.EqualError(t, err, "task type is required")
}

func TestBenchmarkServiceCreateTaskRejectsInvalidMinScale(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateTask(context.Background(), BenchmarkTaskInput{
		SuiteID:      1,
		Title:        "task",
		Type:         "reasoning",
		MinScale:     "giant",
		PublicPrompt: true,
	})
	require.EqualError(t, err, "unsupported task scale")
}

func TestBenchmarkServiceCreateProfileRejectsEmptyTargets(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TaskTypes: []string{"reasoning"},
	})
	require.EqualError(t, err, "at least one target is required")
}

func TestBenchmarkServiceCreateProfileRejectsEmptyTaskTypes(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TargetIDs: []int64{1},
	})
	require.EqualError(t, err, "at least one task type is required")
}

func TestBenchmarkServiceCreateProfileRejectsInvalidTaskScale(t *testing.T) {
	svc := NewBenchmarkService(&benchmarkServiceRepoStub{})

	_, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   1,
		Name:      "profile",
		TargetIDs: []int64{1},
		TaskTypes: []string{"reasoning"},
		TaskScale: "giant",
	})
	require.EqualError(t, err, "unsupported task scale")
}

func TestBenchmarkServiceCreateProfileAcceptsValidInput(t *testing.T) {
	var gotInput BenchmarkProfileInput
	want := &ent.BenchmarkProfile{ID: 123}
	repo := &benchmarkServiceRepoStub{
		createProfileFn: func(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
			gotInput = input
			return want, nil
		},
	}

	svc := NewBenchmarkService(repo)
	got, err := svc.CreateProfile(context.Background(), BenchmarkProfileInput{
		SuiteID:   42,
		Name:      "profile",
		TargetIDs: []int64{7, 8},
		TaskTypes: []string{"reasoning", "coding"},
		TaskScale: "",
		Enabled:   true,
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, BenchmarkProfileInput{
		SuiteID:   42,
		Name:      "profile",
		TargetIDs: []int64{7, 8},
		TaskTypes: []string{"reasoning", "coding"},
		TaskScale: "",
		Enabled:   true,
	}, gotInput)
}
