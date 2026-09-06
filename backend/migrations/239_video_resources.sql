SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS video_resources (
    id                         BIGSERIAL PRIMARY KEY,
    public_id                  VARCHAR(64) NOT NULL UNIQUE,
    resource_type              VARCHAR(32) NOT NULL,
    user_id                    BIGINT NOT NULL,
    api_key_id                 BIGINT,
    group_id                   BIGINT,
    provider                   VARCHAR(32) NOT NULL,
    channel_id                 BIGINT,
    account_id                 BIGINT NOT NULL,
    source_task_id             BIGINT REFERENCES video_tasks(id) ON DELETE SET NULL,
    provider_resource_id       VARCHAR(255) NOT NULL,
    model                      VARCHAR(200) NOT NULL DEFAULT '',
    status                     VARCHAR(32) NOT NULL DEFAULT 'ready',
    metadata                   JSONB NOT NULL DEFAULT '{}'::jsonb,
    provider_access_kind       VARCHAR(32),
    provider_access_scope      VARCHAR(64),
    provider_access_enc        TEXT,
    provider_access_expires_at TIMESTAMPTZ,
    version                    BIGINT NOT NULL DEFAULT 0,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at                 TIMESTAMPTZ,
    deleted_at                 TIMESTAMPTZ,

    CONSTRAINT video_resources_type_check
        CHECK (resource_type IN ('character')),
    CONSTRAINT video_resources_status_check
        CHECK (status IN ('creating', 'ready', 'failed', 'expired', 'deleted')),
    CONSTRAINT video_resources_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_resources_provider_resource
    ON video_resources (provider, account_id, provider_resource_id)
    WHERE provider_resource_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_resources_source_task
    ON video_resources (source_task_id)
    WHERE source_task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_video_resources_owner_created
    ON video_resources (user_id, resource_type, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_video_resources_account_active
    ON video_resources (account_id, resource_type, status)
    WHERE status IN ('creating', 'ready');

CREATE INDEX IF NOT EXISTS idx_video_resources_expiry
    ON video_resources (expires_at, id)
    WHERE expires_at IS NOT NULL AND deleted_at IS NULL;

COMMENT ON TABLE video_resources IS 'Metadata-only mappings for reusable provider video resources such as characters';
COMMENT ON COLUMN video_resources.metadata IS 'Filtered metadata only; media bytes and local spool paths are forbidden';
