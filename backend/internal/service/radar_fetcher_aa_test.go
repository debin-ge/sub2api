package service

import (
	"context"
	"errors"
	"fmt"
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
	payloadJSON := `{"tier":"free","intelligence_index_version":4.1,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false},"data":[{"slug":"claude-sonnet-4","name":"Claude Sonnet 4","release_date":"2026-05-22","model_creator":{"name":"Anthropic"},"evaluations":{"artificial_analysis_intelligence_index":90,"artificial_analysis_coding_index":80,"artificial_analysis_agentic_index":70}}]}`
	body := newRadarTrackingBody(payloadJSON)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          body,
			ContentLength: int64(len(payloadJSON)),
		}, nil
	})
	cfg := validRadarFetcherTestConfig()
	fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
	require.NoError(t, err)
	require.Equal(t, RadarSourceAA, fetcher.Source())
	require.Equal(t, 360*time.Minute, fetcher.Interval())

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	models, decodeErr := DecodeArtificialAnalysisModels(payload)
	require.NoError(t, decodeErr)
	require.Len(t, models, 1)
	require.True(t, body.isClosed())
	require.NotNil(t, captured)
	require.Equal(t, http.MethodGet, captured.Method)
	require.Equal(t, "https", captured.URL.Scheme)
	require.Equal(t, "artificialanalysis.ai", captured.URL.Host)
	require.Equal(t, "/api/v2/language/models/free", captured.URL.EscapedPath())
	require.Equal(t, "page=1", captured.URL.RawQuery)
	require.Equal(t, cfg.Radar.ArtificialAnalysisAPIKey, captured.Header.Get("x-api-key"))
	require.Nil(t, meta.Error)
	require.NotNil(t, meta.LastSuccessAt)
	require.Equal(t, time.UTC, meta.LastAttemptAt.Location())
	require.Equal(t, meta.LastAttemptAt, *meta.LastSuccessAt)
	require.NotNil(t, meta.HTTPStatus)
	require.Equal(t, http.StatusOK, *meta.HTTPStatus)
}

func TestArtificialAnalysisModelsFetcherMergesAllPagesInOrder(t *testing.T) {
	requestedPages := make([]string, 0, 3)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		payloads := map[string]string{
			"1": aaModelsPagePayload(1, 3, true, 4.1, "model-a"),
			"2": aaModelsPagePayload(2, 3, true, 4.1, "model-b"),
			"3": aaModelsPagePayload(3, 3, false, 4.1, "model-c"),
		}
		payload := payloads[page]
		return &http.Response{
			StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
		}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)

	payload, _, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"1", "2", "3"}, requestedPages)
	models, version, err := DecodeArtificialAnalysisSnapshot(payload)
	require.NoError(t, err)
	require.Equal(t, 4.1, *version)
	require.Equal(t, []string{"model-a", "model-b", "model-c"}, []string{
		models[0].Slug, models[1].Slug, models[2].Slug,
	})
}

func TestArtificialAnalysisModelsFetcherRejectsEmptyPage(t *testing.T) {
	payloadJSON := `{"tier":"free","intelligence_index_version":4.1,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false},"data":[]}`
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: newRadarTrackingBody(payloadJSON), ContentLength: int64(len(payloadJSON)),
		}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	setRadarFetcherSleep(t, fetcher, func(context.Context, time.Duration) error { return nil })

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.NotNil(t, meta.Error)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
}

