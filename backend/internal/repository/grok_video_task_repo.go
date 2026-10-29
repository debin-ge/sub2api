package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type grokVideoTaskRepository struct{ db *sql.DB }

func NewGrokVideoTaskRepository(db *sql.DB) service.GrokVideoTaskRepository {
	return &grokVideoTaskRepository{db}
}

const grokVideoJSON = `(g.data || (to_jsonb(g)-'data'))`

func scanGrokVideo(row interface{ Scan(...any) error }) (*service.GrokVideoTask, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrGrokVideoNotFound
		}
		return nil, err
	}
	var t service.GrokVideoTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
func (r *grokVideoTaskRepository) Get(ctx context.Context, id string) (*service.GrokVideoTask, error) {
	return scanGrokVideo(r.db.QueryRowContext(ctx, `SELECT `+grokVideoJSON+` FROM grok_video_tasks g WHERE public_id=$1`, id))
}
func (r *grokVideoTaskRepository) FindIdempotent(ctx context.Context, u, k int64, op, key string) (*service.GrokVideoTask, error) {
	return scanGrokVideo(r.db.QueryRowContext(ctx, `SELECT `+grokVideoJSON+` FROM grok_video_tasks g WHERE user_id=$1 AND api_key_id=$2 AND operation=$3 AND idempotency_key=$4`, u, k, op, key))
}
func (r *grokVideoTaskRepository) FindOwned(ctx context.Context, u, k, g int64, id string) (*service.GrokVideoTask, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+grokVideoJSON+` FROM grok_video_tasks g WHERE user_id=$1 AND api_key_id=$2 AND group_id=$3 AND provider_task_id=$4 LIMIT 2`, u, k, g, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *service.GrokVideoTask
	for rows.Next() {
		t, e := scanGrokVideo(rows)
		if e != nil {
			return nil, e
		}
		if result != nil {
			return nil, service.ErrGrokVideoConflict
		}
		result = t
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, service.ErrGrokVideoNotFound
	}
	return result, nil
}
func (r *grokVideoTaskRepository) Create(ctx context.Context, t *service.GrokVideoTask) (*service.GrokVideoTask, bool, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return nil, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	saved, err := scanGrokVideo(tx.QueryRowContext(ctx, `INSERT INTO grok_video_tasks AS g
 (public_id,user_id,api_key_id,group_id,account_id,operation,request_hash,idempotency_key,provider_task_id,generation_state,sync_status,settlement_status,data,next_action_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING RETURNING `+grokVideoJSON,
		t.PublicID, t.UserID, t.APIKeyID, t.GroupID, t.AccountID, t.Operation, t.RequestHash, t.IdempotencyKey, t.ProviderTaskID, t.GenerationState, t.SyncStatus, t.SettlementStatus, raw, t.NextActionAt))
	if errors.Is(err, service.ErrGrokVideoNotFound) || isGrokCreationReceiptConflict(err) {
		tx.Rollback()
		if t.IdempotencyKey != nil {
			saved, err = r.FindIdempotent(ctx, t.UserID, t.APIKeyID, t.Operation, *t.IdempotencyKey)
		} else if t.ProviderTaskID != nil {
			saved, err = r.FindOwned(ctx, t.UserID, t.APIKeyID, t.GroupID, *t.ProviderTaskID)
		}
		if errors.Is(err, service.ErrGrokVideoNotFound) {
			return nil, false, service.ErrGrokVideoConflict
		}
		if err != nil {
			return nil, false, err
		}
		if saved == nil || saved.RequestHash != t.RequestHash {
			return nil, false, service.ErrGrokVideoConflict
		}
		return saved, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO grok_video_task_events(task_id,event_type,generation_state,settlement_status) VALUES($1,'created',$2,$3)`, saved.ID, saved.GenerationState, saved.SettlementStatus); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return saved, true, nil
}
func (r *grokVideoTaskRepository) Save(ctx context.Context, t *service.GrokVideoTask, event string) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(ctx, `UPDATE grok_video_tasks SET data=$2,provider_task_id=$3,generation_state=$4,sync_status=$5,settlement_status=$6,next_action_at=$7,version=version+1,updated_at=NOW(),lease_owner=$8,lease_expires_at=$9
 WHERE public_id=$1 AND version=$10 AND lease_epoch=$11 AND (lease_owner IS NULL OR lease_expires_at>clock_timestamp()) RETURNING version`, t.PublicID, raw, t.ProviderTaskID, t.GenerationState, t.SyncStatus, t.SettlementStatus, t.NextActionAt, t.LeaseOwner, t.LeaseExpiresAt, t.Version, t.LeaseEpoch).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrGrokVideoBusy
	}
	if err != nil {
		return err
	}
	if event != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO grok_video_task_events(task_id,event_type,generation_state,settlement_status) VALUES($1,$2,$3,$4)`, t.ID, event, t.GenerationState, t.SettlementStatus); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	t.Version = version
	return nil
}
func (r *grokVideoTaskRepository) Claim(ctx context.Context, id, owner string, force bool) (*service.GrokVideoTask, error) {
	t, err := scanGrokVideo(r.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT id FROM grok_video_tasks WHERE public_id=$1 AND (lease_owner IS NULL OR lease_expires_at<=clock_timestamp()) AND ($3 OR next_action_at<=NOW()) FOR UPDATE SKIP LOCKED)
 UPDATE grok_video_tasks g SET lease_owner=$2,lease_expires_at=clock_timestamp()+INTERVAL '60 seconds',lease_epoch=lease_epoch+1,version=version+1 FROM candidate c WHERE g.id=c.id RETURNING `+grokVideoJSON, id, owner, force))
	if errors.Is(err, service.ErrGrokVideoNotFound) {
		return nil, service.ErrGrokVideoBusy
	}
	return t, err
}
func (r *grokVideoTaskRepository) Due(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT public_id FROM grok_video_tasks WHERE next_action_at<=NOW() AND (lease_owner IS NULL OR lease_expires_at<=clock_timestamp()) ORDER BY next_action_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *grokVideoTaskRepository) ExistingBilling(ctx context.Context, id string, key int64) (*service.PreparedGrokVideoBilling, error) {
	var cmdRaw, logRaw []byte
	err := r.db.QueryRowContext(ctx, `SELECT command_payload,usage_log_payload FROM usage_billing_outbox WHERE request_id=$1 AND api_key_id=$2`, id, key).Scan(&cmdRaw, &logRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cmd usageBillingCommandPayloadV1
	var log usageLogPayloadV1
	if err = json.Unmarshal(cmdRaw, &cmd); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(logRaw, &log); err != nil {
		return nil, err
	}
	return &service.PreparedGrokVideoBilling{Command: cmd.command(), UsageLog: log.usageLog()}, nil
}
func (r *grokVideoTaskRepository) RecordedUsage(ctx context.Context, id string, key int64) (*service.UsageLog, error) {
	var raw []byte
	var logID int64
	err := r.db.QueryRowContext(ctx, `SELECT id,to_jsonb(u) FROM usage_logs u WHERE request_id=$1 AND api_key_id=$2`, id, key).Scan(&logID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p usageLogPayloadV1
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	l := p.usageLog()
	l.ID = logID
	return l, nil
}
func (r *grokVideoTaskRepository) RecordSimpleUsage(ctx context.Context, l *service.UsageLog) error {
	if l == nil {
		return fmt.Errorf("missing usage")
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	_, err := (&usageLogRepository{}).createSingle(ctx, r.db, l)
	return err
}
