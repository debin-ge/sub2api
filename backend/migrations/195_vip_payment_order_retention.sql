-- Preserve paid VIP qualification facts across payment-order archival.
--
-- There is intentionally no application hard-delete endpoint for payment
-- orders. This database guard also covers direct Ent and SQL deletes so a
-- future cleanup job cannot silently erase the source of VIP eligibility.

CREATE TABLE IF NOT EXISTS vip_payment_order_fact_archive (
    order_id          BIGINT PRIMARY KEY,
    user_id           BIGINT NOT NULL,
    amount            NUMERIC(20,2) NOT NULL,
    pay_amount        NUMERIC(20,2) NOT NULL,
    order_type        VARCHAR(20) NOT NULL,
    payment_type      VARCHAR(30) NOT NULL,
    status            VARCHAR(30) NOT NULL,
    paid_at           TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ NOT NULL,
    order_snapshot    JSONB NOT NULL,
    archived_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT vip_payment_order_fact_archive_qualifying_check
        CHECK (
            order_type IN ('balance', 'subscription')
            AND pay_amount > 0
            AND status IN (
                'COMPLETED',
                'REFUND_REQUESTED',
                'REFUNDING',
                'REFUND_PENDING',
                'PARTIALLY_REFUNDED',
                'REFUNDED',
                'REFUND_FAILED'
            )
        )
);

COMMENT ON TABLE vip_payment_order_fact_archive IS
    'Immutable qualification facts retained before eligible paid orders are hard-deleted';
COMMENT ON COLUMN vip_payment_order_fact_archive.order_snapshot IS
    'Complete payment_orders row at archival time, retained for audit and reconciliation';

CREATE INDEX IF NOT EXISTS idx_vip_payment_order_fact_archive_user_completed
    ON vip_payment_order_fact_archive (user_id, completed_at, order_id);

CREATE INDEX IF NOT EXISTS idx_vip_payment_order_fact_archive_completed
    ON vip_payment_order_fact_archive (completed_at, order_id);

-- Every entitlement reader uses this view as the durable payment-fact source.
-- The trigger below makes the two branches disjoint: an archived order id can
-- never become a qualifying live fact again. Keeping this as a pure UNION ALL
-- lets keyset reconciliation use both (completed_at, order_id) indexes.
CREATE OR REPLACE VIEW vip_qualifying_payment_order_facts AS
(
SELECT
    archived.order_id,
    archived.user_id,
    archived.completed_at
FROM vip_payment_order_fact_archive archived
ORDER BY archived.completed_at, archived.order_id
)
UNION ALL
(
SELECT
    po.id AS order_id,
    po.user_id,
    po.completed_at
FROM payment_orders po
WHERE po.order_type IN ('balance', 'subscription')
  AND po.pay_amount > 0
  AND po.paid_at IS NOT NULL
  AND po.completed_at IS NOT NULL
  AND po.status IN (
      'COMPLETED',
      'REFUND_REQUESTED',
      'REFUNDING',
      'REFUND_PENDING',
      'PARTIALLY_REFUNDED',
      'REFUNDED',
      'REFUND_FAILED'
  )
ORDER BY po.completed_at, po.id
);

COMMENT ON VIEW vip_qualifying_payment_order_facts IS
    'Durable active plus archived facts used by AUTO, activation-pending, L1, and L2 VIP reconciliation';

CREATE OR REPLACE FUNCTION archive_materialized_vip_payment_order_fact()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    old_qualifies BOOLEAN;
    new_qualifies BOOLEAN;
    first_order_id BIGINT;
    second_order_id BIGINT;
