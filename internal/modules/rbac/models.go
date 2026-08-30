package rbac

import (
	"time"

	"github.com/google/uuid"
)

// HospitalRole represents a hospital service role. Roles are GLOBAL by default (TenantID nil)
// — the same role code carries the same permission grants on every tenant. TenantID is set only
// on a tenant's own clone (copy-on-write customization of a global role) or a from-scratch
// custom role — see rbac.Service.CustomizeRole/CreateCustomRole.
type HospitalRole struct {
	ID               uuid.UUID
	TenantID         *uuid.UUID
	RoleCode         string
	Name             string
	Description      *string
	IsSystemRole     bool
	ClonedFromRoleID *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HospitalPermission represents a hospital service permission. Also global.
type HospitalPermission struct {
	ID             uuid.UUID
	PermissionCode string
	Name           string
	Module         string
	Action         string
	Resource       *string
	Description    *string
	CreatedAt      time.Time
}

// UserRoleAssignment represents a tenant-scoped user role assignment — the only RBAC
// relation that carries a tenant_id.
type UserRoleAssignment struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	RoleID     uuid.UUID
	AssignedBy uuid.UUID
	AssignedAt time.Time
	ExpiresAt  *time.Time
}
