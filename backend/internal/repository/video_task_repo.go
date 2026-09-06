package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoTaskRepository struct {
	db      *sql.DB
	billing *usageBillingRepository
}

func NewVideoTaskRepository(db *sql.DB, billing *usageBillingRepository) service.VideoTaskRepository {
	return &videoTaskRepository{db: db, billing: billing}
}

type videoTaskRow struct {
	BillingReviewID         *int64                            `json:"billing_review_id"`
	SubmissionReviewID      *int64                            `json:"submission_review_id"`
	ID                      int64                             `json:"id"`
	PublicID                string                            `json:"public_id"`
	Source                  string                            `json:"source"`
	UserID                  *int64                            `json:"user_id"`
	APIKeyID                *int64                            `json:"api_key_id"`
	GroupID                 *int64                            `json:"group_id"`
	ChannelID               *int64                            `json:"channel_id"`
	AccountID               *int64                            `json:"account_id"`
	AccountOwnerUserID      *int64                            `json:"account_owner_user_id"`
	Provider                string                            `json:"provider"`
	Operation               string                            `json:"operation"`
	ParentTaskID            *int64                            `json:"parent_task_id"`
	RootTaskID              *int64                            `json:"root_task_id"`
	Endpoint                string                            `json:"endpoint"`
	RequestedModel          string                            `json:"requested_model"`
	PublicModel             string                            `json:"public_model"`
	ChannelModel            string                            `json:"channel_model"`
	UpstreamModel           string                            `json:"upstream_model"`
	RequestHash             string                            `json:"request_hash"`
	IdempotencyKey          *string                           `json:"idempotency_key"`
	InputManifest           []service.VideoInputManifestEntry `json:"input_manifest"`
	RequestAttributes       map[string]any                    `json:"request_attributes"`
	ProviderTaskID          *string                           `json:"provider_task_id"`
	ProviderStatus          *string                           `json:"provider_status"`
	ProviderCreatedAt       *time.Time                        `json:"provider_created_at"`
	ProviderFinishedAt      *time.Time                        `json:"provider_finished_at"`
	StableClientToken       *string                           `json:"stable_client_token"`
	GenerationState         string                            `json:"generation_state"`
	BillingState            string                            `json:"billing_state"`
	DeleteState             string                            `json:"delete_state"`
	Version                 int64                             `json:"version"`
	Progress                *float64                          `json:"progress"`
	UsageSnapshot           map[string]any                    `json:"usage_snapshot"`
	ResponseMetadata        map[string]any                    `json:"response_metadata"`
	ContentVariants         []string                          `json:"content_variants"`
	ContentExpiresAt        *time.Time                        `json:"content_expires_at"`
	ProviderAccessKind      *string                           `json:"provider_access_kind"`
	ProviderAccessScope     *string                           `json:"provider_access_scope"`
	ProviderAccessEnc       *string                           `json:"provider_access_enc"`
	ProviderAccessExpiresAt *time.Time                        `json:"provider_access_expires_at"`
	ProviderVideoURLEnc     *string                           `json:"provider_video_url_enc"`
	ProviderVideoProxyKey   *string                           `json:"provider_video_proxy_key"`
	BillingUnit             *string                           `json:"billing_unit"`
	EstimatedUnits          *float64                          `json:"estimated_units"`
	ActualUnits             *float64                          `json:"actual_units"`
	PriceSnapshot           map[string]any                    `json:"price_snapshot"`
	ProviderCostSnapshot    map[string]any                    `json:"provider_cost_snapshot"`
	Currency                string                            `json:"currency"`
	HoldID                  *string                           `json:"hold_id"`
	HoldAmount              *float64                          `json:"hold_amount"`
	ActualCost              *float64                          `json:"actual_cost"`
	CallbackURLEnc          *string                           `json:"callback_url_enc"`
	CallbackIntentState     string                            `json:"callback_intent_state"`
	NextActionAt            *time.Time                        `json:"next_action_at"`
	PollAttempts            int                               `json:"poll_attempts"`
	SubmitAttempts          int                               `json:"submit_attempts"`
	LeaseOwner              *string                           `json:"lease_owner"`
	LeaseEpoch              int64                             `json:"lease_epoch"`
	LeaseExpiresAt          *time.Time                        `json:"lease_expires_at"`
	LastErrorKind           *string                           `json:"last_error_kind"`
	LastErrorCode           *string                           `json:"last_error_code"`
	LastErrorMessage        *string                           `json:"last_error_message"`
	CreatedAt               time.Time                         `json:"created_at"`
	UpdatedAt               time.Time                         `json:"updated_at"`
	SubmittedAt             *time.Time                        `json:"submitted_at"`
	StartedAt               *time.Time                        `json:"started_at"`
	FinishedAt              *time.Time                        `json:"finished_at"`
	SettledAt               *time.Time                        `json:"settled_at"`
	SubmissionUnknownAt     *time.Time                        `json:"submission_unknown_at"`
	QuarantinedAt           *time.Time                        `json:"quarantined_at"`
	DeletedAt               *time.Time                        `json:"deleted_at"`
}

