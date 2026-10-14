package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type costObservationRepo struct {
	VideoTaskRepository
	item               *VideoProviderCost
	id, status, reason string
	next               *time.Time
	observed, resolved int
}

func (r *costObservationRepo) ClaimVideoProviderCosts(context.Context, string, int) ([]*VideoProviderCost, error) {
	return []*VideoProviderCost{r.item}, nil
}
func (r *costObservationRepo) RecordVideoProviderCostObservation(_ context.Context, _ *VideoProviderCost, _, id, status, reason string, next *time.Time) error {
	r.id, r.status, r.reason, r.next = id, status, reason, next
	r.observed++
	return nil
}
func (r *costObservationRepo) ListVideoProviderCosts(context.Context, string, int64, int) (*VideoProviderCostPage, error) {
	panic("unexpected list")
}
func (r *costObservationRepo) ResolveVideoProviderCost(context.Context, int64, VideoProviderCostResolution) (*VideoProviderCost, error) {
	r.resolved++
	panic("worker must never resolve supplier costs")
}

type costObservationProvider struct {
	VideoProvider
	searchSupported         bool
	result                  *ProviderVideoTask
	gets, searches, creates int
}

func (*costObservationProvider) Name() string { return VideoProviderRunware }
func (p *costObservationProvider) Capabilities() VideoCapabilities {
	return VideoCapabilities{Operations: map[VideoCapability]bool{VideoCapabilityTaskSearch: p.searchSupported}}
}
func (p *costObservationProvider) Get(context.Context, *Account, ProviderTaskRef) (*ProviderVideoTask, error) {
	p.gets++
	return p.result, nil
}
func (p *costObservationProvider) SearchByClientToken(context.Context, *Account, string) (*ProviderVideoTask, error) {
	p.searches++
	return p.result, nil
}
func (p *costObservationProvider) Create(context.Context, *Account, VideoCreateRequest, []VideoInput) (*ProviderVideoTask, error) {
	p.creates++
	panic("must never replay creation")
}

func TestVideoProviderCostWorkerOnlyCollectsEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, taskID, status, wantReason string
		search, identityMismatch         bool
		wantGets, wantSearches           int
		wantNext                         bool
	}{
		{name: "recovered completion stays financially unresolved", status: VideoGenerationCompleted, search: true, wantSearches: 1},
		{name: "known supplier failure is not proof of zero cost", taskID: "upstream-task", status: VideoGenerationFailed, wantGets: 1},
		{name: "supplier cancellation is not proof of zero cost", taskID: "upstream-task", status: VideoGenerationCancelled, wantGets: 1},
		{name: "missing supplier result is not proof of rejection", search: true, wantSearches: 1, wantReason: "not_observed", wantNext: true},
		{name: "unsupported search is not retried", wantReason: "lookup_unsupported"},
		{name: "rotated supplier identity is never queried", taskID: "upstream-task", identityMismatch: true, wantReason: "account_identity_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version := int64(3)
			item := &VideoProviderCost{ID: 1, Version: 4, State: "unresolved", AccountID: 11, Provider: VideoProviderRunware,
				ProviderTaskID: tc.taskID, StableClientToken: "local-task", AccountIdentityVersion: &version, Attempts: 1}
			repo := &costObservationRepo{item: item}
			provider := &costObservationProvider{searchSupported: tc.search}
			if tc.status != "" {
				provider.result = &ProviderVideoTask{ProviderTaskID: "upstream-task", Status: tc.status}
			}
			account := Account{ID: 11, ProviderIdentityVersion: version}
			if tc.identityMismatch {
				account.ProviderIdentityVersion++
			}
			worker := &VideoTaskWorker{tasks: repo, workerID: "cost-worker", providers: NewVideoProviderRegistry(provider),
				accounts: &videoAccountRepoStub{accounts: []Account{account}}}
			require.NoError(t, worker.reconcileProviderCosts(context.Background(), 10))
			require.Equal(t, tc.wantGets, provider.gets)
			require.Equal(t, tc.wantSearches, provider.searches)
			require.Zero(t, provider.creates)
			require.Zero(t, repo.resolved)
			require.Equal(t, "unresolved", item.State)
			require.Equal(t, 1, repo.observed)
			require.Equal(t, tc.wantReason, repo.reason)
			require.Equal(t, tc.wantNext, repo.next != nil)
			if tc.status != "" && !tc.identityMismatch {
				require.Equal(t, tc.status, repo.status)
				require.Equal(t, "upstream-task", repo.id)
			}
		})
	}
}

