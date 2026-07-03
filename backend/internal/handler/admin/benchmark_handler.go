package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type benchmarkAdminService interface {
	ListTargets(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	GetTarget(ctx context.Context, id int64) (*ent.BenchmarkTarget, error)
	UpdateTarget(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	DeleteTarget(ctx context.Context, id int64) error
	ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	GetTask(ctx context.Context, id int64) (*ent.BenchmarkTask, error)
	UpdateTask(ctx context.Context, id int64, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	DeleteTask(ctx context.Context, id int64) error
	EnsureStandardTasks(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error)
	PreviewRun(ctx context.Context, targetIDs []int64, taskCount int) (*service.BenchmarkRunPreview, error)
	CreateRun(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
	CreateStandardRun(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error)
	ListRuns(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	CancelRun(ctx context.Context, runID int64, reason string) error
	ListRunTargets(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error)
	ListRunTasks(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error)
	ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	ListTargetScores(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error)
}

type benchmarkAdminSnapshotService interface {
	PublishPublicSnapshot(ctx context.Context, runID int64) error
	GetTrends(ctx context.Context, days int, limit int) ([]service.BenchmarkPublicTrend, error)
}

type benchmarkAdminProcessor interface {
	ProcessRun(ctx context.Context, runID int64) (int, error)
	ProcessDue(ctx context.Context, options service.BenchmarkProcessOptions) (int, error)
}

type benchmarkScheduleAdminService interface {
	ListSchedules(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error)
	CreateSchedule(ctx context.Context, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	GetSchedule(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error)
	UpdateSchedule(ctx context.Context, id int64, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	DeleteSchedule(ctx context.Context, id int64) error
	TriggerSchedule(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error)
}

type BenchmarkHandler struct {
	benchmarkService benchmarkAdminService
	scheduleService  benchmarkScheduleAdminService
	snapshotService  benchmarkAdminSnapshotService
	processor        benchmarkAdminProcessor
}

func NewBenchmarkHandler(benchmarkService *service.BenchmarkService, snapshotService *service.BenchmarkSnapshotService) *BenchmarkHandler {
	return &BenchmarkHandler{
		benchmarkService: benchmarkService,
		snapshotService:  snapshotService,
	}
}

func (h *BenchmarkHandler) SetScheduleService(scheduleService *service.BenchmarkScheduleService) {
	h.scheduleService = scheduleService
}

func (h *BenchmarkHandler) SetProcessor(processor *service.BenchmarkProcessor) {
	if processor == nil {
		h.processor = nil
		return
	}
	h.processor = processor
}

// ---- request DTOs ----

type benchmarkTargetCreateRequest struct {
	ModelName           string `json:"model_name"`
	ChannelID           int64  `json:"channel_id"`
	DisplayName         string `json:"display_name"`
	ChannelNameSnapshot string `json:"channel_name_snapshot"`
	Enabled             bool   `json:"enabled"`
	PublicVisible       bool   `json:"public_visible"`
	SortOrder           int    `json:"sort_order"`
}

type benchmarkTaskCreateRequest struct {
	Title          string         `json:"title"`
	Type           string         `json:"type"`
	Difficulty     string         `json:"difficulty"`
	Prompt         string         `json:"prompt"`
	InputPayload   map[string]any `json:"input_payload"`
	ExpectedOutput map[string]any `json:"expected_output"`
	VerifierType   string         `json:"verifier_type"`
	VerifierConfig map[string]any `json:"verifier_config"`
	Weight         float64        `json:"weight"`
	PublicPrompt   bool           `json:"public_prompt"`
	Enabled        bool           `json:"enabled"`
	SortOrder      int            `json:"sort_order"`
}

type benchmarkRunCreateRequest struct {
	TargetIDs          []int64 `json:"target_ids"`
	TaskCount          int     `json:"task_count"`
	TriggerType        string  `json:"trigger_type"`
	CreatedBy          *int64  `json:"created_by"`
	ProcessImmediately bool    `json:"process_immediately"`
}

type benchmarkStandardRunCreateRequest struct {
	TargetIDs          []int64 `json:"target_ids"`
	TaskCount          int     `json:"task_count"`
	ProcessImmediately *bool   `json:"process_immediately"`
	CreatedBy          *int64  `json:"created_by"`
}

type benchmarkRunPreviewRequest struct {
	TargetIDs []int64 `json:"target_ids"`
	TaskCount int     `json:"task_count"`
}

type benchmarkRunCancelRequest struct {
	Reason string `json:"reason"`
}

type benchmarkProcessDueRequest struct {
	Limit int `json:"limit"`
}

type benchmarkScheduleCreateRequest struct {
	Name      string  `json:"name"`
	CronExpr  string  `json:"cron_expr"`
	Enabled   bool    `json:"enabled"`
	TargetIDs []int64 `json:"target_ids"`
	TaskCount int     `json:"task_count"`
}

// ---- Targets ----

func (h *BenchmarkHandler) ListTargets(c *gin.Context) {
	input := service.NormalizeBenchmarkListInput(parseBenchmarkListInput(c))
	items, total, err := h.benchmarkService.ListTargets(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) CreateTarget(c *gin.Context) {
	var req benchmarkTargetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	target, err := h.benchmarkService.CreateTarget(c.Request.Context(), benchmarkTargetInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, target)
}

func (h *BenchmarkHandler) GetTarget(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TARGET_ID", "invalid target id")
	if !ok {
		return
	}
	target, err := h.benchmarkService.GetTarget(c.Request.Context(), id)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, target)
}

func (h *BenchmarkHandler) UpdateTarget(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TARGET_ID", "invalid target id")
	if !ok {
		return
	}
	var req benchmarkTargetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	target, err := h.benchmarkService.UpdateTarget(c.Request.Context(), id, benchmarkTargetInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, target)
}

func (h *BenchmarkHandler) DeleteTarget(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TARGET_ID", "invalid target id")
	if !ok {
		return
	}
	if err := h.benchmarkService.DeleteTarget(c.Request.Context(), id); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

// ---- Tasks ----

func (h *BenchmarkHandler) ListTasks(c *gin.Context) {
	input, ok := parseBenchmarkTaskListInput(c)
	if !ok {
		return
	}
	input = service.NormalizeBenchmarkTaskListInput(input)
	items, total, err := h.benchmarkService.ListTasks(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) CreateTask(c *gin.Context) {
	var req benchmarkTaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	task, err := h.benchmarkService.CreateTask(c.Request.Context(), benchmarkTaskInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, task)
}

func (h *BenchmarkHandler) GetTask(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TASK_ID", "invalid task id")
	if !ok {
		return
	}
	task, err := h.benchmarkService.GetTask(c.Request.Context(), id)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, task)
}

func (h *BenchmarkHandler) UpdateTask(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TASK_ID", "invalid task id")
	if !ok {
		return
	}
	var req benchmarkTaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	task, err := h.benchmarkService.UpdateTask(c.Request.Context(), id, benchmarkTaskInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, task)
}

func (h *BenchmarkHandler) DeleteTask(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_TASK_ID", "invalid task id")
	if !ok {
		return
	}
	if err := h.benchmarkService.DeleteTask(c.Request.Context(), id); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

func (h *BenchmarkHandler) ApplyStandardTasks(c *gin.Context) {
	result, err := h.benchmarkService.EnsureStandardTasks(c.Request.Context())
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, result)
}

// ---- Runs ----

func (h *BenchmarkHandler) PreviewRun(c *gin.Context) {
	var req benchmarkRunPreviewRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}
	preview, err := h.benchmarkService.PreviewRun(c.Request.Context(), req.TargetIDs, req.TaskCount)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *BenchmarkHandler) CreateRun(c *gin.Context) {
	var req benchmarkRunCreateRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}
	var processor benchmarkAdminProcessor
	if req.ProcessImmediately {
		processor = h.processor
		if processor == nil {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_PROCESSOR_UNAVAILABLE", "benchmark processor unavailable"))
			return
		}
	}
	run, err := h.benchmarkService.CreateRun(c.Request.Context(), service.BenchmarkCreateRunRequest{
		TargetIDs:   req.TargetIDs,
		TaskCount:   req.TaskCount,
		TriggerType: req.TriggerType,
		CreatedBy:   req.CreatedBy,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	if req.ProcessImmediately {
		processBenchmarkRunInBackground(processor, run.ID)
	}
	response.Success(c, run)
}

func (h *BenchmarkHandler) CreateStandardRun(c *gin.Context) {
	var req benchmarkStandardRunCreateRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}

	processImmediately := true
	if req.ProcessImmediately != nil {
		processImmediately = *req.ProcessImmediately
	}

	var processor benchmarkAdminProcessor
	if processImmediately {
		processor = h.processor
		if processor == nil {
			response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_PROCESSOR_UNAVAILABLE", "benchmark processor unavailable"))
			return
		}
	}

	run, err := h.benchmarkService.CreateStandardRun(c.Request.Context(), service.BenchmarkStandardRunRequest{
		TargetIDs:          req.TargetIDs,
		TaskCount:          req.TaskCount,
		ProcessImmediately: processImmediately,
		CreatedBy:          req.CreatedBy,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	if processImmediately {
		processBenchmarkRunInBackground(processor, run.ID)
	}
	response.Success(c, run)
}

func processBenchmarkRunInBackground(processor benchmarkAdminProcessor, runID int64) {
	go func(processor benchmarkAdminProcessor, runID int64) {
		log := logger.L().With(
			zap.String("component", "handler.admin.benchmark"),
			zap.Int64("run_id", runID),
		)
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error("benchmark.process_immediately_panic_recovered", zap.Any("panic", recovered))
			}
		}()
		if err := processBenchmarkRunUntilDone(context.Background(), processor, runID); err != nil {
			log.Error("benchmark.process_immediately_failed", zap.Error(err))
		}
	}(processor, runID)
}

// processBenchmarkRunUntilDone drives a run through repeated ProcessRun calls
// until no more work is processed (run reaches a terminal state).
func processBenchmarkRunUntilDone(ctx context.Context, processor benchmarkAdminProcessor, runID int64) error {
	for i := 0; i < 1000; i++ {
		processed, err := processor.ProcessRun(ctx, runID)
		if err != nil {
			return err
		}
		if processed == 0 {
			return nil
		}
	}
	return nil
}

func (h *BenchmarkHandler) ListRuns(c *gin.Context) {
	input, ok := parseBenchmarkRunListInput(c)
	if !ok {
		return
	}
	input = service.NormalizeBenchmarkRunListInput(input)
	items, total, err := h.benchmarkService.ListRuns(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) GetRun(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	run, err := h.benchmarkService.GetRun(c.Request.Context(), id)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, run)
}

func (h *BenchmarkHandler) CancelRun(c *gin.Context) {
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	var req benchmarkRunCancelRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}
	if err := h.benchmarkService.CancelRun(c.Request.Context(), runID, req.Reason); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

func (h *BenchmarkHandler) ProcessRun(c *gin.Context) {
	processor, ok := h.requireProcessor(c)
	if !ok {
		return
	}
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	processed, err := processor.ProcessRun(c.Request.Context(), runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"processed": processed})
}

func (h *BenchmarkHandler) ProcessDueRuns(c *gin.Context) {
	processor, ok := h.requireProcessor(c)
	if !ok {
		return
	}
	var req benchmarkProcessDueRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
			return
		}
	}
	processed, err := processor.ProcessDue(c.Request.Context(), service.BenchmarkProcessOptions{RunLimit: req.Limit})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"processed": processed})
}

func (h *BenchmarkHandler) requireProcessor(c *gin.Context) (benchmarkAdminProcessor, bool) {
	if h.processor == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_PROCESSOR_UNAVAILABLE", "benchmark processor unavailable"))
		return nil, false
	}
	return h.processor, true
}

func (h *BenchmarkHandler) GetRunDetail(c *gin.Context) {
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	run, err := h.benchmarkService.GetRun(ctx, runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	targets, err := h.benchmarkService.ListRunTargets(ctx, runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	tasks, err := h.benchmarkService.ListRunTasks(ctx, runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	results, err := h.benchmarkService.ListRunResults(ctx, runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	scores, err := h.benchmarkService.ListTargetScores(ctx, runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{
		"run":     run,
		"targets": targets,
		"tasks":   tasks,
		"results": results,
		"scores":  scores,
	})
}

func (h *BenchmarkHandler) ListRunResults(c *gin.Context) {
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	results, err := h.benchmarkService.ListRunResults(c.Request.Context(), runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, results)
}

func (h *BenchmarkHandler) ListRunScores(c *gin.Context) {
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	scores, err := h.benchmarkService.ListTargetScores(c.Request.Context(), runID)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, scores)
}

func (h *BenchmarkHandler) PublishRun(c *gin.Context) {
	runID, ok := parseBenchmarkPathID(c, "id", "INVALID_RUN_ID", "invalid run id")
	if !ok {
		return
	}
	if h.snapshotService == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_SNAPSHOT_UNAVAILABLE", "benchmark snapshot service unavailable"))
		return
	}
	if err := h.snapshotService.PublishPublicSnapshot(c.Request.Context(), runID); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

// ---- Trends ----

func (h *BenchmarkHandler) GetTrends(c *gin.Context) {
	if h.snapshotService == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_SNAPSHOT_UNAVAILABLE", "benchmark snapshot service unavailable"))
		return
	}
	days := benchmarkQueryInt(c, "days")
	limit := benchmarkQueryInt(c, "limit")
	trends, err := h.snapshotService.GetTrends(c.Request.Context(), days, limit)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"trends": trends})
}

// ---- Schedules ----

func (h *BenchmarkHandler) ListSchedules(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	input, ok := parseBenchmarkScheduleListInput(c)
	if !ok {
		return
	}
	input = service.NormalizeBenchmarkScheduleListInput(input)
	items, total, err := scheduleService.ListSchedules(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) CreateSchedule(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	var req benchmarkScheduleCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	schedule, err := scheduleService.CreateSchedule(c.Request.Context(), benchmarkScheduleInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, schedule)
}

func (h *BenchmarkHandler) GetSchedule(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_SCHEDULE_ID", "invalid schedule id")
	if !ok {
		return
	}
	schedule, err := scheduleService.GetSchedule(c.Request.Context(), id)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, schedule)
}

func (h *BenchmarkHandler) UpdateSchedule(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_SCHEDULE_ID", "invalid schedule id")
	if !ok {
		return
	}
	var req benchmarkScheduleCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	schedule, err := scheduleService.UpdateSchedule(c.Request.Context(), id, benchmarkScheduleInputFromRequest(req))
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, schedule)
}

