package service

import (
	"testing"
	"time"
)

// newContentModerationServiceForTest builds the service like
// NewContentModerationService and stops its background workers when the test
// ends. Each service with a setting repo and a moderation repo starts 33
// goroutines that wake every second and allocate; left running, they pile up
// across the package and skew timing and allocation measurements in later tests.
func newContentModerationServiceForTest(
	t testing.TB,
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	proxyRepo ProxyRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
) *ContentModerationService {
	t.Helper()
	svc := NewContentModerationService(settingRepo, repo, hashCache, groupRepo, userRepo, proxyRepo, authCacheInvalidator, emailService)
	t.Cleanup(svc.Stop)
	return svc
}

func TestContentModerationServiceStopEndsBackgroundWorkers(t *testing.T) {
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}},
		&contentModerationTestRepo{},
		nil, nil, nil, nil, nil, nil,
	)

	// Stop waits for every worker and the cleanup loop to return, so it
	// returning at all means no background goroutine is left running.
	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return: background workers are still running")
	}

	svc.Stop() // idempotent
	(&ContentModerationService{}).Stop()
	(*ContentModerationService)(nil).Stop()
}
