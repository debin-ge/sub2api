package service

import (
	"fmt"
	"slices"
	"strings"
)

const (
	OpenAIVideoModelSora2    = "sora-2"
	OpenAIVideoModelSora2Pro = "sora-2-pro"

	// Ark model identifiers are fully hyphenated and carry a release-date
	// suffix. The output resolution is a request parameter, so it never appears
	// in the identifier.
	ByteDanceVideoModelSeedance10Lite = "doubao-seedance-1-0-lite-t2v-250428"
	ByteDanceVideoModelSeedance10Pro  = "doubao-seedance-1-0-pro-250528"
)

// DefaultByteDanceVideoCapabilities is the built-in snapshot for Ark video.
//
// SupportedModels is descriptive, not a routing whitelist: an account's
// model_mapping decides which upstream models may be reached, and an entry here
// only declares the per-model request constraints this build knows. Newer
// Seedance releases therefore work without a code change, and their duration
// limits can be declared through the runtime capability catalog.
func DefaultByteDanceVideoCapabilities() VideoCapabilities {
	models := []string{ByteDanceVideoModelSeedance10Lite, ByteDanceVideoModelSeedance10Pro}
	supportedModels := make(map[string]bool, len(models))
	supportedSeconds := make(map[string][]int, len(models))
	defaultSeconds := make(map[string]int, len(models))
	supportedResolutions := make(map[string][]string, len(models))
	supportedRatios := make(map[string][]string, len(models))
	defaultResolutions := make(map[string]string, len(models))
	defaultRatios := make(map[string]string, len(models))
	smartDuration := make(map[string]bool, len(models))
	for _, model := range models {
		supportedModels[model] = true
		// Ark 文档：Seedance 1.0 系列 duration 取 [2, 12] 的整数秒，默认 5；不支持智能时长（-1）
		// 与 frames，那是 1.5 Pro 与 2.x 的能力。能力表不认识的新模型由 Ark 自己裁决。
		supportedSeconds[model] = byteDanceSeedance10Durations()
		defaultSeconds[model] = 5
		supportedResolutions[model] = []string{"480p", "720p", "1080p"}
		supportedRatios[model] = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", ArkVideoRatioAdaptive}
		defaultResolutions[model] = "720p"
		defaultRatios[model] = "16:9"
	}
	imageMIMEs := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/bmp": true, "image/tiff": true, "image/gif": true}
	return VideoCapabilities{
		DefaultModel:   ByteDanceVideoModelSeedance10Pro,
		DefaultSeconds: defaultSeconds,
		// Ark exposes asynchronous generation only. Edits, extensions and
		// characters have no counterpart in the API, and DELETE is a cancel.
		Operations: map[VideoCapability]bool{
			VideoCapabilityCreate: true, VideoCapabilityInputReference: true, VideoCapabilityCancel: true,
		},
		InputRolesByOperation: map[string]map[VideoInputRole]bool{
			VideoOperationGenerate: {
				VideoInputRoleFirstFrame: true, VideoInputRoleLastFrame: true, VideoInputRoleTaggedReferenceImage: true,
			},
		},
		InputMIMETypes: map[VideoInputRole]map[string]bool{
			VideoInputRoleFirstFrame: imageMIMEs, VideoInputRoleLastFrame: imageMIMEs, VideoInputRoleTaggedReferenceImage: imageMIMEs,
		},
		// An image reference is inlined into the JSON body as a data URI, so the
		// upload cap is the inline cap rather than a multipart limit.
		MaxInputBytes: map[VideoInputRole]int64{
			VideoInputRoleFirstFrame: byteDanceMaxInlineImageBytes, VideoInputRoleLastFrame: byteDanceMaxInlineImageBytes,
			VideoInputRoleTaggedReferenceImage: byteDanceMaxInlineImageBytes,
		},
		MaxInputsByOperation: map[string]int{VideoOperationGenerate: 11},
		// content[] 角色上限按 Ark 文档：首尾帧各一张，参考图最多 9 张，参考视频 3 段，参考音频 3 段。
		MaxContentByRole: map[VideoInputRole]int{
			VideoInputRoleFirstFrame: 1, VideoInputRoleLastFrame: 1, VideoInputRoleTaggedReferenceImage: 9,
			VideoInputRoleReferenceVideo: 3, VideoInputRoleReferenceAudio: 3,
		},
		ContentVariants:      map[string]bool{"video": true, "last_frame": true},
		SupportedModels:      supportedModels,
		SupportedSeconds:     supportedSeconds,
		SupportedResolutions: supportedResolutions,
		SupportedRatios:      supportedRatios,
		DefaultResolutions:   defaultResolutions,
		DefaultRatios:        defaultRatios,
		SmartDurationModels:  smartDuration,
	}
}

