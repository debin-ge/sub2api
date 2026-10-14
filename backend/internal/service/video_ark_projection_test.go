//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProjectArkVideoStatus(t *testing.T) {
	cases := []struct{ name, generation, billing, want string }{
		{"preparing", VideoGenerationPreparing, VideoBillingNone, ArkVideoStatusQueued},
		{"held", VideoGenerationHeld, VideoBillingHeld, ArkVideoStatusQueued},
		{"submitting", VideoGenerationSubmitting, VideoBillingHeld, ArkVideoStatusQueued},
		{"queued", VideoGenerationQueued, VideoBillingHeld, ArkVideoStatusQueued},
		{"in progress", VideoGenerationInProgress, VideoBillingHeld, ArkVideoStatusRunning},
		// 生成完成但计费未落账：对用户仍是 running，不能提前暴露成功态。
		{"completed before capture", VideoGenerationCompleted, VideoBillingHeld, ArkVideoStatusRunning},
		{"completed capture pending", VideoGenerationCompleted, VideoBillingCapturePending, ArkVideoStatusRunning},
		{"completed and captured", VideoGenerationCompleted, VideoBillingCaptured, ArkVideoStatusSucceeded},
		// 取消与过期不再折叠成 failed。
		{"failed", VideoGenerationFailed, VideoBillingReleased, ArkVideoStatusFailed},
		{"cancelled", VideoGenerationCancelled, VideoBillingReleased, ArkVideoStatusCancelled},
		{"expired", VideoGenerationExpired, VideoBillingReleased, ArkVideoStatusExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ProjectArkVideoStatus(&VideoTask{GenerationState: tc.generation, BillingState: tc.billing}))
		})
	}
	require.Equal(t, ArkVideoStatusFailed, ProjectArkVideoStatus(nil))

	for _, status := range []string{ArkVideoStatusQueued, ArkVideoStatusRunning, ArkVideoStatusSucceeded, ArkVideoStatusFailed, ArkVideoStatusCancelled, ArkVideoStatusExpired} {
		require.True(t, IsArkVideoStatus(status), status)
	}
	require.False(t, IsArkVideoStatus(VideoGenerationCompleted), "internal state names are not Ark statuses")
}

func TestIsArkVideoProvider(t *testing.T) {
	for _, provider := range []string{VideoProviderByteDance, VideoProviderFal, VideoProviderWaveSpeed, VideoProviderRunware, VideoProviderFlova} {
		require.True(t, IsArkVideoProvider(provider), provider)
	}
	require.False(t, IsArkVideoProvider(VideoProviderOpenAI))
	require.False(t, IsArkVideoProvider(""))
	require.True(t, IsArkVideoPlatform(PlatformByteDance))
	require.True(t, IsArkVideoPlatform(PlatformFlova))
	require.False(t, IsArkVideoPlatform(PlatformOpenAI))
	require.False(t, IsArkVideoPlatform(PlatformAnthropic))
}

// arkProjectionTaskForTest 是一条刚提交、还没有上游观测值的 Ark 任务；request_attributes
// 里的指针值就是 videoRequestAttributes 在内存里写下的形态（落库再读回会变成标量）。
func arkProjectionTaskForTest() *VideoTask {
	cameraFixed, watermark, returnLastFrame := true, false, true
	seed := int64(42)
	return &VideoTask{
		PublicID: "video_0123456789abcdef0123456789abcdef", Provider: VideoProviderByteDance,
		RequestedModel: "seedance-alias", PublicModel: ByteDanceVideoModelSeedance10Pro,
		GenerationState: VideoGenerationQueued, BillingState: VideoBillingHeld,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(), UpdatedAt: time.Unix(1_700_000_050, 0).UTC(),
		RequestAttributes: map[string]any{
			"resolution": "720p", "ratio": ArkVideoRatioAdaptive, "duration": 10, "seconds": 10, "smart_duration": false,
			"frames": 0, "seed": &seed, "camera_fixed": &cameraFixed, "watermark": &watermark, "return_last_frame": &returnLastFrame,
			"generate_audio": true, "service_tier": "flex", "execution_expires_after": 7200,
		},
	}
}

