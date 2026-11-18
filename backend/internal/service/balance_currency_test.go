//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBalanceCurrencyPublicSettings(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{"", "USD"}, {"USD", "USD"}, {"CNY", "CNY"}, {"EUR", "USD"},
	} {
		t.Run(tc.stored, func(t *testing.T) {
			repo := &settingPublicRepoStub{values: map[string]string{SettingKeyBalanceCurrency: tc.stored}}
			svc := NewSettingService(repo, &config.Config{})
			settings, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, settings.BalanceCurrency)
			require.Contains(t, repo.requestedKeys, SettingKeyBalanceCurrency)
			require.Equal(t, tc.want, svc.parseSettings(repo.values).BalanceCurrency)
			injected, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, injected.(*PublicSettingsInjectionPayload).BalanceCurrency)
		})
	}
}

func TestBalanceCurrencyRejectsUnsupportedUnit(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{}, &config.Config{})
	err := svc.UpdateSettings(context.Background(), &SystemSettings{BalanceCurrency: "EUR"})
	require.ErrorContains(t, err, "balance_currency must be USD or CNY")
}

func TestBalanceCurrencyNotificationEmails(t *testing.T) {
	for _, tc := range []struct{ currency, symbol string }{{"", "$"}, {"USD", "$"}, {"CNY", "¥"}} {
		svc := &NotificationEmailService{settingRepo: &settingPublicRepoStub{values: map[string]string{SettingKeyBalanceCurrency: tc.currency}}}
		for _, event := range []string{NotificationEmailEventBalanceLow, NotificationEmailEventBalanceRechargeSuccess} {
			for _, locale := range []string{notificationEmailDefaultLocale, notificationEmailLocaleChinese} {
				variables := svc.sampleVariables(context.Background(), event, locale)
				require.Equal(t, tc.symbol, variables["balance_currency_symbol"])
				template := notificationEmailOfficialTemplates[event][locale]
				preview, err := renderNotificationEmail(event, template.Subject, template.HTML, variables, nil)
				require.NoError(t, err)
				require.Contains(t, preview.HTML, tc.symbol+"12.34")
			}
		}
	}
}
