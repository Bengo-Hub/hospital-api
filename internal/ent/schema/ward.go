package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Ward is a physical ward/unit (e.g. "General Ward", "Maternity", "ICU") that groups Beds for
// occupancy tracking and admission (Sprint 6). See docs/erd.md's Inpatient section and
// docs/sprints/sprint-6-inpatient.md. The pricing model's Afya Clinic "Inpatient add-on" and Afya
// Facility/Hospital's core inpatient management are the SAME schema, gated by subscriptions-api's
// inpatient_module feature — a small clinic just runs fewer wards/beds.
type Ward struct{ ent.Schema }

func (Ward) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("outlet_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.Enum("ward_type").Values("general", "private", "semi_private", "isolation", "icu").
			Optional().
			Comment("Classification only — does not replace billable_item_code, which still prices this specific ward. Lets the UI group/filter wards and suggest (not force) a sensible default billable_item_code when a new ward is created"),
		field.Int("capacity").Default(0).
			Comment("Informational headcount target; a bed-occupancy view counts real Bed rows, not this"),
		field.String("billable_item_code").Optional().Nillable().
			Comment("BillableItemCatalog code (department=inpatient) pricing a day in THIS ward, e.g. BED_DAY_ICU vs BED_DAY_GENERAL. Nil falls back to the tenant's default WARD_DAY_RATE code — see inpatient.Service's discharge-time ward-charge computation."),
		field.Bool("is_active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Ward) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("beds", Bed.Type),
	}
}

func (Ward) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "outlet_id"),
		index.Fields("tenant_id", "outlet_id", "name").Unique(),
	}
}
