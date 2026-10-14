package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeNeutralArkParametersClearsOnlyNeutralValues(t *testing.T) {
	on, off := true, false
	random, fixed := int64(-1), int64(7)
	base := VideoCreateRequest{
		Seed: &random, CameraFixed: &off, Watermark: &off, AudioEnabled: &off, ReturnLastFrame: &off,
		ServiceTier: "default", ExecutionExpiresAfter: 3600,
	}

	// 全部不支持：中性值一律清空并记录，排队超时保留给 worker。
	normalized, ignored := normalizeNeutralArkParameters(base, VideoArkParameterSupport{})
	require.Equal(t, []string{"camera_fixed", "generate_audio", "return_last_frame", "seed", "service_tier", "watermark"}, ignored)
	require.Nil(t, normalized.Seed)
	require.Nil(t, normalized.CameraFixed)
	require.Nil(t, normalized.Watermark)
	require.Nil(t, normalized.AudioEnabled)
	require.Nil(t, normalized.ReturnLastFrame)
	require.Empty(t, normalized.ServiceTier)
	require.Equal(t, 3600, normalized.ExecutionExpiresAfter)

	// 支持的字段原样保留，不进 ignored。
	normalized, ignored = normalizeNeutralArkParameters(base, ArkNativeParameterSupport())
	require.Nil(t, ignored)
	require.Equal(t, base, normalized)

	// 非中性值不是空操作：留给 provider 拒绝。return_last_frame 例外：它只决定响应里有没有
	// 尾帧链接，上游给不出时 true 也按空操作接受并记录。
	loud := base
	loud.Seed, loud.CameraFixed, loud.Watermark, loud.AudioEnabled, loud.ReturnLastFrame, loud.ServiceTier = &fixed, &on, &on, &on, &on, "flex"
	normalized, ignored = normalizeNeutralArkParameters(loud, VideoArkParameterSupport{})
	require.Equal(t, []string{"return_last_frame"}, ignored)
	require.Nil(t, normalized.ReturnLastFrame)
	expected := loud
	expected.ReturnLastFrame = nil
	require.Equal(t, expected, normalized)

	// 部分支持：只清不支持的那几个。
	partial := VideoArkParameterSupport{Seed: true, GenerateAudio: true}
	normalized, ignored = normalizeNeutralArkParameters(base, partial)
	require.Equal(t, []string{"camera_fixed", "return_last_frame", "service_tier", "watermark"}, ignored)
	require.Equal(t, &random, normalized.Seed)
	require.Equal(t, &off, normalized.AudioEnabled)
}

func TestVideoVendorArkParameterSupportFollowsProfile(t *testing.T) {
	wavespeed := newVideoVendorProvider(newWaveSpeedVideoAdapter(), nil, nil)
	// Seedance 2.5 只有 generate_audio；seed / camera_fixed / watermark 都不是厂商字段。
	support := wavespeed.ArkParameterSupport(nil, VideoCreateRequest{Model: "bytedance/seedance-2.5/text-to-video"})
	require.Equal(t, VideoArkParameterSupport{GenerateAudio: true}, support)
	// Seedance 1.5 Pro 档案映射了 seed 与 camera_fixed。
	support = wavespeed.ArkParameterSupport(nil, VideoCreateRequest{Model: "bytedance/seedance-v1.5-pro/text-to-video"})
	require.True(t, support.Seed)
	require.True(t, support.CameraFixed)
	require.False(t, support.Watermark)
	require.False(t, support.ExecutionExpiresAfter, "排队超时由本网关执行")

	flova := &FlovaVideoProvider{}
	require.Equal(t, VideoArkParameterSupport{}, flova.ArkParameterSupport(nil, VideoCreateRequest{}))

	bytedance := &ByteDanceVideoProvider{}
	require.Equal(t, ArkNativeParameterSupport(), bytedance.ArkParameterSupport(&Account{Credentials: map[string]any{}}, VideoCreateRequest{}))
	// 中继模式转发四个开关；尾帧、服务档与排队超时没有落点。
	require.Equal(t, VideoArkParameterSupport{Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true},
		bytedance.ArkParameterSupport(&Account{Credentials: map[string]any{byteDanceProtocolCredentialKey: byteDanceProtocolOpenAICompat}}, VideoCreateRequest{}))
}

