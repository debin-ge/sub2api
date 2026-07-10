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
		return account.Type == AccountTypeOAuth && !account.IsGeminiCodeAssist()
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
