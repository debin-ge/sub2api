package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type notificationEmailOutboxRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewNotificationEmailOutboxRepository(client *dbent.Client, db *sql.DB) service.NotificationEmailOutboxRepository {
	return &notificationEmailOutboxRepository{client: client, db: db}
}

func (r *notificationEmailOutboxRepository) Enqueue(ctx context.Context, input service.NotificationEmailOutboxInput) error {
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	_, err = clientFromContext(ctx, r.client).ExecContext(ctx, `
		INSERT INTO notification_email_outbox (
			event_type, user_id, api_key_id, recipient_email, rotation_version, payload, dedup_key
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (dedup_key) DO NOTHING
	`, input.EventType, input.UserID, input.APIKeyID, input.RecipientEmail, input.RotationVersion, string(payload), input.DedupKey)
	return err
}

func (r *notificationEmailOutboxRepository) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.NotificationEmailOutboxEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil notification email outbox database")
	}
	if limit <= 0 {
		limit = 50
	}
	leaseSeconds := max(int64(lease/time.Second), 1)
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM notification_email_outbox
			WHERE sent_at IS NULL AND cancelled_at IS NULL AND available_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - ($3 * INTERVAL '1 second'))
			ORDER BY id ASC LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_email_outbox AS o
		SET claimed_at = NOW(), claimed_by = $1, updated_at = NOW()
		FROM candidates AS c WHERE o.id = c.id
		RETURNING o.id, o.event_type, o.user_id, o.api_key_id, o.recipient_email,
		          o.rotation_version, o.payload, o.attempts, o.created_at
	`, workerID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.NotificationEmailOutboxEvent, 0, limit)
	for rows.Next() {
		var event service.NotificationEmailOutboxEvent
		var payload []byte
		if err := rows.Scan(&event.ID, &event.EventType, &event.UserID, &event.APIKeyID, &event.RecipientEmail, &event.RotationVersion, &payload, &event.Attempts, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *notificationEmailOutboxRepository) MarkSent(ctx context.Context, id int64, workerID string) error {
	return r.updateClaim(ctx, `UPDATE notification_email_outbox SET sent_at=NOW(), claimed_at=NULL, claimed_by=NULL, last_error=NULL, updated_at=NOW() WHERE id=$1 AND claimed_by=$2`, id, workerID)
}

func (r *notificationEmailOutboxRepository) Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE notification_email_outbox SET attempts=attempts+1, available_at=$3, last_error=$4, claimed_at=NULL, claimed_by=NULL, updated_at=NOW() WHERE id=$1 AND claimed_by=$2`, id, workerID, availableAt, lastError)
	return requireOneNotificationOutboxRow(result, err, id)
}

func (r *notificationEmailOutboxRepository) Cancel(ctx context.Context, id int64, workerID, reason string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE notification_email_outbox SET cancelled_at=NOW(), cancel_reason=$3, claimed_at=NULL, claimed_by=NULL, updated_at=NOW() WHERE id=$1 AND claimed_by=$2`, id, workerID, reason)
	return requireOneNotificationOutboxRow(result, err, id)
}

func (r *notificationEmailOutboxRepository) CancelPendingRotationsByAPIKey(ctx context.Context, apiKeyID int64, reason string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notification_email_outbox SET cancelled_at=NOW(), cancel_reason=$2, claimed_at=NULL, claimed_by=NULL, updated_at=NOW() WHERE api_key_id=$1 AND event_type=$3 AND sent_at IS NULL AND cancelled_at IS NULL`, apiKeyID, reason, service.NotificationEmailEventAPIKeyRotated)
	return err
}

func (r *notificationEmailOutboxRepository) Stats(ctx context.Context) (service.NotificationEmailOutboxStats, error) {
	var stats service.NotificationEmailOutboxStats
	var oldest sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE sent_at IS NULL AND cancelled_at IS NULL),
			COUNT(*) FILTER (WHERE sent_at IS NOT NULL),
			COUNT(*) FILTER (WHERE cancelled_at IS NOT NULL),
			COALESCE(MAX(attempts), 0),
			MIN(created_at) FILTER (WHERE sent_at IS NULL AND cancelled_at IS NULL),
			(SELECT last_error FROM notification_email_outbox WHERE last_error IS NOT NULL ORDER BY updated_at DESC LIMIT 1)
		FROM notification_email_outbox
	`).Scan(&stats.Pending, &stats.Sent, &stats.Cancelled, &stats.MaxAttempts, &oldest, &lastError)
	if oldest.Valid {
		stats.OldestCreatedAt = &oldest.Time
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, err
}

func (r *notificationEmailOutboxRepository) DeleteCompletedBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	result, err := r.db.ExecContext(ctx, `
		WITH doomed AS (
			SELECT id FROM notification_email_outbox
			WHERE COALESCE(sent_at, cancelled_at) < $1
			ORDER BY id ASC LIMIT $2
		)
		DELETE FROM notification_email_outbox o USING doomed d WHERE o.id=d.id
	`, before, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *notificationEmailOutboxRepository) updateClaim(ctx context.Context, query string, id int64, workerID string) error {
	result, err := r.db.ExecContext(ctx, query, id, workerID)
	return requireOneNotificationOutboxRow(result, err, id)
}

func requireOneNotificationOutboxRow(result sql.Result, err error, id int64) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("notification email outbox claim %d is no longer owned", id)
	}
	return nil
}

var _ service.NotificationEmailOutboxRepository = (*notificationEmailOutboxRepository)(nil)
