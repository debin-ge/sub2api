package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveVIPStateTruthTable(t *testing.T) {
	for _, tt := range []struct {
		name     string
		paid     bool
		override *bool
		want     bool
		mode     VIPMode
	}{
		{name: "auto unpaid", mode: VIPModeAuto},
		{name: "auto paid", paid: true, want: true, mode: VIPModeAuto},
		{name: "force on unpaid", override: vipBoolPtr(true), want: true, mode: VIPModeForceOn},
		{name: "force on paid", paid: true, override: vipBoolPtr(true), want: true, mode: VIPModeForceOn},
		{name: "force off unpaid", override: vipBoolPtr(false), mode: VIPModeForceOff},
		{name: "force off paid", paid: true, override: vipBoolPtr(false), mode: VIPModeForceOff},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, EffectiveVIPState(tt.paid, tt.override))
			require.Equal(t, tt.mode, VIPModeFromOverride(tt.override))
		})
	}
}

func TestSafeVIPAccessStatePriority(t *testing.T) {
	require.Equal(t, VIPAccessStateActive, SafeVIPAccessState(true, vipBoolPtr(false), true, true))
	require.Equal(t, VIPAccessStateRestricted, SafeVIPAccessState(false, vipBoolPtr(false), true, true))
	require.Equal(t, VIPAccessStateActivationFailed, SafeVIPAccessState(false, nil, true, true))
	require.Equal(t, VIPAccessStateActivationPending, SafeVIPAccessState(false, nil, true, false))
	require.Equal(t, VIPAccessStatePaymentRequired, SafeVIPAccessState(false, nil, false, false))
}

func TestParseVIPMode(t *testing.T) {
	mode, err := ParseVIPMode(" force_off ")
	require.NoError(t, err)
	require.Equal(t, VIPModeForceOff, mode)

	_, err = ParseVIPMode("sometimes")
	require.Error(t, err)
}

func vipBoolPtr(value bool) *bool {
	return &value
}
