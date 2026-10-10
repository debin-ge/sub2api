//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoModelProfileFinalizeArkDimensions(t *testing.T) {
	profile := &videoModelProfile{
		PublicModel: "vendor/model", AllowsImage: true, MaxImages: 2,
		Sizes: []string{"1280x720", "720x1280", "960x960", "1920x1080", "1470x630"}, DefaultSize: "720x1280",
		AudioField: "settings.audio",
		OptionWhitelist: map[string]string{
			"seed": "seed", "cameraFixed": "providerSettings.bytedance.cameraFixed", "negativePrompt": "negativePrompt",
		},
	}
	require.Same(t, profile, profile.finalizeArkDimensions())
	// 比例与档位从尺寸表推导（去重、保持尺寸表顺序）；默认值从默认尺寸推导。
	require.Equal(t, []string{"16:9", "9:16", "1:1", "21:9"}, profile.Ratios)
	require.Equal(t, []string{"720p", "1080p"}, profile.CanonicalResolutions)
	require.Equal(t, "9:16", profile.DefaultRatio)
	require.Equal(t, "720p", profile.DefaultResolution)
	// Ark 显式字段从 provider_options 白名单挪进 ArkFieldMap（cameraFixed 的驼峰别名也认）。
	require.Equal(t, map[string]string{"seed": "seed", "camera_fixed": "providerSettings.bytedance.cameraFixed"}, profile.ArkFieldMap)
	require.Equal(t, map[string]string{"negativePrompt": "negativePrompt"}, profile.OptionWhitelist)
	require.True(t, profile.supportsRatio("21:9"))
	require.False(t, profile.supportsRatio("4:3"))
	require.True(t, profile.supportsResolution("1080p"))
	require.False(t, profile.supportsResolution("4k"))

	// 幂等：再跑一次不重复追加、不改默认值。
	profile.finalizeArkDimensions()
	require.Equal(t, []string{"16:9", "9:16", "1:1", "21:9"}, profile.Ratios)
	require.Equal(t, []string{"720p", "1080p"}, profile.CanonicalResolutions)
	require.Equal(t, "9:16", profile.DefaultRatio)
	require.Equal(t, map[string]string{"negativePrompt": "negativePrompt"}, profile.OptionWhitelist)

	// 厂商词表（MiniMax 的 768p）经 ResolutionLabels 对应回 Ark 档位；显式声明的 Ratios 不被覆盖。
	labelled := &videoModelProfile{
		Resolutions: []string{"768p"}, ResolutionLabels: map[string]string{"720p": "768p"},
		Ratios: []string{"16:9"}, Sizes: []string{"1366x768", "768x1366"},
	}
	labelled.finalizeArkDimensions()
	require.Equal(t, []string{"720p"}, labelled.CanonicalResolutions)
	require.Equal(t, []string{"16:9"}, labelled.Ratios)
	require.Equal(t, "16:9", labelled.DefaultRatio)
	require.Equal(t, "720p", labelled.DefaultResolution)
	label, ok := labelled.vendorResolutionLabel("720p")
	require.True(t, ok)
	require.Equal(t, "768p", label)
	_, ok = labelled.vendorResolutionLabel("1080p")
	require.False(t, ok)

	// 已经手写了 Ark 维度的档案原样保留。
	explicit := &videoModelProfile{Ratios: []string{"9:16"}, CanonicalResolutions: []string{"480p"}, DefaultRatio: "9:16", DefaultResolution: "480p", Sizes: []string{"1920x1080"}, DefaultSize: "1920x1080"}
	explicit.finalizeArkDimensions()
	require.Equal(t, []string{"9:16"}, explicit.Ratios)
	require.Equal(t, []string{"480p"}, explicit.CanonicalResolutions)
	require.Equal(t, "9:16", explicit.DefaultRatio)
	require.Equal(t, "480p", explicit.DefaultResolution)

	// 没有任何尺寸信息：词表留空表示不限制（只认 Ark 词表），ArkFieldMap 仍被初始化。
	bare := (&videoModelProfile{}).finalizeArkDimensions()
	require.Empty(t, bare.Ratios)
	require.Empty(t, bare.CanonicalResolutions)
	require.NotNil(t, bare.ArkFieldMap)
	require.True(t, bare.supportsRatio("21:9"))
	require.False(t, bare.supportsRatio("3:2"))
	require.True(t, bare.supportsResolution("2k"))
	require.False(t, bare.supportsResolution("768p"))
	require.Nil(t, (*videoModelProfile)(nil).finalizeArkDimensions())
}