func (h *BenchmarkHandler) DeleteSchedule(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_SCHEDULE_ID", "invalid schedule id")
	if !ok {
		return
	}
	if err := scheduleService.DeleteSchedule(c.Request.Context(), id); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

func (h *BenchmarkHandler) TriggerSchedule(c *gin.Context) {
	scheduleService, ok := h.requireScheduleService(c)
	if !ok {
		return
	}
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_SCHEDULE_ID", "invalid schedule id")
	if !ok {
		return
	}
	run, err := scheduleService.TriggerSchedule(c.Request.Context(), id, time.Now())
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, run)
}

func (h *BenchmarkHandler) requireScheduleService(c *gin.Context) (benchmarkScheduleAdminService, bool) {
	if h.scheduleService == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BENCHMARK_SCHEDULE_SERVICE_UNAVAILABLE", "benchmark schedule service unavailable"))
		return nil, false
	}
	return h.scheduleService, true
}

// ---- request → input mappers ----

func benchmarkTargetInputFromRequest(req benchmarkTargetCreateRequest) service.BenchmarkTargetInput {
	return service.BenchmarkTargetInput{
		ModelName:           req.ModelName,
		ChannelID:           req.ChannelID,
		DisplayName:         req.DisplayName,
		ChannelNameSnapshot: req.ChannelNameSnapshot,
		Enabled:             req.Enabled,
		PublicVisible:       req.PublicVisible,
		SortOrder:           req.SortOrder,
	}
}

