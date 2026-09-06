package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoTaskAPIFake struct {
	submitRequest         service.VideoSubmitRequest
	submitResult          *service.VideoSubmitResult
	submitErr             error
	getTask               *service.VideoTask
	getErr                error
	contentTask           *service.VideoTask
	contentTaskErr        error
	contentURLTask        *service.VideoTask
	contentURLErr         error
	listPage              *service.VideoTaskPage
	listErr               error
	resource              *service.VideoResource
	resourceErr           error
	disclosure            *service.VideoTaskDisclosure
	disclosureErr         error
	resourceDisclosure    *service.VideoResourceDisclosure
	resourceDisclosureErr error
	content               *service.ProviderContent
	contentErr            error
	contentRequest        service.ProviderContentRequest
	contentReference      string
	contentURLReference   string
	videoURL              string
	videoURLErr           error
	deleteTask            *service.VideoTask
	deleteErr             error
	characterErr          error
	submitCalls           int
	getCalls              int
	getUserID             int64
	getPublicID           string
	listUserID            int64
	listFilter            service.VideoTaskFilter
	deleteUserID          int64
	deletePublicID        string
}

type videoMultipartIntentRepositoryFake struct {
	intent service.VideoCreateIntent
}

func (*videoMultipartIntentRepositoryFake) ClaimVideoCreateIntent(context.Context, service.VideoCreateIntentRequest) (*service.VideoCreateIntentClaim, error) {
	return nil, service.ErrVideoInvalidRequest
}
func (*videoMultipartIntentRepositoryFake) RenewVideoCreateIntent(context.Context, service.VideoCreateIntentGuard, time.Duration) error {
	return nil
}
func (*videoMultipartIntentRepositoryFake) ReleasePreparedVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) error {
	return nil
}

func (repo *videoMultipartIntentRepositoryFake) ReadVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) (*service.VideoCreateIntent, error) {
	intent := repo.intent
	return &intent, nil
}
func (*videoMultipartIntentRepositoryFake) QuarantineUntrackedVideoCreateIntent(context.Context, service.VideoCreateIntentGuard) error {
	return nil
}

type videoMultipartIntentAPIFake struct {
	videoTaskAPIFake
	repository  *videoMultipartIntentRepositoryFake
	beginCalls  int
	requestHash string
	contract    string
	guarded     bool
}

func (fake *videoMultipartIntentAPIFake) BeginVideoCreateIntentWithHash(_ context.Context, key *service.APIKey, operation, idempotencyKey, requestHash, contract string) (*service.VideoCreateIntentSession, error) {
	fake.beginCalls++
	fake.requestHash, fake.contract = requestHash, contract
	guard := service.VideoCreateIntentGuard{ID: 91, UserID: key.UserID, APIKeyID: key.ID, Endpoint: service.CompositeRouteEndpointVideos,
		IdempotencyKey: idempotencyKey, LeaseOwner: "multipart-intent-owner", LeaseEpoch: 1}
	fake.repository = &videoMultipartIntentRepositoryFake{intent: service.VideoCreateIntent{ID: guard.ID, UserID: key.UserID,
		APIKeyID: &key.ID, Endpoint: service.CompositeRouteEndpointVideos, State: service.VideoCreateIntentPrepared}}
	return service.NewVideoCreateIntentSession(fake.repository, &service.VideoCreateIntentClaim{Intent: &fake.repository.intent, Guard: guard, Owned: true}), nil
}

func (fake *videoMultipartIntentAPIFake) Submit(ctx context.Context, request service.VideoSubmitRequest) (*service.VideoSubmitResult, error) {
	_, fake.guarded = service.VideoCreateIntentFromContext(ctx)
	fake.submitCalls++
	fake.submitRequest = request
	fake.repository.intent.State = service.VideoCreateIntentNative
	return fake.submitResult, fake.submitErr
}

func (f *videoTaskAPIFake) Submit(_ context.Context, request service.VideoSubmitRequest) (*service.VideoSubmitResult, error) {
	f.submitCalls++
	f.submitRequest = request
	return f.submitResult, f.submitErr
}

func (f *videoTaskAPIFake) GetForOwner(_ context.Context, userID int64, publicID string) (*service.VideoTask, error) {
	f.getCalls++
	f.getUserID = userID
	f.getPublicID = publicID
	return f.getTask, f.getErr
}

func (f *videoTaskAPIFake) GetContentTaskForOwner(_ context.Context, userID int64, reference string) (*service.VideoTask, error) {
	f.getCalls++
	f.getUserID = userID
	f.getPublicID = reference
	if f.contentTask != nil || f.contentTaskErr != nil {
		return f.contentTask, f.contentTaskErr
	}
	return f.getTask, f.getErr
}

func (f *videoTaskAPIFake) GetContentTaskByURLForOwner(_ context.Context, userID int64, requestURI string) (*service.VideoTask, error) {
	f.getCalls++
	f.getUserID = userID
	f.contentURLReference = requestURI
	if f.contentURLTask != nil || f.contentURLErr != nil {
		return f.contentURLTask, f.contentURLErr
	}
	return f.getTask, f.getErr
}

func (f *videoTaskAPIFake) GetContentTaskByURL(_ context.Context, requestURI string) (*service.VideoTask, error) {
	f.getCalls++
	f.contentURLReference = requestURI
	if f.contentURLTask != nil || f.contentURLErr != nil {
		return f.contentURLTask, f.contentURLErr
	}
	return f.getTask, f.getErr
}

func (f *videoTaskAPIFake) VideoURLForOwner(context.Context, int64, string) (string, error) {
	return f.videoURL, f.videoURLErr
}

