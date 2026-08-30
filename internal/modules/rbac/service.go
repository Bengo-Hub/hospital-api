package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/modules/auditlog"
)

// UserResolver resolves an auth-service user ID (the JWT `sub` claim) to this tenant's local
// HospitalUser.ID. Declared here (not imported from the identity package) to avoid an import
// cycle — identity.Service satisfies this interface.
type UserResolver interface {
	ResolveLocalUserID(ctx context.Context, tenantID, authUserID uuid.UUID) (uuid.UUID, error)
}

// Service provides business logic for RBAC operations.
type Service struct {
	repo         Repository
	logger       *zap.Logger
	userResolver UserResolver
	audit        *auditlog.Writer
}

// NewService creates a new RBAC service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{repo: repo, logger: logger}
}

// SetUserResolver wires the auth-ID -> local-ID resolver (identity.Service). Must be called
// before HasAnyPermissionForAuthUser/GetUserRolesForAuthUser/GetUserPermissionsForAuthUser are
// used — every caller holding only the raw JWT subject (middleware, /auth/me) needs it.
func (s *Service) SetUserResolver(r UserResolver) {
	s.userResolver = r
}

// SetAuditWriter wires the audit log writer. Optional — every recordAudit call is a no-op if
// unset, so this can be omitted in tests without any behavior change to the mutations it
// records.
func (s *Service) SetAuditWriter(w *auditlog.Writer) {
	s.audit = w
}

