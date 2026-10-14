package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// byteDanceDefaultBaseURL is the Volcengine Ark endpoint used when an account
// does not override it. Account creation intentionally permits an empty
// base_url so private deployments can point at their own gateway.
const byteDanceDefaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// Upstream protocol spoken by a ByteDance account. Ark's own asynchronous video
// API and the OpenAI-shaped relays that mirror Seedance are different wire
// formats reaching the same models, so the account records which one it speaks.
const (
	byteDanceProtocolNative        = "native"
	byteDanceProtocolOpenAICompat  = "openai_compatible"
	byteDanceProtocolCredentialKey = "protocol_mode"
	byteDanceNativeTasksPath       = "/contents/generations/tasks"
	byteDanceMaxJSONResponse       = 1 << 20
	byteDanceMaxErrorResponse      = 64 << 10
	// An image reference is inlined into the request body, and every reference
	// image also passes content moderation, so the two limits must agree.
	byteDanceMaxInlineImageBytes  = MaxContentModerationImageBytes
	byteDanceContentURLTTL        = 24 * time.Hour
	byteDanceDefaultPollInterval  = 5 * time.Second
	byteDanceContentMaxRedirects  = 3
	byteDanceContentHeaderTimeout = 30 * time.Second
)

type ByteDanceVideoProvider struct {
	httpUpstream HTTPUpstream
	tlsProfiles  *TLSFingerprintProfileService
	capabilities VideoCapabilities
	catalog      *VideoCapabilityCatalog
	resolver     videoCallbackIPResolver
	redirect     videoContentRedirectExecutor
}

func NewByteDanceVideoProvider(httpUpstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService) *ByteDanceVideoProvider {
	return &ByteDanceVideoProvider{
		httpUpstream: httpUpstream, tlsProfiles: tlsProfiles,
		capabilities: DefaultByteDanceVideoCapabilities(), resolver: net.DefaultResolver,
		redirect: executePinnedVideoContentRedirect,
	}
}

func (p *ByteDanceVideoProvider) Name() string { return VideoProviderByteDance }

func (p *ByteDanceVideoProvider) Capabilities() VideoCapabilities {
	if p == nil {
		return VideoCapabilities{}
	}
	if p.catalog != nil {
		if capabilities, ok := p.catalog.Capabilities(VideoProviderByteDance); ok {
			return capabilities
		}
	}
	return cloneVideoCapabilities(p.capabilities)
}

func (p *ByteDanceVideoProvider) SupportsAccount(account *Account) bool {
	if p == nil || account == nil {
		return false
	}
	if account.Platform != PlatformByteDance || account.Type != AccountTypeAPIKey || p.apiKey(account) == "" {
		return false
	}
	switch byteDanceProtocolMode(account) {
	case byteDanceProtocolNative:
		return true
	case byteDanceProtocolOpenAICompat:
		// There is no canonical OpenAI-shaped Seedance host — Ark's own video
		// API is the native protocol — so a relay account must name its base
		// URL. Falling back to the Ark default would send OpenAI-shaped bodies
		// to an endpoint that does not exist.
		return strings.TrimSpace(account.GetCredential("base_url")) != ""
	default:
		return false
	}
}

func (p *ByteDanceVideoProvider) baseURL(account *Account) string {
	if account == nil {
		return ""
	}
	if value := strings.TrimSpace(account.GetCredential("base_url")); value != "" {
		return value
	}
	return byteDanceDefaultBaseURL
}

func (p *ByteDanceVideoProvider) apiKey(account *Account) string {
	if account == nil {
		return ""
	}
	return strings.TrimSpace(account.GetCredential("api_key"))
}

// byteDanceProtocolMode reports the wire format for an account, or "" when the
// stored value is not a mode this build understands. An unknown mode must fail
// account support rather than silently fall back to a different protocol.
// ArkParameterSupport：Ark 直连原样透传全部开关（含 execution_expires_after 由 Ark 执行）；
// openai_compatible 中继把 generate_audio / seed / camera_fixed / watermark 按 Ark 字段名
// 转发给中继（能否生效取决于中继），尾帧与服务档没有落点，排队超时由本网关执行。
func (p *ByteDanceVideoProvider) ArkParameterSupport(account *Account, _ VideoCreateRequest) VideoArkParameterSupport {
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		return VideoArkParameterSupport{Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true}
	}
	return ArkNativeParameterSupport()
}

