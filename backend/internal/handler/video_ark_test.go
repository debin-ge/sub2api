package handler

import (
	"context"
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var arkVideoTestPlatforms = []string{service.PlatformByteDance, service.PlatformFal, service.PlatformWaveSpeed, service.PlatformRunware, service.PlatformFlova}

const arkVideoTestBody = `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"海浪轻轻拍打沙滩 --rs 720p"}],"duration":5,"ratio":"16:9"}`

// newArkVideoHandlerTestContext 构造带分组平台的测试上下文；Ark 入口按分组平台准入。
func newArkVideoHandlerTestContext(method, target, platform string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	contentType := ""
	if body != nil {
		contentType = "application/json"
	}
	ctx, recorder := newVideoHandlerTestContext(method, target, contentType, body)
	key, _ := servermiddleware.GetAPIKeyFromContext(ctx)
	if platform != "" {
		key.Group = &service.Group{ID: 8, Platform: platform}
	}
	return ctx, recorder
}

func arkVideoHandlerTask() *service.VideoTask {
	task := videoHandlerTask()
	task.Provider = service.VideoProviderByteDance
	task.PublicModel = "doubao-seedance-1-0-pro-250528"
	task.RequestedModel = "doubao-seedance-1-0-pro-250528"
	task.UpdatedAt = time.Unix(1_700_000_060, 0).UTC()
	task.RequestAttributes = map[string]any{
		"prompt": "海浪轻轻拍打沙滩", "resolution": "720p", "ratio": "16:9", "duration": float64(5), "seconds": float64(5),
		"seed": float64(11), "generate_audio": true, "camera_fixed": false, "watermark": false, "return_last_frame": true, "service_tier": "default",
	}
	return task
}

func TestVideoHandlerArkCreateRespondsWithTaskIDOnly(t *testing.T) {
	for _, platform := range append([]string{service.PlatformComposite}, arkVideoTestPlatforms...) {
		t.Run(platform, func(t *testing.T) {
			task := arkVideoHandlerTask()
			fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
			handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			ctx, recorder := newArkVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", platform, strings.NewReader(arkVideoTestBody))
			ctx.Request.Header.Set("Idempotency-Key", "ark-idem-1")

			handler.ArkCreate(ctx)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.JSONEq(t, `{"id":"`+task.PublicID+`"}`, recorder.Body.String(), "Ark create returns the id and nothing else")
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			require.Equal(t, 1, fake.submitCalls)
			require.Equal(t, service.VideoOperationGenerate, fake.submitRequest.Operation)
			require.Equal(t, "doubao-seedance-1-0-pro-250528", fake.submitRequest.Model)
			require.Equal(t, "ark-idem-1", fake.submitRequest.IdempotencyKey)
			require.Equal(t, "720p", fake.submitRequest.Resolution, "prompt flags are resolved before submission so the hold matches")
			require.Equal(t, "16:9", fake.submitRequest.Ratio)
			require.Equal(t, 5, fake.submitRequest.Seconds)
			require.NotNil(t, fake.submitRequest.APIKey)
			require.Equal(t, int64(7), fake.submitRequest.APIKey.ID)
			require.Len(t, fake.submitRequest.Content, 1)
		})
	}
}

func TestVideoHandlerArkCreateMapsProtocolErrors(t *testing.T) {
	text := `{"type":"text","text":"waves"}`
	for name, scenario := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"invalid content":     {`{"model":"m","content":[]}`, http.StatusBadRequest, "invalid_content"},
		"invalid role":        {`{"model":"m","content":[` + text + `,{"type":"video_url","video_url":{"url":"https://x.example/a.mp4"},"role":"first_frame"}]}`, http.StatusBadRequest, "invalid_role"},
		"frames and duration": {`{"model":"m","duration":5,"frames":121,"content":[` + text + `]}`, http.StatusBadRequest, "duration_frames_conflict"},
		"adaptive ratio":      {`{"model":"m","ratio":"adaptive","content":[` + text + `]}`, http.StatusBadRequest, "adaptive_ratio_requires_first_frame"},
		"unsupported field":   {`{"model":"m","priority":1,"content":[` + text + `]}`, http.StatusBadRequest, "unsupported_parameter"},
		"unknown field":       {`{"model":"m","prompt":"waves","content":[` + text + `]}`, http.StatusBadRequest, "video_invalid_request"},
		"duplicate keys":      {`{"model":"m","model":"n","content":[` + text + `]}`, http.StatusBadRequest, "video_invalid_request"},
		"video data uri":      {`{"model":"m","content":[` + text + `,{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AAAA"}}]}`, http.StatusBadRequest, "video_input_unsupported"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &videoTaskAPIFake{}
			handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			ctx, recorder := newArkVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", service.PlatformByteDance, strings.NewReader(scenario.body))

			handler.ArkCreate(ctx)

			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), `"code":"`+scenario.code+`"`)
			require.Zero(t, fake.submitCalls)
		})
	}

	t.Run("multipart is rejected", func(t *testing.T) {
		fake := &videoTaskAPIFake{}
		ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "multipart/form-data; boundary=x", strings.NewReader("--x--"))
		key, _ := servermiddleware.GetAPIKeyFromContext(ctx)
		key.Group = &service.Group{ID: 8, Platform: service.PlatformByteDance}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkCreate(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_invalid_request")
		require.Zero(t, fake.submitCalls)
	})
	t.Run("creation flag still allows idempotent replays", func(t *testing.T) {
		cfg := videoHandlerTestConfig(t)
		cfg.Gateway.Video.CreationEnabled = false
		fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: arkVideoHandlerTask()}}
		handler := newVideoHandler(fake, nil, cfg)
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", service.PlatformByteDance, strings.NewReader(arkVideoTestBody))
		handler.ArkCreate(ctx)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_creation_disabled")
		require.Zero(t, fake.submitCalls)

		ctx, recorder = newArkVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", service.PlatformByteDance, strings.NewReader(arkVideoTestBody))
		ctx.Request.Header.Set("Idempotency-Key", "replay")
		handler.ArkCreate(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, 1, fake.submitCalls)
	})
	t.Run("service errors keep their envelope", func(t *testing.T) {
		fake := &videoTaskAPIFake{submitErr: service.ErrVideoInsufficientBalance}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", service.PlatformByteDance, strings.NewReader(arkVideoTestBody))
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkCreate(ctx)
		require.Equal(t, http.StatusPaymentRequired, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_insufficient_balance")
	})
}

