package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PatientAccount is the running billing ledger for a patient — one per OPD visit, or one
// spanning an entire inpatient admission (Sprint 6 sets admission_id). hospital-api owns this
// ledger (what's charged/settled); treasury-api still owns every actual financial document —
// treasury_invoice_id lives on BillableCharge, not here, since a single account can span many
// separately-collected charges. See docs/architecture.md "Distributed Billing & Patient
// Accounts".
type PatientAccount struct{ ent.Schema }

func (PatientAccount) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}),
		field.UUID("visit_id", uuid.UUID{}).Optional().Nillable().
			Comment("Set for an OPD visit's own account; nil for an inpatient account spanning admission_id"),
		field.UUID("admission_id", uuid.UUID{}).Optional().Nillable().
			Comment("Set once Sprint 6 opens an inpatient admission on this account"),
		field.Enum("status").Values("open", "settled", "written_off").Default("open"),
		field.Float("total_charged").Default(0),
		field.Float("total_paid").Default(0),
		field.Float("balance").Default(0),
		field.Enum("settlement_required_before").
			Values("nothing", "discharge", "body_release").Default("nothing"),
		field.UUID("next_of_kin_id", uuid.UUID{}).Optional().Nillable().
			Comment("Who settled this account on the patient's behalf, if not the patient themselves"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PatientAccount) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("charges", BillableCharge.Type),
	}
}

func (PatientAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "patient_id"),
		index.Fields("tenant_id", "visit_id"),
		index.Fields("tenant_id", "status"),
	}
}