func byteDanceProtocolMode(account *Account) string {
	if account == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(account.GetCredential(byteDanceProtocolCredentialKey))) {
	case "", byteDanceProtocolNative:
		return byteDanceProtocolNative
	case byteDanceProtocolOpenAICompat:
		return byteDanceProtocolOpenAICompat
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func (p *ByteDanceVideoProvider) ValidateSubmission(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	return p.validateAccountAndRequest(account, request, inputs)
}

func (p *ByteDanceVideoProvider) validateAccountAndRequest(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	if p == nil || p.httpUpstream == nil || !p.SupportsAccount(account) {
		return rejectedVideoProviderError("permission", "unsupported_account", "account does not support ByteDance videos", http.StatusForbidden)
	}
	if operation := normalizeVideoOperation(request.Operation); operation != VideoOperationGenerate {
		return rejectedVideoProviderError("validation", "unsupported_operation", "ByteDance videos only support generate", http.StatusBadRequest)
	}
	if err := validateByteDanceArkRequest(request); err != nil {
		return rejectedVideoProviderError("validation", "invalid_seedance_request", err.Error(), http.StatusBadRequest)
	}
	if err := ValidateVideoCreateCapabilities(p.Capabilities(), request, inputs); err != nil {
		return rejectedVideoProviderError("validation", "unsupported_capability", err.Error(), http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return rejectedVideoProviderError("validation", "prompt_required", "video prompt is required", http.StatusBadRequest)
	}
	if request.ParentTask != nil {
		return rejectedVideoProviderError("validation", "unsupported_option", "ByteDance video request contains an unsupported option", http.StatusBadRequest)
	}
	if len(request.ProviderOptions) > 0 {
		// Ark 的全部参数都有显式字段；provider_options 没有可映射的目标，静默丢弃会让
		// 用户以为参数生效，所以直接拒绝。
		return rejectedVideoProviderError("validation", "unsupported_option", "ByteDance videos do not accept provider_options", http.StatusBadRequest)
	}
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		if len(inputs) > 0 {
			return rejectedVideoProviderError("validation", "unsupported_input", "ByteDance openai_compatible mode accepts https media references only", http.StatusBadRequest)
		}
		if _, err := openAICompatibleVideoReferenceFields(normalizeOpenAICompatibleVideoReferenceFraming(request.ReferenceMedia())); err != nil {
			return rejectedVideoProviderError("validation", "invalid_reference_media", "compatible video reference fields are invalid", http.StatusBadRequest)
		}
		return nil
	}
	if _, err := byteDanceContentItems(context.Background(), request, inputs, false); err != nil {
		return err
	}
	return nil
}

// byteDanceContentItems 把规范化请求的 content[] 编码成 Ark 的线格式。文本项原样保留
// （含用户写的 --flag，入口层已把它们解析进显式字段，与 Ark 自己的解析结果一致）；
// https 引用原样透传并带角色；spool 字节只接受图片，内联为 data URI。resolve 为假时
// 只做校验，不读字节。
func byteDanceContentItems(ctx context.Context, request VideoCreateRequest, inputs []VideoInput, resolve bool) ([]byteDanceNativeContentItem, error) {
	content := make([]byteDanceNativeContentItem, 0, len(request.Content)+len(inputs))
	hasText := false
	for _, item := range request.Content {
		if !item.IsMedia() {
			if text := strings.TrimSpace(item.Text); text != "" {
				content = append(content, byteDanceNativeContentItem{Type: VideoContentTypeText, Text: item.Text})
				hasText = true
			}
			continue
		}
		role := item.Role
		if role == VideoInputRoleReferenceImage {
			role = VideoInputRoleFirstFrame
		}
		wireType := role.ArkContentTypeForRole()
		if wireType == "" {
			return nil, rejectedVideoProviderError("validation", "unsupported_reference_media", fmt.Sprintf("content role %q is not supported by ByteDance", item.Role), http.StatusBadRequest)
		}
		if item.Input != nil {
			if wireType != VideoContentTypeImage {
				return nil, rejectedVideoProviderError("validation", "unsupported_reference_media", "ByteDance reference video and audio must be https URLs", http.StatusBadRequest)
			}
			if item.Input.Size > byteDanceMaxInlineImageBytes {
				return nil, rejectedVideoProviderError("validation", "input_too_large", "ByteDance image input is too large to inline", http.StatusBadRequest)
			}
			value := ""
			if resolve {
				dataURI, err := byteDanceInlineImageDataURI(ctx, *item.Input)
				if err != nil {
					return nil, err
				}
				value = dataURI
			}
			content = append(content, byteDanceMediaContentItem(wireType, value, string(role)))
			continue
		}
		normalized, err := normalizeProviderVideoURL(strings.TrimSpace(item.URL))
		if err != nil {
			return nil, rejectedVideoProviderError("validation", "unsupported_reference_media", fmt.Sprintf("content %s url is invalid", wireType), http.StatusBadRequest)
		}
		content = append(content, byteDanceMediaContentItem(wireType, normalized, string(role)))
	}
	if !hasText {
		content = append([]byteDanceNativeContentItem{{Type: VideoContentTypeText, Text: request.Prompt}}, content...)
	}
	// 没有挂到 content 上的 spool 输入（旧式 multipart input_reference）按首帧处理。
	for i := range inputs {
		if byteDanceInputBoundToContent(request.Content, &inputs[i]) {
			continue
		}
		if !inputs[i].Role.IsImageContentRole() {
			return nil, rejectedVideoProviderError("validation", "unsupported_input", "ByteDance video create accepts image inputs only", http.StatusBadRequest)
		}
		if inputs[i].Size > byteDanceMaxInlineImageBytes {
			return nil, rejectedVideoProviderError("validation", "input_too_large", "ByteDance image input is too large to inline", http.StatusBadRequest)
		}
		value := ""
		if resolve {
			dataURI, err := byteDanceInlineImageDataURI(ctx, inputs[i])
			if err != nil {
				return nil, err
			}
			value = dataURI
		}
		role := inputs[i].Role
		if role == VideoInputRoleReferenceImage {
			role = VideoInputRoleFirstFrame
		}
		content = append(content, byteDanceMediaContentItem(VideoContentTypeImage, value, string(role)))
	}
	return content, nil
}

func byteDanceInputBoundToContent(content []VideoContentItem, input *VideoInput) bool {
	for _, item := range content {
		if item.Input != nil && item.Input.SHA256 == input.SHA256 && item.Input.Role == input.Role {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Native Ark wire format
// ---------------------------------------------------------------------------

type byteDanceNativeMediaURL struct {
	URL string `json:"url"`
}

type byteDanceNativeContentItem struct {
	Type     string                   `json:"type"`
	Text     string                   `json:"text,omitempty"`
	Role     string                   `json:"role,omitempty"`
	ImageURL *byteDanceNativeMediaURL `json:"image_url,omitempty"`
	VideoURL *byteDanceNativeMediaURL `json:"video_url,omitempty"`
	AudioURL *byteDanceNativeMediaURL `json:"audio_url,omitempty"`
}

func byteDanceMediaContentItem(wireType, mediaURL, role string) byteDanceNativeContentItem {
	item := byteDanceNativeContentItem{Type: wireType, Role: role}
	reference := &byteDanceNativeMediaURL{URL: mediaURL}
	switch wireType {
	case VideoContentTypeImage:
		item.ImageURL = reference
	case VideoContentTypeVideo:
		item.VideoURL = reference
	case VideoContentTypeAudio:
		item.AudioURL = reference
	}
	return item
}

type byteDanceNativeCreateRequest struct {
	Model                 string                       `json:"model"`
	Content               []byteDanceNativeContentItem `json:"content"`
	Resolution            string                       `json:"resolution,omitempty"`
	Ratio                 string                       `json:"ratio,omitempty"`
	Duration              *int                         `json:"duration,omitempty"`
	Frames                int                          `json:"frames,omitempty"`
	Seed                  *int64                       `json:"seed,omitempty"`
	CameraFixed           *bool                        `json:"camera_fixed,omitempty"`
	Watermark             *bool                        `json:"watermark,omitempty"`
	GenerateAudio         *bool                        `json:"generate_audio,omitempty"`
	ReturnLastFrame       *bool                        `json:"return_last_frame,omitempty"`
	ServiceTier           string                       `json:"service_tier,omitempty"`
	ExecutionExpiresAfter int                          `json:"execution_expires_after,omitempty"`
}

type byteDanceNativeTaskResponse struct {
	ID              string `json:"id"`
	Model           string `json:"model"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	Resolution      string `json:"resolution"`
	Ratio           string `json:"ratio"`
	Duration        *int64 `json:"duration"`
	Frames          *int64 `json:"frames"`
	FramesPerSecond *int64 `json:"framespersecond"`
	Seed            *int64 `json:"seed"`
	GenerateAudio   *bool  `json:"generate_audio"`
	ServiceTier     string `json:"service_tier"`
	RevisedPrompt   string `json:"revised_prompt"`
	Content         *struct {
		VideoURL     string `json:"video_url"`
		LastFrameURL string `json:"last_frame_url"`
	} `json:"content"`
	Usage map[string]any `json:"usage"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func normalizeByteDanceVideoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued":
		return VideoGenerationQueued
	case "running":
		return VideoGenerationInProgress
	case "succeeded":
		return VideoGenerationCompleted
	case "failed":
		return VideoGenerationFailed
	case "cancelled", "canceled":
		return VideoGenerationCancelled
	case "expired":
		return VideoGenerationExpired
	default:
		return ""
	}
}

func decodeByteDanceNativeTask(reader io.Reader, submission bool) (*ProviderVideoTask, error) {
	var response byteDanceNativeTaskResponse
	if err := decodeBoundedJSON(reader, byteDanceMaxJSONResponse, &response); err != nil {
		if submission {
			return nil, unknownVideoProviderError("upstream", "invalid_response", "ByteDance video response could not be decoded", err)
		}
		return nil, controlVideoProviderError(err)
	}
	if !validVideoProviderIdentifier(response.ID) {
		if submission {
			return nil, unknownVideoProviderError("upstream", "invalid_response", "ByteDance video response is missing a task id", nil)
		}
		return nil, controlVideoProviderError(errors.New("ByteDance video response is missing a task id"))
	}
	// A create call answers with the identifier only; treat a missing status as
	// queued rather than failing an accepted submission whose hold is already
	// placed.
	status := normalizeByteDanceVideoStatus(response.Status)
	if status == "" {
		if submission && strings.TrimSpace(response.Status) == "" {
			status = VideoGenerationQueued
		} else {
			return nil, controlVideoProviderError(fmt.Errorf("ByteDance video status %q is not recognized", response.Status))
		}
	}
	task := &ProviderVideoTask{
		ProviderTaskID:        strings.TrimSpace(response.ID),
		Status:                status,
		RawStatus:             strings.TrimSpace(response.Status),
		Usage:                 response.Usage,
		Metadata:              map[string]any{},
		SuggestedPollInterval: byteDanceDefaultPollInterval,
		ProviderCreatedAt:     unixTimePointer(response.CreatedAt),
	}
	if model := strings.TrimSpace(response.Model); model != "" {
		task.Metadata["model"] = model
	}
	// Ark 回报的观测值：结算读 seconds（返回的 duration 是向下取整的整数秒），对外投影
	// 用 resolution / ratio / seed / framespersecond 覆盖请求快照。
	if value := strings.ToLower(strings.TrimSpace(response.Resolution)); value != "" {
		task.Metadata["resolution"] = value
	}
	if value := strings.ToLower(strings.TrimSpace(response.Ratio)); value != "" {
		task.Metadata["ratio"] = value
	}
	if response.Duration != nil && *response.Duration > 0 {
		task.Metadata["seconds"] = *response.Duration
	}
	if response.Frames != nil && *response.Frames > 0 {
		task.Metadata["frames"] = *response.Frames
	}
	if response.FramesPerSecond != nil && *response.FramesPerSecond > 0 {
		task.Metadata["framespersecond"] = *response.FramesPerSecond
	}
	if response.Seed != nil {
		task.Metadata["seed"] = *response.Seed
	}
	if response.GenerateAudio != nil {
		task.Metadata["generate_audio"] = *response.GenerateAudio
	}
	if value := strings.TrimSpace(response.ServiceTier); value != "" {
		task.Metadata["service_tier"] = value
	}
	if value := strings.TrimSpace(response.RevisedPrompt); value != "" {
		task.Metadata["revised_prompt"] = value
	}
	if response.Error != nil {
		task.ErrorCode = boundedVideoProviderCode(strings.TrimSpace(response.Error.Code))
		task.ErrorMessage = boundedProviderMessage(strings.TrimSpace(response.Error.Message))
	}
	if response.Content != nil {
		if value := strings.TrimSpace(response.Content.VideoURL); value != "" {
			normalized, err := normalizeProviderVideoURL(value)
			parsed, parseErr := url.Parse(normalized)
			if err != nil || parseErr != nil || !strings.EqualFold(parsed.Scheme, "https") {
				return nil, controlVideoProviderError(errors.New("ByteDance video url is invalid"))
			}
			task.VideoURL = normalized
		}
		if value := strings.TrimSpace(response.Content.LastFrameURL); value != "" {
			normalized, err := normalizeProviderVideoURL(value)
			parsed, parseErr := url.Parse(normalized)
			if err != nil || parseErr != nil || !strings.EqualFold(parsed.Scheme, "https") {
				return nil, controlVideoProviderError(errors.New("ByteDance last frame url is invalid"))
			}
			task.LastFrameURL = normalized
		}
	}
	if task.Status == VideoGenerationCompleted && task.VideoURL != "" {
		task.ContentVariants = []string{"video"}
		if task.LastFrameURL != "" {
			task.ContentVariants = append(task.ContentVariants, "last_frame")
		}
		finished := unixTimePointer(response.UpdatedAt)
		task.ProviderFinishedAt = finished
		// Ark serves the result from object storage behind a signed URL that
		// expires; record the deadline so content is fetched while still valid.
		if finished != nil {
			expires := finished.Add(byteDanceContentURLTTL)
			task.ContentExpiresAt = &expires
		}
	}
	return task, nil
}

// ---------------------------------------------------------------------------
// Provider operations
// ---------------------------------------------------------------------------

func (p *ByteDanceVideoProvider) Create(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	if err := p.validateAccountAndRequest(account, request, inputs); err != nil {
		return nil, err
	}
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		return p.createOpenAICompatible(ctx, account, request, inputs)
	}
	return p.createNative(ctx, account, request, inputs)
}

func (p *ByteDanceVideoProvider) createNative(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	content, err := byteDanceContentItems(ctx, request, inputs, true)
	if err != nil {
		return nil, err
	}
	payload := byteDanceNativeCreateRequest{
		Model:                 strings.TrimSpace(request.Model),
		Content:               content,
		Resolution:            strings.TrimSpace(request.Resolution),
		Ratio:                 strings.TrimSpace(request.Ratio),
		Frames:                request.Frames,
		Seed:                  request.Seed,
		CameraFixed:           request.CameraFixed,
		Watermark:             request.Watermark,
		GenerateAudio:         request.AudioEnabled,
		ReturnLastFrame:       request.ReturnLastFrame,
		ServiceTier:           strings.TrimSpace(request.ServiceTier),
		ExecutionExpiresAfter: request.ExecutionExpiresAfter,
	}
	if request.Frames <= 0 {
		duration := videoArkDurationValue(request.Seconds, request.SmartDuration)
		payload.Duration = &duration
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "video payload could not be encoded", http.StatusBadRequest)
	}
	response, err := p.do(ctx, account, http.MethodPost, byteDanceNativeTasksPath, strings.NewReader(string(body)), "application/json", request.ClientToken)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", "ByteDance video submission transport failed", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, true, request.Prompt)
	}
	task, err := decodeByteDanceNativeTask(response.Body, true)
	if err != nil {
		return nil, err
	}
	p.applyRequestMetadata(task, request)
	return task, nil
}

