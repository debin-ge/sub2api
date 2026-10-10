package service

// fal Tier A 模型档案（方案 §5.4）。端点 ID 与字段来自各模型 /api 页；fal 没有公开目录
// 接口，标注"待核对"的条目在 P2 PoC 按模型页校正。
//
// 尺寸编码：文生视频用 aspect_ratio + resolution（Seedance / Veo / Sora）或只传比例
// （Kling）；图生视频比例随图，只传 resolution。时长除 Sora 外都是字符串整数，
// Veo 带 "s" 后缀。

const falDefaultVideoModel = "bytedance/seedance-2.0/text-to-video"

var (
	falAllAspects   = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9"}
	falWideAspects  = []string{"16:9", "9:16"}
	falKlingAspects = []string{"16:9", "9:16", "1:1"}
	falRes3         = []string{"480p", "720p", "1080p"}
	falRes2         = []string{"480p", "720p"}
	falResHD        = []string{"720p", "1080p"}
	falResVeo       = []string{"720p", "1080p", "4k"}
)

type falProfileSpec struct {
	endpoint   string
	seconds    []int
	minSeconds int
	maxSeconds int
	defSeconds int
	duration   videoDurationEncoding
	aspects    []string
	resolution []string
	defSize    string
	sizeEnc    videoSizeEncoding
	audio      string
	options    map[string]string
	image      string // "" / "required" / "optional"
	imageField string
	lastImage  bool
}

func falProfile(model string, spec falProfileSpec) *videoModelProfile {
	options := map[string]string{}
	for key, path := range spec.options {
		options[key] = path
	}
	sizes := videoVendorSizesFor(spec.aspects, spec.resolution)
	defSize := spec.defSize
	if defSize == "" && len(sizes) > 0 {
		defSize = sizes[0]
	}
	maxImages := 0
	if spec.image != "" {
		maxImages = 1
		if spec.lastImage {
			maxImages = 2
		}
	}
	duration := spec.duration
	if duration == "" {
		duration = videoDurationString
	}
	imageField := spec.imageField
	if imageField == "" {
		imageField = "image_url"
	}
	endpoint := spec.endpoint
	if endpoint == "" {
		endpoint = model
	}
	return &videoModelProfile{
		PublicModel: model, Endpoint: endpoint,
		RequiresImage: spec.image == "required", AllowsImage: spec.image != "", MaxImages: maxImages,
		Seconds: spec.seconds, MinSeconds: spec.minSeconds, MaxSeconds: spec.maxSeconds, DefaultSeconds: spec.defSeconds,
		DurationEnc: duration, DurationField: "duration",
		Sizes: sizes, DefaultSize: defSize, SizeEnc: spec.sizeEnc, Ratios: append([]string(nil), spec.aspects...), CanonicalResolutions: append([]string(nil), spec.resolution...),
		AudioField: spec.audio, OptionWhitelist: options, OutputPath: "video.url",
		ImageField: imageField, LastImageField: "end_image_url",
	}
}

func falFallbackProfile(model string) *videoModelProfile {
	profile := falProfile(model, falProfileSpec{
		minSeconds: 1, maxSeconds: 20, defSeconds: 5, duration: videoDurationString,
		sizeEnc: videoSizeAspectRatioRes, defSize: "1280x720", audio: "generate_audio",
		image: "optional", lastImage: true,
		options: map[string]string{"seed": "seed", "negative_prompt": "negative_prompt"},
	})
	profile.Fallback = true
	return profile.finalizeArkDimensions()
}

