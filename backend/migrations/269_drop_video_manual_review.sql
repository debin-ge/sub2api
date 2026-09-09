SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- 视频任务的人工复核（manual review）整体下线。
-- 结算不再需要管理员介入：completed 按下单时冻结的报价扣费，其余终态一律释放。
-- 顺序：先摘掉会拒绝回填写入的守卫 -> 重建两个执行守卫 -> 回填存量 -> 删除 schema。

-- 1) 摘掉依赖复核表的守卫。
DROP TRIGGER IF EXISTS video_tasks_manual_billing_guard ON video_tasks;
DROP TRIGGER IF EXISTS video_tasks_unknown_resolution_guard ON video_tasks;
DROP TRIGGER IF EXISTS usage_billing_outbox_video_review_guard ON usage_billing_outbox;
DROP TRIGGER IF EXISTS usage_billing_outbox_video_submission_guard ON usage_billing_outbox;
DROP TRIGGER IF EXISTS video_billing_reviews_guard ON video_billing_reviews;
DROP TRIGGER IF EXISTS video_submission_reviews_guard ON video_submission_reviews;

-- 2) 重建执行写守卫：保留执行/定价快照与未结算身份的不可变性，以及冲突标记的粘性
--    （纯审计用途），移除"冲突结算必须有已批复核"这一条。
CREATE OR REPLACE FUNCTION guard_video_execution_write() RETURNS trigger
LANGUAGE plpgsql AS $$
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
    RETURN NEW;
END;
$$;

-- 3) 重建 outbox 执行守卫：保留"财务意图不可变"与"必须有对应任务"，移除冲突任务
--    必须携带 v4 + 已批复核这一条——它会直接阻断新的自动结算意图。
CREATE OR REPLACE FUNCTION guard_video_execution_outbox() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE task video_tasks;
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
    RETURN NEW;
END;
$$;

-- 4) 清理 ByteDance 冻结规格 Bug 留下的粘性冲突标记。
--    该标记曾被无条件写入每个 ByteDance 任务的 response_metadata，命中后终态结算
--    会走冻结报价而不是真实用量，且 video_tasks_execution_guard 会把清除动作改回去，
--    因此必须在守卫停用的窗口内剥离。只处理尚未结算的 ByteDance 任务：其他 Provider
--    的冲突标记是真实的观测结果，不能一并抹掉。
ALTER TABLE video_tasks DISABLE TRIGGER video_tasks_execution_guard;

WITH stale AS (
    UPDATE video_tasks
    SET response_metadata = response_metadata - 'execution_spec_conflict',
        last_error_kind = CASE WHEN last_error_code = 'execution_spec_conflict' THEN NULL ELSE last_error_kind END,
        last_error_message = CASE WHEN last_error_code = 'execution_spec_conflict' THEN NULL ELSE last_error_message END,
        last_error_code = CASE WHEN last_error_code = 'execution_spec_conflict' THEN NULL ELSE last_error_code END,
        version = version + 1,
        updated_at = NOW()
    WHERE provider = 'bytedance'
      AND billing_state NOT IN ('captured', 'released')
      AND jsonb_typeof(response_metadata) = 'object'
      AND response_metadata ? 'execution_spec_conflict'
      AND NOT (response_metadata ? 'specification_invalid')
    RETURNING id, provider, account_id, provider_task_id, generation_state, billing_state
)
INSERT INTO video_task_events (
    task_id, event_type, provider, account_id, provider_task_id,
    from_generation_state, to_generation_state, from_billing_state, to_billing_state,
    payload, event_hash
)
SELECT id, 'execution_spec_conflict_cleared', provider, account_id, provider_task_id,
    generation_state, generation_state, billing_state, billing_state,
    '{"reason":"bytedance frozen execution specification guard was a bug; the conflict marker was never an observation"}'::JSONB,
    'video_execution_spec_conflict_cleared:269:' || id::TEXT
FROM stale
ON CONFLICT DO NOTHING;

