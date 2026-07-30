package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type VIPEntitlementService struct {
	repository           VIPEntitlementRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

func NewVIPEntitlementService(
	repository VIPEntitlementRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *VIPEntitlementService {
	return &VIPEntitlementService{
		repository:           repository,
		authCacheInvalidator: authCacheInvalidator,
	}
}

func (s *VIPEntitlementService) GrantPaidEligibility(
	ctx context.Context,
	userID, orderID int64,
	completedAt time.Time,
	source VIPPaidSource,
) (VIPMutationResult, error) {
	if s == nil || s.repository == nil {
		return VIPMutationResult{}, fmt.Errorf("VIP entitlement repository is not configured")
	}
	if userID <= 0 {
		return VIPMutationResult{}, fmt.Errorf("user ID must be positive")
	}
	if orderID <= 0 {
		return VIPMutationResult{}, fmt.Errorf("order ID must be positive")
	}
	if completedAt.IsZero() {
		return VIPMutationResult{}, fmt.Errorf("completed time is required")
	}
	if !IsValidVIPPaidSource(source) {
		return VIPMutationResult{}, fmt.Errorf("invalid VIP paid source %q", source)
	}

	result, err := s.repository.ApplyPaidEligibility(
		ctx,
		userID,
		orderID,
		completedAt.UTC(),
		source,
	)
	if err != nil {
		return VIPMutationResult{}, err
	}
	s.invalidateEffectiveChange(ctx, userID, result)
	return result, nil
}

func (s *VIPEntitlementService) SetManualMode(
	ctx context.Context,
	userID int64,
	mode VIPMode,
	actorID int64,
	reason string,
) (VIPMutationResult, error) {
	if s == nil || s.repository == nil {
		return VIPMutationResult{}, fmt.Errorf("VIP entitlement repository is not configured")
	}
	if userID <= 0 {
		return VIPMutationResult{}, fmt.Errorf("user ID must be positive")
	}
	if actorID <= 0 {
		return VIPMutationResult{}, fmt.Errorf("actor ID must be positive")
	}
	parsed, err := ParseVIPMode(string(mode))
	if err != nil {
		return VIPMutationResult{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return VIPMutationResult{}, fmt.Errorf("VIP override reason is required")
	}

	result, err := s.repository.SetManualMode(ctx, userID, parsed, actorID, reason)
	if err != nil {
		return VIPMutationResult{}, err
	}
	s.invalidateEffectiveChange(ctx, userID, result)
	return result, nil
}

func (s *VIPEntitlementService) ListAuditEvents(
	ctx context.Context,
	userID int64,
	page, pageSize int,
) ([]VIPAuditEvent, int64, error) {
	if s == nil || s.repository == nil {
		return nil, 0, fmt.Errorf("VIP entitlement repository is not configured")
	}
	if userID <= 0 {
		return nil, 0, fmt.Errorf("user ID must be positive")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	reader, ok := s.repository.(VIPAuditRepository)
	if !ok {
		return nil, 0, fmt.Errorf("VIP audit repository is not configured")
	}
	return reader.ListAuditEvents(ctx, userID, page, pageSize)
}

func (s *VIPEntitlementService) invalidateEffectiveChange(
	ctx context.Context,
	userID int64,
	result VIPMutationResult,
) {
	if !result.EffectiveChanged || s.authCacheInvalidator == nil {
		return
	}
	// This eager invalidation only reduces propagation latency. Database
	// triggers enqueue the durable cross-instance invalidation in the same
	// mutation transaction.
	s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
}
