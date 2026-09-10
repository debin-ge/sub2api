-- Preserve the local display-only model-list feature after upstream migration
-- 235 renamed its column into the request-enforcing model_allowlist field.
-- Existing selections remain display-only; enforcement starts disabled and
-- must be enabled explicitly by an administrator.

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE groups
   SET models_list_config = model_allowlist,
       model_allowlist = '{}'::jsonb
 WHERE COALESCE(models_list_config, '{}'::jsonb) = '{}'::jsonb
   AND COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb;

UPDATE groups SET models_list_config = '{}'::jsonb WHERE models_list_config IS NULL;

ALTER TABLE groups ALTER COLUMN models_list_config SET DEFAULT '{}'::jsonb;
ALTER TABLE groups ALTER COLUMN models_list_config SET NOT NULL;

COMMENT ON COLUMN groups.models_list_config IS
    'Custom model-list response configuration; does not restrict request admission';
