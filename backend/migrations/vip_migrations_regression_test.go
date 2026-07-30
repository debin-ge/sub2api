package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVIPSchemaMigrationIsAdditiveAndDefersValidation(t *testing.T) {
	sqlBytes, err := FS.ReadFile("192_add_vip_schema.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS vip_paid_eligible BOOLEAN NOT NULL DEFAULT false",
		"ADD COLUMN IF NOT EXISTS vip_manual_override BOOLEAN",
		"ADD COLUMN IF NOT EXISTS is_vip BOOLEAN NOT NULL DEFAULT false",
		"ADD COLUMN IF NOT EXISTS vip_only BOOLEAN NOT NULL DEFAULT false",
		"users_vip_effective_state_check",
		"CHECK (is_vip = COALESCE(vip_manual_override, vip_paid_eligible))",
		"groups_vip_only_standard_check",
		"CREATE TABLE IF NOT EXISTS user_vip_audit_events",
		"actor_snapshot          TEXT NOT NULL DEFAULT ''",
		"user_vip_audit_source_check",
		"CHECK (source IN ('', 'payment', 'backfill', 'reconcile', 'manual_on', 'manual_off'))",
		"CREATE TABLE IF NOT EXISTS vip_reconcile_watermark",
		"CREATE TABLE IF NOT EXISTS vip_reconcile_jobs",
		"VALUES (1, 'epoch'::TIMESTAMPTZ, 0, NULL, NOW(), NOW())",
	} {
		require.Contains(t, sql, fragment)
	}

	require.GreaterOrEqual(t, strings.Count(sql, "NOT VALID"), 6)
	require.NotContains(t, strings.ToUpper(sql), "VALIDATE CONSTRAINT")
	require.NotRegexp(t, regexp.MustCompile(`(?is)\bUPDATE\s+users\b`), sql)
	require.NotRegexp(t, regexp.MustCompile(`(?is)\bDROP\s+(?:COLUMN|TABLE)\b`), sql)
}

func TestVIPAuthInvalidationTracksVIPAndCurrentGroupSnapshot(t *testing.T) {
	sqlBytes, err := FS.ReadFile("193_extend_vip_auth_cache_outbox.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "OLD.is_vip IS NOT DISTINCT FROM NEW.is_vip")
	require.Contains(t, sql, "OLD.vip_manual_override IS NOT DISTINCT FROM NEW.vip_manual_override")
	for _, fieldName := range []string{
		"name",
		"platform",
		"is_exclusive",
		"status",
		"subscription_type",
		"rate_multiplier",
		"daily_limit_usd",
		"weekly_limit_usd",
		"monthly_limit_usd",
		"allow_image_generation",
		"allow_batch_image_generation",
		"image_rate_independent",
		"image_rate_multiplier",
		"image_price_1k",
		"image_price_2k",
		"image_price_4k",
		"video_rate_independent",
		"video_rate_multiplier",
		"video_price_480p",
		"video_price_720p",
		"video_price_1080p",
		"web_search_price_per_call",
		"claude_code_only",
		"fallback_group_id",
		"fallback_group_id_on_invalid_request",
		"model_routing",
		"model_routing_enabled",
		"mcp_xml_inject",
		"supported_model_scopes",
		"allow_messages_dispatch",
		"allow_live",
		"default_mapped_model",
		"messages_dispatch_model_config",
		"models_list_config",
		"rpm_limit",
		"max_reasoning_effort",
		"reasoning_effort_mappings",
		"peak_rate_enabled",
		"peak_start",
		"peak_end",
		"peak_rate_multiplier",
		"vip_only",
		"deleted_at",
	} {
		require.Contains(t, sql, "OLD."+fieldName+" IS NOT DISTINCT FROM NEW."+fieldName)
	}

	require.Contains(t, sql, "target_user_ids := ARRAY[OLD.user_id, NEW.user_id]")
	require.Contains(t, sql, "WHERE k.user_id = ANY(target_user_ids)")
}

func TestVIPConcurrentIndexMigrationContainsOnlyConcurrentIndexes(t *testing.T) {
	sqlBytes, err := FS.ReadFile("194_vip_indexes_notx.sql")
	require.NoError(t, err)

	var executableLines []string
	for _, line := range strings.Split(string(sqlBytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		executableLines = append(executableLines, trimmed)
	}
	sql := strings.Join(executableLines, " ")

	require.Contains(t, sql, "idx_payment_orders_vip_reconcile_cursor")
	require.Contains(t, sql, "idx_payment_orders_vip_user_completed")
	require.Contains(t, sql, "idx_user_vip_audit_order_action")
	require.Contains(t, sql, "idx_vip_reconcile_jobs_one_active")
	require.NotContains(t, strings.ToUpper(sql), "BEGIN")
	require.NotContains(t, strings.ToUpper(sql), "COMMIT")

	for _, statement := range strings.Split(sql, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		require.Regexp(
			t,
			regexp.MustCompile(`(?i)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+IF\s+NOT\s+EXISTS\b`),
			statement,
		)
	}
}

func TestVIPPaymentOrderRetentionMigrationGuardsAndArchivesFacts(t *testing.T) {
	sqlBytes, err := FS.ReadFile("195_vip_payment_order_retention.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS vip_payment_order_fact_archive",
		"order_snapshot    JSONB NOT NULL",
		"vip_payment_order_fact_archive_qualifying_check",
		"CREATE INDEX IF NOT EXISTS idx_vip_payment_order_fact_archive_completed",
		"CREATE OR REPLACE VIEW vip_qualifying_payment_order_facts",
		"CREATE OR REPLACE FUNCTION archive_materialized_vip_payment_order_fact()",
		"pg_advisory_xact_lock",
		"hashtext('vip_payment_order_fact')",
		"MESSAGE = 'VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE'",
		"MESSAGE = 'VIP_PAYMENT_FACT_IDENTITY_IMMUTABLE'",
		"u.vip_paid_eligible",
		"e.order_id = OLD.id",
		"e.new_paid_eligible",
		"MESSAGE = 'VIP_PAYMENT_FACT_NOT_MATERIALIZED'",
		"to_jsonb(OLD)",
		"BEFORE INSERT OR UPDATE OR DELETE ON payment_orders",
	} {
		require.Contains(t, sql, fragment)
	}

	viewStart := strings.Index(sql, "CREATE OR REPLACE VIEW vip_qualifying_payment_order_facts")
	viewEnd := strings.Index(sql, "COMMENT ON VIEW vip_qualifying_payment_order_facts")
	require.GreaterOrEqual(t, viewStart, 0)
	require.Greater(t, viewEnd, viewStart)
	viewSQL := sql[viewStart:viewEnd]
	require.Contains(t, viewSQL, "FROM vip_payment_order_fact_archive archived")
	require.Contains(t, viewSQL, "ORDER BY archived.completed_at, archived.order_id")
	require.Contains(t, viewSQL, "UNION ALL")
	require.Contains(t, viewSQL, "ORDER BY po.completed_at, po.id")
	require.NotContains(t, strings.ToUpper(viewSQL), "NOT EXISTS")

	require.NotRegexp(t, regexp.MustCompile(`(?is)\bUPDATE\s+users\b`), sql)
	require.NotRegexp(t, regexp.MustCompile(`(?is)\bDROP\s+(?:COLUMN|TABLE)\b`), sql)
}
