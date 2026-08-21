package service

// miniMaxOfficialRemainsProbeSupported reports whether both MiniMax protocol
// endpoints resolve to official provider hosts. The Token Plan remains API is
// provider-specific and must not be inferred from a third-party compatible
// inference endpoint.
func miniMaxOfficialRemainsProbeSupported(account *Account) bool {
	if account == nil || !account.IsMiniMaxTokenPlan() {
		return false
	}
	return baseURLUsesOneOfHosts(account.GetMiniMaxAnthropicBaseURL(), miniMaxInternationalHost, miniMaxChinaHost) &&
		baseURLUsesOneOfHosts(account.GetMiniMaxOpenAIBaseURL(), miniMaxInternationalHost, miniMaxChinaHost)
}
