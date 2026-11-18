package service

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// arkWebSearchToolType 是火山方舟 Messages 接口识别为「联网内容插件」的工具类型。
const arkWebSearchToolType = "web_search"

// isArkUpstreamURL 报告出站地址是否为火山方舟（*.volces.com）。
// BytePlus ModelArk（bytepluses.com）文档未提供该工具，不在此列。
func isArkUpstreamURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "volces.com" || strings.HasSuffix(host, ".volces.com")
}

// rewriteArkAnthropicWebSearchTools 把发往火山方舟 Anthropic Messages 接口的
// Anthropic 标准服务端搜索工具（web_search_20250305 等 web_search_* 版本）改写为
// 方舟自有的 {"type":"web_search"}。
//
// 方舟在 Messages 接口上把两种类型交给不同组件：web_search_20250305 走「豆包搜索
// Custom 版」，web_search 走「联网内容插件」，二者需在控制台分别开通。只开通了插件
// 的账号收到 web_search_20250305 会稳定返回 403 AccessDenied（Access denied for
// web search）。这里只改 type，name / max_uses 等其余字段原样保留。
//
// 非方舟地址、无 tools、无需改写时原样返回 body。
func rewriteArkAnthropicWebSearchTools(body []byte, targetURL string) []byte {
	if len(body) == 0 || !bytes.Contains(body, []byte(arkWebSearchToolType)) {
		return body
	}
	if !isArkUpstreamURL(targetURL) {
		return body
	}
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body
	}
	out := body
	for i, tool := range tools.Array() {
		toolType := tool.Get("type").String()
		if toolType == arkWebSearchToolType || !strings.HasPrefix(toolType, arkWebSearchToolType) {
			continue
		}
		rewritten, err := sjson.SetBytes(out, "tools."+strconv.Itoa(i)+".type", arkWebSearchToolType)
		if err != nil {
			return body
		}
		out = rewritten
	}
	return out
}
