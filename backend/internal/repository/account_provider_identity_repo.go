package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type accountProviderIdentityDB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type accountProviderIdentityLock struct {
	AccountID       int64
	Platform        string
	OwnershipMode   string
	OwnerUserID     int64
	IdentityVersion int64
	IsolationState  string
	BindingID       sql.NullInt64
}

func translateAccountProviderIdentityError(err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) {
		return err
	}
	constraint := strings.TrimSpace(postgresError.Constraint)
	if constraint == "account_provider_identity_admin_required" || constraint == "account_provider_identity_independent_admin_required" {
		return service.ErrAccountProviderIdentityForbidden
	}
	if strings.HasPrefix(constraint, "account_provider_identity_") ||
		strings.HasPrefix(constraint, "uq_account_provider_identity_") ||
		strings.HasPrefix(constraint, "account_provider_identity_reviews_") ||
		strings.HasPrefix(constraint, "account_provider_identity_review_actions_") ||
		strings.HasPrefix(constraint, "account_provider_identity_bindings_") ||
		strings.HasPrefix(constraint, "account_provider_identity_revocations_") {
		return service.ErrAccountProviderIdentityConflict
	}
	return err
}

func accountProviderIdentityReviewJSON(alias string) string {
	return fmt.Sprintf(`jsonb_build_object(
		'id',%[1]s.id,'account_id',%[1]s.account_id,'account_identity_version',%[1]s.account_identity_version,
		'platform',%[1]s.platform,'principal_kind',%[1]s.principal_kind,
		'issuer_fingerprint',substring(%[1]s.issuer_hash,1,16),'principal_fingerprint',substring(%[1]s.principal_hash,1,16),
		'status',%[1]s.status,'proposed_by',%[1]s.proposed_by,'decided_by',%[1]s.decided_by,
		'reason',%[1]s.reason,'evidence_ref',%[1]s.evidence_ref,'decision_reason',%[1]s.decision_reason,
		'created_at',%[1]s.created_at,'decided_at',%[1]s.decided_at)`, alias)
}

func accountProviderIdentityBindingJSON(alias string) string {
	return fmt.Sprintf(`jsonb_build_object(
		'id',%[1]s.id,'account_id',%[1]s.account_id,'account_identity_version',%[1]s.account_identity_version,
		'platform',%[1]s.platform,'principal_kind',%[1]s.principal_kind,
		'issuer_fingerprint',substring(%[1]s.issuer_hash,1,16),'principal_fingerprint',substring(%[1]s.principal_hash,1,16),
		'verification_review_id',%[1]s.verification_review_id,'verified_by',%[1]s.verified_by,
		'verified_at',%[1]s.verified_at,'revoked_by',%[1]s.revoked_by,'revoked_at',%[1]s.revoked_at)`, alias)
}

func scanAccountProviderIdentityReview(row interface{ Scan(...any) error }) (*service.AccountProviderIdentityReview, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var review service.AccountProviderIdentityReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return nil, err
	}
	return &review, nil
}

