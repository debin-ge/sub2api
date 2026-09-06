SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE FUNCTION video_execution_has_conflict(metadata JSONB) RETURNS BOOLEAN
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    marker TEXT;
    amount NUMERIC;
BEGIN
    FOREACH marker IN ARRAY ARRAY['execution_spec_conflict','specification_invalid'] LOOP
        IF metadata -> marker IS NULL OR metadata -> marker = 'null'::JSONB THEN
            CONTINUE;
        END IF;
        IF jsonb_typeof(metadata -> marker) NOT IN ('number','string')
            OR metadata ->> marker !~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$' THEN
            RETURN true;
        END IF;
        BEGIN
            amount := (metadata ->> marker)::NUMERIC;
        EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
            RETURN true;
        END;
        IF amount <> 0 THEN RETURN true; END IF;
    END LOOP;
    RETURN false;
END;
$$;

CREATE FUNCTION guard_video_execution_write() RETURNS trigger
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
        AND NEW.billing_state IS DISTINCT FROM OLD.billing_state THEN
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

CREATE TRIGGER video_tasks_execution_guard BEFORE UPDATE ON video_tasks
FOR EACH ROW EXECUTE FUNCTION guard_video_execution_write();

CREATE FUNCTION guard_video_execution_outbox() RETURNS trigger
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

CREATE TRIGGER usage_billing_outbox_video_execution_guard
BEFORE INSERT OR UPDATE OF request_id, api_key_id, request_fingerprint, payload_version, command_payload, usage_log_payload ON usage_billing_outbox
FOR EACH ROW EXECUTE FUNCTION guard_video_execution_outbox();
