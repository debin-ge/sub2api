package service

import (
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