func (p *ByteDanceVideoProvider) Get(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	if p == nil || ref.Provider != VideoProviderByteDance {
		return nil, ErrVideoProviderUnsupported
	}
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		return p.getOpenAICompatible(ctx, account, ref)
	}
	response, err := p.do(ctx, account, http.MethodGet, byteDanceNativeTasksPath+"/"+url.PathEscape(ref.ProviderTaskID), nil, "", "")
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	task, err := decodeByteDanceNativeTask(response.Body, false)
	if err != nil {
		return nil, err
	}
	if task.ProviderTaskID != strings.TrimSpace(ref.ProviderTaskID) {
		return nil, controlVideoProviderError(errors.New("ByteDance video response identity does not match the requested task"))
	}
	return task, nil
}

// SearchByClientToken is the read-only lookup used after a submission response
// was lost. Ark-compatible relays may expose the OpenAI list shape; native Ark
// deployments may reject the lookup, in which case the deterministic
// reconciliation window will eventually release the hold.
func (p *ByteDanceVideoProvider) SearchByClientToken(ctx context.Context, account *Account, clientToken string) (*ProviderVideoTask, error) {
	clientToken = strings.TrimSpace(clientToken)
	if p == nil || clientToken == "" {
		return nil, ErrVideoInvalidRequest
	}
	endpoint := openAIVideosEndpoint + "?client_token=" + url.QueryEscape(clientToken)
	if byteDanceProtocolMode(account) == byteDanceProtocolNative {
		endpoint = byteDanceNativeTasksPath + "?client_token=" + url.QueryEscape(clientToken)
	}
	response, err := p.do(ctx, account, http.MethodGet, endpoint, nil, "", "")
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		items, decodeErr := decodeOpenAIVideoTaskList(response.Body)
		if decodeErr != nil {
			return nil, controlVideoProviderError(decodeErr)
		}
		if len(items) > 0 {
			return validateByteDanceCompatibleVideoTask(items[0], false)
		}
		return nil, nil
	}
	var envelope struct {
		Data  []byteDanceNativeTaskResponse `json:"data"`
		Items []byteDanceNativeTaskResponse `json:"items"`
	}
	if err := decodeBoundedJSON(response.Body, byteDanceMaxJSONResponse, &envelope); err != nil {
		return nil, controlVideoProviderError(err)
	}
	rows := envelope.Data
	if len(rows) == 0 {
		rows = envelope.Items
	}
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		encoded, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			continue
		}
		return decodeByteDanceNativeTask(bytes.NewReader(encoded), false)
	}
	return nil, nil
}

