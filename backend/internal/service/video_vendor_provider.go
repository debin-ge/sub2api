package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	videoVendorMaxJSONResponse   = 1 << 20
	videoVendorMaxErrorResponse  = 64 << 10
	videoVendorMaxGetSteps       = 3
	videoVendorContentRedirects  = 3
	videoVendorContentHeadWait   = 30 * time.Second
	videoVendorDefaultPollPeriod = 10 * time.Second
	videoVendorMaxInlineBytes    = MaxContentModerationImageBytes
)

// VideoVendorProvider 是聚合媒体平台共用的 VideoProvider 骨架；每家厂商只提供一个
// videoVendorAdapter。它实现 VideoProvider、VideoSubmissionValidator、VideoCanceller、
// VideoTaskSearcher 与 VideoModelCatalogContributor。
type VideoVendorProvider struct {
	adapter      videoVendorAdapter
	httpUpstream HTTPUpstream
	tlsProfiles  *TLSFingerprintProfileService
	catalog      *VideoCapabilityCatalog
	resolver     videoCallbackIPResolver
	redirect     videoContentRedirectExecutor
	now          func() time.Time
}

// VideoVendorProviders 把已落地的聚合平台 provider 打包交给注册表；wire 以它为注入单位。
type VideoVendorProviders struct {
	Providers []VideoProvider
}

func newVideoVendorProvider(adapter videoVendorAdapter, httpUpstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService) *VideoVendorProvider {
	return &VideoVendorProvider{
		adapter: adapter, httpUpstream: httpUpstream, tlsProfiles: tlsProfiles,
		resolver: net.DefaultResolver, redirect: executePinnedVideoContentRedirect, now: time.Now,
	}
}

// NewVideoVendorProviders 构造全部已落地的聚合平台 provider。
func NewVideoVendorProviders(httpUpstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService, catalog *VideoCapabilityCatalog) VideoVendorProviders {
	names := make([]string, 0, len(videoVendorAdapters))
	for name := range videoVendorAdapters {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]VideoProvider, 0, len(names))
	for _, name := range names {
		provider := newVideoVendorProvider(videoVendorAdapters[name](), httpUpstream, tlsProfiles)
		provider.catalog = catalog
		providers = append(providers, provider)
	}
	return VideoVendorProviders{Providers: providers}
}

func (p *VideoVendorProvider) Name() string {
	if p == nil || p.adapter == nil {
		return ""
	}
	return p.adapter.Name()
}

func (p *VideoVendorProvider) Capabilities() VideoCapabilities {
	if p == nil || p.adapter == nil {
		return VideoCapabilities{}
	}
	if p.catalog != nil {
		if capabilities, ok := p.catalog.Capabilities(p.adapter.Name()); ok {
			return capabilities
		}
	}
	return p.adapter.DefaultCapabilities()
}

func (p *VideoVendorProvider) SupportsAccount(account *Account) bool {
	if p == nil || p.adapter == nil || account == nil {
		return false
	}
	return account.Platform == p.adapter.Name() && account.Type == AccountTypeAPIKey && p.apiKey(account) != ""
}

func (p *VideoVendorProvider) apiKey(account *Account) string {
	if account == nil {
		return ""
	}
	return strings.TrimSpace(account.GetCredential("api_key"))
}

func (p *VideoVendorProvider) baseURL(account *Account) string {
	if account != nil {
		if value := strings.TrimSpace(account.GetCredential("base_url")); value != "" {
			return value
		}
	}
	return p.adapter.DefaultBaseURL()
}

