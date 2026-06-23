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

// BenchmarkSuite groups a benchmark task library and public radar surface.
type BenchmarkSuite struct {
	ent.Schema
}

func (BenchmarkSuite) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_suites"},
	}
}

func (BenchmarkSuite) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BenchmarkSuite) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			MaxLen(100),
		field.String("slug").
			NotEmpty().
			MaxLen(100),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("enabled").
			Default(true),
		field.Bool("public_visible").
			Default(false),
		field.Int64("default_profile_id").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (BenchmarkSuite) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("default_profile", BenchmarkProfile.Type).
			Field("default_profile_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("tasks", BenchmarkTask.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("profiles", BenchmarkProfile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("runs", BenchmarkRun.Type),
		edge.To("public_snapshots", BenchmarkPublicSnapshot.Type),
	}
}

func (BenchmarkSuite) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").
			Unique().
			StorageKey("benchmark_suites_slug_key"),
		index.Fields("enabled", "public_visible").
			StorageKey("benchmark_suites_enabled_public_visible_idx"),
		index.Fields("default_profile_id").
			StorageKey("benchmark_suites_default_profile_id_idx"),
	}
}
