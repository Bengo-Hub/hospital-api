package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// LabTestCatalogEntry holds a tenant's own custom lab tests, additive to the global
// LabTestCatalogDefault set — always tenant-scoped.
type LabTestCatalogEntry struct{ ent.Schema }

func (LabTestCatalogEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
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

func (LabTestCatalogEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").Unique(),
	}
}
