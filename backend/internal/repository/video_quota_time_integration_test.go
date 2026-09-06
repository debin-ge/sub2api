//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newVideoQuotaTimeFixture(t *testing.T, occurredAt time.Time) (*videoTaskRepository, *service.VideoTask, *service.BalanceSettlementCommand, *service.UsageLog) {
	t.Helper()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "quota-time", "quota-time-body", 4)
	task, settlement, usage := createVideoQuotaTimeFixtureTask(t, repo, params, occurredAt)
	return repo, task, settlement, usage
}

func createVideoQuotaTimeFixtureTask(t *testing.T, repo *videoTaskRepository, params service.VideoCreateTaskParams, occurredAt time.Time) (*service.VideoTask, *service.BalanceSettlementCommand, *service.UsageLog) {
	t.Helper()
	ctx := context.Background()
	params.PriceSnapshot["quota_time_contract_version"], params.PriceSnapshot["quota_time_zone"] = 1, "UTC"
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET created_at = $2, finished_at = $3 WHERE id = $1`, task.ID, occurredAt.Add(-time.Hour), occurredAt)
	require.NoError(t, err)
	settlement, usage := prepareVideoBudgetSettlement(t, repo, task, service.BalanceSettlementCapture, 3)
	task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
	require.NoError(t, err)
	settlement.Billing.MediaType, settlement.Billing.OccurredAt = "video", *task.FinishedAt
	settlement.Billing.QuotaTime = &service.UsageBillingQuotaTime{
		Version: 1, TimeZone: "UTC", DayStart: timezone.StartOfDay(*task.FinishedAt), WeekStart: timezone.StartOfWeek(*task.FinishedAt),
	}
	usage.CreatedAt = *task.FinishedAt
	return task, settlement, usage
}

func TestVideoQuotaTimeLateSettlementPostsToCurrentWindows(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, now.AddDate(0, 0, -45))
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota_used = 5, usage_5h = 7, usage_1d = 8, usage_7d = 9,
		window_5h_start = $2, window_1d_start = $2, window_7d_start = $2 WHERE id = $1`, *task.APIKeyID, now)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_platform_quotas (user_id, platform,
		daily_usage_usd, weekly_usage_usd, monthly_usage_usd, daily_window_start, weekly_window_start, monthly_window_start)
		VALUES ($1, 'openai', 7, 8, 9, $2, $3, $4)`, task.UserID, timezone.StartOfDay(now), timezone.StartOfWeek(now), now)
	require.NoError(t, err)
	result, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	require.True(t, result.Applied)
	var total, usage5h, usage1d, usage7d float64
	var start5h, start1d, start7d time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT quota_used, usage_5h, usage_1d, usage_7d,
		window_5h_start, window_1d_start, window_7d_start FROM api_keys WHERE id = $1`, *task.APIKeyID).
		Scan(&total, &usage5h, &usage1d, &usage7d, &start5h, &start1d, &start7d))
	require.Equal(t, []float64{8, 10, 11, 12}, []float64{total, usage5h, usage1d, usage7d})
	for _, start := range []time.Time{start5h, start1d, start7d} {
		require.True(t, now.Equal(start))
	}
	var daily, weekly, monthly float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd
		FROM user_platform_quotas WHERE user_id = $1 AND platform = 'openai'`, task.UserID).Scan(&daily, &weekly, &monthly))
	require.Equal(t, []float64{10, 11, 12}, []float64{daily, weekly, monthly})
	require.NotNil(t, result.QuotaPostedAt)
	require.False(t, result.QuotaPostedAt.Before(now))
	var settledAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT settled_at FROM video_tasks WHERE id = $1`, task.ID).Scan(&settledAt))
	require.True(t, result.QuotaPostedAt.Equal(settledAt))
	assertVideoBudgetTotals(t, task.UserID, 1, 97, 0)
	var recordedAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT created_at FROM usage_logs WHERE api_key_id = $1`, *task.APIKeyID).Scan(&recordedAt))
	require.True(t, usage.CreatedAt.Equal(recordedAt))
	var version int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT payload_version FROM usage_billing_outbox WHERE id = $1`, result.OutboxReceipt.ID).Scan(&version))
	require.Equal(t, usageBillingOutboxPayloadVersionV3, version)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
}

