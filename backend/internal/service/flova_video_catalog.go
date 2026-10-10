package service

import (
	"fmt"
	"sort"
	"strings"
)

// Flova 一期模型。ID 带 flova/ 前缀，避免与 fal / Runware 等平台上同名底层模型在定价
// 目录里串味；指令里用 DisplayName（Flova 模型手册中的名字）。
const (
	FlovaVideoModelSeedance25     = "flova/seedance-2.5"
	FlovaVideoModelSeedance20     = "flova/seedance-2.0"
	FlovaVideoModelSeedance20Fast = "flova/seedance-2.0-fast"
	FlovaVideoModelVeo31Fast      = "flova/veo-3.1-fast"
	FlovaVideoModelMiniMaxH3      = "flova/minimax-h3"
	FlovaVideoModelKling30Omni    = "flova/kling-3.0-omni"

	// 参考图上限与内容审核字节上限对齐（能力目录校验要求不得超过）。
	flovaMaxReferenceImageBytes = MaxContentModerationImageBytes
)

// flovaModelProfile 是 provider 内部的模型档案：请求约束 + 每秒积分（余额前置校验与
// 高成本确认的价格漂移检测共用）。付费档账单实测值，见《Flova 平台接入实施方案》7.3。
type flovaModelProfile struct {
	ID          string
	DisplayName string
	// RequiresImage：Kling 3.0 Omni 在 Flova 上文生视频不可靠（Agent 要求参考图），只做图生。
	RequiresImage bool
	// Seconds 非空时为离散集合，否则用 [MinSeconds, MaxSeconds] 连续区间。
	Seconds        []int
	MinSeconds     int
	MaxSeconds     int
	DefaultSeconds int
	// Resolutions 按偏好顺序；第一个为默认档位。
	Resolutions []string
	// Ratios 为空表示只允许 16:9 / 9:16。
	Ratios []string
	// CreditsPerSecond 按分辨率档位；缺失的档位表示本档未实测、不可用。
	CreditsPerSecond map[string]float64
}

var flovaSeedanceRatios = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}

var flovaModelProfiles = map[string]*flovaModelProfile{
	FlovaVideoModelSeedance25: {
		ID: FlovaVideoModelSeedance25, DisplayName: "Seedance 2.5",
		MinSeconds: 4, MaxSeconds: 30, DefaultSeconds: 5,
		Resolutions: []string{"720p", "1080p"}, Ratios: flovaSeedanceRatios,
		CreditsPerSecond: map[string]float64{"720p": 26, "1080p": 81},
	},
	FlovaVideoModelSeedance20: {
		ID: FlovaVideoModelSeedance20, DisplayName: "Seedance 2.0",
		MinSeconds: 4, MaxSeconds: 15, DefaultSeconds: 5,
		Resolutions: []string{"720p", "1080p"}, Ratios: flovaSeedanceRatios,
		CreditsPerSecond: map[string]float64{"720p": 24, "1080p": 60},
	},
	FlovaVideoModelSeedance20Fast: {
		ID: FlovaVideoModelSeedance20Fast, DisplayName: "Seedance 2.0 Fast",
		MinSeconds: 4, MaxSeconds: 15, DefaultSeconds: 5,
		Resolutions: []string{"720p"}, Ratios: flovaSeedanceRatios,
		CreditsPerSecond: map[string]float64{"720p": 20},
	},
	FlovaVideoModelVeo31Fast: {
		ID: FlovaVideoModelVeo31Fast, DisplayName: "Veo 3.1 Fast",
		Seconds: []int{4, 6, 8}, DefaultSeconds: 4,
		Resolutions:      []string{"720p"},
		CreditsPerSecond: map[string]float64{"720p": 15},
	},
	FlovaVideoModelMiniMaxH3: {
		ID: FlovaVideoModelMiniMaxH3, DisplayName: "MiniMax H3",
		MinSeconds: 4, MaxSeconds: 15, DefaultSeconds: 5,
		Resolutions: []string{"768p"}, Ratios: []string{"16:9", "9:16", "1:1"},
		CreditsPerSecond: map[string]float64{"768p": 12},
	},
	FlovaVideoModelKling30Omni: {
		ID: FlovaVideoModelKling30Omni, DisplayName: "Kling 3.0 Omni", RequiresImage: true,
		MinSeconds: 3, MaxSeconds: 15, DefaultSeconds: 5,
		Resolutions: []string{"720p"}, Ratios: []string{"16:9", "9:16", "1:1"},
		CreditsPerSecond: map[string]float64{"720p": 12},
	},
}

// flovaModelOrder 固定目录顺序；第一个是默认模型。
var flovaModelOrder = []string{
	FlovaVideoModelSeedance25, FlovaVideoModelSeedance20, FlovaVideoModelSeedance20Fast,
	FlovaVideoModelVeo31Fast, FlovaVideoModelMiniMaxH3, FlovaVideoModelKling30Omni,
}

func flovaVideoModelIDs() []string {
	return cloneStringSlice(flovaModelOrder)
}

