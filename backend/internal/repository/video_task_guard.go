package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *videoTaskRepository) RenewVideoTaskLease(ctx context.Context, lease service.VideoTaskLease, duration time.Duration) (time.Time, error) {
	if lease.TaskID <= 0 || lease.Epoch <= 0 || strings.TrimSpace(lease.Owner) == "" || duration <= 0 {
		return time.Time{}, service.ErrVideoLeaseLost
	}
	var expiry time.Time
	err := r.db.QueryRowContext(ctx, `
		UPDATE video_tasks SET lease_expires_at = clock_timestamp() + ($4 * INTERVAL '1 millisecond')
		WHERE id = $1 AND lease_owner = $2 AND lease_epoch = $3 AND lease_expires_at > clock_timestamp()
		RETURNING lease_expires_at
	`, lease.TaskID, lease.Owner, lease.Epoch, duration.Milliseconds()).Scan(&expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, service.ErrVideoLeaseLost
	}
	return expiry, err
}

func videoTaskGuardTx(ctx context.Context, tx *sql.Tx, task *service.VideoTask) (service.VideoTaskWriteGuard, error) {
	guard, ok := service.VideoTaskWriteGuardFromContext(ctx)
	if !ok || guard.TaskID != task.ID || guard.Version < 0 {
		return guard, service.ErrVideoInvalidRequest
	}
	if guard.Lease != nil {
		if guard.Lease.TaskID != task.ID {
			return guard, service.ErrVideoLeaseLost
		}
		if err := checkVideoTaskLeaseTx(ctx, tx, *guard.Lease); err != nil {
			return guard, err
		}
	}
	if task.Version != guard.Version {
		return guard, service.ErrVideoVersionConflict
	}
	return guard, nil
}

func checkVideoTaskLeaseTx(ctx context.Context, tx *sql.Tx, lease service.VideoTaskLease) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(lease_owner = $2 AND lease_epoch = $3 AND lease_expires_at > clock_timestamp(), false)
		FROM video_tasks WHERE id = $1 FOR UPDATE
	`, lease.TaskID, lease.Owner, lease.Epoch).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrVideoLeaseLost
	}
	if err != nil {
		return err
	}
	if !valid || lease.Epoch <= 0 || strings.TrimSpace(lease.Owner) == "" {
		return service.ErrVideoLeaseLost
	}
	return nil
}

func videoGuardLeaseValues(guard service.VideoTaskWriteGuard) (any, int64) {
	if guard.Lease == nil {
		return nil, 0
	}
	return guard.Lease.Owner, guard.Lease.Epoch
}

func videoGuardWriteResult(result sql.Result, guard service.VideoTaskWriteGuard) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 1 {
		return nil
	}
	if guard.Lease != nil {
		return service.ErrVideoLeaseLost
	}
	return service.ErrVideoVersionConflict
}

func (r *videoTaskRepository) WakeVideoTask(ctx context.Context, publicID string, at time.Time) (*service.VideoTask, error) {
	_, err := r.db.ExecContext(ctx, `
		UPDATE video_tasks SET next_action_at = LEAST(COALESCE(next_action_at, $2), $2)
		WHERE public_id = $1 AND billing_state = 'held'
			AND generation_state IN ('submission_unknown', 'queued', 'in_progress')
	`, publicID, at)
	if err != nil {
		return nil, err
	}
	return r.GetVideoTaskByPublicID(ctx, publicID)
}
