package service

import (
	"context"
	"sort"
	"strings"
)

// Ark 维度的能力目录（GET {prefix}/contents/generations/models），是 sub2api 对 Ark 协议的
// 扩展：Ark 自己没有目录接口。内容只描述"这个分组按 Ark 形态能提交什么"，与 OpenAI 形态
// 的 /v1/videos/models 互不混用。

// ArkVideoModelDurations 是可选时长：离散档位或连续区间，AllowSmart 表示接受 duration=-1。
type ArkVideoModelDurations struct {
	Values     []int `json:"values,omitempty"`
	Min        int   `json:"min,omitempty"`
	Max        int   `json:"max,omitempty"`
	Default    int   `json:"default,omitempty"`
	AllowSmart bool  `json:"allow_smart,omitempty"`
}

// ArkVideoModelFrames 描述 frames 的取值规则（25+4n）。
type ArkVideoModelFrames struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Step int `json:"step"`
}

// ArkVideoModelChoices 是一组枚举取值与默认值。
type ArkVideoModelChoices struct {
	Values  []string `json:"values"`
	Default string   `json:"default,omitempty"`
}

// ArkVideoModelFlags 列出模型接受的 Ark 开关字段。
type ArkVideoModelFlags struct {
	Seed            bool `json:"seed"`
	CameraFixed     bool `json:"camera_fixed"`
	Watermark       bool `json:"watermark"`
	GenerateAudio   bool `json:"generate_audio"`
	ReturnLastFrame bool `json:"return_last_frame"`
}

// ArkVideoContentRole 描述 content[] 里一个角色的约束。
type ArkVideoContentRole struct {
	Max      int      `json:"max"`
	Accepts  []string `json:"accepts"`
	Requires []string `json:"requires,omitempty"`
}

// ArkVideoModelOption 是 provider_options 里可用的厂商特有参数。
type ArkVideoModelOption struct {
	Name   string   `json:"name"`
	Values []string `json:"values,omitempty"`
}

// ArkVideoModel 是一个可按 Ark 形态提交的模型及其约束。
type ArkVideoModel struct {
	ID              string                         `json:"id"`
	Object          string                         `json:"object"`
	Provider        string                         `json:"provider"`
	IsDefault       bool                           `json:"is_default,omitempty"`
	Durations       *ArkVideoModelDurations        `json:"durations,omitempty"`
	Frames          *ArkVideoModelFrames           `json:"frames,omitempty"`
	Resolutions     *ArkVideoModelChoices          `json:"resolutions,omitempty"`
	Ratios          *ArkVideoModelChoices          `json:"ratios,omitempty"`
	Flags           ArkVideoModelFlags             `json:"flags"`
	ContentRoles    map[string]ArkVideoContentRole `json:"content_roles,omitempty"`
	ProviderOptions []ArkVideoModelOption          `json:"provider_options,omitempty"`
}

// ArkVideoModelsResponse 是 Ark 维度目录的响应。
type ArkVideoModelsResponse struct {
	Object string          `json:"object"`
	Data   []ArkVideoModel `json:"data"`
}

// ArkVideoModelSpec 是 provider 对某个模型的 Ark 维度自描述。
type ArkVideoModelSpec struct {
	Flags           ArkVideoModelFlags
	ContentRoles    map[VideoInputRole]ArkVideoContentRole
	ProviderOptions []ArkVideoModelOption
	FramesSupported bool
}

// ArkVideoModelDescriber 由 Ark 形态的 provider 实现，描述开关、角色与厂商参数。
type ArkVideoModelDescriber interface {
	ArkModelSpec(model string) ArkVideoModelSpec
}