func TestVideoQuotaTimeKeyWindowBoundaries(t *testing.T) {
	for _, state := range []string{"missing", "expired", "within"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			occurredAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
			repo, task, settlement, usage := newVideoQuotaTimeFixture(t, occurredAt)
			want := 3.0
			want5h, want1d, want7d := occurredAt, timezone.StartOfDay(occurredAt), timezone.StartOfDay(occurredAt)
			switch state {
			case "missing":
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h = 99, usage_1d = 99, usage_7d = 99 WHERE id = $1`, *task.APIKeyID)
				require.NoError(t, err)
			case "expired":
				padding := time.Hour
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h = 99, usage_1d = 99, usage_7d = 99,
					window_5h_start = $2, window_1d_start = $3, window_7d_start = $4 WHERE id = $1`, *task.APIKeyID,
					occurredAt.Add(-5*time.Hour-padding), occurredAt.Add(-24*time.Hour-padding), occurredAt.Add(-7*24*time.Hour-padding))
				require.NoError(t, err)
			case "within":
				want = 10
				start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
				want5h, want1d, want7d = start, start, start
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h = 7, usage_1d = 7, usage_7d = 7,
					window_5h_start = $2, window_1d_start = $2, window_7d_start = $2 WHERE id = $1`, *task.APIKeyID, want5h)
				require.NoError(t, err)
			}
			result, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
			require.NoError(t, err)
			require.NotNil(t, result.QuotaPostedAt)
			if state != "within" {
				want5h = *result.QuotaPostedAt
				want1d, want7d = timezone.StartOfDay(want5h), timezone.StartOfDay(want5h)
			}
			var usage5h, usage1d, usage7d float64
			var start5h, start1d, start7d time.Time
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_5h, usage_1d, usage_7d, window_5h_start, window_1d_start, window_7d_start
				FROM api_keys WHERE id = $1`, *task.APIKeyID).Scan(&usage5h, &usage1d, &usage7d, &start5h, &start1d, &start7d))
			require.Equal(t, []float64{want, want, want}, []float64{usage5h, usage1d, usage7d})
			require.True(t, want5h.Equal(start5h))
			require.True(t, want1d.Equal(start1d))
			require.True(t, want7d.Equal(start7d))
			assertVideoBudgetTotals(t, task.UserID, 1, 97, 0)
			require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
		})
	}
}

