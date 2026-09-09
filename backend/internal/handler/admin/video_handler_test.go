package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoAdminHandlerStub struct {
	videoAdminController
	task              *service.VideoTask
	retriedGet        string
	retriedSettlement string
	catalog           service.VideoCapabilityCatalogDocument
	capabilityAccount int64
	capabilityProbed  bool
}

func (s *videoAdminHandlerStub) RetryProviderGet(_ context.Context, publicID string) (*service.VideoTask, error) {
	s.retriedGet = publicID
	return s.task, nil
}

func (s *videoAdminHandlerStub) RetrySettlement(_ context.Context, publicID string) (*service.VideoTask, error) {
	s.retriedSettlement = publicID
	return s.task, nil
}

func (s *videoAdminHandlerStub) GetAccountCapability(_ context.Context, accountID int64) (*service.VideoAccountCapabilityStatus, error) {
	s.capabilityAccount = accountID
	return &service.VideoAccountCapabilityStatus{AccountID: accountID}, nil
}

func (s *videoAdminHandlerStub) ProbeAccountCapability(_ context.Context, accountID int64) (*service.VideoAccountCapabilityStatus, error) {
	s.capabilityAccount = accountID
	s.capabilityProbed = true
	return &service.VideoAccountCapabilityStatus{AccountID: accountID}, nil
}

func (s *videoAdminHandlerStub) UpdateCapabilityCatalog(_ context.Context, document service.VideoCapabilityCatalogDocument) (*service.VideoCapabilityCatalogView, error) {
	s.catalog = document
	return &service.VideoCapabilityCatalogView{VideoCapabilityCatalogDocument: document, Source: "settings", LoadedAt: time.Now().UTC()}, nil
}

func (s *videoAdminHandlerStub) GetTask(context.Context, string) (*service.VideoTask, error) {
	return s.task, nil
}

