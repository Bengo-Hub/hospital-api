package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// LabOrder is one test-ordering event for a PatientVisit. Migrated in meaning from pos-api's
// LabOrder — but unlike pos-api's version (which forward-referenced a payment_order_id on
// itself), payment status is resolved by checking the visit's PatientAccount charges (Sprint 5),
// same reasoning as PatientVisit's own no-forward-reference design in Sprint 1.
type LabOrder struct{ ent.Schema }

func (LabOrder) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("visit_id", uuid.UUID{}),
		field.UUID("examination_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("ordered_by", uuid.UUID{}),
		// "collected" was removed 2026-09-02 — confirmed dead (never set by any service method;
		// EnterResult's only real gating condition is requested/resulted, per the frontend's own
		// LabOrderStatus type) and zero live rows used it before removal.
		field.Enum("status").
			Values("requested", "awaiting_payment", "resulted", "cancelled").
			Default("requested"),
		field.String("notes").Optional(),
		field.Time("ordered_at").Default(time.Now).Immutable(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (LabOrder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("visit", PatientVisit.Type).
			Ref("lab_orders").
			Field("visit_id").
			Unique().
			Required(),
		edge.To("lines", LabOrderLine.Type),
	}
}

func (LabOrder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "visit_id"),
		index.Fields("tenant_id", "status"),
	}
}
