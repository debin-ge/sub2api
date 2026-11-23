package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestFailoverState_RecordAccountSlotUnavailable(t *testing.T) {
	ctx := context.Background()

	t.Run("wait timeout excludes account without consuming switches", func(t *testing.T) {
		fs := NewFailoverState(3, true)
		err := &ConcurrencyError{SlotType: "account", IsTimeout: true}

		require.False(t, fs.SlotWaitFailed())
		require.True(t, fs.RecordAccountSlotUnavailable(ctx, 7, err))
		require.Contains(t, fs.FailedAccountIDs, int64(7))
		require.Same(t, err, fs.LastSlotErr)
		require.True(t, fs.SlotWaitFailed())
		require.Zero(t, fs.SwitchCount, "slot failures happen before forwarding and must not consume upstream switches")
		require.False(t, fs.ForceCacheBilling)
		require.Nil(t, fs.LastFailoverErr)
	})

	t.Run("wait queue full is reselectable", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		require.True(t, fs.RecordAccountSlotUnavailable(ctx, 8, &WaitQueueFullError{SlotType: "account"}))
		require.Contains(t, fs.FailedAccountIDs, int64(8))
	})

	t.Run("non slot errors are surfaced", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		require.False(t, fs.RecordAccountSlotUnavailable(ctx, 9, context.Canceled))
		require.False(t, fs.RecordAccountSlotUnavailable(ctx, 9, errors.New("redis down")))
		require.Empty(t, fs.FailedAccountIDs)
		require.False(t, fs.SlotWaitFailed())
	})

	t.Run("client gone is surfaced", func(t *testing.T) {
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		fs := NewFailoverState(3, false)
		require.False(t, fs.RecordAccountSlotUnavailable(canceled, 10, &ConcurrencyError{SlotType: "account", IsTimeout: true}))
		require.Empty(t, fs.FailedAccountIDs)
	})

	t.Run("reselects are bounded", func(t *testing.T) {
		fs := NewFailoverState(10, false)
		for i := 0; i < maxSlotWaitReselects; i++ {
			require.True(t, fs.RecordAccountSlotUnavailable(ctx, int64(100+i), &ConcurrencyError{SlotType: "account"}))
		}
		require.False(t, fs.RecordAccountSlotUnavailable(ctx, 999, &ConcurrencyError{SlotType: "account"}))
		require.NotContains(t, fs.FailedAccountIDs, int64(999))
	})
}

func TestConcurrencyHelper_AcquireAccountSlotForWaitPlan_NoSecondQueueAfterSlotFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var acquireCalls int32
	cache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			atomic.AddInt32(&acquireCalls, 1)
			return false, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	fs := NewFailoverState(3, false)
	require.True(t, fs.RecordAccountSlotUnavailable(context.Background(), 1, &ConcurrencyError{SlotType: "account", IsTimeout: true}))

	plan := &service.AccountWaitPlan{AccountID: 2, MaxConcurrency: 1, Timeout: 10 * time.Second, MaxWaiting: 3}
	streamStarted := false
	start := time.Now()
	release, err := helper.AcquireAccountSlotForWaitPlan(c, fs.SlotWaitFailed(), 2, plan, false, &streamStarted)
	require.Nil(t, release)
	var concurrencyErr *ConcurrencyError
	require.ErrorAs(t, err, &concurrencyErr)
	require.Less(t, time.Since(start), time.Second, "after a slot failure the request must not queue again")
	require.Equal(t, int32(1), atomic.LoadInt32(&acquireCalls))
	require.True(t, fs.RecordAccountSlotUnavailable(context.Background(), 2, err), "immediate miss feeds back into another reselect")
}

func TestConcurrencyHelper_AcquireAccountSlotForWaitPlan_ImmediateSuccessAfterSlotFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	fs := NewFailoverState(3, false)
	require.True(t, fs.RecordAccountSlotUnavailable(context.Background(), 1, &ConcurrencyError{SlotType: "account", IsTimeout: true}))

	streamStarted := false
	release, err := helper.AcquireAccountSlotForWaitPlan(c, fs.SlotWaitFailed(), 2, &service.AccountWaitPlan{AccountID: 2, MaxConcurrency: 1, Timeout: time.Second}, false, &streamStarted)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseAccountCalled))
}

func TestConcurrencyErrorResponse_SlotTimeoutStays429(t *testing.T) {
	status, _, code, message := concurrencyErrorResponse(&ConcurrencyError{SlotType: "account", IsTimeout: true}, "account")
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, gatewayConcurrencyLimitCode, code)
	require.Equal(t, "Concurrency limit exceeded for account, please retry later", message)
}

func TestOpenAISlotReselect_SharesCallerExclusionSet(t *testing.T) {
	ctx := context.Background()
	failedAccountIDs := make(map[int64]struct{})
	reselect := newOpenAISlotReselect(failedAccountIDs)

	require.False(t, reselect.slotWaitFailed())
	require.Nil(t, reselect.lastSlotErr())
	timeoutErr := &ConcurrencyError{SlotType: "account", IsTimeout: true}
	require.True(t, reselect.record(ctx, 42, timeoutErr))
	require.Contains(t, failedAccountIDs, int64(42), "excluded account must land in the caller's failedAccountIDs")
	require.True(t, reselect.slotWaitFailed())
	require.Same(t, timeoutErr, reselect.lastSlotErr())

	var nilReselect *openAISlotReselect
	require.False(t, nilReselect.record(ctx, 43, timeoutErr), "nil reselect keeps the legacy write-error behavior")
	require.False(t, nilReselect.slotWaitFailed())
	require.Nil(t, nilReselect.lastSlotErr())
}

func TestOpenAIGatewayHandler_WriteOpenAISlotReselectExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &OpenAIGatewayHandler{}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.False(t, h.writeOpenAISlotReselectExhausted(c, newOpenAISlotReselect(map[int64]struct{}{}), false))
	require.False(t, c.Writer.Written())

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	reselect := newOpenAISlotReselect(map[int64]struct{}{})
	require.True(t, reselect.record(context.Background(), 7, &ConcurrencyError{SlotType: "account", IsTimeout: true}))
	require.True(t, h.writeOpenAISlotReselectExhausted(c, reselect, false))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Contains(t, rec.Body.String(), "Concurrency limit exceeded for account")
}
