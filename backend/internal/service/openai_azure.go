package service

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func isAzureOpenAIBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		if h, _, splitErr := net.SplitHostPort(strings.ToLower(strings.TrimSpace(parsed.Host))); splitErr == nil {
			host = h
		}
	}
	return strings.HasSuffix(host, ".openai.azure.com") ||
		strings.HasSuffix(host, ".cognitiveservices.azure.com")
}

func isAzureOpenAIAPIKeyAccount(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformOpenAI &&
		account.Type == AccountTypeAPIKey &&
		isAzureOpenAIBaseURL(account.GetOpenAIBaseURL())
}

func IsAzureOpenAIAPIKeyAccount(account *Account) bool {
	return isAzureOpenAIAPIKeyAccount(account)
}

func setOpenAIUpstreamAuthHeader(req *http.Request, account *Account, token string) {
	if req == nil {
		return
	}
	req.Header.Del("Authorization")
	req.Header.Del("authorization")
	req.Header.Del("api-key")

	if isAzureOpenAIAPIKeyAccount(account) {
		req.Header.Set("api-key", token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

const (
	defaultAzureAPIVersion = "2024-02-01"
)

// buildAzureOpenAIImagesURL 构建 Azure OpenAI 图片生成的完整 URL。
// Azure 格式: {base}/openai/deployments/{deployment}/images/generations?api-version={version}
// base 示例: https://xxx.cognitiveservices.azure.com
func buildAzureOpenAIImagesURL(baseURL string, deploymentName string, endpoint string, apiVersion string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	// 移除 base URL 中可能存在的 /openai/v1 或 /openai 后缀
	for _, suffix := range []string{"/openai/v1", "/openai"} {
		if strings.HasSuffix(normalized, suffix) {
			normalized = strings.TrimSuffix(normalized, suffix)
			break
		}
	}
	normalized = strings.TrimRight(normalized, "/")

	// 确定子路径: /images/generations 或 /images/edits
	relative := strings.TrimPrefix(strings.TrimSpace(endpoint), "/v1")

	if strings.TrimSpace(apiVersion) == "" {
		apiVersion = defaultAzureAPIVersion
	}

	return fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s",
		normalized, deploymentName, relative, apiVersion)
}

// getAzureAPIVersion 从账户 Extra 中读取自定义 Azure API version，若未配置或为空则降级为默认值。
func getAzureAPIVersion(account *Account) string {
	if account == nil || account.Extra == nil {
		return defaultAzureAPIVersion
	}
	// 优先尝试 azure_api_version，其次尝试 api_version
	for _, key := range []string{"azure_api_version", "api_version"} {
		if v, ok := account.Extra[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return defaultAzureAPIVersion
}
