CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_callback_intents
    ON video_tasks (settled_at, id)
    WHERE callback_url_enc IS NOT NULL
      AND billing_state IN ('captured', 'released')
      AND callback_intent_state IN ('none', 'pending');