// Ark 入口只对五家平台与 composite 开放；OpenAI / Grok / 其他平台与无分组的 Key 一律 403，
// 且在触达任务存储之前就被拦住。
func TestVideoHandlerArkEndpointsRejectNonArkGroups(t *testing.T) {
	task := arkVideoHandlerTask()
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformAnthropic, service.PlatformGemini, ""} {
		for name, invoke := range map[string]func(*VideoHandler, *gin.Context){
			"create":   (*VideoHandler).ArkCreate,
			"retrieve": (*VideoHandler).ArkRetrieve,
			"list":     (*VideoHandler).ArkList,
			"delete":   (*VideoHandler).ArkDelete,
			"models":   (*VideoHandler).ArkModels,
		} {
			t.Run(platform+"/"+name, func(t *testing.T) {
				fake := &videoTaskAPIFake{getTask: task, deleteTask: task, submitResult: &service.VideoSubmitResult{Task: task},
					listPage: &service.VideoTaskPage{}, arkModels: &service.ArkVideoModelsResponse{Object: "list"}}
				method, body := http.MethodGet, io.Reader(nil)
				if name == "create" {
					method, body = http.MethodPost, strings.NewReader(arkVideoTestBody)
				}
				ctx, recorder := newArkVideoHandlerTestContext(method, "/api/v3/contents/generations/tasks", platform, body)
				ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}

				invoke(newVideoHandler(fake, nil, videoHandlerTestConfig(t)), ctx)

				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "video_ark_platform_unsupported")
				require.Zero(t, fake.submitCalls)
				require.Zero(t, fake.getCalls)
				require.Empty(t, fake.deletePublicID)
				require.Nil(t, fake.modelsAPIKey)
				require.Zero(t, fake.listUserID)
			})
		}
	}

	t.Run("missing api key is 401 before the group check", func(t *testing.T) {
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", service.PlatformByteDance, nil)
		ctx.Set(string(servermiddleware.ContextKeyAPIKey), nil)
		newVideoHandler(&videoTaskAPIFake{}, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Contains(t, recorder.Body.String(), "api_key_required")
	})
}

func TestIsArkVideoGroup(t *testing.T) {
	for _, platform := range append([]string{service.PlatformComposite}, arkVideoTestPlatforms...) {
		ctx, _ := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", platform, nil)
		require.True(t, IsArkVideoGroup(ctx), platform)
	}
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformAnthropic, ""} {
		ctx, _ := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", platform, nil)
		require.False(t, IsArkVideoGroup(ctx), platform)
	}
	ctx, _ := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", service.PlatformByteDance, nil)
	ctx.Set(string(servermiddleware.ContextKeyAPIKey), nil)
	require.False(t, IsArkVideoGroup(ctx))
}

