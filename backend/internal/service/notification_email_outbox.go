package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	notificationEmailOutboxBatchSize    = 50
	notificationEmailOutboxConcurrency  = 4
	notificationEmailOutboxPollInterval = time.Second
	notificationEmailOutboxLease        = 8 * time.Minute
	notificationEmailOutboxSendTimeout  = 30 * time.Second
	notificationEmailOutboxRetention    = 30 * 24 * time.Hour
)

type NotificationEmailOutboxPayload struct {
	Variables map[string]string `json:"variables,omitempty"`
}

type NotificationEmailOutboxInput struct {
	EventType       string
	UserID          int64
	APIKeyID        *int64
	RecipientEmail  string
	RotationVersion *int64
	Payload         NotificationEmailOutboxPayload
	DedupKey        string
}

type NotificationEmailOutboxEvent struct {
	ID              int64
	EventType       string
	UserID          int64
	APIKeyID        *int64
	RecipientEmail  string
	RotationVersion *int64
	Payload         NotificationEmailOutboxPayload
	Attempts        int
	CreatedAt       time.Time
}

type NotificationEmailOutboxStats struct {
	Pending         int64
	Sent            int64
	Cancelled       int64
	MaxAttempts     int
	OldestCreatedAt *time.Time
	LastError       string
}

type NotificationEmailOutboxRepository interface {
	Enqueue(ctx context.Context, input NotificationEmailOutboxInput) error
	Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]NotificationEmailOutboxEvent, error)
	MarkSent(ctx context.Context, id int64, workerID string) error
	Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error
	Cancel(ctx context.Context, id int64, workerID, reason string) error
	CancelPendingRotationsByAPIKey(ctx context.Context, apiKeyID int64, reason string) error
	Stats(ctx context.Context) (NotificationEmailOutboxStats, error)
	DeleteCompletedBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

type NotificationEmailOutboxHealth struct {
	Running     bool          `json:"running"`
	Processed   uint64        `json:"processed"`
	Failures    uint64        `json:"failures"`
	Pending     int64         `json:"pending"`
	Sent        int64         `json:"sent"`
	Cancelled   int64         `json:"cancelled"`
	OldestLag   time.Duration `json:"oldest_lag"`
	MaxAttempts int           `json:"max_attempts"`
	LastError   string        `json:"last_error,omitempty"`
	StatsError  string        `json:"stats_error,omitempty"`
}

type NotificationEmailOutboxWorker struct {
	repo         NotificationEmailOutboxRepository
	apiKeyRepo   APIKeyRepository
	emailService *NotificationEmailService
	workerID     string
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	start        sync.Once
	stop         sync.Once
	running      atomic.Bool
	processed    atomic.Uint64
	failures     atomic.Uint64
	lastError    atomic.Value
	lastCleanup  atomic.Int64
}

func NewNotificationEmailOutboxWorker(repo NotificationEmailOutboxRepository, apiKeyRepo APIKeyRepository, emailService *NotificationEmailService) *NotificationEmailOutboxWorker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &NotificationEmailOutboxWorker{repo: repo, apiKeyRepo: apiKeyRepo, emailService: emailService, workerID: uuid.NewString(), ctx: ctx, cancel: cancel}
	w.lastError.Store("")
	return w
}

func (w *NotificationEmailOutboxWorker) Start() {
	if w == nil || w.repo == nil || w.emailService == nil {
		return
	}
	w.start.Do(func() {
		w.running.Store(true)
		w.wg.Add(1)
		go w.run()
	})
}

func (w *NotificationEmailOutboxWorker) Stop() {
	if w == nil {
		return
	}
	w.stop.Do(w.cancel)
	w.wg.Wait()
	w.running.Store(false)
}

