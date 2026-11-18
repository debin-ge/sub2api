//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsBalanceCurrency(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, currency := range []string{"CNY", "USD"} {
		rec := doUpdateSettings(t, h, map[string]any{"balance_currency": currency}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, currency, repo.values[service.SettingKeyBalanceCurrency])
		var body struct {
			Data struct {
				BalanceCurrency string `json:"balance_currency"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, currency, body.Data.BalanceCurrency)

		rec = doUpdateSettings(t, h, map[string]any{"site_name": "Example"}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, currency, repo.values[service.SettingKeyBalanceCurrency])
	}
	for _, invalid := range []string{"EUR", "usd", "CNY USD"} {
		rec := doUpdateSettings(t, h, map[string]any{"balance_currency": invalid}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, "USD", repo.values[service.SettingKeyBalanceCurrency])
	}
}
