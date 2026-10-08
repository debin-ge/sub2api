package service

// Runware Tier A 模型档案（方案 §5.4）。尺寸表与秒数来自各模型 schema 页；标注"待核对"
// 的条目在 P0 PoC 用 https://runware.ai/docs/models/<id>/schema.json 校正。
//
// 尺寸为空的档案本地不校验尺寸，原样透传 width/height 给上游，由上游的封闭尺寸表裁决。

const runwareDefaultVideoModel = "bytedance:seedance@2.0"

var (
	// Seedance 2.0 / 2.0 Fast / 2.0 Mini（480p / 720p / 1080p / 4K 各 6 组）。
	runwareSeedance20Sizes = []string{
		"864x496", "752x560", "640x640", "560x752", "496x864", "992x432",
		"1280x720", "1112x834", "960x960", "834x1112", "720x1280", "1470x630",
		"1920x1080", "1664x1248", "1440x1440", "1248x1664", "1080x1920", "2206x946",
		"3840x2160", "3326x2494", "2880x2880", "2496x3328", "2160x3840", "4398x1886",
	}
	// Seedance 2.5（480p / 720p / 1080p 各 6 组）。
	runwareSeedance25Sizes = []string{
		"854x480", "752x560", "640x640", "560x752", "480x854", "992x432",
		"1280x720", "1112x834", "960x960", "834x1112", "720x1280", "1470x630",
		"1920x1080", "1664x1248", "1440x1440", "1248x1664", "1080x1920", "2206x946",
	}
	// Seedance 1.0 Pro / Pro Fast（480p 与 1080p 各 7 组）。
	runwareSeedance10ProSizes = []string{
		"864x480", "736x544", "640x640", "544x736", "480x864", "416x960", "960x416",
		"1920x1088", "1664x1248", "1440x1440", "1248x1664", "1088x1920", "928x2176", "2176x928",
	}
	// Kling 3.0 Pro 的三组 1080p 尺寸；Standard / Turbo 沿用，待核对。
	runwareKling1080Sizes = []string{"1920x1080", "1440x1440", "1080x1920"}

	runwareCommonOptions = map[string]string{
		"negativePrompt": "negativePrompt",
		"seed":           "seed",
	}
)

type runwareProfileSpec struct {
	seconds      []int
	minSeconds   int
	maxSeconds   int
	defSeconds   int
	sizes        []string
	defSize      string
	audio        string
	options      map[string]string
	noImage      bool
	maxImages    int
	imageRes     bool
	floatSeconds bool
}

func runwareProfile(model string, spec runwareProfileSpec) *videoModelProfile {
	options := make(map[string]string, len(runwareCommonOptions)+len(spec.options))
	for key, path := range runwareCommonOptions {
		options[key] = path
	}
	for key, path := range spec.options {
		options[key] = path
	}
	maxImages := spec.maxImages
	if maxImages == 0 && !spec.noImage {
		maxImages = 2
	}
	resolutionField := ""
	if spec.imageRes {
		resolutionField = "resolution"
	}
	encoding := videoDurationInt
	if spec.floatSeconds {
		encoding = videoDurationFloatSec
	}
	return &videoModelProfile{
		PublicModel: model, Endpoint: "",
		AllowsImage: !spec.noImage, MaxImages: maxImages,
		Seconds: spec.seconds, MinSeconds: spec.minSeconds, MaxSeconds: spec.maxSeconds, DefaultSeconds: spec.defSeconds,
		DurationEnc: encoding, DurationField: "duration",
		Sizes: spec.sizes, DefaultSize: spec.defSize, SizeEnc: videoSizeWidthHeight, ImageResolutionField: resolutionField,
		AudioField: spec.audio, OptionWhitelist: options, OutputPath: "videoURL",
	}
}

func runwareFallbackProfile(model string) *videoModelProfile {
	profile := runwareProfile(model, runwareProfileSpec{
		minSeconds: 1, maxSeconds: 30, defSeconds: 5, defSize: "1280x720", audio: "settings.audio",
	})
	profile.Fallback = true
	return profile
}

func runwareVideoProfiles() map[string]*videoModelProfile {
	seedance10 := runwareProfileSpec{
		minSeconds: 2, maxSeconds: 12, defSeconds: 5, sizes: runwareSeedance10ProSizes, defSize: "1920x1088",
		imageRes: true, options: map[string]string{"cameraFixed": "providerSettings.bytedance.cameraFixed"},
	}
	seedance20 := runwareProfileSpec{
		minSeconds: 4, maxSeconds: 15, defSeconds: 5, sizes: runwareSeedance20Sizes, defSize: "1280x720",
		audio: "settings.audio", imageRes: true,
	}
	seedance25 := runwareProfileSpec{
		minSeconds: 4, maxSeconds: 30, defSeconds: 5, sizes: runwareSeedance25Sizes, defSize: "1280x720",
		audio: "settings.audio", imageRes: true,
	}
	kling3 := runwareProfileSpec{
		minSeconds: 3, maxSeconds: 15, defSeconds: 5, sizes: runwareKling1080Sizes, defSize: "1920x1080",
		audio: "providerSettings.klingai.sound",
	}
	specs := map[string]runwareProfileSpec{
		// ByteDance Seedance 全系
		"bytedance:2@1":               seedance10,
		"bytedance:2@2":               seedance10,
		"bytedance:seedance@2.0":      seedance20,
		"bytedance:seedance@2.0-fast": seedance20,
		"bytedance:seedance@2.0-mini": seedance20,
		"bytedance:seedance@2.5":      seedance25,
		// Kling 3 Pro（1080p 三组尺寸、sound 已核实）；其余 Kling / Veo / Wan / MiniMax 由
		// 生成的 Tier B 档案（video_vendor_profiles/runware.json）提供。
		"klingai:kling-video@3-pro": kling3,
	}
	profiles := make(map[string]*videoModelProfile, len(specs))
	for model, spec := range specs {
		profiles[model] = runwareProfile(model, spec)
	}
	return mergeVideoVendorProfiles(profiles, loadVideoVendorProfileDocument(VideoProviderRunware), videoVendorProfileDefaults{
		DurationEnc: videoDurationInt, SizeEnc: videoSizeWidthHeight, OutputPath: "videoURL",
	})
}
