package handlers

import (
	"encoding/json"
	"net/http"

	authclient "github.com/Bengo-Hub/shared-auth-client"

	"github.com/bengobox/hospital-service/internal/modules/identity"
)

// ConfigHandler exposes this tenant's resolved hospital-service configuration: a read-only view
// of facility_type/enabled_modules (a CACHE resolved from subscriptions-api's plan metadata —
// see internal/ent/schema/tenant.go's Tenant.metadata), plus a small, genuinely
// hospital-api-owned set of facility operating settings a tenant admin can write
// (hospital.config.manage — Tenant.settings, 2026-08-30). Branding/contact/identity fields stay
// owned by auth-api per the tenant_data_architecture platform rule and are deliberately absent
// from this service's Tenant schema — do not add them here.
type ConfigHandler struct {
	identitySvc *identity.Service
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(identitySvc *identity.Service) *ConfigHandler {
	return &ConfigHandler{identitySvc: identitySvc}
}

// GetConfig handles GET /{tenant}/hospital/config
func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}

	t, err := h.identitySvc.GetTenant(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load tenant config")
		return
	}

	facilityType, _ := t.Metadata["facility_type"].(string)
	// Never nil: a nil Go slice marshals to JSON null, which crashes any frontend doing
	// `enabled_modules.length` — the exact class of bug already fixed once for the Users page's
	// roles field (see handlers/users.go's own comment on the same pattern).
	enabledModules := []string{}
	if raw, ok := t.Metadata["enabled_modules"].([]any); ok {
		for _, m := range raw {
			if s, ok := m.(string); ok {
				enabledModules = append(enabledModules, s)
			}
		}
	}
	settings := t.Settings
	if settings == nil {
		settings = map[string]any{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"tenant_name":     t.Name,
		"tenant_slug":     t.Slug,
		"status":          t.Status,
		"facility_type":   facilityType,
		"enabled_modules": enabledModules,
		"synced_at":       t.LastSyncAt,
		"settings":        settings,
	})
}

// updateConfigRequest carries only the facility operating settings a tenant admin may write —
// see identity.Service.UpdateTenantSettings's allowlist for the exact accepted keys.
type updateConfigRequest struct {
	AutoLogoutMinutes  *int   `json:"auto_logout_minutes,omitempty"`
	DefaultLandingView string `json:"default_landing_view,omitempty"`
	OperatingHours     any    `json:"operating_hours,omitempty"`
}

// UpdateConfig handles PUT /{tenant}/hospital/config — writes the facility operating settings
// (hospital.config.manage). Omitted fields are left unchanged (partial update); this endpoint
// never touches facility_type/enabled_modules (subscriptions-api-owned) or tenant identity
// (auth-api-owned).
func (h *ConfigHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var req updateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updates := map[string]any{}
	if req.AutoLogoutMinutes != nil {
		updates["auto_logout_minutes"] = *req.AutoLogoutMinutes
	}
	if req.DefaultLandingView != "" {
		updates["default_landing_view"] = req.DefaultLandingView
	}
	if req.OperatingHours != nil {
		updates["operating_hours"] = req.OperatingHours
	}
	if len(updates) == 0 {
		respondError(w, http.StatusBadRequest, "no recognized settings in request body")
		return
	}
	t, err := h.identitySvc.UpdateTenantSettings(r.Context(), tenantID, currentUserID(r), updates)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"settings": t.Settings})
}

type outletDTO struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	IsHQ         bool   `json:"is_hq"`
	Status       string `json:"status"`
	FacilityType string `json:"facility_type,omitempty"`
}

// ListOutlets handles GET /{tenant}/hospital/outlets — the source list for hospital-ui's outlet
// switcher (2026-08-30). Single-outlet tenants (the overwhelming majority today — Chemist/Clinic
// tier) get back exactly one row and the frontend skips rendering a switcher at all.
//
// Filtered by per-user outlet assignment (2026-08-30) for callers who cannot access all outlets:
// they only see outlets they're actually assigned to (same progressive-rollout carve-out as the
// enforcement middleware — zero assignments = full tenant list, so existing staff aren't
// suddenly shown an empty switcher before an admin assigns them anything).
func (h *ConfigHandler) ListOutlets(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	outlets, err := h.identitySvc.ListOutlets(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list outlets")
		return
	}

	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok && claims != nil && !claims.CanAccessAllOutlets() && !claims.IsService {
		if authUserID, err := claims.UserID(); err == nil {
			if localUserID, err := h.identitySvc.ResolveLocalUserID(r.Context(), tenantID, authUserID); err == nil {
				assigned, err := h.identitySvc.ListUserOutlets(r.Context(), tenantID, localUserID)
				if err == nil && len(assigned) > 0 {
					allowed := make(map[string]bool, len(assigned))
					for _, a := range assigned {
						allowed[a.OutletID.String()] = true
					}
					filtered := outlets[:0:0]
					for _, o := range outlets {
						if allowed[o.ID.String()] {
							filtered = append(filtered, o)
						}
					}
					outlets = filtered
				}
			}
		}
	}

	out := make([]outletDTO, 0, len(outlets))
	for _, o := range outlets {
		dto := outletDTO{ID: o.ID.String(), Code: o.Code, Name: o.Name, IsHQ: o.IsHq, Status: o.Status}
		if o.FacilityType != nil {
			dto.FacilityType = *o.FacilityType
		}
		out = append(out, dto)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}
