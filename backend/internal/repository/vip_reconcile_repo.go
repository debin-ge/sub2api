package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type vipReconcileRepository struct {
	db *sql.DB
}

func NewVIPReconcileRepository(db *sql.DB) service.VIPReconcileRepository {
	return &vipReconcileRepository{db: db}
}

func (r *vipReconcileRepository) DatabaseNow(ctx context.Context) (time.Time, error) {
	if r == nil || r.db == nil {
		return time.Time{}, fmt.Errorf("VIP reconcile database is not configured")
	}
	var now time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&now); err != nil {
		return time.Time{}, err
	}
	return now.UTC(), nil
}

const vipQualifyingOrderPredicate = `
	po.order_type IN ('balance', 'subscription')
	AND po.pay_amount > 0
	AND po.paid_at IS NOT NULL
	AND po.status IN (
		'COMPLETED',
		'REFUND_REQUESTED',
		'REFUNDING',
		'REFUND_PENDING',
		'PARTIALLY_REFUNDED',
		'REFUNDED',
		'REFUND_FAILED'
	)
`

func (r *vipReconcileRepository) Preview(
	ctx context.Context,
	asOf time.Time,
	afterOrderID int64,
	limit int,
) (*service.VIPReconcilePreview, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("VIP reconcile database is not configured")
	}
	if afterOrderID < 0 {
		afterOrderID = 0
	}
	if limit <= 0 {
		limit = 50
	}

	statsQuery := `
		WITH valid_orders AS (
			SELECT DISTINCT ON (fact.user_id)
				fact.user_id,
				fact.order_id,
				fact.completed_at
			FROM vip_qualifying_payment_order_facts fact
			WHERE fact.completed_at <= $1
			ORDER BY fact.user_id, fact.completed_at ASC, fact.order_id ASC
		),
		repair_candidates AS (
			SELECT
				vo.user_id,
				vo.order_id,
				vo.completed_at,
				u.id AS existing_user_id,
				u.deleted_at,
				u.vip_paid_eligible,
				u.vip_manual_override,
				u.is_vip
			FROM valid_orders vo
			LEFT JOIN users u ON u.id = vo.user_id
			WHERE u.id IS NULL
			   OR u.deleted_at IS NOT NULL
			   OR NOT u.vip_paid_eligible
		),
		invalid_orders AS (
			SELECT po.id
			FROM payment_orders po
			WHERE ` + vipQualifyingOrderPredicate + `
			  AND po.completed_at IS NULL
			  AND po.updated_at <= $1
		)
		SELECT
			COUNT(*) FILTER (
				WHERE existing_user_id IS NOT NULL
				  AND deleted_at IS NULL
				  AND NOT vip_paid_eligible
			) AS eligibility_repair,
			COUNT(*) FILTER (
				WHERE existing_user_id IS NOT NULL
				  AND deleted_at IS NULL
				  AND NOT vip_paid_eligible
				  AND vip_manual_override IS NULL
				  AND NOT is_vip
			) AS effective_change,
			COUNT(*) FILTER (
				WHERE existing_user_id IS NOT NULL
				  AND deleted_at IS NULL
				  AND NOT vip_paid_eligible
				  AND vip_manual_override IS FALSE
			) AS force_off_unchanged,
			(SELECT COUNT(*) FROM invalid_orders) AS invalid_order,
			COUNT(*) FILTER (
				WHERE existing_user_id IS NULL OR deleted_at IS NOT NULL
			) AS deleted_user
		FROM repair_candidates
	`

	var stats service.VIPReconcilePreviewStats
	if err := r.db.QueryRowContext(ctx, statsQuery, asOf.UTC()).Scan(
		&stats.EligibilityRepair,
		&stats.EffectiveChange,
		&stats.ForceOffUnchanged,
		&stats.InvalidOrder,
		&stats.DeletedUser,
	); err != nil {
		return nil, fmt.Errorf("load VIP reconcile preview stats: %w", err)
	}

	itemsQuery := `
		WITH valid_orders AS (
			SELECT DISTINCT ON (fact.user_id)
				fact.user_id,
				fact.order_id,
				fact.completed_at
			FROM vip_qualifying_payment_order_facts fact
			WHERE fact.completed_at <= $1
			ORDER BY fact.user_id, fact.completed_at ASC, fact.order_id ASC
		),
		repair_candidates AS (
			SELECT
				CASE
					WHEN u.id IS NULL OR u.deleted_at IS NOT NULL THEN 'DELETED_USER'
					WHEN u.vip_manual_override IS FALSE THEN 'FORCE_OFF_UNCHANGED'
					WHEN u.vip_manual_override IS NULL AND NOT u.is_vip THEN 'EFFECTIVE_CHANGE'
					ELSE 'ELIGIBILITY_REPAIR'
				END AS category,
				vo.user_id,
				vo.order_id,
				vo.completed_at,
				u.vip_manual_override,
				COALESCE(u.is_vip, false) AS current_is_vip
			FROM valid_orders vo
			LEFT JOIN users u ON u.id = vo.user_id
			WHERE u.id IS NULL
			   OR u.deleted_at IS NOT NULL
			   OR NOT u.vip_paid_eligible
		),
		invalid_orders AS (
			SELECT
				'INVALID_ORDER'::text AS category,
				po.user_id,
				po.id AS order_id,
				NULL::timestamptz AS completed_at,
				u.vip_manual_override,
				COALESCE(u.is_vip, false) AS current_is_vip
			FROM payment_orders po
			LEFT JOIN users u ON u.id = po.user_id
			WHERE ` + vipQualifyingOrderPredicate + `
			  AND po.completed_at IS NULL
			  AND po.updated_at <= $1
		),
		all_items AS (
			SELECT * FROM repair_candidates
			UNION ALL
			SELECT * FROM invalid_orders
		)
		SELECT
			category,
			user_id,
			order_id,
			completed_at,
			vip_manual_override,
			current_is_vip
		FROM all_items
		WHERE order_id > $2
		ORDER BY order_id ASC
		LIMIT $3
	`
	rows, err := r.db.QueryContext(
		ctx,
		itemsQuery,
		asOf.UTC(),
		afterOrderID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load VIP reconcile preview items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.VIPReconcilePreviewItem, 0, limit)
	for rows.Next() {
		var (
			item           service.VIPReconcilePreviewItem
			userID         sql.NullInt64
			completedAt    sql.NullTime
			manualOverride sql.NullBool
		)
		if err := rows.Scan(
			&item.Category,
			&userID,
			&item.OrderID,
			&completedAt,
			&manualOverride,
			&item.CurrentIsVIP,
		); err != nil {
			return nil, fmt.Errorf("scan VIP reconcile preview item: %w", err)
		}
		item.UserID = nullInt64Pointer(userID)
		if completedAt.Valid {
			value := completedAt.Time.UTC()
			item.CompletedAt = &value
		}
		item.CurrentVIPMode = service.VIPModeFromOverride(nullBoolPointer(manualOverride))
		item.WillEffectiveChange = item.Category == "EFFECTIVE_CHANGE"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate VIP reconcile preview items: %w", err)
	}

	total := stats.EligibilityRepair + stats.InvalidOrder + stats.DeletedUser
	return &service.VIPReconcilePreview{
		AsOf:  asOf.UTC(),
		Total: total,
		Stats: stats,
		Items: items,
	}, nil
}

