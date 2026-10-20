package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrGeminiCachedContentCreationPending = infraerrors.New(http.StatusConflict,
	"GEMINI_CACHED_CONTENT_CREATION_PENDING", "a previous cache creation is in progress or requires reconciliation")

// GeminiCachedContentCreationIntent is persisted before sending a supplier POST.
// The upstream displayName is s2a-<PublicID>; it is an observation tag, not an
// idempotency guarantee. No prompt contents or credentials are stored here.
type GeminiCachedContentCreationIntent struct {
	PublicID               string
	UserID, APIKeyID       int64
	AccountID              int64
	AccountIdentityVersion int64
	UpstreamModel          string
	TTL                    time.Duration
}

type GeminiCachedContentCreationRepository interface {
	BeginCreation(context.Context, *GeminiCachedContentCreationIntent) error
	ResolveCreation(context.Context, string, string) error
}

func (s *GeminiCachedContentService) BeginCreation(ctx context.Context, intent *GeminiCachedContentCreationIntent) error {
	repo, ok := s.repo.(GeminiCachedContentCreationRepository)
	if !ok {
		return errors.New("cache creation intent storage unavailable")
	}
	return repo.BeginCreation(ctx, intent)
}

// ResolveCreation is only called after a definitive refusal, a not-sent error,
// or a successfully recorded/cleaned-up resource with recorded usage. Unknown outcomes stay pending
// for evidence-backed reconciliation; neither time nor a disconnect clears them.
func (s *GeminiCachedContentService) ResolveCreation(ctx context.Context, publicID, resolution string) error {
	repo, ok := s.repo.(GeminiCachedContentCreationRepository)
	if !ok {
		return errors.New("cache creation intent storage unavailable")
	}
	return repo.ResolveCreation(ctx, publicID, resolution)
}

// DefinitivelyRejectsCreation excludes timeouts, conflicts and server errors:
// those may be emitted after the supplier has already performed the POST.
func (e *GeminiCachedContentUpstreamError) DefinitivelyRejectsCreation() bool {
	if e == nil {
		return false
	}
	switch e.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}