func TestVideoExecutionExpiryDeadlineAndObservation(t *testing.T) {
	created := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	task := &VideoTask{
		GenerationState: VideoGenerationQueued, CreatedAt: created,
		RequestAttributes: map[string]any{"execution_expires_after": float64(3600), "execution_expiry_enforced_locally": true},
	}
	deadline, ok := VideoExecutionExpiryDeadline(task)
	require.True(t, ok)
	require.Equal(t, created.Add(time.Hour), deadline)

	queued := &ProviderVideoTask{Status: VideoGenerationQueued}
	require.False(t, videoExecutionExpired(task, queued, created.Add(59*time.Minute)), "时限未到")
	require.True(t, videoExecutionExpired(task, queued, created.Add(time.Hour)), "到点仍在排队即过期")
	require.False(t, videoExecutionExpired(task, &ProviderVideoTask{Status: VideoGenerationInProgress}, created.Add(2*time.Hour)), "已开始执行的任务不按排队超时处理")
	running := *task
	running.GenerationState = VideoGenerationInProgress
	require.False(t, videoExecutionExpired(&running, queued, created.Add(2*time.Hour)))

	// 由上游自行执行（Ark 直连）或没有配置时不介入。
	upstream := *task
	upstream.RequestAttributes = map[string]any{"execution_expires_after": float64(3600), "execution_expiry_enforced_locally": false}
	_, ok = VideoExecutionExpiryDeadline(&upstream)
	require.False(t, ok)
	none := *task
	none.RequestAttributes = map[string]any{}
	_, ok = VideoExecutionExpiryDeadline(&none)
	require.False(t, ok)

	observed := videoExecutionExpiredObservation(task)
	require.Equal(t, VideoGenerationExpired, observed.Status)
	require.Equal(t, "execution_expired", observed.ErrorCode)
}

func TestVideoRequestAttributesRecordIgnoredParametersAndLocalExpiry(t *testing.T) {
	resolved := &resolvedVideoSubmission{
		quote:                &VideoPriceQuote{Attributes: VideoPricingAttributes{Resolution: "720p"}},
		account:              &Account{},
		provider:             &videoProviderStub{name: VideoProviderWaveSpeed},
		executionSpec:        ResolvedVideoExecutionSpec{Version: 2},
		ignoredParameters:    []string{"service_tier", "watermark"},
		executionExpiryLocal: true,
	}
	attributes := videoRequestAttributes(VideoSubmitRequest{Operation: VideoOperationGenerate, Seconds: 5, ExecutionExpiresAfter: 7200}, resolved)
	require.Equal(t, []string{"service_tier", "watermark"}, attributes["ignored_parameters"])
	require.Equal(t, true, attributes["execution_expiry_enforced_locally"])
	require.Equal(t, 7200, attributes["execution_expires_after"])

	object := BuildArkVideoTaskObject(&VideoTask{PublicID: "video_x", RequestAttributes: attributes, GenerationState: VideoGenerationQueued}, nil)
	require.Equal(t, []string{"service_tier", "watermark"}, object["ignored_parameters"])
	require.Equal(t, 7200, object["execution_expires_after"])

	plain := videoRequestAttributes(VideoSubmitRequest{Operation: VideoOperationGenerate, Seconds: 5}, &resolvedVideoSubmission{
		quote: resolved.quote, account: resolved.account, provider: resolved.provider, executionSpec: resolved.executionSpec,
	})
	require.NotContains(t, plain, "ignored_parameters")
	require.NotContains(t, BuildArkVideoTaskObject(&VideoTask{PublicID: "video_y", RequestAttributes: plain}, nil), "ignored_parameters")
}
