package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newPendingImageHandlerTest(t *testing.T, timeout time.Duration) (*AsyncImageHandler, *gin.Engine, *redis.Client, service.ImageTaskStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := repository.NewImageTaskStore(rdb)
	h := &AsyncImageHandler{
		tasks:   service.NewImageTaskServiceWithUploader(store, nil, time.Hour, timeout),
		cfg:     &config.Config{Gateway: config.GatewayConfig{ImagesMaxPendingPerUser: 1}},
		execute: func(_ string, c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": []any{}}) },
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, Group: &service.Group{Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	return h, router, rdb, store
}

func submitPendingImage(router http.Handler, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func assertPendingImageFinished(t *testing.T, h *AsyncImageHandler, rdb *redis.Client, w *httptest.ResponseRecorder, status string) {
	t.Helper()
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var accepted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.Eventually(t, func() bool {
		task, err := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.ID)
		return err == nil && task.Status == status && rdb.ZCard(context.Background(), "image_task:pending:7").Val() == 0
	}, 5*time.Second, 5*time.Millisecond)
}

func TestAsyncImagePendingReleasedOnPreflightRejection(t *testing.T) {
	for _, body := range []string{"", `{"stream":true}`} {
		t.Run(body, func(t *testing.T) {
			_, router, rdb, _ := newPendingImageHandlerTest(t, time.Minute)
			w := submitPendingImage(router, strings.NewReader(body))
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Equal(t, int64(0), rdb.ZCard(context.Background(), "image_task:pending:7").Val())
		})
	}
}

type pendingImageBlockingBody struct {
	done chan struct{}
	once sync.Once
}

func (b *pendingImageBlockingBody) Read([]byte) (int, error) {
	<-b.done
	return 0, errors.New("body closed")
}
func (b *pendingImageBlockingBody) Close() error { b.once.Do(func() { close(b.done) }); return nil }

func TestAsyncImagePendingDeadlineInterruptsBody(t *testing.T) {
	_, router, rdb, _ := newPendingImageHandlerTest(t, time.Second)
	body := &pendingImageBlockingBody{done: make(chan struct{})}
	t.Cleanup(func() { _ = body.Close() })
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { finished <- submitPendingImage(router, body) }()
	select {
	case response := <-finished:
		require.Equal(t, http.StatusRequestTimeout, response.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("request body remained blocked past its admission deadline")
	}
	require.Equal(t, int64(0), rdb.ZCard(context.Background(), "image_task:pending:7").Val())
}

type pendingImageCommitFailureStore struct {
	service.ImageTaskStore
	service.ImageTaskPendingStore
	service.ImageTaskTerminalStore
}

func (s pendingImageCommitFailureStore) CommitImageTaskPending(context.Context, *service.ImageTaskRecord, time.Duration) error {
	return errors.New("commit failed")
}

func TestAsyncImagePendingReleasedOnCommitFailure(t *testing.T) {
	h, router, rdb, store := newPendingImageHandlerTest(t, time.Minute)
	failing := pendingImageCommitFailureStore{store, store.(service.ImageTaskPendingStore), store.(service.ImageTaskTerminalStore)}
	h.tasks = service.NewImageTaskServiceWithUploader(failing, nil, time.Hour, time.Minute)
	h.execute = func(_ string, _ *gin.Context) { t.Error("failed admission must not execute") }
	w := submitPendingImage(router, strings.NewReader(`{"prompt":"test"}`))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, int64(0), rdb.ZCard(context.Background(), "image_task:pending:7").Val())
}

func TestAsyncImagePendingReleasedOnExecutionBranches(t *testing.T) {
	tests := []struct {
		name    string
		execute func(string, *gin.Context)
		status  string
	}{
		{"success", func(_ string, c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": []any{}}) }, service.ImageTaskStatusCompleted},
		{"upstream error", func(_ string, c *gin.Context) { c.JSON(http.StatusBadGateway, gin.H{"error": "failed"}) }, service.ImageTaskStatusFailed},
		{"invalid response", func(_ string, c *gin.Context) { c.String(http.StatusOK, "invalid json") }, service.ImageTaskStatusFailed},
		{"panic", func(_ string, _ *gin.Context) { panic("mock panic") }, service.ImageTaskStatusFailed},
		{"timeout with body", func(_ string, c *gin.Context) {
			<-c.Request.Context().Done()
			c.JSON(http.StatusOK, gin.H{"data": []any{}})
		}, service.ImageTaskStatusFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Non-timeout branches must reach execution even when Redis setup and
			// admission are slower under the full suite or race instrumentation.
			timeout := 5 * time.Second
			if tc.name == "timeout with body" {
				timeout = time.Second
			}
			h, router, rdb, _ := newPendingImageHandlerTest(t, timeout)
			h.execute = tc.execute
			w := submitPendingImage(router, strings.NewReader(`{"prompt":"test"}`))
			assertPendingImageFinished(t, h, rdb, w, tc.status)
		})
	}
}

type pendingImageSlowStorage struct{}

func (pendingImageSlowStorage) Save(ctx context.Context, _, _ string, _ []byte) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestAsyncImagePendingDeadlineCoversUpload(t *testing.T) {
	h, router, rdb, store := newPendingImageHandlerTest(t, time.Second)
	uploader := service.NewImageResultUploader(pendingImageSlowStorage{}, "images", 1024, nil)
	h.tasks = service.NewImageTaskServiceWithUploader(store, uploader, time.Hour, time.Second)
	h.execute = func(_ string, c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": "aGk="}}}) }
	w := submitPendingImage(router, strings.NewReader(`{"prompt":"test"}`))
	assertPendingImageFinished(t, h, rdb, w, service.ImageTaskStatusFailed)
}

func TestAsyncImagePendingLimitRequiresSharedStore(t *testing.T) {
	h, router, _, _ := newPendingImageHandlerTest(t, time.Minute)
	h.tasks = service.NewImageTaskServiceWithUploader(&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, nil, time.Hour, time.Minute)
	w := submitPendingImage(router, strings.NewReader(`{"prompt":"test"}`))
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "configured limits cannot fall back to process-local counting")
}