BEGIN
    -- Serialize every payment-order lifecycle transition by its durable id.
    -- This closes the race where an INSERT begins while a qualifying row is
    -- being archived and otherwise observes neither committed representation.
    IF TG_OP = 'INSERT' THEN
        PERFORM pg_advisory_xact_lock(
            hashtext('vip_payment_order_fact'),
            hashtext(NEW.id::TEXT)
        );

        IF EXISTS (
            SELECT 1
            FROM vip_payment_order_fact_archive archived
            WHERE archived.order_id = NEW.id
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE',
                DETAIL = format(
                    'payment order id %s has an immutable archived qualification fact',
                    NEW.id
                );
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' AND OLD.id IS DISTINCT FROM NEW.id THEN
        first_order_id := LEAST(OLD.id, NEW.id);
        second_order_id := GREATEST(OLD.id, NEW.id);
        PERFORM pg_advisory_xact_lock(
            hashtext('vip_payment_order_fact'),
            hashtext(first_order_id::TEXT)
        );
        PERFORM pg_advisory_xact_lock(
            hashtext('vip_payment_order_fact'),
            hashtext(second_order_id::TEXT)
        );
    ELSE
        PERFORM pg_advisory_xact_lock(
            hashtext('vip_payment_order_fact'),
            hashtext(OLD.id::TEXT)
        );
    END IF;

    IF TG_OP = 'UPDATE'
       AND OLD.id IS DISTINCT FROM NEW.id
       AND EXISTS (
           SELECT 1
           FROM vip_payment_order_fact_archive archived
           WHERE archived.order_id = NEW.id
       )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE',
            DETAIL = format(
                'payment order id %s has an immutable archived qualification fact',
                NEW.id
            );
    END IF;

    old_qualifies := (
        OLD.order_type IN ('balance', 'subscription')
        AND OLD.pay_amount > 0
        AND OLD.paid_at IS NOT NULL
        AND OLD.completed_at IS NOT NULL
        AND OLD.status IN (
            'COMPLETED',
            'REFUND_REQUESTED',
            'REFUNDING',
            'REFUND_PENDING',
            'PARTIALLY_REFUNDED',
            'REFUNDED',
            'REFUND_FAILED'
        )
    );

    -- Rows that have never become eligible payment facts remain governed by
    -- the ordinary payment-order lifecycle. They may become qualifying only
    -- if this id has no immutable archived fact.
    IF NOT old_qualifies THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;

        new_qualifies := (
            NEW.order_type IN ('balance', 'subscription')
            AND NEW.pay_amount > 0
            AND NEW.paid_at IS NOT NULL
            AND NEW.completed_at IS NOT NULL
            AND NEW.status IN (
                'COMPLETED',
                'REFUND_REQUESTED',
                'REFUNDING',
                'REFUND_PENDING',
                'PARTIALLY_REFUNDED',
                'REFUNDED',
                'REFUND_FAILED'
            )
        );
        IF new_qualifies
           AND EXISTS (
               SELECT 1
               FROM vip_payment_order_fact_archive archived
               WHERE archived.order_id = NEW.id
           )
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'VIP_PAYMENT_FACT_ARCHIVED_ID_REUSE',
                DETAIL = format(
                    'payment order id %s cannot become qualifying after archival',
                    NEW.id
                );
        END IF;
        RETURN NEW;
    END IF;

    -- Once a row becomes a qualification fact, its identity is immutable.
    -- Refund state and refund metadata are deliberately excluded.
    IF TG_OP = 'UPDATE'
       AND (
           OLD.id IS DISTINCT FROM NEW.id
           OR OLD.user_id IS DISTINCT FROM NEW.user_id
           OR OLD.amount IS DISTINCT FROM NEW.amount
           OR OLD.pay_amount IS DISTINCT FROM NEW.pay_amount
           OR OLD.order_type IS DISTINCT FROM NEW.order_type
           OR OLD.payment_type IS DISTINCT FROM NEW.payment_type
           OR OLD.paid_at IS DISTINCT FROM NEW.paid_at
           OR OLD.completed_at IS DISTINCT FROM NEW.completed_at
       )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'VIP_PAYMENT_FACT_IDENTITY_IMMUTABLE',
            DETAIL = format(
                'payment order %s qualification identity cannot be rewritten',
                OLD.id
            );
    END IF;

    -- Refund-state and refund-metadata updates keep the qualification fact in
    -- the active table. They need no archive copy and must never revoke VIP.
    IF TG_OP = 'UPDATE' THEN
        new_qualifies := (
            NEW.order_type IN ('balance', 'subscription')
            AND NEW.pay_amount > 0
            AND NEW.paid_at IS NOT NULL
            AND NEW.completed_at IS NOT NULL
            AND NEW.status IN (
                'COMPLETED',
                'REFUND_REQUESTED',
                'REFUNDING',
                'REFUND_PENDING',
                'PARTIALLY_REFUNDED',
                'REFUNDED',
                'REFUND_FAILED'
            )
        );
        IF new_qualifies THEN
            RETURN NEW;
        END IF;
    END IF;

    -- Qualifying-to-nonqualifying status changes and deletes may proceed only
    -- after entitlement plus the exact order audit have been materialized.
    IF NOT EXISTS (
        SELECT 1
        FROM users u
        WHERE u.id = OLD.user_id
          AND u.vip_paid_eligible
    ) OR NOT EXISTS (
        SELECT 1
        FROM user_vip_audit_events e
        WHERE e.user_id = OLD.user_id
          AND e.order_id = OLD.id
          AND e.action IN ('paid_grant', 'backfill', 'reconcile')
          AND e.new_paid_eligible
    )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'VIP_PAYMENT_FACT_NOT_MATERIALIZED',
            DETAIL = format(
                'payment order %s cannot lose its qualification fact before VIP eligibility and audit are materialized',
                OLD.id
            );
    END IF;

    INSERT INTO vip_payment_order_fact_archive (
        order_id,
        user_id,
        amount,
        pay_amount,
        order_type,
        payment_type,
        status,
        paid_at,
        completed_at,
        order_snapshot,
        archived_at
    )
    VALUES (
        OLD.id,
        OLD.user_id,
        OLD.amount,
        OLD.pay_amount,
        OLD.order_type,
        OLD.payment_type,
        OLD.status,
        OLD.paid_at,
        OLD.completed_at,
        to_jsonb(OLD),
        CURRENT_TIMESTAMP
    )
    ON CONFLICT (order_id) DO NOTHING;

    IF TG_OP = 'UPDATE' THEN
        RETURN NEW;
    END IF;
    RETURN OLD;
END
$$;

DROP TRIGGER IF EXISTS trg_payment_orders_vip_fact_retention
    ON payment_orders;

CREATE TRIGGER trg_payment_orders_vip_fact_retention
BEFORE INSERT OR UPDATE OR DELETE ON payment_orders
FOR EACH ROW
EXECUTE FUNCTION archive_materialized_vip_payment_order_fact();
