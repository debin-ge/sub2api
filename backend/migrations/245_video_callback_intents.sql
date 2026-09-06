SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE video_tasks
    ADD COLUMN IF NOT EXISTS callback_intent_state VARCHAR(32) NOT NULL DEFAULT 'none';

ALTER TABLE video_tasks
    ADD CONSTRAINT video_tasks_callback_intent_state_check
    CHECK (callback_intent_state IN ('none', 'pending', 'materialized'));
