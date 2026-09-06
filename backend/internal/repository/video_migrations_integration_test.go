//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoPlatformMigrationsSchema(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "accounts", "video_owner_user_id", "bigint", 0, true)
	requireColumn(t, tx, "accounts", "video_disclosure_policy", "character varying", 32, true)
	requireForeignKeyOnDelete(t, tx, "accounts", "video_owner_user_id", "users", "SET NULL")
	requireConstraintDefinitionContains(
		t, tx, "accounts", "accounts_video_disclosure_policy_check",
		"none", "identity", "task_access", "dedicated_credentials",
	)
	requireColumn(t, tx, "groups", "video_disclosure_policy", "character varying", 32, true)
	requireConstraintDefinitionContains(
		t, tx, "groups", "groups_video_disclosure_policy_check",
		"none", "identity", "task_access", "dedicated_credentials",
	)

	requireColumn(t, tx, "video_tasks", "public_id", "character varying", 64, false)
	requireColumn(t, tx, "video_tasks", "input_manifest", "jsonb", 0, false)
	requireColumn(t, tx, "video_tasks", "generation_state", "character varying", 32, false)
	requireColumn(t, tx, "video_tasks", "billing_state", "character varying", 32, false)
	requireColumn(t, tx, "video_tasks", "provider_access_enc", "text", 0, true)
	requireColumn(t, tx, "video_tasks", "provider_video_url_enc", "text", 0, true)
	requireColumn(t, tx, "video_tasks", "provider_video_proxy_key", "character varying", 64, true)
	requireIndex(t, tx, "video_tasks", "idx_video_tasks_owner_provider_video_proxy_key")
	requireIndex(t, tx, "video_tasks", "uq_video_tasks_owner_idempotency")
	requireIndex(t, tx, "video_tasks", "uq_video_tasks_provider_task")
	requireIndex(t, tx, "video_tasks", "idx_video_tasks_next_action")
	requireIndex(t, tx, "video_tasks", "idx_video_tasks_account_active_v2")
	requireConstraintDefinitionContains(t, tx, "video_tasks", "video_tasks_generation_state_check", "submission_unknown", "completed")
	requireConstraintDefinitionContains(t, tx, "video_tasks", "video_tasks_billing_state_check", "capture_pending", "release_pending", "manual_review")
	requireColumn(t, tx, "video_create_intents", "native_task_id", "bigint", 0, true)
	requireColumn(t, tx, "video_create_intents", "lease_epoch", "bigint", 0, false)
	requireConstraintDefinitionContains(t, tx, "video_create_intents", "video_create_intents_request_contract_check", "canonical_json_v1", "canonical_multipart_v1", "native_task_v1")
	requireConstraintDefinitionContains(t, tx, "video_create_intents", "video_create_intents_state_check", "prepared", "native_bound", "untracked")
	var activeIndexDefinition string
	err := tx.QueryRowContext(context.Background(), `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename = 'video_tasks'
		  AND indexname = 'idx_video_tasks_account_active_v2'
	`).Scan(&activeIndexDefinition)
	require.NoError(t, err)
	require.Contains(t, activeIndexDefinition, "'held'::character varying")
	require.Contains(t, activeIndexDefinition, "'capture_pending'::character varying")

	requireColumn(t, tx, "video_task_events", "payload", "jsonb", 0, false)
	requireIndex(t, tx, "video_task_events", "uq_video_task_events_provider_event")

	requireColumn(t, tx, "video_resources", "provider_resource_id", "character varying", 255, false)
	requireColumn(t, tx, "video_resources", "metadata", "jsonb", 0, false)
	requireIndex(t, tx, "video_resources", "uq_video_resources_provider_resource")
	requireForeignKeyOnDelete(t, tx, "video_resources", "source_task_id", "video_tasks", "SET NULL")

	requireColumn(t, tx, "video_callback_deliveries", "payload", "jsonb", 0, false)
	requireColumn(t, tx, "video_callback_deliveries", "target_url_enc", "text", 0, false)
	requireIndex(t, tx, "video_callback_deliveries", "uq_video_callback_task_fingerprint")
	requireForeignKeyOnDelete(t, tx, "video_callback_deliveries", "task_id", "video_tasks", "CASCADE")

	for _, table := range []string{"channel_pricing_intervals", "channel_account_stats_pricing_intervals"} {
		requireColumn(t, tx, table, "conditions", "jsonb", 0, false)
		requireColumn(t, tx, table, "billing_unit", "character varying", 32, true)
		requireColumn(t, tx, table, "priority", "integer", 0, false)
		requireColumn(t, tx, table, "valid_from", "timestamp with time zone", 0, true)
		requireColumn(t, tx, table, "valid_until", "timestamp with time zone", 0, true)
	}

	requireConstraintDefinitionContains(
		t,
		tx,
		"composite_model_routes",
		"composite_model_routes_endpoint_check",
		"'videos'",
		"'video_characters'",
		"'video_edits'",
		"'video_extensions'",
	)

	var binaryColumns int
	err = tx.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('video_tasks', 'video_task_events', 'video_resources', 'video_callback_deliveries')
		  AND data_type = 'bytea'
	`).Scan(&binaryColumns)
	require.NoError(t, err)
	require.Zero(t, binaryColumns, "video platform tables must remain metadata-only")

	for _, migration := range []string{
		"238_video_tasks.sql",
		"239_video_resources.sql",
		"240_video_pricing_conditions.sql",
		"241_video_callback_deliveries.sql",
		"242_composite_videos_endpoint.sql",
		"243_video_terminal_hold_recovery.sql",
		"244_video_tasks_account_active_v2_notx.sql",
		"254_video_create_intents.sql",
		"264_account_provider_principals.sql",
		"265_video_failed_auto_release.sql",
		"266_video_task_provider_url.sql",
	} {
		var applied bool
		err := tx.QueryRowContext(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)
		`, migration).Scan(&applied)
		require.NoError(t, err)
		require.True(t, applied, "expected migration %s to be recorded", migration)
	}
}