ALTER TABLE video_tasks ENABLE TRIGGER video_tasks_execution_guard;

-- 5) 回填存量：提交结果不确定的任务一律失败释放，不再等待管理员确认。
--    事件里的 from_* 必须取 UPDATE 之前的真实取值：这批行的 billing_state 可能是
--    held / manual_review / captured / released，写死一个值就是伪造审计记录。
WITH targets AS (
    SELECT id, generation_state AS from_generation_state, billing_state AS from_billing_state
    FROM video_tasks
    WHERE generation_state = 'submission_unknown'
),
resolved AS (
    UPDATE video_tasks
    SET generation_state = 'failed',
        billing_state = CASE WHEN billing_state IN ('captured','released') THEN billing_state ELSE 'release_pending' END,
        actual_units = CASE WHEN billing_state IN ('captured','released') THEN actual_units ELSE 0 END,
        actual_cost = CASE WHEN billing_state IN ('captured','released') THEN actual_cost ELSE 0 END,
        last_error_kind = COALESCE(last_error_kind, 'transport'),
        last_error_code = COALESCE(NULLIF(last_error_code, ''), 'submission_unknown'),
        last_error_message = COALESCE(NULLIF(last_error_message, ''), 'video provider submission outcome is unknown'),
        billing_review_id = NULL,
        submission_review_id = NULL,
        finished_at = COALESCE(finished_at, NOW()),
        next_action_at = clock_timestamp(),
        version = version + 1,
        updated_at = NOW()
    FROM targets
    WHERE video_tasks.id = targets.id
    RETURNING video_tasks.id, video_tasks.provider, video_tasks.account_id,
        video_tasks.provider_task_id, video_tasks.billing_state
)
INSERT INTO video_task_events (
    task_id, event_type, provider, account_id, provider_task_id,
    from_generation_state, to_generation_state, from_billing_state, to_billing_state,
    payload, event_hash
)
SELECT resolved.id, 'submission_unknown_auto_failed', resolved.provider, resolved.account_id, resolved.provider_task_id,
    targets.from_generation_state, 'failed', targets.from_billing_state, resolved.billing_state,
    '{"reason":"manual submission review removed; an unconfirmed submission is never charged"}'::JSONB,
    'video_submission_unknown_auto_failed:269:' || resolved.id::TEXT
FROM resolved JOIN targets ON targets.id = resolved.id
ON CONFLICT DO NOTHING;

-- 6) 回填存量：待复核且已完成的任务按下单时冻结的报价扣费。
--    WHERE 同时钉死两个状态，因此事件里的 from_* 常量与事实一致。
WITH captured AS (
    UPDATE video_tasks
    SET billing_state = 'capture_pending',
        actual_units = estimated_units,
        actual_cost = hold_amount,
        billing_review_id = NULL,
        submission_review_id = NULL,
        next_action_at = clock_timestamp(),
        version = version + 1,
        updated_at = NOW()
    WHERE billing_state = 'manual_review' AND generation_state = 'completed'
      AND estimated_units IS NOT NULL AND hold_amount IS NOT NULL
    RETURNING id, provider, account_id, provider_task_id
)
INSERT INTO video_task_events (
    task_id, event_type, provider, account_id, provider_task_id,
    from_generation_state, to_generation_state, from_billing_state, to_billing_state,
    payload, event_hash
)
SELECT id, 'manual_review_auto_captured', provider, account_id, provider_task_id,
    'completed', 'completed', 'manual_review', 'capture_pending',
    '{"reason":"manual billing review removed; settled at the quote frozen when the hold was taken"}'::JSONB,
    'video_manual_review_auto_captured:269:' || id::TEXT
FROM captured
ON CONFLICT DO NOTHING;

