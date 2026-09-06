SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_endpoint_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_endpoint_check
    CHECK (endpoint IN (
        'any', 'messages', 'count_tokens', 'responses', 'chat_completions',
        'embeddings', 'images', 'videos', 'video_characters',
        'video_edits', 'video_extensions', 'gemini'
    ));

COMMENT ON COLUMN composite_model_routes.endpoint IS 'Protocol capability used for model routing, including provider-neutral video operations';
