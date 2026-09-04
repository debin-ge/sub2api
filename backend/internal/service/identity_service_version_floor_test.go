package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func TestFloorClaudeCLIUserAgentVersion(t *testing.T) {
	floorUA := "claude-cli/" + claude.CLICurrentVersion
	cases := []struct {
		name        string
		ua          string
		want        string
		wantChanged bool
	}{
		{"old_version_upgraded", "claude-cli/2.1.220 (external, cli)", floorUA + " (external, cli)", true},
		{
			"old_version_with_desktop_suffix",
			"claude-cli/2.1.100 (external, claude-desktop-3p, agent-sdk/0.3.100)",
			floorUA + " (external, claude-desktop-3p, agent-sdk/0.3.100)",
			true,
		},
		{"equal_to_floor", floorUA + " (external, cli)", floorUA + " (external, cli)", false},
		{"newer_than_floor", "claude-cli/2.9.0 (external, cli)", "claude-cli/2.9.0 (external, cli)", false},
		{"other_product", "opencode/1.2.3 (external, cli)", "opencode/1.2.3 (external, cli)", false},
		{"empty", "", "", false},
		{"no_version", "claude-cli (external, cli)", "claude-cli (external, cli)", false},
		{"unparseable_version", "claude-cli/abc (external, cli)", "claude-cli/abc (external, cli)", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := floorClaudeCLIUserAgentVersion(tc.ua)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantChanged, changed)
		})
	}
}

func TestGetOrCreateFingerprintFloorsStaleCachedUserAgent(t *testing.T) {
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent:               "claude-cli/2.1.220 (external, cli)",
		ClientID:                "cid-1",
		StainlessPackageVersion: "0.91.1",
		UpdatedAt:               time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(
		context.Background(), 147,
		headersWithUA("claude-cli/2.1.75 (external, cli)"),
	)

	require.NoError(t, err)
	require.Equal(t, "claude-cli/"+claude.CLICurrentVersion+" (external, cli)", fp.UserAgent)
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, fp.UserAgent, cache.lastSet.UserAgent)
	require.Equal(t, "0.91.1", fp.StainlessPackageVersion)
	require.Equal(t, "cid-1", fp.ClientID)
}

func TestGetOrCreateFingerprintDoesNotTouchCacheAboveFloor(t *testing.T) {
	ua := "claude-cli/2.9.0 (external, cli)"
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent: ua,
		ClientID:  "cid-1",
		UpdatedAt: time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(
		context.Background(), 1,
		headersWithUA("claude-cli/2.1.75 (external, cli)"),
	)

	require.NoError(t, err)
	require.Equal(t, ua, fp.UserAgent)
	require.Zero(t, cache.setCalls)
}

func TestGetOrCreateFingerprintClientNewerThanFloorStillWins(t *testing.T) {
	newUA := "claude-cli/2.9.0 (external, cli)"
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent: "claude-cli/2.1.220 (external, cli)",
		ClientID:  "cid-1",
		UpdatedAt: time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(context.Background(), 1, headersWithUA(newUA))

	require.NoError(t, err)
	require.Equal(t, newUA, fp.UserAgent)
	require.Equal(t, 1, cache.setCalls)
}

func TestGetOrCreateFingerprintFloorWinsOverStaleClientUpgrade(t *testing.T) {
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent: "claude-cli/2.1.22 (external, cli)",
		ClientID:  "cid-1",
		UpdatedAt: time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(
		context.Background(), 1,
		headersWithUA("claude-cli/2.1.223 (external, cli)"),
	)

	require.NoError(t, err)
	require.Equal(t, "claude-cli/"+claude.CLICurrentVersion+" (external, cli)", fp.UserAgent)
	require.Equal(t, 1, cache.setCalls)
}

func TestCreateFingerprintFromHeadersFloorsOldClientUserAgent(t *testing.T) {
	cache := &stubIdentityCache{}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(
		context.Background(), 1,
		headersWithUA("claude-cli/2.1.75 (external, claude-desktop-3p)"),
	)

	require.NoError(t, err)
	require.Equal(t,
		"claude-cli/"+claude.CLICurrentVersion+" (external, claude-desktop-3p)",
		fp.UserAgent,
	)
	require.Equal(t, 1, cache.setCalls)
}

func TestGetOrCreateFingerprintFloorsUserAgentAfterPoisonedCacheRecovery(t *testing.T) {
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent: "claude-cli/999.0.0-local (undefined, cli)",
		ClientID:  "cid-1",
		UpdatedAt: time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)

	fp, err := svc.GetOrCreateFingerprint(
		context.Background(), 1,
		headersWithUA("claude-cli/2.1.75 (external, cli)"),
	)

	require.NoError(t, err)
	require.Equal(t, "claude-cli/"+claude.CLICurrentVersion+" (external, cli)", fp.UserAgent)
	require.Equal(t, 1, cache.setCalls)
}
