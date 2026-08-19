package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ──────────────────────────────────────────────────────────
// Canonical inbound / upstream endpoint paths.
// All normalization and derivation reference this single set
// of constants — add new paths HERE when a new API surface
// is introduced.
// ──────────────────────────────────────────────────────────

const (
	EndpointMessages          = "/v1/messages"
	EndpointChatCompletions   = "/v1/chat/completions"
	EndpointEmbeddings        = "/v1/embeddings"
	EndpointAlphaSearch       = "/v1/alpha/search"
	EndpointResponses         = "/v1/responses"
	EndpointResponsesCompact  = "/v1/responses/compact"
	EndpointImagesGenerations = "/v1/images/generations"
	EndpointImagesEdits       = "/v1/images/edits"
	EndpointImageTasks        = "/v1/images/tasks"
	EndpointVideosGenerations = "/v1/videos/generations"
	EndpointVideosEdits       = "/v1/videos/edits"
	EndpointVideosExtensions  = "/v1/videos/extensions"
	EndpointVideos            = "/v1/videos"
	EndpointGeminiModels      = "/v1beta/models"
)

const EndpointAntigravityGenerateContent = "/v1internal:streamGenerateContent"

// gin.Context keys used by the middleware and helpers below.
const (
	ctxKeyInboundEndpoint        = "_gateway_inbound_endpoint"
	ctxKeyActualUpstreamEndpoint = "_gateway_actual_upstream_endpoint"
)

// ──────────────────────────────────────────────────────────
// Normalization functions
// ──────────────────────────────────────────────────────────

// NormalizeInboundEndpoint maps a raw request path (which may carry
// prefixes like /antigravity, /openai) to its canonical form.
//
//	"/antigravity/v1/messages"   → "/v1/messages"
//	"/v1/chat/completions"       → "/v1/chat/completions"
//	"/openai/v1/responses/foo"   → "/v1/responses"
//	"/v1beta/models/gemini:gen"  → "/v1beta/models"
func NormalizeInboundEndpoint(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case strings.Contains(path, EndpointEmbeddings):
		return EndpointEmbeddings
	case strings.Contains(path, EndpointAlphaSearch) || isBareOrSubpathOf(strings.TrimRight(path, "/"), "/alpha/search") || isBareOrSubpathOf(strings.TrimRight(path, "/"), "/backend-api/codex/alpha/search"):
		return EndpointAlphaSearch
	case strings.Contains(path, EndpointChatCompletions):
		return EndpointChatCompletions
	case strings.Contains(path, EndpointMessages):
		return EndpointMessages
	case strings.Contains(path, EndpointImagesGenerations) || strings.Contains(path, "/images/generations"):
		return EndpointImagesGenerations
	case strings.Contains(path, EndpointImagesEdits) || strings.Contains(path, "/images/edits"):
		return EndpointImagesEdits
	case strings.Contains(path, EndpointImageTasks) || strings.Contains(path, "/images/tasks/"):
		return EndpointImageTasks
	case strings.Contains(path, EndpointVideosGenerations) || strings.Contains(path, "/videos/generations"):
		return EndpointVideosGenerations
	case strings.Contains(path, EndpointVideosEdits) || strings.Contains(path, "/videos/edits"):
		return EndpointVideosEdits
	case strings.Contains(path, EndpointVideosExtensions) || strings.Contains(path, "/videos/extensions"):
		return EndpointVideosExtensions
	case strings.Contains(path, EndpointVideos) || strings.Contains(path, "/videos/"):
		return EndpointVideos
	case strings.Contains(path, EndpointResponsesCompact) || isResponsesCompactAliasPath(path):
		return EndpointResponsesCompact
	case containsEndpointPath(path, EndpointResponses) || isResponsesRootAliasPath(path):
		return EndpointResponses
	case strings.Contains(path, EndpointGeminiModels):
		return EndpointGeminiModels
	default:
		return path
	}
}

