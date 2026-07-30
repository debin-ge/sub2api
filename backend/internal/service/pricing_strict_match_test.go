//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// newStrictMatchCatalog 只放三条真实配过的条目，用来区分"这个模型自己有价"和
// "查价链能给它凑出一个数"。
func newStrictMatchCatalog() *PricingService {
	return &PricingService{pricingData: map[string]*ModelPriceEntry{
		// DefaultTestModel：matchOpenAIModel 把任何 gpt- 开头的未知模型兜到这里。
		"gpt-5.4": {InputCostPerToken: 1.25e-6, OutputCostPerToken: 10e-6},
		// matchByModelFamily 的 opus-5 → opus-4.8 回退目标。
		"claude-opus-4-8": {InputCostPerToken: 5e-6, OutputCostPerToken: 25e-6},
		// 同一模型线的某个日期快照。
		"claude-sonnet-4-5-20250929": {InputCostPerToken: 3e-6, OutputCostPerToken: 15e-6},
	}}
}

func TestLookupModelPricingStrict_RejectsCrossModelInference(t *testing.T) {
	svc := newStrictMatchCatalog()

	tests := []struct {
		name       string
		model      string
		wantStrict bool
		wantLoose  bool
	}{
		{
			name:       "exact catalog entry",
			model:      "gpt-5.4",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			// 严格口径必须保留这一步：Anthropic 的模型 ID 普遍带发布日期，而目录里
			// 通常只有其中一个日期。把它排除掉会误拒大量正常流量，shadow 的信号也就
			// 被噪声淹没了。
			name:       "another date snapshot of the same model line",
			model:      "claude-sonnet-4-5-20251215",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			name:       "trailing date snapshot after a dotted model version",
			model:      "gpt-5.4-20251215",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			name:       "colon version segment is a distinct SKU under strict lookup",
			model:      "gpt-5.4-v1:0",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "embedded date segment is not a release snapshot suffix",
			model:      "gpt-5.4-20250101-preview",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "non-calendar numeric suffix is not a snapshot",
			model:      "gpt-5.4-99999999",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "invalid calendar date suffix is not a snapshot",
			model:      "gpt-5.4-20251399",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "implausibly old date suffix is not a modern model snapshot",
			model:      "gpt-5.4-19000101",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			// 真正的跨模型推断：目录里没有 claude-opus-5，宽松口径拿 opus-4.8 的价格
			// 顶上（见 matchByModelFamily 的注释：这是为了避免掉进 opus-4 的 3 倍超收）。
			// 结算需要这个回退，准入不需要——没人给 opus-5 配过价。
			name:       "unknown Claude model borrows another SKU price",
			model:      "claude-opus-5",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			// 任何 gpt- 开头的模型最终都能兜到 DefaultTestModel，用宽松口径判断
			// "配没配价"在 OpenAI 侧接近恒真。
			name:       "unknown OpenAI model falls back to the default test model",
			model:      "gpt-future-unpriced-v99",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "arbitrary provider prefix cannot inherit a positive catalog price",
			model:      "future-provider/gpt-5.4",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "explicit OpenAI namespace remains the same SKU",
			model:      "openai/gpt-5.4",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			name:       "Google models namespace remains the same SKU",
			model:      "models/gpt-5.4",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			name:       "fully-qualified Vertex resource remains the same SKU",
			model:      "projects/p/locations/us-central1/publishers/google/models/gpt-5.4",
			wantStrict: true,
			wantLoose:  true,
		},
		{
			name:       "lookalike Google resource prefix is not whitelisted",
			model:      "future-provider/publishers/google/models/gpt-5.4",
			wantStrict: false,
			wantLoose:  true,
		},
		{
			name:       "unrelated unknown model resolves under neither",
			model:      "totally-unknown-v99",
			wantStrict: false,
			wantLoose:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantLoose, svc.GetModelPricing(tt.model) != nil, "loose lookup")
			require.Equal(t, tt.wantStrict, svc.LookupModelPricingStrict(tt.model) != nil, "strict lookup")
		})
	}
}

