//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupAccessGraphScannerAdminServiceStub struct {
	service.AdminService
	report *service.GroupAccessGraphScanReport
	err    error
	calls  int
}

func (s *groupAccessGraphScannerAdminServiceStub) ScanGroupAccessGraph(context.Context) (*service.GroupAccessGraphScanReport, error) {
	s.calls++
	return s.report, s.err
}

func TestGroupHandlerScanAccessGraphReturnsMachineReadableReport(t *testing.T) {
	scannedAt := time.Date(2026, time.July, 29, 12, 30, 0, 0, time.UTC)
	svc := &groupAccessGraphScannerAdminServiceStub{
		report: &service.GroupAccessGraphScanReport{
			ScannedAt:  scannedAt,
			GroupCount: 2,
			Violations: []service.GroupAccessGraphViolation{{
				Reason:     service.GroupFallbackReasonVIPEscalation,
				SourceID:   1,
				SourceName: "public",
				TargetID:   2,
				TargetName: "vip",
				Kind:       service.GroupFallbackDefault,
				Path:       []int64{1, 2},
			}},
		},
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewGroupHandler(svc, nil, nil, nil)
	router.GET("/api/v1/admin/groups/access-graph/scan", handler.ScanAccessGraph)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/access-graph/scan", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, svc.calls)
	require.JSONEq(t, `{
		"code": 0,
		"message": "success",
		"data": {
			"scanned_at": "2026-07-29T12:30:00Z",
			"group_count": 2,
			"violations": [{
				"reason": "GROUP_FALLBACK_VIP_ESCALATION",
				"source_id": 1,
				"source_name": "public",
				"target_id": 2,
				"target_name": "vip",
				"fallback_kind": "default",
				"path": [1, 2]
			}]
		}
	}`, recorder.Body.String())
}
