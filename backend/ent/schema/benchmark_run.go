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

// BenchmarkRun records one benchmark evaluation batch (one trend data point).
type BenchmarkRun struct {
	ent.Schema
}

func (BenchmarkRun) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_runs"},
	}
}

func (BenchmarkRun) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("status").
			NotEmpty().
			MaxLen(32),
		field.String("trigger_type").
			NotEmpty().
			MaxLen(32),
		field.Int64("schedule_id").
			Optional().
			Nillable(),
		field.Int("task_count").
			Default(0),
		field.Int("planned_target_count").
			Default(0),
		field.Int("planned_task_count").
			Default(0),
		field.Int("planned_result_count").
			Default(0),
		field.Time("started_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("error_message").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("created_by").
			Optional().
			Nillable(),
	}
}

func (BenchmarkRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("run_targets", BenchmarkRunTarget.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("run_tasks", BenchmarkRunTask.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("results", BenchmarkResult.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("target_scores", BenchmarkTargetScore.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("public_snapshots", BenchmarkPublicSnapshot.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BenchmarkRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status").
			StorageKey("benchmark_runs_status_idx"),
		index.Fields("schedule_id").
			StorageKey("benchmark_runs_schedule_id_idx"),
		index.Fields("created_at").
			StorageKey("benchmark_runs_created_at_idx"),
	}
}
