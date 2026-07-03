package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BenchmarkTarget holds the model/channel target used by benchmark rankings.
type BenchmarkTarget struct {
	ent.Schema
}

func (BenchmarkTarget) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_targets"},
	}
}

func (BenchmarkTarget) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkTarget) Fields() []ent.Field {
	return []ent.Field{
		field.String("model_name").
			NotEmpty().
			MaxLen(200),
		field.Int64("channel_id"),
		field.String("display_name").
			Optional().
			Nillable().
			MaxLen(200),
		field.String("channel_name_snapshot").
			Optional().
			Nillable().
			MaxLen(200),
		field.Bool("enabled").
			Default(true),
		field.Bool("public_visible").
			Default(true),
		field.Int("sort_order").
			Default(0),
	}
}

func (BenchmarkTarget) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("run_targets", BenchmarkRunTarget.Type),
	}
}

func (BenchmarkTarget) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("model_name", "channel_id").
			Unique().
			StorageKey("benchmark_targets_model_channel_key"),
		index.Fields("enabled", "public_visible").
			StorageKey("benchmark_targets_enabled_public_visible_idx"),
		index.Fields("channel_id").
			StorageKey("benchmark_targets_channel_id_idx"),
	}
}
