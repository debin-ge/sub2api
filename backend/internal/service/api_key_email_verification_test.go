package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type apiKeyEmailVerificationMemoryCache struct {
	codes  map[string]*VerificationCodeData
	proofs map[string]APIKeyEmailVerificationProof
	count  map[int64]int64
}

func newAPIKeyEmailVerificationMemoryCache() *apiKeyEmailVerificationMemoryCache {
	return &apiKeyEmailVerificationMemoryCache{codes: map[string]*VerificationCodeData{}, proofs: map[string]APIKeyEmailVerificationProof{}, count: map[int64]int64{}}
}

func (c *apiKeyEmailVerificationMemoryCache) codeKey(userID int64, email string) string {
	return optionalInt64String(&userID) + ":" + email
}

func (c *apiKeyEmailVerificationMemoryCache) GetCode(_ context.Context, userID int64, email string) (*VerificationCodeData, error) {
	value, ok := c.codes[c.codeKey(userID, email)]
	if !ok {
		return nil, errors.New("not found")
	}
	clone := *value
	return &clone, nil
}

func (c *apiKeyEmailVerificationMemoryCache) SetCode(_ context.Context, userID int64, email string, data *VerificationCodeData, _ time.Duration) error {
	clone := *data
	c.codes[c.codeKey(userID, email)] = &clone
	return nil
}

func (c *apiKeyEmailVerificationMemoryCache) DeleteCode(_ context.Context, userID int64, email string) error {
	delete(c.codes, c.codeKey(userID, email))
	return nil
}

func (c *apiKeyEmailVerificationMemoryCache) IncrementSendCount(_ context.Context, userID int64, _ time.Duration) (int64, error) {
	c.count[userID]++
	return c.count[userID], nil
}

func (c *apiKeyEmailVerificationMemoryCache) SetProof(_ context.Context, token string, proof APIKeyEmailVerificationProof, _ time.Duration) error {
	c.proofs[token] = proof
	return nil
}

func (c *apiKeyEmailVerificationMemoryCache) GetProof(_ context.Context, token string) (*APIKeyEmailVerificationProof, error) {
	proof, ok := c.proofs[token]
	if !ok {
		return nil, errors.New("not found")
	}
	return &proof, nil
}

func TestAPIKeyEmailVerificationProofIsBoundToUserEmailAndPurpose(t *testing.T) {
	cache := newAPIKeyEmailVerificationMemoryCache()
	svc := NewAPIKeyEmailVerificationService(cache, nil)
	now := time.Now().UTC()
	require.NoError(t, cache.SetCode(context.Background(), 7, "owner@example.com", &VerificationCodeData{Code: "123456", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}, time.Minute))

	result, err := svc.VerifyCode(context.Background(), 7, "OWNER@example.com", "123456")
	require.NoError(t, err)
	require.Equal(t, "owner@example.com", result.Email)

	verifiedAt, err := svc.ValidateProof(context.Background(), 7, "owner@example.com", result.Token)
	require.NoError(t, err)
	require.False(t, verifiedAt.IsZero())
	_, err = svc.ValidateProof(context.Background(), 8, "owner@example.com", result.Token)
	require.ErrorIs(t, err, ErrAPIKeyNotificationUnverified)
	_, err = svc.ValidateProof(context.Background(), 7, "other@example.com", result.Token)
	require.ErrorIs(t, err, ErrAPIKeyNotificationUnverified)
}

func TestNormalizeAPIKeyNotificationEmailRejectsDisplayAddress(t *testing.T) {
	got, err := NormalizeAPIKeyNotificationEmail(" User@Example.COM ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", got)
	_, err = NormalizeAPIKeyNotificationEmail("User <user@example.com>")
	require.ErrorIs(t, err, ErrAPIKeyNotificationEmailInvalid)
}

func TestValidateAPIKeyNotificationState(t *testing.T) {
	now := time.Now()
	email := "owner@example.com"
	verifiedAt := now.Add(-time.Minute)
	expiresAt := now.Add(time.Hour)
	duration := int64(time.Hour / time.Second)

	require.ErrorIs(t, validateAPIKeyNotificationState(&APIKey{ChangeNotifyEnabled: true}, now), ErrAPIKeyNotificationUnverified)
	require.ErrorIs(t, validateAPIKeyNotificationState(&APIKey{NotificationEmail: &email, NotificationEmailVerifiedAt: &verifiedAt, RotateOnExpiry: true}, now), ErrAPIKeyRotationExpiryRequired)
	require.NoError(t, validateAPIKeyNotificationState(&APIKey{NotificationEmail: &email, NotificationEmailVerifiedAt: &verifiedAt, RotateOnExpiry: true, ExpiresAt: &expiresAt, ValidityDurationSeconds: &duration}, now))
}

func TestAPIKeyEmailVerificationRateLimitUsesIncrementResult(t *testing.T) {
	cache := newAPIKeyEmailVerificationMemoryCache()
	cache.count[7] = notifyCodeUserRateLimit
	svc := NewAPIKeyEmailVerificationService(cache, &EmailService{})

	err := svc.SendCode(context.Background(), 7, "owner@example.com", "en")
	require.ErrorIs(t, err, ErrAPIKeyEmailVerificationLimited)
	require.Equal(t, int64(notifyCodeUserRateLimit+1), cache.count[7])
}
