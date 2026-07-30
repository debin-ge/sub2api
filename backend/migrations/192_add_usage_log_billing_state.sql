-- usage_logs.billing_state：结算状态标记。
--
-- 背景：未定价模型的 fail-closed 策略原先落在记账层，定价解析失败时整条
-- RecordUsage 直接返回错误，导致"上游成本已真实发生、但本地既没扣费也没有
-- 用量记录"。本列把该场景变成一条可见、可对账、可恢复价格的记录。
--
--   0 = settled              正常结算（含管理员显式配置的 $0，那是有效价格）
--   1 = pricing_unavailable  模型无可解析价格，本行未扣费，等待补配价格后重算
--   2 = pricing_recovered    已由补偿流程恢复价格；未执行事后追扣，actual_cost 仍为实际扣除
--
-- PG 11+ 带 DEFAULT 的 NOT NULL 加列不触发全表重写。
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_state SMALLINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_logs.billing_state IS '结算状态：0=已结算 1=定价缺失待处理 2=价格已恢复（未追扣）';
