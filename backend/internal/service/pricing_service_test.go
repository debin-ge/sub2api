package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDashVersionSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"glm-5-1", "glm-5.1"},
		{"kimi-k2-5", "kimi-k2.5"},
		{"kimi-k2-6", "kimi-k2.6"},
		{"minimax-m2-7", "minimax-m2.7"},
		{"claude-opus-4-8", "claude-opus-4.8"},
		// 首个匹配替换后不再递归；日期段 20251101 内部不含 -N-N 结构，无变化
		{"claude-opus-4-5-20251101", "claude-opus-4.5-20251101"},
		// 已是 dot 形式：不变
		{"glm-5.1", "glm-5.1"},
		// 无版本号：不变
		{"deepseek-v4", "deepseek-v4"},
		// 空字符串：不变
		{"", ""},
	}
	for _, tc := range cases {
		got := normalizeDashVersionSuffix(tc.in)
		require.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}

func TestGetModelPricing_DashVersionSuffixHitsDotCatalogKeys(t *testing.T) {
	// Widsurf/OpenCode 用 dash 命名（glm-5-1），catalog 里用 dot 命名（glm-5.1）。
	// 归一化后应该能命中。
	svc := NewPricingServiceForTest(map[string]*ModelPriceEntry{
		"glm-5.1":      {Mode: "chat", InputCostPerToken: 6e-6, OutputCostPerToken: 24e-6},
		"kimi-k2.5":    {Mode: "chat", InputCostPerToken: 1e-6, OutputCostPerToken: 4e-6},
		"kimi-k2.6":    {Mode: "chat", InputCostPerToken: 1.5e-6, OutputCostPerToken: 5e-6},
		"minimax-m2.7": {Mode: "chat", InputCostPerToken: 3e-7, OutputCostPerToken: 1.2e-6},
	})

	// 查询用 dash 命名
	glm := svc.GetModelPricing("glm-5-1")
	require.NotNil(t, glm)
	require.InDelta(t, 6e-6, glm.InputCostPerToken, 1e-12)

	kimi5 := svc.GetModelPricing("kimi-k2-5")
	require.NotNil(t, kimi5)
	require.InDelta(t, 1e-6, kimi5.InputCostPerToken, 1e-12)

	kimi6 := svc.GetModelPricing("kimi-k2-6")
	require.NotNil(t, kimi6)
	require.InDelta(t, 1.5e-6, kimi6.InputCostPerToken, 1e-12)

	// 未归一命名（对照）
	glmDot := svc.GetModelPricing("glm-5.1")
	require.NotNil(t, glmDot)
	require.InDelta(t, 6e-6, glmDot.InputCostPerToken, 1e-12)
}

func TestPricingSchedulerBlankRemoteURLDoesNotStart(t *testing.T) {
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{RemoteURL: "  \t  "}}, nil)
	defer svc.Stop()

	svc.startUpdateScheduler()
	done := make(chan struct{})
	go func() {
		svc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("blank remote URL must not start scheduler")
	}
}

