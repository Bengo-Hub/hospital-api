package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// HospitalRole holds the schema definition for hospital service roles.
//
// Roles (and their permission grants) are GLOBAL — deliberately no tenant_id column.
// The same "doctor" role carries the same permission set on every tenant; per the
// platform's shared-core-reference-data convention, only the assignment of a role to a
// user is tenant-scoped (see UserRoleAssignment). This keeps the role catalog a single
// seeded, platform-maintained table instead of N per-tenant copies to keep in sync.
type HospitalRole struct {
	ent.Schema
}

// Fields of the HospitalRole.
func (HospitalRole) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("role_code").
			NotEmpty().
			Unique().
			Comment("Role code: admin, doctor, nurse, pharmacist, records_clerk, manager"),
		field.String("name").
			NotEmpty().
			Comment("Display name"),
		field.Text("description").
			Optional(),
		field.Bool("is_system_role").
			Default(false).
			Comment("System roles cannot be deleted"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the HospitalRole.
func (HospitalRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("permissions", HospitalPermission.Type).Through("role_permissions", RolePermission.Type),
		edge.From("user_assignments", UserRoleAssignment.Type).Ref("role"),
	}
}

// Indexes of the HospitalRole.
func (HospitalRole) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role_code").Unique(),
		index.Fields("is_system_role"),
	}
}
