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

func scanVideoSubmissionReview(row videoJSONScanner) (*service.VideoSubmissionReview, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var review service.VideoSubmissionReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return nil, err
	}
	return &review, nil
}

func videoSubmissionReviewEligible(task *service.VideoTask) bool {
	return task != nil && task.GenerationState == service.VideoGenerationSubmissionUnknown && task.ProviderTaskID == nil &&
		(task.BillingState == service.VideoBillingHeld || task.BillingState == service.VideoBillingManualReview) &&
		task.AccountID != nil && task.APIKeyID != nil && task.HoldAmount != nil
}

func lockVideoSubmissionIdentity(ctx context.Context, tx *sql.Tx, task *service.VideoTask) (int64, error) {
	var version int64
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT a.provider_identity_version,
		CASE WHEN vt.request_attributes ? 'account_identity_version' THEN vt.request_attributes->'account_identity_version'=to_jsonb(a.provider_identity_version) ELSE true END
		FROM accounts a JOIN video_tasks vt ON vt.account_id=a.id WHERE vt.id=$1 AND a.deleted_at IS NULL FOR SHARE OF a`, task.ID).Scan(&version, &matches)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrVideoReviewConflict
	}
	if err != nil {
		return 0, err
	}
	if version <= 0 || !matches {
		return 0, service.ErrVideoReviewConflict
	}
	return version, nil
}

func videoSubmissionIDAvailable(ctx context.Context, tx *sql.Tx, task *service.VideoTask, providerID string) error {
	if task.AccountOwnerUserID != nil && *task.AccountOwnerUserID != task.UserID {
		return service.ErrVideoReviewForbidden
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `SELECT (owner_user_id IS NULL OR owner_user_id=$2) AND (video_owner_user_id IS NULL OR video_owner_user_id=$2)
		FROM accounts WHERE id=$1`, task.AccountID, task.UserID).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return service.ErrVideoReviewForbidden
	}
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM video_tasks WHERE provider=$1 AND account_id=$2 AND provider_task_id=$3
		UNION ALL SELECT 1 FROM video_resources WHERE provider=$1 AND account_id=$2 AND provider_resource_id=$3
	)`, task.Provider, task.AccountID, providerID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return service.ErrVideoReviewConflict
	}
	return nil
}

func replayVideoSubmissionAction(ctx context.Context, tx *sql.Tx, task *service.VideoTask, key, hash string) (*service.VideoSubmissionReviewResult, error) {
	var stored string
	var reviewID int64
	err := tx.QueryRowContext(ctx, `SELECT review_id,request_hash FROM video_submission_review_actions WHERE task_id=$1 AND operation_key=$2`, task.ID, key).Scan(&reviewID, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stored != hash {
		return nil, service.ErrVideoReviewConflict
	}
	review, err := scanVideoSubmissionReview(tx.QueryRowContext(ctx, `SELECT to_jsonb(r) FROM video_submission_reviews r WHERE id=$1`, reviewID))
	return &service.VideoSubmissionReviewResult{Task: task, Review: review, Replayed: true}, err
}

func saveVideoSubmissionAction(ctx context.Context, tx *sql.Tx, task *service.VideoTask, review *service.VideoSubmissionReview, key, hash, action, reason string, actorID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO video_submission_review_actions(task_id,review_id,operation_key,request_hash,actor_id,action,reason)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, task.ID, review.ID, key, hash, actorID, action, reason)
	if err != nil {
		return err
	}
	_, err = insertVideoTaskEventTx(ctx, tx, service.VideoTaskEvent{TaskID: &task.ID, Provider: task.Provider, AccountID: task.AccountID,
		EventType: "admin_submission_review_" + action, EventHash: fmt.Sprintf("submission_review:%d:%s", review.ID, action),
		Payload: map[string]any{"review_id": review.ID, "actor_id": actorID, "proposed_by": review.ProposedBy, "action": review.Action,
			"provider_task_id": review.ProviderTaskID, "reason": reason, "evidence_ref": review.EvidenceRef, "status": review.Status}})
	return err
}

