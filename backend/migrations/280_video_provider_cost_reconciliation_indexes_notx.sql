-- Existing task tables can be large; build admission indexes without blocking writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_unknown_cost_user
    ON video_tasks (user_id)
    WHERE source = 'managed' AND billing_state NOT IN ('released', 'captured')
      AND ((NULLIF(provider_task_id, '') IS NULL AND (submission_unknown_at IS NOT NULL OR generation_state IN ('preparing', 'held', 'submitting'))) OR
        (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true'));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_unknown_cost_account
    ON video_tasks (account_id)
    WHERE source = 'managed' AND billing_state NOT IN ('released', 'captured')
      AND ((NULLIF(provider_task_id, '') IS NULL AND (submission_unknown_at IS NOT NULL OR generation_state IN ('preparing', 'held', 'submitting'))) OR
        (generation_state = 'expired' AND request_attributes->>'execution_expiry_enforced_locally' = 'true'));
