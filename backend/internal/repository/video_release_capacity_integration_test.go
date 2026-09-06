//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoReleaseSmallConcurrentAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 25)
	const concurrency = 8
	results := make(chan error, concurrency)
	start := make(chan struct{})
	started, before := time.Now(), integrationDB.Stats()
	for i := 0; i < concurrency; i++ {
		go func() {
			<-start
			_, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), uuid.NewString(), uuid.NewString(), 1))
			results <- err
		}()
	}
	close(start)
	var failures []error
	for i := 0; i < concurrency; i++ {
		failures = append(failures, <-results)
	}
	require.NoError(t, errors.Join(failures...))
	var count int
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_tasks WHERE user_id=$1`, user.ID).Scan(&count))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.Equal(t, concurrency, count)
	require.Equal(t, 17.0, balance)
	require.Equal(t, 8.0, frozen)
	after := integrationDB.Stats()
	t.Logf("isolated admission concurrency=%d elapsed=%s pool_waits=%d pool_wait_duration=%s; no Provider requests; not a production capacity guarantee",
		concurrency, time.Since(started), after.WaitCount-before.WaitCount, after.WaitDuration-before.WaitDuration)
}
