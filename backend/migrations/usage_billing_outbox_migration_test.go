package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingOutboxMigrationDefinesDurableLeasedQueue(t *testing.T) {
	content, err := FS.ReadFile("194_usage_billing_outbox.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists usage_billing_outbox")
	require.Contains(t, sql, "alter column request_id type varchar(255)")
	require.Contains(t, sql, "unique (request_id, api_key_id)")
	require.Contains(t, sql, "request_fingerprint char(64) not null")
	require.Contains(t, sql, "command_payload")
	require.Contains(t, sql, "usage_log_payload")
	require.Contains(t, sql, "result_payload")
	require.Contains(t, sql, "stage")
	require.Contains(t, sql, "terminal_at")
	require.Contains(t, sql, "terminal_reason")
	require.Contains(t, sql, "claimed_at")
	require.Contains(t, sql, "claimed_by")
	require.Contains(t, sql, "available_at")
}

func TestUnknownImageSizeMigrationPreservesUnpricedSettlementEvidence(t *testing.T) {
	content, err := FS.ReadFile("195_allow_unpriced_unknown_image_size.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "drop constraint if exists usage_logs_image_billing_size_check")
	require.Contains(t, sql, "add constraint usage_logs_image_billing_size_check")
	require.Contains(t, sql, "or billing_state = 1")
	require.Contains(t, sql, "image_size in ('1k', '2k', '4k', 'mixed')")
	require.Contains(t, sql, "not valid")
}
