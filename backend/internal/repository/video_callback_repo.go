package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoCallbackRepository struct {
	db *sql.DB
}

func NewVideoCallbackRepository(db *sql.DB) service.VideoCallbackRepository {
	return &videoCallbackRepository{db: db}
}

type videoCallbackRow struct {
	ID               int64          `json:"id"`
	TaskID           int64          `json:"task_id"`
	EventID          string         `json:"event_id"`
	EventType        string         `json:"event_type"`
	EventFingerprint string         `json:"event_fingerprint"`
	Payload          map[string]any `json:"payload"`
	TargetURLEnc     string         `json:"target_url_enc"`
	Status           string         `json:"status"`
	Attempts         int            `json:"attempts"`
	NextAttemptAt    time.Time      `json:"next_attempt_at"`
	ExpiresAt        time.Time      `json:"expires_at"`
	LeaseOwner       *string        `json:"lease_owner"`
	LeaseExpiresAt   *time.Time     `json:"lease_expires_at"`
	LastError        *string        `json:"last_error"`
	LastStatusCode   *int           `json:"last_status_code"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeliveredAt      *time.Time     `json:"delivered_at"`
	QuarantinedAt    *time.Time     `json:"quarantined_at"`
}

func (row videoCallbackRow) delivery() *service.VideoCallbackDelivery {
	return &service.VideoCallbackDelivery{
		ID: row.ID, TaskID: row.TaskID, EventID: row.EventID, EventType: row.EventType,
		EventFingerprint: row.EventFingerprint, Payload: row.Payload, TargetURLEnc: row.TargetURLEnc,
		Status: row.Status, Attempts: row.Attempts, NextAttemptAt: row.NextAttemptAt,
		ExpiresAt: row.ExpiresAt, LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt,
		LastError: row.LastError, LastStatusCode: row.LastStatusCode, CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt, DeliveredAt: row.DeliveredAt, QuarantinedAt: row.QuarantinedAt,
	}
}

func scanVideoCallback(scanner videoJSONScanner) (*service.VideoCallbackDelivery, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		return nil, err
	}
	var row videoCallbackRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("decode video callback row: %w", err)
	}
	return row.delivery(), nil
}

func (r *videoCallbackRepository) EnqueueVideoCallback(ctx context.Context, delivery service.VideoCallbackDelivery) (*service.VideoCallbackDelivery, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("video callback repository is not configured")
	}
	if delivery.TaskID <= 0 || strings.TrimSpace(delivery.EventID) == "" ||
		strings.TrimSpace(delivery.EventType) == "" || strings.TrimSpace(delivery.EventFingerprint) == "" ||
		strings.TrimSpace(delivery.TargetURLEnc) == "" {
		return nil, false, service.ErrVideoInvalidRequest
	}
	if delivery.NextAttemptAt.IsZero() {
		delivery.NextAttemptAt = time.Now().UTC()
	}
	if delivery.ExpiresAt.IsZero() || !delivery.ExpiresAt.After(delivery.NextAttemptAt) {
		return nil, false, service.ErrVideoInvalidRequest
	}
	payload, err := videoJSON(delivery.Payload, map[string]any{})
	if err != nil {
		return nil, false, err
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO video_callback_deliveries (
			task_id, event_id, event_type, event_fingerprint, payload,
			target_url_enc, next_attempt_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		ON CONFLICT (task_id, event_fingerprint) DO NOTHING
		RETURNING id
	`, delivery.TaskID, delivery.EventID, delivery.EventType, delivery.EventFingerprint,
		payload, delivery.TargetURLEnc, delivery.NextAttemptAt, delivery.ExpiresAt).Scan(&id)
	created := true
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = r.db.QueryRowContext(ctx, `
			SELECT id FROM video_callback_deliveries
			WHERE task_id = $1 AND event_fingerprint = $2
		`, delivery.TaskID, delivery.EventFingerprint).Scan(&id)
	}
	if err != nil {
		return nil, false, err
	}
	stored, err := scanVideoCallback(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vcd) FROM video_callback_deliveries vcd WHERE id = $1
	`, id))
	return stored, created, err
}

func (r *videoCallbackRepository) ClaimVideoCallbacks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*service.VideoCallbackDelivery, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, errors.New("video callback worker id is required")
	}
	if limit <= 0 {
		limit = 32
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE video_callback_deliveries
		SET status = 'quarantined', quarantined_at = COALESCE(quarantined_at, NOW()),
			last_error = COALESCE(last_error, 'callback retry window expired'),
			lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE status IN ('pending','failed','delivering') AND expires_at <= NOW()
	`); err != nil {
		return nil, err
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 90
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM video_callback_deliveries
			WHERE status IN ('pending','failed','delivering')
			  AND next_attempt_at <= NOW()
			  AND expires_at > NOW()
			  AND (lease_owner IS NULL OR lease_expires_at < NOW())
			ORDER BY next_attempt_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_callback_deliveries d
		SET status = 'delivering', lease_owner = $1,
			lease_expires_at = NOW() + ($3 * INTERVAL '1 second'), updated_at = NOW()
		FROM candidates c
		WHERE d.id = c.id
		RETURNING to_jsonb(d)
	`, workerID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	deliveries := make([]*service.VideoCallbackDelivery, 0, limit)
	for rows.Next() {
		delivery, err := scanVideoCallback(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (r *videoCallbackRepository) MarkVideoCallbackDelivered(ctx context.Context, id int64, workerID string, statusCode int) error {
	return r.completeClaim(ctx, id, workerID, `
		status = 'delivered', attempts = attempts + 1, delivered_at = NOW(),
		last_status_code = $3, last_error = NULLIF($4, ''),
		lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
	`, statusCode, "")
}

func (r *videoCallbackRepository) RenewVideoCallbackLease(ctx context.Context, id int64, workerID string, lease time.Duration) error {
	if id <= 0 || strings.TrimSpace(workerID) == "" || lease < time.Millisecond {
		return service.ErrVideoCallbackLeaseLost
	}
	result, err := r.db.ExecContext(ctx, `UPDATE video_callback_deliveries
		SET lease_expires_at=clock_timestamp()+($3*INTERVAL '1 millisecond'), updated_at=NOW()
		WHERE id=$1 AND lease_owner=$2 AND status='delivering' AND lease_expires_at>clock_timestamp()`,
		id, workerID, lease.Milliseconds())
	return requireVideoCallbackUpdated(result, err)
}

func (r *videoCallbackRepository) RetryVideoCallback(ctx context.Context, id int64, workerID string, nextAttemptAt time.Time, statusCode int, lastError string) error {
	lastError = boundedVideoCallbackError(lastError)
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_callback_deliveries
		SET status = CASE WHEN expires_at <= $3 THEN 'quarantined' ELSE 'failed' END,
			attempts = attempts + 1, next_attempt_at = $3,
			last_status_code = NULLIF($4, 0), last_error = NULLIF($5, ''),
			quarantined_at = CASE WHEN expires_at <= $3 THEN NOW() ELSE quarantined_at END,
			lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2 AND status = 'delivering' AND lease_expires_at > clock_timestamp()
	`, id, strings.TrimSpace(workerID), nextAttemptAt, statusCode, lastError)
	return requireVideoCallbackUpdated(result, err)
}

