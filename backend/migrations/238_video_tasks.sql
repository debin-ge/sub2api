SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS video_owner_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS video_disclosure_policy VARCHAR(32);

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_disclosure_policy VARCHAR(32);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_video_owner_user_id_fkey'
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_video_owner_user_id_fkey
            FOREIGN KEY (video_owner_user_id) REFERENCES users(id) ON DELETE SET NULL NOT VALID;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_video_disclosure_policy_check'
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_video_disclosure_policy_check
            CHECK (video_disclosure_policy IS NULL OR video_disclosure_policy IN ('none', 'identity', 'task_access', 'dedicated_credentials'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'groups_video_disclosure_policy_check'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_video_disclosure_policy_check
            CHECK (video_disclosure_policy IS NULL OR video_disclosure_policy IN ('none', 'identity', 'task_access', 'dedicated_credentials'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_accounts_video_owner
    ON accounts (video_owner_user_id, id)
    WHERE deleted_at IS NULL AND video_owner_user_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS video_tasks (
    id                         BIGSERIAL PRIMARY KEY,
    public_id                  VARCHAR(64) NOT NULL UNIQUE,
    source                     VARCHAR(16) NOT NULL DEFAULT 'managed',
    user_id                    BIGINT,
    api_key_id                 BIGINT,
    group_id                   BIGINT,
    channel_id                 BIGINT,
    account_id                 BIGINT,
    account_owner_user_id      BIGINT,
    provider                   VARCHAR(32) NOT NULL,
    operation                  VARCHAR(32) NOT NULL DEFAULT 'generate',
    parent_task_id             BIGINT REFERENCES video_tasks(id) ON DELETE SET NULL,
    root_task_id               BIGINT REFERENCES video_tasks(id) ON DELETE SET NULL,
    endpoint                   VARCHAR(64) NOT NULL DEFAULT 'videos',

    requested_model            VARCHAR(200) NOT NULL DEFAULT '',
    public_model               VARCHAR(200) NOT NULL DEFAULT '',
    channel_model              VARCHAR(200) NOT NULL DEFAULT '',
    upstream_model             VARCHAR(200) NOT NULL DEFAULT '',

    request_hash               VARCHAR(128) NOT NULL,
    idempotency_key            VARCHAR(255),
    input_manifest             JSONB NOT NULL DEFAULT '[]'::jsonb,
    request_attributes         JSONB NOT NULL DEFAULT '{}'::jsonb,

    provider_task_id           VARCHAR(255),
    provider_status            VARCHAR(64),
    provider_created_at        TIMESTAMPTZ,
    provider_finished_at       TIMESTAMPTZ,
    stable_client_token        VARCHAR(128),

    generation_state           VARCHAR(32) NOT NULL DEFAULT 'preparing',
    billing_state              VARCHAR(32) NOT NULL DEFAULT 'none',
    delete_state               VARCHAR(32) NOT NULL DEFAULT 'none',
    version                    BIGINT NOT NULL DEFAULT 0,

    progress                   NUMERIC(7,4),
    usage_snapshot             JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_variants           JSONB NOT NULL DEFAULT '[]'::jsonb,
    content_expires_at         TIMESTAMPTZ,

    provider_access_kind       VARCHAR(32),
    provider_access_scope      VARCHAR(64),
    provider_access_enc        TEXT,
    provider_access_expires_at TIMESTAMPTZ,

    billing_unit               VARCHAR(32),
    estimated_units            NUMERIC(20,8),
    actual_units               NUMERIC(20,8),
    price_snapshot             JSONB NOT NULL DEFAULT '{}'::jsonb,
    provider_cost_snapshot     JSONB NOT NULL DEFAULT '{}'::jsonb,
    currency                   VARCHAR(16) NOT NULL DEFAULT 'USD',
    hold_id                    VARCHAR(128),
    hold_amount                NUMERIC(20,10),
    actual_cost                NUMERIC(20,10),

    callback_url_enc           TEXT,
    next_action_at             TIMESTAMPTZ,
    poll_attempts              INTEGER NOT NULL DEFAULT 0,
    submit_attempts            INTEGER NOT NULL DEFAULT 0,
    lease_owner                VARCHAR(128),
    lease_expires_at           TIMESTAMPTZ,

    last_error_kind            VARCHAR(32),
    last_error_code            VARCHAR(128),
    last_error_message         TEXT,

    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at               TIMESTAMPTZ,
    started_at                 TIMESTAMPTZ,
    finished_at                TIMESTAMPTZ,
    settled_at                 TIMESTAMPTZ,
    submission_unknown_at      TIMESTAMPTZ,
    quarantined_at             TIMESTAMPTZ,
    deleted_at                 TIMESTAMPTZ,

    CONSTRAINT video_tasks_source_check
        CHECK (source IN ('managed', 'external')),
    CONSTRAINT video_tasks_managed_owner_check
        CHECK (source <> 'managed' OR user_id IS NOT NULL),
    CONSTRAINT video_tasks_operation_check
        CHECK (operation IN ('generate', 'edit', 'extend', 'character_create')),
    CONSTRAINT video_tasks_generation_state_check
        CHECK (generation_state IN (
            'preparing', 'held', 'submitting', 'submission_unknown',
            'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired'
        )),
    CONSTRAINT video_tasks_billing_state_check
        CHECK (billing_state IN (
            'none', 'held', 'capture_pending', 'captured',
            'release_pending', 'released', 'manual_review'
        )),
    CONSTRAINT video_tasks_delete_state_check
        CHECK (delete_state IN ('none', 'requested', 'deleting', 'deleted', 'delete_failed')),
    CONSTRAINT video_tasks_progress_check
        CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100)),
    CONSTRAINT video_tasks_amounts_nonnegative_check
        CHECK (
            (estimated_units IS NULL OR estimated_units >= 0) AND
            (actual_units IS NULL OR actual_units >= 0) AND
            (hold_amount IS NULL OR hold_amount >= 0) AND
            (actual_cost IS NULL OR actual_cost >= 0)
        ),
    CONSTRAINT video_tasks_input_manifest_array_check
        CHECK (jsonb_typeof(input_manifest) = 'array'),
    CONSTRAINT video_tasks_request_attributes_object_check
        CHECK (jsonb_typeof(request_attributes) = 'object'),
    CONSTRAINT video_tasks_usage_snapshot_object_check
        CHECK (jsonb_typeof(usage_snapshot) = 'object'),
    CONSTRAINT video_tasks_response_metadata_object_check
        CHECK (jsonb_typeof(response_metadata) = 'object'),
    CONSTRAINT video_tasks_content_variants_array_check
        CHECK (jsonb_typeof(content_variants) = 'array')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_tasks_provider_task
    ON video_tasks (provider, account_id, provider_task_id)
    WHERE provider_task_id IS NOT NULL AND provider_task_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_tasks_owner_idempotency
    ON video_tasks (user_id, endpoint, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_video_tasks_owner_created
    ON video_tasks (user_id, created_at DESC, id DESC)
    WHERE source = 'managed';

CREATE INDEX IF NOT EXISTS idx_video_tasks_account_active
    ON video_tasks (account_id, generation_state, billing_state)
    WHERE generation_state IN ('submitting', 'submission_unknown', 'queued', 'in_progress')
       OR billing_state = 'capture_pending';

CREATE INDEX IF NOT EXISTS idx_video_tasks_next_action
    ON video_tasks (next_action_at, id)
    WHERE next_action_at IS NOT NULL
      AND generation_state NOT IN ('completed', 'failed', 'cancelled', 'expired');

CREATE INDEX IF NOT EXISTS idx_video_tasks_billing_pending
    ON video_tasks (billing_state, next_action_at, id)
    WHERE billing_state IN ('capture_pending', 'release_pending');

CREATE INDEX IF NOT EXISTS idx_video_tasks_delete_pending
    ON video_tasks (delete_state, next_action_at, id)
    WHERE delete_state IN ('requested', 'delete_failed');

CREATE INDEX IF NOT EXISTS idx_video_tasks_lease_expiry
    ON video_tasks (lease_expires_at, id)
    WHERE lease_owner IS NOT NULL;

CREATE TABLE IF NOT EXISTS video_task_events (
    id                    BIGSERIAL PRIMARY KEY,
    task_id               BIGINT REFERENCES video_tasks(id) ON DELETE SET NULL,
    event_type            VARCHAR(80) NOT NULL,
    provider              VARCHAR(32),
    account_id            BIGINT,
    provider_task_id      VARCHAR(255),
    provider_event_id     VARCHAR(255),
    from_generation_state VARCHAR(32),
    to_generation_state   VARCHAR(32),
    from_billing_state    VARCHAR(32),
    to_billing_state      VARCHAR(32),
    payload               JSONB NOT NULL DEFAULT '{}'::jsonb,
    event_hash            VARCHAR(128),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT video_task_events_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_task_events_provider_event
    ON video_task_events (provider, COALESCE(account_id, 0), provider_event_id)
    WHERE provider_event_id IS NOT NULL AND provider_event_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_video_task_events_task_hash
    ON video_task_events (task_id, event_hash)
    WHERE task_id IS NOT NULL AND event_hash IS NOT NULL AND event_hash <> '';

CREATE INDEX IF NOT EXISTS idx_video_task_events_task_created
    ON video_task_events (task_id, created_at, id)
    WHERE task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_video_task_events_unmatched
    ON video_task_events (provider, account_id, provider_task_id, created_at)
    WHERE task_id IS NULL;

COMMENT ON TABLE video_tasks IS 'Provider-neutral video generation tasks; media bytes are never stored here';
COMMENT ON COLUMN video_tasks.input_manifest IS 'Metadata only: input role, sanitized filename, MIME, size, and SHA-256';
COMMENT ON COLUMN video_tasks.response_metadata IS 'Filtered provider metadata with no media bytes or shared account credentials';
COMMENT ON TABLE video_task_events IS 'Append-only video lifecycle, provider observation, billing, and audit events';
