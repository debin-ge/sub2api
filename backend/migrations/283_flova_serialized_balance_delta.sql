SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- Flova spending contract (docs/Flova 成本约束设计.md): one task per dedicated
-- account, budget-bounded confirmations, and a balance-delta reconciliation
-- that can close the supplier cost record without an operator.

-- 1. New tasks run under serialized_balance_delta; legacy rows keep the blocked
--    policy. A resumed run records its stream once, beside the original identity.
ALTER TABLE video_tasks DROP CONSTRAINT IF EXISTS video_tasks_provider_execution_state_check;
ALTER TABLE video_tasks ADD CONSTRAINT video_tasks_provider_execution_state_check CHECK (
    provider_execution_state = '{}'::JSONB OR (
        jsonb_typeof(provider_execution_state) = 'object'
        AND provider = 'flova'
        AND provider_execution_state ->> 'version' = '1'
        AND provider_execution_state ->> 'provider' = 'flova'
        AND provider_execution_state ->> 'spending_policy' IN ('blocked_unverified_budget', 'serialized_balance_delta')
        AND provider_execution_state ->> 'cost_uncertain' = 'true'
    ) IS TRUE
);

-- 2. Confirmations above the task budget are refused under their own reason.
ALTER TABLE video_provider_blocked_actions DROP CONSTRAINT IF EXISTS video_provider_blocked_actions_reason_check;
ALTER TABLE video_provider_blocked_actions ADD CONSTRAINT video_provider_blocked_actions_reason_check
    CHECK (reason IN ('blocked_unverified_budget', 'exceeds_task_budget'));

-- 3. Balance observations for the stability check, and automatic resolutions
--    that carry an auto: key instead of an operator.
ALTER TABLE video_provider_costs ADD COLUMN IF NOT EXISTS observed_balance NUMERIC(24,8);
ALTER TABLE video_provider_costs ADD COLUMN IF NOT EXISTS observed_balance_at TIMESTAMPTZ;

DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT c.conname FROM pg_constraint c
        WHERE c.conrelid = 'video_provider_costs'::regclass AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) LIKE '%resolved_by > 0%'
    LOOP
        EXECUTE format('ALTER TABLE video_provider_costs DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

ALTER TABLE video_provider_costs ADD CONSTRAINT video_provider_costs_resolution_check CHECK (
    ((state = 'unresolved' AND amount IS NULL AND resolved_at IS NULL)
    OR (state IN ('resolved_no_charge', 'resolved_cost_recorded')
        AND ((state = 'resolved_no_charge' AND amount = 0)
            OR (state = 'resolved_cost_recorded' AND amount > 0))
        AND currency IS NOT NULL AND resolved_at IS NOT NULL
        AND (resolved_by > 0 OR (resolved_by IS NULL AND resolution_key LIKE 'auto:%'))
        AND char_length(btrim(evidence)) >= 20 AND octet_length(evidence) <= 2048
        AND char_length(btrim(resolution_key)) BETWEEN 1 AND 128)) IS TRUE
);

ALTER TABLE video_provider_costs DROP CONSTRAINT IF EXISTS video_provider_costs_observed_balance_check;
ALTER TABLE video_provider_costs ADD CONSTRAINT video_provider_costs_observed_balance_check
    CHECK (observed_balance IS NULL OR (observed_balance >= -1000000000000 AND observed_balance <= 1000000000000));
