SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- Internal recovery state is separate from public metadata and frozen prices.
-- No inferred stream/resource recovery or financial backfill of legacy runs.
ALTER TABLE video_tasks ADD COLUMN IF NOT EXISTS provider_execution_state JSONB NOT NULL DEFAULT '{}'::JSONB;
ALTER TABLE video_tasks DROP CONSTRAINT IF EXISTS video_tasks_provider_execution_state_check;
ALTER TABLE video_tasks ADD CONSTRAINT video_tasks_provider_execution_state_check CHECK (
    provider_execution_state = '{}'::JSONB OR (
        jsonb_typeof(provider_execution_state) = 'object'
        AND provider = 'flova'
        AND provider_execution_state ->> 'version' = '1'
        AND provider_execution_state ->> 'provider' = 'flova'
        AND provider_execution_state ->> 'spending_policy' = 'blocked_unverified_budget'
        AND provider_execution_state ->> 'cost_uncertain' = 'true'
    ) IS TRUE
);

-- This records refusals, not approvals. Repeated observations cannot authorize
-- spend. A future verified spending contract needs a separate explicit design.
CREATE TABLE IF NOT EXISTS video_provider_blocked_actions (
    task_id BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE CASCADE,
    action_key CHAR(64) NOT NULL CHECK (action_key ~ '^[0-9a-f]{64}$'),
    action_type VARCHAR(64) NOT NULL CHECK (action_type IN ('high_cost_model_confirm', 'batch_task_limit', 'subscription_required', 'unknown')),
    reason VARCHAR(64) NOT NULL CHECK (reason = 'blocked_unverified_budget'),
    quoted_credits NUMERIC(24, 8) CHECK (quoted_credits >= 0 AND quoted_credits <= 1000000000000),
    quote_conflict BOOLEAN NOT NULL DEFAULT FALSE,
    first_observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, action_key)
);
