package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRadarModelSlugValidationMatchesConfigContract(t *testing.T) {
	values := []string{
		"model.v1_alpha-beta",
		"../secret",
		"Uppercase",
		".leading",
		"",
		strings.Repeat("a", 128),
		strings.Repeat("a", 129),
	}
	for _, value := range values {
		require.Equal(t, config.IsValidRadarModelSlug(value), isSafeRadarModelSlug(value))
	}
}

func TestArtificialAnalysisModelsFetcherSuccessAndExactRequest(t *testing.T) {
	var captured *http.Request
	body := newRadarTrackingBody(validAAModelsPayload)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          body,
			ContentLength: int64(len(validAAModelsPayload)),
		}, nil
	})
	cfg := validRadarFetcherTestConfig()
	fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
	require.NoError(t, err)
	require.Equal(t, RadarSourceAA, fetcher.Source())
	require.Equal(t, 360*time.Minute, fetcher.Interval())

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.JSONEq(t, validAAModelsPayload, string(payload))
	require.True(t, body.isClosed())
	require.NotNil(t, captured)
	require.Equal(t, http.MethodGet, captured.Method)
	require.Equal(t, "https", captured.URL.Scheme)
	require.Equal(t, "artificialanalysis.ai", captured.URL.Host)
	require.Equal(t, "/api/v2/data/llms/models", captured.URL.EscapedPath())
	require.Empty(t, captured.URL.RawQuery)
	require.Equal(t, cfg.Radar.ArtificialAnalysisAPIKey, captured.Header.Get("x-api-key"))
	require.Nil(t, meta.Error)
	require.NotNil(t, meta.LastSuccessAt)
	require.Equal(t, time.UTC, meta.LastAttemptAt.Location())
	require.Equal(t, meta.LastAttemptAt, *meta.LastSuccessAt)
	require.NotNil(t, meta.HTTPStatus)
	require.Equal(t, http.StatusOK, *meta.HTTPStatus)
}

func TestArtificialAnalysisPerformanceFetcherSuccessAndExactRequest(t *testing.T) {
	const payloadJSON = `{"model_slug":"claude-4.1_opus","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12","intelligence_index":92.1,"coding_index":88.5,"agentic_index":90.2}]}`
	var captured *http.Request
	body := newRadarTrackingBody(payloadJSON)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisModelSlugs = []string{"another-model", " claude-4.1_opus "}
	fetcher, err := NewArtificialAnalysisPerformanceFetcher(cfg, " claude-4.1_opus ", client)
	require.NoError(t, err)
	wantSource, err := RadarAAPerformanceSource("claude-4.1_opus")
	require.NoError(t, err)
	require.Equal(t, wantSource, fetcher.Source())
	require.Equal(t, 1440*time.Minute, fetcher.Interval())

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.JSONEq(t, payloadJSON, string(payload))
	require.True(t, body.isClosed())
	require.Equal(t, "https", captured.URL.Scheme)
	require.Equal(t, "artificialanalysis.ai", captured.URL.Host)
	require.Equal(t, "/api/v2/language/models/claude-4.1_opus/performance", captured.URL.EscapedPath())
	require.Equal(t, "window=90d&interval=daily", captured.URL.RawQuery)
	require.Equal(t, cfg.Radar.ArtificialAnalysisAPIKey, captured.Header.Get("x-api-key"))
	require.Nil(t, meta.Error)
}

