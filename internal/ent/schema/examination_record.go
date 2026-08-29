package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ExaminationRecord is the doctor/dental/MCH/specialist consultation record for a PatientVisit.
// diagnosis_code/diagnosis_name are a SNAPSHOT of the chosen catalogue entry at examination
// time, not a live FK into DiagnosisCatalogDefault/Entry — a later edit or deactivation of the
// catalogue entry must never retroactively change what a patient's chart says they were
// diagnosed with, and a plain FK can't cleanly point at "either of two different tables" anyway.
type ExaminationRecord struct{ ent.Schema }

func (ExaminationRecord) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("visit_id", uuid.UUID{}),
		field.UUID("clinician_id", uuid.UUID{}).Comment("References auth-service user id"),
		field.Enum("queue_type").Values("doctor", "dental", "mch", "specialist").Default("doctor"),
		field.String("chief_complaint").Optional().Comment("Refined from the visit's intake complaint"),
		field.String("diagnosis_code").Optional().Comment("Snapshotted ICD-11 code at examination time"),
		field.String("diagnosis_name").Optional(),
		field.String("notes").Optional(),
		field.Enum("status").Values("in_progress", "awaiting_lab", "completed").Default("in_progress"),
		field.Time("examined_at").Default(time.Now).Immutable(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (ExaminationRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("visit", PatientVisit.Type).
			Ref("examination_records").
			Field("visit_id").
			Unique().
			Required(),
	}
}

func (ExaminationRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "visit_id"),
		index.Fields("tenant_id", "status"),
	}
}
