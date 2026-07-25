//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func querySingleFloat(t *testing.T, ctx context.Context, client *dbent.Client, query string, args ...any) float64 {
	t.Helper()
	rows, err := client.QueryContext(ctx, query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	require.True(t, rows.Next(), "expected one row")
	var value float64
	require.NoError(t, rows.Scan(&value))
	require.NoError(t, rows.Err())
	return value
}

func querySingleInt(t *testing.T, ctx context.Context, client *dbent.Client, query string, args ...any) int {
	t.Helper()
	rows, err := client.QueryContext(ctx, query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	require.True(t, rows.Next(), "expected one row")
	var value int
	require.NoError(t, rows.Scan(&value))
	require.NoError(t, rows.Err())
	return value
}

func TestAffiliateRepository_TransferQuotaToBalance_UsesClaimedQuotaBeforeClear(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	u := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-transfer-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Balance:      5.5,
		Concurrency:  5,
	})

	affCode := fmt.Sprintf("AFF%09d", time.Now().UnixNano()%1_000_000_000)
	_, err := client.ExecContext(txCtx, `
INSERT INTO user_affiliates (user_id, aff_code, aff_quota, aff_history_quota, created_at, updated_at)
VALUES ($1, $2, $3, $3, NOW(), NOW())`, u.ID, affCode, 12.34)
	require.NoError(t, err)

	transferred, balance, err := repo.TransferQuotaToBalance(txCtx, u.ID)
	require.NoError(t, err)
	require.InDelta(t, 12.34, transferred, 1e-9)
	require.InDelta(t, 17.84, balance, 1e-9)

	affQuota := querySingleFloat(t, txCtx, client,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", u.ID)
	require.InDelta(t, 0.0, affQuota, 1e-9)

	persistedBalance := querySingleFloat(t, txCtx, client,
		"SELECT balance::double precision FROM users WHERE id = $1", u.ID)
	require.InDelta(t, 17.84, persistedBalance, 1e-9)

	ledgerCount := querySingleInt(t, txCtx, client,
		"SELECT COUNT(*) FROM user_affiliate_ledger WHERE user_id = $1 AND action = 'transfer'", u.ID)
	require.Equal(t, 1, ledgerCount)

	rows, err := client.QueryContext(txCtx, `
SELECT amount::double precision,
       balance_after::double precision,
       aff_quota_after::double precision,
       aff_frozen_quota_after::double precision,
       aff_history_quota_after::double precision
FROM user_affiliate_ledger
WHERE user_id = $1 AND action = 'transfer'
LIMIT 1`, u.ID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "expected transfer ledger")
	var amount, balanceAfter, quotaAfter, frozenAfter, historyAfter float64
	require.NoError(t, rows.Scan(&amount, &balanceAfter, &quotaAfter, &frozenAfter, &historyAfter))
	require.InDelta(t, 12.34, amount, 1e-9)
	require.InDelta(t, 17.84, balanceAfter, 1e-9)
	require.InDelta(t, 0.0, quotaAfter, 1e-9)
	require.InDelta(t, 0.0, frozenAfter, 1e-9)
	require.InDelta(t, 12.34, historyAfter, 1e-9)
}

