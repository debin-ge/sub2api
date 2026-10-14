//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type arkListLimitQueue struct {
	VideoTaskQueue
	allowed  bool
	err      error
	owner    int64
	deadline time.Time
}

func (q *arkListLimitQueue) AllowVideoTaskList(ctx context.Context, userID int64) (bool, error) {
	q.owner = userID
	q.deadline, _ = ctx.Deadline()
	return q.allowed, q.err
}

func TestArkListAdmissionProtectsRepository(t *testing.T) {
	filter := VideoTaskFilter{ArkOnly: true, Operation: VideoOperationGenerate, Limit: 20, Order: "desc"}
	for _, tc := range []struct {
		name  string
		queue *arkListLimitQueue
		want  error
	}{
		{"allowed", &arkListLimitQueue{allowed: true}, nil},
		{"exhausted", &arkListLimitQueue{}, ErrVideoListRateLimited},
		{"redis failure", &arkListLimitQueue{err: errors.New("redis unavailable")}, ErrBillingServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &videoTaskRepoStub{}
			svc := &VideoTaskService{tasks: repo, queue: tc.queue}
			_, err := svc.ListForOwner(context.Background(), 42, filter)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, int64(42), tc.queue.owner)
			require.WithinDuration(t, time.Now().Add(ArkVideoListQueryTimeout), tc.queue.deadline, time.Second)
			if tc.want != nil {
				require.Zero(t, repo.listUserID)
			} else {
				require.Equal(t, int64(42), repo.listUserID)
			}
		})
	}
	_, err := (&VideoTaskService{tasks: &videoTaskRepoStub{}}).ListForOwner(context.Background(), 42, filter)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable, "missing shared limiter must not fail open")
}
