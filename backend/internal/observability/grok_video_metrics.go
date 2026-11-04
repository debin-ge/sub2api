package observability

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var grokVideoOnce sync.Once
var grokVideoEvents *prometheus.CounterVec
var grokVideoObservationAge prometheus.Histogram
var grokVideoSettlementAge prometheus.Histogram

func initGrokVideoMetrics() {
	grokVideoOnce.Do(func() {
		grokVideoEvents = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "grok_video_events_total", Help: "Grok task lifecycle events; labels never contain task or user IDs."}, []string{"event"})
		grokVideoObservationAge = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "grok_video_observation_age_seconds", Help: "Task age at an upstream observation.", Buckets: []float64{5, 15, 30, 60, 120, 300, 900, 3600, 86400}})
		grokVideoSettlementAge = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "grok_video_settlement_delay_seconds", Help: "Time between completed generation and confirmed settlement.", Buckets: []float64{1, 5, 15, 30, 60, 300, 900, 3600}})
		prometheus.MustRegister(grokVideoEvents, grokVideoObservationAge, grokVideoSettlementAge)
	})
}
func GrokVideoEvent(event string) {
	switch event {
	case "created", "create_failed", "persist_failed", "owner_read_failed", "submission_unknown", "observed", "sync_paused", "settled", "settlement_failed", "settlement_blocked", "imported", "import_failed", "conflict":
	default:
		event = "other"
	}
	initGrokVideoMetrics()
	grokVideoEvents.WithLabelValues(event).Inc()
}
func GrokVideoObservationAge(created time.Time) {
	initGrokVideoMetrics()
	if !created.IsZero() {
		grokVideoObservationAge.Observe(time.Since(created).Seconds())
	}
}
func GrokVideoSettlementDelay(finished *time.Time) {
	initGrokVideoMetrics()
	if finished != nil {
		grokVideoSettlementAge.Observe(time.Since(*finished).Seconds())
	}
}
