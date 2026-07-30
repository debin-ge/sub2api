-- Durable post-usage billing intents.
--
-- The upstream request has already succeeded when these rows are created, so a
-- transient billing or usage-log write failure must survive process restarts.
-- request_id + api_key_id is the same idempotency key used by
-- usage_billing_dedup and usage_logs.

-- Upstream request identifiers are not universally capped at 64 bytes. Keep
-- the log key width aligned with the pre-existing billing dedup key so a
-- successfully forwarded request cannot become a permanently unwriteable
-- outbox item solely because its upstream ID is longer.
ALTER TABLE usage_logs
    ALTER COLUMN request_id TYPE VARCHAR(255);

CREATE TABLE IF NOT EXISTS usage_billing_outbox (
    id                  BIGSERIAL PRIMARY KEY,
    request_id          VARCHAR(255) NOT NULL,
    api_key_id          BIGINT NOT NULL,
    request_fingerprint CHAR(64) NOT NULL
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    payload_version     SMALLINT NOT NULL DEFAULT 1
        CHECK (payload_version >= 1),
    stage               SMALLINT NOT NULL DEFAULT 0
        CHECK (stage IN (0, 1)),
    command_payload     JSONB NOT NULL,
    usage_log_payload   JSONB NOT NULL,
    result_payload      JSONB,
    available_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts            INTEGER NOT NULL DEFAULT 0
        CHECK (attempts >= 0),
    last_error          TEXT,
    claimed_at          TIMESTAMPTZ,
    claimed_by          TEXT,
    terminal_at         TIMESTAMPTZ,
    terminal_reason     TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (request_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_billing_outbox_available
    ON usage_billing_outbox (available_at, id)
    WHERE terminal_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_usage_billing_outbox_claimed_at
    ON usage_billing_outbox (claimed_at)
    WHERE claimed_at IS NOT NULL;

COMMENT ON TABLE usage_billing_outbox IS
    'Durable staged intents for atomic usage billing, usage_log insertion, and replay-safe post effects';
