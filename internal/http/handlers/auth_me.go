package handlers

import (
	"net/http"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// AuthMeHandler returns the current user from JWT claims, enriched with service-level
// roles and permissions from the local RBAC database.
type AuthMeHandler struct {
	rbacService *rbac.Service
}

// NewAuthMeHandler creates a new AuthMeHandler.
func NewAuthMeHandler(rbacService *rbac.Service) *AuthMeHandler {
	return &AuthMeHandler{rbacService: rbacService}
}

// GetMe returns the current user with merged JWT + service-level RBAC data.
// GET /api/v1/{tenant}/hospital/auth/me
func (h *AuthMeHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		respondError(w, http.StatusUnauthorized, "missing auth context")
		return
	}

	roles := claims.Roles
	if roles == nil {
		roles = []string{}
	}
	// Deliberately NOT seeded from claims.Permissions: that's the raw JWT's cross-vertical
	// permission dump (every permission the caller's global SSO role maps to across
	// pos/inventory/ordering/logistics/treasury/auth, not just hospital-service), since the
	// token is shared platform-wide. Permissions here must come exclusively from local hospital
	// RBAC below, so a user with no local role assignment correctly gets an empty array —
	// hospital-ui's useAppPermissions only falls back to its client-side ROLE_PERMISSIONS map
	// when the server array is empty, and a non-empty-but-irrelevant array silently defeated
	// that fallback.
	permissions := []string{}

	ctx := r.Context()
	tenantID := httpware.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = claims.TenantID
	}
	// Report the RESOLVED tenant (this request's URL-scoped target), never the caller's raw JWT
	// claims directly — for a platform owner visiting a tenant other than their own, those
	// differ, and reporting claims.TenantID/claims.GetTenantSlug() here silently told the
	// frontend it was looking at the platform owner's OWN tenant regardless of which tenant's
	// URL slug it actually asked for (apiClient.setTenantInfo then stored the wrong tenant).
	tenantSlug := httpware.GetTenantSlug(ctx)
	if tenantSlug == "" {
		tenantSlug = claims.GetTenantSlug()
	}
	if tenantID != "" && h.rbacService != nil {
		if tenantUUID, err := uuid.Parse(tenantID); err == nil {
			// claims.Subject is the auth-service user ID, NOT the local HospitalUser.ID — a
			// HospitalUser row is keyed by (tenant_id, auth_service_user_id), so this must
			// resolve through the *ForAuthUser variants rather than assuming ID equality.
			authUUID, _ := uuid.Parse(claims.Subject)
			if svcRoles, err := h.rbacService.GetUserRolesForAuthUser(ctx, tenantUUID, authUUID); err == nil {
				for _, sr := range svcRoles {
					roles = appendUniqueStr(roles, sr.RoleCode)
				}
			}
			if svcPerms, err := h.rbacService.GetUserPermissionsForAuthUser(ctx, tenantUUID, authUUID); err == nil {
				for _, sp := range svcPerms {
					permissions = appendUniqueStr(permissions, sp.PermissionCode)
				}
			}
		}
	}

	out := map[string]interface{}{
		"id":                claims.Subject,
		"email":             claims.Email,
		"tenant_id":         tenantID,
		"tenant_slug":       tenantSlug,
		"is_platform_owner": claims.IsPlatformOwner,
		"roles":             roles,
		"permissions":       permissions,
	}
	respondJSON(w, http.StatusOK, out)
}

func appendUniqueStr(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
