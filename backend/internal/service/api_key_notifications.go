package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func APIKeyConfigurationChangeSummary(before, after *APIKey) string {
	if before == nil || after == nil {
		return ""
	}
	changes := make([]string, 0, 10)
	appendChange := func(label, oldValue, newValue string) {
		if oldValue != newValue {
			changes = append(changes, fmt.Sprintf("%s: %s -> %s", label, oldValue, newValue))
		}
	}
	appendChange("Name / 名称", before.Name, after.Name)
	appendChange("Group / 分组", optionalInt64String(before.GroupID), optionalInt64String(after.GroupID))
	appendChange("Status / 状态", before.Status, after.Status)
	appendChange("Quota / 配额", floatString(before.Quota), floatString(after.Quota))
	appendChange("Expiry / 有效期", optionalTimeString(before.ExpiresAt), optionalTimeString(after.ExpiresAt))
	appendChange("5h rate limit / 5小时限流", floatString(before.RateLimit5h), floatString(after.RateLimit5h))
	appendChange("1d rate limit / 1天限流", floatString(before.RateLimit1d), floatString(after.RateLimit1d))
	appendChange("7d rate limit / 7天限流", floatString(before.RateLimit7d), floatString(after.RateLimit7d))
	appendChange("IP whitelist / IP白名单", strings.Join(before.IPWhitelist, ", "), strings.Join(after.IPWhitelist, ", "))
	appendChange("IP blacklist / IP黑名单", strings.Join(before.IPBlacklist, ", "), strings.Join(after.IPBlacklist, ", "))
	return strings.Join(changes, "\n")
}

func NewAPIKeyConfigurationChangedOutboxInput(apiKey *APIKey, summary, changedBy string) NotificationEmailOutboxInput {
	changedAt := apiKey.UpdatedAt.UTC()
	return NotificationEmailOutboxInput{
		EventType:      NotificationEmailEventAPIKeyConfigurationChanged,
		UserID:         apiKey.UserID,
		APIKeyID:       &apiKey.ID,
		RecipientEmail: derefAPIKeyEmail(apiKey.NotificationEmail),
		DedupKey:       fmt.Sprintf("api_key.configuration_changed:%d:%d", apiKey.ID, changedAt.UnixNano()),
		Payload: NotificationEmailOutboxPayload{Variables: map[string]string{
			"api_key_id":     strconv.FormatInt(apiKey.ID, 10),
			"api_key_name":   apiKey.Name,
			"api_key_masked": MaskAPIKeyForNotification(apiKey.Key),
			"changed_at":     changedAt.Format("2006-01-02 15:04:05 MST"),
			"changed_by":     changedBy,
			"change_summary": summary,
		}},
	}
}

func apiKeyChangeNotificationEnabled(apiKey *APIKey) bool {
	return apiKey != nil &&
		apiKey.ChangeNotifyEnabled &&
		apiKey.NotificationEmail != nil &&
		strings.TrimSpace(*apiKey.NotificationEmail) != ""
}

func MaskAPIKeyForNotification(key string) string {
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func cloneAPIKeyForChangeDetection(apiKey *APIKey) *APIKey {
	if apiKey == nil {
		return nil
	}
	clone := *apiKey
	clone.IPWhitelist = append([]string(nil), apiKey.IPWhitelist...)
	clone.IPBlacklist = append([]string(nil), apiKey.IPBlacklist...)
	return &clone
}

func optionalInt64String(value *int64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatInt(*value, 10)
}

func optionalTimeString(value *time.Time) string {
	if value == nil {
		return "never"
	}
	return value.UTC().Format(time.RFC3339)
}

func floatString(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func derefAPIKeyEmail(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
