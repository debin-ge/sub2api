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
		"269_drop_video_manual_review.sql",
		"269_drop_video_manual_review_notx.sql",
		"270_validate_video_state_checks.sql",
		"271_clear_bytedance_execution_spec_conflict.sql",
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

func TestVideoManualReviewMigrationSettlesBacklogAndDropsTheSchema(t *testing.T) {
	content, err := FS.ReadFile("269_drop_video_manual_review.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")

	// The backlog must be settled before the states stop being representable.
	require.Contains(t, statement, "WHERE generation_state = 'submission_unknown'")
	require.Contains(t, statement, "WHERE billing_state = 'manual_review' AND generation_state = 'completed'")
	require.Contains(t, statement, "actual_units = estimated_units, actual_cost = hold_amount")
	settledAt := strings.Index(statement, "billing_state = 'manual_review'")
	droppedAt := strings.Index(statement, "ADD CONSTRAINT video_tasks_billing_state_check")
	require.Positive(t, settledAt)
	require.Positive(t, droppedAt)
	require.Less(t, settledAt, droppedAt, "the manual_review backlog must be settled before the state is rejected")

	// Neither state may survive in the schema, and no review object may remain.
	for _, removed := range []string{
		"'submission_unknown',", "'manual_review',",
		"guard_video_manual_billing_transition()", "guard_video_unknown_resolution()",
	} {
		require.NotContains(t, statement, "CHECK (generation_state IN ('preparing', 'held', 'submitting', 'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired', "+removed)
	}
	require.Contains(t, statement, "CHECK (generation_state IN ( 'preparing', 'held', 'submitting', 'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired' ))")
	require.Contains(t, statement, "CHECK (billing_state IN ( 'none', 'held', 'capture_pending', 'captured', 'release_pending', 'released' ))")
	require.Contains(t, statement, "DROP COLUMN IF EXISTS billing_review_id")
	require.Contains(t, statement, "DROP COLUMN IF EXISTS submission_review_id")
	for _, table := range []string{"video_billing_review_actions", "video_submission_review_actions", "video_billing_reviews", "video_submission_reviews"} {
		require.Contains(t, statement, "DROP TABLE IF EXISTS "+table)
	}

	// The rebuilt guards keep immutability but no longer demand an approved review.
	require.Contains(t, statement, "video execution and pricing snapshots are immutable")
	require.Contains(t, statement, "video execution financial intent is immutable")
	require.NotContains(t, statement, "requires an approved review")
	require.NotContains(t, statement, "requires a reviewed financial intent")

	// The ByteDance frozen-specification guard was a bug: it stamped every task
	// with a conflict marker that the execution guard then makes permanent, so a
	// stamped task would keep settling at the frozen quote long after the code
	// was fixed. The marker can only be stripped while the guard is off.
	clearedAt := strings.Index(statement, "response_metadata - 'execution_spec_conflict'")
	require.Positive(t, clearedAt)
	require.Less(t, strings.Index(statement, "DISABLE TRIGGER video_tasks_execution_guard"), clearedAt)
	require.Less(t, clearedAt, strings.Index(statement, "ENABLE TRIGGER video_tasks_execution_guard"))
	require.Contains(t, statement, "WHERE provider = 'bytedance'")

	// Audit events must carry the states the rows actually came from. Both
	// backfills read them from a snapshot CTE, because RETURNING yields the new
	// value and the WHERE clause only pins one of the two columns.
	require.Contains(t, statement, "targets.from_generation_state, 'failed', targets.from_billing_state, resolved.billing_state")
	require.Contains(t, statement, "targets.from_generation_state, released.generation_state, 'manual_review', 'release_pending'")

	// The tightened CHECKs skip their full-table scan: this transaction already
	// holds ACCESS EXCLUSIVE on video_tasks. 270 validates the backlog under a
	// lock that does not block reads or writes.
	require.Contains(t, statement, "'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired' )) NOT VALID")
	require.Contains(t, statement, "'release_pending', 'released' )) NOT VALID")
	validate, err := FS.ReadFile("270_validate_video_state_checks.sql")
	require.NoError(t, err)
	validateSQL := strings.Join(strings.Fields(string(validate)), " ")
	require.Contains(t, validateSQL, "ALTER TABLE video_tasks VALIDATE CONSTRAINT %I")
	require.Contains(t, validateSQL, "'video_tasks_generation_state_check', 'video_tasks_billing_state_check'")
	// A database that applied the first version of 269 added both constraints
	// already validated, and an older one may not carry them at all: validating a
	// missing constraint is an error, and that database is the one that has to boot.
	require.Contains(t, validateSQL, "AND NOT convalidated")

	// The reservation index is rebuilt concurrently, outside the transaction.
	concurrent, err := FS.ReadFile("269_drop_video_manual_review_notx.sql")
	require.NoError(t, err)
	concurrentSQL := strings.Join(strings.Fields(string(concurrent)), " ")
	require.Contains(t, concurrentSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_budget_reservations_v2")
	// A failed first attempt leaves an INVALID index of the same name that
	// IF NOT EXISTS would silently accept, while the old index is dropped anyway.
	require.Less(t,
		strings.Index(concurrentSQL, "DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_budget_reservations_v2"),
		strings.Index(concurrentSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_budget_reservations_v2"))
	require.Contains(t, concurrentSQL, "WHERE billing_state IN ('held', 'capture_pending', 'release_pending')")
	require.NotContains(t, concurrentSQL, "manual_review")
	require.NotContains(t, statement, "CREATE INDEX CONCURRENTLY")
}

// The first published version of 269 lacked the conflict-marker cleanup, and a
// database that applied it skips 269 forever on its historical checksum. 271
// carries the same cleanup so those rows are still reached; on a database that
// ran the current 269 it matches nothing.
func TestClearBytedanceExecutionSpecConflictMigrationRepeatsTheCleanup(t *testing.T) {
	content, err := FS.ReadFile("271_clear_bytedance_execution_spec_conflict.sql")
	require.NoError(t, err)
	statement := strings.Join(strings.Fields(string(content)), " ")

	clearedAt := strings.Index(statement, "response_metadata - 'execution_spec_conflict'")
	require.Positive(t, clearedAt)
	require.Less(t, strings.Index(statement, "DISABLE TRIGGER video_tasks_execution_guard"), clearedAt)
	require.Less(t, clearedAt, strings.Index(statement, "ENABLE TRIGGER video_tasks_execution_guard"))

	// Same blast radius as 269 step 4: other providers stamp the marker from a
	// real observation, and a settled task has already moved money.
	require.Contains(t, statement, "WHERE provider = 'bytedance'")
	require.Contains(t, statement, "billing_state NOT IN ('captured', 'released')")
	require.Contains(t, statement, "NOT (response_metadata ? 'specification_invalid')")

	// The event hash is keyed to 271 so a database that ran both migrations keeps
	// two distinct audit rows instead of silently dropping the second.
	require.Contains(t, statement, "'video_execution_spec_conflict_cleared:271:' || id::TEXT")
	require.NotContains(t, statement, ":269:")
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
