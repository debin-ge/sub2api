package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	VideoProviderOpenAI = "openai"

	VideoOperationGenerate        = "generate"
	VideoOperationEdit            = "edit"
	VideoOperationExtend          = "extend"
	VideoOperationCharacterCreate = "character_create"

	VideoResourceTypeCharacter = "character"
)

type VideoTask struct {
	ID                    int64
	PublicID              string
	Source                string
	UserID                int64
	APIKeyID              *int64
	GroupID               *int64
	ChannelID             *int64
	AccountID             *int64
	AccountOwnerUserID    *int64
	Provider              string
	Operation             string
	ParentTaskID          *int64
	RootTaskID            *int64
	Endpoint              string
	RequestedModel        string
	PublicModel           string
	ChannelModel          string
	UpstreamModel         string
	RequestHash           string
	IdempotencyKey        *string
	InputManifest         []VideoInputManifestEntry
	RequestAttributes     map[string]any
	ProviderTaskID        *string
	ProviderStatus        *string
	ProviderCreatedAt     *time.Time
	ProviderFinishedAt    *time.Time
	StableClientToken     *string
	GenerationState       string
	BillingState          string
	BillingReviewID       *int64
	SubmissionReviewID    *int64
	DeleteState           string
	Version               int64
	Progress              *float64
	UsageSnapshot         map[string]any
	ResponseMetadata      map[string]any
	ContentVariants       []string
	ContentExpiresAt      *time.Time
	ProviderAccessKind    *string
	ProviderAccessScope   *string
	ProviderAccessEnc     *string
	ProviderAccessExpires *time.Time
	ProviderVideoURLEnc   *string
	ProviderVideoProxyKey *string
	BillingUnit           *string
	EstimatedUnits        *float64
	ActualUnits           *float64
	PriceSnapshot         map[string]any
	ProviderCostSnapshot  map[string]any
	Currency              string
	HoldID                *string
	HoldAmount            *float64
	ActualCost            *float64
	CallbackURLEnc        *string
	CallbackIntentState   string
	NextActionAt          *time.Time
	PollAttempts          int
	SubmitAttempts        int
	LeaseOwner            *string
	LeaseExpiresAt        *time.Time
	LeaseEpoch            int64
	LastErrorKind         *string
	LastErrorCode         *string
	LastErrorMessage      *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	SubmittedAt           *time.Time
	StartedAt             *time.Time
	FinishedAt            *time.Time
	SettledAt             *time.Time
	SubmissionUnknownAt   *time.Time
	QuarantinedAt         *time.Time
	DeletedAt             *time.Time
}

type VideoResource struct {
	ID                    int64
	PublicID              string
	ResourceType          string
	UserID                int64
	APIKeyID              *int64
	GroupID               *int64
	Provider              string
	ChannelID             *int64
	AccountID             int64
	SourceTaskID          *int64
	ProviderResourceID    string
	Model                 string
	Status                string
	Metadata              map[string]any
	ProviderAccessKind    *string
	ProviderAccessScope   *string
	ProviderAccessEnc     *string
	ProviderAccessExpires *time.Time
	Version               int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExpiresAt             *time.Time
	DeletedAt             *time.Time
}

// VideoTaskDisclosure is the only service DTO allowed to carry a decrypted
// task-scoped Provider access value. Callers must never persist or log Access.
type VideoTaskDisclosure struct {
	Policy         string
	Provider       string
	ProviderTaskID string
	Access         *ProviderTaskAccess
}

type VideoResourceDisclosure struct {
	Policy             string
	Provider           string
	ProviderResourceID string
}

type VideoInputRole string

const (
	VideoInputRoleReferenceImage VideoInputRole = "input_reference"
	VideoInputRoleReferenceVideo VideoInputRole = "reference_video"
	VideoInputRoleSourceVideo    VideoInputRole = "source_video"
	VideoInputRoleCharacterClip  VideoInputRole = "character_clip"
	VideoInputRoleMask           VideoInputRole = "mask"
)

