//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountProviderIdentityHandlerService struct {
	service.AdminService
	proposal service.AccountProviderIdentityProposal
	calls    int
}

func (s *accountProviderIdentityHandlerService) GetAccountProviderIdentity(context.Context, int64) (*service.AccountProviderIdentityState, error) {
	return &service.AccountProviderIdentityState{AccountID: 42, IdentityVersion: 3, IsolationState: service.AccountIsolationUnverified, Reviews: []*service.AccountProviderIdentityReview{}}, nil
}

func (s *accountProviderIdentityHandlerService) ProposeAccountProviderIdentity(_ context.Context, accountID int64, request service.AccountProviderIdentityProposal) (*service.AccountProviderIdentityResult, error) {
	s.calls++
	s.proposal = request
	review := &service.AccountProviderIdentityReview{ID: 9, AccountID: accountID, AccountIdentityVersion: request.ExpectedVersion,
		Platform: service.PlatformOpenAI, PrincipalKind: request.PrincipalKind, PrincipalFingerprint: "0123456789abcdef",
		IssuerFingerprint: "fedcba9876543210", Status: "pending", ProposedBy: request.ActorID,
		Reason: request.Reason, EvidenceRef: request.EvidenceRef}
	return &service.AccountProviderIdentityResult{State: &service.AccountProviderIdentityState{
		AccountID: accountID, IdentityVersion: request.ExpectedVersion, IsolationState: service.AccountIsolationUnverified,
		Reviews: []*service.AccountProviderIdentityReview{review},
	}, Review: review}, nil
}

func (s *accountProviderIdentityHandlerService) DecideAccountProviderIdentity(context.Context, int64, int64, service.AccountProviderIdentityDecision) (*service.AccountProviderIdentityResult, error) {
	return nil, service.ErrAccountProviderIdentityInvalid
}

func (s *accountProviderIdentityHandlerService) RevokeAccountProviderIdentity(context.Context, int64, service.AccountProviderIdentityRevocation) (*service.AccountProviderIdentityResult, error) {
	return nil, service.ErrAccountProviderIdentityInvalid
}

func performAccountProviderIdentityRequest(t *testing.T, serviceStub service.AdminService, body string, ifMatch string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	handler := NewAccountHandler(serviceStub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/accounts/:id/provider-identity/reviews", handler.ProposeProviderIdentity)
	request := httptest.NewRequest(http.MethodPost, "/accounts/42/provider-identity/reviews", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "identity:propose")
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAccountProviderIdentityHandlerRequiresVersionAndDoesNotEchoPrincipal(t *testing.T) {
	stub := &accountProviderIdentityHandlerService{}
	body := `{"principal_kind":"project","principal":"proj_sensitive-123","reason":"Provider console identity verified","evidence_ref":"ticket:IDENTITY-1"}`

	missing := performAccountProviderIdentityRequest(t, stub, body, "")
	require.Equal(t, http.StatusPreconditionRequired, missing.Code)
	require.Zero(t, stub.calls)

	invalid := performAccountProviderIdentityRequest(t, stub, body, `"0"`)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Zero(t, stub.calls)

	valid := performAccountProviderIdentityRequest(t, stub, body, `"3"`)
	require.Equal(t, http.StatusOK, valid.Code)
	require.Equal(t, 1, stub.calls)
	require.Equal(t, int64(99), stub.proposal.ActorID)
	require.Equal(t, int64(3), stub.proposal.ExpectedVersion)
	require.Equal(t, "identity:propose", stub.proposal.OperationKey)
	require.Equal(t, "proj_sensitive-123", stub.proposal.Principal)
	require.NotContains(t, valid.Body.String(), "proj_sensitive-123")
	require.Contains(t, valid.Body.String(), "0123456789abcdef")
}

func TestAccountProviderIdentityHandlerRejectsUnknownJSONFields(t *testing.T) {
	stub := &accountProviderIdentityHandlerService{}
	body := `{"principal_kind":"project","principal":"proj_abc-123","reason":"Provider console identity verified","evidence_ref":"ticket:IDENTITY-1","unexpected":true}`
	recorder := performAccountProviderIdentityRequest(t, stub, body, `"3"`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, stub.calls)
}