func (f *videoTaskAPIFake) ListForOwner(_ context.Context, userID int64, filter service.VideoTaskFilter) (*service.VideoTaskPage, error) {
	f.listUserID = userID
	f.listFilter = filter
	return f.listPage, f.listErr
}

func (f *videoTaskAPIFake) GetCharacterForOwner(context.Context, int64, string) (*service.VideoResource, error) {
	return f.resource, f.resourceErr
}

func (f *videoTaskAPIFake) DisclosureForOwner(context.Context, int64, string) (*service.VideoTaskDisclosure, error) {
	if f.disclosure == nil && f.disclosureErr == nil {
		task := f.getTask
		if task == nil && f.submitResult != nil {
			task = f.submitResult.Task
		}
		if task != nil {
			providerTaskID := ""
			if task.ProviderTaskID != nil {
				providerTaskID = *task.ProviderTaskID
			}
			return &service.VideoTaskDisclosure{
				Policy: config.VideoDisclosureIdentity, Provider: task.Provider, ProviderTaskID: providerTaskID,
			}, nil
		}
	}
	return f.disclosure, f.disclosureErr
}

func (f *videoTaskAPIFake) ResourceDisclosureForOwner(context.Context, int64, string) (*service.VideoResourceDisclosure, error) {
	if f.resourceDisclosure == nil && f.resourceDisclosureErr == nil && f.resource != nil {
		return &service.VideoResourceDisclosure{
			Policy: config.VideoDisclosureIdentity, Provider: f.resource.Provider,
			ProviderResourceID: f.resource.ProviderResourceID,
		}, nil
	}
	return f.resourceDisclosure, f.resourceDisclosureErr
}

func (f *videoTaskAPIFake) OpenContentForOwner(_ context.Context, _ int64, reference string, request service.ProviderContentRequest) (*service.ProviderContent, error) {
	f.contentReference = reference
	f.contentRequest = request
	return f.content, f.contentErr
}

func (f *videoTaskAPIFake) OpenContentForTask(_ context.Context, task *service.VideoTask, request service.ProviderContentRequest) (*service.ProviderContent, error) {
	if task != nil {
		f.contentReference = task.PublicID
	}
	f.contentRequest = request
	return f.content, f.contentErr
}

func (f *videoTaskAPIFake) DeleteForOwner(_ context.Context, userID int64, publicID string) (*service.VideoTask, error) {
	f.deleteUserID = userID
	f.deletePublicID = publicID
	return f.deleteTask, f.deleteErr
}

func (f *videoTaskAPIFake) DeleteCharacterForOwner(context.Context, int64, string) error {
	return f.characterErr
}

func TestVideoHandlerProjectsSpecificUpstreamFailure(t *testing.T) {
	code := "content_policy"
	message := "video generation was rejected by content policy"
	task := &service.VideoTask{
		PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 42,
		GenerationState: service.VideoGenerationFailed, BillingState: service.VideoBillingReleased,
		LastErrorCode: &code, LastErrorMessage: &message, CreatedAt: time.Now().UTC(),
	}
	fake := &videoTaskAPIFake{
		getTask:    task,
		disclosure: &service.VideoTaskDisclosure{Policy: config.VideoDisclosureNone},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))

	response, err := handler.projectTask(context.Background(), task.UserID, task)

	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationFailed, response.Status)
	require.NotNil(t, response.Error)
	require.Equal(t, code, response.Error.Code)
	require.Equal(t, message, response.Error.Message)
}

func videoHandlerTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true, CreationEnabled: true, DisclosurePolicy: config.VideoDisclosureIdentity,
		Spool: config.GatewayVideoSpoolConfig{
			Directory: t.TempDir(), MaxPartBytes: 1 << 20, MaxRequestBytes: 2 << 20,
			MaxGlobalBytes: 8 << 20, MaxUserConcurrency: 2, MaxGlobalConcurrency: 4,
			ChunkBytes: 4096, OrphanTTLMinutes: 60,
		},
		ContentProxy: config.GatewayVideoContentProxyConfig{
			Enabled: true, MaxUserConcurrency: 2, MaxAccountConcurrency: 2,
			ResponseHeaderTimeoutSeconds: 5, IdleTimeoutSeconds: 5, TotalTimeoutSeconds: 60,
		},
	}}}
}

func newVideoHandlerTestContext(method, target, contentType string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, body)
	if contentType != "" {
		ctx.Request.Header.Set("Content-Type", contentType)
	}
	groupID := int64(8)
	ctx.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 42, GroupID: &groupID})
	ctx.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 42, Concurrency: 1})
	return ctx, recorder
}

func videoHandlerTask() *service.VideoTask {
	accountID := int64(11)
	providerID := "video_upstream_1"
	return &service.VideoTask{
		ID: 1, PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 42,
		AccountID: &accountID, Provider: service.VideoProviderOpenAI, ProviderTaskID: &providerID,
		Operation: service.VideoOperationGenerate, PublicModel: service.OpenAIVideoModelSora2,
		GenerationState: service.VideoGenerationQueued, BillingState: service.VideoBillingHeld,
		DeleteState: service.VideoDeleteNone, Progress: float64Pointer(0),
		RequestAttributes: map[string]any{"seconds": float64(8), "size": "1280x720"},
		CreatedAt:         time.Unix(1_700_000_000, 0).UTC(),
	}
}