func TestPricingNonEmptyInvalidRemoteURLStillReturnsValidationError(t *testing.T) {
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{
		RemoteURL: "://invalid",
		DataDir:   t.TempDir(),
	}}, nil)

	err := svc.ForceUpdate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid pricing url")
}

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0.0000025,
			"cache_creation_input_token_cost_priority": 0.000005,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"long_context_input_token_threshold": 272000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 3e-5, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 5e-6, pricing.CacheCreationInputTokenCostPriority, 1e-12)
	require.InDelta(t, 5e-7, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputCostMultiplier, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestBillingService_GPT56CacheWritePricingUsesOfficialMultiplier(t *testing.T) {
	tests := []struct {
		model             string
		input             float64
		inputPriority     float64
		output            float64
		outputPriority    float64
		cacheRead         float64
		cacheReadPriority float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, inputPriority: 10e-6, output: 30e-6, outputPriority: 60e-6, cacheRead: 0.5e-6, cacheReadPriority: 1e-6},
		{model: "gpt-5.6-terra", input: 2.5e-6, inputPriority: 5e-6, output: 15e-6, outputPriority: 30e-6, cacheRead: 0.25e-6, cacheReadPriority: 0.5e-6},
		{model: "gpt-5.6-luna", input: 1e-6, inputPriority: 2e-6, output: 6e-6, outputPriority: 12e-6, cacheRead: 0.1e-6, cacheReadPriority: 0.2e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				tt.model: {
					InputCostPerToken:               tt.input,
					InputCostPerTokenPriority:       tt.inputPriority,
					OutputCostPerToken:              tt.output,
					OutputCostPerTokenPriority:      tt.outputPriority,
					CacheReadInputTokenCost:         tt.cacheRead,
					CacheReadInputTokenCostPriority: tt.cacheReadPriority,
				},
			}}
			svc := NewBillingService(&config.Config{}, pricingSvc)

			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input*1.25, pricing.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tt.inputPriority*1.25, pricing.CacheCreationPricePerTokenPriority, 1e-12)
			require.Equal(t, 272000, pricing.LongContextInputThreshold)
			require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)

			tokens := UsageTokens{InputTokens: 700, OutputTokens: 50, CacheCreationTokens: 200, CacheReadTokens: 100}
			standard, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.input*1.25, standard.CacheCreationCost, 1e-12)

			priority, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "priority")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.inputPriority*1.25, priority.CacheCreationCost, 1e-12)

			flex, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "flex")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.input*1.25*0.5, flex.CacheCreationCost, 1e-12)
		})
	}
}

func TestBillingService_GPT56UsesLongContextPricingAcrossModelsAndTiers(t *testing.T) {
	models := []struct {
		name               string
		input, cached      float64
		cacheWrite, output float64
	}{
		{name: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6},
		{name: "gpt-5.6-terra", input: 2.5e-6, cached: 0.25e-6, cacheWrite: 3.125e-6, output: 15e-6},
		{name: "gpt-5.6-luna", input: 1e-6, cached: 0.1e-6, cacheWrite: 1.25e-6, output: 6e-6},
	}
	tiers := []struct {
		name       string
		priceScale float64
	}{
		{name: "standard", priceScale: 1},
		{name: "priority", priceScale: 2},
		{name: "flex", priceScale: 0.5},
	}
	tokens := UsageTokens{
		InputTokens:         100000,
		CacheCreationTokens: 100000,
		CacheReadTokens:     73000,
		OutputTokens:        10,
	}

	for _, model := range models {
		for _, tier := range tiers {
			t.Run(model.name+"/"+tier.name, func(t *testing.T) {
				svc := NewBillingService(&config.Config{}, nil)
				serviceTier := ""
				if tier.name != "standard" {
					serviceTier = tier.name
				}
				cost, err := svc.CalculateCostWithServiceTier(model.name, tokens, 1, serviceTier)
				require.NoError(t, err)
				require.InDelta(t, float64(tokens.InputTokens)*model.input*tier.priceScale*2, cost.InputCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheCreationTokens)*model.cacheWrite*tier.priceScale*2, cost.CacheCreationCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheReadTokens)*model.cached*tier.priceScale*2, cost.CacheReadCost, 1e-12)
				require.InDelta(t, float64(tokens.OutputTokens)*model.output*tier.priceScale*1.5, cost.OutputCost, 1e-12)
			})
		}
	}
}

func TestBillingService_GPT56LongContextBoundaryIsExclusive(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72000, OutputTokens: 10}

	cost, err := svc.CalculateCost("gpt-5.6-sol", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 100000*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 100000*6.25e-6, cost.CacheCreationCost, 1e-12)
	require.InDelta(t, 72000*0.5e-6, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 10*30e-6, cost.OutputCost, 1e-12)
}

