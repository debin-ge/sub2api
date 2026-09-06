//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type submissionReviewAccountReader struct {
	service.AccountRepository
	account service.Account
}

func (r *submissionReviewAccountReader) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.account.ID != id {
		return nil, service.ErrAccountNotFound
	}
	account := r.account
	return &account, nil
}

type submissionReviewProvider struct {
	service.VideoProvider
	get func(context.Context, *service.Account, service.ProviderTaskRef) (*service.ProviderVideoTask, error)
}

func (p *submissionReviewProvider) Name() string { return service.VideoProviderOpenAI }
func (p *submissionReviewProvider) Get(ctx context.Context, account *service.Account, ref service.ProviderTaskRef) (*service.ProviderVideoTask, error) {
	return p.get(ctx, account, ref)
}

func newVideoSubmissionReviewFixture(t *testing.T) (*videoTaskRepository, *videoAdminRepository, *service.VideoTask, int64, int64) {
	t.Helper()
	repo, admin, task, proposer, approver := newVideoBillingReviewFixture(t, 5)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE video_tasks SET generation_state='submission_unknown',billing_state='held',
		provider_task_id=NULL,finished_at=NULL,submission_unknown_at=NOW() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	task, err = repo.GetVideoTaskByPublicID(context.Background(), task.PublicID)
	require.NoError(t, err)
	return repo, admin, task, proposer, approver
}

func videoSubmissionRequest(task *service.VideoTask, actor int64, action string) service.VideoSubmissionReviewRequest {
	request := service.VideoSubmissionReviewRequest{ActorID: actor, OperationKey: "submission:propose", ExpectedVersion: task.Version,
		Action: action, Reason: "Original submission evidence verified", EvidenceRef: "ticket:UNKNOWN"}
	if action == service.VideoSubmissionCreated {
		request.ProviderTaskID = "video_exact"
	}
	return request
}

func videoSubmissionDecision(result *service.VideoSubmissionReviewResult, actor int64) service.VideoBillingReviewDecision {
	return service.VideoBillingReviewDecision{ActorID: actor, OperationKey: "submission:approve", ExpectedVersion: result.Task.Version,
		Approve: true, Reason: "Independently verified original submission"}
}

func videoSubmissionObservation(review *service.VideoSubmissionReview) *service.VideoSubmissionObservation {
	next := time.Now().Add(time.Minute)
	return &service.VideoSubmissionObservation{AccountIdentityVersion: review.AccountIdentityVersion,
		Acceptance: service.VideoProviderAcceptance{ProviderTaskID: *review.ProviderTaskID, GenerationState: service.VideoGenerationQueued,
			BillingState: service.VideoBillingHeld, NextActionAt: &next, ProviderStatus: "queued"}}
}

func TestVideoSubmissionReviewNotCreatedUsesIndependentV4Release(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	request := videoSubmissionRequest(task, proposer, service.VideoSubmissionNotCreated)
	proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
	require.NoError(t, err)
	require.Equal(t, "pending", proposed.Review.Status)
	require.Equal(t, service.VideoGenerationSubmissionUnknown, proposed.Task.GenerationState)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
	replayed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	decision := videoSubmissionDecision(proposed, proposer)
	_, err = admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, nil)
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	decision.ActorID = approver
	approved, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, nil)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationFailed, approved.Task.GenerationState)
	require.Equal(t, service.VideoBillingReleasePending, approved.Task.BillingState)
	require.Equal(t, approved.Review.ID, *approved.Task.SubmissionReviewID)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
	bridge, err := repo.VerifyVideoBillingReview(ctx, approved.Task)
	require.NoError(t, err)
	require.Equal(t, proposed.Review.ID, *bridge.SubmissionReviewID)
	require.Equal(t, proposer, bridge.ProposedBy)
	require.Equal(t, approver, *bridge.DecidedBy)
	settlement, usage := reviewedVideoSettlement(approved.Task, bridge)
	paid, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	require.True(t, paid.Applied)
	assertVideoBudgetTotals(t, task.UserID, 1, 1000, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, paid.OutboxReceipt.WorkerID, paid.OutboxReceipt.ID))
	repeated, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, nil)
	require.NoError(t, err)
	require.True(t, repeated.Replayed)
	require.Equal(t, service.VideoBillingReleased, repeated.Task.BillingState)
	var billings, operations int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_billing_reviews WHERE task_id=$1`, task.ID).Scan(&billings))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_submission_review_actions WHERE task_id=$1`, task.ID).Scan(&operations))
	require.Equal(t, 1, billings)
	require.Equal(t, 2, operations)
}

