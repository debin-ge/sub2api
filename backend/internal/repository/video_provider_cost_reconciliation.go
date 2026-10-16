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

// Called after locking the budget owner and before locking the task. The account
// is selected from the task's persisted identity, never from a caller-supplied
// account ID. Match admission's user -> account -> task order so account deletion
// (which can update task references) cannot introduce the opposite lock order.
func lockVideoProviderCostAccountTx(ctx context.Context, tx *sql.Tx, taskID, userID, apiKeyID int64, publicID string) error {
	var accountID int64
	err := tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN video_tasks vt ON vt.account_id = a.id
		WHERE vt.id = $1 AND vt.user_id = $2 AND vt.api_key_id = $3 AND vt.public_id = $4 FOR UPDATE OF a`,
		taskID, userID, apiKeyID, publicID).Scan(&accountID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// A deleted/null account still gets a ledger item; the task identity itself
	// is checked under its row lock by completeVideoBalanceSettlementTx.
	return nil
}

// Called inside the same transaction as capture/release. Failure must roll back
// the customer settlement too, so the existing durable outbox retries both.
func recordUncertainVideoProviderCostTx(ctx context.Context, tx *sql.Tx, taskID int64, action service.BalanceSettlementAction) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO video_provider_costs (task_id, task_public_id, user_id, account_id, provider, reason,
			provider_task_id, stable_client_token, account_identity_version)
		SELECT id, public_id, user_id, account_id, provider,
			CASE WHEN provider = 'flova' THEN 'flova_spend_unverified'
			 WHEN generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true' THEN 'local_expiry_cost_unverified'
			 ELSE 'submission_outcome_unknown' END,
			COALESCE(provider_task_id, ''), COALESCE(stable_client_token, ''),
			CASE WHEN (request_attributes->>'account_identity_version') ~ '^[0-9]{1,18}$'
				THEN (request_attributes->>'account_identity_version')::BIGINT END
		FROM video_tasks vt
		WHERE id = $1 AND source = 'managed'
		  AND (($2 = 'release' AND ((submission_unknown_at IS NOT NULL AND NULLIF(provider_task_id, '') IS NULL)
			OR (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true')
			OR EXISTS (SELECT 1 FROM video_task_events e WHERE e.task_id = vt.id
				AND e.event_type IN ('submission_unknown_auto_failed', 'submission_reconciliation_expired'))))
			OR (provider = 'flova' AND (provider_task_id IS NOT NULL OR submission_unknown_at IS NOT NULL)))
		ON CONFLICT (task_public_id) DO NOTHING
	`, taskID, action)
	return err
}

// The caller already holds both the user budget row and selected account row.
// Reserve a risk slot before submission, so concurrent first requests cannot
// all pass before their outcomes become unknown. Acceptance frees this slot;
// unresolved submissions keep it until an atomic refund/ledger transfer.
func checkVideoProviderCostExposureTx(ctx context.Context, tx *sql.Tx, userID, accountID int64) error {
	var userCount, accountCount int
	err := tx.QueryRowContext(ctx, `
		SELECT
		 (SELECT COUNT(*) FROM (
			SELECT task_public_id FROM video_provider_costs WHERE user_id = $1 AND state = 'unresolved'
			UNION SELECT public_id FROM video_tasks WHERE user_id = $1 AND source = 'managed'
			  AND billing_state NOT IN ('released', 'captured')
			  AND ((NULLIF(provider_task_id, '') IS NULL AND (submission_unknown_at IS NOT NULL OR generation_state IN ('preparing', 'held', 'submitting'))) OR
			    (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true'))
		 ) u),
		 (SELECT COUNT(*) FROM (
			SELECT task_public_id FROM video_provider_costs WHERE account_id = $2 AND state = 'unresolved'
			UNION SELECT public_id FROM video_tasks WHERE account_id = $2 AND source = 'managed'
			  AND billing_state NOT IN ('released', 'captured')
			  AND ((NULLIF(provider_task_id, '') IS NULL AND (submission_unknown_at IS NOT NULL OR generation_state IN ('preparing', 'held', 'submitting'))) OR
			    (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true'))
		 ) a)
	`, userID, accountID).Scan(&userCount, &accountCount)
	if err != nil {
		return err
	}
	if userCount >= service.VideoProviderCostMaxUnresolvedPerUser || accountCount >= service.VideoProviderCostMaxUnresolvedPerAccount {
		return service.ErrVideoProviderCostLimited
	}
	return nil
}

