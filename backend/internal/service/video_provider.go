package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type VideoCapability string

const (
	VideoCapabilityCreate             VideoCapability = "create"
	VideoCapabilityInputReference     VideoCapability = "input_reference"
	VideoCapabilityCharacters         VideoCapability = "characters"
	VideoCapabilityUploadedVideoEdits VideoCapability = "uploaded_video_edits"
	VideoCapabilityEdits              VideoCapability = "edits"
	VideoCapabilityExtensions         VideoCapability = "extensions"
	VideoCapabilityCancel             VideoCapability = "cancel"
	VideoCapabilityWebhook            VideoCapability = "webhook"
	VideoCapabilityTaskSearch         VideoCapability = "task_search"
)

type VideoCapabilities struct {
	DefaultModel          string                             `json:"default_model"`
	DefaultSeconds        map[string]int                     `json:"default_seconds"`
	DefaultSizes          map[string]string                  `json:"default_sizes"`
	Operations            map[VideoCapability]bool           `json:"operations"`
	InputRolesByOperation map[string]map[VideoInputRole]bool `json:"input_roles_by_operation"`
	InputMIMETypes        map[VideoInputRole]map[string]bool `json:"input_mime_types"`
	MaxInputBytes         map[VideoInputRole]int64           `json:"max_input_bytes"`
	MaxInputsByOperation  map[string]int                     `json:"max_inputs_by_operation"`
	AllowReferenceAndFile bool                               `json:"allow_reference_and_file"`
	ContentVariants       map[string]bool                    `json:"content_variants"`
	SupportedModels       map[string]bool                    `json:"supported_models"`
	SupportedSeconds      map[string][]int                   `json:"supported_seconds"`
	SupportedSizes        map[string][]string                `json:"supported_sizes"`
	// Ark 维度的画幅能力：分辨率档位与比例词表，按 canonical 模型名索引。空表示该
	// provider 不用这两维表达画幅（OpenAI 用 SupportedSizes）。
	SupportedResolutions map[string][]string `json:"supported_resolutions,omitempty"`
	SupportedRatios      map[string][]string `json:"supported_ratios,omitempty"`
	DefaultResolutions   map[string]string   `json:"default_resolutions,omitempty"`
	DefaultRatios        map[string]string   `json:"default_ratios,omitempty"`
	// SmartDurationModels 列出接受 duration=-1（由上游自选时长）的模型。
	SmartDurationModels map[string]bool `json:"smart_duration_models,omitempty"`
	// MaxContentByRole 是 content[] 里各角色媒体引用的数量上限（URL 与字节都算）；
	// 未列出的角色不接受。空表示沿用 InputRolesByOperation 的旧式判定。
	MaxContentByRole map[VideoInputRole]int `json:"max_content_by_role,omitempty"`
}

// SupportsContentRole 报告 content[] 是否接受该角色；count 为该角色的引用数。
func (c VideoCapabilities) SupportsContentRole(role VideoInputRole, count int) bool {
	if len(c.MaxContentByRole) == 0 {
		return c.SupportsInputForOperation(VideoOperationGenerate, role)
	}
	maximum, ok := c.MaxContentByRole[role]
	return ok && maximum > 0 && count <= maximum
}

func (c VideoCapabilities) SupportsInputForOperation(operation string, role VideoInputRole) bool {
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "" {
		operation = VideoOperationGenerate
	}
	return c.InputRolesByOperation != nil && c.InputRolesByOperation[operation][role]
}

func (c VideoCapabilities) Supports(capability VideoCapability) bool {
	return c.Operations != nil && c.Operations[capability]
}

func (c VideoCapabilities) SupportsInput(role VideoInputRole, mimeType string, size int64) bool {
	allowed, ok := c.InputMIMETypes[role]
	if !ok || !allowed[strings.ToLower(strings.TrimSpace(mimeType))] {
		return false
	}
	maximum := c.MaxInputBytes[role]
	return size >= 0 && (maximum <= 0 || size <= maximum)
}

func (c VideoCapabilities) SupportsVariant(variant string) bool {
	return c.ContentVariants != nil && c.ContentVariants[strings.ToLower(strings.TrimSpace(variant))]
}