func performVideoAdminRequest(handler gin.HandlerFunc, method, route, path string, body []byte, ifMatch ...string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.Handle(method, route, handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	value := `"0"`
	if len(ifMatch) > 0 {
		value = ifMatch[0]
	}
	if value != "" {
		request.Header.Set("If-Match", value)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestVideoAdminTaskMutationRequiresExplicitVersion(t *testing.T) {
	task := &service.VideoTask{PublicID: "video_0123456789abcdef0123456789abcdef"}
	for _, header := range []string{"", "*", `W/"0"`, `"-1"`, `"not-a-version"`} {
		t.Run(header, func(t *testing.T) {
			stub := &videoAdminHandlerStub{task: task}
			recorder := performVideoAdminRequest(newVideoHandler(stub).RetryProviderGet, http.MethodPost, "/tasks/:id/retry-get", "/tasks/"+task.PublicID+"/retry-get", nil, header)
			if header == "" {
				require.Equal(t, http.StatusPreconditionRequired, recorder.Code)
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
			}
		})
	}
}

func TestVideoAdminTaskProjectionNeverReturnsEncryptedSecrets(t *testing.T) {
	accessKind, accessScope := "token", "video_content"
	accessCipher, callbackCipher := "cipher:task-secret", "cipher:https://callback.internal/secret"
	providerTaskID := "video_upstream_123"
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	stub := &videoAdminHandlerStub{task: &service.VideoTask{
		ID: 1, PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 9,
		Version: 7, LeaseEpoch: 3,
		Provider: service.VideoProviderOpenAI, Operation: service.VideoOperationGenerate,
		ProviderTaskID: &providerTaskID, GenerationState: service.VideoGenerationQueued,
		BillingState: service.VideoBillingHeld, DeleteState: service.VideoDeleteNone,
		ProviderAccessKind: &accessKind, ProviderAccessScope: &accessScope,
		ProviderAccessEnc: &accessCipher, CallbackURLEnc: &callbackCipher,
		CreatedAt: now, UpdatedAt: now,
	}}

	recorder := performVideoAdminRequest(newVideoHandler(stub).GetTask, http.MethodGet, "/tasks/:id", "/tasks/"+stub.task.PublicID, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Contains(t, recorder.Body.String(), `"provider_task_id":"video_upstream_123"`)
	require.Contains(t, recorder.Body.String(), `"provider_access":{"configured":true,"kind":"token","scope":"video_content"}`)
	require.Contains(t, recorder.Body.String(), `"callback_configured":true`)
	require.Contains(t, recorder.Body.String(), `"version":7`)
	require.Contains(t, recorder.Body.String(), `"lease_epoch":3`)
	require.NotContains(t, recorder.Body.String(), "task-secret")
	require.NotContains(t, recorder.Body.String(), "callback.internal")
	require.NotContains(t, recorder.Body.String(), "provider_access_enc")
	require.NotContains(t, recorder.Body.String(), "callback_url_enc")
}

func TestVideoAdminTaskRetriesAcceptOnlyAnEmptyBody(t *testing.T) {
	task := &service.VideoTask{
		PublicID:        "video_0123456789abcdef0123456789abcdef",
		GenerationState: service.VideoGenerationQueued, BillingState: service.VideoBillingHeld,
		DeleteState: service.VideoDeleteNone, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	stub := &videoAdminHandlerStub{task: task}
	handler := newVideoHandler(stub)

	// The surviving admin mutations are pure retries: they carry no operator
	// decision, so any payload is rejected outright.
	recorder := performVideoAdminRequest(handler.RetryProviderGet, http.MethodPost, "/tasks/:id/retry-get", "/tasks/"+task.PublicID+"/retry-get", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, task.PublicID, stub.retriedGet)

	bad := performVideoAdminRequest(handler.RetrySettlement, http.MethodPost, "/tasks/:id/retry-settlement", "/tasks/"+task.PublicID+"/retry-settlement", []byte(`{"actual_units":3.5}`))
	require.Equal(t, http.StatusBadRequest, bad.Code)
	require.Empty(t, stub.retriedSettlement)
}

func TestVideoAdminUpdateCapabilityCatalogUsesStrictJSON(t *testing.T) {
	stub := &videoAdminHandlerStub{}
	handler := newVideoHandler(stub)
	valid, err := json.Marshal(service.DefaultVideoCapabilityCatalogDocument())
	require.NoError(t, err)

	recorder := performVideoAdminRequest(handler.UpdateCapabilityCatalog, http.MethodPut, "/capabilities", "/capabilities", valid)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, service.VideoCapabilityCatalogVersion, stub.catalog.Version)

	duplicate := performVideoAdminRequest(handler.UpdateCapabilityCatalog, http.MethodPut, "/capabilities", "/capabilities", []byte(`{"version":1,"version":1,"providers":{}}`))
	require.Equal(t, http.StatusBadRequest, duplicate.Code)
}

func TestVideoAdminAccountCapabilityEndpoints(t *testing.T) {
	stub := &videoAdminHandlerStub{}
	handler := newVideoHandler(stub)
	get := performVideoAdminRequest(handler.GetAccountCapability, http.MethodGet, "/accounts/:id/capability", "/accounts/42/capability", nil)
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, int64(42), stub.capabilityAccount)

	probe := performVideoAdminRequest(handler.ProbeAccountCapability, http.MethodPost, "/accounts/:id/capability/probe", "/accounts/42/capability/probe", nil)
	require.Equal(t, http.StatusOK, probe.Code)
	require.True(t, stub.capabilityProbed)

	bad := performVideoAdminRequest(handler.ProbeAccountCapability, http.MethodPost, "/accounts/:id/capability/probe", "/accounts/not-an-id/capability/probe", nil)
	require.Equal(t, http.StatusBadRequest, bad.Code)
}

func TestVideoAdminTaskProjectionIncludesTypedGenerationAndBillingDetails(t *testing.T) {
	billingUnit := service.VideoBillingUnitVideoToken
	actualUnits := 125_000.0
	stub := &videoAdminHandlerStub{task: &service.VideoTask{
		PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 9,
		Provider: service.VideoProviderOpenAI, Operation: service.VideoOperationGenerate,
		RequestedModel: "requested", PublicModel: "public", ChannelModel: "channel", UpstreamModel: "upstream",
		GenerationState: service.VideoGenerationCompleted, BillingState: service.VideoBillingCaptured,
		DeleteState: service.VideoDeleteNone, BillingUnit: &billingUnit, ActualUnits: &actualUnits,
		PriceSnapshot: map[string]any{
			"unit_price": 0.000002, "customer_multiplier": 1.5, "estimated_cost": 0.3,
			"pricing_source": "model_override", "rule_key": "480p-reference-video", "resolution": "480p",
		},
		UsageSnapshot:    map[string]any{"video_tokens": float64(125_000)},
		ResponseMetadata: map[string]any{"size": "864x480", "seconds": "7"},
		CreatedAt:        time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}}

	recorder := performVideoAdminRequest(newVideoHandler(stub).GetTask, http.MethodGet, "/tasks/:id", "/tasks/"+stub.task.PublicID, nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	data, ok := response["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 0.000002, data["unit_price"])
	require.Equal(t, 1.5, data["customer_multiplier"])
	require.Equal(t, 0.3, data["estimated_cost"])
	require.Equal(t, "model_override", data["pricing_source"])
	require.Equal(t, "480p-reference-video", data["pricing_rule_key"])
	require.Equal(t, "864x480", data["resolution"])
	require.Equal(t, float64(7), data["duration_seconds"])
	require.Equal(t, float64(125_000), data["video_tokens"])
}
