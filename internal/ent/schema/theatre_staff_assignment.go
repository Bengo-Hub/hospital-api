package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TheatreStaffAssignment names one staff member's role on a theatre booking's surgical team —
// added as its own entity rather than more named columns on TheatreBooking (assistant_surgeon_id,
// anaesthetist_id, ...) so team size/role mix scales without another migration whenever a case
// needs, say, two assistant surgeons. TheatreBooking.surgeon_id stays exactly as shipped for
// backward compatibility and the common "who's operating" glance/list query — a booking with no
// assignment rows is treated as having exactly one implied "surgeon" row, synthesized from
// surgeon_id at read time. See mvp-gap-backlog-2026-09-02.md's Sprint 7.1 candidates.
type TheatreStaffAssignment struct{ ent.Schema }

func (TheatreStaffAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("theatre_booking_id", uuid.UUID{}),
		field.UUID("staff_user_id", uuid.UUID{}).Comment("References auth-service user id"),
		field.Enum("role").
			Values("surgeon", "assistant_surgeon", "anaesthetist", "scrub_nurse", "circulating_nurse", "other"),
		field.Time("assigned_at").Default(time.Now).Immutable(),
	}
}

func (TheatreStaffAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("theatre_booking", TheatreBooking.Type).
			Ref("staff_assignments").
			Field("theatre_booking_id").
			Unique().
			Required(),
	}
}

func (TheatreStaffAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "theatre_booking_id"),
		index.Fields("tenant_id", "staff_user_id"),
	}
}
