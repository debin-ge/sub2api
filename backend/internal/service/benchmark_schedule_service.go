package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/robfig/cron/v3"
)

var benchmarkScheduleCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type benchmarkScheduleRunCreator interface {
	CreateRun(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
}

type benchmarkScheduleSettingProvider interface {
	GetAllSettings(ctx context.Context) (*SystemSettings, error)
}

type BenchmarkScheduleService struct {
	repo            BenchmarkRepository
	runCreator      benchmarkScheduleRunCreator
	settingProvider benchmarkScheduleSettingProvider
}

func NewBenchmarkScheduleService(repo BenchmarkRepository, runCreator benchmarkScheduleRunCreator) *BenchmarkScheduleService {
	return &BenchmarkScheduleService{
		repo:       repo,
		runCreator: runCreator,
	}
}

func (s *BenchmarkScheduleService) SetSettingProvider(provider benchmarkScheduleSettingProvider) {
	s.settingProvider = provider
}

func (s *BenchmarkScheduleService) SetSettingService(settingService *SettingService) {
	s.SetSettingProvider(settingService)
}

func NormalizeBenchmarkScheduleListInput(input BenchmarkScheduleListInput) BenchmarkScheduleListInput {
	input.BenchmarkListInput = NormalizeBenchmarkListInput(input.BenchmarkListInput)
	return input
}

func ComputeNextRunAt(expr string, from time.Time) (time.Time, error) {
	schedule, err := benchmarkScheduleCronParser.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(from), nil
}

func (s *BenchmarkScheduleService) ListSchedules(ctx context.Context, input BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error) {
	return s.repo.ListSchedules(ctx, NormalizeBenchmarkScheduleListInput(input))
}

func (s *BenchmarkScheduleService) CreateSchedule(ctx context.Context, input BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
	if input.ProfileID <= 0 {
		return nil, errors.New("profile id must be positive")
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, errors.New("schedule name is required")
	}
	if strings.TrimSpace(input.CronExpr) == "" {
		return nil, errors.New("cron expr is required")
	}
	nextRunAt, err := ComputeNextRunAt(input.CronExpr, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	input.NextRunAt = &nextRunAt
	return s.repo.CreateSchedule(ctx, input)
}

func (s *BenchmarkScheduleService) GetSchedule(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error) {
	return s.repo.GetSchedule(ctx, id)
}

func (s *BenchmarkScheduleService) TriggerSchedule(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error) {
	schedule, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.triggerSchedule(ctx, schedule, now)
}

func (s *BenchmarkScheduleService) TriggerDue(ctx context.Context, now time.Time) (int, error) {
	enabled, err := s.isScheduleEnabled(ctx)
	if err != nil || !enabled {
		return 0, nil
	}

	schedules, err := s.repo.ListDueSchedules(ctx, now)
	if err != nil {
		return 0, err
	}

	triggered := 0
	for _, schedule := range schedules {
		if _, err := s.triggerSchedule(ctx, schedule, now); err != nil {
			return triggered, err
		}
		triggered++
	}
	return triggered, nil
}

func (s *BenchmarkScheduleService) triggerSchedule(ctx context.Context, schedule *ent.BenchmarkSchedule, now time.Time) (*ent.BenchmarkRun, error) {
	run, err := s.runCreator.CreateRun(ctx, BenchmarkCreateRunRequest{
		ProfileID:   schedule.ProfileID,
		TriggerType: "scheduled",
	})
	if err != nil {
		return nil, err
	}

	nextRunAt, err := ComputeNextRunAt(schedule.CronExpr, now)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	if err := s.repo.UpdateScheduleAfterRun(ctx, schedule.ID, now, nextRunAt); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *BenchmarkScheduleService) isScheduleEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settingProvider == nil {
		return false, nil
	}
	settings, err := s.settingProvider.GetAllSettings(ctx)
	if err != nil {
		return false, err
	}
	if settings == nil {
		return false, nil
	}
	return settings.BenchmarkScheduleEnabled, nil
}
