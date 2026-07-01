package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PaymentProviderWebhookSubscription struct {
	ent.Schema
}

func (PaymentProviderWebhookSubscription) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "payment_provider_webhook_subscriptions"},
	}
}

func (PaymentProviderWebhookSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("provider_instance_id"),
		field.String("provider_key").
			MaxLen(30).
			NotEmpty(),
		field.String("external_subscription_id").
			MaxLen(128).
			Default(""),
		field.String("trigger_on").
			MaxLen(100).
			NotEmpty(),
		field.String("delivery_version").
			MaxLen(20).
			NotEmpty(),
		field.String("delivery_url").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			NotEmpty(),
		field.String("status").
			MaxLen(30).
			Default("unknown"),
		field.String("last_error").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("synced_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentProviderWebhookSubscription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_instance_id", "trigger_on", "delivery_url").
			Unique(),
		index.Fields("provider_key"),
		index.Fields("external_subscription_id"),
		index.Fields("status"),
	}
}
