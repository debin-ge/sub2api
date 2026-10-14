package service

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// byteDanceSeedanceModelPrefix matches Ark's Seedance family.
//
// Ark model identifiers are fully hyphenated and carry a release-date suffix,
// for example "doubao-seedance-1-0-pro-250528". The output resolution is a
// request parameter, not part of the identifier, so no generation parameter may
// be parsed back out of a model name.
const byteDanceSeedanceModelPrefix = "doubao-seedance-"

// OpenAI 形态的 Seedance 中继大多把分辨率写进模型名（doubao-seedance-2.5-720p、
// doubao-seedance-2.0-pro-1080p），并要求 seconds 以 JSON 字符串传递；Ark 原生 ID 用连字符
// 版本号加发布日期（doubao-seedance-1-0-pro-250528），seconds 是数字。模型名的写法是判断
// 中继家族的依据；账号凭据 seconds_encoding 可显式指定，优先于模型名推断。
const byteDanceSecondsEncodingCredentialKey = "seconds_encoding"

var byteDanceRelayResolutionModelPattern = regexp.MustCompile(`^doubao-seedance-\d+\.\d+(?:-[a-z0-9]+)*-(?:480p|720p|1080p|2k|4k)$`)

// Ark 请求参数的硬边界（创建任务文档）。各模型的实际子集由能力表收窄；这里只拦住
// 明显越界、会在预扣后才被上游拒绝的值。
const (
	byteDanceMaxRequestSeconds       = 60
	byteDanceMinFrames               = 29
	byteDanceMaxFrames               = 289
	byteDanceMaxSeed                 = int64(1)<<32 - 1
	byteDanceMinExecutionExpiresSecs = ArkExecutionExpiresMinSeconds
	byteDanceMaxExecutionExpiresSecs = ArkExecutionExpiresMaxSeconds
)

// validateByteDanceArkRequest 校验 Ark 维度的请求字段。画幅、时长、种子等都以显式字段
// 为准：入口层已把提示词里的 --flag 解析并写回字段，这里不再看提示词。
func validateByteDanceArkRequest(request VideoCreateRequest) error {
	rawModel := strings.TrimSpace(firstNonEmptyString(request.Model, request.RequestedModel))
	if rawModel == "" || rawModel != strings.ToLower(rawModel) {
		return errors.New("model is required and must be lowercase")
	}
	if request.HasOpenAIOnlyFields() {
		return errors.New("size, quality, aspect_ratio, input_reference and characters are OpenAI-only fields; use resolution, ratio and content[]")
	}
	if request.Frames > 0 {
		if request.Seconds > 0 || request.SmartDuration {
			return errors.New("duration and frames cannot both be specified")
		}
		if request.Frames < byteDanceMinFrames || request.Frames > byteDanceMaxFrames || (request.Frames-25)%4 != 0 {
			return fmt.Errorf("frames must satisfy 25+4n within [%d, %d]", byteDanceMinFrames, byteDanceMaxFrames)
		}
	} else if !request.SmartDuration {
		if request.Seconds <= 0 {
			return errors.New("duration is required")
		}
		if request.Seconds > byteDanceMaxRequestSeconds {
			return errors.New("duration exceeds the supported range")
		}
	}
	if resolution := strings.TrimSpace(request.Resolution); resolution != "" && !IsArkVideoResolution(resolution) {
		return fmt.Errorf("resolution %q is not supported", resolution)
	}
	if ratio := strings.TrimSpace(request.Ratio); ratio != "" && !IsArkVideoRatio(ratio) {
		return fmt.Errorf("ratio %q is not supported", ratio)
	}
	if request.Seed != nil && (*request.Seed < -1 || *request.Seed > byteDanceMaxSeed) {
		return errors.New("seed must be within [-1, 2^32-1]")
	}
	if request.ExecutionExpiresAfter != 0 &&
		(request.ExecutionExpiresAfter < byteDanceMinExecutionExpiresSecs || request.ExecutionExpiresAfter > byteDanceMaxExecutionExpiresSecs) {
		return fmt.Errorf("execution_expires_after must be within [%d, %d] seconds", byteDanceMinExecutionExpiresSecs, byteDanceMaxExecutionExpiresSecs)
	}
	if tier := strings.TrimSpace(request.ServiceTier); tier != "" && tier != "default" && tier != "flex" {
		return errors.New("service_tier must be default or flex")
	}
	return nil
}

func hasByteDanceSeedanceModel(request VideoCreateRequest) bool {
	for _, candidate := range []string{request.Model, request.RequestedModel} {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(candidate)), byteDanceSeedanceModelPrefix) {
			return true
		}
	}
	return false
}

// byteDanceOpenAICompatibleSeconds 决定发给中继的 seconds 编码：凭据 seconds_encoding
// （string / number）优先，否则「分辨率写进模型名」的中继用字符串，其余用数字。
func byteDanceOpenAICompatibleSeconds(account *Account, request VideoCreateRequest) any {
	if account != nil {
		switch strings.ToLower(strings.TrimSpace(account.GetCredential(byteDanceSecondsEncodingCredentialKey))) {
		case "string", "str":
			return strconv.Itoa(request.Seconds)
		case "number", "int", "integer":
			return request.Seconds
		}
	}
	if byteDanceRelayModelWithResolutionSuffix(request) {
		return strconv.Itoa(request.Seconds)
	}
	return request.Seconds
}

// byteDanceRelayModelWithResolutionSuffix 报告模型名是否属于「点号版本 + 分辨率后缀」的
// 中继命名家族（大小写必须是小写原样）。
func byteDanceRelayModelWithResolutionSuffix(request VideoCreateRequest) bool {
	for _, candidate := range []string{request.Model, request.RequestedModel} {
		model := strings.TrimSpace(candidate)
		if model != "" && model == strings.ToLower(model) && byteDanceRelayResolutionModelPattern.MatchString(model) {
			return true
		}
	}
	return false
}
