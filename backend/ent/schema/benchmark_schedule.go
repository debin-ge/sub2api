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

// BenchmarkSchedule stores disabled-by-default schedule definitions.
type BenchmarkSchedule struct {
	ent.Schema
}

func (BenchmarkSchedule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_schedules"},
	}
}

func (BenchmarkSchedule) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkSchedule) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("profile_id"),
		field.String("name").
			NotEmpty().
			MaxLen(100),
		field.String("cron_expr").
			NotEmpty().
			MaxLen(100),
		field.Bool("enabled").
			Default(false),
		field.Time("last_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("next_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.JSON("metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (BenchmarkSchedule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("profile", BenchmarkProfile.Type).
			Ref("schedules").
			Field("profile_id").
			Required().
			Unique(),
	}
}

func (BenchmarkSchedule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled").
			StorageKey("benchmark_schedules_enabled_idx"),
		index.Fields("next_run_at").
			StorageKey("benchmark_schedules_next_run_at_idx"),
		index.Fields("profile_id").
			StorageKey("benchmark_schedules_profile_id_idx"),
	}
}
