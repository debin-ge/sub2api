package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func scanVideoCreateIntent(row videoJSONScanner) (*service.VideoCreateIntent, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var intent service.VideoCreateIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		return nil, err
	}
	return &intent, nil
}

func authorizeVideoIntentActor(ctx context.Context, tx *sql.Tx, userID, apiKeyID int64, creating bool) error {
	var authorized bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys ak JOIN users u ON u.id=ak.user_id
		WHERE ak.id=$1 AND ak.user_id=$2 AND ak.deleted_at IS NULL AND u.deleted_at IS NULL AND u.status='active'
		AND ak.status IN ('active','quota_exhausted') AND (ak.expires_at IS NULL OR ak.expires_at>clock_timestamp())
		AND (NOT $3::boolean OR (ak.status='active' AND (ak.quota<=0 OR ak.quota_used<ak.quota))))`, apiKeyID, userID, creating).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return service.ErrVideoInvalidRequest
	}
	return nil
}

func videoIntentByScope(ctx context.Context, tx *sql.Tx, userID int64, endpoint, key string) (*service.VideoCreateIntent, error) {
	intent, err := scanVideoCreateIntent(tx.QueryRowContext(ctx, `SELECT to_jsonb(i) FROM video_create_intents i WHERE user_id=$1 AND endpoint=$2 AND key_hash=$3 FOR UPDATE`,
		userID, endpoint, service.VideoCreateIntentKeyHash(key)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return intent, err
}

func nativeVideoByIntentScope(ctx context.Context, tx *sql.Tx, userID int64, endpoint, key string) (*service.VideoTask, error) {
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE user_id=$1 AND endpoint=$2 AND idempotency_key=$3`,
		userID, endpoint, strings.TrimSpace(key)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return task, err
}

func intentGuardFromRequest(intent *service.VideoCreateIntent, request service.VideoCreateIntentRequest) service.VideoCreateIntentGuard {
	return service.VideoCreateIntentGuard{ID: intent.ID, UserID: request.UserID, APIKeyID: request.APIKeyID, Endpoint: request.Endpoint,
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey), LeaseOwner: request.LeaseOwner, LeaseEpoch: intent.LeaseEpoch}
}

func insertNativeVideoIntent(ctx context.Context, tx *sql.Tx, task *service.VideoTask, key string) (*service.VideoCreateIntent, error) {
	return scanVideoCreateIntent(tx.QueryRowContext(ctx, `INSERT INTO video_create_intents AS i
		(user_id,api_key_id,endpoint,key_hash,request_hash,request_contract,state,target_platform,native_task_id,account_id)
		VALUES($1,$2,$3,$4,$5,'native_task_v1','native_bound',$6,$7,$8) RETURNING to_jsonb(i)`,
		task.UserID, task.APIKeyID, task.Endpoint, service.VideoCreateIntentKeyHash(key), task.RequestHash, task.Provider, task.ID, task.AccountID))
}

