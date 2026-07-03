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

// BenchmarkResult stores one task x target execution result.
type BenchmarkResult struct {
	ent.Schema
}

func (BenchmarkResult) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_results"},
	}
}

func (BenchmarkResult) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkResult) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("run_task_id"),
		field.Int64("run_target_id"),
		field.String("request_id").
			Optional().
			Nillable().
			MaxLen(64),
		field.String("status").
			NotEmpty().
			MaxLen(32),
		// normalized_score is 0-100; pass=100, fail=0, or a partial score.
		field.Float("normalized_score").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}),
		field.String("evaluator_type").
			Optional().
			Nillable().
			MaxLen(50),
		field.JSON("evaluator_output", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("latency_ms").
			Optional().
			Nillable(),
		field.Int("prompt_tokens").
			Default(0),
		field.Int("completion_tokens").
			Default(0),
		field.Int("total_tokens").
			Default(0),
		field.Float("estimated_cost").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.JSON("raw_response", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("error_code").
			Optional().
			Nillable().
			MaxLen(100),
		field.String("error_message").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int("attempt_count").
			Default(0),
		field.Time("started_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkResult) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("results").
			Field("run_id").
			Required().
			Unique(),
		edge.From("run_task", BenchmarkRunTask.Type).
			Ref("results").
			Field("run_task_id").
			Required().
			Unique(),
		edge.From("run_target", BenchmarkRunTarget.Type).
			Ref("results").
			Field("run_target_id").
			Required().
			Unique(),
	}
}

func (BenchmarkResult) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id").
			StorageKey("benchmark_results_run_id_idx"),
		index.Fields("run_id", "run_target_id").
			StorageKey("benchmark_results_run_target_idx"),
		index.Fields("run_id", "run_task_id").
			StorageKey("benchmark_results_run_task_idx"),
		index.Fields("request_id").
			StorageKey("benchmark_results_request_id_idx"),
		index.Fields("status").
			StorageKey("benchmark_results_status_idx"),
		index.Fields("run_task_id", "run_target_id").
			Unique().
			StorageKey("benchmark_results_run_task_target_key"),
	}
}
