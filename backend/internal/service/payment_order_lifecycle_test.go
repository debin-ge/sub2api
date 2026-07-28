//go:build unit

package service

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	paymentprovider "github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type paymentOrderLifecycleQueryProvider struct {
	key               string
	lastQueryContext  context.Context
	lastQueryTradeNo  string
	queryTradeNos     []string
	lastCancelTradeNo string
	queryCalls        int
	cancelCalls       int
	cancelErr         error
	queryFn           func(context.Context, string) (*payment.QueryOrderResponse, error)
	responses         []*payment.QueryOrderResponse
	resp              *payment.QueryOrderResponse
}

type paymentOrderLifecycleRedeemRepo struct {
	codesByCode map[string]*RedeemCode
	useCalls    []struct {
		id     int64
		userID int64
	}
}

func (p *paymentOrderLifecycleQueryProvider) Name() string {
	return "payment-order-lifecycle-query-provider"
}

func (p *paymentOrderLifecycleQueryProvider) ProviderKey() string {
	if p.key != "" {
		return p.key
	}
	return payment.TypeAlipay
}

func (p *paymentOrderLifecycleQueryProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{p.ProviderKey()}
}

func (p *paymentOrderLifecycleQueryProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	p.lastQueryContext = ctx
	p.lastQueryTradeNo = tradeNo
	p.queryTradeNos = append(p.queryTradeNos, tradeNo)
	p.queryCalls++
	if p.queryFn != nil {
		return p.queryFn(ctx, tradeNo)
	}
	if len(p.responses) > 0 {
		resp := p.responses[0]
		if len(p.responses) > 1 {
			p.responses = p.responses[1:]
		}
		return resp, nil
	}
	return p.resp, nil
}

func (p *paymentOrderLifecycleQueryProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) CancelPayment(_ context.Context, tradeNo string) error {
	p.lastCancelTradeNo = tradeNo
	p.cancelCalls++
	return p.cancelErr
}

func (r *paymentOrderLifecycleRedeemRepo) Create(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) CreateBatch(context.Context, []RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range r.codesByCode {
		if code.ID != id {
			continue
		}
		cloned := *code
		return &cloned, nil
	}
	return nil, ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) GetByCode(_ context.Context, code string) (*RedeemCode, error) {
	redeemCode, ok := r.codesByCode[code]
	if !ok {
		return nil, ErrRedeemCodeNotFound
	}
	cloned := *redeemCode
	return &cloned, nil
}

