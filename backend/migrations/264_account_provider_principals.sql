SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE account_provider_identity_reviews (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    account_identity_version BIGINT NOT NULL CHECK (account_identity_version > 0),
    platform VARCHAR(50) NOT NULL,
    issuer_hash CHAR(64) NOT NULL CHECK (issuer_hash ~ '^[0-9a-f]{64}$'),
    principal_kind VARCHAR(24) NOT NULL CHECK (principal_kind IN ('account','organization','project','tenant','workspace')),
    principal_hash CHAR(64) NOT NULL CHECK (principal_hash ~ '^[0-9a-f]{64}$'),
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    proposed_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    decided_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 4 AND 1024),
    evidence_ref VARCHAR(128) NOT NULL CHECK (octet_length(evidence_ref) BETWEEN 3 AND 128),
    facts JSONB NOT NULL CHECK (jsonb_typeof(facts)='object' AND octet_length(facts::text)<=8192),
    decision_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    CHECK (
        (status='pending' AND decided_by IS NULL AND decision_reason IS NULL AND decided_at IS NULL)
        OR (status IN ('approved','rejected') AND decided_by IS NOT NULL AND decided_at IS NOT NULL
            AND octet_length(decision_reason) BETWEEN 4 AND 1024)
    ),
    CHECK (status<>'approved' OR decided_by<>proposed_by),
    UNIQUE(id,account_id)
);

CREATE UNIQUE INDEX uq_account_provider_identity_reviews_pending
    ON account_provider_identity_reviews(account_id) WHERE status='pending';
CREATE INDEX idx_account_provider_identity_reviews_account
    ON account_provider_identity_reviews(account_id,id DESC);

CREATE TABLE account_provider_identity_review_actions (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    review_id BIGINT NOT NULL,
    operation_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    actor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action VARCHAR(16) NOT NULL CHECK (action IN ('propose','approve','reject')),
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 4 AND 1024),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(account_id,operation_key),
    FOREIGN KEY (review_id,account_id) REFERENCES account_provider_identity_reviews(id,account_id) ON DELETE CASCADE
);

CREATE TABLE account_provider_identity_bindings (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    account_identity_version BIGINT NOT NULL CHECK (account_identity_version > 0),
    platform VARCHAR(50) NOT NULL,
    issuer_hash CHAR(64) NOT NULL CHECK (issuer_hash ~ '^[0-9a-f]{64}$'),
    principal_kind VARCHAR(24) NOT NULL CHECK (principal_kind IN ('account','organization','project','tenant','workspace')),
    principal_hash CHAR(64) NOT NULL CHECK (principal_hash ~ '^[0-9a-f]{64}$'),
    verification_review_id BIGINT NOT NULL REFERENCES account_provider_identity_reviews(id) ON DELETE CASCADE,
    verified_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoked_at TIMESTAMPTZ,
    revocation_id BIGINT,
    CHECK ((revoked_at IS NULL AND revoked_by IS NULL AND revocation_id IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND revocation_id IS NOT NULL)),
    UNIQUE(account_id,account_identity_version),
    UNIQUE(id,account_id)
);

CREATE INDEX idx_account_provider_identity_bindings_principal
    ON account_provider_identity_bindings(platform,issuer_hash,principal_kind,principal_hash)
    WHERE revoked_at IS NULL;

CREATE TABLE account_provider_identity_revocations (
    id BIGSERIAL PRIMARY KEY,
    triggering_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    platform VARCHAR(50) NOT NULL,
    issuer_hash CHAR(64) NOT NULL CHECK (issuer_hash ~ '^[0-9a-f]{64}$'),
    principal_kind VARCHAR(24) NOT NULL CHECK (principal_kind IN ('account','organization','project','tenant','workspace')),
    principal_hash CHAR(64) NOT NULL CHECK (principal_hash ~ '^[0-9a-f]{64}$'),
    operation_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    actor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 4 AND 1024),
    evidence_ref VARCHAR(128) NOT NULL CHECK (octet_length(evidence_ref) BETWEEN 3 AND 128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(triggering_account_id,operation_key)
);

