package schema

import (
	"slices"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/stretchr/testify/require"
)

func TestPaymentProviderWebhookSubscriptionSchemaFields(t *testing.T) {
	t.Parallel()

	fields := PaymentProviderWebhookSubscription{}.Fields()
	names := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		desc := f.Descriptor()
		names[desc.Name] = struct{}{}
	}

	for _, name := range []string{
		"provider_instance_id",
		"provider_key",
		"external_subscription_id",
		"trigger_on",
		"delivery_version",
		"delivery_url",
		"status",
		"last_error",
		"synced_at",
		"created_at",
		"updated_at",
	} {
		_, ok := names[name]
		require.Truef(t, ok, "missing field %s", name)
	}

	indexes := PaymentProviderWebhookSubscription{}.Indexes()
	requireIndex(t, indexes, true, "provider_instance_id", "trigger_on", "delivery_url")
	requireIndex(t, indexes, false, "provider_key")
	requireIndex(t, indexes, false, "external_subscription_id")
	requireIndex(t, indexes, false, "status")

	requireTableName(t, PaymentProviderWebhookSubscription{}.Annotations(), "payment_provider_webhook_subscriptions")
}

func TestPaymentWebhookDeliverySchemaFields(t *testing.T) {
	t.Parallel()

	fields := PaymentWebhookDelivery{}.Fields()
	names := make(map[string]field.Type, len(fields))
	for _, f := range fields {
		desc := f.Descriptor()
		names[desc.Name] = desc.Info.Type
	}

	for _, name := range []string{
		"provider_key",
		"delivery_id",
		"event_type",
		"test_notification",
		"status",
		"raw_body_hash",
		"error",
		"received_at",
		"processed_at",
		"created_at",
		"updated_at",
	} {
		_, ok := names[name]
		require.Truef(t, ok, "missing field %s", name)
	}

	indexes := PaymentWebhookDelivery{}.Indexes()
	requireIndex(t, indexes, true, "provider_key", "delivery_id")
	requireIndex(t, indexes, false, "provider_key", "received_at")
	requireIndex(t, indexes, false, "status")

	requireTableName(t, PaymentWebhookDelivery{}.Annotations(), "payment_webhook_deliveries")
}

func requireIndex(t *testing.T, indexes []ent.Index, unique bool, fields ...string) {
	t.Helper()

	for _, idx := range indexes {
		desc := idx.Descriptor()
		if desc.Unique == unique && slices.Equal(fields, desc.Fields) {
			return
		}
	}

	require.Failf(t, "missing index", "unique=%v fields=%v", unique, fields)
}

func requireTableName(t *testing.T, annotations []entschema.Annotation, table string) {
	t.Helper()

	for _, annotation := range annotations {
		if sqlAnnotation, ok := annotation.(entsql.Annotation); ok && sqlAnnotation.Table == table {
			return
		}
	}

	require.Failf(t, "missing table annotation", "table=%s", table)
}
