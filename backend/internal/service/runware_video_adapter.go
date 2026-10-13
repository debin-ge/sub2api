package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Runware 视频适配器。
//
// 协议要点（https://runware.ai/docs/models-api/introduction、…/task-polling）：
//   - 一切请求都是 POST {base}/，body 是任务数组；模型在 body 的 model 字段（AIR ID）。
//   - 提交 videoInference 时 taskUUID 由客户端生成（UUID v4），天然幂等；轮询用
//     getResponse 任务按 taskUUID 查询，丢响应后的回查也走同一条路。
//   - 响应 {data:[…], errors:[…]}；成功项带 videoURL、cost（includeCost=true）。
//   - 没有取消 / 删除接口；输出 URL 默认保留 7 天（ttl）。
const (
	runwareDefaultBaseURL   = "https://api.runware.ai/v1"
	runwareContentTTL       = 7 * 24 * time.Hour
	runwareOutputTTLSeconds = 7 * 24 * 60 * 60
	runwareDefaultPoll      = videoVendorDefaultPollPeriod
	runwareTaskTypeVideo    = "videoInference"
	runwareTaskTypeResponse = "getResponse"
)

var runwareLocalTokenPattern = regexp.MustCompile(`^video_([0-9a-f]{32})$`)

type runwareVideoAdapter struct {
	profiles map[string]*videoModelProfile
}

func newRunwareVideoAdapter() *runwareVideoAdapter {
	return &runwareVideoAdapter{profiles: runwareVideoProfiles()}
}

func (a *runwareVideoAdapter) Name() string           { return VideoProviderRunware }
func (a *runwareVideoAdapter) DefaultBaseURL() string { return runwareDefaultBaseURL }
func (a *runwareVideoAdapter) AuthorizationHeader(apiKey string) string {
	return "Bearer " + apiKey
}
func (a *runwareVideoAdapter) ClientTokenMode() videoClientTokenMode {
	return videoClientTokenMode{Kind: videoClientTokenBody, Name: "taskUUID"}
}
func (a *runwareVideoAdapter) Profiles() map[string]*videoModelProfile { return a.profiles }
func (a *runwareVideoAdapter) FreeSecondsRange() (int, int)            { return 1, 30 }
func (a *runwareVideoAdapter) DefaultPollInterval() time.Duration      { return runwareDefaultPoll }
func (a *runwareVideoAdapter) ContentTTL() time.Duration               { return runwareContentTTL }

func (a *runwareVideoAdapter) DefaultCapabilities() VideoCapabilities {
	return videoVendorCapabilitiesFromProfiles(runwareDefaultVideoModel, a.profiles, map[VideoCapability]bool{
		VideoCapabilityCreate: true, VideoCapabilityInputReference: true, VideoCapabilityTaskSearch: true,
	})
}

func (a *runwareVideoAdapter) FallbackProfile(model string) *videoModelProfile {
	return runwareFallbackProfile(model)
}

// ClientTokenFor 把本地 video_<32hex> 还原成带连字符的 UUID（它本来就是去连字符的
// UUID v4）；其他形态的 token 用 SHA-256 派生一个稳定的 v4 形态 UUID，保证可重放。
func (a *runwareVideoAdapter) ClientTokenFor(localToken string) string {
	return runwareTaskUUID(localToken)
}

func runwareTaskUUID(localToken string) string {
	localToken = strings.ToLower(strings.TrimSpace(localToken))
	if match := runwareLocalTokenPattern.FindStringSubmatch(localToken); match != nil {
		return runwareFormatUUID(match[1])
	}
	if len(localToken) == 36 && strings.Count(localToken, "-") == 4 {
		return localToken
	}
	sum := sha256.Sum256([]byte(localToken))
	raw := sum[:16]
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return runwareFormatUUID(hex.EncodeToString(raw))
}

func runwareFormatUUID(hex32 string) string {
	return hex32[0:8] + "-" + hex32[8:12] + "-" + hex32[12:16] + "-" + hex32[16:20] + "-" + hex32[20:32]
}

func runwareRandomUUID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return runwareTaskUUID("probe-" + time.Now().UTC().Format(time.RFC3339Nano))
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return runwareFormatUUID(hex.EncodeToString(raw))
}

// ---------------------------------------------------------------------------
// Wire format
// ---------------------------------------------------------------------------

