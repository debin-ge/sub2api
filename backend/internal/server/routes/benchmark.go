package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"

	"github.com/gin-gonic/gin"
)

func RegisterBenchmarkRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	public := v1.Group("/public")
	{
		public.GET("/radar", h.Radar.GetCurrent)
	}
}

func registerAdminBenchmarkRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	benchmark := admin.Group("/benchmark")
	{
		benchmark.GET("/suites", h.Admin.Benchmark.ListSuites)
		benchmark.POST("/suites", h.Admin.Benchmark.CreateSuite)
		benchmark.GET("/targets", h.Admin.Benchmark.ListTargets)
		benchmark.POST("/targets", h.Admin.Benchmark.CreateTarget)
		benchmark.GET("/tasks", h.Admin.Benchmark.ListTasks)
		benchmark.POST("/tasks", h.Admin.Benchmark.CreateTask)
		benchmark.GET("/profiles", h.Admin.Benchmark.ListProfiles)
		benchmark.POST("/profiles", h.Admin.Benchmark.CreateProfile)
		benchmark.GET("/profiles/:id", h.Admin.Benchmark.GetProfile)
		benchmark.POST("/profiles/:id/preview", h.Admin.Benchmark.PreviewProfile)
		benchmark.POST("/runs", h.Admin.Benchmark.CreateRun)
		benchmark.GET("/runs", h.Admin.Benchmark.ListRuns)
		benchmark.GET("/runs/:id", h.Admin.Benchmark.GetRun)
		benchmark.GET("/runs/:id/results", h.Admin.Benchmark.ListRunResults)
		benchmark.GET("/runs/:id/scores", h.Admin.Benchmark.ListRunScores)
		benchmark.POST("/runs/:id/publish", h.Admin.Benchmark.PublishRun)
	}
}
