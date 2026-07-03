package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type radarSnapshotReader interface {
	GetPublicRadar(ctx context.Context) (*service.BenchmarkPublicRadar, error)
}

type radarRuntimeProvider interface {
	GetBenchmarkRuntime(ctx context.Context) service.BenchmarkRuntime
}

type RadarHandler struct {
	snapshot        *service.BenchmarkSnapshotService
	reader          radarSnapshotReader
	runtimeProvider radarRuntimeProvider
}

func NewRadarHandler(snapshot *service.BenchmarkSnapshotService) *RadarHandler {
	return &RadarHandler{snapshot: snapshot}
}

func (h *RadarHandler) SetBenchmarkRuntimeProvider(provider radarRuntimeProvider) {
	h.runtimeProvider = provider
}

func (h *RadarHandler) SetSettingService(settingService *service.SettingService) {
	h.SetBenchmarkRuntimeProvider(settingService)
}

type radarResponse struct {
	RankingBasis string                          `json:"ranking_basis"`
	PublishedAt  *time.Time                      `json:"published_at,omitempty"`
	LatestRun    *service.BenchmarkPublicRun     `json:"latest_run"`
	Targets      []service.BenchmarkPublicTarget `json:"targets"`
	Trends       []service.BenchmarkPublicTrend  `json:"trends"`
}

func (h *RadarHandler) GetCurrent(c *gin.Context) {
	runtime := h.getBenchmarkRuntime(c.Request.Context())
	if !runtime.PublicEnabled {
		c.JSON(http.StatusOK, emptyRadarResponse())
		return
	}

	reader := h.reader
	if reader == nil {
		reader = h.snapshot
	}

	radar, err := reader.GetPublicRadar(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if radar == nil {
		c.JSON(http.StatusOK, emptyRadarResponse())
		return
	}

	c.JSON(http.StatusOK, radarResponse{
		RankingBasis: radar.RankingBasis,
		PublishedAt:  radar.PublishedAt,
		LatestRun:    radar.LatestRun,
		Targets:      radar.Targets,
		Trends:       radar.Trends,
	})
}

func (h *RadarHandler) getBenchmarkRuntime(ctx context.Context) service.BenchmarkRuntime {
	if h == nil || h.runtimeProvider == nil {
		return service.BenchmarkRuntime{
			Enabled:               true,
			PublicEnabled:         true,
			GlobalConcurrency:     service.BenchmarkGlobalConcurrencyDefault,
			DefaultTimeoutSeconds: service.BenchmarkDefaultTimeoutSecondsDefault,
		}
	}
	return h.runtimeProvider.GetBenchmarkRuntime(ctx)
}

func emptyRadarResponse() radarResponse {
	return radarResponse{
		RankingBasis: "ability_score_only",
		LatestRun:    nil,
		Targets:      []service.BenchmarkPublicTarget{},
		Trends:       []service.BenchmarkPublicTrend{},
	}
}