func flovaModelProfileFor(model string) *flovaModelProfile {
	return flovaModelProfiles[strings.ToLower(strings.TrimSpace(model))]
}

// flovaSizeSpec 把 sub2api 的 WxH 尺寸映射成 Flova 指令里的「比例 + 分辨率档位」。
type flovaSizeSpec struct {
	Size       string
	Ratio      string
	Resolution string
}

// flovaSizeTable 按分辨率档位分组、固定顺序；每组第一个 16:9 尺寸是该档位的默认尺寸。
var flovaSizeTable = []flovaSizeSpec{
	{"1280x720", "16:9", "720p"}, {"720x1280", "9:16", "720p"}, {"720x720", "1:1", "720p"},
	{"960x720", "4:3", "720p"}, {"720x960", "3:4", "720p"}, {"1680x720", "21:9", "720p"},
	{"1366x768", "16:9", "768p"}, {"768x1366", "9:16", "768p"}, {"768x768", "1:1", "768p"},
	{"1920x1080", "16:9", "1080p"}, {"1080x1920", "9:16", "1080p"}, {"1080x1080", "1:1", "1080p"},
	{"1440x1080", "4:3", "1080p"}, {"1080x1440", "3:4", "1080p"}, {"2560x1080", "21:9", "1080p"},
}

func (p *flovaModelProfile) allowsRatio(ratio string) bool {
	if len(p.Ratios) == 0 {
		return ratio == "16:9" || ratio == "9:16"
	}
	for _, r := range p.Ratios {
		if r == ratio {
			return true
		}
	}
	return false
}

func (p *flovaModelProfile) allowsResolution(resolution string) bool {
	for _, r := range p.Resolutions {
		if r == resolution {
			return true
		}
	}
	return false
}

func (p *flovaModelProfile) supportedSeconds() []int {
	if len(p.Seconds) > 0 {
		return append([]int(nil), p.Seconds...)
	}
	out := make([]int, 0, p.MaxSeconds-p.MinSeconds+1)
	for s := p.MinSeconds; s <= p.MaxSeconds; s++ {
		out = append(out, s)
	}
	return out
}

func (p *flovaModelProfile) allowsSeconds(seconds int) bool {
	if len(p.Seconds) > 0 {
		for _, s := range p.Seconds {
			if s == seconds {
				return true
			}
		}
		return false
	}
	return seconds >= p.MinSeconds && seconds <= p.MaxSeconds
}

// minSeconds 是摊入回合费时用的最短可选时长（方案 D11）。
func (p *flovaModelProfile) minSeconds() int {
	if len(p.Seconds) > 0 {
		m := p.Seconds[0]
		for _, s := range p.Seconds[1:] {
			if s < m {
				m = s
			}
		}
		return m
	}
	return p.MinSeconds
}

// supportedRatios 返回模型接受的比例词表（空表示只有 16:9 / 9:16）。
func (p *flovaModelProfile) supportedRatios() []string {
	if len(p.Ratios) == 0 {
		return []string{"16:9", "9:16"}
	}
	return append([]string(nil), p.Ratios...)
}

func (p *flovaModelProfile) supportedSizes() []string {
	sizes := make([]string, 0, len(flovaSizeTable))
	for _, spec := range flovaSizeTable {
		if p.allowsResolution(spec.Resolution) && p.allowsRatio(spec.Ratio) {
			sizes = append(sizes, spec.Size)
		}
	}
	return sizes
}

func (p *flovaModelProfile) defaultSize() string {
	for _, spec := range flovaSizeTable {
		if spec.Resolution == p.Resolutions[0] && spec.Ratio == "16:9" {
			return spec.Size
		}
	}
	sizes := p.supportedSizes()
	if len(sizes) > 0 {
		return sizes[0]
	}
	return ""
}

// flovaArkResolution 把 Flova 自己的档位名换成 Ark 词表：MiniMax H3 的 1366x768 对外是 720p
// 档（与聚合平台 ResolutionLabels 的处理一致），其余档位名本来就在词表内。能力表、定价属性与
// 任务元数据都用 Ark 词表；只有发给 Flova 的指令与积分表用原生名。
func flovaArkResolution(native string) string {
	if strings.EqualFold(strings.TrimSpace(native), "768p") {
		return "720p"
	}
	return strings.ToLower(strings.TrimSpace(native))
}

