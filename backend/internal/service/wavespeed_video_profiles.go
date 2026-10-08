package service

// WaveSpeed Tier A 模型档案（方案 §5.4）。模型 ID 与字段来自各模型文档页；标注"待核对"
// 的条目在 P1 PoC 用 GET {base}/models 返回的 request_schema 校正。
//
// 尺寸编码：文生视频用 aspect_ratio + resolution（AR+Res）；图生视频比例随图，只传
// resolution；Kling 只认 aspect_ratio；Seedance 1.0 Pro 的分辨率固化在模型 ID 里。

const waveSpeedDefaultVideoModel = "bytedance/seedance-2.0/text-to-video"

var (
	waveSpeedAllAspects   = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9"}
	waveSpeedWideAspects  = []string{"16:9", "9:16"}
	waveSpeedKlingAspects = []string{"16:9", "9:16", "1:1"}
	waveSpeedRes3         = []string{"480p", "720p", "1080p"}
	waveSpeedRes4         = []string{"480p", "720p", "1080p", "4k"}
	waveSpeedResHD        = []string{"720p", "1080p"}
	waveSpeedResVeo       = []string{"720p", "1080p", "4k"}
)

type waveSpeedProfileSpec struct {
	seconds    []int
	minSeconds int
	maxSeconds int
	defSeconds int
	aspects    []string
	resolution []string
	defSize    string
	sizeEnc    videoSizeEncoding
	audio      string
	options    map[string]string
	image      string // "" = 不接受图片；"required" = 必须；"optional"
	lastImage  bool
	resLabels  map[string]string
}

func waveSpeedProfile(model string, spec waveSpeedProfileSpec) *videoModelProfile {
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
	return &videoModelProfile{
		PublicModel: model, Endpoint: model,
		RequiresImage: spec.image == "required", AllowsImage: spec.image != "", MaxImages: maxImages,
		Seconds: spec.seconds, MinSeconds: spec.minSeconds, MaxSeconds: spec.maxSeconds, DefaultSeconds: spec.defSeconds,
		DurationEnc: videoDurationInt, DurationField: "duration",
		Sizes: sizes, DefaultSize: defSize, SizeEnc: spec.sizeEnc, ResolutionLabels: spec.resLabels,
		AudioField: spec.audio, OptionWhitelist: options, OutputPath: "outputs.0",
		ImageField: "image", LastImageField: "last_image",
	}
}

func waveSpeedFallbackProfile(model string) *videoModelProfile {
	profile := waveSpeedProfile(model, waveSpeedProfileSpec{
		minSeconds: 1, maxSeconds: 30, defSeconds: 5, sizeEnc: videoSizeAspectRatioRes, defSize: "1280x720",
		audio: "generate_audio", image: "optional", lastImage: true,
		options: map[string]string{"seed": "seed", "negative_prompt": "negative_prompt"},
	})
	profile.Fallback = true
	return profile
}

