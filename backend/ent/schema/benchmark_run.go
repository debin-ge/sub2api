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

// BenchmarkRun records one benchmark evaluation batch.
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
		field.Int64("suite_id"),
		field.Int64("profile_id"),
		field.String("status").
			NotEmpty().
			MaxLen(32),
		field.String("trigger_type").
			NotEmpty().
			MaxLen(32),
		field.Int("task_scale").
			Default(1),
		field.JSON("task_types", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int64("selection_seed").
			Optional().
			Nillable(),
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
		field.JSON("config_snapshot", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
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
		edge.From("suite", BenchmarkSuite.Type).
			Ref("runs").
			Field("suite_id").
			Required().
			Unique(),
		edge.From("profile", BenchmarkProfile.Type).
			Ref("runs").
			Field("profile_id").
			Required().
			Unique(),
		edge.To("run_targets", BenchmarkRunTarget.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("run_tasks", BenchmarkRunTask.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("results", BenchmarkResult.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("score_snapshots", BenchmarkScoreSnapshot.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("public_snapshots", BenchmarkPublicSnapshot.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BenchmarkRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("suite_id"),
		index.Fields("profile_id"),
		index.Fields("status"),
		index.Fields("created_at"),
	}
}