func TestPricingService_BareGPT56AliasDeterministicallyUsesSol(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5.6-sol":   {InputCostPerToken: 5e-6},
		"gpt-5.6-terra": {InputCostPerToken: 2.5e-6},
		"gpt-5.6-luna":  {InputCostPerToken: 1e-6},
		"gpt-5.4":       {InputCostPerToken: 2.5e-6},
	}}

	for i := 0; i < 100; i++ {
		for _, alias := range []string{"gpt-5.6", "openai/gpt-5.6"} {
			pricing := pricingSvc.GetModelPricing(alias)
			require.NotNil(t, pricing)
			require.InDelta(t, 5e-6, pricing.InputCostPerToken, 1e-12, "iteration=%d alias=%s", i, alias)
		}
	}

	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	for _, alias := range []string{"gpt-5.6", "openai/gpt-5.6"} {
		pricing, err := billingSvc.GetModelPricing(alias)
		require.NoError(t, err)
		require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12)
		require.InDelta(t, 6.25e-6, pricing.CacheCreationPricePerToken, 1e-12)
	}
}

func TestDefaultPricingIncludesOfficialGPT56Rates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)
	pricingSvc.pricingData = pricingData
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	tests := []struct {
		model                                                             string
		input, cached, cacheWrite, output                                 float64
		inputPriority, cachedPriority, cacheWritePriority, outputPriority float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6, inputPriority: 10e-6, cachedPriority: 1e-6, cacheWritePriority: 12.5e-6, outputPriority: 60e-6},
		{model: "gpt-5.6-terra", input: 2.5e-6, cached: 0.25e-6, cacheWrite: 3.125e-6, output: 15e-6, inputPriority: 5e-6, cachedPriority: 0.5e-6, cacheWritePriority: 6.25e-6, outputPriority: 30e-6},
		{model: "gpt-5.6-luna", input: 1e-6, cached: 0.1e-6, cacheWrite: 1.25e-6, output: 6e-6, inputPriority: 2e-6, cachedPriority: 0.2e-6, cacheWritePriority: 2.5e-6, outputPriority: 12e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := billingSvc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.cached, pricing.CacheReadPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.inputPriority, pricing.InputPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cachedPriority, pricing.CacheReadPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cacheWritePriority, pricing.CacheCreationPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.outputPriority, pricing.OutputPricePerTokenPriority, 1e-12)
			require.Equal(t, 272000, pricing.LongContextInputThreshold)
			require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
		})
	}
}

func TestGPT56DedicatedFallbacksUseOfficialRates(t *testing.T) {
	tests := []struct {
		model                             string
		input, cached, cacheWrite, output float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6},
		{model: "gpt-5.6-terra", input: 2.5e-6, cached: 0.25e-6, cacheWrite: 3.125e-6, output: 15e-6},
		{model: "gpt-5.6-luna", input: 1e-6, cached: 0.1e-6, cacheWrite: 1.25e-6, output: 6e-6},
	}

	for _, tt := range tests {
		t.Run(tt.model+"/pricing_service", func(t *testing.T) {
			pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
			}}
			svc := NewBillingService(&config.Config{}, pricingSvc)
			pricing, err := svc.GetModelPricing(tt.model + "-preview")
			require.NoError(t, err)
			assertGPT56FallbackPricing(t, pricing, tt.input, tt.cached, tt.cacheWrite, tt.output)
		})

		t.Run(tt.model+"/billing_service", func(t *testing.T) {
			svc := NewBillingService(&config.Config{}, nil)
			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			assertGPT56FallbackPricing(t, pricing, tt.input, tt.cached, tt.cacheWrite, tt.output)
		})
	}
}

