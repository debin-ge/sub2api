package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// specializedGatewayChannelMapper is shared by provider-specific gateway
// handlers that still use GatewayService for scheduling and usage billing.
type specializedGatewayChannelMapper interface {
	ResolveRequestChannelMapping(ctx context.Context, groupID *int64, model string) service.ChannelMappingResult
}

// resolveSpecializedGatewayChannelMapping resolves the public-to-channel model
// mapping once and applies it to the exact body sent to the provider-specific
// forwarder. Account-level mapping remains the forwarder's responsibility.
//
// Model restriction is deliberately not checked here. Every one of these
// handlers reaches upstream through GatewayService.SelectAccountWithLoadAwareness,
// which runs checkChannelPricingRestriction against the post-Claude-Code-fallback
// group and rejects with ErrNoAvailableAccounts. Repeating the check here would
// use the pre-fallback group and could reject requests the scheduler would allow.
func resolveSpecializedGatewayChannelMapping(
	ctx context.Context,
	mapper specializedGatewayChannelMapper,
	groupID *int64,
	requestedModel string,
	body []byte,
) (service.ChannelMappingResult, []byte) {
	mapping := mapper.ResolveRequestChannelMapping(ctx, groupID, requestedModel)
	if strings.TrimSpace(mapping.MappedModel) == "" {
		mapping.MappedModel = requestedModel
	}
	if !mapping.Mapped {
		return mapping, body
	}
	return mapping, service.ReplaceModelInBody(body, mapping.MappedModel)
}
