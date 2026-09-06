package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type apiKeyRotationRepository struct{ db *sql.DB }

func NewAPIKeyRotationRepository(db *sql.DB) service.APIKeyRotationRepository {
	return &apiKeyRotationRepository{db: db}
}

func (r *apiKeyRotationRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]service.DueAPIKeyRotation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT k.id, k.user_id, k.key, k.name, k.notification_email, k.expires_at,
		       k.validity_duration_seconds, k.rotation_version
		FROM api_keys k
		WHERE k.rotate_on_expiry = TRUE
		  AND k.expires_at <= $1
		  AND k.notification_email IS NOT NULL
		  AND k.validity_duration_seconds > 0
		  AND k.status IN ('active', 'quota_exhausted', 'expired')
		  AND k.deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM notification_email_outbox o
		      WHERE o.api_key_id = k.id AND o.event_type = $2
		        AND o.sent_at IS NULL AND o.cancelled_at IS NULL
		  )
		ORDER BY k.expires_at ASC, k.id ASC
		LIMIT $3
	`, now, service.NotificationEmailEventAPIKeyRotated, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.DueAPIKeyRotation, 0, limit)
	for rows.Next() {
		var item service.DueAPIKeyRotation
		if err := rows.Scan(&item.ID, &item.UserID, &item.OldKey, &item.Name, &item.NotificationEmail, &item.ExpiresAt, &item.ValidityDurationSeconds, &item.RotationVersion); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *apiKeyRotationRepository) RotateIfDue(ctx context.Context, candidate service.DueAPIKeyRotation, newKey string, now time.Time) (int64, bool, error) {
	newVersion := candidate.RotationVersion + 1
	payload, err := json.Marshal(service.NotificationEmailOutboxPayload{Variables: map[string]string{
		"rotated_at": now.Format("2006-01-02 15:04:05 MST"),
	}})
	if err != nil {
		return 0, false, err
	}
	dedupKey := fmt.Sprintf("api_key.rotated:%d:%d", candidate.ID, newVersion)
	var storedVersion int64
	err = r.db.QueryRowContext(ctx, `
		WITH updated AS (
			UPDATE api_keys
			SET key = $2,
			    expires_at = $3 + (validity_duration_seconds * INTERVAL '1 second'),
			    status = CASE WHEN status = 'expired' THEN 'active' ELSE status END,
			    last_rotated_at = $3,
			    rotation_version = rotation_version + 1,
			    updated_at = $3
			WHERE id = $1
			  AND rotation_version = $4
			  AND expires_at <= $3
			  AND rotate_on_expiry = TRUE
			  AND notification_email IS NOT NULL
			  AND validity_duration_seconds > 0
			  AND status IN ('active', 'quota_exhausted', 'expired')
			  AND deleted_at IS NULL
			  AND NOT EXISTS (
			      SELECT 1 FROM notification_email_outbox o
			      WHERE o.api_key_id = api_keys.id AND o.event_type = $5
			        AND o.sent_at IS NULL AND o.cancelled_at IS NULL
			  )
			RETURNING id, user_id, notification_email, rotation_version
		), inserted AS (
			INSERT INTO notification_email_outbox (
				event_type, user_id, api_key_id, recipient_email, rotation_version, payload, dedup_key
			)
			SELECT $5, user_id, id, notification_email, rotation_version, $6::jsonb, $7 FROM updated
			RETURNING rotation_version
		)
		SELECT rotation_version FROM inserted
	`, candidate.ID, newKey, now, candidate.RotationVersion, service.NotificationEmailEventAPIKeyRotated, string(payload), dedupKey).Scan(&storedVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return 0, false, service.ErrAPIKeyExists
		}
		return 0, false, err
	}
	return storedVersion, true, nil
}

var _ service.APIKeyRotationRepository = (*apiKeyRotationRepository)(nil)