// TestAffiliateRepository_AccrueQuota_ReusesOuterTransaction guards the
// cross-layer tx propagation invariant: when AccrueQuota is called with a ctx
// that already carries a transaction (via dbent.NewTxContext), repo.withTx
// must reuse that tx rather than opening a nested one. If this invariant
// breaks, AccrueQuota would commit independently and survive a rollback of
// the outer tx, which would violate payment_fulfillment's all-or-nothing
// semantics.
func TestAffiliateRepository_AccrueQuota_ReusesOuterTransaction(t *testing.T) {
	ctx := context.Background()

	outerTx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err, "begin outer tx")
	// Defensive cleanup: if any require.* below fires before the explicit
	// Rollback, this prevents the tx from leaking until container teardown.
	// Rollback is idempotent at the driver level (extra rollback returns an
	// error we ignore).
	t.Cleanup(func() { _ = outerTx.Rollback() })
	client := outerTx.Client()
	txCtx := dbent.NewTxContext(ctx, outerTx)

	inviter := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	})
	invitee := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-invitee-%d@example.com", time.Now().UnixNano()+1),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	})

	repo := NewAffiliateRepository(client, integrationDB)
	_, err = repo.EnsureUserAffiliate(txCtx, inviter.ID)
	require.NoError(t, err)
	_, err = repo.EnsureUserAffiliate(txCtx, invitee.ID)
	require.NoError(t, err)

	bound, err := repo.BindInviter(txCtx, invitee.ID, inviter.ID)
	require.NoError(t, err)
	require.True(t, bound, "invitee must bind to inviter")

	applied, err := repo.AccrueQuota(txCtx, inviter.ID, invitee.ID, 3.5, 0, 0, nil)
	require.NoError(t, err)
	require.InDelta(t, 3.5, applied, 1e-9, "AccrueQuota must report the applied amount")

	// Visible inside the outer tx.
	innerQuota := querySingleFloat(t, txCtx, client,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID)
	require.InDelta(t, 3.5, innerQuota, 1e-9)

	// Roll back the outer tx; if AccrueQuota had opened its own inner tx and
	// committed it, the rows would still be visible to the global client.
	require.NoError(t, outerTx.Rollback())

	rows, err := integrationEntClient.QueryContext(ctx,
		"SELECT COUNT(*) FROM user_affiliates WHERE user_id IN ($1, $2)",
		inviter.ID, invitee.ID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var postRollbackCount int
	require.NoError(t, rows.Scan(&postRollbackCount))
	require.Equal(t, 0, postRollbackCount,
		"AccrueQuota must propagate the outer tx — found persisted rows after rollback")
}

