//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountMutationReceiptRepo struct {
	accountRepoStub
	account           *Account
	getByIDErr        error
	bindErr           error
	shadows           []*Account
	updateErrByID     map[int64]error
	deleteErrByID     map[int64]error
	updatedIDs        []int64
	deletedIDs        []int64
	clearErrorErr     error
	clearRateLimitErr error
	setSchedulableErr error
}

func (s *accountMutationReceiptRepo) Create(_ context.Context, account *Account) error {
	account.ID = 10
	return nil
}

func (s *accountMutationReceiptRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	account := *s.account
	return &account, nil
}

func (s *accountMutationReceiptRepo) Update(_ context.Context, account *Account) error {
	if err := s.updateErrByID[account.ID]; err != nil {
		return err
	}
	s.updatedIDs = append(s.updatedIDs, account.ID)
	return nil
}

func (s *accountMutationReceiptRepo) Delete(_ context.Context, id int64) error {
	if err := s.deleteErrByID[id]; err != nil {
		return err
	}
	s.deletedIDs = append(s.deletedIDs, id)
	return nil
}

func (s *accountMutationReceiptRepo) BindGroups(_ context.Context, _ int64, _ []int64) error {
	return s.bindErr
}

func (s *accountMutationReceiptRepo) ListShadowsByParent(_ context.Context, _ int64) ([]*Account, error) {
	return s.shadows, nil
}

func (s *accountMutationReceiptRepo) ClearError(context.Context, int64) error {
	return s.clearErrorErr
}

func (s *accountMutationReceiptRepo) ClearRateLimit(context.Context, int64) error {
	return s.clearRateLimitErr
}

func (s *accountMutationReceiptRepo) SetSchedulable(context.Context, int64, bool) error {
	return s.setSchedulableErr
}

func TestAdminServiceCreateAccount_PostCreateFailureCarriesMutationID(t *testing.T) {
	bindErr := errors.New("bind failed")
	repo := &accountMutationReceiptRepo{bindErr: bindErr}
	svc := &adminServiceImpl{accountRepo: repo}
	groupIDs := []int64{20}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name: "created", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		GroupIDs: groupIDs, SkipMixedChannelCheck: true,
	})

	require.ErrorIs(t, err, bindErr)
	require.Equal(t, []int64{10}, AccountMutationIDsFromError(err))
}

func TestAdminServiceUpdateAccount_PostUpdateShadowFailureCarriesAllMutationIDs(t *testing.T) {
	shadowErr := errors.New("shadow update failed")
	repo := &accountMutationReceiptRepo{
		account:       &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive},
		shadows:       []*Account{{ID: 2}, {ID: 3}},
		updateErrByID: map[int64]error{3: shadowErr},
	}
	svc := &adminServiceImpl{accountRepo: repo}
	proxyID := int64(9)

	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{ProxyID: &proxyID})

	require.ErrorIs(t, err, shadowErr)
	require.Equal(t, []int64{1, 2}, AccountMutationIDsFromError(err))
}

func TestAdminServiceClearAndSet_PostWriteFailuresCarryMutationID(t *testing.T) {
	t.Run("clear account error", func(t *testing.T) {
		clearErr := errors.New("clear rate limit failed")
		repo := &accountMutationReceiptRepo{clearRateLimitErr: clearErr}
		svc := &adminServiceImpl{accountRepo: repo}

		_, err := svc.ClearAccountError(context.Background(), 4)

		require.ErrorIs(t, err, clearErr)
		require.Equal(t, []int64{4}, AccountMutationIDsFromError(err))
	})

	t.Run("set schedulable", func(t *testing.T) {
		reloadErr := errors.New("reload failed")
		repo := &accountMutationReceiptRepo{getByIDErr: reloadErr}
		svc := &adminServiceImpl{accountRepo: repo}

		_, err := svc.SetAccountSchedulable(context.Background(), 5, true)

		require.ErrorIs(t, err, reloadErr)
		require.Equal(t, []int64{5}, AccountMutationIDsFromError(err))
	})
}

func TestAdminServiceClearAndSet_NotFoundDoesNotCarryMutationID(t *testing.T) {
	t.Run("clear account error", func(t *testing.T) {
		repo := &accountMutationReceiptRepo{clearErrorErr: ErrAccountNotFound}
		svc := &adminServiceImpl{accountRepo: repo}

		_, err := svc.ClearAccountError(context.Background(), 40)

		require.ErrorIs(t, err, ErrAccountNotFound)
		require.Empty(t, AccountMutationIDsFromError(err))
	})

	t.Run("set schedulable", func(t *testing.T) {
		repo := &accountMutationReceiptRepo{setSchedulableErr: ErrAccountNotFound}
		svc := &adminServiceImpl{accountRepo: repo}

		_, err := svc.SetAccountSchedulable(context.Background(), 50, true)

		require.ErrorIs(t, err, ErrAccountNotFound)
		require.Empty(t, AccountMutationIDsFromError(err))
	})
}

func TestAdminServiceDeleteAccount_PartialCascadeFailureCarriesDeletedIDs(t *testing.T) {
	deleteErr := errors.New("delete failed")
	repo := &accountMutationReceiptRepo{
		shadows:       []*Account{{ID: 6}, {ID: 7}},
		deleteErrByID: map[int64]error{7: deleteErr},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	err := svc.DeleteAccount(context.Background(), 8)

	require.ErrorIs(t, err, deleteErr)
	require.Equal(t, []int64{6}, AccountMutationIDsFromError(err))
}

func TestWithAccountMutationIDs_MergesDeduplicatesAndClones(t *testing.T) {
	cause := errors.New("post-write failure")
	inner := &AccountMutationError{Cause: cause, MutatedAccountIDs: []int64{2, 3}}

	err := WithAccountMutationIDs(inner, 1, 2)

	require.ErrorIs(t, err, cause)
	require.Equal(t, []int64{1, 2, 3}, AccountMutationIDsFromError(err))
	ids := AccountMutationIDsFromError(err)
	ids[0] = 99
	require.Equal(t, []int64{1, 2, 3}, AccountMutationIDsFromError(err))
}