// byteDanceSeedance10Durations 是 Seedance 1.0 系列的时长档位：2 到 12 秒的连续整数。
func byteDanceSeedance10Durations() []int {
	values := make([]int, 0, 11)
	for seconds := 2; seconds <= 12; seconds++ {
		values = append(values, seconds)
	}
	return values
}

// DefaultOpenAIVideoCapabilities is the built-in safe snapshot. Runtime
// catalog overrides can replace it after validation without changing the
// provider adapter or task state machine.
func DefaultOpenAIVideoCapabilities() VideoCapabilities {
	operations := map[VideoCapability]bool{
		VideoCapabilityCreate:             true,
		VideoCapabilityInputReference:     true,
		VideoCapabilityCharacters:         true,
		VideoCapabilityUploadedVideoEdits: true,
		VideoCapabilityEdits:              true,
		VideoCapabilityExtensions:         true,
		VideoCapabilityWebhook:            true,
	}
	inputMIMEs := map[VideoInputRole]map[string]bool{
		VideoInputRoleReferenceImage: {
			"image/jpeg": true,
			"image/png":  true,
			"image/webp": true,
		},
		VideoInputRoleSourceVideo: {
			"video/mp4": true,
		},
		VideoInputRoleCharacterClip: {
			"video/mp4": true,
		},
	}
	return VideoCapabilities{
		DefaultModel: OpenAIVideoModelSora2,
		DefaultSeconds: map[string]int{
			OpenAIVideoModelSora2:    4,
			OpenAIVideoModelSora2Pro: 4,
		},
		DefaultSizes: map[string]string{
			OpenAIVideoModelSora2:    "720x1280",
			OpenAIVideoModelSora2Pro: "720x1280",
		},
		Operations: operations,
		InputRolesByOperation: map[string]map[VideoInputRole]bool{
			VideoOperationGenerate:        {VideoInputRoleReferenceImage: true},
			VideoOperationEdit:            {VideoInputRoleSourceVideo: true},
			VideoOperationCharacterCreate: {VideoInputRoleCharacterClip: true},
		},
		InputMIMETypes: inputMIMEs,
		MaxInputBytes: map[VideoInputRole]int64{
			VideoInputRoleReferenceImage: MaxContentModerationImageBytes,
			VideoInputRoleSourceVideo:    100 << 20,
			VideoInputRoleCharacterClip:  100 << 20,
		},
		MaxInputsByOperation: map[string]int{
			VideoOperationGenerate:        1,
			VideoOperationEdit:            1,
			VideoOperationCharacterCreate: 1,
		},
		ContentVariants: map[string]bool{"video": true, "thumbnail": true, "spritesheet": true},
		SupportedModels: map[string]bool{
			OpenAIVideoModelSora2:    true,
			OpenAIVideoModelSora2Pro: true,
			"sora-2-2025-10-06":      true,
			"sora-2-2025-12-08":      true,
			"sora-2-pro-2025-10-06":  true,
		},
		SupportedSeconds: map[string][]int{
			OpenAIVideoModelSora2:    {4, 8, 12, 16, 20},
			OpenAIVideoModelSora2Pro: {4, 8, 12, 16, 20},
		},
		SupportedSizes: map[string][]string{
			OpenAIVideoModelSora2: {
				"720x1280", "1280x720", "1024x1792", "1792x1024",
			},
			OpenAIVideoModelSora2Pro: {
				"720x1280", "1280x720", "1024x1792", "1792x1024",
				"1080x1920", "1920x1080",
			},
		},
	}
}

