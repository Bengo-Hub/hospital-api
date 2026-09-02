package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/consultation"
)

// ConsultationHandler implements Sprint 2's examination/diagnosis-catalog/referral HTTP surface.
type ConsultationHandler struct {
	svc *consultation.Service
}

// NewConsultationHandler creates a new ConsultationHandler.
func NewConsultationHandler(svc *consultation.Service) *ConsultationHandler {
	return &ConsultationHandler{svc: svc}
}

type recordExaminationRequest struct {
	QueueType            string            `json:"queue_type,omitempty"`
	ChiefComplaint       string            `json:"chief_complaint,omitempty"`
	DiagnosisCode        string            `json:"diagnosis_code,omitempty"`
	DiagnosisName        string            `json:"diagnosis_name,omitempty"`
	ReviewOfSystems      map[string]string `json:"review_of_systems,omitempty"`
	PhysicalExamFindings map[string]string `json:"physical_exam_findings,omitempty"`
	TreatmentPlan        string            `json:"treatment_plan,omitempty"`
	Notes                string            `json:"notes,omitempty"`
	Complete             bool              `json:"complete,omitempty"`
}

// RecordExamination handles POST /{tenant}/hospital/visits/{visitID}/examination
func (h *ConsultationHandler) RecordExamination(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	visitID, err := uuid.Parse(chi.URLParam(r, "visitID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit ID")
		return
	}
	var in recordExaminationRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rec, err := h.svc.RecordExamination(r.Context(), tenantID, consultation.RecordExaminationRequest{
		VisitID: visitID, ClinicianID: currentUserID(r), QueueType: in.QueueType,
		ChiefComplaint: in.ChiefComplaint, DiagnosisCode: in.DiagnosisCode,
		DiagnosisName: in.DiagnosisName, ReviewOfSystems: in.ReviewOfSystems,
		PhysicalExamFindings: in.PhysicalExamFindings, TreatmentPlan: in.TreatmentPlan,
		Notes: in.Notes, Complete: in.Complete,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, rec)
}

// GetLatestExamination handles GET /{tenant}/hospital/visits/{visitID}/examination
// Returns 404 (via respondError, not a raw ent.NotFoundError) when the visit hasn't been
// examined yet — a normal, expected state for a freshly-triaged visit, not a real error.
func (h *ConsultationHandler) GetLatestExamination(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	visitID, err := uuid.Parse(chi.URLParam(r, "visitID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit ID")
		return
	}
	rec, err := h.svc.GetLatestExaminationByVisit(r.Context(), tenantID, visitID)
	if err != nil {
		respondError(w, http.StatusNotFound, "no examination recorded for this visit yet")
		return
	}
	respondJSON(w, http.StatusOK, rec)
}

// ListDiagnosisCatalog handles GET /{tenant}/hospital/diagnosis-catalog
func (h *ConsultationHandler) ListDiagnosisCatalog(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListDiagnosisCatalog(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list diagnosis catalog")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type createDiagnosisEntryRequest struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
}

// CreateDiagnosisEntry handles POST /{tenant}/hospital/diagnosis-catalog
func (h *ConsultationHandler) CreateDiagnosisEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in createDiagnosisEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	e, err := h.svc.CreateDiagnosisEntry(r.Context(), tenantID, consultation.CreateDiagnosisEntryRequest{
		Code: in.Code, Name: in.Name, Category: in.Category,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, e)
}

type createReferralRequest struct {
	ReferredTo string `json:"referred_to"`
	Reason     string `json:"reason,omitempty"`
}

// CreateReferral handles POST /{tenant}/hospital/visits/{visitID}/refer
func (h *ConsultationHandler) CreateReferral(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	visitID, err := uuid.Parse(chi.URLParam(r, "visitID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit ID")
		return
	}
	var in createReferralRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ref, err := h.svc.CreateReferral(r.Context(), tenantID, consultation.CreateReferralRequest{
		VisitID: visitID, ReferredTo: in.ReferredTo, Reason: in.Reason, ReferredBy: currentUserID(r),
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, ref)
}

// ListReferrals handles GET /{tenant}/hospital/visits/{visitID}/referrals — consultation.Service
// already implemented this; it had no handler or route (2026-08-30 fix), so a referral could be
// created but never listed/viewed anywhere.
func (h *ConsultationHandler) ListReferrals(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	visitID, err := uuid.Parse(chi.URLParam(r, "visitID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit ID")
		return
	}
	list, err := h.svc.ListReferrals(r.Context(), tenantID, visitID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list referrals")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// CancelReferral handles POST /{tenant}/hospital/visits/{visitID}/refer/{referralID}/cancel
func (h *ConsultationHandler) CancelReferral(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	referralID, err := uuid.Parse(chi.URLParam(r, "referralID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid referral ID")
		return
	}
	ref, err := h.svc.CancelReferral(r.Context(), tenantID, referralID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, ref)
}
