package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountProviderIdentityRepositoryStub struct {
	AccountRepository
	account  *Account
	proposal AccountProviderIdentityProposal
}

func (stub *accountProviderIdentityRepositoryStub) GetByID(context.Context, int64) (*Account, error) {
	return stub.account, nil
}

func (stub *accountProviderIdentityRepositoryStub) GetAccountProviderIdentity(context.Context, int64) (*AccountProviderIdentityState, error) {
	return &AccountProviderIdentityState{AccountID: stub.account.ID, IdentityVersion: stub.account.ProviderIdentityVersion, IsolationState: stub.account.IsolationState}, nil
}

func (stub *accountProviderIdentityRepositoryStub) ProposeAccountProviderIdentity(_ context.Context, _ int64, request AccountProviderIdentityProposal) (*AccountProviderIdentityResult, error) {
	stub.proposal = request
	return &AccountProviderIdentityResult{State: &AccountProviderIdentityState{AccountID: stub.account.ID, IdentityVersion: stub.account.ProviderIdentityVersion, IsolationState: stub.account.IsolationState}}, nil
}

func (stub *accountProviderIdentityRepositoryStub) DecideAccountProviderIdentity(context.Context, int64, int64, AccountProviderIdentityDecision) (*AccountProviderIdentityResult, error) {
	return nil, ErrAccountProviderIdentityInvalid
}

func (stub *accountProviderIdentityRepositoryStub) RevokeAccountProviderIdentity(context.Context, int64, AccountProviderIdentityRevocation) (*AccountProviderIdentityResult, error) {
	return nil, ErrAccountProviderIdentityInvalid
}

func TestPrepareAccountProviderIdentityProposalHashesAndClearsPrincipal(t *testing.T) {
	ownerID := int64(42)
	account := &Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials:   map[string]any{"base_url": "HTTPS://API.OPENAI.COM:443/v1/"},
		OwnershipMode: AccountOwnershipUserDedicated, OwnerUserID: &ownerID,
		ProviderIdentityVersion: 3, IsolationState: AccountIsolationUnverified,
	}
	prepared, err := prepareAccountProviderIdentityProposal(account, AccountProviderIdentityProposal{
		ActorID: 7, OperationKey: "provider-identity:test", ExpectedVersion: 3,
		PrincipalKind: AccountProviderPrincipalProject, Principal: "proj_abc-123",
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-1",
	})
	require.NoError(t, err)
	require.Empty(t, prepared.Principal)
	require.Equal(t, PlatformOpenAI, prepared.Platform)
	require.Regexp(t, `^[0-9a-f]{64}$`, prepared.IssuerHash)
	require.Regexp(t, `^[0-9a-f]{64}$`, prepared.PrincipalHash)

	same, err := prepareAccountProviderIdentityProposal(account, AccountProviderIdentityProposal{
		ActorID: 7, OperationKey: "provider-identity:test-2", ExpectedVersion: 3,
		PrincipalKind: AccountProviderPrincipalProject, Principal: "proj_abc-123",
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-2",
	})
	require.NoError(t, err)
	require.Equal(t, prepared.IssuerHash, same.IssuerHash)
	require.Equal(t, prepared.PrincipalHash, same.PrincipalHash)
	account.Credentials["base_url"] = "https://api.openai.com/%76%31"
	encodedPath, err := prepareAccountProviderIdentityProposal(account, AccountProviderIdentityProposal{
		ActorID: 7, OperationKey: "provider-identity:test-3", ExpectedVersion: 3,
		PrincipalKind: AccountProviderPrincipalProject, Principal: "proj_abc-123",
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-3",
	})
	require.NoError(t, err)
	require.Equal(t, prepared.IssuerHash, encodedPath.IssuerHash)
}

func TestPrepareAccountProviderIdentityProposalFailsClosed(t *testing.T) {
	ownerID := int64(42)
	base := &Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		OwnershipMode: AccountOwnershipUserDedicated, OwnerUserID: &ownerID,
		ProviderIdentityVersion: 3, IsolationState: AccountIsolationUnverified,
	}
	request := AccountProviderIdentityProposal{
		ActorID: 7, OperationKey: "provider-identity:test", ExpectedVersion: 3,
		PrincipalKind: AccountProviderPrincipalProject, Principal: "proj_abc-123",
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-1",
	}
	for name, mutate := range map[string]func(*Account, *AccountProviderIdentityProposal){
		"shared account": func(account *Account, _ *AccountProviderIdentityProposal) {
			account.OwnershipMode, account.OwnerUserID = AccountOwnershipShared, nil
		},
		"revoked account": func(account *Account, _ *AccountProviderIdentityProposal) {
			account.IsolationState = AccountIsolationRevoked
		},
		"stale version": func(_ *Account, request *AccountProviderIdentityProposal) { request.ExpectedVersion = 2 },
		"secret principal": func(_ *Account, request *AccountProviderIdentityProposal) {
			request.Principal = "sk-provider-secret-12345678"
		},
		"jwt principal": func(_ *Account, request *AccountProviderIdentityProposal) {
			request.Principal = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature"
		},
		"url principal": func(_ *Account, request *AccountProviderIdentityProposal) {
			request.Principal = "https://signed.example.test/value"
		},
		"spaced principal": func(_ *Account, request *AccountProviderIdentityProposal) { request.Principal = "project value" },
		"invalid issuer": func(account *Account, _ *AccountProviderIdentityProposal) {
			account.Credentials = map[string]any{"base_url": "https://user:pass@example.test"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			accountCopy, requestCopy := *base, request
			mutate(&accountCopy, &requestCopy)
			_, err := prepareAccountProviderIdentityProposal(&accountCopy, requestCopy)
			require.Error(t, err)
		})
	}
}

func TestAdminAccountProviderIdentityNeverPassesRawPrincipalToRepository(t *testing.T) {
	ownerID := int64(42)
	repository := &accountProviderIdentityRepositoryStub{account: &Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		OwnershipMode: AccountOwnershipUserDedicated, OwnerUserID: &ownerID,
		ProviderIdentityVersion: 3, IsolationState: AccountIsolationUnverified,
	}}
	admin := &adminServiceImpl{accountRepo: repository}
	_, err := admin.ProposeAccountProviderIdentity(context.Background(), 11, AccountProviderIdentityProposal{
		ActorID: 7, OperationKey: "provider-identity:service", ExpectedVersion: 3,
		PrincipalKind: AccountProviderPrincipalProject, Principal: "proj_sensitive-123",
		Reason: "Provider console identity verified", EvidenceRef: "ticket:IDENTITY-1",
	})
	require.NoError(t, err)
	require.Empty(t, repository.proposal.Principal)
	require.Equal(t, PlatformOpenAI, repository.proposal.Platform)
	require.Regexp(t, `^[0-9a-f]{64}$`, repository.proposal.PrincipalHash)
	require.NotContains(t, repository.proposal.PrincipalHash, "proj_sensitive-123")
}
