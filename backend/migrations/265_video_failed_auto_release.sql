SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE OR REPLACE FUNCTION guard_video_manual_billing_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review video_billing_reviews;
BEGIN
    IF OLD.billing_state = 'manual_review' AND OLD.generation_state IN ('completed','failed','cancelled','expired')
        AND NEW.billing_state IN ('capture_pending','release_pending') THEN
        IF OLD.generation_state = 'failed' AND NEW.billing_state = 'release_pending'
            AND COALESCE(NEW.actual_units, 0) = 0 AND COALESCE(NEW.actual_cost, 0) = 0 THEN
            RETURN NEW;
        END IF;
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

CREATE OR REPLACE FUNCTION guard_video_execution_write() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE review video_billing_reviews;
BEGIN
    IF OLD.request_attributes ?| ARRAY['execution_spec_version','execution_spec_hash']
        OR NEW.request_attributes ?| ARRAY['execution_spec_version','execution_spec_hash']
        OR OLD.request_attributes #>> '{execution_spec,version}' = '2'
        OR NEW.request_attributes #>> '{execution_spec,version}' = '2' THEN
        IF ROW(NEW.request_attributes -> 'execution_spec', NEW.request_attributes -> 'execution_spec_version',
            NEW.request_attributes -> 'execution_spec_hash', NEW.price_snapshot, NEW.provider_cost_snapshot,
            NEW.provider, NEW.operation, NEW.upstream_model, NEW.billing_unit, NEW.currency, NEW.hold_amount)
            IS DISTINCT FROM ROW(OLD.request_attributes -> 'execution_spec', OLD.request_attributes -> 'execution_spec_version',
            OLD.request_attributes -> 'execution_spec_hash', OLD.price_snapshot, OLD.provider_cost_snapshot,
            OLD.provider, OLD.operation, OLD.upstream_model, OLD.billing_unit, OLD.currency, OLD.hold_amount) THEN
            RAISE EXCEPTION 'video execution and pricing snapshots are immutable' USING ERRCODE = '23514';
        END IF;
        IF (OLD.settled_at IS NULL OR OLD.billing_state NOT IN ('captured','released')) AND ROW(NEW.user_id, NEW.api_key_id, NEW.account_id, NEW.group_id, NEW.channel_id,
            NEW.parent_task_id, NEW.root_task_id, NEW.request_hash)
            IS DISTINCT FROM ROW(OLD.user_id, OLD.api_key_id, OLD.account_id, OLD.group_id, OLD.channel_id,
            OLD.parent_task_id, OLD.root_task_id, OLD.request_hash) THEN
            RAISE EXCEPTION 'unsettled video execution identity is immutable' USING ERRCODE = '23514';
        END IF;
    END IF;
    IF video_execution_has_conflict(OLD.response_metadata) AND NOT video_execution_has_conflict(NEW.response_metadata) THEN
        NEW.response_metadata := COALESCE(NEW.response_metadata, '{}'::JSONB) || '{"execution_spec_conflict":1}'::JSONB;
    END IF;
    IF video_execution_has_conflict(NEW.response_metadata)
        AND NEW.billing_state IN ('capture_pending','release_pending')
        AND NEW.billing_state IS DISTINCT FROM OLD.billing_state
        AND NOT (NEW.generation_state = 'failed' AND NEW.billing_state = 'release_pending'
            AND COALESCE(NEW.actual_units, 0) = 0 AND COALESCE(NEW.actual_cost, 0) = 0) THEN
        SELECT * INTO review FROM video_billing_reviews WHERE id = NEW.billing_review_id;
        IF NOT FOUND OR review.status <> 'approved' OR review.task_id <> NEW.id
            OR review.facts IS DISTINCT FROM video_billing_review_facts(NEW)
            OR NEW.billing_state <> (CASE WHEN review.action = 'capture' THEN 'capture_pending' ELSE 'release_pending' END)
            OR NEW.actual_cost IS DISTINCT FROM review.actual_cost
            OR (review.action = 'capture' AND (NOT review.honor_frozen_quote OR NEW.actual_units IS DISTINCT FROM review.actual_units)) THEN
            RAISE EXCEPTION 'video execution conflict requires an approved review' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION guard_video_execution_outbox() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE task video_tasks; review video_billing_reviews;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.command_payload ->> 'settlement_scope' = 'video_task' THEN
        IF ROW(NEW.request_id, NEW.api_key_id, NEW.request_fingerprint, NEW.payload_version, NEW.usage_log_payload,
            NEW.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}')
            IS DISTINCT FROM ROW(OLD.request_id, OLD.api_key_id, OLD.request_fingerprint, OLD.payload_version, OLD.usage_log_payload,
            OLD.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}') THEN
            RAISE EXCEPTION 'video execution financial intent is immutable' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.command_payload ->> 'settlement_scope' IS DISTINCT FROM 'video_task' THEN RETURN NEW; END IF;
    IF TG_OP = 'INSERT' AND EXISTS (SELECT 1 FROM usage_billing_outbox stored
        WHERE stored.request_id = NEW.request_id AND stored.api_key_id = NEW.api_key_id
            AND stored.request_fingerprint = NEW.request_fingerprint AND stored.payload_version = NEW.payload_version
            AND stored.usage_log_payload IS NOT DISTINCT FROM NEW.usage_log_payload
            AND (stored.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}') =
                (NEW.command_payload #- '{billing,platform_quota_snapshot}' #- '{billing,platform_quota_snapshot_needed}')) THEN
        RETURN NEW;
    END IF;
    SELECT * INTO task FROM video_tasks WHERE id = (NEW.command_payload ->> 'video_task_id')::BIGINT FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'video execution financial intent requires its task' USING ERRCODE = '23514';
    END IF;
    IF NOT video_execution_has_conflict(task.response_metadata) THEN RETURN NEW; END IF;
    IF task.generation_state = 'failed' AND NEW.command_payload ->> 'action' = 'release'
        AND COALESCE((NEW.command_payload ->> 'actual_amount')::NUMERIC, 0) = 0 THEN
        RETURN NEW;
    END IF;
    SELECT * INTO review FROM video_billing_reviews WHERE id = task.billing_review_id;
    IF NOT FOUND OR review.status <> 'approved' OR review.task_id <> task.id
        OR review.facts IS DISTINCT FROM video_billing_review_facts(task)
        OR NEW.payload_version <> 4 OR NEW.command_payload ->> 'billing_review_id' IS DISTINCT FROM review.id::TEXT
        OR NEW.command_payload ->> 'action' IS DISTINCT FROM review.action
        OR (review.action = 'capture' AND NOT review.honor_frozen_quote) THEN
        RAISE EXCEPTION 'video execution conflict requires a reviewed financial intent' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

WITH released AS (
    UPDATE video_tasks
    SET billing_state = 'release_pending', actual_units = 0, actual_cost = 0,
        billing_review_id = NULL, next_action_at = clock_timestamp(),
        version = version + 1, updated_at = NOW()
    WHERE generation_state = 'failed' AND billing_state = 'manual_review'
    RETURNING id, provider, account_id, provider_task_id
)
INSERT INTO video_task_events (
    task_id, event_type, provider, account_id, provider_task_id,
    from_generation_state, to_generation_state, from_billing_state, to_billing_state,
    payload, event_hash
)
SELECT id, 'failed_auto_release_migrated', provider, account_id, provider_task_id,
    'failed', 'failed', 'manual_review', 'release_pending',
    '{"reason":"confirmed upstream failure requires no billing review"}'::JSONB,
    'video_failed_auto_release:265:' || id::TEXT
FROM released
ON CONFLICT DO NOTHING;
