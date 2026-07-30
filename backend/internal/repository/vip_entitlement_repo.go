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

type vipEntitlementRepository struct {
	db *sql.DB
}

func NewVIPEntitlementRepository(db *sql.DB) service.VIPEntitlementRepository {
	return &vipEntitlementRepository{db: db}
}

type vipEntitlementRecord struct {
	paidEligible    bool
	paidEligibleAt  sql.NullTime
	paidSource      string
	manualOverride  sql.NullBool
	overrideAt      sql.NullTime
	overrideBy      sql.NullInt64
	overrideReason  string
	isVIP           bool
	grantedAt       sql.NullTime
	effectiveSource string
}

const selectVIPEntitlementForUpdateSQL = `
	SELECT
		vip_paid_eligible,
		vip_paid_eligible_at,
		vip_paid_source,
		vip_manual_override,
		vip_override_at,
		vip_override_by,
		vip_override_reason,
		is_vip,
		vip_granted_at,
		vip_effective_source
	FROM users
	WHERE id = $1
	  AND deleted_at IS NULL
	FOR UPDATE
`

const updateVIPEntitlementSQL = `
	UPDATE users
	SET
		vip_paid_eligible = $2,
		vip_paid_eligible_at = $3,
		vip_paid_source = $4,
		vip_manual_override = $5,
		vip_override_at = $6,
		vip_override_by = $7,
		vip_override_reason = $8,
		is_vip = $9,
		vip_granted_at = $10,
		vip_effective_source = $11,
		updated_at = NOW()
	WHERE id = $1
	  AND deleted_at IS NULL
`

const insertVIPAuditSQL = `
	INSERT INTO user_vip_audit_events (
		user_id,
		actor_type,
		actor_user_id,
		actor_snapshot,
		action,
		reason,
		order_id,
		old_paid_eligible,
		new_paid_eligible,
		old_manual_override,
		new_manual_override,
		old_is_vip,
		new_is_vip,
		source
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`

const insertIdempotentVIPSystemAuditSQL = insertVIPAuditSQL + `
	ON CONFLICT (order_id, action) WHERE order_id IS NOT NULL DO NOTHING
`

