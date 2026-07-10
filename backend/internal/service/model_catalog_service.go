package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/singleflight"
)

var errModelCatalogRefreshBackoff = errors.New("model catalog refresh suppressed by failure backoff")

type modelCatalogAsyncRefresh struct {
	_ byte
}

// ModelCatalogService resolves the model catalog exposed by an account.
type ModelCatalogService struct {
	accountRepo    AccountRepository
	groupRepo      GroupRepository
	channelService *ChannelService
	discoverer     ModelDiscoverer
	cfg            config.ModelCatalogConfig
	cache          *modelCatalogCache
	refreshGroup   singleflight.Group
	refreshSem     chan struct{}
	asyncRefreshMu sync.Mutex
	asyncRefreshes map[int64]*modelCatalogAsyncRefresh
	now            func() time.Time
}

// NewModelCatalogService creates an account-scoped model catalog resolver.
func NewModelCatalogService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	channelService *ChannelService,
	discoverer ModelDiscoverer,
	cfg config.ModelCatalogConfig,
) *ModelCatalogService {
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &ModelCatalogService{
		accountRepo:    accountRepo,
		groupRepo:      groupRepo,
		channelService: channelService,
		discoverer:     discoverer,
		cfg:            cfg,
		cache:          newModelCatalogCache(),
		refreshSem:     make(chan struct{}, maxConcurrency),
		asyncRefreshes: make(map[int64]*modelCatalogAsyncRefresh),
		now:            time.Now,
	}
}

// ListForAccount returns the model IDs exposed by one account.
func (s *ModelCatalogService) ListForAccount(ctx context.Context, account *Account, waitForLive bool) ([]string, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required for model catalog resolution", nil)
	}

	defaults := DefaultModelCatalogIDs(account.Platform)
	fallback := configuredOrDefaultAccountModels(account, defaults)
	if !accountRequiresLiveCatalog(account) {
		return fallback, nil
	}

	if s != nil && s.cache != nil {
		entry, state := s.cache.loadState(account.ID, s.currentTime(), s.freshTTL(), s.staleTTL())
		switch state {
		case catalogCacheFresh:
			return entry.models, nil
		case catalogCacheStale:
			s.RefreshAccountAsync(account)
			return entry.models, nil
		}
	}

	if !waitForLive {
		if s != nil {
			s.RefreshAccountAsync(account)
		}
		return fallback, nil
	}

	models, err := s.RefreshAccount(ctx, account)
	if err == nil {
		return models, nil
	}
	if s != nil && s.cache != nil {
		if entry, ok := s.cache.load(account.ID); ok && len(entry.models) > 0 {
			return entry.models, nil
		}
	}
	return fallback, nil
}

// RefreshAccount refreshes and caches one account's live model catalog.
func (s *ModelCatalogService) RefreshAccount(ctx context.Context, account *Account) ([]string, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required for model catalog refresh", nil)
	}
	return s.refreshAccountForGeneration(ctx, account, s.cacheGeneration(account.ID))
}

func (s *ModelCatalogService) refreshAccountForGeneration(ctx context.Context, account *Account, generation uint64) ([]string, error) {
	if !accountRequiresLiveCatalog(account) {
		return configuredOrDefaultAccountModels(account, DefaultModelCatalogIDs(account.Platform)), nil
	}
	if s == nil || s.discoverer == nil {
		return nil, newUpstreamModelSyncConfigError("Account model discoverer is not configured", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	accountID := account.ID
	value, err, _ := s.refreshGroup.Do(modelCatalogRefreshKey(accountID, generation), func() (any, error) {
		now := s.currentTime()
		if s.cache != nil && s.cache.inFailureBackoff(accountID, now, s.failureBackoff()) {
			return nil, errModelCatalogRefreshBackoff
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, s.requestTimeout())
		defer cancel()
		if s.refreshSem != nil {
			select {
			case s.refreshSem <- struct{}{}:
				defer func() { <-s.refreshSem }()
			case <-timeoutCtx.Done():
				refreshErr := timeoutCtx.Err()
				if errors.Is(refreshErr, context.DeadlineExceeded) && s.cache != nil {
					s.cache.storeFailureForGeneration(accountID, s.currentTime(), generation)
				}
				return nil, refreshErr
			}
		}

		models, discoverErr := s.discoverer.Discover(timeoutCtx, account)
		if discoverErr != nil {
			callerCanceled := errors.Is(discoverErr, context.Canceled) || errors.Is(timeoutCtx.Err(), context.Canceled)
			if s.cache != nil && !callerCanceled {
				s.cache.storeFailureForGeneration(accountID, s.currentTime(), generation)
			}
			return nil, discoverErr
		}
		models = normalizeCatalogModelIDs(models)
		if len(models) == 0 {
			discoverErr = newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
			if s.cache != nil {
				s.cache.storeFailureForGeneration(accountID, s.currentTime(), generation)
			}
			return nil, discoverErr
		}
		if s.cache != nil {
			s.cache.storeSuccessForGeneration(accountID, models, s.currentTime(), generation)
		}
		return cloneStringSlice(models), nil
	})
	if err != nil {
		return nil, err
	}
	models, _ := value.([]string)
	return cloneStringSlice(models), nil
}

// RefreshAccountAsync schedules a best-effort refresh for an eligible account.
func (s *ModelCatalogService) RefreshAccountAsync(account *Account) {
	if s == nil || account == nil || account.Status != StatusActive || !account.Schedulable || !accountRequiresLiveCatalog(account) {
		return
	}
	admission, generation, admitted := s.beginAsyncRefresh(account.ID)
	if !admitted {
		return
	}
	snapshot := *account
	go func() {
		defer s.finishAsyncRefresh(snapshot.ID, admission)
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("model_catalog_async_refresh_panic", "account_id", snapshot.ID, "recover", recovered)
			}
		}()
		_, _ = s.refreshAccountForGeneration(context.Background(), &snapshot, generation)
	}()
}

