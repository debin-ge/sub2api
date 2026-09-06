SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE INDEX IF NOT EXISTS idx_video_tasks_terminal_held_recovery
    ON video_tasks (account_id, id)
    WHERE billing_state = 'held'
      AND generation_state IN ('completed', 'failed', 'cancelled', 'expired');