func waveSpeedVideoProfiles() map[string]*videoModelProfile {
	seedOpt := map[string]string{"seed": "seed"}
	seedCamera := map[string]string{"seed": "seed", "camera_fixed": "camera_fixed"}
	webSearch := map[string]string{"enable_web_search": "enable_web_search"}
	klingOpt := map[string]string{"negative_prompt": "negative_prompt", "cfg_scale": "cfg_scale", "shot_type": "shot_type"}
	kling25Opt := map[string]string{"negative_prompt": "negative_prompt", "cfg_scale": "cfg_scale"}
	veoOpt := map[string]string{"negative_prompt": "negative_prompt", "seed": "seed"}

	// Seedance 1.0 Pro：分辨率固化在 ID；时长 5 / 10（待核对）。
	seedance10T2V := waveSpeedProfileSpec{seconds: []int{5, 10}, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: []string{"720p"}, sizeEnc: videoSizeResolutionInID, options: seedCamera}
	seedance10I2V := func(string) waveSpeedProfileSpec {
		// 分辨率固化在模型 ID 里，比例随图：定价档位从 ID 取，请求体不传尺寸字段。
		return waveSpeedProfileSpec{seconds: []int{5, 10}, defSeconds: 5, sizeEnc: videoSizeResolutionInID, options: seedCamera, image: "required", lastImage: true}
	}
	seedance10FastI2V := waveSpeedProfileSpec{minSeconds: 2, maxSeconds: 12, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes3, sizeEnc: videoSizeResolutionOnly, options: seedCamera, image: "required"}
	// Seedance 1.5 Pro
	seedance15T2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 12, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes3, sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: seedCamera}
	seedance15I2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 12, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes3, sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: seedCamera, image: "required", lastImage: true}
	// Seedance 2.0 / 2.0 Fast
	seedance20T2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 15, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes4, sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: webSearch}
	seedance20I2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 15, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes4, sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: webSearch, image: "required", lastImage: true}
	// Seedance 2.5
	seedance25T2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 30, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes4, sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio"}
	seedance25I2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 30, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes4, sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", image: "required", lastImage: true}
	// Kling 3.0 / Omni：音频字段叫 sound；图生视频比例随图。
	kling3T2V := waveSpeedProfileSpec{minSeconds: 3, maxSeconds: 15, defSeconds: 5, aspects: waveSpeedKlingAspects, resolution: []string{"1080p"}, sizeEnc: videoSizeAspectRatio, audio: "sound", options: klingOpt}
	kling3I2V := waveSpeedProfileSpec{minSeconds: 3, maxSeconds: 15, defSeconds: 5, sizeEnc: videoSizeNotSupported, audio: "sound", options: klingOpt, image: "required", lastImage: true}
	kling25T2V := waveSpeedProfileSpec{seconds: []int{5, 10}, defSeconds: 5, aspects: waveSpeedKlingAspects, resolution: []string{"1080p"}, sizeEnc: videoSizeAspectRatio, options: kling25Opt}
	// Veo 3.1
	veoT2V := waveSpeedProfileSpec{seconds: []int{4, 6, 8}, defSeconds: 8, aspects: waveSpeedWideAspects, resolution: waveSpeedResVeo, defSize: "1920x1080", sizeEnc: videoSizeAspectRatioRes, audio: "generate_audio", options: veoOpt}
	veoI2V := waveSpeedProfileSpec{seconds: []int{4, 6, 8}, defSeconds: 8, aspects: waveSpeedWideAspects, resolution: waveSpeedResVeo, defSize: "1920x1080", sizeEnc: videoSizeResolutionOnly, audio: "generate_audio", options: veoOpt, image: "required"}
	// Sora 2（字段待核对）
	soraT2V := waveSpeedProfileSpec{seconds: []int{4, 8, 12}, defSeconds: 8, aspects: waveSpeedWideAspects, resolution: waveSpeedResHD, sizeEnc: videoSizeAspectRatioRes}
	soraI2V := waveSpeedProfileSpec{seconds: []int{4, 8, 12}, defSeconds: 8, aspects: waveSpeedWideAspects, resolution: waveSpeedResHD, sizeEnc: videoSizeResolutionOnly, image: "required"}
	// MiniMax / Wan（字段待核对）
	minimaxH3I2V := waveSpeedProfileSpec{minSeconds: 4, maxSeconds: 10, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes3, sizeEnc: videoSizeResolutionOnly, image: "required",
		resLabels: map[string]string{"720p": "768p"}}
	hailuoI2V := waveSpeedProfileSpec{seconds: []int{6, 10}, defSeconds: 6, sizeEnc: videoSizeNotSupported, image: "required"}
	wanI2V := waveSpeedProfileSpec{seconds: []int{5, 10}, defSeconds: 5, aspects: waveSpeedAllAspects, resolution: waveSpeedRes3, sizeEnc: videoSizeResolutionOnly, image: "required", options: seedOpt}

	specs := map[string]waveSpeedProfileSpec{
		// ByteDance Seedance 全系
		"bytedance/seedance-v1-pro-t2v-720p":              seedance10T2V,
		"bytedance/seedance-v1-pro-i2v-480p":              seedance10I2V("480p"),
		"bytedance/seedance-v1-pro-i2v-720p":              seedance10I2V("720p"),
		"bytedance/seedance-v1-pro-i2v-1080p":             seedance10I2V("1080p"),
		"bytedance/seedance-v1-pro-fast/image-to-video":   seedance10FastI2V,
		"bytedance/seedance-v1.5-pro/text-to-video":       seedance15T2V,
		"bytedance/seedance-v1.5-pro/image-to-video":      seedance15I2V,
		"bytedance/seedance-v1.5-pro/image-to-video-fast": seedance15I2V,
		"bytedance/seedance-2.0/text-to-video":            seedance20T2V,
		"bytedance/seedance-2.0/image-to-video":           seedance20I2V,
		"bytedance/seedance-2.0-fast/text-to-video":       seedance20T2V,
		"bytedance/seedance-2.0-fast/image-to-video":      seedance20I2V,
		"bytedance/seedance-2.5/text-to-video":            seedance25T2V,
		"bytedance/seedance-2.5/image-to-video":           seedance25I2V,
		"bytedance/seedance-2.5/text-to-video-turbo":      seedance25T2V,
		// Kling
		"kwaivgi/kling-v3.0-pro/text-to-video":       kling3T2V,
		"kwaivgi/kling-v3.0-pro/image-to-video":      kling3I2V,
		"kwaivgi/kling-v3.0-std/text-to-video":       kling3T2V,
		"kwaivgi/kling-v3.0-std/image-to-video":      kling3I2V,
		"kwaivgi/kling-video-o3-std/text-to-video":   kling3T2V,
		"kwaivgi/kling-video-o3-pro/text-to-video":   kling3T2V,
		"kwaivgi/kling-v2.5-turbo-pro/text-to-video": kling25T2V,
		// Google Veo 3.1
		"google/veo3.1/text-to-video":      veoT2V,
		"google/veo3.1/image-to-video":     veoI2V,
		"google/veo3.1-fast/text-to-video": veoT2V,
		// OpenAI Sora 2
		"openai/sora-2/text-to-video":  soraT2V,
		"openai/sora-2/image-to-video": soraI2V,
		// MiniMax / Alibaba
		"wavespeed-ai/minimax-h3/image-to-video": minimaxH3I2V,
		"minimax/hailuo-2.3/i2v-pro":             hailuoI2V,
		"alibaba/wan-2.6/image-to-video":         wanI2V,
	}
	profiles := make(map[string]*videoModelProfile, len(specs))
	for model, spec := range specs {
		profiles[model] = waveSpeedProfile(model, spec)
	}
	return mergeVideoVendorProfiles(profiles, loadVideoVendorProfileDocument(VideoProviderWaveSpeed), videoVendorProfileDefaults{
		DurationEnc: videoDurationInt, SizeEnc: videoSizeAspectRatioRes, ImageField: "image", LastImageField: "last_image",
		OutputPath: "outputs.0", EndpointIsID: true,
	})
}