func assertGPT56FallbackPricing(t *testing.T, pricing *ModelPricing, input, cached, cacheWrite, output float64) {
	t.Helper()
	require.InDelta(t, input, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, cached, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, output, pricing.OutputPricePerToken, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
}

func TestParsePricingData_KeepsImageOnlyPricing(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"image-only-model": {
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["image-only-model"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.034, pricing.OutputCostPerImage, 1e-12)
	require.Equal(t, "image_generation", pricing.Mode)
	// 仅有图片价的条目必须标记 token 价缺失，供 token 计费路径 fail-closed。
	require.True(t, pricing.TokenPricingAbsent)
}

func TestParsePricingData_MarksOneSidedTokenPricingIncomplete(t *testing.T) {
	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(`{
		"input-only-model": {"input_cost_per_token": 0.000001, "mode": "chat"},
		"output-only-model": {"output_cost_per_token": 0.000002, "mode": "chat"},
		"explicit-zero-model": {
			"input_cost_per_token": 0,
			"output_cost_per_token": 0,
			"input_cost_per_image_token": 0,
			"output_cost_per_image": 0,
			"output_cost_per_image_token": 0,
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)
	require.True(t, data["input-only-model"].TokenPricingAbsent)
	require.True(t, data["output-only-model"].TokenPricingAbsent)
	require.False(t, data["explicit-zero-model"].TokenPricingAbsent)
	require.True(t, data["explicit-zero-model"].ImageInputPriceExplicit)
	require.True(t, data["explicit-zero-model"].OutputCostPerImageExplicit)
	require.True(t, data["explicit-zero-model"].ImageOutputPriceExplicit)
}

func TestParsePricingData_MarksIncompleteBillingDimensionsUnavailable(t *testing.T) {
	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(`{
		"incomplete-cache": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"cache_read_input_token_cost": 0.0000001,
			"supports_prompt_caching": true,
			"litellm_provider": "anthropic"
		},
		"openai-native-cache": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"cache_read_input_token_cost": 0.0000001,
			"input_cost_per_token_priority": 0.000002,
			"output_cost_per_token_priority": 0.000004,
			"cache_read_input_token_cost_priority": 0.0000002,
			"supports_prompt_caching": true,
			"supports_service_tier": true,
			"litellm_provider": "openai"
		},
		"incomplete-priority": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"input_cost_per_token_priority": 0.000002,
			"supports_service_tier": true
		},
		"incomplete-long-context": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"long_context_input_token_threshold": 100,
			"long_context_input_cost_multiplier": 2
		},
		"complete-explicit-zero": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"cache_creation_input_token_cost": 0,
			"cache_creation_input_token_cost_above_1hr": 0,
			"cache_read_input_token_cost": 0,
			"input_cost_per_token_priority": 0.000002,
			"output_cost_per_token_priority": 0.000004,
			"cache_creation_input_token_cost_priority": 0,
			"cache_read_input_token_cost_priority": 0,
			"long_context_input_token_threshold": 100,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5,
			"supports_prompt_caching": true,
			"supports_service_tier": true
		}
	}`))
	require.NoError(t, err)

	for _, model := range []string{
		"incomplete-cache",
		"incomplete-priority",
		"incomplete-long-context",
	} {
		require.True(t, data[model].TokenPricingAbsent, model)
	}

	openAINativeCache := data["openai-native-cache"]
	require.False(t, openAINativeCache.TokenPricingAbsent)
	require.False(t, openAINativeCache.CacheCreationPriceExplicit)
	require.True(t, openAINativeCache.CacheReadPriceExplicit)

	complete := data["complete-explicit-zero"]
	require.False(t, complete.TokenPricingAbsent)
	require.True(t, complete.PricePresenceKnown)
	require.True(t, complete.CacheCreationPriceExplicit)
	require.True(t, complete.CacheCreationAbove1hrPriceExplicit)
	require.True(t, complete.CacheReadPriceExplicit)
	require.True(t, complete.InputPriorityPriceExplicit)
	require.True(t, complete.OutputPriorityPriceExplicit)
	require.True(t, complete.CacheCreationPriorityPriceExplicit)
	require.True(t, complete.CacheReadPriorityPriceExplicit)
	require.True(t, complete.LongContextPricingExplicit)

	svc.pricingData = data
	billingSvc := NewBillingService(&config.Config{}, svc)
	for _, model := range []string{
		"incomplete-cache",
		"incomplete-priority",
		"incomplete-long-context",
	} {
		_, err := billingSvc.GetModelPricingStrict(model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
	}
	openAIPricing, err := billingSvc.GetModelPricingStrict("openai-native-cache")
	require.NoError(t, err)
	require.InDelta(t, 0.000001, openAIPricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.0000001, openAIPricing.CacheReadPricePerToken, 1e-12)
	require.False(t, openAIPricing.CacheCreationPriceExplicit)

	resolved := mustModelPricing(t, billingSvc, "complete-explicit-zero")
	require.True(t, resolved.PricePresenceKnown)
	require.True(t, resolved.CacheCreationPriceExplicit)
	require.True(t, resolved.CacheCreation1hPriceExplicit)
	require.True(t, resolved.CacheReadPriceExplicit)
	require.True(t, resolved.InputPriorityPriceExplicit)
	require.True(t, resolved.OutputPriorityPriceExplicit)
	require.True(t, resolved.CacheCreationPriorityPriceExplicit)
	require.True(t, resolved.CacheReadPriorityPriceExplicit)
	require.True(t, resolved.LongContextPricingExplicit)
}

func mustModelPricing(t *testing.T, svc *BillingService, model string) *ModelPricing {
	t.Helper()
	pricing, err := svc.GetModelPricingStrict(model)
	require.NoError(t, err)
	return pricing
}

func TestPricingService_ExplicitZeroPriceDoesNotFuzzyMatchUnknownModel(t *testing.T) {
	pricingSvc := &PricingService{}
	data, err := pricingSvc.parsePricingData([]byte(`{
		"glm-4.7-flash": {
			"input_cost_per_token": 0,
			"output_cost_per_token": 0,
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)
	pricingSvc.pricingData = data

	require.NotNil(t, pricingSvc.GetModelPricing("glm-4.7-flash"))
	require.NotNil(t, pricingSvc.GetModelPricing("glm-4-7-flash"))
	require.NotNil(t, pricingSvc.GetModelPricing("models/glm-4.7-flash"))
	require.Nil(t, pricingSvc.GetModelPricing("glm-4.7-flash-20990101"))
	require.Nil(t, pricingSvc.GetModelPricing("glm-4.7-flash-v1:0"))
	require.Nil(t, pricingSvc.GetModelPricing("future-provider/glm-4.7-flash"))

	// BillingService 仍可选择明确的非零 hardcoded fallback，但绝不能继承动态免费价。
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	for _, model := range []string{"glm-4.7-flash-20990101", "glm-4.7-flash-v1:0"} {
		pricing, err := billingSvc.GetModelPricing(model)
		require.NoError(t, err)
		require.Greater(t, pricing.InputPricePerToken, 0.0)
		require.Greater(t, pricing.OutputPricePerToken, 0.0)
	}
}

