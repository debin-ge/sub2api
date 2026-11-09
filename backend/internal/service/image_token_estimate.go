package service

import (
	"math"
	"strings"
)

// 按 token 出图、上游没回用量时的估算（docs/图片计费二选一改造方案.md §5）。
//
// OpenAI：gpt-image-1、1.5 用官方 token 表；gpt-image-1-mini、gpt-image-2 只公布单张价，
// 表中数字按「单张价 ÷ 图片输出单价」反推，估算金额等于官方价。gpt-image-2.5、2.6 未公布，
// 按 gpt-image-2 估。Gemini 按官方定价页的每张输出 token。

// ImageEstimateMethod 是估算用量记在 usage_logs.usage_estimation_method 里的方法名
// （列宽 VARCHAR(16)）。
const ImageEstimateMethod = "image_estimate"

const (
	imageEstimateBasisTable    = "table"
	imageEstimateBasisArea     = "area_scaled"
	imageEstimateBasisFallback = "fallback"

	// 流式出图每个 partial_images 分片另加的输出 token（官方口径）。
	imageEstimatePartialImageTokens = 100

	// 输入图：Gemini 每张按 560（3 Pro Image 官方值）；OpenAI 按视觉分块口径
	// 85 + 170 × 512px 分块数，1024×1024 约 765。gpt-image-2 输入图一律按高保真处理，
	// 实际可能更多，所以做成可调常量。
	geminiImageInputTokensPerImage = 560
	openAIImageInputBaseTokens     = 85
	openAIImageInputTileTokens     = 170
)

// ImageDims 是一张输入图的像素尺寸。
type ImageDims struct {
	Width  int
	Height int
}

type openAIImageTokenRow struct {
	// 1024×1024、1024×1536、1536×1024 三种尺寸各自的 low / medium / high。
	square, portrait, landscape [3]int
}

var (
	openAIGPTImage1Tokens = openAIImageTokenRow{
		square: [3]int{272, 1056, 4160}, portrait: [3]int{408, 1584, 6240}, landscape: [3]int{400, 1568, 6208},
	}
	openAIGPTImage1MiniTokens = openAIImageTokenRow{
		square: [3]int{625, 1375, 4500}, portrait: [3]int{750, 1875, 6500}, landscape: [3]int{750, 1875, 6500},
	}
	openAIGPTImage2Tokens = openAIImageTokenRow{
		square: [3]int{200, 1767, 7033}, portrait: [3]int{167, 1367, 5500}, landscape: [3]int{167, 1367, 5500},
	}
)

// geminiImageTokenTable 是 Gemini 出图模型每张输出 token，按档位列出。
var geminiImageTokenTable = []struct {
	prefix string
	tiers  map[string]int
}{
	{"gemini-3.1-flash-lite-image", map[string]int{ImageBillingSize1K: 1120}},
	{"gemini-3.1-flash-image", map[string]int{"0.5K": 747, ImageBillingSize1K: 1120, ImageBillingSize2K: 1680, ImageBillingSize4K: 2520}},
	{"gemini-3-pro-image", map[string]int{ImageBillingSize1K: 1120, ImageBillingSize2K: 1120, ImageBillingSize4K: 2000}},
	{"gemini-2.5-flash-image", map[string]int{ImageBillingSize1K: 1290}},
	{"gemini-nano-banana", map[string]int{ImageBillingSize1K: 1120, ImageBillingSize2K: 1680, ImageBillingSize4K: 3780}},
}

func normalizeImageEstimateModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

func openAIImageTokenRowFor(model string) (openAIImageTokenRow, bool) {
	switch {
	case strings.HasPrefix(model, "gpt-image-1-mini"):
		return openAIGPTImage1MiniTokens, true
	case strings.HasPrefix(model, "gpt-image-1"):
		return openAIGPTImage1Tokens, true
	case strings.HasPrefix(model, "gpt-image-2"), strings.HasPrefix(model, "gpt-image-3"):
		return openAIGPTImage2Tokens, true
	default:
		return openAIImageTokenRow{}, false
	}
}

func imageEstimateQualityIndex(quality string) int {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "low":
		return 0
	case "medium":
		return 1
	default:
		// high / auto / 空 / xhigh / max：宁多不少，按 high 估。
		return 2
	}
}

