//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// 迁移 289：模型价格覆盖行改为「按张 / 按 token」二选一（方案 §6.4 的 R1–R5）。
func TestMigration289ModelPriceOverrideImageEitherOr(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// 测试库启动时已经跑过 289（当时表是空的）。这里在事务内重建现场：清掉备份表和
	// 已有覆盖行，再按生产形态造数据，事务结束后全部回滚。
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS model_price_overrides_backup_289`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `DELETE FROM model_price_overrides`)
	require.NoError(t, err)

	type migrationCase struct {
		name        string
		platform    string
		model       string
		mode        string
		payload     string
		wantMode    string
		wantPayload string
	}
	cases := []migrationCase{
		// 生产形态（方案 §9.1，payload 与生产逐字一致）。
		{
			name: "prod-79-R1", platform: "openai", model: "gpt-image-2", mode: "image",
			payload:  `{"output_cost_per_image": 0.05, "input_cost_per_image_token": 0.000008, "output_cost_per_image_token": 0.00003}`,
			wantMode: "image", wantPayload: `{"output_cost_per_image": 0.05}`,
		},
		{
			name: "prod-88-unchanged", platform: "grok", model: "grok-imagine-image", mode: "image",
			payload: `{"output_cost_per_image": 0.05}`, wantMode: "image", wantPayload: `{"output_cost_per_image": 0.05}`,
		},
		{
			name: "prod-89-unchanged", platform: "grok", model: "grok-imagine-image-2.0", mode: "image",
			payload: `{"output_cost_per_image": 0.1}`, wantMode: "image", wantPayload: `{"output_cost_per_image": 0.1}`,
		},
		{
			name: "prod-90-unchanged", platform: "grok", model: "grok-imagine-image-quality", mode: "image",
			payload: `{"output_cost_per_image": 0.1}`, wantMode: "image", wantPayload: `{"output_cost_per_image": 0.1}`,
		},
		{
			name: "prod-32-R5", platform: "openai", model: "gpt-image-2-1k", mode: "token",
			payload:  `{"input_cost_per_token": 0.000005, "output_cost_per_image": 0.1, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0, "cache_creation_input_token_cost": 0.000002}`,
			wantMode: "image", wantPayload: `{"output_cost_per_image": 0.1}`,
		},
		{
			name: "prod-34-R5", platform: "openai", model: "gpt-image-2-2k", mode: "token",
			payload:  `{"input_cost_per_token": 0.000005, "output_cost_per_image": 0.15, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0, "cache_creation_input_token_cost": 0.000002}`,
			wantMode: "image", wantPayload: `{"output_cost_per_image": 0.15}`,
		},
		{
			name: "prod-39-R5", platform: "openai", model: "gpt-image-2-4k", mode: "token",
			payload:  `{"input_cost_per_token": 0.000005, "output_cost_per_image": 0.2, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0, "cache_creation_input_token_cost": 0.000002}`,
			wantMode: "image", wantPayload: `{"output_cost_per_image": 0.2}`,
		},

		// 各规则各一行。
		{
			name: "R1-intervals-keeps-metadata", platform: "*", model: "gemini-3-pro-image-preview", mode: "image",
			payload:     `{"intervals": [{"tier_label": "1K", "per_request_price": 0.134}], "input_cost_per_token": 0.000002, "long_context_input_token_threshold": 200000, "supports_prompt_caching": true}`,
			wantMode:    "image",
			wantPayload: `{"intervals": [{"tier_label": "1K", "per_request_price": 0.134}], "supports_prompt_caching": true}`,
		},
		{
			name: "R2-gpt-image-without-per-image", platform: "openai", model: "gpt-image-1.5", mode: "image",
			payload:     `{"input_cost_per_image_token": 0.000008, "output_cost_per_image_token": 0.000032}`,
			wantMode:    "token",
			wantPayload: `{"input_cost_per_image_token": 0.000008, "output_cost_per_image_token": 0.000032}`,
		},
		{
			name: "R3-gemini-token-row", platform: "gemini", model: "gemini-2.5-flash-image", mode: "token",
			payload:     `{"input_cost_per_token": 3e-7, "output_cost_per_token": 0.0000025, "output_cost_per_image": 0.039, "output_cost_per_image_token": 0.00003}`,
			wantMode:    "image",
			wantPayload: `{"output_cost_per_image": 0.039}`,
		},
		{
			name: "R3-nano-banana-empty", platform: "*", model: "nano-banana-pro", mode: "token",
			payload: `{}`, wantMode: "image", wantPayload: `{}`,
		},
		{
			name: "R4-gpt-image-both", platform: "openai", model: "gpt-image-1", mode: "token",
			payload:     `{"output_cost_per_image": 0.04, "intervals": [{"tier_label": "2K", "per_request_price": 0.06}], "input_cost_per_token": 0.000005, "input_cost_per_image_token": 0.00001, "output_cost_per_image_token": 0.00004}`,
			wantMode:    "token",
			wantPayload: `{"input_cost_per_token": 0.000005, "input_cost_per_image_token": 0.00001, "output_cost_per_image_token": 0.00004}`,
		},

		// 规则外：迁移不动，运行时按档忽略多余字段。
		{
			name: "manual-token-row-with-per-image", platform: "openai", model: "flux-pro", mode: "token",
			payload:     `{"output_cost_per_image": 0.05, "input_cost_per_token": 0.000001}`,
			wantMode:    "token",
			wantPayload: `{"output_cost_per_image": 0.05, "input_cost_per_token": 0.000001}`,
		},
		{
			name: "manual-image-row-without-per-image", platform: "openai", model: "flux-dev", mode: "image",
			payload:     `{"input_cost_per_token": 0.000001}`,
			wantMode:    "image",
			wantPayload: `{"input_cost_per_token": 0.000001}`,
		},
		{
			name: "unchanged-chat", platform: "*", model: "claude-sonnet-4", mode: "token",
			payload:     `{"input_cost_per_token": 0.000003, "output_cost_per_token": 0.000015}`,
			wantMode:    "token",
			wantPayload: `{"input_cost_per_token": 0.000003, "output_cost_per_token": 0.000015}`,
		},
		{
			name: "unchanged-disabled-gpt-image-token", platform: "openai", model: "gpt-image-2.5-flare", mode: "token",
			payload:     `{"output_cost_per_image_token": 0.00004}`,
			wantMode:    "token",
			wantPayload: `{"output_cost_per_image_token": 0.00004}`,
		},
	}

	ids := make([]int64, len(cases))
	updatedAt := make([]time.Time, len(cases))
	for i, c := range cases {
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO model_price_overrides (platform, model_name, billing_mode, payload, enabled, updated_at)
VALUES ($1, $2, $3, $4::jsonb, $5, NOW() - INTERVAL '3 days')
RETURNING id, updated_at`, c.platform, c.model, c.mode, c.payload, c.name != "unchanged-disabled-gpt-image-token").
			Scan(&ids[i], &updatedAt[i]), c.name)
	}

	migrationSQL, err := dbmigrations.FS.ReadFile("289_model_price_override_image_either_or.sql")
	require.NoError(t, err)

	assertMigrated := func(round int) {
		t.Helper()
		for i, c := range cases {
			var mode, payload string
			var gotUpdatedAt time.Time
			require.NoError(t, tx.QueryRowContext(ctx,
				`SELECT billing_mode, payload::text, updated_at FROM model_price_overrides WHERE id = $1`, ids[i]).
				Scan(&mode, &payload, &gotUpdatedAt), c.name)
			require.Equal(t, c.wantMode, mode, "round %d: %s", round, c.name)
			require.JSONEq(t, c.wantPayload, payload, "round %d: %s", round, c.name)
			require.True(t, updatedAt[i].Equal(gotUpdatedAt), "round %d: %s must keep updated_at", round, c.name)

			var backupMode, backupPayload string
			require.NoError(t, tx.QueryRowContext(ctx,
				`SELECT billing_mode, payload::text FROM model_price_overrides_backup_289 WHERE id = $1`, ids[i]).
				Scan(&backupMode, &backupPayload), c.name)
			require.Equal(t, c.mode, backupMode, "round %d: backup of %s", round, c.name)
			require.JSONEq(t, c.payload, backupPayload, "round %d: backup of %s", round, c.name)
		}
	}

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	assertMigrated(1)

	// 重放：规则不再命中已转换的行，备份表保留第一次的原值。
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	assertMigrated(2)

	var backupRows int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_price_overrides_backup_289`).Scan(&backupRows))
	require.Equal(t, len(cases), backupRows)
}
