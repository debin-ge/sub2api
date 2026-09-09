package service

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Shared validation for attributed administrative actions: an opaque operation
// key, an evidence reference that is not a live URL, and a human-written reason
// that leaks no credentials.
var adminAuditOpaqueReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{2,127}$`)
var adminAuditCredentialPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9])sk-[a-z0-9_-]{8,}`)

func validAdminAuditReason(value string) bool {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 4 || len(value) > 1024 || strings.Contains(value, "://") ||
		strings.Contains(strings.ToLower(value), "bearer ") || adminAuditCredentialPattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
