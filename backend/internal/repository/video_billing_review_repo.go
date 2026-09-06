package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func scanVideoBillingReview(row interface{ Scan(...any) error }) (*service.VideoBillingReview, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var review service.VideoBillingReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return nil, err
	}
	return &review, nil
}

func lockVideoReviewTask(ctx context.Context, tx *sql.Tx, publicID string, actorID, proposerID int64) (*service.VideoTask, error) {
	var ownerID int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM video_tasks WHERE public_id = $1`, publicID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrVideoTaskNotFound
		}
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, role, status, deleted_at FROM users WHERE id IN ($1,$2,$3) ORDER BY id FOR NO KEY UPDATE`, ownerID, actorID, proposerID)
	if err != nil {
		return nil, err
	}
	allowed := map[int64]bool{}
	for rows.Next() {
		var id int64
		var role, status string
		var deleted sql.NullTime
		if err := rows.Scan(&id, &role, &status, &deleted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		allowed[id] = role == service.RoleAdmin && status == service.StatusActive && !deleted.Valid
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if !allowed[actorID] {
		return nil, service.ErrVideoReviewForbidden
	}
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE public_id = $1 FOR UPDATE`, publicID))
	if err != nil {
		return nil, err
	}
	if task.UserID != ownerID {
		return nil, service.ErrVideoReviewConflict
	}
	return task, nil
}

func ensureNoVideoFinancialIntent(ctx context.Context, tx *sql.Tx, task *service.VideoTask) error {
	if task.APIKeyID == nil {
		return service.ErrVideoInvalidRequest
	}
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM usage_billing_outbox WHERE api_key_id = $1 AND request_id IN ($2,$3)
		UNION ALL SELECT 1 FROM usage_billing_dedup WHERE api_key_id = $1 AND request_id IN ($2,$3)
		UNION ALL SELECT 1 FROM usage_billing_dedup_archive WHERE api_key_id = $1 AND request_id IN ($2,$3)
		UNION ALL SELECT 1 FROM usage_logs WHERE api_key_id = $1 AND request_id IN ($2,$3)
	)`, *task.APIKeyID, service.VideoTaskCaptureRequestID(task.PublicID), service.VideoTaskReleaseRequestID(task.PublicID)).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return service.ErrVideoReviewIntentExists
	}
	return nil
}

func replayVideoReviewAction(ctx context.Context, tx *sql.Tx, task *service.VideoTask, key, hash string) (*service.VideoBillingReviewResult, error) {
	var reviewID int64
	var stored string
	err := tx.QueryRowContext(ctx, `SELECT review_id, request_hash FROM video_billing_review_actions WHERE task_id = $1 AND operation_key = $2`, task.ID, key).Scan(&reviewID, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stored != hash {
		return nil, service.ErrVideoReviewConflict
	}
	review, err := scanVideoBillingReview(tx.QueryRowContext(ctx, `SELECT to_jsonb(r) FROM video_billing_reviews r WHERE id = $1`, reviewID))
	if err != nil {
		return nil, err
	}
	return &service.VideoBillingReviewResult{Review: review, Task: task, Replayed: true}, nil
}

func saveVideoReviewAction(ctx context.Context, tx *sql.Tx, task *service.VideoTask, review *service.VideoBillingReview, key, hash, action, reason string, actorID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO video_billing_review_actions(task_id,review_id,operation_key,request_hash,actor_id,action,reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, task.ID, review.ID, key, hash, actorID, action, reason)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"review_id": review.ID, "actor_id": actorID, "proposed_by": review.ProposedBy,
		"action": review.Action, "reason": reason, "evidence_ref": review.EvidenceRef, "actual_units": review.ActualUnits,
		"actual_cost": review.ActualCost, "honor_frozen_quote": review.HonorFrozenQuote, "status": review.Status})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO video_task_events(task_id,event_type,payload,event_hash) VALUES ($1,$2,$3::jsonb,$4)`,
		task.ID, "admin_billing_review_"+action, string(payload), fmt.Sprintf("billing_review:%d:%s", review.ID, action))
	return err
}

func applyVideoBillingReview(ctx context.Context, tx *sql.Tx, task *service.VideoTask, review *service.VideoBillingReview, actorID int64, reason string) (*service.VideoTask, *service.VideoBillingReview, error) {
	review, err := scanVideoBillingReview(tx.QueryRowContext(ctx, `UPDATE video_billing_reviews r SET status = 'approved', decided_by = $2,
		decision_reason = $3, decided_at = clock_timestamp() WHERE id = $1 AND status = 'pending' RETURNING to_jsonb(r)`, review.ID, actorID, reason))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrVideoReviewConflict
	}
	if err != nil {
		return nil, nil, err
	}
	state := service.VideoBillingCapturePending
	var units any = review.ActualUnits
	if review.Action == service.BalanceSettlementRelease {
		state, units = service.VideoBillingReleasePending, nil
	}
	updated, err := scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET billing_state = $2, actual_units = $3, actual_cost = $4,
		billing_review_id = $5, next_action_at = clock_timestamp(), version = version + 1, updated_at = NOW()
		WHERE id = $1 AND version = $6 AND billing_state = 'manual_review' RETURNING to_jsonb(vt)`, task.ID, state, units, review.ActualCost, review.ID, task.Version))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrVideoVersionConflict
	}
	return updated, review, err
}

