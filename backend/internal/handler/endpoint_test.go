package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() { gin.SetMode(gin.TestMode) }

// ──────────────────────────────────────────────────────────
// NormalizeInboundEndpoint
// ──────────────────────────────────────────────────────────

func TestNormalizeInboundEndpoint(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Direct canonical paths.
		{"/v1/messages", EndpointMessages},
		{"/v1/chat/completions", EndpointChatCompletions},
		{"/v1/embeddings", EndpointEmbeddings},
		{"/v1/alpha/search", EndpointAlphaSearch},
		{"/v1/responses", EndpointResponses},
		{"/responses", EndpointResponses},
		{"/backend-api/codex/responses", EndpointResponses},
		{"/v1/responses/input_tokens", EndpointResponsesInputTokens},
		{"/v1/responses/compact", EndpointResponsesCompact},
		{"/v1/responses/compact/detail", EndpointResponsesCompact},
		{"/v1/images/generations", EndpointImagesGenerations},
		{"/v1/images/edits", EndpointImagesEdits},
		{"/v1/images/tasks/imgtask_123", EndpointImageTasks},
		{"/v1/videos/generations", EndpointVideosGenerations},
		{"/v1/videos/req_123", EndpointVideos},
		{"/v1beta/models", EndpointGeminiModels},

		// Prefixed paths (antigravity, openai).
		{"/antigravity/v1/messages", EndpointMessages},
		{"/openai/v1/responses", EndpointResponses},
		{"/openai/v1/responses/compact", EndpointResponsesCompact},
		{"/openai/v1/images/generations", EndpointImagesGenerations},
		{"/openai/v1/images/edits", EndpointImagesEdits},
		{"/antigravity/v1beta/models/gemini:generateContent", EndpointGeminiModels},

		// Gin route patterns with wildcards.
		{"/v1beta/models/*modelAction", EndpointGeminiModels},
		{"/v1/responses/*subpath", EndpointResponses},
		{"/responses/*subpath", EndpointResponses},
		{"/backend-api/codex/responses/*subpath", EndpointResponses},

		// Prefixed paths — "/responses/compact" is its OWN distinct
		// inbound endpoint, not folded into the root Responses endpoint.
		{"/openai/v1/responses/compact", EndpointResponsesCompact},
		{"/openai/v1/responses/compact/detail", EndpointResponsesCompact},

		// Bare top-level alias route "/responses" — root vs. compact.
		{"/responses", EndpointResponses},
		{"/responses/input_tokens", EndpointResponsesInputTokens},
		{"/responses/compact", EndpointResponsesCompact},
		{"/responses/compact/detail", EndpointResponsesCompact},
		{"/alpha/search", EndpointAlphaSearch},
		{"/images/tasks/imgtask_123", EndpointImageTasks},

		// Bare Codex direct alias route — root vs. compact.
		{"/backend-api/codex/responses", EndpointResponses},
		{"/backend-api/codex/responses/input_tokens", EndpointResponsesInputTokens},
		{"/backend-api/codex/responses/compact", EndpointResponsesCompact},
		{"/backend-api/codex/responses/compact/detail", EndpointResponsesCompact},
		{"/backend-api/codex/alpha/search", EndpointAlphaSearch},

		// Must NOT generalize to arbitrary paths merely ending in
		// "/responses" (or "/responses/compact") that are unrelated to
		// the two known bare alias roots, unless they already carry a
		// supported "/v1/responses..." prefix form.
		{"/foo/responses", "/foo/responses"},
		{"/foo/responses/compact", "/foo/responses/compact"},

		// Unknown path is returned as-is.
		{"/v1/embeddings", "/v1/embeddings"},
		{"/foo/v1/responses-not-root", "/foo/v1/responses-not-root"},
		{"/foo/responses-not-root", "/foo/responses-not-root"},
		{"", ""},
		{"  /v1/messages  ", EndpointMessages},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeInboundEndpoint(tt.path))
		})
	}
}

