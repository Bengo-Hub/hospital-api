package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Admission is the admission-to-discharge record for one inpatient stay (Sprint 6). Creates its
// own PatientAccount (admission_id set, see patient_account.go) — every department's charges
// during the stay accrue onto this SAME account rather than each posting a separate mini-invoice,
// per docs/architecture.md "Distributed Billing & Patient Accounts". One active (non-discharged)
// admission per PatientVisit at a time is enforced at the service layer, not a DB constraint.
type Admission struct{ ent.Schema }

func (Admission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("patient_visit_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}),
		field.String("admission_number").NotEmpty().
			Comment("Sequence-generated per tenant (sequence.Next, kind=admission_number) — the folio/reference number"),
		field.UUID("ward_id", uuid.UUID{}),
		field.UUID("bed_id", uuid.UUID{}),
		field.Enum("status").Values("active", "discharged").Default("active"),
		field.UUID("admitted_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("admitted_at").Default(time.Now).Immutable(),
		field.Time("discharged_at").Optional().Nillable(),
		field.UUID("discharged_by", uuid.UUID{}).Optional().Nillable(),
		field.String("discharge_summary").Optional(),
		field.Bool("ward_charge_posted").Default(false).
			Comment("Guards against double-posting the final ward/day-rate charge across repeated discharge attempts while balance is still outstanding"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Admission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("visit", PatientVisit.Type).
			Ref("admissions").
			Field("patient_visit_id").
			Unique().
			Required(),
		edge.From("bed", Bed.Type).
			Ref("admissions").
			Field("bed_id").
			Unique().
			Required(),
		edge.To("medication_administrations", MedicationAdministration.Type),
	}
}

func (Admission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "patient_visit_id"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "ward_id"),
		index.Fields("tenant_id", "bed_id"),
		index.Fields("tenant_id", "admission_number").Unique(),
	}
}
