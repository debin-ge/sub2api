package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const defaultRadarPublicMinBucketAccounts = 2

// ErrRadarQuotaAggregation is intentionally safe to surface to a background
// runner. Repository, credential, and account details are never wrapped into it.
var ErrRadarQuotaAggregation = errors.New("radar quota aggregation failed")

// RadarQuotaAggregationReport contains only bounded-cardinality operational
// counts. It deliberately excludes account identifiers, bucket keys, plan
// tiers, and raw model names so callers can safely attach it to logs/metrics.
type RadarQuotaAggregationReport struct {
	ScannedAccountCount        int
	CandidateAccountCount      int
	UsableAccountCount         int
	BucketCount                int
	SkippedAccountCount        int
	PrivacyFilteredBucketCount int
	InferenceRejectCounts      map[InferenceRejectReason]int
	SkippedAccountCounts       map[string]int
	InferenceCounts            map[RadarQuotaInferenceMetric]int
}

// RadarQuotaInferenceMetric is a bounded-cardinality aggregation result. Its
// Bucket value is a canonical platform family, never a raw public bucket key.
type RadarQuotaInferenceMetric struct {
	Bucket string
	Result string
	Reason InferenceRejectReason
}

const (
	radarQuotaSkipUsageReadError = "usage_read_error"
	radarQuotaSkipInvalidWindow  = "invalid_window"
	radarQuotaSkipInvalidBucket  = "invalid_bucket"
	radarQuotaSkipDuplicate      = "duplicate_account"
)

// radarQuotaAccountLister is the only account-repository capability required by
// Radar aggregation. Keeping it narrow prevents accidental per-account reads.
type radarQuotaAccountLister interface {
	ListAllWithFilters(
		ctx context.Context,
		platform, accountType, status, search string,
		groupID int64,
		privacyMode string,
	) ([]Account, error)
}

// radarQuotaSnapshotWriter excludes every Radar cache read and source method.
type radarQuotaSnapshotWriter interface {
	AppendBucketSnapshot(ctx context.Context, snapshot BucketSnapshotDTO) error
}

type radarQuotaAggregatorConfig struct {
	PublicMinBucketAccounts int
	InferMinUtilization     float64
	InferMaxStdevRatio      float64
}

// RadarQuotaAggregator creates privacy-gated, account-free quota bucket DTOs
// exclusively from passive snapshots and batch database aggregates.
type RadarQuotaAggregator struct {
	accountLister radarQuotaAccountLister
	usageReader   RadarUsageSnapshotReader
	batchReader   RadarQuotaBatchReader
	cacheWriter   radarQuotaSnapshotWriter
	cfg           radarQuotaAggregatorConfig
	now           func() time.Time
}

// NewRadarQuotaAggregator validates and copies its configuration. The returned
// service is immutable and safe for concurrent RunOnce calls when its injected
// repositories are themselves concurrency-safe.
func NewRadarQuotaAggregator(
	accountLister radarQuotaAccountLister,
	usageReader RadarUsageSnapshotReader,
	batchReader RadarQuotaBatchReader,
	cacheWriter radarQuotaSnapshotWriter,
	cfg *config.RadarConfig,
) (*RadarQuotaAggregator, error) {
	return newRadarQuotaAggregator(
		accountLister,
		usageReader,
		batchReader,
		cacheWriter,
		cfg,
		time.Now,
	)
}