// ──────────────────────────────────────────────────────────
// DeriveUpstreamEndpoint
// ──────────────────────────────────────────────────────────

func TestDeriveUpstreamEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		inbound  string
		rawPath  string
		platform string
		want     string
	}{
		// Anthropic.
		{"anthropic messages", EndpointMessages, "/v1/messages", service.PlatformAnthropic, EndpointMessages},

		// Gemini.
		{"gemini models", EndpointGeminiModels, "/v1beta/models/gemini:gen", service.PlatformGemini, EndpointGeminiModels},

		// OpenAI — always /v1/responses.
		{"openai responses root", EndpointResponses, "/v1/responses", service.PlatformOpenAI, EndpointResponses},
		{"openai responses compact", EndpointResponses, "/openai/v1/responses/compact", service.PlatformOpenAI, "/v1/responses/compact"},
		{"openai responses nested", EndpointResponses, "/openai/v1/responses/compact/detail", service.PlatformOpenAI, "/v1/responses/compact/detail"},
		{"openai responses input tokens", EndpointResponsesInputTokens, "/v1/responses/input_tokens", service.PlatformOpenAI, EndpointResponsesInputTokens},

		// OpenAI — compact, raw path carries the derivable "/compact"
		// (or nested) suffix, which must be preserved on the upstream
		// endpoint.
		{"openai responses compact", EndpointResponsesCompact, "/openai/v1/responses/compact", service.PlatformOpenAI, "/v1/responses/compact"},
		{"openai responses nested", EndpointResponsesCompact, "/openai/v1/responses/compact/detail", service.PlatformOpenAI, "/v1/responses/compact/detail"},
		{"openai bare responses compact", EndpointResponsesCompact, "/responses/compact", service.PlatformOpenAI, "/v1/responses/compact"},
		{"openai bare responses compact detail", EndpointResponsesCompact, "/responses/compact/detail", service.PlatformOpenAI, "/v1/responses/compact/detail"},
		{"openai codex direct responses compact", EndpointResponsesCompact, "/backend-api/codex/responses/compact", service.PlatformOpenAI, "/v1/responses/compact"},
		{"openai codex direct responses compact detail", EndpointResponsesCompact, "/backend-api/codex/responses/compact/detail", service.PlatformOpenAI, "/v1/responses/compact/detail"},

		// OpenAI — bare root alias routes normalize to root Responses.
		{"openai bare responses", EndpointResponses, "/responses", service.PlatformOpenAI, EndpointResponses},
		{"openai codex direct responses", EndpointResponses, "/backend-api/codex/responses", service.PlatformOpenAI, EndpointResponses},

		// OpenAI — inbound is already the canonical compact endpoint but
		// the raw path carries no derivable "/responses..." suffix (e.g.
		// it was already normalized upstream). Must not silently fall
		// back to the root Responses endpoint.
		{"openai responses compact inbound only, unrelated raw path", EndpointResponsesCompact, "/v1/messages", service.PlatformOpenAI, EndpointResponsesCompact},

		{"openai from messages", EndpointMessages, "/v1/messages", service.PlatformOpenAI, EndpointResponses},
		{"openai from completions", EndpointChatCompletions, "/v1/chat/completions", service.PlatformOpenAI, EndpointResponses},
		{"openai embeddings", EndpointEmbeddings, "/v1/embeddings", service.PlatformOpenAI, EndpointEmbeddings},
		{"openai alpha search", EndpointAlphaSearch, "/backend-api/codex/alpha/search", service.PlatformOpenAI, EndpointAlphaSearch},
		{"openai image generations", EndpointImagesGenerations, "/v1/images/generations", service.PlatformOpenAI, EndpointImagesGenerations},
		{"openai image edits", EndpointImagesEdits, "/openai/v1/images/edits", service.PlatformOpenAI, EndpointImagesEdits},
		{"grok chat defaults to responses without runtime result", EndpointChatCompletions, "/v1/chat/completions", service.PlatformGrok, EndpointResponses},
		{"grok responses", EndpointResponses, "/v1/responses", service.PlatformGrok, EndpointResponses},
		{"grok video generations", EndpointVideosGenerations, "/v1/videos/generations", service.PlatformGrok, EndpointVideosGenerations},
		{"grok video status", EndpointVideos, "/videos/req_123", service.PlatformGrok, EndpointVideos},

		// Antigravity — uses inbound to pick Claude vs Gemini upstream.
		{"antigravity claude", EndpointMessages, "/antigravity/v1/messages", service.PlatformAntigravity, EndpointMessages},
		{"antigravity gemini", EndpointGeminiModels, "/antigravity/v1beta/models", service.PlatformAntigravity, EndpointGeminiModels},

		// MiniMax.
		{
			name:     "minimax messages",
			inbound:  EndpointMessages,
			rawPath:  "/v1/messages",
			platform: service.PlatformMiniMax,
			want:     EndpointMessages,
		},
		{
			name:     "minimax chat completions",
			inbound:  EndpointChatCompletions,
			rawPath:  "/v1/chat/completions",
			platform: service.PlatformMiniMax,
			want:     EndpointChatCompletions,
		},
		{
			name:     "minimax responses remains inbound unsupported endpoint",
			inbound:  EndpointResponses,
			rawPath:  "/v1/responses",
			platform: service.PlatformMiniMax,
			want:     EndpointResponses,
		},
		{
			name:     "glm messages",
			inbound:  EndpointMessages,
			rawPath:  "/v1/messages",
			platform: service.PlatformGLM,
			want:     EndpointMessages,
		},
		{
			name:     "glm chat completions",
			inbound:  EndpointChatCompletions,
			rawPath:  "/chat/completions",
			platform: service.PlatformGLM,
			want:     EndpointChatCompletions,
		},
		{
			name:     "glm responses remains inbound unsupported endpoint",
			inbound:  EndpointResponses,
			rawPath:  "/v1/responses",
			platform: service.PlatformGLM,
			want:     EndpointResponses,
		},
		{
			name:     "zhipu compact normalizes to responses",
			inbound:  EndpointResponsesCompact,
			rawPath:  "/v1/responses/compact",
			platform: service.PlatformZhipu,
			want:     EndpointResponses,
		},
		{
			name:     "glm compact still normalizes after CanonicalCNPlatform",
			inbound:  EndpointResponsesCompact,
			rawPath:  "/v1/responses/compact",
			platform: service.PlatformGLM,
			want:     EndpointResponses,
		},
		{
			name:     "kimi messages",
			inbound:  EndpointMessages,
			rawPath:  "/v1/messages",
			platform: service.PlatformKimi,
			want:     EndpointMessages,
		},
		{
			name:     "kimi chat completions",
			inbound:  EndpointChatCompletions,
			rawPath:  "/chat/completions",
			platform: service.PlatformKimi,
			want:     EndpointChatCompletions,
		},
		{
			name:     "kimi responses remains inbound unsupported endpoint",
			inbound:  EndpointResponses,
			rawPath:  "/v1/responses",
			platform: service.PlatformKimi,
			want:     EndpointResponses,
		},
		{
			name:     "deepseek messages",
			inbound:  EndpointMessages,
			rawPath:  "/v1/messages",
			platform: service.PlatformDeepSeek,
			want:     EndpointMessages,
		},
		{
			name:     "deepseek chat completions",
			inbound:  EndpointChatCompletions,
			rawPath:  "/v1/chat/completions",
			platform: service.PlatformDeepSeek,
			want:     EndpointChatCompletions,
		},
		{
			name:     "deepseek responses remains inbound unsupported endpoint",
			inbound:  EndpointResponses,
			rawPath:  "/v1/responses",
			platform: service.PlatformDeepSeek,
			want:     EndpointResponses,
		},
		{
			name:     "windsurf messages",
			inbound:  EndpointMessages,
			rawPath:  "/v1/messages",
			platform: service.PlatformWindsurf,
			want:     EndpointMessages,
		},
		{
			name:     "windsurf chat completions",
			inbound:  EndpointChatCompletions,
			rawPath:  "/v1/chat/completions",
			platform: service.PlatformWindsurf,
			want:     EndpointChatCompletions,
		},
		{
			name:     "windsurf responses remains inbound unsupported endpoint",
			inbound:  EndpointResponses,
			rawPath:  "/v1/responses",
			platform: service.PlatformWindsurf,
			want:     EndpointResponses,
		},

		// Unknown platform — passthrough.
		{"unknown platform", "/v1/embeddings", "/v1/embeddings", "unknown", "/v1/embeddings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, DeriveUpstreamEndpoint(tt.inbound, tt.rawPath, tt.platform))
		})
	}
}