// VideoContentItem 是 Ark 协议 content[] 的一项：文本，或带角色的媒体引用。
//
// 媒体引用要么是远端 URL（URL 非空、Input 为空），要么是已落入加密 spool 的字节
// （Input 非空、URL 为空；data: URI 与 multipart 上传都走这里）。Input 不参与 JSON
// 序列化，字节清单由 VideoSubmitRequest.Inputs 单独哈希。
type VideoContentItem struct {
	Type  string         `json:"type"`
	Role  VideoInputRole `json:"role,omitempty"`
	Text  string         `json:"text,omitempty"`
	URL   string         `json:"url,omitempty"`
	Input *VideoInput    `json:"-"`
}

const (
	VideoContentTypeText  = "text"
	VideoContentTypeImage = "image_url"
	VideoContentTypeVideo = "video_url"
	VideoContentTypeAudio = "audio_url"
)

// IsMedia 报告该项是否为媒体引用（非文本）。
func (item VideoContentItem) IsMedia() bool { return item.Type != VideoContentTypeText }

// VideoCreateRequest 是提交给 provider 的规范化请求，字段以 Ark 视频协议为准：
//   - Seconds 即 Ark 的 duration（整数秒）；SmartDuration 为真表示 duration=-1，由上游自选时长。
//   - Frames 与 Seconds 二选一，Frames 优先（仅 Ark 支持）。
//   - Resolution / Ratio 表达画幅；Ratio 允许 "adaptive"（跟随首帧）。
//   - AudioEnabled 即 Ark 的 generate_audio。
//   - Content 承载提示词与带角色的媒体引用；Prompt 是全部 text 项的拼接，便于审计与审核。
//
// 下面这组字段只服务 OpenAI（Sora）公开形态，五家 Ark 形态的 provider 对其非空值一律拒绝：
// Size（WxH）、Quality、AspectRatio（OpenAI 兼容上游的 aspect_ratio 字段名）、InputReference
// （file_id 引用）、Characters、ParentTask。
type VideoCreateRequest struct {
	TaskID                string
	ClientToken           string
	Operation             string
	Model                 string
	RequestedModel        string
	Prompt                string
	Content               []VideoContentItem
	Seconds               int
	SmartDuration         bool
	Frames                int
	Resolution            string
	Ratio                 string
	AudioEnabled          *bool
	Seed                  *int64
	CameraFixed           *bool
	Watermark             *bool
	ReturnLastFrame       *bool
	ServiceTier           string
	ExecutionExpiresAfter int
	ProviderOptions       map[string]any

	// OpenAI（Sora）专用。
	Size           string
	Quality        string
	AspectRatio    string
	InputReference *ProviderInputReference
	ParentTask     *ProviderTaskRef
	Characters     []ProviderResourceRef
}

// MediaContent 返回全部媒体引用项（保持声明顺序）。
func (request VideoCreateRequest) MediaContent() []VideoContentItem {
	return videoMediaContent(request.Content)
}

// ContentByRole 返回指定角色的媒体引用项。
func (request VideoCreateRequest) ContentByRole(role VideoInputRole) []VideoContentItem {
	return videoContentByRole(request.Content, role)
}

// HasMediaContent 报告是否带任何媒体引用。
func (request VideoCreateRequest) HasMediaContent() bool { return len(request.MediaContent()) > 0 }

// HasOpenAIOnlyFields 报告是否带了只有 OpenAI（Sora）形态才有的字段；Ark 形态的 provider 用它一票否决。
func (request VideoCreateRequest) HasOpenAIOnlyFields() bool {
	return strings.TrimSpace(request.Size) != "" || strings.TrimSpace(request.Quality) != "" ||
		strings.TrimSpace(request.AspectRatio) != "" || request.InputReference != nil ||
		request.ParentTask != nil || len(request.Characters) > 0
}

// ReferenceMedia 把 Content 里的远端 URL 引用折回 OpenAI 兼容上游的结构化字段。只有
// OpenAI provider 与 ByteDance 的 openai_compatible 中继模式使用；spool 字节不在其中。
func (request VideoCreateRequest) ReferenceMedia() ProviderVideoReferenceMedia {
	media := ProviderVideoReferenceMedia{Ratio: strings.TrimSpace(request.Ratio), AspectRatio: strings.TrimSpace(request.AspectRatio)}
	for _, item := range request.Content {
		if !item.IsMedia() || item.Input != nil || strings.TrimSpace(item.URL) == "" {
			continue
		}
		url := strings.TrimSpace(item.URL)
		switch item.Role {
		case VideoInputRoleReferenceImage:
			if media.ImageURL == "" {
				media.ImageURL = url
			} else {
				media.ReferenceImages = append(media.ReferenceImages, url)
			}
		case VideoInputRoleFirstFrame:
			media.FirstImageURL = url
		case VideoInputRoleLastFrame:
			media.LastImageURL = url
		case VideoInputRoleTaggedReferenceImage:
			media.ReferenceImages = append(media.ReferenceImages, url)
		case VideoInputRoleReferenceVideo:
			media.ReferenceVideos = append(media.ReferenceVideos, url)
		case VideoInputRoleReferenceAudio:
			media.ReferenceAudios = append(media.ReferenceAudios, url)
		}
	}
	return media
}

