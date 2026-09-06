//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newOwnershipTestAccount(t *testing.T, ownerID *int64, credentials map[string]any) *dbent.Account {
	t.Helper()
	create := testEntClient(t).Account.Create().SetName("ownership-regression").
		SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(credentials)
	if ownerID != nil {
		create.SetOwnershipMode(service.AccountOwnershipUserDedicated).SetOwnerUserID(*ownerID)
	}
	account, err := create.Save(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, account.ID)
		require.NoError(t, err)
	})
	loaded, err := testEntClient(t).Account.Get(context.Background(), account.ID)
	require.NoError(t, err)
	return loaded
}

func TestAccountOwnershipGlobalAliasesAndShadowAuthorization(t *testing.T) {
	ctx := context.Background()
	_, _, _, user, _, shared := newVideoRepositoryFixture(t, 10)
	credentials := map[string]any{"api_key": fmt.Sprintf("synthetic-isolation-%d", shared.ID), "project_id": fmt.Sprintf("test-project-%d", shared.ID)}
	dedicated := newOwnershipTestAccount(t, &user.ID, credentials)
	alias := newOwnershipTestAccount(t, nil, credentials)
	projectAlias := newOwnershipTestAccount(t, nil, map[string]any{"api_key": "different-synthetic-key", "project_id": credentials["project_id"]})
	crossPlatformAlias := newOwnershipTestAccount(t, nil, credentials)
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET platform = 'anthropic' WHERE id = $1`, crossPlatformAlias.ID)
	require.NoError(t, err)
	authorizer := NewAccountRepository(testEntClient(t), integrationDB, nil).(service.AccountSchedulingAuthorizationRepository)
	for _, accountID := range []int64{dedicated.ID, alias.ID, projectAlias.ID, crossPlatformAlias.ID} {
		for _, userID := range []int64{user.ID, user.ID + 999, 0} {
			allowed, err := authorizer.CanScheduleAccountForUser(ctx, accountID, userID)
			require.NoError(t, err)
			require.False(t, allowed)
		}
	}
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET type = 'oauth' WHERE id = $1`, dedicated.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET parent_account_id = $1, type = 'oauth', quota_dimension = 'spark' WHERE id = $2`, dedicated.ID, alias.ID)
	require.NoError(t, err)
	allowed, err := authorizer.CanScheduleAccountForUser(ctx, alias.ID, user.ID+999)
	require.NoError(t, err)
	require.False(t, allowed)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET isolation_state = 'revoked' WHERE id = $1`, dedicated.ID)
	require.NoError(t, err)
	allowed, err = authorizer.CanScheduleAccountForUser(ctx, alias.ID, user.ID)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestAccountOwnershipCannotRelabelOrForgeVerification(t *testing.T) {
	ctx := context.Background()
	_, _, _, user, _, shared := newVideoRepositoryFixture(t, 10)
	dedicated := newOwnershipTestAccount(t, &user.ID, map[string]any{"api_key": fmt.Sprintf("synthetic-version-%d", shared.ID)})
	require.Equal(t, user.ID, *dedicated.VideoOwnerUserID)
	require.Equal(t, service.AccountIsolationUnverified, dedicated.IsolationState)
	for _, command := range []string{
		`UPDATE accounts SET ownership_mode = 'user_dedicated', owner_user_id = $2, video_owner_user_id = $2 WHERE id = $1`,
		`UPDATE accounts SET isolation_state = 'verified', isolation_verified_version = provider_identity_version WHERE id = $1`,
	} {
		var err error
		if command == `UPDATE accounts SET isolation_state = 'verified', isolation_verified_version = provider_identity_version WHERE id = $1` {
			_, err = integrationDB.ExecContext(ctx, command, dedicated.ID)
		} else {
			_, err = integrationDB.ExecContext(ctx, command, shared.ID, user.ID)
		}
		require.Error(t, err)
	}
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET owner_user_id = NULL, video_owner_user_id = NULL, ownership_mode = 'shared' WHERE id = $1`, dedicated.ID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET isolation_state = 'revoked' WHERE id = $1`, dedicated.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = '{"api_key":"synthetic-rotated"}'::jsonb WHERE id = $1`, dedicated.ID)
	require.NoError(t, err)
	var version, verifiedVersion int64
	var state string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT provider_identity_version, isolation_verified_version, isolation_state FROM accounts WHERE id = $1`, dedicated.ID).Scan(&version, &verifiedVersion, &state))
	require.Equal(t, int64(2), version)
	require.Zero(t, verifiedVersion)
	require.Equal(t, service.AccountIsolationRevoked, state)
}

func TestVideoOwnershipHoldRechecksIdentityAndKeepsBoundTaskReadable(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 10)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "ownership-version", "ownership-version-hash", 2)
	params.RequestAttributes = map[string]any{"account_identity_version": 1}
	params.RequestAttributes["requires_verified_isolation"] = true
	_, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoNoAccountAvailable)
	delete(params.RequestAttributes, "requires_verified_isolation")
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = '{"api_key":"synthetic-before-submit"}'::jsonb WHERE id = $1`, account.ID)
	require.NoError(t, err)
	_, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoNoAccountAvailable)
	require.False(t, created)
	params.RequestAttributes["account_identity_version"] = 2
	task, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, created)
	accountRepo := NewAccountRepository(testEntClient(t), integrationDB, nil).(*accountRepository)
	err = accountRepo.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "synthetic-different-principal"})
	require.ErrorIs(t, err, service.ErrAccountIdentityInUse)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = credentials || '{"model_mapping":{"public-model":"upstream-model"}}'::jsonb WHERE id = $1`, account.ID)
	require.NoError(t, err)
	var currentVersion int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT provider_identity_version FROM accounts WHERE id = $1`, account.ID).Scan(&currentVersion))
	require.Equal(t, int64(2), currentVersion)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET isolation_state = 'revoked' WHERE id = $1`, account.ID)
	require.NoError(t, err)
	replayed, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, task.ID, replayed.ID)
	_, err = repo.GetVideoTaskForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	params.PublicID, params.IdempotencyKey = service.NewVideoTaskID(), "ownership-revoked"
	_, _, err = repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoNoAccountAvailable)
}
