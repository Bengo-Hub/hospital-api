package rbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service provides business logic for RBAC operations.
type Service struct {
	repo   Repository
	logger *zap.Logger
}

// NewService creates a new RBAC service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{repo: repo, logger: logger}
}

// MapGlobalRolesToServiceRole maps global SSO roles to a hospital-api service role code.
// Order matters: the first (most privileged) match wins.
func MapGlobalRolesToServiceRole(roles []string) string {
	order := []struct{ from, to string }{
		{"superuser", RoleAdmin}, {"super_admin", RoleAdmin}, {"admin", RoleAdmin},
		{"manager", RoleManager},
		{"doctor", RoleDoctor}, {"clinician", RoleDoctor}, {"physician", RoleDoctor},
		{"nurse", RoleNurse},
		{"pharmacist", RolePharmacist},
		{"records_clerk", RoleRecordsClerk}, {"receptionist", RoleRecordsClerk},
		{"cashier", RoleCashier},
	}
	for _, m := range order {
		for _, r := range roles {
			if r == m.from {
				return m.to
			}
		}
	}
	return ""
}

// AssignRoleByCode idempotently assigns the named global role to a user within a tenant.
// If the role hasn't been seeded yet, it seeds the global RBAC catalog once and retries —
// this is the JIT self-heal path so an admin/doctor/etc. is never permanently stuck with
// zero permissions just because SeedRoles never ran for this deployment.
func (s *Service) AssignRoleByCode(ctx context.Context, tenantID uuid.UUID, localUserID, authUserID uuid.UUID, roleCode string) error {
	if roleCode == "" {
		return nil
	}
	role, err := s.repo.GetRoleByCode(ctx, roleCode)
	if err != nil {
		if seedErr := s.SeedRoles(ctx); seedErr != nil {
			s.logger.Warn("AssignRoleByCode: seed retry failed", zap.String("role_code", roleCode), zap.Error(seedErr))
		}
		role, err = s.repo.GetRoleByCode(ctx, roleCode)
		if err != nil {
			return fmt.Errorf("role %q not found (even after seed retry): %w", roleCode, err)
		}
	}

	// Idempotent: skip if already assigned.
	assignments, err := s.repo.ListUserAssignments(ctx, tenantID, AssignmentFilters{
		UserID: &localUserID,
		RoleID: &role.ID,
	})
	if err == nil && len(assignments) > 0 {
		return nil
	}

	assignment := &UserRoleAssignment{
		ID:         uuid.New(),
		TenantID:   tenantID,
		UserID:     localUserID,
		RoleID:     role.ID,
		AssignedBy: authUserID,
	}
	if err := s.repo.AssignRoleToUser(ctx, tenantID, assignment); err != nil {
		return fmt.Errorf("assign role to user: %w", err)
	}
	s.logger.Info("JIT assigned hospital role",
		zap.String("role_code", roleCode),
		zap.String("user_id", localUserID.String()),
		zap.String("tenant_id", tenantID.String()))
	return nil
}

// HasPermission checks if a user holds a specific permission code (directly, or via a
// wildcard "*" role grant).
func (s *Service) HasPermission(ctx context.Context, tenantID, userID uuid.UUID, permissionCode string) (bool, error) {
	return s.HasAnyPermission(ctx, tenantID, userID, permissionCode)
}

// HasAnyPermission checks if a user holds at least one of the given permission codes
// (directly assigned via their roles, or via a wildcard "*" grant such as the admin role).
func (s *Service) HasAnyPermission(ctx context.Context, tenantID, userID uuid.UUID, permissionCodes ...string) (bool, error) {
	if len(permissionCodes) == 0 {
		return false, nil
	}
	permissions, err := s.repo.GetUserPermissions(ctx, tenantID, userID)
	if err != nil {
		return false, fmt.Errorf("get user permissions: %w", err)
	}
	want := make(map[string]struct{}, len(permissionCodes))
	for _, code := range permissionCodes {
		want[code] = struct{}{}
	}
	for _, perm := range permissions {
		if perm.PermissionCode == WildcardPermission {
			return true, nil
		}
		if _, ok := want[perm.PermissionCode]; ok {
			return true, nil
		}
	}
	return false, nil
}

