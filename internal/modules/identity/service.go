package identity

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/hospitaluser"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/modules/tenant"
)

// Service handles identity-related operations (JIT user/tenant provisioning) using Ent.
type Service struct {
	client       *ent.Client
	tenantSyncer *tenant.Syncer
	rbacService  *rbac.Service
}

// NewService creates a new Identity Service.
func NewService(client *ent.Client, tenantSyncer *tenant.Syncer) *Service {
	return &Service{
		client:       client,
		tenantSyncer: tenantSyncer,
	}
}

// SetRBACService sets the RBAC service for JIT role assignment.
func (s *Service) SetRBACService(svc *rbac.Service) {
	s.rbacService = svc
}

// EnsureUserFromToken performs JIT (Just-In-Time) provisioning of users and tenants.
// If the user doesn't exist locally, it creates them. If the tenant doesn't exist, it
// syncs it from auth-service first.
//
// CRITICAL: role-healing must run on the "user already exists" branch too, not only on
// first creation. A user provisioned before the JWT->hospital role mapping existed (or
// whose initial role assignment failed, e.g. because SeedRoles hadn't run yet) would
// otherwise be stuck with NO hospital permissions forever and hit 403s on every gated
// route despite being a legitimate doctor/admin/etc. AssignRoleByCode is idempotent
// (skips if already assigned), so re-running it on every request's JIT check is cheap on
// the steady-state path — see the treasury-api EnsureUserFromToken bug this mirrors the
// fix for (reference_service_rbac_authme_sync).
func (s *Service) EnsureUserFromToken(ctx context.Context, authServiceID uuid.UUID, tenantSlug string, claims map[string]any) (*ent.HospitalUser, error) {
	// 1. Check if user exists by auth_service_id.
	u, err := s.client.HospitalUser.Query().
		Where(hospitaluser.AuthServiceUserIDEQ(authServiceID)).
		Only(ctx)

	if err == nil {
		// User exists — STILL (idempotently) re-run role assignment so a role added or
		// corrected after first login is never silently missed.
		if s.rbacService != nil {
			s.assignDefaultRoleFromJWT(ctx, u.TenantID, u.ID, authServiceID, claims)
		}
		return u, nil
	}

	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("identity.Service: query user: %w", err)
	}

	// 2. User not found — ensure tenant exists.
	tenantID, err := s.tenantSyncer.SyncTenant(ctx, tenantSlug)
	if err != nil {
		return nil, fmt.Errorf("identity.Service: sync tenant %q: %w", tenantSlug, err)
	}

	// 3. Create user.
	email, _ := claims["email"].(string)
	fullName, _ := claims["full_name"].(string)
	if fullName == "" {
		if idx := strings.Index(email, "@"); idx > 0 {
			fullName = email[:idx]
		} else {
			fullName = email
		}
	}

	newUsr, err := s.client.HospitalUser.Create().
		SetID(authServiceID).
		SetAuthServiceUserID(authServiceID).
		SetTenantID(tenantID).
		SetEmail(email).
		SetName(fullName).
		SetStatus("active").
		SetSyncStatus("synced").
		SetLastSyncAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity.Service: create user: %w", err)
	}

	log.Printf("  [jit-provisioning] created user %s (AuthID %s) for tenant %s", email, authServiceID, tenantSlug)

	// 4. Assign default hospital role based on JWT roles.
	if s.rbacService != nil {
		s.assignDefaultRoleFromJWT(ctx, tenantID, newUsr.ID, authServiceID, claims)
	}

	return newUsr, nil
}

// assignDefaultRoleFromJWT maps global JWT roles to a hospital-api service role and
// idempotently assigns it. No-op when no claims["roles"] maps to a recognised role.
func (s *Service) assignDefaultRoleFromJWT(ctx context.Context, tenantID uuid.UUID, localUserID, authUserID uuid.UUID, claims map[string]any) {
	roles := extractRoles(claims)
	roleCode := mapSSORoleToHospital(roles)
	if roleCode == "" {
		return
	}
	if err := s.rbacService.AssignRoleByCode(ctx, tenantID, localUserID, authUserID, roleCode); err != nil {
		log.Printf("  [jit-provisioning] role assignment failed for %s: %v", roleCode, err)
	}
}

// GetTenant returns the tenant row — used by the read-only Config admin page to display the
// facility_type/enabled_modules resolved from subscriptions-api (Tenant.metadata cache).
func (s *Service) GetTenant(ctx context.Context, tenantID uuid.UUID) (*ent.Tenant, error) {
	return s.client.Tenant.Get(ctx, tenantID)
}

// ListUsers returns every HospitalUser provisioned for a tenant — the Users admin page's
// source list. Ordered by email for stable pagination-free display (facility tenants are
// small; a real paged list can be added if that stops being true).
func (s *Service) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]*ent.HospitalUser, error) {
	return s.client.HospitalUser.Query().
		Where(hospitaluser.TenantIDEQ(tenantID)).
		Order(ent.Asc(hospitaluser.FieldEmail)).
		All(ctx)
}

// extractRoles pulls the "roles" claim out of either a []string or []interface{} shape
// (JWT claims maps decode differently depending on the caller).
func extractRoles(claims map[string]any) []string {
	if rolesRaw, ok := claims["roles"].([]string); ok {
		return rolesRaw
	}
	var roles []string
	if rolesIface, ok := claims["roles"].([]interface{}); ok {
		for _, r := range rolesIface {
			if str, ok := r.(string); ok {
				roles = append(roles, str)
			}
		}
	}
	return roles
}
