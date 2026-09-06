package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoPlatformMigrationsAreEmbeddedAndMetadataOnly(t *testing.T) {
	files := []string{
		"238_video_tasks.sql",
		"239_video_resources.sql",
		"240_video_pricing_conditions.sql",
		"241_video_callback_deliveries.sql",
		"242_composite_videos_endpoint.sql",
		"243_video_terminal_hold_recovery.sql",
		"244_video_tasks_account_active_v2_notx.sql",
		"245_video_callback_intents.sql",
		"246_video_callback_intents_index_notx.sql",
		"247_account_ownership.sql",
		"248_video_budget_reservations_index_notx.sql",
		"249_video_task_lease_epoch.sql",
		"250_video_quota_time_contract.sql",
		"251_video_billing_reviews.sql",
		"252_video_execution_write_guards.sql",
		"253_video_submission_reviews.sql",
		"254_video_create_intents.sql",
		"264_account_provider_principals.sql",
		"265_video_failed_auto_release.sql",
		"266_video_task_provider_url.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err)
			require.NotEmpty(t, strings.TrimSpace(string(content)))
			require.NotContains(t, strings.ToUpper(string(content)), " BYTEA")
		})
	}
}

func TestVideoFailedAutoReleaseMigrationRemovesReviewRequirement(t *testing.T) {
	content, err := FS.ReadFile("265_video_failed_auto_release.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, statement, "OLD.generation_state = 'failed' AND NEW.billing_state = 'release_pending'")
	require.Contains(t, statement, "task.generation_state = 'failed' AND NEW.command_payload ->> 'action' = 'release'")
	require.Contains(t, statement, "WHERE generation_state = 'failed' AND billing_state = 'manual_review'")
	require.Contains(t, statement, "billing_review_id = NULL")
}

func TestVideoPlatformMigrationsDoNotInstallRemovedGrokWorkflows(t *testing.T) {
	files, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	for _, name := range files {
		content, err := FS.ReadFile(name)
		require.NoError(t, err)
		for _, removed := range []string{"grok_video_", "usage_billing_effect_archive", "archive_usage_billing_effect", "guard_applied_usage_billing_outbox_delete", "video_create_intent_reviews", "video_create_intent_review_actions", "legacy_grok_import_v1"} {
			require.NotContains(t, strings.ToLower(string(content)), removed, name)
		}
	}
}

func TestVideoTasksMigration238IsImmutable(t *testing.T) {
	tasks, err := FS.ReadFile("238_video_tasks.sql")
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(tasks))))
	require.Equal(t, "762c2ea1e60d76fccff64c5dcd001449f5e0c5b7a11ec9fd2285feea2a72a851", hex.EncodeToString(sum[:]))
}

