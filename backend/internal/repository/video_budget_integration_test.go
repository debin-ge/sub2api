//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoBudgetKeyIncludesSpentAndInFlightReservations(t *testing.T) {
	for _, test := range []struct {
		name, limit, usage, window string
		want                       error
	}{
		{"total", "quota", "quota_used", "", service.ErrAPIKeyQuotaExhausted},
		{"5h", "rate_limit_5h", "usage_5h", "window_5h_start", service.ErrAPIKeyRateLimit5hExceeded},
		{"1d", "rate_limit_1d", "usage_1d", "window_1d_start", service.ErrAPIKeyRateLimit1dExceeded},
		{"7d", "rate_limit_7d", "usage_7d", "window_7d_start", service.ErrAPIKeyRateLimit7dExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			statement := fmt.Sprintf(`UPDATE api_keys SET %s = 10, %s = 7`, test.limit, test.usage)
			if test.window != "" {
				statement += fmt.Sprintf(`, %s = NOW()`, test.window)
			}
			_, err := integrationDB.ExecContext(ctx, statement+` WHERE id = $1`, key.ID)
			require.NoError(t, err)
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "too-large", "same-body", 4)
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.ErrorIs(t, err, test.want)
			params.HoldAmount = 2
			_, created, err := repo.CreateHeldVideoTask(ctx, params)
			require.NoError(t, err)
			require.True(t, created)
			params.PublicID, params.IdempotencyKey = service.NewVideoTaskID(), "over-reserved"
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.ErrorIs(t, err, test.want)
			assertVideoBudgetTotals(t, user.ID, 1, 98, 2)
		})
	}
}

func TestVideoBudgetPlatformReservationsSpanAPIKeys(t *testing.T) {
	for _, test := range []struct {
		limit, usage, window string
		want                 error
	}{
		{"daily_limit_usd", "daily_usage_usd", "daily_window_start", service.ErrUserPlatformDailyQuotaExhausted},
		{"weekly_limit_usd", "weekly_usage_usd", "weekly_window_start", service.ErrUserPlatformWeeklyQuotaExhausted},
		{"monthly_limit_usd", "monthly_usage_usd", "monthly_window_start", service.ErrUserPlatformMonthlyQuotaExhausted},
	} {
		t.Run(test.limit, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			extraKey := newVideoBudgetKey(t, user.ID)
			statement := fmt.Sprintf(`INSERT INTO user_platform_quotas (user_id, platform, %s, %s, %s) VALUES ($1, 'openai', 10, 7, NOW())`, test.limit, test.usage, test.window)
			_, err := integrationDB.ExecContext(ctx, statement, user.ID)
			require.NoError(t, err)
			_, _, err = repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "platform-first", "first-body", 2))
			require.NoError(t, err)
			_, _, err = repo.CreateHeldVideoTask(ctx, videoCreateParams(user, extraKey, account, service.NewVideoTaskID(), "platform-second", "second-body", 2))
			require.ErrorIs(t, err, test.want)
			assertVideoBudgetTotals(t, user.ID, 1, 98, 2)
		})
	}
}

func TestVideoBudgetConcurrentAccountsCannotOversubscribe(t *testing.T) {
	for _, scope := range []string{"key", "platform"} {
		t.Run(scope, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			extraAccount := accountEntityToService(newOwnershipTestAccount(t, nil, map[string]any{"api_key": uuid.NewString()}))
			keys := []*service.APIKey{key, key}
			accounts := []*service.Account{account, extraAccount}
			if scope == "key" {
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET rate_limit_5h = 5 WHERE id = $1`, key.ID)
				require.NoError(t, err)
			} else {
				keys[1] = newVideoBudgetKey(t, user.ID)
				_, err := integrationDB.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id, platform, daily_limit_usd) VALUES ($1, 'openai', 5)`, user.ID)
				require.NoError(t, err)
			}
			const callers = 16
			results := make(chan error, callers)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for index := 0; index < callers; index++ {
				workers.Add(1)
				go func(index int) {
					defer workers.Done()
					<-start
					callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					_, _, err := repo.CreateHeldVideoTask(callCtx, videoCreateParams(user, keys[index%2], accounts[index%2], service.NewVideoTaskID(), fmt.Sprint(index), fmt.Sprint(index), 2))
					results <- err
				}(index)
			}
			close(start)
			workers.Wait()
			close(results)
			accepted := 0
			for err := range results {
				if err == nil {
					accepted++
				} else {
					require.True(t, errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded) || errors.Is(err, service.ErrUserPlatformDailyQuotaExhausted), "%v", err)
				}
			}
			require.Equal(t, 2, accepted)
			assertVideoBudgetTotals(t, user.ID, 2, 96, 4)
		})
	}
}

