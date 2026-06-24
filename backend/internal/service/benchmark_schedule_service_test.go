package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type benchmarkScheduleRunCreatorStub struct {
	createRunFn func(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
}

func (s *benchmarkScheduleRunCreatorStub) CreateRun(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	return s.createRunFn(ctx, input)
}

type benchmarkScheduleSettingProviderStub struct {
	getAllSettingsFn func(ctx context.Context) (*SystemSettings, error)
}

func (s *benchmarkScheduleSettingProviderStub) GetAllSettings(ctx context.Context) (*SystemSettings, error) {
	return s.getAllSettingsFn(ctx)
}

func TestBenchmarkScheduleComputesNextRunAt(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 6, 24, 10, 7, 0, 0, time.UTC)

	nextRunAt, err := ComputeNextRunAt("*/15 * * * *", from)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 6, 24, 10, 15, 0, 0, time.UTC), nextRunAt)
}

func TestBenchmarkScheduleRejectsInvalidCron(t *testing.T) {
	t.Parallel()

	_, err := ComputeNextRunAt("not-a-cron", time.Date(2026, 6, 24, 10, 7, 0, 0, time.UTC))
	require.Error(t, err)
}

func TestBenchmarkScheduleTriggerDueSkipsWhenDisabled(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkServiceRepoStub(t)
	runCreator := &benchmarkScheduleRunCreatorStub{
		createRunFn: func(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			t.Fatal("CreateRun should not be called when disabled")
			return nil, nil
		},
	}

	svc := NewBenchmarkScheduleService(repo, runCreator)
	svc.SetSettingProvider(&benchmarkScheduleSettingProviderStub{
		getAllSettingsFn: func(ctx context.Context) (*SystemSettings, error) {
			return &SystemSettings{BenchmarkScheduleEnabled: false}, nil
		},
	})

	count, err := svc.TriggerDue(context.Background(), time.Date(2026, 6, 24, 10, 7, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestBenchmarkScheduleTriggerScheduleCreatesScheduledRunAndAdvancesNextRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 7, 0, 0, time.UTC)
	repo := newBenchmarkServiceRepoStub(t)
	repo.getScheduleFn = func(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error) {
		require.Equal(t, int64(42), id)
		return &ent.BenchmarkSchedule{
			ID:        42,
			ProfileID: 88,
			Name:      "nightly",
			CronExpr:  "*/15 * * * *",
			Enabled:   false,
		}, nil
	}
	repo.updateScheduleAfterRunFn = func(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
		require.Equal(t, int64(42), id)
		require.Equal(t, now, lastRunAt)
		require.Equal(t, time.Date(2026, 6, 24, 10, 15, 0, 0, time.UTC), nextRunAt)
		return nil
	}

	runCreator := &benchmarkScheduleRunCreatorStub{
		createRunFn: func(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(88), input.ProfileID)
			require.Equal(t, "scheduled", input.TriggerType)
			require.Nil(t, input.CreatedBy)
			require.Equal(t, BenchmarkProfilePreviewInput{}, input.Override)
			return &ent.BenchmarkRun{ID: 99, ProfileID: 88, TriggerType: "scheduled"}, nil
		},
	}

	svc := NewBenchmarkScheduleService(repo, runCreator)
	run, err := svc.TriggerSchedule(context.Background(), 42, now)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, int64(99), run.ID)
}

func TestBenchmarkScheduleTriggerDueSkipsWhenSettingsReadFails(t *testing.T) {
	t.Parallel()

	repo := newBenchmarkServiceRepoStub(t)
	runCreator := &benchmarkScheduleRunCreatorStub{
		createRunFn: func(ctx context.Context, input BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			t.Fatal("CreateRun should not be called when settings read fails")
			return nil, nil
		},
	}

	svc := NewBenchmarkScheduleService(repo, runCreator)
	svc.SetSettingProvider(&benchmarkScheduleSettingProviderStub{
		getAllSettingsFn: func(ctx context.Context) (*SystemSettings, error) {
			return nil, errors.New("boom")
		},
	})

	count, err := svc.TriggerDue(context.Background(), time.Date(2026, 6, 24, 10, 7, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, 0, count)
}