func (r *videoAdminRepository) ProposeVideoSubmissionReview(ctx context.Context, publicID string, request service.VideoSubmissionReviewRequest) (*service.VideoSubmissionReviewResult, error) {
	request.Reason, request.EvidenceRef = strings.TrimSpace(request.Reason), strings.TrimSpace(request.EvidenceRef)
	if err := service.ValidateVideoSubmissionReviewRequest(request); err != nil {
		return nil, err
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "task": publicID, "task_version": request.ExpectedVersion,
		"actor": request.ActorID, "action": request.Action, "provider_task_id": request.ProviderTaskID, "reason": request.Reason, "evidence_ref": request.EvidenceRef})
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
	if replay, err := replayVideoSubmissionAction(ctx, tx, task, request.OperationKey, hash); err != nil || replay != nil {
		if err != nil {
			return nil, err
		}
		return replay, tx.Commit()
	}
	if task.Version != request.ExpectedVersion {
		return nil, service.ErrVideoVersionConflict
	}
	if !videoSubmissionReviewEligible(task) {
		return nil, service.ErrVideoInvalidTransition
	}
	if err := ensureNoVideoFinancialIntent(ctx, tx, task); err != nil {
		return nil, err
	}
	version, err := lockVideoSubmissionIdentity(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM video_submission_reviews WHERE task_id=$1 AND status='pending')`, task.ID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, service.ErrVideoReviewConflict
	}
	if request.Action == service.VideoSubmissionCreated {
		if err := videoSubmissionIDAvailable(ctx, tx, task, request.ProviderTaskID); err != nil {
			return nil, err
		}
	}
	review, err := scanVideoSubmissionReview(tx.QueryRowContext(ctx, `INSERT INTO video_submission_reviews AS r
		(task_id,action,provider_task_id,proposed_by,task_version,account_identity_version,reason,evidence_ref,facts)
		SELECT id,$2,NULLIF($3,''),$4,version,$5,$6,$7,video_submission_review_facts(vt) FROM video_tasks vt WHERE id=$1 RETURNING to_jsonb(r)`,
		task.ID, request.Action, request.ProviderTaskID, request.ActorID, version, request.Reason, request.EvidenceRef))
	if err != nil {
		return nil, err
	}
	task, err = scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET version=version+1,updated_at=NOW() WHERE id=$1 RETURNING to_jsonb(vt)`, task.ID))
	if err != nil {
		return nil, err
	}
	if err := saveVideoSubmissionAction(ctx, tx, task, review, request.OperationKey, hash, "propose", request.Reason, request.ActorID); err != nil {
		return nil, err
	}
	return &service.VideoSubmissionReviewResult{Task: task, Review: review}, tx.Commit()
}

