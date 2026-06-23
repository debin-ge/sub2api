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

// BenchmarkScoreSnapshot stores aggregated ability and operational metrics.
type BenchmarkScoreSnapshot struct {
	ent.Schema
}

func (BenchmarkScoreSnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_score_snapshots"},
	}
}

func (BenchmarkScoreSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("run_target_id"),
		field.Float("overall_score").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.JSON("dimension_scores", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("planned_tasks").
			Default(0),
		field.Int("scored_tasks").
			Default(0),
		field.Int("invalid_tasks").
			Default(0),
		field.Float("coverage_rate").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.String("confidence_level").
			MaxLen(20).
			Default("low"),
		field.Bool("insufficient_sample").
			Default(false),
		field.Float("success_rate").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.Float("latency_p50_ms").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(12,4)"}),
		field.Float("latency_p95_ms").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(12,4)"}),
		field.Float("avg_total_tokens").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,4)"}),
		field.Float("estimated_cost").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.JSON("invalid_reason_breakdown", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("ranking_metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkScoreSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("score_snapshots").
			Field("run_id").
			Required().
			Unique(),
		edge.From("run_target", BenchmarkRunTarget.Type).
			Ref("score_snapshots").
			Field("run_target_id").
			Required().
			Unique(),
	}
}

func (BenchmarkScoreSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id").
			StorageKey("benchmark_score_snapshots_run_id_idx"),
		index.Fields("run_target_id").
			StorageKey("benchmark_score_snapshots_run_target_id_idx"),
		index.Fields("run_id", "overall_score").
			StorageKey("benchmark_score_snapshots_run_overall_score_idx"),
		index.Fields("run_id", "run_target_id").
			Unique().
			StorageKey("benchmark_score_snapshots_run_target_key"),
	}
}
