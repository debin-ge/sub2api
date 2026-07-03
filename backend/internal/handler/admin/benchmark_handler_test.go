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
	listTargetsFn         func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	createTargetFn        func(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	getTargetFn           func(ctx context.Context, id int64) (*ent.BenchmarkTarget, error)
	updateTargetFn        func(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	deleteTargetFn        func(ctx context.Context, id int64) error
	listTasksFn           func(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	createTaskFn          func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	getTaskFn             func(ctx context.Context, id int64) (*ent.BenchmarkTask, error)
	updateTaskFn          func(ctx context.Context, id int64, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	deleteTaskFn          func(ctx context.Context, id int64) error
	ensureStandardTasksFn func(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error)
	previewRunFn          func(ctx context.Context, targetIDs []int64, taskCount int) (*service.BenchmarkRunPreview, error)
	createRunFn           func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
	createStandardRunFn   func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error)
	listRunsFn            func(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	getRunFn              func(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	cancelRunFn           func(ctx context.Context, runID int64, reason string) error
	listRunTargetsFn      func(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error)
	listRunTasksFn        func(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error)
	listRunResultsFn      func(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	listTargetScoresFn    func(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error)
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

func (s *benchmarkAdminServiceStub) EnsureStandardTasks(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error) {
	return s.ensureStandardTasksFn(ctx)
}

func (s *benchmarkAdminServiceStub) PreviewRun(ctx context.Context, targetIDs []int64, taskCount int) (*service.BenchmarkRunPreview, error) {
	return s.previewRunFn(ctx, targetIDs, taskCount)
}

func (s *benchmarkAdminServiceStub) CreateRun(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
	return s.createRunFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateStandardRun(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
	return s.createStandardRunFn(ctx, input)
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

func (s *benchmarkAdminServiceStub) ListRunTargets(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error) {
	return s.listRunTargetsFn(ctx, runID)
}

func (s *benchmarkAdminServiceStub) ListRunTasks(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error) {
	return s.listRunTasksFn(ctx, runID)
}

func (s *benchmarkAdminServiceStub) ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
	return s.listRunResultsFn(ctx, runID)
}

func (s *benchmarkAdminServiceStub) ListTargetScores(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error) {
	return s.listTargetScoresFn(ctx, runID)
}

type benchmarkSnapshotServiceStub struct {
	publishPublicSnapshotFn func(ctx context.Context, runID int64) error
	getTrendsFn             func(ctx context.Context, days int, limit int) ([]service.BenchmarkPublicTrend, error)
}

func (s *benchmarkSnapshotServiceStub) PublishPublicSnapshot(ctx context.Context, runID int64) error {
	if s.publishPublicSnapshotFn == nil {
		return nil
	}
	return s.publishPublicSnapshotFn(ctx, runID)
}

func (s *benchmarkSnapshotServiceStub) GetTrends(ctx context.Context, days int, limit int) ([]service.BenchmarkPublicTrend, error) {
	if s.getTrendsFn == nil {
		return nil, nil
	}
	return s.getTrendsFn(ctx, days, limit)
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
	if s.processRunFn == nil {
		return 0, nil
	}
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

	router.GET("/api/v1/admin/benchmark/targets", handler.ListTargets)
	router.POST("/api/v1/admin/benchmark/targets", handler.CreateTarget)
	router.GET("/api/v1/admin/benchmark/targets/:id", handler.GetTarget)
	router.PUT("/api/v1/admin/benchmark/targets/:id", handler.UpdateTarget)
	router.DELETE("/api/v1/admin/benchmark/targets/:id", handler.DeleteTarget)
	router.GET("/api/v1/admin/benchmark/tasks", handler.ListTasks)
	router.POST("/api/v1/admin/benchmark/tasks/standard/apply", handler.ApplyStandardTasks)
	router.POST("/api/v1/admin/benchmark/tasks", handler.CreateTask)
	router.GET("/api/v1/admin/benchmark/tasks/:id", handler.GetTask)
	router.PUT("/api/v1/admin/benchmark/tasks/:id", handler.UpdateTask)
	router.DELETE("/api/v1/admin/benchmark/tasks/:id", handler.DeleteTask)
	router.POST("/api/v1/admin/benchmark/runs/preview", handler.PreviewRun)
	router.POST("/api/v1/admin/benchmark/runs/standard", handler.CreateStandardRun)
	router.POST("/api/v1/admin/benchmark/runs", handler.CreateRun)
	router.GET("/api/v1/admin/benchmark/runs", handler.ListRuns)
	router.POST("/api/v1/admin/benchmark/runs/process-due", handler.ProcessDueRuns)
	router.GET("/api/v1/admin/benchmark/runs/:id", handler.GetRun)
	router.GET("/api/v1/admin/benchmark/runs/:id/detail", handler.GetRunDetail)
	router.GET("/api/v1/admin/benchmark/runs/:id/results", handler.ListRunResults)
	router.GET("/api/v1/admin/benchmark/runs/:id/scores", handler.ListRunScores)
	router.POST("/api/v1/admin/benchmark/runs/:id/process", handler.ProcessRun)
	router.POST("/api/v1/admin/benchmark/runs/:id/publish", handler.PublishRun)
	router.POST("/api/v1/admin/benchmark/runs/:id/cancel", handler.CancelRun)
	router.GET("/api/v1/admin/benchmark/trends", handler.GetTrends)
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
	svc := &benchmarkAdminServiceStub{
		updateTargetFn: func(ctx context.Context, id int64, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
			require.Equal(t, int64(11), id)
			require.Equal(t, "gpt-4.1-mini", input.ModelName)
			require.Equal(t, int64(7), input.ChannelID)
			require.Equal(t, "GPT Mini", input.DisplayName)
			require.Equal(t, "primary", input.ChannelNameSnapshot)
			require.True(t, input.Enabled)
			require.False(t, input.PublicVisible)
			require.Equal(t, 4, input.SortOrder)
			return &ent.BenchmarkTarget{ID: id, ModelName: input.ModelName, ChannelID: input.ChannelID}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"model_name":"gpt-4.1-mini","channel_id":7,"display_name":"GPT Mini","channel_name_snapshot":"primary","enabled":true,"public_visible":false,"sort_order":4}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/benchmark/targets/11", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateTaskSuccess(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTaskFn: func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
			require.Equal(t, "task", input.Title)
			require.Equal(t, "reasoning", input.Type)
			require.Equal(t, "easy", input.Difficulty)
			require.Equal(t, "p", input.Prompt)
			require.Equal(t, map[string]any{"answer": "42"}, input.ExpectedOutput)
			require.Equal(t, "exact_match", input.VerifierType)
			require.True(t, input.PublicPrompt)
			require.True(t, input.Enabled)
			require.Equal(t, 3, input.SortOrder)
			return &ent.BenchmarkTask{ID: 21, Type: input.Type}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"title":"task","type":"reasoning","difficulty":"easy","prompt":"p","expected_output":{"answer":"42"},"verifier_type":"exact_match","public_prompt":true,"enabled":true,"sort_order":3}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateTaskEmptyTitleReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createTaskFn: func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
			return nil, errors.New("task title is required")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	body := `{"title":"","type":"reasoning","prompt":"p","verifier_type":"exact_match"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}

func TestBenchmarkHandlerApplyStandardTasks(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		ensureStandardTasksFn: func(ctx context.Context) (*service.BenchmarkStandardTaskApplyResult, error) {
			return &service.BenchmarkStandardTaskApplyResult{
				CreatedCount:  2,
				ExistingCount: 4,
				EnabledCount:  6,
				Tasks: []*ent.BenchmarkTask{
					{ID: 1, Title: "Standard Reasoning - Arithmetic"},
				},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/tasks/standard/apply", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"created_count":2`)
	require.Contains(t, rec.Body.String(), `"existing_count":4`)
	require.Contains(t, rec.Body.String(), `"enabled_count":6`)
}

func TestBenchmarkHandlerPreviewRun(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		previewRunFn: func(ctx context.Context, targetIDs []int64, taskCount int) (*service.BenchmarkRunPreview, error) {
			require.Equal(t, []int64{11, 12}, targetIDs)
			require.Equal(t, 3, taskCount)
			return &service.BenchmarkRunPreview{
				TargetCount:  2,
				TaskCount:    3,
				ResultCount:  6,
				RankingBasis: "fixed_task_set",
				TargetIDs:    []int64{11, 12},
				TaskIDs:      []int64{101, 102, 103},
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/preview", bytes.NewBufferString(`{"target_ids":[11,12],"task_count":3}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var preview service.BenchmarkRunPreview
	require.NoError(t, json.Unmarshal(resp.Data, &preview))
	require.Equal(t, 2, preview.TargetCount)
	require.Equal(t, 3, preview.TaskCount)
	require.Equal(t, 6, preview.ResultCount)
}

func TestBenchmarkHandlerCreateRun(t *testing.T) {
	createdBy := int64(99)
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, []int64{11, 12}, input.TargetIDs)
			require.Equal(t, 4, input.TaskCount)
			require.Equal(t, service.BenchmarkTriggerManual, input.TriggerType)
			require.Equal(t, &createdBy, input.CreatedBy)
			return &ent.BenchmarkRun{ID: 31, TriggerType: input.TriggerType, TaskCount: input.TaskCount, CreatedBy: input.CreatedBy}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[11,12],"task_count":4,"trigger_type":"manual","created_by":99}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateRunProcessImmediatelyRequiresProcessor(t *testing.T) {
	createCalled := false
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			createCalled = true
			return &ent.BenchmarkRun{ID: 31}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[7],"task_count":2,"process_immediately":true}`))
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
			require.Equal(t, []int64{7}, input.TargetIDs)
			return &ent.BenchmarkRun{ID: 31}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			return 0, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: svc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
		processor:        processor,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[7],"process_immediately":true}`))
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
			return &ent.BenchmarkRun{ID: 31}, nil
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[7],"process_immediately":true}`))
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
			return &ent.BenchmarkRun{ID: 31}, nil
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[7],"process_immediately":true}`))
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
			require.Equal(t, []int64{7}, input.TargetIDs)
			return nil, infraerrors.Forbidden("BENCHMARK_DISABLED", "benchmark is disabled")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[7]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "BENCHMARK_DISABLED")
}

