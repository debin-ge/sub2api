-- Public Ark status filtering must happen before pagination. The CASE mirrors
-- ProjectArkVideoStatus, including completed-but-not-captured => running.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_ark_owner_status_created
ON video_tasks (user_id, (CASE
 WHEN generation_state = 'completed' AND billing_state = 'captured' THEN 'succeeded'
 WHEN generation_state IN ('in_progress', 'completed') THEN 'running'
 WHEN generation_state = 'failed' THEN 'failed'
 WHEN generation_state = 'cancelled' THEN 'cancelled'
 WHEN generation_state = 'expired' THEN 'expired'
 ELSE 'queued' END), created_at DESC, id DESC)
WHERE source = 'managed' AND operation = 'generate'
  AND provider IN ('bytedance', 'fal', 'wavespeed', 'runware', 'flova');

-- Queries without a status predicate must not sort across the status groups
-- above or walk non-Ark history using the general owner index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_ark_owner_created
ON video_tasks (user_id, created_at DESC, id DESC)
WHERE source = 'managed' AND operation = 'generate'
  AND provider IN ('bytedance', 'fal', 'wavespeed', 'runware', 'flova');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_ark_owner_model_created
ON video_tasks (user_id, public_model, created_at DESC, id DESC)
WHERE source = 'managed' AND operation = 'generate'
  AND provider IN ('bytedance', 'fal', 'wavespeed', 'runware', 'flova');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_ark_owner_model_status_created
ON video_tasks (user_id, public_model, (CASE
 WHEN generation_state = 'completed' AND billing_state = 'captured' THEN 'succeeded'
 WHEN generation_state IN ('in_progress', 'completed') THEN 'running'
 WHEN generation_state = 'failed' THEN 'failed'
 WHEN generation_state = 'cancelled' THEN 'cancelled'
 WHEN generation_state = 'expired' THEN 'expired'
 ELSE 'queued' END), created_at DESC, id DESC)
WHERE source = 'managed' AND operation = 'generate'
  AND provider IN ('bytedance', 'fal', 'wavespeed', 'runware', 'flova');