func TestVideoExecutionWriteMigrationProtectsSnapshotsAndFrozenIntents(t *testing.T) {
	content, err := FS.ReadFile("252_video_execution_write_guards.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, statement, "CREATE TRIGGER video_tasks_execution_guard BEFORE UPDATE")
	require.Contains(t, statement, "video execution and pricing snapshots are immutable")
	require.Contains(t, statement, "video execution financial intent is immutable")
	require.Contains(t, statement, "FOR UPDATE")
	require.Contains(t, statement, "NOT review.honor_frozen_quote")
	require.Contains(t, statement, "BEFORE INSERT OR UPDATE OF request_id, api_key_id, request_fingerprint, payload_version, command_payload, usage_log_payload")
}

func TestAccountOwnershipMigrationPreservesFailClosedConstraints(t *testing.T) {
	content, err := FS.ReadFile("247_account_ownership.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, statement, "owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT")
	require.Contains(t, statement, "CREATE TRIGGER accounts_ownership_guard BEFORE INSERT OR UPDATE")
	require.Contains(t, statement, "NEW.isolation_verified_version := 0")
	require.Contains(t, statement, "NEW.provider_identity_version := OLD.provider_identity_version + 1")
	require.Contains(t, statement, "CREATE FUNCTION account_user_can_schedule")
	require.Contains(t, statement, "alias.owner_user_id IS DISTINCT FROM requesting_user_id")
}

func TestAccountProviderPrincipalMigrationRequiresAuditedBinding(t *testing.T) {
	content, err := FS.ReadFile("264_account_provider_principals.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, statement, "account_provider_identity_reviews")
	require.Contains(t, statement, "provider_principal_binding_id")
	require.Contains(t, statement, "account_provider_identity_alias_conflict")
	require.Contains(t, statement, "account_identity_credentials_overlap")
	require.Contains(t, statement, "account_provider_identity_bindings_guard")
	require.Contains(t, statement, "SELECT 'account_changed',id FROM downgraded")
}

func TestVideoCreateIntentMigrationKeepsNativeBindingAndQuarantine(t *testing.T) {
	content, err := FS.ReadFile("254_video_create_intents.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, statement, "state IN ('prepared','native_bound','untracked')")
	require.Contains(t, statement, "UNIQUE(user_id,endpoint,key_hash)")
	require.Contains(t, statement, "native video creation binding differs from its task")
	require.Contains(t, statement, "video creation outcome is immutable")
	require.Contains(t, statement, "video creation intent cannot be reopened")
	require.Contains(t, statement, "WHERE user_id=OLD.id AND state='untracked'")
	require.Contains(t, statement, "users_video_create_intents_soft_delete_guard")
	for _, removed := range []string{"grok", "receipt_", "review_version", "dispatching", "not_created"} {
		require.NotContains(t, statement, removed)
	}
}

func TestVideoPlatformMigrationsContainRequiredSafetyConstraints(t *testing.T) {
	tasks, err := FS.ReadFile("238_video_tasks.sql")
	require.NoError(t, err)
	tasksSQL := strings.Join(strings.Fields(string(tasks)), " ")
	require.Contains(t, tasksSQL, "uq_video_tasks_owner_idempotency")
	require.Contains(t, tasksSQL, "uq_video_tasks_provider_task")
	require.Contains(t, tasksSQL, "submission_unknown")
	require.Contains(t, tasksSQL, "capture_pending")
	require.Contains(t, tasksSQL, "release_pending")
	require.Contains(t, tasksSQL, "ADD COLUMN IF NOT EXISTS video_owner_user_id BIGINT")
	require.Contains(t, tasksSQL, "ADD COLUMN IF NOT EXISTS video_disclosure_policy VARCHAR(32)")
	require.Contains(t, tasksSQL, "accounts_video_owner_user_id_fkey")
	require.Contains(t, tasksSQL, "accounts_video_disclosure_policy_check")
	require.Contains(t, tasksSQL, "groups_video_disclosure_policy_check")

	pricing, err := FS.ReadFile("240_video_pricing_conditions.sql")
	require.NoError(t, err)
	pricingSQL := strings.Join(strings.Fields(string(pricing)), " ")
	require.Contains(t, pricingSQL, "ADD COLUMN IF NOT EXISTS conditions JSONB")
	require.Contains(t, pricingSQL, "ADD COLUMN IF NOT EXISTS billing_unit VARCHAR(32)")
	require.Contains(t, pricingSQL, "valid_until > valid_from")

	endpoints, err := FS.ReadFile("242_composite_videos_endpoint.sql")
	require.NoError(t, err)
	endpointSQL := strings.Join(strings.Fields(string(endpoints)), " ")
	for _, endpoint := range []string{"'videos'", "'video_characters'", "'video_edits'", "'video_extensions'"} {
		require.Contains(t, endpointSQL, endpoint)
	}

	recovery, err := FS.ReadFile("243_video_terminal_hold_recovery.sql")
	require.NoError(t, err)
	recoverySQL := strings.Join(strings.Fields(string(recovery)), " ")
	require.Contains(t, recoverySQL, "idx_video_tasks_terminal_held_recovery")
	require.Contains(t, recoverySQL, "billing_state = 'held'")

	activeIndex, err := FS.ReadFile("244_video_tasks_account_active_v2_notx.sql")
	require.NoError(t, err)
	activeIndexSQL := strings.Join(strings.Fields(string(activeIndex)), " ")
	require.Contains(t, activeIndexSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_account_active_v2")
	require.Contains(t, activeIndexSQL, "billing_state IN ('held', 'capture_pending')")
	require.Contains(t, activeIndexSQL, "DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_account_active")
}
