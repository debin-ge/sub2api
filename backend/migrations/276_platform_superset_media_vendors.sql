-- 新增三个聚合媒体平台（仅视频、API Key 直连）：fal / wavespeed / runware。
-- 平台集的终态权威沿用 274 的写法：DROP IF EXISTS + ADD，取 274 的全集再加三项。
--
-- 该集合必须与以下两处保持同步（见 273 / 274 的同步契约）：
--   1. internal/service/domain_constants.go 的 AllowedQuotaPlatforms
--   2. ent/schema/user_platform_quota.go 的构建期 validator
-- internal/repository/platform_check_constraint_test.go 会在单测里校验相等 / 包含关系。
--
-- channel_monitors 的两条 provider CHECK 不动：三家与 bytedance 一样不进渠道监控。
-- 幂等：约束一律 DROP IF EXISTS + ADD。相对 274 是一次放宽（superset），
-- 蓝绿窗口期内旧 slot 只写入更小的平台子集，同样满足新约束。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance',
                        'opencode_go', 'fal', 'wavespeed', 'runware'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'windsurf',
                               'bytedance', 'opencode_go', 'fal', 'wavespeed', 'runware'));