func TestBuildArkVideoTaskObjectProjectsRequestAndObservedValues(t *testing.T) {
	task := arkProjectionTaskForTest()
	object := BuildArkVideoTaskObject(task, nil)
	require.Equal(t, task.PublicID, object["id"])
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, object["model"], "the public model wins over the requested alias")
	require.Equal(t, ArkVideoStatusQueued, object["status"])
	require.Equal(t, int64(1_700_000_000), object["created_at"])
	require.Equal(t, int64(1_700_000_050), object["updated_at"])
	require.Equal(t, "720p", object["resolution"])
	require.Equal(t, ArkVideoRatioAdaptive, object["ratio"])
	require.Equal(t, 10, object["duration"])
	require.NotContains(t, object, "frames")
	require.NotContains(t, object, "framespersecond")
	require.Equal(t, int64(42), object["seed"])
	require.Equal(t, true, object["generate_audio"])
	require.Equal(t, true, object["camera_fixed"])
	require.Equal(t, false, object["watermark"])
	require.Equal(t, true, object["return_last_frame"])
	require.Equal(t, "flex", object["service_tier"])
	require.Equal(t, 7200, object["execution_expires_after"])
	for _, absent := range []string{"usage", "content", "error", "revised_prompt", "object", "provider", "actual_cost", "provider_task_id"} {
		require.NotContains(t, object, absent)
	}

	// 落库读回后指针都成了标量，投影结果必须一致。
	task.RequestAttributes["seed"] = float64(42)
	task.RequestAttributes["camera_fixed"] = true
	task.RequestAttributes["duration"] = float64(10)
	stored := BuildArkVideoTaskObject(task, nil)
	require.Equal(t, object, stored)

	// 上游观测值覆盖请求快照：adaptive 的真实比例、智能时长的真实秒数、种子的实际值。
	task.ResponseMetadata = map[string]any{
		"resolution": "1080p", "ratio": "9:16", "seconds": float64(12), "frames": float64(289), "framespersecond": float64(24),
		"seed": float64(99), "generate_audio": false, "service_tier": "default", "revised_prompt": "A tracking shot, cinematic",
	}
	object = BuildArkVideoTaskObject(task, nil)
	require.Equal(t, "1080p", object["resolution"])
	require.Equal(t, "9:16", object["ratio"])
	require.Equal(t, 12, object["duration"])
	require.Equal(t, 289, object["frames"])
	require.Equal(t, 24, object["framespersecond"])
	require.Equal(t, int64(99), object["seed"])
	require.Equal(t, false, object["generate_audio"])
	require.Equal(t, "default", object["service_tier"])
	require.Equal(t, "A tracking shot, cinematic", object["revised_prompt"])
	require.Equal(t, true, object["camera_fixed"], "switches the upstream never echoes keep the request value")

	// 智能时长：请求快照 duration=-1 原样透出，直到上游回报真实秒数。
	smart := arkProjectionTaskForTest()
	smart.RequestAttributes["duration"], smart.RequestAttributes["seconds"], smart.RequestAttributes["smart_duration"] = -1, 0, true
	require.Equal(t, -1, BuildArkVideoTaskObject(smart, nil)["duration"])
	smart.ResponseMetadata = map[string]any{"seconds": float64(7)}
	require.Equal(t, 7, BuildArkVideoTaskObject(smart, nil)["duration"])

	// frames 请求：frames 来自快照。
	frames := arkProjectionTaskForTest()
	frames.RequestAttributes["frames"] = 121
	require.Equal(t, 121, BuildArkVideoTaskObject(frames, nil)["frames"])

	// 旧任务只有 seconds 没有 duration 时也能投影。
	legacy := arkProjectionTaskForTest()
	delete(legacy.RequestAttributes, "duration")
	require.Equal(t, 10, BuildArkVideoTaskObject(legacy, nil)["duration"])

	require.Nil(t, BuildArkVideoTaskObject(nil, nil))
}

