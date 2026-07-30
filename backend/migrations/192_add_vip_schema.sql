-- Paid VIP entitlement schema expand.
--
-- This migration is intentionally additive and contains no historical backfill.
-- Existing rows satisfy every new NOT VALID constraint through the column defaults.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS vip_paid_eligible BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS vip_paid_eligible_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS vip_paid_source VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS vip_manual_override BOOLEAN,
    ADD COLUMN IF NOT EXISTS vip_override_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS vip_override_by BIGINT,
    ADD COLUMN IF NOT EXISTS vip_override_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS is_vip BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS vip_granted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS vip_effective_source VARCHAR(20) NOT NULL DEFAULT '';

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS vip_only BOOLEAN NOT NULL DEFAULT false;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'users'::regclass
          AND conname = 'users_vip_effective_state_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_vip_effective_state_check
            CHECK (is_vip = COALESCE(vip_manual_override, vip_paid_eligible))
            NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'users'::regclass
          AND conname = 'users_vip_paid_state_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_vip_paid_state_check
            CHECK (
                (
                    vip_paid_eligible
                    AND vip_paid_eligible_at IS NOT NULL
                    AND vip_paid_source IN ('payment', 'backfill', 'reconcile')
                )
                OR
                (
                    NOT vip_paid_eligible
                    AND vip_paid_eligible_at IS NULL
                    AND vip_paid_source = ''
                )
            )
            NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'users'::regclass
          AND conname = 'users_vip_granted_at_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_vip_granted_at_check
            CHECK (
                (is_vip AND vip_granted_at IS NOT NULL)
                OR (NOT is_vip AND vip_granted_at IS NULL)
            )
            NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'users'::regclass
          AND conname = 'users_vip_override_metadata_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_vip_override_metadata_check
            CHECK (
                (
                    vip_manual_override IS NULL
                    AND vip_override_at IS NULL
                    AND vip_override_by IS NULL
                    AND vip_override_reason = ''
                )
                OR
                (
                    vip_manual_override IS NOT NULL
                    AND vip_override_at IS NOT NULL
                    AND vip_override_by IS NOT NULL
                    AND BTRIM(vip_override_reason) <> ''
                )
            )
            NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'users'::regclass
          AND conname = 'users_vip_effective_source_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_vip_effective_source_check
            CHECK (
                (
                    vip_manual_override IS TRUE
                    AND is_vip
                    AND vip_effective_source = 'manual_on'
                )
                OR
                (
                    vip_manual_override IS FALSE
                    AND NOT is_vip
                    AND vip_effective_source = 'manual_off'
                )
                OR
                (
                    vip_manual_override IS NULL
                    AND vip_paid_eligible
                    AND is_vip
                    AND vip_effective_source = vip_paid_source
                )
                OR
                (
                    vip_manual_override IS NULL
                    AND NOT vip_paid_eligible
                    AND NOT is_vip
                    AND vip_effective_source = ''
                )
            )
            NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'groups'::regclass
          AND conname = 'groups_vip_only_standard_check'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_vip_only_standard_check
            CHECK (NOT vip_only OR subscription_type = 'standard')
            NOT VALID;
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS user_vip_audit_events (
    id                      BIGSERIAL PRIMARY KEY,
    user_id                 BIGINT NOT NULL,
    actor_type              VARCHAR(20) NOT NULL,
    actor_user_id           BIGINT,
    actor_snapshot          TEXT NOT NULL DEFAULT '',
    action                  VARCHAR(40) NOT NULL,
    reason                  TEXT NOT NULL DEFAULT '',
    order_id                BIGINT,
    request_id              VARCHAR(128) NOT NULL DEFAULT '',
    old_paid_eligible       BOOLEAN NOT NULL,
    new_paid_eligible       BOOLEAN NOT NULL,
    old_manual_override     BOOLEAN,
    new_manual_override     BOOLEAN,
    old_is_vip              BOOLEAN NOT NULL,
    new_is_vip              BOOLEAN NOT NULL,
    source                  VARCHAR(20) NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_vip_audit_actor_check
        CHECK (
            (actor_type = 'system' AND actor_user_id IS NULL)
            OR
            (
                actor_type = 'admin'
                AND actor_user_id IS NOT NULL
                AND BTRIM(actor_snapshot) <> ''
            )
        ),
    CONSTRAINT user_vip_audit_action_check
        CHECK (action IN ('paid_grant', 'manual_mode', 'backfill', 'reconcile')),
    CONSTRAINT user_vip_audit_source_check
        CHECK (source IN ('', 'payment', 'backfill', 'reconcile', 'manual_on', 'manual_off')),
    CONSTRAINT user_vip_audit_admin_reason_check
        CHECK (action <> 'manual_mode' OR BTRIM(reason) <> '')
);

