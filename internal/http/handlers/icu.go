package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/icu"
)

// ICUHandler implements Sprint 7's critical-care monitoring HTTP surface.
type ICUHandler struct {
	svc *icu.Service
}

// NewICUHandler creates a new ICUHandler.
func NewICUHandler(svc *icu.Service) *ICUHandler {
	return &ICUHandler{svc: svc}
}

type startEpisodeRequest struct {
	AdmissionID     string `json:"admission_id"`
	SeverityFlag    string `json:"severity_flag,omitempty"`
	MonitoringNotes string `json:"monitoring_notes,omitempty"`
}

// StartEpisode handles POST /{tenant}/hospital/icu-episodes
func (h *ICUHandler) StartEpisode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in startEpisodeRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	admissionID, err := uuid.Parse(in.AdmissionID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid admission_id")
		return
	}
	episode, err := h.svc.StartEpisode(r.Context(), tenantID, icu.StartEpisodeRequest{
		AdmissionID: admissionID, SeverityFlag: in.SeverityFlag, MonitoringNotes: in.MonitoringNotes,
		StartedBy: currentUserID(r),
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, episode)
}

// ListEpisodes handles GET /{tenant}/hospital/icu-episodes?status=active
func (h *ICUHandler) ListEpisodes(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	activeOnly := r.URL.Query().Get("status") != "all"
	list, err := h.svc.ListEpisodes(r.Context(), tenantID, activeOnly)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list ICU episodes")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetEpisode handles GET /{tenant}/hospital/icu-episodes/{episodeID}
func (h *ICUHandler) GetEpisode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	episodeID, err := uuid.Parse(chi.URLParam(r, "episodeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid episode ID")
		return
	}
	episode, err := h.svc.GetEpisode(r.Context(), tenantID, episodeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "episode not found")
		return
	}
	respondJSON(w, http.StatusOK, episode)
}

type updateEpisodeRequest struct {
	SeverityFlag    *string  `json:"severity_flag,omitempty"`
	MonitoringNotes *string  `json:"monitoring_notes,omitempty"`
	EquipmentIDs    []string `json:"equipment_asset_ids,omitempty"`
}

// UpdateEpisode handles PATCH /{tenant}/hospital/icu-episodes/{episodeID}
func (h *ICUHandler) UpdateEpisode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	episodeID, err := uuid.Parse(chi.URLParam(r, "episodeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid episode ID")
		return
	}
	var in updateEpisodeRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := icu.UpdateEpisodeRequest{SeverityFlag: in.SeverityFlag, MonitoringNotes: in.MonitoringNotes}
	if in.EquipmentIDs != nil {
		assetIDs := make([]uuid.UUID, 0, len(in.EquipmentIDs))
		for _, s := range in.EquipmentIDs {
			if id, perr := uuid.Parse(s); perr == nil {
				assetIDs = append(assetIDs, id)
			}
		}
		req.EquipmentAssetIDs = &assetIDs
	}
	episode, err := h.svc.UpdateEpisode(r.Context(), tenantID, episodeID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, episode)
}

// EndEpisode handles POST /{tenant}/hospital/icu-episodes/{episodeID}/end
func (h *ICUHandler) EndEpisode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	episodeID, err := uuid.Parse(chi.URLParam(r, "episodeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid episode ID")
		return
	}
	episode, err := h.svc.EndEpisode(r.Context(), tenantID, episodeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, episode)
}
