//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoMaximumOutputSeconds(t *testing.T) {
	caps := DefaultByteDanceVideoCapabilities()
	pro := func(mutate func(*VideoCreateRequest)) VideoCreateRequest {
		request := VideoCreateRequest{Model: ByteDanceVideoModelSeedance10Pro}
		mutate(&request)
		return request
	}
	cases := []struct {
		name     string
		request  VideoSubmitRequest
		provider VideoCreateRequest
		want     int
	}{
		{"explicit seconds", VideoSubmitRequest{Seconds: 8}, pro(func(r *VideoCreateRequest) { r.Seconds = 8 }), 8},
		{"provider seconds fallback", VideoSubmitRequest{}, pro(func(r *VideoCreateRequest) { r.Seconds = 5 }), 5},
		// frames 按 24fps 向上取整：121 帧是 5.04 秒，预扣 6 秒。
		{"frames round up", VideoSubmitRequest{}, pro(func(r *VideoCreateRequest) { r.Frames = 121 }), 6},
		{"frames exact", VideoSubmitRequest{}, pro(func(r *VideoCreateRequest) { r.Frames = 48 }), 2},
		{"frames beat request seconds", VideoSubmitRequest{Seconds: 8}, pro(func(r *VideoCreateRequest) { r.Frames = 29 }), 2},
		// 智能时长按模型声明的最长档位预扣；没有声明时退回 Ark 的自由区间上限。
		{"smart duration takes the longest tier", VideoSubmitRequest{Seconds: 8}, pro(func(r *VideoCreateRequest) { r.SmartDuration = true }), 10},
		{"smart duration on unknown model", VideoSubmitRequest{}, VideoCreateRequest{Model: "doubao-seedance-9-9-pro-270101", SmartDuration: true}, videoByteDanceFreeSecondsMax},
		{"smart duration resolves the alias", VideoSubmitRequest{}, VideoCreateRequest{Model: "alias", RequestedModel: ByteDanceVideoModelSeedance10Lite, SmartDuration: true}, 10},
		{"nothing known", VideoSubmitRequest{}, VideoCreateRequest{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, videoMaximumOutputSeconds(tc.request, tc.provider, caps))
		})
	}
}

