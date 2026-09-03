package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// VitalsChartEntry is a nursing vitals reading during an inpatient stay — a repeated time series
// (multiple readings per shift, over a multi-day admission) tied to the admission rather than the
// visit, deliberately NOT a reuse of TriageRecord (Sprint 2's OPD-intake-only, one-shot-per-visit
// acuity capture with no notion of "which reading in the series is this"). See
// mvp-gap-backlog-2026-09-02.md's Sprint 6.1 candidates. Same vitals shape as TriageRecord for
// consistency, plus pain_score, which OPD triage has no use for.
type VitalsChartEntry struct{ ent.Schema }

func (VitalsChartEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("admission_id", uuid.UUID{}),
		field.UUID("recorded_by", uuid.UUID{}).Comment("Charting nurse — references auth-service user id"),
		field.Int("bp_systolic").Optional().Nillable(),
		field.Int("bp_diastolic").Optional().Nillable(),
		field.Float("temperature_celsius").Optional().Nillable(),
		field.Int("pulse_bpm").Optional().Nillable(),
		field.Int("respiration_rate").Optional().Nillable(),
		field.Float("spo2_percent").Optional().Nillable(),
		field.Int("pain_score").Optional().Nillable().Comment("0-10 self-reported pain scale"),
		field.String("notes").Optional(),
		field.Time("recorded_at").Default(time.Now).Immutable(),
	}
}

func (VitalsChartEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("admission", Admission.Type).
			Ref("vitals_chart_entries").
			Field("admission_id").
			Unique().
			Required(),
	}
}

func (VitalsChartEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "admission_id"),
	}
}
