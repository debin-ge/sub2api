//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsImageGenerationModel_GeminiProImage 测试 gemini-3-pro-image 识别
func TestIsImageGenerationModel_GeminiProImage(t *testing.T) {
	require.True(t, isImageGenerationModel("gemini-3-pro-image"))
	require.True(t, isImageGenerationModel("gemini-3-pro-image-preview"))
	require.True(t, isImageGenerationModel("models/gemini-3-pro-image"))
}

// TestIsImageGenerationModel_GeminiFlashImage 测试 gemini-2.5-flash-image 识别
func TestIsImageGenerationModel_GeminiFlashImage(t *testing.T) {
	require.True(t, isImageGenerationModel("gemini-2.5-flash-image"))
	require.True(t, isImageGenerationModel("gemini-2.5-flash-image-preview"))
}

// TestIsImageGenerationModel_RegularModel 测试普通模型不被识别为图片模型
func TestIsImageGenerationModel_RegularModel(t *testing.T) {
	require.False(t, isImageGenerationModel("claude-3-opus"))
	require.False(t, isImageGenerationModel("claude-sonnet-4-20250514"))
	require.False(t, isImageGenerationModel("gpt-4o"))
	require.False(t, isImageGenerationModel("gemini-2.5-pro")) // 非图片模型
	require.False(t, isImageGenerationModel("gemini-2.5-flash"))
	// 验证不会误匹配包含关键词的自定义模型名
	require.False(t, isImageGenerationModel("my-gemini-3-pro-image-test"))
	require.False(t, isImageGenerationModel("custom-gemini-2.5-flash-image-wrapper"))
}

// TestIsImageGenerationModel_CaseInsensitive 测试大小写不敏感
func TestIsImageGenerationModel_CaseInsensitive(t *testing.T) {
	require.True(t, isImageGenerationModel("GEMINI-3-PRO-IMAGE"))
	require.True(t, isImageGenerationModel("Gemini-3-Pro-Image"))
	require.True(t, isImageGenerationModel("GEMINI-2.5-FLASH-IMAGE"))
}

// TestExtractImageSize_ValidSizes 测试有效尺寸解析
func TestExtractImageSize_ValidSizes(t *testing.T) {
	// 1K
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K"}}}`)
	requireGeminiImageBillingTier(t, body, "1K")

	// 2K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"2K"}}}`)
	requireGeminiImageBillingTier(t, body, "2K")

	// 4K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4K"}}}`)
	requireGeminiImageBillingTier(t, body, "4K")
}

// TestExtractImageSize_CaseInsensitive 测试大小写不敏感
func TestExtractImageSize_CaseInsensitive(t *testing.T) {
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1k"}}}`)
	requireGeminiImageBillingTier(t, body, "1K")

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4k"}}}`)
	requireGeminiImageBillingTier(t, body, "4K")
}

// TestExtractImageSize_Default 测试无 imageConfig 返回默认 2K
func TestExtractImageSize_Default(t *testing.T) {
	// 无 generationConfig
	body := []byte(`{"contents":[]}`)
	requireGeminiImageBillingTier(t, body, "2K")

	// 有 generationConfig 但无 imageConfig
	body = []byte(`{"generationConfig":{"temperature":0.7}}`)
	requireGeminiImageBillingTier(t, body, "2K")

	// 有 imageConfig 但无 imageSize
	body = []byte(`{"generationConfig":{"imageConfig":{}}}`)
	requireGeminiImageBillingTier(t, body, "2K")
}

// TestExtractImageSize_InvalidJSON 测试非法 JSON 必须 fail-closed。
func TestExtractImageSize_InvalidJSON(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`not valid json`),
		[]byte(`{"broken":`),
	} {
		_, err := geminiImageBillingTier(body)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
	}
}

// TestExtractImageSize_EmptySize 测试空 imageSize 返回默认 2K
func TestExtractImageSize_EmptySize(t *testing.T) {
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":""}}}`)
	requireGeminiImageBillingTier(t, body, "2K")

	// 空格
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"   "}}}`)
	requireGeminiImageBillingTier(t, body, "2K")
}

// TestExtractImageSize_InvalidSize 测试显式无效尺寸不能回落到默认 2K
func TestExtractImageSize_InvalidSize(t *testing.T) {
	for _, imageSize := range []string{"3K", "8K", "invalid"} {
		body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"` + imageSize + `"}}}`)
		_, err := geminiImageBillingTier(body)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
	}
}

func requireGeminiImageBillingTier(t *testing.T, body []byte, expected string) {
	t.Helper()
	tier, err := geminiImageBillingTier(body)
	require.NoError(t, err)
	require.Equal(t, expected, tier)
}

func geminiImageBillingTier(body []byte) (string, error) {
	inputSize, err := extractGeminiImageInputSizeStrict(body)
	if err != nil {
		return "", err
	}
	return normalizeGeminiImageSizeTierStrict(inputSize)
}
