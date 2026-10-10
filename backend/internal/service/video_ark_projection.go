package service

import (
	"strings"
)

// Ark 视频协议的词表。分辨率与比例取火山方舟文档的全集；各模型实际支持的子集由
// 能力表与定价配置收窄。
var (
	arkVideoResolutions = []string{"480p", "720p", "1080p", "2k", "4k"}
	arkVideoRatios      = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", "adaptive"}
)

// ArkVideoRatioAdaptive 表示比例跟随首帧图片。
const ArkVideoRatioAdaptive = "adaptive"

// IsArkVideoResolution 报告是否为 Ark 词表中的分辨率档位。
func IsArkVideoResolution(value string) bool {
	for _, candidate := range arkVideoResolutions {
		if candidate == value {
			return true
		}
	}
	return false
}

// IsArkVideoRatio 报告是否为 Ark 词表中的比例。
func IsArkVideoRatio(value string) bool {
	for _, candidate := range arkVideoRatios {
		if candidate == value {
			return true
		}
	}
	return false
}

// ArkVideoResolutions 返回 Ark 分辨率词表的拷贝。
func ArkVideoResolutions() []string { return append([]string(nil), arkVideoResolutions...) }

// ArkVideoRatios 返回 Ark 比例词表的拷贝。
func ArkVideoRatios() []string { return append([]string(nil), arkVideoRatios...) }

// ArkVideoContentURLs 是投影时注入的内容链接：键为变体名（video / last_frame）。
type ArkVideoContentURLs map[string]string

// BuildArkVideoTaskObject 把本地任务投影成 Ark「查询视频生成任务」的响应体。
//
// 请求快照（RequestAttributes）给出用户要的 resolution / ratio / duration 等；上游观测值
// （ResponseMetadata）若存在则覆盖，因为 adaptive 比例与智能时长只有上游知道结果。
// usage 只在上游真实回报过 completion_tokens 时输出。披露字段（provider / actual_cost /
// provider_access）由调用方按披露策略追加。
func BuildArkVideoTaskObject(task *VideoTask, contentURLs ArkVideoContentURLs) map[string]any {
	if task == nil {
		return nil
	}
	status := ProjectArkVideoStatus(task)
	object := map[string]any{
		"id":         task.PublicID,
		"model":      firstNonEmptyString(task.PublicModel, task.RequestedModel),
		"status":     status,
		"created_at": task.CreatedAt.Unix(),
		"updated_at": task.UpdatedAt.Unix(),
	}
	attributes, observed := task.RequestAttributes, task.ResponseMetadata
	if value := arkObservedString(observed, attributes, "resolution"); value != "" {
		object["resolution"] = value
	}
	if value := arkObservedString(observed, attributes, "ratio"); value != "" {
		object["ratio"] = value
	}
	if seconds, ok := numericMapValue(observed, "seconds"); ok && seconds > 0 {
		object["duration"] = int(seconds)
	} else if duration, ok := numericMapValue(attributes, "duration"); ok {
		object["duration"] = int(duration)
	} else if seconds, ok := numericMapValue(attributes, "seconds"); ok && seconds > 0 {
		object["duration"] = int(seconds)
	}
	if frames, ok := numericMapValue(observed, "frames"); ok && frames > 0 {
		object["frames"] = int(frames)
	} else if frames, ok := numericMapValue(attributes, "frames"); ok && frames > 0 {
		object["frames"] = int(frames)
	}
	if fps, ok := numericMapValue(observed, "framespersecond"); ok && fps > 0 {
		object["framespersecond"] = int(fps)
	}
	if seed, ok := numericMapValue(observed, "seed"); ok {
		object["seed"] = int64(seed)
	} else if seed, ok := numericMapValue(attributes, "seed"); ok {
		object["seed"] = int64(seed)
	}
	if value, ok := arkBoolValue(observed, "generate_audio"); ok {
		object["generate_audio"] = value
	} else if value, ok := arkBoolValue(attributes, "generate_audio"); ok {
		object["generate_audio"] = value
	}
	for _, key := range []string{"camera_fixed", "watermark", "return_last_frame"} {
		if value, ok := arkBoolValue(attributes, key); ok {
			object[key] = value
		}
	}
	if value := arkObservedString(observed, attributes, "service_tier"); value != "" {
		object["service_tier"] = value
	}
	if value, ok := observed["revised_prompt"].(string); ok && strings.TrimSpace(value) != "" {
		object["revised_prompt"] = value
	}
	if value, ok := numericMapValue(attributes, "execution_expires_after"); ok && value > 0 {
		object["execution_expires_after"] = int(value)
	}
	if usage := arkUsageObject(task.UsageSnapshot); usage != nil {
		object["usage"] = usage
	}
	if status == ArkVideoStatusSucceeded {
		content := map[string]any{}
		if url := strings.TrimSpace(contentURLs["video"]); url != "" {
			content["video_url"] = url
		}
		if url := strings.TrimSpace(contentURLs["last_frame"]); url != "" {
			content["last_frame_url"] = url
		}
		if len(content) > 0 {
			object["content"] = content
		}
	}
	if status == ArkVideoStatusFailed || status == ArkVideoStatusCancelled || status == ArkVideoStatusExpired {
		object["error"] = map[string]any{
			"code":    firstNonEmptyString(videoStringValue(task.LastErrorCode), arkVideoDefaultErrorCode(status)),
			"message": firstNonEmptyString(videoStringValue(task.LastErrorMessage), arkVideoDefaultErrorMessage(status)),
		}
	}
	return object
}

