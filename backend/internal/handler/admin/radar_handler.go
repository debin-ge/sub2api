package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const radarAdminMaxBodyBytes = 1024

type RadarHandler struct {
	controller radarAdminController
}

type radarAdminController interface {
	GetStatus(context.Context) (*service.RadarAdminStatus, error)
	SetEnabled(context.Context, bool, service.RadarAdminAuditContext) (*service.RadarAdminSettings, error)
	TriggerRefresh(service.RadarAdminAuditContext) (*service.RadarAdminRefreshResult, error)
}

func NewRadarHandler(controller *service.RadarAdminController) *RadarHandler {
	return &RadarHandler{controller: controller}
}

func newRadarHandler(controller radarAdminController) *RadarHandler {
	return &RadarHandler{controller: controller}
}

type radarSettingsRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h *RadarHandler) GetStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !radarAdminHasNoQuery(c) {
		response.BadRequest(c, "invalid radar request")
		return
	}
	status, err := h.controller.GetStatus(c.Request.Context())
	if err != nil {
		radarAdminUnavailable(c)
		return
	}
	response.Success(c, status)
}

func (h *RadarHandler) UpdateSettings(c *gin.Context) {
	if !radarAdminHasNoQuery(c) {
		response.BadRequest(c, "invalid radar settings")
		return
	}
	var input radarSettingsRequest
	if err := decodeStrictRadarAdminJSON(c, &input); err != nil || input.Enabled == nil {
		response.BadRequest(c, "invalid radar settings")
		return
	}
	audit, ok := radarAdminAuditContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	settings, err := h.controller.SetEnabled(c.Request.Context(), *input.Enabled, audit)
	if err != nil {
		radarAdminUnavailable(c)
		return
	}
	response.Success(c, settings)
}

func (h *RadarHandler) Refresh(c *gin.Context) {
	if !radarAdminHasNoQuery(c) || !radarAdminBodyIsEmpty(c) {
		response.BadRequest(c, "invalid radar refresh request")
		return
	}
	audit, ok := radarAdminAuditContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, err := h.controller.TriggerRefresh(audit)
	if err != nil {
		radarAdminUnavailable(c)
		return
	}
	response.Accepted(c, result)
}

func radarAdminHasNoQuery(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.URL != nil && c.Request.URL.RawQuery == ""
}

func decodeStrictRadarAdminJSON(c *gin.Context, target any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return errors.New("missing body")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, radarAdminMaxBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing json")
	}
	return nil
}

func radarAdminBodyIsEmpty(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.ContentLength > 0 {
		return false
	}
	if c.Request.Body == nil {
		return true
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1)
	var one [1]byte
	n, err := c.Request.Body.Read(one[:])
	return n == 0 && errors.Is(err, io.EOF)
}

func radarAdminAuditContext(c *gin.Context) (service.RadarAdminAuditContext, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return service.RadarAdminAuditContext{}, false
	}
	requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
	return service.RadarAdminAuditContext{AdminUserID: subject.UserID, RequestID: strings.TrimSpace(requestID)}, true
}

func radarAdminUnavailable(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, "radar control unavailable")
}