func TestVideoHandlerArkRetrieveProjectsArkTaskObject(t *testing.T) {
	errorCode, errorMessage := "content_policy", "rejected by content policy"
	for _, scenario := range []struct {
		name, generation, billing, status string
		retryAfter, content, cost, err    bool
		errCode                           string
		variants                          []string
	}{
		{name: "preparing", generation: service.VideoGenerationPreparing, billing: service.VideoBillingHeld, status: "queued", retryAfter: true},
		{name: "queued", generation: service.VideoGenerationQueued, billing: service.VideoBillingHeld, status: "queued", retryAfter: true},
		{name: "in progress", generation: service.VideoGenerationInProgress, billing: service.VideoBillingHeld, status: "running", retryAfter: true},
		{name: "completed but capture pending stays running", generation: service.VideoGenerationCompleted, billing: service.VideoBillingCapturePending, status: "running", retryAfter: true, variants: []string{"video"}},
		{name: "succeeded", generation: service.VideoGenerationCompleted, billing: service.VideoBillingCaptured, status: "succeeded", content: true, cost: true, variants: []string{"video", "last_frame"}},
		{name: "succeeded without last frame", generation: service.VideoGenerationCompleted, billing: service.VideoBillingCaptured, status: "succeeded", content: true, cost: true, variants: []string{"video"}},
		{name: "failed", generation: service.VideoGenerationFailed, billing: service.VideoBillingReleased, status: "failed", err: true, errCode: errorCode},
		{name: "cancelled", generation: service.VideoGenerationCancelled, billing: service.VideoBillingReleased, status: "cancelled", err: true, errCode: errorCode},
		{name: "expired", generation: service.VideoGenerationExpired, billing: service.VideoBillingReleased, status: "expired", err: true, errCode: errorCode},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			task := arkVideoHandlerTask()
			task.GenerationState, task.BillingState = scenario.generation, scenario.billing
			task.ContentVariants = scenario.variants
			task.ActualCost, task.Currency = float64Pointer(0.42), "USD"
			if scenario.err {
				task.LastErrorCode, task.LastErrorMessage = &errorCode, &errorMessage
				task.ActualCost = float64Pointer(0)
			}
			fake := &videoTaskAPIFake{getTask: task}
			handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/"+task.PublicID, service.PlatformByteDance, nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
			ctx.Request.Host = "api.current.example"
			ctx.Request.Header.Set("X-Forwarded-Proto", "https")

			handler.ArkRetrieve(ctx)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, int64(42), fake.getUserID)
			require.Equal(t, task.PublicID, fake.getPublicID, "the :task_id route parameter must reach the store")
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			if scenario.retryAfter {
				require.Equal(t, "10", recorder.Header().Get("Retry-After"))
			} else {
				require.Empty(t, recorder.Header().Get("Retry-After"))
			}
			var response map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, task.PublicID, response["id"])
			require.Equal(t, "doubao-seedance-1-0-pro-250528", response["model"])
			require.Equal(t, scenario.status, response["status"])
			require.EqualValues(t, task.CreatedAt.Unix(), response["created_at"])
			require.EqualValues(t, task.UpdatedAt.Unix(), response["updated_at"])
			require.Equal(t, "海浪轻轻拍打沙滩", response["prompt"])
			require.Equal(t, "720p", response["resolution"])
			require.Equal(t, "16:9", response["ratio"])
			require.EqualValues(t, 5, response["duration"])
			require.EqualValues(t, 11, response["seed"])
			require.Equal(t, true, response["generate_audio"])
			require.Equal(t, "default", response["service_tier"])
			if scenario.generation == service.VideoGenerationCompleted && scenario.billing != service.VideoBillingCaptured {
				require.NotContains(t, response, "provider")
				require.NotContains(t, response, "provider_access")
			} else {
				require.Equal(t, "bytedance", response["provider"])
			}
			require.NotContains(t, response, "object")
			require.NotContains(t, response, "url")
			require.NotContains(t, response, "provider_task_id")
			require.NotContains(t, recorder.Body.String(), "video_upstream_1")

			if scenario.content {
				content, ok := response["content"].(map[string]any)
				require.True(t, ok, "succeeded tasks carry content links")
				videoURL, _ := content["video_url"].(string)
				parsed, err := url.Parse(videoURL)
				require.NoError(t, err)
				require.Equal(t, "https", parsed.Scheme)
				require.Equal(t, "api.current.example", parsed.Host)
				require.Equal(t, "/api/v3/contents/generations/tasks/"+task.PublicID+"/content", parsed.Path)
				require.Equal(t, "video", parsed.Query().Get("variant"))
				exp, err := strconv.ParseInt(parsed.Query().Get("exp"), 10, 64)
				require.NoError(t, err)
				require.Greater(t, exp, time.Now().Unix())
				require.True(t, handler.verifyArkContentSignature(task.PublicID, "video", exp, parsed.Query().Get("sig")))
				if hasVideoContentVariant(scenario.variants, "last_frame") {
					lastFrame, err := url.Parse(content["last_frame_url"].(string))
					require.NoError(t, err)
					require.Equal(t, "last_frame", lastFrame.Query().Get("variant"))
					lastExp, _ := strconv.ParseInt(lastFrame.Query().Get("exp"), 10, 64)
					require.True(t, handler.verifyArkContentSignature(task.PublicID, "last_frame", lastExp, lastFrame.Query().Get("sig")))
				} else {
					require.NotContains(t, content, "last_frame_url")
				}
			} else {
				require.NotContains(t, response, "content")
			}
			if scenario.cost {
				require.InDelta(t, 0.42, response["actual_cost"], 1e-9)
				require.Equal(t, "USD", response["currency"])
			} else {
				require.NotContains(t, response, "actual_cost", "unsettled or released tasks never disclose a cost")
				require.NotContains(t, response, "currency")
			}
			if scenario.err {
				errorObject, ok := response["error"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, scenario.errCode, errorObject["code"])
				require.Equal(t, errorMessage, errorObject["message"])
			} else {
				require.NotContains(t, response, "error")
			}
		})
	}
}