func IsValidVideoInputRole(role VideoInputRole) bool {
	value := string(role)
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		character := value[i]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

type VideoInputManifestEntry struct {
	Role     VideoInputRole `json:"role"`
	FileName string         `json:"file_name,omitempty"`
	MIMEType string         `json:"mime_type"`
	Size     int64          `json:"size"`
	SHA256   string         `json:"sha256"`
	Width    int            `json:"width,omitempty"`
	Height   int            `json:"height,omitempty"`
}

type VideoInput struct {
	VideoInputManifestEntry
	Open func(context.Context) (io.ReadCloser, error) `json:"-"`
}

type VideoOwner struct {
	UserID   int64
	APIKeyID int64
	GroupID  *int64
}

type VideoTaskFilter struct {
	Status    string
	Model     string
	Operation string
	Limit     int
	After     string
	Order     string
}

type VideoCreateTaskParams struct {
	PublicID              string
	Owner                 VideoOwner
	ChannelID             *int64
	AccountID             int64
	AccountOwnerUserID    *int64
	Provider              string
	Operation             string
	ParentTaskID          *int64
	RootTaskID            *int64
	Endpoint              string
	RequestedModel        string
	PublicModel           string
	ChannelModel          string
	UpstreamModel         string
	RequestHash           string
	IdempotencyKey        string
	InputManifest         []VideoInputManifestEntry
	RequestAttributes     map[string]any
	StableClientToken     string
	BillingUnit           string
	EstimatedUnits        float64
	PriceSnapshot         map[string]any
	ProviderCostSnapshot  map[string]any
	Currency              string
	HoldID                string
	HoldAmount            float64
	CallbackURLEnc        string
	NextActionAt          *time.Time
	MaxAccountConcurrency int
}

type VideoTaskPage struct {
	Data    []*VideoTask
	HasMore bool
	After   string
}

type VideoTaskTransition struct {
	GenerationState         string
	BillingState            string
	DeleteState             string
	ProviderStatus          string
	Progress                *float64
	UsageSnapshot           map[string]any
	ResponseMetadata        map[string]any
	ContentVariants         []string
	ContentExpiresAt        *time.Time
	ProviderFinishedAt      *time.Time
	ProviderVideoURLEnc     string
	ProviderVideoProxyKey   string
	ActualUnits             *float64
	ActualCost              *float64
	NextActionAt            *time.Time
	ErrorKind               string
	ErrorCode               string
	ErrorMessage            string
	Quarantine              bool
	SubmissionUnknown       bool
	IncrementSubmitAttempts bool
	IncrementPollAttempts   bool
	EventType               string
	EventPayload            map[string]any
}

type VideoProviderAcceptance struct {
	ProviderTaskID          string
	ProviderStatus          string
	ProviderCreatedAt       *time.Time
	ProviderFinishedAt      *time.Time
	GenerationState         string
	BillingState            string
	Progress                *float64
	UsageSnapshot           map[string]any
	ResponseMetadata        map[string]any
	ContentVariants         []string
	ContentExpiresAt        *time.Time
	ProviderAccessKind      string
	ProviderAccessScope     string
	ProviderAccessEnc       string
	ProviderAccessExpiresAt *time.Time
	ProviderVideoURLEnc     string
	ProviderVideoProxyKey   string
	ActualUnits             *float64
	ActualCost              *float64
	NextActionAt            *time.Time
	ErrorKind               string
	ErrorCode               string
	ErrorMessage            string
	Quarantine              bool
}

type VideoTaskRepository interface {
	CreateHeldVideoTask(ctx context.Context, params VideoCreateTaskParams) (*VideoTask, bool, error)
	GetVideoTaskByIdempotency(ctx context.Context, userID int64, endpoint, idempotencyKey string) (*VideoTask, error)
	GetVideoTaskForOwner(ctx context.Context, userID int64, publicID string) (*VideoTask, error)
	GetVideoTaskByProviderIDForOwner(ctx context.Context, userID int64, providerTaskID string) (*VideoTask, error)
	GetVideoTaskByPublicID(ctx context.Context, publicID string) (*VideoTask, error)
	GetVideoTaskByProviderID(ctx context.Context, provider string, accountID int64, providerTaskID string) (*VideoTask, error)
	ListVideoTasksForOwner(ctx context.Context, userID int64, filter VideoTaskFilter) (*VideoTaskPage, error)
	TransitionVideoTask(ctx context.Context, publicID string, transition VideoTaskTransition) (*VideoTask, error)
	SaveVideoProviderAccepted(ctx context.Context, publicID string, acceptance VideoProviderAcceptance) (*VideoTask, error)
	MarkVideoSubmissionUnknown(ctx context.Context, publicID string, providerError *VideoProviderError, nextActionAt time.Time) (*VideoTask, error)
	ClaimVideoTask(ctx context.Context, publicID, workerID string, lease time.Duration) (*VideoTask, error)
	ClaimDueVideoTasks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*VideoTask, error)
	RenewVideoTaskLease(ctx context.Context, lease VideoTaskLease, duration time.Duration) (time.Time, error)
	ReleaseVideoTaskLease(ctx context.Context, lease VideoTaskLease, nextActionAt *time.Time) error
	ClearExpiredVideoProviderAccess(ctx context.Context, limit int) (int64, error)
	AppendVideoTaskEvent(ctx context.Context, event VideoTaskEvent) (bool, error)
}

type VideoTaskProxyLookupRepository interface {
	GetVideoTaskByProxyKeyForOwner(ctx context.Context, userID int64, proxyKey string) (*VideoTask, error)
	GetVideoTaskByProxyKey(ctx context.Context, proxyKey string) (*VideoTask, error)
}

type VideoTaskStateSnapshot struct {
	Provider        string     `json:"provider"`
	Operation       string     `json:"operation"`
	State           string     `json:"state"`
	Count           int64      `json:"count"`
	OldestEnteredAt *time.Time `json:"oldest_entered_at,omitempty"`
}

type VideoOperationalSnapshot struct {
	DeletePending           int64
	OldestDeletePending     *time.Time
	TaskStates              []VideoTaskStateSnapshot
	SubmissionUnknown       int64
	UnknownHoldAmount       float64
	HeldAmount              float64
	OldestSettlementPending *time.Time
	OldestManualReview      *time.Time
}