func TestBillingService_GetModelPricing_FailsClosedForImageOnlyEntries(t *testing.T) {
	pricingSvc := &PricingService{}
	data, err := pricingSvc.parsePricingData([]byte(`{
		"imagen-9.0-generate": {
			"output_cost_per_image": 0.04,
			"litellm_provider": "vertex_ai-image-models",
			"mode": "image_generation"
		},
		"input-only-future-model": {
			"input_cost_per_token": 0.000001,
			"litellm_provider": "future-provider",
			"mode": "chat"
		},
		"gemini-image-with-token-price": {
			"input_cost_per_token": 0.0,
			"output_cost_per_token": 0.0,
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`))
	require.NoError(t, err)
	pricingSvc.pricingData = data
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	// image-only 条目不得进入 token 计费（否则 token 流量按 $0 计费），
	// 必须落到 fallback / ErrModelPricingUnavailable 的 fail-closed 路径。
	_, err = billingSvc.GetModelPricing("imagen-9.0-generate")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = billingSvc.GetModelPricing("input-only-future-model")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// 显式 0 token 价的免费条目保持历史行为：正常返回。
	pricing, err := billingSvc.GetModelPricing("gemini-image-with-token-price")
	require.NoError(t, err)
	require.Zero(t, pricing.InputPricePerToken)

	// 图片计费路径不受影响：仍能读到 image-only 条目的图片单价。
	raw := pricingSvc.GetModelPricing("imagen-9.0-generate")
	require.NotNil(t, raw)
	require.InDelta(t, 0.04, raw.OutputCostPerImage, 1e-12)
}

