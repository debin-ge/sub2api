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

	applied, err := repo.AccrueQuota(txCtx, inviter.ID, invitee.ID, 3.5, 0, nil)
	require.NoError(t, err)
	require.True(t, applied, "AccrueQuota must report applied=true")

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
	require.Zero(t, rechargeAccrued, "registration rewards must not consume the per-invitee recharge rebate cap")

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
		24,
	)
	require.NoError(t, err)
	require.True(t, frozenResult.RewardApplied)

	zeroResult, err := repo.BindInviterAndGrantRegistrationReward(
		txCtx,
		zeroInvitee.ID,
		inviterAffiliate.AffCode,
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