func ValidateVideoCreateCapabilities(caps VideoCapabilities, request VideoCreateRequest, inputs []VideoInput) error {
	operation := VideoCapabilityCreate
	switch request.Operation {
	case "", VideoOperationGenerate:
	case VideoOperationEdit:
		operation = VideoCapabilityEdits
	case VideoOperationExtend:
		operation = VideoCapabilityExtensions
	case VideoOperationCharacterCreate:
		operation = VideoCapabilityCharacters
	default:
		return fmt.Errorf("%w: operation %q", ErrVideoCapabilityUnsupported, request.Operation)
	}
	if !caps.Supports(operation) {
		return fmt.Errorf("%w: operation %q", ErrVideoCapabilityUnsupported, request.Operation)
	}
	if request.Operation == VideoOperationEdit && len(inputs) > 0 && !caps.Supports(VideoCapabilityUploadedVideoEdits) {
		return fmt.Errorf("%w: uploaded video edits", ErrVideoCapabilityUnsupported)
	}
	operationName := normalizeVideoOperation(request.Operation)
	if maximum := caps.MaxInputsByOperation[operationName]; maximum > 0 && len(inputs) > maximum {
		return fmt.Errorf("%w: operation %q accepts at most %d binary input(s)", ErrVideoInputUnsupported, operationName, maximum)
	}
	if request.InputReference != nil {
		hasFile := strings.TrimSpace(request.InputReference.FileID) != ""
		hasURL := strings.TrimSpace(request.InputReference.ImageURL) != ""
		if hasFile == hasURL || !caps.Supports(VideoCapabilityInputReference) {
			return fmt.Errorf("%w: input_reference", ErrVideoInputUnsupported)
		}
		if len(inputs) > 0 && !caps.AllowReferenceAndFile {
			return fmt.Errorf("%w: input_reference cannot be combined with uploaded binary input", ErrVideoInputUnsupported)
		}
	}
	model := strings.ToLower(strings.TrimSpace(request.RequestedModel))
	if model == "" {
		model = strings.ToLower(strings.TrimSpace(request.Model))
	}
	if model == "" {
		return fmt.Errorf("%w: model %q", ErrVideoCapabilityUnsupported, model)
	}
	// SupportedModels describes models whose request constraints are known to
	// the built-in catalog. Account model_mapping remains the routing whitelist;
	// custom compatible upstream models must not be rejected merely because
	// they are absent from this snapshot.
	knownModel := caps.SupportedModels[model]
	if allowed, ok := caps.SupportedSeconds[canonicalOpenAIVideoModel(model)]; operationName != VideoOperationEdit && knownModel && ok && request.Seconds > 0 && !slices.Contains(allowed, request.Seconds) {
		return fmt.Errorf("%w: seconds %d", ErrVideoCapabilityUnsupported, request.Seconds)
	}
	if allowed, ok := caps.SupportedSizes[canonicalOpenAIVideoModel(model)]; knownModel && ok && strings.TrimSpace(request.Size) != "" && !slices.Contains(allowed, strings.ToLower(strings.TrimSpace(request.Size))) {
		return fmt.Errorf("%w: size %q", ErrVideoCapabilityUnsupported, request.Size)
	}
	if err := validateVideoArkCreateCapabilities(caps, request, knownModel); err != nil {
		return err
	}
	for _, input := range inputs {
		if !caps.SupportsInputForOperation(request.Operation, input.Role) {
			return fmt.Errorf("%w: role=%s operation=%s", ErrVideoInputUnsupported, input.Role, request.Operation)
		}
		if !caps.SupportsInput(input.Role, input.MIMEType, input.Size) {
			return fmt.Errorf("%w: role=%s mime=%s", ErrVideoInputUnsupported, input.Role, input.MIMEType)
		}
		if input.Role == VideoInputRoleReferenceImage && !caps.Supports(VideoCapabilityInputReference) {
			return fmt.Errorf("%w: input_reference", ErrVideoCapabilityUnsupported)
		}
		if input.Role == VideoInputRoleReferenceImage {
			_, width, height, ok := parseVideoDimensions(request.Size)
			model := canonicalOpenAIVideoModel(firstNonEmptyString(request.RequestedModel, request.Model))
			if len(caps.SupportedSizes[model]) == 0 && strings.TrimSpace(request.Size) == "" {
				continue
			}
			if !ok || input.Width <= 0 || input.Height <= 0 || int64(input.Width) != width || int64(input.Height) != height {
				return fmt.Errorf("%w: input_reference dimensions must match size %q", ErrVideoInputUnsupported, request.Size)
			}
		}
	}
	return nil
}