func scanAccountProviderIdentityBinding(row interface{ Scan(...any) error }) (*service.AccountProviderIdentityBinding, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var binding service.AccountProviderIdentityBinding
	if err := json.Unmarshal(raw, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (r *accountRepository) accountProviderIdentityDB() (accountProviderIdentityDB, error) {
	database, ok := r.sql.(accountProviderIdentityDB)
	if !ok || database == nil {
		return nil, errors.New("account provider identity repository requires a transactional database")
	}
	return database, nil
}

func lockAccountProviderIdentity(ctx context.Context, tx *sql.Tx, accountID int64) (*accountProviderIdentityLock, error) {
	var account accountProviderIdentityLock
	var ownerID sql.NullInt64
	var parentID sql.NullInt64
	var deletedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id,platform,ownership_mode,owner_user_id,provider_identity_version,
		isolation_state,provider_principal_binding_id,parent_account_id,deleted_at
		FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(
		&account.AccountID, &account.Platform, &account.OwnershipMode, &ownerID, &account.IdentityVersion,
		&account.IsolationState, &account.BindingID, &parentID, &deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) || deletedAt.Valid {
		return nil, service.ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	if !ownerID.Valid || ownerID.Int64 <= 0 || parentID.Valid || account.OwnershipMode != service.AccountOwnershipUserDedicated {
		return nil, service.ErrAccountProviderIdentityInvalid
	}
	account.OwnerUserID = ownerID.Int64
	return &account, nil
}

func requireAccountProviderIdentityActors(ctx context.Context, tx *sql.Tx, actorID, proposerID, ownerID int64, requireProposer bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,role,status,deleted_at FROM users WHERE id IN ($1,$2,$3) ORDER BY id FOR NO KEY UPDATE`, actorID, proposerID, ownerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	activeAdmins := make(map[int64]bool, 2)
	ownerActive := false
	for rows.Next() {
		var id int64
		var role, status string
		var deleted sql.NullTime
		if err := rows.Scan(&id, &role, &status, &deleted); err != nil {
			return err
		}
		active := status == service.StatusActive && !deleted.Valid
		if id == ownerID {
			ownerActive = active
		}
		activeAdmins[id] = active && role == service.RoleAdmin
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !ownerActive || !activeAdmins[actorID] || (requireProposer && !activeAdmins[proposerID]) {
		return service.ErrAccountProviderIdentityForbidden
	}
	return nil
}

func (r *accountRepository) GetAccountProviderIdentity(ctx context.Context, accountID int64) (*service.AccountProviderIdentityState, error) {
	database, err := r.accountProviderIdentityDB()
	if err != nil {
		return nil, err
	}
	state := &service.AccountProviderIdentityState{AccountID: accountID, Reviews: make([]*service.AccountProviderIdentityReview, 0)}
	var bindingID sql.NullInt64
	if err := database.QueryRowContext(ctx, `SELECT provider_identity_version,isolation_state,provider_principal_binding_id
		FROM accounts WHERE id=$1 AND deleted_at IS NULL`, accountID).Scan(&state.IdentityVersion, &state.IsolationState, &bindingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountNotFound
		}
		return nil, err
	}
	if bindingID.Valid {
		binding, bindingErr := scanAccountProviderIdentityBinding(database.QueryRowContext(ctx,
			`SELECT `+accountProviderIdentityBindingJSON("b")+` FROM account_provider_identity_bindings b WHERE b.id=$1 AND b.account_id=$2`, bindingID.Int64, accountID))
		if bindingErr != nil && !errors.Is(bindingErr, sql.ErrNoRows) {
			return nil, bindingErr
		}
		state.Binding = binding
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT `+accountProviderIdentityReviewJSON("r")+`
		FROM account_provider_identity_reviews r WHERE r.account_id=$1 ORDER BY r.id DESC LIMIT 50`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		review, scanErr := scanAccountProviderIdentityReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		state.Reviews = append(state.Reviews, review)
	}
	return state, rows.Err()
}

func replayAccountProviderIdentityReview(ctx context.Context, tx *sql.Tx, accountID int64, operationKey, requestHash string) (*service.AccountProviderIdentityReview, bool, error) {
	review, err := scanAccountProviderIdentityReview(tx.QueryRowContext(ctx, `SELECT `+accountProviderIdentityReviewJSON("r")+`
		FROM account_provider_identity_review_actions a JOIN account_provider_identity_reviews r ON r.id=a.review_id
		WHERE a.account_id=$1 AND a.operation_key=$2 AND a.request_hash=$3`, accountID, operationKey, requestHash))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if checkErr := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_provider_identity_review_actions WHERE account_id=$1 AND operation_key=$2)`, accountID, operationKey).Scan(&exists); checkErr != nil {
			return nil, false, checkErr
		}
		if exists {
			return nil, false, service.ErrAccountProviderIdentityConflict
		}
		return nil, false, nil
	}
	return review, err == nil, err
}

func saveAccountProviderIdentityReviewAction(ctx context.Context, tx *sql.Tx, accountID, reviewID, actorID int64, operationKey, requestHash, action, reason string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO account_provider_identity_review_actions
		(account_id,review_id,operation_key,request_hash,actor_id,action,reason) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		accountID, reviewID, operationKey, requestHash, actorID, action, reason)
	return err
}

func (r *accountRepository) ProposeAccountProviderIdentity(ctx context.Context, accountID int64, request service.AccountProviderIdentityProposal) (*service.AccountProviderIdentityResult, error) {
	if accountID <= 0 || request.ActorID <= 0 || request.ExpectedVersion <= 0 || request.IssuerHash == "" || request.PrincipalHash == "" {
		return nil, service.ErrAccountProviderIdentityRequired
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "actor": request.ActorID, "account_id": accountID,
		"identity_version": request.ExpectedVersion, "principal_kind": request.PrincipalKind, "issuer_hash": request.IssuerHash,
		"principal_hash": request.PrincipalHash, "reason": request.Reason, "evidence_ref": request.EvidenceRef})
	if err != nil {
		return nil, err
	}
	database, err := r.accountProviderIdentityDB()
	if err != nil {
		return nil, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	account, err := lockAccountProviderIdentity(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	if err := requireAccountProviderIdentityActors(ctx, tx, request.ActorID, 0, account.OwnerUserID, false); err != nil {
		return nil, err
	}
	if replay, ok, replayErr := replayAccountProviderIdentityReview(ctx, tx, accountID, request.OperationKey, hash); replayErr != nil || ok {
		if replayErr != nil {
			return nil, replayErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		state, stateErr := r.GetAccountProviderIdentity(ctx, accountID)
		return &service.AccountProviderIdentityResult{State: state, Review: replay, Replayed: true}, stateErr
	}
	if account.IdentityVersion != request.ExpectedVersion || account.IsolationState != service.AccountIsolationUnverified || account.BindingID.Valid || account.Platform != request.Platform {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_provider_identity_reviews WHERE account_id=$1 AND status='pending')`, accountID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	review, err := scanAccountProviderIdentityReview(tx.QueryRowContext(ctx, `INSERT INTO account_provider_identity_reviews AS r
		(account_id,account_identity_version,platform,issuer_hash,principal_kind,principal_hash,proposed_by,reason,evidence_ref,facts)
		SELECT id,provider_identity_version,platform,$2,$3,$4,$5,$6,$7,account_provider_identity_review_facts(a)
		FROM accounts a WHERE id=$1 RETURNING `+accountProviderIdentityReviewJSON("r"), accountID, request.IssuerHash,
		request.PrincipalKind, request.PrincipalHash, request.ActorID, request.Reason, request.EvidenceRef))
	if err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	if err := saveAccountProviderIdentityReviewAction(ctx, tx, accountID, review.ID, request.ActorID, request.OperationKey, hash, "propose", request.Reason); err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	state, err := r.GetAccountProviderIdentity(ctx, accountID)
	return &service.AccountProviderIdentityResult{State: state, Review: review}, err
}

func (r *accountRepository) DecideAccountProviderIdentity(ctx context.Context, accountID, reviewID int64, decision service.AccountProviderIdentityDecision) (*service.AccountProviderIdentityResult, error) {
	if accountID <= 0 || reviewID <= 0 {
		return nil, service.ErrAccountProviderIdentityRequired
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "actor": decision.ActorID, "account_id": accountID,
		"identity_version": decision.ExpectedVersion, "review_id": reviewID, "approve": decision.Approve, "reason": decision.Reason})
	if err != nil {
		return nil, err
	}
	database, err := r.accountProviderIdentityDB()
	if err != nil {
		return nil, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	account, err := lockAccountProviderIdentity(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	var proposerID int64
	if err := tx.QueryRowContext(ctx, `SELECT proposed_by FROM account_provider_identity_reviews WHERE id=$1 AND account_id=$2`, reviewID, accountID).Scan(&proposerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountProviderIdentityConflict
		}
		return nil, err
	}
	if decision.Approve && proposerID == decision.ActorID {
		return nil, service.ErrAccountProviderIdentityForbidden
	}
	if err := requireAccountProviderIdentityActors(ctx, tx, decision.ActorID, proposerID, account.OwnerUserID, decision.Approve); err != nil {
		return nil, err
	}
	if replay, ok, replayErr := replayAccountProviderIdentityReview(ctx, tx, accountID, decision.OperationKey, hash); replayErr != nil || ok {
		if replayErr != nil {
			return nil, replayErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		state, stateErr := r.GetAccountProviderIdentity(ctx, accountID)
		return &service.AccountProviderIdentityResult{State: state, Review: replay, Replayed: true}, stateErr
	}
	if account.IdentityVersion != decision.ExpectedVersion || account.IsolationState != service.AccountIsolationUnverified || account.BindingID.Valid {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	var reviewStatus string
	var reviewVersion int64
	var factsMatch bool
	if err := tx.QueryRowContext(ctx, `SELECT r.status,r.account_identity_version,r.facts=account_provider_identity_review_facts(a)
		FROM account_provider_identity_reviews r JOIN accounts a ON a.id=r.account_id
		WHERE r.id=$1 AND r.account_id=$2 FOR UPDATE OF r`, reviewID, accountID).Scan(&reviewStatus, &reviewVersion, &factsMatch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountProviderIdentityConflict
		}
		return nil, err
	}
	if reviewStatus != "pending" || reviewVersion != decision.ExpectedVersion || !factsMatch {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	verb := "reject"
	reviewStatus = "rejected"
	if decision.Approve {
		verb, reviewStatus = "approve", "approved"
	}
	review, err := scanAccountProviderIdentityReview(tx.QueryRowContext(ctx, `UPDATE account_provider_identity_reviews AS r
		SET status=$2,decided_by=$3,decision_reason=$4,decided_at=clock_timestamp()
		WHERE id=$1 AND status='pending' RETURNING `+accountProviderIdentityReviewJSON("r"), reviewID, reviewStatus, decision.ActorID, decision.Reason))
	if err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	if decision.Approve {
		var bindingID int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO account_provider_identity_bindings
			(account_id,account_identity_version,platform,issuer_hash,principal_kind,principal_hash,verification_review_id,verified_by)
			SELECT account_id,account_identity_version,platform,issuer_hash,principal_kind,principal_hash,id,$2
			FROM account_provider_identity_reviews WHERE id=$1 RETURNING id`, reviewID, decision.ActorID).Scan(&bindingID); err != nil {
			return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE accounts SET provider_principal_binding_id=$2,isolation_state='verified',
			isolation_verified_version=provider_identity_version,updated_at=NOW()
			WHERE id=$1 AND provider_identity_version=$3 AND isolation_state='unverified' AND provider_principal_binding_id IS NULL`,
			accountID, bindingID, decision.ExpectedVersion)
		if updateErr != nil {
			return nil, translateAccountProviderIdentityError(translatePersistenceError(updateErr, service.ErrAccountNotFound, nil))
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return nil, service.ErrAccountProviderIdentityConflict
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			return nil, err
		}
	}
	if err := saveAccountProviderIdentityReviewAction(ctx, tx, accountID, reviewID, decision.ActorID, decision.OperationKey, hash, verb, decision.Reason); err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if decision.Approve {
		r.syncSchedulerAccountSnapshot(ctx, accountID)
	}
	state, err := r.GetAccountProviderIdentity(ctx, accountID)
	return &service.AccountProviderIdentityResult{State: state, Review: review, AffectedAccountIDs: []int64{accountID}}, err
}

func (r *accountRepository) RevokeAccountProviderIdentity(ctx context.Context, accountID int64, request service.AccountProviderIdentityRevocation) (*service.AccountProviderIdentityResult, error) {
	database, err := r.accountProviderIdentityDB()
	if err != nil {
		return nil, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	account, err := lockAccountProviderIdentity(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	if err := requireAccountProviderIdentityActors(ctx, tx, request.ActorID, 0, account.OwnerUserID, false); err != nil {
		return nil, err
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "actor": request.ActorID, "account_id": accountID,
		"reason": request.Reason, "evidence_ref": request.EvidenceRef})
	if err != nil {
		return nil, err
	}
	var storedHash string
	err = tx.QueryRowContext(ctx, `SELECT request_hash FROM account_provider_identity_revocations WHERE triggering_account_id=$1 AND operation_key=$2`,
		accountID, request.OperationKey).Scan(&storedHash)
	if err == nil {
		if storedHash != hash {
			return nil, service.ErrAccountProviderIdentityConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		state, stateErr := r.GetAccountProviderIdentity(ctx, accountID)
		return &service.AccountProviderIdentityResult{State: state, Replayed: true}, stateErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !account.BindingID.Valid {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	var platform, issuerHash, principalKind, principalHash string
	var revokedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT b.platform,b.issuer_hash,b.principal_kind,b.principal_hash
		,b.revoked_at FROM account_provider_identity_bindings b WHERE b.id=$1 AND b.account_id=$2 FOR UPDATE`,
		account.BindingID.Int64, accountID).Scan(&platform, &issuerHash, &principalKind, &principalHash, &revokedAt); err != nil {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	if revokedAt.Valid || account.IsolationState != service.AccountIsolationVerified {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	var revocationID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO account_provider_identity_revocations
		(triggering_account_id,platform,issuer_hash,principal_kind,principal_hash,operation_key,request_hash,actor_id,reason,evidence_ref)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, accountID, platform, issuerHash, principalKind,
		principalHash, request.OperationKey, hash, request.ActorID, request.Reason, request.EvidenceRef).Scan(&revocationID); err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_provider_identity_bindings SET revoked_by=$2,revoked_at=clock_timestamp(),revocation_id=$3
		WHERE platform=$1 AND issuer_hash=$4 AND principal_kind=$5 AND principal_hash=$6 AND revoked_at IS NULL`,
		platform, request.ActorID, revocationID, issuerHash, principalKind, principalHash); err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	rows, err := tx.QueryContext(ctx, `UPDATE accounts SET isolation_state='revoked',isolation_verified_version=0,
		schedulable=false,updated_at=NOW() WHERE provider_principal_binding_id IN (
			SELECT id FROM account_provider_identity_bindings WHERE revocation_id=$1
		) RETURNING id`, revocationID)
	if err != nil {
		return nil, translateAccountProviderIdentityError(translatePersistenceError(err, service.ErrAccountNotFound, nil))
	}
	affectedIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		affectedIDs = append(affectedIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(affectedIDs) == 0 {
		return nil, service.ErrAccountProviderIdentityConflict
	}
	for _, id := range affectedIDs {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.syncSchedulerAccountSnapshots(ctx, affectedIDs)
	state, err := r.GetAccountProviderIdentity(ctx, accountID)
	return &service.AccountProviderIdentityResult{State: state, AffectedAccountIDs: affectedIDs}, err
}

var _ service.AccountProviderIdentityRepository = (*accountRepository)(nil)
