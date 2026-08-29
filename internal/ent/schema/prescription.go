package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Prescription is the pharmacy dispensing header — migrated in meaning from pos-api's
// Prescription (see migration-pos-pharmacy.md), the core entity of the migration. patient_id/
// visit_id stay OPTIONAL (unlike Sprint 1-3's required visit_id) deliberately: a full OPD flow
// always has both (created from an ExaminationRecord/Referral), but a standalone chemist's
// walk-in sale has neither — the free-text patient_name/dob/id_number fields (pos-api's original
// design) cover that case. metadata carries the same free-form audit trail pos-api used
// (interaction_check_id, approved_by/at, reservation_id, cancel_reason, ...) rather than adding
// a column per field — additive, no migration needed for new keys later.
type Prescription struct{ ent.Schema }

func (Prescription) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("visit_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("examination_id", uuid.UUID{}).Optional().Nillable().
			Comment("The consultation this prescription was written from, when there is one"),
		field.String("external_facility_name").Optional().
			Comment("Set when dispensing against a prescription written elsewhere (chemist walk-in)"),
		field.String("prescription_number").NotEmpty().Comment("Sequence-generated per tenant"),
		field.String("prescriber_name").Optional(),
		field.String("prescriber_license").Optional(),
		field.String("patient_name").Optional().Comment("Free-text fallback when patient_id is nil (walk-in)"),
		field.Time("patient_dob").Optional().Nillable(),
		field.String("patient_id_number").Optional(),
		field.Enum("status").
			Values("pending", "pharmacist_review", "flagged", "approved", "locked",
				"partially_dispensed", "dispensed", "rejected", "cancelled").
			Default("pending"),
		field.String("notes").Optional(),
		field.Time("dispensed_at").Optional().Nillable(),
		field.UUID("dispensed_by", uuid.UUID{}).Optional().Nillable(),
		field.JSON("metadata", map[string]any{}).Optional().
			Comment("allergy_flags, interaction_check_id, approved_by/at, reservation_id, cancel_reason, approval_override_reason"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Prescription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("lines", PrescriptionLine.Type),
	}
}

func (Prescription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "prescription_number").Unique(),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "patient_id"),
		index.Fields("tenant_id", "visit_id"),
	}
}