func TestVideoSubmissionReviewCreatedRequiresVerifiedExactIdentity(t *testing.T) {
	ctx := context.Background()
	_, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated))
	require.NoError(t, err)
	decision := videoSubmissionDecision(proposed, approver)
	for _, invalid := range []string{"missing", "id", "identity"} {
		observation := videoSubmissionObservation(proposed.Review)
		switch invalid {
		case "missing":
			observation = nil
		case "id":
			observation.Acceptance.ProviderTaskID = "wrong_id"
		case "identity":
			observation.AccountIdentityVersion++
		}
		_, err = admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, observation)
		require.ErrorIs(t, err, service.ErrVideoReviewConflict)
	}
	approved, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, videoSubmissionObservation(proposed.Review))
	require.NoError(t, err)
	require.Equal(t, "video_exact", *approved.Task.ProviderTaskID)
	require.Equal(t, service.VideoGenerationQueued, approved.Task.GenerationState)
	require.Contains(t, string(approved.Review.ProviderObservation), "video_exact")
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, proposer)
	require.NoError(t, err)
	replayed, err := admin.PrepareVideoSubmissionDecision(ctx, task.PublicID, proposed.Review.ID, decision)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewCharacterApprovalAndResourceAreAtomic(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET operation='character_create',billing_unit='request' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	request := videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated)
	request.ProviderTaskID = "char_exact"
	proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
	require.NoError(t, err)
	decision := videoSubmissionDecision(proposed, approver)
	observation := videoSubmissionObservation(proposed.Review)
	observation.CharacterName, observation.Acceptance.GenerationState = "Character", service.VideoGenerationSubmitting
	_, err = admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, observation)
	require.ErrorIs(t, err, service.ErrVideoInvalidTransition)
	var resources int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_resources WHERE source_task_id=$1`, task.ID).Scan(&resources))
	require.Zero(t, resources)
	reviews, err := admin.ListVideoSubmissionReviews(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, "pending", reviews[0].Status)
	observation.Acceptance.GenerationState, observation.Acceptance.BillingState = service.VideoGenerationCompleted, service.VideoBillingCapturePending
	units, cost := 1.0, 1.0
	observation.Acceptance.ActualUnits, observation.Acceptance.ActualCost = &units, &cost
	approved, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, observation)
	require.NoError(t, err)
	resource, err := (&videoResourceRepository{db: integrationDB}).GetVideoResourceBySourceTaskForOwner(ctx, task.UserID, task.ID)
	require.NoError(t, err)
	require.Equal(t, *task.AccountID, resource.AccountID)
	require.Equal(t, "char_exact", resource.ProviderResourceID)
	require.Equal(t, service.VideoBillingCapturePending, approved.Task.BillingState)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewRevalidatesAfterProviderRead(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated))
	require.NoError(t, err)
	decision := videoSubmissionDecision(proposed, approver)
	_, err = admin.PrepareVideoSubmissionDecision(ctx, task.PublicID, proposed.Review.ID, decision)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET usage_snapshot='{"seconds":7}',version=version+1 WHERE id=$1`, task.ID)
	require.NoError(t, err)
	_, err = admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, videoSubmissionObservation(proposed.Review))
	require.ErrorIs(t, err, service.ErrVideoVersionConflict)
	current, err := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	decision.ExpectedVersion = current.Version
	_, err = admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, videoSubmissionObservation(proposed.Review))
	require.ErrorIs(t, err, service.ErrVideoReviewConflict)
	decision.Approve = false
	rejected, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, nil)
	require.NoError(t, err)
	require.Equal(t, "rejected", rejected.Review.Status)
	require.Nil(t, rejected.Task.ProviderTaskID)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewRejectsOldWritersAndUnsafeEvidence(t *testing.T) {
	ctx := context.Background()
	_, admin, task, proposer, _ := newVideoSubmissionReviewFixture(t)
	for _, mutation := range []string{`generation_state='failed',billing_state='release_pending'`, `generation_state='queued',provider_task_id='unreviewed'`, `provider_task_id='unreviewed'`} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET `+mutation+` WHERE id=$1`, task.ID)
		require.ErrorContains(t, err, "approved submission review")
	}
	request := videoSubmissionRequest(task, proposer, service.VideoSubmissionNotCreated)
	for _, providerID := range []string{"sk-provider-secret", "https://provider.test/video?token=private"} {
		unsafe := request
		unsafe.Action, unsafe.ProviderTaskID = service.VideoSubmissionCreated, providerID
		_, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, unsafe)
		require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	}
	request.EvidenceRef = "https://private.invalid/secret"
	_, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewRequired)
	request.EvidenceRef, request.ActorID = "ticket:UNKNOWN", task.UserID
	_, err = admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET billing_state='manual_review',quarantined_at=NOW() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewRefusesRegisteredResourcesAndRevokedProposer(t *testing.T) {
	ctx := context.Background()
	_, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated))
	require.NoError(t, err)
	_, err = (&videoResourceRepository{db: integrationDB}).CreateVideoResource(ctx, service.VideoCreateResourceParams{
		Owner: service.VideoOwner{UserID: approver, APIKeyID: *task.APIKeyID}, Provider: task.Provider, AccountID: *task.AccountID, ProviderResourceID: "video_exact"})
	require.NoError(t, err)
	decision := videoSubmissionDecision(proposed, approver)
	_, err = admin.PrepareVideoSubmissionDecision(ctx, task.PublicID, proposed.Review.ID, decision)
	require.ErrorIs(t, err, service.ErrVideoReviewConflict)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM video_resources WHERE provider_resource_id='video_exact' AND account_id=$1`, task.AccountID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, proposer)
	require.NoError(t, err)
	_, err = admin.PrepareVideoSubmissionDecision(ctx, task.PublicID, proposed.Review.ID, decision)
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewConcurrentReplayAndDecisionRace(t *testing.T) {
	ctx := context.Background()
	_, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	request := videoSubmissionRequest(task, proposer, service.VideoSubmissionNotCreated)
	type result struct {
		value *service.VideoSubmissionReviewResult
		err   error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, request)
			results <- result{value, err}
		}()
	}
	wait.Wait()
	close(results)
	var proposed *service.VideoSubmissionReviewResult
	for outcome := range results {
		require.NoError(t, outcome.err)
		if proposed != nil {
			require.Equal(t, proposed.Review.ID, outcome.value.Review.ID)
		}
		proposed = outcome.value
	}
	decisions := make(chan error, 2)
	for _, approve := range []bool{true, false} {
		wait.Add(1)
		go func(approve bool) {
			defer wait.Done()
			decision := videoSubmissionDecision(proposed, approver)
			decision.Approve = approve
			if !approve {
				decision.OperationKey = "submission:reject"
			}
			_, err := admin.DecideVideoSubmissionReview(ctx, task.PublicID, proposed.Review.ID, decision, nil)
			decisions <- err
		}(approve)
	}
	wait.Wait()
	close(decisions)
	winners := 0
	for err := range decisions {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, service.ErrVideoVersionConflict)
		}
	}
	require.Equal(t, 1, winners)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewUnknownCannotCreateFinancialIntentDirectly(t *testing.T) {
	ctx := context.Background()
	repo, _, task, _, _ := newVideoSubmissionReviewFixture(t)
	settlement, usage := reviewedVideoSettlement(task, &service.VideoBillingReview{Action: service.BalanceSettlementRelease})
	_, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.ErrorContains(t, err, "unresolved video submission")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_outbox WHERE api_key_id=$1`, task.APIKeyID).Scan(&count))
	require.Zero(t, count)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}

func TestVideoSubmissionReviewKnownIDKeepsMachineObservationButCannotRebind(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "known-id", "known-body", 4))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state='submission_unknown',provider_task_id='known_id' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	for _, mutation := range []string{`provider_task_id='different_id'`, `provider_task_id=NULL`, `generation_state='failed',billing_state='release_pending',last_error_code='confirmed_not_created'`} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET `+mutation+` WHERE id=$1`, task.ID)
		require.ErrorContains(t, err, "known video submission identity")
	}
	updated, err := repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
		service.VideoProviderAcceptance{ProviderTaskID: "known_id", GenerationState: service.VideoGenerationQueued, BillingState: service.VideoBillingHeld})
	require.NoError(t, err)
	require.Equal(t, "known_id", *updated.ProviderTaskID)
	require.Equal(t, service.VideoGenerationQueued, updated.GenerationState)
	assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
}

