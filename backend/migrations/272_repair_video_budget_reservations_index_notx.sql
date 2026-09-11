-- 修复 269_drop_video_manual_review_notx.sql 首版可能留下的 INVALID 索引。
--
-- 首版只有 CREATE v2 + DROP v1 两条语句。CREATE INDEX CONCURRENTLY 失败会留下一个
-- 同名的 INVALID 索引，而 IF NOT EXISTS 会把它当成已建好而静默跳过——迁移随后照常
-- 记录成功并删掉 v1，预算预留查询从此走全表扫描，而且不会再报任何错。
--
-- 269_notx 的当前版本用一条前置 DROP 堵住了这个洞，但已经应用首版的库会凭历史
-- checksum 整体跳过 269_notx（见 migrations_runner.go 的兼容规则），永远拿不到它。
-- 因此把修复搬到这里：runner 的 prepareNonTransactionalMigration 钩子只在 v2 索引
-- 确实处于 INVALID 状态时才删除它，随后由下面两条语句补齐。索引正常的库上，三个
-- 动作全是空操作。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_budget_reservations_v2
ON video_tasks (user_id, api_key_id, provider) INCLUDE (hold_amount)
WHERE billing_state IN ('held', 'capture_pending', 'release_pending');

DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_budget_reservations;
