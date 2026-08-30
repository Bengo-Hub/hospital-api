package handlers

import (
	"net/http"

	"github.com/bengobox/hospital-service/internal/modules/identity"
)

// ConfigHandler exposes a read-only view of this tenant's resolved hospital-service
// configuration. Deliberately read-only and deliberately thin: facility_type/enabled_modules
// are a CACHE resolved from subscriptions-api's plan metadata (Tenant.metadata — see
// internal/ent/schema/tenant.go), not something a tenant admin edits here directly; branding/
// contact/identity fields are owned by auth-api per the tenant_data_architecture platform
// rule and are deliberately absent from this service's Tenant schema. There is currently
// nothing hospital-api-specific for a tenant admin to WRITE, so hospital.config.manage exists
// in the permission catalog but has no handler yet — add one the day a real writable setting
// (e.g. a hospital-api-only preference) actually needs it, rather than inventing scope now.
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
	var enabledModules []string
	if raw, ok := t.Metadata["enabled_modules"].([]any); ok {
		for _, m := range raw {
			if s, ok := m.(string); ok {
				enabledModules = append(enabledModules, s)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"tenant_name":     t.Name,
		"tenant_slug":     t.Slug,
		"status":          t.Status,
		"facility_type":   facilityType,
		"enabled_modules": enabledModules,
		"synced_at":       t.LastSyncAt,
	})
}

type outletDTO struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	IsHQ   bool   `json:"is_hq"`
	Status string `json:"status"`
}

// ListOutlets handles GET /{tenant}/hospital/outlets — the source list for hospital-ui's outlet
// switcher (2026-08-30). Single-outlet tenants (the overwhelming majority today — Chemist/Clinic
// tier) get back exactly one row and the frontend skips rendering a switcher at all.
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
	out := make([]outletDTO, 0, len(outlets))
	for _, o := range outlets {
		out = append(out, outletDTO{ID: o.ID.String(), Code: o.Code, Name: o.Name, IsHQ: o.IsHq, Status: o.Status})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}
