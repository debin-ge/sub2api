package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type benchmarkAdminServiceStub struct {
	listSuitesFn         func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	createSuiteFn        func(ctx context.Context, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	listTargetsFn        func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	createTargetFn       func(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	listTasksFn          func(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	createTaskFn         func(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	listProfilesFn       func(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkProfile, int, error)
	createProfileFn      func(ctx context.Context, input service.BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	getProfileFn         func(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
	previewProfileFn     func(ctx context.Context, profileID int64, override service.BenchmarkProfilePreviewInput) (*service.BenchmarkProfilePreview, error)
	createRunFn          func(ctx context.Context, input service.BenchmarkCreateRunRequest) (*ent.BenchmarkRun, error)
	listRunsFn           func(ctx context.Context, input service.BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	getRunFn             func(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	listRunResultsFn     func(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	listScoreSnapshotsFn func(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error)
}

func (s *benchmarkAdminServiceStub) ListSuites(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error) {
	return s.listSuitesFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateSuite(ctx context.Context, input service.BenchmarkSuiteInput) (*ent.BenchmarkSuite, error) {
	return s.createSuiteFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) ListTargets(ctx context.Context, input service.BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error) {
	return s.listTargetsFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateTarget(ctx context.Context, input service.BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	return s.createTargetFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) ListTasks(ctx context.Context, input service.BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error) {
	return s.listTasksFn(ctx, input)
}

func (s *benchmarkAdminServiceStub) CreateTask(ctx context.Context, input service.BenchmarkTaskInput) (*ent.BenchmarkTask, error) {
	return s.createTaskFn(ctx, input)
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
	triggerScheduleFn func(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error)
}

func (s *benchmarkScheduleAdminServiceStub) ListSchedules(ctx context.Context, input service.BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error) {
	return s.listSchedulesFn(ctx, input)
}

func (s *benchmarkScheduleAdminServiceStub) CreateSchedule(ctx context.Context, input service.BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error) {
	return s.createScheduleFn(ctx, input)
}

func (s *benchmarkScheduleAdminServiceStub) TriggerSchedule(ctx context.Context, id int64, now time.Time) (*ent.BenchmarkRun, error) {
	return s.triggerScheduleFn(ctx, id, now)
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

func newBenchmarkTestRouter(handler *BenchmarkHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/v1/admin/benchmark/suites", handler.ListSuites)
	router.POST("/api/v1/admin/benchmark/suites", handler.CreateSuite)
	router.GET("/api/v1/admin/benchmark/targets", handler.ListTargets)
	router.POST("/api/v1/admin/benchmark/targets", handler.CreateTarget)
	router.GET("/api/v1/admin/benchmark/tasks", handler.ListTasks)
	router.POST("/api/v1/admin/benchmark/tasks", handler.CreateTask)
	router.GET("/api/v1/admin/benchmark/profiles", handler.ListProfiles)
	router.POST("/api/v1/admin/benchmark/profiles", handler.CreateProfile)
	router.GET("/api/v1/admin/benchmark/profiles/:id", handler.GetProfile)
	router.POST("/api/v1/admin/benchmark/profiles/:id/preview", handler.PreviewProfile)
	router.POST("/api/v1/admin/benchmark/runs", handler.CreateRun)
	router.GET("/api/v1/admin/benchmark/runs", handler.ListRuns)
	router.GET("/api/v1/admin/benchmark/runs/:id", handler.GetRun)
	router.GET("/api/v1/admin/benchmark/runs/:id/results", handler.ListRunResults)
	router.GET("/api/v1/admin/benchmark/runs/:id/scores", handler.ListRunScores)
	router.POST("/api/v1/admin/benchmark/runs/:id/publish", handler.PublishRun)
	router.GET("/api/v1/admin/benchmark/schedules", handler.ListSchedules)
	router.POST("/api/v1/admin/benchmark/schedules", handler.CreateSchedule)
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
