package service

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRadarFetchersWithoutAAKeySkipsAllAARequests(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = " \t "

	fetchers, err := NewRadarFetchers(cfg, radarFetcherTestCatalog())

	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf,
		RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxChina,
		RadarSourceStatusDeepSeek,
	}, radarFetcherSources(fetchers))
	requireUniqueRadarFetcherSources(t, fetchers)
}

func TestNewRadarFetchersWithAAKeyUsesStableSourceOrder(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor must not access the network")
		return nil, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = " configured-key "

	fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceAA,
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf,
		RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxChina,
		RadarSourceStatusDeepSeek,
	}, radarFetcherSources(fetchers))
	requireUniqueRadarFetcherSources(t, fetchers)
}

func TestNewRadarFetchersWithAAKeyIncludesModels(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor must not access the network")
		return nil, nil
	})
	cfg := validRadarFetcherTestConfig()

	fetchers, err := newRadarFetchers(cfg, client, radarFetcherTestCatalog())

	require.NoError(t, err)
	require.Equal(t, []RadarSourceKey{
		RadarSourceAA,
		RadarSourceLMArena,
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf,
		RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxChina,
		RadarSourceStatusDeepSeek,
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
	cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"

	requests := make(map[string]*http.Request)
	var requestsMu sync.Mutex
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[req.URL.String()] = req.Clone(req.Context())
		requestsMu.Unlock()
		var payload string
		switch {
		case req.URL.String() == artificialAnalysisModelsURL+"?page=1":
			payload = validAAModelsPayload
		case req.URL.Host == "datasets-server.huggingface.co":
			offset, err := strconv.Atoi(req.URL.Query().Get("offset"))
			require.NoError(t, err)
			payload = hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{})
		case req.URL.String() == openAIStatuspageFeedURL:
			payload = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><id>https://status.openai.com/</id><title>OpenAI status</title><updated>2026-07-10T10:00:00Z</updated><generator>incident.io</generator></feed>`
		case strings.HasPrefix(req.URL.String(), openAIComponentImpactsURL+"?"):
			payload = `{"component_impacts":[],"incident_links":[],"component_uptimes":[]}`
		case req.URL.String() == openAIStatusSummaryURL:
			payload = `{"summary":{"components":[{"id":"responses","name":"Responses"}],"structure":{"items":[{"group":{"id":"apis","name":"APIs","components":[{"component_id":"responses","name":"Responses"}]}}]}}}`
		case req.URL.Host == "status.claude.com", req.URL.Host == "status.openai.com",
			req.URL.Host == "status.windsurf.com", req.URL.Host == "status.moonshot.cn",
			req.URL.Host == "status.minimaxi.com":
			source := radarStatuspageSourceForHost(t, req.URL.Host)
			if req.URL.EscapedPath() == "/history" {
				payload = statuspageCalendarFixture(t, source, req.URL.Query().Get("filter"))
			} else if req.URL.EscapedPath() == "/api/v2/incidents.json" {
				payload = `{"page":{"id":"p","name":"Status","url":"https://payload.example","updated_at":"2026-07-10T10:00:00Z"},"incidents":[]}`
			} else {
				payload = strings.Replace(statuspageSummaryFixture(t, source), `"id":"page"`, `"id":"p"`, 1)
			}
		case req.URL.Host == "statuspage.flashcat.cloud":
			payload = string(deepSeekStatusHTML(t, `{"initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[{"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600}]},"active_changes":[]},"initialDataUpdatedAt":1784165568367}`))
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

	require.Len(t, requests, len(fetchers)+13)
	for endpoint, req := range requests {
		require.Equal(t, http.MethodGet, req.Method)
		if strings.Contains(endpoint, "artificialanalysis.ai") {
			require.Equal(t, cfg.Radar.ArtificialAnalysisAPIKey, req.Header.Get("x-api-key"))
		} else {
			require.Empty(t, req.Header.Get("x-api-key"))
		}
	}
	require.Contains(t, requests, artificialAnalysisModelsURL+"?page=1")
	require.True(t, hasRadarFetcherRequestHost(requests, "datasets-server.huggingface.co"))
	require.Contains(t, requests, claudeStatuspageAPIURL)
	require.Contains(t, requests, openAIStatuspageAPIURL)
	require.Contains(t, requests, windsurfStatuspageAPIURL)
	require.Contains(t, requests, kimiStatuspageAPIURL)
	require.Contains(t, requests, miniMaxChinaStatuspageAPIURL)
	require.Contains(t, requests, openAIStatuspageFeedURL)
	require.Contains(t, requests, openAIStatusSummaryURL)
	require.True(t, hasRadarFetcherRequestPath(requests, "/proxy/openai-1/component_impacts"))
	require.Contains(t, requests, claudeAPIHistoryURL)
	require.Contains(t, requests, claudeCodeHistoryURL)
	require.Contains(t, requests, windsurfHistoryURL)
	require.Contains(t, requests, kimiHistoryURL)
	require.Contains(t, requests, miniMaxChinaLLMHistoryURL)
	require.Contains(t, requests, deepSeekStatusDataURL)
}

func radarStatuspageSourceForHost(t *testing.T, host string) RadarSourceKey {
	t.Helper()
	switch host {
	case "status.claude.com":
		return RadarSourceStatusClaude
	case "status.openai.com":
		return RadarSourceStatusOpenAI
	case "status.windsurf.com":
		return RadarSourceStatusWindsurf
	case "status.moonshot.cn":
		return RadarSourceStatusKimi
	case "status.minimaxi.com":
		return RadarSourceStatusMiniMaxChina
	default:
		t.Fatalf("unknown statuspage host %s", host)
		return ""
	}
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

func hasRadarFetcherRequestPath(requests map[string]*http.Request, path string) bool {
	for _, request := range requests {
		if request.URL.Path == path {
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