func TestShouldUseAntigravityCompat(t *testing.T) {
	tests := []struct {
		name    string
		account *service.Account
		want    bool
	}{
		{"oauth", &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth}, true},
		{"setup token", &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeSetupToken}, false},
		{"upstream", &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeUpstream}, false},
		{"api key", &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeAPIKey}, false},
		{"anthropic oauth", &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldUseAntigravityCompat(tt.account))
		})
	}
}

func TestGetUpstreamEndpointPrefersRuntimeOverride(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, EndpointChatCompletions, nil)
	c.Set(ctxKeyInboundEndpoint, EndpointChatCompletions)

	setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
	require.Equal(t, EndpointAntigravityGenerateContent, GetUpstreamEndpoint(c, service.PlatformAntigravity))

	setActualUpstreamEndpoint(c, "")
	require.Equal(t, EndpointMessages, GetUpstreamEndpoint(c, service.PlatformAntigravity))
}

func TestResolveOpenAIUpstreamEndpointPrefersForwardResult(t *testing.T) {
	tests := []struct {
		name            string
		account         *service.Account
		result          *service.OpenAIForwardResult
		runtimeEndpoint string
		want            string
	}{
		{
			name:            "grok raw chat result overrides stale context",
			account:         &service.Account{Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			result:          &service.OpenAIForwardResult{UpstreamEndpoint: EndpointChatCompletions},
			runtimeEndpoint: EndpointResponses,
			want:            EndpointChatCompletions,
		},
		{
			name:    "grok chat bridged to responses",
			account: &service.Account{Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			result:  &service.OpenAIForwardResult{UpstreamEndpoint: EndpointResponses},
			want:    EndpointResponses,
		},
		{
			name:    "grok empty result keeps responses default",
			account: &service.Account{Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			result:  &service.OpenAIForwardResult{},
			want:    EndpointResponses,
		},
		{
			name:            "grok raw error uses runtime endpoint",
			account:         &service.Account{Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			runtimeEndpoint: EndpointChatCompletions,
			want:            EndpointChatCompletions,
		},
		{
			name:    "openai behavior remains responses",
			account: &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth},
			result:  &service.OpenAIForwardResult{},
			want:    EndpointResponses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, EndpointChatCompletions, nil)
			c.Set(ctxKeyInboundEndpoint, EndpointChatCompletions)
			service.SetActualOpenAIUpstreamEndpoint(c, tt.runtimeEndpoint)
			require.Equal(t, tt.want, resolveOpenAIUpstreamEndpoint(c, tt.account, tt.result))
		})
	}
}

// ──────────────────────────────────────────────────────────
// responsesSubpathSuffix
// ──────────────────────────────────────────────────────────

func TestResponsesSubpathSuffix(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"/v1/responses", ""},
		{"/v1/responses/", ""},
		{"/v1/responses/compact", "/compact"},
		{"/openai/v1/responses/compact/detail", "/compact/detail"},
		{"/v1/messages", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			require.Equal(t, tt.want, responsesSubpathSuffix(tt.raw))
		})
	}
}

