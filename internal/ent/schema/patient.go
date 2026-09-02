package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Patient is the OPD/IPD patient registration record — the spine every other clinical module
// (Triage/Examination/Lab/Prescription/Admission) attaches to via PatientVisit. Migrated in
// meaning from pos-service/pos-api's Patient schema (see migration-pos-pharmacy.md), not
// copy-pasted: this is the only place patient records live on the platform going forward.
type Patient struct{ ent.Schema }

func (Patient) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}).Comment("Registering outlet"),
		field.String("mrn").NotEmpty().Comment("Medical record number, sequence-generated per tenant"),
		field.String("full_name").NotEmpty(),
		field.Time("dob").Optional().Nillable(),
		field.String("sex").Optional(),
		field.String("phone").Optional(),
		field.String("id_number").Optional().Comment("National ID / passport"),
		field.String("identification_type").Optional().
			Comment("national_id|passport|birth_certificate|maisha_number|alien_id — which scheme id_number came from, null on legacy rows"),
		field.String("sha_beneficiary_number").Optional().
			Comment("SHA/SHIF beneficiary number, captured once at registration and auto-populated into billing.CheckEligibility's fields map so it never needs re-typing"),
		field.String("photo_url").Optional().
			Comment("Visual-ID aid for chart confirmation at a busy front desk — not a biometric credential"),
		field.UUID("household_id", uuid.UUID{}).Optional().Nillable().
			Comment("Pointer only, to a head-of-household Patient row — no edge, mirrors crm_contact_id's loose-reference style. Nothing consumes this yet; added now so Sprint 10 (ANC/PNC) doesn't need a retrofit"),
		field.String("address").Optional(),
		field.String("next_of_kin").Optional().
			Comment("Free-text quick-reference field for the chart — distinct from PatientNextOfKin (Sprint 5), the structured record used to authorize a bill settlement/discharge"),
		field.JSON("allergy_flags", []string{}).Optional().Default([]string{}).
			Comment("Patient-level allergy flags, checked at examination/prescribing time"),
		field.String("client_registry_id").Optional().
			Comment("DHA/Taifa Care Client Registry ID (CR...), reserved so treasury-api's claims never need a second lookup — see docs/integrations.md §2.4"),
		field.UUID("crm_contact_id", uuid.UUID{}).Optional().Nillable().
			Comment("Pointer only — marketflow/CRM remains the source of truth for contact identity, no PII duplication"),
		field.String("status").Default("active").
			Comment("active|inactive|merged — Kenya DPA mandates a 20-year retention minimum, patients are never hard-deleted"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Patient) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("visits", PatientVisit.Type),
	}
}

func (Patient) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "mrn").Unique(),
		index.Fields("tenant_id", "phone"),
		index.Fields("tenant_id", "id_number"),
		index.Fields("tenant_id", "full_name"),
	}
}
