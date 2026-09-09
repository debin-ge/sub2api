package service

import (
	"errors"
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

// byteDanceMaxRequestSeconds is a gateway sanity bound, not an Ark contract.
// Per-model duration limits belong in the capability catalog's SupportedSeconds;
// this only stops an absurd value from being quoted and held before the
// upstream rejects it.
const byteDanceMaxRequestSeconds = 60

func validateByteDanceSeedanceRequest(request VideoCreateRequest) error {
	rawModel := strings.TrimSpace(firstNonEmptyString(request.Model, request.RequestedModel))
	if rawModel == "" || rawModel != strings.ToLower(rawModel) {
		return errors.New("model is required and must be lowercase")
	}
	// Ark selects the frame size from resolution plus ratio. Accepting OpenAI's
	// "WxH" here would have to guess a mapping, and a wrong guess bills one
	// resolution while generating another, so it is refused outright.
	if strings.TrimSpace(request.Size) != "" || request.Width > 0 || request.Height > 0 {
		return errors.New("size is not supported; use provider_options.resolution and provider_options.ratio")
	}
	if request.Seconds <= 0 {
		return errors.New("seconds is required")
	}
	if request.Seconds > byteDanceMaxRequestSeconds {
		return errors.New("seconds exceeds the supported range")
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