func videoMediaContent(content []VideoContentItem) []VideoContentItem {
	items := make([]VideoContentItem, 0, len(content))
	for _, item := range content {
		if item.IsMedia() {
			items = append(items, item)
		}
	}
	return items
}

func videoContentByRole(content []VideoContentItem, role VideoInputRole) []VideoContentItem {
	items := make([]VideoContentItem, 0, 2)
	for _, item := range content {
		if item.IsMedia() && item.Role == role {
			items = append(items, item)
		}
	}
	return items
}

// VideoContentRoleCounts 按角色统计媒体引用数（审计快照与能力校验共用）。
func VideoContentRoleCounts(content []VideoContentItem) map[string]int {
	counts := map[string]int{}
	for _, item := range content {
		if item.IsMedia() {
			counts[string(item.Role)]++
		}
	}
	return counts
}

type ProviderInputReference struct {
	FileID   string
	ImageURL string
}

// ProviderVideoReferenceMedia contains structured compatibility fields for
// custom OpenAI-compatible video upstreams. Signed URLs are request-scoped and
// must not be copied into durable task metadata.
type ProviderVideoReferenceMedia struct {
	Ratio           string   `json:"ratio,omitempty"`
	AspectRatio     string   `json:"aspect_ratio,omitempty"`
	ImageURL        string   `json:"image_url,omitempty"`
	FirstImageURL   string   `json:"first_image_url,omitempty"`
	LastImageURL    string   `json:"last_image_url,omitempty"`
	ReferenceImages []string `json:"reference_images,omitempty"`
	ReferenceVideos []string `json:"reference_videos,omitempty"`
	ReferenceAudios []string `json:"reference_audios,omitempty"`
}

type VideoEditRequest struct {
	VideoCreateRequest
	SourceTask *ProviderTaskRef
}

type VideoExtendRequest struct {
	VideoCreateRequest
	SourceTask ProviderTaskRef
}

type VideoCharacterRequest struct {
	TaskID          string
	ClientToken     string
	Name            string
	ProviderOptions map[string]any
}

type ProviderTaskRef struct {
	Provider       string
	AccountID      int64
	ProviderTaskID string
	// Private recovery state is never part of a public provider reference.
	ProviderExecutionState *VideoProviderExecutionState `json:"-"`
}

type ProviderResourceRef struct {
	Provider           string
	AccountID          int64
	ProviderResourceID string
}

type ProviderVideoTask struct {
	ProviderTaskID string
	VideoURL       string
	// Control is private execution state, never public provider metadata.
	Control *VideoProviderControlObservation `json:"-"`
	// LastFrameURL 是 Ark return_last_frame=true 时回报的尾帧图链接（变体 last_frame）。
	LastFrameURL          string
	Status                string
	RawStatus             string
	Progress              *float64
	Usage                 map[string]any
	Metadata              map[string]any
	ContentVariants       []string
	ContentExpiresAt      *time.Time
	Access                *ProviderTaskAccess
	ErrorCode             string
	ErrorMessage          string
	ProviderCreatedAt     *time.Time
	ProviderFinishedAt    *time.Time
	SuggestedPollInterval time.Duration
}

type ProviderVideoResource struct {
	ProviderResourceID string
	Status             string
	Metadata           map[string]any
	ExpiresAt          *time.Time
	Access             *ProviderTaskAccess
}

type ProviderTaskAccess struct {
	Kind      string
	Value     string
	Scope     string
	ExpiresAt *time.Time
}

type ProviderContentRequest struct {
	TaskRef               ProviderTaskRef
	UpstreamURL           string
	Variant               string
	Method                string
	Range                 string
	IfRange               string
	ResponseHeaderTimeout time.Duration
}

type ProviderContent struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

type ProviderWebhookRequest struct {
	Headers http.Header
	Body    []byte
}

