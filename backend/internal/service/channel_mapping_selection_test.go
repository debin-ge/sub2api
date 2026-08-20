//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectWithChannelMappingFallback_RetriesMappedModelWithPinnedIdentity(t *testing.T) {
	const (
		requestedModel = "public-model"
		mappedModel    = "provider-model"
	)
	mapping := ChannelMappingResult{
		Mapped:             true,
		MappedModel:        mappedModel,
		BillingModelSource: BillingModelSourceRequested,
	}
	var attempts []string

	selection, routingModel, err := SelectWithChannelMappingFallback(
		context.Background(),
		nil,
		requestedModel,
		mapping,
		func(ctx context.Context, attemptModel string) (string, error) {
			attempts = append(attempts, attemptModel)
			if attemptModel == requestedModel {
				return "", ErrNoAvailableAccounts
			}
			identity, ok := resolvedChannelPricingIdentityFromContext(ctx, attemptModel)
			require.True(t, ok)
			require.Equal(t, requestedModel, identity.requestedModel)
			require.Equal(t, mappedModel, identity.channelMappedModel)
			require.Equal(t, BillingModelSourceRequested, identity.billingModelSource)
			require.True(t, identity.mapped)
			return "mapped-account", nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "mapped-account", selection)
	require.Equal(t, mappedModel, routingModel)
	require.Equal(t, []string{requestedModel, mappedModel}, attempts)
}

// 同一请求内的 failover 重试共享 latch：请求模型池一旦被证明为空，后续每次选号
// 必须直接从渠道映射模型起选，不再重复那一跳必然失败的选号；回退态下的定价身份
// 仍必须被钉住，否则计价与限流守卫会二次套用渠道映射。
func TestSelectWithChannelMappingFallback_LatchSkipsProvenEmptyRequestedModel(t *testing.T) {
	const (
		requestedModel = "public-model"
		mappedModel    = "provider-model"
	)
	mapping := ChannelMappingResult{
		Mapped:             true,
		MappedModel:        mappedModel,
		BillingModelSource: BillingModelSourceRequested,
	}
	var attempts []string
	var state ChannelMappingFallbackState

	attempt := func(ctx context.Context, attemptModel string) (string, error) {
		attempts = append(attempts, attemptModel)
		if attemptModel == requestedModel {
			return "", ErrNoAvailableAccounts
		}
		identity, ok := resolvedChannelPricingIdentityFromContext(ctx, attemptModel)
		require.True(t, ok)
		require.Equal(t, requestedModel, identity.requestedModel)
		require.Equal(t, mappedModel, identity.channelMappedModel)
		require.True(t, identity.mapped)
		return "mapped-account", nil
	}

	for range 2 {
		selection, routingModel, err := SelectWithChannelMappingFallback(
			context.Background(),
			&state,
			requestedModel,
			mapping,
			attempt,
		)
		require.NoError(t, err)
		require.Equal(t, "mapped-account", selection)
		require.Equal(t, mappedModel, routingModel)
	}

	// 第二轮不再出现 requestedModel。
	require.Equal(t, []string{requestedModel, mappedModel, mappedModel}, attempts)
}

func TestSelectWithChannelMappingFallback_DoesNotRetryNonCapacityErrors(t *testing.T) {
	sentinel := errors.New("repository unavailable")
	attempts := 0

	_, routingModel, err := SelectWithChannelMappingFallback(
		context.Background(),
		nil,
		"public-model",
		ChannelMappingResult{Mapped: true, MappedModel: "provider-model"},
		func(context.Context, string) (string, error) {
			attempts++
			return "", sentinel
		},
	)

	require.ErrorIs(t, err, sentinel)
	require.Equal(t, "public-model", routingModel)
	require.Equal(t, 1, attempts)
}

