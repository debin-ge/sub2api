package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"go.uber.org/zap"
)

var (
	openAIModelDatePattern = regexp.MustCompile(`-\d{8}$`)
	openAIModelBasePattern = regexp.MustCompile(`^(gpt-\d+(?:\.\d+)?)(?:-|$)`)
	// dashVersionSuffixPattern 匹配「非数字字符 + 数字 + '-' + 数字」的位置，
	// 用于把 dash 分隔的版本号（glm-5-1、kimi-k2-5）改成 dot 分隔的
	// canonical 形式（glm-5.1、kimi-k2.5）。前置的 (\D) 避免匹配纯数字序列
	// （如 -20251101 尾缀）。
	dashVersionSuffixPattern = regexp.MustCompile(`(\D)(\d+)-(\d+)`)
	// aboveTierPricePattern derives catalog long-context multipliers from absolute tier prices.
	aboveTierPricePattern      = regexp.MustCompile(`^(input|output)_cost_per_token_above_(\d+)k_tokens$`)
	cacheTierPricePattern      = regexp.MustCompile(`^(cache_(?:creation|read)_input_token_cost)(_above_1hr)?_above_\d+k_tokens((?:_[a-z]+)?)$`)
	openAIGPT54FallbackPricing = &ModelPriceEntry{
		InputCostPerToken:       2.5e-06, // $2.5 per MTok
		OutputCostPerToken:      1.5e-05, // $15 per MTok
		CacheReadInputTokenCost: 2.5e-07, // $0.25 per MTok
		PricingCatalogProvider:  "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
	openAIGPT56SolFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   5e-06,
		InputCostPerTokenPriority:           1e-05,
		OutputCostPerToken:                  3e-05,
		OutputCostPerTokenPriority:          6e-05,
		CacheCreationInputTokenCost:         6.25e-06,
		CacheCreationInputTokenCostPriority: 1.25e-05,
		CacheReadInputTokenCost:             5e-07,
		CacheReadInputTokenCostPriority:     1e-06,
		SupportsServiceTier:                 true,
		PricingCatalogProvider:              "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT56TerraFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   2e-06,
		InputCostPerTokenPriority:           4e-06,
		OutputCostPerToken:                  1.2e-05,
		OutputCostPerTokenPriority:          2.4e-05,
		CacheCreationInputTokenCost:         2.5e-06,
		CacheCreationInputTokenCostPriority: 5e-06,
		CacheReadInputTokenCost:             2e-07,
		CacheReadInputTokenCostPriority:     4e-07,
		SupportsServiceTier:                 true,
		PricingCatalogProvider:              "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT56LunaFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   2e-07,
		InputCostPerTokenPriority:           4e-07,
		OutputCostPerToken:                  1.2e-06,
		OutputCostPerTokenPriority:          2.4e-06,
		CacheCreationInputTokenCost:         2.5e-07,
		CacheCreationInputTokenCostPriority: 5e-07,
		CacheReadInputTokenCost:             2e-08,
		CacheReadInputTokenCostPriority:     4e-08,
		SupportsServiceTier:                 true,
		PricingCatalogProvider:              "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT54MiniFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:       7.5e-07,
		OutputCostPerToken:      4.5e-06,
		CacheReadInputTokenCost: 7.5e-08,
		PricingCatalogProvider:  "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
	openAIGPT54NanoFallbackPricing = &ModelPriceEntry{
		InputCostPerToken:       2e-07,
		OutputCostPerToken:      1.25e-06,
		CacheReadInputTokenCost: 2e-08,
		PricingCatalogProvider:  "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
)

// ModelPriceEntry is one entry from the configured pricing catalog.
// The catalog schema is compatible with model-price-repo JSON and includes
// fields such as input_cost_per_token and litellm_provider.
type ModelPriceEntry struct {
	Currency                            string  `json:"currency,omitempty"`
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority           float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      int     `json:"long_context_input_token_threshold,omitempty"`
	LongContextInputCostMultiplier      float64 `json:"long_context_input_cost_multiplier,omitempty"`
	LongContextOutputCostMultiplier     float64 `json:"long_context_output_cost_multiplier,omitempty"`
	SupportsServiceTier                 bool    `json:"supports_service_tier"`
	PricingCatalogProvider              string  `json:"litellm_provider"`
	Mode                                string  `json:"mode"`
	SupportsPromptCaching               bool    `json:"supports_prompt_caching"`
	OutputCostPerImage                  float64 `json:"output_cost_per_image"`       // 图片生成模型每张图片价格
	OutputCostPerImageToken             float64 `json:"output_cost_per_image_token"` // 图片输出 token 价格
	InputCostPerImageToken              float64 `json:"input_cost_per_image_token"`  // 图片输入 token 价格（如 gpt-image-2 图片编辑）
	OutputCostPerImageExplicit          bool    `json:"-"`
	ImageOutputPriceExplicit            bool    `json:"-"`
	ImageInputPriceExplicit             bool    `json:"-"`
	InputPriceExplicit                  bool    `json:"-"`
	OutputPriceExplicit                 bool    `json:"-"`
	// PricePresenceKnown is true for entries parsed from the JSON catalog.
	// In-memory fallback/test entries predate presence tracking and keep the
	// zero value for backward compatibility.
	PricePresenceKnown                 bool `json:"-"`
	CacheCreationPriceExplicit         bool `json:"-"`
	CacheCreationAbove1hrPriceExplicit bool `json:"-"`
	CacheReadPriceExplicit             bool `json:"-"`
	InputPriorityPriceExplicit         bool `json:"-"`
	OutputPriorityPriceExplicit        bool `json:"-"`
	CacheCreationPriorityPriceExplicit bool `json:"-"`
	CacheReadPriorityPriceExplicit     bool `json:"-"`
	LongContextPricingExplicit         bool `json:"-"`
	LongContextThresholdInclusive      bool `json:"-"`
	OperatorOverride                   bool `json:"-"`

	// TokenPricingAbsent 表示源数据缺少一组完整、自洽的 token 价格：
	// input/output 必须同时存在；声明 cache、priority 或 long-context 时，
	// 该维度的整组字段也必须完整。此类条目只可用于图片等专用计费，
	// token 计费必须回退到 fallback 或 fail-closed；显式 0 仍视为已配置。
	// 零值（false）兼容代码内构造的条目。
	TokenPricingAbsent bool `json:"-"`
}

// LiteLLMModelPricing is kept as a compatibility alias for code paths merged
// from release tags that still use the previous name.
type LiteLLMModelPricing = ModelPriceEntry

// PricingRemoteClient 远程价格数据获取接口
type PricingRemoteClient interface {
	FetchPricingJSON(ctx context.Context, url string) ([]byte, error)
	FetchHashText(ctx context.Context, url string) (string, error)
}

// RawModelPriceEntry parses raw JSON while preserving whether optional price
// fields were present.
type RawModelPriceEntry struct {
	Currency                            string   `json:"currency"`
	InputCostPerToken                   *float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority           *float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  *float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          *float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         *float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority *float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr *float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             *float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     *float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      *int     `json:"long_context_input_token_threshold"`
	LongContextInputCostMultiplier      *float64 `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier     *float64 `json:"long_context_output_cost_multiplier"`
	SupportsServiceTier                 bool     `json:"supports_service_tier"`
	PricingCatalogProvider              string   `json:"litellm_provider"`
	Mode                                string   `json:"mode"`
	SupportsPromptCaching               bool     `json:"supports_prompt_caching"`
	OutputCostPerImage                  *float64 `json:"output_cost_per_image"`
	OutputCostPerImageToken             *float64 `json:"output_cost_per_image_token"`
	InputCostPerImageToken              *float64 `json:"input_cost_per_image_token"`
}

// codexAutoReviewHasUnpricedServiceTier is an explicit catalog policy for the
// internal Codex Auto Review SKU. The upstream catalog keeps the capability
// metadata (supports_service_tier) but intentionally publishes no priority
// price dimensions for this model. Its base and cache-read prices are still
// authoritative; do not infer public GPT pricing for the omitted dimensions.
// Keep this exception narrow so an unrelated partial priority entry continues
// to fail closed.
func codexAutoReviewHasUnpricedServiceTier(model string, entry *RawModelPriceEntry) bool {
	if entry == nil || !entry.SupportsServiceTier ||
		!strings.EqualFold(strings.TrimSpace(model), "codex-auto-review") {
		return false
	}
	return entry.InputCostPerTokenPriority == nil &&
		entry.OutputCostPerTokenPriority == nil &&
		entry.CacheCreationInputTokenCostPriority == nil &&
		entry.CacheReadInputTokenCostPriority == nil
}

func rawModelTokenPricingIncomplete(model string, entry *RawModelPriceEntry) bool {
	if entry == nil || entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil {
		return true
	}
	hasDerivedGPT56CacheWritePolicy := isOpenAIGPT56Model(normalizeKnownOpenAICodexModel(model))
	usesOpenAINativeCacheBilling := strings.EqualFold(strings.TrimSpace(entry.PricingCatalogProvider), "openai")

	// A cache-capable catalog entry must say what both cache operations cost.
	// Explicit zero is valid; omission is not, except for native OpenAI pricing:
	// OpenAI publishes a cached-input read discount but no separate cache-write
	// price because cache-populating input remains ordinary input. OpenAI usage
	// also normally reports only total/cached input, so no cache-write bucket is
	// created. If an OpenAI-compatible upstream unexpectedly reports explicit
	// cache-write tokens, validateUsedModelPricingDimensions still fails closed
	// unless a model policy or channel price supplies that dimension.
	cachePricingDeclared := entry.SupportsPromptCaching ||
		entry.CacheCreationInputTokenCost != nil ||
		entry.CacheCreationInputTokenCostAbove1hr != nil ||
		entry.CacheReadInputTokenCost != nil
	if cachePricingDeclared && entry.CacheReadInputTokenCost == nil {
		return true
	}
	if cachePricingDeclared && entry.CacheCreationInputTokenCost == nil &&
		!hasDerivedGPT56CacheWritePolicy && !usesOpenAINativeCacheBilling {
		return true
	}
	if entry.CacheCreationInputTokenCostAbove1hr != nil &&
		entry.CacheCreationInputTokenCost == nil {
		return true
	}

	// Priority pricing is one coherent tier. Once the catalog declares support
	// or provides any priority field, every base dimension exposed by this
	// entry needs its corresponding priority price. Otherwise the calculator
	// would silently mix priority and standard prices.
	priorityPricingDeclared := entry.SupportsServiceTier &&
		!codexAutoReviewHasUnpricedServiceTier(model, entry) ||
		entry.InputCostPerTokenPriority != nil ||
		entry.OutputCostPerTokenPriority != nil ||
		entry.CacheCreationInputTokenCostPriority != nil ||
		entry.CacheReadInputTokenCostPriority != nil
	if priorityPricingDeclared {
		if entry.InputCostPerTokenPriority == nil || entry.OutputCostPerTokenPriority == nil {
			return true
		}
		if entry.CacheCreationInputTokenCost != nil &&
			entry.CacheCreationInputTokenCostPriority == nil {
			return true
		}
		if entry.CacheReadInputTokenCost != nil &&
			entry.CacheReadInputTokenCostPriority == nil {
			return true
		}
	}

	// Long-context pricing is also an indivisible tuple. A partial tuple can
	// make one side of a long request get multiplied by the float zero value.
	longContextPricingDeclared := entry.LongContextInputTokenThreshold != nil ||
		entry.LongContextInputCostMultiplier != nil ||
		entry.LongContextOutputCostMultiplier != nil
	if longContextPricingDeclared {
		if entry.LongContextInputTokenThreshold == nil ||
			entry.LongContextInputCostMultiplier == nil ||
			entry.LongContextOutputCostMultiplier == nil {
			return true
		}
		if *entry.LongContextInputTokenThreshold <= 0 ||
			!isFiniteNonNegativePrice(*entry.LongContextInputCostMultiplier) ||
			!isFiniteNonNegativePrice(*entry.LongContextOutputCostMultiplier) ||
			*entry.LongContextInputCostMultiplier <= 0 ||
			*entry.LongContextOutputCostMultiplier <= 0 {
			return true
		}
	}

	return false
}

// PricingService 动态价格服务
type PricingService struct {
	cfg               *config.Config
	remoteClient      PricingRemoteClient
	mu                sync.RWMutex
	pricingData       map[string]*ModelPriceEntry
	catalogData       map[string]*ModelPriceEntry
	overrideStore     ModelPriceOverrideStore
	overrideCache     ModelPriceOverrideCache
	overrideRows      []ModelPriceOverride
	platformOverrides map[string]map[string]*ModelPriceEntry
	syncInvalidated   map[string]struct{}
	lastUpdated       time.Time
	localHash         string

	// 停止信号
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewPricingService 创建价格服务
func NewPricingService(cfg *config.Config, remoteClient PricingRemoteClient) *PricingService {
	s := &PricingService{
		cfg:               cfg,
		remoteClient:      remoteClient,
		pricingData:       make(map[string]*ModelPriceEntry),
		catalogData:       make(map[string]*ModelPriceEntry),
		platformOverrides: make(map[string]map[string]*ModelPriceEntry),
		syncInvalidated:   make(map[string]struct{}),
		stopCh:            make(chan struct{}),
	}
	return s
}

func (s *PricingService) SetOverrideDependencies(store ModelPriceOverrideStore, cache ModelPriceOverrideCache) {
	if s == nil {
		return
	}
	s.overrideStore = store
	s.overrideCache = cache
}

// SeedCatalogForTest replaces the synced catalog and rebuilds effective prices.
func (s *PricingService) SeedCatalogForTest(data map[string]*ModelPriceEntry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogData = data
	s.rebuildEffectiveLocked(s.catalogData)
}

func (s *PricingService) SeedOverridesForTest(rows []ModelPriceOverride) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range rows {
		rows[i].Currency = modelPriceCurrencyOrUSD(rows[i].Currency)
	}
	s.overrideRows = rows
	s.rebuildEffectiveLocked(s.catalogData)
}

func (s *PricingService) ReloadOverrides(ctx context.Context) error {
	if s == nil || s.overrideStore == nil {
		return nil
	}
	rows, err := s.overrideStore.List(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Platform = normalizeOverridePlatform(rows[i].Platform)
		rows[i].ModelName = normalizePricingModelKey(rows[i].ModelName)
		rows[i].Currency = modelPriceCurrencyOrUSD(rows[i].Currency)
	}
	s.mu.Lock()
	s.overrideRows = rows
	s.rebuildEffectiveLocked(s.catalogData)
	s.mu.Unlock()
	return nil
}

func (s *PricingService) startOverrideReconcile() {
	if s == nil || s.overrideStore == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-s.stopCh
		cancel()
	}()
	if s.overrideCache != nil {
		s.overrideCache.SubscribeRefresh(ctx, func() {
			if err := s.ReloadOverrides(context.Background()); err != nil {
				logger.LegacyPrintf("service.pricing", "[Pricing] Override refresh failed: %v", err)
			}
		})
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.ReloadOverrides(context.Background()); err != nil {
					logger.LegacyPrintf("service.pricing", "[Pricing] Override reconcile failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// rebuildEffectiveLocked is the sole production writer of pricingData and
// platformOverrides. The caller must hold s.mu.
func (s *PricingService) rebuildEffectiveLocked(catalog map[string]*ModelPriceEntry) {
	base := cloneCatalog(catalog)
	invalidated := make(map[string]struct{})
	for i := range s.overrideRows {
		row := &s.overrideRows[i]
		if !row.Enabled || row.Platform != ModelPriceOverrideWildcardPlatform {
			continue
		}
		before := base[row.ModelName]
		merged := buildOverrideModelPriceEntry(row.ModelName, before, row)
		base[row.ModelName] = merged
		if catalogHasUsablePrice(before) && !catalogHasUsablePrice(merged) {
			invalidated[ModelPriceOverrideWildcardPlatform+"\x00"+row.ModelName] = struct{}{}
		}
	}
	overlay := make(map[string]map[string]*ModelPriceEntry)
	for i := range s.overrideRows {
		row := &s.overrideRows[i]
		if !row.Enabled || row.Platform == ModelPriceOverrideWildcardPlatform {
			continue
		}
		if overlay[row.Platform] == nil {
			overlay[row.Platform] = make(map[string]*ModelPriceEntry)
		}
		before := base[row.ModelName]
		merged := buildOverrideModelPriceEntry(row.ModelName, before, row)
		overlay[row.Platform][row.ModelName] = merged
		if catalogHasUsablePrice(before) && !catalogHasUsablePrice(merged) {
			invalidated[row.Platform+"\x00"+row.ModelName] = struct{}{}
		}
	}
	s.pricingData = base
	s.platformOverrides = overlay
	s.syncInvalidated = invalidated
}

func (s *PricingService) effectiveEntryLocked(platform, model string) *ModelPriceEntry {
	model = normalizePricingModelKey(model)
	candidates := s.buildModelLookupCandidates(model)
	if overlay := s.platformOverrides[normalizeOverridePlatform(platform)]; len(overlay) > 0 {
		if entry := lookupIdentifiedIn(overlay, model, candidates); entry != nil {
			return entry
		}
	}
	return lookupIdentifiedIn(s.pricingData, model, candidates)
}

func (s *PricingService) isInvalidatedLocked(platform, model string) bool {
	if s == nil {
		return false
	}
	platform = normalizeOverridePlatform(platform)
	model = normalizePricingModelKey(model)
	if _, ok := s.syncInvalidated[platform+"\x00"+model]; ok {
		return true
	}
	_, ok := s.syncInvalidated[ModelPriceOverrideWildcardPlatform+"\x00"+model]
	return ok
}

// Initialize 初始化价格服务
func (s *PricingService) Initialize() error {
	// 确保数据目录存在
	if err := os.MkdirAll(s.cfg.Pricing.DataDir, 0755); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to create data directory: %v", err)
	}

	// 首次加载价格数据
	if err := s.checkAndUpdatePricing(); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Initial load failed, using fallback: %v", err)
		if err := s.useFallbackPricing(); err != nil {
			return fmt.Errorf("failed to load pricing data: %w", err)
		}
	}

	if err := s.ReloadOverrides(context.Background()); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Initial override load failed: %v", err)
	}
	s.startOverrideReconcile()

	// 启动定时更新
	s.startUpdateScheduler()

	logger.LegacyPrintf("service.pricing", "[Pricing] Service initialized with %d models", len(s.pricingData))
	return nil
}

// Stop 停止价格服务
func (s *PricingService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
	logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Service stopped")
}

// startUpdateScheduler 启动定时更新调度器
func (s *PricingService) startUpdateScheduler() {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Pricing.RemoteURL) == "" {
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Remote sync disabled: pricing remote URL is empty")
		return
	}

	// 定期检查哈希更新
	hashInterval := time.Duration(s.cfg.Pricing.HashCheckIntervalMinutes) * time.Minute
	if hashInterval < time.Minute {
		hashInterval = 10 * time.Minute
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(hashInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.syncWithRemote(); err != nil {
					logger.LegacyPrintf("service.pricing", "[Pricing] Sync failed: %v", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()

	logger.LegacyPrintf("service.pricing", "[Pricing] Update scheduler started (check every %v)", hashInterval)
}

// checkAndUpdatePricing 检查并更新价格数据
func (s *PricingService) checkAndUpdatePricing() error {
	pricingFile := s.getPricingFilePath()

	// 检查本地文件是否存在
	if _, err := os.Stat(pricingFile); os.IsNotExist(err) {
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Local pricing file not found, downloading...")
		return s.downloadPricingData()
	}

	// 先加载本地文件（确保服务可用），再检查是否需要更新
	if err := s.loadPricingData(pricingFile); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to load local file, downloading: %v", err)
		return s.downloadPricingData()
	}

	// 如果配置了哈希URL，通过远程哈希检查是否有更新
	if s.cfg.Pricing.HashURL != "" {
		remoteHash, err := s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash on startup: %v", err)
			return nil // 已加载本地文件，哈希获取失败不影响启动
		}

		s.mu.RLock()
		localHash := s.localHash
		s.mu.RUnlock()

		if localHash == "" || remoteHash != localHash {
			logger.LegacyPrintf("service.pricing", "[Pricing] Remote hash differs on startup (local=%s remote=%s), downloading...",
				localHash[:min(8, len(localHash))], remoteHash[:min(8, len(remoteHash))])
			if err := s.downloadPricingData(); err != nil {
				logger.LegacyPrintf("service.pricing", "[Pricing] Download failed, using existing file: %v", err)
			}
		}
		return nil
	}

	// 没有哈希URL时，基于文件年龄检查
	info, err := os.Stat(pricingFile)
	if err != nil {
		return nil // 已加载本地文件
	}

	fileAge := time.Since(info.ModTime())
	maxAge := time.Duration(s.cfg.Pricing.UpdateIntervalHours) * time.Hour

	if fileAge > maxAge {
		logger.LegacyPrintf("service.pricing", "[Pricing] Local file is %v old, updating...", fileAge.Round(time.Hour))
		if err := s.downloadPricingData(); err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Download failed, using existing file: %v", err)
		}
	}

	return nil
}

// syncWithRemote 与远程同步（基于哈希校验）
func (s *PricingService) syncWithRemote() error {
	// 如果配置了哈希URL，从远程获取哈希进行比对
	if s.cfg.Pricing.HashURL != "" {
		remoteHash, err := s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash: %v", err)
			return nil // 哈希获取失败不影响正常使用
		}

		s.mu.RLock()
		localHash := s.localHash
		s.mu.RUnlock()

		if localHash == "" || remoteHash != localHash {
			logger.LegacyPrintf("service.pricing", "[Pricing] Remote hash differs (local=%s remote=%s), downloading new version...",
				localHash[:min(8, len(localHash))], remoteHash[:min(8, len(remoteHash))])
			return s.downloadPricingData()
		}
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Hash check passed, no update needed")
		return nil
	}

	// 没有哈希URL时，基于时间检查
	pricingFile := s.getPricingFilePath()
	info, err := os.Stat(pricingFile)
	if err != nil {
		return s.downloadPricingData()
	}

	fileAge := time.Since(info.ModTime())
	maxAge := time.Duration(s.cfg.Pricing.UpdateIntervalHours) * time.Hour

	if fileAge > maxAge {
		logger.LegacyPrintf("service.pricing", "[Pricing] File is %v old, downloading...", fileAge.Round(time.Hour))
		return s.downloadPricingData()
	}

	return nil
}

// downloadPricingData 从远程下载价格数据
func (s *PricingService) downloadPricingData() error {
	remoteURL, err := s.validatePricingURL(s.cfg.Pricing.RemoteURL)
	if err != nil {
		return err
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Downloading from %s", remoteURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 获取远程哈希（用于同步锚点，不作为完整性校验）
	var remoteHash string
	if strings.TrimSpace(s.cfg.Pricing.HashURL) != "" {
		remoteHash, err = s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash (continuing): %v", err)
		}
	}

	body, err := s.remoteClient.FetchPricingJSON(ctx, remoteURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// 哈希校验：不匹配时仅告警，不阻止更新
	// 远程哈希文件可能与数据文件不同步（如维护者更新了数据但未更新哈希文件）
	dataHash := sha256.Sum256(body)
	dataHashStr := hex.EncodeToString(dataHash[:])
	if remoteHash != "" && !strings.EqualFold(remoteHash, dataHashStr) {
		logger.LegacyPrintf("service.pricing", "[Pricing] Hash mismatch warning: remote=%s data=%s (hash file may be out of sync)",
			remoteHash[:min(8, len(remoteHash))], dataHashStr[:8])
	}

	// 解析JSON数据（使用灵活的解析方式）
	data, err := s.parsePricingData(body)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}
	data = s.mergeFallbackPricingData(data)
	data = s.mergeOverrideOnlyModels(data)

	// 保存到本地文件
	pricingFile := s.getPricingFilePath()
	if err := os.WriteFile(pricingFile, body, 0644); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to save file: %v", err)
	}

	// 使用远程哈希作为同步锚点，防止重复下载
	// 当远程哈希不可用时，回退到数据本身的哈希
	syncHash := dataHashStr
	if remoteHash != "" {
		syncHash = remoteHash
	}
	hashFile := s.getHashFilePath()
	if err := os.WriteFile(hashFile, []byte(syncHash+"\n"), 0644); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to save hash: %v", err)
	}

	// 更新内存数据
	s.mu.Lock()
	warnDroppedLongContextLadders(s.catalogData, data)
	s.catalogData = data
	s.rebuildEffectiveLocked(s.catalogData)
	s.lastUpdated = time.Now()
	s.localHash = syncHash
	s.mu.Unlock()

	logger.LegacyPrintf("service.pricing", "[Pricing] Downloaded %d models successfully", len(data))
	return nil
}

// parsePricingData 解析价格数据（处理各种格式）
func (s *PricingService) parsePricingData(body []byte) (map[string]*ModelPriceEntry, error) {
	// 首先解析为 map[string]json.RawMessage
	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, fmt.Errorf("parse raw JSON: %w", err)
	}
	rawData = s.applyPricingOverrides(rawData)

	result := make(map[string]*ModelPriceEntry)
	skipped := 0
	var orphanCacheTiers, lopsidedLadders []string

	for modelName, rawEntry := range rawData {
		// 跳过 sample_spec 等文档条目
		if modelName == "sample_spec" {
			continue
		}

		// 尝试解析每个条目
		var entry RawModelPriceEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			skipped++
			continue
		}

		// 只保留有有效价格的条目
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil && entry.OutputCostPerImage == nil && entry.OutputCostPerImageToken == nil && entry.InputCostPerImageToken == nil {
			continue
		}
		if normalized, err := NormalizeModelPriceCurrency(entry.Currency); err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Model %s has unsupported currency %q; defaulting to USD", modelName, entry.Currency)
			entry.Currency = ModelPriceCurrencyUSD
		} else {
			entry.Currency = normalized
		}

		pricing := buildModelPriceEntry(modelName, &entry)
		pricing.LongContextThresholdInclusive = strings.EqualFold(strings.TrimSpace(entry.PricingCatalogProvider), "xai")
		hasExplicitLongContext := entry.LongContextInputTokenThreshold != nil ||
			entry.LongContextInputCostMultiplier != nil ||
			entry.LongContextOutputCostMultiplier != nil
		if !hasExplicitLongContext {
			deriveLongContextFromAboveTierFields(rawEntry, pricing)
			if isLopsidedLongContextLadder(pricing) {
				lopsidedLadders = append(lopsidedLadders, fmt.Sprintf("%s(input x%.2f, output x%.2f)", modelName,
					pricing.LongContextInputCostMultiplier, pricing.LongContextOutputCostMultiplier))
			}
		}
		if orphans := orphanCacheTierFields(rawEntry); len(orphans) > 0 {
			orphanCacheTiers = append(orphanCacheTiers, modelName+"("+strings.Join(orphans, ",")+")")
		}

		result[normalizePricingModelKey(modelName)] = pricing
	}

	if skipped > 0 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Skipped %d invalid entries", skipped)
	}
	warnOrphanCacheTierFields(orphanCacheTiers)
	warnLopsidedLongContextLadders(lopsidedLadders)

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid pricing entries found")
	}

	return result, nil
}

// deriveLongContextFromAboveTierFields 把 LiteLLM 目录的 *_above_XXXk_tokens 绝对价字段
// 折算成 long_context_* 阈值+倍率（sub2api 计费机制的内部表达）：阈值取自字段名，
// 倍率 = above 价 ÷ 基础价。条目显式携带任一 long_context_* 字段（含显式 0）时由
// 调用方跳过折算，以显式配置为准——显式写 threshold=0 或 multiplier=1 均可关闭该
// 模型的阶梯。多个阈值并存时取最小阈值。
// cache_read/cache_creation 的 above 档在计费中统一跟随输入倍率，不单独折算：
// 目录条目的 cache above 档须恰为基础价 × 输入倍率；缺基础价的 cache above 字段
// 无法参与计费，由 orphanCacheTierFields 哨兵告警。
func deriveLongContextFromAboveTierFields(rawEntry json.RawMessage, pricing *LiteLLMModelPricing) {
	if pricing == nil ||
		pricing.LongContextInputTokenThreshold > 0 ||
		pricing.LongContextInputCostMultiplier > 0 ||
		pricing.LongContextOutputCostMultiplier > 0 {
		return
	}
	if !bytes.Contains(rawEntry, []byte("_above_")) {
		return
	}
	var fields map[string]any
	if err := json.Unmarshal(rawEntry, &fields); err != nil {
		return
	}
	type tierPrices struct{ input, output float64 }
	tiers := make(map[int]*tierPrices)
	for key, value := range fields {
		m := aboveTierPricePattern.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		price, ok := value.(float64)
		if !ok || price <= 0 {
			continue
		}
		thousands, err := strconv.Atoi(m[2])
		if err != nil || thousands <= 0 {
			continue
		}
		threshold := thousands * 1000
		tp := tiers[threshold]
		if tp == nil {
			tp = &tierPrices{}
			tiers[threshold] = tp
		}
		if m[1] == "input" {
			tp.input = price
		} else {
			tp.output = price
		}
	}
	if len(tiers) == 0 {
		return
	}
	threshold := 0
	for t := range tiers {
		if threshold == 0 || t < threshold {
			threshold = t
		}
	}
	tp := tiers[threshold]
	inputMultiplier, outputMultiplier := 1.0, 1.0
	if tp.input > 0 && pricing.InputCostPerToken > 0 {
		inputMultiplier = tp.input / pricing.InputCostPerToken
	}
	if tp.output > 0 && pricing.OutputCostPerToken > 0 {
		outputMultiplier = tp.output / pricing.OutputCostPerToken
	}
	// above 价不高于基础价时视为无附加费，不生成阶梯。
	if inputMultiplier <= 1 && outputMultiplier <= 1 {
		return
	}
	pricing.LongContextInputTokenThreshold = threshold
	pricing.LongContextInputCostMultiplier = inputMultiplier
	pricing.LongContextOutputCostMultiplier = outputMultiplier
}

// isLopsidedLongContextLadder 判断折算出的阶梯是否只有一侧带附加费。官方阶梯（OpenAI、
// Google、Anthropic、xAI）都同时抬高 input 与 output；单侧附加费意味着条目的基础价与
// above 档来自不同价格版本（如基础价被手工 pin、above 档随上游更新），折算出的倍率失真。
func isLopsidedLongContextLadder(pricing *LiteLLMModelPricing) bool {
	if pricing == nil || pricing.LongContextInputTokenThreshold <= 0 {
		return false
	}
	return (pricing.LongContextInputCostMultiplier > 1) != (pricing.LongContextOutputCostMultiplier > 1)
}

// warnLopsidedLongContextLadders 对单侧附加费的折算阶梯打 WARN：应成组修正该条目的
// 基础价与 above 档（目录或 pricing.override_file）。
func warnLopsidedLongContextLadders(entries []string) {
	if len(entries) == 0 {
		return
	}
	sort.Strings(entries)
	total := len(entries)
	if total > 20 {
		entries = append(entries[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: %d model(s) derive a one-sided long-context ladder (surcharge on only input or only output); base prices and above-tier prices likely come from different price versions: %s", total, strings.Join(entries, ", "))
}

// orphanCacheTierFields 返回条目中没有对应基础价的 cache 侧 above 档字段名。
// cache 侧 above 档不参与计费取值，计费按"基础价 × 输入倍率"；基础价缺失或为 0 时，
// 该缓存分项在整个阶梯上都按 0 计。计费对变体有回落：服务档变体（_priority/_flex）
// 缺自身基础价时用标准基础价，1h 缓存写入缺 above_1hr 价时全部按 5m 价——因此沿
// 回落链任一基础价存在即不算孤儿。
func orphanCacheTierFields(rawEntry json.RawMessage) []string {
	if !bytes.Contains(rawEntry, []byte("_above_")) {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(rawEntry, &fields); err != nil {
		return nil
	}
	positive := func(key string) bool {
		price, ok := fields[key].(float64)
		return ok && price > 0
	}
	var orphans []string
	for key := range fields {
		m := cacheTierPricePattern.FindStringSubmatch(key)
		if m == nil || !positive(key) {
			continue
		}
		stem, hourly, tier := m[1], m[2], m[3]
		if positive(stem+hourly+tier) || positive(stem+hourly) || positive(stem+tier) || positive(stem) {
			continue
		}
		orphans = append(orphans, key)
	}
	sort.Strings(orphans)
	return orphans
}

// warnOrphanCacheTierFields 对带 cache 侧 above 档却没有基础价的条目打 WARN：
// 该缓存分项按 0 计费，目录或 pricing.override_file 补上基础价即可消除。
func warnOrphanCacheTierFields(entries []string) {
	if len(entries) == 0 {
		return
	}
	sort.Strings(entries)
	total := len(entries)
	if total > 20 {
		entries = append(entries[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: %d model(s) carry cache above-tier prices without a base cache price; that cache item bills at $0 until the catalog/override supplies the base: %s", total, strings.Join(entries, ", "))
}

// applyPricingOverrides 把 override 文件的条目逐字段修补进原始目录数据。目录与回退
// 文件的解析都经过 parsePricingData，因此 override 是最高优先级的数据源。这里只修补
// 已存在的条目：目录/回退里都没有的模型由 mergeOverrideOnlyModels 在两层数据合并后
// 统一并入——若在此处抢先建条目，纯 override 条目会挡住回退文件中同名完整条目的合并。
func (s *PricingService) applyPricingOverrides(rawData map[string]json.RawMessage) map[string]json.RawMessage {
	overrides := s.loadPricingOverrideEntries()
	if len(overrides) == 0 {
		return rawData
	}
	for name, patch := range overrides {
		base, ok := rawData[name]
		if !ok {
			continue
		}
		merged, valid := mergePricingOverrideEntry(base, patch)
		if !valid {
			logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override entry %q skipped: not a JSON object", name)
			continue
		}
		rawData[name] = merged
	}
	return rawData
}

// loadPricingOverrideEntries 读取 override 文件的原始条目。未配置返回 nil；
// 读取或解析失败打日志并跳过，不影响目录加载。
func (s *PricingService) loadPricingOverrideEntries() map[string]json.RawMessage {
	if s == nil || s.cfg == nil {
		return nil
	}
	path := strings.TrimSpace(s.cfg.Pricing.OverrideFile)
	if path == "" {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override merge skipped: %v", err)
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override merge skipped: %v", err)
		return nil
	}
	return entries
}

// mergePricingOverrideEntry 在 JSON 字段层浅合并：patch 字段覆盖 base 同名字段，
// 值为 null 的 patch 字段从结果中删除，base 为空时结果即 patch 本身。
// patch 不是 JSON 对象时返回 ok=false。
func mergePricingOverrideEntry(base, patch json.RawMessage) (json.RawMessage, bool) {
	var patchFields map[string]any
	if err := json.Unmarshal(patch, &patchFields); err != nil || patchFields == nil {
		return nil, false
	}
	merged := make(map[string]any, len(patchFields))
	if len(base) > 0 {
		// base 非对象时忽略，仅以 patch 为准。
		if err := json.Unmarshal(base, &merged); err != nil {
			merged = make(map[string]any, len(patchFields))
		}
	}
	for k, v := range patchFields {
		if v == nil {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, false
	}
	return out, true
}

// mergeOverrideOnlyModels 把 override 中目录/回退两层都不存在的模型作为独立条目并入
// （条目须自带价格字段才能通过有效性过滤），并对最终仍未生效的条目打 WARN：
// 模型名拼错、或纯补丁条目落在不存在的模型上时会被静默丢弃，让"已改价/已关阶梯"
// 的运营预期与实际计费脱节，这里是唯一的哨兵。
func (s *PricingService) mergeOverrideOnlyModels(data map[string]*LiteLLMModelPricing) map[string]*LiteLLMModelPricing {
	overrides := s.loadPricingOverrideEntries()
	if len(overrides) == 0 {
		return data
	}
	if data == nil {
		data = make(map[string]*LiteLLMModelPricing)
	}
	leftover := make(map[string]json.RawMessage)
	for name, patch := range overrides {
		if _, ok := data[name]; !ok {
			leftover[name] = patch
		}
	}
	if len(leftover) == 0 {
		return data
	}
	// 复用主解析路径（含 above_XXXk 折算与有效性过滤）；applyPricingOverrides
	// 对已存在条目做的自我修补是幂等的，不会二次改值。
	if body, err := json.Marshal(leftover); err == nil {
		if parsed, err := s.parsePricingData(body); err == nil {
			maps.Copy(data, parsed)
		}
	}
	var missing []string
	for name := range leftover {
		if _, ok := data[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return data
	}
	sort.Strings(missing)
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override had no effect for %d model(s): %s (unknown model name, or patch-only entry without price fields)", len(missing), strings.Join(missing, ", "))
	return data
}

// loadPricingData 从本地文件加载价格数据
func (s *PricingService) loadPricingData(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file failed: %w", err)
	}

	// 使用灵活的解析方式
	pricingData, err := s.parsePricingData(data)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}
	pricingData = s.mergeFallbackPricingData(pricingData)
	pricingData = s.mergeOverrideOnlyModels(pricingData)

	// 计算哈希
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	s.mu.Lock()
	warnDroppedLongContextLadders(s.catalogData, pricingData)
	s.catalogData = pricingData
	s.rebuildEffectiveLocked(s.catalogData)
	s.localHash = hashStr

	info, _ := os.Stat(filePath)
	if info != nil {
		s.lastUpdated = info.ModTime()
	} else {
		s.lastUpdated = time.Now()
	}
	s.mu.Unlock()

	logger.LegacyPrintf("service.pricing", "[Pricing] Loaded %d models from %s", len(pricingData), filePath)
	return nil
}

func (s *PricingService) mergeFallbackPricingData(data map[string]*LiteLLMModelPricing) map[string]*LiteLLMModelPricing {
	if data == nil {
		data = make(map[string]*LiteLLMModelPricing)
	}
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Pricing.FallbackFile) == "" {
		return data
	}
	fallbackBody, err := os.ReadFile(s.cfg.Pricing.FallbackFile)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Fallback merge skipped: %v", err)
		return data
	}
	fallbackData, err := s.parsePricingData(fallbackBody)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Fallback merge parse skipped: %v", err)
		return data
	}
	merged := 0
	for modelName, pricing := range fallbackData {
		if _, ok := data[modelName]; ok {
			continue
		}
		data[modelName] = pricing
		merged++
	}
	if merged > 0 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Merged %d fallback-only models", merged)
	}
	return data
}

// warnDroppedLongContextLadders 对比新旧目录数据：原本带长上下文阶梯的条目在新数据里
// 丢失阈值时打 WARN。阶梯已完全数据驱动（无代码兜底），数据源一次误提交就会把阶梯
// 静默变成基础价少收（07-14~08-21 漏收事故的形态），这里是唯一的哨兵。
// 调用方需持有 s.mu 写锁。
func warnDroppedLongContextLadders(old, next map[string]*LiteLLMModelPricing) {
	if len(old) == 0 {
		return
	}
	var dropped []string
	for name, prev := range old {
		if prev == nil || prev.LongContextInputTokenThreshold <= 0 {
			continue
		}
		if cur, ok := next[name]; ok && (cur == nil || cur.LongContextInputTokenThreshold <= 0) {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) == 0 {
		return
	}
	sort.Strings(dropped)
	total := len(dropped)
	if total > 20 {
		dropped = append(dropped[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Long-context ladder dropped for %d model(s) after reload: %s (verify catalog/override data if unintended)", total, strings.Join(dropped, ", "))
}

// useFallbackPricing 使用回退价格文件
func (s *PricingService) useFallbackPricing() error {
	fallbackFile := s.cfg.Pricing.FallbackFile

	if _, err := os.Stat(fallbackFile); os.IsNotExist(err) {
		return fmt.Errorf("fallback file not found: %s", fallbackFile)
	}

	logger.LegacyPrintf("service.pricing", "[Pricing] Using fallback file: %s", fallbackFile)

	// 复制到数据目录
	data, err := os.ReadFile(fallbackFile)
	if err != nil {
		return fmt.Errorf("read fallback failed: %w", err)
	}

	pricingFile := s.getPricingFilePath()
	if err := os.WriteFile(pricingFile, data, 0644); err != nil { //nolint:gosec // G703: 路径为配置的数据目录 + 硬编码文件名，非请求输入
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to copy fallback: %v", err)
	}

	return s.loadPricingData(fallbackFile)
}

// fetchRemoteHash 从远程获取哈希值
func (s *PricingService) fetchRemoteHash() (string, error) {
	hashURL, err := s.validatePricingURL(s.cfg.Pricing.HashURL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hash, err := s.remoteClient.FetchHashText(ctx, hashURL)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

func (s *PricingService) validatePricingURL(raw string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid pricing url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.PricingHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid pricing url: %w", err)
	}
	return normalized, nil
}

// GetModelPricing 获取模型价格（带模糊匹配）
func (s *PricingService) GetModelPricing(modelName string) *ModelPriceEntry {
	return s.GetModelPricingForPlatform("", modelName)
}

func (s *PricingService) GetModelPricingForPlatform(platform, modelName string) *ModelPriceEntry {
	return s.GetModelPricingForPlatforms([]string{platform}, modelName)
}

func (s *PricingService) GetModelPricingForPlatforms(platforms []string, modelName string) *ModelPriceEntry {
	pricing, _ := s.lookupModelPricingForPlatforms(platforms, modelName, true)
	return pricing
}

// LookupModelPricingStrict 只在"这个模型自己有价目条目"时返回价格，不做跨模型推断。
//
// GetModelPricing 回答的是"我能不能给这个模型算出一个数"，而不是"有没有人给这个模型
// 配过价"。两者的差距在最后两步：matchByModelFamily 会把任何含 opus/sonnet/haiku 的
// 未知型号按关键字粗分到某个系列，matchOpenAIModel 更是在剥掉 -codex/-mini/-max 后缀、
// 试过若干静态兜底之后，把**任何** gpt- 开头的模型兜到 DefaultTestModel 上。于是
// "gpt-<明年发布的新模型>" 在准入守卫看来是"有价的"，实际按 DefaultTestModel 收费。
//
// 这和媒体路由的 $0.134 占位价是同一种形态：不是免费，是一个和真实上游成本无关的
// 猜测值，账面上还"正常收费"了，比记成 0 更难发现。所以准入守卫需要一个更严的口径：
// 恰好命中这个模型自己的条目（含大小写/前缀别名、dash↔dot 拼写归一化，以及同一模型
// 不同日期快照之间的互认），才算"配过价"。
//
// 在线准入、实时后扣与 recovery 都使用严格口径。宽松 GetModelPricing 只保留给
// 明确需要兼容旧模型推断的非准入调用方；已经产生上游成本但严格价缺失时，账务链会
// 持久化 pricing_unavailable，而不是借别的 SKU 凑出一个金额。
func (s *PricingService) LookupModelPricingStrict(modelName string) *ModelPriceEntry {
	return s.LookupModelPricingStrictForPlatform("", modelName)
}

func (s *PricingService) LookupModelPricingStrictForPlatform(platform, modelName string) *ModelPriceEntry {
	return s.LookupModelPricingStrictForPlatforms([]string{platform}, modelName)
}

func (s *PricingService) LookupModelPricingStrictForPlatforms(platforms []string, modelName string) *ModelPriceEntry {
	pricing, _ := s.lookupModelPricingForPlatforms(platforms, modelName, false)
	return pricing
}

// lookupModelPricingForPlatforms 是上面两个入口的唯一实现。
//
// 合并成一份是刻意的：准入用严格口径、结算用宽松口径，两边对"前缀别名 / 拼写归一化"
// 的理解必须完全一致。各写一份迟早会在某个归一化分支上分叉，那时守卫放行的模型和结算
// 查价的模型就不是同一个了。allowInference 只控制最后两步跨模型推断开不开。
// lookupModelPricingForPlatforms checks every platform overlay before the
// shared catalog. This ordering is essential for composite routing: looking up
// the composite platform first through the old single-platform helper would
// immediately fall through to the shared catalog and mask a concrete provider
// override that should be the next candidate. It is the shared implementation
// for strict admission and permissive settlement lookup; allowInference only
// controls the final cross-model inference steps.
func (s *PricingService) lookupModelPricingForPlatforms(platforms []string, modelName string, allowInference bool) (*ModelPriceEntry, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if modelName == "" {
		return nil, ""
	}

	// 标准化模型名称（同时兼容 "models/xxx"、VertexAI 资源名等前缀）
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	lookupCandidates := s.buildModelLookupCandidates(modelLower)

	for _, platform := range normalizePricingPlatforms(platforms) {
		if overlay := s.platformOverrides[platform]; len(overlay) > 0 {
			if pricing := lookupIdentifiedIn(overlay, modelLower, lookupCandidates); pricing != nil {
				return pricing, platform
			}
		}
	}

	// 1~3. 确定性识别（精确名 / 已知拼写变体 / 去掉日期版本后缀）
	if pricing := lookupIdentifiedIn(s.pricingData, modelLower, lookupCandidates); pricing != nil {
		return pricing, basePricingPlatform(platforms)
	}
	if !allowInference {
		return nil, ""
	}

	// 4. 基于模型系列匹配（Claude）
	if pricing := s.matchByModelFamily(lookupCandidates[0]); inferredPricingCandidateAllowed(pricing) {
		return pricing, basePricingPlatform(platforms)
	}

	// 5. OpenAI 模型回退策略
	if strings.HasPrefix(lookupCandidates[0], "gpt-") {
		if pricing := s.matchOpenAIModel(lookupCandidates[0]); inferredPricingCandidateAllowed(pricing) {
			return pricing, basePricingPlatform(platforms)
		}
	}

	return nil, ""
}

// lookupIdentifiedModelPricingLocked 只做"确定性识别"的三步查找：精确键、已知拼写
// 变体、去掉日期/版本后缀后的同名条目。它刻意不包含 matchByModelFamily /
// matchOpenAIModel 这类按子串猜系列的兜底——那些兜底会给任意名字都返回一个价格。
// 调用方必须持有 s.mu 读锁。
func lookupIdentifiedIn(data map[string]*ModelPriceEntry, modelLower string, lookupCandidates []string) *LiteLLMModelPricing {
	if len(lookupCandidates) == 0 {
		return nil
	}

	// 1. 精确匹配
	for _, candidate := range lookupCandidates {
		if candidate == "" {
			continue
		}
		if pricing, ok := data[candidate]; ok {
			if sameSKUPricingCandidateAllowed(modelLower, candidate) {
				return pricing
			}
		}
	}

	// 2. 处理常见的模型名称变体
	// 2a. 定向替换：claude-opus-4-5-20251101 -> claude-opus-4.5-20251101
	for _, candidate := range lookupCandidates {
		normalized := strings.ReplaceAll(candidate, "-4-5-", "-4.5-")
		if pricing, ok := data[normalized]; ok && sameSKUPricingCandidateAllowed(modelLower, candidate) {
			return pricing
		}
	}
	// 2b. 通用 dash → dot 版本号归一化（覆盖 glm-5-1 → glm-5.1、
	// kimi-k2-5 → kimi-k2.5、minimax-m2-7 → minimax-m2.7 等 dash 版本命名）。
	// 前置的 \D 限定避免误伤已经是 dot 版本或纯 `-N`（如 -20251101）尾缀的情况。
	for _, candidate := range lookupCandidates {
		normalized := normalizeDashVersionSuffix(candidate)
		if normalized == candidate {
			continue
		}
		if pricing, ok := data[normalized]; ok && sameSKUPricingCandidateAllowed(modelLower, candidate) {
			return pricing
		}
	}

	// 3. 尝试模糊匹配（去掉版本号后缀）
	// claude-opus-4-5-20251101 -> claude-opus-4.5
	//
	// 严格口径只剥尾部的 8 位日期快照；宽松口径保留历史 extractBaseName 行为。
	// extractBaseName 还会删除任意位置的 8 位数字段和包含 ":" 的版本段，若用于
	// 严格匹配，会让 gpt-5.4-v1:0 等未知 SKU 借用 gpt-5.4 的价格。
	baseCandidate := modelLower
	baseName := strictPricingSnapshotBase(baseCandidate)
	for key, pricing := range data {
		keyBase := strictPricingSnapshotBase(strings.ToLower(key))
		if keyBase == baseName && inferredPricingCandidateAllowed(pricing) {
			return pricing
		}
	}

	return nil
}

// sameSKUPricingCandidateAllowed only accepts the requested SKU itself or an
// explicitly supported spelling of the same SKU.
func sameSKUPricingCandidateAllowed(requested, candidate string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if requested == "" || candidate == "" {
		return false
	}
	if candidate == requested {
		return true
	}
	aliasTarget, ok := explicitPricingAliasTarget(requested)
	if !ok {
		return false
	}
	if candidate == aliasTarget {
		return true
	}
	return candidate == normalizeModelNameForPricing(requested)
}

func explicitPricingAliasTarget(model string) (string, bool) {
	if !strings.Contains(model, "/") {
		return model, true
	}
	for _, prefix := range []string{"models/", "openai/", "publishers/google/models/"} {
		if target, ok := strings.CutPrefix(model, prefix); ok {
			return target, target != "" && !strings.Contains(target, "/")
		}
	}
	parts := strings.Split(model, "/")
	if len(parts) != 8 || parts[0] != "projects" || parts[1] == "" ||
		parts[2] != "locations" || parts[3] == "" || parts[4] != "publishers" ||
		parts[5] != "google" || parts[6] != "models" || parts[7] == "" {
		return "", false
	}
	return parts[7], true
}

func inferredPricingCandidateAllowed(pricing *LiteLLMModelPricing) bool {
	return pricing != nil && pricing.InputCostPerToken > 0 && pricing.OutputCostPerToken > 0
}

// GetIdentifiedModelPricing 在价格表中确定性地识别模型，识别不到时返回 nil。
// 与 GetModelPricing 的区别：不会退化成按 "opus"/"haiku" 之类子串猜出的系列兜底价。
// 用于必须区分"这是价格表里已知的模型"和"这只是名字里带某个关键词"的场景。
func (s *PricingService) GetIdentifiedModelPricing(modelName string) *LiteLLMModelPricing {
	return s.GetIdentifiedModelPricingForPlatform("", modelName)
}

func (s *PricingService) GetIdentifiedModelPricingForPlatform(platform, modelName string) *LiteLLMModelPricing {
	return s.GetIdentifiedModelPricingForPlatforms([]string{platform}, modelName)
}

func (s *PricingService) GetIdentifiedModelPricingForPlatforms(platforms []string, modelName string) *LiteLLMModelPricing {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return nil
	}
	candidates := s.buildModelLookupCandidates(modelLower)
	for _, platform := range normalizePricingPlatforms(platforms) {
		if overlay := s.platformOverrides[platform]; len(overlay) > 0 {
			if pricing := lookupIdentifiedIn(overlay, modelLower, candidates); pricing != nil {
				return pricing
			}
		}
	}
	return lookupIdentifiedIn(s.pricingData, modelLower, candidates)
}

func (s *PricingService) buildModelLookupCandidates(modelLower string) []string {
	rawCandidates := []string{
		modelLower,
		strings.TrimPrefix(modelLower, "models/"),
		lastSegment(modelLower),
		lastSegment(strings.TrimPrefix(modelLower, "models/")),
	}
	normalized := normalizeModelNameForPricing(modelLower)

	// A tier-specific entry should take precedence when the pricing catalog gains
	// one later. Today Antigravity's Gemini 3.6 Flash tiers share the base rate,
	// so the normalized base remains the fallback after the exact aliases.
	candidates := rawCandidates
	if normalizeGeminiThinkingTierAlias(lastSegment(modelLower)) != lastSegment(modelLower) {
		candidates = append(candidates, normalized)
	} else {
		// Prefer canonical model names for all other aliases (including models/xxx).
		candidates = append([]string{normalized}, candidates...)
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return []string{modelLower}
	}
	return out
}

// normalizeDashVersionSuffix 把 dash 分隔的版本号改成 dot 分隔的形式，
// 只影响 `<非数字><digits>-<digits>` 位置，避免误伤 -20251101 之类的日期尾缀。
//
//	glm-5-1              -> glm-5.1
//	kimi-k2-5            -> kimi-k2.5
//	claude-opus-4-5-...  -> claude-opus-4.5-... （首次匹配即返回，日期段不受影响）
//	claude-opus-4-8      -> claude-opus-4.8
//	deepseek-v4          -> deepseek-v4        （无 -digit 尾缀，不变）
func normalizeDashVersionSuffix(model string) string {
	return dashVersionSuffixPattern.ReplaceAllString(model, "${1}${2}.${3}")
}

// strictPricingSnapshotBase canonicalizes only spelling and a trailing
// -YYYYMMDD release snapshot that is both a real calendar date and within a
// plausible modern-model release window. It deliberately does not use
// extractBaseName: embedded/invalid/far-future numeric segments are part of the
// SKU and must not inherit another model's price under the fail-closed lookup.
func strictPricingSnapshotBase(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if lastDash := strings.LastIndexByte(model, '-'); lastDash >= 0 {
		suffix := model[lastDash+1:]
		if len(suffix) == 8 && isNumeric(suffix) {
			if snapshotDate, err := time.Parse("20060102", suffix); err == nil {
				earliest := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
				latest := time.Now().UTC().AddDate(1, 0, 0)
				if !snapshotDate.Before(earliest) && !snapshotDate.After(latest) {
					model = model[:lastDash]
				}
			}
		}
	}
	return normalizeDashVersionSuffix(model)
}

func normalizeModelNameForPricing(model string) string {
	// Common Gemini/VertexAI forms:
	// - models/gemini-2.0-flash-exp
	// - publishers/google/models/gemini-2.5-pro
	// - projects/.../locations/.../publishers/google/models/gemini-2.5-pro
	model = strings.TrimSpace(model)
	model = strings.TrimLeft(model, "/")
	model = strings.TrimPrefix(model, "models/")
	model = strings.TrimPrefix(model, "publishers/google/models/")

	if idx := strings.LastIndex(model, "/publishers/google/models/"); idx != -1 {
		model = model[idx+len("/publishers/google/models/"):]
	}
	if idx := strings.LastIndex(model, "/models/"); idx != -1 {
		model = model[idx+len("/models/"):]
	}

	model = strings.TrimLeft(model, "/")
	if canonical := canonicalizeOpenAIModelAliasSpelling(model); canonical != "" {
		if canonical == "gpt-5.6" {
			return "gpt-5.6-sol"
		}
		if suffix, ok := strings.CutPrefix(canonical, "gpt-5.6-"); ok && (suffix == "max" || isKnownCodexModelSuffix(suffix)) {
			return "gpt-5.6-sol"
		}
		return canonical
	}
	return normalizeGeminiThinkingTierAlias(model)
}

// normalizeGeminiThinkingTierAlias maps Antigravity's Gemini 3.6 Flash
// thinking-tier model IDs to the public base model. The tier controls reasoning
// behavior, not the published token rate, so this keeps -high/-low/-medium and
// -tiered requests on the same price card as gemini-3.6-flash.
func normalizeGeminiThinkingTierAlias(model string) string {
	const baseModel = "gemini-3.6-flash"
	for _, tier := range []string{"-high", "-low", "-medium", "-tiered"} {
		if model == baseModel+tier {
			return baseModel
		}
	}
	return model
}

func lastSegment(model string) string {
	if idx := strings.LastIndex(model, "/"); idx != -1 {
		return model[idx+1:]
	}
	return model
}

// matchByModelFamily 基于模型系列匹配
func (s *PricingService) matchByModelFamily(model string) *ModelPriceEntry {
	// modelFamily 定义一个模型系列的匹配和定价查找规则。
	type modelFamily struct {
		name    string   // 系列名称
		match   []string // 用于将模型归类到此系列的模式（strings.Contains 匹配）
		pricing []string // 用于在定价数据中查找价格的模式（nil 则复用 match；可包含低版本 fallback）
	}

	// 按特异性降序排列：高版本号在前，避免 "claude-opus-4"（opus-4 系列）
	// 因子串关系误匹配 "claude-opus-4-7"（opus-4.7 系列）。
	// 注意：原 map 实现存在 Go map 迭代随机性导致的同类 bug，此处改为有序切片修复。
	families := []modelFamily{
		// Opus 5 与 Opus 4.8 同价（$5/$25 per MTok）。定价数据缺失 claude-opus-5 时
		// 必须回退到 4.8，否则会掉进 "opus-4" 系列按 $15/$75 计费（3 倍超收）。
		{name: "opus-5", match: []string{"claude-opus-5"}, pricing: []string{"claude-opus-5", "claude-opus-4-8"}},
		{name: "opus-4.8", match: []string{"claude-opus-4-8", "claude-opus-4.8"}, pricing: []string{"claude-opus-4-8", "claude-opus-4.8", "claude-opus-4-7"}},
		{name: "opus-4.7", match: []string{"claude-opus-4-7", "claude-opus-4.7"}, pricing: []string{"claude-opus-4-7", "claude-opus-4.7", "claude-opus-4-6"}},
		{name: "opus-4.6", match: []string{"claude-opus-4-6", "claude-opus-4.6"}},
		{name: "opus-4.5", match: []string{"claude-opus-4-5", "claude-opus-4.5"}},
		{name: "opus-4", match: []string{"claude-opus-4", "claude-3-opus"}},
		{name: "sonnet-4.5", match: []string{"claude-sonnet-4-5", "claude-sonnet-4.5"}},
		{name: "sonnet-4", match: []string{"claude-sonnet-4", "claude-3-5-sonnet"}},
		{name: "sonnet-3.5", match: []string{"claude-3-5-sonnet", "claude-3.5-sonnet"}},
		{name: "sonnet-3", match: []string{"claude-3-sonnet"}},
		{name: "haiku-3.5", match: []string{"claude-3-5-haiku", "claude-3.5-haiku"}},
		{name: "haiku-3", match: []string{"claude-3-haiku"}},
	}

	// Phase 1: 按有序切片归类（最具体的系列优先匹配）
	var matched *modelFamily
	for i := range families {
		for _, pattern := range families[i].match {
			if strings.Contains(model, pattern) || strings.Contains(model, strings.ReplaceAll(pattern, "-", "")) {
				matched = &families[i]
				break
			}
		}
		if matched != nil {
			break
		}
	}

	// Phase 2: 二次兜底——当模型 ID 不含已知模式串时，按关键字粗分
	if matched == nil {
		var fallbackName string
		switch {
		case strings.Contains(model, "opus"):
			switch {
			// "opus-5" 必须先判：不能用裸 "5" 匹配，否则 claude-opus-4-5 会被误判。
			case strings.Contains(model, "opus-5") || strings.Contains(model, "opus5"):
				fallbackName = "opus-5"
			case strings.Contains(model, "4.8") || strings.Contains(model, "4-8"):
				fallbackName = "opus-4.8"
			case strings.Contains(model, "4.7") || strings.Contains(model, "4-7"):
				fallbackName = "opus-4.7"
			case strings.Contains(model, "4.6") || strings.Contains(model, "4-6"):
				fallbackName = "opus-4.6"
			case strings.Contains(model, "4.5") || strings.Contains(model, "4-5"):
				fallbackName = "opus-4.5"
			default:
				fallbackName = "opus-4"
			}
		case strings.Contains(model, "sonnet"):
			switch {
			case strings.Contains(model, "4.5") || strings.Contains(model, "4-5"):
				fallbackName = "sonnet-4.5"
			case strings.Contains(model, "3-5") || strings.Contains(model, "3.5"):
				fallbackName = "sonnet-3.5"
			default:
				fallbackName = "sonnet-4"
			}
		case strings.Contains(model, "haiku"):
			switch {
			case strings.Contains(model, "3-5") || strings.Contains(model, "3.5"):
				fallbackName = "haiku-3.5"
			default:
				fallbackName = "haiku-3"
			}
		}
		if fallbackName != "" {
			for i := range families {
				if families[i].name == fallbackName {
					matched = &families[i]
					break
				}
			}
		}
	}

	if matched == nil {
		return nil
	}

	// Phase 3: 在定价数据中查找该系列的价格
	lookups := matched.pricing
	if lookups == nil {
		lookups = matched.match
	}
	for _, pattern := range lookups {
		for key, pricing := range s.pricingData {
			keyLower := strings.ToLower(key)
			if strings.Contains(keyLower, pattern) {
				logger.LegacyPrintf("service.pricing", "[Pricing] Fuzzy matched %s -> %s", model, key)
				return pricing
			}
		}
	}

	return nil
}

// matchOpenAIModel OpenAI 模型回退匹配策略
// 回退顺序：
// 1. gpt-5.3-codex-spark* -> gpt-5.1-codex（按业务要求固定计费）
// 2. gpt-5.2-codex -> gpt-5.2（去掉后缀如 -codex, -mini, -max 等）
// 3. gpt-5.2-20251222 -> gpt-5.2（去掉日期版本号）
// 4. gpt-5.3-codex -> gpt-5.2-codex
// 5. gpt-5.4* -> 业务静态兜底价
// 6. 最终回退到 DefaultTestModel (gpt-5.1-codex)
func (s *PricingService) matchOpenAIModel(model string) *ModelPriceEntry {
	if strings.HasPrefix(model, "gpt-5.3-codex-spark") {
		if pricing, ok := s.pricingData["gpt-5.1-codex"]; ok {
			logger.LegacyPrintf("service.pricing", "[Pricing][SparkBilling] %s -> %s billing", model, "gpt-5.1-codex")
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.1-codex"))
			return pricing
		}
	}

	// 尝试的回退变体
	variants := s.generateOpenAIModelVariants(model, openAIModelDatePattern)

	for _, variant := range variants {
		if pricing, ok := s.pricingData[variant]; ok {
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, variant))
			return pricing
		}
	}

	if strings.HasPrefix(model, "gpt-5.3-codex") {
		if pricing, ok := s.pricingData["gpt-5.2-codex"]; ok {
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.2-codex"))
			return pricing
		}
	}

	if strings.HasPrefix(model, "gpt-5.6-sol") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-sol(static)"))
		return openAIGPT56SolFallbackPricing
	}
	if strings.HasPrefix(model, "gpt-5.6-terra") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-terra(static)"))
		return openAIGPT56TerraFallbackPricing
	}
	if strings.HasPrefix(model, "gpt-5.6-luna") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-luna(static)"))
		return openAIGPT56LunaFallbackPricing
	}

	// GPT-5.5 回退到 GPT-5.4 定价
	if strings.HasPrefix(model, "gpt-5.5") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4(static)"))
		return openAIGPT54FallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4-mini") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4-mini(static)"))
		return openAIGPT54MiniFallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4-nano") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4-nano(static)"))
		return openAIGPT54NanoFallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4(static)"))
		return openAIGPT54FallbackPricing
	}

	if isOpenAIImageGenerationModel(model) {
		for _, candidate := range []string{"gpt-image-2", "gpt-image-1.5", "gpt-image-1"} {
			if pricing, ok := s.pricingData[candidate]; ok {
				logger.LegacyPrintf("service.pricing", "[Pricing] OpenAI image fallback matched %s -> %s", model, candidate)
				return pricing
			}
		}
		return nil
	}

	// 最终回退到 DefaultTestModel
	defaultModel := strings.ToLower(openai.DefaultTestModel)
	if pricing, ok := s.pricingData[defaultModel]; ok {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenAI fallback to default model %s -> %s", model, defaultModel)
		return pricing
	}

	return nil
}

// generateOpenAIModelVariants 生成 OpenAI 模型的回退变体列表
func (s *PricingService) generateOpenAIModelVariants(model string, datePattern *regexp.Regexp) []string {
	seen := make(map[string]bool)
	var variants []string

	addVariant := func(v string) {
		if v != model && !seen[v] {
			seen[v] = true
			variants = append(variants, v)
		}
	}

	// 1. 去掉日期版本号: gpt-5.2-20251222 -> gpt-5.2
	withoutDate := datePattern.ReplaceAllString(model, "")
	if withoutDate != model {
		addVariant(withoutDate)
	}

	// 2. 提取基础版本号: gpt-5.2-codex -> gpt-5.2
	// 只匹配纯数字版本号格式 gpt-X 或 gpt-X.Y，不匹配 gpt-4o 这种带字母后缀的
	if matches := openAIModelBasePattern.FindStringSubmatch(model); len(matches) > 1 {
		addVariant(matches[1])
	}

	// 3. 同时去掉日期后再提取基础版本号
	if withoutDate != model {
		if matches := openAIModelBasePattern.FindStringSubmatch(withoutDate); len(matches) > 1 {
			addVariant(matches[1])
		}
	}

	return variants
}

// GetStatus 获取服务状态
func (s *PricingService) GetStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"model_count":         len(s.pricingData),
		"catalog_model_count": len(s.catalogData),
		"override_count":      len(s.overrideRows),
		"last_updated":        s.lastUpdated,
		"local_hash":          s.localHash[:min(8, len(s.localHash))],
	}
}

// ForceUpdate 强制更新
func (s *PricingService) ForceUpdate() error {
	return s.downloadPricingData()
}

func (s *PricingService) ForceUpdateWithOverrideCount() (map[string]any, error) {
	if err := s.ForceUpdate(); err != nil {
		return nil, err
	}
	status := s.GetStatus()
	s.mu.RLock()
	reapplied := 0
	for i := range s.overrideRows {
		if s.overrideRows[i].Enabled {
			reapplied++
		}
	}
	s.mu.RUnlock()
	status["overrides_reapplied"] = reapplied
	return status, nil
}

// getPricingFilePath 获取价格文件路径
func (s *PricingService) getPricingFilePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "model_pricing.json")
}

// getHashFilePath 获取哈希文件路径
func (s *PricingService) getHashFilePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "model_pricing.sha256")
}

// ListModelNamesByProvider returns all model names in the catalog whose
// litellm_provider schema field matches the given provider string
// (case-insensitive).
// The returned slice is sorted alphabetically.
func (s *PricingService) ListModelNamesByProvider(provider string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provider = strings.ToLower(strings.TrimSpace(provider))
	names := make([]string, 0)
	for name, p := range s.pricingData {
		if strings.ToLower(p.PricingCatalogProvider) == provider {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// isNumeric 检查字符串是否为纯数字
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