func TestVideoRequestAttributesCarryArkFields(t *testing.T) {
	on, off := true, false
	seed := int64(7)
	request := VideoSubmitRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Prompt: "  A tracking shot  ",
		SmartDuration: true, Resolution: "720p", Ratio: ArkVideoRatioAdaptive, AudioEnabled: &on, Seed: &seed,
		CameraFixed: &on, Watermark: &off, ReturnLastFrame: &on, ServiceTier: "flex", ExecutionExpiresAfter: 7200,
		Content: []VideoContentItem{
			{Type: VideoContentTypeText, Text: "A tracking shot"},
			{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://media.example.com/first.png?sig=secret-token"},
			{Type: VideoContentTypeImage, Role: VideoInputRoleTaggedReferenceImage, URL: "https://media.example.com/ref.png"},
			{Type: VideoContentTypeVideo, Role: VideoInputRoleReferenceVideo, URL: "https://media.example.com/clip.mp4"},
			{Type: VideoContentTypeAudio, Role: VideoInputRoleReferenceAudio, URL: "https://media.example.com/voice.mp3"},
		},
	}
	resolved := &resolvedVideoSubmission{
		request: request, account: &Account{ProviderIdentityVersion: 3},
		provider: &videoProviderStub{name: VideoProviderByteDance},
		executionSpec: ResolvedVideoExecutionSpec{
			Version: 2, Provider: VideoProviderByteDance, Resolution: "720p", Ratio: ArkVideoRatioAdaptive, SmartDuration: true, DurationSemantics: "smart_output",
		},
		executionSpecHash: "spec-hash", callbackRetryHours: 24, callbackDisclosurePolicy: "none",
		quote: &VideoPriceQuote{BillingModel: ByteDanceVideoModelSeedance10Pro, Attributes: VideoPricingAttributes{
			Resolution: "720p", Ratio: ArkVideoRatioAdaptive, AudioEnabled: &on, InputHasVideo: true,
			RequestMode: VideoRequestModeStandard, InferenceMode: VideoInferenceModeOnline,
		}},
	}

	attributes := videoRequestAttributes(request, resolved)
	require.Equal(t, 3, videoClientRequestContractVersion)
	require.Equal(t, videoClientRequestContractVersion, attributes["client_request_contract_version"])
	// seconds 与 duration 并存：结算读 seconds，Ark 投影读 duration（智能时长为 -1）。
	require.Equal(t, 0, attributes["seconds"])
	require.Equal(t, -1, attributes["duration"])
	require.Equal(t, true, attributes["smart_duration"])
	require.Equal(t, 0, attributes["frames"])
	require.Equal(t, "720p", attributes["resolution"])
	require.Equal(t, ArkVideoRatioAdaptive, attributes["ratio"])
	require.Equal(t, &seed, attributes["seed"])
	require.Equal(t, &on, attributes["camera_fixed"])
	require.Equal(t, &off, attributes["watermark"])
	require.Equal(t, &on, attributes["return_last_frame"])
	require.Equal(t, &on, attributes["generate_audio"])
	require.Equal(t, &on, attributes["audio_enabled"])
	require.Equal(t, 7200, attributes["execution_expires_after"])
	require.Equal(t, "flex", attributes["service_tier"])
	require.Equal(t, VideoProviderByteDance, attributes["provider"])
	require.Equal(t, true, attributes["input_has_video"])
	require.Equal(t, "A tracking shot", attributes["prompt"], "the prompt is stored trimmed")
	// 媒体引用只留角色计数，不留 URL：签名 URL 是请求级凭据。
	require.Equal(t, map[string]int{"first_frame": 1, "reference_image": 1, "reference_video": 1, "reference_audio": 1}, attributes["content_roles"])
	require.Equal(t, 2, attributes["reference_image_count"])
	require.Equal(t, 1, attributes["reference_video_count"])
	require.Equal(t, 1, attributes["reference_audio_count"])
	raw, err := json.Marshal(attributes)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "media.example.com")
	require.NotContains(t, string(raw), "sig=secret-token")
	require.Equal(t, 2, attributes["execution_spec_version"])
	require.Equal(t, "spec-hash", attributes["execution_spec_hash"])

	// 显式时长：seconds 与 duration 同值；frames 请求原样记录。
	request.SmartDuration, request.Seconds, request.Frames = false, 10, 0
	resolved.request = request
	attributes = videoRequestAttributes(request, resolved)
	require.Equal(t, 10, attributes["seconds"])
	require.Equal(t, 10, attributes["duration"])
	require.Equal(t, false, attributes["smart_duration"])
	request.Seconds, request.Frames = 0, 121
	attributes = videoRequestAttributes(request, resolved)
	require.Equal(t, 121, attributes["frames"])
	require.Equal(t, 0, attributes["duration"])

	// 空提示词不写键；没有媒体引用时计数为零而不是缺键。
	request.Prompt, request.Content = "   ", nil
	attributes = videoRequestAttributes(request, resolved)
	require.NotContains(t, attributes, "prompt")
	require.Equal(t, map[string]int{}, attributes["content_roles"])
	require.Equal(t, 0, attributes["reference_image_count"])
	require.Equal(t, 0, attributes["reference_video_count"])
}

