package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type vipIncrementalReconcileRepository struct {
	db *sql.DB
}

func NewVIPIncrementalReconcileRepository(
	db *sql.DB,
) service.VIPIncrementalReconcileRepository {
	return &vipIncrementalReconcileRepository{db: db}
}

func (r *vipIncrementalReconcileRepository) InitializeBackfillCutoff(
	ctx context.Context,
) (service.VIPIncrementalReconcileWatermark, error) {
	if r == nil || r.db == nil {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"VIP incremental reconcile database is not configured",
		)
	}

	const initializeQuery = `
		UPDATE vip_reconcile_watermark
		SET backfill_cutoff = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
		  AND backfill_cutoff IS NULL
		RETURNING completed_at_cursor, order_id_cursor, backfill_cutoff, updated_at
	`
	watermark, err := scanVIPIncrementalWatermark(
		r.db.QueryRowContext(ctx, initializeQuery),
	)
	if err == nil {
		return watermark, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"initialize VIP reconcile backfill cutoff: %w",
			err,
		)
	}

	const loadQuery = `
		SELECT completed_at_cursor, order_id_cursor, backfill_cutoff, updated_at
		FROM vip_reconcile_watermark
		WHERE id = 1
	`
	watermark, err = scanVIPIncrementalWatermark(
		r.db.QueryRowContext(ctx, loadQuery),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"VIP reconcile watermark singleton is missing",
		)
	}
	if err != nil {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"load VIP reconcile watermark: %w",
			err,
		)
	}
	return watermark, nil
}

func (r *vipIncrementalReconcileRepository) DatabaseNow(
	ctx context.Context,
) (time.Time, error) {
	if r == nil || r.db == nil {
		return time.Time{}, fmt.Errorf(
			"VIP incremental reconcile database is not configured",
		)
	}
	var now time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("load database time: %w", err)
	}
	return now.UTC(), nil
}

func (r *vipIncrementalReconcileRepository) ProcessNextBatch(
	ctx context.Context,
	scanBefore time.Time,
	limit int,
) (service.VIPIncrementalReconcileBatchResult, error) {
	if r == nil || r.db == nil {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP incremental reconcile database is not configured",
		)
	}
	if scanBefore.IsZero() {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile scan upper bound is required",
		)
	}
	if limit <= 0 {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile batch size must be positive",
		)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	watermark, err := loadVIPIncrementalWatermark(ctx, tx, true)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	if watermark.BackfillCutoff.IsZero() {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile backfill cutoff is not initialized",
		)
	}

	orders, err := loadNextVIPIncrementalOrderPage(
		ctx,
		tx,
		watermark.Cursor,
		scanBefore.UTC(),
		limit,
	)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	result := service.VIPIncrementalReconcileBatchResult{
		Cursor:  watermark.Cursor,
		Scanned: len(orders),
		Full:    len(orders) == limit,
	}

	if len(orders) == 0 {
		if scanBefore.UTC().After(watermark.Cursor.CompletedAt) {
			if err := advanceVIPIncrementalWatermark(
				ctx,
				tx,
				service.VIPIncrementalReconcileCursor{
					CompletedAt: scanBefore.UTC(),
					OrderID:     0,
				},
			); err != nil {
				return service.VIPIncrementalReconcileBatchResult{}, err
			}
			result.Cursor = service.VIPIncrementalReconcileCursor{
				CompletedAt: scanBefore.UTC(),
				OrderID:     0,
			}
		}
		if err := tx.Commit(); err != nil {
			return service.VIPIncrementalReconcileBatchResult{}, err
		}
		return result, nil
	}

	candidates := dedupeVIPIncrementalOrdersByUser(
		orders,
		watermark.BackfillCutoff,
	)
	mutation, err := applyVIPIncrementalCandidates(ctx, tx, candidates)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	last := orders[len(orders)-1]
	nextCursor := service.VIPIncrementalReconcileCursor{
		CompletedAt: last.completedAt,
		OrderID:     last.orderID,
	}
	if err := advanceVIPIncrementalWatermark(ctx, tx, nextCursor); err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}

	result.Cursor = nextCursor
	result.Repaired = mutation.repaired
	result.BackfillRepaired = mutation.backfillRepaired
	result.ReconcileRepaired = mutation.reconcileRepaired
	result.EffectiveChanged = mutation.effectiveChanged
	result.ForceOffUnchanged = mutation.forceOffUnchanged
	result.ChangedUserIDs = mutation.changedUserIDs
	return result, nil
}

