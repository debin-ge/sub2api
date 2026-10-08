package service

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 聚合媒体平台（fal / WaveSpeed / Runware）共用一个通用视频 provider 骨架
// （video_vendor_provider.go），每家只实现本文件定义的 videoVendorAdapter：请求体编码、
// 响应解码、状态词表、任务 ID 编码。骨架负责账号判定、认证头、客户端 token 放置、
// HTTP 执行、错误分类、内容代理与元数据写入。

// videoClientTokenMode 描述本地 StableClientToken 在上游请求里的落点。
type videoClientTokenMode struct {
	// Kind 取 "none"（厂商无幂等键）、"header"（请求头，如 Idempotency-Key）、
	// "body"（请求体字段，如 Runware 的 taskUUID，由 adapter 自己写入）。
	Kind string
	Name string
}

const (
	videoClientTokenNone   = "none"
	videoClientTokenHeader = "header"
	videoClientTokenBody   = "body"
)

// vendorHTTPRequest 是 adapter 构造的上游请求描述；Path 相对账号 base_url。
type vendorHTTPRequest struct {
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	ContentType string
	Headers     http.Header
}

// vendorHTTPResponse 是骨架读完响应体后交给 adapter 解码的快照。
type vendorHTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// videoVendorInput 是骨架从加密 spool 读出的参考素材；Data 已受 MaxContentModerationImageBytes 约束。
type videoVendorInput struct {
	Role     VideoInputRole
	MIMEType string
	Data     []byte
	// URL 非空时素材由客户端以 URL 形式提交（input_reference.image_url 等），Data 为空。
	URL string
}

func (i videoVendorInput) dataURI() string {
	if i.URL != "" {
		return i.URL
	}
	mimeType := strings.TrimSpace(i.MIMEType)
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(i.Data)
}

// videoDurationEncoding 描述公开 seconds 如何落到厂商字段。
type videoDurationEncoding string

const (
	videoDurationInt      videoDurationEncoding = "int"        // 5
	videoDurationString   videoDurationEncoding = "string_int" // "5"
	videoDurationStringS  videoDurationEncoding = "string_s"   // "8s"
	videoDurationFloatSec videoDurationEncoding = "float"      // 5.0
)

// videoSizeEncoding 描述公开 size（WxH）如何落到厂商字段。
type videoSizeEncoding string

const (
	videoSizeWidthHeight    videoSizeEncoding = "width_height"            // width / height 两个整数字段
	videoSizeAspectRatio    videoSizeEncoding = "aspect_ratio"            // 只传比例
	videoSizeAspectRatioRes videoSizeEncoding = "aspect_ratio_resolution" // 比例 + 分辨率档位
	videoSizeResolutionOnly videoSizeEncoding = "resolution_only"         // 只传分辨率档位
	videoSizeResolutionInID videoSizeEncoding = "resolution_in_id"        // 分辨率固化在模型 ID 里
	videoSizeNotSupported   videoSizeEncoding = "none"                    // 尺寸由素材决定
)

