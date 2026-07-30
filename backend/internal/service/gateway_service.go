package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/cespare/xxhash/v2"
	gocache "github.com/patrickmn/go-cache"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

const (
	claudeAPIURL            = "https://api.anthropic.com/v1/messages?beta=true"
	claudeAPICountTokensURL = "https://api.anthropic.com/v1/messages/count_tokens?beta=true"
	stickySessionTTL        = time.Hour // 粘性会话TTL
	defaultMaxLineSize      = 500 * 1024 * 1024
	// Canonical Claude Code banner. Keep it EXACT (no trailing whitespace/newlines)
	// to match real Claude CLI traffic as closely as possible. When we need a visual
	// separator between system blocks, we add "\n\n" at concatenation time.
	claudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."
	// claudeCodeSystemPromptExpansion 是真实 Claude Code 主系统提示词中"与具体工具无关"
	// 的通用段落（身份/用途总述 + 安全声明 + URL 告警 + Tone and style），逐字取自真实
	// CLI（2.1.x 一致）。伪装路径用它把 system 块数从 2 提升到 3、体量贴近真实 CC，同时
	// 刻意排除 # Doing tasks / # Using your tools / # Executing actions 等会污染被代理
	// 用户行为的工具专属指令。
	claudeCodeSystemPromptExpansion = `You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.

# Tone and style
 - Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.
 - Your responses should be short and concise.
 - When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.
 - When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.
 - Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`
	maxCacheControlBlocks = 4 // Anthropic API 允许的最大 cache_control 块数量

	defaultUserGroupRateCacheTTL           = 30 * time.Second
	defaultModelsListCacheTTL              = 15 * time.Second
	postUsageBillingTimeout                = 15 * time.Second
	claudeCodeNoopDeltaKeepaliveMinVersion = "2.1.193"
	debugGatewayBodyEnv                    = "SUB2API_DEBUG_GATEWAY_BODY"
	// 上游错误体只需要提取错误 JSON/日志摘要，默认 512KiB 避免错误风暴叠加大请求体。
	gatewayUpstreamErrorBodyReadLimit int64 = 512 << 10
)

const (
	claudeMimicDebugInfoKey = "claude_mimic_debug_info"
)

const (
	cacheTTLTarget5m = "5m"
	cacheTTLTarget1h = "1h"
)

// ForceCacheBillingContextKey 强制缓存计费上下文键
// 用于粘性会话切换时，将 input_tokens 转为 cache_read_input_tokens 计费
type forceCacheBillingKeyType struct{}

// accountWithLoad 账号与负载信息的组合，用于负载感知调度
type accountWithLoad struct {
	account  *Account
	loadInfo *AccountLoadInfo
}

var ForceCacheBillingContextKey = forceCacheBillingKeyType{}

var (
	windowCostPrefetchCacheHitTotal  atomic.Int64
	windowCostPrefetchCacheMissTotal atomic.Int64
	windowCostPrefetchBatchSQLTotal  atomic.Int64
	windowCostPrefetchFallbackTotal  atomic.Int64
	windowCostPrefetchErrorTotal     atomic.Int64

	userGroupRateCacheHitTotal      atomic.Int64
	userGroupRateCacheMissTotal     atomic.Int64
	userGroupRateCacheLoadTotal     atomic.Int64
	userGroupRateCacheSFSharedTotal atomic.Int64
	userGroupRateCacheFallbackTotal atomic.Int64

	modelsListCacheHitTotal   atomic.Int64
	modelsListCacheMissTotal  atomic.Int64
	modelsListCacheStoreTotal atomic.Int64

	// Deprecated: flusher_enabled=true 后不再增长(仅 flag=false 降级直写路径使用);新主路径见 FlusherMetrics。remove after 2026-09。
	// userPlatformQuotaDBIncrErrorTotal 统计 finalizePostUsageBilling 异步 goroutine
	// 中 IncrementUsageWithReset 失败次数。Redis 已成功累加 + DB 写失败意味着
	// Redis cache TTL 过期或被清后该笔 cost 会丢失（与实际消费偏差）。
	// oncall 通过 GatewayUserPlatformQuotaIncrStats() 暴露给 ops 面板做阈值告警。
	userPlatformQuotaDBIncrErrorTotal atomic.Int64
	// Deprecated: flusher_enabled=true 后不再增长(仅 flag=false 降级直写路径使用);新主路径见 FlusherMetrics。remove after 2026-09。
	// userPlatformQuotaDBIncrLegacyErrorTotal 统计 legacy postUsageBilling
	// （applyUsageBilling 在 repo==nil 时 fallback）路径下的失败次数；
	// 与 DB Incr 失败分开计数，便于区分"主路径暂时故障"vs"基础设施长期未配齐"。
	userPlatformQuotaDBIncrLegacyErrorTotal atomic.Int64
	// userPlatformQuotaSentinelSetCacheErrorTotal 统计 checkUserPlatformQuotaEligibility
	// 在 DB 无行时回填 sentinel cache entry 写 Redis 失败的次数（phase A）。
	userPlatformQuotaSentinelSetCacheErrorTotal atomic.Int64
)

func GatewayWindowCostPrefetchStats() (cacheHit, cacheMiss, batchSQL, fallback, errCount int64) {
	return windowCostPrefetchCacheHitTotal.Load(),
		windowCostPrefetchCacheMissTotal.Load(),
		windowCostPrefetchBatchSQLTotal.Load(),
		windowCostPrefetchFallbackTotal.Load(),
		windowCostPrefetchErrorTotal.Load()
}

func GatewayUserGroupRateCacheStats() (cacheHit, cacheMiss, load, singleflightShared, fallback int64) {
	return userGroupRateCacheHitTotal.Load(),
		userGroupRateCacheMissTotal.Load(),
		userGroupRateCacheLoadTotal.Load(),
		userGroupRateCacheSFSharedTotal.Load(),
		userGroupRateCacheFallbackTotal.Load()
}

func GatewayModelsListCacheStats() (cacheHit, cacheMiss, store int64) {
	return modelsListCacheHitTotal.Load(), modelsListCacheMissTotal.Load(), modelsListCacheStoreTotal.Load()
}

// GatewayUserPlatformQuotaIncrStats 返回 (mainPathErr, legacyPathErr, sentinelSetErr)。
// mainPathErr：finalizePostUsageBilling 异步 goroutine 写 DB 失败累计次数；
// legacyPathErr：postUsageBilling fallback 路径写 DB 失败累计次数；
// sentinelSetErr：DB 无行时回填 sentinel cache entry 写 Redis 失败累计次数。
// ops 监控面板可以按"持续上升斜率"做告警阈值。
func GatewayUserPlatformQuotaIncrStats() (mainPathErr, legacyPathErr, sentinelSetErr int64) {
	return userPlatformQuotaDBIncrErrorTotal.Load(),
		userPlatformQuotaDBIncrLegacyErrorTotal.Load(),
		userPlatformQuotaSentinelSetCacheErrorTotal.Load()
}

// GatewayUserPlatformQuotaFlusherStats 暴露 flusher 运行指标供 ops/health 面板查询。
func GatewayUserPlatformQuotaFlusherStats(f *UserPlatformQuotaUsageFlusher) map[string]int64 {
	if f == nil || f.metrics == nil {
		return nil
	}
	m := f.metrics
	return map[string]int64{
		"flush_success":        m.FlushSuccessTotal.Load(),
		"flush_error":          m.FlushErrorTotal.Load(),
		"flush_batch_size":     m.FlushBatchSizeTotal.Load(),
		"flush_latency_ms_max": m.FlushLatencyMsMax.Load(),
		"dirty_readd":          m.DirtyReaddTotal.Load(),
		"dirty_lost":           m.DirtyLostTotal.Load(),
		"flush_fk_violation":   m.FlushFKViolationTotal.Load(),
	}
}

func openAIStreamEventIsTerminal(data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return openAIStreamEventTypeIsTerminal(gjson.Get(trimmed, "type").String())
}

// openAIStreamEventIsTerminalWithType 复用已提取的 type，避免 SSE 热路径重复扫描 JSON。
func openAIStreamEventIsTerminalWithType(data, eventType string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return openAIStreamEventTypeIsTerminal(eventType)
}

func openAIStreamEventTypeIsTerminal(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func anthropicStreamEventIsTerminal(eventName, data string) bool {
	if strings.EqualFold(strings.TrimSpace(eventName), "message_stop") {
		return true
	}
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return gjson.Get(trimmed, "type").String() == "message_stop"
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// IsForceCacheBilling 检查是否启用强制缓存计费
func IsForceCacheBilling(ctx context.Context) bool {
	v, _ := ctx.Value(ForceCacheBillingContextKey).(bool)
	return v
}

// WithForceCacheBilling 返回带有强制缓存计费标记的上下文
func WithForceCacheBilling(ctx context.Context) context.Context {
	return context.WithValue(ctx, ForceCacheBillingContextKey, true)
}

func (s *GatewayService) debugModelRoutingEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugModelRouting.Load()
}

func (s *GatewayService) debugClaudeMimicEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugClaudeMimic.Load()
}

func parseDebugEnvBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shortSessionHash(sessionHash string) string {
	if sessionHash == "" {
		return ""
	}
	if len(sessionHash) <= 8 {
		return sessionHash
	}
	return sessionHash[:8]
}

func redactAuthHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Keep scheme for debugging, redact secret.
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "Bearer [redacted]"
	}
	return "[redacted]"
}

func safeHeaderValueForLog(key string, v string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "authorization", "x-api-key":
		return redactAuthHeaderValue(v)
	default:
		return strings.TrimSpace(v)
	}
}

func extractSystemPreviewFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sys := gjson.GetBytes(body, "system")
	if !sys.Exists() {
		return ""
	}

	switch {
	case sys.IsArray():
		for _, item := range sys.Array() {
			if !item.IsObject() {
				continue
			}
			if strings.EqualFold(item.Get("type").String(), "text") {
				if t := item.Get("text").String(); strings.TrimSpace(t) != "" {
					return t
				}
			}
		}
		return ""
	case sys.Type == gjson.String:
		return sys.String()
	default:
		return ""
	}
}

