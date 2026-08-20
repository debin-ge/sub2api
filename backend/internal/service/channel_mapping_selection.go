package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ChannelMappingSelectionFunc performs one platform-specific account
// selection attempt for the supplied routing model. T deliberately remains
// unconstrained so the same fallback policy can wrap load-aware, capability,
// media, and future schedulers without flattening their result metadata.
//
// The callback must report an exhausted pool as ErrNoAvailableAccounts: that
// error is the only signal that triggers the mapped-model retry. A callback
// that returns a nil selection with a nil error never falls back.
type ChannelMappingSelectionFunc[T any] func(ctx context.Context, routingModel string) (T, error)

// ChannelMappingFallbackState remembers, for the lifetime of a single request,
// that the client-requested model pool was already proven empty. Gateways
// re-select inside a failover loop; without this latch every retry pays for a
// full requested-model pass that is known to fail before falling back.
//
// Latching is safe because exclusions only grow within one request: an
// account pool that could not serve the requested model at the first attempt
// cannot start serving it because more accounts were excluded afterwards.
//
// The zero value is ready to use. Declare it outside the failover loop and
// pass its address; pass nil for single-shot selection.
type ChannelMappingFallbackState struct {
	mappedOnly bool
}

// SelectWithChannelMappingFallback applies the common account-routing policy
// for a request whose channel mapping was resolved once at ingress:
//
//  1. select with the client-requested model to preserve existing account
//     aliases and compatibility;
//  2. only when that attempt reports ErrNoAvailableAccounts, retry with the
//     concrete channel-mapped model that will be forwarded upstream;
//  3. pin the original pricing identity on the fallback context so pricing and
//     model restrictions cannot apply the channel mapping a second time.
//
// routingModel is the model used by the final attempt. Callers should use it
// for availability diagnostics and scheduler health attribution while keeping
// the original requested model for client-visible errors and usage records.
func SelectWithChannelMappingFallback[T any](
	ctx context.Context,
	state *ChannelMappingFallbackState,
	requestedModel string,
	mapping ChannelMappingResult,
	selectAttempt ChannelMappingSelectionFunc[T],
) (selection T, routingModel string, err error) {
	return SelectWithChannelMappingRoutingFallback(
		ctx,
		state,
		requestedModel,
		requestedModel,
		mapping,
		selectAttempt,
	)
}

// SelectWithChannelMappingRoutingFallback is the extended form used by
// compatibility endpoints that normalize the public model or have a separate
// dispatch preference before account selection. requestedModel remains the
// client-visible and pricing identity; primaryRoutingModel is only the first
// model presented to the platform scheduler.
func SelectWithChannelMappingRoutingFallback[T any](
	ctx context.Context,
	state *ChannelMappingFallbackState,
	requestedModel string,
	primaryRoutingModel string,
	mapping ChannelMappingResult,
	selectAttempt ChannelMappingSelectionFunc[T],
) (selection T, routingModel string, err error) {
	if strings.TrimSpace(primaryRoutingModel) == "" {
		primaryRoutingModel = requestedModel
	}
	if selectAttempt == nil {
		return selection, primaryRoutingModel, fmt.Errorf("channel mapping selection callback is nil")
	}

	mappedModel := strings.TrimSpace(mapping.MappedModel)
	canFallBack := mapping.Mapped && mappedModel != "" && mappedModel != strings.TrimSpace(primaryRoutingModel)

	selectMapped := func() (T, string, error) {
		fallbackCtx := WithResolvedChannelPricingIdentity(ctx, requestedModel, mapping)
		mappedSelection, mappedErr := selectAttempt(fallbackCtx, mappedModel)
		return mappedSelection, mappedModel, mappedErr
	}

	// 上一轮已经证明请求模型池为空：直接从映射模型起选，跳过必然失败的一跳。
	if canFallBack && state != nil && state.mappedOnly {
		return selectMapped()
	}

	selection, err = selectAttempt(ctx, primaryRoutingModel)
	if err == nil || !errors.Is(err, ErrNoAvailableAccounts) {
		return selection, primaryRoutingModel, err
	}
	if !canFallBack {
		return selection, primaryRoutingModel, err
	}
	if state != nil {
		state.mappedOnly = true
	}
	return selectMapped()
}
