package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	errVideoDisclosurePolicyInvalid = infraerrors.BadRequest(
		"INVALID_VIDEO_DISCLOSURE_POLICY",
		"video_disclosure_policy must be one of none, identity, task_access, or dedicated_credentials",
	)
	errVideoOwnerUserIDInvalid = infraerrors.BadRequest(
		"INVALID_VIDEO_OWNER_USER_ID",
		"video_owner_user_id must be positive",
	)
	errVideoDedicatedOwnerRequired = infraerrors.BadRequest(
		"VIDEO_DEDICATED_OWNER_REQUIRED",
		"dedicated video credential disclosure requires video_owner_user_id",
	)
	errVideoDedicatedAPIKeyRequired = infraerrors.BadRequest(
		"VIDEO_DEDICATED_API_KEY_REQUIRED",
		"dedicated video credential disclosure requires an API key account",
	)
)

func normalizeVideoDisclosurePolicy(value string, allowEmpty bool) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" && allowEmpty {
		return "", nil
	}
	switch value {
	case config.VideoDisclosureNone,
		config.VideoDisclosureIdentity,
		config.VideoDisclosureTaskAccess,
		config.VideoDisclosureDedicatedCredentials:
		return value, nil
	default:
		return "", errVideoDisclosurePolicyInvalid
	}
}

func effectiveVideoDisclosurePolicy(global, group, account string) string {
	global, err := normalizeVideoDisclosurePolicy(global, false)
	if err != nil {
		return config.VideoDisclosureNone
	}
	effective := global
	for _, scoped := range []string{group, account} {
		scoped, err = normalizeVideoDisclosurePolicy(scoped, true)
		if err != nil {
			return config.VideoDisclosureNone
		}
		if scoped != "" && videoDisclosureRank(scoped) < videoDisclosureRank(effective) {
			effective = scoped
		}
	}
	return effective
}

func videoDisclosureRank(policy string) int {
	switch policy {
	case config.VideoDisclosureDedicatedCredentials:
		return 3
	case config.VideoDisclosureTaskAccess:
		return 2
	case config.VideoDisclosureIdentity:
		return 1
	default:
		return 0
	}
}

func validateVideoAccountDisclosure(account *Account) error {
	if account == nil {
		return ErrAccountNilInput
	}
	policy, err := normalizeVideoDisclosurePolicy(account.VideoDisclosurePolicy, true)
	if err != nil {
		return err
	}
	account.VideoDisclosurePolicy = policy
	if account.VideoOwnerUserID != nil && *account.VideoOwnerUserID <= 0 {
		return errVideoOwnerUserIDInvalid
	}
	if policy == config.VideoDisclosureDedicatedCredentials {
		if account.VideoOwnerUserID == nil || *account.VideoOwnerUserID <= 0 {
			return errVideoDedicatedOwnerRequired
		}
		if account.Type != AccountTypeAPIKey {
			return errVideoDedicatedAPIKeyRequired
		}
	}
	return nil
}
