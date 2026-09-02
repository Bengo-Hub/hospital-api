package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// MedicationAdministration is the Medication Administration Record (MAR) — the legal record of
// what a nurse actually administered to an admitted patient, distinct from what pharmacy
// dispensed (Prescription/PrescriptionLine, Sprint 4). An admitted patient's whole multi-day
// course is dispensed to the ward in one pharmacy transaction; this entity tracks each individual
// dose event against that dispense, charted on-demand by a nurse rather than pre-scheduled — this
// codebase has no dosing-frequency data model (PrescriptionLine.instructions is free text) to
// auto-generate a schedule of future dose slots from, so "chart a dose" is the MVP unit of work,
// not "check off a pre-populated slot." Deliberately its own entity, not a repurposed
// PrescriptionLine or TriageRecord row (see mvp-gap-backlog-2026-09-02.md Sprint 4 item 1): a
// nurse's administration act is a different author, timing, and clinical question ("was THIS dose
// given") from the pharmacist's dispense.
type MedicationAdministration struct{ ent.Schema }

func (MedicationAdministration) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("admission_id", uuid.UUID{}),
		field.UUID("prescription_line_id", uuid.UUID{}),
		field.Time("scheduled_time").Default(time.Now).
			Comment("When this dose was due — defaults to charting time for the common on-time case, settable for a late-logged dose"),
		field.Time("administered_at").Optional().Nillable().
			Comment("Set only when status=given — a refused/missed/held dose has no real administration time"),
		field.UUID("administered_by", uuid.UUID{}).Optional().Nillable().
			Comment("The charting nurse — set regardless of status, since a refusal/miss/hold is still an observed, attributable event"),
		field.Enum("status").Values("scheduled", "given", "refused", "missed", "held").Default("scheduled"),
		field.String("notes").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (MedicationAdministration) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("admission", Admission.Type).
			Ref("medication_administrations").
			Field("admission_id").
			Unique().
			Required(),
		edge.From("prescription_line", PrescriptionLine.Type).
			Ref("medication_administrations").
			Field("prescription_line_id").
			Unique().
			Required(),
	}
}

func (MedicationAdministration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "admission_id"),
		index.Fields("tenant_id", "prescription_line_id"),
	}
}