func TestBillingServiceGetModelPricingStrict_RejectsKeywordFallback(t *testing.T) {
	svc := newTestBillingService()

	// 兜底表里逐个 SKU 写死的价格是开发者的真实决定。
	strict, err := svc.GetModelPricingStrict("claude-sonnet-4")
	require.NoError(t, err)
	require.NotNil(t, strict)

	// GLM 的 SKU 拼写归一化仍算命中模型自己的条目：glm-4.7-0727 是 glm-4.7 的一个
	// 拼写，不是另一个模型。
	strictGLM, err := svc.GetModelPricingStrict("glm-4.7-0727")
	require.NoError(t, err)
	require.NotNil(t, strictGLM)
	for _, model := range []string{
		"glm-4.7-future-unpriced",
		"glm-5.1-preview",
		"glm-4.5-air-experimental",
	} {
		looseGLM, looseErr := svc.GetModelPricing(model)
		require.NoError(t, looseErr, model)
		require.NotNil(t, looseGLM, model)

		_, strictErr := svc.GetModelPricingStrict(model)
		require.ErrorIs(t, strictErr, ErrModelPricingUnavailable, model)
	}

	// 受支持的显式命名空间和 Kimi Code bare ID 是同一 SKU 的公开拼写，
	// 即使动态价格目录不可用也应命中严格 fallback。
	for _, model := range []string{"openai/gpt-5.4", "models/gpt-5.4", "k3", "k3-256k"} {
		strictAlias, aliasErr := svc.GetModelPricingStrict(model)
		require.NoError(t, aliasErr, model)
		require.NotNil(t, strictAlias, model)
	}

	// 相同尾段挂在任意 provider 下是另一个成本域，不能继承正价 fallback。
	_, err = svc.GetModelPricingStrict("future-provider/gpt-5.4")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// getFallbackPricing 的最后一条："剩下任何含 claude 的一律套 claude-sonnet-4"。
	// 宽松口径算得出数，严格口径不认。
	loose, err := svc.GetModelPricing("claude-future-unpriced-v99")
	require.NoError(t, err)
	require.NotNil(t, loose)

	_, err = svc.GetModelPricingStrict("claude-future-unpriced-v99")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// TestGatewayServiceValidateUsagePricing_StrictModelMatchMode 固定在线准入不能被
// 直接构造的 off/shadow 配置降级：所有档位都必须采用严格模型价。
func TestGatewayServiceValidateUsagePricing_StrictModelMatchMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		model     string
		wantError bool
	}{
		{
			name:      "off cannot admit a model priced only by keyword fallback",
			mode:      config.PricingGuardModeOff,
			model:     "claude-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "shadow cannot admit a model priced only by keyword fallback",
			mode:      config.PricingGuardModeShadow,
			model:     "claude-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "enforce rejects it",
			mode:      config.PricingGuardModeEnforce,
			model:     "claude-future-unpriced-v99",
			wantError: true,
		},
		{
			name:  "enforce still admits a model with its own fallback entry",
			mode:  config.PricingGuardModeEnforce,
			model: "claude-sonnet-4",
		},
		{
			// 正常配置加载会拒绝非法档位；运行时防线必须继续 fail-closed。
			name:      "unrecognized mode fails closed",
			mode:      "not-a-mode",
			model:     "claude-future-unpriced-v99",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGatewayPricingGuardService(&config.Config{
				RunMode: config.RunModeStandard,
				Pricing: config.PricingConfig{StrictModelMatchMode: tt.mode},
			})
			err := svc.ValidateUsagePricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformAnthropic},
				tt.model,
			)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGatewayServiceValidateUsagePricing_MiniMaxHonorsStrictModelMatchMode(t *testing.T) {
	for _, tt := range []struct {
		mode      string
		wantError bool
	}{
		{mode: config.PricingGuardModeOff, wantError: true},
		{mode: config.PricingGuardModeShadow, wantError: true},
		{mode: config.PricingGuardModeEnforce, wantError: true},
		{mode: "invalid-mode", wantError: true},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			svc := newGatewayPricingGuardService(&config.Config{
				RunMode: config.RunModeStandard,
				Pricing: config.PricingConfig{StrictModelMatchMode: tt.mode},
			})
			err := svc.ValidateUsagePricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformMiniMax},
				"MiniMax-M3-future",
			)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestGatewayServiceValidateUsagePricing_StrictEnforceKeepsExplicitChannelPrice 保证
