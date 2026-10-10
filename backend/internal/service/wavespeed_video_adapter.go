package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WaveSpeed 视频适配器。
//
// 协议要点（https://wavespeed.ai/docs/rest-api、…/submit-task、…/get-result）：
//   - 提交：POST {base}/{model_id}，body 按模型；响应信封 {code, message, data:{id, status, …}}，
//     HTTP 200 但 code ≠ 200 同样是失败。
//   - 轮询：GET {base}/predictions/{id}/result → data.{status, outputs[], error, timings}。
//   - 图片输入必须是 URL：先 POST {base}/media/upload/binary（multipart 字段 file）换
//     data.download_url，文件保留 7 天。
//   - 没有幂等键；删除只删已完成记录（POST {base}/predictions/delete），不能取消。
//   - 输出 URL 保留 7 天。
const (
	waveSpeedContentTTL     = 7 * 24 * time.Hour
	waveSpeedDefaultPoll    = videoVendorDefaultPollPeriod
	waveSpeedResultPath     = "/predictions/%s/result"
	waveSpeedDeletePath     = "/predictions/delete"
	waveSpeedUploadPath     = "/media/upload/binary"
	waveSpeedBalancePath    = "/balance"
	waveSpeedPricePath      = "/model/price"
	waveSpeedEnvelopeOK     = 200
	waveSpeedMaxUploadBytes = videoVendorMaxInlineBytes
)

type waveSpeedVideoAdapter struct {
	profiles map[string]*videoModelProfile
}

func newWaveSpeedVideoAdapter() *waveSpeedVideoAdapter {
	return &waveSpeedVideoAdapter{profiles: waveSpeedVideoProfiles()}
}

func (a *waveSpeedVideoAdapter) Name() string           { return VideoProviderWaveSpeed }
func (a *waveSpeedVideoAdapter) DefaultBaseURL() string { return waveSpeedDefaultBaseURL }
func (a *waveSpeedVideoAdapter) AuthorizationHeader(apiKey string) string {
	return "Bearer " + apiKey
}
func (a *waveSpeedVideoAdapter) ClientTokenMode() videoClientTokenMode {
	return videoClientTokenMode{Kind: videoClientTokenNone}
}
func (a *waveSpeedVideoAdapter) ClientTokenFor(localToken string) string { return localToken }
func (a *waveSpeedVideoAdapter) Profiles() map[string]*videoModelProfile { return a.profiles }
func (a *waveSpeedVideoAdapter) FreeSecondsRange() (int, int)            { return 1, 30 }
func (a *waveSpeedVideoAdapter) DefaultPollInterval() time.Duration      { return waveSpeedDefaultPoll }
func (a *waveSpeedVideoAdapter) ContentTTL() time.Duration               { return waveSpeedContentTTL }
func (a *waveSpeedVideoAdapter) FallbackProfile(model string) *videoModelProfile {
	return waveSpeedFallbackProfile(model)
}

func (a *waveSpeedVideoAdapter) DefaultCapabilities() VideoCapabilities {
	return videoVendorCapabilitiesFromProfiles(waveSpeedDefaultVideoModel, a.profiles, map[VideoCapability]bool{
		VideoCapabilityCreate: true, VideoCapabilityInputReference: true,
	})
}

// ---------------------------------------------------------------------------
// Wire format
// ---------------------------------------------------------------------------

type waveSpeedEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type waveSpeedPrediction struct {
	ID        string            `json:"id"`
	Model     string            `json:"model"`
	Status    string            `json:"status"`
	Outputs   []any             `json:"outputs"`
	Error     string            `json:"error"`
	Code      int               `json:"code"`
	URLs      map[string]string `json:"urls"`
	CreatedAt string            `json:"created_at"`
	Timings   map[string]any    `json:"timings"`
}

func waveSpeedDecodeEnvelope(body []byte) (*waveSpeedEnvelope, error) {
	var envelope waveSpeedEnvelope
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("wavespeed response is empty")
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func waveSpeedDecodePrediction(envelope *waveSpeedEnvelope) (*waveSpeedPrediction, error) {
	var prediction waveSpeedPrediction
	if envelope == nil || len(envelope.Data) == 0 {
		return nil, fmt.Errorf("wavespeed response has no data")
	}
	if err := json.Unmarshal(envelope.Data, &prediction); err != nil {
		return nil, err
	}
	return &prediction, nil
}

func (a *waveSpeedVideoAdapter) BuildCreate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput, _ string) (*vendorHTTPRequest, error) {
	body, endpoint, err := a.buildBody(request, profile, inputs)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, videoVendorValidationError("invalid_payload", "video payload could not be encoded")
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: "/" + strings.TrimLeft(endpoint, "/"), Body: payload, ContentType: "application/json"}, nil
}

