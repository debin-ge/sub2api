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

type PaymentWebhookDelivery struct {
	ent.Schema
}

func (PaymentWebhookDelivery) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "payment_webhook_deliveries"},
	}
}

func (PaymentWebhookDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider_key").
			MaxLen(30).
			NotEmpty(),
		field.String("delivery_id").
			MaxLen(128).
			NotEmpty(),
		field.String("event_type").
			MaxLen(100).
			Default(""),
		field.Bool("test_notification").
			Default(false),
		field.String("status").
			MaxLen(30).
			Default("received"),
		field.String("raw_body_hash").
			MaxLen(64).
			Default(""),
		field.String("error").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("received_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("processed_at").
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

func (PaymentWebhookDelivery) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_key", "delivery_id").
			Unique(),
		index.Fields("provider_key", "received_at"),
		index.Fields("status"),
	}
}