func (row videoTaskRow) task() *service.VideoTask {
	task := &service.VideoTask{
		BillingReviewID:    row.BillingReviewID,
		SubmissionReviewID: row.SubmissionReviewID,
		ID:                 row.ID, PublicID: row.PublicID, Source: row.Source,
		APIKeyID: row.APIKeyID, GroupID: row.GroupID, ChannelID: row.ChannelID,
		AccountID: row.AccountID, AccountOwnerUserID: row.AccountOwnerUserID,
		Provider: row.Provider, Operation: row.Operation, ParentTaskID: row.ParentTaskID,
		RootTaskID: row.RootTaskID, Endpoint: row.Endpoint,
		RequestedModel: row.RequestedModel, PublicModel: row.PublicModel,
		ChannelModel: row.ChannelModel, UpstreamModel: row.UpstreamModel,
		RequestHash: row.RequestHash, IdempotencyKey: row.IdempotencyKey,
		InputManifest: row.InputManifest, RequestAttributes: row.RequestAttributes,
		ProviderTaskID: row.ProviderTaskID, ProviderStatus: row.ProviderStatus,
		ProviderCreatedAt: row.ProviderCreatedAt, ProviderFinishedAt: row.ProviderFinishedAt,
		StableClientToken: row.StableClientToken, GenerationState: row.GenerationState,
		BillingState: row.BillingState, DeleteState: row.DeleteState, Version: row.Version,
		Progress: row.Progress, UsageSnapshot: row.UsageSnapshot, ResponseMetadata: row.ResponseMetadata,
		ContentVariants: row.ContentVariants, ContentExpiresAt: row.ContentExpiresAt,
		ProviderAccessKind: row.ProviderAccessKind, ProviderAccessScope: row.ProviderAccessScope,
		ProviderAccessEnc: row.ProviderAccessEnc, ProviderAccessExpires: row.ProviderAccessExpiresAt,
		ProviderVideoURLEnc: row.ProviderVideoURLEnc, ProviderVideoProxyKey: row.ProviderVideoProxyKey,
		BillingUnit: row.BillingUnit, EstimatedUnits: row.EstimatedUnits, ActualUnits: row.ActualUnits,
		PriceSnapshot: row.PriceSnapshot, ProviderCostSnapshot: row.ProviderCostSnapshot,
		Currency: row.Currency, HoldID: row.HoldID, HoldAmount: row.HoldAmount,
		ActualCost: row.ActualCost, CallbackURLEnc: row.CallbackURLEnc,
		CallbackIntentState: row.CallbackIntentState,
		NextActionAt:        row.NextActionAt, PollAttempts: row.PollAttempts, SubmitAttempts: row.SubmitAttempts,
		LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt,
		LeaseEpoch:    row.LeaseEpoch,
		LastErrorKind: row.LastErrorKind, LastErrorCode: row.LastErrorCode, LastErrorMessage: row.LastErrorMessage,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SubmittedAt: row.SubmittedAt,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, SettledAt: row.SettledAt,
		SubmissionUnknownAt: row.SubmissionUnknownAt, QuarantinedAt: row.QuarantinedAt,
		DeletedAt: row.DeletedAt,
	}
	if row.UserID != nil {
		task.UserID = *row.UserID
	}
	return task
}

type videoJSONScanner interface {
	Scan(dest ...any) error
}

func scanVideoTask(scanner videoJSONScanner) (*service.VideoTask, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		return nil, err
	}
	var row videoTaskRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("decode video task row: %w", err)
	}
	return row.task(), nil
}

