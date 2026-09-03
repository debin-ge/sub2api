package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryClearsVerificationWhenEmailChangesWithoutProof(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "notification-clear@test.com")
	oldEmail := "old@example.com"
	newEmail := "new@example.com"
	verifiedAt := time.Now().UTC()
	key := &service.APIKey{
		UserID: user.ID, Key: "sk-notification-clear", Name: "notification", Status: service.StatusActive,
		NotificationEmail: &oldEmail, NotificationEmailVerifiedAt: &verifiedAt,
	}
	require.NoError(t, repo.Create(ctx, key))

	key.NotificationEmail = &newEmail
	key.NotificationEmailVerifiedAt = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{NotificationEmail: true}))

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, newEmail, *got.NotificationEmail)
	require.Nil(t, got.NotificationEmailVerifiedAt)
}

func TestAPIKeyRepositoryBulkNotificationIncludesUnverifiedEmail(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "notification-bulk@test.com")
	group, err := client.Group.Create().
		SetName("notification-group").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)
	email := "owner@example.com"
	key := &service.APIKey{
		UserID: user.ID, Key: "sk-notification-bulk", Name: "notification", Status: service.StatusActive,
		GroupID: &group.ID, NotificationEmail: &email, ChangeNotifyEnabled: true,
	}
	require.NoError(t, repo.Create(ctx, key))

	keys, err := repo.ListNotificationKeysByUserAndGroup(ctx, user.ID, group.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Nil(t, keys[0].NotificationEmailVerifiedAt)
}