func TestBenchmarkHandlerCreateRunMissingTargetsReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createRunFn: func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error) {
			require.Equal(t, []int64{999}, input.TargetIDs)
			return nil, errors.New("benchmark targets missing: [999]")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs", bytes.NewBufferString(`{"target_ids":[999]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_TARGET_IDS")
}

func TestBenchmarkHandlerCreateStandardRunDefaultsToImmediateProcessing(t *testing.T) {
	processed := make(chan int64, 1)
	svc := &benchmarkAdminServiceStub{
		createStandardRunFn: func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
			require.True(t, input.ProcessImmediately)
			require.Empty(t, input.TargetIDs)
			require.Zero(t, input.TaskCount)
			return &ent.BenchmarkRun{ID: 41, Status: service.BenchmarkRunStatusQueued, TriggerType: service.BenchmarkTriggerManual}, nil
		},
	}
	processor := &benchmarkAdminProcessorStub{
		processRunFn: func(ctx context.Context, runID int64) (int, error) {
			processed <- runID
			return 0, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}, processor: processor})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/standard", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case runID := <-processed:
		require.Equal(t, int64(41), runID)
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}
}

func TestBenchmarkHandlerCreateStandardRunCanSkipImmediateProcessing(t *testing.T) {
	createdBy := int64(55)
	svc := &benchmarkAdminServiceStub{
		createStandardRunFn: func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
			require.False(t, input.ProcessImmediately)
			require.Equal(t, []int64{11}, input.TargetIDs)
			require.Equal(t, 2, input.TaskCount)
			require.Equal(t, &createdBy, input.CreatedBy)
			return &ent.BenchmarkRun{ID: 42, Status: service.BenchmarkRunStatusQueued, TriggerType: service.BenchmarkTriggerManual}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/standard", bytes.NewBufferString(`{"target_ids":[11],"task_count":2,"process_immediately":false,"created_by":55}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBenchmarkHandlerCreateStandardRunNoStandardTasksReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createStandardRunFn: func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
			require.False(t, input.ProcessImmediately)
			return nil, errors.New("no enabled standard benchmark tasks available")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/standard", bytes.NewBufferString(`{"process_immediately":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}

func TestBenchmarkHandlerCreateStandardRunNegativeTaskCountReturns400(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		createStandardRunFn: func(ctx context.Context, input service.BenchmarkStandardRunRequest) (*ent.BenchmarkRun, error) {
			require.False(t, input.ProcessImmediately)
			require.Equal(t, -1, input.TaskCount)
			return nil, errors.New("task count must not be negative")
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/benchmark/runs/standard", bytes.NewBufferString(`{"task_count":-1,"process_immediately":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
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
		listTargetScoresFn: func(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error) {
			require.Equal(t, int64(42), runID)
			return []*ent.BenchmarkTargetScore{
				{
					ID:           1,
					RunID:        runID,
					RunTargetID:  601,
					ModelName:    "claude-3-5-sonnet",
					ChannelID:    8,
					OverallScore: 0.92,
					Edges: ent.BenchmarkTargetScoreEdges{
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
	require.EqualValues(t, 601, scores[0]["run_target_id"])
	require.EqualValues(t, 0.92, scores[0]["overall_score"])
	edges, ok := scores[0]["edges"].(map[string]any)
	require.True(t, ok)
	runTarget, ok := edges["run_target"].(map[string]any)
	require.True(t, ok)
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
				Enabled:            &enabled,
			}, input)
			return []*ent.BenchmarkSchedule{{ID: 1, Name: "nightly"}}, 1, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/schedules?page=0&page_size=999&enabled=true", nil)
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
			require.Equal(t, "nightly", input.Name)
			require.Equal(t, "0 * * * *", input.CronExpr)
			require.True(t, input.Enabled)
			require.Equal(t, []int64{11, 12}, input.TargetIDs)
			require.Equal(t, 6, input.TaskCount)
			require.Nil(t, input.NextRunAt)
			return &ent.BenchmarkSchedule{
				ID:        12,
				Name:      input.Name,
				CronExpr:  input.CronExpr,
				Enabled:   input.Enabled,
				TargetIds: input.TargetIDs,
				TaskCount: input.TaskCount,
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	body := `{"name":"nightly","cron_expr":"0 * * * *","enabled":true,"target_ids":[11,12],"task_count":6}`
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
			require.Equal(t, "weekday smoke", input.Name)
			require.Equal(t, "15 2 * * 1-5", input.CronExpr)
			require.False(t, input.Enabled)
			require.Equal(t, []int64{21, 22}, input.TargetIDs)
			require.Equal(t, 4, input.TaskCount)
			require.Nil(t, input.NextRunAt)
			return &ent.BenchmarkSchedule{
				ID:        id,
				Name:      input.Name,
				CronExpr:  input.CronExpr,
				Enabled:   input.Enabled,
				TargetIds: input.TargetIDs,
				TaskCount: input.TaskCount,
			}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{
		benchmarkService: &benchmarkAdminServiceStub{},
		scheduleService:  scheduleSvc,
		snapshotService:  &benchmarkSnapshotServiceStub{},
	})

	body := `{"name":"weekday smoke","cron_expr":"15 2 * * 1-5","enabled":false,"target_ids":[21,22],"task_count":4}`
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
			return &ent.BenchmarkRun{ID: 52, TriggerType: service.BenchmarkTriggerScheduled}, nil
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
		{method: http.MethodPost, path: "/api/v1/admin/benchmark/schedules", body: `{"name":"nightly","cron_expr":"0 * * * *"}`},
		{method: http.MethodGet, path: "/api/v1/admin/benchmark/schedules/12"},
		{method: http.MethodPut, path: "/api/v1/admin/benchmark/schedules/12", body: `{"name":"nightly","cron_expr":"0 * * * *"}`},
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
