//go:build integration

package repository

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func videoIntentRequest(user *service.User, key *service.APIKey) service.VideoCreateIntentRequest {
	hash, _ := service.CanonicalVideoCreateRequestHash([]byte(`{"model":"video-alias","prompt":"example"}`))
	return service.VideoCreateIntentRequest{UserID: user.ID, APIKeyID: key.ID, Endpoint: service.CompositeRouteEndpointVideos,
		IdempotencyKey: "intent:example", RequestHash: hash, LeaseOwner: uuid.NewString(), LeaseDuration: time.Minute}
}

func attachVideoIntentGroup(t *testing.T, key *service.APIKey, platform string) *service.Group {
	t.Helper()
	group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "video-intent-" + uuid.NewString(), Platform: platform,
		Status: service.StatusActive, Hydrated: true, RateMultiplier: 1})
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE api_keys SET group_id=$1 WHERE id=$2`, group.ID, key.ID)
	require.NoError(t, err)
	key.GroupID = &group.ID
	key.Group = group
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `UPDATE api_keys SET group_id=NULL WHERE id=$1`, key.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id=$1`, group.ID)
	})
	return group
}

func TestVideoCreateIntentRequestContractCannotChangeOnRetry(t *testing.T) {
	repo, _, _, user, key, _ := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	claim, err := repo.ClaimVideoCreateIntent(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, repo.ReleasePreparedVideoCreateIntent(context.Background(), claim.Guard))
	request.RequestContract = service.VideoCreateIntentMultipartContract
	_, err = repo.ClaimVideoCreateIntent(context.Background(), request)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)
	request.IdempotencyKey = "multipart-intent"
	claim, err = repo.ClaimVideoCreateIntent(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, service.VideoCreateIntentMultipartContract, claim.Intent.RequestContract)
	request.RequestContract = service.VideoCreateIntentJSONContract
	_, err = repo.ClaimVideoCreateIntent(context.Background(), request)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)
}

func TestVideoCreateIntentNativeTaskAndHoldAreAtomic(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.True(t, claim.Owned)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-body", 4)
	_, _, err = repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoCreateInProgress)
	assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
	task, created, err := repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, claim.Guard), params)
	require.NoError(t, err)
	require.True(t, created)
	replay, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.False(t, replay.Owned)
	require.Equal(t, service.VideoCreateIntentNative, replay.Intent.State)
	require.Equal(t, task.ID, *replay.Intent.NativeTaskID)
	require.Equal(t, request.RequestHash, replay.Intent.RequestHash)
	assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
	request.RequestHash = strings.Repeat("0", 64)
	_, err = repo.ClaimVideoCreateIntent(ctx, request)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)
}

func TestVideoCreateIntentMultipartBindsNativeBeforeRouteCanChange(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	request.IdempotencyKey = "multipart-native-binding"
	request.RequestContract = service.VideoCreateIntentMultipartContract
	request.RequestHash = strings.Repeat("d", 64)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.True(t, claim.Owned)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-multipart-body", 4)
	task, created, err := repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, claim.Guard), params)
	require.NoError(t, err)
	require.True(t, created)

	request.LeaseOwner = uuid.NewString()
	replay, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.False(t, replay.Owned)
	require.Equal(t, service.VideoCreateIntentNative, replay.Intent.State)
	require.Equal(t, task.ID, *replay.Intent.NativeTaskID)

	request.RequestHash = strings.Repeat("e", 64)
	_, err = repo.ClaimVideoCreateIntent(ctx, request)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)
	request.RequestHash = strings.Repeat("d", 64)
	request.RequestContract = service.VideoCreateIntentJSONContract
	_, err = repo.ClaimVideoCreateIntent(ctx, request)
	require.ErrorIs(t, err, service.ErrVideoIdempotencyConflict)
}

func TestVideoCreateIntentNativeLegacyPathRegistersScopeWithoutChangingReplay(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), " "+request.IdempotencyKey+" ", "native-body", 4)
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.False(t, claim.Owned)
	require.Equal(t, service.VideoCreateIntentNativeContract, claim.Intent.RequestContract)
	require.Equal(t, task.ID, *claim.Intent.NativeTaskID)
	repeated, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, task.ID, repeated.ID)
	assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
}

func TestVideoCreateIntentPreparationTakeoverFencesOldWorker(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	first, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.NoError(t, repo.RenewVideoCreateIntent(ctx, first.Guard, time.Minute))
	_, err = repo.ClaimVideoCreateIntent(ctx, request)
	require.ErrorIs(t, err, service.ErrVideoCreateInProgress)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_create_intents SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, first.Intent.ID)
	require.NoError(t, err)
	request.LeaseOwner = uuid.NewString()
	second, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.Greater(t, second.Guard.LeaseEpoch, first.Guard.LeaseEpoch)
	require.ErrorIs(t, repo.RenewVideoCreateIntent(ctx, first.Guard, time.Minute), service.ErrVideoCreateFenceLost)
	require.ErrorIs(t, repo.ReleasePreparedVideoCreateIntent(ctx, first.Guard), service.ErrVideoCreateFenceLost)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-body", 4)
	_, _, err = repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, first.Guard), params)
	require.ErrorIs(t, err, service.ErrVideoCreateFenceLost)
	assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
	_, _, err = repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, second.Guard), params)
	require.NoError(t, err)
	assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
}