func TestVideoHandlerCreateJSONProjectsSafeTask(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"sora-2","prompt":"A tracking shot","seconds":"8","size":"1280x720",
		"input_reference":{"file_id":"file_123"}
	}`))
	ctx.Request.Header.Set("Idempotency-Key", "idem-1")

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "idem-1", fake.submitRequest.IdempotencyKey)
	require.Equal(t, 8, fake.submitRequest.Seconds)
	require.Equal(t, "file_123", fake.submitRequest.InputReference.FileID)
	require.Empty(t, fake.submitRequest.CharacterIDs)
	require.Contains(t, recorder.Body.String(), `"id":"video_0123456789abcdef0123456789abcdef"`)
	require.Contains(t, recorder.Body.String(), `"provider":"openai"`)
	require.NotContains(t, recorder.Body.String(), "provider_task_id")
	require.NotContains(t, recorder.Body.String(), "request_hash")
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
}

func TestVideoHandlerCreateJSONAcceptsStructuredReferenceVideos(t *testing.T) {
	task := videoHandlerTask()
	task.PublicModel = "doubao-seedance-2.0-mini-480p"
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	referenceURL := "https://media.example.com/reference.mp4?preview=1&auth_key=signed"
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"doubao-seedance-2.0-mini-480p","prompt":"A sports car",
		"seconds":"10","reference_videos":["`+referenceURL+`"]
	}`))

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 10, fake.submitRequest.Seconds)
	require.Equal(t, []string{referenceURL}, fake.submitRequest.ReferenceMedia.ReferenceVideos)
	require.Equal(t, 1, fake.submitCalls)
}

func TestVideoHandlerCreateJSONNormalizesVolcengineContentReferences(t *testing.T) {
	task := videoHandlerTask()
	task.PublicModel = "doubao-seedance-2.0-fast-720p"
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"doubao-seedance-2.0-fast-720p",
		"seconds":10,
		"ratio":"16:9",
		"content":[
			{"type":"text","text":"A sports car accelerates smoothly"},
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://media.example.com/car.png"}},
			{"type":"video_url","role":"reference_video","video_url":"https://media.example.com/motion.mp4"},
			{"type":"audio_url","role":"reference_audio","audio_url":{"url":"data:audio/mpeg;base64,YXVkaW8="}}
		]
	}`))

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "A sports car accelerates smoothly", fake.submitRequest.Prompt)
	require.Equal(t, []string{"https://media.example.com/car.png"}, fake.submitRequest.ReferenceMedia.ReferenceImages)
	require.Equal(t, []string{"https://media.example.com/motion.mp4"}, fake.submitRequest.ReferenceMedia.ReferenceVideos)
	require.Equal(t, []string{"data:audio/mpeg;base64,YXVkaW8="}, fake.submitRequest.ReferenceMedia.ReferenceAudios)
	require.Empty(t, fake.submitRequest.ReferenceMedia.Ratio, "reference video determines framing")
}

func TestVideoHandlerCreateJSONNormalizesVolcengineFirstAndLastFrames(t *testing.T) {
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: videoHandlerTask(), Created: true}}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"doubao-seedance-2.0-mini-480p","seconds":"8",
		"content":[
			{"type":"text","text":"Transition from day to night"},
			{"type":"image_url","role":"first_frame","image_url":"https://media.example.com/first.png"},
			{"type":"image_url","role":"last_frame","image_url":{"url":"https://media.example.com/last.png"}}
		]
	}`))

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "https://media.example.com/first.png", fake.submitRequest.ReferenceMedia.FirstImageURL)
	require.Equal(t, "https://media.example.com/last.png", fake.submitRequest.ReferenceMedia.LastImageURL)
}

