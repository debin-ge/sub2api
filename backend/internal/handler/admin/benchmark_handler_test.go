package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	applogger "github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type benchmarkAdminServiceStub struct {
	listSuitesFn         func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	createSuiteFn        func(ctx context.Context, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	getSuiteFn           func(ctx context.Context, id int64) (*ent.BenchmarkSuite, error)
	updateSuiteFn        func(ctx context.Context, id int64, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	deleteSuiteFn        func(ctx context.Context, id int64) error
	listTargetsFn        func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	createTargetFn       func(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	getTargetFn          func(ctx context.Context, id int64) (*ent.BenchmarkTarget, error)
	updateTargetFn       func(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	deleteTargetFn       func(ctx context.Context, id int64) error
	listTasksFn          func(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	createTaskFn         func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	getTaskFn            func(ctx context.Context, id int64) (*ent.BenchmarkTask, error)
	updateTaskFn         func(ctx context.Context, id int64, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	deleteTaskFn         func(ctx context.Context, id int64) error
	listProfilesFn       func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkProfile, int, error)
	createProfileFn      func(ctx context.Context, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	getProfileFn         func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
	updateProfileFn      func(ctx context.Context, id int64, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	deleteProfileFn      func(ctx context.Context, id int64) error
	previewProfileFn     func(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error)
	createRunFn          func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
	listRunsFn           func(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	getRunFn             func(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	cancelRunFn          func(ctx context.Context, runID int64, reason string) error
	listRunResultsFn     func(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	listScoreSnapshotsFn func(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error)
}

func (s *benchmarkAdminServiceStub) ListSuites(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	return s.listSuitesFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateSuite(ctx context.Context, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	return s.createSuiteFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) GetSuite(ctx context.Context, id int64) (*ent.BenchmarkSuite, error) {
	return s.getSuiteFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) UpdateSuite(ctx context.Context, id int64, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	return s.updateSuiteFn(ctx, id, input)
}

func (s *benchmarkAdminServiceStub) DeleteSuite(ctx context.Context, id int64) error {
	return s.deleteSuiteFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) ListTargets(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	return s.listTargetsFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	return s.createTargetFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) GetTarget(ctx context.Context, id int64) (*ent.BenchmarkTarget, error) {
	return s.getTargetFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) UpdateTarget(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	return s.updateTargetFn(ctx, id, input)
}

func (s *benchmarkAdminServiceStub) DeleteTarget(ctx context.Context, id int64) error {
	return s.deleteTargetFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	return s.listTasksFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	return s.createTaskFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) GetTask(ctx context.Context, id int64) (*ent.BenchmarkTask, error) {
	return s.getTaskFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) UpdateTask(ctx context.Context, id int64, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	return s.updateTaskFn(ctx, id, input)
}

func (s *benchmarkAdminServiceStub) DeleteTask(ctx context.Context, id int64) error {
	return s.deleteTaskFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) ListProfiles(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkProfile, int, error) {
	return s.listProfilesFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateProfile(ctx context.Context, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	return s.createProfileFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error) {
	return s.getProfileFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) UpdateProfile(ctx context.Context, id int64, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
	return s.updateProfileFn(ctx, id, input)
}

func (s *benchmarkAdminServiceStub) DeleteProfile(ctx context.Context, id int64) error {
	return s.deleteProfileFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) PreviewProfile(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error) {
	return s.previewProfileFn(ctx, profileID, override)
}

func (s *benchmarkAdminServiceStub) CreateRun(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	return s.createRunFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) ListRuns(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error) {
	return s.listRunsFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error) {
	return s.getRunFn(ctx, id)
}

func (s *benchmarkAdminServiceStub) CancelRun(ctx context.Context, runID int64, reason string) error {
	return s.cancelRunFn(ctx, runID, reason)
}

func (s *benchmarkAdminServiceStub) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	return s.listRunResultsFn(ctx, runID)
}

func (s *benchmarkAdminServiceStub) ListScoreSnapshots(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
	return s.listScoreSnapshotsFn(ctx, runID)
}

type benchmarkSnapshotServiceStub struct {
	publishPublicSnapshotFn func(ctx context.Context, runID int64) error
}

func (s *benchmarkSnapshotServiceStub) PublishPublicSnapshot(ctx context.Context, runID int64) error {
	return s.publishPublicSnapshotFn(ctx, runID)
}

type benchmarkScheduleAdminServiceStub struct {
	listSchedulesFn   func(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error)
	createScheduleFn  func(ctx context.Context, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	getScheduleFn     func(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error)
	updateScheduleFn  func(ctx context.Context, id int64, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	deleteScheduleFn  func(ctx context.Context, id int64) error
	triggerScheduleFn func(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error)
}

func (s *benchmarkScheduleAdminServiceStub) ListSchedules(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error) {
	return s.listSchedulesFn(ctx, input)
}

func (s *benchmarkScheduleAdminServiceStub) CreateSchedule(ctx context.Context, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
	return s.createScheduleFn(ctx, input)
}

func (s *benchmarkScheduleAdminServiceStub) GetSchedule(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error) {
	return s.getScheduleFn(ctx, id)
}

func (s *benchmarkScheduleAdminServiceStub) UpdateSchedule(ctx context.Context, id int64, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
	return s.updateScheduleFn(ctx, id, input)
}

func (s *benchmarkScheduleAdminServiceStub) DeleteSchedule(ctx context.Context, id int64) error {
	return s.deleteScheduleFn(ctx, id)
}

func (s *benchmarkScheduleAdminServiceStub) TriggerSchedule(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error) {
	return s.triggerScheduleFn(ctx, id, now)
}

type benchmarkAdminProcessorStub struct {
	processRunFn func(ctx context.Context, runID int64) (int, error)
	processDueFn func(ctx context.Context, options service.BenchmarkProcessOptions) (int, error)
}

func (s *benchmarkAdminProcessorStub) ProcessRun(ctx context.Context, runID int64) (int, error) {
	return s.processRunFn(ctx, runID)
}

func (s *benchmarkAdminProcessorStub) ProcessDue(ctx context.Context, options service.BenchmarkProcessOptions) (int, error) {
	return s.processDueFn(ctx, options)
}

type benchmarkHTTPResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

type benchmarkPaginatedResponse struct {
	Items    json.RawMessage `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Pages    int             `json:"pages"`
}

type benchmarkLogSink struct {
	events chan *applogger.LogEvent
}

func (s *benchmarkLogSink) WriteLogEvent(event *applogger.LogEvent) {
	select {
	case s.events <- event:
	default:
	}
}

func newBenchmarkLogSink(t *testing.T) *benchmarkLogSink {
	t.Helper()
	require.NoError(t, applogger.Init(applogger.InitOptions{
		Level:           "debug",
		Format:          "json",
		StacktraceLevel: "none",
		Output: applogger.OutputOptions{
			ToStdout: false,
			ToFile:   true,
			FilePath: filepath.Join(t.TempDir(), "benchmark-handler.log"),
		},
	}))
	sink := &benchmarkLogSink{events: make(chan *applogger.LogEvent, 8)}
	applogger.SetSink(sink)
	t.Cleanup(func() {
		applogger.SetSink(nil)
	})
	return sink
}

func (s *benchmarkLogSink) waitForMessage(t *testing.T, message string) *applogger.LogEvent {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-s.events:
			if event.Message == message {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for log message %q", message)
		}
	}
}

func newBenchmarkTestRouter(handler *BenchmarkHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/v1/admin/benchmark/suites", handler.ListSuites)
	router.POST("/api/v1/admin/benchmark/suites", handler.CreateSuite)
	router.GET("/api/v1/admin/benchmark/suites/:id", handler.GetSuite)
	router.PUT("/api/v1/admin/benchmark/suites/:id", handler.UpdateSuite)
	router.DELETE("/api/v1/admin/benchmark/suites/:id", handler.DeleteSuite)
	router.GET("/api/v1/admin/benchmark/targets", handler.ListTargets)
	router.POST("/api/v1/admin/benchmark/targets", handler.CreateTarget)
	router.GET("/api/v1/admin/benchmark/targets/:id", handler.GetTarget)
	router.PUT("/api/v1/admin/benchmark/targets/:id", handler.UpdateTarget)
	router.DELETE("/api/v1/admin/benchmark/targets/:id", handler.DeleteTarget)
	router.GET("/api/v1/admin/benchmark/tasks", handler.ListTasks)
	router.POST("/api/v1/admin/benchmark/tasks", handler.CreateTask)
	router.GET("/api/v1/admin/benchmark/tasks/:id", handler.GetTask)
	router.PUT("/api/v1/admin/benchmark/tasks/:id", handler.UpdateTask)
	router.DELETE("/api/v1/admin/benchmark/tasks/:id", handler.DeleteTask)
	router.GET("/api/v1/admin/benchmark/profiles", handler.ListProfiles)
	router.POST("/api/v1/admin/benchmark/profiles", handler.CreateProfile)
	router.GET("/api/v1/admin/benchmark/profiles/:id", handler.GetProfile)
	router.PUT("/api/v1/admin/benchmark/profiles/:id", handler.UpdateProfile)
	router.DELETE("/api/v1/admin/benchmark/profiles/:id", handler.DeleteProfile)
	router.POST("/api/v1/admin/benchmark/profiles/:id/preview", handler.PreviewProfile)
	router.POST("/api/v1/admin/benchmark/runs", handler.CreateRun)
	router.GET("/api/v1/admin/benchmark/runs", handler.ListRuns)
	router.POST("/api/v1/admin/benchmark/runs/process-due", handler.ProcessDueRuns)
	router.GET("/api/v1/admin/benchmark/runs/:id", handler.GetRun)
	router.GET("/api/v1/admin/benchmark/runs/:id/results", handler.ListRunResults)
	router.GET("/api/v1/admin/benchmark/runs/:id/scores", handler.ListRunScores)
	router.POST("/api/v1/admin/benchmark/runs/:id/process", handler.ProcessRun)
	router.POST("/api/v1/admin/benchmark/runs/:id/publish", handler.PublishRun)
	router.POST("/api/v1/admin/benchmark/runs/:id/cancel", handler.CancelRun)
	router.GET("/api/v1/admin/benchmark/schedules", handler.ListSchedules)
	router.POST("/api/v1/admin/benchmark/schedules", handler.CreateSchedule)
	router.GET("/api/v1/admin/benchmark/schedules/:id", handler.GetSchedule)
	router.PUT("/api/v1/admin/benchmark/schedules/:id", handler.UpdateSchedule)
	router.DELETE("/api/v1/admin/benchmark/schedules/:id", handler.DeleteSchedule)
	router.POST("/api/v1/admin/benchmark/schedules/:id/trigger", handler.TriggerSchedule)

	return router
}

func TestBenchmarkHandlerCreateTargetSuccess(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTargetFn: func(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
			require.Equal(t, "gpt-4.1", input.ModelName)
			require.Equal(t, int64(7), input.ChannelID)
			return &ent.BenchmarkTarget{ID: 11, ModelName: input.ModelName, ChannelID: input.ChannelID}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"model_name":"gpt-4.1","channel_id":7}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/targets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	var target ent.BenchmarkTarget
	require.NoError(t, json.Unmarshal(resp.Data, &target))
	require.Equal(t, int64(11), target.ID)
}

func TestBenchmarkHandlerCreateTargetEmptyModelReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTargetFn: func(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
			return nil, errors.New("model name is required")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"model_name":"","channel_id":7}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/targets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}

func TestBenchmarkHandlerListTargetsPagination(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		listTargetsFn: func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
			require.Equal(t, service.BenchmarkListInput{Page: 1, PageSize: 100}, input)
			return []*ent.BenchmarkTarget{
				{ID: 1, ModelName: "a"},
				{ID: 2, ModelName: "b"},
			}, 2, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/targets?page=0&page_size=999", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var page benchmarkPaginatedResponse
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 100, page.PageSize)
}

func TestBenchmarkHandlerUpdateTargetMapsRequest(t *testing.T) {
	perRunBudget := 1.5
	dailyBudget := 9.25
	svc := &benchmarkAdminServiceStub{
		updateTargetFn: func(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
			require.Equal(t, int64(11), id)
			require.Equal(t, "gpt-4.1-mini", input.ModelName)
			require.Equal(t, int64(7), input.ChannelID)
			require.Equal(t, "GPT Mini", input.DisplayName)
			require.Equal(t, "openai", input.ProviderSnapshot)
			require.Equal(t, "primary", input.ChannelNameSnapshot)
			require.Equal(t, []string{"reasoning", "coding"}, input.SupportedTaskTypes)
			require.Equal(t, 3, input.MaxConcurrency)
			require.Equal(t, &perRunBudget, input.PerRunBudget)
			require.Equal(t, &dailyBudget, input.DailyBudget)
			require.True(t, input.Enabled)
			require.False(t, input.PublicVisible)
			require.Equal(t, 4, input.SortOrder)
			require.Equal(t, map[string]any{"tier": "fast"}, input.Metadata)
			return &ent.BenchmarkTarget{ID: id, ModelName: input.ModelName, ChannelID: input.ChannelID}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"model_name":"gpt-4.1-mini","channel_id":7,"display_name":"GPT Mini","provider_snapshot":"openai","channel_name_snapshot":"primary","supported_task_types":["reasoning","coding"],"max_concurrency":3,"per_run_budget":1.5,"daily_budget":9.25,"enabled":true,"public_visible":false,"sort_order":4,"metadata":{"tier":"fast"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/benchmark/targets/11", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateTaskSuccess(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTaskFn: func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
			require.Equal(t, "reasoning", input.Type)
			require.Equal(t, service.BenchmarkTaskScaleSmall, input.MinScale)
			return &ent.BenchmarkTask{ID: 21, Type: input.Type}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"suite_id":1,"title":"task","type":"reasoning","prompt":"p","verifier_type":"exact_match","min_scale":"small"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateTaskInvalidScaleReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTaskFn: func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
			return nil, errors.New("unsupported task scale")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"suite_id":1,"title":"task","type":"reasoning","prompt":"p","verifier_type":"exact_match","min_scale":"giant"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}

func TestBenchmarkHandlerPreviewProfileReturnsCountsAndRankingBasis(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		previewProfileFn: func(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error) {
			require.Equal(t, int64(7), profileID)
			return &service.BenchmarkProfilePreview{
				TargetCount:       2,
				TaskCount:         3,
				ResultCount:       6,
				TaskTypes:         []string{"reasoning", "coding"},
				TaskScale:         service.BenchmarkTaskScaleMedium,
				RankingBasis:      "ability_score_only",
				EstimatedCost:     1.25,
				SelectedTaskIDs:   []int64{101, 102, 103},
				SelectedTargetIDs: []int64{201, 202},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/profiles/7/preview", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var preview struct {
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
	require.NoError(t, json.Unmarshal(resp.Data, &preview))
	require.Equal(t, 2, preview.TargetCount)
	require.Equal(t, 3, preview.TaskCount)
	require.Equal(t, 6, preview.ResultCount)
	require.Equal(t, []string{"reasoning", "coding"}, preview.TaskTypes)
	require.Equal(t, service.BenchmarkTaskScaleMedium, preview.TaskScale)
	require.Equal(t, "ability_score_only", preview.RankingBasis)
	require.InDelta(t, 1.25, preview.EstimatedCost, 0.000001)
	require.Equal(t, []int64{101, 102, 103}, preview.SelectedTaskIDs)
	require.Equal(t, []int64{201, 202}, preview.SelectedTargetIDs)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &raw))
	require.Contains(t, raw, "target_count")
	require.Contains(t, raw, "task_count")
	require.Contains(t, raw, "result_count")
	require.Contains(t, raw, "task_types")
	require.Contains(t, raw, "task_scale")
	require.Contains(t, raw, "ranking_basis")
	require.Contains(t, raw, "estimated_cost")
	require.Contains(t, raw, "selected_task_ids")
	require.Contains(t, raw, "selected_target_ids")
	require.NotContains(t, raw, "TargetCount")
	require.NotContains(t, raw, "TaskCount")
	require.NotContains(t, raw, "ResultCount")
	require.NotContains(t, raw, "RankingBasis")
}

func TestBenchmarkHandlerPreviewProfileMissingOverrideTargetsReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		previewProfileFn: func(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error) {
			require.Equal(t, int64(7), profileID)
			require.Equal(t, []int64{999}, override.TargetIDs)
			return nil, errors.New("benchmark targets missing: [999]")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/profiles/7/preview", bytes.NewBufferString(`{"target_ids":[999]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_TARGET_IDS")
}

func TestBenchmarkHandlerUpdateProfileMapsRequest(t *testing.T) {
	taskCountLimit := 8
	selectionSeed := int64(12345)
	svc := &benchmarkAdminServiceStub{
		updateProfileFn: func(ctx context.Context, id int64, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error) {
			require.Equal(t, int64(7), id)
			require.Equal(t, int64(2), input.SuiteID)
			require.Equal(t, "smoke profile", input.Name)
			require.Equal(t, "profile for smoke benchmark", input.Description)
			require.Equal(t, []int64{11, 12}, input.TargetIDs)
			require.Equal(t, []string{"reasoning", "coding"}, input.TaskTypes)
			require.Equal(t, service.BenchmarkTaskScaleCustom, input.TaskScale)
			require.Equal(t, &taskCountLimit, input.TaskCountLimit)
			require.Equal(t, map[string]int{"reasoning": 3, "coding": 5}, input.PerTypeLimit)
			require.Equal(t, []string{"easy", "medium"}, input.DifficultyFilter)
			require.Equal(t, []string{"math", "code"}, input.TagFilter)
			require.Equal(t, "weighted", input.SamplingStrategy)
			require.Equal(t, &selectionSeed, input.SelectionSeed)
			require.Equal(t, map[string]any{"timeout_ms": float64(30000), "temperature": 0.2}, input.RuntimeConfig)
			require.Equal(t, map[string]any{"ranking_basis": "ability_score_only", "min_coverage": 0.8}, input.ScoringConfig)
			require.Equal(t, map[string]any{"owner": "admin", "priority": "high"}, input.Metadata)
			require.True(t, input.Enabled)
			return &ent.BenchmarkProfile{ID: id, SuiteID: input.SuiteID, Name: input.Name, Enabled: input.Enabled}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"suite_id":2,"name":"smoke profile","description":"profile for smoke benchmark","target_ids":[11,12],"task_types":["reasoning","coding"],"task_scale":"custom","task_count_limit":8,"per_type_limit":{"reasoning":3,"coding":5},"difficulty_filter":["easy","medium"],"tag_filter":["math","code"],"sampling_strategy":"weighted","selection_seed":12345,"runtime_config":{"timeout_ms":30000,"temperature":0.2},"scoring_config":{"ranking_basis":"ability_score_only","min_coverage":0.8},"metadata":{"owner":"admin","priority":"high"},"enabled":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/benchmark/profiles/7", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateRun(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(7), input.ProfileID)
			require.Equal(t, "manual", input.TriggerType)
			return &ent.BenchmarkRun{ID: 31, ProfileID: input.ProfileID}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"trigger_type":"manual"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateRunProcessImmediatelyRequiresProcessor(t *testing.T) {
	createCalled := false
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			createCalled = true
			return &ent.BenchmarkRun{ID: 31, ProfileID: input.ProfileID}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"process_immediately":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "BENCHMARK_PROCESSOR_UNAVAILABLE")
	require.False(t, createCalled)
}

func TestBenchmarkHandlerCreateRunProcessImmediatelyTriggersProcessor(t *testing.T) {
	processed := make(chan int64, 1)
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(7), input.ProfileID)
			return &ent.BenchmarkRun{ID: 31, ProfileID: input.ProfileID}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			return 1, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: svc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"process_immediately":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case runID := <-processed:
		require.Equal(t, int64(31), runID)
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}
}

func TestBenchmarkHandlerCreateRunProcessImmediatelyLogsProcessorError(t *testing.T) {
	sink := newBenchmarkLogSink(t)
	processed := make(chan int64, 1)
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			return &ent.BenchmarkRun{ID: 31, ProfileID: input.ProfileID}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			return 0, errors.New("processor failed")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: svc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"process_immediately":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case runID := <-processed:
		require.Equal(t, int64(31), runID)
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}
	event := sink.waitForMessage(t, "benchmark.process_immediately_failed")
	require.EqualValues(t, 31, event.Fields["run_id"])
	require.Contains(t, event.Fields["error"], "processor failed")
}

func TestBenchmarkHandlerCreateRunProcessImmediatelyRecoversProcessorPanic(t *testing.T) {
	sink := newBenchmarkLogSink(t)
	processed := make(chan int64, 1)
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			return &ent.BenchmarkRun{ID: 31, ProfileID: input.ProfileID}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			panic("processor panic")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: svc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"process_immediately":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case runID := <-processed:
		require.Equal(t, int64(31), runID)
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}
	event := sink.waitForMessage(t, "benchmark.process_immediately_panic_recovered")
	require.EqualValues(t, 31, event.Fields["run_id"])
	require.Equal(t, "processor panic", event.Fields["panic"])
}

func TestBenchmarkHandlerCreateRunReturns403WhenBenchmarkDisabled(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(7), input.ProfileID)
			return nil, infraerrors.Forbidden("BENCHMARK_DISABLED", "benchmark is disabled")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "BENCHMARK_DISABLED")
}

func TestBenchmarkHandlerCreateRunMissingOverrideTargetsReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(7), input.ProfileID)
			require.Equal(t, []int64{999}, input.Override.TargetIDs)
			return nil, errors.New("benchmark targets missing: [999]")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"profile_id":7,"override":{"target_ids":[999]}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_TARGET_IDS")
}

func TestBenchmarkHandlerListRunResults(t *testing.T) {
	displayName := "Run Snapshot Model"
	svc := &benchmarkAdminServiceStub{
		listRunResultsFn: func(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
			require.Equal(t, int64(41), runID)
			return []*ent.BenchmarkResult{
				{
					ID:          1,
					RunTargetID: 501,
					Edges: ent.BenchmarkResultEdges{
						RunTarget: &ent.BenchmarkRunTarget{
							ID:                  501,
							TargetID:            11,
							ModelName:           "gpt-4.1",
							ChannelID:           7,
							DisplayNameSnapshot: &displayName,
						},
					},
				},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/runs/41/results", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var results []map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &results))
	require.Len(t, results, 1)
	edges, ok := results[0]["edges"].(map[string]any)
	require.True(t, ok)
	runTarget, ok := edges["run_target"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 501, runTarget["id"])
	require.EqualValues(t, 11, runTarget["target_id"])
	require.Equal(t, "Run Snapshot Model", runTarget["display_name_snapshot"])
}

func TestBenchmarkHandlerListRunScores(t *testing.T) {
	displayName := "Run Snapshot Score Model"
	svc := &benchmarkAdminServiceStub{
		listScoreSnapshotsFn: func(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
			require.Equal(t, int64(42), runID)
			return []*ent.BenchmarkScoreSnapshot{
				{
					ID:          1,
					RunTargetID: 601,
					Edges: ent.BenchmarkScoreSnapshotEdges{
						RunTarget: &ent.BenchmarkRunTarget{
							ID:                  601,
							TargetID:            12,
							ModelName:           "claude-3-5-sonnet",
							ChannelID:           8,
							DisplayNameSnapshot: &displayName,
						},
					},
				},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/runs/42/scores", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var scores []map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &scores))
	require.Len(t, scores, 1)
	edges, ok := scores[0]["edges"].(map[string]any)
	require.True(t, ok)
	runTarget, ok := edges["run_target"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 601, runTarget["id"])
	require.EqualValues(t, 12, runTarget["target_id"])
	require.Equal(t, "Run Snapshot Score Model", runTarget["display_name_snapshot"])
}

func TestBenchmarkHandlerProcessRun(t *testing.T) {
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			require.Equal(t, int64(41), runID)
			return 3, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/41/process", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var data struct {
		Processed int `json:"processed"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	require.Equal(t, 3, data.Processed)
}

func TestBenchmarkHandlerSetNilProcessorRequiresProcessor(t *testing.T) {
	handler := &BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		snapshotService:  &benchmarkSnapshotServiceStub{},
	}
	handler.SetProcessor(nil)
	router := newBenchmarkTestRouter(handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/41/process", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "BENCHMARK_PROCESSOR_UNAVAILABLE")
}

func TestBenchmarkHandlerProcessDueRunsMapsLimit(t *testing.T) {
	processor := &benchmarkAdminProcessorStub{
		processDueFn: func(ctx context.Context, options service.BenchmarkProcessOptions) (int, error) {
			require.Equal(t, 8, options.RunLimit)
			return 5, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/process-due", bytes.NewBufferString(`{"limit":8}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var data struct {
		Processed int `json:"processed"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	require.Equal(t, 5, data.Processed)
}

func TestBenchmarkHandlerPublishRun(t *testing.T) {
	called := false
	snapshotSvc := &benchmarkSnapshotServiceStub{
		publishPublicSnapshotFn: func(ctx context.Context, runID int64) error {
			require.Equal(t, int64(43), runID)
			called = true
			return nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: &benchmarkAdminServiceStub{}, snapshotService: snapshotSvc})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/43/publish", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestBenchmarkHandlerPublishIncompleteRunPropagatesConflict(t *testing.T) {
	snapshotSvc := &benchmarkSnapshotServiceStub{
		publishPublicSnapshotFn: func(ctx context.Context, runID int64) error {
			require.Equal(t, int64(43), runID)
			return infraerrors.Conflict("BENCHMARK_RUN_INCOMPLETE", "benchmark run is incomplete")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: &benchmarkAdminServiceStub{}, snapshotService: snapshotSvc})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/43/publish", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "BENCHMARK_RUN_INCOMPLETE")
}

func TestBenchmarkHandlerCancelRunWithReason(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		cancelRunFn: func(ctx context.Context, runID int64, reason string) error {
			require.Equal(t, int64(44), runID)
			require.Equal(t, "operator stop", reason)
			return nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/44/cancel", bytes.NewBufferString(`{"reason":"operator stop"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerListSchedules(t *testing.T) {
	enabled := true
	scheduleSvc := &benchmarkScheduleAdminServiceStub{
		listSchedulesFn: func(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error) {
			require.Equal(t, service.BenchmarkScheduleListInput{
				BenchmarkListInput: service.BenchmarkListInput{Page: 1, PageSize: 100},
				ProfileID:          7,
				Enabled:            &enabled,
			}, input)
			return []*ent.BenchmarkSchedule{{ID: 1, ProfileID: 7, Name: "nightly"}}, 1, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/schedules?page=0&page_size=999&profile_id=7&enabled=true", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var page benchmarkPaginatedResponse
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 100, page.PageSize)
}

func TestBenchmarkHandlerCreateSchedule(t *testing.T) {
	scheduleSvc := &benchmarkScheduleAdminServiceStub{
		createScheduleFn: func(ctx context.Context, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
			require.Equal(t, int64(7), input.ProfileID)
			require.Equal(t, "nightly", input.Name)
			require.Equal(t, "0 * * * *", input.CronExpr)
			require.True(t, input.Enabled)
			require.Equal(t, map[string]any{"scope": "daily"}, input.Metadata)
			require.Nil(t, input.NextRunAt)
			return &ent.BenchmarkSchedule{
				ID:        12,
				ProfileID: input.ProfileID,
				Name:      input.Name,
				CronExpr:  input.CronExpr,
				Enabled:   input.Enabled,
				Metadata:  input.Metadata,
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	body := `{"profile_id":7,"name":"nightly","cron_expr":"0 * * * *","enabled":true,"metadata":{"scope":"daily"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var schedule ent.BenchmarkSchedule
	require.NoError(t, json.Unmarshal(resp.Data, &schedule))
	require.Equal(t, int64(12), schedule.ID)
}

func TestBenchmarkHandlerUpdateScheduleMapsRequest(t *testing.T) {
	called := false
	scheduleSvc := &benchmarkScheduleAdminServiceStub{
		updateScheduleFn: func(ctx context.Context, id int64, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
			called = true
			require.Equal(t, int64(12), id)
			require.Equal(t, int64(7), input.ProfileID)
			require.Equal(t, "weekday smoke", input.Name)
			require.Equal(t, "15 2 * * 1-5", input.CronExpr)
			require.False(t, input.Enabled)
			require.Equal(t, map[string]any{"scope": "weekday", "batch": "smoke"}, input.Metadata)
			require.Nil(t, input.NextRunAt)
			return &ent.BenchmarkSchedule{
				ID:        id,
				ProfileID: input.ProfileID,
				Name:      input.Name,
				CronExpr:  input.CronExpr,
				Enabled:   input.Enabled,
				Metadata:  input.Metadata,
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	body := `{"profile_id":7,"name":"weekday smoke","cron_expr":"15 2 * * 1-5","enabled":false,"metadata":{"scope":"weekday","batch":"smoke"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/benchmark/schedules/12", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestBenchmarkHandlerTriggerSchedule(t *testing.T) {
	scheduleSvc := &benchmarkScheduleAdminServiceStub{
		triggerScheduleFn: func(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error) {
			require.Equal(t, int64(12), id)
			require.False(t, now.IsZero())
			return &ent.BenchmarkRun{ID: 52, ProfileID: 7, TriggerType: "scheduled"}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/schedules/12/trigger", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var run ent.BenchmarkRun
	require.NoError(t, json.Unmarshal(resp.Data, &run))
	require.Equal(t, int64(52), run.ID)
}

func TestBenchmarkHandlerScheduleServiceUnavailableDoesNotPanic(t *testing.T) {
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/admin/benchmark/schedules"},
		{method: http.MethodPost, path: "/api/v1/admin/benchmark/schedules", body: `{"profile_id":7,"name":"nightly","cron_expr":"0 * * * *"}`},
		{method: http.MethodGet, path: "/api/v1/admin/benchmark/schedules/12"},
		{method: http.MethodPut, path: "/api/v1/admin/benchmark/schedules/12", body: `{"profile_id":7,"name":"nightly","cron_expr":"0 * * * *"}`},
		{method: http.MethodDelete, path: "/api/v1/admin/benchmark/schedules/12"},
		{method: http.MethodPost, path: "/api/v1/admin/benchmark/schedules/12/trigger"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			require.NotPanics(t, func() {
				router.ServeHTTP(rec, req)
			})
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Contains(t, rec.Body.String(), "BENCHMARK_SCHEDULE_SERVICE_UNAVAILABLE")
		})
	}
}