func benchmarkTaskInputFromRequest(req benchmarkTaskCreateRequest) service.BenchmarkTaskInput {
	return service.BenchmarkTaskInput{
		Title:          req.Title,
		Type:           req.Type,
		Difficulty:     req.Difficulty,
		Prompt:         req.Prompt,
		InputPayload:   req.InputPayload,
		ExpectedOutput: req.ExpectedOutput,
		VerifierType:   req.VerifierType,
		VerifierConfig: req.VerifierConfig,
		Weight:         req.Weight,
		PublicPrompt:   req.PublicPrompt,
		Enabled:        req.Enabled,
		SortOrder:      req.SortOrder,
	}
}

func benchmarkScheduleInputFromRequest(req benchmarkScheduleCreateRequest) service.BenchmarkScheduleInput {
	return service.BenchmarkScheduleInput{
		Name:      req.Name,
		CronExpr:  req.CronExpr,
		Enabled:   req.Enabled,
		TargetIDs: req.TargetIDs,
		TaskCount: req.TaskCount,
	}
}

// ---- query parsing ----

func parseBenchmarkPathID(c *gin.Context, param, reason, message string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest(reason, message))
		return 0, false
	}
	return id, true
}

func parseBenchmarkListInput(c *gin.Context) service.BenchmarkListInput {
	return service.BenchmarkListInput{
		Page:     benchmarkQueryInt(c, "page"),
		PageSize: benchmarkQueryPageSize(c),
	}
}