// profileFor 返回模型档案；未知模型回落到厂商兜底档案（可能为 nil）。
func (p *VideoVendorProvider) profileFor(model string) *videoModelProfile {
	key := strings.ToLower(strings.TrimSpace(model))
	if profile := p.adapter.Profiles()[key]; profile != nil {
		return profile
	}
	return p.adapter.FallbackProfile(key)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func (p *VideoVendorProvider) ValidateSubmission(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	_, _, err := p.validateAndPlan(account, request, inputs)
	return err
}

// videoVendorPlan 是校验后的提交计划：选定档案、归一化后的素材清单（尚未读取字节）、
// 时长与 Ark 维度的画幅（resolution 为 Ark 档位，adapter 再换成厂商词表）。
type videoVendorPlan struct {
	profile    *videoModelProfile
	images     []videoVendorPlanImage
	seconds    int
	resolution string
	ratio      string
}

// videoVendorPlanImage 是一张起止帧图：远端 URL 或 spool 字节。
type videoVendorPlanImage struct {
	role  VideoInputRole
	url   string
	input *VideoInput
}

func (p *VideoVendorProvider) validateAndPlan(account *Account, request VideoCreateRequest, inputs []VideoInput) (*videoVendorPlan, *videoModelProfile, error) {
	if !p.SupportsAccount(account) {
		return nil, nil, rejectedVideoProviderError("permission", "unsupported_account", "account does not support "+p.Name()+" videos", http.StatusForbidden)
	}
	if normalizeVideoOperation(request.Operation) != VideoOperationGenerate {
		return nil, nil, rejectedVideoProviderError("validation", "unsupported_operation", p.Name()+" supports generate only", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, nil, videoVendorValidationError("missing_prompt", "prompt is required")
	}
	if request.HasOpenAIOnlyFields() || request.ParentTask != nil {
		return nil, nil, videoVendorValidationError("unsupported_option", "size, quality, aspect_ratio, input_reference, characters and parent tasks are not supported by %s", p.Name())
	}
	if request.SmartDuration || request.Frames > 0 {
		return nil, nil, videoVendorValidationError("unsupported_parameter", "duration -1 and frames are not supported by %s", p.Name())
	}
	if request.ReturnLastFrame != nil || request.ExecutionExpiresAfter != 0 || strings.TrimSpace(request.ServiceTier) != "" {
		return nil, nil, videoVendorValidationError("unsupported_parameter", "return_last_frame, service_tier and execution_expires_after are not supported by %s", p.Name())
	}
	profile := p.profileFor(request.Model)
	if profile == nil {
		return nil, nil, videoVendorValidationError("unsupported_model", "model %q is not supported by %s", request.Model, p.Name())
	}
	profile.finalizeArkDimensions()

	// 起止帧：content[] 里的 first_frame / last_frame（URL 或字节），总数受档案 MaxImages 约束。
	images := make([]videoVendorPlanImage, 0, 2)
	var first, last *videoVendorPlanImage
	for i := range request.Content {
		item := request.Content[i]
		if !item.IsMedia() {
			continue
		}
		switch item.Role {
		case VideoInputRoleFirstFrame, VideoInputRoleReferenceImage:
			if first != nil {
				return nil, nil, videoVendorValidationError("unsupported_reference_media", "only one first_frame image is accepted")
			}
			first = &videoVendorPlanImage{role: VideoInputRoleFirstFrame, url: strings.TrimSpace(item.URL), input: item.Input}
		case VideoInputRoleLastFrame:
			if last != nil {
				return nil, nil, videoVendorValidationError("unsupported_reference_media", "only one last_frame image is accepted")
			}
			last = &videoVendorPlanImage{role: VideoInputRoleLastFrame, url: strings.TrimSpace(item.URL), input: item.Input}
		default:
			return nil, nil, videoVendorValidationError("unsupported_reference_media", "content role %q is not supported by %s yet", item.Role, p.Name())
		}
	}
	// 没挂到 content 上的 spool 输入（旧式 multipart）按首帧、尾帧顺序补位。
	for i := range inputs {
		if videoVendorInputBound(request.Content, &inputs[i]) {
			continue
		}
		image := &videoVendorPlanImage{role: inputs[i].Role, input: &inputs[i]}
		switch {
		case first == nil:
			image.role = VideoInputRoleFirstFrame
			first = image
		case last == nil:
			image.role = VideoInputRoleLastFrame
			last = image
		default:
			return nil, nil, videoVendorValidationError("unsupported_reference_media", "model %q accepts at most 2 image(s)", request.Model)
		}
	}
	if last != nil && first == nil {
		return nil, nil, videoVendorValidationError("unsupported_reference_media", "last_frame requires a first_frame image")
	}
	if first != nil {
		images = append(images, *first)
	}
	if last != nil {
		images = append(images, *last)
	}
	for _, image := range images {
		if image.input != nil {
			if !image.input.Role.IsImageContentRole() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(image.input.MIMEType)), "image/") {
				return nil, nil, videoVendorValidationError("unsupported_input", "input role %q is not supported by %s", image.input.Role, p.Name())
			}
			if image.input.Size > videoVendorMaxInlineBytes {
				return nil, nil, rejectedVideoProviderError("validation", "input_too_large", "image input is too large to inline", http.StatusBadRequest)
			}
			continue
		}
		if !strings.HasPrefix(strings.ToLower(image.url), "https://") {
			return nil, nil, videoVendorValidationError("unsupported_reference_media", "image references must be https URLs or inline data URIs")
		}
		if len(image.url) > openAIVideoMaxReferenceBytes {
			return nil, nil, videoVendorValidationError("unsupported_reference_media", "image reference is too large")
		}
	}
	imageCount := len(images)
	if imageCount > 0 && !profile.AllowsImage {
		return nil, nil, videoVendorValidationError("unsupported_reference_media", "model %q does not accept image input", request.Model)
	}
	if imageCount == 0 && profile.RequiresImage {
		return nil, nil, videoVendorValidationError("missing_reference_media", "model %q requires an input image", request.Model)
	}
	maxImages := profile.MaxImages
	if maxImages <= 0 && profile.AllowsImage {
		maxImages = 1
	}
	if imageCount > maxImages {
		return nil, nil, videoVendorValidationError("unsupported_reference_media", "model %q accepts at most %d image(s)", request.Model, maxImages)
	}

	seconds := request.Seconds
	if seconds <= 0 {
		seconds = profile.DefaultSeconds
	}
	if !profile.supportsSeconds(seconds) {
		return nil, nil, videoVendorValidationError("unsupported_seconds", "model %q does not support %d second videos", request.Model, seconds)
	}
	resolution := strings.ToLower(strings.TrimSpace(request.Resolution))
	if resolution == "" && profile.SizeEnc != videoSizeNotSupported && profile.SizeEnc != videoSizeAspectRatio {
		resolution = profile.DefaultResolution
	}
	if resolution != "" {
		if !profile.supportsResolution(resolution) {
			return nil, nil, videoVendorValidationError("unsupported_resolution", "model %q does not support resolution %s", request.Model, resolution)
		}
		if _, ok := profile.vendorResolutionLabel(resolution); !ok && profile.SizeEnc != videoSizeResolutionInID && profile.SizeEnc != videoSizeNotSupported {
			return nil, nil, videoVendorValidationError("unsupported_resolution", "model %q does not support resolution %s", request.Model, resolution)
		}
	}
	ratio := strings.ToLower(strings.TrimSpace(request.Ratio))
	if ratio == ArkVideoRatioAdaptive {
		if imageCount == 0 {
			return nil, nil, videoVendorValidationError("unsupported_ratio", "ratio adaptive requires a first_frame image")
		}
		ratio = ""
	}
	if ratio == "" && imageCount == 0 && profile.SizeEnc != videoSizeNotSupported && profile.SizeEnc != videoSizeResolutionOnly {
		ratio = profile.DefaultRatio
	}
	if ratio != "" && !profile.supportsRatio(ratio) {
		return nil, nil, videoVendorValidationError("unsupported_ratio", "model %q does not support ratio %s", request.Model, ratio)
	}
	if imageCount > 0 {
		// 图生视频比例随图：显式比例会被厂商忽略，不送也不记。
		ratio = ""
	}
	if err := videoVendorApplyArkFields(map[string]any{}, profile, request); err != nil {
		return nil, nil, err
	}
	for key, raw := range request.ProviderOptions {
		if _, ok := profile.OptionWhitelist[key]; !ok {
			return nil, nil, videoVendorValidationError("unsupported_option", "provider_options.%s is not supported by model %q", key, request.Model)
		}
		if _, ok := videoVendorOptionValue(raw); !ok {
			return nil, nil, videoVendorValidationError("unsupported_option", "provider_options.%s must be a scalar value", key)
		}
	}
	return &videoVendorPlan{profile: profile, images: images, seconds: seconds, resolution: resolution, ratio: ratio}, profile, nil
}

