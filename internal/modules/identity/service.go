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
	"github.com/bengobox/hospital-service/internal/ent/outlet"
	"github.com/bengobox/hospital-service/internal/modules/auditlog"
	"github.com/bengobox/hospital-service/internal/modules/authapi"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/modules/tenant"
)

// validUserStatuses are the only values SetUserStatus accepts — matches HospitalUser.status's
// own doc comment ("Status: active, inactive, suspended").
var validUserStatuses = map[string]bool{"active": true, "inactive": true, "suspended": true}

// Service handles identity-related operations (JIT user/tenant provisioning) using Ent.
type Service struct {
	client       *ent.Client
	tenantSyncer *tenant.Syncer
	rbacService  *rbac.Service
	audit        *auditlog.Writer
	authAPI      *authapi.Client
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

// SetAuditWriter wires the audit log writer (see rbac.Service.SetAuditWriter for the same
// optional, always-safe contract).
func (s *Service) SetAuditWriter(w *auditlog.Writer) {
	s.audit = w
}

// SetAuthAPIClient wires the auth-api S2S client used by InviteMember.
func (s *Service) SetAuthAPIClient(c *authapi.Client) {
	s.authAPI = c
}

// InviteMember invites a new (or attaches an existing) staff member by email to tenantID via
// auth-api's S2S tenant-membership endpoint (see authapi.Client.InviteTenantMember) — the same
// real mechanism every other service's own staff-invite flow uses. roleCode drives
// auth_events.go's JIT role assignment the moment the invitee first logs in: no separate
// "pending role assignment" table is needed because inviting with the right role name (or an
// outletID resolving to a hospital outlet) is already sufficient signal.
func (s *Service) InviteMember(ctx context.Context, tenantID, actorID uuid.UUID, email, name, roleCode, outletID string) (*authapi.InviteMemberResult, error) {
	if s.authAPI == nil {
		return nil, fmt.Errorf("invite is not configured")
	}
	if email == "" || roleCode == "" {
		return nil, fmt.Errorf("email and role_code are required")
	}
	result, err := s.authAPI.InviteTenantMember(ctx, tenantID, authapi.InviteMemberRequest{
		Email: email, Name: name, Roles: []string{roleCode}, OutletID: outletID,
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		targetID, _ := uuid.Parse(result.UserID) // best-effort; Nil if unparsable
		s.audit.Record(ctx, auditlog.Entry{
			TenantID: tenantID, ActorID: actorID, Action: "user.invited",
			TargetType: "user", TargetID: targetID,
			After: map[string]any{"email": email, "role_code": roleCode, "auth_user_id": result.UserID},
		})
	}
	return result, nil
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
	// 1. Resolve the tenant FIRST — a HospitalUser row is now unique per (tenant, auth user),
	// not per auth user alone, so the existence check itself must be tenant-scoped. Cheap on
	// the steady-state path: tenantSyncer.SyncTenant is a single indexed local lookup.
	tenantID, err := s.tenantSyncer.SyncTenant(ctx, tenantSlug)
	if err != nil {
		return nil, fmt.Errorf("identity.Service: sync tenant %q: %w", tenantSlug, err)
	}

	// 2. Check if user exists for THIS tenant.
	u, err := s.client.HospitalUser.Query().
		Where(hospitaluser.TenantIDEQ(tenantID), hospitaluser.AuthServiceUserIDEQ(authServiceID)).
		Only(ctx)

	if err == nil {
		// User exists — STILL (idempotently) re-run role assignment so a role added or
		// corrected after first login is never silently missed. Gated: an existing row (e.g.
		// provisioned before this hardening shipped) is never deleted here, but it must stop
		// accruing hospital permissions once its outlet/role evidence no longer supports them.
		if s.rbacService != nil && s.shouldProvisionForHospital(ctx, u.TenantID, claims) {
			s.assignDefaultRoleFromJWT(ctx, u.TenantID, u.ID, authServiceID, claims)
		}
		return u, nil
	}

	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("identity.Service: query user: %w", err)
	}

	if !s.shouldProvisionForHospital(ctx, tenantID, claims) {
		log.Printf("  [jit-provisioning] skipping user (not hospital-relevant) for tenant %s", tenantSlug)
		return nil, nil
	}

	// 3. Create user. ID is a locally generated UUID (ent's default) — never the auth-service
	// user's own UUID, since the same auth user may now hold one HospitalUser row per tenant.
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

// shouldProvisionForHospital applies the same use-case gate as
// AuthEventHandler.shouldProvisionForHospital (see isHospitalRelevant's doc comment in
// auth_events.go for the full policy) to the synchronous HTTP JIT path. Prefers the JWT's
// outlet_use_case claim — free, no DB round trip — and falls back to resolving outlet_id
// against the local Outlet mirror for callers that only pass the ID.
//
// Platform owners always pass: they're never outlet-scoped and must be able to reach every
// tenant's every service (the whole point of the role), which is exactly the property this
// gate would otherwise mistake for "not hospital-relevant" — see reference_tenant_uuid_drift
// and this service's own platform-owner cross-tenant fix.
func (s *Service) shouldProvisionForHospital(ctx context.Context, tenantID uuid.UUID, claims map[string]any) bool {
	if isPlatformOwner, _ := claims["is_platform_owner"].(bool); isPlatformOwner {
		return true
	}
	var (
		outletUseCase string
		resolved      bool
	)
	if uc, ok := claims["outlet_use_case"].(string); ok && uc != "" {
		outletUseCase, resolved = uc, true
	} else if outletIDStr, ok := claims["outlet_id"].(string); ok && outletIDStr != "" {
		if outletID, err := uuid.Parse(outletIDStr); err == nil {
			if o, err := s.client.Outlet.Get(ctx, outletID); err == nil {
				resolved = true
				if o.UseCase != nil {
					outletUseCase = *o.UseCase
				}
			}
		}
	}
	var tenantUseCase *string
	if t, err := s.client.Tenant.Get(ctx, tenantID); err == nil {
		tenantUseCase = t.UseCase
	}
	return isHospitalRelevant(outletUseCase, resolved, tenantUseCase, extractRoles(claims))
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

// ResolveLocalUserID resolves an auth-service user ID (the JWT `sub` claim) to this tenant's
// local HospitalUser.ID. Implements rbac.UserResolver — since a HospitalUser row is unique per
// (tenant_id, auth_service_user_id) rather than globally keyed by the auth ID, every permission
// check that only has the raw JWT subject must resolve through here before querying
// UserRoleAssignment (which is keyed on the local ID).
//
// Also the single enforcement choke point for deactivation: a non-"active" user resolves to an
// error here, which every caller (HasAnyPermissionForAuthUser, GetUserRolesForAuthUser,
// GetUserPermissionsForAuthUser) already treats as "no local row" and falls through accordingly
// — without this, SetUserStatus would be purely cosmetic.
func (s *Service) ResolveLocalUserID(ctx context.Context, tenantID, authUserID uuid.UUID) (uuid.UUID, error) {
	u, err := s.client.HospitalUser.Query().
		Where(hospitaluser.TenantIDEQ(tenantID), hospitaluser.AuthServiceUserIDEQ(authUserID)).
		Only(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if u.Status != "active" {
		return uuid.Nil, fmt.Errorf("user %s is not active (status=%s)", u.ID, u.Status)
	}
	return u.ID, nil
}

// SetUserStatus updates userID's lifecycle status (active/inactive/suspended) — the
// deactivate/reactivate/suspend action on the Users admin page. userID is the LOCAL
// HospitalUser.ID (as returned by ListUsers), not the auth-service user ID.
func (s *Service) SetUserStatus(ctx context.Context, tenantID, actorID, userID uuid.UUID, status string) (*ent.HospitalUser, error) {
	if !validUserStatuses[status] {
		return nil, fmt.Errorf("invalid status %q: must be one of active, inactive, suspended", status)
	}
	existing, err := s.client.HospitalUser.Query().
		Where(hospitaluser.IDEQ(userID), hospitaluser.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	before := existing.Status
	updated, err := s.client.HospitalUser.UpdateOne(existing).SetStatus(status).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update user status: %w", err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, auditlog.Entry{
			TenantID: tenantID, ActorID: actorID, Action: "user.status_changed",
			TargetType: "user", TargetID: userID,
			Before: map[string]any{"status": before}, After: map[string]any{"status": status},
		})
	}
	return updated, nil
}

// GetTenant returns the tenant row — used by the read-only Config admin page to display the
// facility_type/enabled_modules resolved from subscriptions-api (Tenant.metadata cache).
func (s *Service) GetTenant(ctx context.Context, tenantID uuid.UUID) (*ent.Tenant, error) {
	return s.client.Tenant.Get(ctx, tenantID)
}

// ListOutlets returns every outlet synced for a tenant — the source list for hospital-ui's
// outlet switcher (2026-08-30). Outlets are synced in via auth-service NATS events
// (auth_outlet_events.go); this is the first place they're ever exposed over HTTP. No
// per-user outlet-membership restriction exists yet (see outlet_context.go's own doc comment —
// "Any authenticated user in the tenant may currently select any of the tenant's outlets"), so
// this deliberately returns the full tenant list rather than a scoped subset.
func (s *Service) ListOutlets(ctx context.Context, tenantID uuid.UUID) ([]*ent.Outlet, error) {
	return s.client.Outlet.Query().
		Where(outlet.TenantID(tenantID), outlet.StatusEQ("active")).
		Order(ent.Desc(outlet.FieldIsHq), ent.Asc(outlet.FieldName)).
		All(ctx)
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
