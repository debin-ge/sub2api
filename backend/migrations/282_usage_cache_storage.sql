-- Explicit cache storage is a separate metered dimension, including pending pricing rows.
-- ALTERing the partitioned parent propagates columns to existing partitions.
ALTER TABLE usage_logs
  ADD COLUMN IF NOT EXISTS cache_storage_token_hours NUMERIC(30,10) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS cache_storage_cost NUMERIC(20,8) NOT NULL DEFAULT 0;
