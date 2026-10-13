//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func arkContentItems(role VideoInputRole, count int) []VideoContentItem {
	items := make([]VideoContentItem, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, VideoContentItem{Type: role.ArkContentTypeForRole(), Role: role, URL: "https://media.example.com/" + string(role) + ".bin"})
	}
	return items
}

func TestValidateVideoArkCreateCapabilities(t *testing.T) {
	ark := DefaultByteDanceVideoCapabilities()
	// 内置快照里 1.0 系列不支持智能时长；smart 模拟运行时目录给某模型补上了声明。
	smart := cloneVideoCapabilities(ark)
	smart.SmartDurationModels = map[string]bool{ByteDanceVideoModelSeedance10Pro: true}
	vendor := newRunwareVideoAdapter().DefaultCapabilities()
	openAI := DefaultOpenAIVideoCapabilities()

	cases := []struct {
		name    string
		caps    VideoCapabilities
		request VideoCreateRequest
		known   bool
		wantErr error
	}{
		{"text only", ark, VideoCreateRequest{Seconds: 5}, true, nil},
		{"first and last frame", ark, VideoCreateRequest{Content: append(arkContentItems(VideoInputRoleFirstFrame, 1), arkContentItems(VideoInputRoleLastFrame, 1)...)}, true, nil},
		{"two first frames", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleFirstFrame, 2)}, true, ErrVideoInputUnsupported},
		{"nine reference images", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleTaggedReferenceImage, 9)}, true, nil},
		{"ten reference images", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleTaggedReferenceImage, 10)}, true, ErrVideoInputUnsupported},
		{"three reference videos", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceVideo, 3)}, true, nil},
		{"four reference videos", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceVideo, 4)}, true, ErrVideoInputUnsupported},
		{"unknown role", ark, VideoCreateRequest{Content: []VideoContentItem{{Type: VideoContentTypeImage, Role: VideoInputRoleMask, URL: "https://x/m.png"}}}, true, ErrVideoInputUnsupported},
		{"unknown model still checks roles", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleFirstFrame, 2)}, false, ErrVideoInputUnsupported},
		// 三家聚合平台只收首尾帧。
		{"vendor accepts first frame", vendor, VideoCreateRequest{Content: arkContentItems(VideoInputRoleFirstFrame, 1)}, true, nil},
		{"vendor rejects reference video", vendor, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceVideo, 1)}, true, ErrVideoInputUnsupported},
		// 没有 MaxContentByRole 的能力表（OpenAI 兼容上游）不在这里限角色：first_image_url /
		// reference_* 由 openai_video_compatible_references.go 自己校验。
		{"legacy caps accept input_reference", openAI, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceImage, 1)}, true, nil},
		{"legacy caps skip role limits", openAI, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceVideo, 4)}, true, nil},
		// adaptive 必须有首帧（或旧式 input_reference 图）可跟随。
		{"adaptive without image", ark, VideoCreateRequest{Ratio: ArkVideoRatioAdaptive}, true, ErrVideoAdaptiveRatioRequiresImage},
		{"adaptive with last frame only", ark, VideoCreateRequest{Ratio: ArkVideoRatioAdaptive, Content: arkContentItems(VideoInputRoleLastFrame, 1)}, true, ErrVideoAdaptiveRatioRequiresImage},
		{"adaptive with first frame", ark, VideoCreateRequest{Ratio: ArkVideoRatioAdaptive, Content: arkContentItems(VideoInputRoleFirstFrame, 1)}, true, nil},
		{"adaptive with input_reference", openAI, VideoCreateRequest{Ratio: ArkVideoRatioAdaptive, Content: arkContentItems(VideoInputRoleReferenceImage, 1)}, true, nil},
		{"ark caps reject generic input_reference content", ark, VideoCreateRequest{Content: arkContentItems(VideoInputRoleReferenceImage, 1)}, true, ErrVideoInputUnsupported},
		// 智能时长与 frames 由 SmartDurationModels 一并门控，且只对能力表认识的模型生效：
		// 新模型 / ep-* 接入点交给 Ark 自己裁决。
		{"smart duration allowed", smart, VideoCreateRequest{SmartDuration: true}, true, nil},
		{"smart duration gated", ark, VideoCreateRequest{SmartDuration: true}, true, ErrVideoCapabilityUnsupported},
		{"smart duration on unknown model deferred", ark, VideoCreateRequest{Model: "doubao-seedance-2-0-260128", SmartDuration: true}, false, nil},
		{"frames allowed", smart, VideoCreateRequest{Frames: 29}, true, nil},
		{"frames gated", ark, VideoCreateRequest{Frames: 29}, true, ErrVideoCapabilityUnsupported},
		{"frames on unknown model deferred", ark, VideoCreateRequest{Model: "ep-20261013-abcdef", Frames: 29}, false, nil},
		{"vendor gates smart duration", vendor, VideoCreateRequest{SmartDuration: true}, true, ErrVideoCapabilityUnsupported},
		// 分辨率与比例词表只对已知模型生效；取值大小写不敏感。
		{"unknown resolution", ark, VideoCreateRequest{Resolution: "2k"}, true, ErrVideoCapabilityUnsupported},
		{"unknown resolution on unknown model", ark, VideoCreateRequest{Resolution: "2k"}, false, nil},
		{"uppercase resolution", ark, VideoCreateRequest{Resolution: "720P"}, true, nil},
		{"unknown ratio", ark, VideoCreateRequest{Ratio: "3:2"}, true, ErrVideoCapabilityUnsupported},
		{"unknown ratio on unknown model", ark, VideoCreateRequest{Ratio: "3:2"}, false, nil},
		// adaptive 是模式不是比例：词表里没有它也放行，只要求有首帧图。
		{"adaptive outside vocabulary with first frame", vendor, VideoCreateRequest{Model: "bytedance:seedance@2.0", Ratio: ArkVideoRatioAdaptive, Content: arkContentItems(VideoInputRoleFirstFrame, 1)}, true, nil},
		{"adaptive outside vocabulary without image", vendor, VideoCreateRequest{Model: "bytedance:seedance@2.0", Ratio: ArkVideoRatioAdaptive}, true, ErrVideoAdaptiveRatioRequiresImage},
		{"known ratio", ark, VideoCreateRequest{Ratio: "21:9"}, true, nil},
		// 非 generate 操作不做 Ark 维度校验。
		{"edit skips ark checks", ark, VideoCreateRequest{Operation: VideoOperationEdit, Ratio: "5:2", Content: arkContentItems(VideoInputRoleFirstFrame, 3)}, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := tc.request
			if request.Operation == "" {
				request.Operation = VideoOperationGenerate
			}
			if request.Model == "" {
				request.Model = ByteDanceVideoModelSeedance10Pro
			}
			err := validateVideoArkCreateCapabilities(tc.caps, request, tc.known)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}

	// ValidateVideoCreateCapabilities 把 Ark 维度接进了总入口。
	err := ValidateVideoCreateCapabilities(ark, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Ratio: ArkVideoRatioAdaptive,
	}, nil)
	require.ErrorIs(t, err, ErrVideoAdaptiveRatioRequiresImage)
	err = ValidateVideoCreateCapabilities(smart, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 121, Resolution: "1080p",
		Content: arkContentItems(VideoInputRoleFirstFrame, 1),
	}, nil)
	require.NoError(t, err)
	// 1.0 系列按 Ark 文档接受 2 到 12 秒；能力表外的秒数才被拦。
	err = ValidateVideoCreateCapabilities(ark, VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 8}, nil)
	require.NoError(t, err)
	err = ValidateVideoCreateCapabilities(ark, VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 13}, nil)
	require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
	require.Contains(t, err.Error(), "seconds 13", "细节随错误一起返回给调用方")
}

