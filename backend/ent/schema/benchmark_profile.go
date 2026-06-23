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

// BenchmarkProfile stores a reusable run configuration template.
type BenchmarkProfile struct {
	ent.Schema
}

func (BenchmarkProfile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_profiles"},
	}
}

func (BenchmarkProfile) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkProfile) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("suite_id"),
		field.String("name").
			NotEmpty().
			MaxLen(100),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("target_ids", []int64{}).
			Default(func() []int64 { return []int64{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("task_types", []string{}).
			Default(func() []string { return []string{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("task_scale").
			MaxLen(20).
			Default("medium"),
		field.Int("task_count_limit").
			Optional().
			Nillable(),
		field.JSON("per_type_limit", map[string]int{}).
			Default(func() map[string]int { return map[string]int{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("difficulty_filter", []string{}).
			Default(func() []string { return []string{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("tag_filter", []string{}).
			Default(func() []string { return []string{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("sampling_strategy").
			NotEmpty().
			MaxLen(50),
		field.Int64("selection_seed").
			Optional().
			Nillable(),
		field.JSON("runtime_config", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("scoring_config", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Bool("enabled").
			Default(true),
		field.JSON("metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (BenchmarkProfile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("suite", BenchmarkSuite.Type).
			Ref("profiles").
			Field("suite_id").
			Required().
			Unique(),
		edge.To("schedules", BenchmarkSchedule.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("runs", BenchmarkRun.Type),
		edge.To("public_snapshots", BenchmarkPublicSnapshot.Type),
	}
}

func (BenchmarkProfile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("suite_id").
			StorageKey("benchmark_profiles_suite_id_idx"),
		index.Fields("enabled").
			StorageKey("benchmark_profiles_enabled_idx"),
	}
}
