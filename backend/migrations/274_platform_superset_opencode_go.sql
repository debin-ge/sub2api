-- 合并 upstream v0.2.7 的 OpenCode 平台，并把四个平台 CHECK 重建为 fork ∪ upstream
-- 的真并集。排在 268 / 273 之后，是平台集的终态权威。
--
-- 为什么不能只靠 238_a 跳过 upstream 238：生产库止于 241，268（composite 加
-- bytedance）与 273（quotas 并集）本次才首度执行，它们的平台集都不含 'opencode_go'，
-- 会把 238 的成果覆盖掉。因此必须由一条排在它们之后的迁移写定终态。
--
-- 平台 ID 归一：本 fork 早先自建的 'opencode' 与 upstream 的 'opencode_go' 是同一家
-- 上游，统一采用 upstream 的 'opencode_go'（账号类型 Zen 按量 / Go 订阅），本地
-- opencode 网关随代码一并下线。存量数据在收紧约束之前先行改写。
--
-- 该集合必须与以下两处保持同步（见 273 的同步契约）：
--   1. internal/service/domain_constants.go 的 AllowedQuotaPlatforms
--   2. ent/schema/user_platform_quota.go 的构建期 validator
--
-- 幂等：UPDATE 重跑影响 0 行；约束一律 DROP IF EXISTS + ADD。
-- 兼容性：相对 268 / 273 是一次放宽（superset），不与既有数据冲突；蓝绿窗口期内
-- 旧 slot 只写入更小的平台子集，同样满足新约束，符合 expand-contract。

-- 1) 平台 ID 归一，必须先于约束收紧执行
UPDATE accounts
   SET platform = 'opencode_go'
 WHERE platform = 'opencode';

UPDATE composite_model_routes
   SET target_platform = 'opencode_go'
 WHERE target_platform = 'opencode';

UPDATE channel_monitors
   SET provider = 'opencode_go'
 WHERE provider = 'opencode';

UPDATE channel_monitor_request_templates
   SET provider = 'opencode_go'
 WHERE provider = 'opencode';

-- 2) user_platform_quotas：273 的集合 + opencode_go
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance',
                        'opencode_go'));

-- 3) composite_model_routes：268 的集合，opencode → opencode_go
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'windsurf',
                               'bytedance', 'opencode_go'));

-- 4) channel_monitors / channel_monitor_request_templates：237 的集合，
--    保留本地 glm / windsurf，opencode → opencode_go
ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'glm', 'windsurf',
                        'opencode_go'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'glm', 'windsurf',
                        'opencode_go'));