func (r *videoTaskRepository) ClaimVideoCreateIntent(ctx context.Context, request service.VideoCreateIntentRequest) (*service.VideoCreateIntentClaim, error) {
	if request.RequestContract == "" {
		request.RequestContract = service.VideoCreateIntentJSONContract
	}
	if err := service.ValidateVideoCreateIntentRequest(request); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := lockVideoBudgetOwnerTx(ctx, tx, request.UserID); err != nil {
		return nil, err
	}
	if err := authorizeVideoIntentActor(ctx, tx, request.UserID, request.APIKeyID, false); err != nil {
		return nil, err
	}
	intent, err := videoIntentByScope(ctx, tx, request.UserID, request.Endpoint, request.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	native, err := nativeVideoByIntentScope(ctx, tx, request.UserID, request.Endpoint, request.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if native != nil {
		if intent == nil {
			intent, err = insertNativeVideoIntent(ctx, tx, native, request.IdempotencyKey)
			if err != nil {
				return nil, err
			}
		} else if intent.State != service.VideoCreateIntentNative || intent.NativeTaskID == nil || *intent.NativeTaskID != native.ID {
			return nil, service.ErrVideoCreateOutcomeUnknown
		}
	}
	if intent != nil {
		if intent.State == service.VideoCreateIntentNative && native == nil {
			return nil, service.ErrVideoCreateOutcomeUnknown
		}
		if intent.RequestContract != service.VideoCreateIntentNativeContract && (intent.RequestHash != request.RequestHash || intent.RequestContract != request.RequestContract) {
			return nil, service.ErrVideoIdempotencyConflict
		}
		switch intent.State {
		case service.VideoCreateIntentNative:
			return &service.VideoCreateIntentClaim{Intent: intent}, tx.Commit()
		case service.VideoCreateIntentPrepared:
		default:
			return nil, service.ErrVideoCreateOutcomeUnknown
		}
		intent, err = scanVideoCreateIntent(tx.QueryRowContext(ctx, `UPDATE video_create_intents i SET lease_owner=$2,lease_epoch=lease_epoch+1,
			api_key_id=$3,lease_expires_at=clock_timestamp()+($4*INTERVAL '1 second'),updated_at=NOW()
			WHERE id=$1 AND state='prepared' AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp()) RETURNING to_jsonb(i)`,
			intent.ID, request.LeaseOwner, request.APIKeyID, request.LeaseDuration.Seconds()))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrVideoCreateInProgress
		}
	} else {
		intent, err = scanVideoCreateIntent(tx.QueryRowContext(ctx, `INSERT INTO video_create_intents AS i
			(user_id,api_key_id,endpoint,key_hash,request_hash,request_contract,state,lease_owner,lease_expires_at)
			VALUES($1,$2,$3,$4,$5,$8,'prepared',$6,clock_timestamp()+($7*INTERVAL '1 second')) RETURNING to_jsonb(i)`,
			request.UserID, request.APIKeyID, request.Endpoint, service.VideoCreateIntentKeyHash(request.IdempotencyKey), request.RequestHash, request.LeaseOwner, request.LeaseDuration.Seconds(), request.RequestContract))
	}
	if err != nil {
		return nil, err
	}
	return &service.VideoCreateIntentClaim{Intent: intent, Guard: intentGuardFromRequest(intent, request), Owned: true}, tx.Commit()
}

func validVideoIntentGuard(guard service.VideoCreateIntentGuard) bool {
	return guard.ID > 0 && guard.UserID > 0 && guard.APIKeyID > 0 && guard.LeaseEpoch > 0 && len(guard.LeaseOwner) >= 16 &&
		service.ValidVideoCreateIntentEndpoint(guard.Endpoint) && strings.TrimSpace(guard.IdempotencyKey) != ""
}

func lockGuardedVideoIntent(ctx context.Context, tx *sql.Tx, guard service.VideoCreateIntentGuard) (*service.VideoCreateIntent, error) {
	if !validVideoIntentGuard(guard) {
		return nil, service.ErrVideoCreateFenceLost
	}
	intent, err := scanVideoCreateIntent(tx.QueryRowContext(ctx, `SELECT to_jsonb(i) FROM video_create_intents i
		WHERE id=$1 AND user_id=$2 AND endpoint=$3 AND key_hash=$4 AND lease_owner=$5 AND lease_epoch=$6 FOR UPDATE`,
		guard.ID, guard.UserID, guard.Endpoint, service.VideoCreateIntentKeyHash(guard.IdempotencyKey), guard.LeaseOwner, guard.LeaseEpoch))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoCreateFenceLost
	}
	return intent, err
}

func (r *videoTaskRepository) RenewVideoCreateIntent(ctx context.Context, guard service.VideoCreateIntentGuard, duration time.Duration) error {
	if !validVideoIntentGuard(guard) || duration < time.Second || duration > 5*time.Minute {
		return service.ErrVideoCreateFenceLost
	}
	result, err := r.db.ExecContext(ctx, `UPDATE video_create_intents SET lease_expires_at=clock_timestamp()+($7*INTERVAL '1 second'),updated_at=NOW()
		WHERE id=$1 AND user_id=$2 AND endpoint=$3 AND key_hash=$4 AND lease_owner=$5 AND lease_epoch=$6
		AND state='prepared' AND lease_expires_at>clock_timestamp()`,
		guard.ID, guard.UserID, guard.Endpoint, service.VideoCreateIntentKeyHash(guard.IdempotencyKey), guard.LeaseOwner, guard.LeaseEpoch, duration.Seconds())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrVideoCreateFenceLost
	}
	return nil
}

