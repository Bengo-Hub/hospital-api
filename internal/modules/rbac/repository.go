package rbac

import (
	"context"

	"github.com/google/uuid"
)

// Repository abstracts persistence for RBAC entities. Role/Permission operations are
// global (no tenantID) — only the user-role assignment operations are tenant-scoped.
type Repository interface {
	// Role operations (global)
	CreateRole(ctx context.Context, role *HospitalRole) error
	GetRole(ctx context.Context, roleID uuid.UUID) (*HospitalRole, error)
	GetRoleByCode(ctx context.Context, roleCode string) (*HospitalRole, error)
	ListRoles(ctx context.Context) ([]*HospitalRole, error)

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
