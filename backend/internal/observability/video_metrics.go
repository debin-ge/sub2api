package observability

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const videoMetricsNamespace = "video_platform"

var (
	defaultVideoMetricsOnce sync.Once
	defaultVideoMetrics     *VideoMetrics
)

type VideoMetrics struct {
	submissions       *prometheus.CounterVec
	submitDuration    *prometheus.HistogramVec
	providerGets      *prometheus.CounterVec
	pollDuration      *prometheus.HistogramVec
	stateTransitions  *prometheus.CounterVec
	taskCurrent       *prometheus.GaugeVec
	taskStateAge      *prometheus.GaugeVec
	submissionUnknown prometheus.Gauge
	unknownHoldAmount prometheus.Gauge
	heldAmount        prometheus.Gauge
	oldestSettlement  prometheus.Gauge
	manualReviewAge   prometheus.Gauge
	deletePending     prometheus.Gauge
	oldestDelete      prometheus.Gauge
	holds             *prometheus.CounterVec
	holdAmounts       *prometheus.CounterVec
	settlements       *prometheus.CounterVec
	settlementAmounts *prometheus.CounterVec
	overCaptures      *prometheus.CounterVec
	overCaptureAmount *prometheus.CounterVec
	workerRecoveries  *prometheus.CounterVec
	workerItems       *prometheus.CounterVec
	queueDepth        *prometheus.GaugeVec
	spoolBytes        prometheus.Gauge
	spoolMaxBytes     prometheus.Gauge
	spoolUtilization  prometheus.Gauge
	spoolActive       prometheus.Gauge
	spoolOrphans      prometheus.Gauge
	spoolCleanup      *prometheus.CounterVec
	webhooks          *prometheus.CounterVec
	webhookDelay      *prometheus.HistogramVec
	callbacks         *prometheus.CounterVec
	callbackDuration  *prometheus.HistogramVec
	callbackDelay     *prometheus.HistogramVec
	contentRequests   *prometheus.CounterVec
	contentDuration   *prometheus.HistogramVec
	contentTTFB       *prometheus.HistogramVec
	contentBytes      *prometheus.CounterVec
	contentStreams    *prometheus.CounterVec
	contentActive     prometheus.Gauge
	accessDisclosures *prometheus.CounterVec
	capabilityProbes  *prometheus.CounterVec
}

type VideoTaskStateMetric struct {
	Provider        string
	Operation       string
	State           string
	Count           int64
	OldestEnteredAt *time.Time
}

type VideoOperationalMetrics struct {
	DeletePending           int64
	OldestDeletePending     *time.Time
	TaskStates              []VideoTaskStateMetric
	SubmissionUnknown       int64
	UnknownHoldAmount       float64
	HeldAmount              float64
	OldestSettlementPending *time.Time
	OldestManualReview      *time.Time
}

func DefaultVideoMetrics() *VideoMetrics {
	defaultVideoMetricsOnce.Do(func() {
		metrics, err := NewVideoMetrics(prometheus.DefaultRegisterer)
		if err != nil {
			panic(err)
		}
		defaultVideoMetrics = metrics
	})
	return defaultVideoMetrics
}