func TestVideoExecutionSpecArkDimensions(t *testing.T) {
	accountID := int64(11)
	newTask := func(t *testing.T, spec ResolvedVideoExecutionSpec) *VideoTask {
		hash, err := HashVideoRequest(spec)
		require.NoError(t, err)
		return &VideoTask{
			Provider: VideoProviderByteDance, AccountID: &accountID, UpstreamModel: ByteDanceVideoModelSeedance10Pro, Operation: VideoOperationGenerate,
			RequestAttributes: map[string]any{"execution_spec": spec, "execution_spec_version": float64(2), "execution_spec_hash": hash},
		}
	}
	spec := ResolvedVideoExecutionSpec{
		Version: 2, Provider: VideoProviderByteDance, AccountID: accountID, Operation: VideoOperationGenerate,
		Model: ByteDanceVideoModelSeedance10Pro, Resolution: "720p", Ratio: "16:9", Seconds: 5, DurationSemantics: "output",
	}

	// resolveVideoExecutionSpec 把 Ark 维度冻进规格：缺省分辨率按 provider 默认补，比例小写化。
	request := VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Ratio: "9:16", SmartDuration: true}
	_, resolvedSpec, err := resolveVideoExecutionSpec(request, accountID, VideoProviderByteDance, nil)
	require.NoError(t, err)
	require.Equal(t, "720p", resolvedSpec.Resolution)
	require.Equal(t, "9:16", resolvedSpec.Ratio)
	require.True(t, resolvedSpec.SmartDuration)
	require.Zero(t, resolvedSpec.Seconds)
	require.Equal(t, "smart_output", resolvedSpec.DurationSemantics)
	framesRequest := VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 121, Resolution: "1080p", Ratio: "ADAPTIVE"}
	_, framesSpec, err := resolveVideoExecutionSpec(framesRequest, accountID, VideoProviderByteDance, nil)
	require.NoError(t, err)
	require.Equal(t, 121, framesSpec.Frames)
	require.Equal(t, "adaptive", framesSpec.Ratio)
	require.Equal(t, "1080p", framesSpec.Resolution)
	require.Equal(t, "output", framesSpec.DurationSemantics)

	// 冻结的规格经序列化后仍带 Ark 字段。
	frozen, err := videoFrozenExecutionSpec(newTask(t, resolvedSpec))
	require.NoError(t, err)
	require.True(t, frozen.SmartDuration)
	require.Equal(t, "9:16", frozen.Ratio)

	// 观测值比对：分辨率、比例（大小写不敏感）、秒数都要与规格一致。
	task := newTask(t, spec)
	require.NoError(t, videoCheckObservedSpecification(task, map[string]any{"resolution": "720P", "ratio": "16:9", "seconds": float64(5)}))
	require.ErrorIs(t, videoCheckObservedSpecification(task, map[string]any{"ratio": "9:16"}), ErrVideoSourceSpecConflict)
	require.ErrorIs(t, videoCheckObservedSpecification(task, map[string]any{"resolution": "1080p"}), ErrVideoSourceSpecConflict)
	require.ErrorIs(t, videoCheckObservedSpecification(task, map[string]any{"seconds": float64(7)}), ErrVideoSourceSpecConflict)
	require.ErrorIs(t, videoCheckObservedSpecification(task, map[string]any{"ratio": 169}), ErrVideoSourceSpecConflict, "a non-string ratio is a conflict")

	// adaptive 比例：上游回报的任何比例都接受——只有上游知道图片的比例。
	adaptive := spec
	adaptive.Ratio = ArkVideoRatioAdaptive
	require.NoError(t, videoCheckObservedSpecification(newTask(t, adaptive), map[string]any{"ratio": "9:16"}))
	// 规格没有比例时也不比对。
	noRatio := spec
	noRatio.Ratio = ""
	require.NoError(t, videoCheckObservedSpecification(newTask(t, noRatio), map[string]any{"ratio": "4:3"}))

	// 智能时长：秒数没有预期值，任何正数都是结算依据；非正数仍是冲突。
	smart := spec
	smart.Seconds, smart.SmartDuration, smart.DurationSemantics = 0, true, "smart_output"
	require.NoError(t, videoCheckObservedSpecification(newTask(t, smart), map[string]any{"seconds": float64(12)}))
	require.ErrorIs(t, videoCheckObservedSpecification(newTask(t, smart), map[string]any{"seconds": float64(0)}), ErrVideoSourceSpecConflict)
	require.ErrorIs(t, videoCheckObservedSpecification(newTask(t, smart), map[string]any{"ratio": "9:16"}), ErrVideoSourceSpecConflict, "smart duration does not relax the ratio check")
	observed := videoObservedMetadata(newTask(t, smart), map[string]any{"seconds": float64(12), "ratio": "16:9"})
	require.NotContains(t, observed, "execution_spec_conflict")
}
