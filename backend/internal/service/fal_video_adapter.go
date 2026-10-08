package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// fal.ai 视频适配器。
//
// 协议要点（https://fal.ai/docs/documentation/model-apis/inference/queue）：
//   - 提交：POST {base}/{endpoint_id}，endpoint_id 是完整的多段模型 ID；响应
//     {request_id, status_url, response_url, cancel_url, queue_position}。
//   - 状态 / 结果 / 取消 URL 只保留 endpoint_id 的前两段（owner/alias），后面接
//     /requests/{request_id}[/status|/cancel]（与官方 JS 客户端 queue.ts 一致）。本适配器
//     把 ProviderTaskID 存成 "owner/alias/requests/{request_id}"，轮询时直接拼在 base 后。
//   - 状态词表 IN_QUEUE / IN_PROGRESS / COMPLETED；COMPLETED 且无 error 时再取结果，
//     结果体按模型不同，视频 URL 在 video.url。
//   - 没有幂等键；取消用 PUT …/cancel（202 已请求、400 已完成、404 不存在）；5xx 不计费，
//     422 计费。结果响应头 X-Fal-Billable-Units 给出计费单位数。
//   - 提交时带 X-Fal-Object-Lifecycle-Preference 把输出保留期显式设为 7 天。
const (
	falContentTTL           = 7 * 24 * time.Hour
	falDefaultPoll          = videoVendorDefaultPollPeriod
	falLifecycleHeader      = "X-Fal-Object-Lifecycle-Preference"
	falQueuePriorityHeader  = "X-Fal-Queue-Priority"
	falBillableUnitsHeader  = "X-Fal-Billable-Units"
	falErrorTypeHeader      = "X-Fal-Error-Type"
	falRequestsSegment      = "/requests/"
	falStatusInQueue        = "IN_QUEUE"
	falStatusInProgress     = "IN_PROGRESS"
	falStatusCompleted      = "COMPLETED"
	falProbeAppID           = "fal-ai/kling-video"
	falProbeRequestID       = "00000000-0000-4000-8000-000000000000"
	falObjectLifecycleValue = `{"expiration_duration_seconds":604800}`
)

type falVideoAdapter struct {
	profiles map[string]*videoModelProfile
}

func newFalVideoAdapter() *falVideoAdapter {
	return &falVideoAdapter{profiles: falVideoProfiles()}
}

func (a *falVideoAdapter) Name() string           { return VideoProviderFal }
func (a *falVideoAdapter) DefaultBaseURL() string { return falDefaultBaseURL }
func (a *falVideoAdapter) AuthorizationHeader(apiKey string) string {
	return "Key " + apiKey
}
func (a *falVideoAdapter) ClientTokenMode() videoClientTokenMode {
	return videoClientTokenMode{Kind: videoClientTokenNone}
}
func (a *falVideoAdapter) ClientTokenFor(localToken string) string { return localToken }
func (a *falVideoAdapter) Profiles() map[string]*videoModelProfile { return a.profiles }
func (a *falVideoAdapter) FreeSecondsRange() (int, int)            { return 1, 20 }
func (a *falVideoAdapter) DefaultPollInterval() time.Duration      { return falDefaultPoll }
func (a *falVideoAdapter) ContentTTL() time.Duration               { return falContentTTL }
func (a *falVideoAdapter) FallbackProfile(model string) *videoModelProfile {
	return falFallbackProfile(model)
}

func (a *falVideoAdapter) DefaultCapabilities() VideoCapabilities {
	return videoVendorCapabilitiesFromProfiles(falDefaultVideoModel, a.profiles, map[VideoCapability]bool{
		VideoCapabilityCreate: true, VideoCapabilityInputReference: true, VideoCapabilityCancel: true,
	})
}

