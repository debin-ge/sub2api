//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// api_keys 的 quota_used / usage_5h|1d|7d 由计费热路径原子递增。
// 编辑 Key（改名、换分组……）若整行回写，并发累计的用量就会被旧快照覆盖。
// 这些用例锁死"只声明请求真正要改的列"。

type updateFieldsAPIKeyRepoStub struct {
	quotaBaseAPIKeyRepoStub
	key          *APIKey
	updated      *APIKey
	updateFields []APIKeyUpdateFields
}

// IncrementQuotaUsed 模拟计费热路径上的原子递增：只动 quota_used。
func (s *updateFieldsAPIKeyRepoStub) IncrementQuotaUsed(_ context.Context, _ int64, amount float64) (float64, error) {
	s.key.QuotaUsed += amount
	return s.key.QuotaUsed, nil
}

func (s *updateFieldsAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	clone := *s.key
	return &clone, nil
}

func (s *updateFieldsAPIKeyRepoStub) Update(_ context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	clone := *key
	s.updated = &clone
	s.updateFields = append(s.updateFields, fields)
	return nil
}

func TestAPIKeyUpdate_AllowsUnverifiedNotificationEmail(t *testing.T) {
	oldEmail := "old@example.com"
	newEmail := "new@example.com"
	verifiedAt := time.Now().UTC().Add(-time.Hour)
	expiresAt := time.Now().UTC().Add(time.Hour)
	duration := int64(time.Hour / time.Second)
	changeNotifyEnabled := true
	rotateOnExpiry := true
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusActive,
		NotificationEmail: &oldEmail, NotificationEmailVerifiedAt: &verifiedAt,
		ExpiresAt: &expiresAt, ValidityDurationSeconds: &duration,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{
		NotificationEmail:   &newEmail,
		ChangeNotifyEnabled: &changeNotifyEnabled,
		RotateOnExpiry:      &rotateOnExpiry,
	})
	require.NoError(t, err)
	require.Equal(t, newEmail, *repo.updated.NotificationEmail)
	require.Nil(t, repo.updated.NotificationEmailVerifiedAt)
	require.True(t, repo.updated.ChangeNotifyEnabled)
	require.True(t, repo.updated.RotateOnExpiry)
	require.Equal(t, []APIKeyUpdateFields{{NotificationEmail: true, NotificationSettings: true}}, repo.updateFields)
}

func TestAPIKeyUpdate_CanOptionallyVerifyExistingEmail(t *testing.T) {
	ctx := context.Background()
	email := "owner@example.com"
	verifiedAt := time.Now().UTC()
	cache := newAPIKeyEmailVerificationMemoryCache()
	require.NoError(t, cache.SetProof(ctx, "valid-proof", APIKeyEmailVerificationProof{
		UserID: 7, Email: email, Purpose: apiKeyEmailVerificationPurpose, VerifiedAt: verifiedAt,
	}, time.Minute))
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusActive, NotificationEmail: &email,
	})
	svc.emailVerification = NewAPIKeyEmailVerificationService(cache, nil)

	_, err := svc.Update(ctx, 1, 7, UpdateAPIKeyRequest{
		NotificationEmail:             &email,
		NotificationEmailVerification: "valid-proof",
	})
	require.NoError(t, err)
	require.Equal(t, verifiedAt, *repo.updated.NotificationEmailVerifiedAt)

	_, err = svc.Update(ctx, 1, 7, UpdateAPIKeyRequest{
		NotificationEmail:             &email,
		NotificationEmailVerification: "invalid-proof",
	})
	require.ErrorIs(t, err, ErrAPIKeyNotificationUnverified)
}

func newUpdateFieldsAPIKeyService(key *APIKey) (*APIKeyService, *updateFieldsAPIKeyRepoStub) {
	repo := &updateFieldsAPIKeyRepoStub{key: key}
	return &APIKeyService{apiKeyRepo: repo}, repo
}

func TestAPIKeyUpdate_OnlyDeclaresRequestedColumns(t *testing.T) {
	name := "renamed"
	quota := 500.0
	rateLimit := 42.0
	whitelist := []string{"10.0.0.1"}

	tests := []struct {
		name string
		req  UpdateAPIKeyRequest
		want APIKeyUpdateFields
	}{
		{
			name: "name only",
			req:  UpdateAPIKeyRequest{Name: &name},
			want: APIKeyUpdateFields{Name: true},
		},
		{
			name: "quota only",
			req:  UpdateAPIKeyRequest{Quota: &quota},
			want: APIKeyUpdateFields{Quota: true},
		},
		{
			name: "rate limit threshold only",
			req:  UpdateAPIKeyRequest{RateLimit5h: &rateLimit},
			want: APIKeyUpdateFields{RateLimits: true},
		},
		{
			name: "ip whitelist only",
			req:  UpdateAPIKeyRequest{IPWhitelist: &whitelist},
			want: APIKeyUpdateFields{IPRules: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
				ID:        1,
				UserID:    7,
				Key:       "sk-test",
				Name:      "before",
				Status:    StatusActive,
				Quota:     100,
				QuotaUsed: 30,
				Usage5h:   12,
			})

			_, err := svc.Update(context.Background(), 1, 7, tt.req)
			require.NoError(t, err)
			require.Equal(t, []APIKeyUpdateFields{tt.want}, repo.updateFields)
		})
	}
}

// 显式重置仍需声明对应的列，避免收窄写入列时把功能改坏。
func TestAPIKeyUpdate_DeclaresUsageColumnsOnExplicitReset(t *testing.T) {
	reset := true
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusActive, Quota: 100, QuotaUsed: 30, Usage5h: 12,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{
		ResetQuota:          &reset,
		ResetRateLimitUsage: &reset,
	})
	require.NoError(t, err)
	require.Equal(t, []APIKeyUpdateFields{{QuotaUsed: true, RateLimitUsage: true}}, repo.updateFields)
}

// 配额扩容会顺带把 quota_exhausted 复活为 active，此时必须声明 status。
func TestAPIKeyUpdate_DeclaresStatusWhenReactivated(t *testing.T) {
	quota := 500.0
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusAPIKeyQuotaExhausted, Quota: 100, QuotaUsed: 100,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Quota: &quota})
	require.NoError(t, err)
	require.Equal(t, []APIKeyUpdateFields{{Quota: true, Status: true}}, repo.updateFields)
}

// 计费热路径把 Key 标记为配额耗尽时只写 status，
// 否则会把刚原子递增的 quota_used 按快照覆盖掉。
func TestUpdateQuotaUsed_ExhaustedMarkOnlyDeclaresStatus(t *testing.T) {
	repo := &updateFieldsAPIKeyRepoStub{key: &APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusActive, Quota: 10, QuotaUsed: 10,
	}}
	svc := &APIKeyService{apiKeyRepo: repo}

	require.NoError(t, svc.UpdateQuotaUsed(context.Background(), 1, 5))
	require.Equal(t, []APIKeyUpdateFields{{Status: true}}, repo.updateFields)
}