func (r *videoAdminRepository) ProposeVideoBillingReview(ctx context.Context, publicID string, request service.VideoBillingReviewRequest) (*service.VideoBillingReviewResult, error) {
	request.Reason, request.EvidenceRef = strings.TrimSpace(request.Reason), strings.TrimSpace(request.EvidenceRef)
	if err := service.ValidateVideoBillingReviewRequest(request); err != nil {
		return nil, err
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "actor": request.ActorID, "task": publicID, "task_version": request.ExpectedVersion,
		"action": request.Action, "actual_units": request.ActualUnits, "reason": request.Reason, "evidence_ref": request.EvidenceRef, "honor_frozen_quote": request.HonorFrozenQuote})
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	task, err := lockVideoReviewTask(ctx, tx, publicID, request.ActorID, 0)
	if err != nil {
		return nil, err
	}
	if replay, err := replayVideoReviewAction(ctx, tx, task, request.OperationKey, hash); err != nil || replay != nil {
		if err != nil {
			return nil, err
		}
		return replay, tx.Commit()
	}
	if task.Version != request.ExpectedVersion {
		return nil, service.ErrVideoVersionConflict
	}
	if err := ensureNoVideoFinancialIntent(ctx, tx, task); err != nil {
		return nil, err
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM video_billing_reviews WHERE task_id = $1 AND status = 'pending')`, task.ID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, service.ErrVideoReviewConflict
	}
	plan, err := service.PlanVideoBillingReview(task, request)
	if err != nil {
		return nil, err
	}
	review, err := scanVideoBillingReview(tx.QueryRowContext(ctx, `INSERT INTO video_billing_reviews AS r
		(task_id,action,proposed_by,task_version,actual_units,actual_cost,hold_amount,reason,evidence_ref,honor_frozen_quote,requires_second_actor,approval_threshold_usd,billing_model,facts)
		SELECT id,$2,$3,version,$4,$5,hold_amount,$6,$7,$8,$9,$10,$11,video_billing_review_facts(vt) FROM video_tasks vt WHERE id = $1 RETURNING to_jsonb(r)`,
		task.ID, plan.Action, request.ActorID, plan.ActualUnits, plan.ActualCost, plan.Reason, plan.EvidenceRef, plan.HonorFrozenQuote, plan.RequiresSecondActor, plan.ApprovalThresholdUSD, canonicalizeUsageBillingIdentity(task.UpstreamModel, 100)))
	if err != nil {
		return nil, err
	}
	if review.RequiresSecondActor {
		task, err = scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET billing_review_id = NULL, version = version + 1, updated_at = NOW() WHERE id = $1 RETURNING to_jsonb(vt)`, task.ID))
	} else {
		task, review, err = applyVideoBillingReview(ctx, tx, task, review, request.ActorID, request.Reason)
	}
	if err != nil {
		return nil, err
	}
	if err := saveVideoReviewAction(ctx, tx, task, review, request.OperationKey, hash, "propose", request.Reason, request.ActorID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.VideoBillingReviewResult{Review: review, Task: task}, nil
}