func TestVideoCreateIntentFailedHoldDoesNotCommitBinding(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 1)
	request := videoIntentRequest(user, key)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-body", 4)
	_, _, err = repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, claim.Guard), params)
	require.ErrorIs(t, err, service.ErrVideoInsufficientBalance)
	intent, err := scanVideoCreateIntent(integrationDB.QueryRowContext(ctx, `SELECT to_jsonb(i) FROM video_create_intents i WHERE id=$1`, claim.Intent.ID))
	require.NoError(t, err)
	require.Equal(t, service.VideoCreateIntentPrepared, intent.State)
	require.Nil(t, intent.NativeTaskID)
	require.NoError(t, repo.ReleasePreparedVideoCreateIntent(ctx, claim.Guard))
	request.LeaseOwner = uuid.NewString()
	retry, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.True(t, retry.Owned)
	assertVideoBudgetTotals(t, user.ID, 0, 1, 0)
}

func TestVideoCreateIntentConcurrentClaimHasOneOwner(t *testing.T) {
	repo, _, _, user, key, _ := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	var wait sync.WaitGroup
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			candidate := request
			candidate.LeaseOwner = uuid.NewString()
			_, err := repo.ClaimVideoCreateIntent(context.Background(), candidate)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, service.ErrVideoCreateInProgress)
		}
	}
	require.Equal(t, 1, winners)
	assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
}

func TestVideoCreateIntentQuarantineStillFencesRetriesAndRetainsOwner(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, _ := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	require.NoError(t, repo.QuarantineUntrackedVideoCreateIntent(ctx, claim.Guard))
	intent, err := repo.ReadVideoCreateIntent(ctx, claim.Guard)
	require.NoError(t, err)
	require.Equal(t, service.VideoCreateIntentUntracked, intent.State)
	require.Nil(t, intent.LeaseExpiresAt)
	_, err = repo.ClaimVideoCreateIntent(ctx, request)
	require.ErrorIs(t, err, service.ErrVideoCreateOutcomeUnknown)
	require.ErrorIs(t, repo.RenewVideoCreateIntent(ctx, claim.Guard, time.Minute), service.ErrVideoCreateFenceLost)
	require.ErrorIs(t, repo.ReleasePreparedVideoCreateIntent(ctx, claim.Guard), service.ErrVideoCreateFenceLost)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_create_intents SET state='prepared' WHERE id=$1`, intent.ID)
	require.ErrorContains(t, err, "video creation intent cannot be reopened")
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, user.ID)
	require.ErrorContains(t, err, "user is retained by unresolved video creation intents")
	assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
}

func TestVideoCreateIntentNativeBindingRemainsImmutable(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	task, created, err := repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, claim.Guard),
		videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-body", 4))
	require.NoError(t, err)
	require.True(t, created)
	for _, mutation := range []string{"state='prepared',native_task_id=NULL", "target_platform='grok'", "account_id=NULL", "request_hash=repeat('0',64)"} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE video_create_intents SET `+mutation+` WHERE id=$1`, claim.Intent.ID)
		require.Error(t, err, mutation)
	}
	second := request
	second.IdempotencyKey = "different-operation"
	second.Endpoint = service.CompositeRouteEndpointVideoEdits
	other, err := repo.ClaimVideoCreateIntent(ctx, second)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_create_intents SET state='native_bound',native_task_id=$2,
		target_platform='openai',lease_expires_at=NULL WHERE id=$1`, other.Intent.ID, task.ID)
	require.ErrorContains(t, err, "native video creation binding differs from its task")
	for _, removedState := range []string{"dispatching", "completed", "unknown", "not_created"} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO video_create_intents
			(user_id,api_key_id,endpoint,key_hash,request_hash,request_contract,state)
			VALUES($1,$2,'videos',repeat('a',64),repeat('b',64),'canonical_json_v1',$3)`, user.ID, key.ID, removedState)
		require.ErrorContains(t, err, "video_create_intents_state_check")
	}
	assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
}

func TestVideoCreateIntentExpiryDuringAccountWaitRollsBackTaskAndHold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	request := videoIntentRequest(user, key)
	request.LeaseDuration = 2 * time.Second
	claim, err := repo.ClaimVideoCreateIntent(ctx, request)
	require.NoError(t, err)
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	var blockerPID, accountID int64
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, account.ID).Scan(&accountID))
	finished := make(chan error, 1)
	go func() {
		_, _, err := repo.CreateHeldVideoTask(service.WithVideoCreateIntent(ctx, claim.Guard),
			videoCreateParams(user, key, account, service.NewVideoTaskID(), request.IdempotencyKey, "native-body", 4))
		finished <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		err := integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()>$1::timestamptz`, claim.Intent.LeaseExpiresAt).Scan(&expired)
		return err == nil && expired
	}, 4*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Commit())
	require.ErrorIs(t, <-finished, service.ErrVideoCreateFenceLost)
	assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
	intent, err := scanVideoCreateIntent(integrationDB.QueryRowContext(ctx, `SELECT to_jsonb(i) FROM video_create_intents i WHERE id=$1`, claim.Intent.ID))
	require.NoError(t, err)
	require.Equal(t, service.VideoCreateIntentPrepared, intent.State)
	require.Nil(t, intent.NativeTaskID)
}
