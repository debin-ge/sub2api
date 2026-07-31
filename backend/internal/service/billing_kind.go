package service

import "strings"

// BillingKind 是一次请求的**结算口径**：它决定该问哪张价格表要价。
//
// 在此之前，口径是在两个地方各自"猜"出来的：
//
//   - 准入侧（转发前）：一个名为 useUpstreamTokenCost 的布尔量兼任调度偏好和
//     定价守卫开关，于是所有非 token 口径的路由（图片 / 视频 / 网页搜索 /
//     count_tokens）连带跳过了守卫。
//   - 结算侧（转发后）：从上游返回的 result 反推 —— WebSearchCalls > 0 就算
//     网页搜索、VideoCount > 0 且模型名像 grok video 就算视频、ImageCount > 0
//     就算图片。上游少回一个字段，口径就换一张价格表。
//
// 但口径在请求进入网关的那一刻就已经由路由确定，与上游回了什么无关。把它显式地
// 从入口带下来，准入与结算才能对同一个口径达成一致 —— 否则守卫查 token 价放行，
// 结算却按图片价扣费，二者都"成功"了，钱却是错的。
type BillingKind string

const (
	// BillingKindUnspecified 是零值：调用方尚未迁移到显式口径。
	// 结算侧仍沿用旧的 result 反推逻辑；准入侧必须保守地按 token 守卫处理，
	// 不能因为调用方漏传口径就绕过价格检查。
	BillingKindUnspecified BillingKind = ""

	// BillingKindToken 按 token 计费：/v1/responses、/v1/messages、
	// /v1/chat/completions、/v1/embeddings、realtime 等。
	BillingKindToken BillingKind = "token"

	// BillingKindImage 图片生成工作负载：/v1/images/*、grok images。
	// 实际结算算法由 ResolvedBillingPlan/BillingMode 决定；GPT Image 可以按
	// 图片 token 计费，Grok 等模型仍可按张计费。
	BillingKindImage BillingKind = "image"

	// BillingKindVideo 按秒计费：grok videos。
	BillingKindVideo BillingKind = "video"

	// BillingKindWebSearch 按次计费：codex alpha/search。上游不返回 usage。
	BillingKindWebSearch BillingKind = "web_search"

	// BillingKindNone 是产品明确列入非计费白名单的端点：本地 Grok
	// count_tokens，以及当前不向用户收费的 OpenAI responses/input_tokens
	// bridge。它不是“查不到价格”的兜底；新增调用方必须先有明确的免费政策，
	// 不能因为模型未知就选 None 绕过守卫。
	BillingKindNone BillingKind = "none"
)

// Valid 报告 k 是否为已知口径（不含零值）。
func (k BillingKind) Valid() bool {
	switch k {
	case BillingKindToken, BillingKindImage, BillingKindVideo, BillingKindWebSearch, BillingKindNone:
		return true
	default:
		return false
	}
}

// String 实现 fmt.Stringer，零值渲染为 "unspecified" 便于日志排查。
func (k BillingKind) String() string {
	if k == BillingKindUnspecified {
		return "unspecified"
	}
	return string(k)
}

// RequiresPricing 报告该口径是否必须在转发前解析出价格。
//
// BillingKindNone 不查价（端点已被明确列入非计费白名单）；其余已知口径以及
// BillingKindUnspecified 一律要求价格可解析。零值按 token 守卫处理，避免新增
// 调用方漏传口径时 fail-open。
func (k BillingKind) RequiresPricing() bool {
	switch k {
	case BillingKindUnspecified, BillingKindToken, BillingKindImage, BillingKindVideo, BillingKindWebSearch:
		return true
	default:
		return false
	}
}

// ResponseDrivenMediaOverride 报告该口径是否仍允许"上游确实产出了媒体"改写结算方式。
//
// token 路由天然是多形的：/v1/chat/completions、/v1/responses 上的 gemini-image
// 之类模型会在一次对话里产出图片，历史上就按张结算。所以 token 只是**基线**，
// 拿到 image_count / video_count 时仍会升级为按张、按秒。
//
// 危险的是反方向：图片 / 视频路由若因为上游漏回计数而降级到 token 路径，而该模型
// 又没有 token 价，最终就是免费送出一张图。所以媒体口径由入口独占决定，不接受改写。
func (k BillingKind) ResponseDrivenMediaOverride() bool {
	return k == BillingKindToken || k == BillingKindUnspecified
}

// BillingMode 返回该口径对应的用量行 billing_mode 标签。
func (k BillingKind) BillingMode() BillingMode {
	switch k {
	case BillingKindImage:
		return BillingModeImage
	case BillingKindVideo:
		return BillingModeVideo
	default:
		return BillingModeToken
	}
}

// ParseBillingKind 解析配置/请求头等外部输入，无法识别时返回零值。
func ParseBillingKind(raw string) BillingKind {
	kind := BillingKind(strings.ToLower(strings.TrimSpace(raw)))
	if kind.Valid() {
		return kind
	}
	return BillingKindUnspecified
}

// BillingKind 返回 grok media 端点的结算口径。
//
// 端点本身就区分了图片与视频，无需等上游回 VideoCount 再猜；查询类端点
// （视频状态 / 内容拉取）不产生生成成本，归 None。
func (e GrokMediaEndpoint) BillingKind() BillingKind {
	switch e {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		return BillingKindImage
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		return BillingKindVideo
	default:
		return BillingKindNone
	}
}
