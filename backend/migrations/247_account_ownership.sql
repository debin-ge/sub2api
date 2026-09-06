SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE accounts
    ADD COLUMN ownership_mode VARCHAR(32) NOT NULL DEFAULT 'shared',
    ADD COLUMN owner_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN isolation_state VARCHAR(32) NOT NULL DEFAULT 'unverified',
    ADD COLUMN provider_identity_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN isolation_verified_version BIGINT NOT NULL DEFAULT 0;

UPDATE accounts SET ownership_mode = 'user_dedicated', owner_user_id = video_owner_user_id
WHERE video_owner_user_id IS NOT NULL;

CREATE INDEX idx_accounts_dedicated_authorization ON accounts (owner_user_id, id)
WHERE ownership_mode = 'user_dedicated' AND parent_account_id IS NULL;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_ownership_valid CHECK (
        (ownership_mode = 'shared' AND owner_user_id IS NULL AND video_owner_user_id IS NULL)
        OR (ownership_mode = 'user_dedicated' AND owner_user_id IS NOT NULL AND owner_user_id > 0
            AND video_owner_user_id IS NOT DISTINCT FROM owner_user_id)
    ),
    ADD CONSTRAINT accounts_isolation_valid CHECK (
        isolation_state IN ('unverified', 'verified', 'revoked')
        AND provider_identity_version > 0
        AND isolation_verified_version >= 0
        AND (isolation_state <> 'verified' OR
            (ownership_mode = 'user_dedicated' AND isolation_verified_version = provider_identity_version))
    );

CREATE FUNCTION enforce_account_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
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
    ELSE
        IF NEW.ownership_mode IS DISTINCT FROM OLD.ownership_mode
            OR NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
            OR NEW.video_owner_user_id IS DISTINCT FROM OLD.video_owner_user_id THEN
            RAISE EXCEPTION 'account ownership is immutable; use a new isolated upstream identity'
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
        ELSIF NEW.isolation_state = 'verified' AND (
            OLD.isolation_state <> 'verified'
            OR NEW.isolation_verified_version IS DISTINCT FROM OLD.isolation_verified_version) THEN
            RAISE EXCEPTION 'upstream isolation verification requires a trusted verification workflow'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER accounts_ownership_guard BEFORE INSERT OR UPDATE ON accounts
FOR EACH ROW EXECUTE FUNCTION enforce_account_ownership();

CREATE FUNCTION account_user_can_schedule(selected_account_id BIGINT, requesting_user_id BIGINT)
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
            AND NOT EXISTS (
                SELECT 1 FROM accounts alias
                WHERE alias.id <> credential.id AND alias.parent_account_id IS NULL
                    AND alias.ownership_mode = 'user_dedicated'
                    AND (alias.owner_user_id IS DISTINCT FROM requesting_user_id OR alias.isolation_state = 'revoked')
                    AND (
                        (NULLIF(BTRIM(credential.credentials ->> 'api_key'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'api_key') = BTRIM(credential.credentials ->> 'api_key'))
                        OR (NULLIF(BTRIM(credential.credentials ->> 'access_token'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'access_token') = BTRIM(credential.credentials ->> 'access_token'))
                        OR (NULLIF(BTRIM(credential.credentials ->> 'refresh_token'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'refresh_token') = BTRIM(credential.credentials ->> 'refresh_token'))
                        OR (alias.platform = credential.platform AND NULLIF(BTRIM(credential.credentials ->> 'project_id'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'project_id') = BTRIM(credential.credentials ->> 'project_id'))
                        OR (alias.platform = credential.platform AND NULLIF(BTRIM(credential.credentials ->> 'chatgpt_account_id'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'chatgpt_account_id') = BTRIM(credential.credentials ->> 'chatgpt_account_id'))
                        OR (alias.platform = credential.platform AND NULLIF(BTRIM(credential.credentials ->> 'organization_id'), '') IS NOT NULL
                            AND BTRIM(alias.credentials ->> 'organization_id') = BTRIM(credential.credentials ->> 'organization_id'))
                    )
            )
    );
$$;
