package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ControlledSubstanceLog is the dual-witness dispensing register for scheduled/controlled
// drugs — migrated in meaning from pos-api's ControlledSubstanceLog. This is the ONLY place
// this regulatory record lives on the platform now (see migration-pos-pharmacy.md).
type ControlledSubstanceLog struct{ ent.Schema }

func (ControlledSubstanceLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("prescription_id", uuid.UUID{}).Optional().Nillable(),
		field.String("item_sku").NotEmpty(),
		field.String("item_name").NotEmpty(),
		field.Float("quantity_dispensed").Positive(),
		field.UUID("dispensed_by", uuid.UUID{}),
		field.String("patient_name").Optional(),
		field.String("patient_id_number").Optional(),
		field.UUID("witness_staff_id", uuid.UUID{}).Optional().Nillable().
			Comment("The second, independent staff member who co-signs this controlled-substance dispense"),
		field.String("notes").Optional(),
		field.String("lot_number").Optional(),
		field.Time("lot_expiry_date").Optional().Nillable(),
		field.Time("dispensed_at").Default(time.Now).Immutable(),
	}
}

func (ControlledSubstanceLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "prescription_id"),
		index.Fields("tenant_id", "dispensed_at"),
	}
}
