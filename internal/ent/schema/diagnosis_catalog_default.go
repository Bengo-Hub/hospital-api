package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DiagnosisCatalogDefault is the GLOBAL diagnosis reference catalogue — same for every tenant,
// per feedback_shared_core_reference_data.md (reference/catalog data carries no tenant_id).
// Coded ICD-11 (confirmed by the real Taifa Care claim sample payload, see
// docs/integrations.md §2.4 — NOT ICD-10, an earlier doc draft's assumption). Tenant-specific
// additions live in the separate DiagnosisCatalogEntry, never mixed into this table.
type DiagnosisCatalogDefault struct{ ent.Schema }

func (DiagnosisCatalogDefault) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("code").NotEmpty().Comment("ICD-11 code, e.g. 1A00"),
		field.String("name").NotEmpty(),
		field.String("category").Optional(),
		field.Bool("is_active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (DiagnosisCatalogDefault) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
		index.Fields("category"),
	}
}
