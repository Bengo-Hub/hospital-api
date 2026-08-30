package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/identity"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// UsersHandler implements the tenant staff/role-management admin surface: list/status-manage
// the tenant's provisioned hospital-api users, list/customize/create hospital roles, and manage
// per-user role assignments (primary + additive extra roles). Real user identity/creation stays
// owned by auth-api (Trinity convention — see
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
	ID               string  `json:"id"`
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	Description      string  `json:"description,omitempty"`
	IsSystemRole     bool    `json:"is_system_role"`
	IsCustom         bool    `json:"is_custom"`
	ClonedFromRoleID *string `json:"cloned_from_role_id,omitempty"`
}

func toRoleDTO(role *rbac.HospitalRole) roleDTO {
	desc := ""
	if role.Description != nil {
		desc = *role.Description
	}
	dto := roleDTO{
		ID:           role.ID.String(),
		Code:         role.RoleCode,
		Name:         role.Name,
		Description:  desc,
		IsSystemRole: role.IsSystemRole,
		IsCustom:     role.TenantID != nil,
	}
	if role.ClonedFromRoleID != nil {
		s := role.ClonedFromRoleID.String()
		dto.ClonedFromRoleID = &s
	}
	return dto
}

type permissionDTO struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Module string `json:"module"`
	Action string `json:"action"`
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

// ListRoles handles GET /{tenant}/hospital/roles — the role picker's source list, and the
// Roles & Permissions admin page's role list (global catalog + this tenant's own clones/custom
// roles).
func (h *UsersHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	roles, err := h.rbacSvc.ListRoles(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	out := make([]roleDTO, 0, len(roles))
	for _, role := range roles {
		out = append(out, toRoleDTO(role))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ListPermissions handles GET /{tenant}/hospital/permissions — the full permission catalog,
// the checkbox source for the Roles & Permissions matrix editor.
func (h *UsersHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.rbacSvc.ListPermissions(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list permissions")
		return
	}
	out := make([]permissionDTO, 0, len(perms))
	for _, p := range perms {
		out = append(out, permissionDTO{Code: p.PermissionCode, Name: p.Name, Module: p.Module, Action: p.Action})
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

type setUserStatusRequest struct {
	Status string `json:"status"`
}

// SetUserStatus handles PUT /{tenant}/hospital/users/{userID}/status — the
// deactivate/reactivate/suspend action.
func (h *UsersHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
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
	var req setUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := h.identitySvc.SetUserStatus(r.Context(), tenantID, currentUserID(r), userID, req.Status); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type roleCodeRequest struct {
	RoleCode string `json:"role_code"`
}

// AssignExtraRole handles POST /{tenant}/hospital/users/{userID}/roles — grants an ADDITIONAL
// role on top of the user's primary one, without touching any existing assignment.
func (h *UsersHandler) AssignExtraRole(w http.ResponseWriter, r *http.Request) {
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
	var req roleCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RoleCode == "" {
		respondError(w, http.StatusBadRequest, "role_code is required")
		return
	}
	if err := h.rbacSvc.AssignExtraRole(r.Context(), tenantID, userID, currentUserID(r), req.RoleCode); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// RevokeExtraRole handles DELETE /{tenant}/hospital/users/{userID}/roles/{roleCode}.
func (h *UsersHandler) RevokeExtraRole(w http.ResponseWriter, r *http.Request) {
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
	roleCode := chi.URLParam(r, "roleCode")
	if roleCode == "" {
		respondError(w, http.StatusBadRequest, "role code is required")
		return
	}
	if err := h.rbacSvc.RevokeExtraRole(r.Context(), tenantID, userID, currentUserID(r), roleCode); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// CustomizeRole handles POST /{tenant}/hospital/roles/customize — clones a global role into a
// tenant-scoped copy on first edit (idempotent), so the tenant can then edit its permission set
// via UpdateRolePermissions without affecting any other tenant.
func (h *UsersHandler) CustomizeRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var req roleCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RoleCode == "" {
		respondError(w, http.StatusBadRequest, "role_code is required")
		return
	}
	role, err := h.rbacSvc.CustomizeRole(r.Context(), tenantID, currentUserID(r), req.RoleCode)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": toRoleDTO(role)})
}

type createRoleRequest struct {
	RoleCode        string   `json:"role_code"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	PermissionCodes []string `json:"permission_codes"`
}

// CreateRole handles POST /{tenant}/hospital/roles — creates a brand-new, tenant-only custom
// role from scratch (never global).
func (h *UsersHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var req createRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := h.rbacSvc.CreateCustomRole(r.Context(), tenantID, currentUserID(r), req.RoleCode, req.Name, req.Description, req.PermissionCodes)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": toRoleDTO(role)})
}

type updateRolePermissionsRequest struct {
	PermissionCodes []string `json:"permission_codes"`
}

// UpdateRolePermissions handles PUT /{tenant}/hospital/roles/{roleID}/permissions — replaces a
// TENANT-scoped role's (clone or from-scratch) entire permission set. Editing a global role
// requires CustomizeRole first.
func (h *UsersHandler) UpdateRolePermissions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	roleID, err := uuid.Parse(chi.URLParam(r, "roleID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid role id")
		return
	}
	var req updateRolePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.rbacSvc.UpdateRolePermissions(r.Context(), tenantID, currentUserID(r), roleID, req.PermissionCodes); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
