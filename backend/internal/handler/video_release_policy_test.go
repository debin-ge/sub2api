package handler

import (
	"bytes"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoReleaseAllowsExtensionAndCharacterReads(t *testing.T) {
	task := videoHandlerTask()
	task.Operation = service.VideoOperationExtend
	resource := &service.VideoResource{
		PublicID: "char_cccccccccccccccccccccccccccccccc", UserID: 42,
		Provider: service.VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream",
		Status: "ready", Metadata: map[string]any{"name": "Mossy"}, CreatedAt: task.CreatedAt,
	}
	fake := &videoTaskAPIFake{
		submitResult: &service.VideoSubmitResult{Task: task, Created: true},
		resource:     resource,
	}
	handler := newVideoHandler(fake, nil, videoHandlerTestConfig(t))

	extendContext, extendRecorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos/extensions", "application/json", strings.NewReader(`{
		"video":{"id":"video_72c01ea0467147f393daf5326901f12a"},"prompt":"Continue","seconds":8
	}`))
	handler.Extend(extendContext)
	require.Equal(t, http.StatusOK, extendRecorder.Code, extendRecorder.Body.String())
	require.Equal(t, service.VideoOperationExtend, fake.submitRequest.Operation)

	getContext, getRecorder := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/characters/"+resource.PublicID, "", nil)
	getContext.Params = gin.Params{{Key: "character_id", Value: resource.PublicID}}
	handler.GetCharacter(getContext)
	require.Equal(t, http.StatusOK, getRecorder.Code, getRecorder.Body.String())
	require.Contains(t, getRecorder.Body.String(), resource.PublicID)
}

func TestVideoReleaseAllowsUploadedEdit(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	task := videoHandlerTask()
	task.Operation = service.VideoOperationEdit
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Task: task, Created: true}}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("video", "source.mp4")
	require.NoError(t, err)
	_, err = file.Write([]byte("video"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("model", service.OpenAIVideoModelSora2Pro))
	require.NoError(t, writer.WriteField("prompt", "Change the palette"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos/edits", writer.FormDataContentType(), &body)

	handler.Edit(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, service.VideoOperationEdit, fake.submitRequest.Operation)
	require.Len(t, fake.submitRequest.Inputs, 1)
	require.Equal(t, service.VideoInputRoleSourceVideo, fake.submitRequest.Inputs[0].Role)
}

func TestVideoReleaseAllowsCharacterCreationMultipart(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	spool, err := service.NewVideoSubmissionSpool(cfg)
	require.NoError(t, err)
	resource := &service.VideoResource{
		PublicID: "char_cccccccccccccccccccccccccccccccc", UserID: 42,
		Provider: service.VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream",
		Status: "ready", Metadata: map[string]any{"name": "Mossy"},
	}
	fake := &videoTaskAPIFake{submitResult: &service.VideoSubmitResult{Resource: resource, Created: true}, resource: resource}
	handler := newVideoHandler(fake, spool, cfg)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("video", "character.mp4")
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(file, image.NewNRGBA(image.Rect(0, 0, 4, 4)), nil))
	require.NoError(t, writer.WriteField("name", "Mossy"))
	require.NoError(t, writer.Close())
	ctx, recorder := newVideoHandlerTestContext(http.MethodPost, "/v1/videos/characters", writer.FormDataContentType(), &body)

	handler.CreateCharacter(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, service.VideoOperationCharacterCreate, fake.submitRequest.Operation)
	require.Len(t, fake.submitRequest.Inputs, 1)
}
