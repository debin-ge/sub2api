package service

import "time"

const (
	BenchmarkConfidenceHigh   = "high"
	BenchmarkConfidenceMedium = "medium"
	BenchmarkConfidenceLow    = "low"

	// Run status: a simple 5-state machine.
	BenchmarkRunStatusQueued    = "queued"
	BenchmarkRunStatusRunning   = "running"
	BenchmarkRunStatusCompleted = "completed"
	BenchmarkRunStatusFailed    = "failed"
	BenchmarkRunStatusCanceled  = "canceled"

	// Result status: terminal reasons are kept so failures stay diagnosable.
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

	BenchmarkTriggerManual    = "manual"
	BenchmarkTriggerScheduled = "scheduled"
)

// BenchmarkTaskCandidate is a lightweight view of an enabled task used when
// selecting which tasks a run executes (all enabled, or the first N).
type BenchmarkTaskCandidate struct {
	ID        int64
	Type      string
	SortOrder int
	Enabled   bool
}

// BenchmarkScoredResult is one task x target result fed into score aggregation.
type BenchmarkScoredResult struct {
	TaskID          int64
	TaskType        string
	Weight          float64
	Status          string
	NormalizedScore float64
	LatencyMS       int
	TotalTokens     int
	EstimatedCost   float64
}

// BenchmarkTargetScoreResult is the aggregated ability score for one target in
// one run. It is the trend data point.
type BenchmarkTargetScoreResult struct {
	OverallScore           float64
	DimensionScores        map[string]float64
	PassedCount            int
	TotalCount             int
	InvalidReasonBreakdown map[string]int
	AvgLatencyMS           float64
	AvgTotalTokens         float64
	TotalCost              float64
	ComputedAt             time.Time
}
