package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
)

// internalRelayBaseURLCredentialKeys returns every endpoint an API-key account
// can use for normal model traffic. Platforms with separate Anthropic/OpenAI
// compatibility endpoints must point both endpoints at loopback before the
// account can be enabled as an internal relay.
func internalRelayBaseURLCredentialKeys(platform string) []string {
	switch strings.TrimSpace(platform) {
	case PlatformMiniMax, PlatformGLM, PlatformKimi, PlatformDeepSeek:
		return []string{"base_url_anthropic", "base_url_openai"}
	default:
		return []string{"base_url"}
	}
}

func internalRelayBaseURLs(platform string, credentials map[string]any) []string {
	keys := internalRelayBaseURLCredentialKeys(platform)
	urls := make([]string, 0, len(keys))
	for _, key := range keys {
		raw, _ := credentials[key].(string)
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil
		}
		urls = append(urls, value)
	}
	return urls
}

func isInternalRelayEligibleConfiguration(platform, accountType string, credentials map[string]any) bool {
	if accountType != AccountTypeAPIKey {
		return false
	}
	urls := internalRelayBaseURLs(platform, credentials)
	if len(urls) == 0 {
		return false
	}
	for _, raw := range urls {
		if !internalrelay.IsLoopbackBaseURL(raw) {
			return false
		}
	}
	return true
}

func (a *Account) hasValidInternalRelayConfiguration() bool {
	return a != nil && a.IsInternalRelay() &&
		isInternalRelayEligibleConfiguration(a.Platform, a.Type, a.Credentials)
}

// validateInternalRelayOrUpstreamBaseURL keeps the normal operator URL policy
// for ordinary accounts, while allowing an explicitly configured and
// validated InternalRelay account to reach this process over HTTP loopback.
func validateInternalRelayOrUpstreamBaseURL(account *Account, raw string, validate func(string) (string, error)) (string, error) {
	if account != nil && account.hasValidInternalRelayConfiguration() && internalrelay.IsLoopbackBaseURL(raw) {
		return strings.TrimRight(strings.TrimSpace(raw), "/"), nil
	}
	return validate(raw)
}
