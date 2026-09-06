SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE video_billing_reviews (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE RESTRICT,
    action VARCHAR(16) NOT NULL CHECK (action IN ('capture', 'release')),
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    proposed_by BIGINT NOT NULL CHECK (proposed_by > 0),
    decided_by BIGINT CHECK (decided_by > 0),
    task_version BIGINT NOT NULL CHECK (task_version >= 0),
    billing_model VARCHAR(100) NOT NULL,
    actual_units NUMERIC(24,8) NOT NULL CHECK (actual_units >= 0 AND actual_units <= 1000000000),
    actual_cost NUMERIC(20,8) NOT NULL CHECK (actual_cost >= 0 AND actual_cost < 10000000000),
    hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount >= 0 AND hold_amount < 10000000000),
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 4 AND 1024),
    evidence_ref VARCHAR(128) NOT NULL,
    honor_frozen_quote BOOLEAN NOT NULL DEFAULT false,
    requires_second_actor BOOLEAN NOT NULL,
    approval_threshold_usd NUMERIC(20,8) NOT NULL CHECK (approval_threshold_usd >= 0 AND approval_threshold_usd < 10000000000),
    facts JSONB NOT NULL,
    decision_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    CHECK (status = 'pending' OR (decided_by IS NOT NULL AND decided_at IS NOT NULL AND octet_length(decision_reason) BETWEEN 4 AND 1024)),
    CHECK (status <> 'approved' OR NOT requires_second_actor OR decided_by <> proposed_by),
    CHECK (requires_second_actor = (honor_frozen_quote OR actual_cost > hold_amount OR hold_amount >= approval_threshold_usd OR actual_cost >= approval_threshold_usd OR (action = 'capture' AND actual_units = 0 AND hold_amount > 0))),
    CHECK (action <> 'release' OR (actual_units = 0 AND actual_cost = 0 AND NOT honor_frozen_quote))
);

CREATE UNIQUE INDEX uq_video_billing_review_pending ON video_billing_reviews(task_id) WHERE status = 'pending';
CREATE INDEX idx_video_billing_reviews_task ON video_billing_reviews(task_id, id DESC);

CREATE TABLE video_billing_review_actions (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES video_tasks(id) ON DELETE RESTRICT,
    review_id BIGINT NOT NULL REFERENCES video_billing_reviews(id) ON DELETE CASCADE,
    operation_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    action VARCHAR(16) NOT NULL CHECK (action IN ('propose', 'approve', 'reject')),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (task_id, operation_key)
);

ALTER TABLE video_tasks ADD COLUMN billing_review_id BIGINT REFERENCES video_billing_reviews(id) ON DELETE SET NULL;

CREATE FUNCTION video_billing_review_facts(task video_tasks) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT jsonb_build_object(
        'id', task.id, 'user_id', task.user_id, 'api_key_id', task.api_key_id,
        'account_id', task.account_id, 'account_owner_user_id', task.account_owner_user_id,
        'group_id', task.group_id, 'channel_id', task.channel_id,
        'parent_task_id', task.parent_task_id, 'root_task_id', task.root_task_id,
        'provider', task.provider, 'provider_task_id', task.provider_task_id,
        'operation', task.operation, 'generation_state', task.generation_state,
        'upstream_model', task.upstream_model, 'request_hash', task.request_hash,
        'billing_unit', task.billing_unit, 'hold_amount', task.hold_amount, 'currency', task.currency,
        'price_snapshot', task.price_snapshot, 'provider_cost_snapshot', task.provider_cost_snapshot,
        'request_attributes', task.request_attributes, 'usage_snapshot', task.usage_snapshot,
        'response_metadata', task.response_metadata,
        'finished_at', task.finished_at AT TIME ZONE 'UTC'
    );
$$;

CREATE FUNCTION guard_video_billing_review_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW) - ARRAY['status','decided_by','decision_reason','decided_at']) IS DISTINCT FROM
       (to_jsonb(OLD) - ARRAY['status','decided_by','decision_reason','decided_at'])
       OR OLD.status <> 'pending' OR NEW.status NOT IN ('approved','rejected') THEN
        RAISE EXCEPTION 'video billing review is immutable' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = NEW.decided_by AND role = 'admin' AND status = 'active' AND deleted_at IS NULL) THEN
        RAISE EXCEPTION 'video billing review requires an active administrator' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER video_billing_reviews_guard BEFORE UPDATE ON video_billing_reviews
