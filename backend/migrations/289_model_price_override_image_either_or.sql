-- 289: 模型价格覆盖行改为「按张 / 按 token」二选一。
-- image 档只保留按张价（output_cost_per_image、intervals）；token 档不再带按张价，
-- 出图按 token 只用图片输入价、图片输出价两项。
--
-- 规则 R1–R5 见 docs/图片计费二选一改造方案.md §6.4。规则外的行不改：运行时按
-- billing_mode 忽略不属于本档的字段，并在后台列为「被忽略字段」，由运营者处理。
-- 不修改 updated_at，以免影响同步失效（sync_invalidated）的判断。
--
-- 可重复执行：备份表只在第一次执行时创建，保留改动前的原值；规则重放不会再命中
-- 已转换的行。回滚时用备份表按 id 恢复 billing_mode 与 payload。

CREATE TABLE IF NOT EXISTS model_price_overrides_backup_289 AS
    SELECT * FROM model_price_overrides;

COMMENT ON TABLE model_price_overrides_backup_289 IS
    'Snapshot of model_price_overrides taken by migration 289 (image billing either-or); restore by id to roll back';

-- R1: image 档、有按张价：删掉 token 与图片 token 字段。
UPDATE model_price_overrides
SET payload = payload - ARRAY[
        'input_cost_per_token', 'output_cost_per_token',
        'cache_creation_input_token_cost', 'cache_creation_input_token_cost_above_1hr',
        'cache_read_input_token_cost', 'input_cost_per_token_priority',
        'output_cost_per_token_priority', 'cache_creation_input_token_cost_priority',
        'cache_read_input_token_cost_priority', 'long_context_input_token_threshold',
        'long_context_input_cost_multiplier', 'long_context_output_cost_multiplier',
        'input_cost_per_image_token', 'output_cost_per_image_token'
    ]::text[]
WHERE billing_mode = 'image'
  AND payload ?| ARRAY['output_cost_per_image', 'intervals']::text[]
  AND payload ?| ARRAY[
        'input_cost_per_token', 'output_cost_per_token',
        'cache_creation_input_token_cost', 'cache_creation_input_token_cost_above_1hr',
        'cache_read_input_token_cost', 'input_cost_per_token_priority',
        'output_cost_per_token_priority', 'cache_creation_input_token_cost_priority',
        'cache_read_input_token_cost_priority', 'long_context_input_token_threshold',
        'long_context_input_cost_multiplier', 'long_context_output_cost_multiplier',
        'input_cost_per_image_token', 'output_cost_per_image_token'
    ]::text[];

-- R2: gpt-image 系 image 档、没有按张价：改回 token 档（/v1/images 一直按 token 结算）。
UPDATE model_price_overrides
SET billing_mode = 'token'
WHERE billing_mode = 'image'
  AND model_name ILIKE 'gpt-image%'
  AND NOT (payload ?| ARRAY['output_cost_per_image', 'intervals']::text[]);

-- R3: 按张出图模型（Gemini 出图、Grok imagine、Imagen、Nano Banana）的 token 行：
-- 改为 image 档，删掉 token 与图片 token 字段。这些模型现在在各入口都按张收，账单不变。
UPDATE model_price_overrides
SET billing_mode = 'image',
    payload = payload - ARRAY[
        'input_cost_per_token', 'output_cost_per_token',
        'cache_creation_input_token_cost', 'cache_creation_input_token_cost_above_1hr',
        'cache_read_input_token_cost', 'input_cost_per_token_priority',
        'output_cost_per_token_priority', 'cache_creation_input_token_cost_priority',
        'cache_read_input_token_cost_priority', 'long_context_input_token_threshold',
        'long_context_input_cost_multiplier', 'long_context_output_cost_multiplier',
        'input_cost_per_image_token', 'output_cost_per_image_token'
    ]::text[]
WHERE billing_mode = 'token'
  AND model_name ILIKE ANY (ARRAY['gemini-%image%', 'grok-imagine%', 'imagen%', '%nano-banana%']);

-- R4: gpt-image 系 token 行、按张价与图片 token 价都有：删掉按张字段。
UPDATE model_price_overrides
SET payload = payload - ARRAY['output_cost_per_image', 'intervals']::text[]
WHERE billing_mode = 'token'
  AND model_name ILIKE 'gpt-image%'
  AND payload ?| ARRAY['output_cost_per_image', 'intervals']::text[]
  AND payload ?| ARRAY['input_cost_per_image_token', 'output_cost_per_image_token']::text[];

-- R5: gpt-image 系 token 行、只有按张价（没有图片 token 价）：改为 image 档，删掉 token 字段。
-- 运营者只填了按张价，按张是唯一明确的出图意图；文本 token 价在出图请求里本来就不用。
-- 必须排在 R4 之后：R4 已经删掉了「两种价都有」那类行的按张字段，这里不会再命中它们。
UPDATE model_price_overrides
SET billing_mode = 'image',
    payload = payload - ARRAY[
        'input_cost_per_token', 'output_cost_per_token',
        'cache_creation_input_token_cost', 'cache_creation_input_token_cost_above_1hr',
        'cache_read_input_token_cost', 'input_cost_per_token_priority',
        'output_cost_per_token_priority', 'cache_creation_input_token_cost_priority',
        'cache_read_input_token_cost_priority', 'long_context_input_token_threshold',
        'long_context_input_cost_multiplier', 'long_context_output_cost_multiplier'
    ]::text[]
WHERE billing_mode = 'token'
  AND model_name ILIKE 'gpt-image%'
  AND payload ?| ARRAY['output_cost_per_image', 'intervals']::text[]
  AND NOT (payload ?| ARRAY['input_cost_per_image_token', 'output_cost_per_image_token']::text[]);