func TestVideoPlatformMigrationsExcludeRemovedGrokObjects(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	for _, query := range []string{
		`SELECT COUNT(*) FROM pg_class WHERE relnamespace='public'::regnamespace
		 AND (position('grok_video' in relname)>0 OR position('usage_billing_effect' in relname)>0
		 OR relname IN ('video_create_intent_reviews','video_create_intent_review_actions'))`,
		`SELECT COUNT(*) FROM pg_proc WHERE pronamespace='public'::regnamespace
		 AND (position('grok_video' in proname)>0 OR position('usage_billing_effect' in proname)>0
		 OR proname IN ('guard_applied_usage_billing_outbox_delete','video_create_intent_review_facts',
		 'guard_video_create_intent_review_change','guard_video_create_intent_review_action_change'))`,
		`SELECT COUNT(*) FROM pg_trigger WHERE NOT tgisinternal
		 AND (position('grok_video' in tgname)>0 OR tgname IN ('usage_billing_outbox_effect_archive',
		 'usage_billing_outbox_applied_delete_guard','video_create_intents_legacy_import_guard',
		 'api_keys_video_create_intents_delete_guard','groups_video_create_intents_delete_guard',
		 'accounts_video_create_intents_delete_guard','accounts_video_create_intents_soft_delete_guard'))`,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='video_create_intents'
		 AND column_name IN ('receipt_ciphertext','receipt_hash','receipt_expires_at','receipt_purged_at',
		 'dispatch_contract_version','grok_group_id','grok_billing_snapshot','review_version','resolution_review_id',
		 'dispatched_at','completed_at','account_identity_version')`,
		`SELECT COUNT(*) FROM schema_migrations WHERE filename ~ '^(25[5-9]|26[0-3]|26[5-9])_'
		 AND filename NOT IN ('265_video_failed_auto_release.sql', '266_video_task_provider_url.sql')`,
	} {
		var count int
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&count))
		require.Zero(t, count, query)
	}
}