func (w *NotificationEmailOutboxWorker) run() {
	defer w.wg.Done()
	defer w.running.Store(false)
	ticker := time.NewTicker(notificationEmailOutboxPollInterval)
	defer ticker.Stop()
	for {
		if err := w.processBatch(w.ctx); err != nil && w.ctx.Err() == nil {
			w.recordFailure(err)
		}
		w.cleanupIfDue()
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *NotificationEmailOutboxWorker) processBatch(ctx context.Context) error {
	events, err := w.repo.Claim(ctx, w.workerID, notificationEmailOutboxBatchSize, notificationEmailOutboxLease)
	if err != nil {
		return fmt.Errorf("claim notification email outbox: %w", err)
	}
	semaphore := make(chan struct{}, notificationEmailOutboxConcurrency)
	var wg sync.WaitGroup
	for i := range events {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case semaphore <- struct{}{}:
		}
		event := events[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			w.processEvent(ctx, event)
		}()
	}
	wg.Wait()
	return nil
}

func (w *NotificationEmailOutboxWorker) processEvent(parent context.Context, event NotificationEmailOutboxEvent) {
	variables := cloneNotificationEmailVariables(event.Payload.Variables)
	secret := ""
	if event.EventType == NotificationEmailEventAPIKeyRotated {
		apiKey, reason, err := w.loadRotationKey(parent, event)
		if err != nil {
			w.retryEvent(event, err, "")
			return
		}
		if reason != "" {
			w.cancelEvent(event, reason)
			return
		}
		secret = apiKey.Key
		variables["new_api_key"] = apiKey.Key
		variables["api_key_id"] = strconv.FormatInt(apiKey.ID, 10)
		variables["api_key_name"] = apiKey.Name
		if apiKey.ExpiresAt != nil {
			variables["next_expiry_time"] = apiKey.ExpiresAt.Format("2006-01-02 15:04:05 MST")
		}
	}

	ctx, cancel := context.WithTimeout(parent, notificationEmailOutboxSendTimeout)
	err := w.emailService.Send(ctx, NotificationEmailSendInput{
		Event: event.EventType, RecipientEmail: event.RecipientEmail, UserID: event.UserID,
		SourceType: "notification_email_outbox", SourceID: strconv.FormatInt(event.ID, 10), Variables: variables,
	})
	cancel()
	if err != nil {
		w.retryEvent(event, err, secret)
		return
	}
	ackCtx, ackCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = w.repo.MarkSent(ackCtx, event.ID, w.workerID)
	ackCancel()
	if err != nil {
		w.recordFailure(fmt.Errorf("mark notification email %d sent: %w", event.ID, err))
		return
	}
	w.processed.Add(1)
	w.lastError.Store("")
}

func (w *NotificationEmailOutboxWorker) loadRotationKey(ctx context.Context, event NotificationEmailOutboxEvent) (*APIKey, string, error) {
	if event.APIKeyID == nil || event.RotationVersion == nil || w.apiKeyRepo == nil {
		return nil, "invalid_rotation_event", nil
	}
	apiKey, err := w.apiKeyRepo.GetByID(ctx, *event.APIKeyID)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, "api_key_deleted", nil
		}
		return nil, "", err
	}
	if apiKey.RotationVersion != *event.RotationVersion {
		return nil, "rotation_version_superseded", nil
	}
	if apiKey.ExpiresAt == nil || !apiKey.ExpiresAt.After(time.Now()) {
		return nil, "rotated_key_expired_before_delivery", nil
	}
	return apiKey, "", nil
}

func (w *NotificationEmailOutboxWorker) retryEvent(event NotificationEmailOutboxEvent, err error, secret string) {
	w.failures.Add(1)
	message := boundedNotificationEmailError(err, secret)
	w.lastError.Store(message)
	slog.Warn("notification email outbox delivery failed", "outbox_id", event.ID, "attempt", event.Attempts+1, "error", message)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	retryErr := w.repo.Retry(ctx, event.ID, w.workerID, time.Now().UTC().Add(notificationEmailRetryDelay(event.Attempts+1)), message)
	cancel()
	if retryErr != nil {
		w.recordFailure(fmt.Errorf("release notification email %d: %w", event.ID, retryErr))
	}
}

