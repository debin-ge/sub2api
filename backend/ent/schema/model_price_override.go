// Package schema 定义 Ent ORM 的数据库 schema。
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

// ModelPriceOverride 模型价格手动覆盖。
//
// 删除策略：硬删除。覆盖行被删除即表示“回落到自动同步值”，
// 变更历史由审计日志承载，无需在本表保留墓碑。
type ModelPriceOverride struct {
	ent.Schema
}

func (ModelPriceOverride) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "model_price_overrides"},
	}
}

func (ModelPriceOverride) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (ModelPriceOverride) Fields() []ent.Field {
	return []ent.Field{
		field.String("platform").MaxLen(50).NotEmpty(),
		field.String("model_name").MaxLen(200).NotEmpty(),
		field.JSON("payload", map[string]any{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Bool("enabled").Default(true),
		field.Text("note").Optional().Nillable(),
		field.Int64("updated_by").Optional().Nillable(),
	}
}

func (ModelPriceOverride) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("platform", "model_name").Unique(),
	}
}
