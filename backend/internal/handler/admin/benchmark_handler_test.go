package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
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

func TestBenchmarkHandlerListRunResults(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		listRunResultsFn: func(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error) {
			require.Equal(t, int64(41), runID)
			return []*ent.BenchmarkResult{{ID: 1}, {ID: 2}}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/runs/41/results", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var results []ent.BenchmarkResult
	require.NoError(t, json.Unmarshal(resp.Data, &results))
	require.Len(t, results, 2)
}

func TestBenchmarkHandlerListRunScores(t *testing.T) {
	svc := &benchmarkAdminServiceStub{
		listScoreSnapshotsFn: func(ctx context.Context, runID int64) ([]*ent.BenchmarkScoreSnapshot, error) {
			require.Equal(t, int64(42), runID)
			return []*ent.BenchmarkScoreSnapshot{{ID: 1}, {ID: 2}}, nil
		},
	}
	router := newBenchmarkTestRouter(&BenchmarkHandler{benchmarkService: svc, snapshotService: &benchmarkSnapshotServiceStub{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/benchmark/runs/42/scores", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp benchmarkHTTPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var scores []ent.BenchmarkScoreSnapshot
	require.NoError(t, json.Unmarshal(resp.Data, &scores))
	require.Len(t, scores, 2)
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