FOR EACH ROW EXECUTE FUNCTION guard_video_billing_review_change();

CREATE FUNCTION guard_video_manual_billing_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review video_billing_reviews;
BEGIN
    IF OLD.billing_state = 'manual_review' AND OLD.generation_state IN ('completed','failed','cancelled','expired')
        AND NEW.billing_state IN ('capture_pending','release_pending') THEN
        IF OLD.operation = 'character_create' AND OLD.generation_state = 'completed'
            AND OLD.last_error_code IN ('resource_persistence_pending','resource_persistence_failed')
            AND NEW.billing_state = 'capture_pending' AND OLD.billing_unit = 'request'
            AND NEW.actual_units = 1 AND NEW.actual_cost = round((OLD.price_snapshot ->> 'unit_price')::NUMERIC * (OLD.price_snapshot ->> 'customer_multiplier')::NUMERIC, 8)
            AND EXISTS (SELECT 1 FROM video_resources r WHERE r.source_task_id = OLD.id AND r.user_id = OLD.user_id
                AND r.account_id = OLD.account_id AND r.provider = OLD.provider AND r.provider_resource_id = OLD.provider_task_id
                AND r.status = 'ready' AND r.deleted_at IS NULL) THEN
            RETURN NEW;
        END IF;
        SELECT * INTO review FROM video_billing_reviews WHERE id = NEW.billing_review_id;
        IF NOT FOUND OR review.task_id <> NEW.id OR review.status <> 'approved'
            OR review.facts IS DISTINCT FROM video_billing_review_facts(OLD)
            OR NEW.billing_state <> (CASE WHEN review.action = 'capture' THEN 'capture_pending' ELSE 'release_pending' END)
            OR NEW.actual_cost IS DISTINCT FROM review.actual_cost
            OR (review.action = 'capture' AND NEW.actual_units IS DISTINCT FROM review.actual_units) THEN
            RAISE EXCEPTION 'video manual billing requires an approved review' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER video_tasks_manual_billing_guard BEFORE UPDATE ON video_tasks
FOR EACH ROW EXECUTE FUNCTION guard_video_manual_billing_transition();

CREATE OR REPLACE FUNCTION guard_video_quota_time_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    task_snapshot JSONB;
    terminal_at TIMESTAMPTZ;
    time_zone TEXT;
    event_at TIMESTAMPTZ;
