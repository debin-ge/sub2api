//go:build unit

package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestEvaluateAccountSchedulingThreshold_OpenAIChoosesLatestResetWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	wantUntil := now.Add(72 * time.Hour)
	account := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_5h_used_percent": 90.0,
			"codex_5h_reset_at":     now.Add(2 * time.Hour).Format(time.RFC3339),
			"codex_7d_used_percent": 85.0,
			"codex_7d_reset_at":     wantUntil.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformOpenAI: 80,
	}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, PlatformOpenAI, decision.Platform)
	require.Equal(t, "7d", decision.Window)
	require.Empty(t, decision.Scope)
	require.Equal(t, 85.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_OpenAIIgnoresMismatchedCodexSnapshotIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 8, 50, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"email":                "CageLeen9208@outlook.com",
			"chatgpt_account_id":   "1f945aa7-d9a9-4369-9542-0c702ff4adb0",
			"workspace_id":         "org-nU4goUxMmureroyswT5oYPv4",
			"chatgpt_workspace_id": "org-nU4goUxMmureroyswT5oYPv4",
		},
		Extra: map[string]any{
			"email":                 "MasonDobies01@outlook.com",
			"name":                  "Paul Clark",
			"workspace_id":          "org-avRk1G4qdXg7qph3cRIraNKf",
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformOpenAI: 99,
	}, now)

	require.False(t, decision.ShouldPause)
}

func TestEvaluateAccountSchedulingThreshold_AnthropicIgnoresExpiredFiveHourWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	expiredEnd := now.Add(-30 * time.Minute)
	wantUntil := now.Add(5 * 24 * time.Hour)
	account := &Account{
		Platform:         PlatformAnthropic,
		SessionWindowEnd: &expiredEnd,
		Extra: map[string]any{
			"session_window_utilization":   0.99,
			"passive_usage_7d_utilization": 0.82,
			"passive_usage_7d_reset":       float64(wantUntil.Unix()),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformAnthropic: 80,
	}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, PlatformAnthropic, decision.Platform)
	require.Equal(t, "7d", decision.Window)
	require.Empty(t, decision.Scope)
	require.Equal(t, 82.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAnthropicFableSchedulingThreshold_UsesAccountOverrideWithoutPausingAccount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)
	wantUntil := now.Add(4 * 24 * time.Hour)
	account := &Account{
		Platform: PlatformAnthropic,
		Credentials: map[string]any{
			"account_scheduling_threshold": 60,
		},
		Extra: map[string]any{
			"passive_usage_7d_utilization":    0.40,
			"passive_usage_7d_reset":          float64(now.Add(3 * 24 * time.Hour).Unix()),
			"passive_usage_7d_oi_utilization": 0.61,
			"passive_usage_7d_oi_reset":       float64(wantUntil.Unix()),
		},
	}

	thresholds := map[string]int{
		PlatformAnthropic: 100,
	}

	accountDecision := EvaluateAccountSchedulingThreshold(account, thresholds, now)
	require.False(t, accountDecision.ShouldPause)

	decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, PlatformAnthropic, decision.Platform)
	require.Equal(t, "7d_oi", decision.Window)
	require.Equal(t, anthropicFableRateLimitKey, decision.Scope)
	require.Equal(t, 60, decision.ThresholdPercent)
	require.Equal(t, 61.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_OpenAIPreservesPercentageSemantics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	// 5h 窗口的 reset 必须落在 schedulingMax5hResetAge 之内，否则会被判为不可信而丢弃。
	openAIUntil := now.Add(2 * time.Hour)
	openAIAccount := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     openAIUntil.Format(time.RFC3339),
		},
	}

	candidate := openAIThresholdCandidate(openAIAccount.Extra, "5h", now)
	require.NotNil(t, candidate)
	require.Equal(t, 1.0, candidate.usedPercent)

	openAIDecision := EvaluateAccountSchedulingThreshold(openAIAccount, map[string]int{
		PlatformOpenAI: 90,
	}, now)
	require.False(t, openAIDecision.ShouldPause)

	openAIAccount.Extra["codex_5h_used_percent"] = 91.0
	openAIDecision = EvaluateAccountSchedulingThreshold(openAIAccount, map[string]int{
		PlatformOpenAI: 90,
	}, now)
	require.True(t, openAIDecision.ShouldPause)
	require.Equal(t, 91.0, openAIDecision.UsedPercent)
}