func flovaArkResolutions(native []string) []string {
	out := make([]string, 0, len(native))
	seen := map[string]struct{}{}
	for _, value := range native {
		label := flovaArkResolution(value)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func flovaSizeSpecFor(size string) (flovaSizeSpec, bool) {
	size = strings.ToLower(strings.TrimSpace(size))
	for _, spec := range flovaSizeTable {
		if spec.Size == size {
			return spec, true
		}
	}
	return flovaSizeSpec{}, false
}

// flovaResolveRendition 把请求的 ratio / resolution 归一成 Flova 指令用的 (ratio, resolution)。
// 都缺省时用模型默认档位（首选分辨率 + 16:9）；adaptive 由参考图决定，Flova 侧退回 16:9。
func flovaResolveRendition(profile *flovaModelProfile, requestRatio, requestResolution string) (ratio, resolution string, err error) {
	if profile == nil {
		return "", "", fmt.Errorf("flova: unknown model")
	}
	ratio = strings.TrimSpace(requestRatio)
	resolution = strings.ToLower(strings.TrimSpace(requestResolution))
	if ratio == ArkVideoRatioAdaptive {
		ratio = ""
	}
	if resolution == "" {
		resolution = profile.Resolutions[0]
	}
	if ratio == "" {
		ratio = "16:9"
	}
	if !profile.allowsResolution(resolution) {
		// 请求用的是 Ark 词表（720p）；换回该模型的原生档位名（768p）。
		for _, native := range profile.Resolutions {
			if flovaArkResolution(native) == resolution {
				resolution = native
				break
			}
		}
	}
	if !profile.allowsResolution(resolution) {
		return "", "", fmt.Errorf("flova: %s does not support resolution %s", profile.DisplayName, resolution)
	}
	if !profile.allowsRatio(ratio) {
		return "", "", fmt.Errorf("flova: %s does not support aspect ratio %s", profile.DisplayName, ratio)
	}
	return ratio, resolution, nil
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	if v, ok := options[key].(string); ok {
		return v
	}
	return ""
}

// flovaRequestResolution 供 providerRequestResolution 使用：定价的 resolutions 条件与
// 执行规格冻结都按分辨率档位匹配。
func flovaRequestResolution(request VideoCreateRequest) string {
	profile := flovaModelProfileFor(request.Model)
	if profile == nil {
		return ""
	}
	_, resolution, err := flovaResolveRendition(profile, request.Ratio, request.Resolution)
	if err != nil {
		return ""
	}
	return flovaArkResolution(resolution)
}

// flovaCreditsPerSecond 返回模型在该分辨率档位的每秒积分；未实测档位返回 0, false。
func flovaCreditsPerSecond(model, resolution string) (float64, bool) {
	profile := flovaModelProfileFor(model)
	if profile == nil {
		return 0, false
	}
	v, ok := profile.CreditsPerSecond[strings.ToLower(strings.TrimSpace(resolution))]
	return v, ok
}

// DefaultFlovaVideoCapabilities 是 Flova 的内置能力快照。只有 generate 一种操作；参考
// 输入只收一张图（上传到 Flova 存储后在 chat 里引用）。
func DefaultFlovaVideoCapabilities() VideoCapabilities {
	supportedModels := make(map[string]bool, len(flovaModelOrder))
	supportedSeconds := make(map[string][]int, len(flovaModelOrder))
	defaultSeconds := make(map[string]int, len(flovaModelOrder))
	supportedResolutions := make(map[string][]string, len(flovaModelOrder))
	supportedRatios := make(map[string][]string, len(flovaModelOrder))
	defaultResolutions := make(map[string]string, len(flovaModelOrder))
	defaultRatios := make(map[string]string, len(flovaModelOrder))
	for _, id := range flovaModelOrder {
		profile := flovaModelProfiles[id]
		supportedModels[id] = true
		supportedSeconds[id] = profile.supportedSeconds()
		defaultSeconds[id] = profile.DefaultSeconds
		supportedResolutions[id] = flovaArkResolutions(profile.Resolutions)
		supportedRatios[id] = profile.supportedRatios()
		defaultResolutions[id] = flovaArkResolution(profile.Resolutions[0])
		defaultRatios[id] = "16:9"
	}
	imageMIMEs := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
	return VideoCapabilities{
		DefaultModel:         FlovaVideoModelSeedance25,
		DefaultSeconds:       defaultSeconds,
		SupportedResolutions: supportedResolutions,
		SupportedRatios:      supportedRatios,
		DefaultResolutions:   defaultResolutions,
		DefaultRatios:        defaultRatios,
		Operations: map[VideoCapability]bool{
			VideoCapabilityCreate: true, VideoCapabilityInputReference: true,
		},
		InputRolesByOperation: map[string]map[VideoInputRole]bool{
			VideoOperationGenerate: {VideoInputRoleFirstFrame: true},
		},
		InputMIMETypes: map[VideoInputRole]map[string]bool{
			VideoInputRoleFirstFrame: imageMIMEs,
		},
		MaxInputBytes:        map[VideoInputRole]int64{VideoInputRoleFirstFrame: flovaMaxReferenceImageBytes},
		MaxInputsByOperation: map[string]int{VideoOperationGenerate: 1},
		MaxContentByRole:     map[VideoInputRole]int{VideoInputRoleFirstFrame: 1},
		ContentVariants:      map[string]bool{"video": true},
		SupportedModels:      supportedModels,
		SupportedSeconds:     supportedSeconds,
	}
}

// flovaModelDisplayNames 供日志与前端展示；按目录顺序。
func flovaModelDisplayNames() map[string]string {
	out := make(map[string]string, len(flovaModelProfiles))
	keys := make([]string, 0, len(flovaModelProfiles))
	for k := range flovaModelProfiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = flovaModelProfiles[k].DisplayName
	}
	return out
}
