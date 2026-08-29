package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// LabTestCatalogDefault is the GLOBAL lab-test reference catalogue (common panels — FBC,
// malaria smear, urinalysis, etc.) — same for every tenant, per
// feedback_shared_core_reference_data.md. Tenant-specific additions live in the separate
// LabTestCatalogEntry, never mixed into this table.
type LabTestCatalogDefault struct{ ent.Schema }

func (LabTestCatalogDefault) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("code").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("specimen_type").Optional(),
		field.String("reference_range").Optional(),
		field.String("unit").Optional(),
		field.Int("turnaround_hours").Optional(),
		field.Float("price").Default(0),
		field.Bool("is_active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (LabTestCatalogDefault) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
	}
}
