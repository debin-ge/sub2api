package service

import "time"

const (
	BenchmarkTaskScaleSmall  = "small"
	BenchmarkTaskScaleMedium = "medium"
	BenchmarkTaskScaleFull   = "full"
	BenchmarkTaskScaleCustom = "custom"

	BenchmarkConfidenceHigh   = "high"
	BenchmarkConfidenceMedium = "medium"
	BenchmarkConfidenceLow    = "low"

	BenchmarkRunStatusPending      = "pending"
	BenchmarkRunStatusSelecting    = "selecting"
	BenchmarkRunStatusQueued       = "queued"
	BenchmarkRunStatusRunning      = "running"
	BenchmarkRunStatusScoring      = "scoring"
	BenchmarkRunStatusSnapshotting = "snapshotting"
	BenchmarkRunStatusCompleted    = "completed"
	BenchmarkRunStatusFailed       = "failed"
	BenchmarkRunStatusCanceled     = "canceled"

	BenchmarkResultStatusPending       = "pending"
	BenchmarkResultStatusRunning       = "running"
	BenchmarkResultStatusScored        = "scored"
	BenchmarkResultStatusFailed        = "failed"
	BenchmarkResultStatusTimeout       = "timeout"
	BenchmarkResultStatusChannelError  = "channel_error"
	BenchmarkResultStatusParseError    = "parse_error"
	BenchmarkResultStatusRateLimited   = "rate_limited"
	BenchmarkResultStatusVerifierError = "verifier_error"
	BenchmarkResultStatusSkipped       = "skipped"
)

type BenchmarkTaskCandidate struct {
	ID         int64
	Type       string
	Category   string
	Difficulty string
	Tags       []string
	Weight     float64
	MinScale   string
	Enabled    bool
}

type BenchmarkSelectionConfig struct {
	TaskTypes        []string
	TaskScale        string
	TaskCountLimit   int
	PerTypeLimit     map[string]int
	DifficultyFilter []string
	TagFilter        []string
	SelectionSeed    int64
}

type BenchmarkConfidenceThresholds struct {
	HighCoverage   float64
	MediumCoverage float64
}

type BenchmarkScoredResult struct {
	TaskID           int64
	TaskType         string
	Weight           float64
	Status           string
	NormalizedScore  float64
	LatencyMS        int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCost    float64
}

type BenchmarkTargetScore struct {
	OverallScore       float64
	DimensionScores    map[string]float64
	PlannedTasks       int
	ScoredTasks        int
	InvalidTasks       int
	CoverageRate       float64
	ConfidenceLevel    string
	InsufficientSample bool
	SuccessRate        float64
	LatencyP50MS       int
	LatencyP95MS       int
	AvgTotalTokens     float64
	EstimatedCost      float64
	ComputedAt         time.Time
}