func parseBenchmarkTaskListInput(c *gin.Context) (service.BenchmarkTaskListInput, bool) {
	input := service.BenchmarkTaskListInput{
		BenchmarkListInput: parseBenchmarkListInput(c),
		TaskTypes:          benchmarkQueryStringList(c, "task_types"),
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		value, err := strconv.ParseBool(enabled)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ENABLED", "invalid enabled"))
			return service.BenchmarkTaskListInput{}, false
		}
		input.Enabled = &value
	}
	return input, true
}

func parseBenchmarkRunListInput(c *gin.Context) (service.BenchmarkRunListInput, bool) {
	input := service.BenchmarkRunListInput{
		BenchmarkListInput: parseBenchmarkListInput(c),
		Status:             benchmarkQueryStringList(c, "status"),
	}
	return input, true
}

func parseBenchmarkScheduleListInput(c *gin.Context) (service.BenchmarkScheduleListInput, bool) {
	input := service.BenchmarkScheduleListInput{
		BenchmarkListInput: parseBenchmarkListInput(c),
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		value, err := strconv.ParseBool(enabled)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ENABLED", "invalid enabled"))
			return service.BenchmarkScheduleListInput{}, false
		}
		input.Enabled = &value
	}
	return input, true
}

func benchmarkQueryInt(c *gin.Context, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return 0
	}
	return value
}