// recordAudit is a thin, always-safe wrapper: never blocks or fails the caller (the writer
// itself is fire-and-forget/log-and-continue; this just no-ops when unwired).
func (s *Service) recordAudit(ctx context.Context, tenantID, actorID uuid.UUID, action, targetType string, targetID uuid.UUID, before, after map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, auditlog.Entry{
		TenantID: tenantID, ActorID: actorID, Action: action, TargetType: targetType, TargetID: targetID,
		Before: before, After: after,
	})
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
	role, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode)
	if err != nil {
		if seedErr := s.SeedRoles(ctx); seedErr != nil {
			s.logger.Warn("AssignRoleByCode: seed retry failed", zap.String("role_code", roleCode), zap.Error(seedErr))
		}
		role, err = s.repo.GetRoleByCode(ctx, tenantID, roleCode)
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

// HasAnyPermissionForAuthUser resolves authUserID (the JWT `sub` claim) to this tenant's local
// HospitalUser.ID via the wired UserResolver, then delegates to HasAnyPermission. Returns
// (false, nil) — never an error — when the caller has no local row yet for this tenant (e.g. a
// brand-new SSO principal that hasn't been JIT-provisioned): callers fall through to the
// existing HasAnyPermissionViaGlobalRoles leg for that case, exactly as before this resolver
// existed.
func (s *Service) HasAnyPermissionForAuthUser(ctx context.Context, tenantID, authUserID uuid.UUID, permissionCodes ...string) (bool, error) {
	if s.userResolver == nil {
		return false, nil
	}
	localID, err := s.userResolver.ResolveLocalUserID(ctx, tenantID, authUserID)
	if err != nil {
		return false, nil
	}
	return s.HasAnyPermission(ctx, tenantID, localID, permissionCodes...)
}

// GetUserRolesForAuthUser is GetUserRoles resolved from an auth-service user ID instead of the
// local HospitalUser.ID. Returns an empty slice (not an error) when unresolvable.
func (s *Service) GetUserRolesForAuthUser(ctx context.Context, tenantID, authUserID uuid.UUID) ([]*HospitalRole, error) {
	if s.userResolver == nil {
		return nil, nil
	}
	localID, err := s.userResolver.ResolveLocalUserID(ctx, tenantID, authUserID)
	if err != nil {
		return nil, nil
	}
	return s.GetUserRoles(ctx, tenantID, localID)
}

// GetUserPermissionsForAuthUser is GetUserPermissions resolved from an auth-service user ID
// instead of the local HospitalUser.ID. Returns an empty slice (not an error) when unresolvable.
func (s *Service) GetUserPermissionsForAuthUser(ctx context.Context, tenantID, authUserID uuid.UUID) ([]*HospitalPermission, error) {
	if s.userResolver == nil {
		return nil, nil
	}
	localID, err := s.userResolver.ResolveLocalUserID(ctx, tenantID, authUserID)
	if err != nil {
		return nil, nil
	}
	return s.GetUserPermissions(ctx, tenantID, localID)
}

// unambiguousHospitalRoles are SSO role names used ONLY by the hospital vertical. Unlike
// admin/manager/cashier/receptionist/superuser — reused verbatim by every other vertical
// (POS, hospitality, ERP, TruLoad, ...) sharing the same auth-api tenant — these cannot be
// held by a non-hospital user, so they're trusted even with no outlet context at all.
var unambiguousHospitalRoles = map[string]bool{
	"doctor": true, "clinician": true, "physician": true,
	"nurse": true, "pharmacist": true, "records_clerk": true,
}

// HasUnambiguousHospitalRole reports whether any of the given global SSO roles is
// hospital-exclusive (see unambiguousHospitalRoles). Used to decide whether a caller with no
// outlet context (HQ users, pre-outlet-selection tokens, self-registration) can still be
// trusted as hospital-relevant purely from their role name.
func HasUnambiguousHospitalRole(roles []string) bool {
	for _, r := range roles {
		if unambiguousHospitalRoles[r] {
			return true
		}
	}
	return false
}

// HasAnyPermissionViaGlobalRoles resolves permissions the way /auth/me does — from the
// hospital service role(s) matching the caller's GLOBAL JWT roles — for SSO principals
// that have no UserRoleAssignment rows yet. Third leg of the middleware resolution chain:
// JWT canonical perms -> per-user assignments -> role-mapped service role.
//
// outletUseCase is the caller's JWT outlet_use_case claim (empty for HQ/admin users or a
// pre-outlet-selection token). codevertex-demo hosts every vertical under one tenant, and
// role names like "manager"/"cashier"/"admin" are reused verbatim across all of them, so a
// non-empty, confirmed-non-hospital outlet use case is trusted immediately: a demo retail
// manager must never be granted hospital.* permissions just because "manager" also maps to
// a hospital role. An empty outlet_use_case is not proof of anything either way, so it falls
// through to the existing role-mapped resolution unchanged.
func (s *Service) HasAnyPermissionViaGlobalRoles(ctx context.Context, tenantID uuid.UUID, globalRoles []string, outletUseCase string, permissionCodes ...string) (bool, error) {
	if outletUseCase != "" && outletUseCase != "hospital" {
		return false, nil
	}
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
		role, err := s.repo.GetRoleByCode(ctx, tenantID, code)
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

// AssignExtraRole idempotently ADDS roleCode to userID's assignments without touching any
// existing assignment — unlike SetUserRole (which always revokes every other role first), this
// never revokes anything. Re-granting an already-held role is a no-op success. Note: userID's
// PRIMARY role (set via SetUserRole) still fully overwrites every assignment including any
// extras granted here — that is SetUserRole's existing, unchanged, intentional contract.
func (s *Service) AssignExtraRole(ctx context.Context, tenantID, userID, actorID uuid.UUID, roleCode string) error {
	role, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode)
	if err != nil {
		return fmt.Errorf("role %q not found: %w", roleCode, err)
	}
	if err := s.AssignRole(ctx, tenantID, userID, role.ID, actorID); err != nil {
		if strings.Contains(err.Error(), "already assigned") {
			return nil
		}
		return err
	}
	s.recordAudit(ctx, tenantID, actorID, "role.granted_extra", "user", userID, nil, map[string]any{"role_code": roleCode})
	return nil
}

// RevokeExtraRole removes exactly one role assignment (by code), leaving all others untouched.
func (s *Service) RevokeExtraRole(ctx context.Context, tenantID, userID, actorID uuid.UUID, roleCode string) error {
	role, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode)
	if err != nil {
		return fmt.Errorf("role %q not found: %w", roleCode, err)
	}
	if err := s.RevokeRole(ctx, tenantID, userID, role.ID); err != nil {
		return err
	}
	s.recordAudit(ctx, tenantID, actorID, "role.revoked_extra", "user", userID, map[string]any{"role_code": roleCode}, nil)
	return nil
}

// GetUserRoles retrieves all roles for a user.
func (s *Service) GetUserRoles(ctx context.Context, tenantID, userID uuid.UUID) ([]*HospitalRole, error) {
	return s.repo.GetUserRoles(ctx, tenantID, userID)
}

// ListRoles returns every role visible to tenantID — the global catalog plus this tenant's own
// clones/custom roles — the picker source for the Users admin page's "change role" action and
// the Roles & Permissions admin page.
func (s *Service) ListRoles(ctx context.Context, tenantID uuid.UUID) ([]*HospitalRole, error) {
	return s.repo.ListRoles(ctx, tenantID)
}

