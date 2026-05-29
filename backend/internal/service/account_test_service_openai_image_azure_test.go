//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_OpenAIImageAPIKeyAzure_FallbackAPIVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, `{"created": 123, "data": [{"url": "https://azure.com/image.png"}]}`)
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}

	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          rawChatCompletionsTestConfig(),
	}

	account := &Account{
		ID:          101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "azure-image-key",
			"base_url": "https://test-images.cognitiveservices.azure.com/openai/v1",
		},
		// 没有在 Extra 中配置 api_version
	}

	err := svc.testOpenAIImageAPIKey(ctx, context.Background(), account, "gpt-image-2", "A red fox")
	require.NoError(t, err)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	// 验证降级到默认版本 2024-02-01 
	require.Equal(t, "https://test-images.cognitiveservices.azure.com/openai/deployments/gpt-image-2/images/generations?api-version=2024-02-01", req.URL.String())
	require.Equal(t, "azure-image-key", req.Header.Get("api-key"))
	require.Empty(t, req.Header.Get("Authorization"))

	// 校验 payload，不能含有 response_format 参数
	bodyBytes, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var payload map[string]any
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	require.Nil(t, payload["response_format"])
}

func TestAccountTestService_OpenAIImageAPIKeyAzure_CustomAPIVersionAndAlternativeDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, `{"created": 123, "data": [{"b64_json": "test_image_data"}]}`)
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}

	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          rawChatCompletionsTestConfig(),
	}

	account := &Account{
		ID:          102,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "azure-image-key-2",
			"base_url": "https://test-alt.openai.azure.com/openai",
		},
		Extra: map[string]any{
			"azure_api_version": "2024-05-01-preview",
		},
	}

	err := svc.testOpenAIImageAPIKey(ctx, context.Background(), account, "dall-e-3", "A cool landscape")
	require.NoError(t, err)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	// 验证自定义 api-version 以及 openai.azure.com 域名
	require.Equal(t, "https://test-alt.openai.azure.com/openai/deployments/dall-e-3/images/generations?api-version=2024-05-01-preview", req.URL.String())
	require.Equal(t, "azure-image-key-2", req.Header.Get("api-key"))
	require.Empty(t, req.Header.Get("Authorization"))

	// 校验 payload，不能含有 response_format 参数
	bodyBytes, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var payload map[string]any
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	require.Nil(t, payload["response_format"])
}

func TestAccountTestService_OpenAIImageAPIKeyAzure_FallbackKeysInExtra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, `{"created": 123, "data": [{"b64_json": "test_image_data"}]}`)
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}

	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          rawChatCompletionsTestConfig(),
	}

	account := &Account{
		ID:          103,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "azure-image-key-3",
			"base_url": "https://test-keys.openai.azure.com",
		},
		Extra: map[string]any{
			"api_version": "2024-06-01-preview", // 使用 api_version 键 (方案B的备用键)
		},
	}

	err := svc.testOpenAIImageAPIKey(ctx, context.Background(), account, "gpt-image-2", "A blue sea")
	require.NoError(t, err)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	// 验证从 api_version 读取到的版本
	require.Equal(t, "https://test-keys.openai.azure.com/openai/deployments/gpt-image-2/images/generations?api-version=2024-06-01-preview", req.URL.String())
	require.Equal(t, "azure-image-key-3", req.Header.Get("api-key"))

	// 校验 payload，不能含有 response_format 参数
	bodyBytes, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var payload map[string]any
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	require.Nil(t, payload["response_format"])
}
