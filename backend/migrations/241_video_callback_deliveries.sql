SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS video_callback_deliveries (
    id                BIGSERIAL PRIMARY KEY,
    task_id           BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE CASCADE,
    event_id          VARCHAR(128) NOT NULL,
    event_type        VARCHAR(80) NOT NULL,
    event_fingerprint VARCHAR(128) NOT NULL,
    payload           JSONB NOT NULL DEFAULT '{}'::jsonb,
    target_url_enc    TEXT NOT NULL,
    status            VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    lease_owner       VARCHAR(128),
    lease_expires_at  TIMESTAMPTZ,
    last_error        TEXT,
    last_status_code  INTEGER,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at      TIMESTAMPTZ,
    quarantined_at    TIMESTAMPTZ,

    CONSTRAINT video_callback_deliveries_status_check
        CHECK (status IN ('pending', 'delivering', 'delivered', 'failed', 'quarantined')),
    CONSTRAINT video_callback_deliveries_attempts_check
        CHECK (attempts >= 0),
    CONSTRAINT video_callback_deliveries_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_callback_task_fingerprint
    ON video_callback_deliveries (task_id, event_fingerprint);

CREATE INDEX IF NOT EXISTS idx_video_callback_ready
    ON video_callback_deliveries (next_attempt_at, id)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS idx_video_callback_lease
    ON video_callback_deliveries (lease_expires_at, id)
    WHERE status = 'delivering';

CREATE INDEX IF NOT EXISTS idx_video_callback_expiry
    ON video_callback_deliveries (expires_at, id)
    WHERE status NOT IN ('delivered', 'quarantined');

COMMENT ON TABLE video_callback_deliveries IS 'Durable downstream callbacks for video task terminal events';
COMMENT ON COLUMN video_callback_deliveries.payload IS 'Normalized metadata only; provider raw bodies, media bytes, and credential values are forbidden';
