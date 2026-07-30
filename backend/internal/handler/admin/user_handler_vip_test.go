package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type vipAdminServiceStub struct {
	service.AdminService

	mode          service.VIPMode
	actorID       int64
	reason        string
	auditPage     int
	auditLimit    int
	previewCursor string
	previewLimit  int
}

func (s *vipAdminServiceStub) SetUserVIPMode(
	_ context.Context,
	userID int64,
	mode service.VIPMode,
	actorID int64,
	reason string,
) (*service.User, error) {
	s.mode = mode
	s.actorID = actorID
	s.reason = reason
	override := mode.ManualOverride()
	return &service.User{
		ID: userID,
		VIPEntitlementSnapshot: service.VIPEntitlementSnapshot{
			ManualOverride: override,
			IsVIP:          service.EffectiveVIPState(false, override),
		},
	}, nil
}

func (s *vipAdminServiceStub) ListUserVIPAudit(
	_ context.Context,
	userID int64,
	page, pageSize int,
) ([]service.VIPAuditEvent, int64, error) {
	s.auditPage = page
	s.auditLimit = pageSize
	return []service.VIPAuditEvent{{
		ID:            5,
		UserID:        userID,
		ActorType:     "admin",
		Action:        "manual_mode",
		Source:        "manual_on",
		NewIsVIP:      true,
		ActorSnapshot: "operator@example.com",
		CreatedAt:     time.Unix(100, 0).UTC(),
	}}, 1, nil
}

func (s *vipAdminServiceStub) GetUserGroupCatalog(
	_ context.Context,
	_ int64,
) ([]service.AdminGroupCatalogEntry, error) {
	return []service.AdminGroupCatalogEntry{{
		GroupCatalogEntry: service.GroupCatalogEntry{
			Group: service.Group{
				ID:          42,
				Name:        "VIP",
				Status:      service.StatusActive,
				IsExclusive: true,
				VIPOnly:     true,
			},
			CanBind: true,
		},
		WillGrantExclusive: true,
	}}, nil
}

func (s *vipAdminServiceStub) PreviewVIPReconcile(
	_ context.Context,
	cursor string,
	limit int,
) (*service.VIPReconcilePreview, error) {
	s.previewCursor = cursor
	s.previewLimit = limit
	return &service.VIPReconcilePreview{
		AsOf:  time.Unix(200, 0).UTC(),
		Total: 1,
		Stats: service.VIPReconcilePreviewStats{
			EligibilityRepair: 1,
		},
		Items: []service.VIPReconcilePreviewItem{{
			Category: "EFFECTIVE_CHANGE",
			OrderID:  10,
		}},
	}, nil
}

func (s *vipAdminServiceStub) GetVIPReconcileJob(
	_ context.Context,
	jobID int64,
) (*service.VIPReconcileJob, error) {
	return &service.VIPReconcileJob{ID: jobID, Status: "running"}, nil
}

func setupVIPAdminRouter(t *testing.T) (*gin.Engine, *vipAdminServiceStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	base := newStubAdminService()
	stub := &vipAdminServiceStub{AdminService: base}
	handler := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 91})
		c.Next()
	})
	router.PUT("/admin/users/:id/vip-mode", handler.UpdateVIPMode)
	router.GET("/admin/users/:id/vip-audit", handler.ListVIPAudit)
	router.GET("/admin/users/:id/group-catalog", handler.GetGroupCatalog)
	router.GET("/admin/users/vip-reconcile/preview", handler.PreviewVIPReconcile)
	router.POST("/admin/users/vip-reconcile/jobs", handler.CreateVIPReconcileJob)
	router.GET("/admin/users/vip-reconcile/jobs/:job_id", handler.GetVIPReconcileJob)
	return router, stub
}

