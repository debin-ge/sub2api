package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
)

const (
	VideoCreateIntentPrepared          = "prepared"
	VideoCreateIntentNative            = "native_bound"
	VideoCreateIntentUntracked         = "untracked"
	VideoCreateIntentJSONContract      = "canonical_json_v1"
	VideoCreateIntentMultipartContract = "canonical_multipart_v1"
	VideoCreateIntentNativeContract    = "native_task_v1"
	VideoCreateJSONMaxBytes            = 1 << 20
)

var (
	ErrVideoCreateInProgress     = infraerrors.Conflict("VIDEO_CREATE_IN_PROGRESS", "this video creation is already being prepared; retry the same request later")
	ErrVideoCreateOutcomeUnknown = infraerrors.Conflict("VIDEO_CREATE_OUTCOME_UNKNOWN", "the original video creation outcome is uncertain; do not create another request before reconciliation")
	ErrVideoCreateFenceLost      = infraerrors.Conflict("VIDEO_CREATE_FENCE_LOST", "video creation ownership changed; this attempt must not submit")
)

type VideoCreateIntent struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	APIKeyID        *int64     `json:"api_key_id,omitempty"`
	Endpoint        string     `json:"endpoint"`
	KeyHash         string     `json:"key_hash"`
	RequestHash     string     `json:"request_hash"`
	RequestContract string     `json:"request_contract"`
	State           string     `json:"state"`
	TargetPlatform  string     `json:"target_platform"`
	NativeTaskID    *int64     `json:"native_task_id,omitempty"`
	AccountID       *int64     `json:"account_id,omitempty"`
	LeaseOwner      string     `json:"lease_owner"`
	LeaseEpoch      int64      `json:"lease_epoch"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	LastErrorCode   string     `json:"last_error_code"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type VideoCreateIntentRequest struct {
	UserID          int64
	APIKeyID        int64
	Endpoint        string
	IdempotencyKey  string `json:"-"`
	RequestHash     string
	RequestContract string
	LeaseOwner      string `json:"-"`
	LeaseDuration   time.Duration
}

type VideoCreateIntentGuard struct {
	ID             int64
	UserID         int64
	APIKeyID       int64
	Endpoint       string
	IdempotencyKey string `json:"-"`
	LeaseOwner     string `json:"-"`
	LeaseEpoch     int64
}

func (intent VideoCreateIntent) MarshalJSON() ([]byte, error) {
	type fields VideoCreateIntent
	return json.Marshal(struct {
		fields
		LeaseOwner string `json:"lease_owner,omitempty"`
	}{fields: fields(intent)})
}

type VideoCreateIntentClaim struct {
	Intent *VideoCreateIntent
	Guard  VideoCreateIntentGuard
	Owned  bool
}

type VideoCreateIntentRepository interface {
	ClaimVideoCreateIntent(context.Context, VideoCreateIntentRequest) (*VideoCreateIntentClaim, error)
	RenewVideoCreateIntent(context.Context, VideoCreateIntentGuard, time.Duration) error
	ReleasePreparedVideoCreateIntent(context.Context, VideoCreateIntentGuard) error
	ReadVideoCreateIntent(context.Context, VideoCreateIntentGuard) (*VideoCreateIntent, error)
	QuarantineUntrackedVideoCreateIntent(context.Context, VideoCreateIntentGuard) error
}

type videoCreateIntentContextKey struct{}

func WithVideoCreateIntent(ctx context.Context, guard VideoCreateIntentGuard) context.Context {
	return context.WithValue(ctx, videoCreateIntentContextKey{}, guard)
}

func VideoCreateIntentFromContext(ctx context.Context) (VideoCreateIntentGuard, bool) {
	guard, ok := ctx.Value(videoCreateIntentContextKey{}).(VideoCreateIntentGuard)
	return guard, ok
}

func VideoCreateIntentKeyHash(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func ValidVideoCreateIntentEndpoint(endpoint string) bool {
	return endpoint == CompositeRouteEndpointVideos || endpoint == CompositeRouteEndpointVideoEdits ||
		endpoint == CompositeRouteEndpointVideoExtensions || endpoint == CompositeRouteEndpointVideoCharacters
}

func ValidateVideoCreateIntentRequest(request VideoCreateIntentRequest) error {
	key := strings.TrimSpace(request.IdempotencyKey)
	decoded, err := hex.DecodeString(request.RequestHash)
	if request.UserID <= 0 || request.APIKeyID <= 0 || !ValidVideoCreateIntentEndpoint(request.Endpoint) || key == "" || len(key) > 255 ||
		!utf8.ValidString(key) || strings.ContainsAny(key, "\r\n\x00") || len(request.LeaseOwner) < 16 || len(request.LeaseOwner) > 128 ||
		request.LeaseDuration < time.Second || request.LeaseDuration > 5*time.Minute || err != nil || len(decoded) != sha256.Size ||
		strings.ToLower(request.RequestHash) != request.RequestHash ||
		(request.RequestContract != "" && request.RequestContract != VideoCreateIntentJSONContract && request.RequestContract != VideoCreateIntentMultipartContract) {
		return ErrVideoInvalidRequest
	}
	return nil
}

func CanonicalVideoCreateRequestHash(body []byte) (string, error) {
	if len(body) == 0 || len(body) > VideoCreateJSONMaxBytes || !utf8.Valid(body) || jsonstrict.RejectDuplicateKeys(body) != nil {
		return "", ErrVideoInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return "", ErrVideoInvalidRequest
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", ErrVideoInvalidRequest
	}
	return HashVideoRequest(value)
}
