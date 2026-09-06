//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newAccountProviderIdentityAdmin(t *testing.T, prefix string) *service.User {
	t.Helper()
	return mustCreateUser(t, testEntClient(t), &service.User{
		Email: prefix + "-" + uuid.NewString() + "@example.test", PasswordHash: "hash",
		Role: service.RoleAdmin, Status: service.StatusActive,
	})
}

func accountProviderIdentityProposal(actorID, version int64, operationKey, issuerHash, principalHash string) service.AccountProviderIdentityProposal {
	return service.AccountProviderIdentityProposal{
		ActorID: actorID, OperationKey: operationKey, ExpectedVersion: version, Platform: service.PlatformOpenAI,
		PrincipalKind: service.AccountProviderPrincipalProject, IssuerHash: issuerHash, PrincipalHash: principalHash,
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-1",
	}
}

func TestAccountProviderIdentityReviewApprovalAndPrincipalRevocation(t *testing.T) {
	ctx := context.Background()
	videoTasks, _, _, owner, apiKey, seed := newVideoRepositoryFixture(t, 10)
	first := newOwnershipTestAccount(t, &owner.ID, map[string]any{"api_key": "identity-first-" + fmt.Sprint(seed.ID)})
	second := newOwnershipTestAccount(t, &owner.ID, map[string]any{"api_key": "identity-second-" + fmt.Sprint(seed.ID)})
	proposer := newAccountProviderIdentityAdmin(t, "identity-proposer")
	approver := newAccountProviderIdentityAdmin(t, "identity-approver")
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id IN ($1,$2)`, first.ID, second.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM video_create_intents WHERE native_task_id IN (SELECT id FROM video_tasks WHERE account_id IN ($1,$2))`, first.ID, second.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM video_task_events WHERE task_id IN (SELECT id FROM video_tasks WHERE account_id IN ($1,$2))`, first.ID, second.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM video_tasks WHERE account_id IN ($1,$2)`, first.ID, second.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id IN ($1,$2)`, first.ID, second.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `UPDATE users SET status='disabled',deleted_at=COALESCE(deleted_at,NOW()),updated_at=NOW() WHERE id IN ($1,$2)`, proposer.ID, approver.ID)
		require.NoError(t, err)
	})
	repository := NewAccountRepository(testEntClient(t), integrationDB, nil).(*accountRepository)
	issuerHash, principalHash := strings.Repeat("a", 64), strings.Repeat("b", 64)

	proposal := accountProviderIdentityProposal(proposer.ID, 1, "identity:propose:first", issuerHash, principalHash)
	created, err := repository.ProposeAccountProviderIdentity(ctx, first.ID, proposal)
	require.NoError(t, err)
	require.Len(t, created.Review.PrincipalFingerprint, 16)
	require.Equal(t, service.AccountIsolationUnverified, created.State.IsolationState)

	_, err = repository.DecideAccountProviderIdentity(ctx, first.ID, created.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: proposer.ID, OperationKey: "identity:approve:self", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.ErrorIs(t, err, service.ErrAccountProviderIdentityForbidden)
	var currentVersion int64
	var currentState string
	var currentBinding sql.NullInt64
	var factsMatch bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT a.provider_identity_version,a.isolation_state,a.provider_principal_binding_id,
		r.facts=account_provider_identity_review_facts(a) FROM accounts a JOIN account_provider_identity_reviews r ON r.account_id=a.id
		WHERE a.id=$1 AND r.id=$2`, first.ID, created.Review.ID).Scan(&currentVersion, &currentState, &currentBinding, &factsMatch))
	require.Equal(t, int64(1), currentVersion)
	require.Equal(t, service.AccountIsolationUnverified, currentState)
	require.False(t, currentBinding.Valid)
	require.True(t, factsMatch)

	approved, err := repository.DecideAccountProviderIdentity(ctx, first.ID, created.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: approver.ID, OperationKey: "identity:approve:first", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.NoError(t, err)
	require.Equal(t, service.AccountIsolationVerified, approved.State.IsolationState)
	require.NotNil(t, approved.State.Binding)
	require.Equal(t, principalHash[:16], approved.State.Binding.PrincipalFingerprint)
	_, err = integrationDB.ExecContext(ctx, `UPDATE account_provider_identity_bindings SET principal_hash=$2 WHERE id=$1`, approved.State.Binding.ID, strings.Repeat("e", 64))
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET provider_principal_binding_id=NULL WHERE id=$1`, first.ID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM account_provider_identity_review_actions WHERE review_id=$1`, created.Review.ID)
	require.Error(t, err)
	verifiedAccount, err := repository.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, verifiedAccount.ProviderPrincipalBindingID)
	params := videoCreateParams(owner, apiKey, verifiedAccount, service.NewVideoTaskID(), "identity-verified-task-"+uuid.NewString(), "identity-verified-hash", 1)
	params.AccountOwnerUserID = &owner.ID
	params.RequestAttributes["requires_verified_isolation"] = true
	params.RequestAttributes["account_identity_version"] = verifiedAccount.ProviderIdentityVersion
	task, createdTask, err := videoTasks.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, createdTask)

	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET video_disclosure_policy='dedicated_credentials' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	cfg := &config.Config{}
	cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureDedicatedCredentials
	svc := service.NewVideoTaskService(videoTasks, nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg)
	disclosure, err := svc.DisclosureForOwner(ctx, owner.ID, task.PublicID)
	require.NoError(t, err)
	require.NotNil(t, disclosure.Access)

	// A later paused shared copy must invalidate provider-wide disclosure even
	// though the original binding and identity version have not changed.
	sharedAlias := newOwnershipTestAccount(t, nil, first.Credentials)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET schedulable=false WHERE id=$1`, sharedAlias.ID)
	require.NoError(t, err)
	stillVerified, err := repository.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountIsolationVerified, stillVerified.IsolationState)
	disclosure, err = svc.DisclosureForOwner(ctx, owner.ID, task.PublicID)
	require.NoError(t, err)
	require.Nil(t, disclosure.Access)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, sharedAlias.ID)
	require.NoError(t, err)
	disclosure, err = svc.DisclosureForOwner(ctx, owner.ID, task.PublicID)
	require.NoError(t, err)
	require.NotNil(t, disclosure.Access)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM video_create_intents WHERE native_task_id IN (SELECT id FROM video_tasks WHERE account_id=$1)`, first.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM video_task_events WHERE task_id IN (SELECT id FROM video_tasks WHERE account_id=$1)`, first.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM video_tasks WHERE account_id=$1`, first.ID)
	require.NoError(t, err)

	replayed, err := repository.DecideAccountProviderIdentity(ctx, first.ID, created.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: approver.ID, OperationKey: "identity:approve:first", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.NoError(t, err)
	require.True(t, replayed.Replayed)

	otherOwner := mustCreateUser(t, testEntClient(t), &service.User{
		Email: "identity-other-owner-" + uuid.NewString() + "@example.test", PasswordHash: "hash", Status: service.StatusActive,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `UPDATE users SET status='disabled',deleted_at=COALESCE(deleted_at,NOW()),updated_at=NOW() WHERE id=$1`, otherOwner.ID)
		require.NoError(t, err)
	})
	foreignAlias := newOwnershipTestAccount(t, &otherOwner.ID, map[string]any{"api_key": "identity-foreign-" + fmt.Sprint(seed.ID)})
	foreignProposal, err := repository.ProposeAccountProviderIdentity(ctx, foreignAlias.ID,
		accountProviderIdentityProposal(proposer.ID, 1, "identity:propose:foreign", issuerHash, principalHash))
	require.NoError(t, err)
	_, err = repository.DecideAccountProviderIdentity(ctx, foreignAlias.ID, foreignProposal.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: approver.ID, OperationKey: "identity:approve:foreign", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.ErrorIs(t, err, service.ErrAccountProviderIdentityConflict)

	secondProposal, err := repository.ProposeAccountProviderIdentity(ctx, second.ID,
		accountProviderIdentityProposal(proposer.ID, 1, "identity:propose:second", issuerHash, principalHash))
	require.NoError(t, err)
	_, err = repository.DecideAccountProviderIdentity(ctx, second.ID, secondProposal.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: approver.ID, OperationKey: "identity:approve:second", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.NoError(t, err)

	revoked, err := repository.RevokeAccountProviderIdentity(ctx, first.ID, service.AccountProviderIdentityRevocation{
		ActorID: approver.ID, OperationKey: "identity:revoke:principal", Reason: "Provider principal was compromised",
		EvidenceRef: "incident:IDENTITY-1",
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{first.ID, second.ID}, revoked.AffectedAccountIDs)
	for _, accountID := range []int64{first.ID, second.ID} {
		var state string
		var schedulable bool
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT isolation_state,schedulable FROM accounts WHERE id=$1`, accountID).Scan(&state, &schedulable))
		require.Equal(t, service.AccountIsolationRevoked, state)
		require.False(t, schedulable)
	}
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials='{"api_key":"rotated-after-revocation"}'::jsonb WHERE id=$1`, first.ID)
	require.NoError(t, err)
	revocationReplay, err := repository.RevokeAccountProviderIdentity(ctx, first.ID, service.AccountProviderIdentityRevocation{
		ActorID: approver.ID, OperationKey: "identity:revoke:principal", Reason: "Provider principal was compromised",
		EvidenceRef: "incident:IDENTITY-1",
	})
	require.NoError(t, err)
	require.True(t, revocationReplay.Replayed)
}

func TestAccountProviderIdentityRejectsSharedCredentialAlias(t *testing.T) {
	ctx := context.Background()
	_, _, _, owner, _, seed := newVideoRepositoryFixture(t, 10)
	credential := "identity-shared-alias-" + fmt.Sprint(seed.ID)
	dedicated := newOwnershipTestAccount(t, &owner.ID, map[string]any{"api_key": credential})
	shared := newOwnershipTestAccount(t, nil, map[string]any{"api_key": credential})
	proposer := newAccountProviderIdentityAdmin(t, "identity-alias-proposer")
	approver := newAccountProviderIdentityAdmin(t, "identity-alias-approver")
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id IN ($1,$2)`, dedicated.ID, shared.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id IN ($1,$2)`, dedicated.ID, shared.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `UPDATE users SET status='disabled',deleted_at=COALESCE(deleted_at,NOW()),updated_at=NOW() WHERE id IN ($1,$2)`, proposer.ID, approver.ID)
		require.NoError(t, err)
	})
	repository := NewAccountRepository(testEntClient(t), integrationDB, nil).(*accountRepository)
	issuerHash, principalHash := strings.Repeat("c", 64), strings.Repeat("d", 64)

	created, err := repository.ProposeAccountProviderIdentity(ctx, dedicated.ID,
		accountProviderIdentityProposal(proposer.ID, 1, "identity:alias:propose", issuerHash, principalHash))
	require.NoError(t, err)
	_, err = repository.DecideAccountProviderIdentity(ctx, dedicated.ID, created.Review.ID, service.AccountProviderIdentityDecision{
		ActorID: approver.ID, OperationKey: "identity:alias:approve", ExpectedVersion: 1, Approve: true,
		Reason: "Independent verification completed",
	})
	require.ErrorIs(t, err, service.ErrAccountProviderIdentityConflict)

	authorizer := repository
	for _, accountID := range []int64{dedicated.ID, shared.ID} {
		allowed, authorizeErr := authorizer.CanScheduleAccountForUser(ctx, accountID, owner.ID)
		require.NoError(t, authorizeErr)
		require.False(t, allowed)
	}
}
