package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type memoryOverrideStore struct {
	mu   sync.Mutex
	rows map[string]ModelPriceOverride
	next int64
}

func newMemoryOverrideStore() *memoryOverrideStore {
	return &memoryOverrideStore{rows: map[string]ModelPriceOverride{}}
}

func overrideKey(platform, model string) string {
	return platform + "\x00" + model
}

func (s *memoryOverrideStore) List(context.Context) ([]ModelPriceOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ModelPriceOverride, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *memoryOverrideStore) Upsert(_ context.Context, row *ModelPriceOverride) (*ModelPriceOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved := *row
	if saved.ID == 0 {
		s.next++
		saved.ID = s.next
	}
	s.rows[overrideKey(saved.Platform, saved.ModelName)] = saved
	return &saved, nil
}

func (s *memoryOverrideStore) Delete(_ context.Context, platform, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := overrideKey(platform, model)
	if _, ok := s.rows[key]; !ok {
		return infraerrors.NotFound("NOT_FOUND", "override not found")
	}
	delete(s.rows, key)
	return nil
}

func pricedEntry(input, output float64) *ModelPriceEntry {
	return &ModelPriceEntry{
		InputCostPerToken:   input,
		OutputCostPerToken:  output,
		InputPriceExplicit:  true,
		OutputPriceExplicit: true,
		PricePresenceKnown:  true,
	}
}

func ptrPrice(v float64) *float64 { return &v }

func TestRebuildEffectiveDoesNotMutateCatalog(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  "*",
			ModelName: "claude-sonnet-4",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(9e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	require.InDelta(t, 1e-6, svc.catalogData["claude-sonnet-4"].InputCostPerToken, 1e-12)
	require.InDelta(t, 9e-6, svc.pricingData["claude-sonnet-4"].InputCostPerToken, 1e-12)

	svc.pricingData["claude-sonnet-4"].InputCostPerToken = 99
	require.InDelta(t, 1e-6, svc.catalogData["claude-sonnet-4"].InputCostPerToken, 1e-12)
}

func TestPlatformCandidateOverridesPrecedeSharedCatalog(t *testing.T) {
	const model = "deepseek-v4-flash-vision-exp"
	svc := NewPricingService(&config.Config{}, nil)
	svc.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: pricedEntry(1e-6, 2e-6),
	})
	svc.SeedOverridesForTest([]ModelPriceOverride{
		{
			Platform:  PlatformDeepSeek,
			ModelName: model,
			Enabled:   true,
			Payload: ModelPriceOverridePayload{
				InputCostPerToken:  ptrPrice(3e-6),
				OutputCostPerToken: ptrPrice(9e-6),
			},
		},
		{
			Platform:  PlatformComposite,
			ModelName: model,
			Enabled:   true,
			Payload: ModelPriceOverridePayload{
				InputCostPerToken:  ptrPrice(4e-6),
				OutputCostPerToken: ptrPrice(12e-6),
			},
		},
	})

	composite := svc.LookupModelPricingStrictForPlatforms(
		[]string{PlatformComposite, PlatformDeepSeek}, model,
	)
	require.NotNil(t, composite)
	require.InDelta(t, 4e-6, composite.InputCostPerToken, 1e-12)

	provider := svc.LookupModelPricingStrictForPlatforms(
		[]string{PlatformOpenAI, PlatformDeepSeek}, model,
	)
	require.NotNil(t, provider)
	require.InDelta(t, 3e-6, provider.InputCostPerToken, 1e-12)
}

func TestOverrideExplicitZeroIsPreserved(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  "*",
			ModelName: "claude-sonnet-4",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{OutputCostPerToken: ptrPrice(0)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	got := svc.GetModelPricing("claude-sonnet-4")
	require.NotNil(t, got)
	require.Equal(t, 0.0, got.OutputCostPerToken)
	require.True(t, got.OutputPriceExplicit)
}

func TestImageOnlyOverrideIsAllowed(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{}}
	_, err := svc.validateOverrideWrite("*", "gpt-image-1", ModelPriceCurrencyUSD, &ModelPriceOverridePayload{
		OutputCostPerImage: ptrPrice(0.04),
	}, true)
	require.NoError(t, err)
}