func (p *ByteDanceVideoProvider) Delete(ctx context.Context, account *Account, ref ProviderTaskRef) error {
	if p == nil || ref.Provider != VideoProviderByteDance {
		return ErrVideoProviderUnsupported
	}
	endpoint := byteDanceNativeTasksPath + "/" + url.PathEscape(ref.ProviderTaskID)
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		endpoint = openAIVideosEndpoint + "/" + url.PathEscape(ref.ProviderTaskID)
	}
	response, err := p.do(ctx, account, http.MethodDelete, endpoint, nil, "", "")
	if err != nil {
		return controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return p.responseError(response, account, false)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, byteDanceMaxJSONResponse))
	return nil
}

// Cancel maps to the same endpoint as Delete: Ark exposes a single verb that
// cancels a running task and removes a finished one.
func (p *ByteDanceVideoProvider) Cancel(ctx context.Context, account *Account, ref ProviderTaskRef) error {
	return p.Delete(ctx, account, ref)
}

// OpenContent streams the generated video. Ark never serves bytes from the API
// host: a finished task carries an object-storage URL, so the upstream URL is
// always treated as external — HTTPS enforced, address pinned, and no upstream
// credential attached.
func (p *ByteDanceVideoProvider) OpenContent(ctx context.Context, account *Account, request ProviderContentRequest) (*ProviderContent, error) {
	if p == nil || request.TaskRef.Provider != VideoProviderByteDance {
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
		return nil, rejectedVideoProviderError("validation", "content_unavailable", "ByteDance video content requires a stored upstream url", http.StatusConflict)
	}
	normalized, err := normalizeProviderVideoURL(upstreamURL)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	target, err := url.Parse(normalized)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if !strings.EqualFold(target.Scheme, "https") {
		return nil, controlVideoProviderError(errors.New("ByteDance video_url must use HTTPS"))
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
		req.Header.Set("User-Agent", "sub2api-video-content/1")
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
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent &&
		response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		defer func() { _ = response.Body.Close() }()
		return nil, p.responseError(response, account, false)
	}
	return &ProviderContent{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: response.Body}, nil
}

