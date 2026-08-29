package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DrugInteractionCheck is an audit-trail record of an interaction/allergy check performed at
// prescribing time — migrated in meaning from pos-api's DrugInteractionCheck. The actual
// interaction engine lives in inventory-api (internal/modules/inventory.Client.CheckInteractions,
// see docs/architecture.md Data Authority table); this table only snapshots the result.
type DrugInteractionCheck struct{ ent.Schema }

func (DrugInteractionCheck) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("prescription_id", uuid.UUID{}).Optional().Nillable(),
		field.JSON("drug_skus", []string{}).Optional(),
		field.String("result").Default("clear").Comment("clear|interactions_found|allergy_match"),
		field.JSON("details", map[string]any{}).Optional().
			Comment("Raw inventory-api CheckInteractions response snapshot"),
		field.UUID("checked_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("checked_at").Default(time.Now).Immutable(),
	}
}

func (DrugInteractionCheck) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "prescription_id"),
	}
}
