//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type cachedCreationConcurrency struct {
	fakeConcurrencyCache
	users, accounts, userReleases, accountReleases atomic.Int32
	keys, keyReleases                              atomic.Int32
}

func (s *cachedCreationConcurrency) TrackAPIKeySlot(context.Context, int64, string) error {
	s.keys.Add(1)
	return nil
}
func (s *cachedCreationConcurrency) ReleaseAPIKeySlot(context.Context, int64, string) error {
	s.keys.Add(-1)
	s.keyReleases.Add(1)
	return nil
}
func (s *cachedCreationConcurrency) GetAPIKeyConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	counts := make(map[int64]int)
	for _, id := range ids {
		counts[id] = int(s.keys.Load())
	}
	return counts, nil
}

func (s *cachedCreationConcurrency) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	s.users.Add(1)
	return true, nil
}
func (s *cachedCreationConcurrency) ReleaseUserSlot(context.Context, int64, string) error {
	s.users.Add(-1)
	s.userReleases.Add(1)
	return nil
}
func (s *cachedCreationConcurrency) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	s.accounts.Add(1)
	return true, nil
}
func (s *cachedCreationConcurrency) ReleaseAccountSlot(context.Context, int64, string) error {
	s.accounts.Add(-1)
	s.accountReleases.Add(1)
	return nil
}

func TestGeminiCachedContentsCreate_CancellationHoldsSlotsUntilCompletion(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.upstream.beforeCreate = func(upstreamCtx context.Context) {
		require.EqualValues(t, 1, env.slots.users.Load())
		require.EqualValues(t, 1, env.slots.accounts.Load())
		require.EqualValues(t, 1, env.slots.keys.Load())
		cancel()
		require.NoError(t, upstreamCtx.Err(), "the supplier POST continues after client cancellation")
		require.Never(t, func() bool {
			return env.slots.users.Load() != 1 || env.slots.accounts.Load() != 1 || env.slots.keys.Load() != 1
		}, 50*time.Millisecond, time.Millisecond)
	}
	w := env.serve(http.MethodPost, "/v1beta/cachedContents",
		`{"model":"models/gemini-2.5-pro","ttl":"3600s","contents":[{"parts":[{"text":"doc"}]}]}`, nil,
		func(c *gin.Context) {
			c.Request = c.Request.WithContext(ctx)
			env.h.GeminiCachedContentsCreate(c)
		})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, env.repo.records, 1, "a cancelled caller still gets a persisted, billed resource")
	env.waitForUsage(t, 1)
	require.Empty(t, env.repo.intents)
	require.Zero(t, env.slots.users.Load())
	require.Zero(t, env.slots.accounts.Load())
	require.EqualValues(t, 1, env.slots.userReleases.Load())
	require.EqualValues(t, 1, env.slots.accountReleases.Load())
	require.Zero(t, env.slots.keys.Load())
	require.EqualValues(t, 1, env.slots.keyReleases.Load())
}

func TestGeminiCachedContentAccountSlotCancellation(t *testing.T) {
	for _, preacquired := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait_plan", true: "preacquired"}[preacquired], func(t *testing.T) {
			slots := &cachedCreationConcurrency{}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatNone, 0)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
			selection := &service.AccountSelectionResult{Account: &service.Account{ID: 9}, Acquired: preacquired,
				WaitPlan: &service.AccountWaitPlan{MaxConcurrency: 1, MaxWaiting: 1, Timeout: time.Second}}
			if preacquired {
				slots.accounts.Add(1)
				selection.ReleaseFunc = func() { _ = slots.ReleaseAccountSlot(context.Background(), 9, "synthetic") }
			}
			release, ok := (&GatewayHandler{}).acquireGeminiCachedContentAccountSlot(c, zap.NewNop(), helper, selection)
			require.True(t, ok)
			cancel()
			require.Never(t, func() bool { return slots.accounts.Load() != 1 }, 30*time.Millisecond, time.Millisecond)
			release()
			release()
			require.Zero(t, slots.accounts.Load())
			require.EqualValues(t, 1, slots.accountReleases.Load())
		})
	}
}