func videoVendorInputBound(content []VideoContentItem, input *VideoInput) bool {
	for _, item := range content {
		if item.Input != nil && item.Input.SHA256 == input.SHA256 && item.Input.Role == input.Role {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Provider operations
// ---------------------------------------------------------------------------

func (p *VideoVendorProvider) Create(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	plan, profile, err := p.validateAndPlan(account, request, inputs)
	if err != nil {
		return nil, err
	}
	request.Seconds = plan.seconds
	request.Resolution = plan.resolution
	request.Ratio = plan.ratio
	vendorInputs := make([]videoVendorInput, 0, len(plan.images))
	for _, image := range plan.images {
		if image.input == nil {
			vendorInputs = append(vendorInputs, videoVendorInput{Role: image.role, URL: image.url})
			continue
		}
		data, err := p.readInput(ctx, *image.input)
		if err != nil {
			return nil, err
		}
		vendorInputs = append(vendorInputs, videoVendorInput{Role: image.role, MIMEType: image.input.MIMEType, Data: data})
	}
	if uploader, ok := p.adapter.(videoVendorInputUploader); ok {
		for i := range vendorInputs {
			if len(vendorInputs[i].Data) == 0 {
				continue
			}
			uploadURL, err := p.uploadInput(ctx, account, uploader, vendorInputs[i])
			if err != nil {
				return nil, err
			}
			vendorInputs[i].URL = uploadURL
			vendorInputs[i].Data = nil
		}
	}
	clientToken := p.adapter.ClientTokenFor(strings.TrimSpace(request.ClientToken))
	upstream, err := p.adapter.BuildCreate(request, profile, vendorInputs, clientToken)
	if err != nil {
		return nil, err
	}
	response, err := p.do(ctx, account, upstream, clientToken, true)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", p.Name()+" video submission transport failed", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, true, request.Prompt)
	}
	task, err := p.adapter.DecodeCreate(response, request, clientToken)
	if err != nil {
		return nil, err
	}
	p.applyRequestMetadata(task, request)
	return task, nil
}

// uploadInput 把素材字节上传到厂商媒体库换取 URL。上传失败不会产生计费，一律按
// rejected / 可重试的传输错误处理，不进入 submission_unknown。
func (p *VideoVendorProvider) uploadInput(ctx context.Context, account *Account, uploader videoVendorInputUploader, input videoVendorInput) (string, error) {
	upstream, err := uploader.BuildUpload(input)
	if err != nil {
		return "", err
	}
	response, err := p.do(ctx, account, upstream, "", false)
	if err != nil {
		return "", &VideoProviderError{Kind: "transport", Code: "upload_failed", Message: p.Name() + " image upload transport failed", Retryable: true, Certainty: VideoSubmissionRejected, Cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		providerErr := p.responseError(response, account, false)
		if typed, ok := providerErr.(*VideoProviderError); ok {
			typed.Certainty = VideoSubmissionRejected
			if typed.Code == "" || strings.HasPrefix(typed.Code, "http_") {
				typed.Code = "upload_failed"
			}
		}
		return "", providerErr
	}
	uploadURL, err := uploader.DecodeUpload(response)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.ToLower(uploadURL), "https://") {
		return "", rejectedVideoProviderError("upstream", "upload_failed", p.Name()+" image upload returned an invalid url", http.StatusBadGateway)
	}
	return uploadURL, nil
}

func (p *VideoVendorProvider) readInput(ctx context.Context, input VideoInput) ([]byte, error) {
	if input.Open == nil {
		return nil, rejectedVideoProviderError("validation", "unsupported_input", p.Name()+" image input is not readable", http.StatusBadRequest)
	}
	reader, err := input.Open(ctx)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "input_unavailable", p.Name()+" image input could not be read", err)
	}
	defer func() { _ = reader.Close() }()
	// 多读一个字节：流比清单声明的更长时拒绝，而不是截断成坏图。
	payload, err := io.ReadAll(io.LimitReader(reader, int64(videoVendorMaxInlineBytes)+1))
	if err != nil {
		return nil, unknownVideoProviderError("transport", "input_unavailable", p.Name()+" image input could not be read", err)
	}
	if len(payload) > videoVendorMaxInlineBytes {
		return nil, rejectedVideoProviderError("validation", "input_too_large", p.Name()+" image input is too large to inline", http.StatusBadRequest)
	}
	return payload, nil
}

