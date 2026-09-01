package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	apiKeyRotationInterval  = time.Minute
	apiKeyRotationBatchSize = 100
	apiKeyRotationLockKey   = "api_key:rotation:leader"
	apiKeyRotationLockTTL   = 2 * time.Minute
)

type DueAPIKeyRotation struct {
	ID                      int64
	UserID                  int64
	OldKey                  string
	Name                    string
	NotificationEmail       string
	ExpiresAt               time.Time
	ValidityDurationSeconds int64
	RotationVersion         int64
}

type APIKeyRotationRepository interface {
	ListDue(ctx context.Context, now time.Time, limit int) ([]DueAPIKeyRotation, error)
	RotateIfDue(ctx context.Context, candidate DueAPIKeyRotation, newKey string, now time.Time) (int64, bool, error)
}

type APIKeyRotationHealth struct {
	Running          bool      `json:"running"`
	Rotated          uint64    `json:"rotated"`
	Failures         uint64    `json:"failures"`
	DeferredNoSMTP   uint64    `json:"deferred_no_smtp"`
	LastRunAt        time.Time `json:"last_run_at,omitempty"`
	LastSuccessfulAt time.Time `json:"last_successful_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

type APIKeyRotationService struct {
	repo         APIKeyRotationRepository
	apiKey       *APIKeyService
	email        *EmailService
	lockCache    LeaderLockCache
	db           *sql.DB
	instanceID   string
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	start        sync.Once
	stop         sync.Once
	running      atomic.Bool
	rotated      atomic.Uint64
	failures     atomic.Uint64
	deferredSMTP atomic.Uint64
	lastRun      atomic.Int64
	lastSuccess  atomic.Int64
	lastError    atomic.Value
}

func NewAPIKeyRotationService(repo APIKeyRotationRepository, apiKey *APIKeyService, email *EmailService, lockCache LeaderLockCache, db *sql.DB) *APIKeyRotationService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &APIKeyRotationService{repo: repo, apiKey: apiKey, email: email, lockCache: lockCache, db: db, instanceID: uuid.NewString(), ctx: ctx, cancel: cancel}
	s.lastError.Store("")
	return s
}

func (s *APIKeyRotationService) Start() {
	if s == nil || s.repo == nil || s.apiKey == nil || s.email == nil {
		return
	}
	s.start.Do(func() {
		s.running.Store(true)
		s.wg.Add(1)
		go s.run()
	})
}

func (s *APIKeyRotationService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(s.cancel)
	s.wg.Wait()
	s.running.Store(false)
}

func (s *APIKeyRotationService) run() {
	defer s.wg.Done()
	defer s.running.Store(false)
	ticker := time.NewTicker(apiKeyRotationInterval)
	defer ticker.Stop()
	s.runOnce()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func (s *APIKeyRotationService) runOnce() {
	now := time.Now().UTC()
	s.lastRun.Store(now.UnixNano())
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	defer cancel()
	if _, err := s.email.GetSMTPConfig(ctx); err != nil {
		s.deferredSMTP.Add(1)
		if !errors.Is(err, ErrEmailNotConfigured) {
			s.recordFailure(fmt.Errorf("read SMTP configuration before API key rotation: %w", err))
		}
		return
	}
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, apiKeyRotationLockKey, s.instanceID, apiKeyRotationLockTTL)
	if !ok {
		return
	}
	defer release()
	candidates, err := s.repo.ListDue(ctx, now, apiKeyRotationBatchSize)
	if err != nil {
		s.recordFailure(fmt.Errorf("list due API key rotations: %w", err))
		return
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		if err := s.rotateOne(ctx, candidate, now); err != nil {
			s.recordFailure(fmt.Errorf("rotate API key %d: %w", candidate.ID, err))
		}
	}
	s.lastSuccess.Store(time.Now().UTC().UnixNano())
}

func (s *APIKeyRotationService) rotateOne(ctx context.Context, candidate DueAPIKeyRotation, now time.Time) error {
	for attempt := 0; attempt < 5; attempt++ {
		newKey, err := s.apiKey.GenerateKey()
		if err != nil {
			return err
		}
		_, rotated, err := s.repo.RotateIfDue(ctx, candidate, newKey, now)
		if errors.Is(err, ErrAPIKeyExists) {
			continue
		}
		if err != nil {
			return err
		}
		if !rotated {
			return nil
		}
		s.apiKey.InvalidateAuthCacheByKey(ctx, candidate.OldKey)
		s.apiKey.InvalidateAuthCacheByKey(ctx, newKey)
		s.rotated.Add(1)
		return nil
	}
	return ErrAPIKeyExists
}

func (s *APIKeyRotationService) recordFailure(err error) {
	if err == nil {
		return
	}
	s.failures.Add(1)
	message := boundedNotificationEmailError(err, "")
	s.lastError.Store(message)
	slog.Warn("API key rotation failed", "error", message)
}

func (s *APIKeyRotationService) Health() APIKeyRotationHealth {
	health := APIKeyRotationHealth{Running: s != nil && s.running.Load()}
	if s == nil {
		return health
	}
	health.Rotated = s.rotated.Load()
	health.Failures = s.failures.Load()
	health.DeferredNoSMTP = s.deferredSMTP.Load()
	if value := s.lastError.Load(); value != nil {
		health.LastError, _ = value.(string)
	}
	if value := s.lastRun.Load(); value > 0 {
		health.LastRunAt = time.Unix(0, value).UTC()
	}
	if value := s.lastSuccess.Load(); value > 0 {
		health.LastSuccessfulAt = time.Unix(0, value).UTC()
	}
	return health
}
