-- 重建预算预留索引：谓词收敛到仍会占用冻结额度的三个状态。
-- 先删可能存在的残留：CREATE INDEX CONCURRENTLY 失败会留下一个同名的 INVALID 索引，
-- 而 IF NOT EXISTS 会把它当成已建好而静默跳过，随后旧索引照删，预算查询从此走全表扫描。
DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_budget_reservations_v2;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_budget_reservations_v2
ON video_tasks (user_id, api_key_id, provider) INCLUDE (hold_amount)
WHERE billing_state IN ('held', 'capture_pending', 'release_pending');

DROP INDEX CONCURRENTLY IF EXISTS idx_video_tasks_budget_reservations;