// videoModelProfile 是一个厂商模型的请求档案。Tier A 由各 adapter 手写；Tier B 由
// 厂商 schema 生成；每家另有一个保守的兜底档案（Fallback），运营给模型配价后即可用。
type videoModelProfile struct {
	PublicModel string
	// Endpoint 是提交路径：fal / WaveSpeed 等于模型 ID；Runware 固定为 ""（模型进 body）。
	Endpoint string
	// Fallback 标记兜底档案：不校验尺寸与秒数集合，只做类型与白名单检查。
	Fallback bool

	RequiresImage bool
	AllowsImage   bool
	// MaxImages 是起止帧图片的上限（1 = 仅首帧，2 = 首帧 + 尾帧）。
	MaxImages int

	// Seconds 为空时用 [MinSeconds, MaxSeconds] 连续区间。
	Seconds        []int
	MinSeconds     int
	MaxSeconds     int
	DefaultSeconds int
	DurationEnc    videoDurationEncoding
	DurationField  string

	// Sizes 为空表示本地不校验尺寸（原样透传给上游）。
	Sizes       []string
	DefaultSize string
	SizeEnc     videoSizeEncoding
	// ImageResolutionField 非空时，带图请求不传宽高而传分辨率档位（Runware 的 resolution）。
	ImageResolutionField string

	// AudioField 是音频开关的 JSON 路径（点号分层，如 "settings.audio"）；空表示不支持。
	AudioField string

	// ImageField / LastImageField 是首帧 / 尾帧图片 URL 的 JSON 路径（fal / WaveSpeed 等
	// 以字段承载图片的厂商使用；Runware 用 inputs.frameImages 自行编码）。
	ImageField     string
	LastImageField string
	// AspectRatioField / ResolutionField 是比例与分辨率档位的 JSON 路径，空用厂商默认名。
	AspectRatioField string
	ResolutionField  string
	// ResolutionLabels 把通用档位名改写成厂商词表（MiniMax H3 把 720p 叫 768p）。
	ResolutionLabels map[string]string
	// Resolutions 是厂商接受的分辨率档位（已按 ResolutionLabels 改写）；为空表示不限制。
	// 带图请求的分辨率只在词表允许时才发送，避免把上游不认的档位名送过去换一个 400。
	Resolutions []string

	// OptionWhitelist 把 provider_options 的键映射到请求体 JSON 路径；不在表里的键被拒绝。
	OptionWhitelist map[string]string

	// OutputPath 是结果里视频 URL 的 JSON 路径（点号分层，数组下标用数字）。
	OutputPath string

	// Static 是每次提交都附带的厂商固定字段。
	Static map[string]any
}

func (p *videoModelProfile) supportsSeconds(seconds int) bool {
	if p == nil || p.Fallback {
		return seconds > 0
	}
	if len(p.Seconds) > 0 {
		for _, allowed := range p.Seconds {
			if allowed == seconds {
				return true
			}
		}
		return false
	}
	if p.MinSeconds > 0 || p.MaxSeconds > 0 {
		return seconds >= p.MinSeconds && seconds <= p.MaxSeconds
	}
	return seconds > 0
}

func (p *videoModelProfile) supportedSecondsList() []int {
	if p == nil {
		return nil
	}
	if len(p.Seconds) > 0 {
		return append([]int(nil), p.Seconds...)
	}
	if p.MinSeconds > 0 && p.MaxSeconds >= p.MinSeconds {
		values := make([]int, 0, p.MaxSeconds-p.MinSeconds+1)
		for s := p.MinSeconds; s <= p.MaxSeconds; s++ {
			values = append(values, s)
		}
		return values
	}
	return nil
}

func (p *videoModelProfile) supportsSize(size string) bool {
	if p == nil || p.Fallback || len(p.Sizes) == 0 {
		return true
	}
	normalized, _, _, valid := parseVideoDimensions(size)
	if !valid {
		return false
	}
	for _, allowed := range p.Sizes {
		if candidate, _, _, ok := parseVideoDimensions(allowed); ok && candidate == normalized {
			return true
		}
	}
	return false
}

// videoVendorResolutionLabel 把尺寸归到分辨率档位。三家的尺寸表都按 480p / 720p /
// 1080p / 4k 分档，同一档内各比例的像素总量几乎相同（720p：1280x720、960x960、
// 1470x630 都约 92 万像素），而短边 / 长边在 21:9 下会跨档，所以按面积判档。
func videoVendorResolutionLabel(size string) string {
	_, width, height, ok := parseVideoDimensions(size)
	if !ok || width <= 0 || height <= 0 {
		return ""
	}
	area := width * height
	switch {
	case area >= 4_000_000:
		return "4k"
	case area >= 1_400_000:
		return "1080p"
	case area >= 650_000:
		return "720p"
	case area >= 250_000:
		return "480p"
	default:
		short := width
		if height < short {
			short = height
		}
		return strconv.FormatInt(short, 10) + "p"
	}
}

