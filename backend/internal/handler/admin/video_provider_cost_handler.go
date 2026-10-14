package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type videoProviderCostController interface {
	ListProviderCosts(context.Context, string, int64, int) (*service.VideoProviderCostPage, error)
	ResolveProviderCost(context.Context, int64, service.VideoProviderCostResolution) (*service.VideoProviderCost, error)
}

func (h *VideoHandler) ListProviderCosts(c *gin.Context) {
	controller, ok := h.requireController().(videoProviderCostController)
	if !ok {
		response.ErrorFrom(c, service.ErrVideoInvalidRequest)
		return
	}
	after, err := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	if err != nil || after < 0 {
		response.BadRequest(c, "invalid provider cost cursor")
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		response.BadRequest(c, "provider cost limit must be between 1 and 100")
		return
	}
	state := c.DefaultQuery("state", "unresolved")
	if state != "" && state != "unresolved" && state != "resolved_no_charge" && state != "resolved_cost_recorded" {
		response.BadRequest(c, "invalid provider cost state")
		return
	}
	result, err := controller.ListProviderCosts(c.Request.Context(), state, after, limit)
	videoAdminRespond(c, result, err)
}

// This records supplier accounting evidence only. It never authorizes customer
// capture/release, restarts generation, or restores access to refunded content.
func (h *VideoHandler) ResolveProviderCost(c *gin.Context) {
	controller, ok := h.requireController().(videoProviderCostController)
	if !ok {
		response.ErrorFrom(c, service.ErrVideoInvalidRequest)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid provider cost id")
		return
	}
	var request service.VideoProviderCostResolution
	if err := decodeStrictVideoAdminJSON(c, &request); err != nil {
		response.BadRequest(c, "invalid provider cost resolution")
		return
	}
	request.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	request.ActorID = getAdminIDFromContext(c)
	if err := service.ValidateVideoProviderCostResolution(request); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := controller.ResolveProviderCost(c.Request.Context(), id, request)
	videoAdminRespond(c, result, err)
}
