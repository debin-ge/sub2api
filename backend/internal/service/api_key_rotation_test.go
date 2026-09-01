package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type apiKeyRotationRepoStub struct {
	candidates []DueAPIKeyRotation
	rotations  int
	newKey     string
}

func (r *apiKeyRotationRepoStub) ListDue(context.Context, time.Time, int) ([]DueAPIKeyRotation, error) {
	return append([]DueAPIKeyRotation(nil), r.candidates...), nil
}

func (r *apiKeyRotationRepoStub) RotateIfDue(_ context.Context, _ DueAPIKeyRotation, newKey string, _ time.Time) (int64, bool, error) {
	r.rotations++
	r.newKey = newKey
	return 1, true, nil
}

func TestAPIKeyRotationDefersWithoutSMTPConfiguration(t *testing.T) {
	repo := &apiKeyRotationRepoStub{candidates: []DueAPIKeyRotation{{ID: 1, OldKey: "old"}}}
	settingRepo := newNotificationEmailMemorySettingRepo()
	email := NewEmailService(settingRepo, nil)
	apiKey := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})
	svc := NewAPIKeyRotationService(repo, apiKey, email, nil, nil)

	svc.runOnce()
	require.Zero(t, repo.rotations)
	require.Equal(t, uint64(1), svc.deferredSMTP.Load())
}

func TestAPIKeyRotationRunsWhenSMTPIsConfigured(t *testing.T) {
	repo := &apiKeyRotationRepoStub{candidates: []DueAPIKeyRotation{{ID: 1, OldKey: "old", RotationVersion: 0}}}
	settingRepo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, settingRepo.Set(context.Background(), SettingKeySMTPHost, "smtp.example.com"))
	email := NewEmailService(settingRepo, nil)
	apiKey := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-"}})
	svc := NewAPIKeyRotationService(repo, apiKey, email, nil, nil)

	svc.runOnce()
	require.Equal(t, 1, repo.rotations)
	require.NotEmpty(t, repo.newKey)
	require.NotEqual(t, "old", repo.newKey)
}

func TestAPIKeyConfigurationSummaryExcludesRuntimeFields(t *testing.T) {
	before := &APIKey{Name: "key", QuotaUsed: 1, Usage5h: 2}
	after := &APIKey{Name: "key", QuotaUsed: 9, Usage5h: 10}
	require.Empty(t, APIKeyConfigurationChangeSummary(before, after))
	after.Name = "renamed"
	require.Contains(t, APIKeyConfigurationChangeSummary(before, after), "Name / 名称")
}

func TestNotificationEmailErrorRedactsRotationKey(t *testing.T) {
	secret := "sk-secret-value"
	message := boundedNotificationEmailError(assertiveError("delivery failed for "+secret), secret)
	require.NotContains(t, message, secret)
	require.Contains(t, message, "[REDACTED_API_KEY]")
}

func TestAPIKeyRotationTemplateRequiresCompleteNewKeyPlaceholder(t *testing.T) {
	official := notificationEmailOfficialTemplates[NotificationEmailEventAPIKeyRotated][notificationEmailDefaultLocale]
	require.NoError(t, validateNotificationEmailTemplate(NotificationEmailEventAPIKeyRotated, official.Subject, official.HTML))
	require.Error(t, validateNotificationEmailTemplate(NotificationEmailEventAPIKeyRotated, official.Subject, "<p>rotated</p>"))
}

type assertiveError string

func (e assertiveError) Error() string { return string(e) }
