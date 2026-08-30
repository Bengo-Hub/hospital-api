package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/identity"
)

// UserOutletsHandler implements the per-user outlet/branch assignment admin surface: which of
// the tenant's outlets a given staff member may operate in (enforced server-side by
// internal/http/middleware/outlet_context.go's OutletContextMiddleware, not just a UI
// convenience default).
type UserOutletsHandler struct {
	identitySvc *identity.Service
}

// NewUserOutletsHandler creates a new UserOutletsHandler.
func NewUserOutletsHandler(identitySvc *identity.Service) *UserOutletsHandler {
	return &UserOutletsHandler{identitySvc: identitySvc}
}

type userOutletDTO struct {
	ID           string `json:"id"`
	OutletID     string `json:"outlet_id"`
	IsHomeOutlet bool   `json:"is_home_outlet"`
	AssignedAt   string `json:"assigned_at"`
}

// ListUserOutlets handles GET /{tenant}/hospital/users/{userID}/outlets
func (h *UserOutletsHandler) ListUserOutlets(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.identitySvc.ListUserOutlets(r.Context(), tenantID, userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list outlet assignments")
		return
	}
	out := make([]userOutletDTO, 0, len(rows))
	for _, a := range rows {
		out = append(out, userOutletDTO{
			ID: a.ID.String(), OutletID: a.OutletID.String(), IsHomeOutlet: a.IsHomeOutlet,
			AssignedAt: a.AssignedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

type assignUserOutletRequest struct {
	OutletID     string `json:"outlet_id"`
	IsHomeOutlet bool   `json:"is_home_outlet"`
}

// AssignUserOutlet handles POST /{tenant}/hospital/users/{userID}/outlets
func (h *UserOutletsHandler) AssignUserOutlet(w http.ResponseWriter, r *http.Request) {
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
	var req assignUserOutletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outletID, err := uuid.Parse(req.OutletID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "outlet_id is required")
		return
	}
	if err := h.identitySvc.AssignUserOutlet(r.Context(), tenantID, currentUserID(r), userID, outletID, req.IsHomeOutlet); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"status": "assigned"})
}

// RemoveUserOutlet handles DELETE /{tenant}/hospital/users/{userID}/outlets/{outletID}
func (h *UserOutletsHandler) RemoveUserOutlet(w http.ResponseWriter, r *http.Request) {
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
	outletID, err := uuid.Parse(chi.URLParam(r, "outletID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid outlet id")
		return
	}
	if err := h.identitySvc.RemoveUserOutlet(r.Context(), tenantID, currentUserID(r), userID, outletID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "removed"})
}
