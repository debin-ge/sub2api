//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAzureOpenAIImagesURL_Generation(t *testing.T) {
	// 测试普通的 cognitiveservices 域名
	got := buildAzureOpenAIImagesURL(
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com",
		"gpt-image-2",
		openAIImagesGenerationsEndpoint,
		"2024-02-01",
	)
	require.Equal(t,
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/deployments/gpt-image-2/images/generations?api-version=2024-02-01",
		got,
	)

	// 测试 openai.azure.com 域名
	got2 := buildAzureOpenAIImagesURL(
		"https://my-deployment.openai.azure.com",
		"dall-e-3",
		openAIImagesGenerationsEndpoint,
		"2024-05-01-preview",
	)
	require.Equal(t,
		"https://my-deployment.openai.azure.com/openai/deployments/dall-e-3/images/generations?api-version=2024-05-01-preview",
		got2,
	)
}

func TestBuildAzureOpenAIImagesURL_BaseWithSuffix(t *testing.T) {
	// 测试包含 /openai/v1 后缀
	got1 := buildAzureOpenAIImagesURL(
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/v1",
		"gpt-image-2",
		openAIImagesGenerationsEndpoint,
		"2024-02-01",
	)
	require.Equal(t,
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/deployments/gpt-image-2/images/generations?api-version=2024-02-01",
		got1,
	)

	// 测试包含 /openai 后缀并带斜杠
	got2 := buildAzureOpenAIImagesURL(
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/",
		"gpt-image-2",
		openAIImagesGenerationsEndpoint,
		"2024-02-01",
	)
	require.Equal(t,
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/deployments/gpt-image-2/images/generations?api-version=2024-02-01",
		got2,
	)
}

func TestBuildAzureOpenAIImagesURL_Edits(t *testing.T) {
	got := buildAzureOpenAIImagesURL(
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com",
		"gpt-image-2",
		openAIImagesEditsEndpoint,
		"",
	)
	require.Equal(t,
		"https://daqia-mpf7tfno-eastus2.cognitiveservices.azure.com/openai/deployments/gpt-image-2/images/edits?api-version=2024-02-01",
		got,
	)
}

func TestGetAzureAPIVersion_FallbackAndKeys(t *testing.T) {
	// 1. 无 Extra 时降级为默认值
	require.Equal(t, "2024-02-01", getAzureAPIVersion(&Account{}))

	// 2. Extra 中有 azure_api_version 时
	acct1 := &Account{
		Extra: map[string]any{
			"azure_api_version": "2025-04-01-preview",
		},
	}
	require.Equal(t, "2025-04-01-preview", getAzureAPIVersion(acct1))

	// 3. Extra 中有 api_version 时（作为 B 方案的一部分）
	acct2 := &Account{
		Extra: map[string]any{
			"api_version": "2024-05-01-preview",
		},
	}
	require.Equal(t, "2024-05-01-preview", getAzureAPIVersion(acct2))

	// 4. 两个都有时，优先 azure_api_version
	acct3 := &Account{
		Extra: map[string]any{
			"azure_api_version": "2025-04-01-preview",
			"api_version":       "2024-05-01-preview",
		},
	}
	require.Equal(t, "2025-04-01-preview", getAzureAPIVersion(acct3))

	// 5. 为空字符串时降级
	acct4 := &Account{
		Extra: map[string]any{
			"api_version": "  ",
		},
	}
	require.Equal(t, "2024-02-01", getAzureAPIVersion(acct4))
}
