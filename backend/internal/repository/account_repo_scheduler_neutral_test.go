package repository

import "testing"

func TestMiniMaxRemainsExtraKeysAreSchedulerNeutral(t *testing.T) {
	for _, key := range []string{
		"minimax_text_5h_limit",
		"minimax_text_5h_remaining",
		"minimax_remains_synced_at",
		"minimax_remains_sync_status",
		"minimax_remains_sync_error",
		"minimax_remains_local_used",
		"minimax_remains_synthetic_added",
		"minimax_remains_synthetic_removed",
		"minimax_remains_calibrated_at",
	} {
		t.Run(key, func(t *testing.T) {
			if !isSchedulerNeutralExtraKey(key) {
				t.Fatalf("expected %q to be scheduler-neutral", key)
			}
		})
	}
}

func TestDeepSeekBalanceExtraKeysAreSchedulerNeutral(t *testing.T) {
	for _, key := range []string{
		"deepseek_balance_available",
		"deepseek_balance_amount",
		"deepseek_balance_currency",
		"deepseek_balance_checked_at",
		"deepseek_balance_status",
		"deepseek_balance_error",
	} {
		t.Run(key, func(t *testing.T) {
			if !isSchedulerNeutralExtraKey(key) {
				t.Fatalf("expected %q to be scheduler-neutral", key)
			}
		})
	}
}