// InvalidateAccount removes one account's cached model catalog.
func (s *ModelCatalogService) InvalidateAccount(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.asyncRefreshMu.Lock()
	oldGeneration := uint64(0)
	if s.cache != nil {
		oldGeneration = s.cache.invalidate(accountID)
	}
	s.refreshGroup.Forget(modelCatalogRefreshKey(accountID, oldGeneration))
	delete(s.asyncRefreshes, accountID)
	s.asyncRefreshMu.Unlock()
}

func (s *ModelCatalogService) beginAsyncRefresh(accountID int64) (*modelCatalogAsyncRefresh, uint64, bool) {
	s.asyncRefreshMu.Lock()
	defer s.asyncRefreshMu.Unlock()
	if _, exists := s.asyncRefreshes[accountID]; exists {
		return nil, 0, false
	}
	if s.asyncRefreshes == nil {
		s.asyncRefreshes = make(map[int64]*modelCatalogAsyncRefresh)
	}
	admission := &modelCatalogAsyncRefresh{}
	s.asyncRefreshes[accountID] = admission
	return admission, s.cacheGeneration(accountID), true
}

func (s *ModelCatalogService) finishAsyncRefresh(accountID int64, admission *modelCatalogAsyncRefresh) {
	s.asyncRefreshMu.Lock()
	defer s.asyncRefreshMu.Unlock()
	if s.asyncRefreshes[accountID] == admission {
		delete(s.asyncRefreshes, accountID)
	}
}

func (s *ModelCatalogService) cacheGeneration(accountID int64) uint64 {
	if s == nil || s.cache == nil {
		return 0
	}
	return s.cache.generation(accountID)
}

func modelCatalogRefreshKey(accountID int64, generation uint64) string {
	accountKey := strconv.FormatInt(accountID, 10)
	if generation == 0 {
		return accountKey
	}
	return accountKey + ":" + strconv.FormatUint(generation, 10)
}

func (s *ModelCatalogService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *ModelCatalogService) freshTTL() time.Duration {
	if s != nil && s.cfg.RefreshIntervalSeconds > 0 {
		return time.Duration(s.cfg.RefreshIntervalSeconds) * time.Second
	}
	return 5 * time.Minute
}

func (s *ModelCatalogService) staleTTL() time.Duration {
	if s != nil && s.cfg.StaleTTLSeconds > 0 {
		return time.Duration(s.cfg.StaleTTLSeconds) * time.Second
	}
	return 24 * time.Hour
}

func (s *ModelCatalogService) failureBackoff() time.Duration {
	if s != nil && s.cfg.FailureBackoffSeconds > 0 {
		return time.Duration(s.cfg.FailureBackoffSeconds) * time.Second
	}
	return time.Minute
}

