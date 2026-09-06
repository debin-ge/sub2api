package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const VideoCapabilityProbeExtraKey = "video_capability_probe"

type openAIVideoProbeIdentity struct {
	Platform        string
	AccountType     string
	APIKey          string
	BaseURL         string
	ProxyID         int64
	UserAgent       string
	HeaderOverrides string
}

func openAIVideoCapabilityProbeIdentity(account *Account) openAIVideoProbeIdentity {
	if account == nil {
		return openAIVideoProbeIdentity{}
	}
	proxyID := int64(0)
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	headerOverrides, _ := json.Marshal(account.Credentials["header_overrides"])
	return openAIVideoProbeIdentity{
		Platform: account.Platform, AccountType: account.Type, APIKey: account.GetOpenAIApiKey(),
		BaseURL: account.GetOpenAIBaseURL(), ProxyID: proxyID, UserAgent: account.GetOpenAIUserAgent(),
		HeaderOverrides: string(headerOverrides),
	}
}

type VideoAccountCapabilityStatus struct {
	AccountID          int64                       `json:"account_id"`
	Probe              *VideoCapabilityProbeResult `json:"probe,omitempty"`
	OverrideConfigured bool                        `json:"override_configured"`
	OverrideEnabled    bool                        `json:"override_enabled"`
	Effective          bool                        `json:"effective"`
}

type VideoCapabilityProbeService struct {
	accounts  AccountRepository
	providers *VideoProviderRegistry
	now       func() time.Time
}

func NewVideoCapabilityProbeService(accounts AccountRepository, providers *VideoProviderRegistry) *VideoCapabilityProbeService {
	return &VideoCapabilityProbeService{accounts: accounts, providers: providers, now: time.Now}
}

func (s *AccountTestService) ProbeOpenAIVideosSupport(ctx context.Context, accountID int64) {
	if s == nil || s.videoCapabilityProbe == nil || accountID <= 0 {
		return
	}
	if _, err := s.videoCapabilityProbe.ProbeAccount(ctx, accountID); err != nil &&
		!errors.Is(err, ErrVideoCapabilityUnsupported) {
		slog.Warn("OpenAI Videos capability probe failed", "account_id", accountID, "error", err)
		return
	}
}

func (s *VideoCapabilityProbeService) ProbeAccount(ctx context.Context, accountID int64) (*VideoAccountCapabilityStatus, error) {
	if s == nil || s.accounts == nil || s.providers == nil || accountID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(VideoProviderOpenAI)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	prober, ok := provider.(VideoCapabilityProber)
	if !ok {
		return nil, ErrVideoCapabilityUnsupported
	}
	result, err := prober.ProbeCapability(ctx, account, VideoCapabilityCreate)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("video capability probe returned no result")
	}
	result.Provider = VideoProviderOpenAI
	result.Capability = string(OpenAIEndpointCapabilityVideos)
	result.Status = normalizeVideoCapabilityProbeStatus(result.Status)
	if result.CheckedAt.IsZero() {
		result.CheckedAt = s.now().UTC()
	}
	result.ErrorSummary = boundedVideoProviderCode(result.ErrorSummary)
	if err := s.accounts.UpdateExtra(ctx, account.ID, map[string]any{VideoCapabilityProbeExtraKey: result}); err != nil {
		return nil, err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[VideoCapabilityProbeExtraKey] = result
	observability.DefaultVideoMetrics().RecordCapabilityProbe(result.Provider, result.Status)
	return VideoCapabilityStatusForAccount(account), nil
}

func (s *VideoCapabilityProbeService) Status(ctx context.Context, accountID int64) (*VideoAccountCapabilityStatus, error) {
	if s == nil || s.accounts == nil || accountID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return VideoCapabilityStatusForAccount(account), nil
}

const (
	videoCapabilityProbePageSize         = 8
	videoCapabilityProbeFreshness        = 6 * time.Hour
	videoCapabilityProbeUnknownFreshness = 5 * time.Minute
	videoCapabilityProbeInterval         = time.Minute
)

