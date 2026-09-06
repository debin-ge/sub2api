SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE video_tasks ADD COLUMN lease_epoch BIGINT NOT NULL DEFAULT 0
    CHECK (lease_epoch >= 0);
