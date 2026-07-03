package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type benchmarkRuntimeSettingRepoStub struct {
	values         map[string]string
	getMultipleErr error
}

func (s *benchmarkRuntimeSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *benchmarkRuntimeSettingRepoStub) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *benchmarkRuntimeSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *benchmarkRuntimeSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.getMultipleErr != nil {
		return nil, s.getMultipleErr
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *benchmarkRuntimeSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *benchmarkRuntimeSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *benchmarkRuntimeSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestSettingServiceGetBenchmarkRuntimeFailsClosedOnRepoError(t *testing.T) {
	t.Parallel()

	svc := NewSettingService(&benchmarkRuntimeSettingRepoStub{
		getMultipleErr: errors.New("boom"),
	}, &config.Config{})

	runtime := svc.GetBenchmarkRuntime(context.Background())
	require.False(t, runtime.Enabled)
	require.False(t, runtime.PublicEnabled)
	require.Equal(t, BenchmarkGlobalConcurrencyDefault, runtime.GlobalConcurrency)
	require.Equal(t, BenchmarkDefaultTimeoutSecondsDefault, runtime.DefaultTimeoutSeconds)
}

func TestSettingServiceGetBenchmarkRuntimeParsesEnabledFlags(t *testing.T) {
	t.Parallel()

	svc := NewSettingService(&benchmarkRuntimeSettingRepoStub{
		values: map[string]string{
			SettingKeyBenchmarkEnabled:               "true",
			SettingKeyBenchmarkPublicEnabled:         "true",
			SettingKeyBenchmarkGlobalConcurrency:     "7",
			SettingKeyBenchmarkDefaultTimeoutSeconds: "180",
		},
	}, &config.Config{})

	runtime := svc.GetBenchmarkRuntime(context.Background())
	require.True(t, runtime.Enabled)
	require.True(t, runtime.PublicEnabled)
	require.Equal(t, 7, runtime.GlobalConcurrency)
	require.Equal(t, 180, runtime.DefaultTimeoutSeconds)
}
