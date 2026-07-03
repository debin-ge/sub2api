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
		benchmark.GET("/targets", h.Admin.Benchmark.ListTargets)
		benchmark.POST("/targets", h.Admin.Benchmark.CreateTarget)
		benchmark.GET("/targets/:id", h.Admin.Benchmark.GetTarget)
		benchmark.PUT("/targets/:id", h.Admin.Benchmark.UpdateTarget)
		benchmark.DELETE("/targets/:id", h.Admin.Benchmark.DeleteTarget)

		benchmark.GET("/tasks", h.Admin.Benchmark.ListTasks)
		benchmark.POST("/tasks", h.Admin.Benchmark.CreateTask)
		benchmark.GET("/tasks/:id", h.Admin.Benchmark.GetTask)
		benchmark.PUT("/tasks/:id", h.Admin.Benchmark.UpdateTask)
		benchmark.DELETE("/tasks/:id", h.Admin.Benchmark.DeleteTask)

		benchmark.POST("/runs/preview", h.Admin.Benchmark.PreviewRun)
		benchmark.POST("/runs", h.Admin.Benchmark.CreateRun)
		benchmark.GET("/runs", h.Admin.Benchmark.ListRuns)
		benchmark.POST("/runs/process-due", h.Admin.Benchmark.ProcessDueRuns)
		benchmark.GET("/runs/:id", h.Admin.Benchmark.GetRun)
		benchmark.GET("/runs/:id/detail", h.Admin.Benchmark.GetRunDetail)
		benchmark.GET("/runs/:id/results", h.Admin.Benchmark.ListRunResults)
		benchmark.GET("/runs/:id/scores", h.Admin.Benchmark.ListRunScores)
		benchmark.POST("/runs/:id/process", h.Admin.Benchmark.ProcessRun)
		benchmark.POST("/runs/:id/publish", h.Admin.Benchmark.PublishRun)
		benchmark.POST("/runs/:id/cancel", h.Admin.Benchmark.CancelRun)

		benchmark.GET("/trends", h.Admin.Benchmark.GetTrends)

		benchmark.GET("/schedules", h.Admin.Benchmark.ListSchedules)
		benchmark.POST("/schedules", h.Admin.Benchmark.CreateSchedule)
		benchmark.GET("/schedules/:id", h.Admin.Benchmark.GetSchedule)
		benchmark.PUT("/schedules/:id", h.Admin.Benchmark.UpdateSchedule)
		benchmark.DELETE("/schedules/:id", h.Admin.Benchmark.DeleteSchedule)
		benchmark.POST("/schedules/:id/trigger", h.Admin.Benchmark.TriggerSchedule)
	}
}