func TestVideoProviderCostResolutionRequiresFinancialEvidence(t *testing.T) {
	valid := VideoProviderCostResolution{ExpectedVersion: 1, State: "resolved_cost_recorded", Amount: floatPointer(0.25),
		Currency: "USD", Evidence: "Supplier invoice INV-test, line 7; cost verified.", ActorID: 2, IdempotencyKey: "cost-resolution-1"}
	require.NoError(t, ValidateVideoProviderCostResolution(valid))
	for _, change := range []func(*VideoProviderCostResolution){
		func(r *VideoProviderCostResolution) { r.Amount = nil },
		func(r *VideoProviderCostResolution) { r.Amount = floatPointer(math.NaN()) },
		func(r *VideoProviderCostResolution) { r.Amount = floatPointer(math.Inf(1)) },
		func(r *VideoProviderCostResolution) { r.Amount = floatPointer(-1) },
		func(r *VideoProviderCostResolution) { r.Amount = floatPointer(0.0000000001) },
		func(r *VideoProviderCostResolution) { r.Currency = "" },
		func(r *VideoProviderCostResolution) { r.State = "completed" },
		func(r *VideoProviderCostResolution) { r.State = "resolved_no_charge" },
		func(r *VideoProviderCostResolution) { r.Evidence = "looks fine" },
		func(r *VideoProviderCostResolution) { r.ActorID = 0 },
		func(r *VideoProviderCostResolution) { r.ExpectedVersion = 0 },
		func(r *VideoProviderCostResolution) { r.IdempotencyKey = "" },
		func(r *VideoProviderCostResolution) { r.IdempotencyKey = "   " },
	} {
		request := valid
		change(&request)
		require.ErrorIs(t, ValidateVideoProviderCostResolution(request), ErrVideoInvalidRequest)
	}
	valid.State, valid.Amount = "resolved_no_charge", floatPointer(0)
	require.NoError(t, ValidateVideoProviderCostResolution(valid))
}

type costExpiryCanceller struct {
	*videoProviderStub
	err   error
	calls int
}

func (p *costExpiryCanceller) Cancel(context.Context, *Account, ProviderTaskRef) error {
	p.calls++
	return p.err
}

func TestVideoProviderCostLocalExpiryKeepsUncertaintyRegardlessOfCancelResult(t *testing.T) {
	for _, mode := range []string{"unsupported", "acknowledged", "failed"} {
		t.Run(mode, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.CreatedAt = time.Now().Add(-2 * time.Hour)
			task.RequestAttributes["execution_expires_after"] = float64(3600)
			task.RequestAttributes["execution_expiry_enforced_locally"] = true
			worker, tasks, settlements, _ := newVideoWorkerForTest(task, &ProviderVideoTask{
				ProviderTaskID: *task.ProviderTaskID, Status: VideoGenerationQueued,
			})
			var canceller *costExpiryCanceller
			if mode != "unsupported" {
				provider, _ := worker.providers.Get(VideoProviderOpenAI)
				canceller = &costExpiryCanceller{videoProviderStub: provider.(*videoProviderStub)}
				if mode == "failed" {
					canceller.err = errors.New("simulated cancellation transport failure")
				}
				worker.providers = NewVideoProviderRegistry(canceller)
			}
			require.NoError(t, worker.processTask(context.Background(), task))
			require.Equal(t, VideoGenerationExpired, tasks.task.GenerationState)
			require.Equal(t, VideoBillingReleased, tasks.task.BillingState)
			require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
			// These persistent fields, not Cancel's return value, select the cost
			// ledger insert in the same transaction as the customer refund.
			require.Equal(t, true, tasks.task.RequestAttributes["execution_expiry_enforced_locally"])
			require.NotNil(t, tasks.task.ProviderTaskID)
			if canceller != nil {
				require.Equal(t, 1, canceller.calls)
			}
		})
	}
}
