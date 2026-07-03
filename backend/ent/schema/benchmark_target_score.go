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

// BenchmarkTargetScore stores the aggregated ability score for one run x target.
// It doubles as the trend data point: rows carry redundant model_name/channel_id
// and finished_at so a target's score history is a simple ordered query.
type BenchmarkTargetScore struct {
	ent.Schema
}

func (BenchmarkTargetScore) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_target_scores"},
	}
}

func (BenchmarkTargetScore) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("run_target_id"),
		field.String("model_name").
			NotEmpty().
			MaxLen(200),
		field.Int64("channel_id"),
		// overall_score is the weighted pass rate (0-100), i.e. the IQ index.
		field.Float("overall_score").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.Int("passed_count").
			Default(0),
		field.Int("total_count").
			Default(0),
		field.JSON("dimension_scores", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Float("avg_latency_ms").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(12,4)"}),
		field.Float("avg_total_tokens").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,4)"}),
		field.Float("total_cost").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.JSON("invalid_reason_breakdown", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("finished_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkTargetScore) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("target_scores").
			Field("run_id").
			Required().
			Unique(),
		edge.From("run_target", BenchmarkRunTarget.Type).
			Ref("target_scores").
			Field("run_target_id").
			Required().
			Unique(),
	}
}

func (BenchmarkTargetScore) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id").
			StorageKey("benchmark_target_scores_run_id_idx"),
		index.Fields("run_id", "overall_score").
			StorageKey("benchmark_target_scores_run_overall_score_idx"),
		index.Fields("model_name", "channel_id", "finished_at").
			StorageKey("benchmark_target_scores_trend_idx"),
		index.Fields("run_id", "run_target_id").
			Unique().
			StorageKey("benchmark_target_scores_run_target_key"),
	}
}
