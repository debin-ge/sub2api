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

func TestAntigravityRadarExtraKeysAreSchedulerNeutral(t *testing.T) {
	keys := []string{
		"radar_antigravity_sampled_at",
		"radar_antigravity_5h_utilization",
		"radar_antigravity_5h_reset_at",
		"radar_antigravity_subscription_tier",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			if !isSchedulerNeutralExtraKey(key) {
				t.Fatalf("expected %q to be scheduler-neutral", key)
			}
		})
	}

	pureRadarUpdates := make(map[string]any, len(keys))
	for _, key := range keys {
		pureRadarUpdates[key] = "value"
	}
	if shouldEnqueueSchedulerOutboxForExtraUpdates(pureRadarUpdates) {
		t.Fatal("pure Radar observation updates must not enqueue a scheduler rebuild")
	}

	pureRadarUpdates["model_mapping"] = map[string]string{"old": "new"}
	if !shouldEnqueueSchedulerOutboxForExtraUpdates(pureRadarUpdates) {
		t.Fatal("mixing a real scheduling key with Radar observations must remain scheduler-relevant")
	}
}
