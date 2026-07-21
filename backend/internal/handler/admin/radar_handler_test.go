package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type radarAdminHandlerStub struct {
	status        *service.RadarAdminStatus
	statusErr     error
	settings      *service.RadarAdminSettings
	settingsErr   error
	refresh       *service.RadarAdminRefreshResult
	refreshErr    error
	enabled       bool
	settingsAudit service.RadarAdminAuditContext
	refreshAudit  service.RadarAdminAuditContext
	refreshCalled chan struct{}
}

func (s *radarAdminHandlerStub) GetStatus(context.Context) (*service.RadarAdminStatus, error) {
	return s.status, s.statusErr
}
func (s *radarAdminHandlerStub) SetEnabled(_ context.Context, enabled bool, audit service.RadarAdminAuditContext) (*service.RadarAdminSettings, error) {
	s.enabled, s.settingsAudit = enabled, audit
	return s.settings, s.settingsErr
}
func (s *radarAdminHandlerStub) TriggerRefresh(audit service.RadarAdminAuditContext) (*service.RadarAdminRefreshResult, error) {
	s.refreshAudit = audit
	if s.refreshCalled != nil {
		close(s.refreshCalled)
	}
	return s.refresh, s.refreshErr
}

func performRadarAdminRequest(t *testing.T, handler gin.HandlerFunc, method, path string, body []byte, canceled bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 77})
		ctx := context.WithValue(c.Request.Context(), ctxkey.RequestID, "request-77")
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	routePath, _, _ := strings.Cut(path, "?")
	router.Handle(method, routePath, handler)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRadarAdminHandlerStatusNoStoreAndStrictDTO(t *testing.T) {
	now := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	stub := &radarAdminHandlerStub{status: &service.RadarAdminStatus{
		Enabled:    true,
		Sources:    []service.RadarAdminSourceStatus{{Key: "lmarena", Status: service.RadarAdminStateFailed, LastAttemptAt: &now, LastFailureAt: &now}},
		Aggregator: service.RadarAdminSourceStatus{Key: "quota_aggregator", Status: service.RadarAdminStateNeverAttempted},
	}}
	handler := newRadarHandler(stub)
	response := performRadarAdminRequest(t, handler.GetStatus, http.MethodGet, "/status", nil, false)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"code":0,"message":"success","data":{"enabled":true,"sources":[{"key":"lmarena","status":"failed","stale":false,"last_attempt_at":"2026-07-15T03:00:00Z","last_success_at":null,"last_failure_at":"2026-07-15T03:00:00Z","next_fire_at":null,"http_status":null,"error":null}],"aggregator":{"key":"quota_aggregator","status":"never_attempted","stale":false,"last_attempt_at":null,"last_success_at":null,"last_failure_at":null,"next_fire_at":null,"http_status":null,"error":null}}}`, response.Body.String())

	bad := performRadarAdminRequest(t, handler.GetStatus, http.MethodGet, "/status?secret=1", nil, false)
	require.Equal(t, http.StatusBadRequest, bad.Code)
	require.Equal(t, "no-store", bad.Header().Get("Cache-Control"))
}

func TestRadarAdminHandlerSettingsStrictBodyAndAuditIdentity(t *testing.T) {
	stub := &radarAdminHandlerStub{settings: &service.RadarAdminSettings{Enabled: false}}
	handler := newRadarHandler(stub)
	response := performRadarAdminRequest(t, handler.UpdateSettings, http.MethodPut, "/settings", []byte(`{"enabled":false}`), false)
	require.Equal(t, http.StatusOK, response.Code)
	require.False(t, stub.enabled)
	require.Equal(t, int64(77), stub.settingsAudit.AdminUserID)
	require.Equal(t, "request-77", stub.settingsAudit.RequestID)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"enabled":false}}`, response.Body.String())

	for _, body := range []string{`{}`, `{"enabled":false,"extra":"secret"}`, `{"enabled":false}{"enabled":true}`, `null`} {
		bad := performRadarAdminRequest(t, handler.UpdateSettings, http.MethodPut, "/settings", []byte(body), false)
		require.Equal(t, http.StatusBadRequest, bad.Code, "body=%s response=%s", body, bad.Body.String())
		require.NotContains(t, bad.Body.String(), "secret")
	}
}

func TestRadarAdminHandlerRefreshRequiresNoBodyAndIgnoresRequestCancellation(t *testing.T) {
	stub := &radarAdminHandlerStub{refresh: &service.RadarAdminRefreshResult{RefreshID: "refresh-0123456789abcdef0123456789abcdef", Status: service.RadarAdminRefreshTriggered, Tasks: []string{"lmarena", "quota_aggregator"}}, refreshCalled: make(chan struct{})}
	handler := newRadarHandler(stub)
	response := performRadarAdminRequest(t, handler.Refresh, http.MethodPost, "/refresh", nil, true)
	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, int64(77), stub.refreshAudit.AdminUserID)
	select {
	case <-stub.refreshCalled:
	default:
		t.Fatal("refresh controller was not called after request cancellation")
	}

	for _, body := range []string{`{}`, `{"force_all":true}`, ` `} {
		badStub := &radarAdminHandlerStub{}
		bad := performRadarAdminRequest(t, newRadarHandler(badStub).Refresh, http.MethodPost, "/refresh", []byte(body), false)
		require.Equal(t, http.StatusBadRequest, bad.Code, "body=%q", body)
	}
}

func TestRadarAdminHandlerUsesFixedSafeUnavailableResponse(t *testing.T) {
	stub := &radarAdminHandlerStub{statusErr: errors.New("redis://secret@internal"), settingsErr: errors.New("database secret"), refreshErr: service.ErrRadarAdminUnavailable}
	handler := newRadarHandler(stub)
	requests := []*httptest.ResponseRecorder{
		performRadarAdminRequest(t, handler.GetStatus, http.MethodGet, "/status", nil, false),
		performRadarAdminRequest(t, handler.UpdateSettings, http.MethodPut, "/settings", []byte(`{"enabled":true}`), false),
		performRadarAdminRequest(t, handler.Refresh, http.MethodPost, "/refresh", nil, false),
	}
	for _, response := range requests {
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.JSONEq(t, `{"code":503,"message":"radar control unavailable"}`, response.Body.String())
		require.NotContains(t, response.Body.String(), "secret")
	}
}
