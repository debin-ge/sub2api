-- 模型价格手动覆盖表。
-- 优先级：本表 > 第三方同步目录 > fallback 文件。
-- payload 只存管理员显式配置过的价格维度，与目录条目做字段级合并；
-- 未出现的 key 表示“继承同步值”。禁止用 JSON null 表示“删除该字段”。
--
-- 编号 231：223–230 已被占用。本文件幂等，已应用过旧稿
-- 223_model_price_overrides.sql 的库可以安全再跑。

CREATE TABLE IF NOT EXISTS model_price_overrides (
    id          BIGSERIAL PRIMARY KEY,
    platform    VARCHAR(50)  NOT NULL,
    model_name  VARCHAR(200) NOT NULL,
    payload     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    note        TEXT,
    updated_by  BIGINT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS model_price_overrides_platform_model_unique
    ON model_price_overrides (platform, model_name);

CREATE INDEX IF NOT EXISTS model_price_overrides_enabled_idx
    ON model_price_overrides (enabled) WHERE enabled;

COMMENT ON TABLE model_price_overrides IS
    'Manual model price overrides; takes precedence over the synced pricing catalog';
COMMENT ON COLUMN model_price_overrides.platform IS
    'Platform key, or * for all platforms';
COMMENT ON COLUMN model_price_overrides.model_name IS
    'Lowercased model name, matching the pricing catalog key convention';
