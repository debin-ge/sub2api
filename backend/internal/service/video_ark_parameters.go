package service

import (
	"sort"
	"strings"
	"time"
)

// Ark 的 execution_expires_after 取值范围（秒）：任务创建后在此时限内未开始执行则过期。
const (
	ArkExecutionExpiresMinSeconds = 3600
	ArkExecutionExpiresMaxSeconds = 259200
)

// VideoArkParameterSupport 描述 provider 对 Ark 开关字段的真实支持情况。
//
// 字段为 false 不等于「带值就拒绝」：与厂商固有行为等价的中性值（不加水印、不要尾帧、
// 不固定镜头、不出声、随机种子、默认服务档）是空操作，由 normalizeNeutralArkParameters
// 清空并记为 ignored；只有真正要求厂商做到做不到的事（watermark=true 等）才留给校验拒绝。
// ExecutionExpiresAfter 为 true 表示 provider 会把排队超时原样交给上游执行（Ark 直连），
// 否则由本网关的 worker 自己执行。
type VideoArkParameterSupport struct {
	Seed                  bool
	CameraFixed           bool
	Watermark             bool
	GenerateAudio         bool
	ReturnLastFrame       bool
	ServiceTier           bool
	ExecutionExpiresAfter bool
}

// ArkNativeParameterSupport 是 Ark 直连 provider 的支持表：全部字段原样透传。
func ArkNativeParameterSupport() VideoArkParameterSupport {
	return VideoArkParameterSupport{
		Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true,
		ReturnLastFrame: true, ServiceTier: true, ExecutionExpiresAfter: true,
	}
}

// VideoArkParameterNormalizer 由非 Ark 直连的 provider 实现，告诉任务服务哪些 Ark 开关
// 它能真正落到上游；任务服务据此在校验前清掉中性值。
type VideoArkParameterNormalizer interface {
	ArkParameterSupport(account *Account, request VideoCreateRequest) VideoArkParameterSupport
}

// ArkServiceTierDefault 是 Ark 的默认服务档；flex 是更慢更便宜的档，非 Ark 上游没有对应物。
const ArkServiceTierDefault = "default"

// normalizeNeutralArkParameters 清空 provider 不支持但取值等于其固有行为的 Ark 开关，返回
// 被忽略的字段名（按字典序）。非中性值原样保留，交给 provider 校验拒绝。
func normalizeNeutralArkParameters(request VideoCreateRequest, support VideoArkParameterSupport) (VideoCreateRequest, []string) {
	ignored := make([]string, 0, 6)
	if !support.Seed && request.Seed != nil && *request.Seed == -1 {
		request.Seed = nil
		ignored = append(ignored, "seed")
	}
	if !support.CameraFixed && request.CameraFixed != nil && !*request.CameraFixed {
		request.CameraFixed = nil
		ignored = append(ignored, "camera_fixed")
	}
	if !support.Watermark && request.Watermark != nil && !*request.Watermark {
		request.Watermark = nil
		ignored = append(ignored, "watermark")
	}
	if !support.GenerateAudio && request.AudioEnabled != nil && !*request.AudioEnabled {
		request.AudioEnabled = nil
		ignored = append(ignored, "generate_audio")
	}
	if !support.ReturnLastFrame && request.ReturnLastFrame != nil && !*request.ReturnLastFrame {
		request.ReturnLastFrame = nil
		ignored = append(ignored, "return_last_frame")
	}
	if !support.ServiceTier && strings.EqualFold(strings.TrimSpace(request.ServiceTier), ArkServiceTierDefault) {
		request.ServiceTier = ""
		ignored = append(ignored, "service_tier")
	}
	sort.Strings(ignored)
	if len(ignored) == 0 {
		return request, nil
	}
	return request, ignored
}

// VideoExecutionExpiryDeadline 返回任务按 execution_expires_after 应过期的时刻；没有配置
// 或由上游自行执行时返回 ok=false。
func VideoExecutionExpiryDeadline(task *VideoTask) (time.Time, bool) {
	if task == nil {
		return time.Time{}, false
	}
	enforced, _ := arkBoolValue(task.RequestAttributes, "execution_expiry_enforced_locally")
	if !enforced {
		return time.Time{}, false
	}
	seconds, ok := numericMapValue(task.RequestAttributes, "execution_expires_after")
	if !ok || seconds <= 0 {
		return time.Time{}, false
	}
	return task.CreatedAt.Add(time.Duration(seconds) * time.Second), true
}

// videoExecutionExpired 报告一次上游观测是否应被本网关判为「排队超时」：任务仍未开始
// 执行（上游与本地都停在 queued），且已过 execution_expires_after 的时限。
func videoExecutionExpired(task *VideoTask, observed *ProviderVideoTask, now time.Time) bool {
	if task == nil || observed == nil {
		return false
	}
	if task.GenerationState != VideoGenerationQueued {
		return false
	}
	if normalizedVideoProviderGenerationState(observed.Status, task.GenerationState) != VideoGenerationQueued {
		return false
	}
	deadline, ok := VideoExecutionExpiryDeadline(task)
	return ok && !now.Before(deadline)
}

// videoExecutionExpiredObservation 合成一条过期观测，交给 ReconcileProviderObservation 走
// 与上游 expired 相同的终态与释放预扣路径。
func videoExecutionExpiredObservation(task *VideoTask) *ProviderVideoTask {
	return &ProviderVideoTask{
		ProviderTaskID: videoStringValue(task.ProviderTaskID),
		Status:         VideoGenerationExpired,
		RawStatus:      "expired",
		ErrorCode:      "execution_expired",
		ErrorMessage:   "task did not start within execution_expires_after",
	}
}
