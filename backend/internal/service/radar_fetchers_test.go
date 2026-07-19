package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRadarFetchersWithoutAAKeySkipsAllAARequests(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = " \t "
	cfg.Radar.ArtificialAnalysisModelSlugs = []string{"ignored-without-key"}

	fetchers, err := NewRadarFetchers(cfg, radarFetcherTestCatalog())

	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
	}, radarFetcherSources(fetchers))
	requireUniqueRadarFetcherSources(t, fetchers)
}

func TestNewRadarFetchersWithAAKeyUsesStableConfiguredOrder(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor must not access the network")
		return nil, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = " configured-key "
	cfg.Radar.ArtificialAnalysisModelSlugs = []string{"model-b", "model-a", "model-b"}

	fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

	require.NoError(t, err)
	modelBSource, err := RadarAAPerformanceSource("model-b")
	require.NoError(t, err)
	modelASource, err := RadarAAPerformanceSource("model-a")
	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceAA,
		modelBSource,
		modelASource,
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
	}, radarFetcherSources(fetchers))
	requireUniqueRadarFetcherSources(t, fetchers)
}

func TestNewRadarFetchersWithAAKeyAndNoSlugsStillIncludesModels(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor must not access the network")
		return nil, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisModelSlugs = nil

	fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceAA,
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
	}, radarFetcherSources(fetchers))
}

func TestNewRadarFetchersConstructionIsAtomicAndSanitized(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor must not access the network")
		return nil, nil
	})

	t.Run("nil config", func(t *testing.T) {
		fetchers, err := NewRadarFetchers(nil, radarFetcherTestCatalog())
		require.Error(t, err)
		require.Nil(t, fetchers)
	})

	t.Run("nil client", func(t *testing.T) {
		fetchers, err := newRadarFetchers(validRadarFetcherTestConfig(), nil, radarFetcherTestCatalog())
		require.Error(t, err)
		require.Nil(t, fetchers)
	})

	t.Run("nil public model catalog", func(t *testing.T) {
		fetchers, err := newRadarFetchers(validRadarFetcherTestConfig(), client, nil)
		require.Error(t, err)
		require.Nil(t, fetchers)
	})

	t.Run("invalid common config", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ExternalResponseMaxBytes = 0
		fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())
		require.Error(t, err)
		require.Nil(t, fetchers)
	})

	t.Run("invalid later AA slug returns no partial slice", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ArtificialAnalysisAPIKey = "assembly-secret-key"
		cfg.Radar.ArtificialAnalysisModelSlugs = []string{"valid-model", "../sensitive-model"}

		fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

		require.Error(t, err)
		require.Nil(t, fetchers)
		require.NotContains(t, err.Error(), cfg.Radar.ArtificialAnalysisAPIKey)
		require.NotContains(t, err.Error(), "sensitive-model")
		var configErr *RadarFetcherConfigError
		require.True(t, errors.As(err, &configErr))
	})

	t.Run("invalid configured URL returns no partial slice", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.LMArenaURL = "https://user:url-secret@example.test/#fragment"

		fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

		require.Error(t, err)
		require.Nil(t, fetchers)
		require.NotContains(t, err.Error(), "url-secret")
	})
}

func TestNewRadarFetchersEndpointsHeadersAndIntervalsWithoutLiveNetwork(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = "assembly-api-key"
	cfg.Radar.ArtificialAnalysisModelSlugs = []string{"model-a"}
	cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"

	requests := make(map[string]*http.Request)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		requests[req.URL.String()] = req.Clone(req.Context())
		var payload string
		switch {
		case req.URL.String() == artificialAnalysisModelsURL:
			payload = `{"data":[]}`
		case strings.Contains(req.URL.Path, "/model-a/performance"):
			payload = `{"model_slug":"model-a","window":"90d","interval":"daily","data_points":[]}`
		case req.URL.Host == "datasets-server.huggingface.co":
			offset, err := strconv.Atoi(req.URL.Query().Get("offset"))
			require.NoError(t, err)
			payload = hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{})
		case req.URL.Host == "status.claude.com", req.URL.Host == "status.openai.com":
			payload = `{"page":{"id":"p","name":"Status","url":"https://payload.example","updated_at":"2026-07-10T10:00:00Z"},"status":{"indicator":"none","description":"OK"},"components":[]}`
		default:
			t.Fatalf("unexpected endpoint %s", req.URL.Redacted())
		}
		response := &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(payload)),
			ContentLength: int64(len(payload)),
			Header:        make(http.Header),
		}
		if req.URL.Host == "datasets-server.huggingface.co" {
			response.Header.Set("Content-Type", "application/json")
			response.Header.Set("X-Revision", strings.Repeat("a", 40))
		}
		return response, nil
	})

	fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())
	require.NoError(t, err)
	for _, fetcher := range fetchers {
		_, _, err := fetcher.Fetch(context.Background())
		require.NoError(t, err, "source %s", fetcher.Source())
	}

	require.Len(t, requests, len(fetchers))
	for endpoint, req := range requests {
		require.Equal(t, http.MethodGet, req.Method)
		if strings.Contains(endpoint, "artificialanalysis.ai") {
			require.Equal(t, cfg.Radar.ArtificialAnalysisAPIKey, req.Header.Get("x-api-key"))
		} else {
			require.Empty(t, req.Header.Get("x-api-key"))
		}
	}
	require.Contains(t, requests, artificialAnalysisModelsURL)
	require.Contains(t, requests, artificialAnalysisPerformanceURL+"/model-a/performance?window=90d&interval=daily")
	require.True(t, hasRadarFetcherRequestHost(requests, "datasets-server.huggingface.co"))
	require.Contains(t, requests, claudeStatuspageAPIURL)
	require.Contains(t, requests, openAIStatuspageAPIURL)
}

func radarFetcherTestCatalog() *radarLMArenaCatalogStub {
	return &radarLMArenaCatalogStub{models: map[string][]string{
		PlatformOpenAI: {"model-1", "gpt-5.5"},
	}}
}

func hasRadarFetcherRequestHost(requests map[string]*http.Request, host string) bool {
	for _, request := range requests {
		if request.URL.Hostname() == host {
			return true
		}
	}
	return false
}

func radarFetcherSources(fetchers []RadarFetcher) []RadarSourceKey {
	sources := make([]RadarSourceKey, 0, len(fetchers))
	for _, fetcher := range fetchers {
		sources = append(sources, fetcher.Source())
	}
	return sources
}

func requireUniqueRadarFetcherSources(t *testing.T, fetchers []RadarFetcher) {
	t.Helper()
	seen := make(map[RadarSourceKey]struct{}, len(fetchers))
	for _, fetcher := range fetchers {
		_, duplicate := seen[fetcher.Source()]
		require.False(t, duplicate, "duplicate source %s", fetcher.Source())
		seen[fetcher.Source()] = struct{}{}
	}
}