// HasAnyPermissionViaGlobalRoles resolves permissions the way /auth/me does — from the
// hospital service role(s) matching the caller's GLOBAL JWT roles — for SSO principals
// that have no UserRoleAssignment rows yet. Third leg of the middleware resolution chain:
// JWT canonical perms -> per-user assignments -> role-mapped service role.
func (s *Service) HasAnyPermissionViaGlobalRoles(ctx context.Context, tenantID uuid.UUID, globalRoles []string, permissionCodes ...string) (bool, error) {
	if len(permissionCodes) == 0 || len(globalRoles) == 0 {
		return false, nil
	}
	want := make(map[string]struct{}, len(permissionCodes))
	for _, code := range permissionCodes {
		want[code] = struct{}{}
	}
	candidates := make([]string, 0, len(globalRoles)+1)
	candidates = append(candidates, globalRoles...)
	candidates = append(candidates, MapGlobalRolesToServiceRole(globalRoles))
	seen := make(map[string]struct{}, len(candidates))
	for _, code := range candidates {
		if code == "" {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		role, err := s.repo.GetRoleByCode(ctx, code)
		if err != nil {
			continue
		}
		rolePerms, err := s.repo.GetRolePermissions(ctx, role.ID)
		if err != nil {
			continue
		}
		for _, perm := range rolePerms {
			if perm.PermissionCode == WildcardPermission {
				return true, nil
			}
			if _, ok := want[perm.PermissionCode]; ok {
				return true, nil
			}
		}
	}
	return false, nil
}

// HasRole checks if a user has a specific role.
func (s *Service) HasRole(ctx context.Context, tenantID, userID uuid.UUID, roleCode string) (bool, error) {
	roles, err := s.repo.GetUserRoles(ctx, tenantID, userID)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	for _, role := range roles {
		if role.RoleCode == roleCode {
			return true, nil
		}
	}
	return false, nil
}

// AssignRole assigns a role to a user (admin-driven, explicit).
func (s *Service) AssignRole(ctx context.Context, tenantID, userID, roleID, assignedBy uuid.UUID) error {
	assignments, err := s.repo.ListUserAssignments(ctx, tenantID, AssignmentFilters{
		UserID: &userID,
		RoleID: &roleID,
	})
	if err != nil {
		return fmt.Errorf("check existing assignment: %w", err)
	}
	if len(assignments) > 0 {
		return fmt.Errorf("role already assigned to user")
	}
	assignment := &UserRoleAssignment{
		ID:         uuid.New(),
		TenantID:   tenantID,
		UserID:     userID,
		RoleID:     roleID,
		AssignedBy: assignedBy,
	}
	if err := s.repo.AssignRoleToUser(ctx, tenantID, assignment); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	s.logger.Info("role assigned",
		zap.String("tenant_id", tenantID.String()),
		zap.String("user_id", userID.String()),
		zap.String("role_id", roleID.String()),
		zap.String("assigned_by", assignedBy.String()))
	return nil
}

// RevokeRole revokes a role from a user.
func (s *Service) RevokeRole(ctx context.Context, tenantID, userID, roleID uuid.UUID) error {
	if err := s.repo.RevokeRoleFromUser(ctx, tenantID, userID, roleID); err != nil {
		return fmt.Errorf("revoke role: %w", err)
	}
	s.logger.Info("role revoked",
		zap.String("tenant_id", tenantID.String()),
		zap.String("user_id", userID.String()),
		zap.String("role_id", roleID.String()))
	return nil
}

// GetUserRoles retrieves all roles for a user.
func (s *Service) GetUserRoles(ctx context.Context, tenantID, userID uuid.UUID) ([]*HospitalRole, error) {
	return s.repo.GetUserRoles(ctx, tenantID, userID)
}

// GetUserPermissions retrieves all permissions for a user.
func (s *Service) GetUserPermissions(ctx context.Context, tenantID, userID uuid.UUID) ([]*HospitalPermission, error) {
	return s.repo.GetUserPermissions(ctx, tenantID, userID)
}
