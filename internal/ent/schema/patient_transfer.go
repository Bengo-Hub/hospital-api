package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PatientTransfer records one ward/bed move (intra-facility) or transfer-out to another facility
// (inter-facility, closes the Admission) — added 2026-09-02 after a gap audit found Sprint 6's
// original scope had admit/discharge but no transfer concept. A transfer is always its own row,
// never a silent field overwrite on Admission: billing needs to know which ward a patient occupied
// on which calendar days (see inpatient.Service's ward-charge segmentation at discharge), and
// occupancy/audit history needs the same thing. See docs/architecture.md's "Referral, Transfer &
// Ambulance Billing" section.
type PatientTransfer struct{ ent.Schema }

func (PatientTransfer) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("admission_id", uuid.UUID{}),
		field.Enum("transfer_type").Values("intra_facility", "inter_facility"),
		field.UUID("from_ward_id", uuid.UUID{}),
		field.UUID("from_bed_id", uuid.UUID{}),
		field.UUID("to_ward_id", uuid.UUID{}).Optional().Nillable().
			Comment("Set for intra_facility; nil for inter_facility (the patient leaves this facility's bed register)"),
		field.UUID("to_bed_id", uuid.UUID{}).Optional().Nillable(),
		field.String("receiving_facility_name").Optional().
			Comment("Set for inter_facility transfers"),
		field.UUID("referral_id", uuid.UUID{}).Optional().Nillable().
			Comment("Links back to the Referral that triggered this transfer, when applicable"),
		field.UUID("ambulance_booking_id", uuid.UUID{}).Optional().Nillable().
			Comment("Reference only — the ambulance dispatch itself lives in logistics-api via AmbulanceBooking (Sprint 9)"),
		field.String("reason").Optional(),
		field.UUID("transferred_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("transferred_at").Default(time.Now).Immutable(),
	}
}

func (PatientTransfer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "admission_id"),
		index.Fields("tenant_id", "transferred_at"),
	}
}
