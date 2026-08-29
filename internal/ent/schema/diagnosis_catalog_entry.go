package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DiagnosisCatalogEntry holds a tenant's own custom diagnosis entries, additive to the global
// DiagnosisCatalogDefault set (e.g. a facility-specific presenting complaint code not in the
// standard ICD-11 list they use day to day). Always tenant-scoped — the global defaults live in
// the separate DiagnosisCatalogDefault table, never mixed into this one.
type DiagnosisCatalogEntry struct{ ent.Schema }

func (DiagnosisCatalogEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("code").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("category").Optional(),
		field.Bool("is_active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (DiagnosisCatalogEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").Unique(),
	}
}