// buildBody 产出提交体与端点；询价接口把同一份 body 作为 inputs 送去。
func (a *waveSpeedVideoAdapter) buildBody(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput) (map[string]any, string, error) {
	if profile == nil {
		return nil, "", videoVendorValidationError("unsupported_model", "model %q is not supported by wavespeed", request.Model)
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
		imageField = "image"
	}
	lastImageField := profile.LastImageField
	if lastImageField == "" {
		lastImageField = "last_image"
	}
	for i, input := range inputs {
		if input.URL == "" {
			return nil, "", videoVendorValidationError("unsupported_input", "wavespeed image inputs must be uploaded before submission")
		}
		switch i {
		case 0:
			videoVendorSetPath(body, imageField, input.URL)
		case 1:
			videoVendorSetPath(body, lastImageField, input.URL)
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
	videoVendorApplyFraming(body, profile, request, len(inputs) > 0, aspectField, resolutionField)
	if err := videoVendorApplyArkFields(body, profile, request); err != nil {
		return nil, "", err
	}
	for key, raw := range request.ProviderOptions {
		path, ok := profile.OptionWhitelist[key]
		if !ok {
			return nil, "", videoVendorValidationError("unsupported_option", "provider_options.%s is not supported by model %q", key, request.Model)
		}
		value, ok := videoVendorOptionValue(raw)
		if !ok {
			return nil, "", videoVendorValidationError("unsupported_option", "provider_options.%s must be a scalar value", key)
		}
		videoVendorSetPath(body, path, value)
	}
	endpoint := profile.Endpoint
	if endpoint == "" {
		endpoint = strings.TrimSpace(request.Model)
	}
	return body, endpoint, nil
}

// BuildEstimate 调 POST {base}/model/price，inputs 与提交体一致，让厂商按动态公式报价。
func (a *waveSpeedVideoAdapter) BuildEstimate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput) (*vendorHTTPRequest, bool) {
	for _, input := range inputs {
		if input.URL == "" {
			return nil, false
		}
	}
	body, endpoint, err := a.buildBody(request, profile, inputs)
	if err != nil {
		return nil, false
	}
	payload, err := json.Marshal(map[string]any{"model_id": strings.Trim(endpoint, "/"), "inputs": body})
	if err != nil {
		return nil, false
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: waveSpeedPricePath, Body: payload, ContentType: "application/json"}, true
}

// DecodeEstimate 取 data.price（折扣前的 USD 价，与本地配置的基础价同口径）。
func (a *waveSpeedVideoAdapter) DecodeEstimate(response *vendorHTTPResponse) (float64, error) {
	envelope, err := waveSpeedDecodeEnvelope(response.Body)
	if err != nil {
		return 0, fmt.Errorf("wavespeed price response could not be decoded: %w", err)
	}
	if envelope.Code != waveSpeedEnvelopeOK {
		return 0, fmt.Errorf("wavespeed price code %d: %s", envelope.Code, strings.TrimSpace(envelope.Message))
	}
	var payload struct {
		Price    *float64 `json:"price"`
		Currency string   `json:"currency"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil || payload.Price == nil || *payload.Price < 0 {
		return 0, fmt.Errorf("wavespeed price response is missing price")
	}
	if payload.Currency != "" && !strings.EqualFold(payload.Currency, "USD") {
		return 0, fmt.Errorf("wavespeed price currency %q is not USD", payload.Currency)
	}
	return *payload.Price, nil
}

func (a *waveSpeedVideoAdapter) DecodeCreate(response *vendorHTTPResponse, _ VideoCreateRequest, _ string) (*ProviderVideoTask, error) {
	envelope, err := waveSpeedDecodeEnvelope(response.Body)
	if err != nil {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "wavespeed video response could not be decoded", err)
	}
	if envelope.Code != waveSpeedEnvelopeOK {
		kind, code, message, retryable := waveSpeedClassifyCode(response.StatusCode, envelope.Code, envelope.Message)
		providerErr := rejectedVideoProviderError(kind, code, message, http.StatusBadRequest)
		providerErr.Retryable = retryable
		if retryable {
			providerErr.Certainty = VideoSubmissionUnknown
		}
		return nil, providerErr
	}
	prediction, err := waveSpeedDecodePrediction(envelope)
	if err != nil || !validVideoProviderIdentifier(prediction.ID) {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "wavespeed video response is missing a prediction id", err)
	}
	task := waveSpeedTaskFromPrediction(prediction)
	if task.Status == "" {
		task.Status = VideoGenerationQueued
	}
	return task, nil
}

func (a *waveSpeedVideoAdapter) BuildGet(providerTaskID string) (*vendorHTTPRequest, error) {
	if !validVideoProviderIdentifier(providerTaskID) {
		return nil, fmt.Errorf("wavespeed prediction id is invalid")
	}
	return &vendorHTTPRequest{Method: http.MethodGet, Path: fmt.Sprintf(waveSpeedResultPath, url.PathEscape(providerTaskID))}, nil
}

func (a *waveSpeedVideoAdapter) DecodeGet(response *vendorHTTPResponse, providerTaskID string, _ int) (*ProviderVideoTask, *vendorHTTPRequest, error) {
	envelope, err := waveSpeedDecodeEnvelope(response.Body)
	if err != nil {
		return nil, nil, controlVideoProviderError(err)
	}
	if envelope.Code != waveSpeedEnvelopeOK {
		return nil, nil, controlVideoProviderError(fmt.Errorf("wavespeed result code %d: %s", envelope.Code, strings.TrimSpace(envelope.Message)))
	}
	prediction, err := waveSpeedDecodePrediction(envelope)
	if err != nil {
		return nil, nil, controlVideoProviderError(err)
	}
	if prediction.ID != "" && !strings.EqualFold(prediction.ID, providerTaskID) {
		return nil, nil, controlVideoProviderError(fmt.Errorf("wavespeed result identity does not match the requested prediction"))
	}
	task := waveSpeedTaskFromPrediction(prediction)
	task.ProviderTaskID = providerTaskID
	if task.Status == "" {
		return nil, nil, controlVideoProviderError(fmt.Errorf("wavespeed status %q is not recognized", prediction.Status))
	}
	return task, nil, nil
}

// waveSpeedTaskFromPrediction 把 prediction 翻译成 ProviderVideoTask；未知状态时 Status 为空。
func waveSpeedTaskFromPrediction(prediction *waveSpeedPrediction) *ProviderVideoTask {
	task := &ProviderVideoTask{
		ProviderTaskID: strings.TrimSpace(prediction.ID), RawStatus: strings.TrimSpace(prediction.Status),
		Metadata: map[string]any{}, SuggestedPollInterval: waveSpeedDefaultPoll,
	}
	if model := strings.TrimSpace(prediction.Model); model != "" {
		task.Metadata["model"] = model
	}
	if created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(prediction.CreatedAt)); err == nil {
		created = created.UTC()
		task.ProviderCreatedAt = &created
	}
	switch strings.ToLower(strings.TrimSpace(prediction.Status)) {
	case "created", "pending", "queued", "":
		if prediction.Status == "" {
			return task
		}
		task.Status = VideoGenerationQueued
	case "processing", "running", "in_progress":
		task.Status = VideoGenerationInProgress
	case "completed", "succeeded", "success":
		task.Status = VideoGenerationCompleted
	case "failed", "timeout", "error":
		task.Status = VideoGenerationFailed
	case "cancelled", "canceled":
		task.Status = VideoGenerationCancelled
	case "deleted":
		task.Status = VideoGenerationFailed
		task.ErrorCode = "deleted"
		task.ErrorMessage = "wavespeed prediction was deleted upstream"
	default:
		return task
	}
	if task.Status == VideoGenerationFailed && task.ErrorCode == "" {
		task.ErrorCode = waveSpeedErrorCode(prediction.Code, strings.ToLower(strings.TrimSpace(prediction.Status)))
		task.ErrorMessage = boundedProviderMessage(strings.TrimSpace(prediction.Error))
		if task.ErrorMessage == "" {
			task.ErrorMessage = "wavespeed prediction " + strings.TrimSpace(prediction.Status)
		}
	}
	if inference, ok := numericMapValue(prediction.Timings, "inference"); ok && inference > 0 {
		task.Usage = map[string]any{"inference_ms": inference}
	}
	if task.Status != VideoGenerationCompleted {
		return task
	}
	output := ""
	for _, raw := range prediction.Outputs {
		if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
			output = strings.TrimSpace(text)
			break
		}
	}
	if output == "" {
		task.Status = VideoGenerationFailed
		task.RawStatus = "completed_without_output"
		task.ErrorCode = "missing_output"
		task.ErrorMessage = "wavespeed reported completion without an output url"
		return task
	}
	normalized, err := normalizeProviderVideoURL(output)
	if err != nil || !strings.HasPrefix(strings.ToLower(normalized), "https://") {
		task.Status = VideoGenerationFailed
		task.RawStatus = "invalid_output"
		task.ErrorCode = "invalid_output"
		task.ErrorMessage = "wavespeed output url is invalid"
		return task
	}
	task.VideoURL = normalized
	task.ContentVariants = []string{"video"}
	now := time.Now().UTC()
	task.ProviderFinishedAt = &now
	return task
}

func waveSpeedErrorCode(code int, fallback string) string {
	if code > 0 && code != waveSpeedEnvelopeOK {
		return "wavespeed_" + strconv.Itoa(code)
	}
	return fallback
}

func (a *waveSpeedVideoAdapter) BuildCancel(string) (*vendorHTTPRequest, bool) { return nil, false }

// BuildDelete 调批量删除接口；运行中的任务会被上游跳过，不构成错误。
func (a *waveSpeedVideoAdapter) BuildDelete(providerTaskID string) (*vendorHTTPRequest, bool) {
	if !validVideoProviderIdentifier(providerTaskID) {
		return nil, false
	}
	payload, err := json.Marshal(map[string]any{"ids": []string{providerTaskID}})
	if err != nil {
		return nil, false
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: waveSpeedDeletePath, Body: payload, ContentType: "application/json"}, true
}

func (a *waveSpeedVideoAdapter) BuildSearch(string) (*vendorHTTPRequest, bool) { return nil, false }
func (a *waveSpeedVideoAdapter) DecodeSearch(*vendorHTTPResponse, string) (*ProviderVideoTask, error) {
	return nil, nil
}

func (a *waveSpeedVideoAdapter) BuildProbe() (*vendorHTTPRequest, bool) {
	return &vendorHTTPRequest{Method: http.MethodGet, Path: waveSpeedBalancePath}, true
}

// BuildUpload 走单请求 multipart 上传（字段 file）。
func (a *waveSpeedVideoAdapter) BuildUpload(input videoVendorInput) (*vendorHTTPRequest, error) {
	if len(input.Data) == 0 || len(input.Data) > waveSpeedMaxUploadBytes {
		return nil, rejectedVideoProviderError("validation", "input_too_large", "wavespeed image input is empty or too large", http.StatusBadRequest)
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", "input"+waveSpeedExtensionFor(input.MIMEType))
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "wavespeed upload payload could not be encoded", http.StatusBadRequest)
	}
	if _, err := part.Write(input.Data); err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "wavespeed upload payload could not be encoded", http.StatusBadRequest)
	}
	if err := writer.Close(); err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "wavespeed upload payload could not be encoded", http.StatusBadRequest)
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: waveSpeedUploadPath, Body: buffer.Bytes(), ContentType: writer.FormDataContentType()}, nil
}

func (a *waveSpeedVideoAdapter) DecodeUpload(response *vendorHTTPResponse) (string, error) {
	envelope, err := waveSpeedDecodeEnvelope(response.Body)
	if err != nil {
		return "", rejectedVideoProviderError("upstream", "upload_failed", "wavespeed upload response could not be decoded", http.StatusBadGateway)
	}
	if envelope.Code != waveSpeedEnvelopeOK {
		kind, code, message, _ := waveSpeedClassifyCode(response.StatusCode, envelope.Code, envelope.Message)
		if code == "" {
			code = "upload_failed"
		}
		return "", rejectedVideoProviderError(kind, code, message, http.StatusBadGateway)
	}
	var payload struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil || strings.TrimSpace(payload.DownloadURL) == "" {
		return "", rejectedVideoProviderError("upstream", "upload_failed", "wavespeed upload response is missing download_url", http.StatusBadGateway)
	}
	return strings.TrimSpace(payload.DownloadURL), nil
}

func waveSpeedExtensionFor(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func (a *waveSpeedVideoAdapter) ClassifyError(status int, body []byte) (string, string, string, bool, bool) {
	envelope, err := waveSpeedDecodeEnvelope(body)
	if err != nil || envelope == nil {
		return "", "", "", false, false
	}
	kind, code, message, retryable := waveSpeedClassifyCode(status, envelope.Code, envelope.Message)
	return kind, code, message, retryable, true
}

// waveSpeedClassifyCode 把 WaveSpeed 业务码映射到骨架的错误分类
// （https://wavespeed.ai/docs/error-codes）。
func waveSpeedClassifyCode(status, code int, message string) (string, string, string, bool) {
	message = strings.TrimSpace(message)
	codeName := waveSpeedErrorCode(code, "")
	switch code {
	case 1200:
		return "validation", "content_moderation_rejected", message, false
	case 1400, 1401:
		return "validation", codeName, message, false
	case 1402:
		return "validation", "media_access_failed", message, false
	case 1407:
		return "upstream", "insufficient_balance", message, false
	case 4005, 4006, 4007:
		return "validation", "pricing_failed", message, false
	case 1406, 5000, 5003, 5004:
		return "upstream", codeName, message, true
	}
	switch status {
	case http.StatusUnauthorized:
		return "authentication", codeName, message, false
	case http.StatusForbidden:
		return "permission", codeName, message, false
	case http.StatusPaymentRequired:
		return "upstream", "insufficient_balance", message, false
	case http.StatusTooManyRequests:
		return "rate_limit", codeName, message, true
	case http.StatusRequestEntityTooLarge:
		return "validation", "input_too_large", message, false
	}
	if status >= 500 || code >= 5000 {
		return "upstream", codeName, message, true
	}
	return "validation", codeName, message, false
}
