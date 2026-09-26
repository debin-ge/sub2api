package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageTaskStatusProcessing = "processing"
	ImageTaskStatusCompleted  = "completed"
	ImageTaskStatusFailed     = "failed"

	defaultImageTaskTTL              = 24 * time.Hour
	defaultImageTaskExecutionTimeout = 30 * time.Minute
)

var (
	ErrImageTaskNotFound    = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_NOT_FOUND", "image task not found")
	ErrImageTaskForbidden   = infraerrors.New(http.StatusForbidden, "IMAGE_TASK_FORBIDDEN", "image task does not belong to this API key")
	ErrImageTaskUnavailable = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image task storage is unavailable")
)

// ImageTaskRecord is the private Redis representation of an asynchronous image
// request. Ownership fields are intentionally omitted from the public view.
type ImageTaskRecord struct {
	ID          string          `json:"id"`
	UserID      int64           `json:"user_id"`
	APIKeyID    int64           `json:"api_key_id"`
	Status      string          `json:"status"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       json.RawMessage `json:"error,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	ExpiresAt   int64           `json:"expires_at"`
}

// ImageTask is the API-safe task representation returned to callers.
type ImageTask struct {
	ID          string          `json:"id"`
	TaskID      string          `json:"task_id"`
	Object      string          `json:"object"`
	Status      string          `json:"status"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	ImageURL    string          `json:"image_url,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       json.RawMessage `json:"error,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	ExpiresAt   int64           `json:"expires_at"`
}

type ImageTaskOwner struct {
	UserID   int64
	APIKeyID int64
}

type ImageTaskStore interface {
	Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
	Get(ctx context.Context, id string) (*ImageTaskRecord, error)
}

// ImageTaskPendingCounter 是 store 的可选扩展：若底层存储能够按用户统计未终态任务数
// （例如维护了 per-user 索引），实现该接口后 CountPendingForUser 优先采用其结果；
// 否则回落到服务内的进程本地追踪。
type ImageTaskPendingCounter interface {
	CountPendingForUser(ctx context.Context, userID int64) (int, error)
}

// imageTaskPendingGrace 是进程本地追踪对未收到终态回写任务的宽限：任务创建时间超过
// 执行超时 + 宽限仍未终结（如 finish 落库失败），视为已不再占用挂起配额。
const imageTaskPendingGrace = time.Minute

// ImageStorageResolver reports the currently effective object-storage binding.
// It exists so the async image feature can be switched on and off from the admin
// UI without a restart: the wiring below is fixed at startup, but the answer to
// "is object storage configured right now" is re-read (and cached) per call.
type ImageStorageResolver func() (uploader *ImageResultUploader, enabled bool)

type ImageTaskService struct {
	store            ImageTaskStore
	uploader         *ImageResultUploader
	enabled          bool
	resolve          ImageStorageResolver
	ttl              time.Duration
	executionTimeout time.Duration

	now func() time.Time

	// pending 记录本进程创建且尚未终结的任务：userID -> taskID -> 创建时间。
	// Redis store 只有按 ID 的 K/V，没有 per-user 索引，因此挂起数在此本地统计；
	// 任务由创建它的进程执行并回写终态，本地视图对该进程是完整的。
	pendingMu sync.Mutex
	pending   map[int64]map[string]time.Time
}

func NewImageTaskService(store ImageTaskStore) *ImageTaskService {
	return NewImageTaskServiceWithOptions(store, defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

func NewImageTaskServiceWithOptions(store ImageTaskStore, ttl, executionTimeout time.Duration) *ImageTaskService {
	if ttl <= 0 {
		ttl = defaultImageTaskTTL
	}
	if executionTimeout <= 0 {
		executionTimeout = defaultImageTaskExecutionTimeout
	}
	return &ImageTaskService{
		store:            store,
		ttl:              ttl,
		executionTimeout: executionTimeout,
		now:              time.Now,
		pending:          make(map[int64]map[string]time.Time),
	}
}

// NewImageTaskServiceWithUploader 构造一个已启用的图片任务服务：结果会先经 uploader
// 转存到对象存储再落 Redis。uploader 为 nil 时不做转存（仅用于测试）。
func NewImageTaskServiceWithUploader(store ImageTaskStore, uploader *ImageResultUploader, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.uploader = uploader
	s.enabled = true
	return s
}

// NewImageTaskServiceWithResolver 构造一个由 resolver 决定启用状态的服务：
// 开关与凭证来自后台设置，保存后立即生效，无需重启。
func NewImageTaskServiceWithResolver(store ImageTaskStore, resolve ImageStorageResolver, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.resolve = resolve
	return s
}

// current 返回当前生效的 uploader 与启用状态。
// 注入了 resolver 时以 resolver 为准（后台设置可热切换），否则回落到构造时固定的值。
func (s *ImageTaskService) current() (*ImageResultUploader, bool) {
	if s == nil {
		return nil, false
	}
	if s.resolve != nil {
		return s.resolve()
	}
	return s.uploader, s.enabled
}

// Enabled 表示异步图片任务功能是否可用（总开关 + 凭证齐全）。
// 关闭时 handler 直接返回 404，不创建任务、不写 Redis。
func (s *ImageTaskService) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	_, enabled := s.current()
	return enabled
}

// Pollable 表示已创建的任务能否被查询。
// 比 Enabled 弱：只要 store 可用即可，从而在功能被关掉后仍能取回进行中的任务结果。
func (s *ImageTaskService) Pollable() bool {
	return s != nil && s.store != nil
}

func (s *ImageTaskService) ExecutionTimeout() time.Duration {
	if s == nil || s.executionTimeout <= 0 {
		return defaultImageTaskExecutionTimeout
	}
	return s.executionTimeout
}

func (s *ImageTaskService) Create(ctx context.Context, owner ImageTaskOwner) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	now := s.clock().UTC()
	task := &ImageTaskRecord{
		ID:        "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:    owner.UserID,
		APIKeyID:  owner.APIKeyID,
		Status:    ImageTaskStatusProcessing,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
	}
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	s.trackPending(task.UserID, task.ID, now)
	return imageTaskToPublic(task), nil
}

