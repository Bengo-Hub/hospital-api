package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Referral records a hand-off from one stage/department of a visit to another (Consultation ->
// Lab, Consultation -> Pharmacy, or out to another facility). New in hospital-api — pos-api had
// no equivalent, since its OPD chain never needed a formal referral record.
type Referral struct{ ent.Schema }

func (Referral) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("visit_id", uuid.UUID{}),
		field.String("referred_to").NotEmpty().Comment("lab|pharmacy|external_facility|specialist"),
		field.String("reason").Optional(),
		field.String("status").Default("pending").Comment("pending|acted_on|cancelled"),
		field.UUID("referred_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Referral) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("visit", PatientVisit.Type).
			Ref("referrals").
			Field("visit_id").
			Unique().
			Required(),
	}
}

func (Referral) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "visit_id"),
		index.Fields("tenant_id", "status"),
	}
}
