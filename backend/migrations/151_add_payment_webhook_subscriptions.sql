-- Add provider webhook subscription state and webhook delivery idempotency records.

CREATE TABLE IF NOT EXISTS payment_provider_webhook_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    provider_instance_id BIGINT NOT NULL,
    provider_key VARCHAR(30) NOT NULL,
    external_subscription_id VARCHAR(128) NOT NULL DEFAULT '',
    trigger_on VARCHAR(100) NOT NULL,
    delivery_version VARCHAR(20) NOT NULL,
    delivery_url TEXT NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'unknown',
    last_error TEXT,
    synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_unique
    ON payment_provider_webhook_subscriptions (provider_instance_id, trigger_on, delivery_url);

CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_provider_key
    ON payment_provider_webhook_subscriptions (provider_key);

CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_external_id
    ON payment_provider_webhook_subscriptions (external_subscription_id);

CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_status
    ON payment_provider_webhook_subscriptions (status);

CREATE TABLE IF NOT EXISTS payment_webhook_deliveries (
    id BIGSERIAL PRIMARY KEY,
    provider_key VARCHAR(30) NOT NULL,
    delivery_id VARCHAR(128) NOT NULL,
    event_type VARCHAR(100) NOT NULL DEFAULT '',
    test_notification BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(30) NOT NULL DEFAULT 'received',
    raw_body_hash VARCHAR(64) NOT NULL DEFAULT '',
    error TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_webhook_deliveries_provider_delivery
    ON payment_webhook_deliveries (provider_key, delivery_id);

CREATE INDEX IF NOT EXISTS idx_payment_webhook_deliveries_provider_received
    ON payment_webhook_deliveries (provider_key, received_at);

CREATE INDEX IF NOT EXISTS idx_payment_webhook_deliveries_status
    ON payment_webhook_deliveries (status);
