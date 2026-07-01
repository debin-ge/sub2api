-- Enforce Wise upstream transaction id uniqueness at the database layer.
-- Non-transactional migration: CREATE INDEX CONCURRENTLY cannot run in a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_payment_type_payment_trade_no
  ON payment_orders (payment_type, payment_trade_no)
  WHERE payment_type = 'wise' AND payment_trade_no <> '';
