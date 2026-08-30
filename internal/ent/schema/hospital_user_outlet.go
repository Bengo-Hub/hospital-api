package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// HospitalUserOutlet links a HospitalUser to the outlets they are assigned to. Mirrors
// inventory-api's UserOutlet / pos-api's StaffOutlet (the fleet's proven per-user outlet-
// access-control pattern) — a user may be assigned to multiple outlets, and a non-HQ/non-admin
// user may only operate within an outlet they are assigned to (enforced in
// internal/http/middleware/outlet_context.go).
//
// user_id is the LOCAL HospitalUser.ID (not the auth-service user id) — consistent with
// UserRoleAssignment's own convention, unlike inventory-api's UserOutlet (which predates
// hospital-api's 2026-08-30 tenant-scoped-identity fix and still stores the raw auth id).
//
// Outlets are owned by auth-service (hospital-api mirrors them via internal/ent/schema/outlet.go),
// so outlet_id is a bare UUID rather than an ent edge — consistent with how outlet_id is stored
// elsewhere in this service.
type HospitalUserOutlet struct {
	ent.Schema
}

// Fields of the HospitalUserOutlet.
func (HospitalUserOutlet) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}).
			Comment("Local HospitalUser.ID"),
		field.UUID("outlet_id", uuid.UUID{}).
			Comment("Outlet id owned by auth-service"),
		field.Bool("is_home_outlet").Default(false).
			Comment("True for the user's primary outlet"),
		field.UUID("assigned_by", uuid.UUID{}).Optional().Nillable().
			Comment("auth-service user id of the admin who made the assignment, or nil for an auto-reconciled row"),
		field.Time("assigned_at").Default(time.Now).Immutable(),
	}
}

// Indexes of the HospitalUserOutlet.
func (HospitalUserOutlet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "user_id", "outlet_id").Unique(),
		index.Fields("tenant_id", "outlet_id"),
		index.Fields("tenant_id", "user_id"),
	}
}
