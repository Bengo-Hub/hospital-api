package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// BillableItemCatalog is the tenant-configured price list for the Distributed Billing & Patient
// Accounts model (see docs/architecture.md). Tenant-scoped, not global reference data — pricing
// is a real per-tenant business decision, but each facility tier is seeded with sane defaults at
// provisioning time (see refdata.SeedFacilityBillableItems).
type BillableItemCatalog struct{ ent.Schema }

func (BillableItemCatalog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.Enum("department").
			Values("records", "triage", "consultation", "lab", "pharmacy", "theatre", "inpatient", "mortuary"),
		field.String("code").NotEmpty(),
		field.String("name").NotEmpty(),
		field.Float("price").Optional().Nillable().
			Comment("Nil = priced elsewhere (drugs from inventory-api ItemPricing, lab tests from LabTest.price)"),
		field.Enum("applies_to").Values("first_visit", "return_visit", "all").Default("all"),
		field.Bool("requires_prepayment").Default(false).
			Comment("The originating clinical step blocks until this charge is paid — see billing.RequireSettled"),
		field.Enum("collection_mode").Values("direct", "billing_queue", "either").Default("billing_queue").
			Comment("direct = the originating department may collect itself; billing_queue = Billing desk only; either = both"),
		field.Bool("is_active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BillableItemCatalog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").Unique(),
		index.Fields("tenant_id", "department"),
	}
}
