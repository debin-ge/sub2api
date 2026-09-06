package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoQuotaTimeFreezesTerminalObservationAndCalendar(t *testing.T) {
	for _, test := range []struct {
		name, zone, occurred, day, week string
	}{
		{"UTC", "UTC", "2026-09-05T12:00:00Z", "2026-09-05T00:00:00Z", "2026-08-31T00:00:00Z"},
		{"Shanghai", "Asia/Shanghai", "2026-09-06T17:00:00Z", "2026-09-06T16:00:00Z", "2026-09-06T16:00:00Z"},
		{"DST_spring", "America/Los_Angeles", "2026-03-08T18:00:00Z", "2026-03-08T08:00:00Z", "2026-03-02T08:00:00Z"},
		{"DST_fall", "America/Los_Angeles", "2026-11-02T07:30:00Z", "2026-11-01T07:00:00Z", "2026-10-26T07:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			finished, err := time.Parse(time.RFC3339, test.occurred)
			require.NoError(t, err)
			task := baseVideoWorkerTask()
			task.CreatedAt, task.FinishedAt = finished.Add(-48*time.Hour), &finished
			task.PriceSnapshot["quota_time_contract_version"] = 1
			task.PriceSnapshot["quota_time_zone"] = test.zone
			untrustedTime := finished.Add(365 * 24 * time.Hour)
			task.ProviderFinishedAt = &untrustedTime
			command, usage, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
			require.NoError(t, err)
			require.NoError(t, command.ValidateQuotaTime())
			require.Equal(t, finished, command.OccurredAt)
			require.Equal(t, finished, usage.CreatedAt)
			require.Equal(t, test.day, command.QuotaTime.DayStart.Format(time.RFC3339))
			require.Equal(t, test.week, command.QuotaTime.WeekStart.Format(time.RFC3339))
			command.Normalize()
			encoded, err := json.Marshal(task)
			require.NoError(t, err)
			var restored VideoTask
			require.NoError(t, json.Unmarshal(encoded, &restored))
			replayed, replayedUsage, err := buildVideoUsageSettlement(&restored, command.RequestID, 8)
			require.NoError(t, err)
			replayed.Normalize()
			require.Equal(t, command.RequestFingerprint, replayed.RequestFingerprint)
			require.Equal(t, command.QuotaTime, replayed.QuotaTime)
			require.Equal(t, usage.CreatedAt, replayedUsage.CreatedAt)
		})
	}
}

func TestVideoQuotaTimeRejectsIncompleteNewSnapshots(t *testing.T) {
	for _, failure := range []string{"missing_finish", "zero_finish", "finish_before_create", "missing_zone", "invalid_zone", "local_zone", "unknown_version", "null_version"} {
		t.Run(failure, func(t *testing.T) {
			task := baseVideoWorkerTask()
			finished := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
			task.CreatedAt, task.FinishedAt = finished.Add(-time.Hour), &finished
			task.PriceSnapshot["quota_time_contract_version"], task.PriceSnapshot["quota_time_zone"] = 1, "UTC"
			switch failure {
			case "missing_finish":
				task.FinishedAt = nil
			case "zero_finish":
				task.FinishedAt = &time.Time{}
			case "finish_before_create":
				task.CreatedAt = finished.Add(time.Hour)
			case "missing_zone":
				delete(task.PriceSnapshot, "quota_time_zone")
			case "invalid_zone":
				task.PriceSnapshot["quota_time_zone"] = "Not/A_Timezone"
			case "local_zone":
				task.PriceSnapshot["quota_time_zone"] = "Local"
			case "unknown_version":
				task.PriceSnapshot["quota_time_contract_version"] = 2
			case "null_version":
				task.PriceSnapshot["quota_time_contract_version"] = nil
			}
			_, _, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
			require.ErrorIs(t, err, ErrUsageBillingPayloadInvalid)
		})
	}
}

func TestVideoQuotaTimeFingerprintVersionsDoNotReinterpretLegacy(t *testing.T) {
	task := baseVideoWorkerTask()
	task.CreatedAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	legacy, _, err := buildVideoUsageSettlement(task, VideoTaskCaptureRequestID(task.PublicID), 8)
	require.NoError(t, err)
	require.Nil(t, legacy.QuotaTime)
	legacy.Normalize()
	changedLegacy := *legacy
	changedLegacy.OccurredAt = legacy.OccurredAt.Add(time.Hour)
	changedLegacy.RequestFingerprint = ""
	changedLegacy.Normalize()
	require.Equal(t, legacy.RequestFingerprint, changedLegacy.RequestFingerprint)

	finished := task.CreatedAt.Add(time.Hour)
	task.FinishedAt = &finished
	task.PriceSnapshot["quota_time_contract_version"], task.PriceSnapshot["quota_time_zone"] = 1, "UTC"
	current, _, err := buildVideoUsageSettlement(task, legacy.RequestID, 8)
	require.NoError(t, err)
	current.Normalize()
	require.NotEqual(t, legacy.RequestFingerprint, current.RequestFingerprint)
	changed := *current
	changed.OccurredAt = current.OccurredAt.Add(time.Minute)
	changed.RequestFingerprint = ""
	changed.Normalize()
	require.NotEqual(t, current.RequestFingerprint, changed.RequestFingerprint)
}