func TestArtificialAnalysisModelsDecodeAndMapPreservesMissingMetricsAndNormalizesUTC(t *testing.T) {
	payload := []byte(`{"data":[{"slug":"claude-sonnet-4","name":"Claude Sonnet 4","creator":"","released_at":"2026-05-22T08:00:00+08:00","last_updated_at":"2026-07-10T14:00:00+08:00"}]}`)

	models, err := DecodeArtificialAnalysisModels(payload)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Nil(t, models[0].IntelligenceIndex)
	require.Nil(t, models[0].CodingIndex)
	require.Nil(t, models[0].AgenticIndex)
	require.Nil(t, models[0].PriceInputPer1M)
	require.Nil(t, models[0].PriceOutputPer1M)

	dtos, err := MapArtificialAnalysisModels(models, []string{"claude-sonnet-4"})
	require.NoError(t, err)
	require.Len(t, dtos, 1)
	require.Empty(t, dtos[0].Vendor)
	require.Nil(t, dtos[0].IntelligenceIndex)
	require.Nil(t, dtos[0].CodingIndex)
	require.Nil(t, dtos[0].AgenticIndex)
	require.Nil(t, dtos[0].PriceInputPer1M)
	require.Nil(t, dtos[0].PriceOutputPer1M)
	require.NotNil(t, dtos[0].LastUpdatedAt)
	require.Equal(t, time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC), *dtos[0].LastUpdatedAt)
	require.Equal(t, time.UTC, dtos[0].LastUpdatedAt.Location())
}

func TestMapArtificialAnalysisModelsFiltersInConfiguredOrderWithoutMutatingInputs(t *testing.T) {
	models, err := DecodeArtificialAnalysisModels([]byte(`{"data":[
		{"slug":"unconfigured-model","name":"Unconfigured","intelligence_index":11},
		{"slug":"model-a","name":"Model A","intelligence_index":22},
		{"slug":"model-b","name":"Model B","intelligence_index":33}
	]}`))
	require.NoError(t, err)
	originalModels := make([]ArtificialAnalysisModel, len(models))
	copy(originalModels, models)
	allowedSlugs := []string{" model-b ", "model-a", "model-b", "missing-model"}
	originalAllowedSlugs := append([]string(nil), allowedSlugs...)

	dtos, err := MapArtificialAnalysisModels(models, allowedSlugs)

	require.NoError(t, err)
	require.Equal(t, []string{"model-b", "model-a"}, []string{dtos[0].Slug, dtos[1].Slug}, "configured order must win over upstream order")
	require.Equal(t, float64(33), *dtos[0].IntelligenceIndex)
	require.Equal(t, float64(22), *dtos[1].IntelligenceIndex)
	require.Equal(t, originalModels, models)
	require.Equal(t, originalAllowedSlugs, allowedSlugs)

	*dtos[0].IntelligenceIndex = 99
	require.Equal(t, float64(33), *models[2].IntelligenceIndex, "mapped metric pointers must not alias decoded input")
}

func TestMapArtificialAnalysisModelsEmptyAllowlistReturnsEmptyNonNilSlice(t *testing.T) {
	models, err := DecodeArtificialAnalysisModels([]byte(validAAModelsPayload))
	require.NoError(t, err)

	dtos, err := MapArtificialAnalysisModels(models, nil)

	require.NoError(t, err)
	require.NotNil(t, dtos)
	require.Empty(t, dtos)
}

func TestMapArtificialAnalysisModelsRejectsUnsafeAllowlistWithoutLeakingValue(t *testing.T) {
	models, err := DecodeArtificialAnalysisModels([]byte(validAAModelsPayload))
	require.NoError(t, err)
	const unsafeSlug = "../sensitive-configured-model"

	dtos, err := MapArtificialAnalysisModels(models, []string{unsafeSlug})

	require.Error(t, err)
	require.Nil(t, dtos)
	var configErr *RadarFetcherConfigError
	require.True(t, errors.As(err, &configErr))
	require.Equal(t, "radar.artificial_analysis_model_slugs", configErr.Field)
	require.NotContains(t, err.Error(), unsafeSlug)
}