func (p *ByteDanceVideoProvider) executeContent(ctx context.Context, request *http.Request, account *Account, responseHeaderTimeout time.Duration, addresses []netip.Addr) (*http.Response, error) {
	redirect := p.redirect
	if redirect == nil {
		redirect = executePinnedVideoContentRedirect
	}
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = byteDanceContentHeaderTimeout
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
		if hop >= byteDanceContentMaxRedirects {
			return nil, errors.New("ByteDance video content exceeded the redirect limit")
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

// ---------------------------------------------------------------------------
// OpenAI-compatible relay mode
// ---------------------------------------------------------------------------

func (p *ByteDanceVideoProvider) createOpenAICompatible(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	if len(inputs) > 0 {
		return nil, rejectedVideoProviderError("validation", "unsupported_input",
			"ByteDance openai_compatible mode does not accept uploaded inputs", http.StatusBadRequest)
	}
	if request.SmartDuration || request.Frames > 0 {
		return nil, rejectedVideoProviderError("validation", "unsupported_option",
			"ByteDance openai_compatible mode requires an explicit duration", http.StatusBadRequest)
	}
	// 中继的请求体是 OpenAI 形态的信封（prompt / seconds / resolution / ratio / 参考字段）加
	// Seedance 扩展：generate_audio、seed、camera_fixed、watermark 按 Ark 字段名原样转发，
	// 是否生效取决于中继本身。return_last_frame 在任务服务里已按空操作清掉（中继回的是
	// OpenAI 形态，没有尾帧链接）；service_tier 的默认档同样被清掉，到这里的只剩 flex。
	// execution_expires_after 由本网关 worker 执行。
	if tier := strings.TrimSpace(request.ServiceTier); tier != "" {
		return nil, rejectedVideoProviderError("validation", "unsupported_option",
			fmt.Sprintf("ByteDance openai_compatible mode does not support service_tier %q", tier), http.StatusBadRequest)
	}
	if request.ReturnLastFrame != nil {
		return nil, rejectedVideoProviderError("validation", "unsupported_option",
			"ByteDance openai_compatible mode cannot return the last frame", http.StatusBadRequest)
	}
	payload := map[string]any{"model": strings.TrimSpace(request.Model), "prompt": request.Prompt}
	if request.AudioEnabled != nil {
		payload["generate_audio"] = *request.AudioEnabled
	}
	if request.Seed != nil {
		payload["seed"] = *request.Seed
	}
	if request.CameraFixed != nil {
		payload["camera_fixed"] = *request.CameraFixed
	}
	if request.Watermark != nil {
		payload["watermark"] = *request.Watermark
	}
	if request.Seconds > 0 {
		payload["seconds"] = byteDanceOpenAICompatibleSeconds(account, request)
	}
	if resolution := strings.TrimSpace(request.Resolution); resolution != "" {
		payload["resolution"] = resolution
	}
	if ratio := strings.TrimSpace(request.Ratio); ratio != "" && ratio != ArkVideoRatioAdaptive {
		payload["ratio"] = ratio
	}
	media := request.ReferenceMedia()
	media.Ratio = ""
	referenceFields, err := openAICompatibleVideoReferenceFields(normalizeOpenAICompatibleVideoReferenceFraming(media))
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_reference_media",
			"compatible video reference fields are invalid", http.StatusBadRequest)
	}
	for key, value := range referenceFields {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "video payload could not be encoded", http.StatusBadRequest)
	}
	response, err := p.do(ctx, account, http.MethodPost, openAIVideosEndpoint, strings.NewReader(string(body)), "application/json", request.ClientToken)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", "ByteDance video submission transport failed", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, true, request.Prompt)
	}
	task, err := decodeOpenAIVideoTask(response.Body, true)
	if err != nil {
		return nil, err
	}
	if task, err = validateByteDanceCompatibleVideoTask(task, true); err != nil {
		return nil, err
	}
	p.applyRequestMetadata(task, request)
	return task, nil
}

