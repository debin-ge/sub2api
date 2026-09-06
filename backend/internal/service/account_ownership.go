package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AccountOwnershipShared        = "shared"
	AccountOwnershipUserDedicated = "user_dedicated"
	AccountIsolationUnverified    = "unverified"
	AccountIsolationVerified      = "verified"
	AccountIsolationRevoked       = "revoked"
)

var (
	ErrAccountIdentityInUse      = infraerrors.Conflict("ACCOUNT_IDENTITY_IN_USE", "upstream identity is retained by unsettled video tasks or available video resources; drain them or use a separate account")
	ErrAccountOwnershipDenied    = infraerrors.Forbidden("ACCOUNT_OWNERSHIP_DENIED", "account is not authorized for this user")
	ErrAccountOwnershipInvalid   = infraerrors.BadRequest("INVALID_ACCOUNT_OWNERSHIP", "dedicated accounts require one positive global owner; shared accounts cannot have an owner")
	ErrAccountOwnershipImmutable = infraerrors.BadRequest("ACCOUNT_OWNERSHIP_IMMUTABLE", "account ownership cannot be changed in place; create a new isolated upstream account")
)

type AccountSchedulingAuthorizationRepository interface {
	CanScheduleAccountForUser(context.Context, int64, int64) (bool, error)
}

type accountLookupRepository interface {
	GetByID(context.Context, int64) (*Account, error)
}

func NormalizeAccountOwnership(account *Account) error {
	if account == nil {
		return ErrAccountNilInput
	}
	if account.OwnershipMode == "" {
		account.OwnershipMode = AccountOwnershipShared
		if account.OwnerUserID != nil || account.VideoOwnerUserID != nil {
			account.OwnershipMode = AccountOwnershipUserDedicated
		}
	}
	if account.OwnerUserID == nil && account.VideoOwnerUserID != nil {
		ownerID := *account.VideoOwnerUserID
		account.OwnerUserID = &ownerID
	}
	switch account.OwnershipMode {
	case AccountOwnershipShared:
		if account.OwnerUserID != nil || account.VideoOwnerUserID != nil {
			return ErrAccountOwnershipInvalid
		}
	case AccountOwnershipUserDedicated:
		if account.OwnerUserID == nil || *account.OwnerUserID <= 0 ||
			(account.VideoOwnerUserID != nil && *account.VideoOwnerUserID != *account.OwnerUserID) {
			return ErrAccountOwnershipInvalid
		}
		ownerID := *account.OwnerUserID
		account.VideoOwnerUserID = &ownerID
	default:
		return ErrAccountOwnershipInvalid
	}
	if account.IsolationState == "" {
		account.IsolationState = AccountIsolationUnverified
	}
	if account.ProviderIdentityVersion == 0 {
		account.ProviderIdentityVersion = 1
	}
	if account.IsolationState == AccountIsolationVerified && !account.hasVerifiedDedicatedIsolation() {
		return ErrAccountProviderIdentityInvalid
	}
	return nil
}

func (account *Account) CanScheduleForUser(userID int64) bool {
	if account == nil || (account.IsolationState != "" && account.IsolationState != AccountIsolationUnverified && account.IsolationState != AccountIsolationVerified) {
		return false
	}
	if account.IsolationState == AccountIsolationVerified && !account.hasVerifiedDedicatedIsolation() {
		return false
	}
	if account.VideoOwnerUserID != nil && (*account.VideoOwnerUserID <= 0 || *account.VideoOwnerUserID != userID) {
		return false
	}
	switch account.OwnershipMode {
	case "", AccountOwnershipShared:
		return account.OwnerUserID == nil
	case AccountOwnershipUserDedicated:
		return userID > 0 && account.OwnerUserID != nil && *account.OwnerUserID == userID
	default:
		return false
	}
}

func (account *Account) hasVerifiedDedicatedIsolation() bool {
	return account != nil && !account.IsShadow() &&
		account.OwnershipMode == AccountOwnershipUserDedicated &&
		account.OwnerUserID != nil && *account.OwnerUserID > 0 &&
		account.VideoOwnerUserID != nil && *account.VideoOwnerUserID == *account.OwnerUserID &&
		account.IsolationState == AccountIsolationVerified && account.ProviderIdentityVersion > 0 &&
		account.IsolationVerifiedVersion == account.ProviderIdentityVersion &&
		account.ProviderPrincipalBindingID != nil && *account.ProviderPrincipalBindingID > 0
}

func accountSchedulingUserID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	userID, _ := ctx.Value(ctxkey.UserID).(int64)
	return userID
}

func canScheduleAccountForUser(ctx context.Context, repo accountLookupRepository, account *Account, userID int64) bool {
	if account == nil || !account.CanScheduleForUser(userID) {
		return false
	}
	if authorizer, ok := repo.(AccountSchedulingAuthorizationRepository); ok {
		allowed, err := authorizer.CanScheduleAccountForUser(ctx, account.ID, userID)
		return err == nil && allowed
	}
	if account.IsShadow() {
		parent, err := resolveCredentialAccount(ctx, repo, account)
		return err == nil && parent != nil && parent.CanScheduleForUser(userID)
	}
	return true
}

func validateAccountOwnershipUpdate(previous, current *Account) error {
	if err := NormalizeAccountOwnership(previous); err != nil {
		return err
	}
	if err := NormalizeAccountOwnership(current); err != nil {
		return err
	}
	if previous.OwnershipMode != current.OwnershipMode || !sameAccountOwner(previous.OwnerUserID, current.OwnerUserID) {
		return ErrAccountOwnershipImmutable
	}
	return nil
}

func sameAccountOwner(previous, current *int64) bool {
	if previous == nil || current == nil {
		return previous == nil && current == nil
	}
	return *previous == *current
}
