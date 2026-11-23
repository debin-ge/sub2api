package service

import "context"

// normalizeBalanceCurrency preserves the legacy USD unit for unset or invalid settings.
// This setting changes balance labels only; it does not convert stored amounts or gateway currencies.
func normalizeBalanceCurrency(currency string) string {
	if currency == "CNY" {
		return "CNY"
	}
	return "USD"
}

// balanceCurrencySymbol returns the display symbol for a balance currency setting value.
func balanceCurrencySymbol(currency string) string {
	if normalizeBalanceCurrency(currency) == "CNY" {
		return "¥"
	}
	return "$"
}

// balanceCurrencySymbolFromRepo reads the site balance currency symbol, falling back to "$"
// when the setting is unavailable so labels keep the legacy USD unit.
func balanceCurrencySymbolFromRepo(ctx context.Context, repo SettingRepository) string {
	if repo == nil {
		return "$"
	}
	currency, err := repo.GetValue(ctx, SettingKeyBalanceCurrency)
	if err != nil {
		return "$"
	}
	return balanceCurrencySymbol(currency)
}

// BalanceCurrencySymbol returns the symbol ("$" or "¥") used to label user balance amounts.
func (s *SettingService) BalanceCurrencySymbol(ctx context.Context) string {
	if s == nil {
		return "$"
	}
	return balanceCurrencySymbolFromRepo(ctx, s.settingRepo)
}

// BalanceCurrencySymbol returns the site balance currency symbol for user-facing billing messages.
func (s *OpenAIGatewayService) BalanceCurrencySymbol(ctx context.Context) string {
	if s == nil {
		return "$"
	}
	return s.settingService.BalanceCurrencySymbol(ctx)
}
