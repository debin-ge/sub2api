package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/ent"
)

type BenchmarkService struct {
	repo BenchmarkRepository
}

func NewBenchmarkService(repo BenchmarkRepository) *BenchmarkService {
	return &BenchmarkService{repo: repo}
}

func (s *BenchmarkService) CreateSuite(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	return s.repo.CreateSuite(ctx, input)
}

func (s *BenchmarkService) ListSuites(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	return s.repo.ListSuites(ctx, input)
}

func (s *BenchmarkService) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	if input.ModelName == "" {
		return nil, errors.New("model name is required")
	}
	if input.ChannelID <= 0 {
		return nil, errors.New("channel id must be positive")
	}
	return s.repo.CreateTarget(ctx, input)
}

func (s *BenchmarkService) ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	return s.repo.ListTargets(ctx, input)
}

func (s *BenchmarkService) CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	if input.Type == "" {
		return nil, errors.New("task type is required")
	}
	if !isSupportedBenchmarkTaskScale(input.MinScale) {
		return nil, errors.New("unsupported task scale")
	}
	return s.repo.CreateTask(ctx, input)
}

func (s *BenchmarkService) ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	return s.repo.ListTasks(ctx, input)
}

func (s *BenchmarkService) CreateProfile(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	if len(input.TargetIDs) == 0 {
		return nil, errors.New("at least one target is required")
	}
	if len(input.TaskTypes) == 0 {
		return nil, errors.New("at least one task type is required")
	}
	if !isSupportedBenchmarkTaskScale(input.TaskScale) {
		return nil, errors.New("unsupported task scale")
	}
	return s.repo.CreateProfile(ctx, input)
}

func (s *BenchmarkService) GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
	return s.repo.GetProfile(ctx, id)
}

func isSupportedBenchmarkTaskScale(scale string) bool {
	switch scale {
	case "", BenchmarkTaskScaleSmall, BenchmarkTaskScaleMedium, BenchmarkTaskScaleFull, BenchmarkTaskScaleCustom:
		return true
	default:
		return false
	}
}