func (r *paymentOrderLifecycleRedeemRepo) Update(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) BatchUpdate(context.Context, []int64, RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Delete(context.Context, int64) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Use(_ context.Context, id, userID int64) error {
	for code, redeemCode := range r.codesByCode {
		if redeemCode.ID != id {
			continue
		}
		now := time.Now().UTC()
		redeemCode.Status = StatusUsed
		redeemCode.UsedBy = &userID
		redeemCode.UsedAt = &now
		r.codesByCode[code] = redeemCode
		r.useCalls = append(r.useCalls, struct {
			id     int64
			userID int64
		}{id: id, userID: userID})
		return nil
	}
	return ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) List(context.Context, pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUser(context.Context, int64, int) ([]RedeemCode, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUserPaginated(context.Context, int64, pagination.PaginationParams, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	panic("unexpected call")
}

func TestVerifyOrderByOutTradeNoBackfillsTradeNoFromPaidQuery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_trade_no_missing").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-123", got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "upstream-trade-123", reloaded.PaymentTradeNo)

	require.Equal(t, 88.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
	require.Equal(t, int64(1), redeemRepo.useCalls[0].id)
	require.Equal(t, user.ID, redeemRepo.useCalls[0].userID)
}

func TestVerifyOrderByOutTradeNoRetriesZeroAmountPaidQueryOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-retry@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-retry-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-RETRY").
		SetOutTradeNo("sub2_checkpaid_retry_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		responses: []*payment.QueryOrderResponse{
			{
				TradeNo: "upstream-trade-zero",
				Status:  payment.ProviderStatusPaid,
				Amount:  0,
			},
			{
				TradeNo: "upstream-trade-retry",
				Status:  payment.ProviderStatusPaid,
				Amount:  88,
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, 2, provider.queryCalls)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-retry", got.PaymentTradeNo)
}

func TestVerifyOrderByOutTradeNoRejectsPaidQueryWithZeroAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-zero-amount@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-zero-amount-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-ZERO-AMOUNT").
		SetOutTradeNo("sub2_checkpaid_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-zero",
			Status:  payment.ProviderStatusPaid,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Empty(t, got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)

	require.Equal(t, 0.0, userRepo.getByIDUser.Balance)
	require.Empty(t, redeemRepo.useCalls)
}

func TestReconcilePaidDoesNotReturnAlreadyPaidWhenFulfillmentRejectsAmountMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-amount-mismatch@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-amount-mismatch-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-AMOUNT-MISMATCH").
		SetOutTradeNo("sub2_checkpaid_amount_mismatch").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-amount-mismatch",
			Status:  payment.ProviderStatusPaid,
			Amount:  87.99,
			Metadata: map[string]string{
				"profile_id":          "profile-123",
				"balance_id":          "balance-123",
				"currency":            "USD",
				"settlement_strategy": "exact_only",
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result := svc.reconcilePaid(ctx, order)
	require.Empty(t, result)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)
	require.Equal(t, 88.0, reloaded.PayAmount)
}

func TestHandleWiseQueryOrderResponseDoesNotFulfillWisePaidQueryWithMissingSnapshotMetadata(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-missing-metadata@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-missing-metadata-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-MISSING-METADATA").
		SetOutTradeNo("sub2_wise_missing_metadata").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_key":         payment.TypeWise,
			"merchant_id":          "profile-123",
			"balance_id":           "balance-123",
			"currency":             "USD",
			"settlement_strategy":  "exact_only",
			"provider_instance_id": "88",
		}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient: client,
	}

	result, err := svc.handleWiseQueryOrderResponse(ctx, order, &payment.QueryOrderResponse{
		TradeNo: "wise-upstream-missing-metadata",
		Status:  payment.ProviderStatusPaid,
		Amount:  88,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Matched)
	require.False(t, result.AutoFulfill)
	require.Equal(t, "metadata_mismatch", result.Reason)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)
	require.Equal(t, 88.0, reloaded.PayAmount)
}

func TestVerifyOrderByOutTradeNoDoesNotCancelUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-PENDING").
		SetOutTradeNo("sub2_checkpaid_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestVerifyOrderByOutTradeNoReloadsAfterPendingQueryObservesConcurrentCompletion(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-concurrent-completion@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-concurrent-completion-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-CONCURRENT-COMPLETION").
		SetOutTradeNo("sub2_checkpaid_concurrent_completion").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	completedAt := time.Now()
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		queryFn: func(context.Context, string) (*payment.QueryOrderResponse, error) {
			// Simulate a webhook committing completion while this request's
			// upstream query still reports pending. The service must not return
			// the PaymentOrder object loaded before the provider call.
			_, updateErr := client.PaymentOrder.UpdateOneID(order.ID).
				SetStatus(OrderStatusCompleted).
				SetCompletedAt(completedAt).
				Save(ctx)
			require.NoError(t, updateErr)
			return &payment.QueryOrderResponse{
				TradeNo: order.OutTradeNo,
				Status:  payment.ProviderStatusPending,
			}, nil
		},
	}
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)

	require.NoError(t, err)
	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.NotNil(t, got.CompletedAt)
	require.WithinDuration(t, completedAt, *got.CompletedAt, time.Second)
}

func TestCancelOrderStillClosesUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("cancel-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-PENDING").
		SetOutTradeNo("sub2_cancel_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Equal(t, order.OutTradeNo, provider.lastCancelTradeNo)
	require.Equal(t, 1, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}

func TestCheckPaidMarksWiseProviderCancelledOrderCancelled(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-provider-cancelled@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-provider-cancelled-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-PROVIDER-CANCELLED").
		SetOutTradeNo("sub2_wise_provider_cancelled").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-cancelled-1",
			Status:  payment.ProviderStatusCancelled,
			Metadata: map[string]string{
				"reconcile_reason":    "status_cancelled",
				"wise_transaction_id": "wise-cancelled-1",
			},
		},
	}
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result := svc.checkPaid(ctx, order)

	require.Empty(t, result)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
	require.True(t, svc.hasAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_CANCELLED"))
}

