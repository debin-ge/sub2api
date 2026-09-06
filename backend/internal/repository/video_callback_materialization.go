package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *videoCallbackRepository) ListVideoCallbackIntents(ctx context.Context, limit int) ([]*service.VideoTask, error) {
	if limit <= 0 || limit > 1000 {
		limit = 32
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE callback_url_enc IS NOT NULL AND BTRIM(callback_url_enc) <> ''
		  AND billing_state IN ('captured', 'released')
		  AND callback_intent_state IN ('none', 'pending')
		ORDER BY settled_at, id LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]*service.VideoTask, 0, limit)
	for rows.Next() {
		task, err := scanVideoTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *videoCallbackRepository) MaterializeVideoCallback(ctx context.Context, delivery *service.VideoCallbackDelivery) error {
	if delivery == nil || delivery.TaskID <= 0 || delivery.EventID == "" || delivery.EventFingerprint == "" || delivery.TargetURLEnc == "" ||
		(delivery.Status != "pending" && delivery.Status != "quarantined") {
		return service.ErrVideoInvalidRequest
	}
	if delivery.Status == "pending" && !delivery.ExpiresAt.After(delivery.NextAttemptAt) {
		return service.ErrVideoInvalidRequest
	}
	payload, err := videoJSON(delivery.Payload, map[string]any{})
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var state, billing, target string
	err = tx.QueryRowContext(ctx, `SELECT callback_intent_state, billing_state, COALESCE(callback_url_enc, '') FROM video_tasks WHERE id = $1 FOR UPDATE`, delivery.TaskID).Scan(&state, &billing, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrVideoTaskNotFound
	}
	if err != nil {
		return err
	}
	if state == "materialized" {
		return tx.Commit()
	}
	if !service.IsVideoBillingTerminal(billing) || strings.TrimSpace(target) == "" || target != delivery.TargetURLEnc {
		return service.ErrVideoInvalidTransition
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO video_callback_deliveries
		(task_id, event_id, event_type, event_fingerprint, payload, target_url_enc, status, next_attempt_at, expires_at, created_at, last_error, quarantined_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7::text,$8,$9,$10,$11,CASE WHEN $7::text = 'quarantined' THEN NOW() ELSE NULL END)
		ON CONFLICT (task_id, event_fingerprint) DO NOTHING
	`, delivery.TaskID, delivery.EventID, delivery.EventType, delivery.EventFingerprint, payload, target, delivery.Status,
		delivery.NextAttemptAt, delivery.ExpiresAt, delivery.CreatedAt, delivery.LastError)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE video_tasks SET callback_intent_state = 'materialized', updated_at = NOW() WHERE id = $1`, delivery.TaskID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