BEGIN
    IF NEW.command_payload ->> 'settlement_scope' IS DISTINCT FROM 'video_task'
        OR NEW.command_payload ->> 'action' IS DISTINCT FROM 'capture' THEN RETURN NEW; END IF;
    SELECT price_snapshot, finished_at INTO task_snapshot, terminal_at
    FROM video_tasks WHERE id = (NEW.command_payload ->> 'video_task_id')::BIGINT;
    IF NOT COALESCE(task_snapshot ? 'quota_time_contract_version', false) AND NEW.payload_version <> 3 THEN RETURN NEW; END IF;
    IF task_snapshot ->> 'quota_time_contract_version' IS DISTINCT FROM '1' OR NEW.payload_version NOT IN (3,4)
        OR NEW.command_payload #>> '{billing,quota_time,version}' IS DISTINCT FROM '1' OR terminal_at IS NULL THEN
        RAISE EXCEPTION 'video quota time requires a terminal event and outbox v3 or v4' USING ERRCODE = '23514';
    END IF;
    time_zone := task_snapshot ->> 'quota_time_zone';
    event_at := (NEW.command_payload #>> '{billing,occurred_at}')::TIMESTAMPTZ;
    IF time_zone IS NULL OR time_zone = '' OR time_zone = 'Local'
        OR NEW.command_payload #>> '{billing,quota_time,time_zone}' IS DISTINCT FROM time_zone
        OR event_at IS DISTINCT FROM terminal_at
        OR (NEW.command_payload #>> '{billing,quota_time,day_start}')::TIMESTAMPTZ IS DISTINCT FROM
            (date_trunc('day', terminal_at AT TIME ZONE time_zone) AT TIME ZONE time_zone)
        OR (NEW.command_payload #>> '{billing,quota_time,week_start}')::TIMESTAMPTZ IS DISTINCT FROM
            (date_trunc('week', terminal_at AT TIME ZONE time_zone) AT TIME ZONE time_zone) THEN
        RAISE EXCEPTION 'video quota time differs from the frozen terminal event' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION guard_video_billing_review_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE task video_tasks; review video_billing_reviews;
BEGIN
    IF TG_OP = 'UPDATE' AND (OLD.payload_version = 4 OR NEW.payload_version = 4) THEN
        IF OLD.payload_version <> NEW.payload_version OR
            (OLD.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}') IS DISTINCT FROM
            (NEW.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}') THEN
            RAISE EXCEPTION 'video reviewed financial intent is immutable' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' AND EXISTS (SELECT 1 FROM usage_billing_outbox stored
        WHERE stored.request_id = NEW.request_id AND stored.api_key_id = NEW.api_key_id AND stored.payload_version = 4
          AND stored.request_fingerprint = NEW.request_fingerprint) THEN RETURN NEW; END IF;
    IF NEW.command_payload ->> 'settlement_scope' IS DISTINCT FROM 'video_task' THEN RETURN NEW; END IF;
    SELECT * INTO task FROM video_tasks WHERE id = (NEW.command_payload ->> 'video_task_id')::BIGINT;
    IF task.billing_review_id IS NULL AND NEW.payload_version <> 4 THEN RETURN NEW; END IF;
    SELECT * INTO review FROM video_billing_reviews WHERE id = task.billing_review_id;
    IF NOT FOUND OR NEW.payload_version <> 4 OR review.status <> 'approved' OR review.task_id <> task.id
        OR NEW.command_payload ->> 'billing_review_id' IS DISTINCT FROM review.id::TEXT
        OR NEW.command_payload ->> 'action' IS DISTINCT FROM review.action
        OR (NEW.command_payload ->> 'actual_amount')::NUMERIC IS DISTINCT FROM review.actual_cost
        OR review.facts IS DISTINCT FROM video_billing_review_facts(task) THEN
        RAISE EXCEPTION 'video reviewed settlement requires matching outbox v4' USING ERRCODE = '23514';
    END IF;
    IF review.action = 'capture' AND (
        task.actual_units IS DISTINCT FROM review.actual_units OR task.actual_cost IS DISTINCT FROM review.actual_cost
        OR (NEW.command_payload #>> '{billing,user_id}')::BIGINT IS DISTINCT FROM task.user_id
        OR (NEW.command_payload #>> '{billing,api_key_id}')::BIGINT IS DISTINCT FROM task.api_key_id
        OR (NEW.command_payload #>> '{billing,account_id}')::BIGINT IS DISTINCT FROM task.account_id
        OR (NEW.command_payload #>> '{billing,group_id}')::BIGINT IS DISTINCT FROM task.group_id
        OR NEW.command_payload #>> '{billing,model}' IS DISTINCT FROM review.billing_model
        OR NEW.command_payload #>> '{billing,platform}' IS DISTINCT FROM task.provider
        OR NEW.command_payload #>> '{billing,media_type}' IS DISTINCT FROM 'video'
        OR (NEW.command_payload #>> '{billing,actual_cost}')::NUMERIC IS DISTINCT FROM review.actual_cost
        OR (NEW.command_payload #>> '{billing,api_key_quota_cost}')::NUMERIC IS DISTINCT FROM review.actual_cost
        OR (NEW.command_payload #>> '{billing,api_key_rate_limit_cost}')::NUMERIC IS DISTINCT FROM review.actual_cost
        OR (NEW.command_payload #>> '{billing,platform_quota_cost}')::NUMERIC IS DISTINCT FROM review.actual_cost
    ) THEN
        RAISE EXCEPTION 'video reviewed billing allocation differs from approval' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER usage_billing_outbox_video_review_guard BEFORE INSERT OR UPDATE OF command_payload, payload_version ON usage_billing_outbox
FOR EACH ROW EXECUTE FUNCTION guard_video_billing_review_outbox();
