package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestFingerprintForClientRequest(t *testing.T) {
	const fpUA = "claude-cli/2.9.0 (external, cli)"
	cases := []struct {
		name     string
		fpUA     string
		clientUA string
		want     string
	}{
		{"client_entrypoint_wins", fpUA, "claude-cli/2.1.81 (external, sdk-cli)", "claude-cli/2.9.0 (external, sdk-cli)"},
		{"interactive_client_over_sdk_fingerprint", "claude-cli/2.9.0 (external, sdk-cli)", "claude-cli/2.9.0 (external, cli)", fpUA},
		{"keeps_extra_client_fields", fpUA, "claude-cli/2.1.81 (external, sdk-ts, agent-sdk/0.3.81)", "claude-cli/2.9.0 (external, sdk-ts, agent-sdk/0.3.81)"},
		{"client_without_suffix", fpUA, "claude-cli/2.1.81", "claude-cli/2.9.0"},
		{"same_ua_unchanged", fpUA, fpUA, fpUA},
		{"empty_client_ua", fpUA, "", fpUA},
		{"non_cli_client", fpUA, "Go-http-client/1.1", fpUA},
		{"malformed_client", fpUA, "claude-cli/999.0.0-local (undefined, cli)", fpUA},
		{"non_cli_fingerprint", "python-httpx/0.27.0", "claude-cli/2.1.81 (external, cli)", "python-httpx/0.27.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp := &Fingerprint{UserAgent: tc.fpUA, ClientID: "client-1", StainlessLang: "js"}
			got := fingerprintForClientRequest(fp, tc.clientUA)
			require.Equal(t, tc.want, got.UserAgent)
			require.Equal(t, "client-1", got.ClientID)
			require.Equal(t, "js", got.StainlessLang)
			require.Equal(t, tc.fpUA, fp.UserAgent, "cached fingerprint must not be mutated")
		})
	}
	require.Nil(t, fingerprintForClientRequest(nil, "claude-cli/2.1.81 (external, cli)"))
}

func TestGetOrCreateFingerprint_ClaudeCLITakesOverForeignProduct(t *testing.T) {
	clientUA := "claude-cli/" + claude.CLIVersion() + " (external, cli)"
	cache := &stubIdentityCache{fingerprint: &Fingerprint{
		UserAgent:     "python-httpx/0.27.0",
		StainlessLang: "python",
		StainlessOS:   "MacOS",
		ClientID:      "client-1",
		UpdatedAt:     time.Now().Unix(),
	}}
	svc := NewIdentityService(cache)
	headers := headersWithUA(clientUA)
	headers.Set("X-Stainless-Lang", "js")

	fp, err := svc.GetOrCreateFingerprint(context.Background(), 1, headers)
	require.NoError(t, err)
	require.Equal(t, clientUA, fp.UserAgent)
	require.Equal(t, "js", fp.StainlessLang)
	require.Equal(t, defaultFingerprint().StainlessOS, fp.StainlessOS, "foreign runtime info must not survive the takeover")
	require.Equal(t, "client-1", fp.ClientID)
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, clientUA, cache.lastSet.UserAgent)
}

func TestGetOrCreateFingerprint_ForeignProductNeverTakesOver(t *testing.T) {
	cliUA := "claude-cli/" + claude.CLIVersion() + " (external, cli)"
	for _, tc := range []struct {
		name     string
		cachedUA string
		clientUA string
	}{
		{"foreign_client_over_cli", cliUA, "curl/8.7.1"},
		{"foreign_client_over_foreign", "python-httpx/0.27.0", "curl/8.7.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &stubIdentityCache{fingerprint: &Fingerprint{
				UserAgent: tc.cachedUA, ClientID: "client-1", UpdatedAt: time.Now().Unix(),
			}}
			fp, err := NewIdentityService(cache).GetOrCreateFingerprint(context.Background(), 1, headersWithUA(tc.clientUA))
			require.NoError(t, err)
			require.Equal(t, tc.cachedUA, fp.UserAgent)
			require.Equal(t, 0, cache.setCalls)
		})
	}
}

// 透传路径：出站 UA 的入口标签与请求体 cc_entrypoint 必须都来自当前客户端，
// 版本号仍统一为账号指纹版本（与 cc_version 一致）。
func TestBuildOAuthPassthrough_UserAgentEntrypointFollowsClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "count_tokens"} {
		for _, tc := range []struct {
			name       string
			cachedUA   string
			clientUA   string
			entrypoint string
		}{
			{"sdk_client_on_cli_fingerprint", "claude-cli/2.9.0 (external, cli)", "claude-cli/2.1.81 (external, sdk-cli)", "sdk-cli"},
			{"cli_client_on_sdk_fingerprint", "claude-cli/2.9.0 (external, sdk-cli)", "claude-cli/2.1.81 (external, cli)", "cli"},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				resetGatewayForwardingSettingsCacheForTest(t)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				c.Request.Header.Set("User-Agent", tc.clientUA)

				body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":""}],"messages":[{"role":"user","content":"hello world"}]}`)
				billing := fmt.Sprintf("x-anthropic-billing-header: cc_version=2.1.81.%s; cc_entrypoint=%s;",
					computeClaudeCodeFingerprint(body, "2.1.81"), tc.entrypoint)
				body, err := sjson.SetBytes(body, "system.0.text", billing)
				require.NoError(t, err)

				svc := &GatewayService{cfg: &config.Config{}}
				svc.identityService = NewIdentityService(&stubIdentityCache{fingerprint: &Fingerprint{
					UserAgent: tc.cachedUA, ClientID: "test-client", UpdatedAt: time.Now().Unix(),
				}})
				account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

				var req *http.Request
				var wireBody []byte
				if endpoint == "messages" {
					req, wireBody, err = svc.buildUpstreamRequest(context.Background(), c, account,
						body, "test-token", "oauth", "claude-haiku-4-5", false, false)
				} else {
					req, wireBody, err = svc.buildCountTokensRequest(context.Background(), c, account,
						body, "test-token", "oauth", "claude-haiku-4-5", false)
				}
				require.NoError(t, err)
				defer func() { require.NoError(t, req.Body.Close()) }()

				require.Equal(t, "claude-cli/2.9.0 (external, "+tc.entrypoint+")", getHeaderRaw(req.Header, "User-Agent"))
				require.Equal(t,
					fmt.Sprintf("x-anthropic-billing-header: cc_version=2.9.0.%s; cc_entrypoint=%s;",
						computeClaudeCodeFingerprint(wireBody, "2.9.0"), tc.entrypoint),
					gjson.GetBytes(wireBody, "system.0.text").String())
				actualBody, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, wireBody, actualBody)
			})
		}
	}
}
