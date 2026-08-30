package rbac

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/hospitalpermission"
	"github.com/bengobox/hospital-service/internal/ent/hospitalrole"
	"github.com/bengobox/hospital-service/internal/ent/predicate"
	"github.com/bengobox/hospital-service/internal/ent/rolepermission"
	"github.com/bengobox/hospital-service/internal/ent/userroleassignment"
)

// EntRepository implements the Repository interface using Ent ORM.
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new Ent-backed repository.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// CreateRole persists a new role (global if role.TenantID is nil, tenant-scoped otherwise).
func (r *EntRepository) CreateRole(ctx context.Context, role *HospitalRole) error {
	if role == nil {
		return errors.New("role cannot be nil")
	}
	builder := r.client.HospitalRole.Create().
		SetID(role.ID).
		SetRoleCode(role.RoleCode).
		SetName(role.Name).
		SetIsSystemRole(role.IsSystemRole).
		SetNillableTenantID(role.TenantID).
		SetNillableClonedFromRoleID(role.ClonedFromRoleID)
	if role.Description != nil {
		builder.SetDescription(*role.Description)
	}
	if _, err := builder.Save(ctx); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	return nil
}

// GetRole retrieves a role by ID.
func (r *EntRepository) GetRole(ctx context.Context, roleID uuid.UUID) (*HospitalRole, error) {
	entRole, err := r.client.HospitalRole.Get(ctx, roleID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("role not found: %w", err)
		}
		return nil, fmt.Errorf("get role: %w", err)
	}
	return mapEntRole(entRole), nil
}

// GetGlobalRoleByCode looks up a role by code, GLOBAL SCOPE ONLY. See the Repository interface
// doc comment for why SeedRoles/CustomizeRole must use this instead of GetRoleByCode.
func (r *EntRepository) GetGlobalRoleByCode(ctx context.Context, roleCode string) (*HospitalRole, error) {
	entRole, err := r.client.HospitalRole.Query().
		Where(hospitalrole.RoleCode(roleCode), hospitalrole.TenantIDIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("global role not found: %s", roleCode)
		}
		return nil, fmt.Errorf("get global role by code: %w", err)
	}
	return mapEntRole(entRole), nil
}

// GetRoleByCode retrieves a role by code for tenantID, preferring the tenant's own clone/custom
// row over the global role when both share the same role_code.
func (r *EntRepository) GetRoleByCode(ctx context.Context, tenantID uuid.UUID, roleCode string) (*HospitalRole, error) {
	rows, err := r.client.HospitalRole.Query().
		Where(
			hospitalrole.RoleCode(roleCode),
			hospitalrole.Or(hospitalrole.TenantIDEQ(tenantID), hospitalrole.TenantIDIsNil()),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get role by code: %w", err)
	}
	var global *ent.HospitalRole
	for _, row := range rows {
		if row.TenantID != nil && *row.TenantID == tenantID {
			return mapEntRole(row), nil
		}
		global = row
	}
	if global == nil {
		return nil, fmt.Errorf("role not found: %s", roleCode)
	}
	return mapEntRole(global), nil
}

// ListRoles returns every role visible to tenantID: the tenant's own rows (clones + custom)
// plus every global role not shadowed by one of the tenant's own clones (same role_code).
func (r *EntRepository) ListRoles(ctx context.Context, tenantID uuid.UUID) ([]*HospitalRole, error) {
	tenantRows, err := r.client.HospitalRole.Query().
		Where(hospitalrole.TenantIDEQ(tenantID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant roles: %w", err)
	}
	shadowed := make(map[string]bool, len(tenantRows))
	roles := make([]*HospitalRole, 0, len(tenantRows))
	for _, row := range tenantRows {
		shadowed[row.RoleCode] = true
		roles = append(roles, mapEntRole(row))
	}

	globalRows, err := r.client.HospitalRole.Query().
		Where(hospitalrole.TenantIDIsNil()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list global roles: %w", err)
	}
	for _, row := range globalRows {
		if shadowed[row.RoleCode] {
			continue
		}
		roles = append(roles, mapEntRole(row))
	}
	return roles, nil
}

// ReplaceRolePermissions atomically replaces roleID's entire permission set.
func (r *EntRepository) ReplaceRolePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("replace role permissions: start tx: %w", err)
	}
	if _, err := tx.RolePermission.Delete().Where(rolepermission.RoleID(roleID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("replace role permissions: clear existing: %w", err)
	}
	for _, permID := range permissionIDs {
		if _, err := tx.RolePermission.Create().SetRoleID(roleID).SetPermissionID(permID).Save(ctx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("replace role permissions: assign %s: %w", permID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace role permissions: commit: %w", err)
	}
	return nil
}

// CreatePermission persists a new global permission.
func (r *EntRepository) CreatePermission(ctx context.Context, permission *HospitalPermission) error {
	if permission == nil {
		return errors.New("permission cannot be nil")
	}
	builder := r.client.HospitalPermission.Create().
		SetID(permission.ID).
		SetPermissionCode(permission.PermissionCode).
		SetName(permission.Name).
		SetModule(permission.Module).
		SetAction(permission.Action)
	if permission.Resource != nil {
		builder.SetResource(*permission.Resource)
	}
	if permission.Description != nil {
		builder.SetDescription(*permission.Description)
	}
	if _, err := builder.Save(ctx); err != nil {
		return fmt.Errorf("create permission: %w", err)
	}
	return nil
}

// GetPermission retrieves a permission by ID.
func (r *EntRepository) GetPermission(ctx context.Context, permissionID uuid.UUID) (*HospitalPermission, error) {
	entPerm, err := r.client.HospitalPermission.Get(ctx, permissionID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("permission not found: %w", err)
		}
		return nil, fmt.Errorf("get permission: %w", err)
	}
	return mapEntPermission(entPerm), nil
}

// GetPermissionByCode retrieves a permission by code.
func (r *EntRepository) GetPermissionByCode(ctx context.Context, permissionCode string) (*HospitalPermission, error) {
	entPerm, err := r.client.HospitalPermission.Query().
		Where(hospitalpermission.PermissionCode(permissionCode)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("permission not found: %w", err)
		}
		return nil, fmt.Errorf("get permission by code: %w", err)
	}
	return mapEntPermission(entPerm), nil
}

