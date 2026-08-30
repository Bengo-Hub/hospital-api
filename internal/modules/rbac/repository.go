package rbac

import (
	"context"

	"github.com/google/uuid"
)

// Repository abstracts persistence for RBAC entities. Role/Permission operations are global by
// default (nil tenantID) — see HospitalRole's own doc comment for the tenant-clone exception.
// Only the user-role assignment operations are unconditionally tenant-scoped.
type Repository interface {
	// Role operations
	CreateRole(ctx context.Context, role *HospitalRole) error
	GetRole(ctx context.Context, roleID uuid.UUID) (*HospitalRole, error)
	// GetGlobalRoleByCode looks up a role by code, GLOBAL SCOPE ONLY (tenant_id IS NULL). Used
	// only by SeedRoles and by CustomizeRole's "find the source role" step — using the
	// tenant-aware GetRoleByCode here would risk matching/mutating a tenant's own shadow clone
	// instead of the platform template.
	GetGlobalRoleByCode(ctx context.Context, roleCode string) (*HospitalRole, error)
	// GetRoleByCode looks up a role by code for tenantID, preferring the tenant's own clone over
	// the global role when both exist under the same code.
	GetRoleByCode(ctx context.Context, tenantID uuid.UUID, roleCode string) (*HospitalRole, error)
	// ListRoles returns every role visible to tenantID: the tenant's own rows (clones + custom)
	// plus every global role not shadowed by one of the tenant's own clones.
	ListRoles(ctx context.Context, tenantID uuid.UUID) ([]*HospitalRole, error)
	// ReplaceRolePermissions atomically replaces roleID's entire permission set.
	ReplaceRolePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error
	// RepointRoleAssignments moves every UserRoleAssignment in tenantID currently pointing at
	// fromRoleID to toRoleID instead. Used by CustomizeRole so a role clone takes effect for
	// already-assigned staff immediately, not just future assignments.
	RepointRoleAssignments(ctx context.Context, tenantID, fromRoleID, toRoleID uuid.UUID) error

	// Permission operations (global)
	CreatePermission(ctx context.Context, permission *HospitalPermission) error
	GetPermission(ctx context.Context, permissionID uuid.UUID) (*HospitalPermission, error)
	GetPermissionByCode(ctx context.Context, permissionCode string) (*HospitalPermission, error)
	ListPermissions(ctx context.Context, filters PermissionFilters) ([]*HospitalPermission, error)

	// Role-Permission operations
	AssignPermissionToRole(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error
	RemovePermissionFromRole(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error
	GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]*HospitalPermission, error)

	// User-Role assignment operations (tenant-scoped)
	AssignRoleToUser(ctx context.Context, tenantID uuid.UUID, assignment *UserRoleAssignment) error
	RevokeRoleFromUser(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID, roleID uuid.UUID) error
	GetUserRoles(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) ([]*HospitalRole, error)
	GetUserPermissions(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) ([]*HospitalPermission, error)
	ListUserAssignments(ctx context.Context, tenantID uuid.UUID, filters AssignmentFilters) ([]*UserRoleAssignment, error)
}

// PermissionFilters for listing permissions.
type PermissionFilters struct {
	Module *string
	Action *string
}

// AssignmentFilters for listing role assignments.
type AssignmentFilters struct {
	UserID *uuid.UUID
	RoleID *uuid.UUID
}
