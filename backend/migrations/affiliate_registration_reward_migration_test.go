package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffiliateRegistrationRewardMigrationIsIdempotentAndInviteeUnique(t *testing.T) {
	content, err := FS.ReadFile("185_affiliate_registration_reward.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "accrue|registration_reward|transfer")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_registration_reward_invitee_uniq")
	require.Contains(t, sql, "ON user_affiliate_ledger(source_user_id)")
	require.Contains(t, sql, "WHERE action = 'registration_reward'")
	require.Contains(t, sql, "AND source_user_id IS NOT NULL")
	require.NotContains(t, sql, "INSERT INTO user_affiliate_ledger")
	require.NotContains(t, sql, "UPDATE user_affiliates")
}

func TestRegistrationEmailBlacklistMigrationWarnsOnLegacyWhitelist(t *testing.T) {
	seedContent, err := FS.ReadFile("186_registration_email_suffix_blacklist.sql")
	require.NoError(t, err)

	seedSQL := strings.Join(strings.Fields(string(seedContent)), " ")
	// Seeds the new blacklist without copying legacy values (inverted semantics).
	require.Contains(t, seedSQL, "INSERT INTO settings (key, value, updated_at)")
	require.Contains(t, seedSQL, "'registration_email_suffix_blacklist', '[]'")
	require.NotContains(t, seedSQL, "UPDATE settings SET value")

	warningContent, err := FS.ReadFile("187_registration_email_suffix_blacklist_legacy_warning.sql")
	require.NoError(t, err)
	warningSQL := strings.Join(strings.Fields(string(warningContent)), " ")
	// Breaking-change guard: a non-empty legacy whitelist must raise a warning so
	// operators learn the registration restriction has stopped being enforced.
	require.Contains(t, warningSQL, "registration_email_suffix_whitelist")
	require.Contains(t, warningSQL, "RAISE WARNING")
	require.NotContains(t, warningSQL, "UPDATE settings")
}