func (r *videoAdminRepository) DecideVideoBillingReview(ctx context.Context, publicID string, reviewID int64, decision service.VideoBillingReviewDecision) (*service.VideoBillingReviewResult, error) {
	decision.Reason = strings.TrimSpace(decision.Reason)
	if err := service.ValidateVideoBillingReviewDecision(decision); err != nil {
		return nil, err
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "actor": decision.ActorID, "task": publicID, "task_version": decision.ExpectedVersion,
		"review_id": reviewID, "approve": decision.Approve, "reason": decision.Reason})
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var proposer int64
	err = tx.QueryRowContext(ctx, `SELECT proposed_by FROM video_billing_reviews WHERE id = $1 AND task_id = (SELECT id FROM video_tasks WHERE public_id = $2)`, reviewID, publicID).Scan(&proposer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoReviewConflict
	}
	if err != nil {
		return nil, err
	}
	checkProposer := int64(0)
	if decision.Approve {
		checkProposer = proposer
	}
	task, err := lockVideoReviewTask(ctx, tx, publicID, decision.ActorID, checkProposer)
	if err != nil {
		return nil, err
	}
	if replay, err := replayVideoReviewAction(ctx, tx, task, decision.OperationKey, hash); err != nil || replay != nil {
		if err != nil {
			return nil, err
		}
		return replay, tx.Commit()
	}
	if task.Version != decision.ExpectedVersion {
		return nil, service.ErrVideoVersionConflict
	}
	if decision.Approve {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND role = 'admin' AND status = 'active' AND deleted_at IS NULL)`, proposer).Scan(&active); err != nil {
			return nil, err
		}
		if !active {
			return nil, service.ErrVideoReviewForbidden
		}
	}
	review, err := scanVideoBillingReview(tx.QueryRowContext(ctx, `SELECT to_jsonb(r) FROM video_billing_reviews r WHERE id = $1 FOR UPDATE`, reviewID))
	if err != nil {
		return nil, err
	}
	if review.Status != "pending" || task.BillingState != service.VideoBillingManualReview {
		return nil, service.ErrVideoReviewConflict
	}
	if decision.Approve && review.ProposedBy == decision.ActorID {
		return nil, service.ErrVideoReviewForbidden
	}
	verb := "reject"
	if decision.Approve {
		if err := ensureNoVideoFinancialIntent(ctx, tx, task); err != nil {
			return nil, err
		}
		var unchanged bool
		if err := tx.QueryRowContext(ctx, `SELECT facts = video_billing_review_facts(vt) FROM video_billing_reviews r JOIN video_tasks vt ON vt.id = r.task_id WHERE r.id = $1`, review.ID).Scan(&unchanged); err != nil {
			return nil, err
		}
		if !unchanged {
			return nil, service.ErrVideoReviewConflict
		}
		task, review, err = applyVideoBillingReview(ctx, tx, task, review, decision.ActorID, decision.Reason)
		verb = "approve"
	} else {
		review, err = scanVideoBillingReview(tx.QueryRowContext(ctx, `UPDATE video_billing_reviews r SET status = 'rejected', decided_by = $2, decision_reason = $3, decided_at = clock_timestamp() WHERE id = $1 RETURNING to_jsonb(r)`, review.ID, decision.ActorID, decision.Reason))
		if err == nil {
			task, err = scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET version = version + 1, updated_at = NOW() WHERE id = $1 RETURNING to_jsonb(vt)`, task.ID))
		}
	}
	if err != nil {
		return nil, err
	}
	if err := saveVideoReviewAction(ctx, tx, task, review, decision.OperationKey, hash, verb, decision.Reason, decision.ActorID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.VideoBillingReviewResult{Review: review, Task: task}, nil
}

func (r *videoAdminRepository) ListVideoBillingReviews(ctx context.Context, publicID string) ([]*service.VideoBillingReview, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT to_jsonb(r) FROM video_billing_reviews r JOIN video_tasks vt ON vt.id = r.task_id WHERE vt.public_id = $1 ORDER BY r.id DESC LIMIT 50`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reviews := make([]*service.VideoBillingReview, 0)
	for rows.Next() {
		review, err := scanVideoBillingReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}

func (r *videoTaskRepository) VerifyVideoBillingReview(ctx context.Context, task *service.VideoTask) (*service.VideoBillingReview, error) {
	if task == nil || task.BillingReviewID == nil {
		return nil, service.ErrVideoReviewRequired
	}
	var raw []byte
	var matches bool
	err := r.db.QueryRowContext(ctx, `SELECT to_jsonb(r), r.facts = video_billing_review_facts(vt)
		FROM video_tasks vt JOIN video_billing_reviews r ON r.id = vt.billing_review_id
		WHERE vt.id = $1 AND vt.version = $2 AND r.id = $3 AND r.task_id = vt.id AND r.status = 'approved'`, task.ID, task.Version, *task.BillingReviewID).Scan(&raw, &matches)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoReviewConflict
	}
	if err != nil {
		return nil, err
	}
	var review service.VideoBillingReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return nil, err
	}
	if !matches || task.ActualCost == nil || *task.ActualCost != review.ActualCost || task.HoldAmount == nil || *task.HoldAmount != review.HoldAmount ||
		(task.BillingState == service.VideoBillingCapturePending && (review.Action != service.BalanceSettlementCapture || task.ActualUnits == nil || *task.ActualUnits != review.ActualUnits)) ||
		(task.BillingState == service.VideoBillingReleasePending && review.Action != service.BalanceSettlementRelease) {
		return nil, service.ErrVideoReviewConflict
	}
	return &review, nil
}
