//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const arkTestTargetURL = "https://ark.cn-beijing.volces.com/api/compatible/v1/messages"

func TestIsArkUpstreamURL(t *testing.T) {
	cases := map[string]bool{
		arkTestTargetURL: true,
		"https://ark.cn-shanghai.volces.com/api/v3":      true,
		"https://ARK.CN-BEIJING.VOLCES.COM/api/coding":   true,
		"https://volces.com/x":                           true,
		"https://ark.ap-southeast.bytepluses.com/api/v3": false,
		"https://api.deepseek.com/anthropic/v1/messages": false,
		"https://evilvolces.com/v1/messages":             false,
		"https://volces.com.evil.example/v1/messages":    false,
		"":           false,
		"://bad-url": false,
	}
	for raw, want := range cases {
		require.Equal(t, want, isArkUpstreamURL(raw), raw)
	}
}

func TestRewriteArkAnthropicWebSearchTools(t *testing.T) {
	t.Run("rewrites web_search_20250305 and keeps other fields", func(t *testing.T) {
		body := []byte(`{"model":"deepseek-v4.1-flash","tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],"messages":[]}`)
		out := rewriteArkAnthropicWebSearchTools(body, arkTestTargetURL)
		require.Equal(t, "web_search", gjson.GetBytes(out, "tools.0.type").String())
		require.Equal(t, "web_search", gjson.GetBytes(out, "tools.0.name").String())
		require.Equal(t, int64(8), gjson.GetBytes(out, "tools.0.max_uses").Int())
		require.Equal(t, "deepseek-v4.1-flash", gjson.GetBytes(out, "model").String())
	})

	t.Run("rewrites any web_search_* version", func(t *testing.T) {
		body := []byte(`{"tools":[{"type":"web_search_20260101","name":"web_search"}]}`)
		out := rewriteArkAnthropicWebSearchTools(body, arkTestTargetURL)
		require.Equal(t, "web_search", gjson.GetBytes(out, "tools.0.type").String())
	})

	t.Run("only rewrites search tools among mixed tools", func(t *testing.T) {
		body := []byte(`{"tools":[` +
			`{"name":"Read","description":"read","input_schema":{"type":"object"}},` +
			`{"type":"web_search_20250305","name":"web_search"},` +
			`{"type":"custom","name":"web_search_helper","input_schema":{"type":"object"}}]}`)
		out := rewriteArkAnthropicWebSearchTools(body, arkTestTargetURL)
		require.False(t, gjson.GetBytes(out, "tools.0.type").Exists())
		require.Equal(t, "Read", gjson.GetBytes(out, "tools.0.name").String())
		require.Equal(t, "web_search", gjson.GetBytes(out, "tools.1.type").String())
		require.Equal(t, "custom", gjson.GetBytes(out, "tools.2.type").String())
	})

	t.Run("leaves body untouched", func(t *testing.T) {
		cases := []struct {
			name   string
			body   string
			target string
		}{
			{"non ark upstream", `{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`, "https://api.deepseek.com/anthropic/v1/messages"},
			{"byteplus upstream", `{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`, "https://ark.ap-southeast.bytepluses.com/api/v3"},
			{"already web_search", `{"tools":[{"type":"web_search","name":"web_search"}]}`, arkTestTargetURL},
			{"function tools only", `{"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`, arkTestTargetURL},
			{"no tools", `{"messages":[{"role":"user","content":"web_search"}]}`, arkTestTargetURL},
			{"tools not array", `{"tools":{"type":"web_search_20250305"}}`, arkTestTargetURL},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := []byte(tc.body)
				require.Equal(t, tc.body, string(rewriteArkAnthropicWebSearchTools(body, tc.target)))
			})
		}
	})
}

func nativeAnthropicArkTestAccount(baseURL string) *Account {
	return &Account{
		ID:          703,
		Name:        "deepseek-ark",
		Platform:    PlatformDeepSeek,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":            "sk-test",
			"api_protocol":       APIProtocolAnthropic,
			"base_url_anthropic": baseURL,
		},
	}
}

func TestNativeAnthropicPassthroughRewritesArkWebSearchTool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-v4.1-flash","max_tokens":32,"stream":false,` +
		`"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],` +
		`"messages":[{"role":"user","content":"今天有什么热点新闻"}]}`)

	t.Run("ark upstream", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		account := nativeAnthropicArkTestAccount("https://ark.cn-beijing.volces.com/api/compatible")

		_, err := svc.ForwardAsAnthropic(context.Background(),
			adaptiveProtocolTestContext("/v1/messages", body), account, body, "", "")
		require.NoError(t, err)
		require.Equal(t, arkTestTargetURL, upstream.lastReq.URL.String())
		require.Equal(t, "web_search", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
		require.Equal(t, int64(8), gjson.GetBytes(upstream.lastBody, "tools.0.max_uses").Int())
	})

	t.Run("non ark upstream keeps anthropic tool type", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		account := nativeAnthropicArkTestAccount("http://anthropic.example")

		_, err := svc.ForwardAsAnthropic(context.Background(),
			adaptiveProtocolTestContext("/v1/messages", body), account, body, "", "")
		require.NoError(t, err)
		require.Equal(t, "http://anthropic.example/v1/messages", upstream.lastReq.URL.String())
		require.Equal(t, "web_search_20250305", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
	})
}