func TestArtificialAnalysisModelsFetcherRejectsPaginationInconsistencies(t *testing.T) {
	tests := []struct {
		name  string
		pages map[string]string
	}{
		{
			name: "version changes",
			pages: map[string]string{
				"1": aaModelsPagePayload(1, 2, true, 4.1, "model-a"),
				"2": aaModelsPagePayload(2, 2, false, 4.2, "model-b"),
			},
		},
		{
			name: "total pages changes",
			pages: map[string]string{
				"1": aaModelsPagePayload(1, 2, true, 4.1, "model-a"),
				"2": aaModelsPagePayload(2, 3, true, 4.1, "model-b"),
			},
		},
		{
			name: "response page is not requested page",
			pages: map[string]string{
				"1": aaModelsPagePayload(1, 2, true, 4.1, "model-a"),
				"2": aaModelsPagePayload(1, 2, true, 4.1, "model-b"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
				payload := tt.pages[req.URL.Query().Get("page")]
				return &http.Response{
					StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
				}, nil
			})
			fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
			require.NoError(t, err)

			payload, meta, err := fetcher.Fetch(context.Background())

			require.Error(t, err)
			require.Nil(t, payload)
			require.NotNil(t, meta.Error)
			require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
		})
	}
}

func TestArtificialAnalysisModelsFetcherDoesNotReturnPartialPayload(t *testing.T) {
	requestedPages := make([]string, 0, 2)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		payload := aaModelsPagePayload(1, 2, true, 4.1, "model-a")
		if page == "2" {
			payload = `{"tier":"free","pagination":`
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
		}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, []string{"1", "2"}, requestedPages)
	require.NotNil(t, meta.Error)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
}

func TestArtificialAnalysisModelsFetcherRejectsCrossPageDuplicateSlug(t *testing.T) {
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		payload := aaModelsPagePayload(1, 2, true, 4.1, "duplicate")
		if page == "2" {
			payload = aaModelsPagePayload(2, 2, false, 4.1, "duplicate")
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
		}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.NotNil(t, meta.Error)
	require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
}

func TestArtificialAnalysisModelsFetcherEnforcesPageAndAggregateLimits(t *testing.T) {
	t.Run("page limit", func(t *testing.T) {
		payload := aaModelsPagePayload(1, artificialAnalysisMaxPages+1, true, 4.1, "model-a")
		client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
			}, nil
		})
		fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
		require.NoError(t, err)

		result, meta, err := fetcher.Fetch(context.Background())

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
	})

	t.Run("aggregate response limit", func(t *testing.T) {
		first := aaModelsPagePayload(1, 2, true, 4.1, "model-a")
		second := aaModelsPagePayload(2, 2, false, 4.1, "model-b")
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ExternalResponseMaxBytes = int64(max(len(first), len(second)) + 32)
		client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
			payload := first
			if req.URL.Query().Get("page") == "2" {
				payload = second
			}
			return &http.Response{
				StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload), ContentLength: int64(len(payload)),
			}, nil
		})
		fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
		require.NoError(t, err)

		result, meta, err := fetcher.Fetch(context.Background())

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, DataSourceErrorCodeInvalidResponse, *meta.Error)
	})
}

func aaModelsPagePayload(page, totalPages int, hasMore bool, version float64, slug string) string {
	return fmt.Sprintf(
		`{"tier":"free","intelligence_index_version":%.1f,"pagination":{"page":%d,"page_size":1,"total_pages":%d,"has_more":%t},"data":[{"slug":%q,"name":%q,"release_date":"2026-07-01","model_creator":{"name":"Vendor"},"evaluations":{"artificial_analysis_intelligence_index":90,"artificial_analysis_coding_index":80,"artificial_analysis_agentic_index":70}}]}`,
		version, page, totalPages, hasMore, slug, "AA "+slug,
	)
}

func TestArtificialAnalysisModelsDecodePreservesMissingMetricsAndMapExcludesIncompleteModel(t *testing.T) {
	payload := []byte(`{"data":[{"slug":"claude-sonnet-4","name":"Claude Sonnet 4","creator":"","released_at":"2026-05-22T08:00:00+08:00","last_updated_at":"2026-07-10T14:00:00+08:00"}]}`)

	models, err := DecodeArtificialAnalysisModels(payload)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Nil(t, models[0].IntelligenceIndex)
	require.Nil(t, models[0].CodingIndex)
	require.Nil(t, models[0].AgenticIndex)
	require.Nil(t, models[0].PriceInputPer1M)
	require.Nil(t, models[0].PriceOutputPer1M)

	dtos, err := MapArtificialAnalysisModels(models)
	require.NoError(t, err)
	require.Empty(t, dtos)
}

