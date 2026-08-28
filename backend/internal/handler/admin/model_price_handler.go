package admin

import (
	"context"
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type modelPriceCallableCatalog interface {
	ListAllPassive(ctx context.Context) (map[string][]string, error)
}

type modelPriceOfficialProvider interface {
	GetFallbackPricing(model string) *service.ModelPricing
}

type ModelPriceHandler struct {
	pricingService  *service.PricingService
	callableCatalog modelPriceCallableCatalog
	officialPrices  modelPriceOfficialProvider
}

func NewModelPriceHandler(
	pricingService *service.PricingService,
	callableCatalog modelPriceCallableCatalog,
	officialPrices modelPriceOfficialProvider,
) *ModelPriceHandler {
	return &ModelPriceHandler{
		pricingService:  pricingService,
		callableCatalog: callableCatalog,
		officialPrices:  officialPrices,
	}
}

// ProvideModelPriceHandler binds catalog and official-price ports for Wire.
func ProvideModelPriceHandler(
	pricingService *service.PricingService,
	callableCatalog *service.ModelCatalogService,
	officialPrices *service.BillingService,
) *ModelPriceHandler {
	return NewModelPriceHandler(pricingService, callableCatalog, officialPrices)
}

type upsertModelPriceRequest struct {
	Platform string          `json:"platform"`
	Model    string          `json:"model"`
	Currency string          `json:"currency"`
	Payload  json.RawMessage `json:"payload"`
	Enabled  *bool           `json:"enabled"`
	Note     *string         `json:"note"`
}

func (h *ModelPriceHandler) List(c *gin.Context) {
	if h == nil || h.pricingService == nil {
		response.Paginated(c, []service.ModelPriceListItem{}, 0, 1, 50)
		return
	}
	page, pageSize := response.ParsePagination(c)
	query := service.ModelPriceListQuery{
		Platform: c.Query("platform"),
		Query:    c.Query("q"),
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
	}
	if h.callableCatalog != nil {
		byPlatform, err := h.callableCatalog.ListAllPassive(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		query.RestrictTo = flattenCallableModels(byPlatform)
		query.OfficialLookup = h.officialLookup()
	}
	result := h.pricingService.ListCatalog(query)
	response.Paginated(c, result.Items, int64(result.Total), page, pageSize)
}

func (h *ModelPriceHandler) ListPlatforms(c *gin.Context) {
	response.Success(c, gin.H{"platforms": service.CatalogOverridePlatforms()})
}

func (h *ModelPriceHandler) Detail(c *gin.Context) {
	model := c.Query("model")
	detail, err := h.pricingService.GetPriceDetailWithOfficial(
		c.Query("platform"),
		model,
		h.officialEntry(model),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

func (h *ModelPriceHandler) Upsert(c *gin.Context) {
	var req upsertModelPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_PAYLOAD", err.Error()))
		return
	}
	payload, err := service.DecodeModelPriceOverridePayload(req.Payload)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var updatedBy *int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok && subject.UserID > 0 {
		id := subject.UserID
		updatedBy = &id
	}
	result, err := h.pricingService.UpsertOverride(c.Request.Context(), service.ModelPriceUpsertInput{
		Platform:  req.Platform,
		Model:     req.Model,
		Currency:  req.Currency,
		Payload:   payload,
		Enabled:   req.Enabled,
		Note:      req.Note,
		UpdatedBy: updatedBy,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ModelPriceHandler) Delete(c *gin.Context) {
	if err := h.pricingService.DeleteOverride(c.Request.Context(), c.Query("platform"), c.Query("model")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ModelPriceHandler) Sync(c *gin.Context) {
	status, err := h.pricingService.ForceUpdateWithOverrideCount()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	status["success"] = true
	response.Success(c, status)
}

func (h *ModelPriceHandler) SyncStatus(c *gin.Context) {
	status := h.pricingService.GetStatus()
	response.Success(c, status)
}

func (h *ModelPriceHandler) officialLookup() func(string) *service.ModelPriceEntry {
	return func(model string) *service.ModelPriceEntry {
		return h.officialEntry(model)
	}
}

func (h *ModelPriceHandler) officialEntry(model string) *service.ModelPriceEntry {
	if h == nil || h.officialPrices == nil {
		return nil
	}
	return service.ModelPriceEntryFromOfficial(model, h.officialPrices.GetFallbackPricing(model))
}

func flattenCallableModels(byPlatform map[string][]string) []service.CallableModelRef {
	out := make([]service.CallableModelRef, 0)
	for platform, models := range byPlatform {
		for _, model := range models {
			if service.IsPublicCatalogRoutingOnlyModelID(model) {
				continue
			}
			out = append(out, service.CallableModelRef{
				Platform: platform,
				Model:    model,
			})
		}
	}
	return out
}