func TestVideoVendorApplyArkFields(t *testing.T) {
	profile := (&videoModelProfile{
		PublicModel: "vendor/model", AudioField: "settings.audio",
		OptionWhitelist: map[string]string{"seed": "seed", "watermark": "providerSettings.watermark"},
	}).finalizeArkDimensions()
	on, off := true, false
	seed := int64(42)

	body := map[string]any{}
	require.NoError(t, videoVendorApplyArkFields(body, profile, VideoCreateRequest{Seed: &seed, Watermark: &off, AudioEnabled: &on}))
	require.Equal(t, map[string]any{
		"seed": int64(42), "providerSettings": map[string]any{"watermark": false}, "settings": map[string]any{"audio": true},
	}, body)

	// 未映射的开关带值即拒绝：静默丢弃等于计费与产出不一致。
	var providerErr *VideoProviderError
	err := videoVendorApplyArkFields(map[string]any{}, profile, VideoCreateRequest{Model: "vendor/model", CameraFixed: &on})
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "unsupported_parameter", providerErr.Code)
	require.Equal(t, VideoSubmissionRejected, providerErr.Certainty)
	require.Contains(t, providerErr.Message, "camera_fixed")
	require.Contains(t, providerErr.Message, "vendor/model")

	// 没有音频字段的模型：generate_audio=true 拒绝，false 是空操作（它本来就不出声）。
	silent := (&videoModelProfile{PublicModel: "vendor/silent"}).finalizeArkDimensions()
	err = videoVendorApplyArkFields(map[string]any{}, silent, VideoCreateRequest{AudioEnabled: &on})
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "unsupported_parameter", providerErr.Code)
	body = map[string]any{}
	require.NoError(t, videoVendorApplyArkFields(body, silent, VideoCreateRequest{AudioEnabled: &off}))
	require.Empty(t, body)

	// ArkFieldMap 里显式映射的 generate_audio 优先于 AudioField。
	mapped := &videoModelProfile{AudioField: "settings.audio", ArkFieldMap: map[string]string{"generate_audio": "sound"}}
	body = map[string]any{}
	require.NoError(t, videoVendorApplyArkFields(body, mapped, VideoCreateRequest{AudioEnabled: &on}))
	require.Equal(t, map[string]any{"sound": true}, body)

	// nil 档案或没有任何开关：空操作。
	require.NoError(t, videoVendorApplyArkFields(map[string]any{}, nil, VideoCreateRequest{Seed: &seed}))
	body = map[string]any{}
	require.NoError(t, videoVendorApplyArkFields(body, profile, VideoCreateRequest{}))
	require.Empty(t, body)
}

func TestVideoVendorApplyFraming(t *testing.T) {
	request := VideoCreateRequest{Ratio: "9:16", Resolution: "720p"}
	framing := func(profile *videoModelProfile, request VideoCreateRequest, hasImage bool) map[string]any {
		body := map[string]any{}
		videoVendorApplyFraming(body, profile, request, hasImage, "aspect_ratio", "resolution")
		return body
	}
	both := &videoModelProfile{SizeEnc: videoSizeAspectRatioRes}
	require.Equal(t, map[string]any{"aspect_ratio": "9:16", "resolution": "720p"}, framing(both, request, false))
	// 带图时比例随图；adaptive 也不送。
	require.Equal(t, map[string]any{"resolution": "720p"}, framing(both, request, true))
	require.Equal(t, map[string]any{"resolution": "720p"}, framing(both, VideoCreateRequest{Ratio: ArkVideoRatioAdaptive, Resolution: "720p"}, false))
	require.Equal(t, map[string]any{"aspect_ratio": "9:16"}, framing(both, VideoCreateRequest{Ratio: "9:16"}, false), "no resolution means no resolution field")

	// 分辨率先换成厂商词表；不在词表内就不送，换一个 400 不如少送一个字段。
	labelled := &videoModelProfile{SizeEnc: videoSizeAspectRatioRes, Resolutions: []string{"768p", "1080p"}, ResolutionLabels: map[string]string{"720p": "768p"}}
	require.Equal(t, map[string]any{"aspect_ratio": "9:16", "resolution": "768p"}, framing(labelled, request, false))
	require.Equal(t, map[string]any{"aspect_ratio": "16:9"}, framing(labelled, VideoCreateRequest{Ratio: "16:9", Resolution: "4k"}, false))

	// 其余编码：只传比例 / 只传档位 / 档位固化在模型 ID / 由素材决定 / 宽高由 adapter 自己编码。
	require.Equal(t, map[string]any{"aspect_ratio": "9:16"}, framing(&videoModelProfile{SizeEnc: videoSizeAspectRatio}, request, false))
	require.Equal(t, map[string]any{"aspect_ratio": "9:16"}, framing(&videoModelProfile{SizeEnc: videoSizeResolutionInID}, request, false))
	require.Equal(t, map[string]any{"resolution": "720p"}, framing(&videoModelProfile{SizeEnc: videoSizeResolutionOnly}, request, false))
	require.Equal(t, map[string]any{"resolution": "720p"}, framing(&videoModelProfile{SizeEnc: videoSizeResolutionOnly}, request, true), "an image only drops the ratio, never the tier")
	require.Empty(t, framing(&videoModelProfile{SizeEnc: videoSizeNotSupported}, request, false))
	require.Empty(t, framing(&videoModelProfile{SizeEnc: videoSizeWidthHeight}, request, false))
	require.Empty(t, framing(nil, request, false))

	// 字段名走点号路径。
	body := map[string]any{}
	videoVendorApplyFraming(body, both, request, false, "settings.aspect", "settings.res")
	require.Equal(t, map[string]any{"settings": map[string]any{"aspect": "9:16", "res": "720p"}}, body)
}

