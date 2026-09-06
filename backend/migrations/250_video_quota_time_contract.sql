SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE FUNCTION guard_video_quota_time_snapshot() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.price_snapshot -> 'quota_time_contract_version') IS DISTINCT FROM (OLD.price_snapshot -> 'quota_time_contract_version')
        OR (NEW.price_snapshot -> 'quota_time_zone') IS DISTINCT FROM (OLD.price_snapshot -> 'quota_time_zone')
        OR (OLD.price_snapshot ? 'quota_time_contract_version' AND OLD.finished_at IS NOT NULL
            AND NEW.finished_at IS DISTINCT FROM OLD.finished_at) THEN
        RAISE EXCEPTION 'video quota time snapshot is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER video_tasks_quota_time_guard
BEFORE UPDATE OF price_snapshot, finished_at ON video_tasks
FOR EACH ROW EXECUTE FUNCTION guard_video_quota_time_snapshot();

CREATE FUNCTION guard_video_quota_time_outbox() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    task_snapshot JSONB;
    terminal_at TIMESTAMPTZ;
    time_zone TEXT;
    event_at TIMESTAMPTZ;
BEGIN
    IF NEW.command_payload ->> 'settlement_scope' IS DISTINCT FROM 'video_task'
        OR NEW.command_payload ->> 'action' IS DISTINCT FROM 'capture' THEN
        RETURN NEW;
    END IF;
    SELECT price_snapshot, finished_at INTO task_snapshot, terminal_at
    FROM video_tasks WHERE id = (NEW.command_payload ->> 'video_task_id')::BIGINT;

    IF NOT COALESCE(task_snapshot ? 'quota_time_contract_version', false) AND NEW.payload_version <> 3 THEN
        RETURN NEW;
    END IF;
    IF task_snapshot ->> 'quota_time_contract_version' IS DISTINCT FROM '1'
        OR NEW.payload_version <> 3
        OR NEW.command_payload #>> '{billing,quota_time,version}' IS DISTINCT FROM '1'
        OR terminal_at IS NULL THEN
        RAISE EXCEPTION 'video quota time requires a terminal event and outbox v3' USING ERRCODE = '23514';
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

CREATE TRIGGER usage_billing_outbox_video_quota_time_guard
BEFORE INSERT OR UPDATE OF command_payload, payload_version ON usage_billing_outbox
FOR EACH ROW EXECUTE FUNCTION guard_video_quota_time_outbox();
