package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/mar"
)

// MARHandler implements the Medication Administration Record HTTP surface.
type MARHandler struct {
	svc *mar.Service
}

// NewMARHandler creates a new MARHandler.
func NewMARHandler(svc *mar.Service) *MARHandler {
	return &MARHandler{svc: svc}
}

type chartDoseRequest struct {
	PrescriptionLineID string     `json:"prescription_line_id"`
	ScheduledTime      *time.Time `json:"scheduled_time,omitempty"`
	Status             string     `json:"status"`
	Notes              string     `json:"notes,omitempty"`
}

// ChartDose handles POST /{tenant}/hospital/admissions/{admissionID}/mar
func (h *MARHandler) ChartDose(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	admissionID, err := uuid.Parse(chi.URLParam(r, "admissionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid admission ID")
		return
	}
	var in chartDoseRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lineID, err := uuid.Parse(in.PrescriptionLineID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription_line_id")
		return
	}
	entry, err := h.svc.ChartDose(r.Context(), tenantID, mar.ChartDoseRequest{
		AdmissionID: admissionID, PrescriptionLineID: lineID, ScheduledTime: in.ScheduledTime,
		Status: in.Status, AdministeredBy: currentUserID(r), Notes: in.Notes,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, entry)
}

// ListByAdmission handles GET /{tenant}/hospital/admissions/{admissionID}/mar
func (h *MARHandler) ListByAdmission(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	admissionID, err := uuid.Parse(chi.URLParam(r, "admissionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid admission ID")
		return
	}
	list, err := h.svc.ListByAdmission(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list medication administrations")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// ListActivePrescriptions handles GET /{tenant}/hospital/admissions/{admissionID}/mar/prescriptions
// Powers the "chart a dose" drug picker — every non-cancelled/rejected prescription for this
// admission's own visit, with lines eager-loaded.
func (h *MARHandler) ListActivePrescriptions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	admissionID, err := uuid.Parse(chi.URLParam(r, "admissionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid admission ID")
		return
	}
	list, err := h.svc.ListActivePrescriptionLines(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list prescriptions")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}