func buildClaudeMimicDebugLine(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) string {
	if req == nil {
		return ""
	}

	// Only log a minimal fingerprint to avoid leaking user content.
	interesting := []string{
		"user-agent",
		"x-app",
		"anthropic-dangerous-direct-browser-access",
		"anthropic-version",
		"anthropic-beta",
		"x-stainless-lang",
		"x-stainless-package-version",
		"x-stainless-os",
		"x-stainless-arch",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
		"x-stainless-retry-count",
		"x-stainless-timeout",
		"authorization",
		"x-api-key",
		"content-type",
		"accept",
		"x-stainless-helper-method",
	}

	h := make([]string, 0, len(interesting))
	for _, k := range interesting {
		if v := req.Header.Get(k); v != "" {
			h = append(h, fmt.Sprintf("%s=%q", k, safeHeaderValueForLog(k, v)))
		}
	}

	metaUserID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	sysPreview := strings.TrimSpace(extractSystemPreviewFromBody(body))

	// Truncate preview to keep logs sane.
	if len(sysPreview) > 300 {
		sysPreview = sysPreview[:300] + "..."
	}
	sysPreview = strings.ReplaceAll(sysPreview, "\n", "\\n")
	sysPreview = strings.ReplaceAll(sysPreview, "\r", "\\r")

	aid := int64(0)
	aname := ""
	if account != nil {
		aid = account.ID
		aname = account.Name
	}

	return fmt.Sprintf(
		"url=%s account=%d(%s) tokenType=%s mimic=%t meta.user_id=%q system.preview=%q headers={%s}",
		req.URL.String(),
		aid,
		aname,
		tokenType,
		mimicClaudeCode,
		metaUserID,
		sysPreview,
		strings.Join(h, " "),
	)
}

func logClaudeMimicDebug(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) {
	line := buildClaudeMimicDebugLine(req, body, account, tokenType, mimicClaudeCode)
	if line == "" {
		return
	}
	logger.LegacyPrintf("service.gateway", "[ClaudeMimicDebug] %s", line)
}

func isClaudeCodeCredentialScopeError(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	return strings.Contains(m, "only authorized for use with claude code") &&
		strings.Contains(m, "cannot be used for other api requests")
}

