package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubCallableCatalog struct {
	models map[string][]string
}

func (s stubCallableCatalog) ListAllPassive(context.Context) (map[string][]string, error) {
	return s.models, nil
}

type stubOfficialPrices struct {
	prices map[string]*service.ModelPricing
}

func (s stubOfficialPrices) GetFallbackPricing(model string) *service.ModelPricing {
	return s.prices[model]
}

func TestModelPriceHandlerEntryUsesQueryForSlashModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &service.PricingService{}
	svc.SeedCatalogForTest(map[string]*service.ModelPriceEntry{
		"openai/gpt-5.4": {
			InputCostPerToken:   1e-6,
			OutputCostPerToken:  2e-6,
			InputPriceExplicit:  true,
			OutputPriceExplicit: true,
			PricePresenceKnown:  true,
		},
	})
	handler := NewModelPriceHandler(svc, nil, nil)

	router := gin.New()
	router.GET("/entry", handler.Detail)
	router.GET("/sync/status", handler.SyncStatus)

	req := httptest.NewRequest(http.MethodGet, "/entry?platform="+url.QueryEscape("*")+"&model="+url.QueryEscape("openai/gpt-5.4"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data, _ := body["data"].(map[string]any)
	require.Equal(t, "openai/gpt-5.4", data["model"])

	statusReq := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)
	require.Equal(t, http.StatusOK, statusW.Code)
	require.Contains(t, statusW.Body.String(), "catalog_model_count")
}

func TestModelPriceHandlerListRestrictsToCallableAndFillsOfficial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &service.PricingService{}
	svc.SeedCatalogForTest(map[string]*service.ModelPriceEntry{
		"claude-sonnet-4": {
			InputCostPerToken:      1e-6,
			OutputCostPerToken:     2e-6,
			InputPriceExplicit:     true,
			OutputPriceExplicit:    true,
			PricePresenceKnown:     true,
			PricingCatalogProvider: "anthropic",
		},
		"gpt-unrelated": {
			InputCostPerToken:      3e-6,
			OutputCostPerToken:     4e-6,
			InputPriceExplicit:     true,
			OutputPriceExplicit:    true,
			PricePresenceKnown:     true,
			PricingCatalogProvider: "openai",
		},
	})
	handler := NewModelPriceHandler(svc, stubCallableCatalog{
		models: map[string][]string{
			service.PlatformAnthropic: {"claude-sonnet-4"},
			service.PlatformGLM:       {"glm-4.7"},
		},
	}, stubOfficialPrices{
		prices: map[string]*service.ModelPricing{
			"glm-4.7": {
				InputPricePerToken:  0.6e-6,
				OutputPricePerToken: 2.2e-6,
			},
		},
	})

	router := gin.New()
	router.GET("/", handler.List)
	req := httptest.NewRequest(http.MethodGet, "/?page=1&page_size=50", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data, _ := body["data"].(map[string]any)
	items, _ := data["items"].([]any)
	require.Len(t, items, 2)
	names := map[string]string{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		names[item["model"].(string)] = item["source"].(string)
	}
	require.Equal(t, "catalog", names["claude-sonnet-4"])
	require.Equal(t, "official", names["glm-4.7"])
	require.NotContains(t, names, "gpt-unrelated")
}
