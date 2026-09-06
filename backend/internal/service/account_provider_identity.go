package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AccountProviderPrincipalAccount      = "account"
	AccountProviderPrincipalOrganization = "organization"
	AccountProviderPrincipalProject      = "project"
	AccountProviderPrincipalTenant       = "tenant"
	AccountProviderPrincipalWorkspace    = "workspace"
)

var (
	ErrAccountProviderIdentityRequired  = infraerrors.BadRequest("ACCOUNT_PROVIDER_IDENTITY_REQUIRED", "an attributed provider identity review with a safe principal, evidence and idempotency key is required")
	ErrAccountProviderIdentityConflict  = infraerrors.Conflict("ACCOUNT_PROVIDER_IDENTITY_CONFLICT", "provider identity facts, aliases or idempotency key conflict; refresh before retrying")
	ErrAccountProviderIdentityForbidden = infraerrors.Forbidden("ACCOUNT_PROVIDER_IDENTITY_FORBIDDEN", "an active administrator with independent approval is required")
	ErrAccountProviderIdentityInvalid   = infraerrors.BadRequest("ACCOUNT_PROVIDER_IDENTITY_INVALID", "provider identity verification requires a root dedicated account with an unchanged identity version")
)

var providerPrincipalValuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{2,255}$`)
var providerPrincipalCredentialPattern = regexp.MustCompile(`(?i)^(sk-|xai-|sess-|bearer[._:-]?|eyj[a-z0-9_-]{8,}|ya29\.|gh[pousr]_|aiza)[a-z0-9._:/@-]*$`)

type AccountProviderIdentityProposal struct {
	ActorID         int64
	OperationKey    string
	ExpectedVersion int64
	Platform        string
	PrincipalKind   string
	Principal       string
	Reason          string
	EvidenceRef     string
	IssuerHash      string
	PrincipalHash   string
}

type AccountProviderIdentityDecision struct {
	ActorID         int64
	OperationKey    string
	ExpectedVersion int64
	Approve         bool
	Reason          string
}

type AccountProviderIdentityRevocation struct {
	ActorID      int64
	OperationKey string
	Reason       string
	EvidenceRef  string
}

type AccountProviderIdentityReview struct {
	ID                     int64      `json:"id"`
	AccountID              int64      `json:"account_id"`
	AccountIdentityVersion int64      `json:"account_identity_version"`
	Platform               string     `json:"platform"`
	PrincipalKind          string     `json:"principal_kind"`
	IssuerFingerprint      string     `json:"issuer_fingerprint"`
	PrincipalFingerprint   string     `json:"principal_fingerprint"`
	Status                 string     `json:"status"`
	ProposedBy             int64      `json:"proposed_by"`
	DecidedBy              *int64     `json:"decided_by,omitempty"`
	Reason                 string     `json:"reason"`
	EvidenceRef            string     `json:"evidence_ref"`
	DecisionReason         *string    `json:"decision_reason,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	DecidedAt              *time.Time `json:"decided_at,omitempty"`
}

type AccountProviderIdentityBinding struct {
	ID                     int64      `json:"id"`
	AccountID              int64      `json:"account_id"`
	AccountIdentityVersion int64      `json:"account_identity_version"`
	Platform               string     `json:"platform"`
	PrincipalKind          string     `json:"principal_kind"`
	IssuerFingerprint      string     `json:"issuer_fingerprint"`
	PrincipalFingerprint   string     `json:"principal_fingerprint"`
	VerificationReviewID   int64      `json:"verification_review_id"`
	VerifiedBy             int64      `json:"verified_by"`
	VerifiedAt             time.Time  `json:"verified_at"`
	RevokedBy              *int64     `json:"revoked_by,omitempty"`
	RevokedAt              *time.Time `json:"revoked_at,omitempty"`
}

type AccountProviderIdentityState struct {
	AccountID       int64                            `json:"account_id"`
	IdentityVersion int64                            `json:"identity_version"`
	IsolationState  string                           `json:"isolation_state"`
	Binding         *AccountProviderIdentityBinding  `json:"binding,omitempty"`
	Reviews         []*AccountProviderIdentityReview `json:"reviews"`
}

type AccountProviderIdentityResult struct {
	State              *AccountProviderIdentityState  `json:"state"`
	Review             *AccountProviderIdentityReview `json:"review,omitempty"`
	Replayed           bool                           `json:"replayed"`
	AffectedAccountIDs []int64                        `json:"affected_account_ids,omitempty"`
}

type AccountProviderIdentityRepository interface {
	GetAccountProviderIdentity(context.Context, int64) (*AccountProviderIdentityState, error)
	ProposeAccountProviderIdentity(context.Context, int64, AccountProviderIdentityProposal) (*AccountProviderIdentityResult, error)
	DecideAccountProviderIdentity(context.Context, int64, int64, AccountProviderIdentityDecision) (*AccountProviderIdentityResult, error)
	RevokeAccountProviderIdentity(context.Context, int64, AccountProviderIdentityRevocation) (*AccountProviderIdentityResult, error)
}

func validAccountProviderPrincipalKind(kind string) bool {
	switch kind {
	case AccountProviderPrincipalAccount, AccountProviderPrincipalOrganization, AccountProviderPrincipalProject,
		AccountProviderPrincipalTenant, AccountProviderPrincipalWorkspace:
		return true
	default:
		return false
	}
}