// videoVendorCanonicalSizes 是各比例在各档位下的代表尺寸，取自 Seedance 2.0 的尺寸表
// （三家的 Seedance 页面一致）。按"比例 + 档位"表达尺寸的厂商用它生成目录里的尺寸列表。
var videoVendorCanonicalSizes = map[string]map[string]string{
	"480p":  {"21:9": "992x432", "16:9": "864x496", "4:3": "752x560", "1:1": "640x640", "3:4": "560x752", "9:16": "496x864"},
	"720p":  {"21:9": "1470x630", "16:9": "1280x720", "4:3": "1112x834", "1:1": "960x960", "3:4": "834x1112", "9:16": "720x1280"},
	"1080p": {"21:9": "2206x946", "16:9": "1920x1080", "4:3": "1664x1248", "1:1": "1440x1440", "3:4": "1248x1664", "9:16": "1080x1920"},
	"4k":    {"21:9": "4398x1886", "16:9": "3840x2160", "4:3": "3326x2494", "1:1": "2880x2880", "3:4": "2496x3328", "9:16": "2160x3840"},
}

var videoVendorResolutionOrder = []string{"480p", "720p", "1080p", "4k"}

// videoVendorSizesFor 按比例 × 档位展开代表尺寸，顺序固定（档位升序、比例按传入顺序）。
func videoVendorSizesFor(aspects, resolutions []string) []string {
	sizes := make([]string, 0, len(aspects)*len(resolutions))
	for _, resolution := range videoVendorResolutionOrder {
		matched := false
		for _, wanted := range resolutions {
			if strings.EqualFold(strings.TrimSpace(wanted), resolution) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, aspect := range aspects {
			if size := videoVendorCanonicalSizes[resolution][strings.TrimSpace(aspect)]; size != "" {
				sizes = append(sizes, size)
			}
		}
	}
	return sizes
}

// videoVendorResolutionFor 返回厂商词表下的分辨率档位；档位不在厂商词表里时返回空串。
func videoVendorResolutionFor(profile *videoModelProfile, size string) string {
	label := videoVendorResolutionLabel(size)
	if label == "" {
		return ""
	}
	if profile != nil && profile.ResolutionLabels != nil {
		if mapped, ok := profile.ResolutionLabels[label]; ok {
			label = mapped
		}
	}
	if profile != nil && len(profile.Resolutions) > 0 {
		for _, allowed := range profile.Resolutions {
			if strings.EqualFold(strings.TrimSpace(allowed), label) {
				return label
			}
		}
		return ""
	}
	return label
}

// videoVendorAspectRatioLabel 把尺寸归到厂商常用的比例词表。
func videoVendorAspectRatioLabel(size string) string {
	_, width, height, ok := parseVideoDimensions(size)
	if !ok || width <= 0 || height <= 0 {
		return ""
	}
	ratio := float64(width) / float64(height)
	candidates := []struct {
		label string
		value float64
	}{
		{"21:9", 21.0 / 9}, {"16:9", 16.0 / 9}, {"4:3", 4.0 / 3}, {"3:2", 1.5}, {"1:1", 1},
		{"2:3", 2.0 / 3}, {"3:4", 0.75}, {"9:16", 9.0 / 16}, {"9:21", 9.0 / 21},
	}
	best := ""
	bestDiff := 1e9
	for _, candidate := range candidates {
		diff := ratio - candidate.value
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = candidate.label, diff
		}
	}
	return best
}

// videoVendorRequestResolution 供 providerRequestResolution 使用：没有尺寸时返回空串，
// 定价规则就只能按模型匹配。
func videoVendorRequestResolution(adapter videoVendorAdapter, request VideoCreateRequest) string {
	if adapter == nil {
		return ""
	}
	if profile := adapter.Profiles()[strings.ToLower(strings.TrimSpace(request.Model))]; profile != nil && profile.SizeEnc == videoSizeResolutionInID {
		return videoVendorResolutionFromModelID(request.Model)
	}
	if label := videoVendorResolutionLabel(request.Size); label != "" {
		return label
	}
	// 没有尺寸（图生视频比例随图）时退回模型 ID 里的档位，没有就留空让定价按模型匹配。
	return videoVendorResolutionFromModelID(request.Model)
}

// videoVendorResolutionFromModelID 从模型 ID 里抽分辨率档位（WaveSpeed 的 seedance-v1-pro-i2v-720p）。
func videoVendorResolutionFromModelID(model string) string {
	lower := strings.ToLower(model)
	for _, label := range []string{"1080p", "720p", "480p", "4k"} {
		if strings.Contains(lower, label) {
			return label
		}
	}
	return ""
}

