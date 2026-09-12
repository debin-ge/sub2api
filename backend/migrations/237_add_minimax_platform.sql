-- 把 MiniMax 加入国产供应商平台白名单：
--   1. user_platform_quotas.platform CHECK
--   2. composite_model_routes.target_platform CHECK
--   3. channel_monitors / channel_monitor_request_templates.provider CHECK
--
-- 与 224/226/227 同型：DROP IF EXISTS 后重建超集约束，存量行瞬时校验通过。
--
-- 2026-09 修订（main → test 合并之后）：首版的前两条 CHECK 不含 'bytedance'。
-- 本文件按文件名排在 267/268 之前，但在 test 血脉的库上是合并后才第一次执行的，
-- 于是它会在 267/268 已经放行 bytedance 之后，拿不含 bytedance 的旧集合重建约束：
--   * user_platform_quotas：库里已有 platform='bytedance' 的行，ADD CONSTRAINT 当场失败，
--     整批迁移中止、应用启动崩溃循环（273 救不了，它排在后面）；
--   * composite_model_routes：无声收窄，把 268 已经授予的 bytedance 抹掉，而 268 已应用
--     不会再跑，此后 bytedance 复合路由再也写不进去。
-- 改法是把这两条写成含 bytedance 的超集：迟到执行不再收窄任何既有取值，干净库上的终态
-- 也不变（此后仍由 267 收窄、273 重新放开为 10 个全集）。
-- 修订时已核实可达环境（本地 + 测试，均为 test 血脉）都还没应用过本迁移，因此没有登记
-- checksum 兼容规则；若日后出现应用过首版的库，启动报错会把所需的历史 checksum 直接打出来，
-- 届时再补一条 internal/repository/migrations_runner.go 的 migrationChecksumCompatibilityRules。
-- 下面两段 provider CHECK 故意未动：226 已把它们设成同样的 12 个取值，
-- position('minimax') 守卫会让整段短路跳过。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'windsurf', 'opencode',
                               'bytedance'));

DO $$
DECLARE
    monitor_constraint_def TEXT;
    template_constraint_def TEXT;
BEGIN
    SELECT pg_get_constraintdef(c.oid)
      INTO monitor_constraint_def
      FROM pg_constraint c
      JOIN pg_class t ON t.oid = c.conrelid
     WHERE t.relname = 'channel_monitors'
       AND c.conname = 'channel_monitors_provider_check';

    IF monitor_constraint_def IS NULL OR position('minimax' IN monitor_constraint_def) = 0 THEN
        ALTER TABLE channel_monitors
            DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax',
                                'glm', 'windsurf', 'opencode'));
    END IF;

    SELECT pg_get_constraintdef(c.oid)
      INTO template_constraint_def
      FROM pg_constraint c
      JOIN pg_class t ON t.oid = c.conrelid
     WHERE t.relname = 'channel_monitor_request_templates'
       AND c.conname = 'channel_monitor_request_templates_provider_check';

    IF template_constraint_def IS NULL OR position('minimax' IN template_constraint_def) = 0 THEN
        ALTER TABLE channel_monitor_request_templates
            DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax',
                                'glm', 'windsurf', 'opencode'));
    END IF;
END $$;