func TestVideoBudgetRolloverKeepsUnknownAndReviewReservations(t *testing.T) {
	for _, billing := range []string{service.VideoBillingHeld, service.VideoBillingCapturePending, service.VideoBillingReleasePending, service.VideoBillingManualReview} {
		t.Run(billing, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET rate_limit_5h = 5 WHERE id = $1`, key.ID)
			require.NoError(t, err)
			task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "old-intent", "old-body", 4))
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET generation_state = 'submission_unknown', billing_state = $2, created_at = NOW() - INTERVAL '60 days', deleted_at = NOW() WHERE id = $1`, task.ID, billing)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h = 99, window_5h_start = NOW() - INTERVAL '10 hours' WHERE id = $1`, key.ID)
			require.NoError(t, err)
			_, _, err = repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "new-intent", "new-body", 2))
			require.ErrorIs(t, err, service.ErrAPIKeyRateLimit5hExceeded)
			assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
		})
	}
}

func TestVideoBudgetCaptureReplacesReservationAndReleaseRemovesIt(t *testing.T) {
	for _, action := range []service.BalanceSettlementAction{service.BalanceSettlementCapture, service.BalanceSettlementRelease} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 10, rate_limit_5h = 10 WHERE id = $1`, key.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id, platform, daily_limit_usd) VALUES ($1, 'openai', 10)`, user.ID)
			require.NoError(t, err)
			task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "settled", "settled-body", 4))
			require.NoError(t, err)
			actual := 0.0
			if action == service.BalanceSettlementCapture {
				actual = 2
			}
			settleVideoBudgetTask(t, repo, task, action, actual)
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "next", "next-body", 10-actual)
			_, created, err := repo.CreateHeldVideoTask(ctx, params)
			require.NoError(t, err)
			require.True(t, created)
			params.PublicID, params.IdempotencyKey = service.NewVideoTaskID(), "over"
			params.HoldAmount = 1
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.ErrorIs(t, err, service.ErrAPIKeyQuotaExhausted)
			assertVideoBudgetTotals(t, user.ID, 2, 90, 10-actual)
		})
	}
}

func TestVideoBudgetReplayDoesNotNeedNewBudgetAndCannotBypassSettlement(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 2)
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 2 WHERE id = $1`, key.ID)
	require.NoError(t, err)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "replay", "body", 2)
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	replayed, created, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, task.ID, replayed.ID)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{BillingState: service.VideoBillingReleasePending})
	require.NoError(t, err)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{BillingState: service.VideoBillingReleased})
	require.ErrorIs(t, err, service.ErrVideoInvalidTransition)
	assertVideoBudgetTotals(t, user.ID, 1, 0, 2)
}