func TestSelectWithChannelMappingFallback_DoesNotRetryWithoutDistinctMapping(t *testing.T) {
	tests := []struct {
		name    string
		mapping ChannelMappingResult
	}{
		{name: "not mapped", mapping: ChannelMappingResult{MappedModel: "provider-model"}},
		{name: "empty mapped model", mapping: ChannelMappingResult{Mapped: true}},
		{name: "same model", mapping: ChannelMappingResult{Mapped: true, MappedModel: "public-model"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			_, routingModel, err := SelectWithChannelMappingFallback(
				context.Background(),
				nil,
				"public-model",
				tt.mapping,
				func(context.Context, string) (string, error) {
					attempts++
					return "", ErrNoAvailableAccounts
				},
			)

			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Equal(t, "public-model", routingModel)
			require.Equal(t, 1, attempts)
		})
	}
}

func TestSelectWithChannelMappingFallback_ReturnsMappedAttemptError(t *testing.T) {
	mappedErr := errors.New("mapped selector failed")

	_, routingModel, err := SelectWithChannelMappingFallback(
		context.Background(),
		nil,
		"public-model",
		ChannelMappingResult{Mapped: true, MappedModel: "provider-model"},
		func(_ context.Context, attemptModel string) (string, error) {
			if attemptModel == "public-model" {
				return "", ErrNoAvailableAccounts
			}
			return "", mappedErr
		},
	)

	require.ErrorIs(t, err, mappedErr)
	require.Equal(t, "provider-model", routingModel)
}

// compact 池被排空同样是「池子为空」，必须触发渠道映射回退；否则 legacy
// /responses/compact 请求会卡在请求模型那一跳，永远碰不到能承接映射模型的账号。
func TestNoAvailableCompactAccountsUnwrapsToNoAvailableAccounts(t *testing.T) {
	require.ErrorIs(t, ErrNoAvailableCompactAccounts, ErrNoAvailableAccounts)
	require.NotErrorIs(t, ErrNoAvailableAccounts, ErrNoAvailableCompactAccounts,
		"通用容量错误不能反向被当成 compact 专有错误，handler 的 compact_not_supported 分支依赖这个方向性")
	require.EqualError(t, ErrNoAvailableCompactAccounts, "no available accounts support /responses/compact")
}

func TestSelectWithChannelMappingFallback_RetriesAfterCompactPoolExhausted(t *testing.T) {
	const (
		requestedModel = "public-model"
		mappedModel    = "provider-model"
	)
	var attempts []string

	selection, routingModel, err := SelectWithChannelMappingFallback(
		context.Background(),
		nil,
		requestedModel,
		ChannelMappingResult{Mapped: true, MappedModel: mappedModel},
		func(_ context.Context, attemptModel string) (string, error) {
			attempts = append(attempts, attemptModel)
			if attemptModel == requestedModel {
				return "", fmt.Errorf("select: %w", ErrNoAvailableCompactAccounts)
			}
			return "compact-account", nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "compact-account", selection)
	require.Equal(t, mappedModel, routingModel)
	require.Equal(t, []string{requestedModel, mappedModel}, attempts)
}

func TestSelectWithChannelMappingRoutingFallback_SeparatesPrimaryRouteFromPricingIdentity(t *testing.T) {
	const (
		requestedModel = "public-model-high"
		primaryModel   = "dispatch-model"
		mappedModel    = "channel-model"
	)
	mapping := ChannelMappingResult{
		Mapped:             true,
		MappedModel:        mappedModel,
		BillingModelSource: BillingModelSourceChannelMapped,
	}
	var attempts []string

	_, routingModel, err := SelectWithChannelMappingRoutingFallback(
		context.Background(),
		nil,
		requestedModel,
		primaryModel,
		mapping,
		func(ctx context.Context, attemptModel string) (string, error) {
			attempts = append(attempts, attemptModel)
			if attemptModel == primaryModel {
				return "", ErrNoAvailableAccounts
			}
			identity, ok := resolvedChannelPricingIdentityFromContext(ctx, attemptModel)
			require.True(t, ok)
			require.Equal(t, requestedModel, identity.requestedModel)
			return "channel-account", nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, mappedModel, routingModel)
	require.Equal(t, []string{primaryModel, mappedModel}, attempts)
}