// ──────────────────────────────────────────────────────────
// InboundEndpointMiddleware + context helpers
// ──────────────────────────────────────────────────────────

func TestInboundEndpointMiddleware(t *testing.T) {
	router := gin.New()
	router.Use(InboundEndpointMiddleware())

	var captured string
	router.POST("/v1/messages", func(c *gin.Context) {
		captured = GetInboundEndpoint(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, EndpointMessages, captured)
}

func TestGetInboundEndpoint_FallbackWithoutMiddleware(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1/messages", nil)

	// Middleware did not run — fallback to normalizing c.Request.URL.Path.
	got := GetInboundEndpoint(c)
	require.Equal(t, EndpointMessages, got)
}

func TestGetUpstreamEndpoint_FullFlow(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses/compact", nil)

	// Simulate middleware.
	c.Set(ctxKeyInboundEndpoint, NormalizeInboundEndpoint(c.Request.URL.Path))

	got := GetUpstreamEndpoint(c, service.PlatformOpenAI)
	require.Equal(t, "/v1/responses/compact", got)
}

func TestGetUpstreamEndpoint_ProviderResponsesRootPOSTUsesPlannedBridge(t *testing.T) {
	tests := []struct {
		platform string
		want     string
	}{
		{service.PlatformMiniMax, EndpointMessages},
		{service.PlatformGLM, EndpointMessages},
		{service.PlatformZhipu, EndpointMessages},
		{service.PlatformKimi, EndpointMessages},
		{service.PlatformDeepSeek, EndpointChatCompletions},
		{service.PlatformWindsurf, EndpointChatCompletions},
	}
	paths := []string{
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	}
	for _, tt := range tests {
		for _, path := range paths {
			t.Run(tt.platform+path, func(t *testing.T) {
				require.Equal(t, tt.want, getUpstreamEndpointForRequest(t, http.MethodPost, path, tt.platform))
			})
		}
	}
}

func TestGetUpstreamEndpoint_ProviderResponsesSubpathPOSTRemainsResponses(t *testing.T) {
	platforms := []string{
		service.PlatformMiniMax,
		service.PlatformGLM,
		service.PlatformZhipu,
		service.PlatformKimi,
		service.PlatformDeepSeek,
		service.PlatformWindsurf,
	}
	paths := []string{
		"/v1/responses/compact",
		"/responses/custom",
		"/backend-api/codex/responses/compact",
	}
	for _, platform := range platforms {
		for _, path := range paths {
			t.Run(platform+path, func(t *testing.T) {
				require.Equal(t, EndpointResponses, getUpstreamEndpointForRequest(t, http.MethodPost, path, platform))
			})
		}
	}
}

func TestGetUpstreamEndpoint_ProviderResponsesGETRemainsResponses(t *testing.T) {
	platforms := []string{
		service.PlatformMiniMax,
		service.PlatformGLM,
		service.PlatformZhipu,
		service.PlatformKimi,
		service.PlatformDeepSeek,
		service.PlatformWindsurf,
	}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			require.Equal(t, EndpointResponses, getUpstreamEndpointForRequest(t, http.MethodGet, "/v1/responses", platform))
		})
	}
}

func TestGetUpstreamEndpoint_OpenCodeResponsesRootPOSTRemainsResponses(t *testing.T) {
	require.Equal(t, EndpointResponses, getUpstreamEndpointForRequest(t, http.MethodPost, "/v1/responses", service.PlatformOpenCode))
}

func getUpstreamEndpointForRequest(t *testing.T, method, path, platform string) string {
	t.Helper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, nil)
	c.Set(ctxKeyInboundEndpoint, NormalizeInboundEndpoint(c.Request.URL.Path))
	return GetUpstreamEndpoint(c, platform)
}
