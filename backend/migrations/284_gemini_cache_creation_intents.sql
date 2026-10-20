-- Persist admission before a non-idempotent supplier POST. An unknown outcome
-- must survive process loss and cannot expire merely because the client left.
CREATE TABLE gemini_cache_creation_intents (
    public_id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    account_identity_version BIGINT NOT NULL,
    upstream_model VARCHAR(512) NOT NULL,
    ttl_seconds BIGINT NOT NULL CHECK (ttl_seconds > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMPTZ,
    resolution TEXT NOT NULL DEFAULT '',
    CHECK ((resolved_at IS NULL AND resolution = '') OR
           (resolved_at IS NOT NULL AND length(trim(resolution)) > 0))
);

-- Serialize unresolved creates across API keys and gateway instances.
CREATE UNIQUE INDEX idx_gemini_cache_creation_user_pending
    ON gemini_cache_creation_intents (user_id) WHERE resolved_at IS NULL;
CREATE UNIQUE INDEX idx_gemini_cache_creation_account_pending
    ON gemini_cache_creation_intents (account_id) WHERE resolved_at IS NULL;
