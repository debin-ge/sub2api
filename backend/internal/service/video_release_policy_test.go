package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestVideoReleaseAllowsSupportedOperationsAndFields(t *testing.T) {
	for _, operation := range []string{
		VideoOperationGenerate,
		VideoOperationEdit,
		VideoOperationExtend,
		VideoOperationCharacterCreate,
	} {
		require.NoError(t, ValidateVideoReleaseOperation(operation))
	}
	for _, body := range []string{
		`{"prompt":"test","characters":[]}`,
		`{"prompt":"test","callback_url":"https://example.com/callback"}`,
	} {
		require.NoError(t, ValidateVideoReleaseJSON(VideoOperationGenerate, []byte(body)))
	}
	require.ErrorIs(t, ValidateVideoReleaseOperation("unknown"), ErrVideoOperationDisabled)
}

func TestVideoCallbackRuntimeFollowsConfiguration(t *testing.T) {
	disabled := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true,
	}}}
	disabledRuntime := ProvideVideoCallbackRuntime(&VideoCallbackWorker{}, disabled)
	defer disabledRuntime.Stop()
	require.Nil(t, disabledRuntime.cancel)

	enabled := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true,
		Callback: config.GatewayVideoCallbackConfig{
			Enabled: true, SigningSecret: "configured",
		},
	}}}
	enabledRuntime := ProvideVideoCallbackRuntime(&VideoCallbackWorker{}, enabled)
	defer enabledRuntime.Stop()
	require.NotNil(t, enabledRuntime.cancel)
	require.NotNil(t, enabledRuntime.done)
}
