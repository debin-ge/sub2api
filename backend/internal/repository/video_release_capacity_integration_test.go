//go:build integration

package repository

import (
	"context"
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
	require.Greater(t, concurrency, service.VideoProviderCostMaxUnresolvedPerUser)
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
	accepted, limited := 0, 0
	for i := 0; i < concurrency; i++ {
		if err := <-results; err != nil {
			require.ErrorIs(t, err, service.ErrVideoProviderCostLimited)
			limited++
		} else {
			accepted++
		}
	}
	// No fake provider accepts these tasks: each admitted hold must retain its
	// confirmation slot. Rejected contenders must not reserve money or rows.
	require.Equal(t, service.VideoProviderCostMaxUnresolvedPerUser, accepted)
	require.Equal(t, concurrency-accepted, limited)
	var count int
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_tasks WHERE user_id=$1`, user.ID).Scan(&count))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.Equal(t, accepted, count)
	require.Equal(t, 25-float64(accepted), balance)
	require.Equal(t, float64(accepted), frozen)
	after := integrationDB.Stats()
	t.Logf("isolated admission concurrency=%d accepted=%d limited=%d elapsed=%s pool_waits=%d pool_wait_duration=%s; no Provider requests; not a production capacity guarantee",
		concurrency, accepted, limited, time.Since(started), after.WaitCount-before.WaitCount, after.WaitDuration-before.WaitDuration)
}
