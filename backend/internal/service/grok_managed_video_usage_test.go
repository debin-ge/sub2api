package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokManagedNestedUsageDoesNotChangeBillingUnits(t *testing.T) {
	task, err := decodeGrokManagedTask([]byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/video.mp4","duration":8,"respect_moderation":true},"usage":{"cost_in_usd_ticks":6900000000,"input_tokens":20,"output_tokens":50,"input_tokens_details":{"image_tokens":10,"cached_tokens":5},"output_tokens_details":{"reasoning_tokens":40},"unknown":{"secret":"discard"}}}`), "task")
	require.NoError(t, err)
	require.Equal(t, VideoGenerationCompleted, task.Status)
	stored := sanitizeVideoProviderUsage(task.Usage)
	require.NotContains(t, stored, "video_tokens")
	require.NotContains(t, stored, "output_tokens")
	require.NotContains(t, stored, "seconds")
	require.Equal(t, float64(6900000000), stored["provider_cost_usd_ticks"])
	wire := GrokManagedPublicUsage(stored)
	require.Equal(t, map[string]any{
		"cost_in_usd_ticks": float64(6900000000), "input_tokens": float64(20), "output_tokens": float64(50),
		"input_tokens_details":  map[string]any{"image_tokens": float64(10), "cached_tokens": float64(5)},
		"output_tokens_details": map[string]any{"reasoning_tokens": float64(40)},
	}, wire)
	require.Equal(t, true, sanitizeVideoProviderMetadata(task.Metadata)["respect_moderation"])
}

func TestGrokManagedMalformedUsageDoesNotBlockCompletedVideo(t *testing.T) {
	for _, usage := range []string{`null`, `{}`, `[]`, `"unexpected"`, `{"cost_in_usd_ticks":-1,"input_tokens":{},"output_tokens":"12","total_tokens":1e999}`} {
		task, err := decodeGrokManagedTask([]byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/video.mp4"},"usage":`+usage+`}`), "task")
		require.NoError(t, err)
		require.Equal(t, VideoGenerationCompleted, task.Status)
		require.Empty(t, task.Usage)
		require.NotContains(t, task.Metadata, "respect_moderation")
	}
}

func TestGrokManagedModerationRejectionWithoutURLReleasesTask(t *testing.T) {
	task, err := decodeGrokManagedTask([]byte(`{"status":"done","video":{"respect_moderation":false}}`), "task")
	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, task.Status)
	require.Equal(t, "content_rejected", task.ErrorCode)
	require.Empty(t, task.VideoURL)
}