func TestApplyVideoCapabilityDefaultsArkBranch(t *testing.T) {
	caps := DefaultByteDanceVideoCapabilities()

	// 文生视频：补模型、秒数、分辨率与比例；绝不补 OpenAI 的 size。
	applied := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{Operation: VideoOperationGenerate})
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, applied.Model)
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, applied.RequestedModel)
	require.Equal(t, 5, applied.Seconds)
	require.Equal(t, "720p", applied.Resolution)
	require.Equal(t, "16:9", applied.Ratio)
	require.Empty(t, applied.Size)

	// 图生视频：比例跟随图片，不补默认比例；分辨率仍补。
	for _, role := range []VideoInputRole{VideoInputRoleFirstFrame, VideoInputRoleReferenceImage} {
		withImage := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Content: arkContentItems(role, 1),
		})
		require.Empty(t, withImage.Ratio, role)
		require.Equal(t, "720p", withImage.Resolution, role)
	}
	// 只有参考图 / 参考视频（没有首帧）时比例仍需补齐。
	withReference := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro,
		Content: append(arkContentItems(VideoInputRoleTaggedReferenceImage, 1), arkContentItems(VideoInputRoleReferenceVideo, 1)...),
	})
	require.Equal(t, "16:9", withReference.Ratio)

	// 显式值不被覆盖；adaptive 是显式值。
	explicit := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Lite, RequestedModel: "alias",
		Seconds: 10, Resolution: "1080p", Ratio: ArkVideoRatioAdaptive,
	})
	require.Equal(t, "alias", explicit.RequestedModel)
	require.Equal(t, 10, explicit.Seconds)
	require.Equal(t, "1080p", explicit.Resolution)
	require.Equal(t, ArkVideoRatioAdaptive, explicit.Ratio)

	// 智能时长 / frames 不补秒数。
	smart := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, SmartDuration: true})
	require.Zero(t, smart.Seconds)
	require.True(t, smart.SmartDuration)
	frames := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 29})
	require.Zero(t, frames.Seconds)
	require.Equal(t, 29, frames.Frames)

	// 非 generate 操作只补模型。
	edit := ApplyVideoCapabilityDefaults(caps, VideoCreateRequest{Operation: VideoOperationEdit})
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, edit.Model)
	require.Zero(t, edit.Seconds)
	require.Empty(t, edit.Resolution)
	require.Empty(t, edit.Ratio)

	// 聚合平台的能力表同样走 Ark 分支（Seedance 2.0 在 Runware 默认 720p / 16:9）。
	vendor := ApplyVideoCapabilityDefaults(newRunwareVideoAdapter().DefaultCapabilities(), VideoCreateRequest{Operation: VideoOperationGenerate})
	require.Equal(t, "bytedance:seedance@2.0", vendor.Model)
	require.Equal(t, "720p", vendor.Resolution)
	require.Equal(t, "16:9", vendor.Ratio)
	require.Empty(t, vendor.Size)

	// OpenAI 能力表仍走 size 分支，不碰 Ark 维度。
	openAI := ApplyVideoCapabilityDefaults(DefaultOpenAIVideoCapabilities(), VideoCreateRequest{Operation: VideoOperationGenerate})
	require.Equal(t, OpenAIVideoModelSora2, openAI.Model)
	require.Equal(t, 4, openAI.Seconds)
	require.Equal(t, "720x1280", openAI.Size)
	require.Empty(t, openAI.Resolution)
	require.Empty(t, openAI.Ratio)
}