func (p *ByteDanceVideoProvider) getOpenAICompatible(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	response, err := p.do(ctx, account, http.MethodGet, openAIVideosEndpoint+"/"+url.PathEscape(ref.ProviderTaskID), nil, "", "")
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	task, err := decodeOpenAIVideoTask(response.Body, false)
	if err != nil {
		return nil, err
	}
	if task, err = validateByteDanceCompatibleVideoTask(task, false); err != nil {
		return nil, err
	}
	if task.ProviderTaskID != strings.TrimSpace(ref.ProviderTaskID) {
		return nil, controlVideoProviderError(errors.New("ByteDance video response identity does not match the requested task"))
	}
	return task, nil
}

// OpenAI's shared decoder accepts both HTTP and HTTPS video URLs for backwards
// compatibility with existing providers. ByteDance content is always fetched
// through the hardened external-content path, so its relay mode must apply the
// same HTTPS-only policy as the native Ark mode.
func validateByteDanceCompatibleVideoTask(task *ProviderVideoTask, submission bool) (*ProviderVideoTask, error) {
	if task == nil || strings.TrimSpace(task.VideoURL) == "" {
		return task, nil
	}
	parsed, err := url.Parse(task.VideoURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		cause := errors.New("ByteDance video_url must use HTTPS")
		if err != nil {
			cause = fmt.Errorf("ByteDance video_url is invalid: %w", err)
		}
		if submission {
			return nil, unknownVideoProviderError("upstream", "invalid_video_url", "ByteDance video response included an invalid video_url", cause)
		}
		return nil, &VideoProviderError{
			Kind: "upstream", Code: "invalid_video_url",
			Message:   "ByteDance video response included an invalid video_url",
			Retryable: false, Certainty: VideoSubmissionAccepted, Cause: cause,
		}
	}
	return task, nil
}