// falAppID 取 endpoint_id 的前两段（owner/alias）。
func falAppID(endpointID string) (string, bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(endpointID), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// falTaskID 把 owner/alias 与 request_id 编码成 ProviderTaskID。
func falTaskID(appID, requestID string) string {
	return appID + falRequestsSegment[:len(falRequestsSegment)-1] + "/" + requestID
}

// falSplitTaskID 校验并拆开 ProviderTaskID："owner/alias/requests/<id>"。
func falSplitTaskID(providerTaskID string) (string, string, bool) {
	providerTaskID = strings.Trim(strings.TrimSpace(providerTaskID), "/")
	index := strings.Index(providerTaskID, falRequestsSegment)
	if index <= 0 {
		return "", "", false
	}
	appID := providerTaskID[:index]
	requestID := providerTaskID[index+len(falRequestsSegment):]
	if _, ok := falAppID(appID); !ok || requestID == "" || strings.Contains(requestID, "/") {
		return "", "", false
	}
	return appID, requestID, true
}

// ---------------------------------------------------------------------------
// Wire format
// ---------------------------------------------------------------------------

type falSubmitResponse struct {
	RequestID     string `json:"request_id"`
	ResponseURL   string `json:"response_url"`
	StatusURL     string `json:"status_url"`
	CancelURL     string `json:"cancel_url"`
	QueuePosition *int   `json:"queue_position"`
}

type falStatusResponse struct {
	Status        string         `json:"status"`
	QueuePosition *int           `json:"queue_position"`
	Error         any            `json:"error"`
	ErrorType     string         `json:"error_type"`
	Metrics       map[string]any `json:"metrics"`
}

func (a *falVideoAdapter) BuildCreate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput, _ string) (*vendorHTTPRequest, error) {
	if profile == nil {
		return nil, videoVendorValidationError("unsupported_model", "model %q is not supported by fal", request.Model)
	}
	endpoint := profile.Endpoint
	if endpoint == "" {
		endpoint = strings.TrimSpace(request.Model)
	}
	if _, ok := falAppID(endpoint); !ok {
		return nil, videoVendorValidationError("unsupported_model", "fal endpoint %q must be owner/alias[/path]", endpoint)
	}
	body := map[string]any{"prompt": request.Prompt}
	for key, value := range profile.Static {
		videoVendorSetPath(body, key, value)
	}
	durationField := profile.DurationField
	if durationField == "" {
		durationField = "duration"
	}
	if request.Seconds > 0 {
		videoVendorSetPath(body, durationField, videoVendorEncodeDuration(profile, request.Seconds))
	}
	imageField := profile.ImageField
	if imageField == "" {
		imageField = "image_url"
	}
	lastImageField := profile.LastImageField
	if lastImageField == "" {
		lastImageField = "end_image_url"
	}
	for i, input := range inputs {
		switch i {
		case 0:
			videoVendorSetPath(body, imageField, input.dataURI())
		case 1:
			videoVendorSetPath(body, lastImageField, input.dataURI())
		}
	}
	aspectField := profile.AspectRatioField
	if aspectField == "" {
		aspectField = "aspect_ratio"
	}
	resolutionField := profile.ResolutionField
	if resolutionField == "" {
		resolutionField = "resolution"
	}
	if size := strings.TrimSpace(request.Size); size != "" {
		switch profile.SizeEnc {
		case videoSizeAspectRatioRes:
			if aspect := videoVendorAspectRatioLabel(size); aspect != "" && len(inputs) == 0 {
				videoVendorSetPath(body, aspectField, aspect)
			}
			if resolution := videoVendorResolutionFor(profile, size); resolution != "" {
				videoVendorSetPath(body, resolutionField, resolution)
			}
		case videoSizeAspectRatio:
			if aspect := videoVendorAspectRatioLabel(size); aspect != "" && len(inputs) == 0 {
				videoVendorSetPath(body, aspectField, aspect)
			}
		case videoSizeResolutionOnly:
			if resolution := videoVendorResolutionFor(profile, size); resolution != "" {
				videoVendorSetPath(body, resolutionField, resolution)
			}
		}
	}
	if request.AudioEnabled != nil && profile.AudioField != "" {
		videoVendorSetPath(body, profile.AudioField, *request.AudioEnabled)
	}
	for key, raw := range request.ProviderOptions {
		path, ok := profile.OptionWhitelist[key]
		if !ok {
			return nil, videoVendorValidationError("unsupported_option", "provider_options.%s is not supported by model %q", key, request.Model)
		}
		value, ok := videoVendorOptionValue(raw)
		if !ok {
			return nil, videoVendorValidationError("unsupported_option", "provider_options.%s must be a scalar value", key)
		}
		videoVendorSetPath(body, path, value)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, videoVendorValidationError("invalid_payload", "video payload could not be encoded")
	}
	headers := http.Header{}
	headers.Set(falLifecycleHeader, falObjectLifecycleValue)
	headers.Set(falQueuePriorityHeader, "normal")
	return &vendorHTTPRequest{
		Method: http.MethodPost, Path: "/" + strings.TrimLeft(endpoint, "/"), Body: payload,
		ContentType: "application/json", Headers: headers,
	}, nil
}

