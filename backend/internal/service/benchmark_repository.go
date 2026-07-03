package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

type BenchmarkRepository interface {
	// Targets
	CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	GetTarget(ctx context.Context, id int64) (*ent.BenchmarkTarget, error)
	UpdateTarget(ctx context.Context, id int64, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	DeleteTarget(ctx context.Context, id int64) error
	ListTargetsByIDs(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error)
	ListEnabledTargets(ctx context.Context) ([]*ent.BenchmarkTarget, error)

	// Tasks
	CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	GetTask(ctx context.Context, id int64) (*ent.BenchmarkTask, error)
	UpdateTask(ctx context.Context, id int64, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	DeleteTask(ctx context.Context, id int64) error
	ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	ListEnabledTasks(ctx context.Context) ([]*ent.BenchmarkTask, error)

	// Schedules
	ListSchedules(ctx context.Context, input BenchmarkScheduleListInput) ([]*ent.BenchmarkSchedule, int, error)
	CreateSchedule(ctx context.Context, input BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	GetSchedule(ctx context.Context, id int64) (*ent.BenchmarkSchedule, error)
	UpdateSchedule(ctx context.Context, id int64, input BenchmarkScheduleInput) (*ent.BenchmarkSchedule, error)
	DeleteSchedule(ctx context.Context, id int64) error
	ListDueSchedules(ctx context.Context, now time.Time) ([]*ent.BenchmarkSchedule, error)
	UpdateScheduleAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error

	// Runs
	CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error)
	GetRun(ctx context.Context, id int64) (*ent.BenchmarkRun, error)
	ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	CancelRun(ctx context.Context, runID int64, reason string) error
	ClaimRunnableRuns(ctx context.Context, limit int) ([]*ent.BenchmarkRun, error)
	MarkRunStarted(ctx context.Context, runID int64) error
	MarkRunFinished(ctx context.Context, runID int64, status string, errorMessage *string) error
	UpdateRunStatus(ctx context.Context, runID int64, status string, errorMessage *string) error

	// Run children
	ListRunTargets(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTarget, error)
	ListRunTasks(ctx context.Context, runID int64) ([]*ent.BenchmarkRunTask, error)
	ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	ListRunScoreInputs(ctx context.Context, runID int64) ([]BenchmarkRunScoreInput, error)
	ClaimPendingResults(ctx context.Context, runID int64, limit int) ([]*ent.BenchmarkResult, error)
	RequeueClaimedResults(ctx context.Context, resultIDs []int64) error
	CountRunResultsByStatus(ctx context.Context, runID int64) (map[string]int, error)
	GetRunResultContext(ctx context.Context, resultID int64) (*BenchmarkRunResultContext, error)
	UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error

	// Scores & trends
	SaveTargetScores(ctx context.Context, runID int64, scores []BenchmarkTargetScoreInput) error
	ListTargetScores(ctx context.Context, runID int64) ([]*ent.BenchmarkTargetScore, error)
	ListTrendScores(ctx context.Context, since time.Time, limit int) ([]*ent.BenchmarkTargetScore, error)

	// Public snapshots
	PublishPublicSnapshot(ctx context.Context, input BenchmarkPublicSnapshotInput) error
	GetLatestPublicSnapshot(ctx context.Context) (*ent.BenchmarkPublicSnapshot, error)
}

type BenchmarkListInput struct {
	Page     int
	PageSize int
}

type BenchmarkTaskListInput struct {
	BenchmarkListInput
	TaskTypes []string
	Enabled   *bool
}

type BenchmarkRunListInput struct {
	BenchmarkListInput
	Status []string
}

type BenchmarkScheduleListInput struct {
	BenchmarkListInput
	Enabled *bool
}

type BenchmarkTargetInput struct {
	ModelName           string
	ChannelID           int64
	DisplayName         string
	ChannelNameSnapshot string
	Enabled             bool
	PublicVisible       bool
	SortOrder           int
}

type BenchmarkTaskInput struct {
	Title          string
	Type           string
	Difficulty     string
	Prompt         string
	InputPayload   map[string]any
	ExpectedOutput map[string]any
	VerifierType   string
	VerifierConfig map[string]any
	Weight         float64
	PublicPrompt   bool
	Enabled        bool
	SortOrder      int
}

type BenchmarkScheduleInput struct {
	Name      string
	CronExpr  string
	Enabled   bool
	TargetIDs []int64
	TaskCount int
	NextRunAt *time.Time
}

type BenchmarkCreateRunInput struct {
	Status             string
	TriggerType        string
	ScheduleID         *int64
	TaskCount          int
	PlannedTargetCount int
	PlannedTaskCount   int
	PlannedResultCount int
	CreatedBy          *int64
	Targets            []BenchmarkRunTargetInput
	Tasks              []BenchmarkRunTaskInput
}

type BenchmarkRunTargetInput struct {
	TargetID            int64
	ModelName           string
	ChannelID           int64
	DisplayNameSnapshot string
	ChannelNameSnapshot string
	TargetOrder         int
}

type BenchmarkRunTaskInput struct {
	TaskID                 int64
	TaskOrder              int
	Type                   string
	Difficulty             string
	WeightSnapshot         float64
	PromptSnapshot         string
	VerifierTypeSnapshot   string
	VerifierConfigSnapshot map[string]any
	TaskSnapshot           map[string]any
}

type BenchmarkRunResultContext struct {
	Result *ent.BenchmarkResult
	Run    *ent.BenchmarkRun
	Target *ent.BenchmarkRunTarget
	Task   *ent.BenchmarkRunTask
}

type BenchmarkRunScoreInput struct {
	RunTarget *ent.BenchmarkRunTarget
	RunTask   *ent.BenchmarkRunTask
	Result    *ent.BenchmarkResult
}

// BenchmarkResultUpdateInput uses three-state nullable semantics:
// nil leaves a field unchanged, a non-nil pointer sets it, and Clear* forces NULL.
type BenchmarkResultUpdateInput struct {
	Status               *string
	RequestID            *string
	ClearRequestID       bool
	NormalizedScore      *float64
	ClearNormalizedScore bool
	EvaluatorType        *string
	ClearEvaluatorType   bool
	EvaluatorOutput      map[string]any
	ClearEvaluatorOutput bool
	LatencyMS            *int
	ClearLatencyMS       bool
	PromptTokens         *int
	CompletionTokens     *int
	TotalTokens          *int
	EstimatedCost        *float64
	RawResponse          map[string]any
	ErrorCode            *string
	ClearErrorCode       bool
	ErrorMessage         *string
	ClearErrorMessage    bool
	AttemptCount         *int
	StartedAt            *time.Time
	ClearStartedAt       bool
	FinishedAt           *time.Time
	ClearFinishedAt      bool
}

type BenchmarkTargetScoreInput struct {
	RunTargetID            int64
	ModelName              string
	ChannelID              int64
	OverallScore           float64
	PassedCount            int
	TotalCount             int
	DimensionScores        map[string]any
	AvgLatencyMS           *float64
	AvgTotalTokens         *float64
	TotalCost              float64
	InvalidReasonBreakdown map[string]any
	FinishedAt             time.Time
}

type BenchmarkPublicSnapshotInput struct {
	RunID       int64
	Snapshot    map[string]any
	PublishedAt *time.Time
}