func TestVideoBudgetRejectsStaleIdentityBeforeHold(t *testing.T) {
	for _, test := range []struct {
		name, statement string
		userTarget      bool
		want            error
	}{
		{"disabled-user", `UPDATE users SET status = 'inactive' WHERE id = $1`, true, service.ErrVideoInvalidRequest},
		{"disabled-key", `UPDATE api_keys SET status = 'inactive' WHERE id = $1`, false, service.ErrVideoInvalidRequest},
		{"expired-key", `UPDATE api_keys SET expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, false, service.ErrAPIKeyExpired},
		{"exhausted-key", `UPDATE api_keys SET status = 'quota_exhausted' WHERE id = $1`, false, service.ErrAPIKeyQuotaExhausted},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			target := key.ID
			if test.userTarget {
				target = user.ID
			}
			_, err := integrationDB.ExecContext(context.Background(), test.statement, target)
			require.NoError(t, err)
			_, _, err = repo.CreateHeldVideoTask(context.Background(), videoCreateParams(user, key, account, service.NewVideoTaskID(), "stale", "body", 1))
			require.ErrorIs(t, err, test.want)
			assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
		})
	}
}

func TestVideoBudgetConcurrentSettlementAndCreation(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	extraAccount := accountEntityToService(newOwnershipTestAccount(t, nil, map[string]any{"api_key": uuid.NewString()}))
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 10 WHERE id = $1`, key.ID)
	require.NoError(t, err)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "capture-old", "old-body", 6))
	require.NoError(t, err)
	settlement, usage := prepareVideoBudgetSettlement(t, repo, task, service.BalanceSettlementCapture, 2)
	const callers = 12
	results := make(chan error, callers)
	settled := make(chan error, 1)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		result, err := repo.billing.SettleVideoBalance(callCtx, settlement, usage)
		if err == nil {
			err = repo.billing.AcknowledgeVideoBalanceSettlement(callCtx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID)
		}
		settled <- err
	}()
	for index := 0; index < callers; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			selected := account
			if index%2 == 0 {
				selected = extraAccount
			}
			_, _, err := repo.CreateHeldVideoTask(callCtx, videoCreateParams(user, key, selected, service.NewVideoTaskID(), fmt.Sprint(index), fmt.Sprint(index), 4))
			results <- err
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)
	require.NoError(t, <-settled)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, service.ErrAPIKeyQuotaExhausted)
		}
	}
	require.GreaterOrEqual(t, accepted, 1)
	require.LessOrEqual(t, accepted, 2)
	assertVideoBudgetTotals(t, user.ID, accepted+1, 98-float64(accepted)*4, float64(accepted)*4)
	var spent float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT quota_used FROM api_keys WHERE id = $1`, key.ID).Scan(&spent))
	require.Equal(t, 2.0, spent)
	require.LessOrEqual(t, spent+float64(accepted)*4, 10.0)
}

func TestVideoBudgetSnapshotIsConsistentAndDoesNotExposeCredentials(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	loader := NewAPIKeyRepository(testEntClient(t), integrationDB).(service.VideoBudgetSnapshotLoader)
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 10, rate_limit_5h = 5 WHERE id = $1`, key.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id, platform, daily_limit_usd) VALUES ($1, 'openai', 10)`, user.ID)
	require.NoError(t, err)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "snapshot", "body", 4))
	require.NoError(t, err)
	snapshot, err := loader.GetVideoBudgetSnapshot(ctx, user.ID, key.ID, "openai")
	require.NoError(t, err)
	require.Equal(t, 4.0, snapshot.KeyReserved)
	require.Equal(t, 4.0, snapshot.PlatformReserved)
	require.Equal(t, 10.0, *snapshot.Platform.DailyLimitUSD)
	require.Empty(t, snapshot.APIKey.Key)
	require.Zero(t, snapshot.APIKey.QuotaUsed)
	require.Zero(t, snapshot.Platform.DailyUsageUSD)
	settleVideoBudgetTask(t, repo, task, service.BalanceSettlementCapture, 2)
	snapshot, err = loader.GetVideoBudgetSnapshot(ctx, user.ID, key.ID, "openai")
	require.NoError(t, err)
	require.Zero(t, snapshot.KeyReserved)
	require.Zero(t, snapshot.PlatformReserved)
	require.Equal(t, 2.0, snapshot.APIKey.QuotaUsed)
	require.Equal(t, 2.0, snapshot.APIKey.Usage5h)
	require.Equal(t, 2.0, snapshot.Platform.DailyUsageUSD)
	_, err = loader.GetVideoBudgetSnapshot(ctx, user.ID+100, key.ID, "openai")
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
}

func TestVideoBudgetInvalidDatabaseNumbersFailClosed(t *testing.T) {
	for _, scope := range []string{"key", "platform", "balance", "hold"} {
		t.Run(scope, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			var err error
			switch scope {
			case "key":
				_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 'NaN'::numeric WHERE id = $1`, key.ID)
			case "platform":
				_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id, platform, daily_limit_usd) VALUES ($1, 'openai', 'NaN'::numeric)`, user.ID)
			case "balance":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance = 'NaN'::numeric WHERE id = $1`, user.ID)
			}
			require.NoError(t, err)
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "invalid", "body", 1)
			expected := service.ErrBillingServiceUnavailable
			if scope == "hold" {
				params.HoldAmount = math.NaN()
				expected = service.ErrVideoInvalidRequest
			}
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.ErrorIs(t, err, expected)
		})
	}
}

