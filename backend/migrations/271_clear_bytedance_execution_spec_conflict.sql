SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- 补齐 269 首版遗漏的清理动作。
--
-- videoFrozenExecutionSpec() 曾对 ByteDance 无条件返回 ErrVideoSourceSpecUnavailable，
-- 于是每个 ByteDance 任务在终态结算时都会被判成规格冲突，并把 execution_spec_conflict
-- 写进 response_metadata。该标记从来不是一次真实观测，而是那个 Bug 的产物。
--
-- guard_video_execution_write() 会把清除动作改回去（标记具备粘性，纯审计用途），
-- 因此必须在守卫停用的窗口内剥离。
--
-- 269 的首版没有这一步，已经应用首版的库会跳过 269（见 migrations_runner.go 的
-- checksum 兼容规则），所以这里单独补一遍；对已经跑过 269 当前版本的新库，
-- 标记早已清空，本迁移匹配 0 行，是无副作用的空操作。
--
-- 只处理尚未结算的 ByteDance 任务：其他 Provider 的冲突标记是真实观测结果，
-- 已 captured / released 的任务资金已经落地，重写标记只会掩盖历史。
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
    'video_execution_spec_conflict_cleared:271:' || id::TEXT
FROM stale
ON CONFLICT DO NOTHING;

ALTER TABLE video_tasks ENABLE TRIGGER video_tasks_execution_guard;
