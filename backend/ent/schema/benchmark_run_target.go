package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BenchmarkRunTarget stores the per-run target snapshot.
type BenchmarkRunTarget struct {
	ent.Schema
}

func (BenchmarkRunTarget) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_run_targets"},
	}
}

func (BenchmarkRunTarget) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("target_id"),
		field.String("model_name").
			NotEmpty().
			MaxLen(200),
		field.Int64("channel_id"),
		field.String("display_name_snapshot").
			Optional().
			Nillable().
			MaxLen(200),
		field.String("channel_name_snapshot").
			Optional().
			Nillable().
			MaxLen(200),
		field.String("provider_snapshot").
			Optional().
			Nillable().
			MaxLen(100),
		field.Int("target_order").
			Default(0),
		field.JSON("config_snapshot", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkRunTarget) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("run_targets").
			Field("run_id").
			Required().
			Unique(),
		edge.From("target", BenchmarkTarget.Type).
			Ref("run_targets").
			Field("target_id").
			Required().
			Unique(),
		edge.To("results", BenchmarkResult.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("score_snapshots", BenchmarkScoreSnapshot.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BenchmarkRunTarget) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id"),
		index.Fields("target_id"),
		index.Fields("run_id", "channel_id"),
	}
}
