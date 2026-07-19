package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/gin-gonic/gin"
)

// RegisterRadarRoutes registers public read-only Radar endpoints without auth
// middleware. Authentication-protected groups must remain siblings of this one.
func RegisterRadarRoutes(v1 *gin.RouterGroup, radarHandler *handler.RadarHandler) {
	publicRadar := v1.Group("/public/radar")
	publicRadar.Use(radarMetricsMiddleware(observability.DefaultRadarMetrics()))
	{
		publicRadar.GET("/service-health", radarHandler.GetServiceHealth)
		publicRadar.GET("/quota-buckets/latest", radarHandler.GetQuotaBucketsLatest)
		publicRadar.GET("/quota-buckets/trend", radarHandler.GetQuotaBucketsTrend)
		publicRadar.GET("/degradation/latest", radarHandler.GetDegradationLatest)
		publicRadar.GET("/degradation/trend", radarHandler.GetDegradationTrend)
		publicRadar.GET("/lmarena", radarHandler.GetLMArena)
		publicRadar.GET("/sources", radarHandler.GetDataSources)
	}
}

func radarMetricsMiddleware(metrics *observability.RadarMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		metrics.ObserveHTTP(c.FullPath(), c.Writer.Status(), time.Since(startedAt))
	}
}
