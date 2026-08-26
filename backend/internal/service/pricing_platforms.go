package service

// pricingPlatformCandidates returns the ordered platform identities used by
// global model-price overrides. Group scope wins because the admin catalog is
// grouped by Group.Platform (including composite); the concrete account
// platform is the fallback for composite and forced-platform routing.
func pricingPlatformCandidates(apiKey *APIKey, account *Account) []string {
	platforms := make([]string, 0, 2)
	if apiKey != nil && apiKey.Group != nil {
		platforms = append(platforms, apiKey.Group.Platform)
	}
	if account != nil {
		platforms = append(platforms, account.Platform)
	}
	return normalizePricingPlatforms(platforms)
}

func normalizePricingPlatforms(platforms []string) []string {
	out := make([]string, 0, len(platforms))
	seen := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platform = normalizeOverridePlatform(platform)
		if platform == "" {
			continue
		}
		if _, ok := seen[platform]; ok {
			continue
		}
		seen[platform] = struct{}{}
		out = append(out, platform)
	}
	return out
}

// basePricingPlatform is used for provider-specific policy attached to a base
// catalog/fallback price (currently DeepSeek peak/off-peak pricing). Composite
// is a routing scope, not an upstream provider, so prefer the first concrete
// platform when one is available.
func basePricingPlatform(platforms []string) string {
	normalized := normalizePricingPlatforms(platforms)
	for _, platform := range normalized {
		if platform != PlatformComposite && platform != ModelPriceOverrideWildcardPlatform {
			return platform
		}
	}
	if len(normalized) > 0 {
		return normalized[0]
	}
	return ""
}
