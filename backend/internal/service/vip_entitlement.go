package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// VIPMode is the administrator-facing entitlement mode.
type VIPMode string

const (
	VIPModeAuto     VIPMode = "AUTO"
	VIPModeForceOn  VIPMode = "FORCE_ON"
	VIPModeForceOff VIPMode = "FORCE_OFF"
)

// VIPPaidSource records the first source that materialized paid eligibility.
type VIPPaidSource string

const (
	VIPPaidSourcePayment   VIPPaidSource = "payment"
	VIPPaidSourceBackfill  VIPPaidSource = "backfill"
	VIPPaidSourceReconcile VIPPaidSource = "reconcile"
)

// VIPEffectiveSource describes the source of the current materialized state.
type VIPEffectiveSource string

const (
	VIPEffectiveSourceNone      VIPEffectiveSource = ""
	VIPEffectiveSourcePayment   VIPEffectiveSource = "payment"
	VIPEffectiveSourceBackfill  VIPEffectiveSource = "backfill"
	VIPEffectiveSourceReconcile VIPEffectiveSource = "reconcile"
	VIPEffectiveSourceManualOn  VIPEffectiveSource = "manual_on"
	VIPEffectiveSourceManualOff VIPEffectiveSource = "manual_off"
)

// VIPAccessState is the safe user-facing state. It intentionally contains no
// administrator identity, override reason, or internal audit source.
type VIPAccessState string

const (
	VIPAccessStateActive            VIPAccessState = "ACTIVE"
	VIPAccessStatePaymentRequired   VIPAccessState = "PAYMENT_REQUIRED"
	VIPAccessStateRestricted        VIPAccessState = "RESTRICTED"
	VIPAccessStateActivationPending VIPAccessState = "ACTIVATION_PENDING"
	VIPAccessStateActivationFailed  VIPAccessState = "ACTIVATION_FAILED"
)

// VIPMutationResult reports both source-of-truth and effective-state changes.
// FORCE_OFF may therefore change Eligibility without changing Effective.
type VIPMutationResult struct {
	EligibilityChanged bool    `json:"eligibility_changed"`
	EffectiveChanged   bool    `json:"effective_changed"`
	ManualMode         VIPMode `json:"manual_mode"`
}

// VIPAuditEvent is the immutable administrator-facing entitlement history.
// ActorSnapshot is stored at mutation time so a later administrator rename or
// deletion cannot make the audit record ambiguous.
type VIPAuditEvent struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	ActorType         string    `json:"actor_type"`
	ActorUserID       *int64    `json:"actor_user_id"`
	ActorSnapshot     string    `json:"actor_snapshot"`
	Action            string    `json:"action"`
	Reason            string    `json:"reason"`
	OrderID           *int64    `json:"order_id"`
	RequestID         string    `json:"request_id"`
	OldPaidEligible   bool      `json:"old_paid_eligible"`
	NewPaidEligible   bool      `json:"new_paid_eligible"`
	OldManualOverride *bool     `json:"old_manual_override"`
	NewManualOverride *bool     `json:"new_manual_override"`
	OldIsVIP          bool      `json:"old_is_vip"`
	NewIsVIP          bool      `json:"new_is_vip"`
	Source            string    `json:"source"`
	CreatedAt         time.Time `json:"created_at"`
}

type VIPEntitlementRepository interface {
	ApplyPaidEligibility(
		ctx context.Context,
		userID, orderID int64,
		completedAt time.Time,
		source VIPPaidSource,
	) (VIPMutationResult, error)
	SetManualMode(
		ctx context.Context,
		userID int64,
		mode VIPMode,
		actorID int64,
		reason string,
	) (VIPMutationResult, error)
}

// VIPAuditRepository is intentionally separate from the mutation interface so
// payment/reconcile tests and alternative mutation implementations do not need
// an unrelated read dependency.
type VIPAuditRepository interface {
	ListAuditEvents(
		ctx context.Context,
		userID int64,
		page, pageSize int,
	) ([]VIPAuditEvent, int64, error)
}

func ParseVIPMode(value string) (VIPMode, error) {
	mode := VIPMode(strings.ToUpper(strings.TrimSpace(value)))
	switch mode {
	case VIPModeAuto, VIPModeForceOn, VIPModeForceOff:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown VIP mode %q", value)
	}
}

func (m VIPMode) ManualOverride() *bool {
	switch m {
	case VIPModeForceOn:
		value := true
		return &value
	case VIPModeForceOff:
		value := false
		return &value
	default:
		return nil
	}
}

func VIPModeFromOverride(value *bool) VIPMode {
	if value == nil {
		return VIPModeAuto
	}
	if *value {
		return VIPModeForceOn
	}
	return VIPModeForceOff
}

func IsValidVIPPaidSource(source VIPPaidSource) bool {
	switch source {
	case VIPPaidSourcePayment, VIPPaidSourceBackfill, VIPPaidSourceReconcile:
		return true
	default:
		return false
	}
}

// EffectiveVIPState implements the invariant:
//
//	is_vip = COALESCE(vip_manual_override, vip_paid_eligible)
func EffectiveVIPState(paidEligible bool, manualOverride *bool) bool {
	if manualOverride != nil {
		return *manualOverride
	}
	return paidEligible
}

func EffectiveVIPSource(paidEligible bool, paidSource VIPPaidSource, manualOverride *bool) VIPEffectiveSource {
	if manualOverride != nil {
		if *manualOverride {
			return VIPEffectiveSourceManualOn
		}
		return VIPEffectiveSourceManualOff
	}
	if !paidEligible {
		return VIPEffectiveSourceNone
	}
	return VIPEffectiveSource(paidSource)
}

func SafeVIPAccessState(
	isVIP bool,
	manualOverride *bool,
	activationPending bool,
	activationFailed bool,
) VIPAccessState {
	if isVIP {
		return VIPAccessStateActive
	}
	if manualOverride != nil && !*manualOverride {
		return VIPAccessStateRestricted
	}
	if activationFailed {
		return VIPAccessStateActivationFailed
	}
	if activationPending {
		return VIPAccessStateActivationPending
	}
	return VIPAccessStatePaymentRequired
}

// VIPEntitlementSnapshot contains the internal entitlement fields carried by
// service.User. User-facing DTOs must only expose IsVIP and VIPAccessState.
type VIPEntitlementSnapshot struct {
	PaidEligible    bool
	PaidEligibleAt  *time.Time
	PaidSource      VIPPaidSource
	ManualOverride  *bool
	OverrideAt      *time.Time
	OverrideBy      *int64
	OverrideReason  string
	IsVIP           bool
	GrantedAt       *time.Time
	EffectiveSource VIPEffectiveSource
	// ActivationPending is derived from completed payment facts and is never
	// accepted as a writable entitlement source.
	ActivationPending bool
	// ActivationFailed means a qualifying payment fact exists but the automatic
	// projection is still missing after the repair SLA. It must never degrade to
	// PAYMENT_REQUIRED, which could encourage a duplicate payment.
	ActivationFailed bool
}

func (v VIPEntitlementSnapshot) Mode() VIPMode {
	return VIPModeFromOverride(v.ManualOverride)
}

func (v VIPEntitlementSnapshot) AccessState() VIPAccessState {
	return SafeVIPAccessState(
		v.IsVIP,
		v.ManualOverride,
		v.ActivationPending,
		v.ActivationFailed,
	)
}
