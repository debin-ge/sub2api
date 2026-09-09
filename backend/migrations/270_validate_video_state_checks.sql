SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30min';

-- 269 用 NOT VALID 添加了两条状态取值约束，避免在持有 ACCESS EXCLUSIVE 的事务里
-- 再做一次全表扫描。此处补上存量校验：VALIDATE CONSTRAINT 只取 SHARE UPDATE EXCLUSIVE，
-- 不阻塞读写；269 的回填已经把 submission_unknown / manual_review 全部清空，正常应当通过。
--
-- 用 DO 块而不是裸语句：已经应用 269 首版的库里，这两条约束是直接以已校验状态添加的，
-- 而更早的库可能根本没有同名约束。对已校验的约束再 VALIDATE 是空操作，但对不存在的
-- 约束会直接报错并让应用启动失败——正是这类库最需要顺利启动。
DO $$
DECLARE
    target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['video_tasks_generation_state_check', 'video_tasks_billing_state_check'] LOOP
        IF EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conrelid = 'video_tasks'::REGCLASS
              AND conname = target
              AND contype = 'c'
              AND NOT convalidated
        ) THEN
            EXECUTE format('ALTER TABLE video_tasks VALIDATE CONSTRAINT %I', target);
        END IF;
    END LOOP;
END;
$$;
