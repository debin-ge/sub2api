package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type videoCapabilityProbeRepo struct {
	AccountRepository
	account *Account
	updates map[string]any
}

type videoCapabilityProbePageRepo struct {
	AccountRepository
	accounts []Account
	updates  int
}

func (r *videoCapabilityProbePageRepo) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _, _ string, _ int64, _ string) ([]Account, *pagination.PaginationResult, error) {
	return append([]Account(nil), r.accounts...), &pagination.PaginationResult{Page: 1, Pages: 2}, nil
}

func (r *videoCapabilityProbePageRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *videoCapabilityProbePageRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.updates++
	account, err := r.GetByID(context.Background(), id)
	if err != nil {
		return err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	for key, value := range updates {
		account.Extra[key] = value
	}
	return nil
}

func (r *videoCapabilityProbeRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *videoCapabilityProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	if r.account.Extra == nil {
		r.account.Extra = make(map[string]any)
	}
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return nil
}

func TestVideoCapabilityProbeServicePersistsSafeSnapshot(t *testing.T) {
	account := openAIVideoTestAccount()
	delete(account.Credentials, "openai_capabilities")
	repository := &videoCapabilityProbeRepo{account: account}
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusOK, `{"data":[]}`, nil), nil
	}}, nil)
	service := NewVideoCapabilityProbeService(repository, NewVideoProviderRegistry(provider))

	status, err := service.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.True(t, status.Effective)
	require.False(t, status.OverrideConfigured)
	require.Equal(t, VideoCapabilityProbeSupported, status.Probe.Status)
	require.Contains(t, repository.updates, VideoCapabilityProbeExtraKey)
	encoded, err := json.Marshal(repository.updates)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sk-video-secret")
}

func TestVideoCapabilityStatusManualOverrideWinsProbe(t *testing.T) {
	account := openAIVideoTestAccount()
	account.Credentials["openai_capabilities"] = []any{"chat_completions"}
	account.Extra = map[string]any{VideoCapabilityProbeExtraKey: VideoCapabilityProbeResult{
		Provider: VideoProviderOpenAI, Capability: string(OpenAIEndpointCapabilityVideos),
		Status: VideoCapabilityProbeSupported, CheckedAt: time.Unix(1, 0).UTC(),
	}}

	status := VideoCapabilityStatusForAccount(account)

	require.True(t, status.OverrideConfigured)
	require.False(t, status.OverrideEnabled)
	require.False(t, status.Effective)
}

func TestVideoCapabilityProbeDuePageIsBoundedAndOfficialOnly(t *testing.T) {
	unknown := *openAIVideoTestAccount()
	unknown.ID = 1
	delete(unknown.Credentials, "openai_capabilities")
	fresh := unknown
	fresh.ID = 2
	fresh.Extra = map[string]any{VideoCapabilityProbeExtraKey: VideoCapabilityProbeResult{
		Provider: VideoProviderOpenAI, Capability: string(OpenAIEndpointCapabilityVideos),
		Status: VideoCapabilityProbeSupported, CheckedAt: time.Now().UTC(),
	}}
	custom := unknown
	custom.ID = 3
	custom.Credentials = map[string]any{"api_key": "sk-custom", "base_url": "https://gateway.example.test"}
	repository := &videoCapabilityProbePageRepo{accounts: []Account{unknown, fresh, custom}}
	calls := 0
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		return openAIVideoResponseForTest(http.StatusOK, `{"data":[]}`, nil), nil
	}}, nil)
	service := NewVideoCapabilityProbeService(repository, NewVideoProviderRegistry(provider))

	nextPage, err := service.ProbeDuePage(context.Background(), 1)

	require.NoError(t, err)
	require.Equal(t, 2, nextPage)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, repository.updates)
}
