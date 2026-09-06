SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE video_create_intents (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    endpoint VARCHAR(32) NOT NULL CHECK (endpoint IN ('videos','video_edits','video_extensions','video_characters')),
    key_hash CHAR(64) NOT NULL CHECK (key_hash ~ '^[0-9a-f]{64}$'),
    request_hash VARCHAR(128) NOT NULL,
    request_contract VARCHAR(32) NOT NULL CHECK (request_contract IN ('canonical_json_v1','canonical_multipart_v1','native_task_v1')),
    state VARCHAR(24) NOT NULL CHECK (state IN ('prepared','native_bound','untracked')),
    target_platform VARCHAR(32),
    native_task_id BIGINT UNIQUE REFERENCES video_tasks(id) ON DELETE RESTRICT,
    account_id BIGINT CHECK (account_id > 0),
    lease_owner VARCHAR(128),
    lease_epoch BIGINT NOT NULL DEFAULT 1 CHECK (lease_epoch > 0),
    lease_expires_at TIMESTAMPTZ,
    last_error_code VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id,endpoint,key_hash),
    CHECK (request_contract = 'native_task_v1' OR request_hash ~ '^[0-9a-f]{64}$'),
    CHECK ((state='native_bound' AND native_task_id IS NOT NULL AND target_platform IS NOT NULL) OR (state<>'native_bound' AND native_task_id IS NULL)),
    CHECK (state='prepared' OR lease_expires_at IS NULL)
);
CREATE INDEX idx_video_create_intents_pending ON video_create_intents(state,created_at) WHERE state IN ('prepared','untracked');

CREATE FUNCTION guard_video_create_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state='native_bound' AND (TG_OP='INSERT' OR NEW.native_task_id IS DISTINCT FROM OLD.native_task_id) AND NOT EXISTS (
        SELECT 1 FROM video_tasks WHERE id=NEW.native_task_id AND user_id=NEW.user_id AND endpoint=NEW.endpoint AND provider=NEW.target_platform
    ) THEN
        RAISE EXCEPTION 'native video creation binding differs from its task' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' THEN RETURN NEW; END IF;
    IF ROW(NEW.user_id,NEW.endpoint,NEW.key_hash,NEW.request_hash,NEW.request_contract,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.user_id,OLD.endpoint,OLD.key_hash,OLD.request_hash,OLD.request_contract,OLD.created_at)
        OR (OLD.state<>'prepared' AND NEW.api_key_id IS NOT NULL AND NEW.api_key_id IS DISTINCT FROM OLD.api_key_id) THEN
        RAISE EXCEPTION 'video creation identity is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.state='native_bound' AND
        (to_jsonb(NEW)-ARRAY['updated_at','api_key_id']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['updated_at','api_key_id']) THEN
        RAISE EXCEPTION 'video creation outcome is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.state='prepared' AND NEW.state NOT IN ('prepared','native_bound','untracked')
        OR OLD.state='untracked' AND NEW.state<>'untracked' THEN
        RAISE EXCEPTION 'video creation intent cannot be reopened' USING ERRCODE='23514';
    END IF;
    IF OLD.state<>'prepared' AND ROW(NEW.lease_owner,NEW.lease_epoch,NEW.target_platform,NEW.account_id)
        IS DISTINCT FROM ROW(OLD.lease_owner,OLD.lease_epoch,OLD.target_platform,OLD.account_id) THEN
        RAISE EXCEPTION 'video creation execution identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER video_create_intents_guard BEFORE INSERT OR UPDATE ON video_create_intents
FOR EACH ROW EXECUTE FUNCTION guard_video_create_intent();

CREATE FUNCTION guard_user_video_create_intents() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM video_create_intents WHERE user_id=OLD.id AND state='untracked') THEN
        RAISE EXCEPTION 'user is retained by unresolved video creation intents'
            USING ERRCODE='23514', CONSTRAINT='users_video_create_intent_in_use';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER users_video_create_intents_delete_guard BEFORE DELETE ON users
FOR EACH ROW EXECUTE FUNCTION guard_user_video_create_intents();
CREATE TRIGGER users_video_create_intents_soft_delete_guard BEFORE UPDATE OF deleted_at ON users
FOR EACH ROW WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION guard_user_video_create_intents();