func (w *NotificationEmailOutboxWorker) cancelEvent(event NotificationEmailOutboxEvent, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := w.repo.Cancel(ctx, event.ID, w.workerID, reason)
	cancel()
	if err != nil {
		w.recordFailure(fmt.Errorf("cancel notification email %d: %w", event.ID, err))
		return
	}
	w.processed.Add(1)
}

func notificationEmailRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		attempt = 9
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	delay = time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func boundedNotificationEmailError(err error, secret string) string {
	if err == nil {
		return ""
	}
	message := strings.ToValidUTF8(err.Error(), "\uFFFD")
	message = strings.ReplaceAll(message, "\x00", "\uFFFD")
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[REDACTED_API_KEY]")
	}
	const maxBytes = 1024
	if len(message) <= maxBytes {
		return message
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func cloneNotificationEmailVariables(input map[string]string) map[string]string {
	result := make(map[string]string, len(input)+4)
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (w *NotificationEmailOutboxWorker) cleanupIfDue() {
	now := time.Now().UTC()
	last := time.Unix(w.lastCleanup.Load(), 0)
	if !last.IsZero() && now.Sub(last) < time.Hour {
		return
	}
	if !w.lastCleanup.CompareAndSwap(last.Unix(), now.Unix()) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := w.repo.DeleteCompletedBefore(ctx, now.Add(-notificationEmailOutboxRetention), 1000)
	cancel()
	if err != nil {
		w.recordFailure(fmt.Errorf("clean notification email outbox: %w", err))
	}
}

func (w *NotificationEmailOutboxWorker) recordFailure(err error) {
	if err == nil {
		return
	}
	w.failures.Add(1)
	message := boundedNotificationEmailError(err, "")
	w.lastError.Store(message)
	slog.Warn("notification email outbox processing failed", "error", message)
}

func (w *NotificationEmailOutboxWorker) Health(ctx context.Context) NotificationEmailOutboxHealth {
	health := NotificationEmailOutboxHealth{Running: w != nil && w.running.Load()}
	if w == nil {
		return health
	}
	health.Processed = w.processed.Load()
	health.Failures = w.failures.Load()
	if value := w.lastError.Load(); value != nil {
		health.LastError, _ = value.(string)
	}
	if w.repo == nil {
		return health
	}
	stats, err := w.repo.Stats(ctx)
	if err != nil {
		health.StatsError = boundedNotificationEmailError(err, "")
		return health
	}
	health.Pending, health.Sent, health.Cancelled, health.MaxAttempts = stats.Pending, stats.Sent, stats.Cancelled, stats.MaxAttempts
	if health.LastError == "" {
		health.LastError = stats.LastError
	}
	if stats.OldestCreatedAt != nil {
		health.OldestLag = time.Since(*stats.OldestCreatedAt)
		if health.OldestLag < 0 {
			health.OldestLag = 0
		}
	}
	return health
}

type OpsAPIKeyEmailAutomationHealth struct {
	Outbox   NotificationEmailOutboxHealth `json:"outbox"`
	Rotation APIKeyRotationHealth          `json:"rotation"`
}

func (s *OpsService) GetAPIKeyEmailAutomationHealth(ctx context.Context) OpsAPIKeyEmailAutomationHealth {
	health := OpsAPIKeyEmailAutomationHealth{}
	if s == nil {
		return health
	}
	if s.notificationEmailWorker != nil {
		health.Outbox = s.notificationEmailWorker.Health(ctx)
	}
	if s.apiKeyRotationService != nil {
		health.Rotation = s.apiKeyRotationService.Health()
	}
	return health
}

func encodeNotificationEmailPayload(payload NotificationEmailOutboxPayload) ([]byte, error) {
	return json.Marshal(payload)
}
