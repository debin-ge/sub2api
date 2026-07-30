package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type vipEntitlementRepositoryStub struct {
	applyResult VIPMutationResult
	applyErr    error
	setResult   VIPMutationResult
	setErr      error
	auditEvents []VIPAuditEvent
	auditTotal  int64
	auditErr    error

	applyCalls []vipApplyCall
	setCalls   []vipSetCall
	auditPage  int
	auditLimit int
}

type vipApplyCall struct {
	userID      int64
	orderID     int64
	completedAt time.Time
	source      VIPPaidSource
}

type vipSetCall struct {
	userID  int64
	mode    VIPMode
	actorID int64
	reason  string
}

func (r *vipEntitlementRepositoryStub) ApplyPaidEligibility(
	_ context.Context,
	userID, orderID int64,
	completedAt time.Time,
	source VIPPaidSource,
) (VIPMutationResult, error) {
	r.applyCalls = append(r.applyCalls, vipApplyCall{
		userID: userID, orderID: orderID, completedAt: completedAt, source: source,
	})
	return r.applyResult, r.applyErr
}

func (r *vipEntitlementRepositoryStub) SetManualMode(
	_ context.Context,
	userID int64,
	mode VIPMode,
	actorID int64,
	reason string,
) (VIPMutationResult, error) {
	r.setCalls = append(r.setCalls, vipSetCall{
		userID: userID, mode: mode, actorID: actorID, reason: reason,
	})
	return r.setResult, r.setErr
}

func (r *vipEntitlementRepositoryStub) ListAuditEvents(
	_ context.Context,
	_ int64,
	page, pageSize int,
) ([]VIPAuditEvent, int64, error) {
	r.auditPage = page
	r.auditLimit = pageSize
	return r.auditEvents, r.auditTotal, r.auditErr
}

type vipCacheInvalidatorStub struct {
	userIDs []int64
}

func (*vipCacheInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *vipCacheInvalidatorStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (*vipCacheInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

func TestVIPEntitlementServiceGrantInvalidatesOnlyEffectiveChanges(t *testing.T) {
	completedAt := time.Date(2026, 7, 29, 10, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repo := &vipEntitlementRepositoryStub{
		applyResult: VIPMutationResult{
			EligibilityChanged: true,
			EffectiveChanged:   true,
			ManualMode:         VIPModeAuto,
		},
	}
	cache := &vipCacheInvalidatorStub{}
	svc := NewVIPEntitlementService(repo, cache)

	result, err := svc.GrantPaidEligibility(
		context.Background(), 7, 11, completedAt, VIPPaidSourcePayment,
	)
	require.NoError(t, err)
	require.True(t, result.EffectiveChanged)
	require.Equal(t, []int64{7}, cache.userIDs)
	require.Len(t, repo.applyCalls, 1)
	require.Equal(t, completedAt.UTC(), repo.applyCalls[0].completedAt)

	repo.applyResult = VIPMutationResult{
		EligibilityChanged: true,
		EffectiveChanged:   false,
		ManualMode:         VIPModeForceOff,
	}
	_, err = svc.GrantPaidEligibility(
		context.Background(), 8, 12, completedAt, VIPPaidSourcePayment,
	)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, cache.userIDs, "FORCE_OFF paid grant must not invalidate unchanged effective auth")
}

func TestVIPEntitlementServiceSetManualModeValidatesReason(t *testing.T) {
	repo := &vipEntitlementRepositoryStub{}
	svc := NewVIPEntitlementService(repo, nil)

	_, err := svc.SetManualMode(context.Background(), 1, VIPModeForceOff, 9, " ")
	require.ErrorContains(t, err, "reason is required")
	require.Empty(t, repo.setCalls)

	repo.setResult = VIPMutationResult{EffectiveChanged: true, ManualMode: VIPModeForceOff}
	result, err := svc.SetManualMode(context.Background(), 1, VIPMode(" force_off "), 9, " risk review ")
	require.NoError(t, err)
	require.Equal(t, VIPModeForceOff, result.ManualMode)
	require.Equal(t, "risk review", repo.setCalls[0].reason)
	require.Equal(t, VIPModeForceOff, repo.setCalls[0].mode)
}

func TestVIPEntitlementServiceDoesNotInvalidateFailedMutation(t *testing.T) {
	repo := &vipEntitlementRepositoryStub{setErr: errors.New("database unavailable")}
	cache := &vipCacheInvalidatorStub{}
	svc := NewVIPEntitlementService(repo, cache)

	_, err := svc.SetManualMode(context.Background(), 1, VIPModeForceOn, 2, "support grant")
	require.Error(t, err)
	require.Empty(t, cache.userIDs)
}

func TestVIPEntitlementServiceListsBoundedAuditPage(t *testing.T) {
	repo := &vipEntitlementRepositoryStub{
		auditEvents: []VIPAuditEvent{{ID: 1, UserID: 7}},
		auditTotal:  1,
	}
	svc := NewVIPEntitlementService(repo, nil)

	events, total, err := svc.ListAuditEvents(context.Background(), 7, 0, 1000)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, []VIPAuditEvent{{ID: 1, UserID: 7}}, events)
	require.Equal(t, 1, repo.auditPage)
	require.Equal(t, 100, repo.auditLimit)
}