// ---------------------------------------------------------------------------
// Metadata, transport and errors
// ---------------------------------------------------------------------------

// applyRequestMetadata records what this gateway asked for. The requested
// values are what the quote, hold and settlement were computed from, so they
// are recorded without overwriting anything the upstream reported itself.
func (p *ByteDanceVideoProvider) applyRequestMetadata(task *ProviderVideoTask, request VideoCreateRequest) {
	if task == nil {
		return
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	if _, ok := task.Metadata["model"]; !ok {
		if model := strings.TrimSpace(request.Model); model != "" {
			task.Metadata["model"] = model
		}
	}
	if _, ok := task.Metadata["resolution"]; !ok && strings.TrimSpace(request.Resolution) != "" {
		task.Metadata["resolution"] = strings.TrimSpace(request.Resolution)
	}
	if _, ok := task.Metadata["ratio"]; !ok && strings.TrimSpace(request.Ratio) != "" && request.Ratio != ArkVideoRatioAdaptive {
		task.Metadata["ratio"] = strings.TrimSpace(request.Ratio)
	}
	if _, ok := task.Metadata["seconds"]; !ok && request.Seconds > 0 && !request.SmartDuration && request.Frames <= 0 {
		task.Metadata["seconds"] = request.Seconds
	}
	if _, ok := task.Metadata["seed"]; !ok && request.Seed != nil && *request.Seed >= 0 {
		task.Metadata["seed"] = *request.Seed
	}
}

func byteDanceInlineImageDataURI(ctx context.Context, input VideoInput) (string, error) {
	if input.Open == nil {
		return "", rejectedVideoProviderError("validation", "unsupported_input", "ByteDance image input is not readable", http.StatusBadRequest)
	}
	reader, err := input.Open(ctx)
	if err != nil {
		return "", unknownVideoProviderError("transport", "input_unavailable", "ByteDance image input could not be read", err)
	}
	defer func() { _ = reader.Close() }()
	// Read one byte past the cap so a stream longer than the manifest claims is
	// rejected rather than silently truncated into a corrupt image.
	payload, err := io.ReadAll(io.LimitReader(reader, byteDanceMaxInlineImageBytes+1))
	if err != nil {
		return "", unknownVideoProviderError("transport", "input_unavailable", "ByteDance image input could not be read", err)
	}
	if len(payload) > byteDanceMaxInlineImageBytes {
		return "", rejectedVideoProviderError("validation", "input_too_large", "ByteDance image input is too large to inline", http.StatusBadRequest)
	}
	mimeType := strings.TrimSpace(input.MIMEType)
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload), nil
}