func TestVideoBudgetCaptureOverHoldKeepsRealCostAndExhaustsBudget(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 4)
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 4 WHERE id = $1`, key.ID)
	require.NoError(t, err)
	task, _, err := repo.CreateHeldVideoTask(ctx, videoCreateParams(user, key, account, service.NewVideoTaskID(), "overdraft", "body", 2))
	require.NoError(t, err)
	settleVideoBudgetTask(t, repo, task, service.BalanceSettlementCapture, 5)
	assertVideoBudgetTotals(t, user.ID, 1, -1, 0)
	var used float64
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT quota_used, status FROM api_keys WHERE id = $1`, key.ID).Scan(&used, &status))
	require.Equal(t, 5.0, used)
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)
	loaded, err := repo.GetVideoTaskForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingCaptured, loaded.BillingState)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "next", "body", 1)
	_, _, err = repo.CreateHeldVideoTask(ctx, params)
	require.ErrorIs(t, err, service.ErrVideoInsufficientBalance)
}

func TestVideoBudgetGroupPermissionsRecheckedFromDatabase(t *testing.T) {
	for _, mode := range []string{"exclusive", "restricted-public", "vip"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
			var groupID int64
			err := integrationDB.QueryRowContext(ctx, `INSERT INTO groups(name, platform, is_exclusive, vip_only) VALUES ($1, 'openai', $2, $3) RETURNING id`, "budget-group-"+uuid.NewString(), mode == "exclusive", mode == "vip").Scan(&groupID)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE id = $1`, groupID)
				require.NoError(t, err)
			})
			_, err = integrationDB.ExecContext(ctx, `UPDATE users SET restrict_public_groups = $2 WHERE id = $1`, user.ID, mode == "restricted-public")
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET group_id = $2 WHERE id = $1`, key.ID, groupID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `INSERT INTO account_groups(account_id, group_id) VALUES ($1, $2)`, account.ID, groupID)
			require.NoError(t, err)
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "permission", "body", 2)
			params.Owner.GroupID = &groupID
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.Error(t, err)
			assertVideoBudgetTotals(t, user.ID, 0, 100, 0)
			if mode == "vip" {
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET is_vip = true, vip_manual_override = true,
					vip_override_at = NOW(), vip_override_by = id, vip_override_reason = 'video budget integration fixture',
					vip_granted_at = NOW(), vip_effective_source = 'manual_on' WHERE id = $1`, user.ID)
			} else {
				_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id, group_id) VALUES ($1, $2)`, user.ID, groupID)
			}
			require.NoError(t, err)
			task, created, err := repo.CreateHeldVideoTask(ctx, params)
			require.NoError(t, err)
			require.True(t, created)
			if mode == "vip" {
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET is_vip = false, vip_manual_override = false,
					vip_granted_at = NULL, vip_effective_source = 'manual_off' WHERE id = $1`, user.ID)
			} else {
				_, err = integrationDB.ExecContext(ctx, `DELETE FROM user_allowed_groups WHERE user_id = $1 AND group_id = $2`, user.ID, groupID)
			}
			require.NoError(t, err)
			params.PublicID, params.IdempotencyKey = service.NewVideoTaskID(), "revoked"
			_, _, err = repo.CreateHeldVideoTask(ctx, params)
			require.Error(t, err)
			_, err = repo.GetVideoTaskForOwner(ctx, user.ID, task.PublicID)
			require.NoError(t, err)
			assertVideoBudgetTotals(t, user.ID, 1, 98, 2)
		})
	}
}

func newVideoBudgetKey(t *testing.T, userID int64) *service.APIKey {
	t.Helper()
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: userID, Key: "sk-budget-" + uuid.NewString(), Name: "budget-other-key"})
	t.Cleanup(func() {
		for _, table := range []string{"usage_billing_outbox", "usage_billing_dedup", "usage_billing_dedup_archive"} {
			_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM `+table+` WHERE api_key_id = $1`, key.ID)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM api_keys WHERE id = $1`, key.ID)
		require.NoError(t, err)
	})
	return key
}

func assertVideoBudgetTotals(t *testing.T, userID int64, tasks int, balance, frozen float64) {
	t.Helper()
	var count int
	var actualBalance, actualFrozen float64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM video_tasks WHERE user_id = $1`, userID).Scan(&count))
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT balance, frozen_balance FROM users WHERE id = $1`, userID).Scan(&actualBalance, &actualFrozen))
	require.Equal(t, tasks, count)
	require.InDelta(t, balance, actualBalance, 0.00000001)
	require.InDelta(t, frozen, actualFrozen, 0.00000001)
}

func settleVideoBudgetTask(t *testing.T, repo *videoTaskRepository, task *service.VideoTask, action service.BalanceSettlementAction, actual float64) {
	t.Helper()
	settlement, usage := prepareVideoBudgetSettlement(t, repo, task, action, actual)
	result, err := repo.billing.SettleVideoBalance(context.Background(), settlement, usage)
	require.NoError(t, err)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(context.Background(), result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
}

func prepareVideoBudgetSettlement(t *testing.T, repo *videoTaskRepository, task *service.VideoTask, action service.BalanceSettlementAction, actual float64) (*service.BalanceSettlementCommand, *service.UsageLog) {
	t.Helper()
	ctx := context.Background()
	state, requestID := service.VideoBillingReleasePending, service.VideoTaskReleaseRequestID(task.PublicID)
	if action == service.BalanceSettlementCapture {
		state, requestID = service.VideoBillingCapturePending, service.VideoTaskCaptureRequestID(task.PublicID)
	}
	_, err := repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
	require.NoError(t, err)
	_, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID, service.VideoTaskTransition{GenerationState: service.VideoGenerationFailed, BillingState: state})
	require.NoError(t, err)
	settlement := &service.BalanceSettlementCommand{TaskID: task.ID, Action: action, Hold: service.BalanceHoldCommand{
		RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID,
		Scope: service.BalanceHoldScopeVideoTask, RefID: task.PublicID, HoldAmount: *task.HoldAmount, ActualAmount: actual,
	}}
	var usage *service.UsageLog
	if action == service.BalanceSettlementCapture {
		now := time.Now().UTC()
		settlement.Billing = &service.UsageBillingCommand{
			RequestID: requestID, APIKeyID: *task.APIKeyID, UserID: task.UserID, AccountID: *task.AccountID,
			AccountType: service.AccountTypeAPIKey, Model: task.UpstreamModel, BillingType: service.BillingTypeBalance,
			ActualCost: actual, TotalCost: actual, APIKeyQuotaCost: actual, APIKeyRateLimitCost: actual,
			Platform: task.Provider, PlatformQuotaCost: actual, OccurredAt: now,
		}
		usage = &service.UsageLog{UserID: task.UserID, APIKeyID: *task.APIKeyID, AccountID: *task.AccountID,
			RequestID: requestID, Model: task.UpstreamModel, BillingType: service.BillingTypeBalance,
			ActualCost: actual, TotalCost: actual, CreatedAt: now, VideoCount: 1}
	}
	return settlement, usage
}