func (r *videoTaskRepository) GetVideoOperationalSnapshot(ctx context.Context) (*service.VideoOperationalSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task repository is not configured")
	}
	snapshot := &service.VideoOperationalSnapshot{}
	rows, err := r.db.QueryContext(ctx, `
		SELECT vt.provider, vt.operation, vt.generation_state, COUNT(*),
			MIN(COALESCE((
				SELECT MAX(vte.created_at)
				FROM video_task_events vte
				WHERE vte.task_id = vt.id AND vte.to_generation_state = vt.generation_state
			), vt.created_at))
		FROM video_tasks vt
		GROUP BY vt.provider, vt.operation, vt.generation_state
	`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var state service.VideoTaskStateSnapshot
		var oldest time.Time
		if err := rows.Scan(&state.Provider, &state.Operation, &state.State, &state.Count, &oldest); err != nil {
			_ = rows.Close()
			return nil, err
		}
		state.OldestEnteredAt = &oldest
		snapshot.TaskStates = append(snapshot.TaskStates, state)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE generation_state = 'submission_unknown'),
			COALESCE(SUM(hold_amount) FILTER (WHERE generation_state = 'submission_unknown'), 0),
			COALESCE(SUM(hold_amount) FILTER (WHERE billing_state IN ('held','capture_pending','release_pending','manual_review')), 0),
			MIN(COALESCE((
				SELECT MAX(vte.created_at) FROM video_task_events vte
				WHERE vte.task_id = vt.id AND vte.to_billing_state = vt.billing_state
			), vt.updated_at)) FILTER (WHERE billing_state IN ('capture_pending','release_pending')),
			MIN(COALESCE((
				SELECT MAX(vte.created_at) FROM video_task_events vte
				WHERE vte.task_id = vt.id AND vte.to_billing_state = vt.billing_state
			), vt.updated_at)) FILTER (WHERE billing_state = 'manual_review'),
			COUNT(*) FILTER (WHERE delete_state IN ('requested','deleting','delete_failed')),
			MIN(COALESCE((
				SELECT MIN(vte.created_at) FROM video_task_events vte
				WHERE vte.task_id=vt.id AND vte.event_type='delete_requested'
			), vt.updated_at)) FILTER (WHERE delete_state IN ('requested','deleting','delete_failed'))
		FROM video_tasks vt
	`).Scan(
		&snapshot.SubmissionUnknown,
		&snapshot.UnknownHoldAmount,
		&snapshot.HeldAmount,
		&snapshot.OldestSettlementPending,
		&snapshot.OldestManualReview,
		&snapshot.DeletePending,
		&snapshot.OldestDeletePending,
	); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *videoTaskRepository) CreateHeldVideoTask(ctx context.Context, params service.VideoCreateTaskParams) (_ *service.VideoTask, created bool, err error) {
	if r == nil || r.db == nil || r.billing == nil {
		return nil, false, errors.New("video task repository is not configured")
	}
	params.PublicID = strings.TrimSpace(params.PublicID)
	if params.PublicID == "" {
		params.PublicID = service.NewVideoTaskID()
	}
	if !service.IsValidVideoTaskID(params.PublicID) || params.Owner.UserID <= 0 || params.Owner.APIKeyID <= 0 ||
		params.AccountID <= 0 || strings.TrimSpace(params.RequestHash) == "" {
		return nil, false, service.ErrVideoInvalidRequest
	}
	if params.Operation == "" {
		params.Operation = service.VideoOperationGenerate
	}
	if params.Endpoint == "" {
		params.Endpoint = service.CompositeRouteEndpointVideos
	}
	if params.Provider == "" {
		params.Provider = service.VideoProviderOpenAI
	}
	if params.Currency == "" {
		params.Currency = "USD"
	}
	if params.HoldID == "" {
		params.HoldID = service.VideoTaskHoldRequestID(params.PublicID)
	}
	if params.StableClientToken == "" {
		params.StableClientToken = params.PublicID
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)
	params.IdempotencyKey = idempotencyKey
	budgetOwner, err := lockVideoBudgetOwnerTx(ctx, tx, params.Owner.UserID)
	if err != nil {
		return nil, false, err
	}
	if idempotencyKey != "" {
		existing, queryErr := scanVideoTask(tx.QueryRowContext(ctx, `
			SELECT to_jsonb(vt) FROM video_tasks vt
			WHERE user_id = $1 AND endpoint = $2 AND idempotency_key = $3
			FOR UPDATE
		`, params.Owner.UserID, params.Endpoint, idempotencyKey))
		if queryErr == nil {
			if existing.RequestHash != params.RequestHash {
				return nil, false, service.ErrVideoIdempotencyConflict
			}
			if err := tx.Commit(); err != nil {
				return nil, false, err
			}
			tx = nil
			return existing, false, nil
		}
		if !errors.Is(queryErr, sql.ErrNoRows) {
			return nil, false, queryErr
		}
	}

	var accountOwner sql.NullInt64
	createIntent, err := checkNativeVideoCreateIntentTx(ctx, tx, params)
	if err != nil {
		return nil, false, err
	}
	var budgetTime time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&budgetTime); err != nil {
		return nil, false, err
	}
	if err := checkVideoKeyBudgetTx(ctx, tx, params, budgetOwner, budgetTime); err != nil {
		return nil, false, err
	}
	if err := checkVideoBudgetGroupTx(ctx, tx, params.Owner.UserID, budgetOwner, params.Owner.GroupID); err != nil {
		return nil, false, err
	}
	var identityVersion, verifiedVersion int64
	var principalBindingID sql.NullInt64
	var isolationState string
	if err := tx.QueryRowContext(ctx, `
		SELECT owner_user_id, provider_identity_version, isolation_state, isolation_verified_version, provider_principal_binding_id
		FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
	`, params.AccountID).Scan(&accountOwner, &identityVersion, &isolationState, &verifiedVersion, &principalBindingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, service.ErrAccountNotFound
		}
		return nil, false, err
	}
	if accountOwner.Valid && (accountOwner.Int64 != params.Owner.UserID || params.AccountOwnerUserID == nil || *params.AccountOwnerUserID != accountOwner.Int64) {
		return nil, false, service.ErrVideoNoAccountAvailable
	}
	if !accountOwner.Valid && params.AccountOwnerUserID != nil {
		return nil, false, service.ErrVideoNoAccountAvailable
	}
	var accountAuthorized bool
	if err := tx.QueryRowContext(ctx, `SELECT account_user_can_schedule($1, $2)`, params.AccountID, params.Owner.UserID).Scan(&accountAuthorized); err != nil {
		return nil, false, err
	}
	if !accountAuthorized {
		return nil, false, service.ErrVideoNoAccountAvailable
	}
	if required, _ := params.RequestAttributes["requires_verified_isolation"].(bool); required &&
		(!accountOwner.Valid || isolationState != service.AccountIsolationVerified || verifiedVersion != identityVersion || !principalBindingID.Valid) {
		return nil, false, service.ErrVideoNoAccountAvailable
	}
	if requestedVersion, exists := params.RequestAttributes["account_identity_version"]; exists {
		encodedVersion, encodeErr := json.Marshal(requestedVersion)
		if encodeErr != nil || string(encodedVersion) != strconv.FormatInt(identityVersion, 10) {
			return nil, false, service.ErrVideoNoAccountAvailable
		}
	}
	attributes := make(map[string]any, len(params.RequestAttributes)+1)
	for name, value := range params.RequestAttributes {
		attributes[name] = value
	}
	attributes["account_identity_version"] = identityVersion
	params.RequestAttributes = attributes
	if params.Owner.GroupID != nil {
		var membership int
		if err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM account_groups WHERE account_id = $1 AND group_id = $2 FOR SHARE
		`, params.AccountID, *params.Owner.GroupID).Scan(&membership); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, service.ErrVideoNoAccountAvailable
			}
			return nil, false, err
		}
	}
	if params.MaxAccountConcurrency > 0 {
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM video_tasks
			WHERE account_id = $1
			  AND (
				generation_state IN ('submitting', 'submission_unknown', 'queued', 'in_progress')
				OR billing_state = 'held'
				OR billing_state = 'capture_pending'
			  )
		`, params.AccountID).Scan(&active); err != nil {
			return nil, false, err
		}
		if active >= params.MaxAccountConcurrency {
			return nil, false, service.ErrVideoAccountConcurrencyLimited
		}
	}
	if err := checkVideoPlatformBudgetTx(ctx, tx, params, budgetTime); err != nil {
		return nil, false, err
	}

	inputManifest, err := videoJSON(params.InputManifest, []service.VideoInputManifestEntry{})
	if err != nil {
		return nil, false, err
	}
	requestAttributes, err := videoJSON(params.RequestAttributes, map[string]any{})
	if err != nil {
		return nil, false, err
	}
	priceSnapshot, err := videoJSON(params.PriceSnapshot, map[string]any{})
	if err != nil {
		return nil, false, err
	}
	providerCostSnapshot, err := videoJSON(params.ProviderCostSnapshot, map[string]any{})
	if err != nil {
		return nil, false, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO video_tasks (
			public_id, user_id, api_key_id, group_id, channel_id, account_id, account_owner_user_id,
			provider, operation, parent_task_id, root_task_id, endpoint,
			requested_model, public_model, channel_model, upstream_model,
			request_hash, idempotency_key, input_manifest, request_attributes,
			stable_client_token, generation_state, billing_state, billing_unit,
			estimated_units, price_snapshot, provider_cost_snapshot, currency,
			hold_id, hold_amount, callback_url_enc, next_action_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, NULLIF($18, ''), $19::jsonb, $20::jsonb,
			$21, 'held', 'held', NULLIF($22, ''),
			$23, $24::jsonb, $25::jsonb, $26,
			$27, $28, NULLIF($29, ''), COALESCE($30::timestamptz, NOW())
		)
		RETURNING id
	`,
		params.PublicID, params.Owner.UserID, params.Owner.APIKeyID, params.Owner.GroupID,
		params.ChannelID, params.AccountID, params.AccountOwnerUserID,
		params.Provider, params.Operation, params.ParentTaskID, params.RootTaskID, params.Endpoint,
		params.RequestedModel, params.PublicModel, params.ChannelModel, params.UpstreamModel,
		params.RequestHash, params.IdempotencyKey, inputManifest, requestAttributes,
		params.StableClientToken, params.BillingUnit, params.EstimatedUnits,
		priceSnapshot, providerCostSnapshot, params.Currency,
		params.HoldID, params.HoldAmount, params.CallbackURLEnc, params.NextActionAt,
	).Scan(&id)
	if err != nil {
		insertErr := err
		if idempotencyKey != "" && isUniqueConstraintViolation(err) {
			_ = tx.Rollback()
			tx = nil
			existing, queryErr := scanVideoTask(r.db.QueryRowContext(ctx, `
				SELECT to_jsonb(vt) FROM video_tasks vt
				WHERE user_id = $1 AND endpoint = $2 AND idempotency_key = $3
			`, params.Owner.UserID, params.Endpoint, idempotencyKey))
			if queryErr == nil {
				if existing.RequestHash != params.RequestHash {
					return nil, false, service.ErrVideoIdempotencyConflict
				}
				return existing, false, nil
			}
		}
		return nil, false, insertErr
	}

	hold := &service.BalanceHoldCommand{
		RequestID:          params.HoldID,
		APIKeyID:           params.Owner.APIKeyID,
		RequestPayloadHash: params.RequestHash,
		UserID:             params.Owner.UserID,
		Scope:              service.BalanceHoldScopeVideoTask,
		RefID:              params.PublicID,
		HoldAmount:         params.HoldAmount,
	}
	if _, err := r.billing.reserveBalanceHoldTx(ctx, tx, hold); err != nil {
		if errors.Is(err, service.ErrBalanceHoldInsufficientBalance) {
			return nil, false, service.ErrVideoInsufficientBalance
		}
		return nil, false, err
	}
	if _, err := insertVideoTaskEventTx(ctx, tx, service.VideoTaskEvent{
		TaskID:              &id,
		EventType:           "balance_held",
		FromGenerationState: service.VideoGenerationPreparing,
		ToGenerationState:   service.VideoGenerationHeld,
		FromBillingState:    service.VideoBillingNone,
		ToBillingState:      service.VideoBillingHeld,
		EventHash:           hold.RequestFingerprint,
		Payload: map[string]any{
			"hold_amount": params.HoldAmount,
			"currency":    params.Currency,
		},
	}); err != nil {
		return nil, false, err
	}
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE id = $1`, id))
	if err != nil {
		return nil, false, err
	}
	if err := bindNativeVideoCreateIntentTx(ctx, tx, createIntent, task, idempotencyKey); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	tx = nil
	return task, true, nil
}

func (r *videoTaskRepository) GetVideoTaskForOwner(ctx context.Context, userID int64, publicID string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE user_id = $1 AND public_id = $2 AND source = 'managed'
	`, userID, strings.TrimSpace(publicID)))
	return translateVideoTaskNotFound(task, err)
}