func TestVideoQuotaTimeRecoveryKeepsFrozenClockAndAppliesOnce(t *testing.T) {
	ctx := context.Background()
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, time.Now().UTC().Add(-48*time.Hour))
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	event, err := repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "quota-before-crash", settlement.Hold.RequestID,
		*task.APIKeyID, settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV3, commandJSON, usageJSON)
	require.NoError(t, err)
	require.NoError(t, repo.billing.RetryUsageBillingOutbox(ctx, "quota-before-crash", event.ID, time.Now().Add(-time.Minute), "simulated restart"))
	events, err := repo.billing.ClaimUsageBillingOutbox(ctx, "quota-recovery", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, usageBillingOutboxPayloadVersionV3, events[0].PayloadVersion)
	require.True(t, events[0].Command.OccurredAt.Equal(settlement.Billing.OccurredAt))
	events[0].Command.PlatformQuotaSnapshot = &service.UsageBillingPlatformQuotaSnapshot{}
	require.NoError(t, repo.billing.UpdateUsageBillingOutboxCommand(ctx, "quota-recovery", event.ID, events[0].Command))
	changed := *events[0].Command
	changed.OccurredAt = changed.OccurredAt.Add(time.Minute)
	require.ErrorIs(t, repo.billing.UpdateUsageBillingOutboxCommand(ctx, "quota-recovery", event.ID, &changed), service.ErrUsageBillingRequestConflict)
	first, err := repo.billing.CompleteUsageBillingOutbox(ctx, "quota-recovery", events[0])
	require.NoError(t, err)
	require.True(t, first.Applied)
	second, err := repo.billing.CompleteUsageBillingOutbox(ctx, "quota-recovery", events[0])
	require.NoError(t, err)
	require.Equal(t, first.NewBalance, second.NewBalance)
	require.NotNil(t, first.QuotaPostedAt)
	require.Equal(t, first.QuotaPostedAt, second.QuotaPostedAt)
	assertVideoBudgetTotals(t, task.UserID, 1, 97, 0)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE api_key_id = $1`, *task.APIKeyID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, "quota-recovery", event.ID))
}

func TestVideoQuotaTimeReorderedCompletionsCannotEscapeRollingBudgets(t *testing.T) {
	ctx := context.Background()
	observed := time.Now().UTC().Add(-3 * time.Hour)
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, observed)
	settlements := []*service.BalanceSettlementCommand{settlement}
	logs := []*service.UsageLog{usage}
	for offset := 1; offset < 3; offset++ {
		publicID := service.NewVideoTaskID()
		params := videoCreateParams(&service.User{ID: task.UserID}, &service.APIKey{ID: *task.APIKeyID},
			&service.Account{ID: *task.AccountID}, publicID, publicID, publicID, 4)
		_, nextSettlement, nextUsage := createVideoQuotaTimeFixtureTask(t, repo, params, observed.Add(time.Duration(offset)*time.Hour))
		settlements, logs = append(settlements, nextSettlement), append(logs, nextUsage)
	}
	var lastPosted time.Time
	for _, index := range []int{2, 0, 1} {
		result, err := repo.billing.SettleVideoBalance(ctx, settlements[index], logs[index])
		require.NoError(t, err)
		require.NotNil(t, result.QuotaPostedAt)
		require.False(t, result.QuotaPostedAt.Before(lastPosted))
		lastPosted = *result.QuotaPostedAt
		require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
	}
	var usage5h, monthly float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_5h FROM api_keys WHERE id = $1`, *task.APIKeyID).Scan(&usage5h))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT monthly_usage_usd FROM user_platform_quotas WHERE user_id = $1 AND platform = 'openai'`, task.UserID).Scan(&monthly))
	require.Equal(t, 9.0, usage5h)
	require.Equal(t, 9.0, monthly)
	assertVideoBudgetTotals(t, task.UserID, 3, 91, 0)
}

func TestVideoQuotaTimeSamplesDatabaseClockAfterBudgetLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, time.Now().UTC().Add(-time.Hour))
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `SELECT id FROM api_keys WHERE id = $1 FOR UPDATE`, *task.APIKeyID)
	require.NoError(t, err)
	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
		completed <- outcome{result: result, err: err}
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'SELECT window_5h_start, window_1d_start, window_7d_start FROM api_keys%'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 5*time.Second, 10*time.Millisecond)
	var releasedAt time.Time
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&releasedAt))
	require.NoError(t, blocker.Commit())
	result := <-completed
	require.NoError(t, result.err)
	require.NotNil(t, result.result.QuotaPostedAt)
	require.False(t, result.result.QuotaPostedAt.Before(releasedAt))
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.result.OutboxReceipt.WorkerID, result.result.OutboxReceipt.ID))
}

func TestVideoQuotaTimeRolledBackAttemptDoesNotFreezePostingTime(t *testing.T) {
	ctx := context.Background()
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, time.Now().UTC().Add(-48*time.Hour))
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota_used = 999999999999.99999999 WHERE id = $1`, *task.APIKeyID)
	require.NoError(t, err)
	_, err = repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.Error(t, err)
	assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
	var stage int
	var resultMissing bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT stage, result_payload IS NULL
		FROM usage_billing_outbox WHERE request_id = $1`, settlement.Hold.RequestID).Scan(&stage, &resultMissing))
	require.Equal(t, int(usageBillingOutboxStageBilling), stage)
	require.True(t, resultMissing)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota_used = 0 WHERE id = $1`, *task.APIKeyID)
	require.NoError(t, err)
	var repairedAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&repairedAt))
	result, command, found, err := repo.billing.ResumeVideoBalanceSettlement(ctx, task)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, command.OccurredAt.Equal(*task.FinishedAt))
	require.NotNil(t, result.QuotaPostedAt)
	require.False(t, result.QuotaPostedAt.Before(repairedAt))
	assertVideoBudgetTotals(t, task.UserID, 1, 97, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
}

