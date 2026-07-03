package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BenchmarkTask holds reusable benchmark prompts and verifier configuration.
// Tasks are global and form a fixed set; each run executes all enabled tasks
// (or the first N by sort_order).
type BenchmarkTask struct {
	ent.Schema
}

func (BenchmarkTask) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_tasks"},
	}
}

func (BenchmarkTask) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").
			NotEmpty().
			MaxLen(200),
		field.String("type").
			NotEmpty().
			MaxLen(50),
		field.String("difficulty").
			Optional().
			Nillable().
			MaxLen(50),
		field.String("prompt").
			NotEmpty().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("input_payload", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("expected_output", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("verifier_type").
			NotEmpty().
			MaxLen(50),
		field.JSON("verifier_config", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Float("weight").
			Default(1).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.Bool("public_prompt").
			Default(false),
		field.Bool("enabled").
			Default(true),
		field.Int("sort_order").
			Default(0),
	}
}

func (BenchmarkTask) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("run_tasks", BenchmarkRunTask.Type),
	}
}

func (BenchmarkTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type").
			StorageKey("benchmark_tasks_type_idx"),
		index.Fields("enabled").
			StorageKey("benchmark_tasks_enabled_idx"),
		index.Fields("enabled", "sort_order").
			StorageKey("benchmark_tasks_enabled_sort_idx"),
	}
}