// 收紧的只是"全局价目录认不认这个模型"。管理员在渠道上显式配过价的模型必须照常放行，
// 否则 enforce 会把一批配置完全正确的分组拒掉。
func TestGatewayServiceValidateUsagePricing_StrictEnforceKeepsExplicitChannelPrice(t *testing.T) {
	groupID := int64(131)
	model := "claude-future-unpriced-v99"

	zero := 0.0
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformAnthropic
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformAnthropic,
		model:    model,
	}] = &ChannelModelPricing{
		Platform:        PlatformAnthropic,
		BillingMode:     BillingModeToken,
		InputPrice:      &zero,
		OutputPrice:     &zero,
		CacheWritePrice: &zero,
		CacheReadPrice:  &zero,
	}
	cache.loadedAt = time.Now()
	channelService := &ChannelService{}
	channelService.cache.Store(cache)

	svc := newGatewayPricingGuardService(&config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{StrictModelMatchMode: config.PricingGuardModeEnforce},
	})
	svc.channelService = channelService
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic}}

	require.NoError(t, svc.ValidateUsagePricing(
		context.Background(),
		apiKey,
		&Account{Platform: PlatformAnthropic},
		model,
	))
}

func TestOpenAIGatewayServiceValidateSelectedPricing_StrictModelMatchMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		model     string
		wantError bool
	}{
		{
			name:      "off cannot admit a model that only reaches the default test model",
			mode:      config.PricingGuardModeOff,
			model:     "gpt-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "shadow cannot admit a model that only reaches the default test model",
			mode:      config.PricingGuardModeShadow,
			model:     "gpt-future-unpriced-v99",
			wantError: true,
		},
		{
			name:      "enforce rejects it",
			mode:      config.PricingGuardModeEnforce,
			model:     "gpt-future-unpriced-v99",
			wantError: true,
		},
		{
			name:  "enforce still admits the catalog entry itself",
			mode:  config.PricingGuardModeEnforce,
			model: "gpt-5.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				RunMode: config.RunModeStandard,
				Pricing: config.PricingConfig{StrictModelMatchMode: tt.mode},
			}
			svc := &OpenAIGatewayService{
				cfg:            cfg,
				billingService: NewBillingService(cfg, newStrictMatchCatalog()),
			}
			err := svc.ValidateSelectedOpenAIModelPricing(
				context.Background(),
				nil,
				&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				tt.model,
				false,
			)
			if tt.wantError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestStrictGlobalPricingGate(t *testing.T) {
	billingService := NewBillingService(&config.Config{}, newStrictMatchCatalog())

	t.Run("uses the strict verdict", func(t *testing.T) {
		gate := newStrictGlobalPricingGate(billingService, "gpt-future-unpriced-v99")
		require.False(t, gate.strict)
		require.False(t, gate.effective())
	})

	t.Run("admits an exact catalog entry", func(t *testing.T) {
		gate := newStrictGlobalPricingGate(billingService, "gpt-5.4")
		require.True(t, gate.effective())
	})

	t.Run("rejects an unpriced model", func(t *testing.T) {
		gate := newStrictGlobalPricingGate(billingService, "totally-unknown-v99")
		require.False(t, gate.effective())
	})

	t.Run("missing billing service is not priced", func(t *testing.T) {
		gate := newStrictGlobalPricingGate(nil, "gpt-5.4")
		require.False(t, gate.effective())
	})
}
