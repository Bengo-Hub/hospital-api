package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WardRoundNote is a doctor's daily ward-round progress note during an inpatient stay — closely
// related to VitalsChartEntry but a different author (clinician, not nurse), cadence (once/twice a
// day, not per-shift), and content (free-text clinical reasoning + an optional diagnosis update,
// not structured vitals). Kept as its own entity rather than folded into VitalsChartEntry for
// exactly that reason. Conceptually ExaminationRecord's shape, reapplied to an ongoing admission
// instead of a single OPD consultation. See mvp-gap-backlog-2026-09-02.md's Sprint 6.1 candidates.
type WardRoundNote struct{ ent.Schema }

func (WardRoundNote) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("admission_id", uuid.UUID{}),
		field.UUID("clinician_id", uuid.UUID{}).Comment("References auth-service user id"),
		field.String("notes").NotEmpty(),
		field.String("diagnosis_code").Optional().
			Comment("Snapshotted at note time, same catalog ExaminationRecord references — not a live FK"),
		field.String("diagnosis_name").Optional(),
		field.Time("recorded_at").Default(time.Now).Immutable(),
	}
}

func (WardRoundNote) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("admission", Admission.Type).
			Ref("ward_round_notes").
			Field("admission_id").
			Unique().
			Required(),
	}
}

func (WardRoundNote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "admission_id"),
	}
}