func TestIncompletePriorityOverrideRejected(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": pricedEntry(1e-6, 2e-6),
		},
	}
	_, err := svc.validateOverrideWrite("*", "claude-sonnet-4", ModelPriceCurrencyUSD, &ModelPriceOverridePayload{
		InputCostPerTokenPriority: ptrPrice(3e-6),
	}, true)
	require.Error(t, err)
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INCOMPLETE_PRICING", appErr.Reason)
}

func TestWildcardOverrideAppliesAcrossPlatforms(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"minimax-m2": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  "*",
			ModelName: "minimax-m2",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(8e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	require.InDelta(t, 8e-6, svc.GetModelPricingForPlatform("minimax", "minimax-m2").InputCostPerToken, 1e-12)
	require.InDelta(t, 8e-6, svc.GetModelPricingForPlatform("", "minimax-m2").InputCostPerToken, 1e-12)
}

func TestPlatformOverlayDoesNotLeak(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"shared-model": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  PlatformAnthropic,
			ModelName: "shared-model",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(7e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	require.InDelta(t, 7e-6, svc.GetModelPricingForPlatform(PlatformAnthropic, "shared-model").InputCostPerToken, 1e-12)
	require.InDelta(t, 1e-6, svc.GetModelPricingForPlatform(PlatformOpenAI, "shared-model").InputCostPerToken, 1e-12)
	require.InDelta(t, 1e-6, svc.GetModelPricing("shared-model").InputCostPerToken, 1e-12)
}

func TestListCatalogDoesNotCartesianProduct(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": {
				InputCostPerToken:      1e-6,
				OutputCostPerToken:     2e-6,
				InputPriceExplicit:     true,
				OutputPriceExplicit:    true,
				PricePresenceKnown:     true,
				PricingCatalogProvider: "anthropic",
			},
		},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	all := svc.ListCatalog(ModelPriceListQuery{Page: 1, PageSize: 50})
	require.Equal(t, 1, all.Total)
	anthropic := svc.ListCatalog(ModelPriceListQuery{Platform: PlatformAnthropic, Page: 1, PageSize: 50})
	require.Equal(t, 1, anthropic.Total)
	openai := svc.ListCatalog(ModelPriceListQuery{Platform: PlatformOpenAI, Page: 1, PageSize: 50})
	require.Equal(t, 0, openai.Total)
}

func TestListCatalogRestrictsToCallableModels(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": {
				InputCostPerToken:      1e-6,
				OutputCostPerToken:     2e-6,
				InputPriceExplicit:     true,
				OutputPriceExplicit:    true,
				PricePresenceKnown:     true,
				PricingCatalogProvider: "anthropic",
			},
			"gpt-unrelated": {
				InputCostPerToken:      3e-6,
				OutputCostPerToken:     4e-6,
				InputPriceExplicit:     true,
				OutputPriceExplicit:    true,
				PricePresenceKnown:     true,
				PricingCatalogProvider: "openai",
			},
		},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	got := svc.ListCatalog(ModelPriceListQuery{
		Page:     1,
		PageSize: 50,
		RestrictTo: []CallableModelRef{
			{Platform: PlatformAnthropic, Model: "claude-sonnet-4"},
		},
	})
	require.Equal(t, 1, got.Total)
	require.Equal(t, "claude-sonnet-4", got.Items[0].Model)
	require.Equal(t, ModelPriceSourceCatalog, got.Items[0].Source)
}

