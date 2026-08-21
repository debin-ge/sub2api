-- 统一国产供应商账号的 BaseURL 存储：当前管理端使用 credentials.base_url，
-- 历史账号可能只保存了协议专用字段。只在通用字段为空时回填，保证迁移幂等。

UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{base_url}',
    CASE
        WHEN credentials->>'api_protocol' = 'anthropic'
             AND NULLIF(credentials->>'base_url_anthropic', '') IS NOT NULL
            THEN to_jsonb(credentials->>'base_url_anthropic')
        WHEN NULLIF(credentials->>'base_url_openai', '') IS NOT NULL
            THEN to_jsonb(credentials->>'base_url_openai')
        ELSE to_jsonb(credentials->>'base_url_anthropic')
    END,
    true
)
WHERE platform IN ('kimi', 'zhipu', 'glm', 'deepseek')
  AND COALESCE(credentials->>'base_url', '') = ''
  AND (
      NULLIF(credentials->>'base_url_openai', '') IS NOT NULL
      OR NULLIF(credentials->>'base_url_anthropic', '') IS NOT NULL
  );
