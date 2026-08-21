package service

import (
	"net/url"
	"strings"
)

// cnProviderOfficialBalanceProbeSupported reports whether a PAYG account is
// routed exclusively to the provider's official API. Balance endpoints are not
// part of the OpenAI-compatible contract, so third-party gateways must never be
// auto-probed or allowed to influence scheduling.
func cnProviderOfficialBalanceProbeSupported(account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey || account.IsCodingPlan() || strings.TrimSpace(account.GetCNAPIKey()) == "" {
		return false
	}

	var officialHosts []string
	switch CanonicalCNPlatform(account.Platform) {
	case PlatformKimi:
		officialHosts = []string{"api.moonshot.cn"}
	case PlatformDeepseek:
		officialHosts = []string{"api.deepseek.com"}
	default:
		return false
	}
	return cnProviderResolvedEndpointsAreOfficial(account, officialHosts)
}

// cnProviderOfficialQuotaProbeSupported applies the same boundary to Coding
// Plan quota endpoints. Only Kimi and Zhipu expose supported official quota
// APIs; a custom inference endpoint does not imply support for those APIs.
func cnProviderOfficialQuotaProbeSupported(account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey || !account.IsCodingPlan() || strings.TrimSpace(account.GetCNAPIKey()) == "" {
		return false
	}

	var officialHosts []string
	switch account.GetCodingPlanProvider() {
	case PlatformKimi:
		officialHosts = []string{"api.kimi.com"}
	case PlatformZhipu:
		officialHosts = []string{"open.bigmodel.cn", "api.z.ai"}
	default:
		return false
	}
	return cnProviderResolvedEndpointsAreOfficial(account, officialHosts)
}

func cnProviderResolvedEndpointsAreOfficial(account *Account, officialHosts []string) bool {
	protocols := []string{account.GetAPIProtocol()}
	if account.IsAdaptiveAPIProtocol() {
		protocols = []string{APIProtocolChatCompletions, APIProtocolAnthropic}
		if account.Platform == PlatformDeepseek {
			protocols = append(protocols, APIProtocolResponses)
		}
	}
	for _, protocol := range protocols {
		if !baseURLUsesOneOfHosts(ResolveCompatibleGatewayBaseURL(account, protocol), officialHosts...) {
			return false
		}
	}
	return true
}

func baseURLUsesOneOfHosts(raw string, expectedHosts ...string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return false
	}
	for _, expected := range expectedHosts {
		if host == strings.TrimSuffix(strings.ToLower(strings.TrimSpace(expected)), ".") {
			return true
		}
	}
	return false
}