func (r *videoTaskRepository) GetVideoTaskByProviderIDForOwner(ctx context.Context, userID int64, providerTaskID string) (*service.VideoTask, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE user_id = $1 AND provider_task_id = $2 AND source = 'managed'
		ORDER BY id DESC
		LIMIT 2
	`, userID, strings.TrimSpace(providerTaskID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var matched *service.VideoTask
	for rows.Next() {
		task, scanErr := scanVideoTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if matched != nil {
			return nil, service.ErrVideoInvalidRequest
		}
		matched = task
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if matched == nil {
		return nil, service.ErrVideoTaskNotFound
	}
	return matched, nil
}

func (r *videoTaskRepository) GetVideoTaskByProxyKeyForOwner(ctx context.Context, userID int64, proxyKey string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE user_id = $1 AND provider_video_proxy_key = $2 AND source = 'managed'
		ORDER BY id DESC
		LIMIT 1
	`, userID, strings.TrimSpace(proxyKey)))
	return translateVideoTaskNotFound(task, err)
}

func (r *videoTaskRepository) GetVideoTaskByProxyKey(ctx context.Context, proxyKey string) (*service.VideoTask, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE provider_video_proxy_key = $1 AND source = 'managed'
		ORDER BY id DESC
		LIMIT 2
	`, strings.TrimSpace(proxyKey))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var matched *service.VideoTask
	for rows.Next() {
		task, scanErr := scanVideoTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if matched != nil {
			return nil, service.ErrVideoTaskNotFound
		}
		matched = task
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if matched == nil {
		return nil, service.ErrVideoTaskNotFound
	}
	return matched, nil
}

func (r *videoTaskRepository) GetVideoTaskByIdempotency(ctx context.Context, userID int64, endpoint, idempotencyKey string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE user_id = $1 AND endpoint = $2 AND idempotency_key = $3 AND source = 'managed'
	`, userID, strings.TrimSpace(endpoint), strings.TrimSpace(idempotencyKey)))
	return translateVideoTaskNotFound(task, err)
}

