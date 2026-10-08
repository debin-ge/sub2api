package service

import "sort"

// 聚合媒体平台的默认模型 ID 清单（方案 §5.4 Tier A）由各适配器的档案派生，供模型目录
// 默认值与管理端白名单预设使用。
const (
	falDefaultBaseURL       = "https://queue.fal.run"
	waveSpeedDefaultBaseURL = "https://api.wavespeed.ai/api/v3"
)

// videoVendorDefaultModelIDs 返回平台的默认模型 ID（已排序）。
func videoVendorDefaultModelIDs(platform string) []string {
	var ids []string
	switch platform {
	case PlatformFal:
		for model, profile := range falVideoProfiles() {
			if profile != nil && !profile.Fallback {
				ids = append(ids, model)
			}
		}
	case PlatformWaveSpeed:
		for model, profile := range waveSpeedVideoProfiles() {
			if profile != nil && !profile.Fallback {
				ids = append(ids, model)
			}
		}
	case PlatformRunware:
		for model, profile := range runwareVideoProfiles() {
			if profile != nil && !profile.Fallback {
				ids = append(ids, model)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
