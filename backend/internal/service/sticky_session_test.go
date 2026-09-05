//go:build unit

// Package service 提供 API 网关核心服务。
// 本文件包含 shouldClearStickySession 与 Account.IsPermanentlyUnschedulable 的单元测试，
// 验证粘性会话只在账号**永久性**不可用时解绑：限流 / 过载 / 临时停调 / 配额 / 模型级限流
// 都是会自行恢复的瞬时态，本次请求跳过粘性账号但绑定保留，账号恢复后会话自动回来。
//
// This file contains unit tests for shouldClearStickySession, verifying that the
// sticky binding is only dropped for permanent states; transient states keep it.
package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShouldClearStickySession(t *testing.T) {
	now := time.Now()
	future := now.Add(1 * time.Hour)
	past := now.Add(-1 * time.Hour)

	shortRateLimitReset := now.Add(5 * time.Second).Format(time.RFC3339)
	longRateLimitReset := now.Add(30 * time.Second).Format(time.RFC3339)

	tests := []struct {
		name           string
		account        *Account
		requestedModel string
		want           bool
	}{
		{name: "nil account", account: nil, requestedModel: "", want: false},
		// 永久性状态：解绑
		{name: "status error", account: &Account{Status: StatusError, Schedulable: true}, requestedModel: "", want: true},
		{name: "status disabled", account: &Account{Status: StatusDisabled, Schedulable: true}, requestedModel: "", want: true},
		{name: "schedulable false", account: &Account{Status: StatusActive, Schedulable: false}, requestedModel: "", want: true},
		{
			name:           "expired with auto pause",
			account:        &Account{Status: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &past},
			requestedModel: "",
			want:           true,
		},
		{
			name:           "expired without auto pause keeps binding",
			account:        &Account{Status: StatusActive, Schedulable: true, AutoPauseOnExpired: false, ExpiresAt: &past},
			requestedModel: "",
			want:           false,
		},
		{name: "active schedulable", account: &Account{Status: StatusActive, Schedulable: true}, requestedModel: "", want: false},
		// 瞬时状态：保留绑定
		{name: "temp unschedulable keeps binding", account: &Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &future}, requestedModel: "", want: false},
		{name: "temp unschedulable expired", account: &Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &past}, requestedModel: "", want: false},
		{
			name: "model rate limited short duration keeps binding",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"model_rate_limits": map[string]any{
						"claude-sonnet-4": map[string]any{
							"rate_limit_reset_at": shortRateLimitReset,
						},
					},
				},
			},
			requestedModel: "claude-sonnet-4",
			want:           false,
		},
		{
			name: "model rate limited long duration keeps binding",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"model_rate_limits": map[string]any{
						"claude-sonnet-4": map[string]any{
							"rate_limit_reset_at": longRateLimitReset,
						},
					},
				},
			},
			requestedModel: "claude-sonnet-4",
			want:           false,
		},
		{
			name: "apikey quota exceeded keeps binding",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_daily_limit": 10.0,
					"quota_daily_used":  10.0,
					"quota_daily_start": now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
			},
			requestedModel: "",
			want:           false,
		},
		{
			name: "overloaded account keeps binding",
			account: &Account{
				Status:        StatusActive,
				Schedulable:   true,
				OverloadUntil: &future,
			},
			requestedModel: "",
			want:           false,
		},
		{
			name: "account-level rate limited keeps binding",
			account: &Account{
				Status:           StatusActive,
				Schedulable:      true,
				RateLimitResetAt: &future,
			},
			requestedModel: "",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldClearStickySession(tt.account, tt.requestedModel))
		})
	}
}

func TestAccountIsPermanentlyUnschedulable(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	var nilAccount *Account
	require.True(t, nilAccount.IsPermanentlyUnschedulable(), "nil receiver is unusable")

	require.False(t, (&Account{Status: StatusActive, Schedulable: true}).IsPermanentlyUnschedulable())
	require.True(t, (&Account{Status: StatusError, Schedulable: true}).IsPermanentlyUnschedulable())
	require.True(t, (&Account{Status: StatusActive, Schedulable: false}).IsPermanentlyUnschedulable())
	require.True(t, (&Account{Status: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &past}).IsPermanentlyUnschedulable())
	require.False(t, (&Account{Status: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &future}).IsPermanentlyUnschedulable())

	// 瞬时态不算永久不可用，但 IsSchedulable 仍为 false：两者语义必须区分。
	rateLimited := &Account{Status: StatusActive, Schedulable: true, RateLimitResetAt: &future}
	require.False(t, rateLimited.IsPermanentlyUnschedulable())
	require.False(t, rateLimited.IsSchedulable())

	overloaded := &Account{Status: StatusActive, Schedulable: true, OverloadUntil: &future}
	require.False(t, overloaded.IsPermanentlyUnschedulable())
	require.False(t, overloaded.IsSchedulable())
}