func (r *videoTaskRepository) ReleasePreparedVideoCreateIntent(ctx context.Context, guard service.VideoCreateIntentGuard) error {
	if !validVideoIntentGuard(guard) {
		return service.ErrVideoCreateFenceLost
	}
	result, err := r.db.ExecContext(ctx, `UPDATE video_create_intents SET lease_owner=NULL,lease_expires_at=NULL,updated_at=NOW()
		WHERE id=$1 AND user_id=$2 AND endpoint=$3 AND key_hash=$4 AND lease_owner=$5 AND lease_epoch=$6 AND state='prepared'`,
		guard.ID, guard.UserID, guard.Endpoint, service.VideoCreateIntentKeyHash(guard.IdempotencyKey), guard.LeaseOwner, guard.LeaseEpoch)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrVideoCreateFenceLost
	}
	return nil
}

func checkNativeVideoCreateIntentTx(ctx context.Context, tx *sql.Tx, params service.VideoCreateTaskParams) (*service.VideoCreateIntent, error) {
	key := strings.TrimSpace(params.IdempotencyKey)
	if key == "" {
		return nil, nil
	}
	intent, err := videoIntentByScope(ctx, tx, params.Owner.UserID, params.Endpoint, key)
	guard, present := service.VideoCreateIntentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if intent == nil {
		if present {
			return nil, service.ErrVideoCreateFenceLost
		}
		return nil, nil
	}
	if !present || guard.ID != intent.ID || guard.UserID != params.Owner.UserID || guard.APIKeyID != params.Owner.APIKeyID || guard.Endpoint != params.Endpoint ||
		strings.TrimSpace(guard.IdempotencyKey) != key {
		return nil, service.ErrVideoCreateInProgress
	}
	intent, err = lockGuardedVideoIntent(ctx, tx, guard)
	if err != nil {
		return nil, err
	}
	var live bool
	if err := tx.QueryRowContext(ctx, `SELECT state='prepared' AND COALESCE(lease_expires_at>clock_timestamp(),false) FROM video_create_intents WHERE id=$1`, intent.ID).Scan(&live); err != nil {
		return nil, err
	}
	if !live {
		return nil, service.ErrVideoCreateFenceLost
	}
	return intent, nil
}

func bindNativeVideoCreateIntentTx(ctx context.Context, tx *sql.Tx, intent *service.VideoCreateIntent, task *service.VideoTask, key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if intent == nil {
		_, err := insertNativeVideoIntent(ctx, tx, task, key)
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE video_create_intents SET state='native_bound',target_platform=$2,native_task_id=$3,account_id=$4,
		lease_owner=NULL,lease_expires_at=NULL,updated_at=NOW() WHERE id=$1 AND state='prepared' AND lease_expires_at>clock_timestamp()`, intent.ID, task.Provider, task.ID, task.AccountID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrVideoCreateFenceLost
	}
	return nil
}

var _ service.VideoCreateIntentRepository = (*videoTaskRepository)(nil)

func (r *videoTaskRepository) ReadVideoCreateIntent(ctx context.Context, guard service.VideoCreateIntentGuard) (*service.VideoCreateIntent, error) {
	if !validVideoIntentGuard(guard) {
		return nil, service.ErrVideoCreateFenceLost
	}
	intent, err := scanVideoCreateIntent(r.db.QueryRowContext(ctx, `SELECT to_jsonb(intent) FROM video_create_intents intent
		WHERE id=$1 AND user_id=$2 AND endpoint=$3 AND key_hash=$4`, guard.ID, guard.UserID, guard.Endpoint, service.VideoCreateIntentKeyHash(guard.IdempotencyKey)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoCreateFenceLost
	}
	return intent, err
}

func (r *videoTaskRepository) QuarantineUntrackedVideoCreateIntent(ctx context.Context, guard service.VideoCreateIntentGuard) error {
	if !validVideoIntentGuard(guard) {
		return service.ErrVideoCreateFenceLost
	}
	result, err := r.db.ExecContext(ctx, `UPDATE video_create_intents SET state='untracked',lease_expires_at=NULL,last_error_code='untracked_dispatch',updated_at=NOW()
		WHERE id=$1 AND user_id=$2 AND endpoint=$3 AND key_hash=$4 AND lease_owner=$5 AND lease_epoch=$6 AND state='prepared'`,
		guard.ID, guard.UserID, guard.Endpoint, service.VideoCreateIntentKeyHash(guard.IdempotencyKey), guard.LeaseOwner, guard.LeaseEpoch)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrVideoCreateFenceLost
	}
	return nil
}
