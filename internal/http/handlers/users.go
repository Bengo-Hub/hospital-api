package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/identity"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// UsersHandler implements the tenant staff/role-management admin surface: list the tenant's
// provisioned hospital-api users, list the global role catalog, and change a user's role.
// Real user identity/creation stays owned by auth-api (Trinity convention — see
// shared-docs/architecture/cross-service-data-ownership.md's "Auth-Service: Global roles");
// this is scoped to hospital-api's OWN service-level role assignment only.
type UsersHandler struct {
	identitySvc *identity.Service
	rbacSvc     *rbac.Service
}

// NewUsersHandler creates a new UsersHandler.
func NewUsersHandler(identitySvc *identity.Service, rbacSvc *rbac.Service) *UsersHandler {
	return &UsersHandler{identitySvc: identitySvc, rbacSvc: rbacSvc}
}

type userDTO struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Roles     []string `json:"roles"`
	CreatedAt string   `json:"created_at"`
}

type roleDTO struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListUsers handles GET /{tenant}/hospital/users
func (h *UsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}

	users, err := h.identitySvc.ListUsers(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		// Initialized non-nil: a user with zero role assignments must serialize `"roles": []`,
		// never `null` — a nil Go slice marshals to JSON null, which crashed the Users page's
		// `u.roles[0]` access for any user JIT-provisioning hadn't yet mapped to a role.
		roleCodes := []string{}
		if h.rbacSvc != nil {
			if roles, rerr := h.rbacSvc.GetUserRoles(r.Context(), tenantID, u.ID); rerr == nil {
				for _, role := range roles {
					roleCodes = append(roleCodes, role.RoleCode)
				}
			}
		}
		out = append(out, userDTO{
			ID:        u.ID.String(),
			Email:     u.Email,
			Name:      u.Name,
			Status:    u.Status,
			Roles:     roleCodes,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ListRoles handles GET /{tenant}/hospital/roles — the role picker's source list.
func (h *UsersHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.rbacSvc.ListRoles(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	out := make([]roleDTO, 0, len(roles))
	for _, role := range roles {
		desc := ""
		if role.Description != nil {
			desc = *role.Description
		}
		out = append(out, roleDTO{Code: role.RoleCode, Name: role.Name, Description: desc})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

type setUserRoleRequest struct {
	RoleCode string `json:"role_code"`
}

// SetUserRole handles PUT /{tenant}/hospital/users/{userID}/role
func (h *UsersHandler) SetUserRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req setUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RoleCode == "" {
		respondError(w, http.StatusBadRequest, "role_code is required")
		return
	}

	if err := h.rbacSvc.SetUserRole(r.Context(), tenantID, userID, currentUserID(r), req.RoleCode); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
