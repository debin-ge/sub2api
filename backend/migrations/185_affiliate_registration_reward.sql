-- Registration referral rewards share the existing affiliate quota ledger.
-- Historical invite relationships are intentionally not backfilled.

COMMENT ON COLUMN user_affiliate_ledger.action IS 'accrue|registration_reward|transfer';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_registration_reward_invitee_uniq
    ON user_affiliate_ledger(source_user_id)
    WHERE action = 'registration_reward'
      AND source_user_id IS NOT NULL;