// VideoOperationalMetricsReader is optional so narrow repositories and test
// doubles do not need to implement operational-only database queries.
type VideoOperationalMetricsReader interface {
	GetVideoOperationalSnapshot(context.Context) (*VideoOperationalSnapshot, error)
}

type VideoCreateResourceParams struct {
	PublicID           string
	ResourceType       string
	Owner              VideoOwner
	Provider           string
	ChannelID          *int64
	AccountID          int64
	SourceTaskID       *int64
	ProviderResourceID string
	Model              string
	Status             string
	Metadata           map[string]any
	ExpiresAt          *time.Time
}

type VideoResourceRepository interface {
	CreateVideoResource(ctx context.Context, params VideoCreateResourceParams) (*VideoResource, error)
	GetVideoResourceForOwner(ctx context.Context, userID int64, publicID string) (*VideoResource, error)
	GetVideoResourceForOwnerIncludingDeleted(ctx context.Context, userID int64, publicID string) (*VideoResource, error)
	GetVideoResourceBySourceTaskForOwner(ctx context.Context, userID int64, sourceTaskID int64) (*VideoResource, error)
	GetVideoResourceByProviderID(ctx context.Context, provider string, accountID int64, providerResourceID string) (*VideoResource, error)
	MarkVideoResourceDeleted(ctx context.Context, userID int64, publicID string) error
}

type VideoCallbackDelivery struct {
	ID               int64
	TaskID           int64
	EventID          string
	EventType        string
	EventFingerprint string
	Payload          map[string]any
	TargetURLEnc     string
	Status           string
	Attempts         int
	NextAttemptAt    time.Time
	ExpiresAt        time.Time
	LeaseOwner       *string
	LeaseExpiresAt   *time.Time
	LastError        *string
	LastStatusCode   *int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeliveredAt      *time.Time
	QuarantinedAt    *time.Time
}

type VideoCallbackRepository interface {
	EnqueueVideoCallback(ctx context.Context, delivery VideoCallbackDelivery) (*VideoCallbackDelivery, bool, error)
	ClaimVideoCallbacks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*VideoCallbackDelivery, error)
	RenewVideoCallbackLease(ctx context.Context, id int64, workerID string, lease time.Duration) error
	MarkVideoCallbackDelivered(ctx context.Context, id int64, workerID string, statusCode int) error
	RetryVideoCallback(ctx context.Context, id int64, workerID string, nextAttemptAt time.Time, statusCode int, lastError string) error
	QuarantineVideoCallback(ctx context.Context, id int64, workerID string, lastError string) error
}

type VideoCallbackMaterializationRepository interface {
	ListVideoCallbackIntents(context.Context, int) ([]*VideoTask, error)
	MaterializeVideoCallback(context.Context, *VideoCallbackDelivery) error
}

type VideoTaskQueue interface {
	Enqueue(ctx context.Context, taskID string) (bool, error)
	Reserve(ctx context.Context) (string, error)
	RequeueAfter(ctx context.Context, taskID string, delay time.Duration) error
	Ack(ctx context.Context, taskID string) error
	MoveDueToReady(ctx context.Context, limit int) (int, error)
	RecoverStale(ctx context.Context, staleAfter time.Duration, limit int) (int, error)
}

type VideoTaskQueueStats struct {
	Ready   int64 `json:"ready"`
	Delayed int64 `json:"delayed"`
	Active  int64 `json:"active"`
}

// VideoTaskQueueStatsReader is optional because queue correctness does not
// depend on metrics availability.
type VideoTaskQueueStatsReader interface {
	VideoTaskQueueStats(context.Context) (VideoTaskQueueStats, error)
}

type VideoTaskEvent struct {
	ID                  int64          `json:"id"`
	TaskID              *int64         `json:"task_id,omitempty"`
	EventType           string         `json:"event_type"`
	Provider            string         `json:"provider,omitempty"`
	AccountID           *int64         `json:"account_id,omitempty"`
	ProviderTaskID      string         `json:"provider_task_id,omitempty"`
	ProviderEventID     string         `json:"provider_event_id,omitempty"`
	FromGenerationState string         `json:"from_generation_state,omitempty"`
	ToGenerationState   string         `json:"to_generation_state,omitempty"`
	FromBillingState    string         `json:"from_billing_state,omitempty"`
	ToBillingState      string         `json:"to_billing_state,omitempty"`
	Payload             map[string]any `json:"payload"`
	EventHash           string         `json:"event_hash,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
}

func NewVideoTaskID() string {
	return "video_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func NewVideoResourceID() string {
	return "char_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func IsValidVideoTaskID(id string) bool {
	return isValidVideoPublicID(strings.TrimSpace(id), "video_")
}

func IsValidVideoResourceID(id string) bool {
	return isValidVideoPublicID(strings.TrimSpace(id), "char_")
}

func isValidVideoPublicID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) != len(prefix)+32 {
		return false
	}
	for _, character := range id[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func HashVideoRequest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