func TestGeminiCachedContentsCreate_UnknownOutcomeBlocksFurtherCreates(t *testing.T) {
	for _, outcome := range []string{"lost_response", "server_error", "malformed_success"} {
		t.Run(outcome, func(t *testing.T) {
			env := newCachedBillingEnv(t, 500, 900, 901)
			switch outcome {
			case "lost_response":
				env.upstream.transportErrors = map[string]error{http.MethodPost: errors.New("accepted then connection lost")}
			case "server_error":
				env.upstream.failures = map[string]cachedBillingUpstreamFailure{http.MethodPost: {status: 503, body: `{}`}}
			case "malformed_success":
				env.upstream.createBody = `{}`
			}
			w := env.create()
			require.GreaterOrEqual(t, w.Code, 500)
			require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost), "unknown outcomes must not fail over")
			require.Len(t, env.repo.intents, 1)
			require.Empty(t, env.repo.records)
			// The next request uses a different API key but belongs to the same user.
			env.apiKey.ID++
			w = env.create()
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost))
			// A new service instance uses the durable repository state, not local memory.
			restarted := service.NewGeminiCachedContentService(env.repo, nil, nil)
			for _, pending := range env.repo.intents {
				pending.PublicID = "another"
				pending.UserID++
				require.ErrorIs(t, restarted.BeginCreation(context.Background(), &pending), service.ErrGeminiCachedContentCreationPending,
					"the supplier account also stays blocked for other users")
			}
		})
	}
}

func TestGeminiCachedContentsCreate_IntentStorageFailureDoesNotSend(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.repo.failBegin = true
	w := env.create()
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Zero(t, env.upstream.methodCalls(http.MethodPost))
	require.Zero(t, env.slots.users.Load())
	require.Zero(t, env.slots.accounts.Load())
}

func TestGeminiCachedContentsCreate_DefinitiveRejectionCanFailOver(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900, 901)
	env.upstream.failures = map[string]cachedBillingUpstreamFailure{http.MethodPost: {status: 429, body: `{}`}}
	w := env.create()
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, 2, env.upstream.methodCalls(http.MethodPost))
	require.Empty(t, env.repo.intents)
	require.Zero(t, env.slots.accounts.Load())
}

func TestGeminiCachedContentsCreate_FailedResolutionKeepsGuard(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900, 901)
	env.repo.failResolve = true
	env.upstream.failures = map[string]cachedBillingUpstreamFailure{http.MethodPost: {status: 429, body: `{}`}}
	w := env.create()
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost))
	require.Len(t, env.repo.intents, 1)
}

func TestGeminiCachedContentsPatch_ReservationOutageDoesNotMutateUpstream(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.seed(time.Now().Add(time.Hour))
	baseline := env.repo.records["abc"].ExpireTime
	enableCachedBillingReservations(t, env, &cachedBillingReservationCache{balance: 500, failReserve: true})
	w := env.patch()
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Zero(t, env.upstream.methodCalls(http.MethodPatch))
	require.Equal(t, baseline, env.repo.records["abc"].ExpireTime)
	require.Empty(t, env.usage.totals())
	require.Empty(t, env.repo.mutations)
}

func TestGeminiCachedContentsCreate_BillingFailureRetainsIntent(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(time.Hour))
	env.usage.fail = true
	w := env.create()
	require.Equal(t, http.StatusOK, w.Code, "the resource was created and persisted")
	require.Len(t, env.repo.records, 1)
	require.Empty(t, env.usage.totals())
	require.Len(t, env.repo.intents, 1, "failed billing must not silently clear reconciliation admission")
	w = env.create()
	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost))
}

func TestGeminiCachedContentsCreate_PostCreateFailureRetainsIntent(t *testing.T) {
	for _, failure := range []string{"cleanup", "binding"} {
		t.Run(failure, func(t *testing.T) {
			balance := 500.0
			if failure == "cleanup" {
				balance = 1 // storage is rejected only after the upstream token count is known
			}
			env := newCachedBillingEnv(t, balance, 900)
			env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(24 * time.Hour))
			if failure == "cleanup" {
				env.upstream.transportErrors = map[string]error{http.MethodDelete: errors.New("delete response lost")}
			} else {
				env.repo.failCreate = true
			}
			w := env.create()
			require.NotEqual(t, http.StatusOK, w.Code)
			require.Equal(t, 1, env.upstream.methodCalls(http.MethodDelete))
			require.Len(t, env.repo.intents, 1)
			require.Empty(t, env.repo.records)
			w = env.create()
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost))
		})
	}
}