func falVideoProfiles() map[string]*videoModelProfile {
	seedCamera := map[string]string{"seed": "seed", "camera_fixed": "camera_fixed"}
	seedOnly := map[string]string{"seed": "seed"}
	klingOpt := map[string]string{"negative_prompt": "negative_prompt", "cfg_scale": "cfg_scale", "shot_type": "shot_type"}
	veoOpt := map[string]string{"negative_prompt": "negative_prompt", "seed": "seed"}

	// Seedance 1.0 Lite / Pro / Pro Fast：时长字符串 2 到 12；Lite 默认 720p，Pro 默认 1080p。
	seedance10T2V := func(defSize string) falProfileSpec {
		return falProfileSpec{minSeconds: 2, maxSeconds: 12, defSeconds: 5, aspects: falAllAspects, resolution: falRes3, defSize: defSize, sizeEnc: videoSizeAspectRatioRes, options: seedCamera}
	}
	seedance10I2V := func(defSize string) falProfileSpec {
		return falProfileSpec{minSeconds: 2, maxSeconds: 12, defSeconds: 5, aspects: falAllAspects, resolution: falRes3, defSize: defSize, sizeEnc: videoSizeResolutionOnly, options: seedCamera, image: "required", lastImage: true}
	}
	// Seedance 1.5 Pro：时长字符串 4 到 12，有音频。
	seedance15T2V := falProfileSpec{minSeconds: 4, maxSeconds: 12, defSeconds: 5, aspects: falAllAspects, resolution: falRes3, defSize: "1280x720", sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: seedCamera}
	seedance15I2V := falProfileSpec{minSeconds: 4, maxSeconds: 12, defSeconds: 5, aspects: falAllAspects, resolution: falRes3, defSize: "1280x720", sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: seedCamera, image: "required", lastImage: true}
	// Seedance 2.0 / 2.0 Fast：时长字符串 4 到 15，仅 480p / 720p。
	seedance20T2V := falProfileSpec{minSeconds: 4, maxSeconds: 15, defSeconds: 5, aspects: falAllAspects, resolution: falRes2, defSize: "1280x720", sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: seedOnly}
	seedance20I2V := falProfileSpec{minSeconds: 4, maxSeconds: 15, defSeconds: 5, aspects: falAllAspects, resolution: falRes2, defSize: "1280x720", sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: seedOnly, image: "required", lastImage: true}
	// Kling v3：时长字符串 3 到 15；文生视频只传比例，图生视频用 start_image_url。
	kling3T2V := falProfileSpec{minSeconds: 3, maxSeconds: 15, defSeconds: 5, aspects: falKlingAspects, resolution: []string{"1080p"}, sizeEnc: videoSizeAspectRatio, audio: "generate_audio", options: klingOpt}
	kling3I2V := falProfileSpec{minSeconds: 3, maxSeconds: 15, defSeconds: 5, sizeEnc: videoSizeNotSupported, audio: "generate_audio", options: klingOpt, image: "required", imageField: "start_image_url", lastImage: true}
	// Veo 3.1：时长 "4s" / "6s" / "8s"。
	veoT2V := falProfileSpec{seconds: []int{4, 6, 8}, defSeconds: 8, duration: videoDurationStringS, aspects: falWideAspects, resolution: falResVeo, defSize: "1280x720", sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: veoOpt}
	veoI2V := falProfileSpec{seconds: []int{4, 6, 8}, defSeconds: 8, duration: videoDurationStringS, aspects: falWideAspects, resolution: falResVeo, defSize: "1280x720", sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: veoOpt, image: "required"}
	// Sora 2：时长整数 4 / 8 / 12 / 16 / 20；图生视频字段待核对。
	soraT2V := falProfileSpec{seconds: []int{4, 8, 12, 16, 20}, defSeconds: 4, duration: videoDurationInt, aspects: falWideAspects, resolution: falResHD, defSize: "1280x720", sizeEnc: videoSizeAspectRatioRes}
	soraI2V := falProfileSpec{seconds: []int{4, 8, 12, 16, 20}, defSeconds: 4, duration: videoDurationInt, aspects: falWideAspects, resolution: falResHD, defSize: "1280x720", sizeEnc: videoSizeResolutionOnly, image: "required"}

	specs := map[string]falProfileSpec{
		// ByteDance Seedance 全系
		"fal-ai/bytedance/seedance/v1/lite/text-to-video":      seedance10T2V("1280x720"),
		"fal-ai/bytedance/seedance/v1/lite/image-to-video":     seedance10I2V("1280x720"),
		"fal-ai/bytedance/seedance/v1/pro/text-to-video":       seedance10T2V("1920x1080"),
		"fal-ai/bytedance/seedance/v1/pro/image-to-video":      seedance10I2V("1920x1080"),
		"fal-ai/bytedance/seedance/v1/pro/fast/text-to-video":  seedance10T2V("1920x1080"),
		"fal-ai/bytedance/seedance/v1/pro/fast/image-to-video": seedance10I2V("1920x1080"),
		"fal-ai/bytedance/seedance/v1.5/pro/text-to-video":     seedance15T2V,
		"fal-ai/bytedance/seedance/v1.5/pro/image-to-video":    seedance15I2V,
		"bytedance/seedance-2.0/text-to-video":                 seedance20T2V,
		"bytedance/seedance-2.0/image-to-video":                seedance20I2V,
		"bytedance/seedance-2.0/fast/text-to-video":            seedance20T2V,
		"bytedance/seedance-2.0/fast/image-to-video":           seedance20I2V,
		// Kling v3
		"fal-ai/kling-video/v3/standard/text-to-video":  kling3T2V,
		"fal-ai/kling-video/v3/standard/image-to-video": kling3I2V,
		"fal-ai/kling-video/v3/pro/text-to-video":       kling3T2V,
		"fal-ai/kling-video/v3/pro/image-to-video":      kling3I2V,
		// Google Veo 3.1
		"fal-ai/veo3.1":                veoT2V,
		"fal-ai/veo3.1/image-to-video": veoI2V,
		"fal-ai/veo3.1/fast":           veoT2V,
		// OpenAI Sora 2
		"fal-ai/sora-2/text-to-video":     soraT2V,
		"fal-ai/sora-2/text-to-video/pro": soraT2V,
		"fal-ai/sora-2/image-to-video":    soraI2V,
	}
	profiles := make(map[string]*videoModelProfile, len(specs))
	for model, spec := range specs {
		profiles[model] = falProfile(model, spec)
	}
	return mergeVideoVendorProfiles(profiles, loadVideoVendorProfileDocument(VideoProviderFal), videoVendorProfileDefaults{
		DurationEnc: videoDurationString, SizeEnc: videoSizeAspectRatioRes, ImageField: "image_url", LastImageField: "end_image_url",
		OutputPath: "video.url", EndpointIsID: true,
	})
}
