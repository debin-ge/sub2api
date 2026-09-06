package service

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"

	"time"

	"github.com/google/uuid"
)

type videoCreateSessionContextKey struct{}

type VideoCreateIntentSession struct {
	Intent     *VideoCreateIntent
	Guard      VideoCreateIntentGuard
	Owned      bool
	repository VideoCreateIntentRepository
}

func IsIdempotentJSONVideoCreate(method, path, contentType, key string) bool {
	if method != http.MethodPost || strings.TrimSpace(key) == "" {
		return false
	}
	if _, supported := VideoCreateIntentOperationForPath(path); !supported {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func VideoCreateIntentOperationForPath(path string) (string, bool) {
	return ManagedVideoOperationForPath(path)
}

func NewVideoCreateIntentSession(repository VideoCreateIntentRepository, claim *VideoCreateIntentClaim) *VideoCreateIntentSession {
	return &VideoCreateIntentSession{Intent: claim.Intent, Guard: claim.Guard, Owned: claim.Owned, repository: repository}
}

func VideoCreateSessionFromContext(ctx context.Context) (*VideoCreateIntentSession, bool) {
	session, ok := ctx.Value(videoCreateSessionContextKey{}).(*VideoCreateIntentSession)
	return session, ok && session != nil
}

func (session *VideoCreateIntentSession) Context(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, videoCreateSessionContextKey{}, session)
	if session.Owned {
		ctx = WithVideoCreateIntent(ctx, session.Guard)
	}
	return ctx
}

func (s *VideoTaskService) BeginVideoCreateIntent(ctx context.Context, key *APIKey, operation, idempotencyKey, contentType string, body []byte) (*VideoCreateIntentSession, error) {
	if s == nil || key == nil || key.UserID <= 0 || key.ID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return nil, ErrVideoInvalidRequest
	}
	hash, err := CanonicalVideoCreateRequestHash(body)
	if err != nil {
		return nil, err
	}
	if err := ValidateVideoReleaseJSON(operation, body); err != nil {
		return nil, err
	}
	return s.BeginVideoCreateIntentWithHash(ctx, key, operation, idempotencyKey, hash, VideoCreateIntentJSONContract)
}

func (s *VideoTaskService) BeginVideoCreateIntentWithHash(ctx context.Context, key *APIKey, operation, idempotencyKey, requestHash, contract string) (*VideoCreateIntentSession, error) {
	if s == nil || key == nil || key.UserID <= 0 || key.ID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	if err := ValidateVideoReleaseOperation(operation); err != nil {
		return nil, err
	}
	repository, configured := s.tasks.(VideoCreateIntentRepository)
	if !configured {
		if s.tasks != nil {
			if task, err := s.tasks.GetVideoTaskByIdempotency(ctx, key.UserID, videoEndpointForOperation(operation), strings.TrimSpace(idempotencyKey)); err == nil && task != nil && task.UserID == key.UserID {
				return NewVideoCreateIntentSession(nil, &VideoCreateIntentClaim{Intent: &VideoCreateIntent{State: VideoCreateIntentNative, NativeTaskID: &task.ID, TargetPlatform: task.Provider}}), nil
			}
		}
		return nil, ErrBillingServiceUnavailable
	}
	claim, err := repository.ClaimVideoCreateIntent(ctx, VideoCreateIntentRequest{UserID: key.UserID, APIKeyID: key.ID,
		Endpoint: videoEndpointForOperation(operation), IdempotencyKey: strings.TrimSpace(idempotencyKey), RequestHash: strings.TrimSpace(requestHash), RequestContract: strings.TrimSpace(contract),
		LeaseOwner: uuid.NewString(), LeaseDuration: 2 * time.Minute})
	if err != nil {
		return nil, err
	}
	return NewVideoCreateIntentSession(repository, claim), nil
}

func (session *VideoCreateIntentSession) Renew(ctx context.Context) error {
	if session == nil || !session.Owned || session.repository == nil {
		return ErrVideoCreateFenceLost
	}
	err := session.repository.RenewVideoCreateIntent(ctx, session.Guard, 2*time.Minute)
	if errors.Is(err, ErrVideoCreateFenceLost) {
		intent, readErr := session.repository.ReadVideoCreateIntent(ctx, session.Guard)
		if readErr == nil && intent.State == VideoCreateIntentNative {
			return nil
		}
	}
	return err
}

func (session *VideoCreateIntentSession) Finish(ctx context.Context, status int, complete bool) error {
	if session == nil || session.repository == nil || !session.Owned {
		return ErrVideoCreateFenceLost
	}
	intent, err := session.repository.ReadVideoCreateIntent(ctx, session.Guard)
	if err != nil {
		return err
	}
	if intent.State == VideoCreateIntentNative {
		return nil
	}
	if intent.State == VideoCreateIntentPrepared {
		if complete && status >= 200 && status < 300 {
			if err := session.repository.QuarantineUntrackedVideoCreateIntent(ctx, session.Guard); err != nil {
				return err
			}
			return ErrVideoCreateOutcomeUnknown
		}
		return session.repository.ReleasePreparedVideoCreateIntent(ctx, session.Guard)
	}
	return ErrVideoCreateOutcomeUnknown
}
