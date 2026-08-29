package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PatientNextOfKin is who may settle a bill or authorize a discharge/mortuary release on the
// patient's behalf — a structured, ID-numbered record for audit, distinct from Patient's own
// free-text next_of_kin chart field (Sprint 1). Not a login identity — settling a charge records
// this person's details on the charge/payment for audit, nothing more.
type PatientNextOfKin struct{ ent.Schema }

func (PatientNextOfKin) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("patient_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("phone").Optional(),
		field.String("relationship").Optional(),
		field.String("id_number").Optional(),
		field.Bool("is_primary").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PatientNextOfKin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "patient_id"),
	}
}