func TestEvaluateAccountSchedulingThreshold_OpenAISkipsStaleSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			"codex_5h_used_percent":  100.0,
			"codex_5h_reset_at":      now.Add(3 * time.Hour).Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenAI: 90}, now)

	require.False(t, decision.ShouldPause)
}

func TestEvaluateAccountSchedulingThreshold_OpenAISkipsResetWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
			"codex_5h_used_percent":  100.0,
			"codex_5h_reset_at":      now.Add(-time.Second).Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenAI: 90}, now)

	require.False(t, decision.ShouldPause)
}

func TestEvaluateAccountSchedulingThreshold_OpenAIPausesFreshExhaustedSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(3 * time.Hour)
	account := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
			"codex_5h_used_percent":  100.0,
			"codex_5h_reset_at":      resetAt.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenAI: 90}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, "5h", decision.Window)
	require.Equal(t, 100.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, resetAt.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_OpenAIPausesFreshExhaustedSevenDayWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(5 * 24 * time.Hour)
	account := &Account{
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
			"codex_7d_used_percent":  95.0,
			"codex_7d_reset_at":      resetAt.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenAI: 90}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, "7d", decision.Window)
	require.Equal(t, 95.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, resetAt.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_AnthropicPreservesFractionalUtilizationSemantics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)

	anthropicUntil := now.Add(5 * time.Hour)
	anthropicAccount := &Account{
		Platform:         PlatformAnthropic,
		SessionWindowEnd: &anthropicUntil,
		Extra: map[string]any{
			"session_window_utilization": 0.92,
		},
	}

	anthropicDecision := EvaluateAccountSchedulingThreshold(anthropicAccount, map[string]int{
		PlatformAnthropic: 90,
	}, now)

	require.True(t, anthropicDecision.ShouldPause)
	require.Equal(t, 92.0, anthropicDecision.UsedPercent)
}

func TestEvaluateAccountSchedulingThreshold_AccountOverrideCanLowerOpenAIThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	wantUntil := now.Add(12 * time.Hour)
	account := &Account{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"account_scheduling_threshold": 80,
		},
		Extra: map[string]any{
			"codex_7d_used_percent": 85.0,
			"codex_7d_reset_at":     wantUntil.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformOpenAI: 90,
	}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, PlatformOpenAI, decision.Platform)
	require.Equal(t, 80, decision.ThresholdPercent)
	require.Equal(t, "7d", decision.Window)
	require.Empty(t, decision.Scope)
	require.Equal(t, 85.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_AccountOverrideHundredDisablesOpenAI(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"account_scheduling_threshold": 100,
		},
		Extra: map[string]any{
			"codex_7d_used_percent": 99.0,
			"codex_7d_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformOpenAI: 80,
	}, now)

	require.False(t, decision.ShouldPause)
	require.Equal(t, 100, decision.ThresholdPercent)
}

func TestEvaluateAccountSchedulingThreshold_AccountOverrideRoundsDecimalThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	wantUntil := now.Add(12 * time.Hour)
	account := &Account{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"account_scheduling_threshold": 75.5,
		},
		Extra: map[string]any{
			"codex_7d_used_percent": 80.0,
			"codex_7d_reset_at":     wantUntil.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformOpenAI: 90,
	}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, 76, decision.ThresholdPercent)
	require.Equal(t, 80.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_UnsupportedPlatformsDoNotPause(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		platform  string
		threshold int
		extra     map[string]any
	}{
		{
			name:      "gemini",
			platform:  PlatformGemini,
			threshold: 80,
			extra: map[string]any{
				"gemini_usage_raw": map[string]any{
					"buckets": []any{
						map[string]any{
							"modelId":           "gemini-2.5-pro",
							"remainingFraction": 0.05,
							"resetTime":         now.Add(2 * time.Hour).Format(time.RFC3339),
						},
					},
				},
			},
		},
		{
			name:      "kiro",
			platform:  PlatformKiro,
			threshold: 90,
			extra: map[string]any{
				"kiro_sched_utilization": 99.0,
				"kiro_sched_reset_at":    now.Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
		{
			name:      "antigravity",
			platform:  PlatformAntigravity,
			threshold: 90,
			extra: map[string]any{
				"antigravity_sched_utilization": 92.0,
				"antigravity_sched_reset_at":    now.Add(48 * time.Hour).Format(time.RFC3339),
				"antigravity_sched_scope":       "gemini",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			account := &Account{
				Platform: tc.platform,
				Credentials: map[string]any{
					"account_scheduling_threshold": 1,
				},
				Extra: tc.extra,
			}

			decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
				tc.platform: tc.threshold,
			}, now)

			require.False(t, decision.ShouldPause)
			require.Zero(t, decision.ThresholdPercent)
		})
	}
}

