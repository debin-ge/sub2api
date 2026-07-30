-- 待定价用量的部分索引：billing_state = 1 的行是异常少数，正常或已恢复价格的行不进入索引。
-- 管理端按时间倒序展示；补偿流程按 id 游标升序扫描，分别提供匹配访问路径的索引。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_billing_state_pending
    ON usage_logs (created_at DESC)
    WHERE billing_state = 1;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_billing_state_pending_id
    ON usage_logs (id)
    WHERE billing_state = 1;
