package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestAccountOwnershipCompatibilityAndFailClosed(t *testing.T) {
	ownerID := int64(42)
	legacy := &Account{VideoOwnerUserID: &ownerID}
	require.False(t, legacy.CanScheduleForUser(0))
	require.False(t, legacy.CanScheduleForUser(99))
	require.True(t, legacy.CanScheduleForUser(ownerID))
	require.NoError(t, NormalizeAccountOwnership(legacy))
	require.Equal(t, AccountOwnershipUserDedicated, legacy.OwnershipMode)
	require.Equal(t, ownerID, *legacy.OwnerUserID)
	require.Equal(t, AccountIsolationUnverified, legacy.IsolationState)
	require.Equal(t, int64(1), legacy.ProviderIdentityVersion)

	for _, invalid := range []*Account{
		{OwnershipMode: AccountOwnershipShared, OwnerUserID: &ownerID},
		{OwnershipMode: AccountOwnershipUserDedicated},
		{OwnershipMode: "future_mode"},
		{OwnershipMode: AccountOwnershipUserDedicated, OwnerUserID: &ownerID, VideoOwnerUserID: videoInt64Ptr(99)},
	} {
		require.ErrorIs(t, NormalizeAccountOwnership(invalid), ErrAccountOwnershipInvalid)
	}
	legacy.IsolationState = AccountIsolationRevoked
	require.False(t, legacy.CanScheduleForUser(ownerID))
	legacy.IsolationState = "unknown"
	require.False(t, legacy.CanScheduleForUser(ownerID))
}

func TestAccountOwnershipImmutableEvenWithoutUsage(t *testing.T) {
	ownerID := int64(42)
	shared := &Account{}
	dedicated := &Account{VideoOwnerUserID: &ownerID}
	require.ErrorIs(t, validateAccountOwnershipUpdate(shared, dedicated), ErrAccountOwnershipImmutable)
	differentOwner := &Account{VideoOwnerUserID: videoInt64Ptr(99)}
	require.ErrorIs(t, validateAccountOwnershipUpdate(dedicated, differentOwner), ErrAccountOwnershipImmutable)
	copy := *dedicated
	copy.Name = "new name"
	require.NoError(t, validateAccountOwnershipUpdate(dedicated, &copy))
}

type ownershipAuthorizationStub struct {
	videoAccountRepoStub
	allowed bool
	err     error
	calls   int
}

func (repo *ownershipAuthorizationStub) CanScheduleAccountForUser(context.Context, int64, int64) (bool, error) {
	repo.calls++
	return repo.allowed, repo.err
}

func TestAccountOwnershipRechecksSharedCacheAndFailsClosedOnDatabaseError(t *testing.T) {
	account := &Account{ID: 11, OwnershipMode: AccountOwnershipShared}
	repo := &ownershipAuthorizationStub{}
	require.False(t, canScheduleAccountForUser(context.Background(), repo, account, 42))
	repo.allowed = true
	repo.err = errors.New("authorization storage unavailable")
	require.False(t, canScheduleAccountForUser(context.Background(), repo, account, 42))
	repo.err = nil
	require.True(t, canScheduleAccountForUser(context.Background(), repo, account, 42))
	require.Equal(t, 3, repo.calls)
}

func TestAccountOwnershipShadowUsesCredentialParent(t *testing.T) {
	ownerID := int64(42)
	parent := Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, VideoOwnerUserID: &ownerID}
	shadow := Account{ID: 11, ParentAccountID: &parent.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	repo := &videoAccountRepoStub{accounts: []Account{parent, shadow}}
	require.True(t, canScheduleAccountForUser(context.Background(), repo, &shadow, ownerID))
	require.False(t, canScheduleAccountForUser(context.Background(), repo, &shadow, 99))
	require.False(t, canScheduleAccountForUser(context.Background(), repo, &shadow, 0))
}

func TestAccountOwnershipTerminalChecksAreIndependentOfProfitGate(t *testing.T) {
	ownerID := int64(42)
	account := &Account{ID: 11, VideoOwnerUserID: &ownerID, Status: StatusActive, Schedulable: true}
	openai := &OpenAIGatewayService{}
	gateway := &GatewayService{}
	for _, userID := range []int64{0, 99, ownerID} {
		ctx := context.WithValue(context.Background(), ctxkey.UserID, userID)
		_, deniedOpenAI, _ := openai.ProfitControlVetoLatest(ctx, account)
		_, deniedGateway, _ := gateway.GatewayProfitControlVetoLatest(ctx, account)
		require.Equal(t, userID != ownerID, deniedOpenAI)
		require.Equal(t, userID != ownerID, deniedGateway)
		require.Equal(t, userID == ownerID, account.IsSchedulableForModelWithContext(ctx, ""))
	}
}

func TestDedicatedCredentialRequiresVerifiedFrozenIdentity(t *testing.T) {
	ownerID, accountID := int64(42), int64(11)
	bindingID := int64(7)
	account := Account{
		ID: accountID, Type: AccountTypeAPIKey, VideoOwnerUserID: &ownerID,
		VideoDisclosurePolicy: config.VideoDisclosureDedicatedCredentials,
		OwnershipMode:         AccountOwnershipUserDedicated, OwnerUserID: &ownerID,
		IsolationState: AccountIsolationVerified, ProviderIdentityVersion: 2, IsolationVerifiedVersion: 2,
		ProviderPrincipalBindingID: &bindingID,
		Credentials:                map[string]any{"api_key": "synthetic-test-secret"},
	}
	task := VideoTask{UserID: ownerID, AccountID: &accountID, AccountOwnerUserID: &ownerID,
		RequestAttributes: map[string]any{"account_identity_version": float64(2)}}
	require.NotNil(t, dedicatedVideoCredentialForOwner(&task, &account, ownerID))
	for _, mutate := range []func(*Account, *VideoTask){
		func(account *Account, task *VideoTask) { account.OwnershipMode = ""; account.OwnerUserID = nil },
		func(account *Account, task *VideoTask) { account.IsolationState = AccountIsolationUnverified },
		func(account *Account, task *VideoTask) { account.IsolationState = AccountIsolationRevoked },
		func(account *Account, task *VideoTask) { account.IsolationVerifiedVersion = 1 },
		func(account *Account, task *VideoTask) { account.ProviderPrincipalBindingID = nil },
		func(account *Account, task *VideoTask) {
			account.ProviderIdentityVersion = 3
			account.IsolationVerifiedVersion = 3
		},
		func(account *Account, task *VideoTask) { task.RequestAttributes = nil },
		func(account *Account, task *VideoTask) { task.UserID = 99 },
		func(account *Account, task *VideoTask) { account.ParentAccountID = videoInt64Ptr(10) },
	} {
		changedAccount, changedTask := account, task
		mutate(&changedAccount, &changedTask)
		require.Nil(t, dedicatedVideoCredentialForOwner(&changedTask, &changedAccount, ownerID))
	}
}