func TestPricingService_MergesFallbackOnlyModels(t *testing.T) {
	dir := t.TempDir()
	fallbackFile := filepath.Join(dir, "fallback.json")
	require.NoError(t, os.WriteFile(fallbackFile, []byte(`{
		"remote-model": {
			"input_cost_per_token": 0.000001,
			"litellm_provider": "test",
			"mode": "chat"
		},
		"gemini-3.1-flash-lite-image": {
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`), 0644))

	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.FallbackFile = fallbackFile
	remoteData, err := svc.parsePricingData([]byte(`{
		"remote-model": {
			"input_cost_per_token": 0.000002,
			"litellm_provider": "test",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	merged := svc.mergeFallbackPricingData(remoteData)
	require.InDelta(t, 0.000002, merged["remote-model"].InputCostPerToken, 1e-12)
	require.NotNil(t, merged["gemini-3.1-flash-lite-image"])
	require.InDelta(t, 0.034, merged["gemini-3.1-flash-lite-image"].OutputCostPerImage, 1e-12)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &ModelPriceEntry{InputCostPerToken: 1, OutputCostPerToken: 1}
	gpt53Pricing := &ModelPriceEntry{InputCostPerToken: 9, OutputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Same(t, sparkPricing, got)
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &ModelPriceEntry{InputCostPerToken: 2, OutputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &ModelPriceEntry{InputCostPerToken: 2, OutputCostPerToken: 2}
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)

	require.True(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_Gpt54UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.1-codex": &ModelPriceEntry{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2.5e-7, got.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, got.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.LongContextOutputCostMultiplier, 1e-12)
}

func TestGetModelPricing_OpenAICompactAliasUsesStaticFallback(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("openai/gpt5.5")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, got.OutputCostPerToken, 1e-12)
}

func TestPricingService_Gemini36FlashThinkingTiersUseBasePricing(t *testing.T) {
	basePricing := &LiteLLMModelPricing{
		InputCostPerToken:       1.5e-6,
		OutputCostPerToken:      7.5e-6,
		CacheReadInputTokenCost: 0.15e-6,
	}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-3.6-flash": basePricing,
	}}

	for _, model := range []string{
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			require.Same(t, basePricing, svc.GetModelPricing(model))
		})
	}
}

func TestPricingService_Gemini36FlashTierSpecificPricingTakesPrecedence(t *testing.T) {
	basePricing := &LiteLLMModelPricing{InputCostPerToken: 1.5e-6}
	tierPricing := &LiteLLMModelPricing{InputCostPerToken: 2e-6}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-3.6-flash":     basePricing,
		"gemini-3.6-flash-low": tierPricing,
	}}

	require.Same(t, tierPricing, svc.GetModelPricing("models/gemini-3.6-flash-low"))
}

func TestBillingService_Gemini36FlashThinkingTierFallbacksAreBillable(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}

	for _, model := range []string{
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			cost, err := svc.CalculateCost(model, tokens, 1)
			require.NoError(t, err)
			require.InDelta(t, 1.5, cost.InputCost, 1e-12)
			require.InDelta(t, 7.5, cost.OutputCost, 1e-12)
			require.InDelta(t, 0.15, cost.CacheReadCost, 1e-12)
			require.InDelta(t, 9.15, cost.TotalCost, 1e-12)
		})
	}
}

func TestDefaultPricingIncludesGemini36FlashRates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)
	pricingSvc.pricingData = pricingData
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	for _, model := range []string{"gemini-3.6-flash", "gemini-3.6-flash-low", "gemini-3.6-flash-high"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := billingSvc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, 1.5e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, 0.15e-6, pricing.CacheReadPricePerToken, 1e-12)
		})
	}
}

func TestDefaultPricingIncludesCodexAutoReview(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	svc := &PricingService{}
	pricingData, err := svc.parsePricingData(data)
	require.NoError(t, err)
	svc.pricingData = pricingData
	billingSvc := NewBillingService(&config.Config{}, svc)

	raw := svc.LookupModelPricingStrict("codex-auto-review")
	require.NotNil(t, raw)
	require.False(t, raw.TokenPricingAbsent)
	require.False(t, raw.CacheCreationPriceExplicit)

	got, err := billingSvc.GetModelPricingStrict("codex-auto-review")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.InDelta(t, 5e-6, got.InputPricePerToken, 1e-12)
	require.InDelta(t, 3e-5, got.OutputPricePerToken, 1e-12)
	require.InDelta(t, 5e-7, got.CacheReadPricePerToken, 1e-12)
	require.True(t, newStrictGlobalPricingGate(billingSvc, "codex-auto-review").effective())
}

func TestGetModelPricing_Gpt54MiniUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-mini")
	require.NotNil(t, got)
	require.InDelta(t, 7.5e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.5e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 7.5e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54NanoUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-nano")
	require.NotNil(t, got)
	require.InDelta(t, 2e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &ModelPriceEntry{InputCostPerToken: 3, OutputCostPerToken: 3}
	textPricing := &ModelPriceEntry{InputCostPerToken: 9, OutputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	}

	got := svc.GetModelPricing("gpt-image-3")
	require.Same(t, imagePricing, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                     2.5e-6,
			"input_cost_per_token_priority":            5e-6,
			"output_cost_per_token":                    15e-6,
			"output_cost_per_token_priority":           30e-6,
			"cache_creation_input_token_cost":          0.0,
			"cache_creation_input_token_cost_priority": 0.0,
			"cache_read_input_token_cost":              0.25e-6,
			"cache_read_input_token_cost_priority":     0.5e-6,
			"supports_service_tier":                    true,
			"supports_prompt_caching":                  true,
			"litellm_provider":                         "openai",
			"mode":                                     "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	svc := &PricingService{}
	pricingMap, err := svc.parsePricingData(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 2.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 15e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0,
			"cache_creation_input_token_cost_priority": 0,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000025, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000005, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000015, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00003, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000025, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000005, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

// ---------------------------------------------------------------------------
// ListModelNamesByProvider
// ---------------------------------------------------------------------------

func TestListModelNamesByProvider_ReturnsMatchingModels(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"claude-opus-4-5-20251101": {PricingCatalogProvider: "anthropic", InputCostPerToken: 1.5e-5},
			"claude-sonnet-4-5":        {PricingCatalogProvider: "anthropic", InputCostPerToken: 3e-6},
			"gpt-4o":                   {PricingCatalogProvider: "openai", InputCostPerToken: 5e-6},
			"gemini-2.5-pro":           {PricingCatalogProvider: "google", InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.ElementsMatch(t, []string{"claude-opus-4-5-20251101", "claude-sonnet-4-5"}, got)
	// Must be sorted
	require.Equal(t, "claude-opus-4-5-20251101", got[0])
	require.Equal(t, "claude-sonnet-4-5", got[1])
}

func TestListModelNamesByProvider_CaseInsensitive(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-4o": {PricingCatalogProvider: "OpenAI", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.Equal(t, []string{"gpt-4o"}, got)

	got2 := svc.ListModelNamesByProvider("OPENAI")
	require.Equal(t, []string{"gpt-4o"}, got2)
}

func TestListModelNamesByProvider_NoMatch(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"gpt-4o": {PricingCatalogProvider: "openai", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestListModelNamesByProvider_EmptyCatalog(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*ModelPriceEntry{},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.NotNil(t, got)
	require.Empty(t, got)
}