func (a *falVideoAdapter) DecodeCreate(response *vendorHTTPResponse, request VideoCreateRequest, _ string) (*ProviderVideoTask, error) {
	var submitted falSubmitResponse
	if err := json.Unmarshal(response.Body, &submitted); err != nil {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "fal video response could not be decoded", err)
	}
	requestID := strings.TrimSpace(submitted.RequestID)
	if !validVideoProviderIdentifier(requestID) || strings.Contains(requestID, "/") {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "fal video response is missing a request id", nil)
	}
	endpoint := strings.TrimSpace(request.Model)
	if profile := a.profiles[strings.ToLower(endpoint)]; profile != nil && profile.Endpoint != "" {
		endpoint = profile.Endpoint
	}
	appID, ok := falAppID(endpoint)
	if !ok {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "fal endpoint id is not owner/alias[/path]", nil)
	}
	now := time.Now().UTC()
	return &ProviderVideoTask{
		ProviderTaskID: falTaskID(appID, requestID), Status: VideoGenerationQueued, RawStatus: falStatusInQueue,
		Metadata: map[string]any{}, SuggestedPollInterval: falDefaultPoll, ProviderCreatedAt: &now,
	}, nil
}

func (a *falVideoAdapter) BuildGet(providerTaskID string) (*vendorHTTPRequest, error) {
	if _, _, ok := falSplitTaskID(providerTaskID); !ok {
		return nil, fmt.Errorf("fal task id %q is not owner/alias/requests/<id>", providerTaskID)
	}
	return &vendorHTTPRequest{Method: http.MethodGet, Path: "/" + providerTaskID + "/status", Query: url.Values{"logs": {"0"}}}, nil
}

func (a *falVideoAdapter) DecodeGet(response *vendorHTTPResponse, providerTaskID string, step int) (*ProviderVideoTask, *vendorHTTPRequest, error) {
	if step == 0 {
		return a.decodeStatus(response, providerTaskID)
	}
	return a.decodeResult(response, providerTaskID)
}

func (a *falVideoAdapter) decodeStatus(response *vendorHTTPResponse, providerTaskID string) (*ProviderVideoTask, *vendorHTTPRequest, error) {
	var status falStatusResponse
	if err := json.Unmarshal(response.Body, &status); err != nil {
		return nil, nil, controlVideoProviderError(err)
	}
	task := &ProviderVideoTask{
		ProviderTaskID: providerTaskID, RawStatus: strings.TrimSpace(status.Status),
		Metadata: map[string]any{}, SuggestedPollInterval: falDefaultPoll,
	}
	switch strings.ToUpper(strings.TrimSpace(status.Status)) {
	case falStatusInQueue:
		task.Status = VideoGenerationQueued
		return task, nil, nil
	case falStatusInProgress:
		task.Status = VideoGenerationInProgress
		return task, nil, nil
	case falStatusCompleted:
		if message := falErrorMessage(status.Error); message != "" || strings.TrimSpace(status.ErrorType) != "" {
			task.Status = VideoGenerationFailed
			task.ErrorCode = boundedVideoProviderCode(falErrorCode(status.ErrorType, "generation_failed"))
			task.ErrorMessage = boundedProviderMessage(message)
			if task.ErrorMessage == "" {
				task.ErrorMessage = "fal reported the request as failed"
			}
			now := time.Now().UTC()
			task.ProviderFinishedAt = &now
			return task, nil, nil
		}
		if inference, ok := numericMapValue(status.Metrics, "inference_time"); ok && inference > 0 {
			task.Usage = map[string]any{"inference_seconds": inference}
		}
		// 完成且无错误：再取一次结果。
		return task, &vendorHTTPRequest{Method: http.MethodGet, Path: "/" + providerTaskID}, nil
	default:
		return nil, nil, controlVideoProviderError(fmt.Errorf("fal status %q is not recognized", status.Status))
	}
}

func (a *falVideoAdapter) decodeResult(response *vendorHTTPResponse, providerTaskID string) (*ProviderVideoTask, *vendorHTTPRequest, error) {
	var result any
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return nil, nil, controlVideoProviderError(err)
	}
	task := &ProviderVideoTask{
		ProviderTaskID: providerTaskID, RawStatus: falStatusCompleted,
		Metadata: map[string]any{}, SuggestedPollInterval: falDefaultPoll,
	}
	if units := strings.TrimSpace(response.Header.Get(falBillableUnitsHeader)); units != "" {
		if value, err := strconv.ParseFloat(units, 64); err == nil && value >= 0 {
			task.Usage = map[string]any{"billable_units": value}
		}
	}
	videoURL := ""
	for _, path := range []string{"video.url", "videos.0.url", "output.url", "url"} {
		if videoURL = videoVendorLookupString(result, path); videoURL != "" {
			break
		}
	}
	if videoURL == "" {
		task.Status = VideoGenerationFailed
		task.RawStatus = "completed_without_output"
		task.ErrorCode = "missing_output"
		task.ErrorMessage = "fal reported completion without a video url"
		return task, nil, nil
	}
	normalized, err := normalizeProviderVideoURL(videoURL)
	if err != nil || !strings.HasPrefix(strings.ToLower(normalized), "https://") {
		task.Status = VideoGenerationFailed
		task.RawStatus = "invalid_output"
		task.ErrorCode = "invalid_output"
		task.ErrorMessage = "fal video url is invalid"
		return task, nil, nil
	}
	task.Status = VideoGenerationCompleted
	task.VideoURL = normalized
	task.ContentVariants = []string{"video"}
	now := time.Now().UTC()
	task.ProviderFinishedAt = &now
	return task, nil, nil
}