func (r *vipReconcileRepository) CreateOrResumeJob(
	ctx context.Context,
	requestID string,
	actorUserID int64,
	reason string,
) (*service.VIPReconcileJob, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("VIP reconcile database is not configured")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize the low-frequency create/resume decision. The partial unique
	// index remains the database invariant, while this lock makes concurrent
	// retries with the same request ID observe the committed job and return it
	// idempotently instead of surfacing a transient unique-constraint error.
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended('vip_reconcile_jobs_create_v1', 0)
		)
	`); err != nil {
		return nil, fmt.Errorf("lock VIP reconcile job creation: %w", err)
	}

	existing, err := getVIPReconcileJobByRequestID(ctx, tx, requestID, true)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		if existing.ActorUserID != actorUserID || existing.Reason != reason {
			return nil, infraerrors.Conflict(
				"VIP_RECONCILE_IDEMPOTENCY_CONFLICT",
				"request_id was already used with different input",
			)
		}
		if existing.Status == "failed" {
			if err := ensureNoOtherActiveVIPReconcileJob(ctx, tx, existing.ID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE vip_reconcile_jobs
				SET status = 'queued',
				    last_error = '',
				    finished_at = NULL,
				    updated_at = CURRENT_TIMESTAMP
				WHERE id = $1
			`, existing.ID); err != nil {
				return nil, err
			}
			existing.Status = "queued"
			existing.LastError = ""
			existing.FinishedAt = nil
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return existing, nil
	}

	if err := ensureNoOtherActiveVIPReconcileJob(ctx, tx, 0); err != nil {
		return nil, err
	}
	actorSnapshot, err := loadVIPActorSnapshot(ctx, tx, actorUserID)
	if err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO vip_reconcile_jobs (
			request_id,
			actor_user_id,
			actor_snapshot,
			reason,
			status,
			as_of
		)
		VALUES ($1, $2, $3, $4, 'queued', CURRENT_TIMESTAMP)
		RETURNING
			id, request_id, actor_user_id, actor_snapshot, reason, status, as_of,
			cursor_completed_at, cursor_order_id, scanned, eligibility_repaired,
			effective_changed, force_off_unchanged, already_correct, deleted,
			invalid_order, failed, attempts, last_error, started_at, finished_at,
			created_at, updated_at
	`, requestID, actorUserID, actorSnapshot, reason)
	job, err := scanVIPReconcileJob(row)
	if err != nil {
		if strings.Contains(err.Error(), "idx_vip_reconcile_jobs_one_active") {
			return nil, infraerrors.Conflict(
				"VIP_RECONCILE_ACTIVE_JOB",
				"another VIP reconcile job is active",
			)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func ensureNoOtherActiveVIPReconcileJob(
	ctx context.Context,
	tx *sql.Tx,
	excludeID int64,
) error {
	var activeID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM vip_reconcile_jobs
		WHERE status IN ('queued', 'running')
		  AND id <> $1
		ORDER BY id
		LIMIT 1
		FOR UPDATE
	`, excludeID).Scan(&activeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return infraerrors.Conflict(
		"VIP_RECONCILE_ACTIVE_JOB",
		"another VIP reconcile job is active",
	)
}

