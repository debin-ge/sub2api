package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func TestSyncBillingHeaderVersion(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		userAgent string
		wantSub   string // substring expected in result
		unchanged bool   // expect body to remain the same
	}{
		{
			name:      "replaces cc_version preserving message-derived suffix",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81.df2; cc_entrypoint=cli; cch=00000;"},{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22 (external, cli)",
			wantSub:   "cc_version=2.1.22.df2",
		},
		{
			name:      "no billing header in system",
			body:      `{"system":[{"type":"text","text":"You are Claude Code."}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "no system field",
			body:      `{"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
		{
			name:      "user-agent without version",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "Mozilla/5.0",
			unchanged: true,
		},
		{
			name:      "empty user-agent",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "",
			unchanged: true,
		},
		{
			name:      "version already matches",
			body:      `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.22; cc_entrypoint=cli; cch=00000;"}],"messages":[]}`,
			userAgent: "claude-cli/2.1.22",
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncBillingHeaderVersion([]byte(tt.body), tt.userAgent)
			if tt.unchanged {
				assert.Equal(t, tt.body, string(result), "body should remain unchanged")
			} else {
				assert.Contains(t, string(result), tt.wantSub)
				// Ensure old semver is gone
				assert.NotContains(t, string(result), "cc_version=2.1.81")
			}
		})
	}
}

func TestBuildUpstreamRequestFloorsStaleFingerprintForFable51(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "opencode/1.2.3 (external, cli)")

	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent:               "claude-cli/2.1.220 (external, cli)",
		ClientID:                "cid-1",
		StainlessPackageVersion: "0.91.1",
		UpdatedAt:               time.Now().Unix(),
	}}
	svc := &GatewayService{identityService: NewIdentityService(cache)}
	account := &Account{
		ID:          147,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-token"},
	}
	body := []byte(`{"model":"claude-fable-5-1","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.220.abc; cc_entrypoint=cli;"}],"messages":[]}`)

	req, wireBody, err := svc.buildUpstreamRequest(
		context.Background(), c, account, body,
		"oauth-token", "oauth", "claude-fable-5-1", true, true,
	)

	require.NoError(t, err)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(req.Header, "User-Agent"))
	require.Contains(t, string(wireBody), "cc_version="+claude.CLICurrentVersion+".abc")
	require.NotContains(t, string(wireBody), "cc_version=2.1.220")
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], cache.lastSet.UserAgent)
	require.Equal(t, "0.91.1", cache.lastSet.StainlessPackageVersion)
}

func TestBuildUpstreamRequestMimicUsesCurrentVersionWithThirdPartyFingerprint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "opencode/1.2.3 (external, cli)")

	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent: "opencode/1.2.3 (external, cli)",
		ClientID:  "cid-1",
		UpdatedAt: time.Now().Unix(),
	}}
	svc := &GatewayService{identityService: NewIdentityService(cache)}
	account := &Account{ID: 148, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	body := []byte(`{"model":"claude-fable-5-1","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1.2.3.abc; cc_entrypoint=cli;"}],"messages":[]}`)

	req, wireBody, err := svc.buildUpstreamRequest(
		context.Background(), c, account, body,
		"oauth-token", "oauth", "claude-fable-5-1", true, true,
	)

	require.NoError(t, err)
	require.Equal(t, claude.DefaultHeaders["User-Agent"], getHeaderRaw(req.Header, "User-Agent"))
	require.Contains(t, string(wireBody), "cc_version="+claude.CLICurrentVersion+".abc")
	require.NotContains(t, string(wireBody), "cc_version=1.2.3")
	require.Zero(t, cache.setCalls, "third-party fingerprint should remain unchanged in storage")
}