ALTER TABLE account_provider_identity_bindings
    ADD CONSTRAINT account_provider_identity_bindings_revocation_fkey
    FOREIGN KEY (revocation_id) REFERENCES account_provider_identity_revocations(id) ON DELETE RESTRICT;

ALTER TABLE accounts
    ADD COLUMN provider_principal_binding_id BIGINT,
    ADD CONSTRAINT accounts_provider_principal_binding_fkey
        FOREIGN KEY (provider_principal_binding_id) REFERENCES account_provider_identity_bindings(id)
        ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED;

WITH downgraded AS (
    UPDATE accounts
    SET isolation_state='unverified', isolation_verified_version=0
    WHERE isolation_state='verified'
    RETURNING id
)
INSERT INTO scheduler_outbox(event_type,account_id)
SELECT 'account_changed',id FROM downgraded;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_verified_principal_binding_required
    CHECK (isolation_state<>'verified' OR provider_principal_binding_id IS NOT NULL);

CREATE FUNCTION account_provider_identity_review_facts(account accounts) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT jsonb_build_object(
        'account_id',account.id,'platform',account.platform,'type',account.type,
        'ownership_mode',account.ownership_mode,'owner_user_id',account.owner_user_id,
        'provider_identity_version',account.provider_identity_version,
        'isolation_state',account.isolation_state,
        'provider_principal_binding_id',account.provider_principal_binding_id
    );
$$;

CREATE FUNCTION guard_account_provider_identity_review_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF pg_trigger_depth()>1 THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'account provider identity review is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.proposed_by AND role='admin' AND status='active' AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'account provider identity review requires an active administrator'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_admin_required';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM accounts a
            WHERE a.id=NEW.account_id AND a.deleted_at IS NULL AND a.parent_account_id IS NULL
                AND a.platform=NEW.platform AND a.ownership_mode='user_dedicated'
                AND a.owner_user_id IS NOT NULL AND a.owner_user_id>0
                AND a.provider_identity_version=NEW.account_identity_version
                AND a.isolation_state='unverified' AND a.provider_principal_binding_id IS NULL
                AND NEW.facts=account_provider_identity_review_facts(a)
        ) THEN
            RAISE EXCEPTION 'account provider identity review facts are invalid'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_facts_conflict';
        END IF;
        RETURN NEW;
    END IF;
    IF (to_jsonb(NEW)-ARRAY['status','decided_by','decision_reason','decided_at']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['status','decided_by','decision_reason','decided_at'])
       OR OLD.status<>'pending' OR NEW.status NOT IN ('approved','rejected') THEN
        RAISE EXCEPTION 'account provider identity review is immutable'
            USING ERRCODE='23514', CONSTRAINT='account_provider_identity_review_immutable';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.decided_by AND role='admin' AND status='active' AND deleted_at IS NULL) THEN
        RAISE EXCEPTION 'account provider identity review requires an active administrator'
            USING ERRCODE='23514', CONSTRAINT='account_provider_identity_admin_required';
    END IF;
    IF NEW.status='approved' AND NEW.decided_by=NEW.proposed_by THEN
        RAISE EXCEPTION 'account provider identity review requires an independent administrator'
            USING ERRCODE='23514', CONSTRAINT='account_provider_identity_independent_admin_required';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER account_provider_identity_reviews_guard
BEFORE INSERT OR UPDATE OR DELETE ON account_provider_identity_reviews
FOR EACH ROW EXECUTE FUNCTION guard_account_provider_identity_review_change();

