package handler

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type radarSnapshotReader interface {
	GetPublicRadar(ctx context.Context) (*service.BenchmarkPublicRadar, error)
}

type RadarHandler struct {
	snapshot *service.BenchmarkSnapshotService
	reader   radarSnapshotReader
}

func NewRadarHandler(snapshot *service.BenchmarkSnapshotService) *RadarHandler {
	return &RadarHandler{snapshot: snapshot}
}

type radarResponse struct {
	RankingBasis string                          `json:"ranking_basis"`
	LatestRun    *service.BenchmarkPublicRun     `json:"latest_run"`
	Targets      []service.BenchmarkPublicTarget `json:"targets"`
}

func (h *RadarHandler) GetCurrent(c *gin.Context) {
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
		LatestRun:    radar.LatestRun,
		Targets:      radar.Targets,
	})
}

func emptyRadarResponse() radarResponse {
	return radarResponse{
		RankingBasis: "ability_score_only",
		LatestRun:    nil,
		Targets:      []service.BenchmarkPublicTarget{},
	}
}