func newRadarQuotaAggregator(
	accountLister radarQuotaAccountLister,
	usageReader RadarUsageSnapshotReader,
	batchReader RadarQuotaBatchReader,
	cacheWriter radarQuotaSnapshotWriter,
	cfg *config.RadarConfig,
	now func() time.Time,
) (*RadarQuotaAggregator, error) {
	dependencies := []struct {
		name  string
		value any
	}{
		{"account lister", accountLister},
		{"usage snapshot reader", usageReader},
		{"quota batch reader", batchReader},
		{"cache writer", cacheWriter},
	}
	for _, dependency := range dependencies {
		if isNilRadarQuotaDependency(dependency.value) {
			return nil, fmt.Errorf("radar quota aggregator requires %s", dependency.name)
		}
	}
	if cfg == nil {
		return nil, errors.New("radar quota aggregator requires config")
	}
	if now == nil {
		return nil, errors.New("radar quota aggregator requires clock")
	}

	copiedConfig := radarQuotaAggregatorConfig{
		PublicMinBucketAccounts: cfg.PublicMinBucketAccounts,
		InferMinUtilization:     cfg.InferMinUtilization,
		InferMaxStdevRatio:      cfg.InferMaxStdevRatio,
	}
	if copiedConfig.PublicMinBucketAccounts == 0 {
		copiedConfig.PublicMinBucketAccounts = defaultRadarPublicMinBucketAccounts
	}
	if copiedConfig.PublicMinBucketAccounts < defaultRadarPublicMinBucketAccounts {
		return nil, errors.New("radar quota public bucket minimum must be at least two")
	}
	if !isFinite(copiedConfig.InferMinUtilization) ||
		copiedConfig.InferMinUtilization <= 0 || copiedConfig.InferMinUtilization > 100 {
		return nil, errors.New("radar quota inference utilization threshold must be within (0, 100]")
	}
	if !isFinite(copiedConfig.InferMaxStdevRatio) ||
		copiedConfig.InferMaxStdevRatio <= 0 || copiedConfig.InferMaxStdevRatio > 1 {
		return nil, errors.New("radar quota inference deviation ratio must be within (0, 1]")
	}

	return &RadarQuotaAggregator{
		accountLister: accountLister,
		usageReader:   usageReader,
		batchReader:   batchReader,
		cacheWriter:   cacheWriter,
		cfg:           copiedConfig,
		now:           now,
	}, nil
}

func isNilRadarQuotaDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type radarQuotaBucketIdentity struct {
	bucketKey   string
	platform    string
	planTier    string
	displayName string
}

type radarQuotaBucketAccount struct {
	accountID     int64
	contributorID int64
	usage         *UsageInfo
	identity      radarQuotaBucketIdentity
	isShadow      bool
}

// RunOnce preserves the original error-only entry point for callers that do
// not need the low-cardinality operational report.
func (a *RadarQuotaAggregator) RunOnce(ctx context.Context) error {
	return a.runOnce(ctx, nil)
}

// RunOnceWithReport reads each eligible account strictly passively, performs
// exactly four batch aggregate reads for the successful unique account IDs,
// then publishes complete public buckets in deterministic key order.
func (a *RadarQuotaAggregator) RunOnceWithReport(ctx context.Context) (RadarQuotaAggregationReport, error) {
	report := RadarQuotaAggregationReport{}
	err := a.runOnce(ctx, &report)
	return report, err
}

