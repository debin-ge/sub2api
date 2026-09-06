package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestVideoMetricsUseOnlyBoundedLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewVideoMetrics(registry)
	require.NoError(t, err)

	metrics.RecordSubmission("provider-account-123", "raw-operation-secret", "unexpected-secret", time.Second)
	metrics.RecordProviderGet("openai", "admin-user-44", "boom-secret")
	metrics.RecordPoll("provider-account-123", "boom-secret", time.Second)
	metrics.RecordState("openai", "video_credential_value")
	metrics.RecordHold("boom-secret", 2)
	metrics.RecordSettlement("capture", "boom-secret", 1, 2)
	metrics.RecordWorkerRecovery("secret-recovery", "boom-secret", 3)
	metrics.RecordWebhook("provider-account-123", "secret-webhook", time.Second)
	metrics.RecordCallback("secret-callback", time.Second, time.Second)
	metrics.RecordContent("https://signed.example/secret", 503, time.Second)
	metrics.RecordContentTTFB("https://signed.example/secret", time.Second)
	metrics.RecordContentStream("https://signed.example/secret", "secret-outcome", 42)
	metrics.AddContentActive(1)
	metrics.AddContentActive(-1)
	metrics.RecordAccessDisclosure("sk-secret", "secret-policy")
	metrics.RecordAccessDisclosure("api_key", "dedicated_credentials")
	metrics.RecordCapabilityProbe("provider-account-123", "secret-status")
	now := time.Now()
	deleteAt := now.Add(-5 * time.Minute)
	metrics.UpdateOperational(VideoOperationalMetrics{TaskStates: []VideoTaskStateMetric{{
		Provider: "provider-account-123", Operation: "raw-operation-secret", State: "video_credential_value", Count: 1,
	}}, DeletePending: 2, OldestDeletePending: &deleteAt}, now)

	recorder := httptest.NewRecorder()
	MetricsHandler(registry).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	for _, secret := range []string{"provider-account-123", "raw-operation-secret", "unexpected-secret", "admin-user-44", "boom-secret", "video_credential_value", "signed.example", "sk-secret"} {
		require.False(t, strings.Contains(body, secret), body)
	}
	require.Contains(t, body, `video_platform_submissions_total{operation="other",provider="other",result="error"} 1`)
	require.Contains(t, body, `video_platform_delete_pending_current 2`)
	require.Contains(t, body, `video_platform_oldest_delete_pending_age_seconds 300`)
	require.Contains(t, body, `video_platform_content_requests_total{status="5xx",variant="other"} 1`)
	require.Contains(t, body, `video_platform_content_bytes_total{variant="other"} 42`)
	require.Contains(t, body, `video_platform_content_streams_total{result="upstream_error",variant="other"} 1`)
	require.Contains(t, body, `video_platform_settlement_amount_total{action="capture",result="error"} 2`)
	require.Contains(t, body, `video_platform_over_capture_amount_total{result="error"} 1`)
	require.Contains(t, body, `video_platform_access_disclosures_total{kind="other",policy="other"} 1`)
	require.Contains(t, body, `video_platform_access_disclosures_total{kind="dedicated_credential",policy="dedicated_credentials"} 1`)
}
