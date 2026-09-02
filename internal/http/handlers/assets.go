package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/inventory"
)

// AssetsHandler implements a read-only "Biomedical Equipment" surface over inventory-api's
// existing fixed-asset register — reference only, hospital-api never owns asset data. Brought
// forward from Sprint 9's original plan once Sprint 6/7 needed to link equipment to a
// Bed/TheatreBooking/ICUEpisode. See docs/architecture.md's "Biomedical Equipment / Asset
// Integration" section.
type AssetsHandler struct {
	inv *inventory.Client
}

// NewAssetsHandler creates a new AssetsHandler.
func NewAssetsHandler(inv *inventory.Client) *AssetsHandler {
	return &AssetsHandler{inv: inv}
}

// ListAssets handles GET /{tenant}/hospital/assets?search=
func (h *AssetsHandler) ListAssets(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	if !h.inv.Enabled() {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	assets, err := h.inv.ListAssets(r.Context(), tenantID, r.URL.Query().Get("search"))
	if err != nil {
		respondError(w, http.StatusBadGateway, "failed to list biomedical equipment")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": assets})
}

// GetAsset handles GET /{tenant}/hospital/assets/{assetID}
func (h *AssetsHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	assetID, err := uuid.Parse(chi.URLParam(r, "assetID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid asset ID")
		return
	}
	if !h.inv.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "inventory integration not configured")
		return
	}
	asset, err := h.inv.GetAsset(r.Context(), tenantID, assetID)
	if err != nil {
		respondError(w, http.StatusNotFound, "asset not found")
		return
	}
	respondJSON(w, http.StatusOK, asset)
}

// ListAssetMaintenance handles GET /{tenant}/hospital/assets/{assetID}/maintenance
func (h *AssetsHandler) ListAssetMaintenance(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	assetID, err := uuid.Parse(chi.URLParam(r, "assetID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid asset ID")
		return
	}
	if !h.inv.Enabled() {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	records, err := h.inv.ListAssetMaintenance(r.Context(), tenantID, assetID)
	if err != nil {
		respondError(w, http.StatusBadGateway, "failed to list maintenance history")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": records})
}
