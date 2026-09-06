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

type VideoCreateRequest struct {
	TaskID          string
	ClientToken     string
	Operation       string
	Model           string
	RequestedModel  string
	Prompt          string
	Seconds         int
	Size            string
	Width           int
	Height          int
	Quality         string
	AudioEnabled    *bool
	ServiceTier     string
	ParentTask      *ProviderTaskRef
	Characters      []ProviderResourceRef
	InputReference  *ProviderInputReference
	ReferenceMedia  ProviderVideoReferenceMedia
	ProviderOptions map[string]any
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
}

type ProviderResourceRef struct {
	Provider           string
	AccountID          int64
	ProviderResourceID string
}

type ProviderVideoTask struct {
	ProviderTaskID        string
	VideoURL              string
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