func TestEvaluateAccountSchedulingThreshold_GrokUsesConfiguredThresholds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	wantUntil := now.Add(2 * time.Hour)
	account := &Account{
		Platform: PlatformGrok,
		Extra: map[string]any{
			"grok_sched_utilization": 92.0,
			"grok_sched_reset_at":    wantUntil.Format(time.RFC3339),
		},
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{
		PlatformGrok: 90,
	}, now)

	require.True(t, decision.ShouldPause)
	require.Equal(t, PlatformGrok, decision.Platform)
	require.Equal(t, 90, decision.ThresholdPercent)
	require.Equal(t, "grok", decision.Scope)
	require.Equal(t, 92.0, decision.UsedPercent)
	require.NotNil(t, decision.Until)
	require.True(t, wantUntil.Equal(*decision.Until))
}

func TestEvaluateAccountSchedulingThreshold_GrokUsesOnlyHeaderQuotaWindow(t *testing.T) {
	t.Parallel()
	// Billing seven_day/thirty_day must not drive pause; only grok_sched_* may.
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	weeklyEnd := now.Add(3 * time.Hour)
	weeklyPct := 99.0
	headerUntil := now.Add(2 * time.Hour)
	account := &Account{
		Platform: PlatformGrok,
		Extra: map[string]any{
			"grok_sched_utilization": 50.0, // below threshold
			"grok_sched_reset_at":    headerUntil.Format(time.RFC3339),
			grokBillingExtraKey: &xai.BillingSummary{
				UsagePercent: &weeklyPct,
				PeriodEnd:    weeklyEnd.Format(time.RFC3339),
			},
		},
	}
	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformGrok: 90}, now)
	require.False(t, decision.ShouldPause, "high billing % alone must not pause under scheduling windows")

	account.Extra["grok_sched_utilization"] = 95.0
	decision = EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformGrok: 90}, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, "grok", decision.Scope)
	require.Equal(t, "quota", decision.Window)
	require.NotNil(t, decision.Until)
	require.True(t, headerUntil.Equal(*decision.Until))
}