func TestVideoHandlerRejectsVolcengineContentConflictsAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"doubao-seedance-2.0-mini-480p","prompt":"top-level","seconds":8,"content":[{"type":"text","text":"content"}]}`,
		`{"model":"doubao-seedance-2.0-mini-480p","prompt":"top-level","seconds":8,"content":[]}`,
		`{"model":"doubao-seedance-2.0-mini-480p","prompt":"top-level","seconds":8,"content":null}`,
		`{"model":"doubao-seedance-2.0-mini-480p","seconds":8,"content":[{"type":"text","text":"content","unknown":true}]}`,
		`{"model":"doubao-seedance-2.0-mini-480p","seconds":8,"content":[{"type":"video_url","role":"first_frame","video_url":"https://media.example.com/ref.mp4"}]}`,
	} {
		fake := &videoTaskAPIFake{}
		ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(body))
		newVideoHandler(fake, nil, videoHandlerTestConfig(t)).Create(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		require.Zero(t, fake.submitCalls)
	}
}

func TestVideoHandlerEditJSONUsesOfficialEditContract(t *testing.T) {
	task := videoHandlerTask()
	task.Operation = service.VideoOperationEdit
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos/edits", "application/json", strings.NewReader(`{
		"prompt":"Change the car paint","video":{"id":"video_72c01ea0467147f393daf5326901f12a"}
	}`))

	handler.Edit(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, service.VideoOperationEdit, fake.submitRequest.Operation)
	require.Equal(t, "video_72c01ea0467147f393daf5326901f12a", fake.submitRequest.SourceVideoID)
	require.Equal(t, 1, fake.submitCalls)
}

func TestVideoHandlerRetrieveListAndDeleteContracts(t *testing.T) {
	task := videoHandlerTask()

	t.Run("retrieve", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: task}
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID, "", nil)
		ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}

		handler.Retrieve(ctx)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, int64(42), fake.getUserID)
		require.Equal(t, task.PublicID, fake.getPublicID)
		require.Contains(t, recorder.Body.String(), `"id":"`+task.PublicID+`"`)
		require.Contains(t, recorder.Body.String(), `"status":"queued"`)
		require.Equal(t, "10", recorder.Header().Get("Retry-After"))
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})

	t.Run("list", func(t *testing.T) {
		fake := &videoTaskAPIFake{getTask: task, listPage: &service.VideoTaskPage{Data: []*service.VideoTask{task}, HasMore: true, After: "next"}}
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos?limit=5&order=asc&status=queued&model=sora-2&operation=generate&after=cursor", "", nil)

		handler.List(ctx)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, int64(42), fake.listUserID)
		require.Equal(t, service.VideoTaskFilter{
			Status: "queued", Model: "sora-2", Operation: "generate", Limit: 5, After: "cursor", Order: "asc",
		}, fake.listFilter)
		require.Contains(t, recorder.Body.String(), `"object":"list"`)
		require.Contains(t, recorder.Body.String(), `"first_id":"`+task.PublicID+`"`)
		require.Contains(t, recorder.Body.String(), `"last_id":"`+task.PublicID+`"`)
		require.Contains(t, recorder.Body.String(), `"has_more":true`)
		require.Contains(t, recorder.Body.String(), `"after":"next"`)
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})

	t.Run("delete", func(t *testing.T) {
		deleted := *task
		deleted.DeleteState = service.VideoDeleteDeleted
		fake := &videoTaskAPIFake{deleteTask: &deleted}
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newVideoHandlerTestContext(http.MethodDelete, "/v1/videos/"+task.PublicID, "", nil)
		ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}

		handler.Delete(ctx)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, int64(42), fake.deleteUserID)
		require.Equal(t, task.PublicID, fake.deletePublicID)
		require.JSONEq(t, `{"id":"`+task.PublicID+`","object":"video.deleted","deleted":true}`, recorder.Body.String())
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	})
	t.Run("pending delete does not claim success", func(t *testing.T) {
		fake := &videoTaskAPIFake{deleteTask: task}
		handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
		ctx, recorder := newVideoHandlerTestContext(http.MethodDelete, "/v1/videos/"+task.PublicID, "", nil)
		ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
		handler.Delete(ctx)
		require.Equal(t, http.StatusConflict, recorder.Code)
		require.Contains(t, recorder.Body.String(), "video_delete_pending")
		require.NotContains(t, recorder.Body.String(), `"deleted":true`)
		require.Equal(t, "3", recorder.Header().Get("Retry-After"))
	})
}

func TestVideoHandlerCompletedTaskIncludesLocalContentURL(t *testing.T) {
	task := videoHandlerTask()
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCaptured
	task.ContentVariants = []string{"thumbnail", "video"}
	fake := &videoTaskAPIFake{
		getTask:  task,
		videoURL: "https://video-upstream.example/v1/videos/video_upstream_1/content?token=signed&disposition=inline",
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID, "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	ctx.Request.Host = "api.current.example"
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")

	handler.Retrieve(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "token=signed&disposition=inline")
	require.NotContains(t, recorder.Body.String(), `\u0026`)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "https://api.current.example/v1/videos/video_upstream_1/content?token=signed&disposition=inline", response["url"])
	require.NotContains(t, response, "provider_task_id")
}

func TestVideoHandlerContentAcceptsRewrittenProviderTaskPath(t *testing.T) {
	task := videoHandlerTask()
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCaptured
	fake := &videoTaskAPIFake{
		getTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("video-data")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/video_upstream_1/content?token=signed&variant=download", "", nil)
	ctx.Params = gin.Params{{Key: "request_id", Value: "video_upstream_1"}}

	handler.Content(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video-data", recorder.Body.String())
	require.Equal(t, "video_upstream_1", fake.getPublicID)
	require.Equal(t, "video_upstream_1", fake.contentReference)
	require.Equal(t, "video", fake.contentRequest.Variant)
	require.Equal(t, `attachment; filename="video_0123456789abcdef0123456789abcdef.mp4"`, recorder.Header().Get("Content-Disposition"))
}

func TestVideoHandlerContentURLProxiesArbitraryUpstreamPath(t *testing.T) {
	task := videoHandlerTask()
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCaptured
	fake := &videoTaskAPIFake{
		getTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("arbitrary-video")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	requestURI := "/assets/render/final.mp4?token=signed&variant=download"
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, requestURI, "", nil)
	ctx.Set(string(servermiddleware.ContextKeyAPIKey), nil)
	ctx.Set(string(servermiddleware.ContextKeyUser), nil)

	handler.ContentURL(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "arbitrary-video", recorder.Body.String())
	require.Equal(t, requestURI, fake.contentURLReference)
	require.Equal(t, task.PublicID, fake.contentReference)
	require.Equal(t, "video", fake.contentRequest.Variant)
	require.Equal(t, `inline; filename="video_0123456789abcdef0123456789abcdef.mp4"`, recorder.Header().Get("Content-Disposition"))
}

func TestVideoHandlerPublicContentProxyRunsBeforeMatchedOrFrontendRoutes(t *testing.T) {
	task := videoHandlerTask()
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCaptured
	fake := &videoTaskAPIFake{
		contentURLTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("middleware-video")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	router := gin.New()
	router.Use(handler.PublicContentProxy)
	fallbackCalled := false
	router.GET("/1fbe6da3be7446b2af1602ca2a2feeea", func(c *gin.Context) {
		fallbackCalled = true
		c.Status(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/1fbe6da3be7446b2af1602ca2a2feeea?preview=1&auth_key=signed", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "middleware-video", recorder.Body.String())
	require.False(t, fallbackCalled)
	require.Equal(t, req.URL.RequestURI(), fake.contentURLReference)
	require.Equal(t, "inline", strings.Split(recorder.Header().Get("Content-Disposition"), ";")[0])
}

func TestVideoHandlerPublicContentProxyContinuesWhenURLIsUnknown(t *testing.T) {
	fake := &videoTaskAPIFake{contentURLErr: service.ErrVideoTaskNotFound}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	router := gin.New()
	router.Use(handler.PublicContentProxy)
	router.GET("/dashboard", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestVideoHandlerRetrieveFallsBackToMatchedProviderVideoURL(t *testing.T) {
	task := videoHandlerTask()
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCaptured
	fake := &videoTaskAPIFake{
		getErr:         service.ErrVideoTaskNotFound,
		contentTask:    task,
		contentURLTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("route-collision-video")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	requestURI := "/v1/videos/video_upstream_1?token=signed"
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, requestURI, "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "video_upstream_1"}}

	handler.Retrieve(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "route-collision-video", recorder.Body.String())
	require.Equal(t, requestURI, fake.contentURLReference)
	require.Equal(t, task.PublicID, fake.contentReference)
}

func TestVideoHandlerTaskOmitsContentURLUntilItIsAvailable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*service.VideoTask, *config.Config)
	}{
		{name: "not completed", mutate: func(task *service.VideoTask, _ *config.Config) {
			task.GenerationState = service.VideoGenerationInProgress
			task.ContentVariants = []string{"video"}
		}},
		{name: "video variant missing", mutate: func(task *service.VideoTask, _ *config.Config) {
			task.GenerationState = service.VideoGenerationCompleted
			task.BillingState = service.VideoBillingCaptured
			task.ContentVariants = []string{"thumbnail"}
		}},
		{name: "content proxy disabled", mutate: func(task *service.VideoTask, cfg *config.Config) {
			task.GenerationState = service.VideoGenerationCompleted
			task.BillingState = service.VideoBillingCaptured
			task.ContentVariants = []string{"video"}
			cfg.Gateway.Video.ContentProxy.Enabled = false
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := videoHandlerTask()
			cfg := videoHandlerTestConfig(t)
			test.mutate(task, cfg)
			fake := &videoTaskAPIFake{getTask: task}
			handler := newVideoHandler(fake, nil, cfg)
			ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID, "", nil)
			ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}

			handler.Retrieve(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.NotContains(t, response, "url")
		})
	}
}

func TestVideoHandlerCreationFlagDoesNotDisableExistingTaskReads(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{getTask: task}
	cfg := videoHandlerTestConfig(t)
	cfg.Gateway.Video.CreationEnabled = false
	handler := newVideoHandler(fake, nil, cfg)

	createCtx, createRecorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{"model":"sora-2","prompt":"waves"}`))
	handler.Create(createCtx)
	require.Equal(t, http.StatusForbidden, createRecorder.Code)
	require.Contains(t, createRecorder.Body.String(), "video_creation_disabled")
	require.Zero(t, fake.submitCalls)

	retrieveCtx, retrieveRecorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID, "", nil)
	retrieveCtx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	handler.Retrieve(retrieveCtx)
	require.Equal(t, http.StatusOK, retrieveRecorder.Code)
	require.Equal(t, 1, fake.getCalls)
}

