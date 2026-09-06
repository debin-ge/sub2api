package service

import (
	"context"
	"strings"
)

func (s *adminServiceImpl) accountProviderIdentityRepository() (AccountProviderIdentityRepository, error) {
	repository, ok := s.accountRepo.(AccountProviderIdentityRepository)
	if !ok || repository == nil {
		return nil, ErrAccountProviderIdentityInvalid
	}
	return repository, nil
}

func (s *adminServiceImpl) GetAccountProviderIdentity(ctx context.Context, accountID int64) (*AccountProviderIdentityState, error) {
	if accountID <= 0 {
		return nil, ErrAccountProviderIdentityInvalid
	}
	repository, err := s.accountProviderIdentityRepository()
	if err != nil {
		return nil, err
	}
	return repository.GetAccountProviderIdentity(ctx, accountID)
}

func (s *adminServiceImpl) ProposeAccountProviderIdentity(ctx context.Context, accountID int64, request AccountProviderIdentityProposal) (*AccountProviderIdentityResult, error) {
	if accountID <= 0 {
		return nil, ErrAccountProviderIdentityInvalid
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	request, err = prepareAccountProviderIdentityProposal(account, request)
	if err != nil {
		return nil, err
	}
	repository, err := s.accountProviderIdentityRepository()
	if err != nil {
		return nil, err
	}
	return repository.ProposeAccountProviderIdentity(ctx, accountID, request)
}

func (s *adminServiceImpl) DecideAccountProviderIdentity(ctx context.Context, accountID, reviewID int64, decision AccountProviderIdentityDecision) (*AccountProviderIdentityResult, error) {
	if accountID <= 0 || reviewID <= 0 {
		return nil, ErrAccountProviderIdentityInvalid
	}
	decision.OperationKey = strings.TrimSpace(decision.OperationKey)
	decision.Reason = strings.TrimSpace(decision.Reason)
	if err := validateAccountProviderIdentityDecision(decision); err != nil {
		return nil, err
	}
	repository, err := s.accountProviderIdentityRepository()
	if err != nil {
		return nil, err
	}
	return repository.DecideAccountProviderIdentity(ctx, accountID, reviewID, decision)
}

func (s *adminServiceImpl) RevokeAccountProviderIdentity(ctx context.Context, accountID int64, request AccountProviderIdentityRevocation) (*AccountProviderIdentityResult, error) {
	if accountID <= 0 {
		return nil, ErrAccountProviderIdentityInvalid
	}
	request.OperationKey = strings.TrimSpace(request.OperationKey)
	request.Reason = strings.TrimSpace(request.Reason)
	request.EvidenceRef = strings.TrimSpace(request.EvidenceRef)
	if err := validateAccountProviderIdentityRevocation(request); err != nil {
		return nil, err
	}
	repository, err := s.accountProviderIdentityRepository()
	if err != nil {
		return nil, err
	}
	return repository.RevokeAccountProviderIdentity(ctx, accountID, request)
}