// SetUserRole replaces a user's current role assignment(s) with the single named role — the
// "change role" operation the Users admin page needs. Unlike AssignRole (additive, errors if
// already assigned) this is idempotent and always leaves the user with exactly one PRIMARY role:
// revokes every currently-assigned role first, then assigns the new one. roleCode is resolved
// preferring this tenant's own clone/custom role over the global one of the same code.
func (s *Service) SetUserRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCode string) error {
	role, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode)
	if err != nil {
		return fmt.Errorf("role %q not found: %w", roleCode, err)
	}

	current, err := s.repo.GetUserRoles(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("get current roles: %w", err)
	}
	for _, r := range current {
		if r.ID == role.ID {
			continue // already holds this exact role
		}
		if err := s.repo.RevokeRoleFromUser(ctx, tenantID, userID, r.ID); err != nil {
			return fmt.Errorf("revoke existing role %q: %w", r.RoleCode, err)
		}
	}
	if len(current) == 1 && current[0].ID == role.ID {
		return nil // no-op: already exactly this role
	}

	assignment := &UserRoleAssignment{
		ID:         uuid.New(),
		TenantID:   tenantID,
		UserID:     userID,
		RoleID:     role.ID,
		AssignedBy: assignedBy,
	}
	if err := s.repo.AssignRoleToUser(ctx, tenantID, assignment); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	s.logger.Info("user role changed",
		zap.String("tenant_id", tenantID.String()),
		zap.String("user_id", userID.String()),
		zap.String("role_code", roleCode))
	s.recordAudit(ctx, tenantID, assignedBy, "role.assigned", "user", userID, nil, map[string]any{"role_code": roleCode})
	return nil
}

// GetUserPermissions retrieves all permissions for a user.
func (s *Service) GetUserPermissions(ctx context.Context, tenantID, userID uuid.UUID) ([]*HospitalPermission, error) {
	return s.repo.GetUserPermissions(ctx, tenantID, userID)
}

// ListPermissions returns the full global permission catalog — the checkbox source for the
// Roles & Permissions matrix editor.
func (s *Service) ListPermissions(ctx context.Context) ([]*HospitalPermission, error) {
	return s.repo.ListPermissions(ctx, PermissionFilters{})
}

// CustomizeRole idempotently clones the global role roleCode into a tenant-scoped copy on
// first edit (returns the existing clone unchanged if one already exists), copies its current
// permission set, and re-points every existing UserRoleAssignment in this tenant from the
// global role's ID to the new clone's ID — a customization takes effect for already-assigned
// staff immediately, not just future assignments. Rejects an attempt to customize a role that
// isn't global (roleCode must resolve to a tenant_id-nil row) or that doesn't exist.
func (s *Service) CustomizeRole(ctx context.Context, tenantID, actorID uuid.UUID, roleCode string) (*HospitalRole, error) {
	global, err := s.repo.GetGlobalRoleByCode(ctx, roleCode)
	if err != nil {
		return nil, fmt.Errorf("role %q is not a global role: %w", roleCode, err)
	}

	// Idempotent: if this tenant already cloned it, return the existing clone.
	if existing, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode); err == nil && existing.TenantID != nil && *existing.TenantID == tenantID {
		return existing, nil
	}

	globalPerms, err := s.repo.GetRolePermissions(ctx, global.ID)
	if err != nil {
		return nil, fmt.Errorf("read global role permissions: %w", err)
	}

	clone := &HospitalRole{
		ID:               uuid.New(),
		TenantID:         &tenantID,
		RoleCode:         global.RoleCode,
		Name:             global.Name,
		Description:      global.Description,
		IsSystemRole:     false,
		ClonedFromRoleID: &global.ID,
	}
	if err := s.repo.CreateRole(ctx, clone); err != nil {
		return nil, fmt.Errorf("clone role: %w", err)
	}
	permIDs := make([]uuid.UUID, len(globalPerms))
	for i, p := range globalPerms {
		permIDs[i] = p.ID
	}
	if err := s.repo.ReplaceRolePermissions(ctx, clone.ID, permIDs); err != nil {
		return nil, fmt.Errorf("copy permissions to clone: %w", err)
	}
	if err := s.repo.RepointRoleAssignments(ctx, tenantID, global.ID, clone.ID); err != nil {
		return nil, fmt.Errorf("repoint existing assignments to clone: %w", err)
	}

	s.logger.Info("role customized (cloned for tenant)",
		zap.String("tenant_id", tenantID.String()), zap.String("role_code", roleCode))
	s.recordAudit(ctx, tenantID, actorID, "role.customized", "role", clone.ID, nil, map[string]any{
		"role_code": roleCode, "cloned_from_role_id": global.ID.String(),
	})
	return clone, nil
}

