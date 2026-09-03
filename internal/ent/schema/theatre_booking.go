package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TheatreBooking is one surgery-scheduling record (Sprint 7, Afya Hospital tier). Reuses the
// "resource booking + status lifecycle" shape pos-api's Facility/FacilityBooking already
// established platform-wide, but is clinically owned here, not pos-api. theatre_room is a plain
// label (not a foreign-keyed resource registry) — conflict detection is done in
// theatre.Service by comparing [scheduled_at, scheduled_at+duration) windows for the same room,
// not a DB constraint. The THEATRE_FEE BillableItemCatalog entry has no fixed price (procedure
// fees vary too widely to price generically) and requires_prepayment=true, so fee_amount is
// captured explicitly at booking time and snapshotted here — same reasoning as
// LabOrderLine/PrescriptionLine's own catalogue-value snapshots.
type TheatreBooking struct{ ent.Schema }

func (TheatreBooking) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.UUID("patient_visit_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}),
		field.String("theatre_room").NotEmpty(),
		field.String("surgery_type").NotEmpty(),
		field.UUID("surgeon_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("scheduled_at"),
		field.Int("duration_minutes").Default(60),
		field.Enum("status").
			Values("awaiting_payment", "scheduled", "in_progress", "completed", "cancelled").
			Default("scheduled"),
		field.JSON("checklist", map[string]bool{}).Optional().Default(map[string]bool{}).
			Comment("Pre-op checklist items — normalized as JSON rather than a rigid new table per item"),
		field.Float("fee_amount").Optional().Nillable().
			Comment("Snapshotted procedure fee at booking time; nil = no charge posted (tenant hasn't priced this booking)"),
		// EquipmentAssetIDs (2026-09-02, Biomedical Equipment integration brought forward from
		// Sprint 9): references to inventory-api's Asset register (e.g. an anaesthesia machine)
		// reserved for this booking — reference only. See docs/architecture.md.
		field.JSON("equipment_asset_ids", []uuid.UUID{}).Optional().Default([]uuid.UUID{}),
		field.UUID("created_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TheatreBooking) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("staff_assignments", TheatreStaffAssignment.Type),
		edge.To("pacu_stays", PacuStay.Type),
		edge.To("operative_note", OperativeNote.Type).Unique(),
	}
}

func (TheatreBooking) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "patient_visit_id"),
		index.Fields("tenant_id", "theatre_room"),
		index.Fields("tenant_id", "status"),
	}
}