func containsEndpointPath(path, endpoint string) bool {
	for {
		idx := strings.Index(path, endpoint)
		if idx < 0 {
			return false
		}
		suffix := path[idx+len(endpoint):]
		if suffix == "" || strings.HasPrefix(suffix, "/") {
			return true
		}
		path = suffix
	}
}

// isResponsesCompactAliasPath reports whether path is a bare Responses
// compact route or the equivalent Codex direct route. Keep this check before
// the root Responses alias because the latter is a prefix of compact.
func isResponsesCompactAliasPath(path string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return false
	}
	return isBareOrSubpathOf(trimmed, "/responses/compact") ||
		isBareOrSubpathOf(trimmed, "/backend-api/codex/responses/compact")
}

// isResponsesRootAliasPath recognizes only the intentionally exposed bare
// and Codex direct aliases, avoiding false positives such as /foo/responses.
func isResponsesRootAliasPath(path string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return false
	}
	return isBareOrSubpathOf(trimmed, "/responses") ||
		isBareOrSubpathOf(trimmed, "/backend-api/codex/responses")
}

func isBareOrSubpathOf(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// DeriveUpstreamEndpoint determines the upstream endpoint from the
// account platform and the normalized inbound endpoint.
//
// Platform-specific rules:
//   - OpenAI and Grok text compatibility routes forward to /v1/responses
//     (with optional subpath such as /v1/responses/compact preserved from
//     the raw URL); native endpoints such as embeddings and alpha search
//     retain their paths. Grok raw Chat requests override this through the
//     forwarding result consumed by resolveOpenAIUpstreamEndpoint.
//   - Anthropic  → /v1/messages
//   - Gemini     → /v1beta/models
//   - Antigravity → /v1/messages (Claude) or gemini (Gemini)
//   - Antigravity routes may target either Claude or Gemini, so the
//     inbound endpoint is used to distinguish.
func DeriveUpstreamEndpoint(inbound, rawRequestPath, platform string) string {
	inbound = strings.TrimSpace(inbound)

	switch service.CanonicalCNPlatform(platform) {
	case service.PlatformOpenAI, service.PlatformGrok:
		if inbound == EndpointEmbeddings || inbound == EndpointAlphaSearch || inbound == EndpointImagesGenerations || inbound == EndpointImagesEdits || inbound == EndpointVideosGenerations || inbound == EndpointVideosEdits || inbound == EndpointVideosExtensions || inbound == EndpointVideos {
			return inbound
		}
		// OpenAI forwards everything to the Responses API.
		// Preserve subresource suffix (e.g. /v1/responses/compact).
		if suffix := responsesSubpathSuffix(rawRequestPath); suffix != "" {
			return EndpointResponses + suffix
		}
		return EndpointResponses

	case service.PlatformAnthropic:
		return EndpointMessages

	case service.PlatformGemini:
		return EndpointGeminiModels

	case service.PlatformAntigravity:
		// Antigravity accounts serve both Claude and Gemini.
		if inbound == EndpointGeminiModels {
			return EndpointGeminiModels
		}
		return EndpointMessages

	case service.PlatformMiniMax:
		switch inbound {
		case EndpointMessages, EndpointChatCompletions:
			return inbound
		case EndpointResponsesCompact:
			return EndpointResponses
		default:
			return inbound
		}

	case service.PlatformZhipu:
		switch inbound {
		case EndpointMessages, EndpointChatCompletions:
			return inbound
		case EndpointResponsesCompact:
			return EndpointResponses
		default:
			return inbound
		}

	case service.PlatformKimi:
		switch inbound {
		case EndpointMessages, EndpointChatCompletions:
			return inbound
		case EndpointResponsesCompact:
			return EndpointResponses
		default:
			return inbound
		}

	case service.PlatformDeepSeek:
		switch inbound {
		case EndpointMessages, EndpointChatCompletions:
			return inbound
		case EndpointResponsesCompact:
			return EndpointResponses
		default:
			return inbound
		}

	case service.PlatformWindsurf:
		switch inbound {
		case EndpointMessages, EndpointChatCompletions:
			return inbound
		case EndpointResponsesCompact:
			return EndpointResponses
		default:
			return inbound
		}
	}

	// Unknown platform — fall back to inbound.
	return inbound
}

// responsesSubpathSuffix extracts the part after "/responses" in a raw
// request path, e.g. "/openai/v1/responses/compact" → "/compact".
// Returns "" when there is no meaningful suffix.
func responsesSubpathSuffix(rawPath string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rawPath), "/")
	idx := strings.LastIndex(trimmed, "/responses")
	if idx < 0 {
		return ""
	}
	suffix := trimmed[idx+len("/responses"):]
	if suffix == "" || suffix == "/" {
		return ""
	}
	if !strings.HasPrefix(suffix, "/") {
		return ""
	}
	return suffix
}

