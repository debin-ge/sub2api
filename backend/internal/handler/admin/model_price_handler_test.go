package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

type handlerModelPriceOverrideStore struct {
	row *service.ModelPriceOverride
}

func (s *handlerModelPriceOverrideStore) List(context.Context) ([]service.ModelPriceOverride, error) {
	if s.row == nil {
		return nil, nil
	}
	return []service.ModelPriceOverride{*s.row}, nil
}

func (s *handlerModelPriceOverrideStore) Upsert(_ context.Context, row *service.ModelPriceOverride) (*service.ModelPriceOverride, error) {
	saved := *row
	saved.ID = 1
	s.row = &saved
	return &saved, nil
}

func (s *handlerModelPriceOverrideStore) Delete(context.Context, string, string) error {
	s.row = nil
	return nil
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

func TestModelPriceHandlerUpsertPersistsAndReturnsCNY(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &handlerModelPriceOverrideStore{}
	svc := service.NewPricingService(nil, nil)
	svc.SetOverrideDependencies(store, nil)
	handler := NewModelPriceHandler(svc, nil, nil)

	router := gin.New()
	router.PUT("/entry", handler.Upsert)
	router.GET("/entry", handler.Detail)

	requestBody := `{
		"platform":"zhipu",
		"model":"glm-5.1",
		"currency":"cny",
		"payload":{"input_cost_per_token":0.0000014,"output_cost_per_token":0.0000044}
	}`
	request := httptest.NewRequest(http.MethodPut, "/entry", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotNil(t, store.row)
	require.Equal(t, service.ModelPriceCurrencyCNY, store.row.Currency)
	require.Contains(t, response.Body.String(), `"currency":"CNY"`)

	detailRequest := httptest.NewRequest(http.MethodGet, "/entry?platform=zhipu&model=glm-5.1", nil)
	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, detailRequest)
	require.Equal(t, http.StatusOK, detailResponse.Code)
	require.Contains(t, detailResponse.Body.String(), `"currency":"CNY"`)
	require.Contains(t, detailResponse.Body.String(), `"override_currency":"CNY"`)
}

func TestModelPriceHandlerUpsertAllowsEmptyPayloadForInheritance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &handlerModelPriceOverrideStore{}
	svc := service.NewPricingService(nil, nil)
	svc.SetOverrideDependencies(store, nil)
	handler := NewModelPriceHandler(svc, nil, nil)

	router := gin.New()
	router.PUT("/entry", handler.Upsert)
	request := httptest.NewRequest(http.MethodPut, "/entry", strings.NewReader(`{
		"platform":"anthropic",
		"model":"claude-sonnet-4",
		"currency":"USD",
		"payload":{}
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.NotNil(t, store.row)
	require.Empty(t, store.row.Payload)
	require.Contains(t, response.Body.String(), `"override"`)
}

func TestModelPriceHandlerUpsertBillingMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &handlerModelPriceOverrideStore{}
	svc := service.NewPricingService(nil, nil)
	svc.SetOverrideDependencies(store, nil)
	handler := NewModelPriceHandler(svc, nil, nil)

	router := gin.New()
	router.PUT("/entry", handler.Upsert)
	put := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/entry", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	response := put(`{"platform":"openai","model":"my-image","currency":"USD","billing_mode":"image",
		"payload":{"output_cost_per_image":0.04,"intervals":[{"tier_label":"1K","per_request_price":0.03},{"tier_label":"2K","per_request_price":0.05}]}}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, service.BillingModeImage, store.row.BillingMode)
	require.Len(t, store.row.Payload.Intervals, 2)
	require.Equal(t, service.ImageBillingSize1K, store.row.Payload.Intervals[0].TierLabel)

	// 图片按张与按 token 二选一：image 档不接受 token 字段，token 档不接受按张价
	response = put(`{"platform":"openai","model":"my-image","currency":"USD","billing_mode":"image",
		"payload":{"output_cost_per_image":0.04,"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "FIELD_NOT_IN_BILLING_MODE")
	response = put(`{"platform":"openai","model":"my-image","currency":"USD","billing_mode":"token",
		"payload":{"output_cost_per_image":0.04}}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "FIELD_NOT_IN_BILLING_MODE")
	response = put(`{"platform":"openai","model":"my-image","currency":"USD","billing_mode":"token",
		"payload":{"output_cost_per_image_token":0.00003}}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "INCOMPLETE_IMAGE_TOKEN_PRICING")

	// video 档的价格只在 video_pricing 里
	response = put(`{"platform":"openai","model":"my-video","currency":"USD","billing_mode":"video",
		"payload":{"input_cost_per_token":0.000001}}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "FIELD_NOT_IN_BILLING_MODE")

	response = put(`{"platform":"openai","model":"my-image","currency":"USD","billing_mode":"per_request","payload":{}}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "INVALID_BILLING_MODE")
}

// 列表与详情返回出图方式和被忽略字段：289 迁移规则外的存量行（token 档带按张价）
// 在运行时忽略按张价，后台据此提示运营者。
func TestModelPriceHandlerReturnsImageBillingModeAndIgnoredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	perImage := 0.1
	store := &handlerModelPriceOverrideStore{row: &service.ModelPriceOverride{
		ID: 1, Platform: service.PlatformOpenAI, ModelName: "my-image", Currency: service.ModelPriceCurrencyUSD,
		BillingMode: service.BillingModeToken, Enabled: true,
		Payload: service.ModelPriceOverridePayload{OutputCostPerImage: &perImage},
	}}
	svc := service.NewPricingService(nil, nil)
	svc.SetOverrideDependencies(store, nil)
	require.NoError(t, svc.ReloadOverrides(context.Background()))
	handler := NewModelPriceHandler(svc, stubCallableCatalog{
		models: map[string][]string{service.PlatformOpenAI: {"my-image"}},
	}, nil)

	router := gin.New()
	router.GET("/", handler.List)
	router.GET("/entry", handler.Detail)
	router.PUT("/entry", handler.Upsert)
	get := func(target string) map[string]any {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		data, _ := body["data"].(map[string]any)
		return data
	}

	detail := get("/entry?platform=openai&model=my-image")
	require.Equal(t, []any{"output_cost_per_image"}, detail["ignored_fields"])
	require.Equal(t, "", detail["image_billing_mode"])

	list := get("/?page=1&page_size=50")
	items, _ := list["items"].([]any)
	require.Len(t, items, 1)
	item, _ := items[0].(map[string]any)
	require.Equal(t, []any{"output_cost_per_image"}, item["ignored_fields"])

	request := httptest.NewRequest(http.MethodPut, "/entry", strings.NewReader(`{"platform":"openai","model":"my-image",
		"currency":"USD","billing_mode":"image","payload":{"output_cost_per_image":0.1}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	detail = get("/entry?platform=openai&model=my-image")
	require.Equal(t, []any{}, detail["ignored_fields"])
	require.Equal(t, "image", detail["image_billing_mode"])
}

func TestModelPriceHandlerPreviewVideoReturnsRuleMismatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewModelPriceHandler(nil, nil, nil)
	router := gin.New()
	router.POST("/video-preview", handler.PreviewVideo)

	request := httptest.NewRequest(http.MethodPost, "/video-preview", strings.NewReader(`{
		"video_pricing":{
			"version":1,"enabled":true,"currency":"USD",
			"estimators":{"default":{"type":"fixed_tokens_per_second","tokens_per_second":48600}},
			"rules":[{
				"key":"reference-video","billing_unit":"video_token","unit_price_usd":0.00002325,"estimator":"default",
				"conditions":{"operations":["generate"],"input_has_video":true,"qualities":["standard"]}
			}]
		},
		"attributes":{"operation":"generate","seconds":10,"input_type":"reference_video","input_has_video":true}
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"error_code":"video_pricing_rule_missing"`)
	require.Contains(t, response.Body.String(), `"field":"quality"`)
}