// VideoModelCatalogContributor 让 provider 自己描述 /v1/videos/models 里的参数与参考
// 输入槽位；模型目录对实现了它的 provider 不再按名字写 switch。
type VideoModelCatalogContributor interface {
	ModelParameters(model string, profile *VideoPricingConfig) []VideoModelParameter
	ModelReferenceInputs(model string) []VideoModelReferenceInput
}

// videoVendorAdapter 是每家厂商要实现的最小接口。
type videoVendorAdapter interface {
	Name() string
	DefaultBaseURL() string
	AuthorizationHeader(apiKey string) string
	ClientTokenMode() videoClientTokenMode
	// ClientTokenFor 把本地 StableClientToken 变成厂商接受的形式（Runware 要 UUID v4）。
	ClientTokenFor(localToken string) string
	DefaultCapabilities() VideoCapabilities
	Profiles() map[string]*videoModelProfile
	FallbackProfile(model string) *videoModelProfile
	FreeSecondsRange() (int, int)
	DefaultPollInterval() time.Duration
	ContentTTL() time.Duration

	BuildCreate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput, clientToken string) (*vendorHTTPRequest, error)
	DecodeCreate(response *vendorHTTPResponse, request VideoCreateRequest, clientToken string) (*ProviderVideoTask, error)

	BuildGet(providerTaskID string) (*vendorHTTPRequest, error)
	// DecodeGet 解码一次轮询响应。next 非空时骨架继续请求并把响应连同 step+1 再交回来
	// （fal 的 status → result 两步）。
	DecodeGet(response *vendorHTTPResponse, providerTaskID string, step int) (task *ProviderVideoTask, next *vendorHTTPRequest, err error)

	BuildCancel(providerTaskID string) (*vendorHTTPRequest, bool)
	BuildDelete(providerTaskID string) (*vendorHTTPRequest, bool)
	BuildSearch(clientToken string) (*vendorHTTPRequest, bool)
	DecodeSearch(response *vendorHTTPResponse, clientToken string) (*ProviderVideoTask, error)
	// BuildProbe 构造账号测试连接用的只读请求；ok=false 表示厂商没有可用的探测端点。
	BuildProbe() (*vendorHTTPRequest, bool)

	// ClassifyError 把厂商业务错误叠加在 HTTP 状态之上；code 为空表示交给骨架按状态码分类。
	ClassifyError(status int, body []byte) (kind, code, message string, retryable bool, known bool)
}

// videoVendorInputUploader 由"图片必须先上传换 URL"的厂商实现（WaveSpeed）。骨架在
// BuildCreate 之前对每个带字节的素材执行一次上传，并用返回的 URL 替换它。
type videoVendorInputUploader interface {
	BuildUpload(input videoVendorInput) (*vendorHTTPRequest, error)
	DecodeUpload(response *vendorHTTPResponse) (string, error)
}

// videoVendorCostEstimator 由提供"提交前询价"接口的厂商实现（WaveSpeed 的 POST /model/price）。
// ok=false 表示这次请求无法询价（例如图生视频的素材还没有 URL）。
type videoVendorCostEstimator interface {
	BuildEstimate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput) (*vendorHTTPRequest, bool)
	DecodeEstimate(response *vendorHTTPResponse) (float64, error)
}

// videoVendorTerminateJudge 由取消 / 删除接口对"已完成"返回非 2xx 的厂商实现（fal 的
// cancel 对已完成任务返回 400 ALREADY_COMPLETED）：骨架用它判定终止是否算成功。
type videoVendorTerminateJudge interface {
	TerminateSucceeded(status int, body []byte) bool
}

// videoVendorAdapters 列出已落地的适配器构造函数。
var videoVendorAdapters = map[string]func() videoVendorAdapter{
	VideoProviderRunware:   func() videoVendorAdapter { return newRunwareVideoAdapter() },
	VideoProviderWaveSpeed: func() videoVendorAdapter { return newWaveSpeedVideoAdapter() },
	VideoProviderFal:       func() videoVendorAdapter { return newFalVideoAdapter() },
}