func NewVideoMetrics(registerer prometheus.Registerer) (*VideoMetrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &VideoMetrics{
		submissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "submissions_total",
			Help: "Video submissions by bounded provider, operation, and result.",
		}, []string{"provider", "operation", "result"}),
		submitDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "submission_duration_seconds",
			Help:    "End-to-end synchronous video submission duration.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 180},
		}, []string{"provider", "operation"}),
		providerGets: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "provider_get_total",
			Help: "Video Provider Get attempts by provider, caller, and result.",
		}, []string{"provider", "caller", "result"}),
		pollDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "poll_duration_seconds",
			Help:    "Video Provider poll latency by bounded provider and result.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"provider", "result"}),
		stateTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "state_transitions_total",
			Help: "Accepted video generation state observations.",
		}, []string{"provider", "state"}),
		taskCurrent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "tasks_current",
			Help: "Current video task count by bounded provider, operation, and generation state.",
		}, []string{"provider", "operation", "state"}),
		taskStateAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "task_state_oldest_age_seconds",
			Help: "Age of the oldest video task in each bounded provider, operation, and generation state.",
		}, []string{"provider", "operation", "state"}),
		submissionUnknown: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "submission_unknown_current",
			Help: "Current number of video tasks with an unknown submission outcome.",
		}),
		unknownHoldAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "submission_unknown_hold_amount",
			Help: "Current frozen balance amount attached to unknown video submissions.",
		}),
		heldAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "held_amount",
			Help: "Current frozen balance amount attached to unsettled video tasks.",
		}),
		oldestSettlement: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "oldest_settlement_pending_age_seconds",
			Help: "Age of the oldest pending video capture or release.",
		}),
		manualReviewAge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "oldest_manual_review_age_seconds",
			Help: "Age of the oldest video billing task in manual review.",
		}),
		deletePending: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "delete_pending_current", Help: "Video tasks awaiting content deletion.",
		}),
		oldestDelete: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "oldest_delete_pending_age_seconds", Help: "Age since the oldest pending video delete request.",
		}),
		holds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "holds_total",
			Help: "Video balance hold attempts by result.",
		}, []string{"result"}),
		holdAmounts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "hold_amount_total",
			Help: "Cumulative requested video balance hold amount by result.",
		}, []string{"result"}),
		settlements: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "settlements_total",
			Help: "Video balance settlement attempts by action and result.",
		}, []string{"action", "result"}),
		settlementAmounts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "settlement_amount_total",
			Help: "Cumulative video capture or release amount by action and result.",
		}, []string{"action", "result"}),
		overCaptures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "over_captures_total",
			Help: "Video capture attempts whose actual amount exceeded the hold.",
		}, []string{"result"}),
		overCaptureAmount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "over_capture_amount_total",
			Help: "Cumulative video amount captured beyond the original hold.",
		}, []string{"result"}),
		workerRecoveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "worker_recovery_total",
			Help: "Video worker recovery operations by bounded kind and result.",
		}, []string{"kind", "result"}),
		workerItems: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "worker_recovered_items_total",
			Help: "Video task items recovered by bounded recovery kind.",
		}, []string{"kind"}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "queue_depth",
			Help: "Current Redis video task queue depth by bounded queue state.",
		}, []string{"queue"}),
		spoolBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "spool_bytes",
			Help: "Current encrypted video submission spool bytes.",
		}),
		spoolMaxBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "spool_max_bytes",
			Help: "Configured encrypted video submission spool byte limit.",
		}),
		spoolUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "spool_utilization_ratio",
			Help: "Current encrypted video submission spool utilization ratio.",
		}),
		spoolActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "spool_active_sessions",
			Help: "Current active encrypted video submission spool sessions.",
		}),
		spoolOrphans: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "spool_orphan_candidates",
			Help: "Current expired video spool directories that could not be removed.",
		}),
		spoolCleanup: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "spool_cleanup_total",
			Help: "Video spool cleanup operations by bounded kind and result.",
		}, []string{"kind", "result"}),
		webhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "webhooks_total",
			Help: "Video Provider webhook outcomes by bounded provider and result.",
		}, []string{"provider", "result"}),
		webhookDelay: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "webhook_delay_seconds",
			Help:    "Delay from Provider webhook occurrence to local verification.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 900},
		}, []string{"provider"}),
		callbacks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "callbacks_total",
			Help: "Video callback delivery outcomes.",
		}, []string{"result"}),
		callbackDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "callback_duration_seconds",
			Help:    "Video callback delivery attempt duration by bounded result.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"result"}),
		callbackDelay: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "callback_delay_seconds",
			Help:    "Delay from callback creation to each delivery attempt.",
			Buckets: []float64{0.1, 1, 5, 10, 30, 60, 300, 900, 3600, 21600, 86400},
		}, []string{"result"}),
		contentRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "content_requests_total",
			Help: "Video content proxy requests by variant and response status class.",
		}, []string{"variant", "status"}),
		contentDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "content_duration_seconds",
			Help:    "Video content proxy request duration, including downstream streaming.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"variant"}),
		contentTTFB: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: videoMetricsNamespace, Name: "content_ttfb_seconds",
			Help:    "Time until the video Provider returns content response headers.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"variant"}),
		contentBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "content_bytes_total",
			Help: "Video content bytes written to downstream clients.",
		}, []string{"variant"}),
		contentStreams: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "content_streams_total",
			Help: "Video content stream outcomes with bounded result labels.",
		}, []string{"variant", "result"}),
		contentActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: videoMetricsNamespace, Name: "content_active",
			Help: "Current number of active video content proxy requests after concurrency admission.",
		}),
		accessDisclosures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "access_disclosures_total",
			Help: "Provider identity and access disclosures by bounded kind and policy.",
		}, []string{"kind", "policy"}),
		capabilityProbes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: videoMetricsNamespace, Name: "capability_probes_total",
			Help: "Video account capability probes by bounded provider and status.",
		}, []string{"provider", "status"}),
	}
	collectors := []prometheus.Collector{
		metrics.submissions, metrics.submitDuration, metrics.providerGets, metrics.pollDuration,
		metrics.stateTransitions, metrics.taskCurrent, metrics.taskStateAge,
		metrics.submissionUnknown, metrics.unknownHoldAmount, metrics.heldAmount,
		metrics.oldestSettlement, metrics.manualReviewAge, metrics.deletePending, metrics.oldestDelete, metrics.holds, metrics.holdAmounts,
		metrics.settlements, metrics.settlementAmounts, metrics.overCaptures, metrics.overCaptureAmount,
		metrics.workerRecoveries, metrics.workerItems, metrics.queueDepth,
		metrics.spoolBytes, metrics.spoolMaxBytes, metrics.spoolUtilization,
		metrics.spoolActive, metrics.spoolOrphans, metrics.spoolCleanup,
		metrics.webhooks, metrics.webhookDelay, metrics.callbacks, metrics.callbackDuration, metrics.callbackDelay,
		metrics.contentRequests, metrics.contentDuration, metrics.contentTTFB,
		metrics.contentBytes, metrics.contentStreams, metrics.contentActive, metrics.accessDisclosures,
		metrics.capabilityProbes,
	}
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	metrics.initializeZeroSeries()
	return metrics, nil
}

