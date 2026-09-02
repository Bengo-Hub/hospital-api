package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Bed is one physical bed within a Ward — the unit an Admission occupies (Sprint 6). status is a
// lightweight housekeeping/turnover field (available -> occupied -> cleaning -> available), not a
// full housekeeping module.
type Bed struct{ ent.Schema }

func (Bed) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("ward_id", uuid.UUID{}),
		field.String("bed_number").NotEmpty(),
		field.Enum("status").
			Values("available", "occupied", "cleaning", "out_of_service").
			Default("available"),
		// EquipmentAssetIDs (2026-09-02, Biomedical Equipment integration brought forward from
		// Sprint 9): references to inventory-api's Asset register (e.g. a bed-mounted monitor) —
		// reference only, hospital-api never owns asset data. A JSON list rather than a single
		// field since a bed (especially an ICU bed) commonly has more than one piece of fixed
		// equipment. See docs/architecture.md's asset-integration section.
		field.JSON("equipment_asset_ids", []uuid.UUID{}).Optional().Default([]uuid.UUID{}),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Bed) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ward", Ward.Type).
			Ref("beds").
			Field("ward_id").
			Unique().
			Required(),
		edge.To("admissions", Admission.Type),
	}
}

func (Bed) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "ward_id"),
		index.Fields("tenant_id", "status"),
		index.Fields("ward_id", "bed_number").Unique(),
	}
}