func (p *ByteDanceVideoProvider) do(ctx context.Context, account *Account, method, endpoint string, body io.Reader, contentType, clientToken string) (*http.Response, error) {
	if !p.SupportsAccount(account) {
		return nil, rejectedVideoProviderError("permission", "unsupported_account", "account does not support ByteDance videos", http.StatusForbidden)
	}
	// The endpoint constants for the relay mode carry OpenAI's "/v1" prefix, so
	// that mode needs the OpenAI joiner and its de-duplication of "/v1".
	target := buildByteDanceEndpointURL(p.baseURL(account), endpoint)
	if byteDanceProtocolMode(account) == byteDanceProtocolOpenAICompat {
		target = buildOpenAIEndpointURL(p.baseURL(account), endpoint)
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	req, err := http.NewRequestWithContext(requestCtx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey(account))
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if clientToken = strings.TrimSpace(clientToken); clientToken != "" {
		req.Header.Set("Idempotency-Key", clientToken)
	}
	account.ApplyHeaderOverrides(req.Header)
	trackedRequest, wroteRequest := byteDanceTrackRequestWrite(req)
	response, requestErr := p.execute(trackedRequest, account)
	if requestErr == nil || method != http.MethodPost || clientToken == "" || req.GetBody == nil || wroteRequest.Load() || ctx.Err() != nil {
		return response, requestErr
	}
	retryBody, retryBodyErr := req.GetBody()
	if retryBodyErr != nil {
		return nil, requestErr
	}
	retryRequest := req.Clone(req.Context())
	retryRequest.Body = retryBody
	retryRequest.GetBody = req.GetBody
	return p.execute(retryRequest, account)
}

func byteDanceTrackRequestWrite(request *http.Request) (*http.Request, *atomic.Bool) {
	wroteRequest := &atomic.Bool{}
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
		wroteRequest.Store(true)
	}}
	return request.WithContext(httptrace.WithClientTrace(request.Context(), trace)), wroteRequest
}

func (p *ByteDanceVideoProvider) execute(req *http.Request, account *Account) (*http.Response, error) {
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if p.tlsProfiles == nil {
		return p.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	return p.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, p.tlsProfiles.ResolveTLSProfile(account))
}

// buildByteDanceEndpointURL joins an account base URL with an endpoint path.
// Unlike the OpenAI builder it carries no "/v1" semantics: an Ark base already
// ends in "/api/v3" and the endpoint is appended verbatim, while a base that
// already points at the endpoint is left untouched.
func buildByteDanceEndpointURL(base, endpoint string) string {
	normalized := strings.TrimSpace(base)
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(normalized)
	if err != nil {
		return strings.TrimRight(normalized, "/") + endpoint
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, endpoint) {
		path += endpoint
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (p *ByteDanceVideoProvider) responseError(response *http.Response, account *Account, submission bool, sensitiveValues ...string) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, byteDanceMaxErrorResponse))
	secret := ""
	if account != nil {
		secret = p.apiKey(account)
	}
	message, code := parseOpenAIVideoError(body)
	message = boundedProviderMessage(message, append([]string{secret}, sensitiveValues...)...)
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	code = boundedVideoProviderCode(code)
	kind := "upstream"
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = "authentication"
	case http.StatusForbidden:
		kind = "permission"
	case http.StatusNotFound:
		kind = "not_found"
	case http.StatusConflict:
		kind = "conflict"
	case http.StatusTooManyRequests:
		kind = "rate_limit"
	default:
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			kind = "validation"
		}
	}
	certainty := VideoSubmissionRejected
	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	if submission && retryable {
		certainty = VideoSubmissionUnknown
	}
	return &VideoProviderError{
		Kind: kind, Code: code, Message: message, Retryable: retryable,
		Certainty: certainty, RetryAfter: parseRetryAfter(response.Header), StatusCode: response.StatusCode,
	}
}
