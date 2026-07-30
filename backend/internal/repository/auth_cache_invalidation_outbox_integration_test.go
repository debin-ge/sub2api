//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuthCacheInvalidationTriggers_CoverSecurityMutationsOnly(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("auth-outbox-group-%d", suffix), RateMultiplier: 1, IsExclusive: true,
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("auth-outbox-%d@example.com", suffix), Concurrency: 5,
	})
	groupID := group.ID
	keyValue := fmt.Sprintf("sk-auth-outbox-%d", suffix)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: user.ID, GroupID: &groupID, Key: keyValue, Name: "outbox", Status: service.StatusActive}
	require.NoError(t, apiKeyRepo.Create(ctx, key))
	unboundKeyValue := fmt.Sprintf("sk-auth-outbox-unbound-%d", suffix)
	unboundKey := &service.APIKey{UserID: user.ID, Key: unboundKeyValue, Name: "outbox-unbound", Status: service.StatusActive}
	require.NoError(t, apiKeyRepo.Create(ctx, unboundKey))

	sum := sha256.Sum256([]byte(keyValue))
	cacheKey := hex.EncodeToString(sum[:])
	unboundSum := sha256.Sum256([]byte(unboundKeyValue))
	unboundCacheKey := hex.EncodeToString(unboundSum[:])
	clear := func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1", cacheKey)
		require.NoError(t, err)
	}
	clearUnbound := func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1", unboundCacheKey)
		require.NoError(t, err)
	}
	count := func() int {
		var value int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", cacheKey).Scan(&value))
		return value
	}
	countUnbound := func() int {
		var value int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", unboundCacheKey).Scan(&value))
		return value
	}
	clear()
	clearUnbound()
	t.Cleanup(clear)
	t.Cleanup(clearUnbound)
	t.Cleanup(func() {
		// Keep the shared integration database isolated for suites that assert
		// platform-wide group counts. The final clear cleanup runs after this one
		// and removes invalidations emitted by these hard deletes.
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM user_allowed_groups WHERE user_id = $1 OR group_id = $2", user.ID, group.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id IN ($1, $2)", key.ID, unboundKey.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", group.ID)
		require.NoError(t, err)
	})

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + 1,
			usage_5h = usage_5h + 1,
			last_used_at = NOW()
		WHERE id = $1`, key.ID)
	require.NoError(t, err)
	require.Zero(t, count(), "usage-only key updates must not enqueue")

	_, err = integrationDB.ExecContext(ctx, "UPDATE api_keys SET status = 'disabled' WHERE id = $1", key.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "key disable must enqueue")
	clear()
	_, err = integrationDB.ExecContext(ctx, "UPDATE api_keys SET status = 'active' WHERE id = $1", key.ID)
	require.NoError(t, err)
	clear()

	userRepo := NewUserRepository(integrationEntClient, integrationDB)
	loadedUser, err := userRepo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	_, err = userRepo.AdjustBalance(ctx, loadedUser.ID, 10)
	require.NoError(t, err)
	require.Zero(t, count(), "balance update with unchanged allowed groups must not enqueue")

	_, err = integrationDB.ExecContext(ctx, "UPDATE users SET status = 'disabled' WHERE id = $1", user.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "user disable must enqueue all active keys")
	require.Equal(t, 1, countUnbound(), "user disable must enqueue unbound active keys too")
	clear()
	clearUnbound()
	_, err = integrationDB.ExecContext(ctx, "UPDATE users SET status = 'active' WHERE id = $1", user.ID)
	require.NoError(t, err)
	clear()
	clearUnbound()

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE users
		SET vip_manual_override = TRUE,
			vip_override_at = NOW(),
			vip_override_by = id,
			vip_override_reason = 'integration test',
			is_vip = TRUE,
			vip_granted_at = NOW(),
			vip_effective_source = 'manual_on'
		WHERE id = $1`, user.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "effective VIP changes must enqueue every active key")
	require.Equal(t, 1, countUnbound(), "effective VIP changes must enqueue unbound active keys too")
	clear()
	clearUnbound()

	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET name = name || '-cosmetic' WHERE id = $1", group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "all fields materialized in the auth snapshot must enqueue")
	clear()
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET allow_image_generation = NOT allow_image_generation WHERE id = $1", group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "image-generation permission changes must enqueue bound keys")
	clear()
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET status = 'disabled' WHERE id = $1", group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "group disable must enqueue bound keys")
	clear()
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET status = 'active' WHERE id = $1", group.ID)
	require.NoError(t, err)
	clear()

	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET vip_only = TRUE WHERE id = $1", group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "VIP-only changes must enqueue bound keys")
	clear()

	_, err = integrationDB.ExecContext(ctx,
		"INSERT INTO user_allowed_groups (user_id, group_id) VALUES ($1, $2)", user.ID, group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, countUnbound(), "allowlist changes must invalidate keys not originally bound to the exclusive group")
	clear()
	clearUnbound()
	_, err = integrationDB.ExecContext(ctx,
		"DELETE FROM user_allowed_groups WHERE user_id = $1 AND group_id = $2", user.ID, group.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "exclusive-group revocation must enqueue")
	require.Equal(t, 1, countUnbound(), "exclusive-group revocation must invalidate every key owned by the user")
	clear()
	clearUnbound()

	require.NoError(t, apiKeyRepo.DeleteWithAudit(ctx, key.ID))
	require.Equal(t, 1, count(), "tombstone delete must hash OLD.key exactly once")
	var stored string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT cache_key FROM auth_cache_invalidation_outbox WHERE cache_key = $1 LIMIT 1", cacheKey).Scan(&stored))
	require.Equal(t, cacheKey, stored)
	require.NotContains(t, stored, keyValue)
}

