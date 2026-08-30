package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// HospitalRole holds the schema definition for hospital service roles.
//
// Roles (and their permission grants) are GLOBAL by default — tenant_id is NULL for every
// seeded system role. The same "doctor" role carries the same permission set on every tenant.
// Per the platform's shared-core-reference-data convention, a nullable-tenant_id copy-on-write
// override is the sanctioned exception for genuine per-tenant customization: a tenant admin
// "customizing" a global role clones it into a tenant-scoped row (tenant_id set, same
// role_code, cloned_from_role_id pointing at the global parent) rather than mutating the shared
// global row — see rbac.Service.CustomizeRole. A tenant may also create a brand-new,
// from-scratch custom role (tenant_id set, cloned_from_role_id nil). The two partial unique
// indexes below keep global role_codes and per-tenant role_codes in disjoint, collision-free
// scopes, so a clone can reuse its parent's exact role_code with no suffix bookkeeping.
type HospitalRole struct {
	ent.Schema
}

// Fields of the HospitalRole.
func (HospitalRole) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("NULL = global/platform role. Set = tenant-owned clone or from-scratch custom role."),
		field.String("role_code").
			NotEmpty().
			Comment("Role code: admin, doctor, nurse, pharmacist, records_clerk, manager, or a tenant-defined custom code"),
		field.String("name").
			NotEmpty().
			Comment("Display name"),
		field.Text("description").
			Optional(),
		field.Bool("is_system_role").
			Default(false).
			Comment("System roles are the seeded global catalog — never editable/deletable directly (see CustomizeRole)"),
		field.UUID("cloned_from_role_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Set only on a tenant clone created by CustomizeRole — the global role it was cloned from"),
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
		index.Fields("tenant_id"),
		index.Fields("cloned_from_role_id"),
		// Global role codes stay unique platform-wide.
		index.Fields("role_code").Unique().
			Annotations(entsql.IndexWhere("tenant_id IS NULL")),
		// A tenant's own role codes (clones or from-scratch) are unique within that tenant only —
		// disjoint from the global scope above, so a clone can reuse its parent's exact code.
		index.Fields("tenant_id", "role_code").Unique().
			Annotations(entsql.IndexWhere("tenant_id IS NOT NULL")),
		index.Fields("is_system_role"),
	}
}
