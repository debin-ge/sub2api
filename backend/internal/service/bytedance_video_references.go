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

// OpenAI 形态的 Seedance 中继对 seconds 的解析类型不一：有的按数字，有的按字符串。网关
// 不按模型名猜，而是先按数字（或该账号上次成功的编码）发，被中继以类型不匹配拒绝时换另
// 一种重发一次，并记住结果；账号凭据 seconds_encoding（string / number）可显式固定。
const byteDanceSecondsEncodingCredentialKey = "seconds_encoding"

const (
	byteDanceSecondsAsNumber = "number"
	byteDanceSecondsAsString = "string"
)

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

// relaySecondsEncoding 返回本次首选的 seconds 编码；pinned 为真表示由凭据固定，不再探测。
func (p *ByteDanceVideoProvider) relaySecondsEncoding(account *Account) (encoding string, pinned bool) {
	if account != nil {
		switch strings.ToLower(strings.TrimSpace(account.GetCredential(byteDanceSecondsEncodingCredentialKey))) {
		case "string", "str":
			return byteDanceSecondsAsString, true
		case "number", "int", "integer":
			return byteDanceSecondsAsNumber, true
		}
		if p != nil {
			if remembered, ok := p.relaySecondsEncodings.Load(account.ID); ok {
				if value, ok := remembered.(string); ok && value != "" {
					return value, false
				}
			}
		}
	}
	return byteDanceSecondsAsNumber, false
}

func (p *ByteDanceVideoProvider) rememberRelaySecondsEncoding(account *Account, encoding string) {
	if p == nil || account == nil || account.ID == 0 || encoding == "" {
		return
	}
	p.relaySecondsEncodings.Store(account.ID, encoding)
}

func byteDanceEncodeRelaySeconds(seconds int, encoding string) any {
	if encoding == byteDanceSecondsAsString {
		return strconv.Itoa(seconds)
	}
	return seconds
}

func byteDanceOppositeSecondsEncoding(encoding string) string {
	if encoding == byteDanceSecondsAsString {
		return byteDanceSecondsAsNumber
	}
	return byteDanceSecondsAsString
}

// byteDanceRelaySecondsTypeMismatch 识别中继「seconds 类型不对」的拒绝：4xx 且文案同时提到
// seconds 与反序列化/类型。这种错误发生在解析阶段，上游没有创建任务。
func byteDanceRelaySecondsTypeMismatch(status int, err error) bool {
	if status < 400 || status >= 500 || err == nil {
		return false
	}
	var providerErr *VideoProviderError
	if !errors.As(err, &providerErr) || providerErr == nil {
		return false
	}
	message := strings.ToLower(providerErr.Message)
	if !strings.Contains(message, "seconds") {
		return false
	}
	return strings.Contains(message, "unmarshal") || strings.Contains(message, "type") || strings.Contains(message, "string") || strings.Contains(message, "number")
}
