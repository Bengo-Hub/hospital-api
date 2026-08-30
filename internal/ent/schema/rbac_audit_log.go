package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// RbacAuditLog records identity/RBAC mutations within this module only (role assigned/changed,
// role created/customized/edited, user status changed). Deliberately named and scoped narrower
// than Sprint 12's eventual full compliance-grade audit_log (patient/clinical/consent actions
// platform-wide) so the two can coexist without a naming collision, or be folded together later
// if that sprint decides to. No FK edges on purpose — a row must stay inspectable even after its
// target user/role is later hard-deleted.
type RbacAuditLog struct {
	ent.Schema
}

// Fields of the RbacAuditLog.
func (RbacAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Tenant identifier"),
		field.UUID("actor_user_id", uuid.UUID{}).
			Comment("auth-service user ID of the actor — same convention as assigned_by/currentUserID(r) platform-wide, never HospitalUser.ID"),
		field.String("actor_email").
			Optional(),
		field.String("action").
			NotEmpty().
			Comment("role.assigned | role.created | role.customized | role.permissions_updated | role.granted_extra | role.revoked_extra | user.status_changed"),
		field.String("target_type").
			NotEmpty().
			Comment("user | role"),
		field.UUID("target_id", uuid.UUID{}),
		field.JSON("before", map[string]any{}).
			Optional(),
		field.JSON("after", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the RbacAuditLog.
func (RbacAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
		index.Fields("target_type", "target_id"),
		index.Fields("actor_user_id"),
	}
}