func (a *RadarQuotaAggregator) runOnce(ctx context.Context, report *RadarQuotaAggregationReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	accounts, err := a.accountLister.ListAllWithFilters(ctx, "", "", "", "", 0, "")
	if err != nil {
		if terminal := radarQuotaContextError(ctx, err); terminal != nil {
			return terminal
		}
		return ErrRadarQuotaAggregation
	}
	if report != nil {
		report.ScannedAccountCount = len(accounts)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	readAt := a.now().UTC()
	capturedAt := readAt.Truncate(time.Millisecond)
	accountsByID := make(map[int64]*Account, len(accounts))
	for i := range accounts {
		if accounts[i].ID > 0 {
			accountsByID[accounts[i].ID] = &accounts[i]
		}
	}
	byContributorID := make(map[int64]radarQuotaBucketAccount)
	for i := range accounts {
		account := &accounts[i]
		identityAccount, contributorID, ok := resolveRadarQuotaCandidate(account, accountsByID)
		if !ok {
			continue
		}
		if report != nil {
			report.CandidateAccountCount++
		}

		usage, readErr := a.usageReader.GetRadarUsageSnapshot(ctx, account)
		if readErr != nil {
			if terminal := radarQuotaContextError(ctx, readErr); terminal != nil {
				return terminal
			}
			radarQuotaReportSkippedAccount(report, radarQuotaSkipUsageReadError)
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !radarQuotaUsageHasValidWindow(usage) {
			radarQuotaReportSkippedAccount(report, radarQuotaSkipInvalidWindow)
			continue
		}
		identity, ok := buildRadarQuotaBucketIdentity(identityAccount, usage)
		if !ok {
			radarQuotaReportSkippedAccount(report, radarQuotaSkipInvalidBucket)
			continue
		}
		candidate := radarQuotaBucketAccount{
			accountID:     account.ID,
			contributorID: contributorID,
			usage:         usage,
			identity:      identity,
			isShadow:      account.IsShadow(),
		}
		if existing, duplicate := byContributorID[contributorID]; duplicate {
			radarQuotaReportSkippedAccount(report, radarQuotaSkipDuplicate)
			if preferRadarQuotaCandidate(candidate, existing) {
				byContributorID[contributorID] = candidate
			}
			continue
		}
		byContributorID[contributorID] = candidate
	}
	if report != nil {
		report.UsableAccountCount = len(byContributorID)
	}

	if len(byContributorID) == 0 {
		return nil
	}

	accountIDs := make([]int64, 0, len(byContributorID))
	selectedByAccountID := make(map[int64]radarQuotaBucketAccount, len(byContributorID))
	for _, account := range byContributorID {
		accountIDs = append(accountIDs, account.accountID)
		selectedByAccountID[account.accountID] = account
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })

	window5h, err := a.batchReader.GetAccountWindowStatsBatch(ctx, accountIDs, readAt.Add(-5*time.Hour))
	if err != nil {
		return radarQuotaBatchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	window7d, err := a.batchReader.GetAccountWindowStatsBatch(ctx, accountIDs, readAt.Add(-7*24*time.Hour))
	if err != nil {
		return radarQuotaBatchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	breakdown5h, err := a.batchReader.GetAccountModelBreakdownBatch(ctx, accountIDs, readAt.Add(-5*time.Hour))
	if err != nil {
		return radarQuotaBatchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	breakdown7d, err := a.batchReader.GetAccountModelBreakdownBatch(ctx, accountIDs, readAt.Add(-7*24*time.Hour))
	if err != nil {
		return radarQuotaBatchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	buckets := make(map[string][]radarQuotaBucketAccount)
	for _, accountID := range accountIDs {
		account := selectedByAccountID[accountID]
		buckets[account.identity.bucketKey] = append(buckets[account.identity.bucketKey], account)
	}

	bucketKeys := make([]string, 0, len(buckets))
	for bucketKey, bucketAccounts := range buckets {
		if isRadarQuotaBucketPublic(len(bucketAccounts), a.cfg.PublicMinBucketAccounts) {
			bucketKeys = append(bucketKeys, bucketKey)
		} else if report != nil {
			report.PrivacyFilteredBucketCount++
			radarQuotaReportSkippedAccounts(report, "privacy_threshold", len(bucketAccounts))
		}
	}
	sort.Strings(bucketKeys)

	writeFailed := false
	for _, bucketKey := range bucketKeys {
		bucketAccounts := buckets[bucketKey]
		identity := bucketAccounts[0].identity
		snapshot := BucketSnapshotDTO{
			BucketKey:        identity.bucketKey,
			Platform:         identity.platform,
			PlanTier:         identity.planTier,
			DisplayName:      identity.displayName,
			AccountsCount:    len(bucketAccounts),
			PrivacyThreshold: a.cfg.PublicMinBucketAccounts,
			FiveHour:         aggregateRadarQuotaWindow(bucketAccounts, window5h, func(usage *UsageInfo) *UsageProgress { return usage.FiveHour }, a.cfg),
			SevenDay:         aggregateRadarQuotaWindow(bucketAccounts, window7d, func(usage *UsageInfo) *UsageProgress { return usage.SevenDay }, a.cfg),
			ModelBreakdown5h: aggregateRadarModelBreakdown(bucketAccounts, breakdown5h, identity.platform, a.cfg.PublicMinBucketAccounts),
			ModelBreakdown7d: aggregateRadarModelBreakdown(bucketAccounts, breakdown7d, identity.platform, a.cfg.PublicMinBucketAccounts),
			CapturedAt:       capturedAt,
		}
		if identity.platform == PlatformAnthropic {
			snapshot.SevenDaySonnet = aggregateRadarModelWindow(bucketAccounts, "claude-sonnet", a.cfg.PublicMinBucketAccounts, func(usage *UsageInfo) *UsageProgress {
				return usage.SevenDaySonnet
			})
			snapshot.SevenDayFable = aggregateRadarModelWindow(bucketAccounts, "claude-fable", a.cfg.PublicMinBucketAccounts, func(usage *UsageInfo) *UsageProgress {
				return usage.SevenDayFable
			})
		}
		if err := a.cacheWriter.AppendBucketSnapshot(ctx, snapshot); err != nil {
			if terminal := radarQuotaContextError(ctx, err); terminal != nil {
				return terminal
			}
			writeFailed = true
			continue
		}
		radarQuotaReportPublishedBucket(report, snapshot)
	}
	if writeFailed {
		return ErrRadarQuotaAggregation
	}
	return nil
}

// isRadarQuotaBucketPublic is shared by production publication and the
// offline data-quality verifier so the release check cannot drift from the
// actual privacy gate.
func isRadarQuotaBucketPublic(accountCount, minAccounts int) bool {
	return minAccounts >= defaultRadarPublicMinBucketAccounts && accountCount >= minAccounts
}

func radarQuotaReportSkippedAccount(report *RadarQuotaAggregationReport, reason string) {
	radarQuotaReportSkippedAccounts(report, reason, 1)
}

func radarQuotaReportSkippedAccounts(report *RadarQuotaAggregationReport, reason string, count int) {
	if report != nil {
		report.SkippedAccountCount += count
		if report.SkippedAccountCounts == nil {
			report.SkippedAccountCounts = make(map[string]int, 4)
		}
		report.SkippedAccountCounts[reason] += count
	}
}

func radarQuotaReportPublishedBucket(report *RadarQuotaAggregationReport, snapshot BucketSnapshotDTO) {
	if report == nil {
		return
	}
	report.BucketCount++
	for _, window := range []*WindowStatsDTO{snapshot.FiveHour, snapshot.SevenDay} {
		if window == nil {
			continue
		}
		metric := RadarQuotaInferenceMetric{Bucket: snapshot.Platform, Result: "success"}
		if window.InferenceRejectReason != nil {
			metric.Result = "rejected"
			metric.Reason = *window.InferenceRejectReason
			radarQuotaReportInferenceReject(report, *window.InferenceRejectReason)
		}
		if report.InferenceCounts == nil {
			report.InferenceCounts = make(map[RadarQuotaInferenceMetric]int, 6)
		}
		report.InferenceCounts[metric]++
	}
}

func radarQuotaReportInferenceReject(report *RadarQuotaAggregationReport, reason InferenceRejectReason) {
	if report.InferenceRejectCounts == nil {
		report.InferenceRejectCounts = make(map[InferenceRejectReason]int, 3)
	}
	report.InferenceRejectCounts[reason]++
}

func radarQuotaContextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func radarQuotaBatchError(ctx context.Context, err error) error {
	if terminal := radarQuotaContextError(ctx, err); terminal != nil {
		return terminal
	}
	return ErrRadarQuotaAggregation
}

func isRadarQuotaCandidate(account *Account) bool {
	if account == nil || account.ID <= 0 || account.IsShadow() {
		return false
	}
	if account.IsAnthropicOAuthOrSetupToken() {
		return true
	}
	if account.IsOpenAIOAuth() {
		planType := strings.ToLower(strings.TrimSpace(account.GetCredential("plan_type")))
		return planType != "free" && planType != "abnormal"
	}
	return account.Platform == PlatformAntigravity &&
		(account.Type == AccountTypeOAuth || account.Type == AccountTypeUpstream)
}

// resolveRadarQuotaCandidate maps a Spark shadow's passive quota snapshot back
// to its credential-owning parent. The parent remains the privacy contributor
// and plan identity, while batch usage statistics are read from the shadow row
// that actually handled the Spark traffic. This also prevents a parent and its
// shadow from being counted twice.
func resolveRadarQuotaCandidate(account *Account, accountsByID map[int64]*Account) (*Account, int64, bool) {
	if isRadarQuotaCandidate(account) {
		return account, account.ID, true
	}
	if account == nil || account.ID <= 0 || !account.IsShadow() || !account.IsOpenAIOAuth() ||
		account.QuotaDimensionOrDefault() != QuotaDimensionSpark || account.ParentAccountID == nil {
		return nil, 0, false
	}
	parent := accountsByID[*account.ParentAccountID]
	if !isRadarQuotaCandidate(parent) || !parent.IsOpenAIOAuth() {
		return nil, 0, false
	}
	return parent, parent.ID, true
}

func preferRadarQuotaCandidate(candidate, existing radarQuotaBucketAccount) bool {
	if candidate.isShadow != existing.isShadow {
		return !candidate.isShadow
	}
	if candidate.usage != nil && candidate.usage.UpdatedAt != nil &&
		(existing.usage == nil || existing.usage.UpdatedAt == nil || candidate.usage.UpdatedAt.After(*existing.usage.UpdatedAt)) {
		return true
	}
	return candidate.accountID < existing.accountID
}

func radarQuotaUsageHasValidWindow(usage *UsageInfo) bool {
	if usage == nil {
		return false
	}
	for _, window := range []*UsageProgress{
		usage.FiveHour,
		usage.SevenDay,
		usage.SevenDaySonnet,
		usage.SevenDayFable,
	} {
		if window != nil && isFinite(window.Utilization) &&
			window.Utilization >= 0 && window.Utilization <= 100 {
			return true
		}
	}
	return false
}

func buildRadarQuotaBucketIdentity(account *Account, usage *UsageInfo) (radarQuotaBucketIdentity, bool) {
	if account == nil || usage == nil {
		return radarQuotaBucketIdentity{}, false
	}

	var platform, rawTier, fallback string
	switch {
	case account.IsAnthropicOAuthOrSetupToken():
		platform = PlatformAnthropic
		fallback = "generic"
		if account.Extra != nil {
			if raw, exists := account.Extra["plan_slug"]; exists && raw != nil {
				value, ok := raw.(string)
				if !ok {
					return radarQuotaBucketIdentity{}, false
				}
				rawTier = value
			}
		}
	case account.IsOpenAIOAuth():
		platform = PlatformOpenAI
		fallback = "unknown"
		if account.Credentials != nil {
			if raw, exists := account.Credentials["plan_type"]; exists && raw != nil {
				value, ok := raw.(string)
				if !ok {
					return radarQuotaBucketIdentity{}, false
				}
				rawTier = value
			}
		}
	case account.Platform == PlatformAntigravity &&
		(account.Type == AccountTypeOAuth || account.Type == AccountTypeUpstream):
		platform = PlatformAntigravity
		fallback = "basic"
		rawTier = usage.SubscriptionTier
	default:
		return radarQuotaBucketIdentity{}, false
	}

	planTier := strings.ToLower(strings.TrimSpace(rawTier))
	if planTier == "" {
		planTier = fallback
	}
	if !isSafeRadarQuotaPlanTier(planTier) {
		return radarQuotaBucketIdentity{}, false
	}
	return radarQuotaBucketIdentity{
		bucketKey:   platform + "/" + planTier,
		platform:    platform,
		planTier:    planTier,
		displayName: radarQuotaDisplayName(platform, planTier),
	}, true
}

func isSafeRadarQuotaPlanTier(value string) bool {
	if len(value) == 0 || len(value) > 64 || !isRadarQuotaLowerAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isRadarQuotaLowerAlphaNumeric(value[i]) && value[i] != '.' && value[i] != '_' && value[i] != '-' {
			return false
		}
	}
	return true
}

func isRadarQuotaLowerAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func radarQuotaDisplayName(platform, planTier string) string {
	switch platform {
	case PlatformAnthropic:
		return "Claude " + radarQuotaHumanizeTier(planTier)
	case PlatformOpenAI:
		if planTier == "unknown" {
			return "ChatGPT"
		}
		return "ChatGPT " + radarQuotaHumanizeTier(planTier)
	case PlatformAntigravity:
		return "Antigravity " + radarQuotaHumanizeTier(planTier)
	default:
		return ""
	}
}

func radarQuotaHumanizeTier(planTier string) string {
	words := strings.FieldsFunc(planTier, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

type radarQuotaInferenceSample struct {
	utilization float64
	cost        float64
}

type radarQuotaInferenceResult struct {
	limit        *float64
	stdev        *float64
	sampleSize   int
	rejectReason *InferenceRejectReason
}

// inferLimit is a deterministic, I/O-free implementation of the public quota
// inference formula and population-standard-deviation confidence gate.
func inferLimit(samples []radarQuotaInferenceSample, minUtilization, maxStdevRatio float64) radarQuotaInferenceResult {
	candidates := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if !isFinite(sample.utilization) || !isFinite(sample.cost) ||
			sample.utilization < minUtilization || sample.cost < 0 {
			continue
		}
		candidate := sample.cost / (sample.utilization / 100)
		if isFinite(candidate) && candidate >= 0 {
			candidates = append(candidates, candidate)
		}
	}

	result := radarQuotaInferenceResult{sampleSize: len(candidates)}
	if len(candidates) < 2 {
		result.rejectReason = radarInferenceReason(InferenceRejectReasonInsufficientSamples)
		return result
	}

	mean := 0.0
	for i, candidate := range candidates {
		mean += (candidate - mean) / float64(i+1)
	}
	if !isFinite(mean) || mean <= 0 {
		result.rejectReason = radarInferenceReason(InferenceRejectReasonInvalidMean)
		return result
	}

	varianceRatio := 0.0
	for _, candidate := range candidates {
		normalizedDifference := candidate/mean - 1
		varianceRatio += normalizedDifference * normalizedDifference
	}
	varianceRatio /= float64(len(candidates))
	ratio := math.Sqrt(varianceRatio)
	// A mathematically exact boundary can round one ULP above the configured
	// decimal (for example 0.3). Preserve the specified inclusive boundary.
	if !isFinite(ratio) || ratio > math.Nextafter(maxStdevRatio, math.Inf(1)) {
		result.rejectReason = radarInferenceReason(InferenceRejectReasonHighDispersion)
		return result
	}
	stdev := ratio * mean
	if !isFinite(stdev) {
		result.rejectReason = radarInferenceReason(InferenceRejectReasonHighDispersion)
		return result
	}
	result.limit = &mean
	result.stdev = &stdev
	return result
}

func radarInferenceReason(reason InferenceRejectReason) *InferenceRejectReason {
	return &reason
}

func aggregateRadarQuotaWindow(
	accounts []radarQuotaBucketAccount,
	statsByAccount map[int64]*usagestats.AccountStats,
	window func(*UsageInfo) *UsageProgress,
	cfg radarQuotaAggregatorConfig,
) *WindowStatsDTO {
	utilizations := make([]float64, 0, len(accounts))
	costs := make([]float64, 0, len(accounts))
	inferenceSamples := make([]radarQuotaInferenceSample, 0, len(accounts))
	for _, account := range accounts {
		progress := window(account.usage)
		if progress == nil || !isFinite(progress.Utilization) || progress.Utilization < 0 || progress.Utilization > 100 {
			continue
		}
		cost := 0.0
		inferenceCost := math.NaN()
		if stats := statsByAccount[account.accountID]; stats != nil && isFinite(stats.Cost) && stats.Cost >= 0 {
			cost = stats.Cost
			inferenceCost = stats.Cost
		}
		utilizations = append(utilizations, progress.Utilization)
		costs = append(costs, cost)
		inferenceSamples = append(inferenceSamples, radarQuotaInferenceSample{utilization: progress.Utilization, cost: inferenceCost})
	}
	if len(utilizations) == 0 {
		return nil
	}
	if len(utilizations) < cfg.PublicMinBucketAccounts {
		return nil
	}

	minUtilization, maxUtilization := utilizations[0], utilizations[0]
	avgUtilization, avgCost := 0.0, 0.0
	for i, utilization := range utilizations {
		if utilization < minUtilization {
			minUtilization = utilization
		}
		if utilization > maxUtilization {
			maxUtilization = utilization
		}
		avgUtilization += (utilization - avgUtilization) / float64(i+1)
		avgCost += (costs[i] - avgCost) / float64(i+1)
	}

	inference := inferLimit(inferenceSamples, cfg.InferMinUtilization, cfg.InferMaxStdevRatio)
	if inference.sampleSize < cfg.PublicMinBucketAccounts {
		inference.limit = nil
		inference.stdev = nil
		inference.rejectReason = radarInferenceReason(InferenceRejectReasonInsufficientSamples)
	}
	return &WindowStatsDTO{
		AvgUtilization:        avgUtilization,
		MinUtilization:        minUtilization,
		MaxUtilization:        maxUtilization,
		AvgCost:               avgCost,
		InferredLimitUSD:      inference.limit,
		InferredStdev:         inference.stdev,
		SampleSize:            inference.sampleSize,
		ContributorsCount:     len(utilizations),
		InferenceRejectReason: inference.rejectReason,
	}
}

func aggregateRadarModelWindow(
	accounts []radarQuotaBucketAccount,
	model string,
	minContributors int,
	window func(*UsageInfo) *UsageProgress,
) *ModelWindowStatsDTO {
	avgUtilization := 0.0
	sampleSize := 0
	for _, account := range accounts {
		progress := window(account.usage)
		if progress == nil || !isFinite(progress.Utilization) || progress.Utilization < 0 || progress.Utilization > 100 {
			continue
		}
		sampleSize++
		avgUtilization += (progress.Utilization - avgUtilization) / float64(sampleSize)
	}
	if sampleSize < minContributors {
		return nil
	}
	return &ModelWindowStatsDTO{Model: model, AvgUtilization: avgUtilization, SampleSize: sampleSize}
}

type radarModelTotals struct {
	avgCost      float64
	requests     int64
	contributors map[int64]struct{}
}

func aggregateRadarModelBreakdown(
	accounts []radarQuotaBucketAccount,
	statsByAccount map[int64]map[string]ModelCostStats,
	platform string,
	minContributors int,
) []ModelCostBreakdownDTO {
	result := make([]ModelCostBreakdownDTO, 0)
	if len(accounts) == 0 {
		return result
	}

	denominator := float64(len(accounts))
	canonicalModels := make(map[string]string)
	for _, canonical := range DefaultModelCatalogIDs(platform) {
		normalized := strings.ToLower(strings.TrimSpace(canonical))
		if normalized != "" {
			if _, exists := canonicalModels[normalized]; !exists {
				canonicalModels[normalized] = canonical
			}
		}
	}
	modelTotals := make(map[string]radarModelTotals)
	for _, account := range accounts {
		for rawModel, stats := range statsByAccount[account.accountID] {
			label := "other"
			if canonical, ok := canonicalModels[strings.ToLower(strings.TrimSpace(rawModel))]; ok {
				label = canonical
			}
			validCost := isFinite(stats.AccountCost) && stats.AccountCost > 0
			validRequests := stats.Requests > 0
			if !validCost && !validRequests {
				continue
			}
			totals := modelTotals[label]
			if totals.contributors == nil {
				totals.contributors = make(map[int64]struct{})
			}
			contributorID := account.contributorID
			if contributorID <= 0 {
				contributorID = account.accountID
			}
			totals.contributors[contributorID] = struct{}{}
			if validCost {
				totals.avgCost = finiteRadarAdd(totals.avgCost, stats.AccountCost/denominator)
			}
			if validRequests {
				totals.requests = saturatedRadarAddInt64(totals.requests, stats.Requests)
			}
			modelTotals[label] = totals
		}
	}

	other := modelTotals["other"]
	for model, totals := range modelTotals {
		if model == "other" || len(totals.contributors) >= minContributors {
			continue
		}
		other = mergeRadarModelTotals(other, totals)
		delete(modelTotals, model)
	}
	if len(other.contributors) > 0 {
		modelTotals["other"] = other
	}

	maxCost := 0.0
	for _, totals := range modelTotals {
		if len(totals.contributors) < minContributors {
			continue
		}
		if totals.avgCost > maxCost {
			maxCost = totals.avgCost
		}
	}
	scaledTotal := 0.0
	if maxCost > 0 {
		for _, totals := range modelTotals {
			if len(totals.contributors) < minContributors {
				continue
			}
			scaledTotal += totals.avgCost / maxCost
		}
	}

	for model, totals := range modelTotals {
		if len(totals.contributors) < minContributors || totals.avgCost <= 0 && totals.requests <= 0 {
			continue
		}
		percentage := 0.0
		if maxCost > 0 && scaledTotal > 0 && isFinite(scaledTotal) {
			percentage = (totals.avgCost / maxCost) / scaledTotal * 100
			if !isFinite(percentage) {
				percentage = 0
			}
		}
		result = append(result, ModelCostBreakdownDTO{
			Model:             model,
			AvgCost:           totals.avgCost,
			AvgRequests:       roundedRadarAverageRequests(totals.requests, int64(len(accounts))),
			Percentage:        percentage,
			ContributorsCount: len(totals.contributors),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].AvgCost != result[j].AvgCost {
			return result[i].AvgCost > result[j].AvgCost
		}
		if result[i].Percentage != result[j].Percentage {
			return result[i].Percentage > result[j].Percentage
		}
		return result[i].Model < result[j].Model
	})
	return result
}

func mergeRadarModelTotals(target, source radarModelTotals) radarModelTotals {
	target.avgCost = finiteRadarAdd(target.avgCost, source.avgCost)
	target.requests = saturatedRadarAddInt64(target.requests, source.requests)
	if target.contributors == nil {
		target.contributors = make(map[int64]struct{}, len(source.contributors))
	}
	for accountID := range source.contributors {
		target.contributors[accountID] = struct{}{}
	}
	return target
}

func roundedRadarAverageRequests(total, denominator int64) int64 {
	if total <= 0 || denominator <= 0 {
		return 0
	}
	quotient, remainder := total/denominator, total%denominator
	if remainder >= (denominator+1)/2 {
		return saturatedRadarAddInt64(quotient, 1)
	}
	return quotient
}

func saturatedRadarAddInt64(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func finiteRadarAdd(left, right float64) float64 {
	result := left + right
	if math.IsInf(result, 1) {
		return math.MaxFloat64
	}
	if !isFinite(result) {
		return 0
	}
	return result
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
