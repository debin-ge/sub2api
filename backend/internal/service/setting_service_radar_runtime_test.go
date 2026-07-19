package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type radarRuntimeDoneObservedContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *radarRuntimeDoneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type radarRuntimeCoalescingRepo struct {
	*radarRuntimeSettingRepo
	reads   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (r *radarRuntimeCoalescingRepo) GetValue(context.Context, string) (string, error) {
	read := r.reads.Add(1)
	if read == 1 {
		close(r.entered)
		<-r.release
		return "true", nil
	}
	return "false", nil
}

type radarRuntimeStaleReadRepo struct {
	*radarRuntimeSettingRepo
	reads   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (r *radarRuntimeStaleReadRepo) GetValue(context.Context, string) (string, error) {
	switch r.reads.Add(1) {
	case 1:
		return "true", nil
	case 2:
		close(r.entered)
		<-r.release
		return "true", nil
	default:
		return "", errors.New("database unavailable")
	}
}

type radarRuntimeSettingRepo struct {
	mu       sync.Mutex
	values   []string
	errors   []error
	reads    int
	writes   []string
	setError error
}

func (r *radarRuntimeSettingRepo) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (r *radarRuntimeSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyRadarEnabled {
		panic("unexpected setting key: " + key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.reads
	r.reads++
	if len(r.values) == 0 {
		return "", ErrSettingNotFound
	}
	if index >= len(r.values) {
		index = len(r.values) - 1
	}
	var err error
	if len(r.errors) > 0 {
		errorIndex := index
		if errorIndex >= len(r.errors) {
			errorIndex = len(r.errors) - 1
		}
		err = r.errors[errorIndex]
	}
	return r.values[index], err
}

func (r *radarRuntimeSettingRepo) Set(_ context.Context, key, value string) error {
	if key != SettingKeyRadarEnabled {
		panic("unexpected setting key: " + key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setError != nil {
		return r.setError
	}
	r.writes = append(r.writes, value)
	return nil
}

func (*radarRuntimeSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (*radarRuntimeSettingRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (*radarRuntimeSettingRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (*radarRuntimeSettingRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestSettingKeyRadarEnabledCanonicalValue(t *testing.T) {
	require.Equal(t, "radar_enabled", SettingKeyRadarEnabled)
}

func TestRadarEnabledRuntimeResolutionMatrix(t *testing.T) {
	databaseFailure := errors.New("database unavailable")
	tests := []struct {
		name          string
		staticEnabled bool
		value         string
		err           error
		want          bool
	}{
		{name: "canonical true", value: "true", want: true},
		{name: "canonical false", staticEnabled: true, value: "false", want: false},
		{name: "missing falls back to static true", staticEnabled: true, err: ErrSettingNotFound, want: true},
		{name: "missing falls back to static false", err: ErrSettingNotFound, want: false},
		{name: "cold database error fails closed", staticEnabled: true, err: databaseFailure, want: false},
		{name: "cold uppercase value fails closed", staticEnabled: true, value: "TRUE", want: false},
		{name: "cold padded value fails closed", staticEnabled: true, value: " true ", want: false},
		{name: "cold numeric value fails closed", staticEnabled: true, value: "1", want: false},
		{name: "cold empty stored value fails closed", staticEnabled: true, value: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &radarRuntimeSettingRepo{values: []string{tt.value}, errors: []error{tt.err}}
			cfg := &config.Config{}
			cfg.Radar.Enabled = tt.staticEnabled
			svc := NewSettingService(repo, cfg)

			require.Equal(t, tt.want, svc.IsRadarEnabled(context.Background()))
			require.Empty(t, repo.writes, "read must not materialize a default setting row")
		})
	}
}

func TestRadarEnabledLastKnownGoodAndSetterSemantics(t *testing.T) {
	databaseFailure := errors.New("database unavailable")
	repo := &radarRuntimeSettingRepo{
		values: []string{"true", "", "invalid", ""},
		errors: []error{nil, databaseFailure, nil, databaseFailure},
	}
	svc := NewSettingService(repo, &config.Config{})

	require.True(t, svc.IsRadarEnabled(context.Background()))
	require.True(t, svc.IsRadarEnabled(context.Background()), "DB errors must use the instance LKG")
	require.True(t, svc.IsRadarEnabled(context.Background()), "invalid stored values must use the instance LKG")
	require.NoError(t, svc.SetRadarEnabled(context.Background(), false))
	require.False(t, svc.IsRadarEnabled(context.Background()), "a successful setter must update the LKG immediately")
	require.Equal(t, []string{"false"}, repo.writes)

	repo.setError = databaseFailure
	require.ErrorIs(t, svc.SetRadarEnabled(context.Background(), true), databaseFailure)
	require.False(t, svc.IsRadarEnabled(context.Background()), "a failed setter must not update the LKG")
	require.Equal(t, []string{"false"}, repo.writes)
}

func TestRadarEnabledLastKnownGoodIsConcurrentAndInstanceLocal(t *testing.T) {
	databaseFailure := errors.New("database unavailable")
	trueRepo := &radarRuntimeSettingRepo{values: []string{"true", ""}, errors: []error{nil, databaseFailure}}
	falseRepo := &radarRuntimeSettingRepo{values: []string{"false", ""}, errors: []error{nil, databaseFailure}}
	trueService := NewSettingService(trueRepo, &config.Config{})
	falseService := NewSettingService(falseRepo, &config.Config{})
	require.True(t, trueService.IsRadarEnabled(context.Background()))
	require.False(t, falseService.IsRadarEnabled(context.Background()))

	const goroutines = 32
	var wg sync.WaitGroup
	var incorrect atomic.Int32
	wg.Add(goroutines * 2)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range 100 {
				if !trueService.IsRadarEnabled(context.Background()) {
					incorrect.Add(1)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 100 {
				if falseService.IsRadarEnabled(context.Background()) {
					incorrect.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	require.Zero(t, incorrect.Load())
}

func TestRadarEnabledConcurrentReadsShareOneRepositoryCall(t *testing.T) {
	repo := &radarRuntimeCoalescingRepo{
		radarRuntimeSettingRepo: &radarRuntimeSettingRepo{},
		entered:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
	svc := NewSettingService(repo, &config.Config{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(release)

	leaderDone := make(chan bool, 1)
	go func() { leaderDone <- svc.IsRadarEnabled(context.Background()) }()
	<-repo.entered

	const waiters = 32
	results := make(chan bool, waiters)
	observed := make([]chan struct{}, 0, waiters)
	for range waiters {
		observedDone := make(chan struct{})
		observed = append(observed, observedDone)
		ctx := &radarRuntimeDoneObservedContext{
			Context:  context.Background(),
			observed: observedDone,
		}
		go func() { results <- svc.IsRadarEnabled(ctx) }()
	}
	for _, waiterObserved := range observed {
		select {
		case <-waiterObserved:
		case <-time.After(time.Second):
			t.Fatal("concurrent reader did not join the in-flight repository read")
		}
	}

	release()
	require.True(t, <-leaderDone)
	for range waiters {
		require.True(t, <-results)
	}
	require.EqualValues(t, 1, repo.reads.Load(), "one concurrent wave must issue one repository read")
}

func TestRadarEnabledCanceledWaiterReturnsLastKnownGoodWithoutWaitingForRead(t *testing.T) {
	repo := &radarRuntimeCoalescingRepo{
		radarRuntimeSettingRepo: &radarRuntimeSettingRepo{},
		entered:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
	svc := NewSettingService(repo, &config.Config{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(release)

	leaderDone := make(chan bool, 1)
	go func() { leaderDone <- svc.IsRadarEnabled(context.Background()) }()
	<-repo.entered

	baseCtx, cancel := context.WithCancel(context.Background())
	observedDone := make(chan struct{})
	ctx := &radarRuntimeDoneObservedContext{Context: baseCtx, observed: observedDone}
	waiterDone := make(chan bool, 1)
	go func() { waiterDone <- svc.IsRadarEnabled(ctx) }()
	select {
	case <-observedDone:
	case <-time.After(time.Second):
		t.Fatal("waiter did not begin its context-aware wait")
	}
	cancel()

	var waiterResult bool
	select {
	case waiterResult = <-waiterDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("canceled waiter remained blocked behind the in-flight repository read")
	}
	require.False(t, waiterResult, "a canceled cold waiter must fail closed")
	require.EqualValues(t, 1, repo.reads.Load())

	release()
	require.True(t, <-leaderDone)
}

func TestRadarEnabledDeadlineWaiterReturnsLastKnownGoodWithoutWaitingForRead(t *testing.T) {
	repo := &radarRuntimeStaleReadRepo{
		radarRuntimeSettingRepo: &radarRuntimeSettingRepo{},
		entered:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
	svc := NewSettingService(repo, &config.Config{})
	require.True(t, svc.IsRadarEnabled(context.Background()))
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(release)

	leaderDone := make(chan bool, 1)
	go func() { leaderDone <- svc.IsRadarEnabled(context.Background()) }()
	<-repo.entered

	baseCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	observedDone := make(chan struct{})
	ctx := &radarRuntimeDoneObservedContext{Context: baseCtx, observed: observedDone}
	waiterDone := make(chan bool, 1)
	go func() { waiterDone <- svc.IsRadarEnabled(ctx) }()
	select {
	case <-observedDone:
	case <-time.After(time.Second):
		t.Fatal("waiter did not begin its context-aware wait")
	}

	select {
	case result := <-waiterDone:
		require.True(t, result, "a deadline waiter must return the instance LKG")
	case <-time.After(time.Second):
		t.Fatal("deadline waiter remained blocked behind the in-flight repository read")
	}
	require.EqualValues(t, 2, repo.reads.Load())

	release()
	require.True(t, <-leaderDone)
}

func TestRadarEnabledSuccessfulSetterCannotBeOverwrittenByOlderRead(t *testing.T) {
	repo := &radarRuntimeStaleReadRepo{
		radarRuntimeSettingRepo: &radarRuntimeSettingRepo{},
		entered:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
	svc := NewSettingService(repo, &config.Config{})
	require.True(t, svc.IsRadarEnabled(context.Background()))

	readDone := make(chan bool, 1)
	go func() {
		readDone <- svc.IsRadarEnabled(context.Background())
	}()
	<-repo.entered

	setDone := make(chan error, 1)
	go func() { setDone <- svc.SetRadarEnabled(context.Background(), false) }()
	select {
	case err := <-setDone:
		require.NoError(t, err)
	case <-time.After(250 * time.Millisecond):
		close(repo.release)
		<-readDone
		t.Fatal("setter remained blocked behind an older repository read")
	}
	require.False(t, svc.radarEnabledLastKnownGoodOrFalse())

	close(repo.release)
	require.False(t, <-readDone, "an older read must resolve to the newer successful setter value")
	require.False(t, svc.IsRadarEnabled(context.Background()), "the completed setter must remain the latest known-good value")
}
