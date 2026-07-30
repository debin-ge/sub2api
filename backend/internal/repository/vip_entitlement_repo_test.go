package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestApplyPaidEligibilityTransitionPreservesForceOff(t *testing.T) {
	completedAt := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	current := vipEntitlementRecord{
		manualOverride:  sql.NullBool{Bool: false, Valid: true},
		overrideAt:      sql.NullTime{Time: completedAt.Add(-time.Hour), Valid: true},
		overrideBy:      sql.NullInt64{Int64: 9, Valid: true},
		overrideReason:  "risk review",
		effectiveSource: string(service.VIPEffectiveSourceManualOff),
	}

	next := applyPaidEligibilityTransition(current, completedAt, service.VIPPaidSourcePayment)

	require.True(t, next.paidEligible)
	require.Equal(t, completedAt, next.paidEligibleAt.Time)
	require.Equal(t, string(service.VIPPaidSourcePayment), next.paidSource)
	require.True(t, next.manualOverride.Valid)
	require.False(t, next.manualOverride.Bool)
	require.False(t, next.isVIP)
	require.False(t, next.grantedAt.Valid)
	require.Equal(t, string(service.VIPEffectiveSourceManualOff), next.effectiveSource)
	require.Equal(t, current.overrideReason, next.overrideReason)
}

func TestApplyPaidEligibilityTransitionKeepsEarliestTimeAndFirstSource(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := vipEntitlementRecord{
		paidEligible:   true,
		paidEligibleAt: sql.NullTime{Time: first, Valid: true},
		paidSource:     string(service.VIPPaidSourceBackfill),
		isVIP:          true,
		grantedAt:      sql.NullTime{Time: first, Valid: true},
	}

	later := applyPaidEligibilityTransition(
		current,
		first.Add(24*time.Hour),
		service.VIPPaidSourcePayment,
	)
	require.Equal(t, first, later.paidEligibleAt.Time)
	require.Equal(t, string(service.VIPPaidSourceBackfill), later.paidSource)

	earlierAt := first.Add(-24 * time.Hour)
	earlier := applyPaidEligibilityTransition(
		current,
		earlierAt,
		service.VIPPaidSourceReconcile,
	)
	require.Equal(t, earlierAt, earlier.paidEligibleAt.Time)
	require.Equal(t, string(service.VIPPaidSourceBackfill), earlier.paidSource)
	require.Equal(t, first, earlier.grantedAt.Time, "historical correction must not rewrite an already-effective grant timestamp")
}

func TestApplyManualModeTransitionTruthTable(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name         string
		paid         bool
		mode         service.VIPMode
		wantVIP      bool
		wantSource   string
		wantOverride sql.NullBool
	}{
		{
			name: "auto unpaid", mode: service.VIPModeAuto,
			wantSource: string(service.VIPEffectiveSourceNone),
		},
		{
			name: "auto paid", paid: true, mode: service.VIPModeAuto, wantVIP: true,
			wantSource: string(service.VIPPaidSourcePayment),
		},
		{
			name: "force on unpaid", mode: service.VIPModeForceOn, wantVIP: true,
			wantSource:   string(service.VIPEffectiveSourceManualOn),
			wantOverride: sql.NullBool{Bool: true, Valid: true},
		},
		{
			name: "force off paid", paid: true, mode: service.VIPModeForceOff,
			wantSource:   string(service.VIPEffectiveSourceManualOff),
			wantOverride: sql.NullBool{Bool: false, Valid: true},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := vipEntitlementRecord{
				paidEligible: tt.paid,
				paidSource: func() string {
					if tt.paid {
						return string(service.VIPPaidSourcePayment)
					}
					return ""
				}(),
			}
			next := applyManualModeTransition(current, tt.mode, 7, "admin reason", now)
			require.Equal(t, tt.wantVIP, next.isVIP)
			require.Equal(t, tt.wantSource, next.effectiveSource)
			require.Equal(t, tt.wantOverride, next.manualOverride)
		})
	}
}

func TestVIPAuditListIsNewestFirstAndPreservesNullableStates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM user_vip_audit_events WHERE user_id = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM user_vip_audit_events.*WHERE user_id = \$1.*ORDER BY created_at DESC, id DESC.*LIMIT \$2 OFFSET \$3`).
		WithArgs(int64(7), 10, 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "actor_type", "actor_user_id", "actor_snapshot",
			"action", "reason", "order_id", "request_id",
			"old_paid_eligible", "new_paid_eligible",
			"old_manual_override", "new_manual_override",
			"old_is_vip", "new_is_vip", "source", "created_at",
		}).AddRow(
			int64(5), int64(7), "admin", int64(9), "operator@example.com",
			"manual_mode", "risk review", nil, "",
			true, true, nil, false, true, false, "manual_off", createdAt,
		))

	repo := &vipEntitlementRepository{db: db}
	events, total, err := repo.ListAuditEvents(context.Background(), 7, 2, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, events, 1)
	require.Equal(t, int64(9), *events[0].ActorUserID)
	require.Nil(t, events[0].OrderID)
	require.Nil(t, events[0].OldManualOverride)
	require.NotNil(t, events[0].NewManualOverride)
	require.False(t, *events[0].NewManualOverride)
	require.NoError(t, mock.ExpectationsWereMet())
}