// validateVideoArkCreateCapabilities 校验 Ark 维度：content[] 角色数量、分辨率、比例与智能时长。
// 未知模型（不在 SupportedModels）只校验角色是否被接受，取值交给上游裁决。
func validateVideoArkCreateCapabilities(caps VideoCapabilities, request VideoCreateRequest, knownModel bool) error {
	if normalizeVideoOperation(request.Operation) != VideoOperationGenerate {
		return nil
	}
	canonical := canonicalOpenAIVideoModel(firstNonEmptyString(request.RequestedModel, request.Model))
	counts := VideoContentRoleCounts(request.Content)
	// 角色上限只对声明了 MaxContentByRole 的 Ark 形态 provider 生效；OpenAI 兼容上游的
	// first_image_url / reference_* 由 openai_video_compatible_references.go 自己校验。
	if len(caps.MaxContentByRole) > 0 {
		for role, count := range counts {
			if !caps.SupportsContentRole(VideoInputRole(role), count) {
				return fmt.Errorf("%w: content role %q (%d item(s))", ErrVideoInputUnsupported, role, count)
			}
		}
	}
	if !knownModel {
		// 能力表不认识的模型（新发布的 Seedance、ep-* 接入点）：时长、画幅、智能时长与帧数
		// 都交给上游裁决；聚合平台与 Flova 在各自的 provider 校验里另行拒绝 -1 与 frames。
		return nil
	}
	if request.SmartDuration && !caps.SmartDurationModels[canonical] {
		return fmt.Errorf("%w: duration -1", ErrVideoCapabilityUnsupported)
	}
	if request.Frames > 0 && !caps.SmartDurationModels[canonical] {
		return fmt.Errorf("%w: frames", ErrVideoCapabilityUnsupported)
	}
	if resolution := strings.ToLower(strings.TrimSpace(request.Resolution)); resolution != "" {
		if allowed, ok := caps.SupportedResolutions[canonical]; ok && len(allowed) > 0 && !slices.Contains(allowed, resolution) {
			return fmt.Errorf("%w: resolution %q", ErrVideoCapabilityUnsupported, resolution)
		}
	}
	if ratio := strings.ToLower(strings.TrimSpace(request.Ratio)); ratio != "" {
		// adaptive 不是一个比例而是「跟随首帧」模式：能力表的比例词表只约束具体比例，
		// adaptive 只看有没有首帧图；各 provider 再按自己的方式落地（厂商不认 adaptive 时
		// 图生视频本来就不送比例）。
		if ratio == ArkVideoRatioAdaptive {
			if counts[string(VideoInputRoleFirstFrame)] == 0 && counts[string(VideoInputRoleReferenceImage)] == 0 {
				return ErrVideoAdaptiveRatioRequiresImage
			}
		} else if allowed, ok := caps.SupportedRatios[canonical]; ok && len(allowed) > 0 && !slices.Contains(allowed, ratio) {
			return fmt.Errorf("%w: ratio %q", ErrVideoCapabilityUnsupported, ratio)
		}
	}
	return nil
}

// ApplyVideoCapabilityDefaults makes provider defaults explicit before price
// resolution and request hashing. Derived operations keep their own semantics
// and therefore do not inherit create-only duration or size defaults.
func ApplyVideoCapabilityDefaults(caps VideoCapabilities, request VideoCreateRequest) VideoCreateRequest {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = strings.TrimSpace(caps.DefaultModel)
	}
	if strings.TrimSpace(request.RequestedModel) == "" {
		request.RequestedModel = request.Model
	}
	if normalizeVideoOperation(request.Operation) != VideoOperationGenerate {
		return request
	}
	model := canonicalOpenAIVideoModel(firstNonEmptyString(request.RequestedModel, request.Model))
	if request.Seconds <= 0 && !request.SmartDuration && request.Frames <= 0 {
		request.Seconds = caps.DefaultSeconds[model]
	}
	if len(caps.SupportedResolutions) > 0 || len(caps.SupportedRatios) > 0 {
		// Ark 维度的 provider：补分辨率默认档；比例只在没有首帧图时补（图生视频默认跟随
		// 图片，由 provider 决定是否送 adaptive）。
		if strings.TrimSpace(request.Resolution) == "" {
			request.Resolution = strings.TrimSpace(caps.DefaultResolutions[model])
		}
		if strings.TrimSpace(request.Ratio) == "" && len(request.ContentByRole(VideoInputRoleFirstFrame)) == 0 &&
			len(request.ContentByRole(VideoInputRoleReferenceImage)) == 0 {
			request.Ratio = strings.TrimSpace(caps.DefaultRatios[model])
		}
		return request
	}
	if strings.TrimSpace(request.Size) == "" {
		request.Size = strings.TrimSpace(caps.DefaultSizes[model])
	}
	return request
}

func canonicalOpenAIVideoModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, OpenAIVideoModelSora2Pro):
		return OpenAIVideoModelSora2Pro
	case strings.HasPrefix(model, OpenAIVideoModelSora2):
		return OpenAIVideoModelSora2
	default:
		return model
	}
}
