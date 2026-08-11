// Package internalrelay implements the authenticated marker used when an
// OpenAI API-key account intentionally relays a request back into this server.
package internalrelay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// HeaderName is deliberately private to sub2api. Inbound middleware always
	// removes it before the request can be forwarded again.
	HeaderName = "X-Sub2API-Internal-Relay"

	// UsageRequestIDPrefix marks persisted inner usage rows. The remainder is
	// <base64url(parent-request-id)>:<inner-request-id>.
	UsageRequestIDPrefix = "internal-relay:"

	markerVersion  = "v1"
	markerLifetime = 5 * time.Minute
	maxMarkerBytes = 4096
	maxParentBytes = 255
	clockSkew      = 30 * time.Second
)

var (
	errSignerUnavailable = errors.New("internal relay signer is unavailable")
	errInvalidMarker     = errors.New("invalid internal relay marker")
)

// Metadata is stored in request context only after a marker has been verified.
type Metadata struct {
	Version         string
	AccountID       int64
	IssuedAt        time.Time
	ParentRequestID string
}

type markerPayload struct {
	Version         string `json:"v"`
	AccountID       int64  `json:"a"`
	IssuedAtUnix    int64  `json:"iat"`
	ParentRequestID string `json:"p"`
}

// Signer signs and verifies relay markers with a key derived from the JWT
// secret. Keeping a domain-separated key prevents marker signatures from
// being interchangeable with JWTs or other HMAC-based features.
type Signer struct {
	key [sha256.Size]byte
	ok  bool
}

func NewSigner(jwtSecret string) *Signer {
	secret := strings.TrimSpace(jwtSecret)
	s := &Signer{}
	if secret == "" {
		return s
	}
	s.key = sha256.Sum256([]byte("sub2api/internal-relay/v1\x00" + secret))
	s.ok = true
	return s
}

// Sign creates a short-lived marker for one outbound relay request.
func (s *Signer) Sign(accountID int64, parentRequestID string, now time.Time) (string, error) {
	parentRequestID = strings.TrimSpace(parentRequestID)
	if s == nil || !s.ok {
		return "", errSignerUnavailable
	}
	if accountID <= 0 || parentRequestID == "" || len(parentRequestID) > maxParentBytes {
		return "", errInvalidMarker
	}
	if now.IsZero() {
		now = time.Now()
	}
	payloadBytes, err := json.Marshal(markerPayload{
		Version:         markerVersion,
		AccountID:       accountID,
		IssuedAtUnix:    now.UTC().Unix(),
		ParentRequestID: parentRequestID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal internal relay marker: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := s.signature(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// Verify authenticates a marker and enforces the fixed five-minute lifetime.
func (s *Signer) Verify(raw string, now time.Time) (Metadata, error) {
	if s == nil || !s.ok {
		return Metadata{}, errSignerUnavailable
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxMarkerBytes {
		return Metadata{}, errInvalidMarker
	}
	payload, encodedSignature, ok := strings.Cut(raw, ".")
	if !ok || payload == "" || encodedSignature == "" || strings.Contains(encodedSignature, ".") {
		return Metadata{}, errInvalidMarker
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || len(signature) != sha256.Size || !hmac.Equal(signature, s.signature(payload)) {
		return Metadata{}, errInvalidMarker
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Metadata{}, errInvalidMarker
	}
	var decoded markerPayload
	if err := json.Unmarshal(payloadBytes, &decoded); err != nil {
		return Metadata{}, errInvalidMarker
	}
	decoded.ParentRequestID = strings.TrimSpace(decoded.ParentRequestID)
	if decoded.Version != markerVersion || decoded.AccountID <= 0 || decoded.ParentRequestID == "" || len(decoded.ParentRequestID) > maxParentBytes {
		return Metadata{}, errInvalidMarker
	}
	if !strings.HasPrefix(decoded.ParentRequestID, "client:") {
		return Metadata{}, errInvalidMarker
	}
	issuedAt := time.Unix(decoded.IssuedAtUnix, 0).UTC()
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	if issuedAt.After(now.Add(clockSkew)) || now.Sub(issuedAt) > markerLifetime {
		return Metadata{}, errInvalidMarker
	}
	return Metadata{
		Version:         decoded.Version,
		AccountID:       decoded.AccountID,
		IssuedAt:        issuedAt,
		ParentRequestID: decoded.ParentRequestID,
	}, nil
}

func (s *Signer) signature(payload string) []byte {
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// ClientRequestID converts the gateway correlation ID into the same namespace
// used by normal persisted usage request IDs.
func ClientRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "client:") {
		return value
	}
	return "client:" + value
}

// MarkUsageRequestID preserves both the verified parent and the unique inner
// request ID without requiring a schema change.
func MarkUsageRequestID(parentRequestID, innerRequestID string) string {
	parentRequestID = strings.TrimSpace(parentRequestID)
	innerRequestID = strings.TrimSpace(innerRequestID)
	if parentRequestID == "" || innerRequestID == "" || strings.HasPrefix(innerRequestID, UsageRequestIDPrefix) {
		return innerRequestID
	}
	encodedParent := base64.RawURLEncoding.EncodeToString([]byte(parentRequestID))
	return UsageRequestIDPrefix + encodedParent + ":" + innerRequestID
}

// ParseUsageRequestID derives the relay metadata exposed by usage-log APIs.
func ParseUsageRequestID(requestID string) (parentRequestID string, ok bool) {
	requestID = strings.TrimSpace(requestID)
	if !strings.HasPrefix(requestID, UsageRequestIDPrefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(requestID, UsageRequestIDPrefix)
	encodedParent, innerRequestID, found := strings.Cut(remainder, ":")
	if !found || encodedParent == "" || strings.TrimSpace(innerRequestID) == "" {
		return "", false
	}
	parent, err := base64.RawURLEncoding.DecodeString(encodedParent)
	if err != nil {
		return "", false
	}
	parentRequestID = strings.TrimSpace(string(parent))
	if parentRequestID == "" || len(parentRequestID) > maxParentBytes {
		return "", false
	}
	return parentRequestID, true
}

// IsLoopbackBaseURL accepts only HTTP(S) URLs whose host is localhost,
// 127.0.0.0/8, or ::1. It intentionally rejects hostnames that merely resolve
// to loopback so the saved configuration cannot later change via DNS.
func IsLoopbackBaseURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4[0] == 127
	}
	return ip.Equal(net.IPv6loopback)
}