func TestVideoHandlerArkRetrieveHidesOpenAIShapeTasksAndRespectsDisclosure(t *testing.T) {
	t.Run("openai task is not visible through the ark path", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: videoHandlerTask()}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/x", service.PlatformComposite, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: videoHandlerTask().PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkRetrieve(ctx)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"task_not_found"`)
	})
	t.Run("store miss is an ark style 404", func(t *testing.T) {
		fake := &videoTaskAPIFake{getErr: service.ErrVideoTaskNotFound}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/x", service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: "video_ffffffffffffffffffffffffffffffff"}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkRetrieve(ctx)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"task_not_found"`)
		require.Equal(t, "video_ffffffffffffffffffffffffffffffff", fake.getPublicID)
	})
	t.Run("disclosure none drops the sub2api extension fields", func(t *testing.T) {
		task := arkVideoHandlerTask()
		task.GenerationState, task.BillingState = service.VideoGenerationCompleted, service.VideoBillingCaptured
		task.ActualCost, task.Currency = float64Pointer(1.5), "USD"
		fake := &videoTaskAPIFake{getTask: task, disclosure: &service.VideoTaskDisclosure{Policy: config.VideoDisclosureNone}}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/x", service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkRetrieve(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, "succeeded", response["status"])
		require.NotContains(t, response, "provider")
		require.NotContains(t, response, "actual_cost")
		require.NotContains(t, response, "currency")
		require.NotContains(t, response, "provider_access")
	})
	t.Run("video disabled", func(t *testing.T) {
		cfg := videoHandlerTestConfig(t)
		cfg.Gateway.Video.Enabled = false
		fake := &videoTaskAPIFake{getTask: arkVideoHandlerTask()}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/x", service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: arkVideoHandlerTask().PublicID}}
		newVideoHandler(fake, nil, cfg).ArkRetrieve(ctx)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_disabled")
		require.Zero(t, fake.getCalls)
	})
}

// The fake models the repository contract: filter first, then bounded paging.
type arkListAPIFake struct {
	videoTaskAPIFake
	data    []*service.VideoTask
	filters []service.VideoTaskFilter
}

func (f *arkListAPIFake) ListForOwner(_ context.Context, userID int64, filter service.VideoTaskFilter) (*service.VideoTaskPage, error) {
	f.listUserID = userID
	f.filters = append(f.filters, filter)
	data := make([]*service.VideoTask, 0)
	for _, task := range f.data {
		if !service.IsArkVideoProvider(task.Provider) || (filter.ArkStatus != "" && service.ProjectArkVideoStatus(task) != filter.ArkStatus) {
			continue
		}
		data = append(data, task)
	}
	if filter.Offset >= len(data) {
		return &service.VideoTaskPage{}, nil
	}
	data = data[filter.Offset:]
	more := len(data) > filter.Limit
	if more {
		data = data[:filter.Limit]
	}
	return &service.VideoTaskPage{Data: data, HasMore: more}, nil
}

func arkListTestTask(index int, provider, generation, billing string) *service.VideoTask {
	task := arkVideoHandlerTask()
	task.ID = int64(index)
	task.PublicID = "video_" + strings.Repeat(strconv.Itoa(index), 32)
	task.Provider = provider
	task.GenerationState, task.BillingState = generation, billing
	if generation == service.VideoGenerationCompleted {
		task.ContentVariants = []string{"video"}
	}
	return task
}

func newArkListTestFake() *arkListAPIFake {
	return &arkListAPIFake{data: []*service.VideoTask{
		arkListTestTask(0, service.VideoProviderByteDance, service.VideoGenerationQueued, service.VideoBillingHeld),
		arkListTestTask(1, service.VideoProviderFal, service.VideoGenerationInProgress, service.VideoBillingHeld),
		arkListTestTask(2, service.VideoProviderFlova, service.VideoGenerationCompleted, service.VideoBillingCaptured),
		arkListTestTask(9, service.VideoProviderOpenAI, service.VideoGenerationCompleted, service.VideoBillingCaptured),
		arkListTestTask(3, service.VideoProviderWaveSpeed, service.VideoGenerationFailed, service.VideoBillingReleased),
		arkListTestTask(4, service.VideoProviderRunware, service.VideoGenerationCompleted, service.VideoBillingCaptured),
		arkListTestTask(5, service.VideoProviderByteDance, service.VideoGenerationCancelled, service.VideoBillingReleased),
	}}
}

func arkListResponse(t *testing.T, recorder *httptest.ResponseRecorder) (map[string]any, []string) {
	t.Helper()
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	items, _ := response["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		require.True(t, ok)
		ids = append(ids, object["id"].(string))
	}
	return response, ids
}