func arkVideoDefaultErrorCode(status string) string {
	switch status {
	case ArkVideoStatusCancelled:
		return "video_generation_cancelled"
	case ArkVideoStatusExpired:
		return "video_generation_expired"
	default:
		return "video_generation_failed"
	}
}

func arkVideoDefaultErrorMessage(status string) string {
	switch status {
	case ArkVideoStatusCancelled:
		return "video generation was cancelled"
	case ArkVideoStatusExpired:
		return "video generation expired before completion"
	default:
		return "video generation failed"
	}
}

// arkUsageObject 只透出上游回报过的 token 计数。sanitizeVideoProviderUsage 会把
// completion_tokens 折叠进 video_tokens，这里按 Ark 的字段名还原。
func arkUsageObject(usage map[string]any) map[string]any {
	if len(usage) == 0 {
		return nil
	}
	tokens, ok := numericMapValue(usage, "video_tokens")
	if !ok || tokens <= 0 {
		return nil
	}
	total := tokens
	if value, ok := numericMapValue(usage, "total_tokens"); ok && value >= tokens {
		total = value
	}
	return map[string]any{"completion_tokens": int64(tokens), "total_tokens": int64(total)}
}

func arkObservedString(observed, attributes map[string]any, key string) string {
	if value, ok := observed[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if value, ok := attributes[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return ""
}

func arkBoolValue(values map[string]any, key string) (bool, bool) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return false, false
	}
	switch typed := raw.(type) {
	case bool:
		return typed, true
	case *bool:
		if typed == nil {
			return false, false
		}
		return *typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	if number, ok := numericMapValue(values, key); ok {
		return number != 0, true
	}
	return false, false
}

// IsArkVideoProvider 报告该 provider 对外采用 Ark 任务协议（五家托管平台）；OpenAI
// 仍走 /v1/videos 的 OpenAI 形态。
func IsArkVideoProvider(provider string) bool {
	switch strings.TrimSpace(provider) {
	case VideoProviderByteDance, VideoProviderFal, VideoProviderWaveSpeed, VideoProviderRunware, VideoProviderFlova:
		return true
	default:
		return false
	}
}

// IsArkVideoPlatform 报告分组平台是否只通过 Ark 任务协议提供视频能力。
func IsArkVideoPlatform(platform string) bool {
	provider, ok := managedVideoProviderForPlatform(platform)
	return ok && IsArkVideoProvider(provider)
}
