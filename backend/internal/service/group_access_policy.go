package service

import (
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type GroupAccessMode uint8

const (
	GroupAccessVisibility GroupAccessMode = iota
	GroupAccessBinding
	GroupAccessPrimaryAuth
	GroupAccessFallbackRuntime
)

type GroupAccessDenyReason string

const (
	GroupAccessAllowedReason             GroupAccessDenyReason = ""
	GroupAccessDenyGroupNotAllowed       GroupAccessDenyReason = "GROUP_NOT_ALLOWED"
	GroupAccessDenyVIPOnly               GroupAccessDenyReason = "GROUP_VIP_ONLY"
	GroupAccessDenyFallbackSubscription  GroupAccessDenyReason = "GROUP_FALLBACK_SUBSCRIPTION_FORBIDDEN"
	GroupAccessDenyProfileMissing        GroupAccessDenyReason = "GROUP_ACCESS_PROFILE_MISSING"
	GroupAccessDenyFallbackInvalidConfig GroupAccessDenyReason = "GROUP_FALLBACK_INVALID_CONFIG"
	GroupAccessDenyGroupInactive         GroupAccessDenyReason = "GROUP_NOT_ACTIVE"
	GroupAccessDenySubscriptionRequired  GroupAccessDenyReason = "SUBSCRIPTION_REQUIRED"
)

type GroupAccessSuggestedAction string

const (
	GroupAccessActionNone           GroupAccessSuggestedAction = ""
	GroupAccessActionPayment        GroupAccessSuggestedAction = "PAYMENT"
	GroupAccessActionContactSupport GroupAccessSuggestedAction = "CONTACT_SUPPORT"
)

type GroupAccessProfile struct {
	UserID                     int64
	IsVIP                      bool
	VIPAccessState             VIPAccessState
	AllowedGroups              []int64
	ActiveSubscriptionGroupIDs []int64
}

type GroupAccessDecision struct {
	Visible         bool                       `json:"visible"`
	Allowed         bool                       `json:"allowed"`
	Reason          GroupAccessDenyReason      `json:"deny_reason,omitempty"`
	SuggestedAction GroupAccessSuggestedAction `json:"suggested_action,omitempty"`
}

type GroupCatalogEntry struct {
	Group
	CanBind         bool
	DenyReason      GroupAccessDenyReason
	SuggestedAction GroupAccessSuggestedAction
}

func (d GroupAccessDecision) Error() error {
	if d.Allowed {
		return nil
	}
	switch d.Reason {
	case GroupAccessDenyGroupNotAllowed:
		return infraerrors.Forbidden(string(d.Reason), "user is not allowed to access this group")
	case GroupAccessDenyVIPOnly:
		return infraerrors.Forbidden(string(d.Reason), "VIP access is required for this group")
	case GroupAccessDenyFallbackSubscription:
		return infraerrors.BadRequest(string(d.Reason), "subscription groups cannot be fallback targets")
	case GroupAccessDenyFallbackInvalidConfig:
		return infraerrors.New(http.StatusServiceUnavailable, string(d.Reason), "fallback group configuration is invalid")
	case GroupAccessDenyProfileMissing:
		return infraerrors.New(http.StatusInternalServerError, string(d.Reason), "group access profile is missing")
	case GroupAccessDenyGroupInactive:
		return infraerrors.BadRequest(string(d.Reason), "group is not active")
	case GroupAccessDenySubscriptionRequired:
		return infraerrors.BadRequest(string(d.Reason), "an active subscription is required for this group")
	default:
		return infraerrors.Forbidden("GROUP_ACCESS_DENIED", "group access denied")
	}
}

type GroupAccessPolicy struct{}

func NewGroupAccessPolicy() *GroupAccessPolicy {
	return &GroupAccessPolicy{}
}

func (p *GroupAccessPolicy) Evaluate(
	profile *GroupAccessProfile,
	group *Group,
	mode GroupAccessMode,
) GroupAccessDecision {
	if group == nil {
		return denyGroupAccess(false, GroupAccessDenyGroupInactive, GroupAccessActionNone)
	}

	// A subscription target is structurally invalid for every fallback,
	// regardless of the user's subscription or other permissions.
	if mode == GroupAccessFallbackRuntime && group.IsSubscriptionType() {
		return denyGroupAccess(false, GroupAccessDenyFallbackInvalidConfig, GroupAccessActionNone)
	}
	if profile == nil || profile.UserID <= 0 {
		return denyGroupAccess(false, GroupAccessDenyProfileMissing, GroupAccessActionNone)
	}
	if !group.IsActive() {
		return denyGroupAccess(false, GroupAccessDenyGroupInactive, GroupAccessActionNone)
	}

	// Original subscription groups retain their existing endpoint-aware
	// runtime semantics. Catalog and binding still require an active
	// subscription to the concrete group.
	if group.IsSubscriptionType() {
		if mode == GroupAccessPrimaryAuth {
			return allowGroupAccess()
		}
		if containsInt64(profile.ActiveSubscriptionGroupIDs, group.ID) {
			return allowGroupAccess()
		}
		return denyGroupAccess(false, GroupAccessDenySubscriptionRequired, GroupAccessActionNone)
	}

	// Exclusive permission intentionally has priority over VIP so callers do
	// not reveal a hidden group or suggest payment for a group the user still
	// could not access.
	if group.IsExclusive && !containsInt64(profile.AllowedGroups, group.ID) {
		return denyGroupAccess(false, GroupAccessDenyGroupNotAllowed, GroupAccessActionNone)
	}

	if group.VIPOnly && !profile.IsVIP {
		action := GroupAccessActionNone
		if mode == GroupAccessVisibility || mode == GroupAccessBinding {
			switch profile.VIPAccessState {
			case VIPAccessStatePaymentRequired:
				action = GroupAccessActionPayment
			case VIPAccessStateRestricted, VIPAccessStateActivationFailed:
				action = GroupAccessActionContactSupport
			}
		}
		return denyGroupAccess(mode == GroupAccessVisibility, GroupAccessDenyVIPOnly, action)
	}

	return allowGroupAccess()
}

// EvaluateTrustedInternal is the explicit escape hatch for jobs with no user
// identity. It still enforces structural fallback restrictions.
func (p *GroupAccessPolicy) EvaluateTrustedInternal(group *Group, mode GroupAccessMode) GroupAccessDecision {
	if group == nil || !group.IsActive() {
		return denyGroupAccess(false, GroupAccessDenyGroupInactive, GroupAccessActionNone)
	}
	if mode == GroupAccessFallbackRuntime && group.IsSubscriptionType() {
		return denyGroupAccess(false, GroupAccessDenyFallbackInvalidConfig, GroupAccessActionNone)
	}
	return allowGroupAccess()
}

func allowGroupAccess() GroupAccessDecision {
	return GroupAccessDecision{Visible: true, Allowed: true}
}

func denyGroupAccess(visible bool, reason GroupAccessDenyReason, action GroupAccessSuggestedAction) GroupAccessDecision {
	return GroupAccessDecision{
		Visible:         visible,
		Allowed:         false,
		Reason:          reason,
		SuggestedAction: action,
	}
}
