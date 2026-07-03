package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BenchmarkSchedule stores disabled-by-default cron schedules. Each schedule
// carries its own run configuration (which targets, how many tasks) instead of
// referencing a profile.
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
		field.String("name").
			NotEmpty().
			MaxLen(100),
		field.String("cron_expr").
			NotEmpty().
			MaxLen(100),
		field.Bool("enabled").
			Default(false),
		// target_ids is empty to mean "all enabled targets".
		field.JSON("target_ids", []int64{}).
			Default(func() []int64 { return []int64{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		// task_count is 0 to mean "all enabled tasks", else first N by sort_order.
		field.Int("task_count").
			Default(0),
		field.Time("last_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("next_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkSchedule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled").
			StorageKey("benchmark_schedules_enabled_idx"),
		index.Fields("next_run_at").
			StorageKey("benchmark_schedules_next_run_at_idx"),
	}
}