func (p *VideoVendorProvider) Get(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	if p == nil || ref.Provider != p.Name() {
		return nil, ErrVideoProviderUnsupported
	}
	upstream, err := p.adapter.BuildGet(strings.TrimSpace(ref.ProviderTaskID))
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	for step := 0; step < videoVendorMaxGetSteps; step++ {
		response, err := p.do(ctx, account, upstream, "", false)
		if err != nil {
			return nil, controlVideoProviderError(err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, p.responseError(response, account, false)
		}
		task, next, err := p.adapter.DecodeGet(response, strings.TrimSpace(ref.ProviderTaskID), step)
		if err != nil {
			return nil, err
		}
		if next == nil {
			if task == nil {
				return nil, controlVideoProviderError(errors.New(p.Name() + " video poll returned no task"))
			}
			if task.SuggestedPollInterval <= 0 {
				task.SuggestedPollInterval = p.adapter.DefaultPollInterval()
			}
			p.applyContentExpiry(task)
			return task, nil
		}
		upstream = next
	}
	return nil, controlVideoProviderError(errors.New(p.Name() + " video poll exceeded the step limit"))
}

func (p *VideoVendorProvider) SearchByClientToken(ctx context.Context, account *Account, clientToken string) (*ProviderVideoTask, error) {
	clientToken = strings.TrimSpace(clientToken)
	if p == nil || clientToken == "" {
		return nil, ErrVideoInvalidRequest
	}
	vendorToken := p.adapter.ClientTokenFor(clientToken)
	upstream, ok := p.adapter.BuildSearch(vendorToken)
	if !ok {
		return nil, nil
	}
	response, err := p.do(ctx, account, upstream, "", false)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	task, err := p.adapter.DecodeSearch(response, vendorToken)
	if err != nil || task == nil {
		return nil, err
	}
	p.applyContentExpiry(task)
	return task, nil
}

func (p *VideoVendorProvider) Delete(ctx context.Context, account *Account, ref ProviderTaskRef) error {
	if p == nil || ref.Provider != p.Name() {
		return ErrVideoProviderUnsupported
	}
	upstream, ok := p.adapter.BuildDelete(strings.TrimSpace(ref.ProviderTaskID))
	if !ok {
		// 厂商没有删除接口：本地记录照常删除，上游记录由厂商按自己的保留期清理。
		return nil
	}
	return p.terminate(ctx, account, upstream)
}

func (p *VideoVendorProvider) Cancel(ctx context.Context, account *Account, ref ProviderTaskRef) error {
	if p == nil || ref.Provider != p.Name() {
		return ErrVideoProviderUnsupported
	}
	upstream, ok := p.adapter.BuildCancel(strings.TrimSpace(ref.ProviderTaskID))
	if !ok {
		return ErrVideoCapabilityUnsupported
	}
	return p.terminate(ctx, account, upstream)
}

func (p *VideoVendorProvider) terminate(ctx context.Context, account *Account, upstream *vendorHTTPRequest) error {
	response, err := p.do(ctx, account, upstream, "", false)
	if err != nil {
		return controlVideoProviderError(err)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if judge, ok := p.adapter.(videoVendorTerminateJudge); ok && judge.TerminateSucceeded(response.StatusCode, response.Body) {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return p.responseError(response, account, false)
	}
	return nil
}

// applyRequestMetadata 补写结算与规格对账需要的元数据；上游已返回的键不覆盖。
func (p *VideoVendorProvider) applyRequestMetadata(task *ProviderVideoTask, request VideoCreateRequest) {
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
	if _, ok := task.Metadata["seconds"]; !ok && request.Seconds > 0 {
		task.Metadata["seconds"] = request.Seconds
	}
	if _, ok := task.Metadata["resolution"]; !ok && strings.TrimSpace(request.Resolution) != "" {
		task.Metadata["resolution"] = strings.TrimSpace(request.Resolution)
	}
	if _, ok := task.Metadata["ratio"]; !ok && strings.TrimSpace(request.Ratio) != "" {
		task.Metadata["ratio"] = strings.TrimSpace(request.Ratio)
	}
	if _, ok := task.Metadata["seed"]; !ok && request.Seed != nil && *request.Seed >= 0 {
		task.Metadata["seed"] = *request.Seed
	}
}

// applyContentExpiry 给已完成任务补内容有效期；厂商 CDN URL 都有保留期。
func (p *VideoVendorProvider) applyContentExpiry(task *ProviderVideoTask) {
	if task == nil || task.Status != VideoGenerationCompleted || task.VideoURL == "" || task.ContentExpiresAt != nil {
		return
	}
	finished := task.ProviderFinishedAt
	if finished == nil {
		now := p.now().UTC()
		finished = &now
		task.ProviderFinishedAt = finished
	}
	if ttl := p.adapter.ContentTTL(); ttl > 0 {
		expires := finished.Add(ttl)
		task.ContentExpiresAt = &expires
	}
}

// EstimateUpstreamCost 在提交前向厂商询价（VideoUpstreamCostEstimator）。只对 URL 形式
// 的素材询价：spool 上传的图片此时还没有厂商 URL，带图模型直接返回 ok=false。
func (p *VideoVendorProvider) EstimateUpstreamCost(ctx context.Context, account *Account, request VideoCreateRequest) (float64, bool, error) {
	estimator, ok := p.adapter.(videoVendorCostEstimator)
	if !ok {
		return 0, false, nil
	}
	plan, profile, err := p.validateAndPlan(account, request, nil)
	if err != nil {
		return 0, false, err
	}
	inputs := make([]videoVendorInput, 0, len(plan.images))
	for _, image := range plan.images {
		if image.input != nil {
			// spool 字节此时还没有厂商 URL，无法询价。
			return 0, false, nil
		}
		inputs = append(inputs, videoVendorInput{Role: image.role, URL: image.url})
	}
	if profile.RequiresImage && len(inputs) == 0 {
		return 0, false, nil
	}
	request.Seconds = plan.seconds
	request.Resolution = plan.resolution
	request.Ratio = plan.ratio
	upstream, ok := estimator.BuildEstimate(request, profile, inputs)
	if !ok {
		return 0, false, nil
	}
	response, err := p.do(ctx, account, upstream, "", false)
	if err != nil {
		return 0, false, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, false, p.responseError(response, account, false)
	}
	price, err := estimator.DecodeEstimate(response)
	if err != nil {
		return 0, false, err
	}
	return price, true, nil
}

// ---------------------------------------------------------------------------
// Content proxy（与 ByteDance 版同构：HTTPS、IP 钉死、受限跳转、Range 透传）
// ---------------------------------------------------------------------------

func (p *VideoVendorProvider) OpenContent(ctx context.Context, account *Account, request ProviderContentRequest) (*ProviderContent, error) {
	if p == nil || request.TaskRef.Provider != p.Name() {
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
		return nil, rejectedVideoProviderError("validation", "content_unavailable", p.Name()+" video content requires a stored upstream url", http.StatusConflict)
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
		return nil, controlVideoProviderError(errors.New(p.Name() + " video url must use HTTPS"))
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
		body, _ := io.ReadAll(io.LimitReader(response.Body, videoVendorMaxErrorResponse))
		return nil, p.responseError(&vendorHTTPResponse{StatusCode: response.StatusCode, Header: response.Header, Body: body}, account, false)
	}
	return &ProviderContent{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: response.Body}, nil
}

func (p *VideoVendorProvider) executeContent(ctx context.Context, request *http.Request, account *Account, responseHeaderTimeout time.Duration, addresses []netip.Addr) (*http.Response, error) {
	redirect := p.redirect
	if redirect == nil {
		redirect = executePinnedVideoContentRedirect
	}
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = videoVendorContentHeadWait
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
		if hop >= videoVendorContentRedirects {
			return nil, errors.New(p.Name() + " video content exceeded the redirect limit")
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
// HTTP（与 ByteDance 版同构：代理、TLS 指纹、头覆盖、token 落点、未写出重传）
// ---------------------------------------------------------------------------

// buildVendorURL 把账号 base_url 与相对路径拼起来；路径已含 base 尾段时不重复。
func buildVendorURL(base, path string, query url.Values) string {
	normalized := strings.TrimSpace(base)
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" {
		target := strings.TrimRight(normalized, "/")
		if path = strings.TrimSpace(path); path != "" {
			target += "/" + strings.TrimLeft(path, "/")
		}
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
		return target
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if path = strings.TrimSpace(path); path != "" {
		path = "/" + strings.TrimLeft(path, "/")
		if !strings.HasSuffix(basePath, path) {
			basePath += path
		}
	}
	if basePath == "" {
		basePath = "/"
	}
	parsed.Path = basePath
	parsed.RawPath = ""
	parsed.Fragment = ""
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (p *VideoVendorProvider) do(ctx context.Context, account *Account, upstream *vendorHTTPRequest, clientToken string, submission bool) (*vendorHTTPResponse, error) {
	if upstream == nil {
		return nil, errors.New(p.Name() + " upstream request is missing")
	}
	if !p.SupportsAccount(account) {
		return nil, rejectedVideoProviderError("permission", "unsupported_account", "account does not support "+p.Name()+" videos", http.StatusForbidden)
	}
	target := buildVendorURL(p.baseURL(account), upstream.Path, upstream.Query)
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	var body io.Reader
	if len(upstream.Body) > 0 {
		body = bytes.NewReader(upstream.Body)
	}
	req, err := http.NewRequestWithContext(requestCtx, upstream.Method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", p.adapter.AuthorizationHeader(p.apiKey(account)))
	req.Header.Set("Accept", "application/json")
	if upstream.ContentType != "" {
		req.Header.Set("Content-Type", upstream.ContentType)
	}
	for key, values := range upstream.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if mode := p.adapter.ClientTokenMode(); submission && mode.Kind == videoClientTokenHeader && mode.Name != "" && clientToken != "" {
		req.Header.Set(mode.Name, clientToken)
	}
	account.ApplyHeaderOverrides(req.Header)
	trackedRequest, wroteRequest := videoVendorTrackRequestWrite(req)
	response, requestErr := p.execute(trackedRequest, account)
	// 请求体尚未写出就失败（连接阶段）时重传一次是安全的：上游不可能已经受理。
	if requestErr != nil && upstream.Method == http.MethodPost && req.GetBody != nil && !wroteRequest.Load() && ctx.Err() == nil {
		retryBody, retryBodyErr := req.GetBody()
		if retryBodyErr == nil {
			retryRequest := req.Clone(req.Context())
			retryRequest.Body = retryBody
			retryRequest.GetBody = req.GetBody
			response, requestErr = p.execute(retryRequest, account)
		}
	}
	if requestErr != nil {
		return nil, requestErr
	}
	defer func() { _ = response.Body.Close() }()
	limit := int64(videoVendorMaxJSONResponse)
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("%s response exceeds %d bytes", p.Name(), limit)
	}
	return &vendorHTTPResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: payload}, nil
}

func videoVendorTrackRequestWrite(request *http.Request) (*http.Request, *atomic.Bool) {
	wroteRequest := &atomic.Bool{}
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
		wroteRequest.Store(true)
	}}
	return request.WithContext(httptrace.WithClientTrace(request.Context(), trace)), wroteRequest
}

func (p *VideoVendorProvider) execute(req *http.Request, account *Account) (*http.Response, error) {
	if p.httpUpstream == nil {
		return nil, errors.New(p.Name() + " http upstream is not configured")
	}
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if p.tlsProfiles == nil {
		return p.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	return p.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, p.tlsProfiles.ResolveTLSProfile(account))
}

// responseError 先按 HTTP 状态分类（kind / certainty / retryable / Retry-After），再叠加
// adapter 识别出的厂商业务码。
func (p *VideoVendorProvider) responseError(response *vendorHTTPResponse, account *Account, submission bool, sensitiveValues ...string) error {
	secret := ""
	if account != nil {
		secret = p.apiKey(account)
	}
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
	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	code := ""
	message := ""
	if vendorKind, vendorCode, vendorMessage, vendorRetryable, known := p.adapter.ClassifyError(response.StatusCode, response.Body); known {
		if vendorKind != "" {
			kind = vendorKind
		}
		code, message, retryable = vendorCode, vendorMessage, vendorRetryable
	}
	message = boundedProviderMessage(message, append([]string{secret}, sensitiveValues...)...)
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	if code == "" {
		code = "http_" + fmt.Sprint(response.StatusCode)
	}
	certainty := VideoSubmissionRejected
	if submission && retryable {
		certainty = VideoSubmissionUnknown
	}
	return &VideoProviderError{
		Kind: kind, Code: boundedVideoProviderCode(code), Message: message, Retryable: retryable,
		Certainty: certainty, RetryAfter: parseRetryAfter(response.Header), StatusCode: response.StatusCode,
	}
}