func loadVideoSubmissionDecision(ctx context.Context, tx *sql.Tx, publicID string, reviewID int64, decision service.VideoBillingReviewDecision) (*service.VideoSubmissionReviewResult, string, error) {
	if err := service.ValidateVideoBillingReviewDecision(decision); err != nil {
		return nil, "", err
	}
	hash, err := service.HashVideoRequest(map[string]any{"version": 1, "task": publicID, "task_version": decision.ExpectedVersion,
		"review_id": reviewID, "actor": decision.ActorID, "approve": decision.Approve, "reason": strings.TrimSpace(decision.Reason)})
	if err != nil {
		return nil, "", err
	}
	var proposer int64
	err = tx.QueryRowContext(ctx, `SELECT proposed_by FROM video_submission_reviews WHERE id=$1 AND task_id=(SELECT id FROM video_tasks WHERE public_id=$2)`, reviewID, publicID).Scan(&proposer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", service.ErrVideoReviewConflict
	}
	if err != nil {
		return nil, "", err
	}
	task, err := lockVideoReviewTask(ctx, tx, publicID, decision.ActorID, proposer)
	if err != nil {
		return nil, "", err
	}
	if replay, err := replayVideoSubmissionAction(ctx, tx, task, decision.OperationKey, hash); err != nil || replay != nil {
		return replay, hash, err
	}
	if task.Version != decision.ExpectedVersion {
		return nil, "", service.ErrVideoVersionConflict
	}
	review, err := scanVideoSubmissionReview(tx.QueryRowContext(ctx, `SELECT to_jsonb(r) FROM video_submission_reviews r WHERE id=$1 FOR UPDATE`, reviewID))
	if err != nil {
		return nil, "", err
	}
	if review.Status != "pending" {
		return nil, "", service.ErrVideoReviewConflict
	}
	if decision.Approve {
		if proposer == decision.ActorID {
			return nil, "", service.ErrVideoReviewForbidden
		}
		var active, unchanged bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, proposer).Scan(&active); err != nil {
			return nil, "", err
		}
		if !active {
			return nil, "", service.ErrVideoReviewForbidden
		}
		if !videoSubmissionReviewEligible(task) {
			return nil, "", service.ErrVideoInvalidTransition
		}
		if err := ensureNoVideoFinancialIntent(ctx, tx, task); err != nil {
			return nil, "", err
		}
		if err := tx.QueryRowContext(ctx, `SELECT r.facts=video_submission_review_facts(vt) FROM video_submission_reviews r JOIN video_tasks vt ON vt.id=r.task_id WHERE r.id=$1`, review.ID).Scan(&unchanged); err != nil {
			return nil, "", err
		}
		if !unchanged {
			return nil, "", service.ErrVideoReviewConflict
		}
		version, err := lockVideoSubmissionIdentity(ctx, tx, task)
		if err != nil {
			return nil, "", err
		}
		if version != review.AccountIdentityVersion {
			return nil, "", service.ErrVideoReviewConflict
		}
		if review.Action == service.VideoSubmissionCreated {
			if err := videoSubmissionIDAvailable(ctx, tx, task, *review.ProviderTaskID); err != nil {
				return nil, "", err
			}
		}
	}
	return &service.VideoSubmissionReviewResult{Task: task, Review: review}, hash, nil
}

func (r *videoAdminRepository) PrepareVideoSubmissionDecision(ctx context.Context, publicID string, reviewID int64, decision service.VideoBillingReviewDecision) (*service.VideoSubmissionReviewResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	prepared, _, err := loadVideoSubmissionDecision(ctx, tx, publicID, reviewID, decision)
	if err != nil {
		return nil, err
	}
	return prepared, tx.Commit()
}

