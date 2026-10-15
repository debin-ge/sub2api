-- Local historical migrations 274/276/277 recreate these CHECKs on fresh installs.
-- Remove them after both migration lineages; platform membership is validated by
-- the unified application registry in Ent, repositories and admin entry points.
-- Channel monitor CHECKs describe actual probe capabilities and remain intact.
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