// ListPermissions lists permissions with optional filters.
func (r *EntRepository) ListPermissions(ctx context.Context, filters PermissionFilters) ([]*HospitalPermission, error) {
	query := r.client.HospitalPermission.Query()
	if filters.Module != nil {
		query = query.Where(hospitalpermission.Module(*filters.Module))
	}
	if filters.Action != nil {
		query = query.Where(hospitalpermission.Action(*filters.Action))
	}
	entPerms, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	permissions := make([]*HospitalPermission, len(entPerms))
	for i, entPerm := range entPerms {
		permissions[i] = mapEntPermission(entPerm)
	}
	return permissions, nil
}

// AssignPermissionToRole assigns a permission to a role.
func (r *EntRepository) AssignPermissionToRole(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error {
	_, err := r.client.RolePermission.Create().
		SetRoleID(roleID).
		SetPermissionID(permissionID).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("assign permission to role: %w", err)
	}
	return nil
}

// RemovePermissionFromRole removes a permission from a role.
func (r *EntRepository) RemovePermissionFromRole(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error {
	_, err := r.client.RolePermission.Delete().
		Where(
			rolepermission.RoleID(roleID),
			rolepermission.PermissionID(permissionID),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove permission from role: %w", err)
	}
	return nil
}

// GetRolePermissions retrieves all permissions for a role.
func (r *EntRepository) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]*HospitalPermission, error) {
	entPerms, err := r.client.HospitalRole.Query().
		Where(hospitalrole.ID(roleID)).
		QueryPermissions().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get role permissions: %w", err)
	}
	permissions := make([]*HospitalPermission, len(entPerms))
	for i, entPerm := range entPerms {
		permissions[i] = mapEntPermission(entPerm)
	}
	return permissions, nil
}

// AssignRoleToUser assigns a role to a user within a tenant.
func (r *EntRepository) AssignRoleToUser(ctx context.Context, tenantID uuid.UUID, assignment *UserRoleAssignment) error {
	if assignment == nil {
		return errors.New("assignment cannot be nil")
	}
	builder := r.client.UserRoleAssignment.Create().
		SetID(assignment.ID).
		SetTenantID(tenantID).
		SetUserID(assignment.UserID).
		SetRoleID(assignment.RoleID).
		SetAssignedBy(assignment.AssignedBy)
	if assignment.ExpiresAt != nil {
		builder.SetExpiresAt(*assignment.ExpiresAt)
	}
	if _, err := builder.Save(ctx); err != nil {
		return fmt.Errorf("assign role to user: %w", err)
	}
	return nil
}

// RevokeRoleFromUser revokes a role from a user.
func (r *EntRepository) RevokeRoleFromUser(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID, roleID uuid.UUID) error {
	_, err := r.client.UserRoleAssignment.Delete().
		Where(
			userroleassignment.TenantID(tenantID),
			userroleassignment.UserID(userID),
			userroleassignment.RoleID(roleID),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("revoke role from user: %w", err)
	}
	return nil
}

// notExpired filters out assignments whose expires_at has passed — the single choke point
// GetUserRoles/ListUserAssignments both apply, so every permission-check and
// assignment-idempotency path automatically stops honoring an expired grant without a separate
// background sweep job.
func notExpired() predicate.UserRoleAssignment {
	return userroleassignment.Or(
		userroleassignment.ExpiresAtIsNil(),
		userroleassignment.ExpiresAtGT(time.Now()),
	)
}