// CreateCustomRole creates a brand-new tenant-only role (never global). Rejects roleCode if it
// collides with any EXISTING GLOBAL role_code — a tenant role silently shadowing a real global
// one (e.g. re-using "admin") is a scope-confusion footgun the DB's partial indexes alone
// wouldn't catch (they only enforce uniqueness WITHIN each scope, not across them).
func (s *Service) CreateCustomRole(ctx context.Context, tenantID, actorID uuid.UUID, roleCode, name, description string, permissionCodes []string) (*HospitalRole, error) {
	if roleCode == "" || name == "" {
		return nil, fmt.Errorf("role_code and name are required")
	}
	if _, err := s.repo.GetGlobalRoleByCode(ctx, roleCode); err == nil {
		return nil, fmt.Errorf("role_code %q collides with an existing global role", roleCode)
	}
	if existing, err := s.repo.GetRoleByCode(ctx, tenantID, roleCode); err == nil && existing.TenantID != nil {
		return nil, fmt.Errorf("role_code %q already exists for this tenant", roleCode)
	}

	role := &HospitalRole{
		ID:           uuid.New(),
		TenantID:     &tenantID,
		RoleCode:     roleCode,
		Name:         name,
		IsSystemRole: false,
	}
	if description != "" {
		role.Description = &description
	}
	if err := s.repo.CreateRole(ctx, role); err != nil {
		return nil, fmt.Errorf("create custom role: %w", err)
	}

	permIDs, err := s.permissionIDsForCodes(ctx, permissionCodes)
	if err != nil {
		return nil, err
	}
	if len(permIDs) > 0 {
		if err := s.repo.ReplaceRolePermissions(ctx, role.ID, permIDs); err != nil {
			return nil, fmt.Errorf("assign permissions to new role: %w", err)
		}
	}

	s.logger.Info("custom role created",
		zap.String("tenant_id", tenantID.String()), zap.String("role_code", roleCode))
	s.recordAudit(ctx, tenantID, actorID, "role.created", "role", role.ID, nil, map[string]any{
		"role_code": roleCode, "name": name, "permission_codes": permissionCodes,
	})
	return role, nil
}

// UpdateRolePermissions replaces a TENANT-scoped role's (clone or from-scratch) permission set.
// Hard-rejects any role whose TenantID is nil — global rows are never mutable through this path;
// customize it first via CustomizeRole.
func (s *Service) UpdateRolePermissions(ctx context.Context, tenantID, actorID, roleID uuid.UUID, permissionCodes []string) error {
	role, err := s.repo.GetRole(ctx, roleID)
	if err != nil {
		return fmt.Errorf("role not found: %w", err)
	}
	if role.TenantID == nil {
		return fmt.Errorf("cannot edit a global role directly — customize it for this tenant first")
	}
	if *role.TenantID != tenantID {
		return fmt.Errorf("role does not belong to this tenant")
	}

	before, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return fmt.Errorf("read current permissions: %w", err)
	}
	beforeCodes := make([]string, len(before))
	for i, p := range before {
		beforeCodes[i] = p.PermissionCode
	}

	permIDs, err := s.permissionIDsForCodes(ctx, permissionCodes)
	if err != nil {
		return err
	}
	if err := s.repo.ReplaceRolePermissions(ctx, roleID, permIDs); err != nil {
		return fmt.Errorf("replace role permissions: %w", err)
	}

	s.logger.Info("role permissions updated",
		zap.String("tenant_id", tenantID.String()), zap.String("role_id", roleID.String()))
	s.recordAudit(ctx, tenantID, actorID, "role.permissions_updated", "role", roleID,
		map[string]any{"permission_codes": beforeCodes},
		map[string]any{"permission_codes": permissionCodes})
	return nil
}

// permissionIDsForCodes resolves permission codes to their catalog IDs, skipping any code that
// doesn't exist in the seeded catalog (rather than failing the whole operation on one typo).
func (s *Service) permissionIDsForCodes(ctx context.Context, codes []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(codes))
	for _, code := range codes {
		perm, err := s.repo.GetPermissionByCode(ctx, code)
		if err != nil {
			continue
		}
		ids = append(ids, perm.ID)
	}
	return ids, nil
}
