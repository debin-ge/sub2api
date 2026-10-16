package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeUnsupportedArkParametersIgnoresUnsupportedSwitches(t *testing.T) {
	on, off := true, false
	random, fixed := int64(-1), int64(7)
	base := VideoCreateRequest{
		Seed: &random, CameraFixed: &off, Watermark: &off, AudioEnabled: &off, ReturnLastFrame: &off,
		ServiceTier: "default", ExecutionExpiresAfter: 3600,
	}

	// 全部不支持：一律清空并记录，排队超时保留给 worker。
	normalized, ignored := normalizeUnsupportedArkParameters(base, VideoArkParameterSupport{})
	require.Equal(t, []string{"camera_fixed", "generate_audio", "return_last_frame", "seed", "service_tier", "watermark"}, ignored)
	require.Nil(t, normalized.Seed)
	require.Nil(t, normalized.CameraFixed)
	require.Nil(t, normalized.Watermark)
	require.Nil(t, normalized.AudioEnabled)
	require.Nil(t, normalized.ReturnLastFrame)
	require.Empty(t, normalized.ServiceTier)
	require.Equal(t, 3600, normalized.ExecutionExpiresAfter)

	// 支持的字段原样保留，不进 ignored。
	normalized, ignored = normalizeUnsupportedArkParameters(base, ArkNativeParameterSupport())
	require.Nil(t, ignored)
	require.Equal(t, base, normalized)

	// 非中性值同样被忽略（不改变本网关成本）；只有改变计费档位的 service_tier=flex 留给 provider 拒绝。
	loud := base
	loud.Seed, loud.CameraFixed, loud.Watermark, loud.AudioEnabled, loud.ReturnLastFrame, loud.ServiceTier = &fixed, &on, &on, &on, &on, "flex"
	normalized, ignored = normalizeUnsupportedArkParameters(loud, VideoArkParameterSupport{})
	require.Equal(t, []string{"camera_fixed", "generate_audio", "return_last_frame", "seed", "watermark"}, ignored)
	require.Nil(t, normalized.Seed)
	require.Nil(t, normalized.CameraFixed)
	require.Nil(t, normalized.Watermark)
	require.Nil(t, normalized.AudioEnabled)
	require.Nil(t, normalized.ReturnLastFrame)
	require.Equal(t, "flex", normalized.ServiceTier)

	// 部分支持：只清不支持的那几个。
	partial := VideoArkParameterSupport{Seed: true, GenerateAudio: true}
	normalized, ignored = normalizeUnsupportedArkParameters(loud, partial)
	require.Equal(t, []string{"camera_fixed", "return_last_frame", "watermark"}, ignored)
	require.Equal(t, &fixed, normalized.Seed)
	require.Equal(t, &on, normalized.AudioEnabled)
}