// videoVendorAdapterByName 返回 provider 名对应的适配器；未落地的平台返回 nil。
// 适配器无状态、档案静态，直接构造即可。
func videoVendorAdapterByName(name string) videoVendorAdapter {
	constructor, ok := videoVendorAdapters[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	return constructor()
}

// videoVendorCapabilitiesFromProfiles 由档案表生成内置能力快照。
func videoVendorCapabilitiesFromProfiles(defaultModel string, profiles map[string]*videoModelProfile, operations map[VideoCapability]bool) VideoCapabilities {
	models := make([]string, 0, len(profiles))
	for model := range profiles {
		models = append(models, model)
	}
	sort.Strings(models)
	capabilities := VideoCapabilities{
		DefaultModel:     defaultModel,
		DefaultSeconds:   map[string]int{},
		DefaultSizes:     map[string]string{},
		Operations:       operations,
		SupportedModels:  map[string]bool{},
		SupportedSeconds: map[string][]int{},
		SupportedSizes:   map[string][]string{},
		InputRolesByOperation: map[string]map[VideoInputRole]bool{
			VideoOperationGenerate: {VideoInputRoleReferenceImage: true},
		},
		InputMIMETypes: map[VideoInputRole]map[string]bool{
			VideoInputRoleReferenceImage: {"image/jpeg": true, "image/png": true, "image/webp": true},
		},
		MaxInputBytes:        map[VideoInputRole]int64{VideoInputRoleReferenceImage: MaxContentModerationImageBytes},
		MaxInputsByOperation: map[string]int{VideoOperationGenerate: 2},
		ContentVariants:      map[string]bool{"video": true},
	}
	for _, model := range models {
		profile := profiles[model]
		if profile == nil || profile.Fallback {
			continue
		}
		capabilities.SupportedModels[model] = true
		seconds := profile.supportedSecondsList()
		if len(seconds) == 0 {
			// 能力目录要求每个模型都声明时长集合；没有声明的档案给一个保守的 1 到 30 秒区间，
			// 真正的上限由上游裁决。
			seconds = make([]int, 0, 30)
			for s := 1; s <= 30; s++ {
				seconds = append(seconds, s)
			}
		}
		capabilities.SupportedSeconds[model] = seconds
		capabilities.DefaultSeconds[model] = seconds[0]
		for _, candidate := range seconds {
			if candidate == profile.DefaultSeconds {
				capabilities.DefaultSeconds[model] = candidate
				break
			}
		}
		if len(profile.Sizes) > 0 {
			capabilities.SupportedSizes[model] = append([]string(nil), profile.Sizes...)
			if profile.DefaultSize != "" && profile.supportsSize(profile.DefaultSize) {
				capabilities.DefaultSizes[model] = profile.DefaultSize
			}
		}
	}
	return capabilities
}

// videoVendorSetPath 按点号路径写入嵌套字段："settings.audio" → body["settings"]["audio"]。
func videoVendorSetPath(body map[string]any, path string, value any) {
	parts := strings.Split(strings.TrimSpace(path), ".")
	current := body
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

// videoVendorLookupPath 按点号路径读取嵌套字段；数组下标写数字："video.url"、"outputs.0"。
func videoVendorLookupPath(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func videoVendorLookupString(value any, path string) string {
	raw, ok := videoVendorLookupPath(value, path)
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func videoVendorEncodeDuration(profile *videoModelProfile, seconds int) any {
	switch profile.DurationEnc {
	case videoDurationString:
		return strconv.Itoa(seconds)
	case videoDurationStringS:
		return strconv.Itoa(seconds) + "s"
	case videoDurationFloatSec:
		return float64(seconds)
	default:
		return seconds
	}
}

// videoVendorOptionValue 只放行标量 provider_options 值。
func videoVendorOptionValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		return typed, typed != "" && len(typed) <= 2048
	case bool:
		return typed, true
	case float64, float32, int, int64, int32:
		return typed, true
	default:
		return nil, false
	}
}

func videoVendorValidationError(code, format string, args ...any) *VideoProviderError {
	return rejectedVideoProviderError("validation", code, fmt.Sprintf(format, args...), http.StatusBadRequest)
}