func (a *falVideoAdapter) BuildCancel(providerTaskID string) (*vendorHTTPRequest, bool) {
	if _, _, ok := falSplitTaskID(providerTaskID); !ok {
		return nil, false
	}
	return &vendorHTTPRequest{Method: http.MethodPut, Path: "/" + providerTaskID + "/cancel"}, true
}

// BuildDelete 没有删除接口：删除等于取消；已完成的任务由 TerminateSucceeded 放行。
func (a *falVideoAdapter) BuildDelete(providerTaskID string) (*vendorHTTPRequest, bool) {
	return a.BuildCancel(providerTaskID)
}

// TerminateSucceeded 把 cancel 的 400 ALREADY_COMPLETED 视为成功：任务已终态，无需再动。
func (a *falVideoAdapter) TerminateSucceeded(status int, body []byte) bool {
	if status >= 200 && status < 300 {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(payload.Status), "ALREADY_COMPLETED")
}

func (a *falVideoAdapter) BuildSearch(string) (*vendorHTTPRequest, bool) { return nil, false }
func (a *falVideoAdapter) DecodeSearch(*vendorHTTPResponse, string) (*ProviderVideoTask, error) {
	return nil, nil
}

// BuildProbe 查一个不存在的请求：鉴权失败是 401 / 403，鉴权通过时上游只会答 404。
func (a *falVideoAdapter) BuildProbe() (*vendorHTTPRequest, bool) {
	return &vendorHTTPRequest{Method: http.MethodGet, Path: "/" + falTaskID(falProbeAppID, falProbeRequestID) + "/status"}, true
}

// ClassifyError 解析 fal 的 {detail, error_type} 错误体；422 的 detail 可能是 pydantic 的数组。
func (a *falVideoAdapter) ClassifyError(status int, body []byte) (string, string, string, bool, bool) {
	var payload struct {
		Detail    any    `json:"detail"`
		ErrorType string `json:"error_type"`
		Message   string `json:"message"`
	}
	if len(strings.TrimSpace(string(body))) == 0 || json.Unmarshal(body, &payload) != nil {
		return "", "", "", false, false
	}
	message := falErrorMessage(payload.Detail)
	if message == "" {
		message = strings.TrimSpace(payload.Message)
	}
	errorType := strings.ToLower(strings.TrimSpace(payload.ErrorType))
	switch {
	case strings.Contains(errorType, "content_policy") || strings.Contains(strings.ToLower(message), "content policy") ||
		strings.Contains(strings.ToLower(message), "safety"):
		return "validation", "content_moderation_rejected", message, false, true
	case status == http.StatusPaymentRequired || strings.Contains(errorType, "balance") || strings.Contains(strings.ToLower(message), "balance"):
		return "upstream", "insufficient_balance", message, false, true
	case status == http.StatusUnauthorized:
		return "authentication", falErrorCode(errorType, "unauthorized"), message, false, true
	case status == http.StatusForbidden:
		return "permission", falErrorCode(errorType, "forbidden"), message, false, true
	case status == http.StatusTooManyRequests:
		return "rate_limit", falErrorCode(errorType, "rate_limited"), message, true, true
	case status == http.StatusUnprocessableEntity || status == http.StatusBadRequest:
		return "validation", falErrorCode(errorType, "invalid_request"), message, false, true
	case status >= 500:
		return "upstream", falErrorCode(errorType, "upstream_error"), message, true, true
	default:
		return "", falErrorCode(errorType, ""), message, false, true
	}
}

func falErrorCode(errorType, fallback string) string {
	errorType = strings.TrimSpace(errorType)
	if errorType == "" {
		return fallback
	}
	return errorType
}

// falErrorMessage 把 error / detail 字段（字符串、对象或 pydantic 风格数组）压成一行。
func falErrorMessage(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"message", "msg", "detail", "error"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := falErrorMessage(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(typed)
	}
}