func TestVideoHandlerGlobalFlagDisablesExistingTaskReads(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{getTask: task}
	cfg := videoHandlerTestConfig(t)
	cfg.Gateway.Video.Enabled = false
	handler := newVideoHandler(fake, nil, cfg)
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID, "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}

	handler.Retrieve(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "video_disabled")
	require.Zero(t, fake.getCalls)
}

func TestVideoHandlerCreateMultipartAcceptsFileBeforeScalars(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, spool, cfg)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("input_reference", "frame.jpg")
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(file, image.NewNRGBA(image.Rect(0, 0, 1280, 720)), nil))
	require.NoError(t, writer.WriteField("prompt", "Camera pans right"))
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("seconds", "8"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, fake.submitCalls)
	require.Len(t, fake.submitRequest.Inputs, 1)
	require.Equal(t, service.VideoInputRoleReferenceImage, fake.submitRequest.Inputs[0].Role)
	require.Equal(t, "image/jpeg", fake.submitRequest.Inputs[0].MIMEType)
	reader, err := fake.submitRequest.Inputs[0].Open(context.Background())
	require.Error(t, err)
	require.Nil(t, reader, "spool readers must be invalid after the handler returns")
}

func TestVideoHandlerMultipartClaimsSharedIntentAfterEncryptedSpooling(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	task := videoHandlerTask()
	fake := &videoMultipartIntentAPIFake{videoTaskAPIFake: videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}}
	handler := newVideoHandler(fake, spool, cfg)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("input_reference", "frame.jpg")
	require.NoError(t, err)
	var imageBody bytes.Buffer
	require.NoError(t, jpeg.Encode(&imageBody, image.NewNRGBA(image.Rect(0, 0, 16, 16)), nil))
	_, err = file.Write(imageBody.Bytes())
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "Camera pans right"))
	require.NoError(t, writer.Close())
	contentType, raw := writer.FormDataContentType(), append([]byte(nil), body.Bytes()...)
	imageDigest := sha256.Sum256(imageBody.Bytes())
	modelDigest := sha256.Sum256([]byte("sora-2"))
	promptDigest := sha256.Sum256([]byte("Camera pans right"))
	expectedHash, err := service.CanonicalVideoCreateMultipartPartsHash([]service.VideoCreateMultipartPart{
		{Name: "input_reference", File: true, Filename: "frame.jpg", ContentType: "application/octet-stream", Size: int64(imageBody.Len()), Digest: hex.EncodeToString(imageDigest[:])},
		{Name: "model", Size: 6, Digest: hex.EncodeToString(modelDigest[:])},
		{Name: "prompt", Size: 17, Digest: hex.EncodeToString(promptDigest[:])},
	})
	require.NoError(t, err)
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", contentType, bytes.NewReader(raw))
	key, ok := servermiddleware.GetAPIKeyFromContext(ctx)
	require.True(t, ok)
	key.Group = &service.Group{ID: 8, Platform: service.PlatformOpenAI}
	key.User = &service.User{ID: key.UserID, Status: service.StatusActive}
	ctx.Request.Header.Set("Idempotency-Key", "multipart-intent")

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, fake.beginCalls)
	require.Equal(t, expectedHash, fake.requestHash)
	require.Equal(t, service.VideoCreateIntentMultipartContract, fake.contract)
	require.True(t, fake.guarded)
	require.Equal(t, "91", recorder.Header().Get("X-Video-Create-Intent"))
	require.Equal(t, 1, fake.submitCalls)
}

