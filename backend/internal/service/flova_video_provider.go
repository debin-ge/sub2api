package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/google/uuid"
)

// FlovaVideoProvider 把 Flova（Agent 式视频创作平台）接成托管视频 provider：
//
//	Create = live 余额 B0 ≥ 任务预算 + 低水位 → 上传参考图 ×N → POST /create 建项目 → POST /chat
//	         （单镜头约束全部写在每轮指令里；skill_id 只在管理员配置时附带）
//	Get    = 只读 POST /run_result；预算内的高成本确认只做授权（写入执行状态），
//	         由 worker 在授权落库后调用 ResumeAuthorized 恰好一次（POST /chat is_resume）
//	Content= 直拉 CDN 签名 URL（必须带 flova-cli UA），403 时经 /resource_info 刷新
//
// 花费契约见 docs/Flova 成本约束设计.md：专用账号、单账号串行、预算内自动确认、终态后按
// 账号余额差结清供应商成本，超支熔断由平台承担。上游不回传可信用量，用户按冻结报价结算；
// 协议形状来自 CLI v0.0.10 抓包与 2026-10-07 实测，见《Flova 平台接入实施方案》第 5 节。
const (
	flovaDefaultBaseURL       = "https://service.flova.ai/api/agent/v2"
	flovaDefaultUploadURL     = "https://gcp.service.flova.ai/api/agent/v2/upload"
	flovaDefaultUserAgent     = "flova-cli/v0.0.10 (linux; amd64)"
	flovaDefaultClientVersion = "v0.0.10"

	flovaMaxJSONResponse  = 1 << 20
	flovaMaxErrorResponse = 64 << 10

	flovaDefaultPollInterval  = 10 * time.Second
	flovaBusyPollInterval     = 15 * time.Second
	flovaContentHeaderTimeout = 30 * time.Second
	flovaContentMaxRedirects  = 3
	flovaDefaultContentTTL    = 24 * time.Hour

	flovaDefaultMaxRunMinutes     = 60
	flovaDefaultMinCreditsReserve = 60.0
	flovaDefaultPriceDrift        = 0.10
	flovaAssistantTextLimit       = 500

	flovaCredentialAPIKey              = "api_key"
	flovaCredentialBaseURL             = "base_url"
	flovaCredentialUploadURL           = "upload_url"
	flovaCredentialEnvName             = "env_name"
	flovaCredentialSkillID             = "skill_id"
	flovaCredentialMaxRunMinutes       = "max_run_minutes"
	flovaCredentialPriceDriftTolerance = "price_drift_tolerance"
	flovaCredentialMinCreditsReserve   = "min_credits_reserve"
	flovaCredentialClientVersion       = "client_version"

	// accounts.extra 里的积分快照键（FlovaCreditsSyncService 写、SupportsAccount 读）。
	FlovaExtraCredits          = "flova_credits"
	FlovaExtraSubscription     = "flova_subscription"
	FlovaExtraCreditsCheckedAt = "flova_credits_checked_at"
	FlovaExtraEmptyStreak      = "flova_empty_streak"

	flovaActionHighCostConfirm      = "high_cost_model_confirm"
	flovaActionSubscriptionRequired = "subscription_required"
	flovaActionBatchTaskLimit       = "batch_task_limit"

	flovaErrorNoVideoArtifact    = "no_video_artifact"
	flovaErrorMembershipRequired = "membership_required"
	flovaErrorPriceDrift         = "price_drift"
	flovaErrorUserActionRequired = "user_action_required"
	flovaErrorRunTimeout         = "run_timeout"
)

type FlovaVideoProvider struct {
	httpUpstream HTTPUpstream
	tlsProfiles  *TLSFingerprintProfileService
	capabilities VideoCapabilities
	catalog      *VideoCapabilityCatalog
	cfg          config.GatewayFlovaConfig
	resolver     videoCallbackIPResolver
	redirect     videoContentRedirectExecutor
	now          func() time.Time
}

func NewFlovaVideoProvider(httpUpstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService, cfg config.GatewayFlovaConfig) *FlovaVideoProvider {
	return &FlovaVideoProvider{
		httpUpstream: httpUpstream, tlsProfiles: tlsProfiles, cfg: cfg,
		capabilities: DefaultFlovaVideoCapabilities(), resolver: net.DefaultResolver,
		redirect: executePinnedVideoContentRedirect, now: time.Now,
	}
}

func (p *FlovaVideoProvider) Name() string { return VideoProviderFlova }

func (p *FlovaVideoProvider) Capabilities() VideoCapabilities {
	if p == nil {
		return VideoCapabilities{}
	}
	if p.catalog != nil {
		if capabilities, ok := p.catalog.Capabilities(VideoProviderFlova); ok {
			return capabilities
		}
	}
	return cloneVideoCapabilities(p.capabilities)
}

