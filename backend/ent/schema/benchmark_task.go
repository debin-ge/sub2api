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
		field.Int64("suite_id"),
		field.String("title").
			NotEmpty().
			MaxLen(200),
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
		field.JSON("tags", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
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
		field.Int("min_scale").
			Default(1),
		field.Bool("public_prompt").
			Default(false),
		field.Bool("enabled").
			Default(true),
		field.JSON("metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (BenchmarkTask) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("suite", BenchmarkSuite.Type).
			Ref("tasks").
			Field("suite_id").
			Required().
			Unique(),
		edge.To("run_tasks", BenchmarkRunTask.Type),
	}
}

func (BenchmarkTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("suite_id"),
		index.Fields("type"),
		index.Fields("difficulty"),
		index.Fields("enabled"),
		index.Fields("suite_id", "type", "enabled"),
	}
}