// sseDataRe matches SSE data lines with optional whitespace after colon.
// Some upstream APIs return non-standard "data:" without space (should be "data: ").
var (
	sseDataRe            = regexp.MustCompile(`^data:\s*`)
	claudeCliUserAgentRe = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+`)

	// claudeCodePromptPrefixes 用于检测 Claude Code 系统提示词的前缀列表
	// 支持多种变体：标准版、Agent SDK 版、Explore Agent 版、Compact 版等
	// 注意：前缀之间不应存在包含关系，否则会导致冗余匹配
	claudeCodePromptPrefixes = []string{
		"You are Claude Code, Anthropic's official CLI for Claude",             // 标准版 & Agent SDK 版（含 running within...）
		"You are a Claude agent, built on Anthropic's Claude Agent SDK",        // Agent SDK 变体
		"You are a file search specialist for Claude Code",                     // Explore Agent 版
		"You are a helpful AI assistant tasked with summarizing conversations", // Compact 版
	}
)

// ErrNoAvailableAccounts 表示没有可用的账号
var ErrNoAvailableAccounts = errors.New("no available accounts")

// ErrClaudeCodeOnly 表示分组仅允许 Claude Code 客户端访问
var ErrClaudeCodeOnly = errors.New("this group only allows Claude Code clients")

// allowedHeaders 白名单headers（参考CRS项目）
var allowedHeaders = map[string]bool{
	"accept":                                    true,
	"x-stainless-retry-count":                   true,
	"x-stainless-timeout":                       true,
	"x-stainless-lang":                          true,
	"x-stainless-package-version":               true,
	"x-stainless-os":                            true,
	"x-stainless-arch":                          true,
	"x-stainless-runtime":                       true,
	"x-stainless-runtime-version":               true,
	"x-stainless-helper-method":                 true,
	"anthropic-dangerous-direct-browser-access": true,
	"anthropic-version":                         true,
	"x-app":                                     true,
	"anthropic-beta":                            true,
	"accept-language":                           true,
	"sec-fetch-mode":                            true,
	"user-agent":                                true,
	"content-type":                              true,
	"x-claude-code-session-id":                  true,
	"x-client-request-id":                       true,
}

// GatewayCache 定义网关服务的缓存操作接口。
// 提供粘性会话（Sticky Session）的存储、查询、刷新和删除功能。
//
// GatewayCache defines cache operations for gateway service.
// Provides sticky session storage, retrieval, refresh and deletion capabilities.
type GatewayCache interface {
	// GetSessionAccountID 获取粘性会话绑定的账号 ID
	// Get the account ID bound to a sticky session
	GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error)
	// SetSessionAccountID 设置粘性会话与账号的绑定关系
	// Set the binding between sticky session and account
	SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error
	// RefreshSessionTTL 刷新粘性会话的过期时间
	// Refresh the expiration time of a sticky session
	RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error
	// DeleteSessionAccountID 删除粘性会话绑定，用于账号不可用时主动清理
	// Delete sticky session binding, used to proactively clean up when account becomes unavailable
	DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error
}

// derefGroupID safely dereferences *int64 to int64, returning 0 if nil
func derefGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func resolveUserGroupRateCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.UserGroupRateCacheTTLSeconds <= 0 {
		return defaultUserGroupRateCacheTTL
	}
	return time.Duration(cfg.Gateway.UserGroupRateCacheTTLSeconds) * time.Second
}

func resolveModelsListCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.ModelsListCacheTTLSeconds <= 0 {
		return defaultModelsListCacheTTL
	}
	return time.Duration(cfg.Gateway.ModelsListCacheTTLSeconds) * time.Second
}

func modelsListCacheKey(groupID *int64, platform string) string {
	return fmt.Sprintf("%d|%s", derefGroupID(groupID), strings.TrimSpace(platform))
}

func prefetchedStickyGroupIDFromContext(ctx context.Context) (int64, bool) {
	return PrefetchedStickyGroupIDFromContext(ctx)
}

func prefetchedStickyAccountIDFromContext(ctx context.Context, groupID *int64) int64 {
	prefetchedGroupID, ok := prefetchedStickyGroupIDFromContext(ctx)
	if !ok || prefetchedGroupID != derefGroupID(groupID) {
		return 0
	}
	if accountID, ok := PrefetchedStickyAccountIDFromContext(ctx); ok && accountID > 0 {
		return accountID
	}
	return 0
}

// shouldClearStickySession 检查账号是否处于不可调度状态，需要清理粘性会话绑定。
// 委托 IsSchedulable() 判断账号级可调度性（状态、配额、过载、限流等），
// 额外检查模型级限流。
//
// shouldClearStickySession checks if an account is in an unschedulable state
// and the sticky session binding should be cleared.
// Delegates to IsSchedulable() for account-level checks, plus model-level rate limiting.
func shouldClearStickySession(account *Account, requestedModel string) bool {
	if account == nil {
		return false
	}
	if !account.IsSchedulable() {
		return true
	}
	if remaining := account.GetRateLimitRemainingTimeWithContext(context.Background(), requestedModel); remaining > 0 {
		return true
	}
	return false
}

type AccountWaitPlan struct {
	AccountID      int64
	MaxConcurrency int
	Timeout        time.Duration
	MaxWaiting     int
}

type AccountSelectionResult struct {
	Account     *Account
	Acquired    bool
	ReleaseFunc func()
	WaitPlan    *AccountWaitPlan // nil means no wait allowed
}

// ClaudeUsage 表示Claude API返回的usage信息
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreation5mTokens    int // 5分钟缓存创建token（来自嵌套 cache_creation 对象）
	CacheCreation1hTokens    int // 1小时缓存创建token（来自嵌套 cache_creation 对象）
	ImageOutputTokens        int `json:"image_output_tokens,omitempty"`
}

// ForwardResult 转发结果
type ForwardResult struct {
	RequestID string
	Usage     ClaudeUsage
	Model     string
	// UpstreamModel is the actual upstream model after mapping.
	// Prefer empty when it is identical to Model; persistence normalizes equal values away as no-op mappings.
	UpstreamModel string
	// BillingModel locks an independently priced media SKU selected before
	// forwarding (for example Responses tools[].model). When non-empty for an
	// image result, settlement must not replace it with the top-level text
	// model selected by ChannelUsageFields.BillingModelSource.
	BillingModel     string
	Stream           bool
	Duration         time.Duration
	FirstTokenMs     *int // 首字时间（流式请求）
	ClientDisconnect bool // 客户端是否在流式传输过程中断开
	ReasoningEffort  *string

	// 图片生成计费字段（图片生成模型使用）
	ImageCount         int    // 生成的图片数量
	ImageSize          string // 最终计费尺寸 "1K", "2K", "4K"
	ImageInputSize     string // 请求中的原始图片尺寸
	ImageOutputSize    string // 上游响应中的图片尺寸
	ImageOutputSizes   []string
	ImageSizeSource    string
	ImageSizeBreakdown map[string]int
}

// GatewayFailureStage identifies which request stage failed. The zero value is
// intentionally treated as inference so existing UpstreamFailoverError callers
// retain their current behavior.
type GatewayFailureStage string

const (
	GatewayFailureStageInference   GatewayFailureStage = "inference"
	GatewayFailureStageAccountAuth GatewayFailureStage = "account_auth"
)

// GatewayFailureScope identifies whether selecting another account can help.
type GatewayFailureScope string

const (
	GatewayFailureScopeAccount  GatewayFailureScope = "account"
	GatewayFailureScopeProvider GatewayFailureScope = "provider"
	GatewayFailureScopeRequest  GatewayFailureScope = "request"
)

// NextAccountAction is tri-state for backwards compatibility. The zero value
// means legacy retry behavior; only NextAccountStop explicitly short-circuits.
type NextAccountAction uint8

const (
	NextAccountLegacyRetry NextAccountAction = iota
	NextAccountRetry
	NextAccountStop
)

type GatewayFailureReason string

// UpstreamFailoverError indicates an upstream or credential error that may
// trigger account failover. Additive metadata keeps existing composite literals
// source-compatible and preserves their legacy retry-next-account behavior.
type UpstreamFailoverError struct {
	StatusCode               int
	ResponseBody             []byte      // 上游响应体，用于错误透传规则匹配
	ResponseHeaders          http.Header // 上游响应头，用于透传 cf-ray/cf-mitigated/content-type 等诊断信息
	ForceCacheBilling        bool        // Antigravity 粘性会话切换时设为 true
	RetryableOnSameAccount   bool        // 临时性错误（如 Google 间歇性 400、空响应），应在同一账号上重试 N 次再切换
	SafeToFailoverAfterWrite bool        // 仅写出 SSE 注释等非语义字节时，仍可在同一客户端流中切换账号
	Stage                    GatewayFailureStage
	Scope                    GatewayFailureScope
	Reason                   GatewayFailureReason
	NextAccountAction        NextAccountAction
	ClientStatusCode         int
	ClientMessage            string
}

func (e *UpstreamFailoverError) Error() string {
	if e != nil && e.Stage == GatewayFailureStageAccountAuth {
		return fmt.Sprintf("credential failure: %s (failover)", e.Reason)
	}
	return fmt.Sprintf("upstream error: %d (failover)", e.StatusCode)
}

func (e *UpstreamFailoverError) ShouldRetryNextAccount() bool {
	return e != nil && e.NextAccountAction != NextAccountStop
}

func (e *UpstreamFailoverError) IsCredentialFailure() bool {
	return e != nil && e.Stage == GatewayFailureStageAccountAuth
}

// ShouldReportAccountScheduleFailure prevents provider- and request-scoped
// credential failures from being misattributed to the selected account. Legacy
// and inference failures retain their existing scheduler-health behavior.
func (e *UpstreamFailoverError) ShouldReportAccountScheduleFailure() bool {
	if e == nil {
		return false
	}
	return !e.IsCredentialFailure() || e.Scope == GatewayFailureScopeAccount
}

// sseStreamErrorEventError 表示上游 SSE 流体内出现 event:error 帧。
// RawData 是该事件 data: 行的原始 JSON 字符串
// （Anthropic 标准结构 {"type":"error","error":{"type":"...","message":"..."}}）。
// Error() 保持原字符串以兼容现有日志/检索；调用方应通过 errors.As
// 提取 RawData 并构造 UpstreamFailoverError.ResponseBody。
type sseStreamErrorEventError struct {
	RawData string
}

func (e *sseStreamErrorEventError) Error() string { return "have error in stream" }

// TempUnscheduleRetryableError 对 RetryableOnSameAccount 类型的 failover 错误触发临时封禁。
// 由 handler 层在同账号重试全部用尽、切换账号时调用。
func (s *GatewayService) TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return
	}
	// 根据状态码选择封禁策略
	switch failoverErr.StatusCode {
	case http.StatusBadRequest:
		tempUnscheduleGoogleConfigError(ctx, s.accountRepo, accountID, "[handler]")
	case http.StatusBadGateway:
		tempUnscheduleEmptyResponse(ctx, s.accountRepo, accountID, "[handler]")
	}
}

func (s *GatewayService) HandleMiniMaxUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setMiniMaxRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == 529:
		s.setMiniMaxOverloaded(ctx, account.ID, s.miniMaxOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		s.setMiniMaxOverloaded(ctx, account.ID, s.miniMaxOverloadUntil())
	}
}

func (s *GatewayService) HandleGLMUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	mapping := MapGLMUpstreamStatus(failoverErr.StatusCode)
	if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden {
		headers := failoverErr.ResponseHeaders
		if headers == nil {
			headers = http.Header{}
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setGLMAuthError(ctx, account.ID, failoverErr)
		return
	}
	if !mapping.Retryable {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setGLMRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == 529:
		s.setGLMOverloaded(ctx, account.ID, s.glmOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		return
	}
}

func (s *GatewayService) HandleKimiUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	mapping := MapKimiUpstreamStatus(failoverErr.StatusCode)
	if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden {
		headers := failoverErr.ResponseHeaders
		if headers == nil {
			headers = http.Header{}
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setKimiAuthError(ctx, account.ID, failoverErr)
		return
	}
	if !mapping.Retryable {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setKimiRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == 529:
		s.setKimiOverloaded(ctx, account.ID, s.glmOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		s.setKimiOverloaded(ctx, account.ID, s.glmOverloadUntil())
	}
}

func (s *GatewayService) HandleDeepSeekUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	mapping := MapDeepSeekUpstreamStatus(failoverErr.StatusCode)
	if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden || failoverErr.StatusCode == http.StatusPaymentRequired {
		headers := failoverErr.ResponseHeaders
		if headers == nil {
			headers = http.Header{}
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil && failoverErr.StatusCode != http.StatusPaymentRequired {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setDeepSeekAuthError(ctx, account.ID, failoverErr)
		return
	}
	if !mapping.Retryable {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setDeepSeekRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == http.StatusServiceUnavailable || failoverErr.StatusCode == 529:
		s.setDeepSeekOverloaded(ctx, account.ID, s.glmOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		s.setDeepSeekOverloaded(ctx, account.ID, s.glmOverloadUntil())
	}
}

func (s *GatewayService) HandleWindsurfUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	mapping := MapWindsurfUpstreamStatus(failoverErr.StatusCode)
	if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden || failoverErr.StatusCode == http.StatusPaymentRequired {
		headers := failoverErr.ResponseHeaders
		if headers == nil {
			headers = http.Header{}
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil && failoverErr.StatusCode != http.StatusPaymentRequired {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setWindsurfAuthError(ctx, account.ID, failoverErr)
		return
	}
	if !mapping.Retryable {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setWindsurfRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == http.StatusServiceUnavailable || failoverErr.StatusCode == 529:
		s.setWindsurfOverloaded(ctx, account.ID, s.glmOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		s.setWindsurfOverloaded(ctx, account.ID, s.glmOverloadUntil())
	}
}

func (s *GatewayService) HandleOpenCodeUpstreamError(ctx context.Context, account *Account, failoverErr *UpstreamFailoverError) {
	if s == nil || account == nil || failoverErr == nil {
		return
	}
	mapping := MapOpenCodeUpstreamStatus(failoverErr.StatusCode)
	if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden || failoverErr.StatusCode == http.StatusPaymentRequired {
		headers := failoverErr.ResponseHeaders
		if headers == nil {
			headers = http.Header{}
		}
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil && failoverErr.StatusCode != http.StatusPaymentRequired {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setOpenCodeAuthError(ctx, account.ID, failoverErr)
		return
	}
	if !mapping.Retryable {
		return
	}
	headers := failoverErr.ResponseHeaders
	if headers == nil {
		headers = http.Header{}
	}
	switch {
	case failoverErr.StatusCode == http.StatusTooManyRequests:
		if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, failoverErr.StatusCode, headers, failoverErr.ResponseBody)
			return
		}
		s.setOpenCodeRateLimited(ctx, account.ID, time.Now().Add(5*time.Minute))
	case failoverErr.StatusCode == http.StatusServiceUnavailable || failoverErr.StatusCode == 529:
		s.setOpenCodeOverloaded(ctx, account.ID, s.glmOverloadUntil())
	case failoverErr.StatusCode >= http.StatusInternalServerError:
		s.setOpenCodeOverloaded(ctx, account.ID, s.glmOverloadUntil())
	}
}

func (s *GatewayService) setGLMAuthError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.accountRepo == nil || failoverErr == nil {
		return
	}
	message := "Authentication failed"
	if failoverErr.StatusCode == http.StatusForbidden {
		message = "Authorization failed"
	}
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(failoverErr.ResponseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		message = fmt.Sprintf("%s (%d): %s", message, failoverErr.StatusCode, upstreamMsg)
	} else {
		message = fmt.Sprintf("%s (%d): invalid GLM credentials", message, failoverErr.StatusCode)
	}
	if err := s.accountRepo.SetError(ctx, accountID, message); err != nil {
		slog.Warn("glm_auth_error_set_failed", "account_id", accountID, "status_code", failoverErr.StatusCode, "error", err)
	}
}

func (s *GatewayService) setKimiAuthError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.accountRepo == nil || failoverErr == nil {
		return
	}
	message := "Authentication failed"
	if failoverErr.StatusCode == http.StatusForbidden {
		message = "Authorization failed"
	}
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(failoverErr.ResponseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		message = fmt.Sprintf("%s (%d): %s", message, failoverErr.StatusCode, upstreamMsg)
	} else {
		message = fmt.Sprintf("%s (%d): invalid Kimi credentials", message, failoverErr.StatusCode)
	}
	if err := s.accountRepo.SetError(ctx, accountID, message); err != nil {
		slog.Warn("kimi_auth_error_set_failed", "account_id", accountID, "status_code", failoverErr.StatusCode, "error", err)
	}
}

func (s *GatewayService) setDeepSeekAuthError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.accountRepo == nil || failoverErr == nil {
		return
	}
	message := "Authentication failed"
	if failoverErr.StatusCode == http.StatusForbidden {
		message = "Authorization failed"
	}
	if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = "Insufficient DeepSeek balance"
	}
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(failoverErr.ResponseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		message = fmt.Sprintf("%s (%d): %s", message, failoverErr.StatusCode, upstreamMsg)
	} else if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = fmt.Sprintf("%s (%d)", message, failoverErr.StatusCode)
	} else {
		message = fmt.Sprintf("%s (%d): invalid DeepSeek credentials", message, failoverErr.StatusCode)
	}
	if err := s.accountRepo.SetError(ctx, accountID, message); err != nil {
		slog.Warn("deepseek_auth_error_set_failed", "account_id", accountID, "status_code", failoverErr.StatusCode, "error", err)
	}
}

func (s *GatewayService) setWindsurfAuthError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.accountRepo == nil || failoverErr == nil {
		return
	}
	message := "Authentication failed"
	if failoverErr.StatusCode == http.StatusForbidden {
		message = "Authorization failed"
	}
	if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = "Insufficient Windsurf quota"
	}
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(failoverErr.ResponseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		message = fmt.Sprintf("%s (%d): %s", message, failoverErr.StatusCode, upstreamMsg)
	} else if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = fmt.Sprintf("%s (%d)", message, failoverErr.StatusCode)
	} else {
		message = fmt.Sprintf("%s (%d): invalid Windsurf credentials", message, failoverErr.StatusCode)
	}
	if err := s.accountRepo.SetError(ctx, accountID, message); err != nil {
		slog.Warn("windsurf_auth_error_set_failed", "account_id", accountID, "status_code", failoverErr.StatusCode, "error", err)
	}
}

func (s *GatewayService) setOpenCodeAuthError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.accountRepo == nil || failoverErr == nil {
		return
	}
	message := "Authentication failed"
	if failoverErr.StatusCode == http.StatusForbidden {
		message = "Authorization failed"
	}
	if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = "Insufficient OpenCode quota"
	}
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(failoverErr.ResponseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		message = fmt.Sprintf("%s (%d): %s", message, failoverErr.StatusCode, upstreamMsg)
	} else if failoverErr.StatusCode == http.StatusPaymentRequired {
		message = fmt.Sprintf("%s (%d)", message, failoverErr.StatusCode)
	} else {
		message = fmt.Sprintf("%s (%d): invalid OpenCode credentials", message, failoverErr.StatusCode)
	}
	if err := s.accountRepo.SetError(ctx, accountID, message); err != nil {
		slog.Warn("opencode_auth_error_set_failed", "account_id", accountID, "status_code", failoverErr.StatusCode, "error", err)
	}
}

func (s *GatewayService) setMiniMaxRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("minimax_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setMiniMaxOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("minimax_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setGLMRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("glm_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setGLMOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("glm_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setKimiRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("kimi_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setKimiOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("kimi_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setDeepSeekRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("deepseek_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setDeepSeekOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("deepseek_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setWindsurfRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("windsurf_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setWindsurfOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("windsurf_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setOpenCodeRateLimited(ctx context.Context, accountID int64, resetAt time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt); err != nil {
		slog.Warn("opencode_rate_limit_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) setOpenCodeOverloaded(ctx context.Context, accountID int64, until time.Time) {
	if s == nil || s.accountRepo == nil {
		return
	}
	if err := s.accountRepo.SetOverloaded(ctx, accountID, until); err != nil {
		slog.Warn("opencode_overload_set_failed", "account_id", accountID, "error", err)
	}
}

func (s *GatewayService) glmOverloadUntil() time.Time {
	minutes := 10
	if s != nil && s.cfg != nil && s.cfg.RateLimit.OverloadCooldownMinutes > 0 {
		minutes = s.cfg.RateLimit.OverloadCooldownMinutes
	}
	return time.Now().Add(time.Duration(minutes) * time.Minute)
}

func (s *GatewayService) miniMaxOverloadUntil() time.Time {
	minutes := 10
	if s != nil && s.cfg != nil && s.cfg.RateLimit.OverloadCooldownMinutes > 0 {
		minutes = s.cfg.RateLimit.OverloadCooldownMinutes
	}
	return time.Now().Add(time.Duration(minutes) * time.Minute)
}

// GatewayService handles API gateway operations
type gatewayServiceModelCatalog interface {
	ListForPlatform(context.Context, *int64, string, bool) ([]string, error)
}

type GatewayService struct {
	accountRepo      AccountRepository
	groupRepo        GroupRepository
	usageLogRepo     UsageLogRepository
	usageBillingRepo UsageBillingRepository
	// Tests with lightweight stubs may explicitly opt into the pre-durable
	// settlement path. Production constructors intentionally leave this false.
	allowLegacyUsageBillingForTests bool
	userRepo                        UserRepository
	userSubRepo                     UserSubscriptionRepository
	userGroupRateRepo               UserGroupRateRepository
	cache                           GatewayCache
	digestStore                     *DigestSessionStore
	cfg                             *config.Config
	schedulerSnapshot               *SchedulerSnapshotService
	billingService                  *BillingService
	pricingGuardRequired            bool
	rateLimitService                *RateLimitService
	billingCacheService             *BillingCacheService
	identityService                 *IdentityService
	httpUpstream                    HTTPUpstream
	deferredService                 *DeferredService
	concurrencyService              *ConcurrencyService
	claudeTokenProvider             *ClaudeTokenProvider
	sessionLimitCache               SessionLimitCache // 会话数量限制缓存（仅 Anthropic OAuth/SetupToken）
	rpmCache                        RPMCache          // RPM 计数缓存（仅 Anthropic OAuth/SetupToken）
	userGroupRateResolver           *userGroupRateResolver
	userGroupRateCache              *gocache.Cache
	userGroupRateSF                 singleflight.Group
	modelsListCache                 *gocache.Cache
	modelsListCacheTTL              time.Duration
	settingService                  *SettingService
	responseHeaderFilter            *responseheaders.CompiledHeaderFilter
	debugModelRouting               atomic.Bool
	debugClaudeMimic                atomic.Bool
	channelService                  *ChannelService
	resolver                        *ModelPricingResolver
	compositeResolver               *CompositeRouteResolver
	debugGatewayBodyFile            atomic.Pointer[os.File] // non-nil when SUB2API_DEBUG_GATEWAY_BODY is set
	tlsFPProfileService             *TLSFingerprintProfileService
	balanceNotifyService            *BalanceNotifyService
	userPlatformQuotaRepo           UserPlatformQuotaRepository
	modelCatalog                    gatewayServiceModelCatalog
	// billingModelDriftSeen 给漂移告警做 (准入模型, 结算模型) 去重，零值即可用。
	billingModelDriftSeen sync.Map
}

// NewGatewayService creates a new GatewayService
func NewGatewayService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	identityService *IdentityService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	claudeTokenProvider *ClaudeTokenProvider,
	sessionLimitCache SessionLimitCache,
	rpmCache RPMCache,
	digestStore *DigestSessionStore,
	settingService *SettingService,
	tlsFPProfileService *TLSFingerprintProfileService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
	compositeResolver *CompositeRouteResolver,
	balanceNotifyService *BalanceNotifyService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	catalog *ModelCatalogService,
) *GatewayService {
	userGroupRateTTL := resolveUserGroupRateCacheTTL(cfg)
	modelsListTTL := resolveModelsListCacheTTL(cfg)

	var modelCatalog gatewayServiceModelCatalog
	if catalog != nil {
		modelCatalog = catalog
	}

	svc := &GatewayService{
		accountRepo:           accountRepo,
		groupRepo:             groupRepo,
		usageLogRepo:          usageLogRepo,
		usageBillingRepo:      usageBillingRepo,
		userRepo:              userRepo,
		userSubRepo:           userSubRepo,
		userGroupRateRepo:     userGroupRateRepo,
		cache:                 cache,
		digestStore:           digestStore,
		cfg:                   cfg,
		schedulerSnapshot:     schedulerSnapshot,
		concurrencyService:    concurrencyService,
		billingService:        billingService,
		pricingGuardRequired:  true,
		rateLimitService:      rateLimitService,
		billingCacheService:   billingCacheService,
		identityService:       identityService,
		httpUpstream:          httpUpstream,
		deferredService:       deferredService,
		claudeTokenProvider:   claudeTokenProvider,
		sessionLimitCache:     sessionLimitCache,
		rpmCache:              rpmCache,
		userGroupRateCache:    gocache.New(userGroupRateTTL, time.Minute),
		settingService:        settingService,
		modelsListCache:       gocache.New(modelsListTTL, time.Minute),
		modelsListCacheTTL:    modelsListTTL,
		responseHeaderFilter:  compileResponseHeaderFilter(cfg),
		tlsFPProfileService:   tlsFPProfileService,
		channelService:        channelService,
		resolver:              resolver,
		compositeResolver:     compositeResolver,
		balanceNotifyService:  balanceNotifyService,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
		modelCatalog:          modelCatalog,
	}
	svc.userGroupRateResolver = newUserGroupRateResolver(
		userGroupRateRepo,
		svc.userGroupRateCache,
		userGroupRateTTL,
		&svc.userGroupRateSF,
		"service.gateway",
	)
	svc.debugModelRouting.Store(parseDebugEnvBool(os.Getenv("SUB2API_DEBUG_MODEL_ROUTING")))
	svc.debugClaudeMimic.Store(parseDebugEnvBool(os.Getenv("SUB2API_DEBUG_CLAUDE_MIMIC")))
	if path := strings.TrimSpace(os.Getenv(debugGatewayBodyEnv)); path != "" {
		svc.initDebugGatewayBodyFile(path)
	}
	return svc
}

// GenerateSessionHash 从预解析请求计算粘性会话 hash
func (s *GatewayService) GenerateSessionHash(parsed *ParsedRequest) string {
	if parsed == nil {
		return ""
	}

	// 1. 最高优先级：从 metadata.user_id 提取 session_xxx
	if parsed.MetadataUserID != "" {
		uid := ParseMetadataUserID(parsed.MetadataUserID)
		if uid != nil && uid.SessionID != "" {
			slog.Info("sticky.hash_source",
				"source", "metadata_user_id",
				"session_id", uid.SessionID,
				"device_id", uid.DeviceID,
				"is_new_format", uid.IsNewFormat,
			)
			return uid.SessionID
		}
		slog.Info("sticky.hash_metadata_parse_failed",
			"metadata_user_id", parsed.MetadataUserID,
			"parsed_nil", uid == nil,
		)
	}

	// 2. 提取带 cache_control: {type: "ephemeral"} 的内容
	cacheableContent := s.extractCacheableContent(parsed)
	if cacheableContent != "" {
		hash := s.hashContent(cacheableContent)
		slog.Info("sticky.hash_source",
			"source", "cacheable_content",
			"hash", hash,
		)
		return hash
	}

	// 3. 最后 fallback: 使用 session上下文 + system + 所有消息的完整摘要串
	var combined strings.Builder
	// 混入请求上下文区分因子，避免不同用户相同消息产生相同 hash
	if parsed.SessionContext != nil {
		_, _ = combined.WriteString(parsed.SessionContext.ClientIP)
		_, _ = combined.WriteString(":")
		_, _ = combined.WriteString(NormalizeSessionUserAgent(parsed.SessionContext.UserAgent))
		_, _ = combined.WriteString(":")
		_, _ = combined.WriteString(strconv.FormatInt(parsed.SessionContext.APIKeyID, 10))
		_, _ = combined.WriteString("|")
	}
	if systemText := extractTextFromSystemRaw(parsed.SystemRaw()); systemText != "" {
		_, _ = combined.WriteString(systemText)
	}
	contentStart := combined.Len()
	appendMessageTextsFromRaw(&combined, parsed.MessagesRaw())
	if combined.Len() == contentStart {
		appendResponsesSessionAnchorFromRaw(&combined, parsed.InputRaw())
	}
	if combined.Len() > 0 {
		hash := s.hashContent(combined.String())
		slog.Info("sticky.hash_source",
			"source", "message_content_fallback",
			"hash", hash,
			"content_len", combined.Len(),
		)
		return hash
	}

	return ""
}

// BindStickySession sets session -> account binding with standard TTL.
func (s *GatewayService) BindStickySession(ctx context.Context, groupID *int64, sessionHash string, accountID int64) error {
	if sessionHash == "" || accountID <= 0 || s.cache == nil {
		return nil
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), sessionHash, accountID, stickySessionTTL)
}

// GetCachedSessionAccountID retrieves the account ID bound to a sticky session.
// Returns 0 if no binding exists or on error.
func (s *GatewayService) GetCachedSessionAccountID(ctx context.Context, groupID *int64, sessionHash string) (int64, error) {
	if sessionHash == "" || s.cache == nil {
		return 0, nil
	}
	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), sessionHash)
	if err != nil {
		return 0, err
	}
	return accountID, nil
}

// FindGeminiSession 查找 Gemini 会话（基于内容摘要链的 Fallback 匹配）
// 返回最长匹配的会话信息（uuid, accountID）
func (s *GatewayService) FindGeminiSession(_ context.Context, groupID int64, prefixHash, digestChain string) (uuid string, accountID int64, matchedChain string, found bool) {
	if digestChain == "" || s.digestStore == nil {
		return "", 0, "", false
	}
	return s.digestStore.Find(groupID, prefixHash, digestChain)
}

// SaveGeminiSession 保存 Gemini 会话。oldDigestChain 为 Find 返回的 matchedChain，用于删旧 key。
func (s *GatewayService) SaveGeminiSession(_ context.Context, groupID int64, prefixHash, digestChain, uuid string, accountID int64, oldDigestChain string) error {
	if digestChain == "" || s.digestStore == nil {
		return nil
	}
	s.digestStore.Save(groupID, prefixHash, digestChain, uuid, accountID, oldDigestChain)
	return nil
}

// FindAnthropicSession 查找 Anthropic 会话（基于内容摘要链的 Fallback 匹配）
func (s *GatewayService) FindAnthropicSession(_ context.Context, groupID int64, prefixHash, digestChain string) (uuid string, accountID int64, matchedChain string, found bool) {
	if digestChain == "" || s.digestStore == nil {
		return "", 0, "", false
	}
	return s.digestStore.Find(groupID, prefixHash, digestChain)
}

// SaveAnthropicSession 保存 Anthropic 会话
func (s *GatewayService) SaveAnthropicSession(_ context.Context, groupID int64, prefixHash, digestChain, uuid string, accountID int64, oldDigestChain string) error {
	if digestChain == "" || s.digestStore == nil {
		return nil
	}
	s.digestStore.Save(groupID, prefixHash, digestChain, uuid, accountID, oldDigestChain)
	return nil
}

func (s *GatewayService) extractCacheableContent(parsed *ParsedRequest) string {
	if parsed == nil {
		return ""
	}

	systemText := extractCacheableTextFromSystemRaw(parsed.SystemRaw())
	if messageText := extractCacheableTextFromMessagesRaw(parsed.MessagesRaw()); messageText != "" {
		return messageText
	}
	return systemText
}

func parseRawJSONView(raw []byte) gjson.Result {
	if len(raw) == 0 {
		return gjson.Result{}
	}
	// 这里只做同步只读解析，避免 gjson.ParseBytes 为大 messages/contents 复制整段 raw。
	return gjson.Parse(*(*string)(unsafe.Pointer(&raw)))
}

func extractTextFromSystemRaw(raw []byte) string {
	system := parseRawJSONView(raw)
	switch system.Type {
	case gjson.String:
		return system.String()
	case gjson.JSON:
		if !system.IsArray() {
			return ""
		}
		var builder strings.Builder
		system.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
			return true
		})
		return builder.String()
	}
	return ""
}

func extractTextFromContentRaw(content gjson.Result) string {
	switch content.Type {
	case gjson.String:
		return content.String()
	case gjson.JSON:
		if !content.IsArray() {
			return ""
		}
		var builder strings.Builder
		content.ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() == "text" {
				if text := part.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
			}
			return true
		})
		return builder.String()
	}
	return ""
}

func appendMessageTextsFromRaw(builder *strings.Builder, raw []byte) {
	if builder == nil || len(raw) == 0 {
		return
	}
	messages := parseRawJSONView(raw)
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, msg gjson.Result) bool {
		if content := msg.Get("content"); content.Exists() {
			_, _ = builder.WriteString(extractTextFromContentRaw(content))
			return true
		}
		parts := msg.Get("parts")
		if parts.IsArray() {
			parts.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
				return true
			})
		}
		return true
	})
}

func appendResponsesSessionAnchorFromRaw(builder *strings.Builder, raw []byte) {
	if builder == nil || len(raw) == 0 {
		return
	}
	input := parseRawJSONView(raw)
	if input.Type == gjson.String {
		_, _ = builder.WriteString(input.String())
		return
	}
	if !input.IsArray() {
		return
	}

	input.ForEach(func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			_, _ = builder.WriteString(item.String())
			return false
		}

		switch item.Get("role").String() {
		case "system", "developer":
			appendResponsesContentText(builder, item.Get("content"))
		case "user":
			appendResponsesContentText(builder, item.Get("content"))
			return false
		default:
			if item.Get("type").String() == "input_text" {
				if text := item.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
				return false
			}
		}
		return true
	})
}

func appendResponsesContentText(builder *strings.Builder, content gjson.Result) {
	if builder == nil || !content.Exists() {
		return
	}
	if content.Type == gjson.String {
		_, _ = builder.WriteString(content.String())
		return
	}
	if !content.IsArray() {
		return
	}
	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "input_text", "text":
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
		}
		return true
	})
}

func extractCacheableTextFromSystemRaw(raw []byte) string {
	system := parseRawJSONView(raw)
	if !system.IsArray() {
		return ""
	}
	var builder strings.Builder
	system.ForEach(func(_, part gjson.Result) bool {
		if part.Get("cache_control.type").String() == "ephemeral" {
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
		}
		return true
	})
	return builder.String()
}

func extractCacheableTextFromMessagesRaw(raw []byte) string {
	messages := parseRawJSONView(raw)
	if !messages.IsArray() {
		return ""
	}
	var text string
	messages.ForEach(func(_, msg gjson.Result) bool {
		content := msg.Get("content")
		if !content.IsArray() {
			return true
		}
		found := false
		content.ForEach(func(_, part gjson.Result) bool {
			if part.Get("cache_control.type").String() == "ephemeral" {
				found = true
				return false
			}
			return true
		})
		if found {
			text = extractTextFromContentRaw(content)
			return false
		}
		return true
	})
	return text
}

func (s *GatewayService) hashContent(content string) string {
	h := xxhash.Sum64String(content)
	return strconv.FormatUint(h, 36)
}

// replaceModelInBody 替换请求体中的model字段
// 优先使用定点修改，尽量保持客户端原始字段顺序。
// sanitizeSystemText rewrites only the fixed OpenCode identity sentence (if present).
// We intentionally avoid broad keyword replacement in system prompts to prevent
// accidentally changing user-provided instructions.
// applyClaudeCodeOAuthMimicryToBody 将"非 Claude Code 客户端 + Claude OAuth 账号"
// 路径上原本只在 /v1/messages 里做的完整伪装应用到任意 body 上。
//
// 这是 /v1/messages 主路径上 rewriteSystemForNonClaudeCode +
// normalizeClaudeOAuthRequestBody 流程的通用版，供 OpenAI 协议兼容层
// (ForwardAsChatCompletions / ForwardAsResponses) 复用。
//
// 未抽离之前，OpenAI 协议兼容层仅做 injectClaudeCodePrompt（前置追加），
// 而仓内 /v1/messages 路径自己的注释明确说过"仅前置追加无法通过 Anthropic
// 第三方检测"；那条注释就是本函数存在的根因。
//
// 参数：
//   - ctx / c：用于读取指纹和 gateway settings；c 可为 nil（如 count_tokens）。
//   - account：必须是 OAuth 账号，且调用方已判断不是 Claude Code 客户端。
//   - body：已经 marshal 成 Anthropic /v1/messages 格式的请求体。
//   - systemRaw：body 中原始 system 字段（用于判断是否需要 rewrite）。
//   - model：最终会发给上游的模型 ID（用于 haiku 旁路 + metadata 版本选择）。
//
// 返回：改写后的 body。即使中间任何一步失败，也会退化成原 body（不会 panic）。
// buildOAuthMetadataUserIDFromBody 是 buildOAuthMetadataUserID 的变体，
// 适用于调用方手上没有 ParsedRequest 的场景（如 OpenAI 协议兼容层）。
//
// 与 buildOAuthMetadataUserID 的唯一区别：
//   - session hash 从 body 本体按同样规则重算，而不是读取 ParsedRequest 缓存值。
//   - 如果 body 里已经存在 metadata.user_id，则返回空（由 ensureClaudeOAuthMetadataUserID
//     自行决定是否覆盖）。
//
// buildStableSessionSeed 为伪装路径合成的 metadata.user_id session_id 生成"会话级稳定"种子。
//
// 真实 Claude Code 的 session_id 是进程级随机 UUID，在一段会话内跨请求保持不变。无状态代理
// 无法恢复该值，这里用"会话内不变的锚点"近似：账号 ID + 客户端区分因子 + 首条 user 消息文本。
// 对话在尾部追加 messages 时这三者都不变，因此 generateSessionUUID(seed) 跨轮稳定。
//
// 注意：粘性路由键 GenerateSessionHash 按设计逐轮变化（见其测试），本函数与之独立、互不影响。
// accountID 恒存在，故 seed 永不为空 —— 输出始终是确定性 UUID，而非随机值。
// sessionContextDiscriminator 把请求上下文（客户端 IP / 归一化 UA / API Key ID）拼成
// 一个跨客户端的区分因子，避免不同用户的相同首条消息派生出相同 session_id。
// GenerateSessionUUID creates a deterministic UUID4 from a seed string.
// SelectAccount 选择账号（粘性会话+优先级）
// SelectAccountForModel 选择支持指定模型的账号（粘性会话+优先级+模型映射）
// SelectAccountForModelWithExclusions selects an account supporting the requested model while excluding specified accounts.
// SelectAccountWithLoadAwareness selects account with load-awareness and wait plan.
// metadataUserID: 用于客户端亲和调度，从中提取客户端 ID
// sub2apiUserID: 系统用户 ID，用于二维亲和调度
// checkClaudeCodeRestriction 检查分组的 Claude Code 客户端限制
// 如果分组启用了 claude_code_only 且请求不是来自 Claude Code 客户端：
//   - 有降级分组：返回降级分组的 ID
//   - 无降级分组：返回 ErrClaudeCodeOnly 错误
//
// IsSingleAntigravityAccountGroup 检查指定分组是否只有一个 antigravity 平台的可调度账号。
// 用于 Handler 层在首次请求时提前设置 SingleAccountRetry context，
// 避免单账号分组收到 503 时错误地设置模型限流标记导致后续请求连续快速失败。
// isAccountInGroup checks if the account belongs to the specified group.
// When groupID is nil, returns true only for ungrouped accounts (no group assignments).
// isAccountSchedulableForQuota 检查账号是否在配额限制内
// 适用于配置了 quota_limit 的 apikey 和 bedrock 类型账号
// isAccountSchedulableForWindowCost 检查账号是否可根据窗口费用进行调度
// 仅适用于 Anthropic OAuth/SetupToken 账号
// 返回 true 表示可调度，false 表示不可调度
// rpmPrefetchContextKey is the context key for prefetched RPM counts.
// withRPMPrefetch 批量预取所有候选账号的 RPM 计数
// isAccountSchedulableForRPM 检查账号是否可根据 RPM 进行调度
// 仅适用于 Anthropic OAuth/SetupToken 账号
// IncrementAccountRPM increments the RPM counter for the given account.
// 已知 TOCTOU 竞态：调度时读取 RPM 计数与此处递增之间存在时间窗口，
// 高并发下可能短暂超出 RPM 限制。这是与 WindowCost 一致的 soft-limit
// 设计权衡——可接受的少量超额优于加锁带来的延迟和复杂度。
// checkAndRegisterSession 检查并注册会话，用于会话数量限制
// 仅适用于 Anthropic OAuth/SetupToken 账号
// sessionID: 会话标识符（使用粘性会话的 hash）
// 返回 true 表示允许（在限制内或会话已存在），false 表示拒绝（超出限制且是新会话）
// filterByMinPriority 过滤出优先级最小的账号集合
// filterByMinLoadRate 过滤出负载率最低的账号集合
// filterBySoonestReset 过滤出「会话窗口最早重置」的账号集合（use-it-or-lose-it）。
// 仅保留拥有未来重置时间（SessionWindowEnd 在当前时间之后）且最早的账号；
// 窗口为空或已过期的账号视为无活跃窗口、优先级最低。
// 当所有账号都没有活跃窗口时，返回原集合（不改变后续 LRU 选择）。
// selectByLRU 从集合中选择最久未用的账号
// 如果有多个账号具有相同的最小 LastUsedAt，则随机选择一个
// shuffleWithinSortGroups 对排序后的 accountWithLoad 切片，按 (Priority, LoadRate, LastUsedAt) 分组后组内随机打乱。
// 防止并发请求读取同一快照时，确定性排序导致所有请求命中相同账号。
// sameAccountWithLoadGroup 判断两个 accountWithLoad 是否属于同一排序组
// shuffleWithinPriorityAndLastUsed 对排序后的 []*Account 切片，按 (Priority, LastUsedAt) 分组后组内随机打乱。
//
// 注意：当 preferOAuth=true 时，需要保证 OAuth 账号在同组内仍然优先，否则会把排序时的偏好打散掉。
// 因此这里采用"组内分区 + 分区内 shuffle"的方式：
// - 先把同组账号按 (OAuth / 非 OAuth) 拆成两段，保持 OAuth 段在前；
// - 再分别在各段内随机打散，避免热点。
// sameAccountGroup 判断两个 Account 是否属于同一排序组（Priority + LastUsedAt）
// sameLastUsedAt 判断两个 LastUsedAt 是否相同（精度到秒）
// sortCandidatesForFallback 根据配置选择排序策略
// mode: "last_used"(按最后使用时间) 或 "random"(随机)
// sortAccountsByPriorityOnly 仅按优先级排序
// shuffleWithinPriority 在同优先级内随机打乱顺序
// selectAccountForModelWithPlatform 选择单平台账户（完全隔离）
// selectAccountWithMixedScheduling 选择账户（支持混合调度）
// 查询原生平台账户 + 启用 mixed_scheduling 的 antigravity 账户
// isModelSupportedByAccountWithContext 根据账户平台检查模型支持（带 context）
// 对于 Antigravity 平台，会先获取映射后的最终模型名（包括 thinking 后缀）再检查支持
// isModelSupportedByAccount 根据账户平台检查模型支持（无 context，用于非 Antigravity 平台）
// GetAccessToken 获取账号凭证
func (s *GatewayService) GetAccessToken(ctx context.Context, account *Account) (string, string, error) {
	switch account.Type {
	case AccountTypeOAuth, AccountTypeSetupToken:
		// Both oauth and setup-token use OAuth token flow
		return s.getOAuthToken(ctx, account)
	case AccountTypeAPIKey:
		apiKey := account.GetCredential("api_key")
		if apiKey == "" {
			return "", "", errors.New("api_key not found in credentials")
		}
		return apiKey, "apikey", nil
	case AccountTypeBedrock:
		return "", "bedrock", nil // Bedrock 使用 SigV4 签名或 API Key，由 forwardBedrock 处理
	case AccountTypeServiceAccount:
		if account.Platform != PlatformAnthropic {
			return "", "", fmt.Errorf("unsupported service account platform: %s", account.Platform)
		}
		if s.claudeTokenProvider == nil {
			return "", "", errors.New("claude token provider not configured")
		}
		accessToken, err := s.claudeTokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return "", "", err
		}
		return accessToken, "service_account", nil
	default:
		return "", "", fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func (s *GatewayService) getOAuthToken(ctx context.Context, account *Account) (string, string, error) {
	// 对于 Anthropic OAuth 账号，使用 ClaudeTokenProvider 获取缓存的 token
	if account.Platform == PlatformAnthropic && account.Type == AccountTypeOAuth && s.claudeTokenProvider != nil {
		accessToken, err := s.claudeTokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return "", "", err
		}
		return accessToken, "oauth", nil
	}

	// 其他情况（Gemini 有自己的 TokenProvider，setup-token 类型等）直接从账号读取
	accessToken := account.GetCredential("access_token")
	if accessToken == "" {
		return "", "", errors.New("access_token not found in credentials")
	}
	// Token刷新由后台 TokenRefreshService 处理，此处只返回当前token
	return accessToken, "oauth", nil
}

// 重试相关常量
// shouldFailoverUpstreamError determines whether an upstream error should trigger account failover.
// isClaudeCodeClient 判断请求是否来自真正的 Claude Code 客户端。
// 判定条件：
//  1. User-Agent 匹配 claude-cli/X.Y.Z（大小写不敏感）
//  2. metadata.user_id 符合 Claude Code 格式（legacy 或 JSON 格式）
//
// 只检查 metadata.user_id 非空不够严格：第三方工具（opencode 等）可能伪造 UA
// 并附带任意 metadata.user_id 字符串，从而绕过 mimicry。必须通过 ParseMetadataUserID
// 验证格式才能确认是真正的 Claude Code 客户端。
// normalizeSystemParam 将 json.RawMessage 类型的 system 参数转为标准 Go 类型（string / []any / nil），
// 避免 type switch 中 json.RawMessage（底层 []byte）无法匹配 case string / case []any / case nil 的问题。
// 这是 Go 的 typed nil 陷阱：(json.RawMessage, nil) ≠ (nil, nil)。
// systemIncludesClaudeCodePrompt 检查 system 中是否已包含 Claude Code 提示词
// 使用前缀匹配支持多种变体（标准版、Agent SDK 版等）
// hasClaudeCodePrefix 检查文本是否以 Claude Code 提示词的特征前缀开头
// injectClaudeCodePrompt 在 system 开头注入 Claude Code 提示词
// 处理 null、字符串、数组三种格式
// rewriteSystemForNonClaudeCode 将非 Claude Code 客户端的 system prompt 迁移至 messages，
// system 字段仅保留 Claude Code 标识提示词。
// Anthropic 基于 system 参数内容检测第三方应用，仅前置追加 Claude Code 提示词
// 无法通过检测，因为后续内容仍为非 Claude Code 格式。
// 策略：将原始 system prompt 提取并注入为 user/assistant 消息对，system 仅保留 Claude Code 标识。
// enforceCacheControlLimit 强制执行 cache_control 块数量限制（最多 4 个）
// 超限时优先移除工具断点，再移除 messages 断点，最后才移除 system 断点。
// injectAnthropicCacheControlTTL1h 将已有 ephemeral cache_control 块的 ttl 强制写为 1h。
// 仅修改已经存在的 cache_control，不新增缓存断点。
// shouldNormalizeClientDateline reports whether the request body's client
// dateline should be normalized before forwarding to Anthropic. The switch is
// scoped to Anthropic OAuth/SetupToken accounts only; API-Key accounts and
// non-Anthropic platforms bypass this step entirely.
// normalizeClientDatelineIfEnabled applies dateline normalization to body when
// the switch is on and the account qualifies. Returns (nextBody, true) only
// when the body actually changed; otherwise returns (nil, false) so callers
// can skip the writeback.
// Forward 转发请求到Claude API
// ApplyBedrockCCCompat 应用 Bedrock CC 兼容转换（渠道级模型映射后调用）
// 清理 body 中 Anthropic API 专有字段、修复 thinking/tool_use ID、过滤 beta token，
// 同时过滤 HTTP header 中的 anthropic-beta（防止 Passthrough 路径透传不支持的 token）。
// isBedrockCCCompatEnabled 检查渠道是否启用了 Bedrock CC 兼容模式
// forwardBedrock 转发请求到 AWS Bedrock
// executeBedrockUpstream 执行 Bedrock 上游请求（含重试逻辑）
// handleBedrockUpstreamErrors 处理 Bedrock 上游 4xx/5xx 错误（failover + 错误响应）
// buildUpstreamRequestBedrock 构建 Bedrock 上游请求
// buildUpstreamRequestBedrockAPIKey 构建 Bedrock API Key (Bearer Token) 上游请求
// handleBedrockNonStreamingResponse 处理 Bedrock 非流式响应
// Bedrock InvokeModel 非流式响应的 body 格式与 Claude API 兼容
// vertexSupportedBetaTokens 是 Vertex AI 的 Anthropic 端点接受的 anthropic-beta
// 白名单。Vertex 对任何未知 token 直接 HTTP 400，故采用白名单（与 Bedrock 的
// bedrockSupportedBetaTokens 同思路）而非黑名单：未来 Claude Code 新增的、Vertex 尚未
// 支持的 token 天然被剥离。当 Vertex 新增支持某 beta 时在此补充。
//
// 明确排除（issue #3358 中 Vertex 报 400 的 token）：advisor-tool-2026-03-01、
// prompt-caching-scope-2026-01-05、redact-thinking-2026-02-12、
// thinking-token-count-2026-05-13；以及 claude-code-20250219 / oauth-2025-04-20 等
// 客户端身份 beta——Vertex service_account 走 Bearer 鉴权，不需要它们。
// filterVertexBetaTokens 解析 client 的 anthropic-beta header，先剔除 drop 集合中的
// token（BetaPolicy filter + 默认 drop），再只保留 Vertex 支持的 token，去重后逗号拼接。
// 返回最终 header（可能为空字符串）。
// getBetaHeader 处理anthropic-beta header
// 对于OAuth账号，需要确保包含oauth-2025-04-20
// computeFinalAnthropicBeta 计算发往上游的最终 anthropic-beta header 值。
//
// 设计动机：将原本在 buildUpstreamRequest 内联在一起、依赖 req.Header 的
// anthropic-beta 计算逻辑抽成纯函数。这样调用方可以在 NewRequest 之前
// 就提前拿到最终 beta header，进而能按它对 body 做能力维度 sanitize 后再做
// CCH 签名——一举修复了以下之前由顺序依赖导致的能力维度 sanitize
// 无法部署的问题（签名与最终 body 不一致可以被判 third-party）。
//
// 返回 (value, shouldSet)：
//   - shouldSet=false 意为“不主动设置 anthropic-beta header”，与原代码“
//     API-key 账号 + 客户端未传 anthropic-beta + InjectBetaForAPIKey 未开启或
//     requestNeedsBetaFeatures=false”的行为对齐。
//   - shouldSet=true 时 value 可能为空字符串（例如客户端透传的 beta 被 dropSet
//     全部过滤掉），这与原代码中 setHeaderRaw 的结果一致。
//
// clientHeaders 是客户端原始 HTTP header（通常为 c.Request.Header）；nil 时按“客户端
// 未传”处理。body 是已经 metadata 重写 / billing version sync 之后但未 sanitize 上游
// 不兼容字段之前的版本。
// computeFinalCountTokensAnthropicBeta 是 count_tokens 路径上 anthropic-beta header 的
// 计算纯函数。语义与 computeFinalAnthropicBeta 对齐，但备份了 count_tokens 独有的
// 两条特殊规则：
//
//   - OAuth mimic：requiredBetas 为 FullClaudeCodeMimicryBetas + BetaTokenCounting
//     （与 messages 不同的是：不按 haiku 排除；count_tokens 始终携带 token-counting beta）
//   - OAuth 透传 + 客户端未传 anthropic-beta：补齐 CountTokensBetaHeader
//   - OAuth 透传 + 客户端传了：补齐 BetaTokenCounting（如果未含）
//
// 返回语义同 computeFinalAnthropicBeta。
// stripBetaTokens removes the given beta tokens from a comma-separated header value.
// BetaBlockedError indicates a request was blocked by a beta policy rule.
// betaPolicyResult holds the evaluated result of beta policy rules for a single request.
// evaluateBetaPolicy loads settings once and evaluates all rules against the given request.
// mergeDropSets merges the static defaultDroppedBetasSet with dynamic policy filter tokens.
// Returns defaultDroppedBetasSet directly when policySet is empty (zero allocation).
// betaPolicyFilterSetKey is the gin.Context key for caching the policy filter set within a request.
// getBetaPolicyFilterSet returns the beta policy filter set, using the gin context cache if available.
// In the /v1/messages path, Forward() evaluates the policy first and caches the result;
// buildUpstreamRequest reuses it (zero extra DB calls). In the count_tokens path, this
// evaluates on demand (one DB call).
// betaPolicyScopeMatches checks whether a rule's scope matches the current account type.
// matchModelWhitelist checks if a model matches any pattern in the whitelist.
// Reuses matchModelPattern from group.go which supports exact and wildcard prefix matching.
// resolveRuleAction determines the effective action and error message for a rule given the request model.
// When ModelWhitelist is empty, the rule's primary Action/ErrorMessage applies unconditionally.
// When non-empty, Action applies to matching models; FallbackAction/FallbackErrorMessage applies to others.
// droppedBetaSet returns claude.DroppedBetas as a set, with optional extra tokens.
// containsBetaToken checks if a comma-separated header value contains the given token.
// checkBetaPolicyBlockForTokens 检查 token 列表中是否有被管理员 block 规则命中的 token。
// 用于补充 evaluateBetaPolicy 对 header 的检查，覆盖 body 自动注入的 token。
// applyClaudeCodeMimicHeaders forces "Claude Code-like" request headers.
// This mirrors opencode-anthropic-auth behavior: do not trust downstream
// headers when using Claude Code-scoped OAuth credentials.
// shouldRectifySignatureError 统一判断是否应触发签名整流（strip thinking blocks 并重试）。
// 根据账号类型检查对应的开关和匹配模式。
//
// mappedModel 用于按 thinking 协议族分流：passback-required (DeepSeek/Kimi/GLM 等) 上游
// 的 400 不是签名缺失问题，retry 任何 thinking 变形都会破坏「原样回传」契约——直接透传
// 错误给客户端。详见 thinking_protocol.go。
// isSignatureErrorPattern 仅做模式匹配，不检查开关。
// 用于已进入重试流程后的二阶段检测（此时开关已在首次调用时验证过）。
// matchSignaturePatterns 检查响应体是否匹配自定义关键词列表（不区分大小写）。
// isThinkingBlockSignatureError 检测是否是thinking block相关错误
// 这类错误可以通过过滤thinking blocks并重试来解决
// sanitizeStreamError 返回不含网络地址的客户端可见错误描述。
// 默认 (*net.OpError).Error() 会拼接 Source/Addr 字段，泄露内部 IP/端口与上游
// 服务器地址（例如 "read tcp 10.0.0.1:54321->52.1.2.3:443: read: connection
// reset by peer"）。该函数只保留可识别的错误类别，原始 err 仍在调用点写入日志。
// ExtractUpstreamErrorMessage 从上游响应体中提取错误消息
// 支持 Claude 风格的错误格式：{"type":"error","error":{"type":"...","message":"..."}}
// handleRetryExhaustedError 处理重试耗尽后的错误
// OAuth 403：标记账号异常
// API Key 未配置错误码：仅返回错误，不标记账号
// streamingResult 流式响应结果
// applyCacheTTLOverride 将所有 cache creation tokens 归入指定的 TTL 类型。
// target 为 "5m" 或 "1h"。返回 true 表示发生了变更。
// rewriteCacheCreationJSON 在 JSON usage 对象中重写 cache_creation 嵌套对象的 TTL 分类。
// usageObj 是 usage JSON 对象（map[string]any）。
// replaceModelInResponseBody 替换响应体中的model字段
// 使用 gjson/sjson 精确替换，避免全量 JSON 反序列化
// RecordUsageInput 记录使用量的输入参数。
// 异步 worker 只接收计费所需快照，不能持有 ParsedRequest/RequestBodyRef 这类大请求体引用。
// APIKeyQuotaUpdater defines the interface for updating API Key quota and rate limit usage
// postUsageBillingParams 统一扣费所需的参数
// PlatformFromAPIKey 从 APIKey 关联的 Group 推导 platform 名称。
// apiKey 为 nil 或 Group 信息缺失时返回空串（调用方据此 short-circuit quota 累加）。
// 导出供 handler 层调用。
// QuotaPlatform 返回 user×platform 配额计量使用的平台标识。
// 强制平台路由（如 /antigravity）优先按 ctx 中的 ForcePlatform 计量，否则回退到
// APIKey 关联 Group 的平台。
//
// 注意：必须用带 ForcePlatform 的请求 context 调用（如 handler 的 c.Request.Context()）。
// 后扣运行在 worker 池的 background ctx 上没有 ForcePlatform，因此后扣平台由 handler
// 预先算定、经 RecordUsageInput.QuotaPlatform 传入，不要在后扣链路用 worker ctx 调用本函数。
// postUsageBilling is the legacy fallback billing path used when the unified
// billing repo is unavailable (nil). Production uses applyUsageBilling → repo.Apply
// for atomic billing. This path only runs in tests or degraded mode.
// notifyBalanceLow sends balance low notification after deduction.
// When result.NewBalance is available (from DB transaction RETURNING), it is used directly
// to reconstruct oldBalance, avoiding stale Redis reads and concurrent-deduction races.
// resolveOldBalance returns the pre-deduction balance.
// Prefers the DB transaction result (newBalance + cost) over snapshot.
// notifyAccountQuota sends account quota threshold notification after increment.
// When result.QuotaState is available (from DB transaction RETURNING), it is passed directly
// to avoid a separate DB read that may see stale or concurrently-modified data.
// billingDeps 扣费逻辑依赖的服务（由各 gateway service 提供）
// recordUsageOpts 内部选项，参数化普通计费与长上下文计费的差异点。
// RecordUsage 记录使用量并扣费（或更新订阅用量）
// RecordUsageLongContextInput 记录使用量的输入参数（支持长上下文双倍计费）
// RecordUsageWithLongContext 记录使用量并扣费，支持长上下文双倍计费（用于 Gemini）
// recordUsageCoreInput 是 recordUsageCore 的公共输入字段，从两种输入结构体中提取。
// recordUsageCore 是 RecordUsage 和 RecordUsageWithLongContext 的统一实现。
// LongContextThreshold > 0 时 Token 计费回退走 CalculateCostWithLongContext。
func recordUsageBillingModel(result *ForwardResult, account *Account) string {
	if result == nil {
		return ""
	}
	if account != nil && account.IsMiniMax() {
		if upstreamModel := strings.TrimSpace(result.UpstreamModel); upstreamModel != "" {
			return upstreamModel
		}
	}
	return forwardResultBillingModel(result.Model, result.UpstreamModel)
}

// calculateRecordUsageCost 根据请求类型和选项计算费用。
// resolveChannelPricing 检查指定模型是否存在渠道级别定价。
// 返回非 nil 的 ResolvedPricing 表示有渠道定价，nil 表示走默认定价路径。
// calculateImageCost 计算图片生成费用：渠道级别定价优先，否则走按次计费。
// calculateTokenCost 计算 Token 计费：根据 opts 决定走普通/长上下文/渠道统一计费。
// buildRecordUsageLog 构建使用日志并设置计费模式。
// resolveBillingMode 根据计费结果和请求类型确定计费模式。
// ResolveChannelMapping 委托渠道服务解析模型映射
// ReplaceModelInBody 替换请求体中的模型名（导出供 handler 使用）
// IsModelRestricted 检查模型是否被渠道限制
// ResolveRequestChannelMapping 解析入站请求的渠道映射（groupID 可为空）。
// 不含模型限制检查——那一步由调度阶段的 checkChannelPricingRestriction 负责。
// checkChannelPricingRestriction 根据渠道计费基准检查模型是否受定价列表限制。
// 供调度阶段预检查（requested / channel_mapped）。
// upstream 需逐账号检查，此处返回 false。
// billingModelForRestriction 根据计费基准确定限制检查使用的模型。
// upstream 返回空（需逐账号检查）。
// isUpstreamModelRestrictedByChannel 检查账号映射后的上游模型是否受渠道定价限制。
// 仅在 BillingModelSource="upstream" 且 RestrictModels=true 时由调度循环调用。
// resolveAccountUpstreamModel 确定账号将请求模型映射为什么上游模型。
// needsUpstreamChannelRestrictionCheck 判断是否需要在调度循环中逐账号检查上游模型的渠道限制。
// isStickyAccountUpstreamRestricted 检查粘性会话命中的账号是否受 upstream 渠道限制。
// 合并 needsUpstreamChannelRestrictionCheck + isUpstreamModelRestrictedByChannel 两步调用，
// 供 sticky session 条件链使用，避免内联多个函数调用导致行过长。
// ForwardCountTokens 转发 count_tokens 请求到上游 API
// 特点：不记录使用量、仅支持非流式响应
// buildCountTokensRequest 构建 count_tokens 上游请求
// countTokensError 返回 count_tokens 错误响应
// buildCustomRelayURL 构建自定义中继转发 URL
// 在 path 后附加 beta=true 和可选的 proxy 查询参数
// GetAvailableModels delegates to the unified model catalog first. Only when the
// catalog is unavailable or returns an error does it defensively aggregate legacy
// model mappings and provider defaults from schedulable accounts.
func (s *GatewayService) GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string {
	if s == nil {
		return nil
	}
	if s.modelCatalog != nil {
		models, err := s.modelCatalog.ListForPlatform(ctx, groupID, platform, true)
		if err == nil {
			return models
		}
	}
	_, hasDomesticCapabilities := GetProviderGatewayCapabilities(platform)
	cacheKey := modelsListCacheKey(groupID, platform)
	if s.modelsListCache != nil {
		if cached, found := s.modelsListCache.Get(cacheKey); found {
			if models, ok := cached.([]string); ok {
				modelsListCacheHitTotal.Add(1)
				return cloneStringSlice(models)
			}
		}
	}
	modelsListCacheMissTotal.Add(1)

	var accounts []Account
	var err error

	if s.accountRepo == nil {
		if hasDomesticCapabilities {
			return s.cacheAvailableModels(cacheKey, NewGatewayModelListProvider(GatewayModelListOptions{
				IncludeAliases: includeDomesticModelAliasesInModels(s.cfg),
			}).ModelsForProvider(platform, nil))
		}
		return nil
	}

	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}

	if err != nil {
		if hasDomesticCapabilities {
			return NewGatewayModelListProvider(GatewayModelListOptions{
				IncludeAliases: includeDomesticModelAliasesInModels(s.cfg),
			}).ModelsForProvider(platform, nil)
		}
		return nil
	}

	if len(accounts) == 0 && !hasDomesticCapabilities {
		return nil
	}

	// Filter by platform if specified
	if platform != "" {
		filtered := make([]Account, 0)
		for _, acc := range accounts {
			if acc.Platform == platform {
				filtered = append(filtered, acc)
			}
		}
		accounts = filtered
	}

	if hasDomesticCapabilities {
		return s.cacheAvailableModels(cacheKey, NewGatewayModelListProvider(GatewayModelListOptions{
			IncludeAliases: includeDomesticModelAliasesInModels(s.cfg),
		}).ModelsForProvider(platform, accounts))
	}

	// Collect unique models from all accounts
	modelSet := make(map[string]struct{})
	hasAnyMapping := false

	for _, acc := range accounts {
		mapping := acc.GetModelMapping()
		if len(mapping) > 0 {
			hasAnyMapping = true
			for model := range mapping {
				modelSet[model] = struct{}{}
			}
		}
	}

	// If no account has model_mapping, return nil (use default)
	if !hasAnyMapping {
		if s.modelsListCache != nil {
			s.modelsListCache.Set(cacheKey, []string(nil), s.modelsListCacheTTL)
			modelsListCacheStoreTotal.Add(1)
		}
		return nil
	}

	// Convert to slice
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	if s.modelsListCache != nil {
		s.modelsListCache.Set(cacheKey, cloneStringSlice(models), s.modelsListCacheTTL)
		modelsListCacheStoreTotal.Add(1)
	}
	return cloneStringSlice(models)
}

func (s *GatewayService) cacheAvailableModels(cacheKey string, models []string) []string {
	if s != nil && s.modelsListCache != nil {
		s.modelsListCache.Set(cacheKey, cloneStringSlice(models), s.modelsListCacheTTL)
		modelsListCacheStoreTotal.Add(1)
	}
	return cloneStringSlice(models)
}

func includeDomesticModelAliasesInModels(cfg *config.Config) bool {
	return cfg != nil && cfg.Gateway.ModelAliases.Enabled && cfg.Gateway.ModelAliases.IncludeInModels
}

// GetSchedulablePlatforms returns the concrete platforms that currently have
// schedulable accounts in the target group.
func (s *GatewayService) GetSchedulablePlatforms(ctx context.Context, groupID *int64) map[string]struct{} {
	platforms := make(map[string]struct{})
	if s == nil || s.accountRepo == nil {
		return platforms
	}

	var accounts []Account
	var err error
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil {
		return platforms
	}

	for _, acc := range accounts {
		platform := strings.TrimSpace(acc.Platform)
		if platform != "" {
			platforms[platform] = struct{}{}
		}
	}
	return platforms
}

func (s *GatewayService) InvalidateAvailableModelsCache(groupID *int64, platform string) {
	if s == nil || s.modelsListCache == nil {
		return
	}

	normalizedPlatform := strings.TrimSpace(platform)
	// 完整匹配时精准失效；否则按维度批量失效。
	if groupID != nil && normalizedPlatform != "" {
		s.modelsListCache.Delete(modelsListCacheKey(groupID, normalizedPlatform))
		return
	}

	targetGroup := derefGroupID(groupID)
	for key := range s.modelsListCache.Items() {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		groupPart, parseErr := strconv.ParseInt(parts[0], 10, 64)
		if parseErr != nil {
			continue
		}
		if groupID != nil && groupPart != targetGroup {
			continue
		}
		if normalizedPlatform != "" && parts[1] != normalizedPlatform {
			continue
		}
		s.modelsListCache.Delete(key)
	}
}

const debugGatewayBodyDefaultFilename = "gateway_debug.log"

// initDebugGatewayBodyFile 初始化网关调试日志文件。
//
//   - "1"/"true" 等布尔值 → 当前目录下 gateway_debug.log
//   - 已有目录路径        → 该目录下 gateway_debug.log
//   - 其他               → 视为完整文件路径
func (s *GatewayService) initDebugGatewayBodyFile(path string) {
	if parseDebugEnvBool(path) {
		path = debugGatewayBodyDefaultFilename
	}

	// 如果 path 指向一个已存在的目录，自动追加默认文件名
	if info, err := os.Stat(path); err == nil && info.IsDir() { //nolint:gosec // G703: path 来自服务端调试配置，非用户输入
		path = filepath.Join(path, debugGatewayBodyDefaultFilename)
	}

	// 确保父目录存在
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil { //nolint:gosec // G703: path 来自服务端调试配置，非用户输入
			slog.Error("failed to create gateway debug log directory", "dir", dir, "error", err)
			return
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644) //nolint:gosec // G703: path 来自服务端调试配置，非用户输入
	if err != nil {
		slog.Error("failed to open gateway debug log file", "path", path, "error", err)
		return
	}
	s.debugGatewayBodyFile.Store(f)
	slog.Info("gateway debug logging enabled", "path", path)
}

// debugLogGatewaySnapshot 将网关请求的完整快照（headers + body）写入独立的调试日志文件，
// 用于对比客户端原始请求和上游转发请求。
//
// 启用方式（环境变量）：
//
//	SUB2API_DEBUG_GATEWAY_BODY=1                          # 写入 gateway_debug.log
//	SUB2API_DEBUG_GATEWAY_BODY=/tmp/gateway_debug.log     # 写入指定路径
//
// tag: "CLIENT_ORIGINAL" 或 "UPSTREAM_FORWARD"
func (s *GatewayService) debugLogGatewaySnapshot(tag string, headers http.Header, body []byte, extra map[string]string) {
	f := s.debugGatewayBodyFile.Load()
	if f == nil {
		return
	}

	var buf strings.Builder
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(&buf, "\n========== [%s] %s ==========\n", ts, tag)

	// 1. context
	if len(extra) > 0 {
		fmt.Fprint(&buf, "--- context ---\n")
		extraKeys := make([]string, 0, len(extra))
		for k := range extra {
			extraKeys = append(extraKeys, k)
		}
		sort.Strings(extraKeys)
		for _, k := range extraKeys {
			fmt.Fprintf(&buf, "  %s: %s\n", k, extra[k])
		}
	}

	// 2. headers（按真实 Claude CLI wire 顺序排列，便于与抓包对比；auth 脱敏）
	fmt.Fprint(&buf, "--- headers ---\n")
	for _, k := range sortHeadersByWireOrder(headers) {
		for _, v := range headers[k] {
			fmt.Fprintf(&buf, "  %s: %s\n", k, safeHeaderValueForLog(k, v))
		}
	}

	// 3. body（完整输出，格式化 JSON 便于 diff）
	fmt.Fprint(&buf, "--- body ---\n")
	if len(body) == 0 {
		fmt.Fprint(&buf, "  (empty)\n")
	} else {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "  ", "  ") == nil {
			fmt.Fprintf(&buf, "  %s\n", pretty.Bytes())
		} else {
			// JSON 格式化失败时原样输出
			fmt.Fprintf(&buf, "  %s\n", body)
		}
	}

	// 写入文件（调试用，并发写入可能交错但不影响可读性）
	_, _ = f.WriteString(buf.String())
}