func TestVideoHandlerArkListPagesAndFilters(t *testing.T) {
	id := func(index int) string { return "video_" + strings.Repeat(strconv.Itoa(index), 32) }
	t.Run("first page", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?page_num=1&page_size=2", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		response, ids := arkListResponse(t, recorder)
		require.Equal(t, []string{id(0), id(1)}, ids)
		require.EqualValues(t, 1, response["page_num"])
		require.EqualValues(t, 2, response["page_size"])
		require.Equal(t, true, response["has_more"])
		require.NotContains(t, response, "total", "total is only known once the last page is reached")
		require.Equal(t, int64(42), fake.listUserID)
		require.Len(t, fake.filters, 1)
		require.Equal(t, service.VideoTaskFilter{Operation: service.VideoOperationGenerate, Limit: 2, Order: "desc", ArkOnly: true}, fake.filters[0])
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})
	t.Run("last page skips openai tasks and reports total", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?page_num=3&page_size=2", service.PlatformComposite, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		response, ids := arkListResponse(t, recorder)
		require.Equal(t, []string{id(4), id(5)}, ids)
		require.Equal(t, false, response["has_more"])
		require.EqualValues(t, 6, response["total"])
		require.Len(t, fake.filters, 1)
		require.Equal(t, 4, fake.filters[0].Offset)
		require.NotContains(t, recorder.Body.String(), id(9), "OpenAI-shape tasks never show up in the Ark list")
	})
	t.Run("default page size and status words", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		response, ids := arkListResponse(t, recorder)
		require.Equal(t, []string{id(0), id(1), id(2), id(3), id(4), id(5)}, ids)
		require.EqualValues(t, 20, response["page_size"])
		require.EqualValues(t, 6, response["total"])
		statuses := make([]string, 0, 6)
		for _, item := range response["items"].([]any) {
			statuses = append(statuses, item.(map[string]any)["status"].(string))
		}
		require.Equal(t, []string{"queued", "running", "succeeded", "failed", "succeeded", "cancelled"}, statuses)
	})
	t.Run("public status filter reaches repository before pagination", func(t *testing.T) {
		for _, query := range []string{"filter.status=succeeded", "status=succeeded"} {
			fake := newArkListTestFake()
			ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?"+query, service.PlatformByteDance, nil)
			newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			response, ids := arkListResponse(t, recorder)
			require.Equal(t, []string{id(2), id(4)}, ids, query)
			require.EqualValues(t, 2, response["total"])
			require.Equal(t, false, response["has_more"])
			require.Len(t, fake.filters, 1)
			require.Equal(t, service.ArkVideoStatusSucceeded, fake.filters[0].ArkStatus)
			for _, item := range response["items"].([]any) {
				require.Contains(t, item.(map[string]any), "content", "succeeded items carry signed content links")
			}
		}
	})
	t.Run("no matching rows performs one query", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?status=expired&page_size=1", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		response, ids := arkListResponse(t, recorder)
		require.Empty(t, ids)
		require.Len(t, fake.filters, 1)
		require.EqualValues(t, 0, response["total"])
	})
	t.Run("empty page past end does not invent total", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?page_num=50&page_size=100", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		response, ids := arkListResponse(t, recorder)
		require.Empty(t, ids)
		require.Len(t, fake.filters, 1)
		require.Equal(t, 4900, fake.filters[0].Offset)
		require.NotContains(t, response, "total")
	})
	t.Run("model filter reaches the store", func(t *testing.T) {
		fake := newArkListTestFake()
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?filter.model=doubao-seedance-1-0-pro-250528&filter.status=running", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		_, ids := arkListResponse(t, recorder)
		require.Equal(t, []string{id(1)}, ids)
		require.Equal(t, "doubao-seedance-1-0-pro-250528", fake.filters[0].Model)
	})
	t.Run("invalid paging and status are 400", func(t *testing.T) {
		for _, query := range []string{"page_num=0", "page_num=51", "page_num=abc", "page_size=0", "page_size=101", "page_size=ten", "filter.status=done", "status=completed"} {
			fake := newArkListTestFake()
			ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks?"+query, service.PlatformByteDance, nil)
			newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
			require.Equal(t, http.StatusBadRequest, recorder.Code, query)
			require.Contains(t, recorder.Body.String(), "video_invalid_request", query)
			require.Empty(t, fake.filters, query)
		}
	})
	t.Run("store errors propagate", func(t *testing.T) {
		fake := &videoTaskAPIFake{listErr: service.ErrBillingServiceUnavailable}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/tasks", service.PlatformByteDance, nil)
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkList(ctx)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	})
}

func TestVideoHandlerArkDeleteReturnsEmptyObject(t *testing.T) {
	task := arkVideoHandlerTask()
	t.Run("deleted", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: task, deleteTask: task}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodDelete, "/api/v3/contents/generations/tasks/"+task.PublicID, service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkDelete(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.JSONEq(t, `{}`, recorder.Body.String())
		require.Equal(t, int64(42), fake.deleteUserID)
		require.Equal(t, task.PublicID, fake.deletePublicID)
		require.Equal(t, task.PublicID, fake.getPublicID)
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})
	t.Run("openai task is invisible", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: videoHandlerTask(), deleteTask: videoHandlerTask()}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodDelete, "/api/v3/contents/generations/tasks/x", service.PlatformComposite, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkDelete(ctx)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"task_not_found"`)
		require.Empty(t, fake.deletePublicID, "nothing may be deleted through the wrong shape")
	})
	t.Run("missing task", func(t *testing.T) {
		fake := &videoTaskAPIFake{getErr: service.ErrVideoTaskNotFound}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodDelete, "/api/v3/contents/generations/tasks/x", service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkDelete(ctx)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"task_not_found"`)
		require.Empty(t, fake.deletePublicID)
	})
	t.Run("delete conflicts keep their code", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: task, deleteErr: service.ErrVideoDeleteConflict}
		ctx, recorder := newArkVideoHandlerTestContext(http.MethodDelete, "/api/v3/contents/generations/tasks/x", service.PlatformByteDance, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.PublicID}}
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkDelete(ctx)
		require.Equal(t, http.StatusConflict, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_delete_conflict")
	})
}

