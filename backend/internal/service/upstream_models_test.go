package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func upstreamModelSyncTestConfig() *config.Config {
	return &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	}
}

func newOpenAIOAuthDiscovererTest(t *testing.T, body string) (*UpstreamModelDiscoverer, *httpUpstreamRecorder) {
	t.Helper()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}}
	return &UpstreamModelDiscoverer{
		httpUpstream: upstream,
		cfg:          upstreamModelSyncTestConfig(),
	}, upstream
}

type modelDiscoveryRequestRecorder struct {
	req *http.Request
}

func newModelDiscoveryServer(t *testing.T, responseBody string) (*httptest.Server, *modelDiscoveryRequestRecorder) {
	t.Helper()
	recorder := &modelDiscoveryRequestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder.req = req.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

func TestBuildV1ModelsURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com"))
	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com/v1"))
	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com/v1/models"))
	require.Equal(t, "https://gateway.example.com/antigravity/v1/models", buildV1ModelsURL("https://gateway.example.com/antigravity/"))
}

func TestBuildOpenAIModelsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "zhipu v4 coding base url",
			base: "https://open.bigmodel.cn/api/coding/paas/v4",
			want: "https://open.bigmodel.cn/api/coding/paas/v4/models",
		},
		{
			name: "openai v1 base url",
			base: "https://api.openai.com/v1",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "models url unchanged",
			base: "https://api.openai.com/v1/models",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "host fallback uses v1",
			base: "https://api.openai.com",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "trailing slash on v4",
			base: "https://open.bigmodel.cn/api/coding/paas/v4/",
			want: "https://open.bigmodel.cn/api/coding/paas/v4/models",
		},
		{
			name: "v2 base url",
			base: "https://gateway.example.com/openai/v2",
			want: "https://gateway.example.com/openai/v2/models",
		},
		{
			name: "v3 base url",
			base: "https://gateway.example.com/openai/v3",
			want: "https://gateway.example.com/openai/v3/models",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, buildOpenAIModelsURL(tt.base))
		})
	}
}

func TestBuildGeminiModelsURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com"))
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com/v1beta"))
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com/v1beta/models"))
}

func TestExtractUpstreamModelIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "openai and anthropic data array",
			body: `{"data":[{"id":"claude-sonnet-4-5"},{"id":"gpt-5"},{"id":"gpt-5"},{"id":""}]}`,
			want: []string{"claude-sonnet-4-5", "gpt-5"},
		},
		{
			name: "gemini models array strips prefix",
			body: `{"models":[{"name":"models/gemini-2.5-pro"},{"name":"gemini-2.5-flash"}]}`,
			want: []string{"gemini-2.5-flash", "gemini-2.5-pro"},
		},
		{
			name: "top level array",
			body: `[{"id":"z-model"},{"name":"models/a-model"}]`,
			want: []string{"a-model", "z-model"},
		},
		{
			name: "codex slug id and gemini prefix",
			body: `{"models":[{"slug":"gpt-new"},{"id":"gpt-image-new"},{"name":"models/gemini-new"}]}`,
			want: []string{"gemini-new", "gpt-image-new", "gpt-new"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractUpstreamModelIDs([]byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestUpstreamModelDiscoverer_OpenAIOAuthUsesCodexCatalog(t *testing.T) {
	discoverer, upstream := newOpenAIOAuthDiscovererTest(t, `{"models":[{"slug":"gpt-new"},{"id":"gpt-image-new"}]}`)
	models, err := discoverer.Discover(context.Background(), &Account{
		ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "acct-1"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-image-new", "gpt-new"}, models)
	require.Equal(t, "/backend-api/codex/models", upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "acct-1", upstream.lastReq.Header.Get("chatgpt-account-id"))
	require.Equal(t, codexCLIUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, codexCLIVersion, upstream.lastReq.Header.Get("Version"))
	require.Equal(t, "Codex Desktop", upstream.lastReq.Header.Get("originator"))
}

func TestUpstreamModelDiscoverer_OpenAIOAuthUsesConfiguredTokenProvider(t *testing.T) {
	discoverer, upstream := newOpenAIOAuthDiscovererTest(t, `{"models":[{"slug":"gpt-new"}]}`)
	account := &Account{
		ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":               "stale-token",
			"chatgpt_account_id":         "acct-2",
			"chatgpt_account_is_fedramp": true,
		},
	}
	discoverer.openaiTokenProvider = NewOpenAITokenProvider(nil, &stubQuotaTokenCache{tokens: map[string]string{
		OpenAITokenCacheKey(account): "refreshed-token",
	}}, nil)

	_, err := discoverer.Discover(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, "Bearer refreshed-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "true", upstream.lastReq.Header.Get("x-openai-fedramp"))
}

func TestUpstreamModelDiscoverer_ProviderDispatch(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "anthropic_oauth",
			run: func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"claude-new"}]}`)),
				}}
				discoverer := &UpstreamModelDiscoverer{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

				models, err := discoverer.Discover(context.Background(), &Account{
					ID: 8, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "anthropic-token"},
				})

				require.NoError(t, err)
				require.Equal(t, []string{"claude-new"}, models)
				require.Equal(t, "https://api.anthropic.com/v1/models", upstream.lastReq.URL.String())
				require.Equal(t, "Bearer anthropic-token", upstream.lastReq.Header.Get("Authorization"))
			},
		},
		{
			name: "gemini_oauth",
			run: func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"models":[{"name":"models/gemini-new"}]}`)),
				}}
				discoverer := &UpstreamModelDiscoverer{
					geminiTokenProvider: &GeminiTokenProvider{},
					httpUpstream:        upstream,
					cfg:                 upstreamModelSyncTestConfig(),
				}

				models, err := discoverer.Discover(context.Background(), &Account{
					ID: 9, Platform: PlatformGemini, Type: AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "gemini-token"},
				})

				require.NoError(t, err)
				require.Equal(t, []string{"gemini-new"}, models)
				require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", upstream.lastReq.URL.String())
				require.Equal(t, "Bearer gemini-token", upstream.lastReq.Header.Get("Authorization"))
			},
		},
		{
			name: "antigravity_oauth",
			run: func(t *testing.T) {
				server, recorder := newModelDiscoveryServer(t, `{"models":{"gemini-new":{}}}`)
				originalBaseURLs := antigravity.BaseURLs
				originalBaseURL := antigravity.BaseURL
				antigravity.BaseURLs = []string{server.URL}
				antigravity.BaseURL = server.URL
				t.Cleanup(func() {
					antigravity.BaseURLs = originalBaseURLs
					antigravity.BaseURL = originalBaseURL
				})
				discoverer := &UpstreamModelDiscoverer{
					antigravityGatewayService: &AntigravityGatewayService{tokenProvider: &AntigravityTokenProvider{}},
				}

				models, err := discoverer.Discover(context.Background(), &Account{
					ID: 10, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "antigravity-token", "project_id": "project-1"},
				})

				require.NoError(t, err)
				require.Equal(t, []string{"gemini-new"}, models)
				require.Equal(t, server.URL+"/v1internal:fetchAvailableModels", "http://"+recorder.req.Host+recorder.req.URL.RequestURI())
				require.Equal(t, "Bearer antigravity-token", recorder.req.Header.Get("Authorization"))
			},
		},
		{
			name: "windsurf",
			run: func(t *testing.T) {
				server, recorder := newModelDiscoveryServer(t, `{"data":[{"id":"windsurf-new"}]}`)
				discoverer := &UpstreamModelDiscoverer{}

				models, err := discoverer.Discover(context.Background(), &Account{
					ID: 11, Platform: PlatformWindsurf, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "windsurf-key", "base_url": server.URL},
				})

				require.NoError(t, err)
				require.Equal(t, []string{"windsurf-new"}, models)
				require.Equal(t, server.URL+"/v1/models", "http://"+recorder.req.Host+recorder.req.URL.RequestURI())
				require.Equal(t, "Bearer windsurf-key", recorder.req.Header.Get("Authorization"))
			},
		},
		{
			name: "opencode",
			run: func(t *testing.T) {
				server, recorder := newModelDiscoveryServer(t, `{"data":[{"id":"opencode-new"}]}`)
				discoverer := &UpstreamModelDiscoverer{}

				models, err := discoverer.Discover(context.Background(), &Account{
					ID: 12, Platform: PlatformOpenCode, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "opencode-key", "base_url": server.URL},
				})

				require.NoError(t, err)
				require.Equal(t, []string{"opencode-new"}, models)
				require.Equal(t, server.URL+"/v1/models", "http://"+recorder.req.Host+recorder.req.URL.RequestURI())
				require.Equal(t, "Bearer opencode-key", recorder.req.Header.Get("Authorization"))
			},
		},
		{
			name: "grok_oauth_unsupported",
			run: func(t *testing.T) {
				_, err := (&UpstreamModelDiscoverer{httpUpstream: &httpUpstreamRecorder{}}).Discover(
					context.Background(),
					&Account{ID: 13, Platform: PlatformGrok, Type: AccountTypeOAuth},
				)
				require.Error(t, err)

				var syncErr *UpstreamModelSyncError
				require.True(t, errors.As(err, &syncErr))
				require.Equal(t, UpstreamModelSyncErrorUnsupported, syncErr.Kind)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestBuildUpstreamModelsRequestsForAPIKeyAccounts(t *testing.T) {
	t.Parallel()

	svc := &UpstreamModelDiscoverer{cfg: upstreamModelSyncTestConfig()}
	ctx := context.Background()

	anthropicReq, err := svc.buildAnthropicUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "anthropic-key",
			"base_url": "https://anthropic.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://anthropic.example.com/v1/models", anthropicReq.URL.String())
	require.Equal(t, "anthropic-key", anthropicReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", anthropicReq.Header.Get("anthropic-version"))

	anthropicBearerReq, err := svc.buildAnthropicUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ollama-key",
			"base_url": "https://ollama.com",
		},
		Extra: map[string]any{
			"anthropic_apikey_auth_scheme": AnthropicAPIKeyAuthSchemeAuthorizationBearer,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://ollama.com/v1/models", anthropicBearerReq.URL.String())
	require.Equal(t, "Bearer ollama-key", anthropicBearerReq.Header.Get("Authorization"))
	require.Empty(t, anthropicBearerReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", anthropicBearerReq.Header.Get("anthropic-version"))

	openAIReq, err := svc.buildOpenAIUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://openai.example.com/v1/models", openAIReq.URL.String())
	require.Equal(t, "Bearer openai-key", openAIReq.Header.Get("Authorization"))

	geminiReq, err := svc.buildGeminiUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "gemini-key",
			"base_url": "https://generativelanguage.googleapis.com/v1beta",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", geminiReq.URL.String())
	require.Equal(t, "gemini-key", geminiReq.Header.Get("x-goog-api-key"))

	antigravityReq, err := svc.buildAntigravityAPIKeyModelsRequest(ctx, &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "antigravity-key",
			"base_url": "https://gateway.example.com/antigravity",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com/antigravity/v1/models", antigravityReq.URL.String())
	require.Equal(t, "antigravity-key", antigravityReq.Header.Get("x-api-key"))
}

func TestBuildAntigravityAPIKeyModelsRequestRejectsOfficialCloudCodeBase(t *testing.T) {
	t.Parallel()

	svc := &UpstreamModelDiscoverer{cfg: upstreamModelSyncTestConfig()}
	_, err := svc.buildAntigravityAPIKeyModelsRequest(context.Background(), &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "antigravity-key",
			"base_url": "https://cloudcode-pa.googleapis.com",
		},
	})
	require.Error(t, err)

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUnsupported, syncErr.Kind)
	require.Contains(t, syncErr.SafeMessage(), "compatible gateway")
}

func TestBuildAnthropicUpstreamModelsRequestRejectsBedrock(t *testing.T) {
	t.Parallel()

	svc := &UpstreamModelDiscoverer{cfg: upstreamModelSyncTestConfig()}
	_, err := svc.buildAnthropicUpstreamModelsRequest(context.Background(), &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeBedrock,
	})
	require.Error(t, err)

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUnsupported, syncErr.Kind)
}

func TestFetchUpstreamSupportedModelsParsesOpenAIResponse(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-5"},{"id":"gpt-5"},{"name":"o3"}]}`)),
	}}
	svc := &AccountTestService{
		modelDiscoverer: &UpstreamModelDiscoverer{
			httpUpstream: upstream,
			cfg:          upstreamModelSyncTestConfig(),
		},
	}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{
		ID:       7,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5", "o3"}, models)
	require.Equal(t, "https://openai.example.com/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer openai-key", upstream.lastReq.Header.Get("Authorization"))
}

func TestFetchUpstreamSupportedModelsDoesNotExposeUpstreamBody(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	svc := &AccountTestService{
		modelDiscoverer: &UpstreamModelDiscoverer{
			httpUpstream: upstream,
			cfg:          upstreamModelSyncTestConfig(),
		},
	}

	_, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{
		ID:       8,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com/v1",
		},
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET_TOKEN")

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUpstream, syncErr.Kind)
	require.NotContains(t, syncErr.SafeMessage(), "SECRET_TOKEN")
	require.Contains(t, syncErr.SafeMessage(), "HTTP 502")
}
