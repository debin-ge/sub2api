package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

type BenchmarkRepository interface {
	CreateSuite(ctx context.Context, input BenchmarkSuiteInput) (*ent.BenchmarkSuite, error)
	ListSuites(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkSuite, int, error)
	CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error)
	ListTargets(ctx context.Context, input BenchmarkListInput) ([]*ent.BenchmarkTarget, int, error)
	CreateTask(ctx context.Context, input BenchmarkTaskInput) (*ent.BenchmarkTask, error)
	ListTasks(ctx context.Context, input BenchmarkTaskListInput) ([]*ent.BenchmarkTask, int, error)
	CreateProfile(ctx context.Context, input BenchmarkProfileInput) (*ent.BenchmarkProfile, error)
	GetProfile(ctx context.Context, id int64) (*ent.BenchmarkProfile, error)
	CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error)
	ListRuns(ctx context.Context, input BenchmarkRunListInput) ([]*ent.BenchmarkRun, int, error)
	ListRunResults(ctx context.Context, runID int64) ([]*ent.BenchmarkResult, error)
	UpdateResult(ctx context.Context, id int64, input BenchmarkResultUpdateInput) error
	SaveScoreSnapshots(ctx context.Context, runID int64, snapshots []BenchmarkScoreSnapshotInput) error
	PublishPublicSnapshot(ctx context.Context, input BenchmarkPublicSnapshotInput) error
	GetLatestPublicSnapshot(ctx context.Context) (*ent.BenchmarkPublicSnapshot, error)
}

type BenchmarkListInput struct {
	Page     int
	PageSize int
}

type BenchmarkTaskListInput struct {
	BenchmarkListInput
	SuiteID   int64
	TaskTypes []string
	Enabled   *bool
}

type BenchmarkRunListInput struct {
	BenchmarkListInput
	SuiteID   int64
	ProfileID int64
	Status    []string
}

type BenchmarkSuiteInput struct {
	Name             string
	Slug             string
	Description      string
	Enabled          bool
	PublicVisible    bool
	DefaultProfileID *int64
	Metadata         map[string]any
}

type BenchmarkTargetInput struct {
	ModelName           string
	ChannelID           int64
	DisplayName         string
	ProviderSnapshot    string
	ChannelNameSnapshot string
	SupportedTaskTypes  []string
	MaxConcurrency      int
	PerRunBudget        *float64
	DailyBudget         *float64
	Enabled             bool
	PublicVisible       bool
	SortOrder           int
	Metadata            map[string]any
}

type BenchmarkTaskInput struct {
	SuiteID        int64
	Title          string
	Type           string
	Category       string
	Difficulty     string
	Tags           []string
	Prompt         string
	InputPayload   map[string]any
	ExpectedOutput map[string]any
	VerifierType   string
	VerifierConfig map[string]any
	Weight         float64
	MinScale       string
	PublicPrompt   bool
	Enabled        bool
	Metadata       map[string]any
}

type BenchmarkProfileInput struct {
	SuiteID          int64
	Name             string
	Description      string
	TargetIDs        []int64
	TaskTypes        []string
	TaskScale        string
	TaskCountLimit   *int
	PerTypeLimit     map[string]int
	DifficultyFilter []string
	TagFilter        []string
	SamplingStrategy string
	SelectionSeed    *int64
	RuntimeConfig    map[string]any
	ScoringConfig    map[string]any
	Metadata         map[string]any
	Enabled          bool
}

type BenchmarkCreateRunInput struct {
	SuiteID            int64
	ProfileID          int64
	Status             string
	TriggerType        string
	TaskScale          string
	TaskTypes          []string
	SelectionSeed      *int64
	PlannedTargetCount int
	PlannedTaskCount   int
	PlannedResultCount int
	StartedAt          *time.Time
	FinishedAt         *time.Time
	ConfigSnapshot     map[string]any
	ErrorMessage       *string
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
	ProviderSnapshot    string
	TargetOrder         int
	ConfigSnapshot      map[string]any
}

type BenchmarkRunTaskInput struct {
	TaskID                 int64
	TaskOrder              int
	Type                   string
	Category               string
	Difficulty             string
	WeightSnapshot         float64
	PromptSnapshot         string
	VerifierTypeSnapshot   string
	VerifierConfigSnapshot map[string]any
	TaskSnapshot           map[string]any
}

type BenchmarkResultUpdateInput struct {
	Status           *string
	RequestID        *string
	Score            *float64
	MaxScore         *float64
	NormalizedScore  *float64
	EvaluatorType    *string
	EvaluatorOutput  map[string]any
	LatencyMS        *int
	PromptTokens     *int
	CompletionTokens *int
	TotalTokens      *int
	EstimatedCost    *float64
	RawResponse      map[string]any
	ErrorCode        *string
	ErrorMessage     *string
	AttemptCount     *int
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

type BenchmarkScoreSnapshotInput struct {
	RunTargetID            int64
	OverallScore           float64
	DimensionScores        map[string]any
	PlannedTasks           int
	ScoredTasks            int
	InvalidTasks           int
	CoverageRate           float64
	ConfidenceLevel        string
	InsufficientSample     bool
	SuccessRate            float64
	LatencyP50MS           *float64
	LatencyP95MS           *float64
	AvgTotalTokens         *float64
	EstimatedCost          float64
	InvalidReasonBreakdown map[string]any
	RankingMetadata        map[string]any
}

type BenchmarkPublicSnapshotInput struct {
	RunID       int64
	SuiteID     int64
	ProfileID   int64
	Snapshot    map[string]any
	PublishedAt *time.Time
}