func (s *VideoCapabilityProbeService) ProbeDuePage(ctx context.Context, page int) (int, error) {
	if s == nil || s.accounts == nil {
		return 1, ErrVideoInvalidRequest
	}
	if page < 1 {
		page = 1
	}
	accounts, result, err := s.accounts.ListWithFilters(ctx, pagination.PaginationParams{
		Page: page, PageSize: videoCapabilityProbePageSize, SortBy: "id", SortOrder: pagination.SortOrderAsc,
	}, PlatformOpenAI, AccountTypeAPIKey, StatusActive, "", 0, "")
	if err != nil {
		return page, err
	}
	now := s.now().UTC()
	cutoff := now.Add(-videoCapabilityProbeFreshness)
	unknownCutoff := now.Add(-videoCapabilityProbeUnknownFreshness)
	var failures []error
	for i := range accounts {
		account := &accounts[i]
		if !isOfficialOpenAIVideoProbeAccount(account) {
			continue
		}
		probe := OpenAIVideoCapabilityProbe(account)
		if probe != nil {
			if probe.Status == VideoCapabilityProbeUnknown && probe.CheckedAt.After(unknownCutoff) {
				continue
			}
			if probe.Status != VideoCapabilityProbeUnknown && probe.CheckedAt.After(cutoff) {
				continue
			}
		}
		if _, err := s.ProbeAccount(ctx, account.ID); err != nil {
			failures = append(failures, err)
		}
	}
	nextPage := page + 1
	if result == nil || result.Pages <= page {
		nextPage = 1
	}
	return nextPage, errors.Join(failures...)
}

func isOfficialOpenAIVideoProbeAccount(account *Account) bool {
	if account == nil || !account.IsOpenAIApiKey() || isAzureOpenAIAPIKeyAccount(account) {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(account.GetOpenAIBaseURL()))
	return err == nil && strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

type VideoCapabilityProbeRuntime struct {
	service *VideoCapabilityProbeService
	cfg     *config.Config
	page    int
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
}

func NewVideoCapabilityProbeRuntime(service *VideoCapabilityProbeService, cfg *config.Config) *VideoCapabilityProbeRuntime {
	return &VideoCapabilityProbeRuntime{service: service, cfg: cfg, page: 1}
}

func (r *VideoCapabilityProbeRuntime) Start() {
	if r == nil || r.service == nil || r.cfg == nil || !r.cfg.Gateway.Video.Enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(videoCapabilityProbeInterval)
		defer ticker.Stop()
		for {
			nextPage, err := r.service.ProbeDuePage(ctx, r.page)
			if err != nil && ctx.Err() == nil {
				slog.Warn("video capability probe batch failed", "page", r.page, "error", err)
			}
			r.page = nextPage
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *VideoCapabilityProbeRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func ProvideVideoCapabilityProbeRuntime(service *VideoCapabilityProbeService, cfg *config.Config) *VideoCapabilityProbeRuntime {
	runtime := NewVideoCapabilityProbeRuntime(service, cfg)
	runtime.Start()
	return runtime
}

func VideoCapabilityStatusForAccount(account *Account) *VideoAccountCapabilityStatus {
	if account == nil {
		return nil
	}
	overrides, configured := account.openAIEndpointCapabilitySet()
	return &VideoAccountCapabilityStatus{
		AccountID: account.ID, Probe: OpenAIVideoCapabilityProbe(account),
		OverrideConfigured: configured, OverrideEnabled: configured && overrides[string(OpenAIEndpointCapabilityVideos)],
		Effective: account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos),
	}
}

func OpenAIVideoCapabilityProbe(account *Account) *VideoCapabilityProbeResult {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw, exists := account.Extra[VideoCapabilityProbeExtraKey]
	if !exists || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var result VideoCapabilityProbeResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}
	result.Provider = strings.ToLower(strings.TrimSpace(result.Provider))
	result.Capability = strings.ToLower(strings.TrimSpace(result.Capability))
	result.Status = normalizeVideoCapabilityProbeStatus(result.Status)
	result.ErrorSummary = boundedVideoProviderCode(result.ErrorSummary)
	if result.HTTPStatus < 100 || result.HTTPStatus > 599 {
		result.HTTPStatus = 0
	}
	if result.Provider != VideoProviderOpenAI || result.Capability != string(OpenAIEndpointCapabilityVideos) || result.CheckedAt.IsZero() {
		return nil
	}
	return &result
}

func normalizeVideoCapabilityProbeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case VideoCapabilityProbeSupported:
		return VideoCapabilityProbeSupported
	case VideoCapabilityProbeUnsupported:
		return VideoCapabilityProbeUnsupported
	default:
		return VideoCapabilityProbeUnknown
	}
}
