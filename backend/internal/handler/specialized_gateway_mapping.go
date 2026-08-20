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

// channelMappedGatewayAccountSelector is implemented by GatewayService and by
// the provider-specific handler test doubles. Keeping the platform-specific
// selector behind this minimal interface lets every gateway share the same
// mapped-model fallback policy without widening their public handler APIs.
type channelMappedGatewayAccountSelector interface {
	SelectAccountWithLoadAwareness(
		ctx context.Context,
		groupID *int64,
		sessionHash string,
		requestedModel string,
		excludedIDs map[int64]struct{},
		metadataUserID string,
		sub2apiUserID int64,
	) (*service.AccountSelectionResult, error)
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

// selectGatewayAccountWithChannelMapping applies the service-level generic
// fallback policy to GatewayService's load-aware selector. failed/excluded
// accounts are captured by the callback and therefore remain excluded on both
// the requested-model and mapped-model attempts.
//
// fallbackState must be declared outside the caller's failover loop so that a
// requested-model pool proven empty is not re-probed on every account switch.
func selectGatewayAccountWithChannelMapping(
	ctx context.Context,
	selector channelMappedGatewayAccountSelector,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	mapping service.ChannelMappingResult,
	fallbackState *service.ChannelMappingFallbackState,
	excludedIDs map[int64]struct{},
	metadataUserID string,
	sub2apiUserID int64,
) (*service.AccountSelectionResult, string, error) {
	return service.SelectWithChannelMappingFallback(
		ctx,
		fallbackState,
		requestedModel,
		mapping,
		func(attemptCtx context.Context, attemptModel string) (*service.AccountSelectionResult, error) {
			return selector.SelectAccountWithLoadAwareness(
				attemptCtx,
				groupID,
				sessionHash,
				attemptModel,
				excludedIDs,
				metadataUserID,
				sub2apiUserID,
			)
		},
	)
}

// resolveDispatchMappedModelAfterFallback decides whether the /v1/messages
// dispatch mapping ("preferred" model that the group wants this request routed
// to) is still valid after account selection.
//
// The selector first tries attemptRoutingModel and only falls back to the
// channel-mapped model when that pool reports ErrNoAvailableAccounts. A
// different selectedRoutingModel therefore means the dispatch model was just
// proven to have no account behind it, and the account we did get was chosen
// solely for the channel-mapped model. Forwarding still preferring the dispatch
// model would send that account a model it does not serve, so the preference is
// dropped and forwarding falls back to the channel mapping already applied to
// the body.
func resolveDispatchMappedModelAfterFallback(preferredMappedModel, attemptRoutingModel, selectedRoutingModel string) string {
	preferred := strings.TrimSpace(preferredMappedModel)
	if preferred == "" {
		return ""
	}
	selected := strings.TrimSpace(selectedRoutingModel)
	if selected == "" || selected == strings.TrimSpace(attemptRoutingModel) {
		return preferredMappedModel
	}
	return ""
}
