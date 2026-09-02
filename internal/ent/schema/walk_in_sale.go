package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WalkInSale is a Chemist-tier till transaction — the ledgerless counterpart to
// PatientAccount/BillableCharge for a prescription with no patient_id/visit_id. A Chemist
// tenant is feature-gated off ever creating a Patient/Visit (see hospChemistCore() in
// subscriptions-api), so pharmacy.Service.Dispense creates one WalkInSale per dispense action
// for its nil-patient/nil-visit lines instead of posting to the patient ledger — same reference
// pattern pos-api's POSOrder already uses for a walk-in customer (free-text name, no FK to a
// customer entity), per docs/architecture.md "Distributed Billing & Patient Accounts": "Chemist
// -> Billing module is just Walk-in Sale, no PatientAccount complexity at all." Payment is
// still collected through the same treasury-api invoice/payment-intent primitive
// billing.Service.CollectCharge uses — only the local ledger bookkeeping is skipped.
type WalkInSale struct{ ent.Schema }

func (WalkInSale) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("prescription_id", uuid.UUID{}),
		field.String("prescription_number").NotEmpty(),
		field.String("sale_number").NotEmpty().Comment("Sequence-generated per tenant (kind=walk_in_sale_number)"),
		field.String("patient_name").Optional().Comment("Free-text walk-in name, same as Prescription.patient_name — no FK, mirrors pos-api's POSOrder.customer_name"),
		field.JSON("line_items", []map[string]any{}).Optional().
			Comment("Write-once itemized snapshot (drug_name, sku, quantity, unit_price, line_total) for receipt display — never individually queried/mutated"),
		field.Float("amount").Positive(),
		field.Enum("status").Values("pending", "paid", "waived").Default("pending"),
		field.String("payment_method").Optional().Comment("mpesa|card|cash|bank_transfer|paystack — set once collected"),
		field.UUID("treasury_invoice_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("treasury_payment_intent_id", uuid.UUID{}).Optional().Nillable(),
		field.String("etims_invoice_number").Optional(),
		field.String("etims_qr_code_url").Optional(),
		field.UUID("collected_by", uuid.UUID{}).Optional().Nillable(),
		field.UUID("created_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("paid_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (WalkInSale) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("prescription", Prescription.Type).
			Ref("walk_in_sales").
			Field("prescription_id").
			Unique().
			Required(),
	}
}

func (WalkInSale) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "sale_number").Unique(),
		index.Fields("tenant_id", "prescription_id"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "created_at"),
	}
}