// SupportsAccount：平台 / 类型 / Key 齐全，且积分快照（若有）不低于低水位。
// skill_id 可选：管理员配置了就随 /chat 带上，网关不创建 Skill，也不改写账号凭据。
// 快照过滤不发网络请求；提交前还会 live 查一次余额。
func (p *FlovaVideoProvider) SupportsAccount(account *Account) bool {
	if p == nil || account == nil {
		return false
	}
	if account.Platform != PlatformFlova || account.Type != AccountTypeAPIKey {
		return false
	}
	if p.apiKey(account) == "" {
		return false
	}
	if credits, ok := flovaExtraFloat(account, FlovaExtraCredits); ok && credits < p.minCreditsReserve(account) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 凭据与配置读取
// ---------------------------------------------------------------------------

func (p *FlovaVideoProvider) apiKey(account *Account) string {
	return strings.TrimSpace(account.GetCredential(flovaCredentialAPIKey))
}

func (p *FlovaVideoProvider) skillID(account *Account) string {
	return strings.TrimSpace(account.GetCredential(flovaCredentialSkillID))
}

func (p *FlovaVideoProvider) baseURL(account *Account) string {
	if v := strings.TrimSpace(account.GetCredential(flovaCredentialBaseURL)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return flovaDefaultBaseURL
}

func (p *FlovaVideoProvider) userAgent() string {
	if v := strings.TrimSpace(p.cfg.UserAgent); v != "" {
		return v
	}
	return flovaDefaultUserAgent
}

func (p *FlovaVideoProvider) maxRunDuration(account *Account) time.Duration {
	minutes := flovaCredentialFloat(account, flovaCredentialMaxRunMinutes, float64(p.cfg.MaxRunMinutesDefault))
	if minutes <= 0 {
		minutes = flovaDefaultMaxRunMinutes
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (p *FlovaVideoProvider) minCreditsReserve(account *Account) float64 {
	v := flovaCredentialFloat(account, flovaCredentialMinCreditsReserve, p.cfg.CreditsReserveDefault)
	if v < 0 {
		return flovaDefaultMinCreditsReserve
	}
	return v
}

func (p *FlovaVideoProvider) uploadURL(account *Account) string {
	if v := strings.TrimSpace(account.GetCredential(flovaCredentialUploadURL)); v != "" {
		return v
	}
	return flovaDefaultUploadURL
}

func (p *FlovaVideoProvider) clientVersion(account *Account) string {
	if v := strings.TrimSpace(account.GetCredential(flovaCredentialClientVersion)); v != "" {
		return v
	}
	if v := strings.TrimSpace(p.cfg.ClientVersion); v != "" {
		return v
	}
	return flovaDefaultClientVersion
}

// priceDriftTolerance 是任务预算相对价格表的余量（0.10 = 预算为名义积分的 1.1 倍）。
func (p *FlovaVideoProvider) priceDriftTolerance(account *Account) float64 {
	fallback := p.cfg.PriceDriftToleranceDefault
	if fallback <= 0 {
		fallback = flovaDefaultPriceDrift
	}
	v := flovaCredentialFloat(account, flovaCredentialPriceDriftTolerance, fallback)
	if v < 0 || v > 1 {
		return fallback
	}
	return v
}

// taskBudgetCredits 是任务预算：价格表名义积分 × (1 + 容差)。预算内的高成本确认自动放行，
// 终态余额差超过预算即熔断。
func (p *FlovaVideoProvider) taskBudgetCredits(account *Account, plan *flovaPlan) (float64, bool) {
	perSecond, ok := flovaCreditsPerSecond(plan.profile.ID, plan.resolution)
	if !ok || perSecond <= 0 || plan.seconds <= 0 {
		return 0, false
	}
	return perSecond * float64(plan.seconds) * (1 + p.priceDriftTolerance(account)), true
}

func flovaCredentialFloat(account *Account, key string, fallback float64) float64 {
	if account == nil || account.Credentials == nil {
		return fallback
	}
	v, ok := account.Credentials[key]
	if !ok || v == nil {
		return fallback
	}
	if f, ok := flovaToFloat(v); ok {
		return f
	}
	return fallback
}

func flovaExtraFloat(account *Account, key string) (float64, bool) {
	if account == nil || account.Extra == nil {
		return 0, false
	}
	v, ok := account.Extra[key]
	if !ok || v == nil {
		return 0, false
	}
	return flovaToFloat(v)
}

func flovaToFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(val), ",", ""), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// 任务标识：保留历史编码中的运行身份和冻结参数，后续 resource_id 保存于私有
// ProviderExecutionState，避免追加内容导致合法标识超过数据库长度上限。
//
//	<project_id>:<stream_chat_id>|m=<model key>;r=<resolution>;s=<seconds>;t=<unix>[;x=<resource_id>][;b=<B0>;c=<budget>]
//
// b / c 是带预算任务的提交前余额与任务预算（积分），创建时写定、之后不变，供预算内确认与
// 余额差对账使用；没有这两项的是 e8453248d 之前或期间的旧任务，花费只能人工对账。
// ---------------------------------------------------------------------------

type flovaTaskState struct {
	ProjectID    string
	StreamChatID string
	ModelKey     string
	Resolution   string
	Seconds      int
	SubmittedAt  time.Time
	ResourceID   string
	// BalanceBefore / BudgetCredits 仅在 budgeted() 时有意义。
	BalanceBefore float64
	BudgetCredits float64
	// ResumeActionKey / ResumedStreamChatID 来自私有执行状态，不进标识。
	ResumeActionKey     string
	ResumedStreamChatID string
}

// budgeted 报告任务是否在 serialized_balance_delta 契约下提交（带 B0 与预算）。
func (s flovaTaskState) budgeted() bool {
	return s.BudgetCredits > 0
}

func (s flovaTaskState) spendingPolicy() string {
	if s.budgeted() {
		return VideoSpendingSerializedBalanceDelta
	}
	return VideoSpendingBlockedUnverified
}

// activeStream 是当前应观测的 run：自动确认后为 resume 开出的新 run。
func (s flovaTaskState) activeStream() string {
	if s.ResumedStreamChatID != "" {
		return s.ResumedStreamChatID
	}
	return s.StreamChatID
}

func (s flovaTaskState) encode() string {
	var b strings.Builder
	_, _ = b.WriteString(s.ProjectID)
	_, _ = b.WriteString(":")
	_, _ = b.WriteString(s.StreamChatID)
	_, _ = b.WriteString("|m=")
	_, _ = b.WriteString(s.ModelKey)
	_, _ = b.WriteString(";r=")
	_, _ = b.WriteString(s.Resolution)
	_, _ = b.WriteString(";s=")
	_, _ = b.WriteString(strconv.Itoa(s.Seconds))
	_, _ = b.WriteString(";t=")
	_, _ = b.WriteString(strconv.FormatInt(s.SubmittedAt.Unix(), 10))
	if s.ResourceID != "" {
		_, _ = b.WriteString(";x=")
		_, _ = b.WriteString(s.ResourceID)
	}
	if s.budgeted() {
		_, _ = b.WriteString(";b=")
		_, _ = b.WriteString(strconv.FormatFloat(s.BalanceBefore, 'f', -1, 64))
		_, _ = b.WriteString(";c=")
		_, _ = b.WriteString(strconv.FormatFloat(s.BudgetCredits, 'f', -1, 64))
	}
	return b.String()
}

func (s flovaTaskState) model() string {
	return "flova/" + s.ModelKey
}

func parseFlovaTaskID(value string) (flovaTaskState, error) {
	var state flovaTaskState
	value = strings.TrimSpace(value)
	head, tail, _ := strings.Cut(value, "|")
	project, stream, ok := strings.Cut(head, ":")
	if !ok || project == "" || stream == "" {
		return state, fmt.Errorf("flova: malformed task id")
	}
	state.ProjectID, state.StreamChatID = project, stream
	for _, kv := range strings.Split(tail, ";") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "m":
			state.ModelKey = v
		case "r":
			state.Resolution = v
		case "s":
			state.Seconds, _ = strconv.Atoi(v)
		case "t":
			if unix, err := strconv.ParseInt(v, 10, 64); err == nil && unix > 0 {
				state.SubmittedAt = time.Unix(unix, 0).UTC()
			}
		case "x":
			state.ResourceID = v
		case "b", "c":
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil || !finiteNonNegative(parsed) || parsed > 1e12 {
				return flovaTaskState{}, fmt.Errorf("flova: malformed task budget")
			}
			if k == "b" {
				state.BalanceBefore = parsed
			} else {
				state.BudgetCredits = parsed
			}
		}
	}
	if strings.Contains(tail, ";b=") != strings.Contains(tail, ";c=") || (strings.Contains(tail, ";c=") && state.BudgetCredits <= 0) {
		return flovaTaskState{}, fmt.Errorf("flova: incomplete task budget")
	}
	return state, nil
}

// ---------------------------------------------------------------------------
// HTTP：信封、请求执行、错误分类
// ---------------------------------------------------------------------------

type flovaEnvelope struct {
	Code      json.RawMessage `json:"code"`
	Message   string          `json:"message"`
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
	Error     json.RawMessage `json:"error"`
}

func (e *flovaEnvelope) codeString() string {
	if e == nil || len(e.Code) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Code, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(e.Code, &n); err == nil {
		return n.String()
	}
	return strings.Trim(string(e.Code), `"`)
}

func (e *flovaEnvelope) ok() bool {
	return e != nil && e.codeString() == "0"
}

type flovaHTTPResult struct {
	status   int
	headers  http.Header
	body     []byte
	envelope *flovaEnvelope
}

func (p *FlovaVideoProvider) newRequest(ctx context.Context, account *Account, method, target string, body io.Reader, contentType string) (*http.Request, error) {
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	req, err := http.NewRequestWithContext(requestCtx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey(account))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", p.userAgent())
	req.Header.Set("X-Request-Id", strings.ReplaceAll(uuid.NewString(), "-", ""))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if env := strings.TrimSpace(account.GetCredential(flovaCredentialEnvName)); env != "" {
		req.Header.Set("x-env-name", env)
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func (p *FlovaVideoProvider) execute(req *http.Request, account *Account) (*http.Response, error) {
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if p.tlsProfiles == nil {
		return p.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	return p.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, p.tlsProfiles.ResolveTLSProfile(account))
}

// do 向 Agent API v2 发一个 JSON POST，返回状态码 + 解析后的信封。传输层错误原样返回。
func (p *FlovaVideoProvider) do(ctx context.Context, account *Account, endpoint string, payload any) (*flovaHTTPResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	target := p.baseURL(account) + "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	req, err := p.newRequest(ctx, account, http.MethodPost, target, bytes.NewReader(body), "application/json")
	if err != nil {
		return nil, err
	}
	response, err := p.execute(req, account)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	return p.readResult(response)
}

func (p *FlovaVideoProvider) readResult(response *http.Response) (*flovaHTTPResult, error) {
	limit := int64(flovaMaxJSONResponse)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limit = flovaMaxErrorResponse
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("flova: response exceeded the size limit")
	}
	result := &flovaHTTPResult{status: response.StatusCode, headers: response.Header.Clone(), body: raw}
	var envelope flovaEnvelope
	if len(bytes.TrimSpace(raw)) > 0 && json.Unmarshal(raw, &envelope) == nil {
		result.envelope = &envelope
	}
	return result, nil
}

// classifyError 把 HTTP 状态与信封 code 映射为 VideoProviderError（方案 6.5）。
func (p *FlovaVideoProvider) classifyError(result *flovaHTTPResult, account *Account, submission bool) *VideoProviderError {
	message, code := "", ""
	if result.envelope != nil {
		message = result.envelope.Message
		code = result.envelope.codeString()
	}
	secret := ""
	if account != nil {
		secret = p.apiKey(account)
	}
	message = boundedProviderMessage(message, secret)
	if message == "" {
		message = http.StatusText(result.status)
	}
	kind := "upstream"
	retryable := false
	certainty := VideoSubmissionRejected
	switch result.status {
	case http.StatusUnauthorized:
		kind, code = "authentication", "unauthorized"
	case http.StatusForbidden:
		kind, code = "permission", "forbidden"
	case http.StatusNotFound:
		kind, code = "not_found", "not_found"
	case http.StatusTooManyRequests:
		kind, code, retryable = "rate_limit", "rate_limited", true
	default:
		if result.status >= 500 || result.status == 0 {
			kind, code, retryable = "transport", "upstream_unavailable", true
		} else if result.status >= 400 {
			kind = "validation"
		}
	}
	if result.status >= 200 && result.status < 300 {
		// HTTP 2xx 但信封报错：业务错误。
		switch strings.ToLower(code) {
		case "insufficient_credits", "membership_required":
			kind, code = "upstream", videoInsufficientBalanceCode
		case "agent_busy":
			kind, retryable = "conflict", true
		default:
			if code == "" {
				code = "upstream_error"
			}
		}
	}
	if submission && retryable {
		certainty = VideoSubmissionUnknown
	}
	return &VideoProviderError{
		Kind: kind, Code: boundedVideoProviderCode(code), Message: message, Retryable: retryable,
		Certainty: certainty, RetryAfter: parseRetryAfter(result.headers), StatusCode: result.status,
	}
}

func (p *FlovaVideoProvider) resultError(result *flovaHTTPResult, account *Account, submission bool) error {
	if result == nil {
		return controlVideoProviderError(errors.New("flova: empty response"))
	}
	if result.status >= 200 && result.status < 300 && result.envelope != nil && result.envelope.ok() {
		return nil
	}
	return p.classifyError(result, account, submission)
}

func (p *FlovaVideoProvider) decodeData(result *flovaHTTPResult, target any) error {
	if result == nil || result.envelope == nil || len(result.envelope.Data) == 0 {
		return errors.New("flova: response has no data")
	}
	return json.Unmarshal(result.envelope.Data, target)
}

// ---------------------------------------------------------------------------
// 账号级调用：/user、Skill
// ---------------------------------------------------------------------------

// FlovaUserInfo 是 POST /user 的 data（credits_balance 是字符串）。
type FlovaUserInfo struct {
	CreditsBalance   float64
	Email            string
	UserID           string
	UpgradeURL       string
	SubscriptionInfo map[string]any
	Raw              map[string]any
}

func (p *FlovaVideoProvider) FetchUser(ctx context.Context, account *Account) (*FlovaUserInfo, error) {
	result, err := p.do(ctx, account, "/user", map[string]any{})
	if err != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", "Flova user lookup transport failed", err)
	}
	if err := p.resultError(result, account, false); err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := p.decodeData(result, &raw); err != nil {
		return nil, controlVideoProviderError(err)
	}
	info := &FlovaUserInfo{Raw: raw}
	if v, ok := flovaToFloat(raw["credits_balance"]); ok {
		info.CreditsBalance = v
	}
	info.Email, _ = raw["email"].(string)
	info.UserID, _ = raw["user_id"].(string)
	info.UpgradeURL, _ = raw["upgrade_url"].(string)
	if sub, ok := raw["subscription_info"].(map[string]any); ok {
		info.SubscriptionInfo = sub
	}
	return info, nil
}

// GetSkill 校验 skill_id 对账号可见。
func (p *FlovaVideoProvider) GetSkill(ctx context.Context, account *Account, skillID string) error {
	result, err := p.do(ctx, account, "/skill/get", map[string]any{"skill_id": strings.TrimSpace(skillID)})
	if err != nil {
		return unknownVideoProviderError("transport", "request_failed", "Flova skill lookup transport failed", err)
	}
	return p.resultError(result, account, false)
}

// ---------------------------------------------------------------------------
// 校验与计划
// ---------------------------------------------------------------------------

func (p *FlovaVideoProvider) ValidateSubmission(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	plan, err := p.plan(account, request, inputs)
	if err != nil {
		return err
	}
	if _, ok := p.taskBudgetCredits(account, plan); !ok {
		return rejectedVideoProviderError("validation", "unsupported_size", fmt.Sprintf("%s %s has no price on Flova", plan.profile.DisplayName, plan.resolution), http.StatusBadRequest)
	}
	return nil
}

func (p *FlovaVideoProvider) plan(account *Account, request VideoCreateRequest, inputs []VideoInput) (*flovaPlan, error) {
	if !p.SupportsAccount(account) {
		return nil, rejectedVideoProviderError("permission", "unsupported_account", "account does not support Flova videos", http.StatusForbidden)
	}
	if normalizeVideoOperation(request.Operation) != VideoOperationGenerate {
		return nil, rejectedVideoProviderError("validation", "unsupported_operation", "Flova supports generate only", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, rejectedVideoProviderError("validation", "missing_prompt", "prompt is required", http.StatusBadRequest)
	}
	if request.Quality != "" || request.ServiceTier != "" || request.ParentTask != nil || len(request.Characters) != 0 {
		return nil, rejectedVideoProviderError("validation", "unsupported_option", "quality, service_tier, characters and parent tasks are not supported by Flova", http.StatusBadRequest)
	}
	if request.HasOpenAIOnlyFields() {
		return nil, rejectedVideoProviderError("validation", "unsupported_option", "size, quality, aspect_ratio, input_reference and characters are OpenAI-only fields", http.StatusBadRequest)
	}
	// 任务服务已按 ArkParameterSupport 忽略 seed / watermark / generate_audio / return_last_frame，
	// 并把 duration=-1 / frames 换算成秒数；camera_fixed 写进指令。到这里还带着这些值说明
	// 绕过了任务服务（或模型没有默认时长可换算），按原样拒绝。
	if request.SmartDuration || request.Frames > 0 || request.Seed != nil || request.Watermark != nil ||
		request.ReturnLastFrame != nil || (request.AudioEnabled != nil && *request.AudioEnabled) {
		return nil, rejectedVideoProviderError("validation", "unsupported_option", "Flova does not support duration -1, frames, seed, watermark, generate_audio or return_last_frame", http.StatusBadRequest)
	}
	// Flova 的参考图必须先上传到它的存储：content[] 里只接受首帧，且必须是字节（data: URI），
	// https 引用没有可下载再上传的通道。
	for _, item := range request.MediaContent() {
		if item.Role != VideoInputRoleFirstFrame && item.Role != VideoInputRoleReferenceImage {
			return nil, rejectedVideoProviderError("validation", "unsupported_reference_media", fmt.Sprintf("Flova does not accept content role %q", item.Role), http.StatusBadRequest)
		}
		if item.Input == nil {
			return nil, rejectedVideoProviderError("validation", "unsupported_reference_media", "Flova reference images must be inline data: URIs, not remote URLs", http.StatusBadRequest)
		}
	}
	profile := flovaModelProfileFor(request.Model)
	if profile == nil {
		return nil, rejectedVideoProviderError("validation", "unsupported_model", fmt.Sprintf("model %q is not available on Flova", strings.TrimSpace(request.Model)), http.StatusBadRequest)
	}
	capabilities := p.Capabilities()
	if capabilities.SupportedModels != nil && !capabilities.SupportedModels[profile.ID] {
		return nil, rejectedVideoProviderError("validation", "unsupported_model", fmt.Sprintf("model %q is disabled in the Flova catalog", profile.ID), http.StatusBadRequest)
	}
	ratio, resolution, err := flovaResolveRendition(profile, request.Ratio, request.Resolution)
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "unsupported_size", err.Error(), http.StatusBadRequest)
	}
	if _, priced := profile.CreditsPerSecond[resolution]; !priced {
		return nil, rejectedVideoProviderError("validation", "unsupported_size", fmt.Sprintf("%s %s is not enabled on Flova yet", profile.DisplayName, resolution), http.StatusBadRequest)
	}
	seconds := request.Seconds
	if seconds <= 0 {
		seconds = profile.DefaultSeconds
	}
	if !profile.allowsSeconds(seconds) {
		return nil, rejectedVideoProviderError("validation", "unsupported_seconds", fmt.Sprintf("%s does not support %d seconds", profile.DisplayName, seconds), http.StatusBadRequest)
	}
	maxInputs := capabilities.MaxInputsByOperation[VideoOperationGenerate]
	if maxInputs <= 0 {
		maxInputs = 1
	}
	if len(inputs) > maxInputs {
		return nil, rejectedVideoProviderError("validation", "too_many_inputs", fmt.Sprintf("Flova accepts at most %d reference image(s)", maxInputs), http.StatusBadRequest)
	}
	if profile.RequiresImage && len(inputs) == 0 {
		return nil, rejectedVideoProviderError("validation", "missing_reference_image", fmt.Sprintf("%s requires a reference image on Flova", profile.DisplayName), http.StatusBadRequest)
	}
	for i := range inputs {
		input := inputs[i]
		if input.Role != VideoInputRoleReferenceImage && input.Role != VideoInputRoleFirstFrame {
			return nil, rejectedVideoProviderError("validation", "unsupported_input_role", fmt.Sprintf("input role %q is not supported by Flova", input.Role), http.StatusBadRequest)
		}
		mimeType := strings.ToLower(strings.TrimSpace(input.MIMEType))
		if !capabilities.InputMIMETypes[VideoInputRoleFirstFrame][mimeType] {
			return nil, rejectedVideoProviderError("validation", "unsupported_input_type", fmt.Sprintf("reference image type %q is not supported", mimeType), http.StatusBadRequest)
		}
		if limit := capabilities.MaxInputBytes[VideoInputRoleFirstFrame]; limit > 0 && input.Size > limit {
			return nil, rejectedVideoProviderError("validation", "input_too_large", "reference image exceeds the Flova size limit", http.StatusBadRequest)
		}
	}
	cameraFixed := request.CameraFixed != nil && *request.CameraFixed
	return &flovaPlan{profile: profile, ratio: ratio, resolution: resolution, seconds: seconds, cameraFixed: cameraFixed}, nil
}

