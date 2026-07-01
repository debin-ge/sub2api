package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWiseWebhookSubscriptionMigrationSQL(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "151_add_payment_webhook_subscriptions.sql"))
	require.NoError(t, err)

	sql := string(content)
	normalized := strings.Join(strings.Fields(sql), " ")

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS payment_provider_webhook_subscriptions")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS payment_webhook_deliveries")

	require.Contains(t, normalized, "provider_instance_id BIGINT NOT NULL")
	require.Contains(t, normalized, "delivery_url TEXT NOT NULL")
	require.Contains(t, normalized, "test_notification BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, normalized, "received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()")

	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_unique")
	require.Contains(t, normalized, "ON payment_provider_webhook_subscriptions (provider_instance_id, trigger_on, delivery_url)")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_provider_key")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_external_id")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_payment_provider_webhook_subscriptions_status")

	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_webhook_deliveries_provider_delivery")
	require.Contains(t, normalized, "ON payment_webhook_deliveries (provider_key, delivery_id)")
	require.Contains(t, normalized, "ON payment_webhook_deliveries (provider_key, received_at)")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_payment_webhook_deliveries_status")
}
