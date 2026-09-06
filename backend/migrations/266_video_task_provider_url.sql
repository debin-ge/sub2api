SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE video_tasks
    ADD COLUMN IF NOT EXISTS provider_video_url_enc TEXT,
    ADD COLUMN IF NOT EXISTS provider_video_proxy_key VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_video_tasks_owner_provider_video_proxy_key
    ON video_tasks (user_id, provider_video_proxy_key)
    WHERE provider_video_proxy_key IS NOT NULL AND source = 'managed';

COMMENT ON COLUMN video_tasks.provider_video_url_enc IS
    'Encrypted complete provider video_url used for public host rewriting and authenticated content proxying';

COMMENT ON COLUMN video_tasks.provider_video_proxy_key IS
    'SHA-256 lookup key for the exact provider video_url request target; contains no URL plaintext';