func (m *VideoMetrics) initializeZeroSeries() {
	m.submissions.WithLabelValues("openai", "generate", "accepted")
	m.submitDuration.WithLabelValues("openai", "generate")
	m.providerGets.WithLabelValues("openai", "worker", "success")
	m.pollDuration.WithLabelValues("openai", "success")
	m.stateTransitions.WithLabelValues("openai", "queued")
	m.taskCurrent.WithLabelValues("openai", "generate", "queued")
	m.taskStateAge.WithLabelValues("openai", "generate", "queued")
	m.holds.WithLabelValues("success")
	m.holdAmounts.WithLabelValues("success")
	m.settlements.WithLabelValues("capture", "success")
	m.settlementAmounts.WithLabelValues("capture", "success")
	m.overCaptures.WithLabelValues("success")
	m.overCaptureAmount.WithLabelValues("success")
	m.workerRecoveries.WithLabelValues("db_sweep", "success")
	m.workerItems.WithLabelValues("db_sweep")
	m.queueDepth.WithLabelValues("ready")
	m.spoolCleanup.WithLabelValues("session", "success")
	m.webhooks.WithLabelValues("openai", "accepted")
	m.webhookDelay.WithLabelValues("openai")
	m.callbacks.WithLabelValues("delivered")
	m.callbackDuration.WithLabelValues("delivered")
	m.callbackDelay.WithLabelValues("delivered")
	m.contentRequests.WithLabelValues("video", "none")
	m.contentDuration.WithLabelValues("video")
	m.contentTTFB.WithLabelValues("video")
	m.contentBytes.WithLabelValues("video")
	m.contentStreams.WithLabelValues("video", "success")
	m.accessDisclosures.WithLabelValues("token", "task_access")
	m.capabilityProbes.WithLabelValues("openai", "supported")
}

func (m *VideoMetrics) RecordSubmission(provider, operation, result string, duration time.Duration) {
	if m == nil {
		return
	}
	provider, operation = videoProviderLabel(provider), videoOperationLabel(operation)
	m.submissions.WithLabelValues(provider, operation, videoSubmitResultLabel(result)).Inc()
	m.submitDuration.WithLabelValues(provider, operation).Observe(videoNonNegativeSeconds(duration))
}

func (m *VideoMetrics) RecordProviderGet(provider, caller, result string) {
	if m == nil {
		return
	}
	m.providerGets.WithLabelValues(videoProviderLabel(provider), videoCallerLabel(caller), videoResultLabel(result)).Inc()
}

func (m *VideoMetrics) RecordPoll(provider, result string, duration time.Duration) {
	if m != nil {
		m.pollDuration.WithLabelValues(videoProviderLabel(provider), videoResultLabel(result)).Observe(videoNonNegativeSeconds(duration))
	}
}

func (m *VideoMetrics) RecordState(provider, state string) {
	if m == nil {
		return
	}
	m.stateTransitions.WithLabelValues(videoProviderLabel(provider), videoStateLabel(state)).Inc()
}

