package service

import (
	"log/slog"
	"strings"
)

// ServiceTierBillingResolution compares the tier a request asked for with the
// tier the upstream reports having used.
//
// Billing always follows the requested tier: the tier carried by the request
// sent upstream is what the upstream bills, while its response declaration is
// unreliable — the private ChatGPT Codex endpoint, and relays fronting it,
// commonly report "default" for effective Fast turns, and relays may serve or
// declare a tier on their own. The observed tier is kept only for audit so
// mismatches can be reconciled against upstream bills.
type ServiceTierBillingResolution struct {
	Requested string // tier carried by the request sent upstream ("" when none); also the billed tier
	Observed  string // tier declared by the upstream response ("" when none)
	Mismatch  bool   // Observed is a known tier priced differently from Requested
}

// ResolveBillingServiceTier normalises both tiers and flags a mismatch when the
// upstream declares a known tier with a different price level. A missing or
// unknown declaration, and equivalent spellings ("" / "default" / "standard"),
// never count as a mismatch.
func ResolveBillingServiceTier(requested, observed string) ServiceTierBillingResolution {
	requested = normalizeBillingServiceTier(requested)
	observed = normalizeBillingServiceTier(observed)
	resolution := ServiceTierBillingResolution{Requested: requested, Observed: observed}
	if observed == "" || observed == requested {
		return resolution
	}
	observedRank, known := serviceTierCostRank(observed)
	if !known {
		return resolution
	}
	requestedRank, _ := serviceTierCostRank(requested)
	resolution.Mismatch = observedRank != requestedRank
	return resolution
}

// serviceTierCostRank orders tiers by their cost relative to the base rate, so a
// lower rank is always cheaper. Unknown tiers rank as the base rate and report
// known=false so callers can refuse to act on them.
func serviceTierCostRank(tier string) (rank int, known bool) {
	switch normalizeBillingServiceTier(tier) {
	case "flex":
		return 0, true
	case "", "default", "standard", "auto", "scale":
		return 1, true
	case "priority", "fast":
		return 2, true
	case "ultrafast":
		return 3, true
	default:
		return 1, false
	}
}

// codexOAuthResponseTierIsNonAuthoritative reports the known Codex echo: OAuth
// credentials answer effective Fast turns with service_tier "default". It is
// expected on every such turn, so it is not worth an audit line.
func codexOAuthResponseTierIsNonAuthoritative(observed string) bool {
	switch normalizeBillingServiceTier(observed) {
	case "default":
		return true
	default:
		return false
	}
}

// logServiceTierBillingMismatch leaves an audit trail for every request whose
// upstream-declared tier differs from the billed (requested) tier; matching
// tiers and the routine Codex OAuth "default" echo are not logged.
func logServiceTierBillingMismatch(component string, account *Account, requestID string, resolution ServiceTierBillingResolution) {
	if !resolution.Mismatch {
		return
	}
	if account != nil && account.IsOpenAIOAuthLike() && codexOAuthResponseTierIsNonAuthoritative(resolution.Observed) {
		return
	}
	attrs := []any{
		"component", component,
		"request_id", strings.TrimSpace(requestID),
		"requested_tier", resolution.Requested,
		"response_tier", resolution.Observed,
		"billed_tier", resolution.Requested,
	}
	if account != nil {
		attrs = append(attrs, "platform", account.Platform, "account_id", account.ID)
	}
	slog.Info("billing.service_tier_mismatch", attrs...)
}