func TestBuildArkVideoTaskObjectStatusDependentSections(t *testing.T) {
	urls := ArkVideoContentURLs{"video": "https://gateway.example/v/video.mp4", "last_frame": "https://gateway.example/v/last.png"}

	// 完成但未落账：running，无 content；usage 只在上游回报过 token 时输出。
	task := arkProjectionTaskForTest()
	task.GenerationState, task.BillingState = VideoGenerationCompleted, VideoBillingCapturePending
	task.UsageSnapshot = map[string]any{"seconds": float64(10)}
	object := BuildArkVideoTaskObject(task, urls)
	require.Equal(t, ArkVideoStatusRunning, object["status"])
	require.NotContains(t, object, "content")
	require.NotContains(t, object, "usage")
	require.NotContains(t, object, "error")

	// 落账后 succeeded：content 带链接，usage 按 Ark 字段名还原。
	task.BillingState = VideoBillingCaptured
	task.UsageSnapshot = map[string]any{"video_tokens": float64(1234), "total_tokens": float64(1300)}
	object = BuildArkVideoTaskObject(task, urls)
	require.Equal(t, ArkVideoStatusSucceeded, object["status"])
	require.Equal(t, map[string]any{"video_url": urls["video"], "last_frame_url": urls["last_frame"]}, object["content"])
	require.Equal(t, map[string]any{"completion_tokens": int64(1234), "total_tokens": int64(1300)}, object["usage"])
	require.NotContains(t, object, "error")

	// total 小于 completion 时以 completion 为准；没有尾帧链接就只有 video_url；没有任何链接不输出 content。
	task.UsageSnapshot = map[string]any{"video_tokens": float64(1234), "total_tokens": float64(10)}
	object = BuildArkVideoTaskObject(task, ArkVideoContentURLs{"video": urls["video"]})
	require.Equal(t, map[string]any{"completion_tokens": int64(1234), "total_tokens": int64(1234)}, object["usage"])
	require.Equal(t, map[string]any{"video_url": urls["video"]}, object["content"])
	require.NotContains(t, BuildArkVideoTaskObject(task, nil), "content")
	task.UsageSnapshot = map[string]any{"video_tokens": float64(0)}
	require.NotContains(t, BuildArkVideoTaskObject(task, nil), "usage", "a zero token count is not a usage report")

	// 终态错误对象：有记录用记录，没有按状态给默认码；终态不带 content。
	code, message := "ModelNotOpen", "model is not enabled"
	for _, tc := range []struct {
		generation, status, defaultCode, defaultMessage string
	}{
		{VideoGenerationFailed, ArkVideoStatusFailed, "video_generation_failed", "video generation failed"},
		{VideoGenerationCancelled, ArkVideoStatusCancelled, "video_generation_cancelled", "video generation was cancelled"},
		{VideoGenerationExpired, ArkVideoStatusExpired, "video_generation_expired", "video generation expired before completion"},
	} {
		terminal := arkProjectionTaskForTest()
		terminal.GenerationState, terminal.BillingState = tc.generation, VideoBillingReleased
		object := BuildArkVideoTaskObject(terminal, urls)
		require.Equal(t, tc.status, object["status"])
		require.Equal(t, map[string]any{"code": tc.defaultCode, "message": tc.defaultMessage}, object["error"], tc.status)
		require.NotContains(t, object, "content", tc.status)

		terminal.LastErrorCode, terminal.LastErrorMessage = &code, &message
		require.Equal(t, map[string]any{"code": code, "message": message}, BuildArkVideoTaskObject(terminal, nil)["error"], tc.status)
	}
}