func (r *vipEntitlementRepository) ApplyPaidEligibility(
	ctx context.Context,
	userID, orderID int64,
	completedAt time.Time,
	source service.VIPPaidSource,
) (service.VIPMutationResult, error) {
	if r == nil || r.db == nil {
		return service.VIPMutationResult{}, fmt.Errorf("VIP entitlement database is not configured")
	}
	if !service.IsValidVIPPaidSource(source) {
		return service.VIPMutationResult{}, fmt.Errorf("invalid VIP paid source %q", source)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.VIPMutationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := loadVIPEntitlementForUpdate(ctx, tx, userID)
	if err != nil {
		return service.VIPMutationResult{}, err
	}
	next := applyPaidEligibilityTransition(current, completedAt.UTC(), source)

	if err := persistVIPEntitlement(ctx, tx, userID, next); err != nil {
		return service.VIPMutationResult{}, err
	}
	action := vipPaidAuditAction(source)
	if _, err := tx.ExecContext(
		ctx,
		insertIdempotentVIPSystemAuditSQL,
		userID,
		"system",
		nil,
		"system",
		action,
		"",
		orderID,
		current.paidEligible,
		next.paidEligible,
		nullBoolValue(current.manualOverride),
		nullBoolValue(next.manualOverride),
		current.isVIP,
		next.isVIP,
		string(source),
	); err != nil {
		return service.VIPMutationResult{}, fmt.Errorf("insert VIP paid audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return service.VIPMutationResult{}, err
	}
	return service.VIPMutationResult{
		EligibilityChanged: !current.paidEligible && next.paidEligible,
		EffectiveChanged:   current.isVIP != next.isVIP,
		ManualMode:         service.VIPModeFromOverride(nullBoolPointer(next.manualOverride)),
	}, nil
}

func (r *vipEntitlementRepository) SetManualMode(
	ctx context.Context,
	userID int64,
	mode service.VIPMode,
	actorID int64,
	reason string,
) (service.VIPMutationResult, error) {
	if r == nil || r.db == nil {
		return service.VIPMutationResult{}, fmt.Errorf("VIP entitlement database is not configured")
	}
	parsedMode, err := service.ParseVIPMode(string(mode))
	if err != nil {
		return service.VIPMutationResult{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return service.VIPMutationResult{}, fmt.Errorf("VIP override reason is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.VIPMutationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := loadVIPEntitlementForUpdate(ctx, tx, userID)
	if err != nil {
		return service.VIPMutationResult{}, err
	}

	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT NOW()`).Scan(&databaseNow); err != nil {
		return service.VIPMutationResult{}, fmt.Errorf("load database time: %w", err)
	}

	next := current
	var recoveredOrderID sql.NullInt64
	var recoveredFrom, recoveredTo vipEntitlementRecord
	if parsedMode == service.VIPModeAuto && !next.paidEligible {
		orderID, completedAt, found, lookupErr := findEarliestEligibleVIPOrder(ctx, tx, userID)
		if lookupErr != nil {
			return service.VIPMutationResult{}, lookupErr
		}
		if found {
			recoveredOrderID = sql.NullInt64{Int64: orderID, Valid: true}
			recoveredFrom = next
			next = applyPaidEligibilityTransition(next, completedAt, service.VIPPaidSourceReconcile)
			recoveredTo = next
		}
	}
	next = applyManualModeTransition(next, parsedMode, actorID, reason, databaseNow.UTC())

	if err := persistVIPEntitlement(ctx, tx, userID, next); err != nil {
		return service.VIPMutationResult{}, err
	}

	if recoveredOrderID.Valid {
		if _, err := tx.ExecContext(
			ctx,
			insertIdempotentVIPSystemAuditSQL,
			userID,
			"system",
			nil,
			"system",
			"reconcile",
			"",
			recoveredOrderID.Int64,
			recoveredFrom.paidEligible,
			recoveredTo.paidEligible,
			nullBoolValue(recoveredFrom.manualOverride),
			nullBoolValue(recoveredTo.manualOverride),
			recoveredFrom.isVIP,
			recoveredTo.isVIP,
			string(service.VIPPaidSourceReconcile),
		); err != nil {
			return service.VIPMutationResult{}, fmt.Errorf("insert AUTO recovery audit: %w", err)
		}
	}

	actorSnapshot, err := loadVIPActorSnapshot(ctx, tx, actorID)
	if err != nil {
		return service.VIPMutationResult{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		insertVIPAuditSQL,
		userID,
		"admin",
		actorID,
		actorSnapshot,
		"manual_mode",
		reason,
		nil,
		current.paidEligible,
		next.paidEligible,
		nullBoolValue(current.manualOverride),
		nullBoolValue(next.manualOverride),
		current.isVIP,
		next.isVIP,
		string(next.effectiveSource),
	); err != nil {
		return service.VIPMutationResult{}, fmt.Errorf("insert VIP manual audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return service.VIPMutationResult{}, err
	}
	return service.VIPMutationResult{
		EligibilityChanged: !current.paidEligible && next.paidEligible,
		EffectiveChanged:   current.isVIP != next.isVIP,
		ManualMode:         parsedMode,
	}, nil
}

func (r *vipEntitlementRepository) ListAuditEvents(
	ctx context.Context,
	userID int64,
	page, pageSize int,
) ([]service.VIPAuditEvent, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, fmt.Errorf("VIP entitlement database is not configured")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var total int64
	if err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM user_vip_audit_events WHERE user_id = $1`,
		userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count VIP audit events: %w", err)
	}

	const query = `
		SELECT
			id,
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
			source,
			created_at
		FROM user_vip_audit_events
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list VIP audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := make([]service.VIPAuditEvent, 0, pageSize)
	for rows.Next() {
		var (
			event             service.VIPAuditEvent
			actorUserID       sql.NullInt64
			orderID           sql.NullInt64
			oldManualOverride sql.NullBool
			newManualOverride sql.NullBool
		)
		if err := rows.Scan(
			&event.ID,
			&event.UserID,
			&event.ActorType,
			&actorUserID,
			&event.ActorSnapshot,
			&event.Action,
			&event.Reason,
			&orderID,
			&event.RequestID,
			&event.OldPaidEligible,
			&event.NewPaidEligible,
			&oldManualOverride,
			&newManualOverride,
			&event.OldIsVIP,
			&event.NewIsVIP,
			&event.Source,
			&event.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan VIP audit event: %w", err)
		}
		event.ActorUserID = nullInt64Pointer(actorUserID)
		event.OrderID = nullInt64Pointer(orderID)
		event.OldManualOverride = nullBoolPointer(oldManualOverride)
		event.NewManualOverride = nullBoolPointer(newManualOverride)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate VIP audit events: %w", err)
	}
	return events, total, nil
}

func loadVIPEntitlementForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
) (vipEntitlementRecord, error) {
	var out vipEntitlementRecord
	err := tx.QueryRowContext(ctx, selectVIPEntitlementForUpdateSQL, userID).Scan(
		&out.paidEligible,
		&out.paidEligibleAt,
		&out.paidSource,
		&out.manualOverride,
		&out.overrideAt,
		&out.overrideBy,
		&out.overrideReason,
		&out.isVIP,
		&out.grantedAt,
		&out.effectiveSource,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return vipEntitlementRecord{}, service.ErrUserNotFound
	}
	if err != nil {
		return vipEntitlementRecord{}, fmt.Errorf("lock VIP entitlement: %w", err)
	}
	return out, nil
}

func persistVIPEntitlement(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	record vipEntitlementRecord,
) error {
	result, err := tx.ExecContext(
		ctx,
		updateVIPEntitlementSQL,
		userID,
		record.paidEligible,
		nullTimeValue(record.paidEligibleAt),
		record.paidSource,
		nullBoolValue(record.manualOverride),
		nullTimeValue(record.overrideAt),
		nullInt64Value(record.overrideBy),
		record.overrideReason,
		record.isVIP,
		nullTimeValue(record.grantedAt),
		record.effectiveSource,
	)
	if err != nil {
		return fmt.Errorf("update VIP entitlement: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrUserNotFound
	}
	return nil
}

func applyPaidEligibilityTransition(
	current vipEntitlementRecord,
	completedAt time.Time,
	source service.VIPPaidSource,
) vipEntitlementRecord {
	next := current
	next.paidEligible = true
	if !current.paidEligibleAt.Valid || completedAt.Before(current.paidEligibleAt.Time) {
		next.paidEligibleAt = sql.NullTime{Time: completedAt.UTC(), Valid: true}
	}
	if !current.paidEligible || current.paidSource == "" {
		next.paidSource = string(source)
	}

	override := nullBoolPointer(next.manualOverride)
	next.isVIP = service.EffectiveVIPState(next.paidEligible, override)
	next.effectiveSource = string(service.EffectiveVIPSource(
		next.paidEligible,
		service.VIPPaidSource(next.paidSource),
		override,
	))
	if next.isVIP {
		if !current.isVIP || !current.grantedAt.Valid {
			next.grantedAt = sql.NullTime{Time: completedAt.UTC(), Valid: true}
		}
	} else {
		next.grantedAt = sql.NullTime{}
	}
	return next
}

func applyManualModeTransition(
	current vipEntitlementRecord,
	mode service.VIPMode,
	actorID int64,
	reason string,
	now time.Time,
) vipEntitlementRecord {
	next := current
	switch mode {
	case service.VIPModeAuto:
		next.manualOverride = sql.NullBool{}
		next.overrideAt = sql.NullTime{}
		next.overrideBy = sql.NullInt64{}
		next.overrideReason = ""
	case service.VIPModeForceOn:
		next.manualOverride = sql.NullBool{Bool: true, Valid: true}
		next.overrideAt = sql.NullTime{Time: now, Valid: true}
		next.overrideBy = sql.NullInt64{Int64: actorID, Valid: true}
		next.overrideReason = reason
	case service.VIPModeForceOff:
		next.manualOverride = sql.NullBool{Bool: false, Valid: true}
		next.overrideAt = sql.NullTime{Time: now, Valid: true}
		next.overrideBy = sql.NullInt64{Int64: actorID, Valid: true}
		next.overrideReason = reason
	}

	override := nullBoolPointer(next.manualOverride)
	next.isVIP = service.EffectiveVIPState(next.paidEligible, override)
	next.effectiveSource = string(service.EffectiveVIPSource(
		next.paidEligible,
		service.VIPPaidSource(next.paidSource),
		override,
	))
	if next.isVIP {
		if !current.isVIP || !current.grantedAt.Valid {
			grantedAt := now
			if mode == service.VIPModeAuto && next.paidEligibleAt.Valid {
				grantedAt = next.paidEligibleAt.Time
			}
			next.grantedAt = sql.NullTime{Time: grantedAt.UTC(), Valid: true}
		}
	} else {
		next.grantedAt = sql.NullTime{}
	}
	return next
}

func findEarliestEligibleVIPOrder(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
) (int64, time.Time, bool, error) {
	const query = `
		SELECT order_id, completed_at
		FROM vip_qualifying_payment_order_facts
		WHERE user_id = $1
		ORDER BY completed_at ASC, order_id ASC
		LIMIT 1
	`
	var (
		orderID     int64
		completedAt time.Time
	)
	err := tx.QueryRowContext(ctx, query, userID).Scan(&orderID, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("find earliest eligible VIP order: %w", err)
	}
	return orderID, completedAt.UTC(), true, nil
}

func loadVIPActorSnapshot(ctx context.Context, tx *sql.Tx, actorID int64) (string, error) {
	const query = `
		SELECT COALESCE(NULLIF(BTRIM(username), ''), NULLIF(BTRIM(email), ''), 'admin')
		FROM users
		WHERE id = $1
	`
	var snapshot string
	if err := tx.QueryRowContext(ctx, query, actorID).Scan(&snapshot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("VIP entitlement actor not found")
		}
		return "", fmt.Errorf("load VIP actor snapshot: %w", err)
	}
	return snapshot, nil
}

func vipPaidAuditAction(source service.VIPPaidSource) string {
	switch source {
	case service.VIPPaidSourceBackfill:
		return "backfill"
	case service.VIPPaidSourceReconcile:
		return "reconcile"
	default:
		return "paid_grant"
	}
}

func nullBoolPointer(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	out := value.Bool
	return &out
}

func nullBoolValue(value sql.NullBool) any {
	if !value.Valid {
		return nil
	}
	return value.Bool
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func nullInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}