type runwareErrorEntry struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	Parameter     string `json:"parameter"`
	TaskType      string `json:"taskType"`
	TaskUUID      string `json:"taskUUID"`
	Documentation string `json:"documentation"`
}

type runwareEnvelope struct {
	Data   []map[string]any    `json:"data"`
	Errors []runwareErrorEntry `json:"errors"`
}

func runwareDecodeEnvelope(body []byte) (*runwareEnvelope, error) {
	var envelope runwareEnvelope
	if len(strings.TrimSpace(string(body))) == 0 {
		return &envelope, nil
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (a *runwareVideoAdapter) BuildCreate(request VideoCreateRequest, profile *videoModelProfile, inputs []videoVendorInput, clientToken string) (*vendorHTTPRequest, error) {
	if profile == nil {
		return nil, videoVendorValidationError("unsupported_model", "model %q is not supported by runware", request.Model)
	}
	body := map[string]any{
		"taskType":       runwareTaskTypeVideo,
		"taskUUID":       clientToken,
		"model":          strings.TrimSpace(request.Model),
		"positivePrompt": request.Prompt,
		"deliveryMethod": "async",
		"includeCost":    true,
		"outputType":     "URL",
		"outputFormat":   "MP4",
		"ttl":            runwareOutputTTLSeconds,
		"numberResults":  1,
	}
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
	first, last := videoVendorFrameInputs(inputs)
	if err := videoVendorApplyReferences(body, profile, inputs); err != nil {
		return nil, err
	}
	if first != nil {
		frames := make([]any, 0, 2)
		if last == nil {
			frames = append(frames, first.dataURI())
		} else {
			frames = append(frames, map[string]any{"image": first.dataURI(), "frame": "first"})
			frames = append(frames, map[string]any{"image": last.dataURI(), "frame": "last"})
		}
		videoVendorSetPath(body, "inputs.frameImages", frames)
		// 带图时 Runware 禁止 width/height，用 resolution 档位表达尺寸诉求。
		if profile.ImageResolutionField != "" && strings.TrimSpace(request.Resolution) != "" {
			if label, ok := profile.vendorResolutionLabel(request.Resolution); ok {
				videoVendorSetPath(body, profile.ImageResolutionField, label)
			}
		}
	} else if profile.SizeEnc == videoSizeWidthHeight {
		// 文生视频：Ark 的 (resolution, ratio) 换成 Runware 要的 width / height。
		resolution := strings.TrimSpace(request.Resolution)
		ratio := strings.TrimSpace(request.Ratio)
		if resolution == "" {
			resolution = profile.DefaultResolution
		}
		if ratio == "" {
			ratio = profile.DefaultRatio
		}
		if resolution != "" && ratio != "" {
			size, ok := profile.sizeForArkDimensions(resolution, ratio)
			if !ok {
				return nil, videoVendorValidationError("unsupported_resolution", "model %q has no %s %s frame size", request.Model, resolution, ratio)
			}
			_, width, height, _ := parseVideoDimensions(size)
			body["width"] = width
			body["height"] = height
		}
	}
	if err := videoVendorApplyArkFields(body, profile, request); err != nil {
		return nil, err
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
	payload, err := json.Marshal([]any{body})
	if err != nil {
		return nil, videoVendorValidationError("invalid_payload", "video payload could not be encoded")
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: "", Body: payload, ContentType: "application/json"}, nil
}

func (a *runwareVideoAdapter) DecodeCreate(response *vendorHTTPResponse, request VideoCreateRequest, clientToken string) (*ProviderVideoTask, error) {
	envelope, err := runwareDecodeEnvelope(response.Body)
	if err != nil {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "runware video response could not be decoded", err)
	}
	if len(envelope.Errors) > 0 {
		// HTTP 200 里带 errors：任务未被受理，不会计费。
		kind, code, message, retryable := runwareClassifyEntry(response.StatusCode, envelope.Errors[0])
		providerErr := rejectedVideoProviderError(kind, code, message, http.StatusBadRequest)
		providerErr.Retryable = retryable
		return nil, providerErr
	}
	now := time.Now().UTC()
	task := &ProviderVideoTask{
		ProviderTaskID: clientToken, Status: VideoGenerationQueued, RawStatus: "submitted",
		Metadata: map[string]any{}, SuggestedPollInterval: runwareDefaultPoll, ProviderCreatedAt: &now,
	}
	for _, item := range envelope.Data {
		if uuid := videoVendorLookupString(item, "taskUUID"); uuid != "" && !strings.EqualFold(uuid, clientToken) {
			continue
		}
		observed := runwareTaskFromItem(item, clientToken)
		if observed != nil {
			observed.ProviderCreatedAt = &now
			return observed, nil
		}
	}
	return task, nil
}

func (a *runwareVideoAdapter) BuildGet(providerTaskID string) (*vendorHTTPRequest, error) {
	if !validVideoProviderIdentifier(providerTaskID) {
		return nil, fmt.Errorf("runware task id is invalid")
	}
	payload, err := json.Marshal([]map[string]any{{"taskType": runwareTaskTypeResponse, "taskUUID": providerTaskID}})
	if err != nil {
		return nil, err
	}
	return &vendorHTTPRequest{Method: http.MethodPost, Path: "", Body: payload, ContentType: "application/json"}, nil
}

func (a *runwareVideoAdapter) DecodeGet(response *vendorHTTPResponse, providerTaskID string, _ int) (*ProviderVideoTask, *vendorHTTPRequest, error) {
	envelope, err := runwareDecodeEnvelope(response.Body)
	if err != nil {
		return nil, nil, controlVideoProviderError(err)
	}
	for _, entry := range envelope.Errors {
		if entry.TaskUUID != "" && !strings.EqualFold(entry.TaskUUID, providerTaskID) {
			continue
		}
		// 轮询到带本任务 taskUUID 的错误项：上游已终止该任务（timeoutProvider 等），按终态失败处理。
		return &ProviderVideoTask{
			ProviderTaskID: providerTaskID, Status: VideoGenerationFailed, RawStatus: "error",
			ErrorCode: boundedVideoProviderCode(strings.TrimSpace(entry.Code)), ErrorMessage: boundedProviderMessage(entry.Message),
			Metadata: map[string]any{}, SuggestedPollInterval: runwareDefaultPoll,
		}, nil, nil
	}
	for _, item := range envelope.Data {
		if uuid := videoVendorLookupString(item, "taskUUID"); uuid != "" && !strings.EqualFold(uuid, providerTaskID) {
			continue
		}
		if task := runwareTaskFromItem(item, providerTaskID); task != nil {
			return task, nil, nil
		}
	}
	// 既没有结果也没有错误：任务仍在排队。
	return &ProviderVideoTask{
		ProviderTaskID: providerTaskID, Status: VideoGenerationQueued, RawStatus: "pending",
		Metadata: map[string]any{}, SuggestedPollInterval: runwareDefaultPoll,
	}, nil, nil
}

// runwareTaskFromItem 把 data[] 里的一项翻译成 ProviderVideoTask；不认识的项返回 nil。
func runwareTaskFromItem(item map[string]any, providerTaskID string) *ProviderVideoTask {
	if item == nil {
		return nil
	}
	task := &ProviderVideoTask{
		ProviderTaskID: providerTaskID, Metadata: map[string]any{}, SuggestedPollInterval: runwareDefaultPoll,
	}
	status := strings.ToLower(videoVendorLookupString(item, "status"))
	videoURL := videoVendorLookupString(item, "videoURL")
	switch {
	case videoURL != "" || status == "success":
		task.Status = VideoGenerationCompleted
		task.RawStatus = "success"
	case status == "processing":
		task.Status = VideoGenerationInProgress
		task.RawStatus = status
	case status == "error" || status == "failed":
		task.Status = VideoGenerationFailed
		task.RawStatus = status
		task.ErrorCode = boundedVideoProviderCode(videoVendorLookupString(item, "error.code"))
		task.ErrorMessage = boundedProviderMessage(videoVendorLookupString(item, "error.message"))
	case status == "" && videoURL == "":
		return nil
	default:
		task.Status = VideoGenerationQueued
		task.RawStatus = status
	}
	if raw, ok := videoVendorLookupPath(item, "progress"); ok {
		if value, ok := numericMapValue(map[string]any{"progress": raw}, "progress"); ok {
			progress := value / 100
			task.Progress = &progress
		}
	}
	if raw, ok := videoVendorLookupPath(item, "cost"); ok {
		if cost, ok := numericMapValue(map[string]any{"cost": raw}, "cost"); ok {
			task.Usage = map[string]any{"upstream_cost_usd": cost}
		}
	}
	if task.Status == VideoGenerationCompleted {
		if videoURL == "" {
			task.Status = VideoGenerationFailed
			task.RawStatus = "success_without_output"
			task.ErrorCode = "missing_output"
			task.ErrorMessage = "runware reported success without a video url"
			return task
		}
		normalized, err := normalizeProviderVideoURL(videoURL)
		if err != nil || !strings.HasPrefix(strings.ToLower(normalized), "https://") {
			task.Status = VideoGenerationFailed
			task.RawStatus = "invalid_output"
			task.ErrorCode = "invalid_output"
			task.ErrorMessage = "runware video url is invalid"
			return task
		}
		task.VideoURL = normalized
		task.ContentVariants = []string{"video"}
		now := time.Now().UTC()
		task.ProviderFinishedAt = &now
	}
	return task
}

func (a *runwareVideoAdapter) BuildCancel(string) (*vendorHTTPRequest, bool) { return nil, false }
func (a *runwareVideoAdapter) BuildDelete(string) (*vendorHTTPRequest, bool) { return nil, false }

func (a *runwareVideoAdapter) BuildSearch(clientToken string) (*vendorHTTPRequest, bool) {
	upstream, err := a.BuildGet(clientToken)
	if err != nil {
		return nil, false
	}
	return upstream, true
}

// DecodeSearch 用于丢响应后的回查：带本 taskUUID 的错误项说明上游不认识该任务，
// 返回 nil 让调度继续等待直到对账窗口到期。
func (a *runwareVideoAdapter) DecodeSearch(response *vendorHTTPResponse, clientToken string) (*ProviderVideoTask, error) {
	envelope, err := runwareDecodeEnvelope(response.Body)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if len(envelope.Errors) > 0 {
		return nil, nil
	}
	for _, item := range envelope.Data {
		if uuid := videoVendorLookupString(item, "taskUUID"); uuid != "" && !strings.EqualFold(uuid, clientToken) {
			continue
		}
		if task := runwareTaskFromItem(item, clientToken); task != nil {
			return task, nil
		}
	}
	return nil, nil
}

// BuildProbe 用一个随机 taskUUID 发 getResponse：鉴权失败是 401 / 403，鉴权通过时
// 上游只会抱怨任务不存在。
func (a *runwareVideoAdapter) BuildProbe() (*vendorHTTPRequest, bool) {
	upstream, err := a.BuildGet(runwareRandomUUID())
	if err != nil {
		return nil, false
	}
	return upstream, true
}

func (a *runwareVideoAdapter) ClassifyError(status int, body []byte) (string, string, string, bool, bool) {
	envelope, err := runwareDecodeEnvelope(body)
	if err != nil || envelope == nil || len(envelope.Errors) == 0 {
		if status == http.StatusPaymentRequired {
			return "upstream", "insufficient_balance", "runware account balance is insufficient", false, true
		}
		return "", "", "", false, false
	}
	kind, code, message, retryable := runwareClassifyEntry(status, envelope.Errors[0])
	return kind, code, message, retryable, true
}

func runwareClassifyEntry(status int, entry runwareErrorEntry) (kind, code, message string, retryable bool) {
	code = strings.TrimSpace(entry.Code)
	message = strings.TrimSpace(entry.Message)
	lower := strings.ToLower(code)
	switch {
	case status == http.StatusPaymentRequired || strings.Contains(lower, "insufficient") || strings.Contains(lower, "balance"):
		return "upstream", "insufficient_balance", message, false
	case lower == "invalidapikey" || status == http.StatusUnauthorized:
		return "authentication", code, message, false
	case status == http.StatusForbidden:
		return "permission", code, message, false
	case lower == "providerratelimitexceeded" || status == http.StatusTooManyRequests:
		return "rate_limit", code, message, true
	case lower == "timeoutprovider" || status >= 500:
		return "upstream", code, message, true
	case strings.Contains(lower, "nsfw") || strings.Contains(lower, "moderation") || strings.Contains(lower, "safety"):
		return "validation", code, message, false
	default:
		return "validation", code, message, false
	}
}