func TestVideoQuotaTimeMigrationFencesLegacyWritersAndTimeMutation(t *testing.T) {
	ctx := context.Background()
	repo, task, settlement, usage := newVideoQuotaTimeFixture(t, time.Now().UTC().Add(-time.Hour))
	for _, statement := range []string{
		`UPDATE video_tasks SET finished_at = finished_at + INTERVAL '1 minute' WHERE id = $1`,
		`UPDATE video_tasks SET price_snapshot = price_snapshot - 'quota_time_contract_version' WHERE id = $1`,
		`UPDATE video_tasks SET price_snapshot = jsonb_set(price_snapshot, '{quota_time_zone}', '"Asia/Shanghai"') WHERE id = $1`,
	} {
		_, err := integrationDB.ExecContext(ctx, statement, task.ID)
		require.ErrorContains(t, err, "video quota time snapshot is immutable")
	}
	clock := settlement.Billing.QuotaTime
	settlement.Billing.QuotaTime = nil
	_, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.ErrorContains(t, err, "outbox v3")
	assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
	settlement.Billing.QuotaTime = clock
	commandJSON, usageJSON, err := marshalBalanceSettlementOutboxPayload(settlement, usage)
	require.NoError(t, err)
	var payload balanceSettlementPayloadV2
	require.NoError(t, json.Unmarshal(commandJSON, &payload))
	payload.Billing.OccurredAt = payload.Billing.OccurredAt.Add(time.Minute)
	corrupted, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = repo.billing.enqueueAndClaimUsageBillingOutboxPayload(ctx, "clock-corruption", settlement.Hold.RequestID,
		*task.APIKeyID, settlement.Hold.RequestFingerprint, usageBillingOutboxPayloadVersionV3, corrupted, usageJSON)
	require.ErrorContains(t, err, "frozen terminal event")
	assertVideoBudgetTotals(t, task.UserID, 1, 96, 4)
	result, err := repo.billing.SettleVideoBalance(ctx, settlement, usage)
	require.NoError(t, err)
	require.True(t, result.Applied)
	assertVideoBudgetTotals(t, task.UserID, 1, 97, 0)
	require.NoError(t, repo.billing.AcknowledgeVideoBalanceSettlement(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID))
}

func TestVideoQuotaTimeDefaultSchedulingUsesDatabaseCreationTime(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "database-due", "database-due-body", 1)
	task, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, task.NextActionAt)
	require.True(t, task.CreatedAt.Equal(*task.NextActionAt))
	claimed, err := repo.ClaimVideoTask(ctx, task.PublicID, "database-clock", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	params.PublicID, params.IdempotencyKey, params.NextActionAt = service.NewVideoTaskID(), "explicit-deadline", &deadline
	future, _, err := repo.CreateHeldVideoTask(ctx, params)
	require.NoError(t, err)
	require.True(t, deadline.Equal(*future.NextActionAt))
	claimed, err = repo.ClaimVideoTask(ctx, future.PublicID, "too-early", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed)
}

func TestVideoQuotaTimeKeyBoundaryAndDSTDoNotResetRepeatedly(t *testing.T) {
	for _, test := range []struct {
		name, zone string
		postedAt   time.Time
	}{
		{"exact_boundary", "UTC", time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)},
		{"25_hour_day", "America/Los_Angeles", time.Date(2026, 11, 2, 7, 30, 0, 0, time.UTC)},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			_, _, _, _, key, _ := newVideoRepositoryFixture(t, 100)
			clock, err := service.ResolveUsageBillingQuotaTime(test.postedAt, test.zone)
			require.NoError(t, err)
			start1d := test.postedAt.Add(-24 * time.Hour)
			if test.name == "25_hour_day" {
				start1d = clock.DayStart
			}
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h = 99, usage_1d = 99, usage_7d = 99,
				window_5h_start = $2, window_1d_start = $3, window_7d_start = $4 WHERE id = $1`, key.ID,
				test.postedAt.Add(-5*time.Hour), start1d, test.postedAt.Add(-7*24*time.Hour))
			require.NoError(t, err)
			transaction, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = transaction.Rollback() }()
			command := &service.UsageBillingCommand{APIKeyID: key.ID, APIKeyRateLimitCost: 3, MediaType: "video",
				OccurredAt: test.postedAt, QuotaTime: clock}
			require.NoError(t, command.ValidateQuotaTime())
			require.NoError(t, incrementUsageBillingAPIKeyQuotaAtEvent(ctx, transaction, command))
			command.OccurredAt = command.OccurredAt.Add(time.Minute)
			require.NoError(t, incrementUsageBillingAPIKeyQuotaAtEvent(ctx, transaction, command))
			require.NoError(t, transaction.Commit())
			var usage5h, usage1d, usage7d float64
			var anchor1d time.Time
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_5h, usage_1d, usage_7d, window_1d_start
				FROM api_keys WHERE id = $1`, key.ID).Scan(&usage5h, &usage1d, &usage7d, &anchor1d))
			require.Equal(t, []float64{6, 6, 6}, []float64{usage5h, usage1d, usage7d})
			if test.name == "25_hour_day" {
				require.True(t, test.postedAt.Equal(anchor1d))
			} else {
				require.True(t, clock.DayStart.Equal(anchor1d))
			}
		})
	}
}
