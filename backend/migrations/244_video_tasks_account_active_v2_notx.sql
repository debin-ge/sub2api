CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_account_active_v2
    ON video_tasks (account_id, generation_state, billing_state)
    WHERE generation_state IN ('submitting', 'submission_unknown', 'queued', 'in_progress')
       OR billing_state IN ('held', 'capture_pending');

DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_account_active;