func (m *VideoMetrics) RecordHold(result string, amount float64) {
	if m == nil {
		return
	}
	result = videoResultLabel(result)
	m.holds.WithLabelValues(result).Inc()
	if amount > 0 {
		m.holdAmounts.WithLabelValues(result).Add(amount)
	}
}

func (m *VideoMetrics) RecordSettlement(action, result string, holdAmount, actualAmount float64) {
	if m == nil {
		return
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "capture" && action != "release" {
		action = "other"
	}
	result = videoResultLabel(result)
	m.settlements.WithLabelValues(action, result).Inc()
	amount := holdAmount
	if action == "capture" {
		amount = actualAmount
	}
	if amount > 0 {
		m.settlementAmounts.WithLabelValues(action, result).Add(amount)
	}
	if action == "capture" && actualAmount > holdAmount {
		m.overCaptures.WithLabelValues(result).Inc()
		m.overCaptureAmount.WithLabelValues(result).Add(actualAmount - holdAmount)
	}
}

func (m *VideoMetrics) RecordWorkerRecovery(kind, result string, items int) {
	if m == nil {
		return
	}
	kind = videoRecoveryKindLabel(kind)
	m.workerRecoveries.WithLabelValues(kind, videoResultLabel(result)).Inc()
	if items > 0 {
		m.workerItems.WithLabelValues(kind).Add(float64(items))
	}
}

func (m *VideoMetrics) SetQueueDepth(ready, delayed, active int64) {
	if m == nil {
		return
	}
	m.queueDepth.WithLabelValues("ready").Set(float64(maxInt64(ready, 0)))
	m.queueDepth.WithLabelValues("delayed").Set(float64(maxInt64(delayed, 0)))
	m.queueDepth.WithLabelValues("active").Set(float64(maxInt64(active, 0)))
}

func (m *VideoMetrics) SetSpoolHealth(active, bytes, maximum, orphans int64) {
	if m == nil {
		return
	}
	m.spoolActive.Set(float64(maxInt64(active, 0)))
	m.spoolBytes.Set(float64(maxInt64(bytes, 0)))
	m.spoolMaxBytes.Set(float64(maxInt64(maximum, 0)))
	m.spoolOrphans.Set(float64(maxInt64(orphans, 0)))
	utilization := 0.0
	if maximum > 0 && bytes > 0 {
		utilization = float64(bytes) / float64(maximum)
	}
	m.spoolUtilization.Set(utilization)
}

func (m *VideoMetrics) RecordSpoolCleanup(kind, result string) {
	if m != nil {
		m.spoolCleanup.WithLabelValues(videoSpoolCleanupKindLabel(kind), videoResultLabel(result)).Inc()
	}
}

func (m *VideoMetrics) RecordWebhook(provider, result string, delay time.Duration) {
	if m == nil {
		return
	}
	provider = videoProviderLabel(provider)
	result = videoWebhookResultLabel(result)
	m.webhooks.WithLabelValues(provider, result).Inc()
	if delay >= 0 && result != "verify_error" && result != "invalid" {
		m.webhookDelay.WithLabelValues(provider).Observe(videoNonNegativeSeconds(delay))
	}
}

func (m *VideoMetrics) RecordCallback(result string, duration, delay time.Duration) {
	if m == nil {
		return
	}
	result = videoCallbackResultLabel(result)
	m.callbacks.WithLabelValues(result).Inc()
	m.callbackDuration.WithLabelValues(result).Observe(videoNonNegativeSeconds(duration))
	m.callbackDelay.WithLabelValues(result).Observe(videoNonNegativeSeconds(delay))
}

func (m *VideoMetrics) RecordContent(variant string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	variant = videoVariantLabel(variant)
	statusLabel := "none"
	if status > 0 {
		statusLabel = strconv.Itoa(status/100) + "xx"
	}
	m.contentRequests.WithLabelValues(variant, statusLabel).Inc()
	m.contentDuration.WithLabelValues(variant).Observe(videoNonNegativeSeconds(duration))
}

func (m *VideoMetrics) RecordContentTTFB(variant string, duration time.Duration) {
	if m != nil {
		m.contentTTFB.WithLabelValues(videoVariantLabel(variant)).Observe(videoNonNegativeSeconds(duration))
	}
}

func (m *VideoMetrics) RecordContentStream(variant, result string, bytes int64) {
	if m == nil {
		return
	}
	variant = videoVariantLabel(variant)
	m.contentStreams.WithLabelValues(variant, videoContentStreamResultLabel(result)).Inc()
	if bytes > 0 {
		m.contentBytes.WithLabelValues(variant).Add(float64(bytes))
	}
}

func (m *VideoMetrics) AddContentActive(delta float64) {
	if m != nil {
		m.contentActive.Add(delta)
	}
}

func (m *VideoMetrics) RecordAccessDisclosure(kind, policy string) {
	if m == nil {
		return
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "api_key" {
		kind = "dedicated_credential"
	}
	if kind != "identity" && kind != "token" && kind != "signed_url" && kind != "dedicated_credential" {
		kind = "other"
	}
	m.accessDisclosures.WithLabelValues(kind, videoDisclosurePolicyLabel(policy)).Inc()
}

func (m *VideoMetrics) RecordCapabilityProbe(provider, status string) {
	if m == nil {
		return
	}
	provider = videoProviderLabel(provider)
	status = videoCapabilityStatusLabel(status)
	m.capabilityProbes.WithLabelValues(provider, status).Inc()
}

func (m *VideoMetrics) UpdateOperational(snapshot VideoOperationalMetrics, now time.Time) {
	if m == nil {
		return
	}
	m.taskCurrent.Reset()
	m.taskStateAge.Reset()
	for _, state := range snapshot.TaskStates {
		labels := []string{videoProviderLabel(state.Provider), videoOperationLabel(state.Operation), videoStateLabel(state.State)}
		m.taskCurrent.WithLabelValues(labels...).Set(float64(maxInt64(state.Count, 0)))
		m.taskStateAge.WithLabelValues(labels...).Set(videoAgeSeconds(now, state.OldestEnteredAt))
	}
	m.submissionUnknown.Set(float64(maxInt64(snapshot.SubmissionUnknown, 0)))
	m.unknownHoldAmount.Set(nonNegativeFloat(snapshot.UnknownHoldAmount))
	m.heldAmount.Set(nonNegativeFloat(snapshot.HeldAmount))
	m.oldestSettlement.Set(videoAgeSeconds(now, snapshot.OldestSettlementPending))
	m.manualReviewAge.Set(videoAgeSeconds(now, snapshot.OldestManualReview))
	m.deletePending.Set(float64(maxInt64(snapshot.DeletePending, 0)))
	m.oldestDelete.Set(videoAgeSeconds(now, snapshot.OldestDeletePending))
}

func videoProviderLabel(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "openai") {
		return "openai"
	}
	return "other"
}

func videoOperationLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "generate", "edit", "extend", "character_create":
		return value
	default:
		return "other"
	}
}

func videoSubmitResultLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "accepted", "replayed", "rejected", "submission_unknown", "error":
		return value
	default:
		return "error"
	}
}

func videoResultLabel(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "success") {
		return "success"
	}
	return "error"
}

func videoCallerLabel(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "admin") {
		return "admin"
	}
	return "worker"
}

func videoStateLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "preparing", "held", "submitting", "submission_unknown", "queued", "in_progress", "completed", "failed", "cancelled", "expired":
		return value
	default:
		return "other"
	}
}

func videoVariantLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "video":
		return "video"
	case "thumbnail", "spritesheet":
		return value
	default:
		return "other"
	}
}

func videoContentStreamResultLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "success", "client_abort", "idle_timeout", "total_timeout", "upstream_error", "downstream_error":
		return value
	default:
		return "upstream_error"
	}
}

func videoCallbackResultLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "delivered", "retry", "quarantined", "error":
		return value
	default:
		return "error"
	}
}

func videoWebhookResultLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "accepted", "duplicate", "unmatched", "verify_error", "invalid", "ignored_terminal":
		return value
	default:
		return "invalid"
	}
}

func videoRecoveryKindLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "delayed_promotion", "lease_expiry", "db_sweep":
		return value
	default:
		return "other"
	}
}

func videoSpoolCleanupKindLabel(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "orphan") {
		return "orphan"
	}
	return "session"
}

func videoDisclosurePolicyLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "none", "identity", "task_access", "dedicated_credentials":
		return value
	default:
		return "other"
	}
}

func videoCapabilityStatusLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "supported", "unsupported", "unknown":
		return value
	default:
		return "unknown"
	}
}

func videoAgeSeconds(now time.Time, value *time.Time) float64 {
	if value == nil {
		return 0
	}
	return videoNonNegativeSeconds(now.Sub(*value))
}

func nonNegativeFloat(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func videoNonNegativeSeconds(duration time.Duration) float64 {
	if duration < 0 {
		return 0
	}
	return duration.Seconds()
}
