-- Durable notification email delivery for API key configuration and rotation events.
-- Rotation payloads contain only API key id/version; plaintext credentials stay in api_keys.

CREATE TABLE IF NOT EXISTS notification_email_outbox (
    id               BIGSERIAL PRIMARY KEY,
    event_type       VARCHAR(80) NOT NULL,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id       BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    recipient_email  VARCHAR(320) NOT NULL,
    rotation_version BIGINT,
    payload           JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedup_key         VARCHAR(255) NOT NULL UNIQUE,
    attempts          INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at        TIMESTAMPTZ,
    claimed_by        VARCHAR(100),
    last_error        TEXT,
    sent_at           TIMESTAMPTZ,
    cancelled_at      TIMESTAMPTZ,
    cancel_reason     VARCHAR(255),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_email_outbox_available
    ON notification_email_outbox (available_at, id)
    WHERE sent_at IS NULL AND cancelled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_notification_email_outbox_claimed
    ON notification_email_outbox (claimed_at)
    WHERE sent_at IS NULL AND cancelled_at IS NULL AND claimed_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notification_email_outbox_api_key_rotation
    ON notification_email_outbox (api_key_id, rotation_version)
    WHERE event_type = 'api_key.rotated' AND sent_at IS NULL AND cancelled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_notification_email_outbox_completed
    ON notification_email_outbox (COALESCE(sent_at, cancelled_at))
    WHERE sent_at IS NOT NULL OR cancelled_at IS NOT NULL;

COMMENT ON TABLE notification_email_outbox IS 'Durable at-least-once delivery queue for notification emails';
COMMENT ON COLUMN notification_email_outbox.payload IS 'Structured non-secret template variables; rotation credentials are loaded at send time';