// imageEstimateDims 把尺寸解析成像素；1K/2K/4K 档位换算成代表尺寸，未知按 1024×1024。
func imageEstimateDims(size string) (int, int) {
	switch strings.ToUpper(strings.TrimSpace(size)) {
	case "", "AUTO", ImageBillingSize1K:
		return 1024, 1024
	case ImageBillingSize2K:
		return 2048, 2048
	case ImageBillingSize4K:
		return 4096, 4096
	}
	if width, height, ok := parseImageBillingDimensions(strings.TrimSpace(size)); ok && width > 0 && height > 0 {
		return width, height
	}
	return 1024, 1024
}

// EstimateImageOutputTokens 估算一张图的输出 token。basis 说明依据：table（表里查到）、
// area_scaled（按面积折算）、fallback（表里没有这个模型，按 gpt-image-1 表估）。
func EstimateImageOutputTokens(model, size, quality string, partialImages int) (int, string) {
	normalized := normalizeImageEstimateModel(model)
	extra := max(partialImages, 0) * imageEstimatePartialImageTokens

	for _, entry := range geminiImageTokenTable {
		if !strings.HasPrefix(normalized, entry.prefix) && !(entry.prefix == "gemini-nano-banana" && strings.Contains(normalized, "nano-banana")) {
			continue
		}
		tier := strings.ToUpper(strings.TrimSpace(size))
		if tier != "0.5K" {
			tier = NormalizeImageBillingTierOrDefault(size)
		}
		if tokens, ok := entry.tiers[tier]; ok {
			return tokens + extra, imageEstimateBasisTable
		}
		largest := 0
		for _, tokens := range entry.tiers {
			largest = max(largest, tokens)
		}
		return largest + extra, imageEstimateBasisTable
	}

	row, known := openAIImageTokenRowFor(normalized)
	basis := imageEstimateBasisTable
	if !known {
		row = openAIGPTImage1Tokens
		basis = imageEstimateBasisFallback
	}
	q := imageEstimateQualityIndex(quality)
	width, height := imageEstimateDims(size)
	switch {
	case width == 1024 && height == 1024:
		return row.square[q] + extra, basis
	case width == 1024 && height == 1536:
		return row.portrait[q] + extra, basis
	case width == 1536 && height == 1024:
		return row.landscape[q] + extra, basis
	}
	scaled := int(math.Ceil(float64(row.square[q]) * float64(width) * float64(height) / (1024 * 1024)))
	if basis == imageEstimateBasisTable {
		basis = imageEstimateBasisArea
	}
	return scaled + extra, basis
}

// EstimateImageInputTokens 估算输入 token：提示词（调用方用本地 tokenizer 数好）加输入图。
func EstimateImageInputTokens(model string, promptTokens int, inputImages []ImageDims) int {
	total := max(promptTokens, 0)
	normalized := normalizeImageEstimateModel(model)
	gemini := strings.HasPrefix(normalized, "gemini") || strings.Contains(normalized, "nano-banana")
	for _, image := range inputImages {
		if gemini {
			total += geminiImageInputTokensPerImage
			continue
		}
		total += openAIImageInputTokens(image)
	}
	return total
}

// openAIImageInputTokens 按视觉分块口径：先缩到 2048 内，再把短边缩到 768，按 512px 分块。
func openAIImageInputTokens(image ImageDims) int {
	width, height := float64(image.Width), float64(image.Height)
	if width <= 0 || height <= 0 {
		width, height = 1024, 1024
	}
	if longest := math.Max(width, height); longest > 2048 {
		width, height = width*2048/longest, height*2048/longest
	}
	if shortest := math.Min(width, height); shortest > 768 {
		width, height = width*768/shortest, height*768/shortest
	}
	tiles := int(math.Ceil(width/512) * math.Ceil(height/512))
	return openAIImageInputBaseTokens + openAIImageInputTileTokens*tiles
}

// estimateImageTokenUsage 给缺用量的按 token 出图请求估算用量：输出按张数 × 每张估算，
// 输入按提示词 + 输入图。
func estimateImageTokenUsage(model, size, quality string, imageCount, partialImages, promptTokens int, inputImages []ImageDims) (ImageTokenUsage, string) {
	perImage, basis := EstimateImageOutputTokens(model, size, quality, partialImages)
	return ImageTokenUsage{
		TextInputTokens:   max(promptTokens, 0),
		ImageInputTokens:  EstimateImageInputTokens(model, 0, inputImages),
		ImageOutputTokens: perImage * max(imageCount, 1),
	}, basis
}