// ──────────────────────────────────────────────────────────
// Middleware
// ──────────────────────────────────────────────────────────

// InboundEndpointMiddleware normalizes the request path and stores the
// canonical inbound endpoint in gin.Context so that every handler in
// the chain can read it via GetInboundEndpoint.
//
// Apply this middleware to all gateway route groups.
func InboundEndpointMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := ""
		if c.Request != nil && c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		if path == "" {
			path = c.FullPath()
		}
		c.Set(ctxKeyInboundEndpoint, NormalizeInboundEndpoint(path))
		c.Next()
	}
}

// ──────────────────────────────────────────────────────────
// Context helpers — used by handlers before building
// RecordUsageInput / RecordUsageLongContextInput.
// ──────────────────────────────────────────────────────────

// GetInboundEndpoint returns the canonical inbound endpoint stored by
// InboundEndpointMiddleware. If the middleware did not run (e.g. in
// tests), it falls back to normalizing c.Request.URL.Path on the fly
// before c.FullPath(), which may collapse wildcard subpaths.
func GetInboundEndpoint(c *gin.Context) string {
	if v, ok := c.Get(ctxKeyInboundEndpoint); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// Fallback: normalize on the fly.
	path := ""
	if c != nil {
		if c.Request != nil && c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		if path == "" {
			path = c.FullPath()
		}
	}
	return NormalizeInboundEndpoint(path)
}

// GetUpstreamEndpoint derives the upstream endpoint from the context
// and the account platform. Handlers call this after scheduling an
// account, passing account.Platform.
func GetUpstreamEndpoint(c *gin.Context, platform string) string {
	if c != nil {
		if value, ok := c.Get(ctxKeyActualUpstreamEndpoint); ok {
			if endpoint, ok := value.(string); ok && endpoint != "" {
				return endpoint
			}
		}
	}
	inbound := GetInboundEndpoint(c)
	rawPath := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		rawPath = c.Request.URL.Path
	}
	if inbound == EndpointResponses && c != nil && c.Request != nil && c.Request.Method == http.MethodPost && isResponsesRootPath(rawPath) {
		if upstreamEndpoint, ok := providerResponsesBridgeEndpoint(platform); ok {
			return upstreamEndpoint
		}
	}
	return DeriveUpstreamEndpoint(inbound, rawPath, platform)
}

func isResponsesRootPath(path string) bool {
	switch strings.TrimRight(strings.TrimSpace(path), "/") {
	case EndpointResponses, "/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

func providerResponsesBridgeEndpoint(platform string) (string, bool) {
	switch service.CanonicalCNPlatform(platform) {
	case service.PlatformMiniMax, service.PlatformZhipu, service.PlatformKimi:
		return EndpointMessages, true
	case service.PlatformDeepSeek, service.PlatformWindsurf:
		return EndpointChatCompletions, true
	default:
		return "", false
	}
}

func setActualUpstreamEndpoint(c *gin.Context, endpoint string) {
	if c != nil {
		c.Set(ctxKeyActualUpstreamEndpoint, strings.TrimSpace(endpoint))
	}
}

func shouldUseAntigravityCompat(account *service.Account) bool {
	return account != nil &&
		account.Platform == service.PlatformAntigravity &&
		account.Type == service.AccountTypeOAuth
}