func TestCheckPaidMarksWiseProviderFailedOrderFailed(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-provider-failed@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-provider-failed-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-PROVIDER-FAILED").
		SetOutTradeNo("sub2_wise_provider_failed").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-failed-1",
			Status:  payment.ProviderStatusFailed,
			Metadata: map[string]string{
				"reconcile_reason":    "status_failed",
				"wise_transaction_id": "wise-failed-1",
			},
		},
	}
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result := svc.checkPaid(ctx, order)

	require.Empty(t, result)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, reloaded.Status)
	require.NotNil(t, reloaded.FailedAt)
	require.NotNil(t, reloaded.FailedReason)
	require.Contains(t, *reloaded.FailedReason, "status_failed")
	require.True(t, svc.hasAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_FAILED"))
}

func TestCancelOrderAuditsWiseUnsupportedCancel(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wise-cancel-unsupported@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-cancel-unsupported-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CANCEL-UNSUPPORTED").
		SetOutTradeNo("sub2_wise_cancel_unsupported").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
		},
		cancelErr: payment.ErrCancelNotSupported,
	}
	registry.Register(provider)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)

	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Equal(t, 1, provider.cancelCalls)
	require.True(t, svc.hasAuditLog(ctx, order.ID, "PAYMENT_CANCEL_UPSTREAM_SKIPPED"))
}