func TestAffiliateRepository_BindInviterAndGrantRegistrationReward_Idempotent(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewAffiliateRepository(client, integrationDB)

	inviter := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	invitee := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-invitee-%d@example.com", time.Now().UnixNano()+1),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	inviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, inviter.ID)
	require.NoError(t, err)

	first, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		invitee.ID,
		inviterAffiliate.AffCode,
		10,
		0,
		0,
	)
	require.NoError(t, err)
	require.True(t, first.Bound)
	require.True(t, first.RewardApplied)
	require.InDelta(t, 10, first.RewardAmount, 1e-9)

	retry, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		invitee.ID,
		inviterAffiliate.AffCode,
		10,
		0,
		0,
	)
	require.NoError(t, err)
	require.False(t, retry.Bound)
	require.False(t, retry.RewardApplied)

	rows, err := client.QueryContext(txCtx, `
SELECT aff_count,
       aff_quota::double precision,
       aff_frozen_quota::double precision,
       aff_history_quota::double precision
	FROM user_affiliates
	WHERE user_id = $1`, inviter.ID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var count int
	var available, frozen, history float64
	require.NoError(t, rows.Scan(&count, &available, &frozen, &history))
	require.Equal(t, 1, count)
	require.InDelta(t, 10, available, 1e-9)
	require.InDelta(t, 0, frozen, 1e-9)
	require.InDelta(t, 10, history, 1e-9)
	require.NoError(t, rows.Close())

	ledgerCount := querySingleInt(t, txCtx, client, `
SELECT COUNT(*)
FROM user_affiliate_ledger
WHERE action = 'registration_reward'
  AND source_user_id = $1`, invitee.ID)
	require.Equal(t, 1, ledgerCount)

	rechargeAccrued, err := repo.GetAccruedRebateFromInvitee(txCtx, inviter.ID, invitee.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, rechargeAccrued, 1e-9,
		"registration rewards must be included in the inviter's shared rebate total")

	invitees, err := repo.ListInvitees(txCtx, inviter.ID, 10)
	require.NoError(t, err)
	require.Len(t, invitees, 1)
	require.InDelta(t, 10, invitees[0].TotalRebate, 1e-9,
		"invitee totals must include registration rewards")

	inviteRecords, inviteRecordCount, err := repo.ListAffiliateInviteRecords(txCtx, service.AffiliateRecordFilter{
		Search:   invitee.Email,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, inviteRecordCount)
	require.Len(t, inviteRecords, 1)
	require.InDelta(t, 10, inviteRecords[0].RegistrationRewardAmount, 1e-9)
	require.InDelta(t, 10, inviteRecords[0].TotalRebate, 1e-9)

	rebateRecords, rebateRecordCount, err := repo.ListAffiliateRebateRecords(txCtx, service.AffiliateRecordFilter{
		Search:   invitee.Email,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.Zero(t, rebateRecordCount,
		"order rebate records must continue to exclude registration rewards")
	require.Empty(t, rebateRecords)

	overview, err := repo.GetAffiliateUserOverview(txCtx, inviter.ID)
	require.NoError(t, err)
	require.Equal(t, 1, overview.RebatedInviteeCount,
		"registration-reward invitees must count as rebated invitees")
}

func TestAffiliateRepository_BindInviterAndGrantRegistrationReward_RespectsInviterTotalCap(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewAffiliateRepository(client, integrationDB)

	inviter := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-cap-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	invitees := make([]*service.User, 0, 3)
	for i := range 3 {
		invitees = append(invitees, mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("registration-reward-cap-invitee-%d-%d@example.com", i, time.Now().UnixNano()),
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		}))
	}
	inviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, inviter.ID)
	require.NoError(t, err)

	first, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		invitees[0].ID,
		inviterAffiliate.AffCode,
		6,
		10,
		0,
	)
	require.NoError(t, err)
	require.True(t, first.Bound)
	require.True(t, first.RewardApplied)
	require.InDelta(t, 6, first.RewardAmount, 1e-9)

	second, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		invitees[1].ID,
		inviterAffiliate.AffCode,
		6,
		10,
		0,
	)
	require.NoError(t, err)
	require.True(t, second.Bound)
	require.True(t, second.RewardApplied)
	require.InDelta(t, 4, second.RewardAmount, 1e-9)

	third, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		invitees[2].ID,
		inviterAffiliate.AffCode,
		6,
		10,
		0,
	)
	require.NoError(t, err)
	require.True(t, third.Bound, "the invite relationship must still bind after the cap is reached")
	require.False(t, third.RewardApplied)
	require.Zero(t, third.RewardAmount)

	require.Equal(t, 3, querySingleInt(t, txCtx, client,
		"SELECT aff_count FROM user_affiliates WHERE user_id = $1", inviter.ID))
	require.InDelta(t, 10, querySingleFloat(t, txCtx, client,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.InDelta(t, 10, querySingleFloat(t, txCtx, client,
		"SELECT aff_history_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.Equal(t, 2, querySingleInt(t, txCtx, client, `
SELECT COUNT(*)
FROM user_affiliate_ledger
WHERE action = 'registration_reward'
  AND user_id = $1`, inviter.ID))

	capEligibleTotal, err := repo.GetTotalRebateForInviter(txCtx, inviter.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, capEligibleTotal, 1e-9)

	records, total, err := repo.ListAffiliateInviteRecords(txCtx, service.AffiliateRecordFilter{
		Search:   inviter.Email,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Len(t, records, 3)

	expected := map[int64]struct {
		registrationReward float64
		totalRebate        float64
	}{
		invitees[0].ID: {registrationReward: 6, totalRebate: 6},
		invitees[1].ID: {registrationReward: 4, totalRebate: 10},
		invitees[2].ID: {registrationReward: 0, totalRebate: 10},
	}
	for _, record := range records {
		want, ok := expected[record.InviteeID]
		require.True(t, ok, "unexpected invitee %d", record.InviteeID)
		require.InDelta(t, want.registrationReward, record.RegistrationRewardAmount, 1e-9)
		require.InDelta(t, want.totalRebate, record.TotalRebate, 1e-9,
			"invite record totals must stop increasing once the inviter cap is reached")
	}
}

func TestAffiliateRepository_AccrueQuota_ConcurrentInviteesRespectInviterTotalCap(t *testing.T) {
	ctx := context.Background()
	repo := NewAffiliateRepository(integrationEntClient, integrationDB)
	inviter := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        fmt.Sprintf("concurrent-cap-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	invitees := make([]*service.User, 0, 3)
	for i := range 3 {
		invitees = append(invitees, mustCreateUser(t, integrationEntClient, &service.User{
			Email:        fmt.Sprintf("concurrent-cap-invitee-%d-%d@example.com", i, time.Now().UnixNano()),
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		}))
	}
	t.Cleanup(func() {
		_, _ = integrationEntClient.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id IN ($1, $2, $3, $4)",
			invitees[0].ID,
			invitees[1].ID,
			invitees[2].ID,
			inviter.ID,
		)
	})

	inviterAffiliate, err := repo.EnsureUserAffiliate(ctx, inviter.ID)
	require.NoError(t, err)
	registrationResult, err := repo.BindInviterAndGrantRegistrationReward(
		ctx,
		invitees[0].ID,
		inviterAffiliate.AffCode,
		6,
		10,
		0,
	)
	require.NoError(t, err)
	require.InDelta(t, 6, registrationResult.RewardAmount, 1e-9)
	for _, invitee := range invitees[1:] {
		result, bindErr := repo.BindInviterAndGrantRegistrationReward(
			ctx,
			invitee.ID,
			inviterAffiliate.AffCode,
			0,
			10,
			0,
		)
		require.NoError(t, bindErr)
		require.True(t, result.Bound)
	}

	type accrueResult struct {
		amount float64
		err    error
	}
	results := make(chan accrueResult, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, invitee := range invitees[1:] {
		inviteeID := invitee.ID
		go func() {
			start.Wait()
			amount, err := repo.AccrueQuota(
				ctx,
				inviter.ID,
				inviteeID,
				3,
				10,
				0,
				nil,
			)
			results <- accrueResult{amount: amount, err: err}
		}()
	}
	start.Done()

	var concurrentTotal float64
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		concurrentTotal += result.amount
	}
	require.InDelta(t, 4, concurrentTotal, 1e-9,
		"concurrent recharge rebates may only consume the cap remaining after registration")
	require.InDelta(t, 10, querySingleFloat(t, ctx, integrationEntClient,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.InDelta(t, 10, querySingleFloat(t, ctx, integrationEntClient,
		"SELECT aff_history_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.InDelta(t, 4, querySingleFloat(t, ctx, integrationEntClient, `
SELECT COALESCE(SUM(amount), 0)::double precision
FROM user_affiliate_ledger
WHERE action = 'accrue'
  AND user_id = $1`, inviter.ID), 1e-9)

	capEligibleTotal, err := repo.GetTotalRebateForInviter(ctx, inviter.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, capEligibleTotal, 1e-9)
}

func TestAffiliateRepository_ListAffiliateInviteRecords_UsesInvitationTimeRunningTotal(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewAffiliateRepository(client, integrationDB)

	marker := fmt.Sprintf("running-total-%d", time.Now().UnixNano())
	createUser := func(label string) *service.User {
		return mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("%s-%s@example.com", marker, label),
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		})
	}

	firstInviter := createUser("inviter-a")
	firstInvitees := []*service.User{
		createUser("invitee-a1"),
		createUser("invitee-a2"),
		createUser("invitee-a3"),
	}
	secondInviter := createUser("inviter-b")
	secondInvitees := []*service.User{
		createUser("invitee-b1"),
		createUser("invitee-b2"),
	}

	firstInviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, firstInviter.ID)
	require.NoError(t, err)
	secondInviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, secondInviter.ID)
	require.NoError(t, err)

	bindReward := func(inviteeID int64, affiliateCode string, reward float64) *service.AffiliateRegistrationRewardResult {
		result, bindErr := repo.BindInviterAndGrantRegistrationReward(
			txCtx,
			inviteeID,
			affiliateCode,
			reward,
			50,
			0,
		)
		require.NoError(t, bindErr)
		require.True(t, result.Bound)
		return result
	}

	require.InDelta(t, 30, bindReward(firstInvitees[0].ID, firstInviterAffiliate.AffCode, 30).RewardAmount, 1e-9)
	require.InDelta(t, 20, bindReward(firstInvitees[1].ID, firstInviterAffiliate.AffCode, 20).RewardAmount, 1e-9)
	require.Zero(t, bindReward(firstInvitees[2].ID, firstInviterAffiliate.AffCode, 20).RewardAmount)
	require.InDelta(t, 30, bindReward(secondInvitees[0].ID, secondInviterAffiliate.AffCode, 30).RewardAmount, 1e-9)
	require.InDelta(t, 20, bindReward(secondInvitees[1].ID, secondInviterAffiliate.AffCode, 20).RewardAmount, 1e-9)

	baseTime := time.Now().Add(-time.Hour).UTC()
	invitationOrder := []*service.User{
		firstInvitees[0],
		firstInvitees[1],
		firstInvitees[2],
		secondInvitees[0],
		secondInvitees[1],
	}
	for index, invitee := range invitationOrder {
		_, err = client.ExecContext(
			txCtx,
			"UPDATE user_affiliates SET created_at = $1 WHERE user_id = $2",
			baseTime.Add(time.Duration(index)*time.Minute),
			invitee.ID,
		)
		require.NoError(t, err)
	}

	records, total, err := repo.ListAffiliateInviteRecords(txCtx, service.AffiliateRecordFilter{
		Search:   marker,
		Page:     1,
		PageSize: 10,
		SortBy:   "created_at",
		SortDesc: true,
	})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)
	require.Len(t, records, 5)

	expectedInviteeIDs := []int64{
		secondInvitees[1].ID,
		secondInvitees[0].ID,
		firstInvitees[2].ID,
		firstInvitees[1].ID,
		firstInvitees[0].ID,
	}
	expectedRegistrationRewards := []float64{20, 30, 0, 20, 30}
	expectedRunningTotals := []float64{50, 30, 50, 50, 30}
	for index, record := range records {
		require.Equal(t, expectedInviteeIDs[index], record.InviteeID)
		require.InDelta(t, expectedRegistrationRewards[index], record.RegistrationRewardAmount, 1e-9)
		require.InDelta(t, expectedRunningTotals[index], record.TotalRebate, 1e-9,
			"each inviter must have an independent invitation-time running total")
	}

	filteredRecords, filteredTotal, err := repo.ListAffiliateInviteRecords(txCtx, service.AffiliateRecordFilter{
		Search:   secondInvitees[1].Email,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, filteredTotal)
	require.Len(t, filteredRecords, 1)
	require.InDelta(t, 50, filteredRecords[0].TotalRebate, 1e-9,
		"filtering must not truncate the earlier invitations included in the running total")
}

func TestAffiliateRepository_BindInviterAndGrantRegistrationReward_FrozenAndZero(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewAffiliateRepository(client, integrationDB)

	inviter := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-frozen-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	frozenInvitee := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-frozen-invitee-%d@example.com", time.Now().UnixNano()+1),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	zeroInvitee := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-zero-invitee-%d@example.com", time.Now().UnixNano()+2),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	inviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, inviter.ID)
	require.NoError(t, err)

	frozenResult, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		frozenInvitee.ID,
		inviterAffiliate.AffCode,
		7.25,
		0,
		24,
	)
	require.NoError(t, err)
	require.True(t, frozenResult.RewardApplied)

	zeroResult, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		zeroInvitee.ID,
		inviterAffiliate.AffCode,
		0,
		0,
		24,
	)
	require.NoError(t, err)
	require.True(t, zeroResult.Bound)
	require.False(t, zeroResult.RewardApplied)

	require.InDelta(t, 0, querySingleFloat(t, txCtx, client,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.InDelta(t, 7.25, querySingleFloat(t, txCtx, client,
		"SELECT aff_frozen_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.InDelta(t, 7.25, querySingleFloat(t, txCtx, client,
		"SELECT aff_history_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.Equal(t, 2, querySingleInt(t, txCtx, client,
		"SELECT aff_count FROM user_affiliates WHERE user_id = $1", inviter.ID))

	rows, err := client.QueryContext(txCtx, `
SELECT COUNT(*), MIN(frozen_until)
FROM user_affiliate_ledger
	WHERE action = 'registration_reward'
	  AND source_user_id IN ($1, $2)`, frozenInvitee.ID, zeroInvitee.ID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var ledgerCount int
	var frozenUntil *time.Time
	require.NoError(t, rows.Scan(&ledgerCount, &frozenUntil))
	require.Equal(t, 1, ledgerCount)
	require.NotNil(t, frozenUntil)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), *frozenUntil, time.Minute)
	require.NoError(t, rows.Close())

	selfAffiliate, err := repo.EnsureUserAffiliate(txCtx, frozenInvitee.ID)
	require.NoError(t, err)
	_, err = repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		frozenInvitee.ID,
		selfAffiliate.AffCode,
		7.25,
		0,
		0,
	)
	require.ErrorIs(t, err, service.ErrAffiliateCodeInvalid)

	otherInviter := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("registration-reward-other-inviter-%d@example.com", time.Now().UnixNano()+3),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	otherAffiliate, err := repo.EnsureUserAffiliate(txCtx, otherInviter.ID)
	require.NoError(t, err)
	_, err = repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		zeroInvitee.ID,
		otherAffiliate.AffCode,
		7.25,
		0,
		0,
	)
	require.ErrorIs(t, err, service.ErrAffiliateAlreadyBound)
}

func TestAffiliateRepository_BindInviterAndGrantRegistrationReward_RejectsInactiveInviter(t *testing.T) {
	ctx := context.Background()

	run := func(t *testing.T, disable func(t *testing.T, client *dbent.Client, inviterID int64)) {
		tx := testEntTx(t)
		txCtx := dbent.NewTxContext(ctx, tx)
		client := tx.Client()
		repo := NewAffiliateRepository(client, integrationDB)

		inviter := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("registration-reward-inactive-inviter-%d@example.com", time.Now().UnixNano()),
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		})
		invitee := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("registration-reward-inactive-invitee-%d@example.com", time.Now().UnixNano()+1),
			PasswordHash: "hash",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
		})
		inviterAffiliate, err := repo.EnsureUserAffiliate(txCtx, inviter.ID)
		require.NoError(t, err)

		disable(t, client, inviter.ID)

		// The code no longer resolves for validation...
		_, err = repo.GetAffiliateByCode(txCtx, inviterAffiliate.AffCode)
		require.ErrorIs(t, err, service.ErrAffiliateProfileNotFound)

		// ...nor for binding + granting a reward.
		_, err = repo.BindInviterAndGrantRegistrationReward(
			txCtx,
			invitee.ID,
			inviterAffiliate.AffCode,
			10,
			0,
			0,
		)
		require.ErrorIs(t, err, service.ErrAffiliateCodeInvalid)

		// The invitee stays unbound and no reward ledger row was written.
		require.Equal(t, 0, querySingleInt(t, txCtx, client,
			"SELECT COUNT(*) FROM user_affiliate_ledger WHERE action = 'registration_reward' AND source_user_id = $1", invitee.ID))
		rows, err := client.QueryContext(txCtx,
			"SELECT inviter_id FROM user_affiliates WHERE user_id = $1", invitee.ID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var inviterID *int64
		require.NoError(t, rows.Scan(&inviterID))
		require.NoError(t, rows.Close())
		require.Nil(t, inviterID)
	}

	t.Run("disabled status", func(t *testing.T) {
		run(t, func(t *testing.T, client *dbent.Client, inviterID int64) {
			_, err := client.ExecContext(ctx,
				"UPDATE users SET status = $1 WHERE id = $2", service.StatusDisabled, inviterID)
			require.NoError(t, err)
		})
	})

	t.Run("soft deleted", func(t *testing.T) {
		run(t, func(t *testing.T, client *dbent.Client, inviterID int64) {
			_, err := client.ExecContext(ctx,
				"UPDATE users SET deleted_at = NOW() WHERE id = $1", inviterID)
			require.NoError(t, err)
		})
	})
}

func TestAffiliateRepository_BindInviterAndGrantRegistrationReward_ConcurrentOnce(t *testing.T) {
	ctx := context.Background()
	repo := NewAffiliateRepository(integrationEntClient, integrationDB)
	inviter := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        fmt.Sprintf("registration-reward-concurrent-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	invitee := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        fmt.Sprintf("registration-reward-concurrent-invitee-%d@example.com", time.Now().UnixNano()+1),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	t.Cleanup(func() {
		_, _ = integrationEntClient.ExecContext(context.Background(), "DELETE FROM users WHERE id IN ($1, $2)", invitee.ID, inviter.ID)
	})

	inviterAffiliate, err := repo.EnsureUserAffiliate(ctx, inviter.ID)
	require.NoError(t, err)

	type callResult struct {
		result *service.AffiliateRegistrationRewardResult
		err    error
	}
	results := make(chan callResult, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			result, err := repo.BindInviterAndGrantRegistrationReward(
				ctx,
				invitee.ID,
				inviterAffiliate.AffCode,
				3.5,
				0,
				0,
			)
			results <- callResult{result: result, err: err}
		}()
	}
	start.Done()

	boundCount := 0
	for range 2 {
		call := <-results
		require.NoError(t, call.err)
		require.NotNil(t, call.result)
		if call.result.Bound {
			boundCount++
		}
	}
	require.Equal(t, 1, boundCount)

	require.Equal(t, 1, querySingleInt(t, ctx, integrationEntClient,
		"SELECT aff_count FROM user_affiliates WHERE user_id = $1", inviter.ID))
	require.InDelta(t, 3.5, querySingleFloat(t, ctx, integrationEntClient,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1", inviter.ID), 1e-9)
	require.Equal(t, 1, querySingleInt(t, ctx, integrationEntClient, `
SELECT COUNT(*)
FROM user_affiliate_ledger
WHERE action = 'registration_reward'
  AND source_user_id = $1`, invitee.ID))
}

func TestAffiliateRepository_TransferQuotaToBalance_EmptyQuota(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	u := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-empty-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Balance:      3.21,
		Concurrency:  5,
	})

	affCode := fmt.Sprintf("AFF%09d", time.Now().UnixNano()%1_000_000_000)
	_, err := client.ExecContext(txCtx, `
INSERT INTO user_affiliates (user_id, aff_code, aff_quota, aff_history_quota, created_at, updated_at)
VALUES ($1, $2, 0, 0, NOW(), NOW())`, u.ID, affCode)
	require.NoError(t, err)

	transferred, balance, err := repo.TransferQuotaToBalance(txCtx, u.ID)
	require.ErrorIs(t, err, service.ErrAffiliateQuotaEmpty)
	require.InDelta(t, 0.0, transferred, 1e-9)
	require.InDelta(t, 0.0, balance, 1e-9)

	persistedBalance := querySingleFloat(t, txCtx, client,
		"SELECT balance::double precision FROM users WHERE id = $1", u.ID)
	require.InDelta(t, 3.21, persistedBalance, 1e-9)
}

// TestAffiliateRepository_AdminCustomCode covers the success path of admin
// invite-code rewrite + reset within a shared test transaction:
// - UpdateUserAffCode replaces aff_code, sets aff_code_custom=true, lookup works
// - the old code can no longer be found
// - ResetUserAffCode reverts aff_code_custom and assigns a new system-format code
//
// The conflict path (duplicate code → ErrAffiliateCodeTaken) lives in its own
// test because a unique-violation aborts the surrounding Postgres tx, which
// would poison subsequent assertions in the same transaction.
func TestAffiliateRepository_AdminCustomCode(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	u := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-custom-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})

	original, err := repo.EnsureUserAffiliate(txCtx, u.ID)
	require.NoError(t, err)
	require.False(t, original.AffCodeCustom, "system-generated codes start as non-custom")
	originalCode := original.AffCode

	// Rewrite to a custom code
	customCode := fmt.Sprintf("VIP%09d", time.Now().UnixNano()%1_000_000_000)
	require.NoError(t, repo.UpdateUserAffCode(txCtx, u.ID, customCode))

	updated, err := repo.EnsureUserAffiliate(txCtx, u.ID)
	require.NoError(t, err)
	require.Equal(t, customCode, updated.AffCode)
	require.True(t, updated.AffCodeCustom)

	// Lookup by new custom code finds the user
	byCode, err := repo.GetAffiliateByCode(txCtx, customCode)
	require.NoError(t, err)
	require.Equal(t, u.ID, byCode.UserID)

	// Old system code should no longer match
	_, err = repo.GetAffiliateByCode(txCtx, originalCode)
	require.ErrorIs(t, err, service.ErrAffiliateProfileNotFound)

	// Reset back to a fresh system code, clears custom flag
	newSysCode, err := repo.ResetUserAffCode(txCtx, u.ID)
	require.NoError(t, err)
	require.NotEqual(t, customCode, newSysCode)

	reset, err := repo.EnsureUserAffiliate(txCtx, u.ID)
	require.NoError(t, err)
	require.Equal(t, newSysCode, reset.AffCode)
	require.False(t, reset.AffCodeCustom)

	// The old custom code is now free again
	_, err = repo.GetAffiliateByCode(txCtx, customCode)
	require.ErrorIs(t, err, service.ErrAffiliateProfileNotFound)
}

// TestAffiliateRepository_AdminCustomCode_Conflict isolates the unique-violation
// path. PostgreSQL aborts the enclosing tx when a unique constraint fires, so
// this test must be the only assertion and run in its own tx — production
// callers each have their own outer tx, so this matches real behavior.
func TestAffiliateRepository_AdminCustomCode_Conflict(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	taker := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-conflict-taker-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser, Status: service.StatusActive,
	})
	requester := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-conflict-req-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser, Status: service.StatusActive,
	})

	takenCode := fmt.Sprintf("HOT%09d", time.Now().UnixNano()%1_000_000_000)
	require.NoError(t, repo.UpdateUserAffCode(txCtx, taker.ID, takenCode))

	// Now requester tries to grab the same code → conflict.
	err := repo.UpdateUserAffCode(txCtx, requester.ID, takenCode)
	require.ErrorIs(t, err, service.ErrAffiliateCodeTaken)
}

// TestAffiliateRepository_AdminRebateRate covers per-user exclusive rate
// set/clear and the Batch variant including NULL semantics.
func TestAffiliateRepository_AdminRebateRate(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	u1 := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-rate-%d-a@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})
	u2 := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-rate-%d-b@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	})

	// Set exclusive rate for u1
	rate := 42.5
	require.NoError(t, repo.SetUserRebateRate(txCtx, u1.ID, &rate))

	got, err := repo.EnsureUserAffiliate(txCtx, u1.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AffRebateRatePercent)
	require.InDelta(t, 42.5, *got.AffRebateRatePercent, 1e-9)

	// Clear exclusive rate
	require.NoError(t, repo.SetUserRebateRate(txCtx, u1.ID, nil))
	cleared, err := repo.EnsureUserAffiliate(txCtx, u1.ID)
	require.NoError(t, err)
	require.Nil(t, cleared.AffRebateRatePercent)

	// Batch set both users
	batchRate := 15.0
	require.NoError(t, repo.BatchSetUserRebateRate(txCtx, []int64{u1.ID, u2.ID}, &batchRate))

	for _, uid := range []int64{u1.ID, u2.ID} {
		v, err := repo.EnsureUserAffiliate(txCtx, uid)
		require.NoError(t, err)
		require.NotNil(t, v.AffRebateRatePercent)
		require.InDelta(t, 15.0, *v.AffRebateRatePercent, 1e-9)
	}

	// Batch clear
	require.NoError(t, repo.BatchSetUserRebateRate(txCtx, []int64{u1.ID, u2.ID}, nil))
	for _, uid := range []int64{u1.ID, u2.ID} {
		v, err := repo.EnsureUserAffiliate(txCtx, uid)
		require.NoError(t, err)
		require.Nil(t, v.AffRebateRatePercent)
	}
}

// TestAffiliateRepository_ListUsersWithCustomSettings verifies the admin list
// only includes users with at least one override applied.
func TestAffiliateRepository_ListUsersWithCustomSettings(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewAffiliateRepository(client, integrationDB)

	// User without any custom config — should NOT appear in the list.
	plainEmail := fmt.Sprintf("affiliate-plain-%d@example.com", time.Now().UnixNano())
	uPlain := mustCreateUser(t, client, &service.User{
		Email: plainEmail, PasswordHash: "hash",
		Role: service.RoleUser, Status: service.StatusActive,
	})
	_, err := repo.EnsureUserAffiliate(txCtx, uPlain.ID)
	require.NoError(t, err)

	// User with a custom code — should appear.
	uCode := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-codeonly-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser, Status: service.StatusActive,
	})
	require.NoError(t, repo.UpdateUserAffCode(txCtx, uCode.ID, fmt.Sprintf("VIP%09d", time.Now().UnixNano()%1_000_000_000)))

	// User with only an exclusive rate — should appear.
	uRate := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("affiliate-rateonly-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser, Status: service.StatusActive,
	})
	r := 33.3
	require.NoError(t, repo.SetUserRebateRate(txCtx, uRate.ID, &r))

	entries, total, err := repo.ListUsersWithCustomSettings(txCtx, service.AffiliateAdminFilter{
		Page: 1, PageSize: 100,
	})
	require.NoError(t, err)

	// Build a quick lookup to assert per-user attributes (other tests may have
	// inserted custom rows in the same DB; we only care about our 3).
	byUserID := make(map[int64]service.AffiliateAdminEntry, len(entries))
	for _, e := range entries {
		byUserID[e.UserID] = e
	}

	require.NotContains(t, byUserID, uPlain.ID, "users without overrides must not appear")

	codeEntry, ok := byUserID[uCode.ID]
	require.True(t, ok, "custom-code user missing from list")
	require.True(t, codeEntry.AffCodeCustom)
	require.Nil(t, codeEntry.AffRebateRatePercent)

	rateEntry, ok := byUserID[uRate.ID]
	require.True(t, ok, "custom-rate user missing from list")
	require.False(t, rateEntry.AffCodeCustom)
	require.NotNil(t, rateEntry.AffRebateRatePercent)
	require.InDelta(t, 33.3, *rateEntry.AffRebateRatePercent, 1e-9)

	require.GreaterOrEqual(t, total, int64(2), "total must include at least our 2 custom rows")
}
