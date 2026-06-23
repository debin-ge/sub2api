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

// BenchmarkPublicSnapshot stores sanitized public homepage payloads.
type BenchmarkPublicSnapshot struct {
	ent.Schema
}

func (BenchmarkPublicSnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "benchmark_public_snapshots"},
	}
}

func (BenchmarkPublicSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("suite_id"),
		field.Int64("profile_id"),
		field.JSON("snapshot", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("published_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BenchmarkPublicSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BenchmarkRun.Type).
			Ref("public_snapshots").
			Field("run_id").
			Required().
			Unique(),
		edge.From("suite", BenchmarkSuite.Type).
			Ref("public_snapshots").
			Field("suite_id").
			Required().
			Unique(),
		edge.From("profile", BenchmarkProfile.Type).
			Ref("public_snapshots").
			Field("profile_id").
			Required().
			Unique(),
	}
}

func (BenchmarkPublicSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("published_at").
			StorageKey("benchmark_public_snapshots_published_at_idx"),
		index.Fields("run_id").
			StorageKey("benchmark_public_snapshots_run_id_idx"),
	}
}