func TestReconcilePendingWxpayOrdersBackfillsPaidOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wxpay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("wxpay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(50).
		SetFeeRate(0).
		SetRechargeCode("WXPAY-RECONCILE").
		SetOutTradeNo("sub2_wxpay_reconcile").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWxpay,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wxpay-upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  50,
			Metadata: map[string]string{
				"trade_state": "SUCCESS",
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingWxpayOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "wxpay-upstream-trade-123", reloaded.PaymentTradeNo)
	require.Equal(t, 50.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestPaymentOrderExpiryRunOnceReconcilesLegacyStripeWxpayPaymentIntentBeforeExpiry(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("stripe-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("stripe-reconcile-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-reconcile-provider").
		SetConfig(encryptWebhookProviderConfig(t, map[string]string{
			"secretKey": "sk_test_stripe_reconcile",
			"currency":  "USD",
		})).
		SetSupportedTypes("stripe,wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(instance.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("STRIPE-RECONCILE").
		SetOutTradeNo("sub2_stripe_reconcile").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("pi_stripe_reconcile_paid").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(10 * time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeStripe,
		resp: &payment.QueryOrderResponse{
			TradeNo: "pi_stripe_reconcile_paid",
			Status:  payment.ProviderStatusPaid,
			Amount:  10,
			Metadata: map[string]string{
				"currency": "USD",
			},
		},
	}
	restoreProviderFactory := replacePaymentProviderFactoryForTest(t, provider)
	t.Cleanup(restoreProviderFactory)

	paymentSvc := &PaymentService{
		entClient:       client,
		loadBalancer:    newWebhookProviderTestLoadBalancer(client),
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}
	expirySvc := NewPaymentOrderExpiryService(paymentSvc, time.Minute)

	expirySvc.runOnce()

	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, order.PaymentTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, payment.TypeWxpay, reloaded.PaymentType)
	require.Equal(t, payment.TypeStripe, psStringValue(reloaded.ProviderKey))
	require.Equal(t, instanceID, psStringValue(reloaded.ProviderInstanceID))
	require.NotNil(t, reloaded.PaidAt)
	require.True(t, reloaded.PaidAt.Before(order.ExpiresAt))
	require.Equal(t, 10.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)

	// A later sweep must not query or fulfill the already completed order again.
	expirySvc.runOnce()
	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, 10.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestReconcilePendingStripeOrdersFiltersCandidatesAndCapsEachSweep(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	now := time.Now()

	user, err := client.User.Create().
		SetEmail("stripe-reconcile-filter@example.com").
		SetPasswordHash("hash").
		SetUsername("stripe-reconcile-filter-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-reconcile-filter-provider").
		SetConfig(encryptWebhookProviderConfig(t, map[string]string{
			"secretKey": "sk_test_stripe_reconcile_filter",
			"currency":  "USD",
		})).
		SetSupportedTypes("stripe,wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(instance.ID, 10)

	createOrder := func(suffix, status, paymentType, providerKey, tradeNo string, createdAt, expiresAt time.Time) {
		t.Helper()
		builder := client.PaymentOrder.Create().
			SetUserID(user.ID).
			SetUserEmail(user.Email).
			SetUserName(user.Username).
			SetAmount(10).
			SetPayAmount(10).
			SetFeeRate(0).
			SetRechargeCode("STRIPE-FILTER-" + suffix).
			SetOutTradeNo("sub2_stripe_filter_" + suffix).
			SetPaymentType(paymentType).
			SetPaymentTradeNo(tradeNo).
			SetOrderType(payment.OrderTypeBalance).
			SetStatus(status).
			SetExpiresAt(expiresAt).
			SetClientIP("127.0.0.1").
			SetSrcHost("api.example.com").
			SetCreatedAt(createdAt)
		if providerKey == payment.TypeStripe {
			builder.
				SetProviderKey(providerKey).
				SetProviderInstanceID(instanceID).
				SetProviderSnapshot(map[string]any{
					"schema_version":       2,
					"provider_instance_id": instanceID,
					"provider_key":         payment.TypeStripe,
					"currency":             "USD",
				})
		} else if providerKey != "" {
			builder.SetProviderKey(providerKey)
		}
		_, createErr := builder.Save(ctx)
		require.NoError(t, createErr)
	}

	fixedTradeNos := make([]string, 0, pendingStripeReconcileLimit+2)
	for i := 0; i < pendingStripeReconcileLimit+2; i++ {
		suffix := strconv.Itoa(i)
		tradeNo := "pi_stripe_filter_" + suffix
		createOrder(
			suffix,
			OrderStatusPending,
			payment.TypeStripe,
			payment.TypeStripe,
			tradeNo,
			now.Add(-time.Duration(i+1)*time.Minute),
			now.Add(time.Hour),
		)
		fixedTradeNos = append(fixedTradeNos, tradeNo)
	}

	// A migration-era Stripe order can have no provider_key, and payment_type
	// can be the selected sub-method. The pi_ reference safely identifies it as
	// Stripe and resolves the unique Stripe instance.
	createOrder("legacy_wxpay", OrderStatusPending, payment.TypeWxpay, "", "pi_stripe_filter_legacy_wxpay", now, now.Add(time.Hour))
	createOrder("expired", OrderStatusPending, payment.TypeStripe, payment.TypeStripe, "pi_stripe_filter_expired", now, now.Add(-time.Minute))
	createOrder("completed", OrderStatusCompleted, payment.TypeStripe, payment.TypeStripe, "pi_stripe_filter_completed", now, now.Add(time.Hour))
	createOrder("missing_pi", OrderStatusPending, payment.TypeStripe, payment.TypeStripe, "", now, now.Add(time.Hour))
	createOrder("wrong_reference", OrderStatusPending, payment.TypeStripe, payment.TypeStripe, "ch_stripe_filter", now, now.Add(time.Hour))
	createOrder("official_wxpay", OrderStatusPending, payment.TypeWxpay, payment.TypeWxpay, "pi_not_a_stripe_order", now, now.Add(time.Hour))

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeStripe,
		resp: &payment.QueryOrderResponse{
			Status: payment.ProviderStatusPending,
		},
	}
	restoreProviderFactory := replacePaymentProviderFactoryForTest(t, provider)
	t.Cleanup(restoreProviderFactory)
	svc := &PaymentService{
		entClient:       client,
		loadBalancer:    newWebhookProviderTestLoadBalancer(client),
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingStripeOrders(ctx)

	require.NoError(t, err)
	require.Zero(t, recovered)
	require.Equal(t, pendingStripeReconcileLimit, provider.queryCalls)
	expectedTradeNos := append([]string{"pi_stripe_filter_legacy_wxpay"}, fixedTradeNos[:pendingStripeReconcileLimit-1]...)
	require.Equal(t, expectedTradeNos, provider.queryTradeNos)
	require.Zero(t, provider.cancelCalls)
}

func TestReconcilePendingStripeOrdersDoesNotPinAmbiguousLegacyStripeInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("stripe-reconcile-ambiguous@example.com").
		SetPasswordHash("hash").
		SetUsername("stripe-reconcile-ambiguous-user").
		Save(ctx)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		_, err = client.PaymentProviderInstance.Create().
			SetProviderKey(payment.TypeStripe).
			SetName("stripe-reconcile-ambiguous-" + strconv.Itoa(i)).
			SetConfig(encryptWebhookProviderConfig(t, map[string]string{
				"secretKey": "sk_test_stripe_reconcile_ambiguous_" + strconv.Itoa(i),
				"currency":  "USD",
			})).
			SetSupportedTypes("stripe,wxpay").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
	}

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("STRIPE-AMBIGUOUS").
		SetOutTradeNo("sub2_stripe_ambiguous").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("pi_stripe_ambiguous").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeStripe,
		resp: &payment.QueryOrderResponse{
			Status: payment.ProviderStatusPending,
		},
	}
	restoreProviderFactory := replacePaymentProviderFactoryForTest(t, provider)
	t.Cleanup(restoreProviderFactory)
	svc := &PaymentService{
		entClient:       client,
		loadBalancer:    newWebhookProviderTestLoadBalancer(client),
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingStripeOrders(ctx)

	require.NoError(t, err)
	require.Zero(t, recovered)
	require.Zero(t, provider.queryCalls)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Nil(t, reloaded.ProviderKey)
	require.Nil(t, reloaded.ProviderInstanceID)
}

func TestVerifyOrderByOutTradeNoUsesOutTradeNoWhenPaymentTradeNoAlreadyExistsForAlipay(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-existing-trade@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-existing-trade-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-EXISTING-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_use_out_trade_no").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("upstream-trade-existing").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-existing",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, "upstream-trade-existing", got.PaymentTradeNo)
}

func TestPaymentOrderAllowsRegistryFallbackOnlyForLegacyOrdersWithoutPinnedProviderState(t *testing.T) {
	t.Parallel()

	require.True(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
	}))

	instanceID := "12"
	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType:        payment.TypeAlipay,
		ProviderInstanceID: &instanceID,
	}))

	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":       2,
			"provider_instance_id": "12",
		},
	}))
}

