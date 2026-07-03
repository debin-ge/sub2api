package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

type radarSnapshotServiceStub struct {
	getPublicRadarFn func(context.Context) (*service.BenchmarkPublicRadar, error)
	calls            int
}

func (s *radarSnapshotServiceStub) GetPublicRadar(ctx context.Context) (*service.BenchmarkPublicRadar, error) {
	s.calls++
	if s.getPublicRadarFn != nil {
		return s.getPublicRadarFn(ctx)
	}
	return nil, nil
}

type radarRuntimeProviderStub struct {
	runtime service.BenchmarkRuntime
}

func (s *radarRuntimeProviderStub) GetBenchmarkRuntime(context.Context) service.BenchmarkRuntime {
	return s.runtime
}

func newRadarHandlerTestRouter(h *RadarHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/public/radar", h.GetCurrent)
	return router
}

func TestRadarHandlerSnapshotFieldIsConcreteService(t *testing.T) {
	t.Parallel()

	var snapshot *service.BenchmarkSnapshotService = (&RadarHandler{}).snapshot
	require.Nil(t, snapshot)
}

func TestRadarHandlerReturnsEmptyRadarWhenNoSnapshot(t *testing.T) {
	t.Parallel()

	router := newRadarHandlerTestRouter(&RadarHandler{
		reader: &radarSnapshotServiceStub{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/radar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ranking_basis":"ability_score_only","latest_run":null,"targets":[],"trends":[]}`, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"success"`)
}

func TestRadarHandlerReturnsEmptyRadarWhenBenchmarkPublicDisabled(t *testing.T) {
	t.Parallel()

	reader := &radarSnapshotServiceStub{
		getPublicRadarFn: func(context.Context) (*service.BenchmarkPublicRadar, error) {
			return &service.BenchmarkPublicRadar{
				RankingBasis: "ability_score_only",
				Targets: []service.BenchmarkPublicTarget{
					{Rank: 1, Model: "gpt-4.1"},
				},
			}, nil
		},
	}
	handler := &RadarHandler{
		reader: reader,
	}
	handler.SetBenchmarkRuntimeProvider(&radarRuntimeProviderStub{
		runtime: service.BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         false,
			GlobalConcurrency:     service.BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: service.BenchmarkDefaultTimeoutSecondsDefault,
		},
	})

	router := newRadarHandlerTestRouter(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/radar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ranking_basis":"ability_score_only","latest_run":null,"targets":[],"trends":[]}`, rec.Body.String())
}

func TestRadarHandlerSkipsSnapshotLookupWhenBenchmarkPublicDisabled(t *testing.T) {
	t.Parallel()

	reader := &radarSnapshotServiceStub{
		getPublicRadarFn: func(context.Context) (*service.BenchmarkPublicRadar, error) {
			t.Fatal("snapshot lookup should be skipped when benchmark public output is disabled")
			return nil, nil
		},
	}
	handler := &RadarHandler{
		reader: reader,
	}
	handler.SetBenchmarkRuntimeProvider(&radarRuntimeProviderStub{
		runtime: service.BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         false,
			GlobalConcurrency:     service.BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: service.BenchmarkDefaultTimeoutSecondsDefault,
		},
	})

	router := newRadarHandlerTestRouter(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/radar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 0, reader.calls)
}

func TestRadarHandlerReturnsLatestPublicSnapshot(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 6, 24, 8, 30, 0, 0, time.UTC)
	publishedAt := time.Date(2026, 6, 24, 9, 15, 0, 0, time.UTC)
	router := newRadarHandlerTestRouter(&RadarHandler{
		reader: &radarSnapshotServiceStub{
			getPublicRadarFn: func(context.Context) (*service.BenchmarkPublicRadar, error) {
				return &service.BenchmarkPublicRadar{
					RankingBasis: "ability_score_only",
					PublishedAt:  &publishedAt,
					LatestRun: &service.BenchmarkPublicRun{
						ID:          42,
						Status:      service.BenchmarkRunStatusCompleted,
						TaskCount:   12,
						CompletedAt: &completedAt,
					},
					Targets: []service.BenchmarkPublicTarget{
						{
							Rank:         1,
							Model:        "gpt-4.1",
							ChannelID:    1001,
							ChannelName:  "openai-prod",
							DisplayName:  "GPT 4.1",
							OverallScore: 93.4,
							PassedCount:  12,
							TotalCount:   12,
							Dimensions: map[string]float64{
								"reasoning": 93.4,
							},
							Metrics: service.BenchmarkPublicMetrics{
								AvgLatencyMS:   ptr(120.0),
								AvgTotalTokens: ptr(500.0),
								TotalCost:      0.19,
							},
						},
					},
				}, nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/radar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"success"`)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "ability_score_only", payload["ranking_basis"])
	require.Equal(t, publishedAt.Format(time.RFC3339), payload["published_at"])

	latestRun, ok := payload["latest_run"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 42, latestRun["id"])
	require.EqualValues(t, 12, latestRun["task_count"])
	require.Equal(t, service.BenchmarkRunStatusCompleted, latestRun["status"])
	require.Equal(t, completedAt.Format(time.RFC3339), latestRun["completed_at"])

	targets, ok := payload["targets"].([]any)
	require.True(t, ok)
	require.Len(t, targets, 1)

	target, ok := targets[0].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 1, target["rank"])
	require.Equal(t, "gpt-4.1", target["model"])
	require.Equal(t, "GPT 4.1", target["display_name"])
	require.EqualValues(t, 1001, target["channel_id"])
	require.EqualValues(t, 93.4, target["overall_score"])
}

func TestRadarHandlerNeverReturnsSensitiveFields(t *testing.T) {
	t.Parallel()

	router := newRadarHandlerTestRouter(&RadarHandler{
		reader: &radarSnapshotServiceStub{
			getPublicRadarFn: func(context.Context) (*service.BenchmarkPublicRadar, error) {
				return &service.BenchmarkPublicRadar{
					RankingBasis: "ability_score_only",
					Targets: []service.BenchmarkPublicTarget{
						{
							Rank:         1,
							Model:        "claude-sonnet-4-5",
							ChannelID:    77,
							ChannelName:  "anthropic-prod",
							DisplayName:  "Claude Sonnet 4.5",
							OverallScore: 88.8,
							Metrics: service.BenchmarkPublicMetrics{
								AvgLatencyMS:   ptr(200.0),
								AvgTotalTokens: ptr(600.0),
								TotalCost:      0.23,
							},
						},
					},
				}, nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/radar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"success"`)
	require.NotContains(t, rec.Body.String(), `"config_snapshot"`)
	require.NotContains(t, rec.Body.String(), `"verifier_config"`)
	require.NotContains(t, rec.Body.String(), `"raw_response"`)
	require.NotContains(t, rec.Body.String(), `"input_payload"`)
	require.NotContains(t, rec.Body.String(), `"metadata"`)
	require.NotContains(t, rec.Body.String(), `"provider_snapshot"`)
}