func TestAuthCacheInvalidationTriggers_ConvergesAcrossTwoInstances(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:             fmt.Sprintf("auth-convergence-group-%d", suffix),
		Platform:         service.PlatformAnthropic,
		Status:           service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
		RateMultiplier:   1,
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:  fmt.Sprintf("auth-convergence-%d@example.com", suffix),
		Status: service.StatusActive,
	})
	groupID := group.ID
	keyValue := fmt.Sprintf("sk-auth-convergence-%d", suffix)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{
		UserID:  user.ID,
		GroupID: &groupID,
		Key:     keyValue,
		Name:    "auth-convergence",
		Status:  service.StatusActive,
	}
	require.NoError(t, apiKeyRepo.Create(ctx, key))

	sum := sha256.Sum256([]byte(keyValue))
	cacheKey := hex.EncodeToString(sum[:])
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM groups WHERE id = $1", group.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
		_, _ = integrationDB.ExecContext(
			cleanupCtx,
			"DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1",
			cacheKey,
		)
	})
	_, err := integrationDB.ExecContext(
		ctx,
		"DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1",
		cacheKey,
	)
	require.NoError(t, err)

	rdb := testRedis(t)
	cacheA := NewAPIKeyCache(rdb)
	cacheB := NewAPIKeyCache(rdb)
	userRepo := NewUserRepository(integrationEntClient, integrationDB)
	groupRepo := NewGroupRepository(integrationEntClient, integrationDB)
	subscriptionRepo := NewUserSubscriptionRepository(integrationEntClient)
	rateRepo := NewUserGroupRateRepository(integrationDB)
	cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{
		L1Size:             100,
		L1TTLSeconds:       60,
		L2TTLSeconds:       60,
		NegativeTTLSeconds: 5,
		Singleflight:       true,
	}}
	instanceA := service.NewAPIKeyService(
		apiKeyRepo,
		userRepo,
		groupRepo,
		subscriptionRepo,
		rateRepo,
		cacheA,
		cfg,
	)
	instanceB := service.NewAPIKeyService(
		apiKeyRepo,
		userRepo,
		groupRepo,
		subscriptionRepo,
		rateRepo,
		cacheB,
		cfg,
	)
	instanceA.StartAuthCacheInvalidationSubscriber(ctx)
	instanceB.StartAuthCacheInvalidationSubscriber(ctx)
	t.Cleanup(instanceA.StopAuthCacheInvalidationSubscriber)
	t.Cleanup(instanceB.StopAuthCacheInvalidationSubscriber)
	require.Eventually(t, func() bool {
		return instanceA.AuthCacheInvalidationSubscriberHealth().Connected &&
			instanceB.AuthCacheInvalidationSubscriberHealth().Connected
	}, 5*time.Second, 20*time.Millisecond)

	firstA, err := instanceA.GetByKey(ctx, keyValue)
	require.NoError(t, err)
	firstB, err := instanceB.GetByKey(ctx, keyValue)
	require.NoError(t, err)
	require.NotNil(t, firstA.Group)
	require.NotNil(t, firstB.Group)
	require.False(t, firstA.Group.VIPOnly)
	require.False(t, firstB.Group.VIPOnly)

	worker := service.NewAuthCacheInvalidationWorker(
		NewAuthCacheInvalidationOutboxRepository(integrationDB),
		cacheA,
		instanceA,
	)
	worker.Start()
	t.Cleanup(worker.Stop)

	_, err = integrationDB.ExecContext(
		ctx,
		"UPDATE groups SET vip_only = TRUE WHERE id = $1",
		group.ID,
	)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		reloadedA, errA := instanceA.GetByKey(ctx, keyValue)
		reloadedB, errB := instanceB.GetByKey(ctx, keyValue)
		return errA == nil &&
			errB == nil &&
			reloadedA != nil &&
			reloadedB != nil &&
			reloadedA.Group != nil &&
			reloadedB.Group != nil &&
			reloadedA.Group.VIPOnly &&
			reloadedB.Group.VIPOnly
	}, 10*time.Second, 50*time.Millisecond)
}
