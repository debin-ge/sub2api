// Package vendorprofile 定义聚合媒体平台 Tier B 模型档案的数据格式。
//
// 档案由 cmd/tools/video-vendor-profiles 从厂商机器可读 schema 生成（Runware 的
// index.json + schema.json、WaveSpeed 的 GET /models），固定成 JSON 提交进仓库，
// 运行时由 internal/service 嵌入加载。手写的 Tier A 档案优先级高于这里的条目。
package vendorprofile

// Document 是一家厂商的档案文件。
type Document struct {
	Vendor      string  `json:"vendor"`
	GeneratedAt string  `json:"generated_at"`
	Source      string  `json:"source"`
	Profiles    []Entry `json:"profiles"`
}

// Entry 是一个模型的厂商无关描述。字段为空表示"由适配器默认值决定"。
type Entry struct {
	Model string `json:"model"`
	Name  string `json:"name,omitempty"`
	// Endpoint 是提交路径；fal / WaveSpeed 等于模型 ID，Runware 留空。
	Endpoint string `json:"endpoint,omitempty"`

	Seconds        []int `json:"seconds,omitempty"`
	MinSeconds     int   `json:"min_seconds,omitempty"`
	MaxSeconds     int   `json:"max_seconds,omitempty"`
	DefaultSeconds int   `json:"default_seconds,omitempty"`
	// DurationEnc 取 int / float / string_int / string_s；空按适配器默认。
	DurationEnc string `json:"duration_enc,omitempty"`

	// Sizes 是厂商声明的封闭尺寸表（WxH）；为空且 Aspects × Resolutions 非空时由加载方展开。
	Sizes       []string `json:"sizes,omitempty"`
	DefaultSize string   `json:"default_size,omitempty"`
	Aspects     []string `json:"aspects,omitempty"`
	// Resolutions 是厂商接受的分辨率词表（通用档位名）；请求里只会发词表内的档位。
	Resolutions []string `json:"resolutions,omitempty"`
	// ResolutionLabels 把通用档位名改写成厂商词表（MiniMax 把 720p 叫 768p）。
	ResolutionLabels map[string]string `json:"resolution_labels,omitempty"`
	// SizeEnc 取 width_height / aspect_ratio / aspect_ratio_resolution / resolution_only / resolution_in_id / none。
	SizeEnc string `json:"size_enc,omitempty"`
	// ImageResolutionField 非空时带图请求改传分辨率档位（Runware 的 resolution）。
	ImageResolutionField string `json:"image_resolution_field,omitempty"`

	AllowsImage    bool   `json:"allows_image,omitempty"`
	RequiresImage  bool   `json:"requires_image,omitempty"`
	MaxImages      int    `json:"max_images,omitempty"`
	ImageField     string `json:"image_field,omitempty"`
	LastImageField string `json:"last_image_field,omitempty"`

	AudioField string            `json:"audio_field,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}
