package service

import "strings"

var publicCatalogRoutingOnlyModelIDs = map[string]struct{}{
	"adaptive":            {},
	"arena-fast":          {},
	"arena-mixed":         {},
	"arena-smart":         {},
	"swe-1-6":             {},
	"swe-1-6-fast":        {},
	"swe-check":           {},
	"deepseek-v4":         {},
	"minimax-m2-5":        {},
	"opencode/big-pickle": {},
}

// IsPublicCatalogRoutingOnlyModelID reports whether an internal routing alias
// is intentionally hidden from the anonymous Model Plaza and Radar catalog.
func IsPublicCatalogRoutingOnlyModelID(modelID string) bool {
	_, hidden := publicCatalogRoutingOnlyModelIDs[strings.ToLower(strings.TrimSpace(modelID))]
	return hidden
}