func scanVideoProviderCost(row interface{ Scan(...any) error }) (*service.VideoProviderCost, error) {
	var body []byte
	if err := row.Scan(&body); err != nil {
		return nil, err
	}
	// Internal fields have json:"-" on the API model. Decode them explicitly
	// for worker use without ever exposing supplier handles in list responses.
	result := new(service.VideoProviderCost)
	private := struct {
		*service.VideoProviderCost
		ProviderTaskID         string `json:"provider_task_id"`
		StableClientToken      string `json:"stable_client_token"`
		AccountIdentityVersion *int64 `json:"account_identity_version"`
		ResolutionKey          string `json:"resolution_key"`
	}{VideoProviderCost: result}
	if err := json.Unmarshal(body, &private); err != nil {
		return nil, err
	}
	result.ProviderTaskID, result.StableClientToken = private.ProviderTaskID, private.StableClientToken
	result.AccountIdentityVersion, result.ResolutionKey = private.AccountIdentityVersion, private.ResolutionKey
	return result, nil
}

func (r *videoTaskRepository) ClaimVideoProviderCosts(ctx context.Context, worker string, limit int) ([]*service.VideoProviderCost, error) {
	if strings.TrimSpace(worker) == "" || limit < 1 || limit > 100 {
		return nil, service.ErrVideoInvalidRequest
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH due AS (
			SELECT id FROM video_provider_costs WHERE state = 'unresolved' AND next_action_at <= NOW()
			 AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
			 ORDER BY next_action_at, id FOR UPDATE SKIP LOCKED LIMIT $2
		), claimed AS (
			UPDATE video_provider_costs c SET lease_owner = $1, lease_expires_at = NOW() + INTERVAL '2 minutes',
			 attempts = attempts + 1, version = version + 1, updated_at = NOW()
			FROM due WHERE c.id = due.id RETURNING c.*
		) SELECT to_jsonb(claimed) FROM claimed
	`, worker, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.VideoProviderCost, 0, limit)
	for rows.Next() {
		item, err := scanVideoProviderCost(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *videoTaskRepository) RecordVideoProviderCostObservation(ctx context.Context, item *service.VideoProviderCost, worker, providerID, status, reason string, next *time.Time) error {
	if item == nil || item.ID <= 0 || len(providerID) > 8192 || len(status) > 64 || len(reason) > 128 {
		return service.ErrVideoInvalidRequest
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_provider_costs SET observation_status = $4, last_error = $5, next_action_at = $6,
		 provider_task_id = COALESCE(NULLIF($7, ''), provider_task_id),
		 lease_owner = NULL, lease_expires_at = NULL, version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $2 AND lease_owner = $3 AND lease_expires_at > NOW() AND state = 'unresolved'
	`, item.ID, item.Version, worker, status, reason, next, providerID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrVideoProviderCostConflict
	}
	return nil
}

func (r *videoTaskRepository) ListVideoProviderCosts(ctx context.Context, state string, after int64, limit int) (*service.VideoProviderCostPage, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, service.ErrVideoInvalidRequest
	}
	rows, err := r.db.QueryContext(ctx, `SELECT to_jsonb(c) FROM video_provider_costs c
		WHERE id > $1 AND ($2 = '' OR state = $2) ORDER BY id LIMIT $3`, after, state, limit+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	page := &service.VideoProviderCostPage{Items: make([]*service.VideoProviderCost, 0, limit)}
	for rows.Next() {
		item, err := scanVideoProviderCost(rows)
		if err != nil {
			return nil, err
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, item)
		page.NextAfter = item.ID
	}
	return page, rows.Err()
}

