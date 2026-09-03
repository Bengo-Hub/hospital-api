package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PacuStay is a post-anaesthesia care unit (recovery) stay between a completed TheatreBooking and
// the patient going to a ward/home/ICU — a genuinely distinct clinical stage from both the
// booking itself (which tracks the room/procedure, freed the moment surgery completes so it can
// turn over) and ICUEpisode (most PACU patients are not critically ill; a to_icu disposition
// starts a real ICUEpisode separately rather than reusing this entity for it). Mirrors
// ICUEpisode's own minimal shape. See mvp-gap-backlog-2026-09-02.md's Sprint 7.1 candidates.
type PacuStay struct{ ent.Schema }

func (PacuStay) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("theatre_booking_id", uuid.UUID{}),
		field.String("bay_label").Optional(),
		field.Time("admitted_at").Default(time.Now).Immutable(),
		field.Time("discharged_at").Optional().Nillable(),
		field.Enum("discharge_disposition").
			Values("to_ward", "to_icu", "home", "deceased").
			Optional(),
		field.String("monitoring_notes").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PacuStay) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("theatre_booking", TheatreBooking.Type).
			Ref("pacu_stays").
			Field("theatre_booking_id").
			Unique().
			Required(),
	}
}

func (PacuStay) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "theatre_booking_id"),
	}
}
