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

type radarSnapshotServiceStub struct {
	getPublicRadarFn func(context.Context) (*service.BenchmarkPublicRadar, error)
}

func (s *radarSnapshotServiceStub) GetPublicRadar(ctx context.Context) (*service.BenchmarkPublicRadar, error) {
	if s.getPublicRadarFn != nil {
		return s.getPublicRadarFn(ctx)
	}
	return nil, nil
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
	require.JSONEq(t, `{"ranking_basis":"ability_score_only","latest_run":null,"targets":[]}`, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"success"`)
}

func TestRadarHandlerReturnsLatestPublicSnapshot(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 6, 24, 8, 30, 0, 0, time.UTC)
	router := newRadarHandlerTestRouter(&RadarHandler{
		reader: &radarSnapshotServiceStub{
			getPublicRadarFn: func(context.Context) (*service.BenchmarkPublicRadar, error) {
				return &service.BenchmarkPublicRadar{
					RankingBasis: "ability_score_only",
					LatestRun: &service.BenchmarkPublicRun{
						ID:          42,
						SuiteID:     7,
						ProfileID:   9,
						Status:      service.BenchmarkRunStatusCompleted,
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
							Dimensions: map[string]float64{
								"reasoning": 93.4,
							},
							ScoreBasis: service.BenchmarkPublicScoreBasis{
								PlannedTasks:       12,
								ScoredTasks:        12,
								InvalidTasks:       0,
								CoverageRate:       1,
								ConfidenceLevel:    service.BenchmarkConfidenceHigh,
								InsufficientSample: false,
							},
							Metrics: service.BenchmarkPublicMetrics{
								SuccessRate:   1,
								EstimatedCost: 0.19,
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

	latestRun, ok := payload["latest_run"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 42, latestRun["id"])
	require.EqualValues(t, 7, latestRun["suite_id"])
	require.EqualValues(t, 9, latestRun["profile_id"])
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
								SuccessRate:   0.9,
								EstimatedCost: 0.23,
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