func (r *vipIncrementalReconcileRepository) RepairOverlap(
	ctx context.Context,
	overlap time.Duration,
	limit int,
) (service.VIPIncrementalReconcileBatchResult, error) {
	if r == nil || r.db == nil {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP incremental reconcile database is not configured",
		)
	}
	if overlap <= 0 {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile overlap must be positive",
		)
	}
	if limit <= 0 {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile batch size must be positive",
		)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	watermark, err := loadVIPIncrementalWatermark(ctx, tx, false)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	if watermark.BackfillCutoff.IsZero() {
		return service.VIPIncrementalReconcileBatchResult{}, fmt.Errorf(
			"VIP reconcile backfill cutoff is not initialized",
		)
	}

	orders, err := loadVIPIncrementalOverlapPage(
		ctx,
		tx,
		watermark.Cursor,
		overlap,
		limit,
	)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	result := service.VIPIncrementalReconcileBatchResult{
		Cursor:  watermark.Cursor,
		Scanned: len(orders),
		Full:    len(orders) == limit,
	}
	if len(orders) == 0 {
		if err := tx.Commit(); err != nil {
			return service.VIPIncrementalReconcileBatchResult{}, err
		}
		return result, nil
	}

	candidates := dedupeVIPIncrementalOrdersByUser(
		orders,
		watermark.BackfillCutoff,
	)
	mutation, err := applyVIPIncrementalCandidates(ctx, tx, candidates)
	if err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.VIPIncrementalReconcileBatchResult{}, err
	}

	result.Repaired = mutation.repaired
	result.BackfillRepaired = mutation.backfillRepaired
	result.ReconcileRepaired = mutation.reconcileRepaired
	result.EffectiveChanged = mutation.effectiveChanged
	result.ForceOffUnchanged = mutation.forceOffUnchanged
	result.ChangedUserIDs = mutation.changedUserIDs
	return result, nil
}

type vipIncrementalOrder struct {
	orderID     int64
	userID      int64
	completedAt time.Time
	source      service.VIPPaidSource
}

