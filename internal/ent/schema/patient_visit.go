package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PatientVisit is one episode of care — Reception opens it at check-in, and it carries the
// patient through Triage -> Consultation -> (optional) Lab -> Pharmacy as one linked journey.
// Migrated in meaning from pos-api's PatientVisit (see migration-pos-pharmacy.md). Unlike
// pos-api's version, this schema carries NO forward reference to a payment/order — the
// registration fee is posted as a BillableCharge (Sprint 5) whose source_reference_id points
// BACK at this visit, not the other way around, so Sprint 1 has no forward dependency on the
// billing schema landing first.
type PatientVisit struct{ ent.Schema }

func (PatientVisit) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}),
		field.String("visit_number").NotEmpty().Comment("Sequence-generated per tenant"),
		field.Enum("visit_type").Values("OPD", "IPD").Default("OPD"),
		field.Enum("status").
			Values("registered", "triaged", "in_examination", "awaiting_lab", "lab_complete",
				"prescribed", "dispensed", "admitted", "completed", "cancelled").
			Default("registered"),
		field.String("chief_complaint").Optional().Comment("Captured at registration; refined at examination"),
		field.UUID("registered_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("checked_in_at").Default(time.Now).Immutable(),
		field.Time("discharged_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PatientVisit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("patient", Patient.Type).
			Ref("visits").
			Field("patient_id").
			Unique().
			Required(),
		edge.To("triage_records", TriageRecord.Type),
		edge.To("referrals", Referral.Type),
	}
}

func (PatientVisit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "visit_number").Unique(),
		index.Fields("tenant_id", "patient_id"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "outlet_id"),
	}
}
