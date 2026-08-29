package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TriageRecord captures the vitals + acuity taken for a PatientVisit before consultation. A
// visit may be re-triaged (vitals recheck) — rows are append-only, the latest by taken_at is
// authoritative. Migrated in meaning from pos-api's TriageRecord (see migration-pos-pharmacy.md).
type TriageRecord struct{ ent.Schema }

func (TriageRecord) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("visit_id", uuid.UUID{}),
		field.UUID("taken_by", uuid.UUID{}).Comment("Staff member who took vitals"),
		field.Int("bp_systolic").Optional().Nillable(),
		field.Int("bp_diastolic").Optional().Nillable(),
		field.Float("temperature_celsius").Optional().Nillable(),
		field.Int("pulse_bpm").Optional().Nillable(),
		field.Int("respiration_rate").Optional().Nillable(),
		field.Float("spo2_percent").Optional().Nillable(),
		field.Float("weight_kg").Optional().Nillable(),
		field.Float("height_cm").Optional().Nillable(),
		field.String("priority").Optional().
			Comment("ESI-style acuity level (1=most urgent .. 5=least), free-form so a facility can adopt its own scale"),
		field.String("notes").Optional(),
		field.Time("taken_at").Default(time.Now).Immutable(),
	}
}

func (TriageRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("visit", PatientVisit.Type).
			Ref("triage_records").
			Field("visit_id").
			Unique().
			Required(),
	}
}

func (TriageRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "visit_id"),
	}
}
