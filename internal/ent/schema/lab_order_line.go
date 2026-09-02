package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// LabOrderLine is one requested test within a LabOrder. test_code/test_name/price are a
// SNAPSHOT from the catalogue at order time (same reasoning as ExaminationRecord's
// diagnosis_code/diagnosis_name snapshot, Sprint 2) — a later catalogue price change must never
// retroactively alter what was already billed.
type LabOrderLine struct{ ent.Schema }

func (LabOrderLine) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("lab_order_id", uuid.UUID{}),
		field.String("test_code").NotEmpty(),
		field.String("test_name").NotEmpty(),
		field.Float("price").Default(0),
		field.String("specimen_type").Optional(),
		field.Time("specimen_collected_at").Optional().Nillable(),
		field.UUID("specimen_collected_by", uuid.UUID{}).Optional().Nillable(),
		field.String("specimen_id").Optional().
			Comment("Barcode/label string identifying the physical specimen — reintroduces the concept the removed 'collected' LabOrder.status value gestured at, this time backed by real fields and a real Collect call site"),
		field.String("result_value").Optional(),
		field.String("unit").Optional(),
		field.String("reference_range").Optional(),
		field.Enum("flag").Values("pending", "normal", "abnormal", "critical").Default("pending"),
		field.String("notes").Optional(),
		field.UUID("resulted_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("resulted_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (LabOrderLine) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("lab_order", LabOrder.Type).
			Ref("lines").
			Field("lab_order_id").
			Unique().
			Required(),
	}
}

func (LabOrderLine) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "lab_order_id"),
		index.Fields("tenant_id", "flag"),
	}
}
