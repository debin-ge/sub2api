package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoReleaseConfigNeedsNoCallbackSigningSecret(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_VIDEO_ENABLED", "true")
	t.Setenv("GATEWAY_VIDEO_CREATION_ENABLED", "true")
	t.Setenv("GATEWAY_VIDEO_POLL_INTERVAL_SECONDS", "17")
	t.Setenv("GATEWAY_VIDEO_CALLBACK_ENABLED", "false")
	t.Setenv("GATEWAY_VIDEO_CALLBACK_SIGNING_SECRET", "")
	t.Setenv("TOTP_ENCRYPTION_KEY", strings.Repeat("02", 32))
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Gateway.Video.Enabled)
	require.True(t, cfg.Gateway.Video.CreationEnabled)
	require.Equal(t, 17, cfg.Gateway.Video.PollIntervalSeconds)
	require.False(t, cfg.Gateway.Video.Callback.Enabled)
	require.Empty(t, cfg.Gateway.Video.Callback.SigningSecret)
}

func TestVideoReleaseCallbackConfigLoadsFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_VIDEO_ENABLED", "true")
	t.Setenv("GATEWAY_VIDEO_CALLBACK_ENABLED", "true")
	t.Setenv("GATEWAY_VIDEO_CALLBACK_SIGNING_SECRET", "callback-signing-secret")
	t.Setenv("TOTP_ENCRYPTION_KEY", strings.Repeat("03", 32))

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Gateway.Video.Enabled)
	require.True(t, cfg.Gateway.Video.Callback.Enabled)
	require.Equal(t, "callback-signing-secret", cfg.Gateway.Video.Callback.SigningSecret)
}