func TestVideoHandlerMultipartRejectsMalformedOrOversizedParts(t *testing.T) {
	for _, scenario := range []string{"mixed_scalar_file", "transfer_encoding", "duplicate_header", "unknown_header", "missing_name", "empty_filename", "unknown_attribute", "scalar_limit", "file_limit", "part_limit", "truncated", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := videoHandlerTestConfig(t)
			spool, err := service.NewVideoSubmissionSpool(cfg)
			require.NoError(t, err)
			handler := newVideoHandler(&videoTaskAPIFake{}, spool, cfg)
			header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="prompt"`}}
			value := "waves"
			expected := service.ErrVideoInvalidRequest
			switch scenario {
			case "mixed_scalar_file":
				header.Set("Content-Disposition", `form-data; name="model"; filename="frame.png"`)
			case "transfer_encoding":
				header.Set("Content-Transfer-Encoding", "quoted-printable")
			case "duplicate_header":
				header.Add("Content-Disposition", `form-data; name="seconds"`)
			case "unknown_header":
				header.Set("Content-Encoding", "gzip")
			case "missing_name":
				header.Set("Content-Disposition", "form-data")
			case "empty_filename":
				header.Set("Content-Disposition", `form-data; name="input_reference"; filename=""`)
			case "unknown_attribute":
				header.Set("Content-Disposition", `form-data; name="prompt"; extra="value"`)
			case "scalar_limit":
				value = strings.Repeat("x", service.VideoCreateMultipartScalarMaxBytes+1)
			case "file_limit":
				header.Set("Content-Disposition", `form-data; name="input_reference"; filename="frame.png"`)
				value = strings.Repeat("x", int(cfg.Gateway.Video.Spool.MaxPartBytes)+1)
				expected = service.ErrVideoInputTooLarge
			case "part_limit":
				header.Set("Content-Disposition", `form-data; name="input_reference"; filename="frame.png"`)
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if scenario != "empty" {
				require.NoError(t, writer.WriteField("model", "sora-2"))
				count := 1
				if scenario == "part_limit" {
					count = service.VideoCreateMultipartMaxParts
				}
				for i := 0; i < count; i++ {
					part, err := writer.CreatePart(header)
					require.NoError(t, err)
					_, err = io.WriteString(part, value)
					require.NoError(t, err)
				}
			}
			require.NoError(t, writer.Close())
			raw := body.Bytes()
			if scenario == "truncated" {
				raw = raw[:len(raw)-20]
			}
			ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), bytes.NewReader(raw))
			_, session, _, err := handler.parseSubmitRequest(ctx, service.VideoOperationGenerate, 7)
			require.ErrorIs(t, err, expected)
			require.Nil(t, session, "failed parsing must close the encrypted spool")
		})
	}
}

func TestVideoGrokMultipartBodyStreamsParsedInputs(t *testing.T) {
	content := []byte("private-video")
	request := service.VideoSubmitRequest{Operation: service.VideoOperationEdit, Model: "grok-upstream", Prompt: "edit it", Inputs: []service.VideoInput{{
		VideoInputManifestEntry: service.VideoInputManifestEntry{Role: service.VideoInputRoleSourceVideo, FileName: "source.mp4", MIMEType: "video/mp4", Size: int64(len(content))},
		Open:                    func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil },
	}}}
	body, contentType, err := videoGrokMultipartBody(context.Background(), request)
	require.NoError(t, err)
	defer func() { require.NoError(t, body.Close()) }()
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(body, parameters["boundary"])
	fields := map[string]string{}
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		require.NoError(t, partErr)
		data, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		fields[part.FormName()] = string(data)
		require.NoError(t, part.Close())
	}
	require.Equal(t, "grok-upstream", fields["model"])
	require.Equal(t, "edit it", fields["prompt"])
	require.Equal(t, string(content), fields["video"])
}

func TestVideoHandlerCreateMultipartAcceptsProviderDefinedBinaryRole(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	task := videoHandlerTask()
	task.Operation = service.VideoOperationGenerate
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("depth_map", "depth.bin")
	require.NoError(t, err)
	_, err = file.Write([]byte{0, 1, 2, 3})
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("model", service.OpenAIVideoModelSora2))
	require.NoError(t, writer.WriteField("prompt", "Apply the depth map"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, service.VideoOperationGenerate, fake.submitRequest.Operation)
	require.Len(t, fake.submitRequest.Inputs, 1)
	require.Equal(t, service.VideoInputRole("depth_map"), fake.submitRequest.Inputs[0].Role)
}

func TestVideoHandlerMultipartAllowsRepeatedProviderBinaryRole(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	task := videoHandlerTask()
	task.Operation = service.VideoOperationGenerate
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, filename := range []string{"near.bin", "far.bin"} {
		file, createErr := writer.CreateFormFile("depth_map", filename)
		require.NoError(t, createErr)
		_, writeErr := file.Write([]byte{0, 1, 2, 3})
		require.NoError(t, writeErr)
	}
	require.NoError(t, writer.WriteField("model", service.OpenAIVideoModelSora2))
	require.NoError(t, writer.WriteField("prompt", "Use both depth layers"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, fake.submitRequest.Inputs, 2)
	require.Equal(t, service.VideoInputRole("depth_map"), fake.submitRequest.Inputs[0].Role)
	require.Equal(t, service.VideoInputRole("depth_map"), fake.submitRequest.Inputs[1].Role)
}

func TestVideoHandlerMultipartStillRejectsRepeatedScalarField(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	fake := &videoTaskAPIFake{}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "first"))
	require.NoError(t, writer.WriteField("prompt", "second"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, fake.submitCalls)
}

func TestVideoHandlerMultipartRejectsDuplicateKeysInsideJSONScalar(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	fake := &videoTaskAPIFake{}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", service.OpenAIVideoModelSora2))
	require.NoError(t, writer.WriteField("prompt", "test"))
	require.NoError(t, writer.WriteField("provider_options", `{"seed":1,"seed":2}`))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, fake.submitCalls)
	require.Contains(t, recorder.Body.String(), "video_invalid_request")
}

func TestVideoHandlerCharacterProjectionHonorsResourceDisclosurePolicy(t *testing.T) {
	resource := &service.VideoResource{
		ID: 2, PublicID: "char_cccccccccccccccccccccccccccccccc", UserID: 42,
		Provider: service.VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream_secret",
		Status: "ready", Metadata: map[string]any{"name": "Mossy"}, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	fake := &videoTaskAPIFake{
		resource:           resource,
		resourceDisclosure: &service.VideoResourceDisclosure{Policy: config.VideoDisclosureNone},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	value, err := handler.projectResource(context.Background(), resource.UserID, resource)
	require.NoError(t, err)
	require.Equal(t, resource.PublicID, value.ID)
	require.Empty(t, value.ProviderResourceID)
	require.Empty(t, value.Provider)
}

func TestVideoHandlerUnsettledCharacterProjectionIsOnlyPendingIdentity(t *testing.T) {
	resource := &service.VideoResource{PublicID: service.NewVideoResourceID(), UserID: 42, Status: "ready",
		ProviderResourceID: "char_upstream_secret", Metadata: map[string]any{"name": "private-name"}}
	fake := &videoTaskAPIFake{resourceDisclosureErr: service.ErrVideoSettlementPending}
	value, err := newVideoHandler(fake, nil, videoHandlerTestConfig(t)).projectResource(context.Background(), 42, resource)
	require.NoError(t, err)
	require.Equal(t, "creating", value.Status)
	require.Equal(t, resource.PublicID, value.ID)
	require.Empty(t, value.ProviderResourceID)
	require.Empty(t, value.Metadata)
	require.Empty(t, value.Name)
}

func TestVideoHandlerCharacterDeletionReturnsPendingWithRetry(t *testing.T) {
	fake := &videoTaskAPIFake{characterErr: service.ErrVideoDeletePending}
	ctx, recorder := newVideoHandlerTestContext(http.MethodDelete, "/v1/videos/characters/char_test", "", nil)
	ctx.Params = gin.Params{{Key: "character_id", Value: "char_test"}}
	newVideoHandler(fake, nil, videoHandlerTestConfig(t)).DeleteCharacter(ctx)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, "3", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), "video_delete_pending")
}

func TestVideoHandlerRejectsUnknownJSONFieldBeforeSubmit(t *testing.T) {
	fake := &videoTaskAPIFake{}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"sora-2","prompt":"x","unexpected":true
	}`))

	handler.Create(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, fake.submitCalls)
	require.Contains(t, recorder.Body.String(), "video_invalid_request")
}