// CountPendingForUser 返回用户当前未终态（processing）的异步图片任务数。
// store 实现了 ImageTaskPendingCounter 时以其为准，否则使用进程本地追踪。
func (s *ImageTaskService) CountPendingForUser(ctx context.Context, userID int64) (int, error) {
	if s == nil || s.store == nil {
		return 0, ErrImageTaskUnavailable
	}
	if counter, ok := s.store.(ImageTaskPendingCounter); ok {
		return counter.CountPendingForUser(ctx, userID)
	}
	return s.countTrackedPending(userID), nil
}

func (s *ImageTaskService) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// pendingStaleAfter 是本地追踪条目的最长存活时间。
func (s *ImageTaskService) pendingStaleAfter() time.Duration {
	return s.ExecutionTimeout() + imageTaskPendingGrace
}

func (s *ImageTaskService) trackPending(userID int64, taskID string, createdAt time.Time) {
	if s == nil || userID <= 0 || taskID == "" {
		return
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pending == nil {
		s.pending = make(map[int64]map[string]time.Time)
	}
	tasks := s.pending[userID]
	if tasks == nil {
		tasks = make(map[string]time.Time)
		s.pending[userID] = tasks
	}
	tasks[taskID] = createdAt
}

func (s *ImageTaskService) untrackPending(userID int64, taskID string) {
	if s == nil {
		return
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	tasks := s.pending[userID]
	if tasks == nil {
		return
	}
	delete(tasks, taskID)
	if len(tasks) == 0 {
		delete(s.pending, userID)
	}
}

// countTrackedPending 统计用户本地追踪的挂起任务数，顺带清理超过宽限仍未终结的条目。
func (s *ImageTaskService) countTrackedPending(userID int64) int {
	if s == nil {
		return 0
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	tasks := s.pending[userID]
	if len(tasks) == 0 {
		return 0
	}
	cutoff := s.clock().Add(-s.pendingStaleAfter())
	for id, createdAt := range tasks {
		if createdAt.Before(cutoff) {
			delete(tasks, id)
		}
	}
	if len(tasks) == 0 {
		delete(s.pending, userID)
		return 0
	}
	return len(tasks)
}

func (s *ImageTaskService) Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		// Do not reveal whether a random task ID exists for another caller.
		return nil, ErrImageTaskNotFound
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Complete(ctx context.Context, id string, statusCode int, result json.RawMessage) error {
	if !json.Valid(result) {
		return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "upstream returned a non-JSON image response"))
	}
	if uploader, _ := s.current(); uploader != nil {
		rewritten, err := uploader.Rewrite(ctx, id, result)
		if err != nil {
			// 转存失败不回退存 base64，避免大 blob 撑爆 Redis：直接把任务标记为失败。
			logger.L().Error("image_task.offload_failed", zap.String("task_id", id), zap.Error(err))
			return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "failed to store generated image to object storage"))
		}
		result = rewritten
	}
	return s.finish(ctx, id, ImageTaskStatusCompleted, statusCode, result, nil)
}

func (s *ImageTaskService) Fail(ctx context.Context, id string, statusCode int, taskErr json.RawMessage) error {
	if !json.Valid(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	return s.finish(ctx, id, ImageTaskStatusFailed, statusCode, nil, taskErr)
}

func (s *ImageTaskService) finish(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage) error {
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return ErrImageTaskNotFound
		}
		return ErrImageTaskUnavailable.WithCause(err)
	}
	now := s.clock().UTC()
	completedAt := now.Unix()
	task.Status = status
	task.HTTPStatus = statusCode
	task.Result = result
	task.Error = taskErr
	task.CompletedAt = &completedAt
	task.ExpiresAt = now.Add(s.ttl).Unix()
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	s.untrackPending(task.UserID, task.ID)
	return nil
}

func imageTaskToPublic(task *ImageTaskRecord) *ImageTask {
	if task == nil {
		return nil
	}
	return &ImageTask{
		ID:          task.ID,
		TaskID:      task.ID,
		Object:      "image.generation.task",
		Status:      task.Status,
		HTTPStatus:  task.HTTPStatus,
		ImageURL:    firstImageTaskURL(task.Result),
		Result:      task.Result,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.CompletedAt,
		ExpiresAt:   task.ExpiresAt,
	}
}

func firstImageTaskURL(result json.RawMessage) string {
	if len(result) == 0 || !json.Valid(result) {
		return ""
	}
	var response struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil || len(response.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(response.Data[0].URL)
}

func imageTaskErrorJSON(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": errorType, "message": message})
	return data
}
