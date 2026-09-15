package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimateOpenAIUsageFallbackUsesTokenizerAndNoCacheBuckets(t *testing.T) {
	usage, source, method := EstimateOpenAIUsageFallback(
		"gpt-5.4",
		[]byte(`{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_text","text":"hello billing"}]}]}`),
		[]byte("hello back"),
	)

	require.Equal(t, UsageSourceEstimated, source)
	require.Equal(t, "tokenizer", method)
	require.Greater(t, usage.InputTokens, 0)
	require.Greater(t, usage.OutputTokens, 0)
	require.Zero(t, usage.CacheReadInputTokens)
	require.Zero(t, usage.CacheCreationInputTokens)
}

func TestEstimateOpenAIUsageFallbackUsesMinimumForInvalidRequest(t *testing.T) {
	usage, source, method := EstimateOpenAIUsageFallback("gpt-5.4", []byte("not-json"), nil)

	require.Equal(t, UsageSourceMinimum, source)
	require.Equal(t, "minimum", method)
	require.GreaterOrEqual(t, usage.InputTokens, fallbackUsageMinimumTokens)
}

func TestExtractOpenAISemanticOutputForBilling(t *testing.T) {
	output := extractOpenAISemanticOutputForBilling([]byte(`{"response":{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","arguments":"{\"q\":\"x\"}"}]},"usage":{"input_tokens":1}}`))

	require.Contains(t, string(output), "hello")
	require.Contains(t, string(output), `{"q":"x"}`)
	require.NotContains(t, string(output), "input_tokens")
}

func TestApplyOpenAIUsageFallbackMarksMissingUsageAndUsesRequestEstimate(t *testing.T) {
	result := &OpenAIForwardResult{Model: "gpt-5.4", Usage: OpenAIUsage{}}
	applyOpenAIUsageFallback(result, "gpt-5.4", []byte(`{"model":"gpt-5.4","input":"charge this request"}`))

	require.Equal(t, UsageSourceEstimated, result.UsageSource)
	require.Equal(t, "tokenizer", result.UsageEstimationMethod)
	require.Greater(t, result.Usage.InputTokens, 1)
	require.Zero(t, result.Usage.CacheReadInputTokens)
	require.Zero(t, result.Usage.CacheCreationInputTokens)
}

func TestApplyOpenAIUsageFallbackDoesNotReplaceObservedUsage(t *testing.T) {
	result := &OpenAIForwardResult{
		Model: "gpt-5.4",
		Usage: OpenAIUsage{InputTokens: 123, OutputTokens: 45},
	}
	applyOpenAIUsageFallback(result, "gpt-5.4", []byte(`{"model":"gpt-5.4","input":"different request"}`))

	require.Equal(t, UsageSourceUpstream, result.UsageSource)
	require.Equal(t, "upstream", result.UsageEstimationMethod)
	require.Equal(t, 123, result.Usage.InputTokens)
	require.Equal(t, 45, result.Usage.OutputTokens)
}

func TestApplyGatewayUsageFallbackUsesRequestAndSemanticOutput(t *testing.T) {
	result := &ForwardResult{
		Model:                  "gpt-5.4",
		FallbackSemanticOutput: []byte(`{"content":[{"text":"charge the generated response"}]}`),
	}

	applyGatewayUsageFallback(
		result,
		[]byte(`{"messages":[{"role":"user","content":"charge this request"}]}`),
	)

	require.Equal(t, UsageSourceEstimated, result.UsageSource)
	require.Equal(t, "tokenizer", result.UsageEstimationMethod)
	require.Greater(t, result.Usage.InputTokens, fallbackUsageMinimumTokens)
	require.Greater(t, result.Usage.OutputTokens, 0)
}

func TestApplyGatewayUsageFallbackBillsObservedOutputWithoutRequestBody(t *testing.T) {
	result := &ForwardResult{
		Model:                  "gpt-5.4",
		FallbackSemanticOutput: []byte(`{"content":[{"text":"observed output"}]}`),
	}

	applyGatewayUsageFallback(result, nil)

	require.GreaterOrEqual(t, result.Usage.InputTokens, fallbackUsageMinimumTokens)
	require.Greater(t, result.Usage.OutputTokens, 0)
	require.NotEqual(t, UsageSourceUnknown, result.UsageSource)
}
