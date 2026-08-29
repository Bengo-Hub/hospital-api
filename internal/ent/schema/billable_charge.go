package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// BillableCharge is one charge event against a PatientAccount — posted by whichever department
// billed it (records/triage/consultation/lab/pharmacy/theatre/inpatient). treasury_invoice_id
// is filled in once ANY authorized department actually collects payment for it — see
// docs/architecture.md "Distributed Billing & Patient Accounts".
type BillableCharge struct{ ent.Schema }

func (BillableCharge) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("patient_account_id", uuid.UUID{}),
		field.UUID("billable_item_id", uuid.UUID{}).Optional().Nillable().
			Comment("Nil = a free-form charge not tied to a BillableItemCatalog row"),
		field.String("source_module").NotEmpty().
			Comment("records|triage|consultation|lab|pharmacy|theatre|inpatient — which department billed this"),
		field.UUID("source_reference_id", uuid.UUID{}).Optional().Nillable().
			Comment("The LabOrder/Prescription/Admission/etc. that generated this charge"),
		field.String("description").NotEmpty(),
		field.Float("amount").Positive(),
		field.Enum("status").Values("pending", "invoiced", "paid", "exempted", "waived", "written_off").Default("pending").
			Comment("exempted (added 2026-08-29) = an insurance claim covered this charge in full — distinct from waived (facility chose not to charge), see docs/architecture.md 'Distributed Billing & Patient Accounts'"),
		field.UUID("treasury_invoice_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("treasury_payment_intent_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("created_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("paid_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BillableCharge) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", PatientAccount.Type).
			Ref("charges").
			Field("patient_account_id").
			Unique().
			Required(),
	}
}

func (BillableCharge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "patient_account_id"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "source_module"),
	}
}
