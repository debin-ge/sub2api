package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ModelCatalogService resolves the model catalog exposed by an account.
type ModelCatalogService struct {
	accountRepo    AccountRepository
	groupRepo      GroupRepository
	channelService *ChannelService
	discoverer     ModelDiscoverer
	cfg            config.ModelCatalogConfig
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
	return &ModelCatalogService{
		accountRepo:    accountRepo,
		groupRepo:      groupRepo,
		channelService: channelService,
		discoverer:     discoverer,
		cfg:            cfg,
		now:            time.Now,
	}
}

// ListForAccount returns the model IDs exposed by one account.
func (s *ModelCatalogService) ListForAccount(ctx context.Context, account *Account, waitForLive bool) ([]string, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required for model catalog resolution", nil)
	}

	defaults := DefaultModelCatalogIDs(account.Platform)
	if !accountRequiresLiveCatalog(account) {
		return configuredOrDefaultAccountModels(account, defaults), nil
	}
	if !waitForLive {
		return defaults, nil
	}
	if s == nil || s.discoverer == nil {
		return nil, newUpstreamModelSyncConfigError("Account model discoverer is not configured", nil)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	models, err := s.discoverer.Discover(timeoutCtx, account)
	if err != nil {
		var syncErr *UpstreamModelSyncError
		if errors.As(err, &syncErr) && syncErr.Kind == UpstreamModelSyncErrorUnsupported {
			return configuredOrDefaultAccountModels(account, defaults), nil
		}
		return nil, err
	}
	models = normalizeCatalogModelIDs(models)
	if len(models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}
	return models, nil
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
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, nil, err
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
