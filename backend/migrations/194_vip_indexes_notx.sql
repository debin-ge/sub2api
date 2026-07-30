-- This non-transactional migration intentionally contains concurrent indexes only.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_is_vip
    ON users (is_vip)
    WHERE deleted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_vip_manual_override
    ON users (vip_manual_override)
    WHERE deleted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_groups_vip_only
    ON groups (vip_only)
    WHERE deleted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_vip_reconcile_cursor
    ON payment_orders (completed_at, id)
    WHERE completed_at IS NOT NULL
      AND status IN (
          'COMPLETED',
          'PARTIALLY_REFUNDED',
          'REFUND_REQUESTED',
          'REFUNDING',
          'REFUND_PENDING',
          'REFUNDED',
          'REFUND_FAILED'
      );

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_vip_user_completed
    ON payment_orders (user_id, completed_at DESC, id DESC)
    WHERE completed_at IS NOT NULL
      AND paid_at IS NOT NULL
      AND pay_amount > 0
      AND order_type IN ('balance', 'subscription')
      AND status IN (
          'COMPLETED',
          'PARTIALLY_REFUNDED',
          'REFUND_REQUESTED',
          'REFUNDING',
          'REFUND_PENDING',
          'REFUNDED',
          'REFUND_FAILED'
      );

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_user_vip_audit_order_action
    ON user_vip_audit_events (order_id, action)
    WHERE order_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_vip_audit_user_created
    ON user_vip_audit_events (user_id, created_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_vip_audit_actor_created
    ON user_vip_audit_events (actor_user_id, created_at DESC, id DESC)
    WHERE actor_user_id IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_vip_reconcile_jobs_request_id
    ON vip_reconcile_jobs (request_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_vip_reconcile_jobs_one_active
    ON vip_reconcile_jobs ((1))
    WHERE status IN ('queued', 'running');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_vip_reconcile_jobs_status_updated
    ON vip_reconcile_jobs (status, updated_at, id);