func loadNextVIPIncrementalOrderPage(
	ctx context.Context,
	tx *sql.Tx,
	cursor service.VIPIncrementalReconcileCursor,
	scanBefore time.Time,
	limit int,
) ([]vipIncrementalOrder, error) {
	query := `
		SELECT fact.order_id, fact.user_id, fact.completed_at
		FROM vip_qualifying_payment_order_facts fact
		WHERE fact.completed_at <= $1
		  AND (fact.completed_at, fact.order_id) > ($2, $3)
		ORDER BY fact.completed_at ASC, fact.order_id ASC
		LIMIT $4
	`
	rows, err := tx.QueryContext(
		ctx,
		query,
		scanBefore.UTC(),
		cursor.CompletedAt.UTC(),
		cursor.OrderID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load VIP reconcile order page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orders := make([]vipIncrementalOrder, 0, limit)
	for rows.Next() {
		var order vipIncrementalOrder
		if err := rows.Scan(
			&order.orderID,
			&order.userID,
			&order.completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan VIP reconcile order: %w", err)
		}
		order.completedAt = order.completedAt.UTC()
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate VIP reconcile orders: %w", err)
	}
	return orders, nil
}

func loadVIPIncrementalOverlapPage(
	ctx context.Context,
	tx *sql.Tx,
	cursor service.VIPIncrementalReconcileCursor,
	overlap time.Duration,
	limit int,
) ([]vipIncrementalOrder, error) {
	query := `
		SELECT fact.order_id, fact.user_id, fact.completed_at
		FROM vip_qualifying_payment_order_facts fact
		JOIN users u
		  ON u.id = fact.user_id
		 AND u.deleted_at IS NULL
		 AND (
			NOT u.vip_paid_eligible
			OR u.vip_paid_eligible_at IS NULL
			OR fact.completed_at < u.vip_paid_eligible_at
		 )
		WHERE fact.completed_at >= $1
		  AND fact.completed_at <= $2
		ORDER BY fact.completed_at ASC, fact.order_id ASC
		LIMIT $3
	`
	rows, err := tx.QueryContext(
		ctx,
		query,
		cursor.CompletedAt.Add(-overlap).UTC(),
		cursor.CompletedAt.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load VIP reconcile overlap page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orders := make([]vipIncrementalOrder, 0, limit)
	for rows.Next() {
		var order vipIncrementalOrder
		if err := rows.Scan(
			&order.orderID,
			&order.userID,
			&order.completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan VIP reconcile overlap order: %w", err)
		}
		order.completedAt = order.completedAt.UTC()
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate VIP reconcile overlap orders: %w", err)
	}
	return orders, nil
}

func dedupeVIPIncrementalOrdersByUser(
	orders []vipIncrementalOrder,
	backfillCutoff time.Time,
) []vipIncrementalOrder {
	seen := make(map[int64]struct{}, len(orders))
	out := make([]vipIncrementalOrder, 0, len(orders))
	for _, order := range orders {
		if _, ok := seen[order.userID]; ok {
			continue
		}
		seen[order.userID] = struct{}{}
		if !order.completedAt.After(backfillCutoff) {
			order.source = service.VIPPaidSourceBackfill
		} else {
			order.source = service.VIPPaidSourceReconcile
		}
		out = append(out, order)
	}
	return out
}

type vipIncrementalMutationResult struct {
	repaired          int
	backfillRepaired  int
	reconcileRepaired int
	effectiveChanged  int
	forceOffUnchanged int
	existing          int
	alreadyCorrect    int
	changedUserIDs    []int64
}

type vipBatchAuditActor struct {
	actorType     string
	actorUserID   *int64
	actorSnapshot string
	action        string
	reason        string
	requestID     string
}

func applyVIPIncrementalCandidates(
	ctx context.Context,
	tx *sql.Tx,
	candidates []vipIncrementalOrder,
) (vipIncrementalMutationResult, error) {
	return applyVIPCandidates(ctx, tx, candidates, vipBatchAuditActor{
		actorType:     "system",
		actorSnapshot: "system",
	})
}

func applyVIPCandidates(
	ctx context.Context,
	tx *sql.Tx,
	candidates []vipIncrementalOrder,
	actor vipBatchAuditActor,
) (vipIncrementalMutationResult, error) {
	if len(candidates) == 0 {
		return vipIncrementalMutationResult{}, nil
	}
	if strings.TrimSpace(actor.actorType) == "" {
		actor.actorType = "system"
	}
	if strings.TrimSpace(actor.actorSnapshot) == "" {
		actor.actorSnapshot = actor.actorType
	}

	var values strings.Builder
	args := make([]any, 0, len(candidates)*4+6)
	for index, candidate := range candidates {
		if index > 0 {
			_ = values.WriteByte(',')
		}
		base := index*4 + 1
		fmt.Fprintf(
			&values,
			"($%d::bigint,$%d::bigint,$%d::timestamptz,$%d::text)",
			base,
			base+1,
			base+2,
			base+3,
		)
		args = append(
			args,
			candidate.userID,
			candidate.orderID,
			candidate.completedAt.UTC(),
			string(candidate.source),
		)
	}
	actorBase := len(args) + 1
	args = append(
		args,
		actor.actorType,
		actor.actorUserID,
		actor.actorSnapshot,
		actor.action,
		actor.reason,
		actor.requestID,
	)

	query := `
		WITH input(user_id, order_id, completed_at, source) AS (
			VALUES ` + values.String() + `
		),
		locked AS MATERIALIZED (
			SELECT
				u.id AS user_id,
				i.order_id,
				i.completed_at,
				i.source,
				u.vip_paid_eligible AS old_paid_eligible,
				u.vip_paid_eligible_at AS old_paid_eligible_at,
				u.vip_paid_source AS old_paid_source,
				u.vip_manual_override AS old_manual_override,
				u.is_vip AS old_is_vip,
				u.vip_granted_at AS old_granted_at,
				u.vip_effective_source AS old_effective_source
			FROM users u
			JOIN input i ON i.user_id = u.id
			WHERE u.deleted_at IS NULL
			ORDER BY u.id
			FOR UPDATE OF u
		),
		updated AS (
			UPDATE users u
			SET vip_paid_eligible = TRUE,
			    vip_paid_eligible_at = CASE
					WHEN l.old_paid_eligible_at IS NULL
					  OR l.completed_at < l.old_paid_eligible_at
					THEN l.completed_at
					ELSE l.old_paid_eligible_at
				END,
			    vip_paid_source = CASE
					WHEN NOT l.old_paid_eligible OR BTRIM(l.old_paid_source) = ''
					THEN l.source
					ELSE l.old_paid_source
				END,
			    is_vip = COALESCE(l.old_manual_override, TRUE),
			    vip_granted_at = CASE
					WHEN NOT COALESCE(l.old_manual_override, TRUE) THEN NULL
					WHEN l.old_is_vip AND l.old_granted_at IS NOT NULL
					THEN l.old_granted_at
					ELSE l.completed_at
				END,
			    vip_effective_source = CASE
					WHEN l.old_manual_override IS TRUE THEN 'manual_on'
					WHEN l.old_manual_override IS FALSE THEN 'manual_off'
					WHEN NOT l.old_paid_eligible OR BTRIM(l.old_paid_source) = ''
					THEN l.source
					ELSE l.old_paid_source
				END,
			    updated_at = CURRENT_TIMESTAMP
			FROM locked l
			WHERE u.id = l.user_id
			  AND (
				NOT l.old_paid_eligible
				OR l.old_paid_eligible_at IS NULL
				OR l.completed_at < l.old_paid_eligible_at
				OR BTRIM(l.old_paid_source) = ''
				OR l.old_is_vip IS DISTINCT FROM COALESCE(l.old_manual_override, TRUE)
				OR l.old_effective_source IS DISTINCT FROM CASE
					WHEN l.old_manual_override IS TRUE THEN 'manual_on'
					WHEN l.old_manual_override IS FALSE THEN 'manual_off'
					WHEN NOT l.old_paid_eligible OR BTRIM(l.old_paid_source) = ''
					THEN l.source
					ELSE l.old_paid_source
				END
				OR (
					COALESCE(l.old_manual_override, TRUE)
					AND l.old_granted_at IS NULL
				)
				OR (
					NOT COALESCE(l.old_manual_override, TRUE)
					AND l.old_granted_at IS NOT NULL
				)
			  )
			RETURNING
				u.id AS user_id,
				l.order_id,
				l.source,
				l.old_paid_eligible,
				u.vip_paid_eligible AS new_paid_eligible,
				l.old_manual_override,
				u.vip_manual_override AS new_manual_override,
				l.old_is_vip,
				u.is_vip AS new_is_vip
		),
		audit_insert AS (
			INSERT INTO user_vip_audit_events (
				user_id,
				actor_type,
				actor_user_id,
				actor_snapshot,
				action,
				reason,
				order_id,
				request_id,
				old_paid_eligible,
				new_paid_eligible,
				old_manual_override,
				new_manual_override,
				old_is_vip,
				new_is_vip,
				source
			)
			SELECT
				user_id,
				$` + fmt.Sprintf("%d", actorBase) + `::text,
				$` + fmt.Sprintf("%d", actorBase+1) + `::bigint,
				$` + fmt.Sprintf("%d", actorBase+2) + `::text,
				COALESCE(NULLIF($` + fmt.Sprintf("%d", actorBase+3) + `::text, ''), source),
				$` + fmt.Sprintf("%d", actorBase+4) + `::text,
				order_id,
				$` + fmt.Sprintf("%d", actorBase+5) + `::text,
				old_paid_eligible,
				new_paid_eligible,
				old_manual_override,
				new_manual_override,
				old_is_vip,
				new_is_vip,
				source
			FROM updated
			ON CONFLICT (order_id, action)
				WHERE order_id IS NOT NULL
				DO NOTHING
			RETURNING user_id
		)
		SELECT
			l.user_id,
			l.source,
			l.old_paid_eligible,
			u.new_paid_eligible,
			l.old_manual_override,
			u.new_manual_override,
			l.old_is_vip,
			u.new_is_vip,
			u.user_id IS NOT NULL AS was_updated
		FROM locked l
		LEFT JOIN updated u ON u.user_id = l.user_id
		ORDER BY l.user_id
	`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return vipIncrementalMutationResult{}, fmt.Errorf(
			"apply VIP reconcile candidates: %w",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	var out vipIncrementalMutationResult
	for rows.Next() {
		var (
			userID            int64
			source            string
			oldPaidEligible   bool
			newPaidEligible   sql.NullBool
			oldManualOverride sql.NullBool
			newManualOverride sql.NullBool
			oldIsVIP          bool
			newIsVIP          sql.NullBool
			wasUpdated        bool
		)
		if err := rows.Scan(
			&userID,
			&source,
			&oldPaidEligible,
			&newPaidEligible,
			&oldManualOverride,
			&newManualOverride,
			&oldIsVIP,
			&newIsVIP,
			&wasUpdated,
		); err != nil {
			return vipIncrementalMutationResult{}, fmt.Errorf(
				"scan VIP reconcile mutation: %w",
				err,
			)
		}
		out.existing++
		if !wasUpdated {
			out.alreadyCorrect++
			continue
		}
		out.repaired++
		switch service.VIPPaidSource(source) {
		case service.VIPPaidSourceBackfill:
			out.backfillRepaired++
		case service.VIPPaidSourceReconcile:
			out.reconcileRepaired++
		}
		if oldIsVIP != newIsVIP.Bool {
			out.effectiveChanged++
			out.changedUserIDs = append(out.changedUserIDs, userID)
		} else if newManualOverride.Valid && !newManualOverride.Bool && !newIsVIP.Bool {
			out.forceOffUnchanged++
		}
	}
	if err := rows.Err(); err != nil {
		return vipIncrementalMutationResult{}, fmt.Errorf(
			"iterate VIP reconcile mutations: %w",
			err,
		)
	}
	out.changedUserIDs = dedupeVIPIncrementalUserIDs(out.changedUserIDs)
	return out, nil
}

func loadVIPIncrementalWatermark(
	ctx context.Context,
	tx *sql.Tx,
	forUpdate bool,
) (service.VIPIncrementalReconcileWatermark, error) {
	query := `
		SELECT completed_at_cursor, order_id_cursor, backfill_cutoff, updated_at
		FROM vip_reconcile_watermark
		WHERE id = 1
	`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	watermark, err := scanVIPIncrementalWatermark(tx.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"VIP reconcile watermark singleton is missing",
		)
	}
	if err != nil {
		return service.VIPIncrementalReconcileWatermark{}, fmt.Errorf(
			"load VIP reconcile watermark: %w",
			err,
		)
	}
	return watermark, nil
}

type vipIncrementalRowScanner interface {
	Scan(dest ...any) error
}

func scanVIPIncrementalWatermark(
	row vipIncrementalRowScanner,
) (service.VIPIncrementalReconcileWatermark, error) {
	var (
		completedAt    time.Time
		orderID        int64
		backfillCutoff sql.NullTime
		updatedAt      time.Time
	)
	if err := row.Scan(
		&completedAt,
		&orderID,
		&backfillCutoff,
		&updatedAt,
	); err != nil {
		return service.VIPIncrementalReconcileWatermark{}, err
	}
	out := service.VIPIncrementalReconcileWatermark{
		Cursor: service.VIPIncrementalReconcileCursor{
			CompletedAt: completedAt.UTC(),
			OrderID:     orderID,
		},
		UpdatedAt: updatedAt.UTC(),
	}
	if backfillCutoff.Valid {
		out.BackfillCutoff = backfillCutoff.Time.UTC()
	}
	return out, nil
}

func advanceVIPIncrementalWatermark(
	ctx context.Context,
	tx *sql.Tx,
	cursor service.VIPIncrementalReconcileCursor,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE vip_reconcile_watermark
		SET completed_at_cursor = $1,
		    order_id_cursor = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, cursor.CompletedAt.UTC(), cursor.OrderID)
	if err != nil {
		return fmt.Errorf("advance VIP reconcile watermark: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read VIP reconcile watermark update result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("VIP reconcile watermark singleton is missing")
	}
	return nil
}

func dedupeVIPIncrementalUserIDs(values []int64) []int64 {
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
