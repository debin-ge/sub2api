package service

import "strings"

// managedVideoPlatforms 是托管视频平台（走 VideoTaskService 的提交 / 预扣 / 轮询 /
// 结算）与其 VideoProvider 名的唯一映射表。新增供应商只在这里加一行；其余代码一律
// 通过下面的查询函数判断，不再逐处写死平台常量。
//
// provider 名必须与 platform 常量相同：CheckVideoAdmission 拿到的 platform 实参是
// provider.Name()（见 VideoTaskService.Submit），再用它查用户平台配额，名字不同会让
// 配额静默失效。
var managedVideoPlatforms = map[string]string{
	PlatformOpenAI:    VideoProviderOpenAI,
	PlatformByteDance: VideoProviderByteDance,
	PlatformFal:       VideoProviderFal,
	PlatformWaveSpeed: VideoProviderWaveSpeed,
	PlatformRunware:   VideoProviderRunware,
	PlatformFlova:     VideoProviderFlova,
}

// managedVideoPlatformOrder 固定枚举顺序：composite 分组枚举模型、内置能力目录都按
// 这个顺序输出，避免 map 迭代带来的随机顺序。
var managedVideoPlatformOrder = []string{
	PlatformOpenAI, PlatformByteDance, PlatformFal, PlatformWaveSpeed, PlatformRunware, PlatformFlova,
}

// videoOnlyPlatforms 列出"分组只提供 Videos API"的平台：网关对它们拦下除 /videos、
// /models、/balance、/usage 以外的请求，管理端只允许 apikey 类型账号。OpenAI 不在
// 其中，它还承担聊天流量。
var videoOnlyPlatforms = map[string]bool{
	PlatformByteDance: true, PlatformFal: true, PlatformWaveSpeed: true, PlatformRunware: true,
	PlatformFlova: true,
}

func managedVideoProviderForPlatform(platform string) (string, bool) {
	provider, ok := managedVideoPlatforms[strings.TrimSpace(platform)]
	return provider, ok
}

// videoPricingPlatformForProvider 是 managedVideoProviderForPlatform 的反向映射。
// 定价查表用的是平台维度；两边取值目前逐字相同，但仍走表而不写等号，改名时才不会
// 静默串味。
func videoPricingPlatformForProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	for _, platform := range managedVideoPlatformOrder {
		if managedVideoPlatforms[platform] == provider {
			return platform
		}
	}
	return provider
}

// IsManagedVideoPlatform 报告 platform 是否由通用视频任务平台托管（不含 composite）。
func IsManagedVideoPlatform(platform string) bool {
	_, ok := managedVideoProviderForPlatform(platform)
	return ok
}

// IsVideoOnlyPlatform 报告该平台的分组是否只提供 Videos API。
func IsVideoOnlyPlatform(platform string) bool {
	return videoOnlyPlatforms[strings.TrimSpace(platform)]
}

// ManagedVideoProviderNames 按固定顺序返回全部托管视频 provider 名。
func ManagedVideoProviderNames() []string {
	names := make([]string, 0, len(managedVideoPlatformOrder))
	for _, platform := range managedVideoPlatformOrder {
		names = append(names, managedVideoPlatforms[platform])
	}
	return names
}

// VideoOnlyPlatformLabel 给网关的拒绝文案用；bytedance 的文案被路由契约测试锁定。
func VideoOnlyPlatformLabel(platform string) string {
	switch strings.TrimSpace(platform) {
	case PlatformByteDance:
		return "ByteDance"
	case PlatformFal:
		return "fal.ai"
	case PlatformWaveSpeed:
		return "WaveSpeed"
	case PlatformRunware:
		return "Runware"
	case PlatformFlova:
		return "Flova"
	default:
		return strings.TrimSpace(platform)
	}
}

// builtinVideoCapabilitiesForProvider 返回 provider 的内置能力快照。运行时目录里没有
// 该 provider 时，各 provider 的 Capabilities() 也回落到它。尚未落地适配器的聚合平台
// 返回 false，内置目录与"被别家认领的模型"检查都会跳过它。
func builtinVideoCapabilitiesForProvider(provider string) (VideoCapabilities, bool) {
	switch strings.TrimSpace(provider) {
	case VideoProviderOpenAI:
		return DefaultOpenAIVideoCapabilities(), true
	case VideoProviderByteDance:
		return DefaultByteDanceVideoCapabilities(), true
	case VideoProviderFlova:
		return DefaultFlovaVideoCapabilities(), true
	default:
		if adapter := videoVendorAdapterByName(provider); adapter != nil {
			return adapter.DefaultCapabilities(), true
		}
		return VideoCapabilities{}, false
	}
}
