package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ModelPriceOverrideWildcardPlatform = "*"
	ModelPriceCurrencyUSD              = "USD"
	ModelPriceCurrencyCNY              = "CNY"

	ModelPriceSourceCatalog  = "catalog"
	ModelPriceSourceOverride = "override"
	ModelPriceSourceMerged   = "merged"
	ModelPriceSourceOfficial = "official"
	ModelPriceSourceChannel  = "channel"
)

func NormalizeModelPriceCurrency(currency string) (string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return ModelPriceCurrencyUSD, nil
	}
	switch currency {
	case ModelPriceCurrencyUSD, ModelPriceCurrencyCNY:
		return currency, nil
	default:
		return "", infraerrors.BadRequest("INVALID_CURRENCY", "model price currency must be USD or CNY")
	}
}

func modelPriceCurrencyOrUSD(currency string) string {
	normalized, err := NormalizeModelPriceCurrency(currency)
	if err != nil {
		return ModelPriceCurrencyUSD
	}
	return normalized
}

func modelPriceEntryCurrency(entry *ModelPriceEntry) string {
	if entry == nil {
		return ""
	}
	return modelPriceCurrencyOrUSD(entry.Currency)
}

// PlatformToPricingCatalogProvider maps gateway platform names to the provider
// value used by the synchronized model-price catalog.
// 缺失的映射会让「按平台筛选」在目录侧一条都匹配不到（见 ListCatalog 的
// provider == "" 分支），只剩已有的覆盖行。kimi/zhipu/minimax 在同步目录里本来
// 就没有条目（价格全部来自 fallbackPrices），无需也无从映射。
var PlatformToPricingCatalogProvider = map[string]string{
	PlatformAnthropic:   "anthropic",
	PlatformOpenAI:      "openai",
	PlatformGemini:      "google",
	PlatformAntigravity: "anthropic",
	PlatformGrok:        "xai",
	PlatformDeepSeek:    "deepseek",
}

// ModelPriceOverridePayload is a field-level patch. Nil means inherit while a
// non-nil pointer, including a pointer to zero, is an explicit override.
type ModelPriceOverridePayload struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken *float64 `json:"output_cost_per_token,omitempty"`

	CacheCreationInputTokenCost         *float64 `json:"cache_creation_input_token_cost,omitempty"`
	CacheCreationInputTokenCostAbove1hr *float64 `json:"cache_creation_input_token_cost_above_1hr,omitempty"`
	CacheReadInputTokenCost             *float64 `json:"cache_read_input_token_cost,omitempty"`

	InputCostPerTokenPriority           *float64 `json:"input_cost_per_token_priority,omitempty"`
	OutputCostPerTokenPriority          *float64 `json:"output_cost_per_token_priority,omitempty"`
	CacheCreationInputTokenCostPriority *float64 `json:"cache_creation_input_token_cost_priority,omitempty"`
	CacheReadInputTokenCostPriority     *float64 `json:"cache_read_input_token_cost_priority,omitempty"`

	LongContextInputTokenThreshold  *int     `json:"long_context_input_token_threshold,omitempty"`
	LongContextInputCostMultiplier  *float64 `json:"long_context_input_cost_multiplier,omitempty"`
	LongContextOutputCostMultiplier *float64 `json:"long_context_output_cost_multiplier,omitempty"`

	OutputCostPerImage      *float64 `json:"output_cost_per_image,omitempty"`
	OutputCostPerImageToken *float64 `json:"output_cost_per_image_token,omitempty"`
	InputCostPerImageToken  *float64 `json:"input_cost_per_image_token,omitempty"`

	SupportsServiceTier    *bool   `json:"supports_service_tier,omitempty"`
	SupportsPromptCaching  *bool   `json:"supports_prompt_caching,omitempty"`
	PricingCatalogProvider *string `json:"litellm_provider,omitempty"`
	Mode                   *string `json:"mode,omitempty"`
}

