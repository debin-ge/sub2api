package service

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/vendorprofile"
)

// Tier B 档案：由 cmd/tools/video-vendor-profiles 生成、嵌入二进制。文件缺失视为没有
// Tier B 条目，手写的 Tier A 档案始终优先。
//
//go:embed video_vendor_profiles/*.json
var videoVendorProfileFS embed.FS

const (
	videoVendorProfileMaxSeconds = 120
	videoVendorProfileFallbackSz = "1280x720"
)

// loadVideoVendorProfileDocument 读取嵌入的档案文件；不存在或损坏时返回空文档。
func loadVideoVendorProfileDocument(vendor string) vendorprofile.Document {
	raw, err := videoVendorProfileFS.ReadFile("video_vendor_profiles/" + vendor + ".json")
	if err != nil {
		return vendorprofile.Document{Vendor: vendor}
	}
	return decodeVideoVendorProfileDocument(raw, vendor)
}

func decodeVideoVendorProfileDocument(raw []byte, vendor string) vendorprofile.Document {
	var document vendorprofile.Document
	if err := json.Unmarshal(raw, &document); err != nil || !strings.EqualFold(document.Vendor, vendor) {
		return vendorprofile.Document{Vendor: vendor}
	}
	return document
}

// videoVendorProfileDefaults 是把文档条目变成档案时各厂商的默认值。
type videoVendorProfileDefaults struct {
	DurationEnc    videoDurationEncoding
	SizeEnc        videoSizeEncoding
	ImageField     string
	LastImageField string
	OutputPath     string
	EndpointIsID   bool
}

// mergeVideoVendorProfiles 把 Tier B 条目并入 Tier A 档案表：Tier A 已有的模型不覆盖。
func mergeVideoVendorProfiles(tierA map[string]*videoModelProfile, document vendorprofile.Document, defaults videoVendorProfileDefaults) map[string]*videoModelProfile {
	merged := make(map[string]*videoModelProfile, len(tierA)+len(document.Profiles))
	for model, profile := range tierA {
		merged[model] = profile
	}
	for _, entry := range document.Profiles {
		model := strings.ToLower(strings.TrimSpace(entry.Model))
		if model == "" {
			continue
		}
		if _, exists := merged[model]; exists {
			continue
		}
		if profile := videoVendorProfileFromEntry(entry, defaults); profile != nil {
			merged[model] = profile
		}
	}
	for _, profile := range merged {
		profile.finalizeArkDimensions()
	}
	return merged
}

func videoVendorProfileFromEntry(entry vendorprofile.Entry, defaults videoVendorProfileDefaults) *videoModelProfile {
	model := strings.ToLower(strings.TrimSpace(entry.Model))
	if model == "" {
		return nil
	}
	seconds := make([]int, 0, len(entry.Seconds))
	for _, value := range entry.Seconds {
		if value >= 1 && value <= videoVendorProfileMaxSeconds {
			seconds = append(seconds, value)
		}
	}
	sort.Ints(seconds)
	minSeconds, maxSeconds := entry.MinSeconds, entry.MaxSeconds
	if len(seconds) == 0 {
		if minSeconds <= 0 {
			minSeconds = 1
		}
		if maxSeconds > videoVendorProfileMaxSeconds {
			maxSeconds = videoVendorProfileMaxSeconds
		}
		if maxSeconds < minSeconds {
			maxSeconds = minSeconds
		}
	} else {
		minSeconds, maxSeconds = 0, 0
	}
	defaultSeconds := entry.DefaultSeconds
	if defaultSeconds <= 0 {
		if len(seconds) > 0 {
			defaultSeconds = seconds[0]
		} else {
			defaultSeconds = minSeconds
		}
	}
	sizes := make([]string, 0, len(entry.Sizes))
	for _, size := range entry.Sizes {
		if normalized, _, _, ok := parseVideoDimensions(size); ok {
			sizes = append(sizes, normalized)
		}
	}
	if len(sizes) == 0 && len(entry.Aspects) > 0 && len(entry.Resolutions) > 0 {
		sizes = videoVendorSizesFor(entry.Aspects, entry.Resolutions)
	}
	defaultSize := strings.TrimSpace(entry.DefaultSize)
	if defaultSize == "" {
		if len(sizes) > 0 {
			defaultSize = sizes[0]
		} else {
			defaultSize = videoVendorProfileFallbackSz
		}
	}
	durationEnc := videoDurationEncoding(strings.TrimSpace(entry.DurationEnc))
	if durationEnc == "" {
		durationEnc = defaults.DurationEnc
	}
	sizeEnc := videoSizeEncoding(strings.TrimSpace(entry.SizeEnc))
	if sizeEnc == "" {
		sizeEnc = defaults.SizeEnc
	}
	maxImages := entry.MaxImages
	if entry.AllowsImage && maxImages <= 0 {
		maxImages = 1
	}
	options := make(map[string]string, len(entry.Options))
	for key, path := range entry.Options {
		if key = strings.TrimSpace(key); key != "" && strings.TrimSpace(path) != "" {
			options[key] = strings.TrimSpace(path)
		}
	}
	labels := make(map[string]string, len(entry.ResolutionLabels))
	for generic, vendorLabel := range entry.ResolutionLabels {
		if generic = strings.ToLower(strings.TrimSpace(generic)); generic != "" && strings.TrimSpace(vendorLabel) != "" {
			labels[generic] = strings.TrimSpace(vendorLabel)
		}
	}
	if len(labels) == 0 {
		labels = nil
	}
	resolutions := make([]string, 0, len(entry.Resolutions))
	for _, resolution := range entry.Resolutions {
		if resolution = strings.ToLower(strings.TrimSpace(resolution)); resolution != "" {
			resolutions = append(resolutions, resolution)
		}
	}
	endpoint := strings.TrimSpace(entry.Endpoint)
	if endpoint == "" && defaults.EndpointIsID {
		endpoint = model
	}
	imageField := strings.TrimSpace(entry.ImageField)
	if imageField == "" {
		imageField = defaults.ImageField
	}
	lastImageField := strings.TrimSpace(entry.LastImageField)
	if lastImageField == "" {
		lastImageField = defaults.LastImageField
	}
	ratios := make([]string, 0, len(entry.Aspects))
	for _, aspect := range entry.Aspects {
		if aspect = strings.TrimSpace(aspect); IsArkVideoRatio(aspect) && aspect != ArkVideoRatioAdaptive {
			ratios = append(ratios, aspect)
		}
	}
	return &videoModelProfile{
		PublicModel: model, Endpoint: endpoint,
		RequiresImage: entry.AllowsImage && entry.RequiresImage, AllowsImage: entry.AllowsImage, MaxImages: maxImages,
		Seconds: seconds, MinSeconds: minSeconds, MaxSeconds: maxSeconds, DefaultSeconds: defaultSeconds,
		DurationEnc: durationEnc, DurationField: "duration",
		Sizes: sizes, DefaultSize: defaultSize, SizeEnc: sizeEnc, Ratios: ratios,
		ImageResolutionField: strings.TrimSpace(entry.ImageResolutionField),
		Resolutions:          resolutions, ResolutionLabels: labels,
		AudioField: strings.TrimSpace(entry.AudioField), OptionWhitelist: options, OutputPath: defaults.OutputPath,
		ImageField: imageField, LastImageField: lastImageField,
	}
}
