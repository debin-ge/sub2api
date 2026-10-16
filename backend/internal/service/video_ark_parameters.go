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
// 字段为 false 时任务服务不再拒绝：seed / camera_fixed / watermark / generate_audio /
// return_last_frame 不论取值都清空并记为 ignored（不改变本网关的成本，客户端从响应的
// ignored_parameters 得知未生效）；service_tier 只清默认档，flex 等改变计费的档位仍留给
// 校验拒绝。SmartDuration 为 false 时 duration=-1 与 frames 由任务服务按模型能力换算成
// 秒数（记为 adjusted）。ExecutionExpiresAfter 为 true 表示 provider 会把排队超时原样交给
// 上游执行（Ark 直连），否则由本网关的 worker 自己执行。
//
// 能把开关换成别的形式落地的 provider（例如 Flova 把 camera_fixed 写进指令）把对应字段
// 声明为 true，自行解释取值。
type VideoArkParameterSupport struct {
	Seed                  bool
	CameraFixed           bool
	Watermark             bool
	GenerateAudio         bool
	ReturnLastFrame       bool
	ServiceTier           bool
	SmartDuration         bool
	ExecutionExpiresAfter bool
}

// ArkNativeParameterSupport 是 Ark 直连 provider 的支持表：全部字段原样透传。
func ArkNativeParameterSupport() VideoArkParameterSupport {
	return VideoArkParameterSupport{
		Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true,
		ReturnLastFrame: true, ServiceTier: true, SmartDuration: true, ExecutionExpiresAfter: true,
	}
}

// VideoArkParameterNormalizer 由非 Ark 直连的 provider 实现，告诉任务服务哪些 Ark 开关
// 它能真正落到上游；任务服务据此在校验前清掉中性值。
type VideoArkParameterNormalizer interface {
	ArkParameterSupport(account *Account, request VideoCreateRequest) VideoArkParameterSupport
}

// ArkServiceTierDefault 是 Ark 的默认服务档；flex 是更慢更便宜的档，非 Ark 上游没有对应物。
const ArkServiceTierDefault = "default"

// normalizeUnsupportedArkParameters 清空 provider 落不了地的 Ark 开关，返回被忽略的字段名
// （按字典序）。service_tier 只有默认档是空操作，其它档位原样保留交给 provider 校验拒绝。
func normalizeUnsupportedArkParameters(request VideoCreateRequest, support VideoArkParameterSupport) (VideoCreateRequest, []string) {
	ignored := make([]string, 0, 6)
	if !support.Seed && request.Seed != nil {
		request.Seed = nil
		ignored = append(ignored, "seed")
	}
	if !support.CameraFixed && request.CameraFixed != nil {
		request.CameraFixed = nil
		ignored = append(ignored, "camera_fixed")
	}
	if !support.Watermark && request.Watermark != nil {
		request.Watermark = nil
		ignored = append(ignored, "watermark")
	}
	if !support.GenerateAudio && request.AudioEnabled != nil {
		request.AudioEnabled = nil
		ignored = append(ignored, "generate_audio")
	}
	if !support.ReturnLastFrame && request.ReturnLastFrame != nil {
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

// adjustArkDurationForProvider 把 provider 不支持的 duration=-1 / frames 换算成明确秒数，返回
// 被换算的字段名。智能时长取模型默认时长；帧数按 24fps 向上取整后取模型支持的最近档位
// （等距取较长者，与预扣的向上取整口径一致）。能力表不认识的模型没有默认时长可用，
// duration=-1 原样保留交给 provider 校验。
func adjustArkDurationForProvider(request VideoCreateRequest, support VideoArkParameterSupport, caps VideoCapabilities) (VideoCreateRequest, []string) {
	if support.SmartDuration || (!request.SmartDuration && request.Frames <= 0) {
		return request, nil
	}
	model := canonicalOpenAIVideoModel(firstNonEmptyString(request.RequestedModel, request.Model))
	if request.Frames > 0 {
		seconds := (request.Frames + videoArkFramesPerSecond - 1) / videoArkFramesPerSecond
		request.Seconds = nearestVideoSeconds(seconds, caps.SupportedSeconds[model])
		request.Frames = 0
		request.SmartDuration = false
		return request, []string{"frames"}
	}
	seconds := caps.DefaultSeconds[model]
	if seconds <= 0 {
		return request, nil
	}
	request.Seconds = seconds
	request.SmartDuration = false
	return request, []string{"duration"}
}

func nearestVideoSeconds(target int, supported []int) int {
	best := target
	bestDistance := -1
	for _, candidate := range supported {
		distance := candidate - target
		if distance < 0 {
			distance = -distance
		}
		if bestDistance < 0 || distance < bestDistance || (distance == bestDistance && candidate > best) {
			best, bestDistance = candidate, distance
		}
	}
	return best
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