func benchmarkQueryPageSize(c *gin.Context) int {
	pageSize := benchmarkQueryInt(c, "page_size")
	if pageSize != 0 {
		return pageSize
	}
	return benchmarkQueryInt(c, "limit")
}

func benchmarkQueryStringList(c *gin.Context, key string) []string {
	values := c.QueryArray(key)
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			return nil
		}
		values = strings.Split(raw, ",")
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func writeBenchmarkError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if strings.HasPrefix(err.Error(), "benchmark targets missing:") {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_TARGET_IDS", err.Error()))
		return
	}
	if strings.HasPrefix(err.Error(), "invalid cron expression:") {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	if ent.IsNotFound(err) || infraerrors.IsNotFound(err) {
		response.ErrorFrom(c, infraerrors.NotFound("BENCHMARK_NOT_FOUND", "benchmark resource not found"))
		return
	}
	if isBenchmarkValidationError(err) {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	response.ErrorFrom(c, err)
}

func isBenchmarkValidationError(err error) bool {
	switch err.Error() {
	case "model name is required",
		"channel id must be positive",
		"task title is required",
		"task type is required",
		"task prompt is required",
		"verifier type is required",
		"schedule name is required",
		"cron expr is required",
		"task count must not be negative",
		"no enabled benchmark targets selected",
		"no enabled benchmark tasks available",
		"no enabled standard benchmark tasks available":
		return true
	default:
		return false
	}
}