// TestParseSchedulingResetAt_BoundsUntrustedValues account.Extra 里的窗口重置时间
// 来自上游配额接口 / 响应头投影，不可信。越界值一旦被当作候选的 until，会被
// SetTempUnschedulable 原样写入，把账号误停调数天甚至数年。
func TestParseSchedulingResetAt_BoundsUntrustedValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	valid := now.Add(2 * time.Hour)

	t.Run("接受合法值", func(t *testing.T) {
		require.NotNil(t, parseSchedulingResetAt(valid.Format(time.RFC3339), now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(valid.Unix(), now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(float64(valid.Unix()), now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(int(valid.Unix()), now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(json.Number(strconv.FormatInt(valid.Unix(), 10)), now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(valid, now, schedulingMax5hResetAge))
		require.NotNil(t, parseSchedulingResetAt(&valid, now, schedulingMax5hResetAge))
	})

	t.Run("毫秒时间戳自动识别", func(t *testing.T) {
		got := parseSchedulingResetAt(valid.UnixMilli(), now, schedulingMax5hResetAge)
		require.NotNil(t, got)
		require.True(t, valid.Equal(*got))

		got = parseSchedulingResetAt(float64(valid.UnixMilli()), now, schedulingMax5hResetAge)
		require.NotNil(t, got)
		require.True(t, valid.Equal(*got))
	})

	t.Run("拒绝过去时间", func(t *testing.T) {
		past := now.Add(-time.Minute)
		require.Nil(t, parseSchedulingResetAt(past.Format(time.RFC3339), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(past.Unix(), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(past, now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(&past, now, schedulingMax5hResetAge))
	})

	t.Run("拒绝超出 maxAge 的未来时间", func(t *testing.T) {
		far := now.Add(30 * 24 * time.Hour)
		require.Nil(t, parseSchedulingResetAt(far.Format(time.RFC3339), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(far.Unix(), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(far.Unix(), now, schedulingMax7dResetAge))
		require.Nil(t, parseSchedulingResetAt(json.Number(strconv.FormatInt(far.Unix(), 10)), now, schedulingMax7dResetAge))
	})

	t.Run("拒绝非法与零值", func(t *testing.T) {
		require.Nil(t, parseSchedulingResetAt(nil, now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt("", now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt("not-a-time", now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(int64(0), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt(float64(-1), now, schedulingMax5hResetAge))
		require.Nil(t, parseSchedulingResetAt((*time.Time)(nil), now, schedulingMax5hResetAge))
	})
}

// TestAnthropicThresholdCandidates_RejectsOutOfRangeReset passive_usage_7d_reset
// 写入侧（account_usage_service）没有做边界校验，读侧必须兜住：越界值只让候选失效
// （until == nil → candidateMatchesThreshold 判负），不得据此停调。
func TestAnthropicThresholdCandidates_RejectsOutOfRangeReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformAnthropic,
		Extra: map[string]any{
			"passive_usage_7d_utilization": 0.99,
			// 毫秒当秒：被原样接受的话会锁到 50 年后
			"passive_usage_7d_reset": float64(now.Add(3*24*time.Hour).UnixMilli()) * 1000,
		},
	}

	for _, candidate := range anthropicThresholdCandidates(account, now) {
		require.Nil(t, candidate.until)
		require.False(t, candidateMatchesThreshold(candidate, 80, now))
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformAnthropic: 80}, now)
	require.False(t, decision.ShouldPause, "越界 reset 不得触发停调")
}

// TestAnthropicFableThresholdCandidate_RejectsOutOfRangeReset 与整账号级同理，
// 但作用于模型级封锁（SetModelRateLimit）。
func TestAnthropicFableThresholdCandidate_RejectsOutOfRangeReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformAnthropic,
		Extra: map[string]any{
			"passive_usage_7d_oi_utilization": 0.99,
			"passive_usage_7d_oi_reset":       float64(now.Add(365 * 24 * time.Hour).Unix()),
		},
	}

	candidate := anthropicFableThresholdCandidate(account, now)
	require.NotNil(t, candidate)
	require.Nil(t, candidate.until)
	require.False(t, candidateMatchesThreshold(candidate, 80, now))
}

// TestGrokThresholdCandidates_RejectsOutOfRangeReset grok_sched_reset_at 写入侧已限
// now+25h，读侧的 48h 上限是纵深防御：写入侧一旦劣化也不会把账号锁到几年后。
func TestGrokThresholdCandidates_RejectsOutOfRangeReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformGrok,
		Extra: map[string]any{
			"grok_sched_utilization": 95.0,
			"grok_sched_reset_at":    now.Add(10 * 24 * time.Hour).Format(time.RFC3339),
		},
	}

	for _, candidate := range grokThresholdCandidates(account, now) {
		require.Nil(t, candidate.until)
	}

	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformGrok: 90}, now)
	require.False(t, decision.ShouldPause, "越界 reset 不得触发停调")
}

// TestOpenAIThresholdCandidate_RejectsOutOfRangeReset codex_5h/7d_reset_at 越界时
// 候选失效，账号继续可调度（而不是被停到几年后）。
func TestOpenAIThresholdCandidate_RejectsOutOfRangeReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	extra := map[string]any{
		"codex_5h_used_percent": 99.0,
		// 5h 窗口给了 3 天后的时间，超出 schedulingMax5hResetAge
		"codex_5h_reset_at":     now.Add(3 * 24 * time.Hour).Format(time.RFC3339),
		"codex_7d_used_percent": 99.0,
		"codex_7d_reset_at":     now.Add(60 * 24 * time.Hour).Format(time.RFC3339),
	}

	for _, window := range []string{"5h", "7d"} {
		candidate := openAIThresholdCandidate(extra, window, now)
		require.NotNil(t, candidate)
		require.Nil(t, candidate.until, "window %s", window)
	}

	account := &Account{Platform: PlatformOpenAI, Extra: extra}
	decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformOpenAI: 90}, now)
	require.False(t, decision.ShouldPause, "越界 reset 不得触发停调")
}
