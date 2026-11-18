package service

import "strings"

// 火山方舟联网搜索功能级 403 的识别特征（见方舟错误码文档）。
const (
	arkWebSearchAccessDeniedCode    = "AccessDenied"
	arkWebSearchAccessDeniedMessage = "access denied for web search"
	arkWebSearchFreeQuotaCode       = "QuotaExceeded.DoubaoSearchFreeQuotaExceeded"
)

// isUpstreamWebSearchFeature403 识别「联网搜索组件未开通 / 免费搜索额度耗尽」这类
// 功能级 403。它只说明该账号不能执行联网搜索，不影响同账号的其他请求，因此只应换号，
// 不得计入账号 403 升级计数、临时停调或永久禁用——否则每次搜索都会把健康账号踢下线。
//
// 方舟的通用资源无权限同样使用 AccessDenied，故 AccessDenied 必须同时比对搜索文案。
func isUpstreamWebSearchFeature403(responseBody []byte) bool {
	if len(responseBody) == 0 {
		return false
	}
	code := strings.TrimSpace(extractUpstreamErrorCode(responseBody))
	if strings.EqualFold(code, arkWebSearchFreeQuotaCode) {
		return true
	}
	if !strings.EqualFold(code, arkWebSearchAccessDeniedCode) {
		return false
	}
	msg := strings.ToLower(extractUpstreamErrorMessage(responseBody))
	return strings.Contains(msg, arkWebSearchAccessDeniedMessage)
}