// ---------------------------------------------------------------------------
// Create：live 余额 B0 → 上传 → 建项目 → chat
//
// 单账号串行由任务库保证（账号上有在途或未结清的 Flova 任务时不会选到这个账号），所以 B0
// 只属于本任务。B0 与预算写进 ProviderTaskID，创建后不再变化。
// ---------------------------------------------------------------------------

func (p *FlovaVideoProvider) Create(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	plan, err := p.plan(account, request, inputs)
	if err != nil {
		return nil, err
	}
	budget, ok := p.taskBudgetCredits(account, plan)
	if !ok {
		return nil, rejectedVideoProviderError("validation", "unsupported_size", fmt.Sprintf("%s %s has no price on Flova", plan.profile.DisplayName, plan.resolution), http.StatusBadRequest)
	}
	balanceBefore, err := p.preflightCredits(ctx, account, plan, budget)
	if err != nil {
		return nil, err
	}
	files := make([]flovaFile, 0, len(inputs))
	for i := range inputs {
		file, err := p.uploadInput(ctx, account, inputs[i])
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	projectID, err := p.createProject(ctx, account, request)
	if err != nil {
		return nil, err
	}
	content, instructionSHA := flovaRenderInstruction(plan, request.Prompt, files)
	streamChatID, err := p.startRun(ctx, account, projectID, content, files)
	if err != nil {
		return nil, err
	}
	now := p.now().UTC()
	state := flovaTaskState{
		ProjectID: projectID, StreamChatID: streamChatID, ModelKey: strings.TrimPrefix(plan.profile.ID, "flova/"),
		Resolution: plan.resolution, Seconds: plan.seconds, SubmittedAt: now,
		BalanceBefore: balanceBefore, BudgetCredits: budget,
	}
	fileIDs := make([]string, 0, len(files))
	for _, file := range files {
		fileIDs = append(fileIDs, file.ID)
	}
	observability.DefaultVideoMetrics().RecordFlovaOutcome("submitted")
	return &ProviderVideoTask{
		ProviderTaskID: state.encode(), Status: VideoGenerationQueued, RawStatus: "chat_started",
		ProviderCreatedAt: &now, SuggestedPollInterval: flovaDefaultPollInterval,
		// 请求参数不是产出计量，这里只放运行身份，供排障对照 Flova 项目。
		Metadata: map[string]any{
			"project_id": projectID, "stream_chat_id": streamChatID, "submitted_at": now.Format(time.RFC3339),
			"skill_id": p.skillID(account), "instruction_sha256": instructionSHA, "files": fileIDs,
		},
	}, nil
}

// preflightCredits：余额不足时 Flova 不报错而是静默不生成，所以提交前必须 live 查一次。
// 返回的余额就是 B0：单账号串行保证提交时没有别的任务在花这个账号的钱，终态后按 B0 − B1
// 结清本任务的供应商成本。要求 B0 覆盖任务预算再加低水位，低水位留给预估冻结与入账误差。
func (p *FlovaVideoProvider) preflightCredits(ctx context.Context, account *Account, plan *flovaPlan, budget float64) (float64, error) {
	user, err := p.FetchUser(ctx, account)
	if err != nil {
		var typed *VideoProviderError
		if errors.As(err, &typed) {
			typed.Certainty = VideoSubmissionRejected
		}
		return 0, err
	}
	if !finiteNonNegative(user.CreditsBalance) || user.CreditsBalance > 1e12 {
		return 0, rejectedVideoProviderError("upstream", "invalid_balance", "Flova returned an unusable credits balance", http.StatusBadGateway)
	}
	need := budget + p.minCreditsReserve(account)
	if user.CreditsBalance < need {
		observability.DefaultVideoMetrics().RecordFlovaOutcome("insufficient_balance")
		return 0, &VideoProviderError{
			Kind: "upstream", Code: videoInsufficientBalanceCode,
			Message:   fmt.Sprintf("Flova account has %.0f credits, %s %s %ds needs %.0f including the reserve", user.CreditsBalance, plan.profile.DisplayName, plan.resolution, plan.seconds, need),
			Retryable: false, Certainty: VideoSubmissionRejected, StatusCode: http.StatusPaymentRequired,
		}
	}
	return user.CreditsBalance, nil
}

// uploadInput 把参考图 multipart 上传到 Flova 存储（字段名 file），换取 file_url。
func (p *FlovaVideoProvider) uploadInput(ctx context.Context, account *Account, input VideoInput) (flovaFile, error) {
	var empty flovaFile
	if input.Open == nil {
		return empty, rejectedVideoProviderError("validation", "input_unavailable", "reference image payload is unavailable", http.StatusBadRequest)
	}
	reader, err := input.Open(ctx)
	if err != nil {
		return empty, rejectedVideoProviderError("validation", "input_unavailable", "reference image could not be read", http.StatusBadRequest)
	}
	defer func() { _ = reader.Close() }()
	limit := p.Capabilities().MaxInputBytes[VideoInputRoleReferenceImage]
	if limit <= 0 {
		limit = flovaMaxReferenceImageBytes
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return empty, rejectedVideoProviderError("validation", "input_unavailable", "reference image could not be read", http.StatusBadRequest)
	}
	if int64(len(data)) > limit {
		return empty, rejectedVideoProviderError("validation", "input_too_large", "reference image exceeds the Flova size limit", http.StatusBadRequest)
	}
	name := strings.TrimSpace(input.FileName)
	if name == "" {
		name = "reference" + flovaExtensionFor(input.MIMEType)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	header.Set("Content-Type", input.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return empty, controlVideoProviderError(err)
	}
	if _, err := part.Write(data); err != nil {
		return empty, controlVideoProviderError(err)
	}
	if err := writer.Close(); err != nil {
		return empty, controlVideoProviderError(err)
	}
	req, err := p.newRequest(ctx, account, http.MethodPost, p.uploadURL(account), bytes.NewReader(body.Bytes()), writer.FormDataContentType())
	if err != nil {
		return empty, controlVideoProviderError(err)
	}
	response, err := p.execute(req, account)
	if err != nil {
		return empty, &VideoProviderError{Kind: "transport", Code: "upload_failed", Message: "Flova image upload transport failed", Retryable: true, Certainty: VideoSubmissionRejected, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	result, err := p.readResult(response)
	if err != nil {
		return empty, controlVideoProviderError(err)
	}
	if err := p.resultError(result, account, false); err != nil {
		var typed *VideoProviderError
		if errors.As(err, &typed) {
			typed.Certainty = VideoSubmissionRejected
			if typed.Code == "" || typed.Code == "upstream_error" {
				typed.Code = "upload_failed"
			}
		}
		return empty, err
	}
	var data2 struct {
		FileName string `json:"file_name"`
		FileType string `json:"file_type"`
		FileURL  string `json:"file_url"`
	}
	if err := p.decodeData(result, &data2); err != nil {
		return empty, controlVideoProviderError(err)
	}
	fileURL := strings.TrimSpace(data2.FileURL)
	if !strings.HasPrefix(strings.ToLower(fileURL), "https://") {
		return empty, rejectedVideoProviderError("upstream", "upload_failed", "Flova image upload returned an invalid url", http.StatusBadGateway)
	}
	fileType := strings.TrimSpace(data2.FileType)
	if fileType == "" {
		fileType = "image"
	}
	fileName := strings.TrimSpace(data2.FileName)
	if fileName == "" {
		fileName = name
	}
	sum := sha256.Sum256([]byte(fileURL))
	return flovaFile{ID: "file_" + hex.EncodeToString(sum[:6]), Name: fileName, Type: fileType, URL: fileURL}, nil
}

func flovaExtensionFor(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func (p *FlovaVideoProvider) createProject(ctx context.Context, account *Account, request VideoCreateRequest) (string, error) {
	name := "sub2api-" + strings.TrimSpace(request.TaskID)
	if strings.TrimSpace(request.TaskID) == "" {
		name = "sub2api-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	}
	result, err := p.do(ctx, account, "/create", map[string]any{"name": name, "description": "sub2api relay", "project_type": 0})
	if err != nil {
		return "", &VideoProviderError{Kind: "transport", Code: "request_failed", Message: "Flova project creation transport failed", Retryable: true, Certainty: VideoSubmissionRejected, Cause: err}
	}
	if err := p.resultError(result, account, false); err != nil {
		return "", err
	}
	var data struct {
		ProjectID string `json:"project_id"`
	}
	if err := p.decodeData(result, &data); err != nil || !validVideoProviderIdentifier(data.ProjectID) || strings.ContainsAny(data.ProjectID, ":|;=") {
		return "", controlVideoProviderError(errors.New("flova: project creation returned no usable project_id"))
	}
	return strings.TrimSpace(data.ProjectID), nil
}

func (p *FlovaVideoProvider) startRun(ctx context.Context, account *Account, projectID, content string, files []flovaFile) (string, error) {
	extra := map[string]any{
		"client": "flova-cli", "client_version": p.clientVersion(account), "os": "linux", "arch": "amd64", "source": "cli",
	}
	if skillID := p.skillID(account); skillID != "" {
		extra["skill_id"] = skillID
	}
	payload := map[string]any{"project_id": projectID, "content": content, "extra_data": extra}
	if len(files) > 0 {
		payload["files"] = files
	}
	result, err := p.do(ctx, account, "/chat", payload)
	if err != nil {
		// /chat 的结果未知：回合可能已经开始。
		return "", unknownVideoProviderError("transport", "request_failed", "Flova run submission transport failed", err)
	}
	if err := p.resultError(result, account, true); err != nil {
		return "", err
	}
	var data struct {
		StreamChatID string `json:"stream_chat_id"`
	}
	if err := p.decodeData(result, &data); err != nil || !validVideoProviderIdentifier(data.StreamChatID) || strings.ContainsAny(data.StreamChatID, ":|;=") {
		return "", unknownVideoProviderError("upstream", "missing_stream_chat_id", "Flova run submission returned no stream_chat_id", errors.New("missing stream_chat_id"))
	}
	return strings.TrimSpace(data.StreamChatID), nil
}

// ---------------------------------------------------------------------------
// Get：run_result 状态机
// ---------------------------------------------------------------------------

type flovaRunResult struct {
	Status            string                  `json:"status"`
	Terminal          bool                    `json:"terminal"`
	CompletionReason  string                  `json:"completion_reason"`
	ProjectID         string                  `json:"project_id"`
	StreamChatID      string                  `json:"stream_chat_id"`
	AssistantMessages []flovaAssistantMessage `json:"assistant_messages"`
	PendingActions    []flovaPendingAction    `json:"pending_actions"`
}

type flovaAssistantMessage struct {
	MessageID string `json:"message_id"`
	Contents  []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"contents"`
	Artifacts []flovaArtifact `json:"artifacts"`
}

type flovaArtifact struct {
	ResourceID   string `json:"resource_id"`
	MediaType    string `json:"media_type"`
	ArtifactRole string `json:"artifact_role"`
	Status       string `json:"status"`
	ResourceURL  string `json:"resource_url"`
}

type flovaPendingAction struct {
	ActionID        string              `json:"action_id"`
	Type            string              `json:"type"`
	Blocking        bool                `json:"blocking"`
	Message         string              `json:"message"`
	ActionURL       string              `json:"action_url"`
	Options         []flovaActionOption `json:"options"`
	Payload         map[string]any      `json:"payload"`
	ResumeMessageID string              `json:"resume_message_id"`
}

type flovaActionOption struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Effect string `json:"effect"`
	Scope  string `json:"scope"`
}

func (p *FlovaVideoProvider) Get(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	if p == nil || ref.Provider != VideoProviderFlova {
		return nil, ErrVideoProviderUnsupported
	}
	state, err := flovaStateForRef(ref)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	result, err := p.do(ctx, account, "/run_result", map[string]any{"project_id": state.ProjectID, "stream_chat_id": state.activeStream()})
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	var task *ProviderVideoTask
	rawStatus := ""
	if code, notReady := flovaRunNotReadyCode(result); notReady {
		// run 还在跑：上游用业务错误码而不是 processing 状态来表达（实测
		// run_result_not_ready / "Run is still processing"），必须按进行中处理。
		poll := flovaDefaultPollInterval
		if code == "agent_busy" {
			poll = flovaBusyPollInterval
		}
		task, rawStatus = p.progressTask(state, code, VideoGenerationInProgress, poll), code
	} else {
		if err := p.resultError(result, account, false); err != nil {
			return nil, flovaPollError(err)
		}
		var run flovaRunResult
		if err := p.decodeData(result, &run); err != nil {
			return nil, controlVideoProviderError(err)
		}
		if (run.ProjectID != "" && run.ProjectID != state.ProjectID) ||
			(run.StreamChatID != "" && run.StreamChatID != state.activeStream()) {
			return nil, controlVideoProviderError(errors.New("flova observation belongs to another project or run"))
		}
		spent, overspent := p.observeSpend(ctx, account, state)
		task, err = p.evaluateRun(ctx, account, state, &run, spent)
		if err != nil {
			return nil, err
		}
		rawStatus = run.Status
		if spent != nil {
			if task.Control == nil {
				task.Control = &VideoProviderControlObservation{State: flovaControlState(state)}
			}
			task.Control.SpentCredits, task.Control.Overspent = spent, overspent
		}
	}
	if task.Status != VideoGenerationCompleted && task.Status != VideoGenerationFailed && p.runTimedOut(state, account) {
		timedOut := p.failedTask(state, flovaErrorRunTimeout, fmt.Sprintf("Flova run exceeded %s without a terminal result", p.maxRunDuration(account)), rawStatus)
		timedOut.Control = task.Control
		task = timedOut
	}
	if task.Control == nil {
		task.Control = &VideoProviderControlObservation{State: flovaControlState(state)}
	}
	// A read-only observation can enrich private state, never replace the
	// stable provider identity (including legacy encoded resource IDs).
	task.ProviderTaskID = ref.ProviderTaskID
	return task, nil
}

// flovaRunNotReadyCode 识别「run 尚未结束」的业务错误码。
func flovaRunNotReadyCode(result *flovaHTTPResult) (string, bool) {
	if result == nil || result.envelope == nil || result.envelope.ok() || result.status < 200 || result.status >= 300 {
		return "", false
	}
	code := strings.ToLower(result.envelope.codeString())
	switch code {
	case "run_result_not_ready", "agent_busy", "run_in_progress", "run_not_finished", "processing", "not_ready":
		return code, true
	}
	if strings.Contains(code, "not_ready") || strings.Contains(code, "processing") || strings.Contains(code, "in_progress") {
		return code, true
	}
	return "", false
}

// flovaPollError：轮询阶段的业务错误除了明确的终态（余额 / 档位 / 鉴权 / 权限 / 不存在）
// 一律可重试，由 max_run_minutes 兜底；把一个没见过的 code 判成终态会误杀正在生成的任务。
func flovaPollError(err error) error {
	var typed *VideoProviderError
	if !errors.As(err, &typed) || typed == nil {
		return err
	}
	switch typed.Kind {
	case "authentication", "permission", "not_found":
		return typed
	}
	if typed.Code == videoInsufficientBalanceCode {
		return typed
	}
	typed.Retryable = true
	return typed
}

func (p *FlovaVideoProvider) runTimedOut(state flovaTaskState, account *Account) bool {
	if state.SubmittedAt.IsZero() {
		return false
	}
	return p.now().UTC().Sub(state.SubmittedAt) > p.maxRunDuration(account)
}

// observeSpend 在带预算的任务运行中按账号余额估算已花费积分（B0 − 当前余额，含上游冻结的
// 预估额）。读余额失败不影响轮询：花费最终以终态后的余额差对账为准，这里只用于提前熔断。
func (p *FlovaVideoProvider) observeSpend(ctx context.Context, account *Account, state flovaTaskState) (*float64, bool) {
	if !state.budgeted() {
		return nil, false
	}
	user, err := p.FetchUser(ctx, account)
	if err != nil || user == nil || math.IsNaN(user.CreditsBalance) || math.IsInf(user.CreditsBalance, 0) {
		return nil, false
	}
	spent := state.BalanceBefore - user.CreditsBalance
	overspent := spent > state.BudgetCredits
	if overspent {
		observability.DefaultVideoMetrics().RecordFlovaOutcome("overspend")
	}
	return &spent, overspent
}

func (p *FlovaVideoProvider) evaluateRun(ctx context.Context, account *Account, state flovaTaskState, run *flovaRunResult, spent *float64) (*ProviderVideoTask, error) {
	raw := strings.ToLower(strings.TrimSpace(run.Status))
	if flovaIsFailedStatus(raw) {
		return p.failedTask(state, raw, flovaLastAssistantText(run, "Flova reported the run as "+raw), raw), nil
	}
	if strings.EqualFold(run.CompletionReason, "waiting_user") || len(run.PendingActions) > 0 {
		return p.handlePendingActions(ctx, account, state, run, spent)
	}
	if !run.Terminal && !flovaIsTerminalStatus(raw) {
		if flovaIsQueuedStatus(raw) {
			return p.progressTask(state, raw, VideoGenerationQueued, flovaDefaultPollInterval), nil
		}
		return p.progressTask(state, raw, VideoGenerationInProgress, flovaDefaultPollInterval), nil
	}
	videos, images := flovaCollectArtifacts(run)
	if len(videos) == 0 {
		return p.failedTask(state, flovaErrorNoVideoArtifact, flovaLastAssistantText(run, "Flova finished the run without a video"), raw), nil
	}
	video := videos[0]
	if state.ResourceID != "" {
		found := false
		for _, candidate := range videos {
			if candidate.ResourceID == state.ResourceID {
				video, found = candidate, true
				break
			}
		}
		if !found {
			return nil, controlVideoProviderError(errors.New("flova observation no longer contains the stored resource"))
		}
	}
	if !validVideoProviderIdentifier(video.ResourceID) || strings.ContainsAny(video.ResourceID, ":|;=") {
		return nil, controlVideoProviderError(errors.New("flova video has no valid resource identity"))
	}
	state.ResourceID = video.ResourceID
	now := p.now().UTC()
	expires := flovaSignedURLExpiry(video.ResourceURL, now)
	task := &ProviderVideoTask{
		ProviderTaskID: state.encode(), Status: VideoGenerationCompleted, RawStatus: raw,
		VideoURL: video.ResourceURL, ContentExpiresAt: &expires, ProviderFinishedAt: &now,
		ContentVariants: []string{"video"},
		Access:          &ProviderTaskAccess{Kind: "signed_url", Value: video.ResourceURL, Scope: "video_content", ExpiresAt: &expires},
		// Requested parameters are not observed output measurements. Legacy
		// completions use the frozen customer quote, with missing usage audited.
		Usage: map[string]any{},
		Metadata: map[string]any{
			"project_id": state.ProjectID, "stream_chat_id": state.StreamChatID, "resource_id": video.ResourceID,
			"artifact_count": len(videos) + len(images), "image_count": len(images),
			"assistant_text": flovaLastAssistantText(run, ""),
		},
	}
	task.ErrorCode = "flova_output_unverified"
	task.ErrorMessage = "Flova output measurements and provider spend could not be verified; customer billing uses the frozen quote"
	task.Control = &VideoProviderControlObservation{State: flovaControlState(state)}
	task.Control.State.VideoArtifacts = len(videos)
	task.Control.State.ImageArtifacts = len(images)
	if len(videos) > 1 {
		task.Metadata["extra_artifacts"] = len(videos) - 1
		observability.DefaultVideoMetrics().RecordFlovaOutcome("extra_artifacts")
	}
	return task, nil
}

func (p *FlovaVideoProvider) handlePendingActions(_ context.Context, account *Account, state flovaTaskState, run *flovaRunResult, spent *float64) (*ProviderVideoTask, error) {
	if state.budgeted() {
		// 门控类动作本身不扣费（实测空完成、门控无账单）：直接失败，不必等到 run 超时。
		for _, action := range run.PendingActions {
			if strings.EqualFold(strings.TrimSpace(action.Type), flovaActionSubscriptionRequired) || flovaActionOpensURL(action) {
				observability.DefaultVideoMetrics().RecordFlovaOutcome("membership_gate")
				return p.failedTask(state, flovaErrorMembershipRequired, flovaNonEmpty(action.Message, "Flova requires a subscription change for this model or resolution"), run.Status), nil
			}
		}
	}
	reason := "Flova requires an action whose spending limit cannot be verified; automatic confirmation is disabled"
	code := "flova_action_blocked"
	if state.budgeted() {
		switch {
		case state.ResumeActionKey != "":
			// 每任务最多确认一次：再出现待确认动作（含 resume 结果未知时的同一动作）一律阻断。
			code, reason = "flova_resume_unconfirmed", "Flova is waiting for another confirmation; this task already used its single automatic confirmation"
		case len(run.PendingActions) != 1:
			reason = fmt.Sprintf("Flova is waiting for %d actions; only a single in-budget confirmation is approved automatically", len(run.PendingActions))
		default:
			action := run.PendingActions[0]
			optionID, err := p.evaluateBudgetedConfirm(account, state, action, spent)
			if err == nil {
				key := flovaBlockedAction(state, action).ActionKey
				task := p.progressTask(state, "resume_authorized", VideoGenerationInProgress, flovaDefaultPollInterval)
				task.Control = &VideoProviderControlObservation{State: flovaControlState(state)}
				task.Control.State.ResumeActionKey = key
				task.Control.ResumeIntent = &VideoProviderResumeIntent{
					ActionKey: key, ActionID: action.ActionID, ResumeMessageID: action.ResumeMessageID, OptionID: optionID,
				}
				observability.DefaultVideoMetrics().RecordFlovaOutcome("resume_authorized")
				return task, nil
			}
			observability.DefaultVideoMetrics().RecordFlovaOutcome("budget_exceeded")
			reason = "Flova confirmation was not approved automatically: " + err.Error()
		}
	}
	task := p.progressTask(state, "action_blocked", VideoGenerationInProgress, flovaBusyPollInterval)
	task.ErrorCode = code
	task.ErrorMessage = reason
	task.Control = &VideoProviderControlObservation{State: flovaControlState(state)}
	for i, action := range run.PendingActions {
		if i == 16 {
			break
		}
		task.Control.BlockedActions = append(task.Control.BlockedActions, flovaBlockedAction(state, action))
	}
	return task, nil
}

func flovaActionOpensURL(action flovaPendingAction) bool {
	for _, option := range action.Options {
		if strings.EqualFold(option.Effect, "open_url") {
			return true
		}
	}
	return false
}

// evaluateBudgetedConfirm 校验高成本确认：恰好 1 个视频、模型 / 分辨率与请求一致、单价不超过
// 价格表 × (1 + 容差)，且报价加上已观测花费不超过任务预算。通过时返回要 resume 的选项 id。
func (p *FlovaVideoProvider) evaluateBudgetedConfirm(account *Account, state flovaTaskState, action flovaPendingAction, spent *float64) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(action.Type), flovaActionHighCostConfirm) {
		return "", fmt.Errorf("action %q is never confirmed automatically", action.Type)
	}
	items, _ := action.Payload["items"].([]any)
	if len(items) != 1 {
		return "", fmt.Errorf("confirmation lists %d items, expected exactly one video", len(items))
	}
	item, _ := items[0].(map[string]any)
	if item == nil {
		return "", errors.New("confirmation item is malformed")
	}
	if count, ok := numericMapValue(item, "video_count"); ok && int(count) != 1 {
		return "", fmt.Errorf("confirmation covers %d videos, expected one", int(count))
	}
	profile := flovaModelProfileFor(state.model())
	if profile == nil {
		return "", errors.New("task model is unknown to the Flova catalog")
	}
	if modelName, _ := item["model_name"].(string); modelName != "" && !strings.EqualFold(strings.TrimSpace(modelName), profile.DisplayName) {
		return "", fmt.Errorf("quoted model %q differs from the requested %s", modelName, profile.DisplayName)
	}
	if resolution, _ := item["resolution"].(string); resolution != "" && !strings.EqualFold(strings.TrimSpace(resolution), state.Resolution) {
		return "", fmt.Errorf("quoted resolution %s differs from the requested %s", resolution, state.Resolution)
	}
	expected, ok := flovaCreditsPerSecond(profile.ID, state.Resolution)
	if !ok || state.Seconds <= 0 {
		return "", fmt.Errorf("%s %s has no price on Flova", profile.DisplayName, state.Resolution)
	}
	perSecond, hasPerSecond := numericMapValue(item, "credits_per_second")
	total, hasTotal := numericMapValue(item, "estimated_credits")
	if !hasTotal {
		total, hasTotal = numericMapValue(action.Payload, "estimated_credits")
	}
	switch {
	case hasTotal && finiteNonNegative(total):
		if !hasPerSecond {
			perSecond = total / float64(state.Seconds)
		}
	case hasPerSecond && finiteNonNegative(perSecond):
		total = perSecond * float64(state.Seconds)
	default:
		return "", errors.New("confirmation carries no usable price")
	}
	if !finiteNonNegative(perSecond) || perSecond > expected*(1+p.priceDriftTolerance(account)) {
		observability.DefaultVideoMetrics().RecordFlovaOutcome("price_drift")
		return "", fmt.Errorf("quoted %.2f credits/s for %s %s, price table expects %.2f", perSecond, profile.DisplayName, state.Resolution, expected)
	}
	already := 0.0
	if spent != nil && *spent > 0 {
		already = *spent
	}
	if total+already > state.BudgetCredits {
		return "", fmt.Errorf("quoted %.0f credits plus %.0f already spent exceeds the task budget of %.0f", total, already, state.BudgetCredits)
	}
	optionID := flovaResumeOption(action, "once")
	if optionID == "" {
		return "", errors.New("confirmation has no resume option")
	}
	return optionID, nil
}

// flovaResumeOption 选 effect=resume 的选项：优先 preferred id，其次 scope=run，再次任一。
func flovaResumeOption(action flovaPendingAction, preferred string) string {
	var fallback, runScoped string
	for _, option := range action.Options {
		if !strings.EqualFold(option.Effect, "resume") || !validFlovaStreamIdentifier(option.ID) {
			continue
		}
		if preferred != "" && option.ID == preferred {
			return option.ID
		}
		if strings.EqualFold(option.Scope, "run") && runScoped == "" {
			runScoped = option.ID
		}
		if fallback == "" && !strings.EqualFold(option.Action, "select_alternative") {
			fallback = option.ID
		}
	}
	if runScoped != "" {
		return runScoped
	}
	return fallback
}

// ResumeAuthorized 执行一次已授权的确认（POST /chat is_resume），返回上游开出的新 run。
// 由 worker 在授权落库后调用恰好一次；这里只校验授权与任务身份一致，不做任何重试。
func (p *FlovaVideoProvider) ResumeAuthorized(ctx context.Context, account *Account, ref ProviderTaskRef, intent VideoProviderResumeIntent) (string, error) {
	if p == nil || ref.Provider != VideoProviderFlova {
		return "", ErrVideoProviderUnsupported
	}
	state, err := flovaStateForRef(ref)
	if err != nil {
		return "", controlVideoProviderError(err)
	}
	if !state.budgeted() || state.ResumeActionKey == "" || state.ResumeActionKey != intent.ActionKey || state.ResumedStreamChatID != "" ||
		!validFlovaStreamIdentifier(intent.ActionID) || !validFlovaStreamIdentifier(intent.OptionID) ||
		(intent.ResumeMessageID != "" && !validFlovaStreamIdentifier(intent.ResumeMessageID)) {
		return "", controlVideoProviderError(errors.New("flova resume is not authorized for this task"))
	}
	result, err := p.do(ctx, account, "/chat", map[string]any{
		"project_id": state.ProjectID, "is_resume": true,
		"resume_message_id": intent.ResumeMessageID, "action_id": intent.ActionID, "option_id": intent.OptionID,
	})
	if err != nil {
		return "", unknownVideoProviderError("transport", "request_failed", "Flova resume transport failed", err)
	}
	if err := p.resultError(result, account, false); err != nil {
		return "", err
	}
	var data struct {
		StreamChatID string `json:"stream_chat_id"`
	}
	if err := p.decodeData(result, &data); err != nil {
		return "", controlVideoProviderError(err)
	}
	stream := strings.TrimSpace(data.StreamChatID)
	if !validFlovaStreamIdentifier(stream) || stream == state.StreamChatID {
		return "", controlVideoProviderError(errors.New("flova resume returned no usable stream_chat_id"))
	}
	observability.DefaultVideoMetrics().RecordFlovaOutcome("auto_resume")
	return stream, nil
}

func (p *FlovaVideoProvider) progressTask(state flovaTaskState, raw, status string, poll time.Duration) *ProviderVideoTask {
	return &ProviderVideoTask{
		ProviderTaskID: state.encode(), Status: status, RawStatus: raw, SuggestedPollInterval: poll,
		Metadata: map[string]any{
			"model": state.model(), "resolution": flovaArkResolution(state.Resolution), "seconds": state.Seconds,
			"project_id": state.ProjectID, "stream_chat_id": state.StreamChatID,
		},
	}
}

func flovaOutcomeForCode(code string) string {
	switch code {
	case flovaErrorNoVideoArtifact:
		return "empty_completion"
	case flovaErrorMembershipRequired:
		return "membership_gate"
	case flovaErrorPriceDrift:
		return "price_drift"
	case flovaErrorRunTimeout:
		return "run_timeout"
	case flovaErrorUserActionRequired:
		return "user_action_required"
	default:
		return ""
	}
}

func (p *FlovaVideoProvider) failedTask(state flovaTaskState, code, message, raw string) *ProviderVideoTask {
	if outcome := flovaOutcomeForCode(code); outcome != "" {
		observability.DefaultVideoMetrics().RecordFlovaOutcome(outcome)
	}
	now := p.now().UTC()
	task := p.progressTask(state, raw, VideoGenerationFailed, 0)
	task.ErrorCode = boundedVideoProviderCode(code)
	task.ErrorMessage = boundedProviderMessage(message)
	task.ProviderFinishedAt = &now
	return task
}

func flovaIsQueuedStatus(status string) bool {
	switch status {
	case "pending", "queued", "in_queue", "in_free_queue", "wait_model_concurrency", "wait_user_concurrency":
		return true
	}
	return false
}

func flovaIsFailedStatus(status string) bool {
	switch status {
	case "failed", "error", "bind_asset_id_failed", "cancelled", "canceled":
		return true
	}
	return false
}

func flovaIsTerminalStatus(status string) bool {
	switch status {
	case "completed", "succeeded", "success", "done":
		return true
	}
	return flovaIsFailedStatus(status)
}

func flovaCollectArtifacts(run *flovaRunResult) (videos, images []flovaArtifact) {
	for _, message := range run.AssistantMessages {
		for _, artifact := range message.Artifacts {
			switch strings.ToLower(strings.TrimSpace(artifact.MediaType)) {
			case "video":
				if flovaArtifactSucceeded(artifact) && strings.HasPrefix(strings.ToLower(strings.TrimSpace(artifact.ResourceURL)), "https://") {
					videos = append(videos, artifact)
				}
			case "image":
				images = append(images, artifact)
			}
		}
	}
	return videos, images
}

func flovaArtifactSucceeded(artifact flovaArtifact) bool {
	switch strings.ToLower(strings.TrimSpace(artifact.Status)) {
	case "success", "succeeded", "completed", "done", "":
		return true
	}
	return false
}

func flovaLastAssistantText(run *flovaRunResult, fallback string) string {
	for i := len(run.AssistantMessages) - 1; i >= 0; i-- {
		for j := len(run.AssistantMessages[i].Contents) - 1; j >= 0; j-- {
			content := run.AssistantMessages[i].Contents[j]
			if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
				text := strings.TrimSpace(content.Text)
				if len(text) > flovaAssistantTextLimit {
					text = text[:flovaAssistantTextLimit]
				}
				return text
			}
		}
	}
	return fallback
}

func flovaNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

// flovaSignedURLExpiry 从签名 URL 算过期时间：付费档 Expires=<unix>，免费档
// X-Goog-Date + X-Goog-Expires；都没有时按 24 小时。
func flovaSignedURLExpiry(rawURL string, now time.Time) time.Time {
	fallback := now.Add(flovaDefaultContentTTL)
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fallback
	}
	query := parsed.Query()
	for _, key := range []string{"Expires", "expires", "X-Goog-Expires-At", "exp"} {
		if value := query.Get(key); value != "" {
			if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 1_000_000_000 {
				return time.Unix(unix, 0).UTC()
			}
		}
	}
	if ttl := query.Get("X-Goog-Expires"); ttl != "" {
		seconds, err := strconv.ParseInt(ttl, 10, 64)
		if err == nil && seconds > 0 {
			if date := query.Get("X-Goog-Date"); date != "" {
				if issued, err := time.Parse("20060102T150405Z", date); err == nil {
					return issued.Add(time.Duration(seconds) * time.Second)
				}
			}
			return now.Add(time.Duration(seconds) * time.Second)
		}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// 内容访问
// ---------------------------------------------------------------------------

func (p *FlovaVideoProvider) RefreshTaskAccess(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderTaskAccess, error) {
	if p == nil || ref.Provider != VideoProviderFlova {
		return nil, ErrVideoProviderUnsupported
	}
	state, err := flovaStateForRef(ref)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if state.ResourceID == "" {
		// Old completed tasks may predate private state persistence. Recover only
		// from the original run using the side-effect-free Get; never guess a
		// resumed stream or replay /chat. This access API intentionally does not
		// mutate a terminal task or its financial state, so recovery may repeat.
		observed, getErr := p.Get(ctx, account, ref)
		if getErr != nil {
			return nil, getErr
		}
		if observed == nil || observed.Status != VideoGenerationCompleted || observed.Control == nil ||
			observed.Control.State == nil || observed.Control.State.ResourceID == "" {
			return nil, rejectedVideoProviderError("validation", "content_unavailable", "Flova task has no verified completed video resource", http.StatusConflict)
		}
		ref.ProviderExecutionState = observed.Control.State
		state, err = flovaStateForRef(ref)
		if err != nil {
			return nil, controlVideoProviderError(err)
		}
	}
	result, err := p.do(ctx, account, "/resource_info", map[string]any{"project_id": state.ProjectID, "resource_id": state.ResourceID})
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if err := p.resultError(result, account, false); err != nil {
		return nil, err
	}
	var data struct {
		Resource struct {
			ResourceURL string `json:"resource_url"`
			Status      string `json:"status"`
		} `json:"resource"`
	}
	if err := p.decodeData(result, &data); err != nil {
		return nil, controlVideoProviderError(err)
	}
	resourceURL := strings.TrimSpace(data.Resource.ResourceURL)
	if !strings.HasPrefix(strings.ToLower(resourceURL), "https://") {
		return nil, rejectedVideoProviderError("upstream", "content_unavailable", "Flova resource has no signed url", http.StatusConflict)
	}
	expires := flovaSignedURLExpiry(resourceURL, p.now().UTC())
	return &ProviderTaskAccess{Kind: "signed_url", Value: resourceURL, Scope: "video_content", ExpiresAt: &expires}, nil
}

func (p *FlovaVideoProvider) OpenContent(ctx context.Context, account *Account, request ProviderContentRequest) (*ProviderContent, error) {
	if p == nil || request.TaskRef.Provider != VideoProviderFlova {
		return nil, ErrVideoProviderUnsupported
	}
	variant := strings.ToLower(strings.TrimSpace(request.Variant))
	if variant == "" {
		variant = "video"
	}
	if variant != "video" || !p.Capabilities().SupportsVariant(variant) {
		return nil, rejectedVideoProviderError("validation", "unsupported_variant", "video content variant is not supported", http.StatusBadRequest)
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, rejectedVideoProviderError("validation", "unsupported_method", "video content supports GET and HEAD", http.StatusMethodNotAllowed)
	}
	upstreamURL := strings.TrimSpace(request.UpstreamURL)
	if upstreamURL == "" {
		return nil, rejectedVideoProviderError("validation", "content_unavailable", "Flova video content requires a stored upstream url", http.StatusConflict)
	}
	response, err := p.fetchContent(ctx, account, request, method, upstreamURL)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		// 签名失效或 CDN 拒绝：经 /resource_info 换新 URL 重试一次。
		_ = response.Body.Close()
		access, refreshErr := p.RefreshTaskAccess(ctx, account, request.TaskRef)
		if refreshErr != nil {
			return nil, refreshErr
		}
		response, err = p.fetchContent(ctx, account, request, method, access.Value)
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent &&
		response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		defer func() { _ = response.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(response.Body, flovaMaxErrorResponse))
		return nil, p.classifyError(&flovaHTTPResult{status: response.StatusCode, headers: response.Header.Clone(), body: body}, account, false)
	}
	return &ProviderContent{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: response.Body}, nil
}

func (p *FlovaVideoProvider) fetchContent(ctx context.Context, account *Account, request ProviderContentRequest, method, upstreamURL string) (*http.Response, error) {
	normalized, err := normalizeProviderVideoURL(upstreamURL)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	target, err := url.Parse(normalized)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if !strings.EqualFold(target.Scheme, "https") {
		return nil, controlVideoProviderError(errors.New("video content URL must use HTTPS"))
	}
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	execute := func(requestMethod string) (*http.Response, error) {
		_, addresses, err := validateVideoContentRedirect(ctx, target, normalized, resolver)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, requestMethod, normalized, nil)
		if err != nil {
			return nil, err
		}
		// Flova 的 CDN（Cloudflare）按 UA 拦截默认客户端，必须伪装成官方 CLI。
		req.Header.Set("User-Agent", p.userAgent())
		if value := strings.TrimSpace(request.Range); value != "" {
			req.Header.Set("Range", value)
		}
		if value := strings.TrimSpace(request.IfRange); value != "" {
			req.Header.Set("If-Range", value)
		}
		req.Header.Set("Accept", "*/*")
		return p.executeContent(ctx, req, account, request.ResponseHeaderTimeout, addresses)
	}
	response, err := execute(method)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if method == http.MethodHead && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented) {
		_ = response.Body.Close()
		response, err = execute(http.MethodGet)
		if err != nil {
			return nil, controlVideoProviderError(err)
		}
	}
	return response, nil
}

func (p *FlovaVideoProvider) executeContent(ctx context.Context, request *http.Request, account *Account, responseHeaderTimeout time.Duration, addresses []netip.Addr) (*http.Response, error) {
	redirect := p.redirect
	if redirect == nil {
		redirect = executePinnedVideoContentRedirect
	}
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = flovaContentHeaderTimeout
	}
	current := request
	currentAddresses := addresses
	for hop := 0; ; hop++ {
		response, err := executeVideoContentWithHeaderTimeout(ctx, responseHeaderTimeout, func(attemptCtx context.Context) (*http.Response, error) {
			return redirect(attemptCtx, current.Clone(attemptCtx), account, currentAddresses)
		})
		if err != nil {
			return nil, err
		}
		if !isVideoContentRedirect(response.StatusCode) {
			return response, nil
		}
		location := response.Header.Get("Location")
		_ = response.Body.Close()
		if hop >= flovaContentMaxRedirects {
			return nil, errors.New("video content exceeded the redirect limit")
		}
		next, nextAddresses, err := validateVideoContentRedirect(ctx, current.URL, location, resolver)
		if err != nil {
			return nil, err
		}
		following, err := http.NewRequestWithContext(ctx, current.Method, next.String(), nil)
		if err != nil {
			return nil, err
		}
		for _, header := range []string{"Range", "If-Range", "Accept", "User-Agent"} {
			if value := current.Header.Get(header); value != "" {
				following.Header.Set(header, value)
			}
		}
		current = following
		currentAddresses = nextAddresses
	}
}

func (p *FlovaVideoProvider) Delete(context.Context, *Account, ProviderTaskRef) error {
	return rejectedVideoProviderError("validation", "video_capability_unsupported", "Flova does not support deleting generated videos", http.StatusNotImplemented)
}

// ---------------------------------------------------------------------------
// 目录贡献
// ---------------------------------------------------------------------------
