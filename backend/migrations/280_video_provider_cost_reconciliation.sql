-- Customer refunds do not prove that a supplier did not incur a cost. Keep this
-- ledger independent from terminal task states and all customer money ledgers.
CREATE TABLE IF NOT EXISTS video_provider_costs (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT REFERENCES video_tasks(id) ON DELETE SET NULL,
    task_public_id TEXT NOT NULL UNIQUE,
    user_id BIGINT,
    account_id BIGINT,
    provider TEXT NOT NULL,
    reason TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'unresolved' CHECK (state IN ('unresolved', 'resolved_no_charge', 'resolved_cost_recorded')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    provider_task_id TEXT NOT NULL DEFAULT '',
    stable_client_token TEXT NOT NULL DEFAULT '',
    account_identity_version BIGINT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    observation_status TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    next_action_at TIMESTAMPTZ DEFAULT NOW(),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    amount NUMERIC(20,8),
    currency TEXT CHECK (currency IN ('USD', 'CNY', 'CREDITS')),
    evidence TEXT NOT NULL DEFAULT '',
    resolved_by BIGINT,
    resolution_key TEXT,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (amount IS NULL OR (amount >= 0 AND amount <= 10000000000)),
    CHECK (((state = 'unresolved' AND amount IS NULL AND resolved_at IS NULL)
        OR (state IN ('resolved_no_charge', 'resolved_cost_recorded')
            AND ((state = 'resolved_no_charge' AND amount = 0)
                OR (state = 'resolved_cost_recorded' AND amount > 0))
            AND currency IS NOT NULL AND resolved_at IS NOT NULL
            AND resolved_by > 0 AND char_length(btrim(evidence)) >= 20 AND octet_length(evidence) <= 2048
            AND char_length(btrim(resolution_key)) BETWEEN 1 AND 128)) IS TRUE)
);
CREATE INDEX IF NOT EXISTS idx_video_provider_costs_user_unresolved ON video_provider_costs (user_id) WHERE state = 'unresolved';
CREATE INDEX IF NOT EXISTS idx_video_provider_costs_account_unresolved ON video_provider_costs (account_id) WHERE state = 'unresolved';
CREATE INDEX IF NOT EXISTS idx_video_provider_costs_due ON video_provider_costs (next_action_at, id) WHERE state = 'unresolved' AND next_action_at IS NOT NULL;

-- Historical candidates are evidence of uncertainty, never proof of a charge.
-- Do not rewrite balances, usage rows, prior outbox payloads, or task outcomes.
INSERT INTO video_provider_costs (task_id, task_public_id, user_id, account_id, provider, reason,
    provider_task_id, stable_client_token, account_identity_version)
SELECT id, public_id, user_id, account_id, provider,
    CASE WHEN provider = 'flova' THEN 'flova_spend_unverified'
        WHEN generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true' THEN 'local_expiry_cost_unverified'
        ELSE 'submission_outcome_unknown' END,
    COALESCE(provider_task_id, ''), COALESCE(stable_client_token, ''),
    CASE WHEN (request_attributes->>'account_identity_version') ~ '^[0-9]{1,18}$'
        THEN (request_attributes->>'account_identity_version')::BIGINT END
FROM video_tasks vt
WHERE source = 'managed'
  AND billing_state IN ('released', 'release_pending', 'captured')
  AND ((billing_state IN ('released', 'release_pending') AND ((submission_unknown_at IS NOT NULL AND NULLIF(provider_task_id, '') IS NULL)
      OR (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true')
      OR EXISTS (SELECT 1 FROM video_task_events e WHERE e.task_id = vt.id
          AND e.event_type IN ('submission_unknown_auto_failed', 'submission_reconciliation_expired'))))
    OR (provider = 'flova' AND (provider_task_id IS NOT NULL OR submission_unknown_at IS NOT NULL)))
ON CONFLICT (task_public_id) DO NOTHING;