// ListArkVideoModels 按 API Key 所属分组列出可按 Ark 形态提交的模型。模型集合与定价
// 来源同 ListVideoModels；只输出五家 Ark 形态 provider 的条目。
func (s *VideoTaskService) ListArkVideoModels(ctx context.Context, apiKey *APIKey) (*ArkVideoModelsResponse, error) {
	scopes, owners, err := s.videoModelCatalogScopes(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	data := make([]ArkVideoModel, 0, 8)
	for _, scope := range scopes {
		if !IsArkVideoProvider(scope.provider) {
			continue
		}
		for _, model := range scope.models {
			if owners[model] != scope.provider {
				continue
			}
			if entry := s.arkVideoModelEntry(scope, model); entry != nil {
				data = append(data, *entry)
			}
		}
	}
	return &ArkVideoModelsResponse{Object: "list", Data: data}, nil
}

func (s *VideoTaskService) arkVideoModelEntry(scope videoModelCatalogScope, model string) *ArkVideoModel {
	canonical := canonicalOpenAIVideoModel(model)
	profile := s.videoTechnicalProfile(scope.platform, model)
	if videoModelBelongsElsewhere(scope, model, profile) {
		return nil
	}
	caps := scope.capabilities
	entry := &ArkVideoModel{
		ID: model, Object: videoModelObject, Provider: scope.provider,
		IsDefault: model == normalizePricingModelKey(caps.DefaultModel),
	}
	if seconds := videoModelSeconds(scope.provider, caps, canonical, profile); seconds != nil {
		entry.Durations = &ArkVideoModelDurations{
			Values: seconds.Values, Min: seconds.Min, Max: seconds.Max, Default: seconds.Default,
			AllowSmart: caps.SmartDurationModels[canonical],
		}
	}
	entry.Resolutions = arkVideoModelResolutions(caps, canonical, profile)
	if ratios := caps.SupportedRatios[canonical]; len(ratios) > 0 {
		entry.Ratios = &ArkVideoModelChoices{Values: append([]string(nil), ratios...), Default: caps.DefaultRatios[canonical]}
	}
	spec := s.arkVideoModelSpec(scope.provider, model, caps)
	entry.Flags = spec.Flags
	if spec.FramesSupported {
		entry.Frames = &ArkVideoModelFrames{Min: byteDanceMinFrames, Max: byteDanceMaxFrames, Step: 4}
	}
	if len(spec.ContentRoles) > 0 {
		entry.ContentRoles = make(map[string]ArkVideoContentRole, len(spec.ContentRoles))
		for role, constraint := range spec.ContentRoles {
			entry.ContentRoles[string(role)] = constraint
		}
	}
	entry.ProviderOptions = spec.ProviderOptions
	return entry
}

// arkVideoModelResolutions 先取能力表，再用价目声明过的分辨率收窄（没配价的档位提交会被拒）。
func arkVideoModelResolutions(caps VideoCapabilities, canonical string, profile *VideoPricingConfig) *ArkVideoModelChoices {
	values := append([]string(nil), caps.SupportedResolutions[canonical]...)
	if len(values) == 0 {
		return nil
	}
	if profile != nil && len(profile.Resolutions) > 0 {
		narrowed := make([]string, 0, len(values))
		for _, value := range values {
			for name := range profile.Resolutions {
				if strings.EqualFold(strings.TrimSpace(name), value) {
					narrowed = append(narrowed, value)
					break
				}
			}
		}
		if len(narrowed) > 0 {
			values = narrowed
		}
	}
	def := caps.DefaultResolutions[canonical]
	if profile != nil && strings.TrimSpace(profile.Defaults.Resolution) != "" {
		for _, value := range values {
			if strings.EqualFold(value, strings.TrimSpace(profile.Defaults.Resolution)) {
				def = value
			}
		}
	}
	found := false
	for _, value := range values {
		if value == def {
			found = true
		}
	}
	if !found {
		def = values[0]
	}
	return &ArkVideoModelChoices{Values: values, Default: def}
}

func (s *VideoTaskService) arkVideoModelSpec(provider, model string, caps VideoCapabilities) ArkVideoModelSpec {
	if s != nil && s.providers != nil {
		if live, ok := s.providers.Get(provider); ok && live != nil {
			if describer, ok := live.(ArkVideoModelDescriber); ok {
				return describer.ArkModelSpec(model)
			}
		}
	}
	if adapter := videoVendorAdapterByName(provider); adapter != nil {
		return newVideoVendorProvider(adapter, nil, nil).ArkModelSpec(model)
	}
	return arkVideoModelSpecFromCapabilities(caps)
}

// arkVideoModelSpecFromCapabilities 在 provider 没有自描述时，只按能力表里的角色上限给出保守描述。
func arkVideoModelSpecFromCapabilities(caps VideoCapabilities) ArkVideoModelSpec {
	spec := ArkVideoModelSpec{ContentRoles: map[VideoInputRole]ArkVideoContentRole{}}
	roles := make([]VideoInputRole, 0, len(caps.MaxContentByRole))
	for role := range caps.MaxContentByRole {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	for _, role := range roles {
		spec.ContentRoles[role] = ArkVideoContentRole{Max: caps.MaxContentByRole[role], Accepts: []string{videoReferenceAcceptsHTTPS}}
	}
	return spec
}

// arkVideoProviderOptionsFromWhitelist 把厂商 provider_options 白名单整理成目录条目。
func arkVideoProviderOptionsFromWhitelist(whitelist map[string]string) []ArkVideoModelOption {
	keys := make([]string, 0, len(whitelist))
	for key := range whitelist {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	options := make([]ArkVideoModelOption, 0, len(keys))
	for _, key := range keys {
		options = append(options, ArkVideoModelOption{Name: key})
	}
	return options
}

// ArkModelSpec：Ark 原生支持全部开关与全部媒体角色（视频 / 音频引用只接受 https）。
func (p *ByteDanceVideoProvider) ArkModelSpec(string) ArkVideoModelSpec {
	imageAccepts := []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI}
	caps := p.Capabilities()
	roles := map[VideoInputRole]ArkVideoContentRole{}
	for role, maximum := range caps.MaxContentByRole {
		constraint := ArkVideoContentRole{Max: maximum, Accepts: []string{videoReferenceAcceptsHTTPS}}
		if role.IsImageContentRole() {
			constraint.Accepts = imageAccepts
		}
		if role == VideoInputRoleLastFrame {
			constraint.Requires = []string{string(VideoInputRoleFirstFrame)}
		}
		roles[role] = constraint
	}
	return ArkVideoModelSpec{
		Flags:           ArkVideoModelFlags{Seed: true, CameraFixed: true, Watermark: true, GenerateAudio: true, ReturnLastFrame: true},
		ContentRoles:    roles,
		FramesSupported: true,
	}
}

// ArkModelSpec：三家聚合平台按档案映射表给出开关；参考图只有首尾帧。
func (p *VideoVendorProvider) ArkModelSpec(model string) ArkVideoModelSpec {
	profile := p.profileFor(model)
	if profile == nil {
		return ArkVideoModelSpec{}
	}
	profile.finalizeArkDimensions()
	spec := ArkVideoModelSpec{
		Flags: ArkVideoModelFlags{
			Seed: profile.ArkFieldMap["seed"] != "", CameraFixed: profile.ArkFieldMap["camera_fixed"] != "",
			Watermark: profile.ArkFieldMap["watermark"] != "", GenerateAudio: profile.AudioField != "" || profile.ArkFieldMap["generate_audio"] != "",
		},
		ContentRoles:    map[VideoInputRole]ArkVideoContentRole{},
		ProviderOptions: arkVideoProviderOptionsFromWhitelist(profile.OptionWhitelist),
	}
	if profile.AllowsImage {
		accepts := []string{videoReferenceAcceptsHTTPS, videoReferenceAcceptsDataURI}
		spec.ContentRoles[VideoInputRoleFirstFrame] = ArkVideoContentRole{Max: 1, Accepts: accepts}
		if profile.MaxImages >= 2 {
			spec.ContentRoles[VideoInputRoleLastFrame] = ArkVideoContentRole{Max: 1, Accepts: accepts, Requires: []string{string(VideoInputRoleFirstFrame)}}
		}
	}
	return spec
}

// ArkModelSpec：Flova 没有任何开关；参考图必须是字节（先上传到 Flova 存储）。
func (p *FlovaVideoProvider) ArkModelSpec(string) ArkVideoModelSpec {
	return ArkVideoModelSpec{
		ContentRoles: map[VideoInputRole]ArkVideoContentRole{
			VideoInputRoleFirstFrame: {Max: 1, Accepts: []string{videoReferenceAcceptsDataURI}},
		},
	}
}