func TestArtificialAnalysisModelsDecodeAndMapCurrentV2Response(t *testing.T) {
	payload := []byte(`{"status":200,"prompt_options":{"parallel_queries":1,"prompt_length":1000},"data":[{
		"id":"model-id","name":"QwQ 32B Preview","slug":"QwQ-32B-Preview","release_date":"2025-03-01",
		"model_creator":{"id":"creator-id","name":"Alibaba","slug":"alibaba"},
		"evaluations":{"artificial_analysis_intelligence_index":20.1,"artificial_analysis_coding_index":30.2,"artificial_analysis_math_index":40.3},
		"pricing":{"price_1m_blended_3_to_1":0.4,"price_1m_input_tokens":0.2,"price_1m_output_tokens":1.0},
		"median_output_tokens_per_second":100
	}]}`)

	models, err := DecodeArtificialAnalysisModels(payload)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "QwQ-32B-Preview", models[0].Slug)
	require.Equal(t, "Alibaba", models[0].Creator)
	require.Equal(t, "2025-03-01", models[0].ReleasedAt)
	require.Equal(t, 20.1, *models[0].IntelligenceIndex)
	require.Equal(t, 30.2, *models[0].CodingIndex)
	require.Nil(t, models[0].AgenticIndex, "AA v2 does not currently expose an agentic index")
	require.Equal(t, 0.2, *models[0].PriceInputPer1M)
	require.Equal(t, 1.0, *models[0].PriceOutputPer1M)

	dtos, err := MapArtificialAnalysisModels(models)
	require.NoError(t, err)
	require.Empty(t, dtos)
}

func TestMapArtificialAnalysisModelsSelectsIndexedModels(t *testing.T) {
	models, err := DecodeArtificialAnalysisModels([]byte(validAAModelsPayload))
	require.NoError(t, err)

	dtos, err := MapArtificialAnalysisModels(models)

	require.NoError(t, err)
	require.NotNil(t, dtos)
	require.Equal(t, []string{"claude-sonnet-4"}, []string{dtos[0].Slug})
}

func TestMapArtificialAnalysisModelsAutomaticSelectionIsBoundedAndStable(t *testing.T) {
	type candidate struct {
		slug     string
		score    float64
		released string
	}
	input := []candidate{
		{slug: "low", score: 70, released: "2026-01-08"},
		{slug: "top", score: 99, released: "2026-01-01"},
		{slug: "tie-old", score: 90, released: "2026-01-01"},
		{slug: "mid-low", score: 75, released: "2026-01-07"},
		{slug: "high", score: 95, released: "2026-01-02"},
		{slug: "mid", score: 85, released: "2026-01-05"},
		{slug: "tie-new", score: 90, released: "2026-02-01"},
		{slug: "sixth", score: 80, released: "2026-01-06"},
	}
	models := make([]ArtificialAnalysisModel, 0, len(input)+1)
	for _, item := range input {
		score := item.score
		models = append(models, ArtificialAnalysisModel{
			Slug: item.slug, Name: item.slug, ReleasedAt: item.released,
			IntelligenceIndex: &score, CodingIndex: &score, AgenticIndex: &score,
		})
	}
	models = append(models, ArtificialAnalysisModel{Slug: "no-index", Name: "No index"})
	original := append([]ArtificialAnalysisModel(nil), models...)

	dtos, err := MapArtificialAnalysisModels(models)

	require.NoError(t, err)
	require.Equal(t, []string{"top", "high", "tie-new", "tie-old", "mid", "sixth"}, []string{
		dtos[0].Slug, dtos[1].Slug, dtos[2].Slug, dtos[3].Slug, dtos[4].Slug, dtos[5].Slug,
	})
	require.Equal(t, original, models, "automatic selection must not reorder or mutate the decoded payload")
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

}
