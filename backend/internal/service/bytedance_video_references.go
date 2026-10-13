package service

import (
	"errors"
	"fmt"
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

// Some legacy OpenAI-compatible Seedance 2.0 relays use model identifiers
// that include the resolution and require seconds to be encoded as a JSON
// string. Ark and newer compatible relays use numeric duration fields.
const legacyOpenAICompatibleSeedance20Prefix = "doubao-seedance-2.0-"

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

func byteDanceOpenAICompatibleSeconds(request VideoCreateRequest) any {
	if isLegacyOpenAICompatibleSeedance20Request(request) {
		return strconv.Itoa(request.Seconds)
	}
	return request.Seconds
}

func isLegacyOpenAICompatibleSeedance20Request(request VideoCreateRequest) bool {
	for _, candidate := range []string{request.Model, request.RequestedModel} {
		model := strings.TrimSpace(candidate)
		if model == strings.ToLower(model) && strings.HasPrefix(model, legacyOpenAICompatibleSeedance20Prefix) &&
			validLegacyOpenAICompatibleSeedance20Model(model) {
			return true
		}
	}
	return false
}

func validLegacyOpenAICompatibleSeedance20Model(model string) bool {
	parts := strings.Split(strings.TrimPrefix(model, legacyOpenAICompatibleSeedance20Prefix), "-")
	if len(parts) != 2 {
		return false
	}
	variant, resolution := parts[0], parts[1]
	if variant != "mini" && variant != "fast" && variant != "pro" {
		return false
	}
	switch resolution {
	case "480p", "720p", "1080p":
		return true
	case "4k":
		return variant == "pro"
	default:
		return false
	}
}
