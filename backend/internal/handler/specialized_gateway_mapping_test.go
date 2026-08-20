//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type channelMappedGatewaySelectorStub struct {
	attemptedModels []string
	wantExcludedID  int64
}

func (s *channelMappedGatewaySelectorStub) SelectAccountWithLoadAwareness(
	_ context.Context,
	_ *int64,
	_ string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	_ string,
	_ int64,
) (*service.AccountSelectionResult, error) {
	s.attemptedModels = append(s.attemptedModels, requestedModel)
	if s.wantExcludedID != 0 {
		if _, ok := excludedIDs[s.wantExcludedID]; !ok {
			return nil, service.ErrNoAvailableAccounts
		}
	}
	if requestedModel == "public-model" {
		return nil, service.ErrNoAvailableAccounts
	}
	return &service.AccountSelectionResult{
		Account: &service.Account{ID: 202, Platform: service.PlatformDeepSeek},
	}, nil
}

func TestSelectGatewayAccountWithChannelMapping_RoutesMappedModelAcrossProviders(t *testing.T) {
	groupID := int64(10)
	failedID := int64(101)
	selector := &channelMappedGatewaySelectorStub{wantExcludedID: failedID}

	selection, routingModel, err := selectGatewayAccountWithChannelMapping(
		context.Background(),
		selector,
		&groupID,
		"session",
		"public-model",
		service.ChannelMappingResult{Mapped: true, MappedModel: "provider-model"},
		nil,
		map[int64]struct{}{failedID: {}},
		"metadata-user",
		303,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(202), selection.Account.ID)
	require.Equal(t, "provider-model", routingModel)
	require.Equal(t, []string{"public-model", "provider-model"}, selector.attemptedModels)
}

func TestResolveDispatchMappedModelAfterFallback(t *testing.T) {
	tests := []struct {
		name      string
		preferred string
		attempt   string
		selected  string
		want      string
	}{
		{name: "no dispatch preference", preferred: "", attempt: "public-model", selected: "channel-model", want: ""},
		{name: "no fallback keeps preference", preferred: "dispatch-model", attempt: "dispatch-model", selected: "dispatch-model", want: "dispatch-model"},
		{name: "unknown selection keeps preference", preferred: "dispatch-model", attempt: "dispatch-model", selected: "", want: "dispatch-model"},
		{name: "fallback drops preference", preferred: "dispatch-model", attempt: "dispatch-model", selected: "channel-model", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, resolveDispatchMappedModelAfterFallback(tt.preferred, tt.attempt, tt.selected))
		})
	}
}