func (r *videoTaskRepository) GetVideoTaskByPublicID(ctx context.Context, publicID string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE public_id = $1`, strings.TrimSpace(publicID)))
	return translateVideoTaskNotFound(task, err)
}

func (r *videoTaskRepository) GetVideoTaskByProviderID(ctx context.Context, provider string, accountID int64, providerTaskID string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE provider = $1 AND account_id = $2 AND provider_task_id = $3
	`, strings.TrimSpace(provider), accountID, strings.TrimSpace(providerTaskID)))
	return translateVideoTaskNotFound(task, err)
}

func translateVideoTaskNotFound(task *service.VideoTask, err error) (*service.VideoTask, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	return task, err
}

type videoTaskCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func encodeVideoTaskCursor(task *service.VideoTask) string {
	if task == nil {
		return ""
	}
	raw, _ := json.Marshal(videoTaskCursor{CreatedAt: task.CreatedAt, ID: task.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeVideoTaskCursor(value string) (*videoTaskCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, service.ErrVideoInvalidRequest
	}
	var cursor videoTaskCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return nil, service.ErrVideoInvalidRequest
	}
	return &cursor, nil
}

func (r *videoTaskRepository) ListVideoTasksForOwner(ctx context.Context, userID int64, filter service.VideoTaskFilter) (*service.VideoTaskPage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	order := strings.ToLower(strings.TrimSpace(filter.Order))
	if order != "asc" {
		order = "desc"
	}
	cursor, err := decodeVideoTaskCursor(filter.After)
	if err != nil && service.IsValidVideoTaskID(strings.TrimSpace(filter.After)) {
		anchor, lookupErr := r.GetVideoTaskForOwner(ctx, userID, strings.TrimSpace(filter.After))
		if lookupErr != nil {
			return nil, lookupErr
		}
		cursor = &videoTaskCursor{CreatedAt: anchor.CreatedAt, ID: anchor.ID}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	args := []any{userID}
	where := []string{"user_id = $1", "source = 'managed'"}
	add := func(condition string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		add("generation_state = $%d", value)
	}
	if value := strings.TrimSpace(filter.Model); value != "" {
		add("public_model = $%d", value)
	}
	if value := strings.TrimSpace(filter.Operation); value != "" {
		add("operation = $%d", value)
	}
	if cursor != nil {
		args = append(args, cursor.CreatedAt, cursor.ID)
		operator := "<"
		if order == "asc" {
			operator = ">"
		}
		where = append(where, fmt.Sprintf("(created_at, id) %s ($%d, $%d)", operator, len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE %s
		ORDER BY created_at %s, id %s
		LIMIT $%d
	`, strings.Join(where, " AND "), strings.ToUpper(order), strings.ToUpper(order), len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]*service.VideoTask, 0, limit+1)
	for rows.Next() {
		task, err := scanVideoTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	next := ""
	if hasMore && len(tasks) > 0 {
		next = encodeVideoTaskCursor(tasks[len(tasks)-1])
	}
	return &service.VideoTaskPage{Data: tasks, HasMore: hasMore, After: next}, nil
}

func (r *videoTaskRepository) TransitionVideoTask(ctx context.Context, publicID string, transition service.VideoTaskTransition) (_ *service.VideoTask, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE public_id = $1 FOR UPDATE`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	oldGeneration, oldBilling, oldDelete := task.GenerationState, task.BillingState, task.DeleteState
	localCancellation := oldGeneration == service.VideoGenerationHeld && oldBilling == service.VideoBillingHeld &&
		task.ProviderTaskID == nil && task.SubmittedAt == nil && task.SubmitAttempts == 0 &&
		transition.GenerationState == service.VideoGenerationCancelled && transition.BillingState == service.VideoBillingReleasePending
	guard, err := videoTaskGuardTx(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	leaseOwner, leaseEpoch := videoGuardLeaseValues(guard)
	if transition.GenerationState != "" {
		if !service.CanTransitionVideoGeneration(task.GenerationState, transition.GenerationState) {
			return nil, service.ErrVideoInvalidTransition
		}
		task.GenerationState = transition.GenerationState
	}
	if transition.BillingState != "" {
		if (transition.BillingState == service.VideoBillingCaptured || transition.BillingState == service.VideoBillingReleased) && transition.BillingState != task.BillingState {
			return nil, service.ErrVideoInvalidTransition
		}
		if !service.CanTransitionVideoBilling(task.BillingState, transition.BillingState) {
			return nil, service.ErrVideoInvalidTransition
		}
		task.BillingState = transition.BillingState
	}
	if transition.DeleteState != "" {
		if !service.CanTransitionVideoDelete(task.DeleteState, transition.DeleteState) &&
			!(localCancellation && task.DeleteState == service.VideoDeleteNone && transition.DeleteState == service.VideoDeleteDeleted) {
			return nil, service.ErrVideoInvalidTransition
		}
		task.DeleteState = transition.DeleteState
	}
	if transition.ProviderStatus != "" {
		task.ProviderStatus = stringPointer(transition.ProviderStatus)
	}
	if transition.Progress != nil {
		task.Progress = transition.Progress
	}
	if transition.UsageSnapshot != nil {
		task.UsageSnapshot = transition.UsageSnapshot
	}
	if transition.ResponseMetadata != nil {
		task.ResponseMetadata = transition.ResponseMetadata
	}
	if transition.ContentVariants != nil {
		task.ContentVariants = transition.ContentVariants
	}
	if transition.ContentExpiresAt != nil {
		task.ContentExpiresAt = transition.ContentExpiresAt
	}
	if transition.ProviderVideoURLEnc != "" {
		task.ProviderVideoURLEnc = stringPointer(transition.ProviderVideoURLEnc)
	}
	if transition.ProviderVideoProxyKey != "" {
		task.ProviderVideoProxyKey = stringPointer(transition.ProviderVideoProxyKey)
	}
	if transition.ActualUnits != nil {
		task.ActualUnits = transition.ActualUnits
	}
	if transition.ActualCost != nil {
		task.ActualCost = transition.ActualCost
	}
	task.NextActionAt = transition.NextActionAt
	task.LastErrorKind = nullableStringPointer(transition.ErrorKind)
	task.LastErrorCode = nullableStringPointer(transition.ErrorCode)
	task.LastErrorMessage = nullableStringPointer(transition.ErrorMessage)

	usageJSON, _ := videoJSON(task.UsageSnapshot, map[string]any{})
	metadataJSON, _ := videoJSON(task.ResponseMetadata, map[string]any{})
	variantsJSON, _ := videoJSON(task.ContentVariants, []string{})
	result, err := tx.ExecContext(ctx, `
		UPDATE video_tasks
		SET generation_state = $2, billing_state = $3, delete_state = $4,
			provider_status = $5, progress = $6, usage_snapshot = $7::jsonb,
			response_metadata = $8::jsonb, content_variants = $9::jsonb,
			content_expires_at = $10, actual_units = $11, actual_cost = $12,
			provider_video_url_enc = COALESCE(NULLIF($25, ''), provider_video_url_enc),
			provider_video_proxy_key = COALESCE(NULLIF($26, ''), provider_video_proxy_key),
			provider_finished_at = COALESCE($13, provider_finished_at),
			next_action_at = $14, last_error_kind = $15, last_error_code = $16,
			last_error_message = $17,
			quarantined_at = CASE WHEN $18 THEN COALESCE(quarantined_at, NOW()) ELSE quarantined_at END,
			submission_unknown_at = CASE WHEN $19 THEN COALESCE(submission_unknown_at, NOW()) ELSE submission_unknown_at END,
			submit_attempts = submit_attempts + CASE WHEN $20 THEN 1 ELSE 0 END,
			poll_attempts = poll_attempts + CASE WHEN $21 THEN 1 ELSE 0 END,
			started_at = CASE WHEN $2::varchar = 'in_progress' THEN COALESCE(started_at, NOW()) ELSE started_at END,
			finished_at = CASE WHEN $2::varchar IN ('completed','failed','cancelled','expired') THEN COALESCE(finished_at, NOW()) ELSE finished_at END,
			deleted_at = CASE WHEN $4::varchar = 'deleted' THEN COALESCE(deleted_at, NOW()) ELSE deleted_at END,
			version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $22
			AND ($23::text IS NULL OR (lease_owner = $23 AND lease_epoch = $24 AND lease_expires_at > clock_timestamp()))
	`, task.ID, task.GenerationState, task.BillingState, task.DeleteState,
		task.ProviderStatus, task.Progress, usageJSON, metadataJSON, variantsJSON,
		task.ContentExpiresAt, task.ActualUnits, task.ActualCost,
		transition.ProviderFinishedAt, task.NextActionAt,
		task.LastErrorKind, task.LastErrorCode, task.LastErrorMessage, transition.Quarantine,
		transition.SubmissionUnknown, transition.IncrementSubmitAttempts, transition.IncrementPollAttempts,
		guard.Version, leaseOwner, leaseEpoch, transition.ProviderVideoURLEnc, transition.ProviderVideoProxyKey)
	if err != nil {
		return nil, err
	}
	if err := videoGuardWriteResult(result, guard); err != nil {
		return nil, err
	}
	if task.Operation == service.VideoOperationCharacterCreate && task.DeleteState == service.VideoDeleteDeleted {
		// Commit resource retirement with the fenced task transition, so a crash
		// cannot leave a ready character behind after reporting deletion.
		if _, err := tx.ExecContext(ctx, `UPDATE video_resources
			SET status='deleted', deleted_at=COALESCE(deleted_at,NOW()), version=version+1, updated_at=NOW()
			WHERE source_task_id=$1 AND deleted_at IS NULL`, task.ID); err != nil {
			return nil, err
		}
	}
	eventType := strings.TrimSpace(transition.EventType)
	if eventType == "" {
		eventType = "task_transition"
	}
	if _, err := insertVideoTaskEventTx(ctx, tx, service.VideoTaskEvent{
		TaskID: &task.ID, EventType: eventType, Provider: task.Provider,
		AccountID: task.AccountID, ProviderTaskID: valueOrEmptyString(task.ProviderTaskID),
		FromGenerationState: oldGeneration, ToGenerationState: task.GenerationState,
		FromBillingState: oldBilling, ToBillingState: task.BillingState,
		Payload:   transition.EventPayload,
		EventHash: transitionEventHash(task.ID, task.Version+1, eventType, oldDelete, task.DeleteState),
	}); err != nil {
		return nil, err
	}
	task, err = scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE id = $1`, task.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return task, nil
}

func (r *videoTaskRepository) SaveVideoProviderAccepted(ctx context.Context, publicID string, acceptance service.VideoProviderAcceptance) (_ *service.VideoTask, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	task, err := saveVideoProviderAcceptedTx(ctx, tx, publicID, acceptance)
	if err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

func saveVideoProviderAcceptedTx(ctx context.Context, tx *sql.Tx, publicID string, acceptance service.VideoProviderAcceptance) (*service.VideoTask, error) {
	if strings.TrimSpace(acceptance.ProviderTaskID) == "" {
		return nil, service.ErrVideoInvalidRequest
	}
	target := acceptance.GenerationState
	if target == "" {
		target = service.VideoGenerationQueued
	}
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE public_id = $1 FOR UPDATE`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	guard, err := videoTaskGuardTx(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	leaseOwner, leaseEpoch := videoGuardLeaseValues(guard)
	if !service.CanTransitionVideoGeneration(task.GenerationState, target) {
		return nil, service.ErrVideoInvalidTransition
	}
	targetBilling := task.BillingState
	if acceptance.BillingState != "" {
		if (acceptance.BillingState == service.VideoBillingCaptured || acceptance.BillingState == service.VideoBillingReleased) && acceptance.BillingState != task.BillingState {
			return nil, service.ErrVideoInvalidTransition
		}
		if !service.CanTransitionVideoBilling(task.BillingState, acceptance.BillingState) {
			return nil, service.ErrVideoInvalidTransition
		}
		targetBilling = acceptance.BillingState
	}
	usageJSON, _ := videoJSON(acceptance.UsageSnapshot, map[string]any{})
	metadataJSON, _ := videoJSON(acceptance.ResponseMetadata, map[string]any{})
	variantsJSON, _ := videoJSON(acceptance.ContentVariants, []string{})
	result, err := tx.ExecContext(ctx, `
		UPDATE video_tasks
		SET provider_task_id = $2, provider_status = NULLIF($3, ''), provider_created_at = $4,
			provider_finished_at = COALESCE($5, provider_finished_at), generation_state = $6,
			billing_state = $7, progress = $8,
			usage_snapshot = $9::jsonb, response_metadata = $10::jsonb,
			content_variants = $11::jsonb, content_expires_at = $12,
			provider_access_kind = NULLIF($13, ''), provider_access_scope = NULLIF($14, ''),
			provider_access_enc = NULLIF($15, ''), provider_access_expires_at = $16,
			provider_video_url_enc = COALESCE(NULLIF($27, ''), provider_video_url_enc),
			provider_video_proxy_key = COALESCE(NULLIF($28, ''), provider_video_proxy_key),
			next_action_at = $17, actual_units = COALESCE($18, actual_units),
			actual_cost = COALESCE($19, actual_cost),
			last_error_kind = NULLIF($20, ''), last_error_code = NULLIF($21, ''),
			last_error_message = NULLIF($22, ''),
			quarantined_at = CASE WHEN $23 THEN COALESCE(quarantined_at, NOW()) ELSE quarantined_at END,
			submitted_at = COALESCE(submitted_at, NOW()), submit_attempts = submit_attempts + 1,
			started_at = CASE WHEN $6::varchar = 'in_progress' THEN COALESCE(started_at, NOW()) ELSE started_at END,
			finished_at = CASE WHEN $6::varchar IN ('completed','failed','cancelled','expired') THEN COALESCE(finished_at, NOW()) ELSE finished_at END,
			version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $24
			AND ($25::text IS NULL OR (lease_owner = $25 AND lease_epoch = $26 AND lease_expires_at > clock_timestamp()))
	`, task.ID, strings.TrimSpace(acceptance.ProviderTaskID), acceptance.ProviderStatus,
		acceptance.ProviderCreatedAt, acceptance.ProviderFinishedAt, target, targetBilling, acceptance.Progress,
		usageJSON, metadataJSON, variantsJSON, acceptance.ContentExpiresAt, acceptance.ProviderAccessKind,
		acceptance.ProviderAccessScope, acceptance.ProviderAccessEnc,
		acceptance.ProviderAccessExpiresAt, acceptance.NextActionAt, acceptance.ActualUnits, acceptance.ActualCost,
		acceptance.ErrorKind, acceptance.ErrorCode, acceptance.ErrorMessage, acceptance.Quarantine,
		guard.Version, leaseOwner, leaseEpoch, acceptance.ProviderVideoURLEnc, acceptance.ProviderVideoProxyKey)
	if err != nil {
		return nil, err
	}
	if err := videoGuardWriteResult(result, guard); err != nil {
		return nil, err
	}
	if _, err := insertVideoTaskEventTx(ctx, tx, service.VideoTaskEvent{
		TaskID: &task.ID, EventType: "provider_accepted", Provider: task.Provider,
		AccountID: task.AccountID, ProviderTaskID: acceptance.ProviderTaskID,
		FromGenerationState: task.GenerationState, ToGenerationState: target,
		FromBillingState: task.BillingState, ToBillingState: targetBilling,
		Payload:   map[string]any{"provider_status": acceptance.ProviderStatus},
		EventHash: transitionEventHash(task.ID, task.Version+1, "provider_accepted", "", ""),
	}); err != nil {
		return nil, err
	}
	task, err = scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE id = $1`, task.ID))
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *videoTaskRepository) MarkVideoSubmissionUnknown(ctx context.Context, publicID string, providerError *service.VideoProviderError, nextActionAt time.Time) (*service.VideoTask, error) {
	kind, code, message := "transport", "submission_unknown", "video provider submission outcome is unknown"
	if providerError != nil {
		kind, code, message = providerError.Kind, providerError.Code, providerError.Message
	}
	task, err := r.TransitionVideoTask(ctx, publicID, service.VideoTaskTransition{
		GenerationState: service.VideoGenerationSubmissionUnknown,
		NextActionAt:    &nextActionAt, SubmissionUnknown: true, IncrementSubmitAttempts: true,
		ErrorKind: kind, ErrorCode: code, ErrorMessage: message,
		EventType: "submission_unknown",
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *videoTaskRepository) ClaimVideoTask(ctx context.Context, publicID, workerID string, lease time.Duration) (*service.VideoTask, error) {
	publicID = strings.TrimSpace(publicID)
	workerID = strings.TrimSpace(workerID)
	if !service.IsValidVideoTaskID(publicID) || workerID == "" {
		return nil, service.ErrVideoInvalidRequest
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 90
	}
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id FROM video_tasks
				WHERE public_id = $1
				  AND (next_action_at IS NULL OR next_action_at <= NOW())
				  AND (lease_owner IS NULL OR lease_expires_at <= clock_timestamp())
				  AND (
					(generation_state IN ('held','submitting') AND billing_state = 'held')
				OR (generation_state IN ('submission_unknown','queued','in_progress') AND billing_state = 'held')
					OR billing_state IN ('capture_pending','release_pending')
					OR (generation_state IN ('completed','failed','cancelled','expired') AND billing_state = 'held')
				OR (delete_state IN ('requested','deleting','delete_failed')
					AND billing_state IN ('captured','released')
					AND generation_state IN ('completed','failed','cancelled','expired'))
			  )
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_tasks target
		SET lease_owner = $2,
			lease_expires_at = clock_timestamp() + ($3 * INTERVAL '1 second'),
			lease_epoch = lease_epoch + 1,
			updated_at = NOW()
		FROM candidate
		WHERE target.id = candidate.id
		RETURNING to_jsonb(target)
	`, publicID, workerID, leaseSeconds))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return task, err
}

func (r *videoTaskRepository) ClaimDueVideoTasks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*service.VideoTask, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, errors.New("video worker id is required")
	}
	if limit <= 0 {
		limit = 32
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 90
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM video_tasks
				WHERE (next_action_at IS NULL OR next_action_at <= NOW())
				  AND (lease_owner IS NULL OR lease_expires_at <= clock_timestamp())
				  AND (
					(generation_state IN ('held','submitting') AND billing_state = 'held')
				OR
				(generation_state IN ('submission_unknown','queued','in_progress') AND billing_state = 'held')
					OR billing_state IN ('capture_pending','release_pending')
					OR (generation_state IN ('completed','failed','cancelled','expired') AND billing_state = 'held')
				OR (delete_state IN ('requested','deleting','delete_failed')
					AND billing_state IN ('captured','released')
					AND generation_state IN ('completed','failed','cancelled','expired'))
			  )
			ORDER BY next_action_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_tasks vt
		SET lease_owner = $1,
			lease_expires_at = clock_timestamp() + ($3 * INTERVAL '1 second'),
			lease_epoch = lease_epoch + 1,
			updated_at = NOW()
		FROM candidates c
		WHERE vt.id = c.id
		RETURNING to_jsonb(vt)
	`, workerID, limit, leaseSeconds)
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

func (r *videoTaskRepository) ReleaseVideoTaskLease(ctx context.Context, lease service.VideoTaskLease, nextActionAt *time.Time) error {
	if lease.TaskID <= 0 || lease.Epoch <= 0 || strings.TrimSpace(lease.Owner) == "" {
		return service.ErrVideoLeaseLost
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_tasks
		SET lease_owner = NULL, lease_expires_at = NULL,
			next_action_at = CASE WHEN version = $5 THEN COALESCE($3, next_action_at) ELSE next_action_at END, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2 AND lease_epoch = $4 AND lease_expires_at > clock_timestamp()
	`, lease.TaskID, strings.TrimSpace(lease.Owner), nextActionAt, lease.Epoch, lease.Version)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrVideoLeaseLost
	}
	return nil
}

func (r *videoTaskRepository) ClearExpiredVideoProviderAccess(ctx context.Context, limit int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("video task repository is not configured")
	}
	if limit <= 0 {
		limit = 128
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var cleared int64
	for _, statement := range []string{
		`WITH expired AS (
			SELECT id FROM video_tasks
			WHERE provider_access_enc IS NOT NULL
			  AND provider_access_expires_at IS NOT NULL
			  AND provider_access_expires_at <= NOW()
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_tasks target
		SET provider_access_kind = NULL, provider_access_scope = NULL,
			provider_access_enc = NULL, provider_access_expires_at = NULL,
			version = version + 1, updated_at = NOW()
		FROM expired
		WHERE target.id = expired.id`,
		`WITH expired AS (
			SELECT id FROM video_resources
			WHERE provider_access_enc IS NOT NULL
			  AND provider_access_expires_at IS NOT NULL
			  AND provider_access_expires_at <= NOW()
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_resources target
		SET provider_access_kind = NULL, provider_access_scope = NULL,
			provider_access_enc = NULL, provider_access_expires_at = NULL,
			version = version + 1, updated_at = NOW()
		FROM expired
		WHERE target.id = expired.id`,
	} {
		result, execErr := tx.ExecContext(ctx, statement, limit)
		if execErr != nil {
			return 0, execErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, rowsErr
		}
		cleared += rows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return cleared, nil
}

func (r *videoTaskRepository) AppendVideoTaskEvent(ctx context.Context, event service.VideoTaskEvent) (bool, error) {
	if lease, leased := service.VideoTaskLeaseFromContext(ctx); leased {
		if event.TaskID == nil || *event.TaskID != lease.TaskID {
			return false, service.ErrVideoLeaseLost
		}
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		if err := checkVideoTaskLeaseTx(ctx, tx, lease); err != nil {
			return false, err
		}
		created, err := insertVideoTaskEventTx(ctx, tx, event)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return created, nil
	}
	return insertVideoTaskEventSQL(ctx, r.db, event)
}

func insertVideoTaskEventTx(ctx context.Context, tx *sql.Tx, event service.VideoTaskEvent) (bool, error) {
	return insertVideoTaskEventSQL(ctx, tx, event)
}

type videoEventExecutor interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func insertVideoTaskEventSQL(ctx context.Context, exec videoEventExecutor, event service.VideoTaskEvent) (bool, error) {
	payload, err := videoJSON(event.Payload, map[string]any{})
	if err != nil {
		return false, err
	}
	var id int64
	err = exec.QueryRowContext(ctx, `
		INSERT INTO video_task_events (
			task_id, event_type, provider, account_id, provider_task_id, provider_event_id,
			from_generation_state, to_generation_state, from_billing_state, to_billing_state,
			payload, event_hash
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''),
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), $11::jsonb, NULLIF($12, ''))
		ON CONFLICT DO NOTHING
		RETURNING id
	`, event.TaskID, event.EventType, event.Provider, event.AccountID, event.ProviderTaskID,
		event.ProviderEventID, event.FromGenerationState, event.ToGenerationState,
		event.FromBillingState, event.ToBillingState, payload, event.EventHash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func videoJSON(value any, fallback any) (string, error) {
	if value == nil || (reflect.ValueOf(value).Kind() == reflect.Map && reflect.ValueOf(value).IsNil()) ||
		(reflect.ValueOf(value).Kind() == reflect.Slice && reflect.ValueOf(value).IsNil()) {
		value = fallback
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func nullableStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPointer(value string) *string { return &value }

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func transitionEventHash(taskID, version int64, eventType, fromDelete, toDelete string) string {
	value, _ := service.HashVideoRequest(map[string]any{
		"task_id": taskID, "version": version, "event_type": eventType,
		"from_delete": fromDelete, "to_delete": toDelete,
	})
	return value
}
