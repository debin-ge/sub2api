//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLog_ListWithFilters_ResolvesSoftDeletedUser(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	// 一个活跃用户、一个将被软删的用户，各一条日志。
	active := mustCreateUser(t, client, &service.User{Email: "active-listfilter@test.com"})
	deleted := mustCreateUser(t, client, &service.User{Email: "deleted-listfilter@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: deleted.ID, Key: "sk-del-1", Name: "k"})
	apiKey2 := mustCreateApiKey(t, client, &service.APIKey{UserID: active.ID, Key: "sk-act-1", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "acc-listfilter"})

	now := time.Now().UTC()
	for _, u := range []struct {
		uid int64
		kid int64
	}{{deleted.ID, apiKey.ID}, {active.ID, apiKey2.ID}} {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: u.uid, APIKeyID: u.kid, AccountID: account.ID,
			Model: "claude-3", InputTokens: 1, OutputTokens: 1,
			TotalCost: 0.1, ActualCost: 0.1, CreatedAt: now,
		})
		require.NoError(t, err)
	}

	// 软删除该用户（触发 SoftDeleteMixin Hook → UPDATE deleted_at）。
	require.NoError(t, client.User.DeleteOneID(deleted.ID).Exec(ctx))

	logs, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 50},
		usagestats.UsageLogFilters{ExactTotal: true})
	require.NoError(t, err)

	byUser := map[int64]service.UsageLog{}
	for _, l := range logs {
		byUser[l.UserID] = l
	}

	// 已删用户的日志行：富化后 User 非 nil、邮箱正确、DeletedAt 非 nil。
	delLog, ok := byUser[deleted.ID]
	require.True(t, ok, "deleted user's usage log must still be listed")
	require.NotNil(t, delLog.User, "deleted user identity must resolve")
	require.Equal(t, "deleted-listfilter@test.com", delLog.User.Email)
	require.NotNil(t, delLog.User.DeletedAt, "DeletedAt must be set for soft-deleted user")

	// 活跃用户：DeletedAt 为 nil。
	actLog := byUser[active.ID]
	require.NotNil(t, actLog.User)
	require.Nil(t, actLog.User.DeletedAt)
}

func TestUsageLog_GetAccountModelBreakdownBatch_IncludesSoftDeletedUserHistory(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "deleted-breakdown@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-breakdown-del-1", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "acc-breakdown-deleted-user"})
	accountMultiplier := 1.5
	now := time.Now().UTC()
	inserted, err := repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: "req-breakdown-deleted-user", Model: "claude-raw-model",
		InputTokens: 1, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4,
		TotalCost: 2, ActualCost: 0, AccountRateMultiplier: &accountMultiplier, CreatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, client.User.DeleteOneID(user.ID).Exec(ctx))

	got, err := repo.GetAccountModelBreakdownBatch(ctx, []int64{account.ID}, now.Add(-time.Minute))
	require.NoError(t, err)
	require.Equal(t, service.ModelCostStats{
		Requests:    1,
		Tokens:      10,
		AccountCost: 3,
	}, got[account.ID]["claude-raw-model"])
}

func TestUsageLog_GetAccountModelBreakdownBatch_AggregatesPostgresBoundaries(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "breakdown-boundaries@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-breakdown-boundaries", Name: "k"})
	accountA := mustCreateAccount(t, client, &service.Account{Name: "acc-breakdown-a"})
	accountB := mustCreateAccount(t, client, &service.Account{Name: "acc-breakdown-b"})
	startTime := time.Now().UTC().Truncate(time.Microsecond)

	createLog := func(requestID string, accountID int64, model string, createdAt time.Time, tokens [4]int, totalCost float64, accountStatsCost, accountMultiplier *float64) {
		t.Helper()
		inserted, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: accountID,
			RequestID: requestID, Model: model,
			InputTokens: tokens[0], OutputTokens: tokens[1], CacheCreationTokens: tokens[2], CacheReadTokens: tokens[3],
			TotalCost: totalCost, ActualCost: 0, AccountStatsCost: accountStatsCost, AccountRateMultiplier: accountMultiplier,
			CreatedAt: createdAt,
		})
		require.NoError(t, err)
		require.True(t, inserted)
	}

	accountStatsCost := 2.0
	accountMultiplier := 3.0
	zeroAccountStatsCost := 0.0
	zeroMultiplier := 2.0
	otherModelCost := 1.25
	createLog("req-breakdown-before", accountA.ID, "model-a", startTime.Add(-time.Microsecond), [4]int{99, 99, 99, 99}, 100, nil, nil)
	createLog("req-breakdown-boundary", accountA.ID, "model-a", startTime, [4]int{1, 2, 3, 4}, 100, &accountStatsCost, &accountMultiplier)
	createLog("req-breakdown-after", accountA.ID, "model-a", startTime.Add(time.Second), [4]int{5, 6, 7, 8}, 4, nil, nil)
	createLog("req-breakdown-explicit-zero", accountA.ID, "model-zero", startTime.Add(2*time.Second), [4]int{1, 1, 1, 1}, 9, &zeroAccountStatsCost, &zeroMultiplier)
	createLog("req-breakdown-big-tokens", accountB.ID, "model-big", startTime.Add(3*time.Second), [4]int{1_000_000_000, 1_000_000_000, 1_000_000_000, 1_000_000_000}, 5, nil, nil)
	createLog("req-breakdown-other-model", accountB.ID, "model-other", startTime.Add(4*time.Second), [4]int{10, 20, 30, 40}, 99, &otherModelCost, nil)

	got, err := repo.GetAccountModelBreakdownBatch(ctx, []int64{accountA.ID, accountB.ID}, startTime)
	require.NoError(t, err)
	require.Equal(t, map[int64]map[string]service.ModelCostStats{
		accountA.ID: {
			"model-a": {
				Requests:    2,
				Tokens:      36,
				AccountCost: 10,
			},
			"model-zero": {
				Requests:    1,
				Tokens:      4,
				AccountCost: 0,
			},
		},
		accountB.ID: {
			"model-big": {
				Requests:    1,
				Tokens:      4_000_000_000,
				AccountCost: 5,
			},
			"model-other": {
				Requests:    1,
				Tokens:      100,
				AccountCost: 1.25,
			},
		},
	}, got)
}

func TestUsageLog_GetAccountWindowStatsBatch_AggregatesBeyondInt32(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "window-bigint@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-window-bigint", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "acc-window-bigint"})
	startTime := time.Now().UTC().Truncate(time.Microsecond)

	inserted, err := repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: "req-window-bigint", Model: "model-big",
		InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000,
		CacheCreationTokens: 1_000_000_000, CacheReadTokens: 1_000_000_000,
		TotalCost: 5, ActualCost: 4, CreatedAt: startTime,
	})
	require.NoError(t, err)
	require.True(t, inserted)

	got, err := repo.GetAccountWindowStatsBatch(ctx, []int64{account.ID}, startTime)
	require.NoError(t, err)
	require.Equal(t, int64(4_000_000_000), got[account.ID].Tokens)
	require.Equal(t, int64(1), got[account.ID].Requests)
}