func TestPaymentOrderQueryReferenceUsesOutTradeNoForOfficialProviders(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType:    payment.TypeWxpay,
		OutTradeNo:     "sub2_out_trade_no",
		PaymentTradeNo: "wx-transaction-id",
	}

	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, &paymentOrderLifecycleQueryProvider{}))
	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, paymentFulfillmentTestProvider{
		key: payment.TypeWxpay,
	}))
}

func TestPaymentOrderQueryReferenceUsesOutTradeNoForWise(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType:    payment.TypeWise,
		OutTradeNo:     "sub2_wise_123",
		PaymentTradeNo: "wise-tx-123",
	}
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeWise}

	require.Equal(t, "sub2_wise_123", paymentOrderQueryReference(order, provider))
}

func TestReconcilePaidWisePassesOrderCreatedAtToProvider(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	createdAt := time.Now().UTC().Add(-96 * time.Hour).Truncate(time.Second)

	user, err := client.User.Create().
		SetEmail("wise-created-at-query@example.com").
		SetPasswordHash("hash").
		SetUsername("wise-created-at-query-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("WISE-CREATED-AT-QUERY").
		SetOutTradeNo("sub2_wise_created_at_query").
		SetPaymentType(payment.TypeWise).
		SetPaymentTradeNo("wise-upstream-created-at-query").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetCreatedAt(createdAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWise,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wise-upstream-created-at-query",
			Status:  payment.ProviderStatusPending,
			Metadata: map[string]string{
				"reconcile_decision": "no_match",
				"reconcile_reason":   "activity_not_found",
			},
		},
	}
	registry := payment.NewRegistry()
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	result := svc.reconcilePaid(ctx, order)
	require.Empty(t, result)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	gotCreatedAt, ok := paymentprovider.WiseOrderCreatedAtFromContext(provider.lastQueryContext)
	require.True(t, ok)
	require.WithinDuration(t, createdAt, gotCreatedAt, time.Second)
}

func newPaymentOrderLifecycleTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:payment_order_lifecycle?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
