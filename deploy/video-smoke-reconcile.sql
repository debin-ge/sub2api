\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '1s';
SELECT t.public_id, t.generation_state, t.billing_state, t.delete_state,
       t.hold_amount, t.actual_cost, t.settled_at,
       (SELECT COUNT(*) FROM video_task_events e WHERE e.task_id=t.id AND e.event_type='balance_held') AS hold_events,
       (SELECT COUNT(*) FROM usage_logs l WHERE l.user_id=t.user_id AND l.request_id='video_task_capture:' || t.public_id) AS capture_usage_count,
       (SELECT COUNT(*) FROM usage_billing_outbox o WHERE o.api_key_id=t.api_key_id AND o.request_id='video_task_capture:' || t.public_id) AS pending_capture_outbox
FROM video_tasks t WHERE t.public_id=:'task_id' AND t.user_id=:'user_id'::bigint;
SELECT id, balance, frozen_balance FROM users WHERE id=:'user_id'::bigint;
COMMIT;