func TestRunwareTextToVideoSizeFromArkDimensions(t *testing.T) {
	adapter := newRunwareVideoAdapter()
	profiles := adapter.Profiles()
	seedance20 := profiles["bytedance:seedance@2.0"].finalizeArkDimensions()
	seedance10 := profiles["bytedance:2@1"].finalizeArkDimensions()

	size, ok := seedance20.sizeForArkDimensions("720p", "21:9")
	require.True(t, ok)
	require.Equal(t, "1470x630", size)
	// 档案自己的尺寸表优先于 Seedance 代表尺寸表（1.0 Pro 的 1080p 16:9 是 1920x1088）。
	size, ok = seedance10.sizeForArkDimensions("1080p", "16:9")
	require.True(t, ok)
	require.Equal(t, "1920x1088", size)
	// 档案没有的组合回落到代表尺寸表；表里也没有的组合拒绝。
	size, ok = seedance10.sizeForArkDimensions("720p", "16:9")
	require.True(t, ok)
	require.Equal(t, "1280x720", size)
	_, ok = seedance10.sizeForArkDimensions("2k", "16:9")
	require.False(t, ok)
	_, ok = seedance10.sizeForArkDimensions("720p", ArkVideoRatioAdaptive)
	require.False(t, ok)
	size, ok = (*videoModelProfile)(nil).sizeForArkDimensions("4k", "9:16")
	require.True(t, ok)
	require.Equal(t, "2160x3840", size)

	const taskUUID = "01234567-89ab-cdef-0123-456789abcdef"
	decode := func(t *testing.T, upstream *vendorHTTPRequest) map[string]any {
		t.Helper()
		var items []map[string]any
		require.NoError(t, json.Unmarshal(upstream.Body, &items))
		require.Len(t, items, 1)
		return items[0]
	}

	// 文生视频：(resolution, ratio) 换成 Runware 的 width / height。
	upstream, err := adapter.BuildCreate(VideoCreateRequest{Model: "bytedance:2@1", Prompt: "p", Seconds: 5, Resolution: "1080p", Ratio: "16:9"}, seedance10, nil, taskUUID)
	require.NoError(t, err)
	body := decode(t, upstream)
	require.Equal(t, float64(1920), body["width"])
	require.Equal(t, float64(1088), body["height"])
	_, hasResolution := body["resolution"]
	require.False(t, hasResolution)

	// 缺省时用档案默认档位与比例（Seedance 2.0：1280x720）。
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: "bytedance:seedance@2.0", Prompt: "p", Seconds: 5}, seedance20, nil, taskUUID)
	require.NoError(t, err)
	body = decode(t, upstream)
	require.Equal(t, float64(1280), body["width"])
	require.Equal(t, float64(720), body["height"])

	// 没有对应尺寸：拒绝而不是猜一个。
	_, err = adapter.BuildCreate(VideoCreateRequest{Model: "bytedance:seedance@2.0", Prompt: "p", Seconds: 5, Resolution: "2k", Ratio: "16:9"}, seedance20, nil, taskUUID)
	var providerErr *VideoProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, "unsupported_resolution", providerErr.Code)

	// 带图不传宽高，用 resolution 档位表达尺寸诉求。
	upstream, err = adapter.BuildCreate(VideoCreateRequest{Model: "bytedance:2@1", Prompt: "p", Seconds: 5, Resolution: "1080p", Ratio: "16:9"}, seedance10, []videoVendorInput{{URL: "https://cdn.example.com/a.png"}}, taskUUID)
	require.NoError(t, err)
	body = decode(t, upstream)
	_, hasWidth := body["width"]
	require.False(t, hasWidth)
	require.Equal(t, "1080p", body["resolution"])
}