COMMENT ON TABLE user_vip_audit_events IS
    'Immutable VIP entitlement transitions; deliberately independent from general admin audit logs';
COMMENT ON COLUMN user_vip_audit_events.actor_snapshot IS
    'Immutable display identity retained even if the administrator account is later deleted';
COMMENT ON COLUMN user_vip_audit_events.order_id IS
    'Payment fact identifier retained without a foreign key so order archival cannot erase entitlement history';

CREATE TABLE IF NOT EXISTS vip_reconcile_watermark (
    id                    SMALLINT PRIMARY KEY,
    completed_at_cursor   TIMESTAMPTZ NOT NULL,
    order_id_cursor       BIGINT NOT NULL,
    backfill_cutoff       TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT vip_reconcile_watermark_singleton_check CHECK (id = 1),
    CONSTRAINT vip_reconcile_watermark_order_cursor_check CHECK (order_id_cursor >= 0)
);

INSERT INTO vip_reconcile_watermark (
    id,
    completed_at_cursor,
    order_id_cursor,
    backfill_cutoff,
    created_at,
    updated_at
)
VALUES (1, 'epoch'::TIMESTAMPTZ, 0, NULL, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

COMMENT ON TABLE vip_reconcile_watermark IS
    'Singleton durable cursor for L1 VIP reconciliation; backfill_cutoff is initialized once from database time';

CREATE TABLE IF NOT EXISTS vip_reconcile_jobs (
    id                       BIGSERIAL PRIMARY KEY,
    request_id               VARCHAR(128) NOT NULL,
    actor_user_id            BIGINT NOT NULL,
    actor_snapshot           TEXT NOT NULL DEFAULT '',
    reason                   TEXT NOT NULL,
    status                   VARCHAR(20) NOT NULL DEFAULT 'queued',
    as_of                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cursor_completed_at      TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    cursor_order_id          BIGINT NOT NULL DEFAULT 0,
    scanned                  BIGINT NOT NULL DEFAULT 0,
    eligibility_repaired     BIGINT NOT NULL DEFAULT 0,
    effective_changed        BIGINT NOT NULL DEFAULT 0,
    force_off_unchanged      BIGINT NOT NULL DEFAULT 0,
    already_correct          BIGINT NOT NULL DEFAULT 0,
    deleted                  BIGINT NOT NULL DEFAULT 0,
    invalid_order            BIGINT NOT NULL DEFAULT 0,
    failed                   BIGINT NOT NULL DEFAULT 0,
    attempts                 INTEGER NOT NULL DEFAULT 0,
    last_error               TEXT NOT NULL DEFAULT '',
    started_at               TIMESTAMPTZ,
    finished_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT vip_reconcile_jobs_request_id_check CHECK (BTRIM(request_id) <> ''),
    CONSTRAINT vip_reconcile_jobs_actor_snapshot_check CHECK (BTRIM(actor_snapshot) <> ''),
    CONSTRAINT vip_reconcile_jobs_reason_check CHECK (BTRIM(reason) <> ''),
    CONSTRAINT vip_reconcile_jobs_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    CONSTRAINT vip_reconcile_jobs_cursor_check CHECK (cursor_order_id >= 0),
    CONSTRAINT vip_reconcile_jobs_counts_check
        CHECK (
            scanned >= 0
            AND eligibility_repaired >= 0
            AND effective_changed >= 0
            AND force_off_unchanged >= 0
            AND already_correct >= 0
            AND deleted >= 0
            AND invalid_order >= 0
            AND failed >= 0
            AND attempts >= 0
        )
);

COMMENT ON TABLE vip_reconcile_jobs IS
    'Independent resumable L2 VIP reconciliation jobs; these cursors never advance the L1 watermark';
COMMENT ON COLUMN vip_reconcile_jobs.actor_snapshot IS
    'Immutable administrator display identity captured when the repair job is created';

COMMENT ON COLUMN users.vip_paid_eligible IS
    'Whether the user has ever completed a qualifying real payment; only entitlement writers may update it';
COMMENT ON COLUMN users.vip_manual_override IS
    'NULL=AUTO, true=FORCE_ON, false=FORCE_OFF';
COMMENT ON COLUMN users.is_vip IS
    'Materialized effective VIP state: COALESCE(vip_manual_override, vip_paid_eligible)';
COMMENT ON COLUMN groups.vip_only IS
    'Whether this standard group requires the current effective VIP entitlement';
