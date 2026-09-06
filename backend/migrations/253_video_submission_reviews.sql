SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE video_submission_reviews (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE RESTRICT,
    action VARCHAR(16) NOT NULL CHECK (action IN ('created','not_created')),
    provider_task_id VARCHAR(255),
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    proposed_by BIGINT NOT NULL CHECK (proposed_by > 0),
    decided_by BIGINT CHECK (decided_by > 0),
    task_version BIGINT NOT NULL CHECK (task_version >= 0),
    account_identity_version BIGINT NOT NULL CHECK (account_identity_version > 0),
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 4 AND 1024),
    evidence_ref VARCHAR(128) NOT NULL,
    facts JSONB NOT NULL,
    provider_observation JSONB,
    decision_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    CHECK ((action = 'created' AND provider_task_id IS NOT NULL AND length(provider_task_id) > 0)
        OR (action = 'not_created' AND provider_task_id IS NULL)),
    CHECK (provider_task_id IS NULL OR (provider_task_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'
        AND provider_task_id !~* '(^|[^a-z0-9])sk-[a-z0-9_-]{8,}')),
    CHECK (status = 'pending' OR (decided_by IS NOT NULL AND decided_at IS NOT NULL AND octet_length(decision_reason) BETWEEN 4 AND 1024)),
    CHECK (status <> 'approved' OR decided_by <> proposed_by),
    CHECK (status <> 'approved' OR action <> 'created' OR provider_observation IS NOT NULL)
);
CREATE UNIQUE INDEX uq_video_submission_review_pending ON video_submission_reviews(task_id) WHERE status = 'pending';
CREATE INDEX idx_video_submission_reviews_task ON video_submission_reviews(task_id,id DESC);
CREATE TABLE video_submission_review_actions (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE RESTRICT,
    review_id BIGINT NOT NULL REFERENCES video_submission_reviews(id) ON DELETE CASCADE,
    operation_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    action VARCHAR(16) NOT NULL CHECK (action IN ('propose','approve','reject')),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(task_id,operation_key)
);
ALTER TABLE video_tasks ADD COLUMN submission_review_id BIGINT REFERENCES video_submission_reviews(id) ON DELETE SET NULL;
ALTER TABLE video_billing_reviews ADD COLUMN submission_review_id BIGINT REFERENCES video_submission_reviews(id) ON DELETE RESTRICT;

CREATE FUNCTION video_submission_review_facts(task video_tasks) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT video_billing_review_facts(task) || jsonb_build_object('input_manifest',task.input_manifest,
        'submission_unknown_at',task.submission_unknown_at AT TIME ZONE 'UTC', 'submitted_at',task.submitted_at AT TIME ZONE 'UTC');
$$;
CREATE FUNCTION guard_video_submission_review_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW) - ARRAY['status','decided_by','decision_reason','decided_at','provider_observation']) IS DISTINCT FROM
       (to_jsonb(OLD) - ARRAY['status','decided_by','decision_reason','decided_at','provider_observation'])
       OR OLD.status <> 'pending' OR NEW.status NOT IN ('approved','rejected') THEN
        RAISE EXCEPTION 'video submission review is immutable' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.decided_by AND role='admin' AND status='active' AND deleted_at IS NULL) THEN
        RAISE EXCEPTION 'video submission review requires an active administrator' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER video_submission_reviews_guard BEFORE UPDATE ON video_submission_reviews
FOR EACH ROW EXECUTE FUNCTION guard_video_submission_review_change();

CREATE FUNCTION guard_video_unknown_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review video_submission_reviews;
BEGIN
    IF OLD.generation_state='submission_unknown' AND OLD.provider_task_id IS NOT NULL AND
        (NEW.provider_task_id IS DISTINCT FROM OLD.provider_task_id OR NEW.last_error_code='confirmed_not_created') THEN
        RAISE EXCEPTION 'known video submission identity cannot be replaced or declared absent' USING ERRCODE = '23514';
    END IF;
    IF OLD.generation_state='submission_unknown' AND OLD.provider_task_id IS NULL AND
        (NEW.generation_state <> OLD.generation_state OR NEW.provider_task_id IS NOT NULL OR NEW.billing_state NOT IN ('held','manual_review')) THEN
        SELECT * INTO review FROM video_submission_reviews WHERE id=NEW.submission_review_id;
        IF NOT FOUND OR review.task_id <> OLD.id OR review.status <> 'approved'
            OR review.facts IS DISTINCT FROM video_submission_review_facts(OLD)
            OR (review.action='created' AND (NEW.provider_task_id IS DISTINCT FROM review.provider_task_id OR NEW.generation_state='submission_unknown'))
            OR (review.action='not_created' AND (NEW.provider_task_id IS NOT NULL OR NEW.generation_state <> 'failed' OR NEW.billing_state NOT IN ('manual_review','release_pending'))) THEN
            RAISE EXCEPTION 'unknown video resolution requires an approved submission review' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER video_tasks_unknown_resolution_guard BEFORE UPDATE ON video_tasks
FOR EACH ROW EXECUTE FUNCTION guard_video_unknown_resolution();

CREATE FUNCTION guard_video_unknown_financial_intent() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE task video_tasks;
BEGIN
    IF NEW.command_payload->>'settlement_scope' IS DISTINCT FROM 'video_task' THEN RETURN NEW; END IF;
    IF EXISTS (SELECT 1 FROM usage_billing_outbox stored WHERE stored.request_id=NEW.request_id AND stored.api_key_id=NEW.api_key_id
        AND stored.request_fingerprint=NEW.request_fingerprint AND stored.payload_version=NEW.payload_version
        AND (stored.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}') =
            (NEW.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}')) THEN RETURN NEW; END IF;
    SELECT * INTO task FROM video_tasks WHERE id=(NEW.command_payload->>'video_task_id')::BIGINT FOR UPDATE;
    IF task.generation_state='submission_unknown' THEN
        RAISE EXCEPTION 'unresolved video submission cannot create a financial intent' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER usage_billing_outbox_video_submission_guard BEFORE INSERT ON usage_billing_outbox
FOR EACH ROW EXECUTE FUNCTION guard_video_unknown_financial_intent();
