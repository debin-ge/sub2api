package service

// normalizeBalanceCurrency preserves the legacy USD unit for unset or invalid settings.
// This setting changes balance labels only; it does not convert stored amounts or gateway currencies.
func normalizeBalanceCurrency(currency string) string {
	if currency == "CNY" {
		return "CNY"
	}
	return "USD"
}