type ProviderWebhookEvent struct {
	ProviderEventID string
	ProviderTaskID  string
	Status          string
	OccurredAt      time.Time
	Payload         map[string]any
}

type VideoProvider interface {
	Name() string
	Capabilities() VideoCapabilities
	SupportsAccount(account *Account) bool
	Create(context.Context, *Account, VideoCreateRequest, []VideoInput) (*ProviderVideoTask, error)
	Get(context.Context, *Account, ProviderTaskRef) (*ProviderVideoTask, error)
	OpenContent(context.Context, *Account, ProviderContentRequest) (*ProviderContent, error)
	Delete(context.Context, *Account, ProviderTaskRef) error
}

// VideoSubmissionValidator lets an adapter reject provider-specific options
// before a task and balance hold are created. Provider methods must still
// validate defensively when called directly.
type VideoSubmissionValidator interface {
	ValidateSubmission(*Account, VideoCreateRequest, []VideoInput) error
}

type VideoCanceller interface {
	Cancel(context.Context, *Account, ProviderTaskRef) error
}

type VideoEditor interface {
	Edit(context.Context, *Account, VideoEditRequest, []VideoInput) (*ProviderVideoTask, error)
}

type VideoExtender interface {
	Extend(context.Context, *Account, VideoExtendRequest) (*ProviderVideoTask, error)
}

type VideoCharacterProvider interface {
	CreateCharacter(context.Context, *Account, VideoCharacterRequest, VideoInput) (*ProviderVideoResource, error)
	GetCharacter(context.Context, *Account, ProviderResourceRef) (*ProviderVideoResource, error)
	DeleteCharacter(context.Context, *Account, ProviderResourceRef) error
}

type VideoTaskSearcher interface {
	SearchByClientToken(context.Context, *Account, string) (*ProviderVideoTask, error)
}

type VideoWebhookVerifier interface {
	VerifyWebhook(context.Context, *Account, ProviderWebhookRequest) (*ProviderWebhookEvent, error)
}

const (
	VideoCapabilityProbeSupported   = "supported"
	VideoCapabilityProbeUnsupported = "unsupported"
	VideoCapabilityProbeUnknown     = "unknown"
)

type VideoCapabilityProbeResult struct {
	Provider     string    `json:"provider"`
	Capability   string    `json:"capability"`
	Status       string    `json:"status"`
	CheckedAt    time.Time `json:"checked_at"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	ErrorSummary string    `json:"error_summary,omitempty"`
}

// VideoCapabilityProber performs a read-only, non-billing capability probe.
type VideoCapabilityProber interface {
	ProbeCapability(context.Context, *Account, VideoCapability) (*VideoCapabilityProbeResult, error)
}

type VideoAccessRefresher interface {
	RefreshTaskAccess(context.Context, *Account, ProviderTaskRef) (*ProviderTaskAccess, error)
}

type VideoProviderRegistry struct {
	providers map[string]VideoProvider
}

func NewVideoProviderRegistry(providers ...VideoProvider) *VideoProviderRegistry {
	registry := &VideoProviderRegistry{providers: make(map[string]VideoProvider, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		name := provider.Name()
		if validVideoProviderName(name) {
			registry.providers[name] = provider
		}
	}
	return registry
}

func (r *VideoProviderRegistry) Get(name string) (VideoProvider, bool) {
	if r == nil {
		return nil, false
	}
	provider, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	return provider, ok
}

func (r *VideoProviderRegistry) MustGet(name string) (VideoProvider, error) {
	provider, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrVideoProviderUnsupported, strings.TrimSpace(name))
	}
	return provider, nil
}

type VideoSubmissionCertainty string

const (
	VideoSubmissionRejected VideoSubmissionCertainty = "rejected"
	VideoSubmissionAccepted VideoSubmissionCertainty = "accepted"
	VideoSubmissionUnknown  VideoSubmissionCertainty = "unknown"
)

type VideoProviderError struct {
	Kind       string
	Code       string
	Message    string
	Retryable  bool
	Certainty  VideoSubmissionCertainty
	RetryAfter time.Duration
	StatusCode int
	Cause      error
}

func (e *VideoProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("video provider error: kind=%s code=%s certainty=%s: %s: %v", e.Kind, e.Code, e.Certainty, e.Message, e.Cause)
	}
	return fmt.Sprintf("video provider error: kind=%s code=%s certainty=%s: %s", e.Kind, e.Code, e.Certainty, e.Message)
}

func (e *VideoProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
