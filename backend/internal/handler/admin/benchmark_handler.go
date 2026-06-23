package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type benchmarkAdminService interface {
	ListSuites(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	CreateSuite(ctx context.Context, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	ListTargets(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	ListProfiles(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkProfile, int, error)
	CreateProfile(ctx context.Context, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
	PreviewProfile(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error)
	CreateRun(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
	ListRuns(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	ListScoreSnapshots(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error)
}

type benchmarkAdminSnapshotService interface {
	PublishPublicSnapshot(ctx context.Context, runID int64) error
}

type BenchmarkHandler struct {
	benchmarkService benchmarkAdminService
	snapshotService  benchmarkAdminSnapshotService
}

func NewBenchmarkHandler(benchmarkService *service.BenchmarkService, snapshotService *service.BenchmarkSnapshotService) *BenchmarkHandler {
	return &BenchmarkHandler{
		benchmarkService: benchmarkService,
		snapshotService:  snapshotService,
	}
}

type benchmarkSuiteCreateRequest struct {
	Name             string         `json:"name"`
	Slug             string         `json:"slug"`
	Description      string         `json:"description"`
	Enabled          bool           `json:"enabled"`
	PublicVisible    bool           `json:"public_visible"`
	DefaultProfileID *int64         `json:"default_profile_id"`
	Metadata         map[string]any `json:"metadata"`
}

type benchmarkTargetCreateRequest struct {
	ModelName           string         `json:"model_name"`
	ChannelID           int64          `json:"channel_id"`
	DisplayName         string         `json:"display_name"`
	ProviderSnapshot    string         `json:"provider_snapshot"`
	ChannelNameSnapshot string         `json:"channel_name_snapshot"`
	SupportedTaskTypes  []string       `json:"supported_task_types"`
	MaxConcurrency      int            `json:"max_concurrency"`
	PerRunBudget        *float64       `json:"per_run_budget"`
	DailyBudget         *float64       `json:"daily_budget"`
	Enabled             bool           `json:"enabled"`
	PublicVisible       bool           `json:"public_visible"`
	SortOrder           int            `json:"sort_order"`
	Metadata            map[string]any `json:"metadata"`
}

type benchmarkTaskCreateRequest struct {
	SuiteID        int64          `json:"suite_id"`
	Title          string         `json:"title"`
	Type           string         `json:"type"`
	Category       string         `json:"category"`
	Difficulty     string         `json:"difficulty"`
	Tags           []string       `json:"tags"`
	Prompt         string         `json:"prompt"`
	InputPayload   map[string]any `json:"input_payload"`
	ExpectedOutput map[string]any `json:"expected_output"`
	VerifierType   string         `json:"verifier_type"`
	VerifierConfig map[string]any `json:"verifier_config"`
	Weight         float64        `json:"weight"`
	MinScale       string         `json:"min_scale"`
	PublicPrompt   bool           `json:"public_prompt"`
	Enabled        bool           `json:"enabled"`
	Metadata       map[string]any `json:"metadata"`
}

type benchmarkProfileCreateRequest struct {
	SuiteID          int64          `json:"suite_id"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	TargetIDs        []int64        `json:"target_ids"`
	TaskTypes        []string       `json:"task_types"`
	TaskScale        string         `json:"task_scale"`
	TaskCountLimit   *int           `json:"task_count_limit"`
	PerTypeLimit     map[string]int `json:"per_type_limit"`
	DifficultyFilter []string       `json:"difficulty_filter"`
	TagFilter        []string       `json:"tag_filter"`
	SamplingStrategy string         `json:"sampling_strategy"`
	SelectionSeed    *int64         `json:"selection_seed"`
	RuntimeConfig    map[string]any `json:"runtime_config"`
	ScoringConfig    map[string]any `json:"scoring_config"`
	Metadata         map[string]any `json:"metadata"`
	Enabled          bool           `json:"enabled"`
}

type benchmarkRunCreateRequest struct {
	ProfileID   int64                                  `json:"profile_id"`
	TriggerType string                                 `json:"trigger_type"`
	CreatedBy   *int64                                 `json:"created_by"`
	Override    benchmarkProfilePreviewOverrideRequest `json:"override"`
}

type benchmarkProfilePreviewOverrideRequest struct {
	TargetIDs        []int64        `json:"target_ids"`
	TaskTypes        []string       `json:"task_types"`
	TaskScale        string         `json:"task_scale"`
	TaskCountLimit   *int           `json:"task_count_limit"`
	PerTypeLimit     map[string]int `json:"per_type_limit"`
	DifficultyFilter []string       `json:"difficulty_filter"`
	TagFilter        []string       `json:"tag_filter"`
	SelectionSeed    *int64         `json:"selection_seed"`
}

type benchmarkProfilePreviewResponse struct {
	TargetCount       int      `json:"target_count"`
	TaskCount         int      `json:"task_count"`
	ResultCount       int      `json:"result_count"`
	TaskTypes         []string `json:"task_types"`
	TaskScale         string   `json:"task_scale"`
	RankingBasis      string   `json:"ranking_basis"`
	EstimatedCost     float64  `json:"estimated_cost"`
	SelectedTaskIDs   []int64  `json:"selected_task_ids"`
	SelectedTargetIDs []int64  `json:"selected_target_ids"`
}

func (h *BenchmarkHandler) ListSuites(c *gin.Context) {
	input := service.NormalizeBenchmarkListInput(parseBenchmarkListInput(c))
	items, total, err := h.benchmarkService.ListSuites(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) CreateSuite(c *gin.Context) {
	var req benchmarkSuiteCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	suite, err := h.benchmarkService.CreateSuite(c.Request.Context(), service.BenchmarkSuiteInput{
		Name:             req.Name,
		Slug:             req.Slug,
		Description:      req.Description,
		Enabled:          req.Enabled,
		PublicVisible:    req.PublicVisible,
		DefaultProfileID: req.DefaultProfileID,
		Metadata:         req.Metadata,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, suite)
}

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
	target, err := h.benchmarkService.CreateTarget(c.Request.Context(), service.BenchmarkTargetInput{
		ModelName:           req.ModelName,
		ChannelID:           req.ChannelID,
		DisplayName:         req.DisplayName,
		ProviderSnapshot:    req.ProviderSnapshot,
		ChannelNameSnapshot: req.ChannelNameSnapshot,
		SupportedTaskTypes:  req.SupportedTaskTypes,
		MaxConcurrency:      req.MaxConcurrency,
		PerRunBudget:        req.PerRunBudget,
		DailyBudget:         req.DailyBudget,
		Enabled:             req.Enabled,
		PublicVisible:       req.PublicVisible,
		SortOrder:           req.SortOrder,
		Metadata:            req.Metadata,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, target)
}

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
	task, err := h.benchmarkService.CreateTask(c.Request.Context(), service.BenchmarkTaskInput{
		SuiteID:        req.SuiteID,
		Title:          req.Title,
		Type:           req.Type,
		Category:       req.Category,
		Difficulty:     req.Difficulty,
		Tags:           req.Tags,
		Prompt:         req.Prompt,
		InputPayload:   req.InputPayload,
		ExpectedOutput: req.ExpectedOutput,
		VerifierType:   req.VerifierType,
		VerifierConfig: req.VerifierConfig,
		Weight:         req.Weight,
		MinScale:       req.MinScale,
		PublicPrompt:   req.PublicPrompt,
		Enabled:        req.Enabled,
		Metadata:       req.Metadata,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, task)
}

func (h *BenchmarkHandler) ListProfiles(c *gin.Context) {
	input := service.NormalizeBenchmarkListInput(parseBenchmarkListInput(c))
	items, total, err := h.benchmarkService.ListProfiles(c.Request.Context(), input)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Paginated(c, items, int64(total), input.Page, input.PageSize)
}

func (h *BenchmarkHandler) CreateProfile(c *gin.Context) {
	var req benchmarkProfileCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	profile, err := h.benchmarkService.CreateProfile(c.Request.Context(), service.BenchmarkProfileInput{
		SuiteID:          req.SuiteID,
		Name:             req.Name,
		Description:      req.Description,
		TargetIDs:        req.TargetIDs,
		TaskTypes:        req.TaskTypes,
		TaskScale:        req.TaskScale,
		TaskCountLimit:   req.TaskCountLimit,
		PerTypeLimit:     req.PerTypeLimit,
		DifficultyFilter: req.DifficultyFilter,
		TagFilter:        req.TagFilter,
		SamplingStrategy: req.SamplingStrategy,
		SelectionSeed:    req.SelectionSeed,
		RuntimeConfig:    req.RuntimeConfig,
		ScoringConfig:    req.ScoringConfig,
		Metadata:         req.Metadata,
		Enabled:          req.Enabled,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *BenchmarkHandler) GetProfile(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_PROFILE_ID", "invalid profile id")
	if !ok {
		return
	}
	profile, err := h.benchmarkService.GetProfile(c.Request.Context(), id)
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *BenchmarkHandler) PreviewProfile(c *gin.Context) {
	id, ok := parseBenchmarkPathID(c, "id", "INVALID_PROFILE_ID", "invalid profile id")
	if !ok {
		return
	}
	var req benchmarkProfilePreviewOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	preview, err := h.benchmarkService.PreviewProfile(c.Request.Context(), id, service.BenchmarkProfilePreviewInput{
		TargetIDs:        req.TargetIDs,
		TaskTypes:        req.TaskTypes,
		TaskScale:        req.TaskScale,
		TaskCountLimit:   req.TaskCountLimit,
		PerTypeLimit:     req.PerTypeLimit,
		DifficultyFilter: req.DifficultyFilter,
		TagFilter:        req.TagFilter,
		SelectionSeed:    req.SelectionSeed,
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, benchmarkProfilePreviewResponse{
		TargetCount:       preview.TargetCount,
		TaskCount:         preview.TaskCount,
		ResultCount:       preview.ResultCount,
		TaskTypes:         preview.TaskTypes,
		TaskScale:         preview.TaskScale,
		RankingBasis:      preview.RankingBasis,
		EstimatedCost:     preview.EstimatedCost,
		SelectedTaskIDs:   preview.SelectedTaskIDs,
		SelectedTargetIDs: preview.SelectedTargetIDs,
	})
}

func (h *BenchmarkHandler) CreateRun(c *gin.Context) {
	var req benchmarkRunCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	run, err := h.benchmarkService.CreateRun(c.Request.Context(), service.BenchmarkCreateRunRequest{
		ProfileID:   req.ProfileID,
		TriggerType: req.TriggerType,
		CreatedBy:   req.CreatedBy,
		Override: service.BenchmarkProfilePreviewInput{
			TargetIDs:        req.Override.TargetIDs,
			TaskTypes:        req.Override.TaskTypes,
			TaskScale:        req.Override.TaskScale,
			TaskCountLimit:   req.Override.TaskCountLimit,
			PerTypeLimit:     req.Override.PerTypeLimit,
			DifficultyFilter: req.Override.DifficultyFilter,
			TagFilter:        req.Override.TagFilter,
			SelectionSeed:    req.Override.SelectionSeed,
		},
	})
	if err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, run)
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
	scores, err := h.benchmarkService.ListScoreSnapshots(c.Request.Context(), runID)
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
	if err := h.snapshotService.PublishPublicSnapshot(c.Request.Context(), runID); err != nil {
		writeBenchmarkError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

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
	if suiteID := strings.TrimSpace(c.Query("suite_id")); suiteID != "" {
		id, err := strconv.ParseInt(suiteID, 10, 64)
		if err != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_SUITE_ID", "invalid suite id"))
			return service.BenchmarkTaskListInput{}, false
		}
		input.SuiteID = id
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
	if suiteID := strings.TrimSpace(c.Query("suite_id")); suiteID != "" {
		id, err := strconv.ParseInt(suiteID, 10, 64)
		if err != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_SUITE_ID", "invalid suite id"))
			return service.BenchmarkRunListInput{}, false
		}
		input.SuiteID = id
	}
	if profileID := strings.TrimSpace(c.Query("profile_id")); profileID != "" {
		id, err := strconv.ParseInt(profileID, 10, 64)
		if err != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_PROFILE_ID", "invalid profile id"))
			return service.BenchmarkRunListInput{}, false
		}
		input.ProfileID = id
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
		"task type is required",
		"unsupported task scale",
		"at least one target is required",
		"target id must be positive",
		"at least one task type is required",
		"benchmark profile is disabled",
		"no benchmark tasks selected":
		return true
	default:
		return false
	}
}
