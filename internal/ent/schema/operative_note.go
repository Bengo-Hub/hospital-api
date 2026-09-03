package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OperativeNote is the structured surgical/operative report authored after a procedure completes
// — one-to-one with a TheatreBooking, additive and separate from its own scheduling fields (a
// long, free-standing clinical document with its own author/timing, per JCAHO/AAAHC operative-
// report documentation standards). See mvp-gap-backlog-2026-09-02.md's Sprint 7.1 candidates.
type OperativeNote struct{ ent.Schema }

func (OperativeNote) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("theatre_booking_id", uuid.UUID{}).Unique(),
		field.UUID("surgeon_id", uuid.UUID{}).Optional().Nillable(),
		field.String("procedure_performed").NotEmpty(),
		field.String("findings").Optional(),
		field.String("complications").Optional(),
		field.Float("estimated_blood_loss_ml").Optional().Nillable(),
		field.String("implants_used").Optional(),
		field.Bool("specimens_sent").Default(false),
		field.String("specimens_description").Optional(),
		field.String("post_op_diagnosis").Optional(),
		field.UUID("authored_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("authored_at").Default(time.Now).Immutable(),
	}
}

func (OperativeNote) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("theatre_booking", TheatreBooking.Type).
			Ref("operative_note").
			Field("theatre_booking_id").
			Unique().
			Required(),
	}
}

func (OperativeNote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "theatre_booking_id").Unique(),
	}
}
