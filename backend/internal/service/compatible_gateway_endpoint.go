package service

import "strings"

// resolveCNProviderBaseURL is the single endpoint resolver for the shared
// CompatibleGateway and all CN-provider probes. Protocol-specific fields are
// retained for legacy accounts, while the generic base_url written by the
// current admin UI remains the common fallback.
func resolveCNProviderBaseURL(account *Account, protocol string) string {
	return resolveCNProviderBaseURLWithLegacyDefaults(account, protocol, false)
}

func resolveCNProviderBaseURLWithLegacyDefaults(account *Account, protocol string, legacyKimiCodingDefault bool) string {
	if account == nil || !account.IsCNProvider() {
		return ""
	}

	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeUpstream {
		legacyKey := "base_url_openai"
		if protocol == APIProtocolAnthropic {
			legacyKey = "base_url_anthropic"
		}
		if baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential(legacyKey)), "/"); baseURL != "" {
			return baseURL
		}
		if baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/"); baseURL != "" {
			return baseURL
		}
	}

	switch CanonicalCNPlatform(account.Platform) {
	case PlatformKimi:
		if protocol == APIProtocolAnthropic {
			if account.GetAccountMode() == AccountModeCoding || (legacyKimiCodingDefault && account.GetAccountMode() == "") {
				return DefaultKimiCodingAnthropicBaseURL
			}
			return DefaultKimiPayGAnthropicBaseURL
		}
		if account.GetAccountMode() == AccountModeCoding || (legacyKimiCodingDefault && account.GetAccountMode() == "") {
			return DefaultKimiCodingBaseURL
		}
		return DefaultKimiPayGBaseURL
	case PlatformZhipu:
		if protocol == APIProtocolAnthropic {
			return DefaultZhipuAnthropicBaseURL
		}
		if account.GetAccountMode() == AccountModeCoding {
			return DefaultZhipuCodingBaseURL
		}
		return DefaultZhipuPayGBaseURL
	case PlatformDeepseek:
		if protocol == APIProtocolAnthropic {
			return DefaultDeepseekAnthropicBaseURL
		}
		return DefaultDeepseekBaseURL
	default:
		return ""
	}
}

// ResolveCompatibleGatewayBaseURL exposes the canonical resolver to auxiliary
// services such as model discovery and provider probes without reintroducing
// provider-specific URL precedence rules.
func ResolveCompatibleGatewayBaseURL(account *Account, protocol string) string {
	return resolveCNProviderBaseURL(account, protocol)
}
