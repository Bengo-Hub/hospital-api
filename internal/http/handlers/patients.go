package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/patients"
)

// PatientsHandler implements Sprint 1's patient/visit/triage HTTP surface.
type PatientsHandler struct {
	svc *patients.Service
}

// NewPatientsHandler creates a new PatientsHandler.
func NewPatientsHandler(svc *patients.Service) *PatientsHandler {
	return &PatientsHandler{svc: svc}
}

func tenantFromRequest(r *http.Request) (uuid.UUID, bool) {
	tid := httpware.GetTenantID(r.Context())
	if tid == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(tid)
	return id, err == nil
}

func currentOutletID(r *http.Request) uuid.UUID {
	if oc := outletmw.OutletFromContext(r.Context()); oc != nil {
		return oc.ID
	}
	return uuid.Nil
}

func currentUserID(r *http.Request) uuid.UUID {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		return uuid.Nil
	}
	id, _ := uuid.Parse(claims.Subject)
	return id
}

// ── Patients ─────────────────────────────────────────────────────────────────────────────

type registerPatientRequest struct {
	FullName  string     `json:"full_name"`
	DOB       *time.Time `json:"dob,omitempty"`
	Sex       string     `json:"sex,omitempty"`
	Phone     string     `json:"phone,omitempty"`
	IDNumber  string     `json:"id_number,omitempty"`
	Address   string     `json:"address,omitempty"`
	NextOfKin string     `json:"next_of_kin,omitempty"`
	Allergies []string   `json:"allergy_flags,omitempty"`
	OutletID  string     `json:"outlet_id,omitempty"`
}

// RegisterPatient handles POST /{tenant}/hospital/patients
func (h *PatientsHandler) RegisterPatient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in registerPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outletID := currentOutletID(r)
	if in.OutletID != "" {
		if id, err := uuid.Parse(in.OutletID); err == nil {
			outletID = id
		}
	}
	p, err := h.svc.RegisterPatient(r.Context(), tenantID, patients.RegisterPatientRequest{
		FullName: in.FullName, DOB: in.DOB, Sex: in.Sex, Phone: in.Phone,
		IDNumber: in.IDNumber, Address: in.Address, NextOfKin: in.NextOfKin,
		Allergies: in.Allergies, OutletID: outletID,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// ListPatients handles GET /{tenant}/hospital/patients?q=
func (h *PatientsHandler) ListPatients(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListPatients(r.Context(), tenantID, patients.ListPatientsRequest{
		Query: r.URL.Query().Get("q"),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list patients")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetPatient handles GET /{tenant}/hospital/patients/{patientID}
func (h *PatientsHandler) GetPatient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	patientID, err := uuid.Parse(chi.URLParam(r, "patientID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid patient ID")
		return
	}
	p, err := h.svc.GetPatient(r.Context(), tenantID, patientID)
	if err != nil {
		respondError(w, http.StatusNotFound, "patient not found")
		return
	}
	respondJSON(w, http.StatusOK, p)
}

type updatePatientRequest struct {
	FullName  *string    `json:"full_name,omitempty"`
	DOB       *time.Time `json:"dob,omitempty"`
	Sex       *string    `json:"sex,omitempty"`
	Phone     *string    `json:"phone,omitempty"`
	IDNumber  *string    `json:"id_number,omitempty"`
	Address   *string    `json:"address,omitempty"`
	NextOfKin *string    `json:"next_of_kin,omitempty"`
	Allergies *[]string  `json:"allergy_flags,omitempty"`
}

// UpdatePatient handles PUT /{tenant}/hospital/patients/{patientID}
func (h *PatientsHandler) UpdatePatient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	patientID, err := uuid.Parse(chi.URLParam(r, "patientID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid patient ID")
		return
	}
	var in updatePatientRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := h.svc.UpdatePatient(r.Context(), tenantID, patientID, patients.UpdatePatientRequest{
		FullName: in.FullName, DOB: in.DOB, Sex: in.Sex, Phone: in.Phone,
		IDNumber: in.IDNumber, Address: in.Address, NextOfKin: in.NextOfKin, Allergies: in.Allergies,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// ── Visits ───────────────────────────────────────────────────────────────────────────────

type checkInVisitRequest struct {
	PatientID      string `json:"patient_id"`
	OutletID       string `json:"outlet_id,omitempty"`
	VisitType      string `json:"visit_type,omitempty"`
	ChiefComplaint string `json:"chief_complaint,omitempty"`
}

// CheckInVisit handles POST /{tenant}/hospital/visits
func (h *PatientsHandler) CheckInVisit(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in checkInVisitRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	patientID, err := uuid.Parse(in.PatientID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid patient_id")
		return
	}
	outletID := currentOutletID(r)
	if in.OutletID != "" {
		if id, perr := uuid.Parse(in.OutletID); perr == nil {
			outletID = id
		}
	}
	registeredBy := currentUserID(r)
	v, err := h.svc.CheckInVisit(r.Context(), tenantID, patients.CheckInVisitRequest{
		PatientID: patientID, OutletID: outletID, VisitType: in.VisitType,
		ChiefComplaint: in.ChiefComplaint, RegisteredBy: &registeredBy,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, v)
}

// ListVisits handles GET /{tenant}/hospital/visits?status=
func (h *PatientsHandler) ListVisits(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	req := patients.ListVisitsRequest{Status: r.URL.Query().Get("status")}
	if pid := r.URL.Query().Get("patient_id"); pid != "" {
		if id, perr := uuid.Parse(pid); perr == nil {
			req.PatientID = &id
		}
	}
	list, err := h.svc.ListVisits(r.Context(), tenantID, req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list visits")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetVisit handles GET /{tenant}/hospital/visits/{visitID}
func (h *PatientsHandler) GetVisit(w http.ResponseWriter, r *http.Request) {
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
	v, err := h.svc.GetVisit(r.Context(), tenantID, visitID)
	if err != nil {
		respondError(w, http.StatusNotFound, "visit not found")
		return
	}
	respondJSON(w, http.StatusOK, v)
}

// ── Triage ───────────────────────────────────────────────────────────────────────────────

type recordTriageRequest struct {
	BPSystolic         *int     `json:"bp_systolic,omitempty"`
	BPDiastolic        *int     `json:"bp_diastolic,omitempty"`
	TemperatureCelsius *float64 `json:"temperature_celsius,omitempty"`
	PulseBPM           *int     `json:"pulse_bpm,omitempty"`
	RespirationRate    *int     `json:"respiration_rate,omitempty"`
	SpO2Percent        *float64 `json:"spo2_percent,omitempty"`
	WeightKg           *float64 `json:"weight_kg,omitempty"`
	HeightCm           *float64 `json:"height_cm,omitempty"`
	Priority           string   `json:"priority,omitempty"`
	Notes              string   `json:"notes,omitempty"`
}

// RecordTriage handles POST /{tenant}/hospital/visits/{visitID}/triage
func (h *PatientsHandler) RecordTriage(w http.ResponseWriter, r *http.Request) {
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
	var in recordTriageRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.RecordTriage(r.Context(), tenantID, patients.RecordTriageRequest{
		VisitID: visitID, TakenBy: currentUserID(r),
		BPSystolic: in.BPSystolic, BPDiastolic: in.BPDiastolic,
		TemperatureCelsius: in.TemperatureCelsius, PulseBPM: in.PulseBPM,
		RespirationRate: in.RespirationRate, SpO2Percent: in.SpO2Percent,
		WeightKg: in.WeightKg, HeightCm: in.HeightCm,
		Priority: in.Priority, Notes: in.Notes,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, t)
}