// GetUserRoles retrieves all NON-EXPIRED roles assigned to a user within a tenant.
func (r *EntRepository) GetUserRoles(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) ([]*HospitalRole, error) {
	entRoles, err := r.client.UserRoleAssignment.Query().
		Where(
			userroleassignment.TenantID(tenantID),
			userroleassignment.UserID(userID),
			notExpired(),
		).
		QueryRole().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	roles := make([]*HospitalRole, len(entRoles))
	for i, entRole := range entRoles {
		roles[i] = mapEntRole(entRole)
	}
	return roles, nil
}

// GetUserPermissions retrieves all permissions for a user (via their roles).
func (r *EntRepository) GetUserPermissions(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) ([]*HospitalPermission, error) {
	roles, err := r.GetUserRoles(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	permissionMap := make(map[uuid.UUID]*HospitalPermission)
	for _, role := range roles {
		rolePerms, err := r.GetRolePermissions(ctx, role.ID)
		if err != nil {
			continue
		}
		for _, perm := range rolePerms {
			permissionMap[perm.ID] = perm
		}
	}
	permissions := make([]*HospitalPermission, 0, len(permissionMap))
	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}
	return permissions, nil
}

// ListUserAssignments lists NON-EXPIRED role assignments with optional filters. Callers that
// idempotency-check "is this role already assigned" (AssignRoleByCode, AssignRole) rely on this
// excluding expired rows too — otherwise an expired grant would look "still assigned" and block
// a legitimate renewal.
func (r *EntRepository) ListUserAssignments(ctx context.Context, tenantID uuid.UUID, filters AssignmentFilters) ([]*UserRoleAssignment, error) {
	query := r.client.UserRoleAssignment.Query().
		Where(userroleassignment.TenantID(tenantID), notExpired())
	if filters.UserID != nil {
		query = query.Where(userroleassignment.UserID(*filters.UserID))
	}
	if filters.RoleID != nil {
		query = query.Where(userroleassignment.RoleID(*filters.RoleID))
	}
	entAssignments, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list user assignments: %w", err)
	}
	assignments := make([]*UserRoleAssignment, len(entAssignments))
	for i, entAssignment := range entAssignments {
		assignments[i] = mapEntAssignment(entAssignment)
	}
	return assignments, nil
}

// RepointRoleAssignments moves every UserRoleAssignment in tenantID from fromRoleID to
// toRoleID. Safe by construction: toRoleID is always a brand-new clone with no pre-existing
// assignments at call time, so this can never collide with the (tenant_id, user_id, role_id)
// composite unique index.
func (r *EntRepository) RepointRoleAssignments(ctx context.Context, tenantID, fromRoleID, toRoleID uuid.UUID) error {
	_, err := r.client.UserRoleAssignment.Update().
		Where(userroleassignment.TenantID(tenantID), userroleassignment.RoleID(fromRoleID)).
		SetRoleID(toRoleID).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("repoint role assignments: %w", err)
	}
	return nil
}

// Mapping functions

func mapEntRole(entRole *ent.HospitalRole) *HospitalRole {
	role := &HospitalRole{
		ID:               entRole.ID,
		TenantID:         entRole.TenantID,
		RoleCode:         entRole.RoleCode,
		Name:             entRole.Name,
		IsSystemRole:     entRole.IsSystemRole,
		ClonedFromRoleID: entRole.ClonedFromRoleID,
		CreatedAt:        entRole.CreatedAt,
		UpdatedAt:        entRole.UpdatedAt,
	}
	if entRole.Description != "" {
		role.Description = &entRole.Description
	}
	return role
}

func mapEntPermission(entPerm *ent.HospitalPermission) *HospitalPermission {
	perm := &HospitalPermission{
		ID:             entPerm.ID,
		PermissionCode: entPerm.PermissionCode,
		Name:           entPerm.Name,
		Module:         entPerm.Module,
		Action:         entPerm.Action,
		CreatedAt:      entPerm.CreatedAt,
	}
	if entPerm.Resource != "" {
		perm.Resource = &entPerm.Resource
	}
	if entPerm.Description != "" {
		perm.Description = &entPerm.Description
	}
	return perm
}

func mapEntAssignment(entAssignment *ent.UserRoleAssignment) *UserRoleAssignment {
	assignment := &UserRoleAssignment{
		ID:         entAssignment.ID,
		TenantID:   entAssignment.TenantID,
		UserID:     entAssignment.UserID,
		RoleID:     entAssignment.RoleID,
		AssignedBy: entAssignment.AssignedBy,
		AssignedAt: entAssignment.AssignedAt,
	}
	if entAssignment.ExpiresAt != nil {
		assignment.ExpiresAt = entAssignment.ExpiresAt
	}
	return assignment
}
