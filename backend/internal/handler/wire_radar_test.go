package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvideHandlersIncludesRadarHandler(t *testing.T) {
	radar := &RadarHandler{}
	handlers := ProvideHandlers(
		nil, // authHandler
		nil, // userHandler
		nil, // apiKeyHandler
		nil, // usageHandler
		nil, // redeemHandler
		nil, // subscriptionHandler
		nil, // announcementHandler
		nil, // channelMonitorUserHandler
		nil, // channelMonitorV2Handler
		nil, // adminHandlers
		nil, // gatewayHandler
		nil, // openaiGatewayHandler
		nil, // miniMaxGatewayHandler
		nil, // glmGatewayHandler
		nil, // kimiGatewayHandler
		nil, // deepSeekGatewayHandler
		nil, // windsurfGatewayHandler
		nil, // openCodeGatewayHandler
		nil, // settingHandler
		nil, // totpHandler
		nil, // passkeyHandler
		nil, // paymentHandler
		nil, // paymentWebhookHandler
		nil, // availableChannelHandler
		radar,
		nil,         // asyncImageHandler
		nil,         // batchImageHandler
		BuildInfo{}, // buildInfo
		nil,         // idempotencyCoordinator
		nil,         // idempotencyCleanupService
	)

	require.Same(t, radar, handlers.Radar)
}