func (s *ModelCatalogService) requestTimeout() time.Duration {
	if s != nil && s.cfg.RequestTimeoutSeconds > 0 {
		return time.Duration(s.cfg.RequestTimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

// ListForPlatform returns the union of model IDs exposed by schedulable accounts in scope.
func (s *ModelCatalogService) ListForPlatform(ctx context.Context, groupID *int64, platform string, waitForLive bool) ([]string, error) {
	var (
		accounts []Account
		err      error
	)
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
	} else {
		accounts, err = s.accountRepo.ListSchedulableByPlatform(ctx, platform)
	}
	if err != nil {
		return nil, err
	}
	return s.listFromAccounts(ctx, accounts, platform, waitForLive)
}

func (s *ModelCatalogService) listFromAccounts(ctx context.Context, accounts []Account, platform string, waitForLive bool) ([]string, error) {
	models := make([]string, 0)
	for i := range accounts {
		account := &accounts[i]
		if account.Status != StatusActive || !account.Schedulable || account.Platform != platform {
			continue
		}
		accountModels, err := s.ListForAccount(ctx, account, waitForLive)
		if err != nil {
			continue
		}
		models = append(models, accountModels...)
	}
	return normalizeCatalogModelIDs(models), nil
}

// ListForGroup returns the final ordered model list exposed by a group.
func (s *ModelCatalogService) ListForGroup(ctx context.Context, groupID int64, platform string) ([]string, error) {
	return s.listForGroup(ctx, groupID, platform, true)
}

// ListGroupCandidates returns all account and channel models available to a group before its custom-list filter.
func (s *ModelCatalogService) ListGroupCandidates(ctx context.Context, groupID int64, platform string) ([]string, error) {
	return s.listGroupCandidates(ctx, groupID, platform, true)
}

func (s *ModelCatalogService) listForGroup(ctx context.Context, groupID int64, platform string, waitForLive bool) ([]string, error) {
	return s.listForGroupWithMemo(ctx, groupID, platform, waitForLive, make(map[int64][]string))
}

func (s *ModelCatalogService) resolveAccountOnce(ctx context.Context, memo map[int64][]string, account *Account, waitForLive bool) ([]string, error) {
	if models, ok := memo[account.ID]; ok {
		return cloneStringSlice(models), nil
	}
	models, err := s.ListForAccount(ctx, account, waitForLive)
	if err != nil {
		return nil, err
	}
	memo[account.ID] = cloneStringSlice(models)
	return cloneStringSlice(models), nil
}

func (s *ModelCatalogService) listForGroupWithMemo(ctx context.Context, groupID int64, platform string, waitForLive bool, memo map[int64][]string) ([]string, error) {
	candidates, group, err := s.listGroupCandidatesWithMemo(ctx, groupID, platform, waitForLive, memo)
	if err != nil || group == nil || !group.ModelsListConfig.Enabled {
		return candidates, err
	}
	return ApplyGroupModelsList(candidates, group.ModelsListConfig.Models), nil
}

func (s *ModelCatalogService) listGroupCandidates(ctx context.Context, groupID int64, platform string, waitForLive bool) ([]string, error) {
	candidates, _, err := s.listGroupCandidatesWithMemo(ctx, groupID, platform, waitForLive, make(map[int64][]string))
	return candidates, err
}

func (s *ModelCatalogService) listGroupCandidatesWithMemo(ctx context.Context, groupID int64, platform string, waitForLive bool, memo map[int64][]string) ([]string, *Group, error) {
	platform = strings.TrimSpace(platform)
	if groupID <= 0 {
		if platform == "" {
			platform = PlatformAnthropic
		}
		models, err := s.ListForPlatform(ctx, nil, platform, waitForLive)
		return models, nil, err
	}

	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	if platform == "" {
		platform = group.Platform
	}
	if group.Platform != platform {
		return nil, group, nil
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
	if err != nil {
		return nil, nil, err
	}
	models := make([]string, 0)
	for i := range accounts {
		account := &accounts[i]
		if account.Status != StatusActive || !account.Schedulable || account.Platform != platform {
			continue
		}
		resolved, resolveErr := s.resolveAccountOnce(ctx, memo, account, waitForLive)
		if resolveErr == nil {
			models = append(models, resolved...)
		}
	}
	if s.channelService != nil {
		channel, channelErr := s.channelService.GetChannelForGroup(ctx, groupID)
		if channelErr != nil {
			return nil, nil, channelErr
		}
		for _, model := range channel.SupportedModels() {
			if model.Platform == platform {
				models = append(models, model.Name)
			}
		}
	}
	return normalizeCatalogModelIDs(models), group, nil
}

// ListForAPIKey returns the group-scoped catalog for an authenticated API key.
func (s *ModelCatalogService) ListForAPIKey(ctx context.Context, apiKey *APIKey) ([]string, error) {
	if apiKey == nil {
		return nil, newUpstreamModelSyncConfigError("API key is required for model catalog resolution", nil)
	}
	if apiKey.Group == nil {
		if apiKey.GroupID != nil {
			return nil, newUpstreamModelSyncConfigError("API key group must be loaded for model catalog resolution", nil)
		}
		return DefaultModelCatalogIDs(PlatformAnthropic), nil
	}
	return s.ListForGroup(ctx, apiKey.Group.ID, apiKey.Group.Platform)
}

// ListPublic returns catalogs for active non-exclusive groups, unioned by platform.
func (s *ModelCatalogService) ListPublic(ctx context.Context) (map[string][]string, error) {
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	memo := make(map[int64][]string)
	for i := range groups {
		group := &groups[i]
		if group.IsExclusive {
			continue
		}
		models, listErr := s.listForGroupWithMemo(ctx, group.ID, group.Platform, false, memo)
		if listErr != nil {
			return nil, listErr
		}
		result[group.Platform] = append(result[group.Platform], models...)
	}
	for platform, models := range result {
		result[platform] = normalizeCatalogModelIDs(models)
	}
	return result, nil
}

func accountRequiresLiveCatalog(account *Account) bool {
	if account == nil {
		return false
	}
	switch account.Platform {
	case PlatformAnthropic:
		return account.Type == AccountTypeOAuth ||
			account.Type == AccountTypeSetupToken ||
			(account.Type == AccountTypeAPIKey && account.IsAnthropicAPIKeyPassthroughEnabled())
	case PlatformOpenAI:
		return account.Type == AccountTypeOAuth ||
			(account.Type == AccountTypeAPIKey && account.IsOpenAIPassthroughEnabled())
	case PlatformGemini:
		return geminiOAuthSupportsUpstreamModelDiscovery(account)
	case PlatformAntigravity:
		return account.Type == AccountTypeOAuth || account.Type == AccountTypeUpstream
	case PlatformWindsurf, PlatformOpenCode:
		return account.Type == AccountTypeAPIKey
	default:
		return false
	}
}

func configuredOrDefaultAccountModels(account *Account, defaults []string) []string {
	patterns := configuredAccountModelPatterns(account)
	if len(patterns) == 0 {
		return defaults
	}
	return expandModelPatterns(patterns, defaults)
}

func configuredAccountModelPatterns(account *Account) []string {
	if account == nil || account.Credentials == nil {
		return nil
	}

	var mappingKeys []string
	switch mapping := account.Credentials["model_mapping"].(type) {
	case map[string]any:
		mappingKeys = make([]string, 0, len(mapping))
		for source := range mapping {
			mappingKeys = append(mappingKeys, source)
		}
	case map[string]string:
		mappingKeys = make([]string, 0, len(mapping))
		for source := range mapping {
			mappingKeys = append(mappingKeys, source)
		}
	}
	if len(mappingKeys) > 0 {
		sort.Slice(mappingKeys, func(i, j int) bool {
			left := strings.ToLower(strings.TrimSpace(mappingKeys[i]))
			right := strings.ToLower(strings.TrimSpace(mappingKeys[j]))
			if left == right {
				return strings.TrimSpace(mappingKeys[i]) < strings.TrimSpace(mappingKeys[j])
			}
			return left < right
		})
		if patterns := normalizeCatalogModelIDs(mappingKeys); len(patterns) > 0 {
			return patterns
		}
	}

	var whitelist []string
	switch raw := account.Credentials["model_whitelist"].(type) {
	case []any:
		whitelist = make([]string, 0, len(raw))
		for _, value := range raw {
			if pattern, ok := value.(string); ok {
				whitelist = append(whitelist, pattern)
			}
		}
	case []string:
		whitelist = append([]string(nil), raw...)
	}
	return normalizeCatalogModelIDs(whitelist)
}

func expandModelPatterns(patterns, candidates []string) []string {
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if !strings.HasSuffix(pattern, "*") {
			result = append(result, pattern)
			continue
		}
		prefix := strings.ToLower(strings.TrimSuffix(pattern, "*"))
		for _, candidate := range candidates {
			if strings.HasPrefix(strings.ToLower(candidate), prefix) {
				result = append(result, candidate)
			}
		}
	}
	return normalizeCatalogModelIDs(result)
}

func normalizeCatalogModelIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(result[i])
		right := strings.ToLower(result[j])
		if left == right {
			return result[i] < result[j]
		}
		return left < right
	})
	return result
}
