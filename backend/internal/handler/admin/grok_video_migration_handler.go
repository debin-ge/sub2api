package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/gin-gonic/gin"
)

type grokMigrationPreviewer interface {
	PreviewGrokMigration(context.Context, service.GrokVideoMigrationPreviewRequest) (*service.GrokVideoMigrationPreview, error)
}

func (h *VideoHandler) PreviewGrokMigration(c *gin.Context) {
	controller, ok := h.requireController().(grokMigrationPreviewer)
	if !ok {
		response.ErrorFrom(c, service.ErrBillingServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, videoAdminMaxBodyBytes+1))
	if err != nil || len(raw) > videoAdminMaxBodyBytes || jsonstrict.RejectDuplicateKeys(raw) != nil {
		response.BadRequest(c, "invalid migration preview request")
		return
	}
	var request service.GrokVideoMigrationPreviewRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		response.BadRequest(c, "invalid migration preview fields")
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		response.BadRequest(c, "trailing migration preview data")
		return
	}
	result, err := controller.PreviewGrokMigration(c.Request.Context(), request)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
