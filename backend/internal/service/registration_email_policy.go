package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var registrationEmailDomainPattern = regexp.MustCompile(
	`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
)

// RegistrationEmailSuffix extracts normalized suffix in "@domain" form.
func RegistrationEmailSuffix(email string) string {
	_, domain, ok := splitEmailForPolicy(email)
	if !ok {
		return ""
	}
	return "@" + domain
}

// IsRegistrationEmailSuffixBlocked checks whether an email matches the suffix blacklist.
// Empty blacklist means no domain is blocked.
func IsRegistrationEmailSuffixBlocked(email string, blacklist []string) bool {
	if len(blacklist) == 0 {
		return false
	}
	_, domain, ok := splitEmailForPolicy(email)
	if !ok {
		return false
	}
	suffix := "@" + domain
	for _, blocked := range blacklist {
		blocked = strings.ToLower(strings.TrimSpace(blocked))
		if strings.HasPrefix(blocked, "@") && suffix == blocked {
			return true
		}
		if strings.HasPrefix(blocked, "*.") && registrationEmailDomainMatchesWildcard(domain, blocked) {
			return true
		}
	}
	return false
}

// NormalizeRegistrationEmailSuffixBlacklist normalizes and validates suffix blacklist items.
func NormalizeRegistrationEmailSuffixBlacklist(raw []string) ([]string, error) {
	return normalizeRegistrationEmailSuffixBlacklist(raw, true)
}

// ParseRegistrationEmailSuffixBlacklist parses persisted JSON into normalized suffixes.
// Invalid entries are ignored to keep old misconfigurations from breaking runtime reads.
func ParseRegistrationEmailSuffixBlacklist(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return []string{}
	}
	normalized, _ := normalizeRegistrationEmailSuffixBlacklist(items, false)
	if len(normalized) == 0 {
		return []string{}
	}
	return normalized
}

func normalizeRegistrationEmailSuffixBlacklist(raw []string, strict bool) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		normalized, err := normalizeRegistrationEmailSuffix(item)
		if err != nil {
			if strict {
				return nil, err
			}
			continue
		}
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeRegistrationEmailSuffix(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", nil
	}

	if strings.HasPrefix(value, "*.") {
		domain := strings.TrimPrefix(value, "*.")
		if !isValidRegistrationEmailDomain(domain) {
			return "", fmt.Errorf("invalid email suffix: %q", raw)
		}
		return "*." + domain, nil
	}

	domain := value
	if strings.Contains(value, "@") {
		if !strings.HasPrefix(value, "@") || strings.Count(value, "@") != 1 {
			return "", fmt.Errorf("invalid email suffix: %q", raw)
		}
		domain = strings.TrimPrefix(value, "@")
	}

	if !isValidRegistrationEmailDomain(domain) {
		return "", fmt.Errorf("invalid email suffix: %q", raw)
	}

	return "@" + domain, nil
}

func isValidRegistrationEmailDomain(domain string) bool {
	return domain != "" &&
		!strings.Contains(domain, "@") &&
		registrationEmailDomainPattern.MatchString(domain)
}

func registrationEmailDomainMatchesWildcard(domain string, blocked string) bool {
	base := strings.TrimPrefix(blocked, "*.")
	if !isValidRegistrationEmailDomain(base) {
		return false
	}
	return domain == base || strings.HasSuffix(domain, "."+base)
}

// ValidateEmailLocalPart 验证邮箱本地部分（@符号前），拒绝包含加号的邮箱。
// Gmail 点号变体由注册别名归一化和创建时的别名保护处理，合法点号邮箱保持可用。
func ValidateEmailLocalPart(email string) error {
	local, _, ok := splitEmailForPolicy(email)
	if !ok {
		return fmt.Errorf("invalid email format")
	}

	// 检查本地部分是否包含加号（plus addressing）
	if strings.Contains(local, "+") {
		return fmt.Errorf("email address with plus sign (+) is not allowed")
	}

	return nil
}

func splitEmailForPolicy(raw string) (local string, domain string, ok bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", "", false
	}
	return local, domain, true
}
