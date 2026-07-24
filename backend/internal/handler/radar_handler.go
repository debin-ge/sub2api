package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	radarHealthCacheControl      = "public, max-age=300"
	radarQuotaLatestCacheControl = "public, max-age=300"
	radarQuotaTrendCacheControl  = "public, max-age=600"
	radarDegradationCacheControl = "public, max-age=300"
	radarSourcesCacheControl     = "public, max-age=600"
	radarDefaultQuotaTrendDays   = 7
	radarMaxQuotaTrendDays       = 7
)

// RadarHandler exposes the read-only public Model Radar endpoints.
type RadarHandler struct {
	radarService service.RadarPublicService
}

// NewRadarHandler constructs the public Radar handler.
func NewRadarHandler(cfg *config.Config, radarService service.RadarPublicService) (*RadarHandler, error) {
	if cfg == nil {
		return nil, errors.New("radar handler requires config")
	}
	if isNilRadarPublicService(radarService) {
		return nil, errors.New("radar handler requires service")
	}

	return &RadarHandler{radarService: radarService}, nil
}

// GetServiceHealth returns supported public service health and 30-day history.
func (h *RadarHandler) GetServiceHealth(c *gin.Context) {
	if !radarHasNoQuery(c) {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetServiceHealth(c.Request.Context())
	if err != nil {
		writeRadarError(c, err)
		return
	}
	writeRadarSuccess(c, radarHealthCacheControl, data)
}

// GetQuotaBucketsLatest returns all latest public quota bucket snapshots.
func (h *RadarHandler) GetQuotaBucketsLatest(c *gin.Context) {
	if !radarHasNoQuery(c) {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetQuotaBucketsLatest(c.Request.Context())
	if err != nil {
		writeRadarError(c, err)
		return
	}
	writeRadarSuccess(c, radarQuotaLatestCacheControl, data)
}

// GetQuotaBucketsTrend returns one canonical quota bucket's recent trend.
func (h *RadarHandler) GetQuotaBucketsTrend(c *gin.Context) {
	bucket, days, ok := parseQuotaTrendQuery(c)
	if !ok {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetQuotaBucketsTrend(c.Request.Context(), bucket, days)
	if err != nil {
		writeRadarError(c, err)
		return
	}
	writeRadarSuccess(c, radarQuotaTrendCacheControl, data)
}

// GetDegradationLatest returns the latest public model quality snapshot.
func (h *RadarHandler) GetDegradationLatest(c *gin.Context) {
	if !radarHasNoQuery(c) {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetDegradationLatest(c.Request.Context())
	if err != nil {
		writeRadarError(c, err)
		return
	}
	cacheControl := radarDegradationCacheControl
	if data != nil && data.Stale && len(data.AvailableModels) == 0 {
		cacheControl = "no-store"
	}
	writeRadarSuccess(c, cacheControl, data)
}

// GetLMArena returns the complete public LMArena snapshot.
func (h *RadarHandler) GetLMArena(c *gin.Context) {
	if !radarHasNoQuery(c) {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetLMArena(c.Request.Context())
	if err != nil {
		writeRadarError(c, err)
		return
	}
	writeRadarSuccess(c, radarDegradationCacheControl, data)
}

// GetDataSources returns safe public source freshness metadata.
func (h *RadarHandler) GetDataSources(c *gin.Context) {
	if !radarHasNoQuery(c) {
		writeRadarError(c, service.ErrInvalidRadarQuery)
		return
	}
	data, err := h.radarService.GetDataSources(c.Request.Context())
	if err != nil {
		writeRadarError(c, err)
		return
	}
	writeRadarSuccess(c, radarSourcesCacheControl, data)
}

func radarHasNoQuery(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.URL != nil && c.Request.URL.RawQuery == "" && !c.Request.URL.ForceQuery
}

func parseQuotaTrendQuery(c *gin.Context) (string, int, bool) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) < 1 || len(query) > 2 {
		return "", 0, false
	}
	for key := range query {
		if key != "bucket" && key != "days" {
			return "", 0, false
		}
	}
	bucketValues, present := query["bucket"]
	if !present || len(bucketValues) != 1 {
		return "", 0, false
	}
	bucket := bucketValues[0]
	if _, _, err := service.ParseRadarBucketKey(bucket); err != nil {
		return "", 0, false
	}

	days := radarDefaultQuotaTrendDays
	if daysValues, present := query["days"]; present {
		if len(daysValues) != 1 {
			return "", 0, false
		}
		parsedDays, ok := parseRadarDecimalDays(daysValues[0], radarMaxQuotaTrendDays)
		if !ok {
			return "", 0, false
		}
		days = parsedDays
	}
	return bucket, days, true
}

func parseRadarDecimalDays(raw string, maximum int) (int, bool) {
	if raw == "" {
		return 0, false
	}
	for index := range raw {
		if raw[index] < '0' || raw[index] > '9' {
			return 0, false
		}
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > maximum {
		return 0, false
	}
	return days, true
}

func writeRadarSuccess(c *gin.Context, cacheControl string, data any) {
	c.Header("Cache-Control", cacheControl)
	response.Success(c, data)
}

func writeRadarError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	switch {
	case errors.Is(err, service.ErrInvalidRadarQuery):
		response.Error(c, http.StatusBadRequest, "invalid radar query")
	case errors.Is(err, context.Canceled):
		response.Error(c, 499, "context canceled")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, service.ErrRadarUnavailable):
		response.Error(c, http.StatusServiceUnavailable, "Service temporarily unavailable, please retry later")
	default:
		response.Error(c, http.StatusInternalServerError, "internal error")
	}
}

func isNilRadarPublicService(radarService service.RadarPublicService) bool {
	if radarService == nil {
		return true
	}
	value := reflect.ValueOf(radarService)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
