package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

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
	_, err := svc.validateOverrideWrite("*", "gpt-image-1", &ModelPriceOverridePayload{
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
	_, err := svc.validateOverrideWrite("*", "claude-sonnet-4", &ModelPriceOverridePayload{
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
	require.Equal(t, "Asia/Shanghai", detail.TimeSchedule.Timezone)
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
	entry, schedule := ResolvePlazaDisplayPrice(svc, PlatformDeepSeek, "deepseek-v4-flash", official)
	require.NotNil(t, entry)
	require.InDelta(t, 3e-6, entry.InputCostPerToken, 1e-12)
	require.InDelta(t, 2e-6, entry.OutputCostPerToken, 1e-12)
	// 目录价 / 手动覆盖价对官方分时 SKU 是空闲价，高峰 ×2。
	require.NotNil(t, schedule)
	require.Equal(t, 2.0, schedule.PeakMultiplier)
	require.Equal(t, 1.0, schedule.OffPeakMultiplier)

	// 目录没有该 SKU 时回落官方兜底表，那份存的是高峰价。
	missing, officialSchedule := ResolvePlazaDisplayPrice(&PricingService{}, PlatformDeepSeek, "deepseek-v4-pro", official)
	require.Equal(t, official, missing)
	require.NotNil(t, officialSchedule)
	require.Equal(t, 1.0, officialSchedule.PeakMultiplier)
	require.Equal(t, 0.5, officialSchedule.OffPeakMultiplier)

	// 目录和官方表都没有：广场展示的是渠道自配价，不挂官方峰谷。
	none, noSchedule := ResolvePlazaDisplayPrice(&PricingService{}, PlatformDeepSeek, "deepseek-v4-pro", nil)
	require.Nil(t, none)
	require.Nil(t, noSchedule)
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
	status := svc.GetStatus()
	require.Equal(t, 1, status["override_count"])
	require.Equal(t, 0, status["catalog_model_count"])
	require.Equal(t, 1, status["model_count"])
}

func TestDecodeOverridePayloadRejectsUnknownFields(t *testing.T) {
	_, err := DecodeModelPriceOverridePayload(json.RawMessage(`{"not_a_price":1}`))
	require.Error(t, err)
}