func TestVideoSubmissionReviewApprovalDoesNotHoldSQLLocksDuringProviderGet(t *testing.T) {
	for _, scenario := range []string{"approved", "task_changed", "actor_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repo, adminRepo, task, proposer, approver := newVideoSubmissionReviewFixture(t)
			proposed, err := adminRepo.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated))
			require.NoError(t, err)
			calls := 0
			provider := &submissionReviewProvider{get: func(callCtx context.Context, account *service.Account, ref service.ProviderTaskRef) (*service.ProviderVideoTask, error) {
				calls++
				require.Equal(t, *task.AccountID, account.ID)
				require.Equal(t, "video_exact", ref.ProviderTaskID)
				require.Equal(t, task.Provider, ref.Provider)
				_, bounded := callCtx.Deadline()
				require.True(t, bounded)
				check, err := integrationDB.BeginTx(callCtx, nil)
				require.NoError(t, err)
				defer func() { _ = check.Rollback() }()
				var id int64
				require.NoError(t, check.QueryRowContext(callCtx, `SELECT id FROM video_tasks WHERE id=$1 FOR UPDATE NOWAIT`, task.ID).Scan(&id))
				require.NoError(t, check.QueryRowContext(callCtx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT`, account.ID).Scan(&id))
				for _, userID := range []int64{task.UserID, proposer, approver} {
					require.NoError(t, check.QueryRowContext(callCtx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, userID).Scan(&id))
				}
				require.NoError(t, check.Rollback())
				if scenario == "task_changed" {
					_, err = integrationDB.ExecContext(callCtx, `UPDATE video_tasks SET version=version+1 WHERE id=$1`, task.ID)
					require.NoError(t, err)
				} else if scenario == "actor_revoked" {
					_, err = integrationDB.ExecContext(callCtx, `UPDATE users SET status='disabled' WHERE id=$1`, approver)
					require.NoError(t, err)
				}
				return &service.ProviderVideoTask{ProviderTaskID: ref.ProviderTaskID, Status: service.VideoGenerationQueued, RawStatus: "queued"}, nil
			}}
			accountReader := &submissionReviewAccountReader{account: service.Account{ID: *task.AccountID, Type: service.AccountTypeAPIKey, Platform: task.Provider,
				ProviderIdentityVersion: proposed.Review.AccountIdentityVersion}}
			taskService := service.NewVideoTaskService(repo, &videoResourceRepository{db: integrationDB}, nil, accountReader, nil, nil, nil, nil,
				service.NewVideoProviderRegistry(provider), nil, repo.billing, nil, nil, &config.Config{})
			admin := service.NewVideoAdminService(adminRepo, repo, taskService, nil, nil, nil, nil)
			requestCtx := service.WithVideoAdminExpectedVersion(ctx, task.PublicID, proposed.Task.Version)
			requestCtx = service.WithVideoBillingReviewRequest(requestCtx, service.VideoBillingReviewRequest{ActorID: approver,
				OperationKey: "submission:integration", Reason: "Independently verified original submission"})
			updated, err := admin.DecideSubmissionReview(requestCtx, task.PublicID, proposed.Review.ID, true)
			if scenario == "approved" {
				require.NoError(t, err)
				require.Equal(t, service.VideoGenerationQueued, updated.GenerationState)
				_, err = admin.DecideSubmissionReview(requestCtx, task.PublicID, proposed.Review.ID, true)
				require.NoError(t, err)
			} else {
				if scenario == "actor_revoked" {
					require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
				} else {
					require.ErrorIs(t, err, service.ErrVideoVersionConflict)
				}
				current, readErr := repo.GetVideoTaskByPublicID(ctx, task.PublicID)
				require.NoError(t, readErr)
				require.Nil(t, current.ProviderTaskID)
				reviews, readErr := adminRepo.ListVideoSubmissionReviews(ctx, task.PublicID)
				require.NoError(t, readErr)
				require.Equal(t, "pending", reviews[0].Status)
			}
			require.Equal(t, 1, calls)
			assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
		})
	}
}

func TestVideoSubmissionReviewIdentityVersionUsesTypedJSONEquality(t *testing.T) {
	for _, snapshot := range []string{`1.0`, `"1"`, `null`, `2`} {
		t.Run(snapshot, func(t *testing.T) {
			ctx := context.Background()
			repo, admin, task, proposer, _ := newVideoSubmissionReviewFixture(t)
			_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET request_attributes=jsonb_set(request_attributes,'{account_identity_version}',$2::jsonb) WHERE id=$1`, task.ID, snapshot)
			require.NoError(t, err)
			task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
			require.NoError(t, err)
			proposed, err := admin.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionNotCreated))
			if snapshot == `1.0` {
				require.NoError(t, err)
				require.Equal(t, int64(1), proposed.Review.AccountIdentityVersion)
			} else {
				require.ErrorIs(t, err, service.ErrVideoReviewConflict)
			}
			assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
		})
	}
}

func TestVideoSubmissionReviewCannotClaimAcrossFrozenAccountOwner(t *testing.T) {
	ctx := context.Background()
	repo, admin, task, proposer, approver := newVideoSubmissionReviewFixture(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE video_tasks SET account_owner_user_id=$2 WHERE id=$1`, task.ID, approver)
	require.NoError(t, err)
	task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	_, err = admin.ProposeVideoSubmissionReview(ctx, task.PublicID, videoSubmissionRequest(task, proposer, service.VideoSubmissionCreated))
	require.ErrorIs(t, err, service.ErrVideoReviewForbidden)
	assertVideoBudgetTotals(t, task.UserID, 1, 995, 5)
}