CREATE FUNCTION guard_account_provider_identity_review_action_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' AND pg_trigger_depth()>1 THEN RETURN OLD; END IF;
    IF TG_OP='INSERT' THEN
        IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND role='admin' AND status='active' AND deleted_at IS NULL)
            OR NOT EXISTS (
                SELECT 1 FROM account_provider_identity_reviews r
                WHERE r.id=NEW.review_id AND r.account_id=NEW.account_id AND (
                    (NEW.action='propose' AND r.status='pending' AND r.proposed_by=NEW.actor_id)
                    OR (NEW.action='approve' AND r.status='approved' AND r.decided_by=NEW.actor_id)
                    OR (NEW.action='reject' AND r.status='rejected' AND r.decided_by=NEW.actor_id)
                )
            ) THEN
            RAISE EXCEPTION 'account provider identity review action is not attributable'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_action_conflict';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'account provider identity review action is immutable'
        USING ERRCODE='23514', CONSTRAINT='account_provider_identity_action_immutable';
END;
$$;
CREATE TRIGGER account_provider_identity_review_actions_guard
BEFORE INSERT OR UPDATE OR DELETE ON account_provider_identity_review_actions
FOR EACH ROW EXECUTE FUNCTION guard_account_provider_identity_review_action_change();

CREATE FUNCTION account_identity_credentials_overlap(left_account accounts, right_account accounts) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT
        (NULLIF(BTRIM(left_account.credentials ->> 'api_key'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'api_key') = BTRIM(left_account.credentials ->> 'api_key'))
        OR (NULLIF(BTRIM(left_account.credentials ->> 'access_token'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'access_token') = BTRIM(left_account.credentials ->> 'access_token'))
        OR (NULLIF(BTRIM(left_account.credentials ->> 'refresh_token'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'refresh_token') = BTRIM(left_account.credentials ->> 'refresh_token'))
        OR (right_account.platform=left_account.platform AND NULLIF(BTRIM(left_account.credentials ->> 'project_id'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'project_id') = BTRIM(left_account.credentials ->> 'project_id'))
        OR (right_account.platform=left_account.platform AND NULLIF(BTRIM(left_account.credentials ->> 'chatgpt_account_id'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'chatgpt_account_id') = BTRIM(left_account.credentials ->> 'chatgpt_account_id'))
        OR (right_account.platform=left_account.platform AND NULLIF(BTRIM(left_account.credentials ->> 'organization_id'), '') IS NOT NULL
            AND BTRIM(right_account.credentials ->> 'organization_id') = BTRIM(left_account.credentials ->> 'organization_id'));
$$;

CREATE FUNCTION guard_account_provider_identity_binding_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF pg_trigger_depth()>1 THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'account provider identity binding is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.verified_by AND role='admin' AND status='active' AND deleted_at IS NULL)
            OR NOT EXISTS (
                SELECT 1 FROM account_provider_identity_reviews r JOIN accounts a ON a.id=r.account_id
                WHERE r.id=NEW.verification_review_id AND r.account_id=NEW.account_id
                    AND r.status='approved' AND r.decided_by=NEW.verified_by
                    AND r.account_identity_version=NEW.account_identity_version
                    AND r.platform=NEW.platform AND r.issuer_hash=NEW.issuer_hash
                    AND r.principal_kind=NEW.principal_kind AND r.principal_hash=NEW.principal_hash
                    AND a.deleted_at IS NULL AND a.parent_account_id IS NULL
                    AND a.ownership_mode='user_dedicated' AND a.owner_user_id IS NOT NULL
                    AND a.provider_identity_version=NEW.account_identity_version
                    AND a.isolation_state='unverified' AND a.provider_principal_binding_id IS NULL
                    AND r.facts=account_provider_identity_review_facts(a)
            ) THEN
            RAISE EXCEPTION 'account provider identity binding lacks an approved current review'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_binding_review_conflict';
        END IF;
        IF EXISTS (
            SELECT 1 FROM account_provider_identity_bindings b
            JOIN accounts bound ON bound.id=b.account_id
            JOIN accounts target ON target.id=NEW.account_id
            WHERE b.platform=NEW.platform AND b.issuer_hash=NEW.issuer_hash
                AND b.principal_kind=NEW.principal_kind AND b.principal_hash=NEW.principal_hash
                AND b.revoked_at IS NULL AND bound.deleted_at IS NULL
                AND bound.provider_identity_version=b.account_identity_version
                AND (bound.ownership_mode<>'user_dedicated' OR bound.owner_user_id IS DISTINCT FROM target.owner_user_id
                    OR bound.isolation_state='revoked')
        ) OR EXISTS (
            SELECT 1 FROM accounts target JOIN accounts alias ON alias.id<>target.id
            WHERE target.id=NEW.account_id AND alias.deleted_at IS NULL AND alias.parent_account_id IS NULL
                AND account_identity_credentials_overlap(target,alias)
                AND (alias.ownership_mode<>'user_dedicated' OR alias.owner_user_id IS DISTINCT FROM target.owner_user_id
                    OR alias.isolation_state='revoked')
        ) THEN
            RAISE EXCEPTION 'account provider identity has a conflicting alias'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_alias_conflict';
        END IF;
        RETURN NEW;
    END IF;
    IF (to_jsonb(NEW)-ARRAY['revoked_by','revoked_at','revocation_id']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['revoked_by','revoked_at','revocation_id'])
       OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NOT EXISTS (
            SELECT 1 FROM account_provider_identity_revocations r
            WHERE r.id=NEW.revocation_id AND r.actor_id=NEW.revoked_by
                AND r.platform=OLD.platform AND r.issuer_hash=OLD.issuer_hash
                AND r.principal_kind=OLD.principal_kind AND r.principal_hash=OLD.principal_hash
       ) THEN
        RAISE EXCEPTION 'account provider identity binding is immutable'
            USING ERRCODE='23514', CONSTRAINT='account_provider_identity_binding_immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER account_provider_identity_bindings_guard
BEFORE INSERT OR UPDATE OR DELETE ON account_provider_identity_bindings
FOR EACH ROW EXECUTE FUNCTION guard_account_provider_identity_binding_change();

CREATE FUNCTION guard_account_provider_identity_revocation_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND role='admin' AND status='active' AND deleted_at IS NULL)
            OR NOT EXISTS (
                SELECT 1 FROM accounts a JOIN account_provider_identity_bindings b ON b.id=a.provider_principal_binding_id
                WHERE a.id=NEW.triggering_account_id AND a.deleted_at IS NULL
                    AND a.provider_identity_version=b.account_identity_version AND b.revoked_at IS NULL
                    AND b.platform=NEW.platform AND b.issuer_hash=NEW.issuer_hash
                    AND b.principal_kind=NEW.principal_kind AND b.principal_hash=NEW.principal_hash
            ) THEN
            RAISE EXCEPTION 'account provider identity revocation is not attributable'
                USING ERRCODE='23514', CONSTRAINT='account_provider_identity_revocation_conflict';
        END IF;
        RETURN NEW;
    END IF;
	IF TG_OP='UPDATE' AND pg_trigger_depth()>1
		AND OLD.triggering_account_id IS NOT NULL AND NEW.triggering_account_id IS NULL
		AND (to_jsonb(NEW)-'triggering_account_id') IS NOT DISTINCT FROM (to_jsonb(OLD)-'triggering_account_id') THEN
		RETURN NEW;
	END IF;
	IF TG_OP='DELETE' AND pg_trigger_depth()>1 THEN RETURN OLD; END IF;
    RAISE EXCEPTION 'account provider identity revocation is immutable'
        USING ERRCODE='23514', CONSTRAINT='account_provider_identity_revocation_immutable';
END;
$$;
CREATE TRIGGER account_provider_identity_revocations_guard
BEFORE INSERT OR UPDATE OR DELETE ON account_provider_identity_revocations
FOR EACH ROW EXECUTE FUNCTION guard_account_provider_identity_revocation_change();

CREATE FUNCTION assert_revoked_provider_binding_not_verified() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM accounts WHERE provider_principal_binding_id=NEW.id AND isolation_state='verified') THEN
        RAISE EXCEPTION 'revoked provider identity binding cannot remain verified' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER account_provider_identity_binding_revocation_account_guard
AFTER UPDATE OF revoked_at ON account_provider_identity_bindings DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW WHEN (OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL)
EXECUTE FUNCTION assert_revoked_provider_binding_not_verified();

CREATE OR REPLACE FUNCTION enforce_account_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.video_owner_user_id IS NOT NULL AND NEW.owner_user_id IS NULL THEN
            NEW.owner_user_id := NEW.video_owner_user_id;
            NEW.ownership_mode := 'user_dedicated';
        END IF;
        IF NEW.ownership_mode = 'user_dedicated' AND NEW.video_owner_user_id IS NULL THEN
            NEW.video_owner_user_id := NEW.owner_user_id;
        END IF;
        NEW.provider_identity_version := 1;
        NEW.isolation_state := 'unverified';
        NEW.isolation_verified_version := 0;
        NEW.provider_principal_binding_id := NULL;
    ELSE
        IF NEW.ownership_mode IS DISTINCT FROM OLD.ownership_mode
            OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
            OR NEW.video_owner_user_id IS DISTINCT FROM OLD.video_owner_user_id THEN
            RAISE EXCEPTION 'account ownership is immutable; use a new isolated upstream identity'
                USING ERRCODE = '23514';
        END IF;
        IF OLD.isolation_state='revoked' AND NEW.isolation_state<>'revoked' THEN
            RAISE EXCEPTION 'revoked upstream isolation cannot be restored in place'
                USING ERRCODE = '23514';
        END IF;
        NEW.provider_identity_version := OLD.provider_identity_version;
        IF NEW.platform IS DISTINCT FROM OLD.platform OR NEW.type IS DISTINCT FROM OLD.type
            OR (NEW.credentials - ARRAY['model_mapping', 'openai_capabilities'])
                IS DISTINCT FROM (OLD.credentials - ARRAY['model_mapping', 'openai_capabilities'])
            OR NEW.parent_account_id IS DISTINCT FROM OLD.parent_account_id THEN
            IF OLD.platform = 'openai' AND OLD.type = 'apikey' AND (
                EXISTS (SELECT 1 FROM video_tasks WHERE account_id = OLD.id AND (
                    billing_state NOT IN ('none', 'captured', 'released')
                    OR generation_state IN ('preparing', 'held', 'submitting', 'submission_unknown', 'queued', 'in_progress')
                    OR (generation_state = 'completed' AND delete_state <> 'deleted')))
                OR EXISTS (SELECT 1 FROM video_resources WHERE account_id = OLD.id
                    AND status IN ('creating', 'ready') AND deleted_at IS NULL)
            ) THEN
                RAISE EXCEPTION 'upstream identity is retained by video tasks or resources'
                    USING ERRCODE = '23514', CONSTRAINT = 'accounts_video_identity_in_use';
            END IF;
            NEW.provider_identity_version := OLD.provider_identity_version + 1;
            NEW.isolation_state := CASE WHEN OLD.isolation_state = 'revoked' THEN 'revoked' ELSE 'unverified' END;
            NEW.isolation_verified_version := 0;
            NEW.provider_principal_binding_id := NULL;
        ELSE
            IF NEW.provider_principal_binding_id IS DISTINCT FROM OLD.provider_principal_binding_id
				AND NOT (OLD.isolation_state='unverified' AND NEW.isolation_state='verified')
				AND NOT (pg_trigger_depth()>1 AND NEW.provider_principal_binding_id IS NULL) THEN
                RAISE EXCEPTION 'provider principal binding requires the trusted verification workflow'
                    USING ERRCODE = '23514';
            END IF;
            IF NEW.isolation_state='verified' THEN
                IF NEW.ownership_mode<>'user_dedicated' OR NEW.provider_principal_binding_id IS NULL OR NOT EXISTS (
                    SELECT 1 FROM account_provider_identity_bindings b
                    WHERE b.id=NEW.provider_principal_binding_id AND b.account_id=NEW.id
                        AND b.account_identity_version=NEW.provider_identity_version
                        AND b.platform=NEW.platform AND b.revoked_at IS NULL
                ) THEN
                    RAISE EXCEPTION 'upstream isolation verification requires an approved provider principal binding'
                        USING ERRCODE = '23514';
                END IF;
                NEW.isolation_verified_version := NEW.provider_identity_version;
            ELSIF NEW.isolation_state='unverified' THEN
                NEW.isolation_verified_version := 0;
                NEW.provider_principal_binding_id := NULL;
            ELSIF NEW.isolation_state='revoked' THEN
                NEW.isolation_verified_version := 0;
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION account_user_can_schedule(selected_account_id BIGINT, requesting_user_id BIGINT)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM accounts selected
        JOIN accounts credential ON credential.id = COALESCE(selected.parent_account_id, selected.id)
        WHERE selected.id = selected_account_id AND selected.deleted_at IS NULL
            AND credential.deleted_at IS NULL AND credential.parent_account_id IS NULL
            AND (selected.parent_account_id IS NULL OR (
                selected.platform = 'openai' AND selected.type = 'oauth'
                AND credential.platform = 'openai' AND credential.type = 'oauth'))
            AND selected.isolation_state <> 'revoked' AND credential.isolation_state <> 'revoked'
            AND (selected.ownership_mode = 'shared' OR selected.owner_user_id = requesting_user_id)
            AND (credential.ownership_mode = 'shared' OR credential.owner_user_id = requesting_user_id)
            AND (selected.isolation_state<>'verified' OR EXISTS (
                SELECT 1 FROM account_provider_identity_bindings b
                WHERE b.id=selected.provider_principal_binding_id AND b.account_id=selected.id
                    AND b.account_identity_version=selected.provider_identity_version AND b.revoked_at IS NULL))
            AND (credential.isolation_state<>'verified' OR EXISTS (
                SELECT 1 FROM account_provider_identity_bindings b
                WHERE b.id=credential.provider_principal_binding_id AND b.account_id=credential.id
                    AND b.account_identity_version=credential.provider_identity_version AND b.revoked_at IS NULL))
            AND NOT EXISTS (
                SELECT 1 FROM accounts alias
                WHERE alias.id <> credential.id AND alias.parent_account_id IS NULL AND alias.deleted_at IS NULL
                    AND account_identity_credentials_overlap(credential,alias)
                    AND (
                        (credential.ownership_mode='user_dedicated' AND
                            (alias.ownership_mode='shared' OR alias.owner_user_id IS DISTINCT FROM requesting_user_id OR alias.isolation_state='revoked'))
                        OR (credential.ownership_mode='shared' AND alias.ownership_mode='user_dedicated')
                    )
            )
            AND NOT EXISTS (
                SELECT 1 FROM account_provider_identity_bindings own_binding
                JOIN account_provider_identity_bindings alias_binding
                    ON alias_binding.platform=own_binding.platform AND alias_binding.issuer_hash=own_binding.issuer_hash
                    AND alias_binding.principal_kind=own_binding.principal_kind AND alias_binding.principal_hash=own_binding.principal_hash
                    AND alias_binding.id<>own_binding.id AND alias_binding.revoked_at IS NULL
                JOIN accounts alias ON alias.id=alias_binding.account_id
                WHERE own_binding.id=credential.provider_principal_binding_id AND own_binding.revoked_at IS NULL
                    AND alias.deleted_at IS NULL AND alias.provider_identity_version=alias_binding.account_identity_version
                    AND (alias.ownership_mode<>'user_dedicated' OR alias.owner_user_id IS DISTINCT FROM requesting_user_id
                        OR alias.isolation_state='revoked')
            )
    );
$$;
