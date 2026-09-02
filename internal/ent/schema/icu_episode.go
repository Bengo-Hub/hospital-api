package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ICUEpisode is a critical-care monitoring episode for an already-admitted patient (Sprint 7,
// Afya Hospital tier). Billing for the elevated ICU rate is NOT modeled here — it flows through
// the ordinary Sprint 6 mechanism: an ICU bed lives in a Ward whose own billable_item_code names a
// higher day-rate (e.g. BED_DAY_ICU vs BED_DAY_GENERAL), so discharge-time billing already charges
// correctly with zero ICU-specific billing logic. bed_id is a snapshot of the admission's bed at
// episode start — it is NOT auto-updated if the admission later transfers elsewhere without
// explicitly ending this episode first (a documented staff-workflow expectation, not a system
// invariant enforced in code).
type ICUEpisode struct{ ent.Schema }

func (ICUEpisode) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("admission_id", uuid.UUID{}),
		field.UUID("bed_id", uuid.UUID{}),
		field.Enum("severity_flag").Values("stable", "guarded", "critical").Default("stable"),
		field.String("monitoring_notes").Optional(),
		// EquipmentAssetIDs (2026-09-02, Biomedical Equipment integration brought forward from
		// Sprint 9): references to inventory-api's Asset register (e.g. a ventilator) attached to
		// this episode — reference only. See docs/architecture.md.
		field.JSON("equipment_asset_ids", []uuid.UUID{}).Optional().Default([]uuid.UUID{}),
		field.UUID("started_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("started_at").Default(time.Now).Immutable(),
		field.Time("ended_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ICUEpisode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "admission_id"),
		index.Fields("tenant_id", "bed_id"),
	}
}