func validateAccountProviderIdentityProposal(request AccountProviderIdentityProposal) error {
	request.OperationKey = strings.TrimSpace(request.OperationKey)
	request.EvidenceRef = strings.TrimSpace(request.EvidenceRef)
	request.Reason = strings.TrimSpace(request.Reason)
	request.PrincipalKind = strings.ToLower(strings.TrimSpace(request.PrincipalKind))
	request.Principal = strings.TrimSpace(request.Principal)
	if request.ActorID <= 0 || request.ExpectedVersion <= 0 ||
		!videoReviewOpaqueReference.MatchString(request.OperationKey) ||
		!videoReviewOpaqueReference.MatchString(request.EvidenceRef) || strings.Contains(request.EvidenceRef, "://") ||
		videoReviewCredentialPattern.MatchString(request.OperationKey) || videoReviewCredentialPattern.MatchString(request.EvidenceRef) ||
		!validVideoReviewText(request.Reason) || !validAccountProviderPrincipalKind(request.PrincipalKind) ||
		!providerPrincipalValuePattern.MatchString(request.Principal) || strings.Contains(request.Principal, "://") ||
		videoReviewCredentialPattern.MatchString(request.Principal) || providerPrincipalCredentialPattern.MatchString(request.Principal) ||
		(strings.Count(request.Principal, ".") >= 2 && len(request.Principal) > 40) || containsUnicodeControl(request.Principal) {
		return ErrAccountProviderIdentityRequired
	}
	return nil
}

func validateAccountProviderIdentityDecision(request AccountProviderIdentityDecision) error {
	return ValidateVideoBillingReviewDecision(VideoBillingReviewDecision(request))
}

func validateAccountProviderIdentityRevocation(request AccountProviderIdentityRevocation) error {
	if request.ActorID <= 0 || !videoReviewOpaqueReference.MatchString(strings.TrimSpace(request.OperationKey)) ||
		!videoReviewOpaqueReference.MatchString(strings.TrimSpace(request.EvidenceRef)) || strings.Contains(request.EvidenceRef, "://") ||
		videoReviewCredentialPattern.MatchString(request.OperationKey) || videoReviewCredentialPattern.MatchString(request.EvidenceRef) ||
		!validVideoReviewText(request.Reason) {
		return ErrAccountProviderIdentityRequired
	}
	return nil
}

func containsUnicodeControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return true
		}
	}
	return false
}

func hashAccountProviderIdentity(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func accountProviderIdentityIssuer(account *Account) (string, error) {
	if account == nil || strings.TrimSpace(account.Platform) == "" {
		return "", ErrAccountProviderIdentityInvalid
	}
	raw := ""
	switch {
	case account.IsGrok():
		raw = account.GetGrokBaseURL()
	case account.IsOpenAI() || account.IsCNProvider():
		raw = account.GetOpenAIBaseURL()
	default:
		raw = strings.TrimSpace(account.GetCredential("base_url"))
	}
	if raw == "" {
		return "provider://" + strings.ToLower(strings.TrimSpace(account.Platform)), nil
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrAccountProviderIdentityInvalid
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	canonicalHost := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if canonicalHost == "" || containsUnicodeControl(canonicalHost) {
		return "", ErrAccountProviderIdentityInvalid
	}
	hostname := canonicalHost
	if strings.Contains(canonicalHost, ":") {
		hostname = "[" + canonicalHost + "]"
	}
	port := parsed.Port()
	if port != "" && (parsed.Scheme != "https" || port != "443") && (parsed.Scheme != "http" || port != "80") {
		hostname = net.JoinHostPort(canonicalHost, port)
	}
	cleanPath := path.Clean("/" + strings.TrimSpace(parsed.Path))
	if cleanPath == "/" || cleanPath == "/." {
		cleanPath = ""
	}
	return parsed.Scheme + "://" + hostname + cleanPath, nil
}

func prepareAccountProviderIdentityProposal(account *Account, request AccountProviderIdentityProposal) (AccountProviderIdentityProposal, error) {
	if err := validateAccountProviderIdentityProposal(request); err != nil {
		return AccountProviderIdentityProposal{}, err
	}
	if account == nil || account.ParentAccountID != nil || account.OwnershipMode != AccountOwnershipUserDedicated ||
		account.OwnerUserID == nil || *account.OwnerUserID <= 0 || account.ProviderIdentityVersion != request.ExpectedVersion ||
		account.IsolationState == AccountIsolationRevoked {
		return AccountProviderIdentityProposal{}, ErrAccountProviderIdentityInvalid
	}
	issuer, err := accountProviderIdentityIssuer(account)
	if err != nil {
		return AccountProviderIdentityProposal{}, err
	}
	request.OperationKey = strings.TrimSpace(request.OperationKey)
	request.EvidenceRef = strings.TrimSpace(request.EvidenceRef)
	request.Reason = strings.TrimSpace(request.Reason)
	request.PrincipalKind = strings.ToLower(strings.TrimSpace(request.PrincipalKind))
	request.Principal = strings.TrimSpace(request.Principal)
	request.IssuerHash = hashAccountProviderIdentity(strings.ToLower(strings.TrimSpace(account.Platform)) + "\x00" + issuer)
	request.PrincipalHash = hashAccountProviderIdentity(strings.ToLower(strings.TrimSpace(account.Platform)) + "\x00" + request.PrincipalKind + "\x00" + request.Principal)
	request.Platform = strings.TrimSpace(account.Platform)
	request.Principal = ""
	return request, nil
}
