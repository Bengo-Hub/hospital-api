package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PrescriptionLine is one drug line within a Prescription. inventory_item_id/drug_name are
// resolved from inventory-api at prescribe time (drug master data lives there, never
// duplicated — see docs/architecture.md Data Authority table); lot_number/expiry_date are
// filled from inventory-api's ConsumeReservation response at dispense time (its 2026-08-29 FEFO
// fix), not hand-typed by the pharmacist.
type PrescriptionLine struct{ ent.Schema }

func (PrescriptionLine) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("prescription_id", uuid.UUID{}),
		field.String("inventory_item_sku").Optional().
			Comment("inventory-api item SKU — nil for a free-text line the pharmacist typed manually"),
		field.String("drug_name").NotEmpty(),
		field.String("dosage").Optional(),
		field.String("form").Optional().Comment("tablet, capsule, syrup, injection..."),
		field.String("instructions").Optional(),
		field.Float("quantity_prescribed").Positive(),
		field.Float("quantity_dispensed").Default(0),
		field.Float("unit_price").Default(0),
		field.String("lot_number").Optional().Comment("Filled from inventory-api's ConsumeReservation at dispense"),
		field.Time("expiry_date").Optional().Nillable(),
		field.Enum("status").Values("pending", "dispensed", "partially_dispensed", "cancelled").Default("pending"),
	}
}

func (PrescriptionLine) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("prescription", Prescription.Type).
			Ref("lines").
			Field("prescription_id").
			Unique().
			Required(),
		edge.To("medication_administrations", MedicationAdministration.Type),
	}
}

func (PrescriptionLine) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "prescription_id"),
	}
}
