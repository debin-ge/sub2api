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

// BenchmarkRunTask stores the per-run task snapshot.
type BenchmarkRunTask struct {
	ent.Schema
}

func (BenchmarkRunTask) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_run_tasks"},
	}
}

func (BenchmarkRunTask) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("task_id"),
		field.Int("task_order").
			Default(0),
		field.String("type").
			NotEmpty().
			MaxLen(50),
		field.String("category").
			Optional().
			Nillable().
			MaxLen(100),
		field.String("difficulty").
			Optional().
			Nillable().
			MaxLen(50),
		field.Float("weight_snapshot").
			Default(1).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.String("prompt_snapshot").
			NotEmpty().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("verifier_type_snapshot").
			NotEmpty().
			MaxLen(50),
		field.JSON("verifier_config_snapshot", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("task_snapshot", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkRunTask) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("run_tasks").
			Field("run_id").
			Required().
			Unique(),
		edge.From("task", BenchmarkTask.Type).
			Ref("run_tasks").
			Field("task_id").
			Required().
			Unique(),
		edge.To("results", BenchmarkResult.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BenchmarkRunTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id"),
		index.Fields("task_id"),
		index.Fields("run_id", "type"),
	}
}