func TestVideoHandlerArkModelsReturnsCatalog(t *testing.T) {
	fake := &videoTaskAPIFake{arkModels: &service.ArkVideoModelsResponse{Object: "list", Data: []service.ArkVideoModel{{
		ID: "doubao-seedance-1-0-pro-250528", Object: "model", Provider: service.VideoProviderByteDance, IsDefault: true,
	}}}}
	ctx, recorder := newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/models", service.PlatformByteDance, nil)
	newVideoHandler(fake, nil, videoHandlerTestConfig(t)).ArkModels(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"object":"list"`)
	require.Contains(t, recorder.Body.String(), `"id":"doubao-seedance-1-0-pro-250528"`)
	require.Contains(t, recorder.Body.String(), `"is_default":true`)
	require.NotNil(t, fake.modelsAPIKey)
	require.Equal(t, int64(7), fake.modelsAPIKey.ID)
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))

	failing := &videoTaskAPIFake{arkModelsErr: service.ErrVideoProviderUnsupported}
	ctx, recorder = newArkVideoHandlerTestContext(http.MethodGet, "/api/v3/contents/generations/models", service.PlatformFlova, nil)
	newVideoHandler(failing, nil, videoHandlerTestConfig(t)).ArkModels(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "video_provider_unsupported")
}

func completedArkContentTask() *service.VideoTask {
	task := arkVideoHandlerTask()
	task.GenerationState, task.BillingState = service.VideoGenerationCompleted, service.VideoBillingCaptured
	task.ContentVariants = []string{"video", "last_frame"}
	return task
}

// newArkSignedContentTestContext 构造签名内容请求：没有 API Key，身份完全来自链接签名。
func newArkSignedContentTestContext(method, publicID string, query url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	target := "/api/v3/contents/generations/tasks/" + publicID + "/content"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	ctx, recorder := newVideoHandlerTestContext(method, target, "", nil)
	ctx.Set(string(servermiddleware.ContextKeyAPIKey), nil)
	ctx.Set(string(servermiddleware.ContextKeyUser), nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: publicID}}
	return ctx, recorder
}

func arkSignedQuery(handler *VideoHandler, publicID, variant string, exp int64) url.Values {
	query := url.Values{}
	if variant != "" {
		query.Set("variant", variant)
	}
	query.Set("exp", strconv.FormatInt(exp, 10))
	query.Set("sig", handler.signArkContentURL(publicID, variant, exp))
	return query
}

func TestVideoHandlerArkSignedContentRoundTrip(t *testing.T) {
	task := completedArkContentTask()
	newFake := func() *videoTaskAPIFake {
		return &videoTaskAPIFake{publicIDTask: task, content: &service.ProviderContent{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("ark-video")),
		}}
	}
	handler := newVideoHandler(newFake(), nil, videoHandlerTestConfig(t))
	request := httptest.NewRequest(http.MethodGet, "/api/v3/contents/generations/tasks/"+task.PublicID, nil)
	request.Host = "api.current.example"
	request.Header.Set("X-Forwarded-Proto", "https")
	now := time.Unix(1_700_000_000, 0).UTC()

	// 签：链接形态与过期时间截断。
	signed, err := url.Parse(handler.arkSignedContentURL(task, request, "video", now))
	require.NoError(t, err)
	require.Equal(t, "https://api.current.example/api/v3/contents/generations/tasks/"+task.PublicID+"/content", signed.Scheme+"://"+signed.Host+signed.Path)
	require.Equal(t, "video", signed.Query().Get("variant"))
	require.Equal(t, strconv.FormatInt(now.Add(videoContentURLTTL).Unix(), 10), signed.Query().Get("exp"))
	require.True(t, handler.verifyArkContentSignature(task.PublicID, "video", now.Add(videoContentURLTTL).Unix(), signed.Query().Get("sig")))
	contentExpiry := now.Add(time.Hour)
	task.ContentExpiresAt = &contentExpiry
	clamped, err := url.Parse(handler.arkSignedContentURL(task, request, "last_frame", now))
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(contentExpiry.Unix(), 10), clamped.Query().Get("exp"))
	require.Equal(t, "last_frame", clamped.Query().Get("variant"))
	task.ContentExpiresAt = nil
	require.Empty(t, handler.arkSignedContentURL(task, &http.Request{Host: ""}, "video", now))
	require.Empty(t, newVideoHandler(newFake(), nil, &config.Config{}).arkSignedContentURL(task, request, "video", now), "no signing key means no link")

	// 两套签名路由不可互换：OpenAI 形态的签名拿到 Ark 路由上验不过，反之亦然。
	exp := time.Now().Add(time.Hour).Unix()
	require.NotEqual(t, handler.signVideoContentURL(task.PublicID, exp), handler.signArkContentURL(task.PublicID, "video", exp))
	require.NotEqual(t, handler.signArkContentURL(task.PublicID, "video", exp), handler.signArkContentURL(task.PublicID, "last_frame", exp))
	require.False(t, handler.verifyArkContentSignature(task.PublicID, "video", exp, handler.signVideoContentURL(task.PublicID, exp)))
	require.False(t, handler.verifyVideoContentSignature(task.PublicID, exp, handler.signArkContentURL(task.PublicID, "video", exp)))

	// 验：按签名链接取内容。
	t.Run("video", func(t *testing.T) {
		fake := newFake()
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, arkSignedQuery(handler, task.PublicID, "video", exp))
		handler.ArkSignedContent(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, "ark-video", recorder.Body.String())
		require.Equal(t, task.PublicID, fake.publicIDReference, "the :task_id route parameter must reach the store")
		require.Equal(t, task.PublicID, fake.contentReference)
		require.Equal(t, "video", fake.contentRequest.Variant)
		require.Equal(t, `inline; filename="`+task.PublicID+`.mp4"`, recorder.Header().Get("Content-Disposition"))
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})
	t.Run("last frame", func(t *testing.T) {
		fake := newFake()
		fake.content.Header = http.Header{"Content-Type": {"image/jpeg"}}
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, arkSignedQuery(handler, task.PublicID, "last_frame", exp))
		handler.ArkSignedContent(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, "last_frame", fake.contentRequest.Variant)
	})
	t.Run("head", func(t *testing.T) {
		fake := newFake()
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newArkSignedContentTestContext(http.MethodHead, task.PublicID, arkSignedQuery(handler, task.PublicID, "video", exp))
		handler.ArkSignedContent(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Empty(t, recorder.Body.String())
		require.Equal(t, http.MethodHead, fake.contentRequest.Method)
	})
	t.Run("variant defaults to video", func(t *testing.T) {
		fake := newFake()
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		query := arkSignedQuery(handler, task.PublicID, "video", exp)
		query.Del("variant")
		ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, query)
		handler.ArkSignedContent(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, "video", fake.contentRequest.Variant)
	})
	t.Run("expired link is 410 without touching the store", func(t *testing.T) {
		fake := newFake()
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		past := time.Now().Add(-time.Second).Unix()
		ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, arkSignedQuery(handler, task.PublicID, "video", past))
		handler.ArkSignedContent(ctx)
		require.Equal(t, http.StatusGone, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_content_expired")
		require.Empty(t, fake.publicIDReference)
		require.Empty(t, fake.contentReference)
	})
	t.Run("invalid links are 404 without touching the store", func(t *testing.T) {
		otherID := "video_ffffffffffffffffffffffffffffffff"
		videoSig := handler.signArkContentURL(task.PublicID, "video", exp)
		for name, query := range map[string]url.Values{
			"wrong variant name":      arkSignedQuery(handler, task.PublicID, "thumbnail", exp),
			"video sig on last_frame": {"variant": {"last_frame"}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {videoSig}},
			"openai shape signature":  {"variant": {"video"}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {handler.signVideoContentURL(task.PublicID, exp)}},
			"other task same sig":     {"variant": {"video"}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {handler.signArkContentURL(otherID, "video", exp)}},
			"extended expiry":         {"variant": {"video"}, "exp": {strconv.FormatInt(exp+60, 10)}, "sig": {videoSig}},
			"tampered":                {"variant": {"video"}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {"00" + videoSig[2:]}},
			"missing sig":             {"variant": {"video"}, "exp": {strconv.FormatInt(exp, 10)}},
			"non numeric exp":         {"variant": {"video"}, "exp": {"tomorrow"}, "sig": {videoSig}},
			"zero exp":                arkSignedQuery(handler, task.PublicID, "video", 0),
		} {
			t.Run(name, func(t *testing.T) {
				fake := newFake()
				handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
				ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, query)
				handler.ArkSignedContent(ctx)
				require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "video_task_not_found")
				require.Empty(t, fake.publicIDReference)
				require.Empty(t, fake.contentReference)
			})
		}
		t.Run("malformed task id", func(t *testing.T) {
			fake := newFake()
			handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			ctx, recorder := newArkSignedContentTestContext(http.MethodGet, "not-a-task", arkSignedQuery(handler, "not-a-task", "video", exp))
			handler.ArkSignedContent(ctx)
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Empty(t, fake.publicIDReference)
		})
		t.Run("post method", func(t *testing.T) {
			fake := newFake()
			handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			ctx, recorder := newArkSignedContentTestContext(http.MethodPost, task.PublicID, arkSignedQuery(handler, task.PublicID, "video", exp))
			handler.ArkSignedContent(ctx)
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Empty(t, fake.publicIDReference)
		})
	})
	t.Run("task state gates", func(t *testing.T) {
		for name, scenario := range map[string]struct {
			mutate   func(*service.VideoTask, *videoTaskAPIFake, *config.Config)
			variant  string
			status   int
			wantCode string
		}{
			"openai task": {mutate: func(task *service.VideoTask, _ *videoTaskAPIFake, _ *config.Config) {
				task.Provider = service.VideoProviderOpenAI
			}, variant: "video", status: http.StatusNotFound, wantCode: "video_task_not_found"},
			"deleted": {mutate: func(task *service.VideoTask, _ *videoTaskAPIFake, _ *config.Config) {
				task.DeleteState = service.VideoDeleteDeleted
			}, variant: "video", status: http.StatusNotFound, wantCode: "video_task_not_found"},
			"missing": {mutate: func(_ *service.VideoTask, fake *videoTaskAPIFake, _ *config.Config) {
				fake.publicIDTask, fake.publicIDErr = nil, service.ErrVideoTaskNotFound
			}, variant: "video", status: http.StatusNotFound, wantCode: "video_task_not_found"},
			"not completed": {mutate: func(task *service.VideoTask, _ *videoTaskAPIFake, _ *config.Config) {
				task.GenerationState = service.VideoGenerationInProgress
			}, variant: "video", status: http.StatusConflict, wantCode: "video_content_not_ready"},
			"last frame variant missing": {mutate: func(task *service.VideoTask, _ *videoTaskAPIFake, _ *config.Config) {
				task.ContentVariants = []string{"video"}
			}, variant: "last_frame", status: http.StatusConflict, wantCode: "video_content_not_ready"},
			"content proxy disabled": {mutate: func(_ *service.VideoTask, _ *videoTaskAPIFake, cfg *config.Config) {
				cfg.Gateway.Video.ContentProxy.Enabled = false
			}, variant: "video", status: http.StatusNotFound, wantCode: "video_disabled"},
			"video disabled": {mutate: func(_ *service.VideoTask, _ *videoTaskAPIFake, cfg *config.Config) {
				cfg.Gateway.Video.Enabled = false
			}, variant: "video", status: http.StatusNotFound, wantCode: "video_disabled"},
		} {
			t.Run(name, func(t *testing.T) {
				task := completedArkContentTask()
				cfg := videoHandlerTestConfig(t)
				fake := newFake()
				fake.publicIDTask = task
				scenario.mutate(task, fake, cfg)
				handler := newVideoHandler(fake, nil, cfg)
				ctx, recorder := newArkSignedContentTestContext(http.MethodGet, task.PublicID, arkSignedQuery(handler, task.PublicID, scenario.variant, exp))
				handler.ArkSignedContent(ctx)
				require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), scenario.wantCode)
				require.Empty(t, fake.contentReference, "content must not be opened")
			})
		}
	})
}

func TestVideoModerationBodyIncludesContentImageURLsAndSkipsSpooledDuplicates(t *testing.T) {
	inputs := []service.VideoInput{{
		VideoInputManifestEntry: service.VideoInputManifestEntry{Role: service.VideoInputRoleFirstFrame, MIMEType: "image/png", Size: 4},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("test")), nil },
	}}
	body, err := videoModerationBody(context.Background(), service.VideoSubmitRequest{
		Prompt: "moderate me",
		Content: []service.VideoContentItem{
			{Type: service.VideoContentTypeText, Text: "moderate me"},
			{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleFirstFrame, Input: &inputs[0]},
			{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleLastFrame, URL: "https://images.example/last.png"},
			{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleTaggedReferenceImage, URL: " https://images.example/ref.png "},
			{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleReferenceImage, URL: "https://images.example/input.png"},
			{Type: service.VideoContentTypeVideo, Role: service.VideoInputRoleReferenceVideo, URL: "https://images.example/ref.mp4"},
			{Type: service.VideoContentTypeAudio, Role: service.VideoInputRoleReferenceAudio, URL: "https://images.example/ref.mp3"},
		},
		Inputs: inputs,
	})
	require.NoError(t, err)
	input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, body)
	require.Equal(t, "moderate me", input.Text)
	require.Equal(t, []string{
		"https://images.example/last.png", "https://images.example/ref.png", "https://images.example/input.png",
		"data:image/png;base64,dGVzdA==",
	}, input.Images, "remote image roles are sent as URLs, spooled bytes once as a data URI, video/audio never")
}

// 五家平台的 Ark 创建同样先过提示词审核：https 图片引用与 data: URI（经 spool 后内联）都送审，
// 拦截发生在任务创建与预扣之前。
func TestVideoHandlerArkCreatePromptGuardIncludesContentImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	engine := blockingHandlerPromptEngine()
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: arkVideoHandlerTask()}}
	h := newVideoHandler(fake, spool, cfg)
	h.openAI = &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, User: &service.User{ID: 7}, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformByteDance},
		})
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7, Concurrency: 2})
		c.Next()
	})
	router.POST("/api/v3/contents/generations/tasks", h.ArkCreate)
	payload := arkTestPNG(t, color.White)
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{
		"model":"doubao-seedance-1-0-pro-250528",
		"content":[
			{"type":"text","text":"blocked ark prompt"},
			{"type":"image_url","image_url":{"url":"https://images.example/first.png"}},
			{"type":"image_url","image_url":{"url":"`+arkTestDataURI("image/png", payload)+`"},"role":"last_frame"}
		]
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Zero(t, fake.submitCalls, "task creation and balance hold must remain downstream of moderation")
	evaluated, _, requests := engine.snapshot()
	require.Equal(t, 1, evaluated)
	require.Len(t, requests, 1)
	input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, requests[0].Body)
	require.Equal(t, "blocked ark prompt", input.Text)
	require.Equal(t, []string{"https://images.example/first.png", arkTestDataURI("image/png", payload)}, input.Images)
}