func TestListCatalogPlatformFilterDoesNotMaterializeWildcardOverrideFromAnotherPlatform(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4":   pricedEntry(1e-6, 2e-6),
			"deepseek-v4-flash": pricedEntry(3e-6, 9e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  ModelPriceOverrideWildcardPlatform,
			ModelName: "deepseek-v4-flash",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(4e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)

	callable := []CallableModelRef{
		{Platform: PlatformAntigravity, Model: "claude-sonnet-4"},
		{Platform: PlatformDeepSeek, Model: "deepseek-v4-flash"},
	}
	antigravity := svc.ListCatalog(ModelPriceListQuery{
		Platform:   PlatformAntigravity,
		Page:       1,
		PageSize:   50,
		RestrictTo: callable,
	})
	require.Equal(t, 1, antigravity.Total)
	require.Equal(t, "claude-sonnet-4", antigravity.Items[0].Model)

	deepseek := svc.ListCatalog(ModelPriceListQuery{
		Platform:   PlatformDeepSeek,
		Page:       1,
		PageSize:   50,
		RestrictTo: callable,
	})
	require.Equal(t, 1, deepseek.Total)
	require.Equal(t, "deepseek-v4-flash", deepseek.Items[0].Model)
	require.Equal(t, ModelPriceOverrideWildcardPlatform, deepseek.Items[0].OverridePlatform)
	require.InDelta(t, 4e-6, deepseek.Items[0].Effective["input_cost_per_token"], 1e-12)
}

func TestListCatalogUsesOfficialPriceWhenCatalogMissing(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	official := ModelPriceEntryFromOfficial("glm-4.7", &ModelPricing{
		InputPricePerToken:  0.6e-6,
		OutputPricePerToken: 2.2e-6,
	})
	got := svc.ListCatalog(ModelPriceListQuery{
		Page:     1,
		PageSize: 50,
		RestrictTo: []CallableModelRef{
			{Platform: PlatformGLM, Model: "glm-4.7"},
		},
		OfficialLookup: func(model string) *ModelPriceEntry {
			if model == "glm-4.7" {
				return official
			}
			return nil
		},
	})
	require.Equal(t, 1, got.Total)
	require.Equal(t, ModelPriceSourceOfficial, got.Items[0].Source)
	require.False(t, got.Items[0].TokenPricingAbsent)
	require.InDelta(t, 0.6e-6, got.Items[0].Effective["input_cost_per_token"], 1e-12)
}

func TestListCatalogAttachesDeepSeekOfficialTimeSchedule(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	official := ModelPriceEntryFromOfficial("deepseek-v4-flash", &ModelPricing{
		InputPricePerToken:  3e-6,
		OutputPricePerToken: 9e-6,
	})
	got := svc.ListCatalog(ModelPriceListQuery{
		Page:     1,
		PageSize: 50,
		RestrictTo: []CallableModelRef{
			{Platform: PlatformDeepSeek, Model: "deepseek-v4-flash"},
		},
		OfficialLookup: func(model string) *ModelPriceEntry {
			if model == "deepseek-v4-flash" {
				return official
			}
			return nil
		},
	})
	require.Equal(t, 1, got.Total)
	require.NotNil(t, got.Items[0].TimeSchedule)
	require.Equal(t, "deepseek_official", got.Items[0].TimeSchedule.Kind)
	// 目录里没有该 SKU，生效价回落到代码内官方兜底表：表里存的是高峰价。
	require.Equal(t, 1.0, got.Items[0].TimeSchedule.PeakMultiplier)
	require.Equal(t, 0.5, got.Items[0].TimeSchedule.OffPeakMultiplier)
}

// 目录（或管理端覆盖）里有该 SKU 时，生效价是官方空闲价，高峰要 ×2。
func TestListCatalogDeepSeekScheduleUsesOffPeakBaseWhenCatalogPriced(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{
		"deepseek-v4-flash": pricedEntry(0.14e-6, 0.28e-6),
	}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	got := svc.ListCatalog(ModelPriceListQuery{
		Page:     1,
		PageSize: 50,
		RestrictTo: []CallableModelRef{
			{Platform: PlatformDeepSeek, Model: "deepseek-v4-flash"},
		},
	})
	require.Equal(t, 1, got.Total)
	require.NotNil(t, got.Items[0].TimeSchedule)
	require.Equal(t, 2.0, got.Items[0].TimeSchedule.PeakMultiplier)
	require.Equal(t, 1.0, got.Items[0].TimeSchedule.OffPeakMultiplier)
}

func TestGetPriceDetailUsesOfficialWhenCatalogMissing(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	official := ModelPriceEntryFromOfficial("minimax-m2", &ModelPricing{
		InputPricePerToken:  0.3e-6,
		OutputPricePerToken: 1.2e-6,
	})
	detail, err := svc.GetPriceDetailWithOfficial(PlatformMiniMax, "minimax-m2", official)
	require.NoError(t, err)
	require.InDelta(t, 0.3e-6, detail.Catalog["input_cost_per_token"], 1e-12)
	require.InDelta(t, 0.3e-6, detail.Effective["input_cost_per_token"], 1e-12)
	require.False(t, detail.TokenPricingAbsent)
}

func TestGetPriceDetailAttachesDeepSeekOfficialTimeSchedule(t *testing.T) {
	svc := &PricingService{catalogData: map[string]*ModelPriceEntry{}}
	svc.rebuildEffectiveLocked(svc.catalogData)
	official := ModelPriceEntryFromOfficial("deepseek-v4-pro", &ModelPricing{
		InputPricePerToken:  9e-6,
		OutputPricePerToken: 27e-6,
	})
	detail, err := svc.GetPriceDetailWithOfficial(PlatformDeepSeek, "deepseek-v4-pro", official)
	require.NoError(t, err)
	require.NotNil(t, detail.TimeSchedule)
	require.Equal(t, "UTC", detail.TimeSchedule.Timezone)
	// 目录缺该 SKU，生效价来自代码内官方兜底表（高峰价）。
	require.Equal(t, 1.0, detail.TimeSchedule.PeakMultiplier)
	require.Equal(t, 0.5, detail.TimeSchedule.OffPeakMultiplier)
}

func TestResolvePlazaDisplayPriceUsesOverrideThenOfficial(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"deepseek-v4-flash": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  PlatformDeepSeek,
			ModelName: "deepseek-v4-flash",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(3e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)

	official := ModelPriceEntryFromOfficial("deepseek-v4-flash", &ModelPricing{
		InputPricePerToken:  9e-6,
		OutputPricePerToken: 27e-6,
	})
	resolution := ResolvePlazaDisplayPrice(svc, PlatformDeepSeek, "deepseek-v4-flash", official)
	require.NotNil(t, resolution)
	require.NotNil(t, resolution.Pricing)
	require.Equal(t, ModelPriceCurrencyUSD, resolution.Currency)
	require.Equal(t, ModelPriceSourceMerged, resolution.Source)
	require.InDelta(t, 3e-6, resolution.Pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 2e-6, resolution.Pricing.OutputCostPerToken, 1e-12)
	schedule := resolution.TimeSchedule
	// 目录价 / 手动覆盖价对官方分时 SKU 是空闲价，高峰 ×2。
	require.NotNil(t, schedule)
	require.Equal(t, 2.0, schedule.PeakMultiplier)
	require.Equal(t, 1.0, schedule.OffPeakMultiplier)

	// 目录没有该 SKU 时回落官方兜底表，那份存的是高峰价。
	missing := ResolvePlazaDisplayPrice(&PricingService{}, PlatformDeepSeek, "deepseek-v4-pro", official)
	require.NotNil(t, missing)
	require.Equal(t, official, missing.Pricing)
	require.Equal(t, ModelPriceSourceOfficial, missing.Source)
	officialSchedule := missing.TimeSchedule
	require.NotNil(t, officialSchedule)
	require.Equal(t, 1.0, officialSchedule.PeakMultiplier)
	require.Equal(t, 0.5, officialSchedule.OffPeakMultiplier)

	// 目录和官方表都没有：广场展示的是渠道自配价，不挂官方峰谷。
	none := ResolvePlazaDisplayPrice(&PricingService{}, PlatformDeepSeek, "deepseek-v4-pro", nil)
	require.Nil(t, none)
}

func TestSyncKeepsOverrides(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"claude-sonnet-4": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  "*",
			ModelName: "claude-sonnet-4",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(4e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	svc.catalogData["claude-sonnet-4"] = pricedEntry(3e-6, 5e-6)
	svc.rebuildEffectiveLocked(svc.catalogData)
	got := svc.GetModelPricing("claude-sonnet-4")
	require.InDelta(t, 4e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, got.OutputCostPerToken, 1e-12)
}

func TestGetPriceDetailAcceptsSlashModelAndFallsBackToWildcard(t *testing.T) {
	svc := &PricingService{
		catalogData: map[string]*ModelPriceEntry{
			"openai/gpt-5.4": pricedEntry(1e-6, 2e-6),
		},
		overrideRows: []ModelPriceOverride{{
			Platform:  "*",
			ModelName: "openai/gpt-5.4",
			Enabled:   true,
			Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(6e-6)},
		}},
	}
	svc.rebuildEffectiveLocked(svc.catalogData)
	detail, err := svc.GetPriceDetail(PlatformOpenAI, "openai/gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, "*", detail.OverridePlatform)
	require.NotNil(t, detail.Override)
	require.InDelta(t, 6e-6, *detail.Override.InputCostPerToken, 1e-12)
}

func TestUpsertAndStatusCountOverrides(t *testing.T) {
	store := newMemoryOverrideStore()
	svc := &PricingService{
		catalogData:   map[string]*ModelPriceEntry{},
		overrideStore: store,
	}
	_, err := svc.UpsertOverride(context.Background(), ModelPriceUpsertInput{
		Platform: "*",
		Model:    "minimax-m2",
		Payload:  ModelPriceOverridePayload{InputCostPerToken: ptrPrice(1e-6), OutputCostPerToken: ptrPrice(2e-6)},
	})
	require.NoError(t, err)
	require.Equal(t, ModelPriceCurrencyUSD, store.rows[overrideKey("*", "minimax-m2")].Currency)
	status := svc.GetStatus()
	require.Equal(t, 1, status["override_count"])
	require.Equal(t, 0, status["catalog_model_count"])
	require.Equal(t, 1, status["model_count"])
}

func TestUpsertPersistsAndReturnsCNYCurrency(t *testing.T) {
	store := newMemoryOverrideStore()
	svc := NewPricingService(&config.Config{}, nil)
	svc.SetOverrideDependencies(store, nil)

	result, err := svc.UpsertOverride(context.Background(), ModelPriceUpsertInput{
		Platform: PlatformZhipu,
		Model:    "glm-5.1",
		Currency: "cny",
		Payload: ModelPriceOverridePayload{
			InputCostPerToken:  ptrPrice(1.4e-6),
			OutputCostPerToken: ptrPrice(4.4e-6),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	require.Equal(t, ModelPriceCurrencyCNY, result.Override.Currency)
	require.Equal(t, ModelPriceCurrencyCNY, store.rows[overrideKey(PlatformZhipu, "glm-5.1")].Currency)

	detail, err := svc.GetPriceDetail(PlatformZhipu, "glm-5.1")
	require.NoError(t, err)
	require.Equal(t, ModelPriceCurrencyCNY, detail.Currency)
	require.Equal(t, ModelPriceCurrencyCNY, detail.OverrideCurrency)
	require.InDelta(t, 1.4e-6, detail.Effective["input_cost_per_token"], 1e-12)
	require.InDelta(t, 4.4e-6, detail.Effective["output_cost_per_token"], 1e-12)
}

func TestEmptyOverridePayloadIsAllowedAndInheritsCatalog(t *testing.T) {
	store := newMemoryOverrideStore()
	svc := NewPricingService(&config.Config{}, nil)
	svc.SetOverrideDependencies(store, nil)
	svc.SeedCatalogForTest(map[string]*ModelPriceEntry{
		"claude-sonnet-4": pricedEntry(3e-6, 15e-6),
	})

	result, err := svc.UpsertOverride(context.Background(), ModelPriceUpsertInput{
		Platform: PlatformAnthropic,
		Model:    "claude-sonnet-4",
		Currency: ModelPriceCurrencyUSD,
		Payload:  ModelPriceOverridePayload{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	require.Empty(t, payloadToMap(&result.Override.Payload))

	effective := svc.GetModelPricingForPlatform(PlatformAnthropic, "claude-sonnet-4")
	require.NotNil(t, effective)
	require.Equal(t, ModelPriceCurrencyUSD, effective.Currency)
	require.InDelta(t, 3e-6, effective.InputCostPerToken, 1e-12)
	require.InDelta(t, 15e-6, effective.OutputCostPerToken, 1e-12)
}

func TestDecodeOverridePayloadRejectsUnknownFields(t *testing.T) {
	_, err := DecodeModelPriceOverridePayload(json.RawMessage(`{"not_a_price":1}`))
	require.Error(t, err)
}

func TestDecodeOverridePayloadRejectsTrailingJSONValue(t *testing.T) {
	_, err := DecodeModelPriceOverridePayload(json.RawMessage(`{} {}`))
	require.Error(t, err)
}

func TestModelPriceCurrencyDefaultsAndCatalogParsing(t *testing.T) {
	require.Equal(t, ModelPriceCurrencyUSD, modelPriceCurrencyOrUSD(""))
	require.Equal(t, ModelPriceCurrencyCNY, modelPriceCurrencyOrUSD(" cny "))
	if _, err := NormalizeModelPriceCurrency("EUR"); err == nil {
		t.Fatal("expected unsupported currency to be rejected")
	}

	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(`{
		"usd-model": {"input_cost_per_token": 0.000001, "output_cost_per_token": 0.000002},
		"cny-model": {"currency": "CNY", "input_cost_per_token": 0.0000014, "output_cost_per_token": 0.0000044}
	}`))
	require.NoError(t, err)
	require.Equal(t, ModelPriceCurrencyUSD, data["usd-model"].Currency)
	require.Equal(t, ModelPriceCurrencyCNY, data["cny-model"].Currency)
}

func TestCrossCurrencyOverrideReplacesCatalogInsteadOfMerging(t *testing.T) {
	svc := &PricingService{}
	svc.SeedCatalogForTest(map[string]*ModelPriceEntry{
		"glm-5.1": pricedEntry(1e-6, 4e-6),
	})
	svc.SeedOverridesForTest([]ModelPriceOverride{{
		Platform:  PlatformZhipu,
		ModelName: "glm-5.1",
		Currency:  ModelPriceCurrencyCNY,
		Enabled:   true,
		Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(1.4e-6)},
	}})

	effective := svc.GetModelPricingForPlatform(PlatformZhipu, "glm-5.1")
	require.NotNil(t, effective)
	require.Equal(t, ModelPriceCurrencyCNY, effective.Currency)
	require.InDelta(t, 1.4e-6, effective.InputCostPerToken, 1e-12)
	require.Zero(t, effective.OutputCostPerToken)
	require.True(t, effective.TokenPricingAbsent)
}

func TestSameCurrencyOverrideStillMergesCatalog(t *testing.T) {
	svc := &PricingService{}
	svc.SeedCatalogForTest(map[string]*ModelPriceEntry{
		"claude-sonnet-4": pricedEntry(1e-6, 2e-6),
	})
	svc.SeedOverridesForTest([]ModelPriceOverride{{
		Platform:  PlatformAnthropic,
		ModelName: "claude-sonnet-4",
		Currency:  ModelPriceCurrencyUSD,
		Enabled:   true,
		Payload:   ModelPriceOverridePayload{InputCostPerToken: ptrPrice(3e-6)},
	}})

	effective := svc.GetModelPricingForPlatform(PlatformAnthropic, "claude-sonnet-4")
	require.NotNil(t, effective)
	require.Equal(t, ModelPriceCurrencyUSD, effective.Currency)
	require.InDelta(t, 3e-6, effective.InputCostPerToken, 1e-12)
	require.InDelta(t, 2e-6, effective.OutputCostPerToken, 1e-12)
}

func TestBillingIgnoresModelPriceDisplayCurrency(t *testing.T) {
	pricing := NewPricingService(&config.Config{}, nil)
	usd := pricedEntry(1.4e-6, 4.4e-6)
	usd.Currency = ModelPriceCurrencyUSD
	cny := pricedEntry(1.4e-6, 4.4e-6)
	cny.Currency = ModelPriceCurrencyCNY
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		"same-numeric-usd": usd,
		"same-numeric-cny": cny,
	})
	billing := NewBillingService(&config.Config{}, pricing)
	tokens := UsageTokens{InputTokens: 2_000_000, OutputTokens: 500_000}

	usdCost, err := billing.CalculateCost("same-numeric-usd", tokens, 0.6)
	require.NoError(t, err)
	cnyCost, err := billing.CalculateCost("same-numeric-cny", tokens, 0.6)
	require.NoError(t, err)
	require.InDelta(t, usdCost.TotalCost, cnyCost.TotalCost, 1e-12)
	require.InDelta(t, usdCost.ActualCost, cnyCost.ActualCost, 1e-12)
}