type ModelPriceOverride struct {
	ID        int64                     `json:"id"`
	Platform  string                    `json:"platform"`
	ModelName string                    `json:"model_name"`
	Currency  string                    `json:"currency"`
	Payload   ModelPriceOverridePayload `json:"payload"`
	Enabled   bool                      `json:"enabled"`
	Note      *string                   `json:"note,omitempty"`
	UpdatedBy *int64                    `json:"updated_by,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

type ModelPriceOverrideStore interface {
	List(ctx context.Context) ([]ModelPriceOverride, error)
	Upsert(ctx context.Context, row *ModelPriceOverride) (*ModelPriceOverride, error)
	Delete(ctx context.Context, platform, model string) error
}

type ModelPriceOverrideCache interface {
	BroadcastRefresh(ctx context.Context) error
	SubscribeRefresh(ctx context.Context, fn func())
}

type CallableModelRef struct {
	Platform string
	Model    string
}

type ModelPriceListQuery struct {
	Platform       string
	Query          string
	Status         string
	Page           int
	PageSize       int
	RestrictTo     []CallableModelRef
	OfficialLookup func(model string) *ModelPriceEntry
}

// ModelPriceTimeSchedule 描述官方分时定价规则。PeakMultiplier / OffPeakMultiplier
// 都是相对随行的那份基准价：目录价 / 管理端生效价存的是空闲价（1 / 2），代码内官方
// 兜底表存的是高峰价（1 / 0.5）。展示侧一律用这两个倍率换算，不要假设基准价是哪一档。
type ModelPriceTimeSchedule struct {
	Kind              string   `json:"kind"`
	Timezone          string   `json:"timezone"`
	PeakWindows       []string `json:"peak_windows"`
	PeakMultiplier    float64  `json:"peak_multiplier"`
	OffPeakMultiplier float64  `json:"off_peak_multiplier"`
}

type PlazaDisplayPriceResolution struct {
	Pricing      *ModelPriceEntry        `json:"-"`
	Currency     string                  `json:"currency"`
	Source       string                  `json:"source"`
	TimeSchedule *ModelPriceTimeSchedule `json:"time_schedule,omitempty"`
}

type ModelPriceListItem struct {
	Platform           string                  `json:"platform"`
	Model              string                  `json:"model"`
	Source             string                  `json:"source"`
	Currency           string                  `json:"currency"`
	CatalogCurrency    string                  `json:"catalog_currency,omitempty"`
	OverrideCurrency   string                  `json:"override_currency,omitempty"`
	TokenPricingAbsent bool                    `json:"token_pricing_absent"`
	HasImagePricing    bool                    `json:"has_image_pricing"`
	SyncInvalidated    bool                    `json:"sync_invalidated"`
	Redundant          bool                    `json:"redundant"`
	Effective          map[string]any          `json:"effective"`
	OverriddenFields   []string                `json:"overridden_fields"`
	Enabled            bool                    `json:"enabled"`
	OverridePlatform   string                  `json:"override_platform,omitempty"`
	Note               *string                 `json:"note,omitempty"`
	UpdatedBy          *int64                  `json:"updated_by,omitempty"`
	UpdatedAt          *time.Time              `json:"updated_at,omitempty"`
	TimeSchedule       *ModelPriceTimeSchedule `json:"time_schedule,omitempty"`
}

type ModelPriceListResult struct {
	Items []ModelPriceListItem
	Total int
}

type ModelPriceDetail struct {
	Platform           string                     `json:"platform"`
	Model              string                     `json:"model"`
	Currency           string                     `json:"currency"`
	CatalogCurrency    string                     `json:"catalog_currency,omitempty"`
	OverrideCurrency   string                     `json:"override_currency,omitempty"`
	Catalog            map[string]any             `json:"catalog"`
	Override           *ModelPriceOverridePayload `json:"override,omitempty"`
	Effective          map[string]any             `json:"effective"`
	Enabled            bool                       `json:"enabled"`
	TokenPricingAbsent bool                       `json:"token_pricing_absent"`
	HasImagePricing    bool                       `json:"has_image_pricing"`
	SyncInvalidated    bool                       `json:"sync_invalidated"`
	Redundant          bool                       `json:"redundant"`
	OverridePlatform   string                     `json:"override_platform,omitempty"`
	Note               *string                    `json:"note,omitempty"`
	UpdatedBy          *int64                     `json:"updated_by,omitempty"`
	UpdatedAt          *time.Time                 `json:"updated_at,omitempty"`
	TimeSchedule       *ModelPriceTimeSchedule    `json:"time_schedule,omitempty"`
}

type ModelPriceUpsertInput struct {
	Platform  string
	Model     string
	Currency  string
	Payload   ModelPriceOverridePayload
	Enabled   *bool
	Note      *string
	UpdatedBy *int64
}

type ModelPriceWarning struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

type ModelPriceUpsertResult struct {
	Override *ModelPriceOverride `json:"override"`
	Warnings []ModelPriceWarning `json:"warnings,omitempty"`
}

func normalizePricingModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// normalizeOverridePlatform 是覆盖价平台 ID 的唯一入口（写入、校验、overlay 索引共用）。
// 归一 glm→zhipu：229 迁移后账号平台一律是 zhipu，历史 glm 覆盖行仍能被查到。
func normalizeOverridePlatform(platform string) string {
	return CanonicalCNPlatform(strings.ToLower(strings.TrimSpace(platform)))
}

func CatalogOverridePlatforms() []string {
	return []string{
		ModelPriceOverrideWildcardPlatform,
		PlatformAnthropic,
		PlatformOpenAI,
		PlatformGemini,
		PlatformAntigravity,
		PlatformGrok,
		PlatformMiniMax,
		// 229 迁移后 DB 里的智谱账号平台是 zhipu；glm 由 normalizeOverridePlatform
		// 归一到 zhipu，不再单列，否则管理端能选中一个匹配不到任何账号的平台。
		PlatformZhipu,
		PlatformKimi,
		PlatformDeepSeek,
		PlatformWindsurf,
		PlatformOpenCode,
		PlatformComposite,
	}
}

func validOverridePlatform(platform string) bool {
	for _, allowed := range CatalogOverridePlatforms() {
		if platform == allowed {
			return true
		}
	}
	return false
}

func catalogProviderForPlatform(platform string) string {
	return PlatformToPricingCatalogProvider[normalizeOverridePlatform(platform)]
}

func DecodeModelPriceOverridePayload(raw json.RawMessage) (ModelPriceOverridePayload, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return ModelPriceOverridePayload{}, infraerrors.BadRequest("INVALID_PAYLOAD", "invalid model price payload").WithCause(err)
	}
	for key, value := range generic {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ModelPriceOverridePayload{}, infraerrors.BadRequest("INVALID_PAYLOAD", fmt.Sprintf("%s cannot be null", key))
		}
	}
	var payload ModelPriceOverridePayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return ModelPriceOverridePayload{}, infraerrors.BadRequest("INVALID_PAYLOAD", "invalid model price payload").WithCause(err)
	}
	if decoder.More() {
		return ModelPriceOverridePayload{}, infraerrors.BadRequest("INVALID_PAYLOAD", "invalid trailing JSON")
	}
	return payload, nil
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func rawOf(entry *ModelPriceEntry) *RawModelPriceEntry {
	raw := &RawModelPriceEntry{Currency: ModelPriceCurrencyUSD}
	if entry == nil {
		return raw
	}
	raw.Currency = modelPriceEntryCurrency(entry)
	if entry.InputPriceExplicit || (!entry.PricePresenceKnown && entry.InputCostPerToken != 0) {
		raw.InputCostPerToken = catalogPriceFloat64(entry.InputCostPerToken)
	}
	if entry.OutputPriceExplicit || (!entry.PricePresenceKnown && entry.OutputCostPerToken != 0) {
		raw.OutputCostPerToken = catalogPriceFloat64(entry.OutputCostPerToken)
	}
	if entry.InputPriorityPriceExplicit || (!entry.PricePresenceKnown && entry.InputCostPerTokenPriority != 0) {
		raw.InputCostPerTokenPriority = catalogPriceFloat64(entry.InputCostPerTokenPriority)
	}
	if entry.OutputPriorityPriceExplicit || (!entry.PricePresenceKnown && entry.OutputCostPerTokenPriority != 0) {
		raw.OutputCostPerTokenPriority = catalogPriceFloat64(entry.OutputCostPerTokenPriority)
	}
	if entry.CacheCreationPriceExplicit || (!entry.PricePresenceKnown && entry.CacheCreationInputTokenCost != 0) {
		raw.CacheCreationInputTokenCost = catalogPriceFloat64(entry.CacheCreationInputTokenCost)
	}
	if entry.CacheCreationAbove1hrPriceExplicit || (!entry.PricePresenceKnown && entry.CacheCreationInputTokenCostAbove1hr != 0) {
		raw.CacheCreationInputTokenCostAbove1hr = catalogPriceFloat64(entry.CacheCreationInputTokenCostAbove1hr)
	}
	if entry.CacheReadPriceExplicit || (!entry.PricePresenceKnown && entry.CacheReadInputTokenCost != 0) {
		raw.CacheReadInputTokenCost = catalogPriceFloat64(entry.CacheReadInputTokenCost)
	}
	if entry.CacheCreationPriorityPriceExplicit || (!entry.PricePresenceKnown && entry.CacheCreationInputTokenCostPriority != 0) {
		raw.CacheCreationInputTokenCostPriority = catalogPriceFloat64(entry.CacheCreationInputTokenCostPriority)
	}
	if entry.CacheReadPriorityPriceExplicit || (!entry.PricePresenceKnown && entry.CacheReadInputTokenCostPriority != 0) {
		raw.CacheReadInputTokenCostPriority = catalogPriceFloat64(entry.CacheReadInputTokenCostPriority)
	}
	if entry.LongContextPricingExplicit || entry.LongContextInputTokenThreshold != 0 {
		raw.LongContextInputTokenThreshold = catalogPriceInt(entry.LongContextInputTokenThreshold)
		raw.LongContextInputCostMultiplier = catalogPriceFloat64(entry.LongContextInputCostMultiplier)
		raw.LongContextOutputCostMultiplier = catalogPriceFloat64(entry.LongContextOutputCostMultiplier)
	}
	if entry.OutputCostPerImageExplicit || (!entry.PricePresenceKnown && entry.OutputCostPerImage != 0) {
		raw.OutputCostPerImage = catalogPriceFloat64(entry.OutputCostPerImage)
	}
	if entry.ImageOutputPriceExplicit || (!entry.PricePresenceKnown && entry.OutputCostPerImageToken != 0) {
		raw.OutputCostPerImageToken = catalogPriceFloat64(entry.OutputCostPerImageToken)
	}
	if entry.ImageInputPriceExplicit || (!entry.PricePresenceKnown && entry.InputCostPerImageToken != 0) {
		raw.InputCostPerImageToken = catalogPriceFloat64(entry.InputCostPerImageToken)
	}
	raw.SupportsServiceTier = entry.SupportsServiceTier
	raw.SupportsPromptCaching = entry.SupportsPromptCaching
	raw.PricingCatalogProvider = entry.PricingCatalogProvider
	raw.Mode = entry.Mode
	return raw
}

func mergeRawPriceEntry(base *RawModelPriceEntry, patch *ModelPriceOverridePayload) *RawModelPriceEntry {
	out := &RawModelPriceEntry{}
	if base != nil {
		*out = *base
		out.InputCostPerToken = cloneFloat(base.InputCostPerToken)
		out.OutputCostPerToken = cloneFloat(base.OutputCostPerToken)
		out.InputCostPerTokenPriority = cloneFloat(base.InputCostPerTokenPriority)
		out.OutputCostPerTokenPriority = cloneFloat(base.OutputCostPerTokenPriority)
		out.CacheCreationInputTokenCost = cloneFloat(base.CacheCreationInputTokenCost)
		out.CacheCreationInputTokenCostAbove1hr = cloneFloat(base.CacheCreationInputTokenCostAbove1hr)
		out.CacheReadInputTokenCost = cloneFloat(base.CacheReadInputTokenCost)
		out.CacheCreationInputTokenCostPriority = cloneFloat(base.CacheCreationInputTokenCostPriority)
		out.CacheReadInputTokenCostPriority = cloneFloat(base.CacheReadInputTokenCostPriority)
		out.LongContextInputTokenThreshold = cloneInt(base.LongContextInputTokenThreshold)
		out.LongContextInputCostMultiplier = cloneFloat(base.LongContextInputCostMultiplier)
		out.LongContextOutputCostMultiplier = cloneFloat(base.LongContextOutputCostMultiplier)
		out.OutputCostPerImage = cloneFloat(base.OutputCostPerImage)
		out.OutputCostPerImageToken = cloneFloat(base.OutputCostPerImageToken)
		out.InputCostPerImageToken = cloneFloat(base.InputCostPerImageToken)
	}
	if patch == nil {
		return out
	}
	if patch.InputCostPerToken != nil {
		out.InputCostPerToken = cloneFloat(patch.InputCostPerToken)
	}
	if patch.OutputCostPerToken != nil {
		out.OutputCostPerToken = cloneFloat(patch.OutputCostPerToken)
	}
	if patch.InputCostPerTokenPriority != nil {
		out.InputCostPerTokenPriority = cloneFloat(patch.InputCostPerTokenPriority)
	}
	if patch.OutputCostPerTokenPriority != nil {
		out.OutputCostPerTokenPriority = cloneFloat(patch.OutputCostPerTokenPriority)
	}
	if patch.CacheCreationInputTokenCost != nil {
		out.CacheCreationInputTokenCost = cloneFloat(patch.CacheCreationInputTokenCost)
	}
	if patch.CacheCreationInputTokenCostAbove1hr != nil {
		out.CacheCreationInputTokenCostAbove1hr = cloneFloat(patch.CacheCreationInputTokenCostAbove1hr)
	}
	if patch.CacheReadInputTokenCost != nil {
		out.CacheReadInputTokenCost = cloneFloat(patch.CacheReadInputTokenCost)
	}
	if patch.CacheCreationInputTokenCostPriority != nil {
		out.CacheCreationInputTokenCostPriority = cloneFloat(patch.CacheCreationInputTokenCostPriority)
	}
	if patch.CacheReadInputTokenCostPriority != nil {
		out.CacheReadInputTokenCostPriority = cloneFloat(patch.CacheReadInputTokenCostPriority)
	}
	if patch.LongContextInputTokenThreshold != nil {
		out.LongContextInputTokenThreshold = cloneInt(patch.LongContextInputTokenThreshold)
	}
	if patch.LongContextInputCostMultiplier != nil {
		out.LongContextInputCostMultiplier = cloneFloat(patch.LongContextInputCostMultiplier)
	}
	if patch.LongContextOutputCostMultiplier != nil {
		out.LongContextOutputCostMultiplier = cloneFloat(patch.LongContextOutputCostMultiplier)
	}
	if patch.OutputCostPerImage != nil {
		out.OutputCostPerImage = cloneFloat(patch.OutputCostPerImage)
	}
	if patch.OutputCostPerImageToken != nil {
		out.OutputCostPerImageToken = cloneFloat(patch.OutputCostPerImageToken)
	}
	if patch.InputCostPerImageToken != nil {
		out.InputCostPerImageToken = cloneFloat(patch.InputCostPerImageToken)
	}
	if patch.SupportsServiceTier != nil {
		out.SupportsServiceTier = *patch.SupportsServiceTier
	}
	if patch.SupportsPromptCaching != nil {
		out.SupportsPromptCaching = *patch.SupportsPromptCaching
	}
	if patch.PricingCatalogProvider != nil {
		out.PricingCatalogProvider = strings.TrimSpace(*patch.PricingCatalogProvider)
	}
	if patch.Mode != nil {
		out.Mode = strings.TrimSpace(*patch.Mode)
	}
	return out
}

func overrideBaseRaw(entry *ModelPriceEntry, currency string) *RawModelPriceEntry {
	currency = modelPriceCurrencyOrUSD(currency)
	if entry == nil || modelPriceEntryCurrency(entry) != currency {
		return &RawModelPriceEntry{Currency: currency}
	}
	raw := rawOf(entry)
	raw.Currency = currency
	return raw
}

func buildOverrideModelPriceEntry(model string, base *ModelPriceEntry, row *ModelPriceOverride) *ModelPriceEntry {
	if row == nil {
		return base
	}
	currency := modelPriceCurrencyOrUSD(row.Currency)
	raw := mergeRawPriceEntry(overrideBaseRaw(base, currency), &row.Payload)
	raw.Currency = currency
	return buildModelPriceEntry(model, raw)
}

func buildModelPriceEntry(model string, raw *RawModelPriceEntry) *ModelPriceEntry {
	if raw == nil {
		return nil
	}
	entry := &ModelPriceEntry{
		Currency:                           modelPriceCurrencyOrUSD(raw.Currency),
		PricingCatalogProvider:             raw.PricingCatalogProvider,
		Mode:                               raw.Mode,
		SupportsPromptCaching:              raw.SupportsPromptCaching,
		SupportsServiceTier:                raw.SupportsServiceTier,
		TokenPricingAbsent:                 rawModelTokenPricingIncomplete(model, raw),
		OutputCostPerImageExplicit:         raw.OutputCostPerImage != nil,
		ImageOutputPriceExplicit:           raw.OutputCostPerImageToken != nil,
		ImageInputPriceExplicit:            raw.InputCostPerImageToken != nil,
		InputPriceExplicit:                 raw.InputCostPerToken != nil,
		OutputPriceExplicit:                raw.OutputCostPerToken != nil,
		PricePresenceKnown:                 true,
		CacheCreationPriceExplicit:         raw.CacheCreationInputTokenCost != nil,
		CacheCreationAbove1hrPriceExplicit: raw.CacheCreationInputTokenCostAbove1hr != nil,
		CacheReadPriceExplicit:             raw.CacheReadInputTokenCost != nil,
		InputPriorityPriceExplicit:         raw.InputCostPerTokenPriority != nil,
		OutputPriorityPriceExplicit:        raw.OutputCostPerTokenPriority != nil,
		CacheCreationPriorityPriceExplicit: raw.CacheCreationInputTokenCostPriority != nil,
		CacheReadPriorityPriceExplicit:     raw.CacheReadInputTokenCostPriority != nil,
		LongContextPricingExplicit: raw.LongContextInputTokenThreshold != nil ||
			raw.LongContextInputCostMultiplier != nil ||
			raw.LongContextOutputCostMultiplier != nil,
	}
	if raw.InputCostPerToken != nil {
		entry.InputCostPerToken = *raw.InputCostPerToken
	}
	if raw.OutputCostPerToken != nil {
		entry.OutputCostPerToken = *raw.OutputCostPerToken
	}
	if raw.InputCostPerTokenPriority != nil {
		entry.InputCostPerTokenPriority = *raw.InputCostPerTokenPriority
	}
	if raw.OutputCostPerTokenPriority != nil {
		entry.OutputCostPerTokenPriority = *raw.OutputCostPerTokenPriority
	}
	if raw.CacheCreationInputTokenCost != nil {
		entry.CacheCreationInputTokenCost = *raw.CacheCreationInputTokenCost
	}
	if raw.CacheCreationInputTokenCostAbove1hr != nil {
		entry.CacheCreationInputTokenCostAbove1hr = *raw.CacheCreationInputTokenCostAbove1hr
	}
	if raw.CacheReadInputTokenCost != nil {
		entry.CacheReadInputTokenCost = *raw.CacheReadInputTokenCost
	}
	if raw.CacheCreationInputTokenCostPriority != nil {
		entry.CacheCreationInputTokenCostPriority = *raw.CacheCreationInputTokenCostPriority
	}
	if raw.CacheReadInputTokenCostPriority != nil {
		entry.CacheReadInputTokenCostPriority = *raw.CacheReadInputTokenCostPriority
	}
	if raw.LongContextInputTokenThreshold != nil {
		entry.LongContextInputTokenThreshold = *raw.LongContextInputTokenThreshold
	}
	if raw.LongContextInputCostMultiplier != nil {
		entry.LongContextInputCostMultiplier = *raw.LongContextInputCostMultiplier
	}
	if raw.LongContextOutputCostMultiplier != nil {
		entry.LongContextOutputCostMultiplier = *raw.LongContextOutputCostMultiplier
	}
	if raw.OutputCostPerImage != nil {
		entry.OutputCostPerImage = *raw.OutputCostPerImage
	}
	if raw.OutputCostPerImageToken != nil {
		entry.OutputCostPerImageToken = *raw.OutputCostPerImageToken
	}
	if raw.InputCostPerImageToken != nil {
		entry.InputCostPerImageToken = *raw.InputCostPerImageToken
	}
	return entry
}

func cloneCatalog(catalog map[string]*ModelPriceEntry) map[string]*ModelPriceEntry {
	out := make(map[string]*ModelPriceEntry, len(catalog))
	for model, entry := range catalog {
		if entry == nil {
			continue
		}
		cp := *entry
		cp.Currency = modelPriceEntryCurrency(entry)
		out[normalizePricingModelKey(model)] = &cp
	}
	return out
}

func pricePointers(payload *ModelPriceOverridePayload) map[string]*float64 {
	if payload == nil {
		return nil
	}
	return map[string]*float64{
		"input_cost_per_token":                      payload.InputCostPerToken,
		"output_cost_per_token":                     payload.OutputCostPerToken,
		"cache_creation_input_token_cost":           payload.CacheCreationInputTokenCost,
		"cache_creation_input_token_cost_above_1hr": payload.CacheCreationInputTokenCostAbove1hr,
		"cache_read_input_token_cost":               payload.CacheReadInputTokenCost,
		"input_cost_per_token_priority":             payload.InputCostPerTokenPriority,
		"output_cost_per_token_priority":            payload.OutputCostPerTokenPriority,
		"cache_creation_input_token_cost_priority":  payload.CacheCreationInputTokenCostPriority,
		"cache_read_input_token_cost_priority":      payload.CacheReadInputTokenCostPriority,
		"long_context_input_cost_multiplier":        payload.LongContextInputCostMultiplier,
		"long_context_output_cost_multiplier":       payload.LongContextOutputCostMultiplier,
		"output_cost_per_image":                     payload.OutputCostPerImage,
		"output_cost_per_image_token":               payload.OutputCostPerImageToken,
		"input_cost_per_image_token":                payload.InputCostPerImageToken,
	}
}

func validatePayloadNumbers(payload *ModelPriceOverridePayload) error {
	for field, value := range pricePointers(payload) {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return infraerrors.BadRequest("INVALID_PAYLOAD", fmt.Sprintf("%s must be finite and non-negative", field)).
				WithMetadata(map[string]string{"field": field})
		}
	}
	if payload != nil && payload.LongContextInputTokenThreshold != nil && *payload.LongContextInputTokenThreshold <= 0 {
		return infraerrors.BadRequest("INVALID_PAYLOAD", "long_context_input_token_threshold must be positive").
			WithMetadata(map[string]string{"field": "long_context_input_token_threshold"})
	}
	return nil
}

func incompleteDimension(raw *RawModelPriceEntry) string {
	if raw == nil {
		return "input_cost_per_token"
	}
	if raw.InputCostPerToken == nil {
		return "input_cost_per_token"
	}
	if raw.OutputCostPerToken == nil {
		return "output_cost_per_token"
	}
	cacheDeclared := raw.SupportsPromptCaching || raw.CacheCreationInputTokenCost != nil ||
		raw.CacheCreationInputTokenCostAbove1hr != nil || raw.CacheReadInputTokenCost != nil
	if cacheDeclared && raw.CacheReadInputTokenCost == nil {
		return "cache_read_input_token_cost"
	}
	if cacheDeclared && raw.CacheCreationInputTokenCost == nil &&
		!isOpenAIGPT56Model("") && !strings.EqualFold(raw.PricingCatalogProvider, "openai") {
		return "cache_creation_input_token_cost"
	}
	if raw.CacheCreationInputTokenCostAbove1hr != nil && raw.CacheCreationInputTokenCost == nil {
		return "cache_creation_input_token_cost"
	}
	priorityDeclared := raw.SupportsServiceTier || raw.InputCostPerTokenPriority != nil ||
		raw.OutputCostPerTokenPriority != nil || raw.CacheCreationInputTokenCostPriority != nil ||
		raw.CacheReadInputTokenCostPriority != nil
	if priorityDeclared {
		if raw.InputCostPerTokenPriority == nil {
			return "input_cost_per_token_priority"
		}
		if raw.OutputCostPerTokenPriority == nil {
			return "output_cost_per_token_priority"
		}
		if raw.CacheCreationInputTokenCost != nil && raw.CacheCreationInputTokenCostPriority == nil {
			return "cache_creation_input_token_cost_priority"
		}
		if raw.CacheReadInputTokenCost != nil && raw.CacheReadInputTokenCostPriority == nil {
			return "cache_read_input_token_cost_priority"
		}
	}
	longDeclared := raw.LongContextInputTokenThreshold != nil || raw.LongContextInputCostMultiplier != nil ||
		raw.LongContextOutputCostMultiplier != nil
	if longDeclared {
		if raw.LongContextInputTokenThreshold == nil {
			return "long_context_input_token_threshold"
		}
		if raw.LongContextInputCostMultiplier == nil {
			return "long_context_input_cost_multiplier"
		}
		if raw.LongContextOutputCostMultiplier == nil {
			return "long_context_output_cost_multiplier"
		}
	}
	return "pricing"
}

func (s *PricingService) validateOverrideWrite(platform, model, currency string, payload *ModelPriceOverridePayload, enabled bool) ([]ModelPriceWarning, error) {
	platform = normalizeOverridePlatform(platform)
	model = normalizePricingModelKey(model)
	if !validOverridePlatform(platform) {
		return nil, infraerrors.BadRequest("INVALID_PLATFORM", "unsupported model price platform")
	}
	if model == "" || len(model) > 200 {
		return nil, infraerrors.BadRequest("INVALID_MODEL", "model name is required and must not exceed 200 characters")
	}
	currency, err := NormalizeModelPriceCurrency(currency)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadNumbers(payload); err != nil {
		return nil, err
	}
	var warnings []ModelPriceWarning
	for field, value := range pricePointers(payload) {
		if value != nil && *value > 1 {
			warnings = append(warnings, ModelPriceWarning{Code: "PRICE_MAGNITUDE", Field: field})
		}
	}
	if !enabled {
		return warnings, nil
	}
	var catalog *ModelPriceEntry
	if s != nil {
		s.mu.RLock()
		catalog = s.catalogEntryLocked(model)
		s.mu.RUnlock()
	}
	raw := mergeRawPriceEntry(overrideBaseRaw(catalog, currency), payload)
	hasImage := raw.OutputCostPerImage != nil || raw.OutputCostPerImageToken != nil || raw.InputCostPerImageToken != nil
	if raw.InputCostPerToken == nil && raw.OutputCostPerToken == nil && !hasImage {
		return nil, infraerrors.BadRequest("EMPTY_PRICING", "at least one token or image price is required")
	}
	if raw.InputCostPerToken == nil && raw.OutputCostPerToken == nil && hasImage {
		return warnings, nil
	}
	if rawModelTokenPricingIncomplete(model, raw) {
		missing := incompleteDimension(raw)
		return nil, infraerrors.BadRequest("INCOMPLETE_PRICING", fmt.Sprintf("pricing dimension is incomplete: %s", missing)).
			WithMetadata(map[string]string{"missing_dimension": missing})
	}
	return warnings, nil
}

func (s *PricingService) UpsertOverride(ctx context.Context, input ModelPriceUpsertInput) (*ModelPriceUpsertResult, error) {
	if s == nil || s.overrideStore == nil {
		return nil, fmt.Errorf("model price override store unavailable")
	}
	platform := normalizeOverridePlatform(input.Platform)
	model := normalizePricingModelKey(input.Model)
	currency, err := NormalizeModelPriceCurrency(input.Currency)
	if err != nil {
		return nil, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	warnings, err := s.validateOverrideWrite(platform, model, currency, &input.Payload, enabled)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	row, err := s.overrideStore.Upsert(ctx, &ModelPriceOverride{
		Platform: platform, ModelName: model, Currency: currency, Payload: input.Payload, Enabled: enabled,
		Note: input.Note, UpdatedBy: input.UpdatedBy, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.ReloadOverrides(ctx); err != nil {
		return nil, err
	}
	if s.overrideCache != nil {
		_ = s.overrideCache.BroadcastRefresh(ctx)
	}
	return &ModelPriceUpsertResult{Override: row, Warnings: warnings}, nil
}

func (s *PricingService) DeleteOverride(ctx context.Context, platform, model string) error {
	if s == nil || s.overrideStore == nil {
		return fmt.Errorf("model price override store unavailable")
	}
	platform = normalizeOverridePlatform(platform)
	model = normalizePricingModelKey(model)
	if err := s.overrideStore.Delete(ctx, platform, model); err != nil {
		return err
	}
	if err := s.ReloadOverrides(ctx); err != nil {
		return err
	}
	if s.overrideCache != nil {
		_ = s.overrideCache.BroadcastRefresh(ctx)
	}
	return nil
}

func catalogHasUsablePrice(entry *ModelPriceEntry) bool {
	if entry == nil {
		return false
	}
	return !entry.TokenPricingAbsent || hasImagePricing(entry)
}

func catalogPriceFloat64(v float64) *float64 { return &v }

func catalogPriceInt(v int) *int { return &v }

// ModelPriceEntryFromOfficial converts a billing official/fallback price into a
// catalog-shaped entry for admin display. Zero input/output are kept (free models).
func ModelPriceEntryFromOfficial(model string, p *ModelPricing) *ModelPriceEntry {
	if p == nil {
		return nil
	}
	raw := &RawModelPriceEntry{
		Currency:           ModelPriceCurrencyUSD,
		InputCostPerToken:  catalogPriceFloat64(p.InputPricePerToken),
		OutputCostPerToken: catalogPriceFloat64(p.OutputPricePerToken),
	}
	if p.CacheCreationPricePerToken != 0 || p.CacheCreationPriceExplicit {
		raw.CacheCreationInputTokenCost = catalogPriceFloat64(p.CacheCreationPricePerToken)
	}
	if p.CacheCreation1hPrice != 0 || p.CacheCreation1hPriceExplicit {
		raw.CacheCreationInputTokenCostAbove1hr = catalogPriceFloat64(p.CacheCreation1hPrice)
	}
	if p.CacheReadPricePerToken != 0 || p.CacheReadPriceExplicit {
		raw.CacheReadInputTokenCost = catalogPriceFloat64(p.CacheReadPricePerToken)
	}
	if p.InputPricePerTokenPriority != 0 || p.InputPriorityPriceExplicit {
		raw.InputCostPerTokenPriority = catalogPriceFloat64(p.InputPricePerTokenPriority)
	}
	if p.OutputPricePerTokenPriority != 0 || p.OutputPriorityPriceExplicit {
		raw.OutputCostPerTokenPriority = catalogPriceFloat64(p.OutputPricePerTokenPriority)
	}
	if p.CacheCreationPricePerTokenPriority != 0 || p.CacheCreationPriorityPriceExplicit {
		raw.CacheCreationInputTokenCostPriority = catalogPriceFloat64(p.CacheCreationPricePerTokenPriority)
	}
	if p.CacheReadPricePerTokenPriority != 0 || p.CacheReadPriorityPriceExplicit {
		raw.CacheReadInputTokenCostPriority = catalogPriceFloat64(p.CacheReadPricePerTokenPriority)
	}
	if p.LongContextInputThreshold > 0 {
		raw.LongContextInputTokenThreshold = catalogPriceInt(p.LongContextInputThreshold)
		raw.LongContextInputCostMultiplier = catalogPriceFloat64(p.LongContextInputMultiplier)
		raw.LongContextOutputCostMultiplier = catalogPriceFloat64(p.LongContextOutputMultiplier)
	}
	if p.ImageOutputPricePerToken != 0 || p.ImageOutputPriceExplicit {
		raw.OutputCostPerImageToken = catalogPriceFloat64(p.ImageOutputPricePerToken)
	}
	if p.ImageInputPricePerToken != 0 || p.ImageInputPriceExplicit {
		raw.InputCostPerImageToken = catalogPriceFloat64(p.ImageInputPricePerToken)
	}
	return buildModelPriceEntry(model, raw)
}

func (s *PricingService) ListCatalog(q ModelPriceListQuery) ModelPriceListResult {
	if s == nil {
		return ModelPriceListResult{Items: []ModelPriceListItem{}}
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 50
	}
	platformFilter := normalizeOverridePlatform(q.Platform)
	if platformFilter == ModelPriceOverrideWildcardPlatform {
		platformFilter = ""
	}
	query := strings.ToLower(strings.TrimSpace(q.Query))
	status := strings.ToLower(strings.TrimSpace(q.Status))

	s.mu.RLock()
	defer s.mu.RUnlock()

	overridesByKey := make(map[string]*ModelPriceOverride, len(s.overrideRows))
	for i := range s.overrideRows {
		row := s.overrideRows[i]
		overridesByKey[row.Platform+"\x00"+row.ModelName] = &s.overrideRows[i]
	}

	seen := make(map[string]struct{})
	seenModels := make(map[string]struct{})
	var items []ModelPriceListItem

	officialEntry := func(model string) *ModelPriceEntry {
		if q.OfficialLookup == nil {
			return nil
		}
		return q.OfficialLookup(model)
	}

	addItem := func(displayPlatform, model string, effective *ModelPriceEntry) {
		model = normalizePricingModelKey(model)
		if model == "" {
			return
		}
		key := displayPlatform + "\x00" + model
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		seenModels[model] = struct{}{}
		item := s.buildListItemLocked(displayPlatform, model, effective, overridesByKey, officialEntry(model))
		if query != "" && !strings.Contains(item.Model, query) {
			return
		}
		if !listStatusMatch(status, item) {
			return
		}
		items = append(items, item)
	}

	if q.RestrictTo != nil {
		for _, ref := range q.RestrictTo {
			model := normalizePricingModelKey(ref.Model)
			if model == "" || IsPublicCatalogRoutingOnlyModelID(model) {
				continue
			}
			display := normalizeOverridePlatform(ref.Platform)
			if display == "" {
				display = ModelPriceOverrideWildcardPlatform
			}
			if platformFilter != "" && display != platformFilter {
				continue
			}
			addItem(display, model, s.effectiveEntryLocked(display, model))
		}
		for _, row := range s.overrideRows {
			if _, exists := seenModels[row.ModelName]; exists {
				continue
			}
			display := row.Platform
			if platformFilter != "" {
				// A wildcard override changes pricing wherever the model is callable,
				// but it must not make that model appear callable on every platform.
				// Callable wildcard models were already added from RestrictTo above;
				// an unseen wildcard row therefore has no place in this platform tab.
				if row.Platform == ModelPriceOverrideWildcardPlatform {
					continue
				}
				if row.Platform != platformFilter {
					continue
				}
			}
			addItem(display, row.ModelName, s.effectiveEntryLocked(display, row.ModelName))
		}
	} else if platformFilter == "" {
		for model, entry := range s.catalogData {
			display := strings.TrimSpace(entry.PricingCatalogProvider)
			if display == "" {
				display = ModelPriceOverrideWildcardPlatform
			}
			addItem(display, model, s.effectiveEntryLocked("", model))
		}
		for _, row := range s.overrideRows {
			if _, exists := s.catalogData[row.ModelName]; exists {
				continue
			}
			addItem(row.Platform, row.ModelName, s.effectiveEntryLocked(row.Platform, row.ModelName))
		}
	} else {
		provider := catalogProviderForPlatform(platformFilter)
		for model, entry := range s.catalogData {
			if provider == "" || !strings.EqualFold(entry.PricingCatalogProvider, provider) {
				continue
			}
			addItem(platformFilter, model, s.effectiveEntryLocked(platformFilter, model))
		}
		for _, row := range s.overrideRows {
			if row.Platform == platformFilter {
				addItem(platformFilter, row.ModelName, s.effectiveEntryLocked(platformFilter, row.ModelName))
				continue
			}
			if row.Platform == ModelPriceOverrideWildcardPlatform {
				if _, exists := s.catalogData[row.ModelName]; exists {
					continue
				}
				addItem(platformFilter, row.ModelName, s.effectiveEntryLocked(platformFilter, row.ModelName))
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Platform == items[j].Platform {
			return items[i].Model < items[j].Model
		}
		return items[i].Platform < items[j].Platform
	})
	total := len(items)
	start := (q.Page - 1) * q.PageSize
	if start >= total {
		return ModelPriceListResult{Items: []ModelPriceListItem{}, Total: total}
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return ModelPriceListResult{Items: items[start:end], Total: total}
}

func (s *PricingService) buildListItemLocked(
	displayPlatform, model string,
	effective *ModelPriceEntry,
	overrides map[string]*ModelPriceOverride,
	official *ModelPriceEntry,
) ModelPriceListItem {
	row := overrides[displayPlatform+"\x00"+model]
	if row == nil {
		row = overrides[ModelPriceOverrideWildcardPlatform+"\x00"+model]
	}
	catalog := s.catalogEntryLocked(model)
	catalogUsable := catalogHasUsablePrice(catalog)
	catalogCurrency := modelPriceEntryCurrency(catalog)
	if !catalogUsable && official != nil {
		catalogCurrency = modelPriceEntryCurrency(official)
	}
	source := ModelPriceSourceCatalog
	var overridden []string
	rowApplied := row != nil && row.Enabled
	if rowApplied {
		overridden = overriddenFields(&row.Payload)
		if !catalogUsable || modelPriceEntryCurrency(catalog) != modelPriceCurrencyOrUSD(row.Currency) {
			source = ModelPriceSourceOverride
		} else if len(overridden) > 0 {
			source = ModelPriceSourceMerged
		}
	} else if !catalogUsable && official != nil {
		source = ModelPriceSourceOfficial
	}
	if effective == nil {
		effective = s.effectiveEntryLocked(displayPlatform, model)
	}
	// 目录价 / 手动覆盖价对官方分时 SKU 是空闲价；回落到代码内官方兜底表时才是高峰价。
	baseIsOffPeak := true
	if (effective == nil || !catalogHasUsablePrice(effective)) && !rowApplied && official != nil {
		effective = official
		baseIsOffPeak = false
	}
	item := ModelPriceListItem{
		Platform:           displayPlatform,
		Model:              model,
		Source:             source,
		Currency:           modelPriceEntryCurrency(effective),
		CatalogCurrency:    catalogCurrency,
		TokenPricingAbsent: effective == nil || effective.TokenPricingAbsent,
		HasImagePricing:    hasImagePricing(effective),
		SyncInvalidated:    s.isInvalidatedLocked(displayPlatform, model),
		Redundant:          row != nil && isRedundantPayload(catalog, modelPriceCurrencyOrUSD(row.Currency), &row.Payload),
		Effective:          modelPriceEntryToMap(effective),
		OverriddenFields:   overridden,
		Enabled:            row == nil || row.Enabled,
		TimeSchedule:       deepSeekOfficialPriceTimeSchedule(displayPlatform, model, baseIsOffPeak),
	}
	if item.Currency == "" {
		item.Currency = catalogCurrency
		if item.Currency == "" {
			item.Currency = ModelPriceCurrencyUSD
		}
	}
	if row != nil {
		item.OverrideCurrency = modelPriceCurrencyOrUSD(row.Currency)
		item.OverridePlatform = row.Platform
		item.Note = row.Note
		item.UpdatedBy = row.UpdatedBy
		updated := row.UpdatedAt
		item.UpdatedAt = &updated
	}
	return item
}

func listStatusMatch(status string, item ModelPriceListItem) bool {
	switch status {
	case "", "all":
		return true
	case "overridden":
		return item.Source == ModelPriceSourceOverride || item.Source == ModelPriceSourceMerged
	case "missing":
		return item.TokenPricingAbsent && !item.HasImagePricing
	case "sync_invalidated":
		return item.SyncInvalidated
	case "disabled":
		return !item.Enabled
	default:
		return true
	}
}

func (s *PricingService) catalogEntryLocked(model string) *ModelPriceEntry {
	if s == nil || s.catalogData == nil {
		return nil
	}
	model = normalizePricingModelKey(model)
	if entry := s.catalogData[model]; entry != nil {
		return entry
	}
	return lookupIdentifiedIn(s.catalogData, model, s.buildModelLookupCandidates(model))
}

func (s *PricingService) GetPriceDetail(platform, model string) (*ModelPriceDetail, error) {
	return s.GetPriceDetailWithOfficial(platform, model, nil)
}

// ResolvePlazaDisplayPrice 返回模型广场应展示的生效价（手动覆盖 > 目录 > 官方兜底）。
// 渠道价由调用方在 nil 时兜底。币种仅用于展示，不参与计费数值运算。
func ResolvePlazaDisplayPrice(s *PricingService, platform, model string, official *ModelPriceEntry) *PlazaDisplayPriceResolution {
	platform = normalizeOverridePlatform(platform)
	model = normalizePricingModelKey(model)
	// 官方兜底表存的是高峰价，目录价 / 手动覆盖价存的是空闲价，两者挂的峰谷
	// 换算方向相反，必须跟着实际返回的那份价一起给（结算侧同理，见
	// resolvedTokenTimeMultiplier）。
	if s == nil {
		if official == nil {
			return nil
		}
		return &PlazaDisplayPriceResolution{
			Pricing:      official,
			Currency:     modelPriceEntryCurrency(official),
			Source:       ModelPriceSourceOfficial,
			TimeSchedule: deepSeekOfficialPriceTimeSchedule(platform, model, false),
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := findOverrideLocked(s.overrideRows, platform, model)
	if row == nil && platform != ModelPriceOverrideWildcardPlatform {
		row = findOverrideLocked(s.overrideRows, ModelPriceOverrideWildcardPlatform, model)
	}
	if row != nil && !row.Enabled {
		row = nil
	}
	catalog := s.catalogEntryLocked(model)
	effective := s.effectiveEntryLocked(platform, model)
	if catalogHasUsablePrice(effective) {
		source := ModelPriceSourceCatalog
		if row != nil {
			if catalogHasUsablePrice(catalog) && modelPriceEntryCurrency(catalog) == modelPriceCurrencyOrUSD(row.Currency) {
				if len(overriddenFields(&row.Payload)) > 0 {
					source = ModelPriceSourceMerged
				}
			} else {
				source = ModelPriceSourceOverride
			}
		}
		return &PlazaDisplayPriceResolution{
			Pricing:      effective,
			Currency:     modelPriceEntryCurrency(effective),
			Source:       source,
			TimeSchedule: deepSeekOfficialPriceTimeSchedule(platform, model, true),
		}
	}
	if official != nil {
		return &PlazaDisplayPriceResolution{
			Pricing:      official,
			Currency:     modelPriceEntryCurrency(official),
			Source:       ModelPriceSourceOfficial,
			TimeSchedule: deepSeekOfficialPriceTimeSchedule(platform, model, false),
		}
	}
	return nil
}

func (s *PricingService) GetPriceDetailWithOfficial(platform, model string, official *ModelPriceEntry) (*ModelPriceDetail, error) {
	if s == nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "pricing service unavailable")
	}
	platform = normalizeOverridePlatform(platform)
	if platform == "" {
		platform = ModelPriceOverrideWildcardPlatform
	}
	model = normalizePricingModelKey(model)
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := findOverrideLocked(s.overrideRows, platform, model)
	if row == nil && platform != ModelPriceOverrideWildcardPlatform {
		row = findOverrideLocked(s.overrideRows, ModelPriceOverrideWildcardPlatform, model)
	}
	catalog := s.catalogEntryLocked(model)
	effective := s.effectiveEntryLocked(platform, model)
	rowApplied := row != nil && row.Enabled
	// 目录价 / 手动覆盖价对官方分时 SKU 是空闲价；回落到代码内官方兜底表时才是高峰价。
	baseIsOffPeak := true
	if (effective == nil || !catalogHasUsablePrice(effective)) && !rowApplied && official != nil {
		effective = official
		baseIsOffPeak = false
	}
	catalogMap := modelPriceEntryToMap(catalog)
	catalogCurrency := modelPriceEntryCurrency(catalog)
	if !catalogHasUsablePrice(catalog) && official != nil {
		catalogMap = modelPriceEntryToMap(official)
		catalogCurrency = modelPriceEntryCurrency(official)
	}
	if catalog == nil && row == nil && effective == nil {
		currency := catalogCurrency
		if currency == "" {
			currency = ModelPriceCurrencyUSD
		}
		return &ModelPriceDetail{
			Platform:           platform,
			Model:              model,
			Currency:           currency,
			CatalogCurrency:    catalogCurrency,
			Catalog:            catalogMap,
			Effective:          map[string]any{},
			Enabled:            true,
			TokenPricingAbsent: true,
			TimeSchedule:       deepSeekOfficialPriceTimeSchedule(platform, model, baseIsOffPeak),
		}, nil
	}
	detailCurrency := modelPriceEntryCurrency(effective)
	if detailCurrency == "" && row != nil && row.Enabled {
		detailCurrency = modelPriceCurrencyOrUSD(row.Currency)
	}
	if detailCurrency == "" {
		detailCurrency = catalogCurrency
		if detailCurrency == "" {
			detailCurrency = ModelPriceCurrencyUSD
		}
	}
	detail := &ModelPriceDetail{
		Platform:           platform,
		Model:              model,
		Currency:           detailCurrency,
		CatalogCurrency:    catalogCurrency,
		Catalog:            catalogMap,
		Effective:          modelPriceEntryToMap(effective),
		Enabled:            true,
		TokenPricingAbsent: effective == nil || effective.TokenPricingAbsent,
		HasImagePricing:    hasImagePricing(effective),
		SyncInvalidated:    s.isInvalidatedLocked(platform, model),
		TimeSchedule:       deepSeekOfficialPriceTimeSchedule(platform, model, baseIsOffPeak),
	}
	if row != nil {
		payload := row.Payload
		detail.Override = &payload
		detail.OverrideCurrency = modelPriceCurrencyOrUSD(row.Currency)
		detail.OverridePlatform = row.Platform
		detail.Enabled = row.Enabled
		detail.Note = row.Note
		detail.UpdatedBy = row.UpdatedBy
		updated := row.UpdatedAt
		detail.UpdatedAt = &updated
		detail.Redundant = isRedundantPayload(catalog, detail.OverrideCurrency, &row.Payload)
	}
	return detail, nil
}

func findOverrideLocked(rows []ModelPriceOverride, platform, model string) *ModelPriceOverride {
	for i := range rows {
		if rows[i].Platform == platform && rows[i].ModelName == model {
			return &rows[i]
		}
	}
	return nil
}

func overriddenFields(payload *ModelPriceOverridePayload) []string {
	var fields []string
	if payload == nil {
		return fields
	}
	for field, value := range pricePointers(payload) {
		if value != nil {
			fields = append(fields, field)
		}
	}
	if payload.LongContextInputTokenThreshold != nil {
		fields = append(fields, "long_context_input_token_threshold")
	}
	if payload.SupportsServiceTier != nil {
		fields = append(fields, "supports_service_tier")
	}
	if payload.SupportsPromptCaching != nil {
		fields = append(fields, "supports_prompt_caching")
	}
	if payload.PricingCatalogProvider != nil {
		fields = append(fields, "litellm_provider")
	}
	if payload.Mode != nil {
		fields = append(fields, "mode")
	}
	sort.Strings(fields)
	return fields
}

func hasImagePricing(entry *ModelPriceEntry) bool {
	if entry == nil {
		return false
	}
	return entry.OutputCostPerImageExplicit || entry.ImageOutputPriceExplicit || entry.ImageInputPriceExplicit ||
		entry.OutputCostPerImage != 0 || entry.OutputCostPerImageToken != 0 || entry.InputCostPerImageToken != 0
}

func modelPriceEntryToMap(entry *ModelPriceEntry) map[string]any {
	out := map[string]any{}
	if entry == nil {
		return out
	}
	put := func(key string, value float64, explicit bool) {
		if explicit || (!entry.PricePresenceKnown && value != 0) {
			out[key] = value
		}
	}
	put("input_cost_per_token", entry.InputCostPerToken, entry.InputPriceExplicit)
	put("output_cost_per_token", entry.OutputCostPerToken, entry.OutputPriceExplicit)
	put("input_cost_per_token_priority", entry.InputCostPerTokenPriority, entry.InputPriorityPriceExplicit)
	put("output_cost_per_token_priority", entry.OutputCostPerTokenPriority, entry.OutputPriorityPriceExplicit)
	put("cache_creation_input_token_cost", entry.CacheCreationInputTokenCost, entry.CacheCreationPriceExplicit)
	put("cache_creation_input_token_cost_above_1hr", entry.CacheCreationInputTokenCostAbove1hr, entry.CacheCreationAbove1hrPriceExplicit)
	put("cache_read_input_token_cost", entry.CacheReadInputTokenCost, entry.CacheReadPriceExplicit)
	put("cache_creation_input_token_cost_priority", entry.CacheCreationInputTokenCostPriority, entry.CacheCreationPriorityPriceExplicit)
	put("cache_read_input_token_cost_priority", entry.CacheReadInputTokenCostPriority, entry.CacheReadPriorityPriceExplicit)
	put("output_cost_per_image", entry.OutputCostPerImage, entry.OutputCostPerImageExplicit)
	put("output_cost_per_image_token", entry.OutputCostPerImageToken, entry.ImageOutputPriceExplicit)
	put("input_cost_per_image_token", entry.InputCostPerImageToken, entry.ImageInputPriceExplicit)
	if entry.LongContextPricingExplicit || entry.LongContextInputTokenThreshold != 0 {
		out["long_context_input_token_threshold"] = entry.LongContextInputTokenThreshold
		out["long_context_input_cost_multiplier"] = entry.LongContextInputCostMultiplier
		out["long_context_output_cost_multiplier"] = entry.LongContextOutputCostMultiplier
	}
	if entry.SupportsServiceTier {
		out["supports_service_tier"] = true
	}
	if entry.SupportsPromptCaching {
		out["supports_prompt_caching"] = true
	}
	if entry.PricingCatalogProvider != "" {
		out["litellm_provider"] = entry.PricingCatalogProvider
	}
	if entry.Mode != "" {
		out["mode"] = entry.Mode
	}
	return out
}

func isRedundantPayload(catalog *ModelPriceEntry, currency string, payload *ModelPriceOverridePayload) bool {
	if catalog == nil || payload == nil {
		return false
	}
	if modelPriceEntryCurrency(catalog) != modelPriceCurrencyOrUSD(currency) {
		return false
	}
	catalogMap := modelPriceEntryToMap(catalog)
	patchMap := payloadToMap(payload)
	if len(patchMap) == 0 {
		return false
	}
	for key, value := range patchMap {
		if fmt.Sprint(catalogMap[key]) != fmt.Sprint(value) {
			return false
		}
	}
	return true
}

func payloadToMap(payload *ModelPriceOverridePayload) map[string]any {
	out := map[string]any{}
	if payload == nil {
		return out
	}
	body, _ := json.Marshal(payload)
	_ = json.Unmarshal(body, &out)
	return out
}