func TestAdjustArkDurationForProvider(t *testing.T) {
	caps := VideoCapabilities{
		DefaultSeconds:   map[string]int{"flova/veo-3.1-fast": 4, "flova/seedance-2.5": 5},
		SupportedSeconds: map[string][]int{"flova/veo-3.1-fast": {4, 6, 8}},
	}
	smart := VideoCreateRequest{Model: "flova/veo-3.1-fast", SmartDuration: true}

	// provider 自己支持智能时长 / 帧数：原样保留。
	adjusted, fields := adjustArkDurationForProvider(smart, ArkNativeParameterSupport(), caps)
	require.Nil(t, fields)
	require.Equal(t, smart, adjusted)

	// duration=-1 → 模型默认时长。
	adjusted, fields = adjustArkDurationForProvider(smart, VideoArkParameterSupport{}, caps)
	require.Equal(t, []string{"duration"}, fields)
	require.False(t, adjusted.SmartDuration)
	require.Equal(t, 4, adjusted.Seconds)

	// frames → 24fps 向上取整后取最近档位；等距取较长者。121 帧 = 5.04s → 6s；97 帧 = 4.04s → 5s → 等距取 6。
	for frames, want := range map[int]int{29: 4, 121: 6, 97: 6, 193: 8, 289: 8} {
		adjusted, fields = adjustArkDurationForProvider(VideoCreateRequest{Model: "flova/veo-3.1-fast", Frames: frames}, VideoArkParameterSupport{}, caps)
		require.Equal(t, []string{"frames"}, fields, frames)
		require.Zero(t, adjusted.Frames, frames)
		require.Equal(t, want, adjusted.Seconds, frames)
	}
	// 连续区间（没有离散档）的模型直接用向上取整的秒数。
	adjusted, _ = adjustArkDurationForProvider(VideoCreateRequest{Model: "flova/seedance-2.5", Frames: 121}, VideoArkParameterSupport{}, caps)
	require.Equal(t, 6, adjusted.Seconds)

	// 能力表没有默认时长的模型：duration=-1 原样交给 provider 校验。
	unknown := VideoCreateRequest{Model: "unknown-model", SmartDuration: true}
	adjusted, fields = adjustArkDurationForProvider(unknown, VideoArkParameterSupport{}, caps)
	require.Nil(t, fields)
	require.True(t, adjusted.SmartDuration)
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
	// Flova 只有 camera_fixed 能以指令形式落地。
	require.Equal(t, VideoArkParameterSupport{CameraFixed: true}, flova.ArkParameterSupport(nil, VideoCreateRequest{}))

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
		adjustedParameters:   []string{"duration"},
		executionExpiryLocal: true,
	}
	attributes := videoRequestAttributes(VideoSubmitRequest{Operation: VideoOperationGenerate, Seconds: 5, ExecutionExpiresAfter: 7200}, resolved)
	require.Equal(t, []string{"service_tier", "watermark"}, attributes["ignored_parameters"])
	require.Equal(t, true, attributes["execution_expiry_enforced_locally"])
	require.Equal(t, 7200, attributes["execution_expires_after"])

	object := BuildArkVideoTaskObject(&VideoTask{PublicID: "video_x", RequestAttributes: attributes, GenerationState: VideoGenerationQueued}, nil)
	require.Equal(t, []string{"service_tier", "watermark"}, object["ignored_parameters"])
	require.Equal(t, []string{"duration"}, object["adjusted_parameters"])
	require.Equal(t, 7200, object["execution_expires_after"])

	plain := videoRequestAttributes(VideoSubmitRequest{Operation: VideoOperationGenerate, Seconds: 5}, &resolvedVideoSubmission{
		quote: resolved.quote, account: resolved.account, provider: resolved.provider, executionSpec: resolved.executionSpec,
	})
	require.NotContains(t, plain, "ignored_parameters")
	require.NotContains(t, plain, "adjusted_parameters")
	require.NotContains(t, BuildArkVideoTaskObject(&VideoTask{PublicID: "video_y", RequestAttributes: plain}, nil), "ignored_parameters")
}

// arkNormalizingVideoProvider 给测试桩加上 ArkParameterSupport，模拟非 Ark 直连 provider。
type arkNormalizingVideoProvider struct {
	*videoProviderStub
	support VideoArkParameterSupport
}

func (p *arkNormalizingVideoProvider) ArkParameterSupport(*Account, VideoCreateRequest) VideoArkParameterSupport {
	return p.support
}

// 端到端：不支持的开关被忽略并记录、帧数换算成支持的秒数、被忽略的 generate_audio=true 按无声计价。
func TestVideoTaskServiceSubmitIgnoresUnsupportedArkSwitchesAndAdjustsFrames(t *testing.T) {
	stub := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued"}}
	svc, tasks, _ := newVideoTaskServiceForTest(stub, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.providers = NewVideoProviderRegistry(&arkNormalizingVideoProvider{videoProviderStub: stub})

	on, seed := true, int64(42)
	request := videoSubmitRequestForTest()
	request.Seconds, request.Frames = 0, 97
	request.Seed, request.CameraFixed, request.Watermark, request.AudioEnabled = &seed, &on, &on, &on
	_, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)

	require.Nil(t, stub.request.Seed)
	require.Nil(t, stub.request.CameraFixed)
	require.Nil(t, stub.request.Watermark)
	require.Nil(t, stub.request.AudioEnabled)
	require.Zero(t, stub.request.Frames)
	require.Equal(t, 4, stub.request.Seconds, "97 帧向上取整为 5 秒，取 sora-2 支持的最近档位 4 秒")

	attrs := tasks.create.RequestAttributes
	require.Equal(t, []string{"camera_fixed", "generate_audio", "seed", "watermark"}, attrs["ignored_parameters"])
	require.Equal(t, []string{"frames"}, attrs["adjusted_parameters"])
	require.Equal(t, false, *attrs["generate_audio"].(*bool), "交付无声视频，报价按无声档")
	require.Equal(t, 4, attrs["seconds"])
	require.InDelta(t, 4, tasks.create.HoldAmount, 0.000001, "预扣按换算后的 4 秒")
}