func (r *vipReconcileRepository) GetActiveJob(
	ctx context.Context,
) (*service.VIPReconcileJob, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("VIP reconcile database is not configured")
	}
	job, err := scanVIPReconcileJob(r.db.QueryRowContext(ctx, `
		SELECT
			id, request_id, actor_user_id, actor_snapshot, reason, status, as_of,
			cursor_completed_at, cursor_order_id, scanned, eligibility_repaired,
			effective_changed, force_off_unchanged, already_correct, deleted,
			invalid_order, failed, attempts, last_error, started_at, finished_at,
			created_at, updated_at
		FROM vip_reconcile_jobs
		WHERE status IN ('queued', 'running')
		ORDER BY id
		LIMIT 1
	`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func (r *vipReconcileRepository) GetJob(
	ctx context.Context,
	jobID int64,
) (*service.VIPReconcileJob, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("VIP reconcile database is not configured")
	}
	job, err := scanVIPReconcileJob(r.db.QueryRowContext(ctx, `
		SELECT
			id, request_id, actor_user_id, actor_snapshot, reason, status, as_of,
			cursor_completed_at, cursor_order_id, scanned, eligibility_repaired,
			effective_changed, force_off_unchanged, already_correct, deleted,
			invalid_order, failed, attempts, last_error, started_at, finished_at,
			created_at, updated_at
		FROM vip_reconcile_jobs
		WHERE id = $1
	`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("VIP_RECONCILE_JOB_NOT_FOUND", "VIP reconcile job not found")
	}
	return job, err
}

func getVIPReconcileJobByRequestID(
	ctx context.Context,
	tx *sql.Tx,
	requestID string,
	forUpdate bool,
) (*service.VIPReconcileJob, error) {
	query := `
		SELECT
			id, request_id, actor_user_id, actor_snapshot, reason, status, as_of,
			cursor_completed_at, cursor_order_id, scanned, eligibility_repaired,
			effective_changed, force_off_unchanged, already_correct, deleted,
			invalid_order, failed, attempts, last_error, started_at, finished_at,
			created_at, updated_at
		FROM vip_reconcile_jobs
		WHERE request_id = $1
	`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	job, err := scanVIPReconcileJob(tx.QueryRowContext(ctx, query, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	return job, err
}

type vipJobRowScanner interface {
	Scan(dest ...any) error
}

func scanVIPReconcileJob(row vipJobRowScanner) (*service.VIPReconcileJob, error) {
	var (
		job        service.VIPReconcileJob
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	if err := row.Scan(
		&job.ID,
		&job.RequestID,
		&job.ActorUserID,
		&job.ActorSnapshot,
		&job.Reason,
		&job.Status,
		&job.AsOf,
		&job.CursorCompletedAt,
		&job.CursorOrderID,
		&job.Scanned,
		&job.EligibilityRepaired,
		&job.EffectiveChanged,
		&job.ForceOffUnchanged,
		&job.AlreadyCorrect,
		&job.Deleted,
		&job.InvalidOrder,
		&job.Failed,
		&job.Attempts,
		&job.LastError,
		&startedAt,
		&finishedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	); err != nil {
		return nil, err
	}
	job.AsOf = job.AsOf.UTC()
	job.CursorCompletedAt = job.CursorCompletedAt.UTC()
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		job.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time.UTC()
		job.FinishedAt = &value
	}
	return &job, nil
}

type vipReconcileOrder struct {
	id          int64
	userID      int64
	completedAt time.Time
}

func (r *vipReconcileRepository) ProcessJobBatch(
	ctx context.Context,
	jobID int64,
	batchSize int,
) (service.VIPReconcileBatchResult, error) {
	if r == nil || r.db == nil {
		return service.VIPReconcileBatchResult{}, fmt.Errorf("VIP reconcile database is not configured")
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	job, err := scanVIPReconcileJob(tx.QueryRowContext(ctx, `
		SELECT
			id, request_id, actor_user_id, actor_snapshot, reason, status, as_of,
			cursor_completed_at, cursor_order_id, scanned, eligibility_repaired,
			effective_changed, force_off_unchanged, already_correct, deleted,
			invalid_order, failed, attempts, last_error, started_at, finished_at,
			created_at, updated_at
		FROM vip_reconcile_jobs
		WHERE id = $1
		FOR UPDATE
	`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return service.VIPReconcileBatchResult{}, infraerrors.NotFound(
			"VIP_RECONCILE_JOB_NOT_FOUND",
			"VIP reconcile job not found",
		)
	}
	if err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	if job.Status == "succeeded" {
		return service.VIPReconcileBatchResult{Done: true}, tx.Commit()
	}
	if job.Status != "queued" && job.Status != "running" {
		return service.VIPReconcileBatchResult{}, infraerrors.Conflict(
			"VIP_RECONCILE_JOB_NOT_RUNNABLE",
			"VIP reconcile job is not runnable",
		)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE vip_reconcile_jobs
		SET status = 'running',
		    attempts = attempts + CASE WHEN status = 'queued' THEN 1 ELSE 0 END,
		    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, jobID); err != nil {
		return service.VIPReconcileBatchResult{}, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT fact.order_id, fact.user_id, fact.completed_at
		FROM vip_qualifying_payment_order_facts fact
		WHERE fact.completed_at <= $1
		  AND (fact.completed_at, fact.order_id) > ($2, $3)
		ORDER BY fact.completed_at ASC, fact.order_id ASC
		LIMIT $4
	`, job.AsOf, job.CursorCompletedAt, job.CursorOrderID, batchSize)
	if err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	orders := make([]vipReconcileOrder, 0, batchSize)
	for rows.Next() {
		var order vipReconcileOrder
		if err := rows.Scan(&order.id, &order.userID, &order.completedAt); err != nil {
			_ = rows.Close()
			return service.VIPReconcileBatchResult{}, err
		}
		order.completedAt = order.completedAt.UTC()
		orders = append(orders, order)
	}
	if err := rows.Close(); err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	if err := rows.Err(); err != nil {
		return service.VIPReconcileBatchResult{}, err
	}

	if len(orders) == 0 {
		if err := finishVIPReconcileJob(ctx, tx, job); err != nil {
			return service.VIPReconcileBatchResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.VIPReconcileBatchResult{}, err
		}
		return service.VIPReconcileBatchResult{Done: true}, nil
	}

	var (
		repaired          int64
		effectiveChanged  int64
		forceOffUnchanged int64
		alreadyCorrect    int64
		deleted           int64
		changedUserIDs    []int64
	)
	earliestByUser := make(map[int64]vipReconcileOrder, len(orders))
	for _, order := range orders {
		if _, exists := earliestByUser[order.userID]; exists {
			continue
		}
		earliestByUser[order.userID] = order
	}
	candidates := make([]vipIncrementalOrder, 0, len(earliestByUser))
	for _, order := range earliestByUser {
		candidates = append(candidates, vipIncrementalOrder{
			orderID:     order.id,
			userID:      order.userID,
			completedAt: order.completedAt,
			source:      service.VIPPaidSourceReconcile,
		})
	}
	actorUserID := job.ActorUserID
	mutation, err := applyVIPCandidates(ctx, tx, candidates, vipBatchAuditActor{
		actorType:     "admin",
		actorUserID:   &actorUserID,
		actorSnapshot: job.ActorSnapshot,
		action:        "reconcile",
		reason:        job.Reason,
		requestID:     job.RequestID,
	})
	if err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	repaired = int64(mutation.repaired)
	effectiveChanged = int64(mutation.effectiveChanged)
	forceOffUnchanged = int64(mutation.forceOffUnchanged)
	alreadyCorrect = int64(mutation.alreadyCorrect)
	deleted = int64(len(candidates) - mutation.existing)
	changedUserIDs = mutation.changedUserIDs

	last := orders[len(orders)-1]
	done := len(orders) < batchSize
	if _, err := tx.ExecContext(ctx, `
		UPDATE vip_reconcile_jobs
		SET cursor_completed_at = $2,
		    cursor_order_id = $3,
		    scanned = scanned + $4,
		    eligibility_repaired = eligibility_repaired + $5,
		    effective_changed = effective_changed + $6,
		    force_off_unchanged = force_off_unchanged + $7,
		    already_correct = already_correct + $8,
		    deleted = deleted + $9,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, jobID, last.completedAt, last.id, len(orders), repaired, effectiveChanged,
		forceOffUnchanged, alreadyCorrect, deleted); err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	if done {
		job.CursorCompletedAt = last.completedAt
		job.CursorOrderID = last.id
		if err := finishVIPReconcileJob(ctx, tx, job); err != nil {
			return service.VIPReconcileBatchResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return service.VIPReconcileBatchResult{}, err
	}
	return service.VIPReconcileBatchResult{
		Done:           done,
		Repaired:       int(repaired),
		ChangedUserIDs: dedupeInt64s(changedUserIDs),
	}, nil
}

func finishVIPReconcileJob(
	ctx context.Context,
	tx *sql.Tx,
	job *service.VIPReconcileJob,
) error {
	var invalid int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM payment_orders po
		WHERE `+vipQualifyingOrderPredicate+`
		  AND po.completed_at IS NULL
		  AND po.updated_at <= $1
	`, job.AsOf).Scan(&invalid); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE vip_reconcile_jobs
		SET status = 'succeeded',
		    invalid_order = $2,
		    finished_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, job.ID, invalid)
	return err
}

func dedupeInt64s(values []int64) []int64 {
	if len(values) < 2 {
		return values
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (r *vipReconcileRepository) MarkJobFailed(
	ctx context.Context,
	jobID int64,
	failure string,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("VIP reconcile database is not configured")
	}
	failure = strings.TrimSpace(failure)
	if len(failure) > 4000 {
		failure = failure[:4000]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE vip_reconcile_jobs
		SET status = 'failed',
		    failed = failed + 1,
		    last_error = $2,
		    finished_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND status IN ('queued', 'running')
	`, jobID, failure)
	return err
}

func (r *vipReconcileRepository) RequeueJob(
	ctx context.Context,
	jobID int64,
	reason string,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("VIP reconcile database is not configured")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 4000 {
		reason = reason[:4000]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE vip_reconcile_jobs
		SET status = 'queued',
		    last_error = $2,
		    finished_at = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND status = 'running'
	`, jobID, reason)
	return err
}