func performVIPAdminJSON(
	t *testing.T,
	router http.Handler,
	method, path string,
	body map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestAdminUserVIPModeUpdateUsesExplicitModeReasonAndActor(t *testing.T) {
	router, stub := setupVIPAdminRouter(t)
	rec := performVIPAdminJSON(t, router, http.MethodPut, "/admin/users/7/vip-mode", map[string]any{
		"vip_mode":            "force_on",
		"vip_override_reason": "support exception",
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.VIPModeForceOn, stub.mode)
	require.Equal(t, int64(91), stub.actorID)
	require.Equal(t, "support exception", stub.reason)
	require.Contains(t, rec.Body.String(), `"vip_mode":"FORCE_ON"`)
}

func TestAdminUserVIPModeUpdateRejectsInvalidInput(t *testing.T) {
	router, stub := setupVIPAdminRouter(t)
	cases := []map[string]any{
		{"vip_mode": "UNKNOWN", "vip_override_reason": "reason"},
		{"vip_mode": "AUTO", "vip_override_reason": "   "},
	}
	for _, body := range cases {
		rec := performVIPAdminJSON(t, router, http.MethodPut, "/admin/users/7/vip-mode", body)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	require.Empty(t, stub.mode)
}

func TestAdminUserVIPAuditAndTargetCatalogContracts(t *testing.T) {
	router, stub := setupVIPAdminRouter(t)

	auditRec := httptest.NewRecorder()
	router.ServeHTTP(auditRec, httptest.NewRequest(http.MethodGet, "/admin/users/7/vip-audit?page=2&page_size=10", nil))
	require.Equal(t, http.StatusOK, auditRec.Code)
	require.Equal(t, 2, stub.auditPage)
	require.Equal(t, 10, stub.auditLimit)
	require.Contains(t, auditRec.Body.String(), `"actor_snapshot":"operator@example.com"`)
	require.Contains(t, auditRec.Body.String(), `"total":1`)

	catalogRec := httptest.NewRecorder()
	router.ServeHTTP(catalogRec, httptest.NewRequest(http.MethodGet, "/admin/users/7/group-catalog", nil))
	require.Equal(t, http.StatusOK, catalogRec.Code)
	require.Contains(t, catalogRec.Body.String(), `"vip_only":true`)
	require.Contains(t, catalogRec.Body.String(), `"can_bind":true`)
	require.Contains(t, catalogRec.Body.String(), `"will_grant_exclusive":true`)
}

func TestAdminVIPReconcilePreviewAndJobContracts(t *testing.T) {
	router, stub := setupVIPAdminRouter(t)

	previewRec := httptest.NewRecorder()
	router.ServeHTTP(previewRec, httptest.NewRequest(
		http.MethodGet,
		"/admin/users/vip-reconcile/preview?cursor=opaque&limit=25",
		nil,
	))
	require.Equal(t, http.StatusOK, previewRec.Code)
	require.Equal(t, "opaque", stub.previewCursor)
	require.Equal(t, 25, stub.previewLimit)
	require.Contains(t, previewRec.Body.String(), `"eligibility_repair":1`)
	require.Contains(t, previewRec.Body.String(), `"category":"EFFECTIVE_CHANGE"`)

	jobRec := httptest.NewRecorder()
	router.ServeHTTP(jobRec, httptest.NewRequest(
		http.MethodGet,
		"/admin/users/vip-reconcile/jobs/17",
		nil,
	))
	require.Equal(t, http.StatusOK, jobRec.Code)
	require.Contains(t, jobRec.Body.String(), `"id":17`)
	require.Contains(t, jobRec.Body.String(), `"status":"running"`)
}

func TestAdminVIPReconcileCreateRequiresStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUserHandler(newStubAdminService(), nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/admin/users/vip-reconcile/jobs", handler.CreateVIPReconcileJob)

	rec := performVIPAdminJSON(t, router, http.MethodPost, "/admin/users/vip-reconcile/jobs", map[string]any{
		"request_id": "request-1",
		"reason":     "repair history",
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminVIPReconcileCreateIgnoresDisabledGlobalStepUpSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingService := service.NewSettingService(
		&settingHandlerRepoStub{values: map[string]string{
			service.SettingKeyStepUpEnabled: "false",
		}},
		&config.Config{},
	)
	handler := NewUserHandler(
		newStubAdminService(),
		nil,
		nil,
		nil,
		nil,
		nil,
		settingService,
	)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 91})
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
		c.Next()
	})
	router.POST("/admin/users/vip-reconcile/jobs", handler.CreateVIPReconcileJob)

	rec := performVIPAdminJSON(
		t,
		router,
		http.MethodPost,
		"/admin/users/vip-reconcile/jobs",
		map[string]any{
			"request_id": "request-global-step-up-disabled",
			"reason":     "repair history",
		},
	)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
}