func (r *videoAdminRepository) DecideVideoSubmissionReview(ctx context.Context, publicID string, reviewID int64, decision service.VideoBillingReviewDecision, observation *service.VideoSubmissionObservation) (*service.VideoSubmissionReviewResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	prepared, hash, err := loadVideoSubmissionDecision(ctx, tx, publicID, reviewID, decision)
	if err != nil {
		return nil, err
	}
	if prepared.Replayed {
		return prepared, tx.Commit()
	}
	task, review := prepared.Task, prepared.Review
	status, verb := "rejected", "reject"
	var proof any
	if decision.Approve {
		status, verb = "approved", "approve"
		if review.Action == service.VideoSubmissionCreated {
			if observation == nil || observation.AccountIdentityVersion != review.AccountIdentityVersion || observation.Acceptance.ProviderTaskID != *review.ProviderTaskID {
				return nil, service.ErrVideoReviewConflict
			}
			encoded, err := json.Marshal(map[string]any{"provider_task_id": observation.Acceptance.ProviderTaskID,
				"generation_state": observation.Acceptance.GenerationState, "metadata": observation.Acceptance.ResponseMetadata, "usage": observation.Acceptance.UsageSnapshot})
			if err != nil {
				return nil, err
			}
			proof = string(encoded)
		}
	}
	review, err = scanVideoSubmissionReview(tx.QueryRowContext(ctx, `UPDATE video_submission_reviews r SET status=$2,decided_by=$3,
		decision_reason=$4,decided_at=clock_timestamp(),provider_observation=$5::jsonb WHERE id=$1 RETURNING to_jsonb(r)`,
		review.ID, status, decision.ActorID, strings.TrimSpace(decision.Reason), proof))
	if err != nil {
		return nil, err
	}
	if decision.Approve {
		if _, err := tx.ExecContext(ctx, `UPDATE video_tasks SET submission_review_id=$2 WHERE id=$1`, task.ID, review.ID); err != nil {
			return nil, err
		}
		if review.Action == service.VideoSubmissionNotCreated {
			task, err = applyVideoSubmissionRelease(ctx, tx, task, review, decision)
		} else {
			if task.Operation == service.VideoOperationCharacterCreate {
				_, err = createVideoResourceQuery(ctx, tx, service.VideoCreateResourceParams{
					Owner: service.VideoOwner{UserID: task.UserID, APIKeyID: *task.APIKeyID, GroupID: task.GroupID}, Provider: task.Provider,
					ChannelID: task.ChannelID, AccountID: *task.AccountID, SourceTaskID: &task.ID, ProviderResourceID: *review.ProviderTaskID,
					Model: task.UpstreamModel, Status: "ready", Metadata: map[string]any{"name": observation.CharacterName}, ExpiresAt: observation.Acceptance.ContentExpiresAt})
				if err != nil {
					return nil, err
				}
			}
			task, err = saveVideoProviderAcceptedTx(service.WithVideoTaskWriteGuard(ctx, task.ID, task.Version), tx, publicID, observation.Acceptance)
		}
	} else {
		task, err = scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET version=version+1,updated_at=NOW() WHERE id=$1 RETURNING to_jsonb(vt)`, task.ID))
	}
	if err != nil {
		return nil, err
	}
	if err := saveVideoSubmissionAction(ctx, tx, task, review, decision.OperationKey, hash, verb, strings.TrimSpace(decision.Reason), decision.ActorID); err != nil {
		return nil, err
	}
	return &service.VideoSubmissionReviewResult{Task: task, Review: review}, tx.Commit()
}

func applyVideoSubmissionRelease(ctx context.Context, tx *sql.Tx, task *service.VideoTask, review *service.VideoSubmissionReview, decision service.VideoBillingReviewDecision) (*service.VideoTask, error) {
	task, err := scanVideoTask(tx.QueryRowContext(ctx, `UPDATE video_tasks vt SET generation_state='failed',billing_state='manual_review',
		finished_at=COALESCE(finished_at,NOW()),last_error_kind='submission',last_error_code='confirmed_not_created',
		last_error_message='provider submission was confirmed not created',version=version+1,updated_at=NOW() WHERE id=$1 RETURNING to_jsonb(vt)`, task.ID))
	if err != nil {
		return nil, err
	}
	billing, err := scanVideoBillingReview(tx.QueryRowContext(ctx, `INSERT INTO video_billing_reviews AS r
		(task_id,action,proposed_by,task_version,billing_model,actual_units,actual_cost,hold_amount,reason,evidence_ref,
		requires_second_actor,approval_threshold_usd,facts,submission_review_id)
		SELECT id,'release',$2,version,$3,0,0,hold_amount,$4,$5,true,0,video_billing_review_facts(vt),$6 FROM video_tasks vt WHERE id=$1 RETURNING to_jsonb(r)`,
		task.ID, review.ProposedBy, canonicalizeUsageBillingIdentity(task.UpstreamModel, 100), review.Reason, review.EvidenceRef, review.ID))
	if err != nil {
		return nil, err
	}
	hash, err := service.HashVideoRequest(map[string]any{"submission_review_id": review.ID, "billing_review_id": billing.ID})
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("submission:%d", review.ID)
	if err := saveVideoReviewAction(ctx, tx, task, billing, key+":propose", hash, "propose", review.Reason, review.ProposedBy); err != nil {
		return nil, err
	}
	task, billing, err = applyVideoBillingReview(ctx, tx, task, billing, decision.ActorID, strings.TrimSpace(decision.Reason))
	if err != nil {
		return nil, err
	}
	if err := saveVideoReviewAction(ctx, tx, task, billing, key+":approve", hash, "approve", strings.TrimSpace(decision.Reason), decision.ActorID); err != nil {
		return nil, err
	}
	return task, nil
}

func (r *videoAdminRepository) ListVideoSubmissionReviews(ctx context.Context, publicID string) ([]*service.VideoSubmissionReview, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT to_jsonb(r) FROM video_submission_reviews r JOIN video_tasks vt ON vt.id=r.task_id WHERE vt.public_id=$1 ORDER BY r.id DESC LIMIT 50`, publicID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	reviews := make([]*service.VideoSubmissionReview, 0)
	for rows.Next() {
		review, err := scanVideoSubmissionReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}
