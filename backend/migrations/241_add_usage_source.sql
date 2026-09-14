-- Record whether usage billing used upstream usage or a local fallback.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS usage_source SMALLINT NOT NULL DEFAULT 0;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS usage_estimation_method VARCHAR(16);

COMMENT ON COLUMN usage_logs.usage_source IS '用量来源：0=未知 1=上游 2=本地估算 3=最低兜底';
COMMENT ON COLUMN usage_logs.usage_estimation_method IS '用量估算方式：upstream/tokenizer/heuristic/minimum';
