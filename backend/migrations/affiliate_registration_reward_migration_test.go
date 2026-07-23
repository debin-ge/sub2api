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
	content, err := FS.ReadFile("186_registration_email_suffix_blacklist.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	// Seeds the new blacklist without copying legacy values (inverted semantics).
	require.Contains(t, sql, "INSERT INTO settings (key, value, updated_at)")
	require.Contains(t, sql, "'registration_email_suffix_blacklist', '[]'")
	require.NotContains(t, sql, "UPDATE settings SET value")
	// Breaking-change guard: a non-empty legacy whitelist must raise a warning so
	// operators learn the registration restriction has stopped being enforced.
	require.Contains(t, sql, "registration_email_suffix_whitelist")
	require.Contains(t, sql, "RAISE WARNING")
}