func (r *videoCallbackRepository) QuarantineVideoCallback(ctx context.Context, id int64, workerID string, lastError string) error {
	return r.completeClaim(ctx, id, workerID, `
		status = 'quarantined', attempts = attempts + 1, quarantined_at = NOW(),
		last_status_code = COALESCE(NULLIF($3::integer, 0), last_status_code),
		last_error = NULLIF($4, ''), lease_owner = NULL, lease_expires_at = NULL,
		updated_at = NOW()
	`, 0, boundedVideoCallbackError(lastError))
}

func (r *videoCallbackRepository) completeClaim(ctx context.Context, id int64, workerID, setClause string, statusCode int, lastError string) error {
	query := `UPDATE video_callback_deliveries SET ` + setClause + ` WHERE id = $1 AND lease_owner = $2 AND status = 'delivering' AND lease_expires_at > clock_timestamp()`
	result, err := r.db.ExecContext(ctx, query, id, strings.TrimSpace(workerID), statusCode, lastError)
	return requireVideoCallbackUpdated(result, err)
}

func requireVideoCallbackUpdated(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrVideoCallbackLeaseLost
	}
	return nil
}

func boundedVideoCallbackError(value string) string {
	value = strings.ToValidUTF8(strings.ReplaceAll(value, "\x00", "\uFFFD"), "\uFFFD")
	const maxBytes = 1024
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
