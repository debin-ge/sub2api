-- Extend Composite routes to every concrete provider supported by the local
-- gateway. GLM is a legacy alias and is normalized to zhipu by the service
-- before persistence, so the database stores only the canonical platform.
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok',
        'kimi', 'zhipu', 'deepseek', 'minimax', 'windsurf', 'opencode'
    ));