func TestDecodeArtificialAnalysisModelsRejectsMalformedOrAmbiguousData(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "malformed JSON", payload: `{"data":`},
		{name: "trailing JSON", payload: `{"data":[]} {}`},
		{name: "missing data", payload: `{}`},
		{name: "null data", payload: `{"data":null}`},
		{name: "blank slug", payload: `{"data":[{"slug":" ","name":"Model"}]}`},
		{name: "unsafe slug", payload: `{"data":[{"slug":"../model","name":"Model"}]}`},
		{name: "blank name", payload: `{"data":[{"slug":"model","name":"  "}]}`},
		{name: "duplicate slug", payload: `{"data":[{"slug":"model","name":"One"},{"slug":"model","name":"Two"}]}`},
		{name: "bad released timestamp", payload: `{"data":[{"slug":"model","name":"Model","released_at":"yesterday"}]}`},
		{name: "impossible released date", payload: `{"data":[{"slug":"model","name":"Model","released_at":"2026-02-30"}]}`},
		{name: "non canonical released date", payload: `{"data":[{"slug":"model","name":"Model","released_at":"2026-5-2"}]}`},
		{name: "bad updated timestamp", payload: `{"data":[{"slug":"model","name":"Model","last_updated_at":"2026-99-99"}]}`},
		{name: "updated timestamp must not be date only", payload: `{"data":[{"slug":"model","name":"Model","last_updated_at":"2026-05-22"}]}`},
		{name: "negative index", payload: `{"data":[{"slug":"model","name":"Model","coding_index":-0.1}]}`},
		{name: "index above one hundred", payload: `{"data":[{"slug":"model","name":"Model","agentic_index":100.1}]}`},
		{name: "negative input price", payload: `{"data":[{"slug":"model","name":"Model","price_input_per_1m":-1}]}`},
		{name: "negative output price", payload: `{"data":[{"slug":"model","name":"Model","price_output_per_1m":-1}]}`},
		{name: "non finite numeric overflow", payload: `{"data":[{"slug":"model","name":"Model","intelligence_index":1e309}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, err := DecodeArtificialAnalysisModels([]byte(tt.payload))
			require.Error(t, err)
			require.Nil(t, models)
			require.NotContains(t, err.Error(), tt.payload)
		})
	}
}

func TestDecodeArtificialAnalysisModelsAcceptsReleasedAtDateAndRFC3339(t *testing.T) {
	for _, releasedAt := range []string{"2026-05-22", "2026-05-22T08:00:00+08:00"} {
		t.Run(releasedAt, func(t *testing.T) {
			models, err := DecodeArtificialAnalysisModels([]byte(`{"data":[{"slug":"model","name":"Model","released_at":"` + releasedAt + `"}]}`))
			require.NoError(t, err)
			require.Len(t, models, 1)
			require.Equal(t, releasedAt, models[0].ReleasedAt)
		})
	}
}

func TestDecodeArtificialAnalysisModelsAcceptsBoundaryValues(t *testing.T) {
	models, err := DecodeArtificialAnalysisModels([]byte(`{"data":[{"slug":"model","name":"Model","intelligence_index":0,"coding_index":100,"price_input_per_1m":0}]}`))
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, float64(0), *models[0].IntelligenceIndex)
	require.Equal(t, float64(100), *models[0].CodingIndex)
	require.Equal(t, float64(0), *models[0].PriceInputPer1M)
}

func TestDecodeArtificialAnalysisPerformancePreservesOptionalMetrics(t *testing.T) {
	payload, err := DecodeArtificialAnalysisPerformance([]byte(`{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12"}]}`), "model")
	require.NoError(t, err)
	require.Equal(t, "model", payload.ModelSlug)
	require.Len(t, payload.DataPoints, 1)
	require.Nil(t, payload.DataPoints[0].IntelligenceIndex)
	require.Nil(t, payload.DataPoints[0].CodingIndex)
	require.Nil(t, payload.DataPoints[0].AgenticIndex)
}

func TestDecodeArtificialAnalysisPerformanceRejectsMalformedOrAmbiguousData(t *testing.T) {
	tests := []struct {
		name         string
		expectedSlug string
		payload      string
	}{
		{name: "malformed JSON", expectedSlug: "model", payload: `{"model_slug":`},
		{name: "trailing JSON", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[]} {}`},
		{name: "unsafe expected slug", expectedSlug: "../model", payload: `{"model_slug":"../model","window":"90d","interval":"daily","data_points":[]}`},
		{name: "slug mismatch", expectedSlug: "model", payload: `{"model_slug":"other","window":"90d","interval":"daily","data_points":[]}`},
		{name: "wrong window", expectedSlug: "model", payload: `{"model_slug":"model","window":"30d","interval":"daily","data_points":[]}`},
		{name: "wrong interval", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"weekly","data_points":[]}`},
		{name: "missing data points", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily"}`},
		{name: "null data points", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":null}`},
		{name: "bad date format", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-4-2"}]}`},
		{name: "impossible date", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-02-30"}]}`},
		{name: "duplicate date", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12"},{"date":"2026-04-12"}]}`},
		{name: "negative index", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12","intelligence_index":-1}]}`},
		{name: "index above one hundred", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12","coding_index":101}]}`},
		{name: "non finite numeric overflow", expectedSlug: "model", payload: `{"model_slug":"model","window":"90d","interval":"daily","data_points":[{"date":"2026-04-12","agentic_index":1e309}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := DecodeArtificialAnalysisPerformance([]byte(tt.payload), tt.expectedSlug)
			require.Error(t, err)
			require.Zero(t, payload)
			require.NotContains(t, err.Error(), tt.payload)
		})
	}
}

func TestArtificialAnalysisPerformanceFetcherRejectsMismatchedResponseWithoutRetry(t *testing.T) {
	attempts := 0
	body := newRadarTrackingBody(`{"model_slug":"different","window":"90d","interval":"daily","data_points":[]}`)
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisModelSlugs = []string{"model"}
	fetcher, err := NewArtificialAnalysisPerformanceFetcher(cfg, "model", client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.True(t, body.isClosed())
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
}

func TestArtificialAnalysisFetcherConstructorsRejectInvalidConfiguration(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP client must not be called")
		return nil, nil
	})

	t.Run("nil config", func(t *testing.T) {
		fetcher, err := NewArtificialAnalysisModelsFetcher(nil, client)
		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
	})

	t.Run("missing API key", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ArtificialAnalysisAPIKey = " \t "
		fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
		require.Equal(t, "radar.artificial_analysis_api_key", configErr.Field)
		require.NotContains(t, err.Error(), cfg.Radar.ArtificialAnalysisAPIKey)
	})

	t.Run("nil HTTP client", func(t *testing.T) {
		fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), nil)
		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
	})

	t.Run("invalid validated interval", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ArtificialAnalysisModelsIntervalMinutes = 0
		fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
	})

	for _, slug := range []string{"", "../escape", "model/escape", "UPPER", " model/escape ", "model%2fescape"} {
		t.Run("invalid performance slug "+slug, func(t *testing.T) {
			fetcher, err := NewArtificialAnalysisPerformanceFetcher(validRadarFetcherTestConfig(), slug, client)
			require.Error(t, err)
			require.Nil(t, fetcher)
			require.ErrorIs(t, err, ErrInvalidRadarCacheKey)
		})
	}

	t.Run("safe but unconfigured performance slug", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ArtificialAnalysisAPIKey = "secret-key-must-not-leak"
		cfg.Radar.ArtificialAnalysisModelSlugs = []string{"configured-model"}
		const unconfiguredSlug = "lexically-safe-but-unconfigured"

		fetcher, err := NewArtificialAnalysisPerformanceFetcher(cfg, unconfiguredSlug, client)

		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
		require.Equal(t, "radar.artificial_analysis_model_slugs", configErr.Field)
		require.NotContains(t, err.Error(), unconfiguredSlug)
		require.NotContains(t, err.Error(), cfg.Radar.ArtificialAnalysisAPIKey)
	})

	t.Run("unsafe configured performance slug", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		const unsafeConfiguredSlug = "../sensitive-config-value"
		cfg.Radar.ArtificialAnalysisModelSlugs = []string{"model", unsafeConfiguredSlug}

		fetcher, err := NewArtificialAnalysisPerformanceFetcher(cfg, "model", client)

		require.Error(t, err)
		require.Nil(t, fetcher)
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
		require.Equal(t, "radar.artificial_analysis_model_slugs", configErr.Field)
		require.NotContains(t, err.Error(), unsafeConfiguredSlug)
	})
}
