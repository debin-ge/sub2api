CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_budget_reservations
ON video_tasks (user_id, api_key_id, provider) INCLUDE (hold_amount)
WHERE billing_state IN ('held', 'capture_pending', 'release_pending', 'manual_review');