-- 7) 回填存量：其余待复核任务无法给出可信金额，一律释放冻结额度。
--    RETURNING 返回的是 UPDATE 之后的值，直接拿它当 from_generation_state 会把
--    in_progress -> cancelled 记成 cancelled -> cancelled，必须另取旧值。
WITH targets AS (
    SELECT id, generation_state AS from_generation_state
    FROM video_tasks
    WHERE billing_state = 'manual_review'
),
released AS (
    UPDATE video_tasks
    SET generation_state = CASE WHEN generation_state IN ('completed','failed','cancelled','expired')
            THEN generation_state ELSE 'cancelled' END,
        billing_state = 'release_pending',
        actual_units = 0,
        actual_cost = 0,
        billing_review_id = NULL,
        submission_review_id = NULL,
        finished_at = COALESCE(finished_at, NOW()),
        next_action_at = clock_timestamp(),
        version = version + 1,
        updated_at = NOW()
    FROM targets
    WHERE video_tasks.id = targets.id
    RETURNING video_tasks.id, video_tasks.provider, video_tasks.account_id,
        video_tasks.provider_task_id, video_tasks.generation_state
)
INSERT INTO video_task_events (
    task_id, event_type, provider, account_id, provider_task_id,
    from_generation_state, to_generation_state, from_billing_state, to_billing_state,
    payload, event_hash
)
SELECT released.id, 'manual_review_auto_released', released.provider, released.account_id, released.provider_task_id,
    targets.from_generation_state, released.generation_state, 'manual_review', 'release_pending',
    '{"reason":"manual billing review removed; an unpriceable task is released rather than charged"}'::JSONB,
    'video_manual_review_auto_released:269:' || released.id::TEXT
FROM released JOIN targets ON targets.id = released.id
ON CONFLICT DO NOTHING;

-- 8) 删除复核专用函数。
DROP FUNCTION IF EXISTS guard_video_manual_billing_transition();
DROP FUNCTION IF EXISTS guard_video_unknown_resolution();
DROP FUNCTION IF EXISTS guard_video_billing_review_change();
DROP FUNCTION IF EXISTS guard_video_submission_review_change();
DROP FUNCTION IF EXISTS guard_video_billing_review_outbox();
DROP FUNCTION IF EXISTS guard_video_unknown_financial_intent();
DROP FUNCTION IF EXISTS video_submission_review_facts(video_tasks);
DROP FUNCTION IF EXISTS video_billing_review_facts(video_tasks);

-- 9) 删除复核外键列与复核表（先子表后父表）。
ALTER TABLE video_tasks
    DROP COLUMN IF EXISTS billing_review_id,
    DROP COLUMN IF EXISTS submission_review_id;

DROP TABLE IF EXISTS video_billing_review_actions;
DROP TABLE IF EXISTS video_submission_review_actions;
DROP TABLE IF EXISTS video_billing_reviews;
DROP TABLE IF EXISTS video_submission_reviews;

-- 10) 收紧状态机取值：submission_unknown 与 manual_review 不再是合法状态。
--     用 NOT VALID 添加：约束对新写入立即生效，但跳过全表校验，避免在这笔已经
--     持有 video_tasks ACCESS EXCLUSIVE 的事务里再叠加一次全表扫描。存量校验由
--     270 单独执行，那里只需要 SHARE UPDATE EXCLUSIVE。
ALTER TABLE video_tasks DROP CONSTRAINT IF EXISTS video_tasks_generation_state_check;
ALTER TABLE video_tasks ADD CONSTRAINT video_tasks_generation_state_check
    CHECK (generation_state IN (
        'preparing', 'held', 'submitting',
        'queued', 'in_progress', 'completed', 'failed', 'cancelled', 'expired'
    )) NOT VALID;

ALTER TABLE video_tasks DROP CONSTRAINT IF EXISTS video_tasks_billing_state_check;
ALTER TABLE video_tasks ADD CONSTRAINT video_tasks_billing_state_check
    CHECK (billing_state IN (
        'none', 'held', 'capture_pending', 'captured',
        'release_pending', 'released'
    )) NOT VALID;