func (r *videoTaskRepository) ResolveVideoProviderCost(ctx context.Context, id int64, in service.VideoProviderCostResolution) (_ *service.VideoProviderCost, err error) {
	if id <= 0 {
		return nil, service.ErrVideoInvalidRequest
	}
	if err := service.ValidateVideoProviderCostResolution(in); err != nil {
		return nil, err
	}
	amount := service.QuantizeUsageBillingAmount(*in.Amount)
	if in.State == "resolved_cost_recorded" && amount <= 0 {
		return nil, service.ErrVideoInvalidRequest
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := scanVideoProviderCost(tx.QueryRowContext(ctx, `SELECT to_jsonb(c) FROM video_provider_costs c WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if item.State != "unresolved" {
		if item.ResolutionKey != in.IdempotencyKey || item.State != in.State || item.Amount == nil || *item.Amount != amount ||
			item.Currency != in.Currency || item.Evidence != strings.TrimSpace(in.Evidence) || item.ResolvedBy == nil || *item.ResolvedBy != in.ActorID {
			return nil, service.ErrVideoProviderCostConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return item, nil
	}
	if item.Version != in.ExpectedVersion {
		return nil, service.ErrVideoProviderCostConflict
	}
	resolved, err := scanVideoProviderCost(tx.QueryRowContext(ctx, `
		UPDATE video_provider_costs c SET state = $3, amount = $4, currency = $5, evidence = $6,
		 resolved_by = $7, resolution_key = $8, resolved_at = NOW(), next_action_at = NULL,
		 lease_owner = NULL, lease_expires_at = NULL, version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $2 AND state = 'unresolved' RETURNING to_jsonb(c)
	`, id, in.ExpectedVersion, in.State, amount, in.Currency, strings.TrimSpace(in.Evidence), in.ActorID, in.IdempotencyKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoProviderCostConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return resolved, nil
}

var _ service.VideoProviderCostRepository = (*videoTaskRepository)(nil)

// RecordVideoProviderCostBalance stores one balance observation and releases the
// lease. The observation time only moves when the balance changed, so equal
// readings accumulate stability instead of restarting the wait.
func (r *videoTaskRepository) RecordVideoProviderCostBalance(ctx context.Context, item *service.VideoProviderCost, worker string, balance *float64, balanceChanged bool, reason string, next *time.Time) error {
	if item == nil || item.ID <= 0 || len(reason) > 128 || (balance != nil && !service.ValidVideoProviderObservedBalance(*balance)) {
		return service.ErrVideoInvalidRequest
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_provider_costs SET
		 observed_balance = CASE WHEN $4::NUMERIC IS NULL THEN observed_balance ELSE $4::NUMERIC END,
		 observed_balance_at = CASE WHEN $4::NUMERIC IS NULL THEN observed_balance_at
			WHEN $5 OR observed_balance_at IS NULL THEN NOW() ELSE observed_balance_at END,
		 last_error = $6, next_action_at = $7,
		 lease_owner = NULL, lease_expires_at = NULL, version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $2 AND lease_owner = $3 AND lease_expires_at > NOW() AND state = 'unresolved'
	`, item.ID, item.Version, worker, balance, balanceChanged, reason, next)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrVideoProviderCostConflict
	}
	return nil
}

// AutoResolveVideoProviderCost closes a claimed record from verified supplier
// accounting. The lease and version guard make a concurrent admin resolution or
// a second worker lose rather than overwrite.
func (r *videoTaskRepository) AutoResolveVideoProviderCost(ctx context.Context, item *service.VideoProviderCost, worker, state string, amount float64, currency, evidence, key string) error {
	if item == nil || item.ID <= 0 || !strings.HasPrefix(key, "auto:") || len(key) > 128 {
		return service.ErrVideoInvalidRequest
	}
	amount = service.QuantizeUsageBillingAmount(amount)
	if (state != "resolved_no_charge" && state != "resolved_cost_recorded") ||
		(state == "resolved_no_charge" && amount != 0) || (state == "resolved_cost_recorded" && amount <= 0) {
		return service.ErrVideoInvalidRequest
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_provider_costs SET state = $4, amount = $5, currency = $6, evidence = $7,
		 resolved_by = NULL, resolution_key = $8, resolved_at = NOW(), next_action_at = NULL,
		 lease_owner = NULL, lease_expires_at = NULL, version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $2 AND lease_owner = $3 AND lease_expires_at > NOW() AND state = 'unresolved'
	`, item.ID, item.Version, worker, state, amount, currency, strings.TrimSpace(evidence), key)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrVideoProviderCostConflict
	}
	return nil
}

var _ service.VideoProviderCostAutoResolver = (*videoTaskRepository)(nil)