func TestVideoHandlerRejectsDuplicateJSONKeysBeforeSubmit(t *testing.T) {
	fake := &videoTaskAPIFake{}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", "application/json", strings.NewReader(`{
		"model":"sora-2","prompt":"first","prompt":"second"
	}`))

	handler.Create(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, fake.submitCalls)
	require.Contains(t, recorder.Body.String(), "video_invalid_request")
}

func TestVideoHandlerRejectsUnsafeMultipartRoleBeforeSubmit(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	fake := &videoTaskAPIFake{}
	handler := newVideoHandler(fake, spool, cfg)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("attachment-name", "payload.bin")
	require.NoError(t, err)
	_, err = file.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos", writer.FormDataContentType(), &body)

	handler.Create(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, fake.submitCalls)
	require.Contains(t, recorder.Body.String(), "video_input_unsupported")
}

func TestVideoModerationBodyIncludesRemoteAndUploadedReferenceImages(t *testing.T) {
	t.Run("remote image URL", func(t *testing.T) {
		body, err := videoModerationBody(context.Background(), service.VideoSubmitRequest{
			Prompt:         "reference prompt",
			InputReference: &service.ProviderInputReference{ImageURL: "https://images.example/reference.png"},
		})
		require.NoError(t, err)
		input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, body)
		require.Equal(t, "reference prompt", input.Text)
		require.Equal(t, []string{"https://images.example/reference.png"}, input.Images)
	})

	t.Run("encrypted spool reader", func(t *testing.T) {
		body, err := videoModerationBody(context.Background(), service.VideoSubmitRequest{
			Prompt: "uploaded prompt",
			Inputs: []service.VideoInput{{
				VideoInputManifestEntry: service.VideoInputManifestEntry{
					Role: service.VideoInputRoleReferenceImage, MIMEType: "image/png", Size: 4,
				},
				Open: func(context.Context) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("test")), nil
				},
			}},
		})
		require.NoError(t, err)
		input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, body)
		require.Equal(t, "uploaded prompt", input.Text)
		require.Equal(t, []string{"data:image/png;base64,dGVzdA=="}, input.Images)
	})
}

func TestVideoModerationBodyRejectsOversizedImageBeforeOpeningIt(t *testing.T) {
	opened := false
	_, err := videoModerationBody(context.Background(), service.VideoSubmitRequest{
		Prompt: "oversized",
		Inputs: []service.VideoInput{{
			VideoInputManifestEntry: service.VideoInputManifestEntry{
				Role: service.VideoInputRoleReferenceImage, MIMEType: "image/png",
				Size: service.MaxContentModerationImageBytes + 1,
			},
			Open: func(context.Context) (io.ReadCloser, error) {
				opened = true
				return http.NoBody, nil
			},
		}},
	})
	require.ErrorIs(t, err, service.ErrVideoInputTooLarge)
	require.False(t, opened)
}

func TestVideoHandlerContentFiltersHeadersAndForwardsRange(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{
		getTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Type":  {"video/mp4"},
				"Content-Range": {"bytes 0-3/8"},
				"Set-Cookie":    {"secret=1"},
				"Authorization": {"Bearer secret"},
				"X-Request-Id":  {"upstream-secret"},
			},
			Body: io.NopCloser(strings.NewReader("data")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID+"/content", "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	ctx.Request.Header.Set("Range", "bytes=0-3")

	handler.Content(ctx)

	require.Equal(t, http.StatusPartialContent, ctx.Writer.Status())
	require.Equal(t, "data", recorder.Body.String())
	require.Equal(t, "bytes=0-3", fake.contentRequest.Range)
	require.Equal(t, 5*time.Second, fake.contentRequest.ResponseHeaderTimeout)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Empty(t, recorder.Header().Get("Authorization"))
	require.Empty(t, recorder.Header().Get("X-Request-Id"))
	require.Equal(t, `attachment; filename="video_0123456789abcdef0123456789abcdef.mp4"`, recorder.Header().Get("Content-Disposition"))
}

func TestVideoHandlerContentHeadForwardsIfRangeWithoutWritingBody(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{
		getTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Type":   {"video/mp4"},
				"Content-Range":  {"bytes 0-3/8"},
				"Content-Length": {"4"},
			},
			Body: io.NopCloser(strings.NewReader("must-not-be-written")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodHead, "/v1/videos/"+task.PublicID+"/content?variant=thumbnail", "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	ctx.Request.Header.Set("Range", "bytes=0-3")
	ctx.Request.Header.Set("If-Range", `"etag-1"`)

	handler.Content(ctx)

	require.Equal(t, http.StatusPartialContent, ctx.Writer.Status())
	require.Empty(t, recorder.Body.String())
	require.Equal(t, http.MethodHead, fake.contentRequest.Method)
	require.Equal(t, "thumbnail", fake.contentRequest.Variant)
	require.Equal(t, "bytes=0-3", fake.contentRequest.Range)
	require.Equal(t, `"etag-1"`, fake.contentRequest.IfRange)
	require.Equal(t, "bytes 0-3/8", recorder.Header().Get("Content-Range"))
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
}

func TestVideoHandlerContentPreservesProviderRangeNotSatisfiable(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{
		getTask: task,
		content: &service.ProviderContent{
			StatusCode: http.StatusRequestedRangeNotSatisfiable,
			Header:     http.Header{"Content-Range": {"bytes */8"}},
			Body:       io.NopCloser(strings.NewReader("range not satisfiable")),
		},
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID+"/content", "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	ctx.Request.Header.Set("Range", "bytes=99-100")

	handler.Content(ctx)

	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, recorder.Code)
	require.Equal(t, "bytes */8", recorder.Header().Get("Content-Range"))
	require.Equal(t, "range not satisfiable", recorder.Body.String())
}

type blockingVideoContentBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingVideoContentBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingVideoContentBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestCopyVideoContentWithIdleTimeoutCancelsAndClosesSource(t *testing.T) {
	body := &blockingVideoContentBody{closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	var destination bytes.Buffer

	written, err := copyVideoContentWithIdleTimeout(ctx, &destination, body, cancel, 20*time.Millisecond)

	require.ErrorIs(t, err, errVideoContentIdleTimeout)
	require.Zero(t, written)
	select {
	case <-body.closed:
	default:
		t.Fatal("content source was not closed after idle timeout")
	}
}

func TestCopyVideoContentWithIdleTimeoutStreamsWithoutBufferingWholeBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var destination bytes.Buffer
	body := io.NopCloser(strings.NewReader(strings.Repeat("video", 32*1024)))

	written, err := copyVideoContentWithIdleTimeout(ctx, &destination, body, cancel, time.Second)

	require.NoError(t, err)
	require.Equal(t, int64(destination.Len()), written)
	require.Equal(t, strings.Repeat("video", 32*1024), destination.String())
}

type failingVideoContentWriter struct{}

func (failingVideoContentWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCopyVideoContentClassifiesReadAndWriteFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, readErr := copyVideoContentWithIdleTimeout(ctx, io.Discard, io.NopCloser(io.MultiReader(strings.NewReader("video"), errorReader{err: io.ErrUnexpectedEOF})), cancel, time.Second)
	require.ErrorIs(t, readErr, errVideoContentUpstream)
	require.Equal(t, "upstream_error", videoContentStreamResult(readErr, ctx))

	_, writeErr := copyVideoContentWithIdleTimeout(ctx, failingVideoContentWriter{}, io.NopCloser(strings.NewReader("video")), cancel, time.Second)
	require.ErrorIs(t, writeErr, errVideoContentDownstream)
	require.Equal(t, "downstream_error", videoContentStreamResult(writeErr, ctx))
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestVideoHandlerContentRejectsMultipleRanges(t *testing.T) {
	task := videoHandlerTask()
	fake := &videoTaskAPIFake{getTask: task}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
	ctx, recorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+task.PublicID+"/content", "", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: task.PublicID}}
	ctx.Request.Header.Set("Range", "bytes=0-1,4-5")

	handler.Content(ctx)

	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "video_invalid_range")
}

func float64Pointer(value float64) *float64 { return &value }
