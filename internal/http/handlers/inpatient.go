package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/inpatient"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// InpatientHandler implements Sprint 6's ward/bed/admission/transfer/discharge HTTP surface.
type InpatientHandler struct {
	svc     *inpatient.Service
	rbacSvc outletmw.PermissionChecker
}

// NewInpatientHandler creates a new InpatientHandler.
func NewInpatientHandler(svc *inpatient.Service, rbacSvc outletmw.PermissionChecker) *InpatientHandler {
	return &InpatientHandler{svc: svc, rbacSvc: rbacSvc}
}

// ── Wards ────────────────────────────────────────────────────────────────────────────────────

type createWardRequest struct {
	Name             string `json:"name"`
	WardType         string `json:"ward_type,omitempty"`
	Capacity         int    `json:"capacity,omitempty"`
	BillableItemCode string `json:"billable_item_code,omitempty"`
}

// CreateWard handles POST /{tenant}/hospital/wards
func (h *InpatientHandler) CreateWard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in createWardRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outletID := currentOutletID(r)
	ward, err := h.svc.CreateWard(r.Context(), tenantID, outletID, in.Name, in.WardType, in.Capacity)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.BillableItemCode != "" {
		if updated, uerr := h.svc.SetWardBillableItemCode(r.Context(), tenantID, ward.ID, in.BillableItemCode); uerr == nil {
			ward = updated
		}
	}
	respondJSON(w, http.StatusCreated, ward)
}

type updateWardRequest struct {
	Name                  *string `json:"name,omitempty"`
	WardType              *string `json:"ward_type,omitempty"`
	Capacity              *int    `json:"capacity,omitempty"`
	BillableItemCode      *string `json:"billable_item_code,omitempty"`
	ClearBillableItemCode bool    `json:"clear_billable_item_code,omitempty"`
	IsActive              *bool   `json:"is_active,omitempty"`
}

// UpdateWard handles PUT /{tenant}/hospital/wards/{wardID}
func (h *InpatientHandler) UpdateWard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	wardID, err := uuid.Parse(chi.URLParam(r, "wardID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ward ID")
		return
	}
	var in updateWardRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.svc.UpdateWard(r.Context(), tenantID, wardID, inpatient.WardUpdate{
		Name: in.Name, WardType: in.WardType, Capacity: in.Capacity,
		BillableItemCode: in.BillableItemCode, ClearBillableItemCode: in.ClearBillableItemCode,
		IsActive: in.IsActive,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// ListWards handles GET /{tenant}/hospital/wards
func (h *InpatientHandler) ListWards(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var outletFilter *uuid.UUID
	if outletID := currentOutletID(r); outletID != uuid.Nil {
		outletFilter = &outletID
	}
	list, err := h.svc.ListWards(r.Context(), tenantID, outletFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list wards")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetWardOccupancy handles GET /{tenant}/hospital/wards/{wardID}/occupancy
func (h *InpatientHandler) GetWardOccupancy(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	wardID, err := uuid.Parse(chi.URLParam(r, "wardID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ward ID")
		return
	}
	ward, beds, err := h.svc.GetWardOccupancy(r.Context(), tenantID, wardID)
	if err != nil {
		respondError(w, http.StatusNotFound, "ward not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ward": ward, "beds": beds})
}

// ── Beds ─────────────────────────────────────────────────────────────────────────────────────

type createBedRequest struct {
	BedNumber string `json:"bed_number"`
}

// CreateBed handles POST /{tenant}/hospital/wards/{wardID}/beds
func (h *InpatientHandler) CreateBed(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	wardID, err := uuid.Parse(chi.URLParam(r, "wardID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ward ID")
		return
	}
	var in createBedRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bed, err := h.svc.CreateBed(r.Context(), tenantID, wardID, in.BedNumber)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, bed)
}

// ListBeds handles GET /{tenant}/hospital/wards/{wardID}/beds
func (h *InpatientHandler) ListBeds(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	wardID, err := uuid.Parse(chi.URLParam(r, "wardID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ward ID")
		return
	}
	list, err := h.svc.ListBeds(r.Context(), tenantID, wardID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list beds")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type setBedStatusRequest struct {
	Status string `json:"status"`
}

// SetBedStatus handles PATCH /{tenant}/hospital/beds/{bedID}/status
func (h *InpatientHandler) SetBedStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bedID, err := uuid.Parse(chi.URLParam(r, "bedID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid bed ID")
		return
	}
	var in setBedStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bed, err := h.svc.SetBedStatus(r.Context(), tenantID, bedID, in.Status)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, bed)
}

type setBedIsolationPrecautionRequest struct {
	IsolationPrecaution string `json:"isolation_precaution"`
}

// SetBedIsolationPrecaution handles PATCH /{tenant}/hospital/beds/{bedID}/isolation-precaution
func (h *InpatientHandler) SetBedIsolationPrecaution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bedID, err := uuid.Parse(chi.URLParam(r, "bedID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid bed ID")
		return
	}
	var in setBedIsolationPrecautionRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bed, err := h.svc.SetBedIsolationPrecaution(r.Context(), tenantID, bedID, in.IsolationPrecaution)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, bed)
}

type setBedEquipmentRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

// SetBedEquipment handles PUT /{tenant}/hospital/beds/{bedID}/equipment
func (h *InpatientHandler) SetBedEquipment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bedID, err := uuid.Parse(chi.URLParam(r, "bedID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid bed ID")
		return
	}
	var in setBedEquipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	assetIDs := make([]uuid.UUID, 0, len(in.AssetIDs))
	for _, s := range in.AssetIDs {
		if id, perr := uuid.Parse(s); perr == nil {
			assetIDs = append(assetIDs, id)
		}
	}
	bed, err := h.svc.SetBedEquipment(r.Context(), tenantID, bedID, assetIDs)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, bed)
}

// ── Admissions ───────────────────────────────────────────────────────────────────────────────

type admitRequest struct {
	VisitID                     string `json:"visit_id"`
	BedID                       string `json:"bed_id"`
	InsuranceGuaranteeReference string `json:"insurance_guarantee_reference,omitempty"`
	IsolationPrecaution         string `json:"isolation_precaution,omitempty"`
}

// Admit handles POST /{tenant}/hospital/admissions
func (h *InpatientHandler) Admit(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in admitRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visitID, err := uuid.Parse(in.VisitID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit_id")
		return
	}
	bedID, err := uuid.Parse(in.BedID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid bed_id")
		return
	}
	adm, err := h.svc.Admit(r.Context(), tenantID, inpatient.AdmitRequest{
		VisitID: visitID, BedID: bedID, AdmittedBy: currentUserID(r),
		InsuranceGuaranteeReference: in.InsuranceGuaranteeReference,
		IsolationPrecaution:         in.IsolationPrecaution,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, adm)
}

// ListAdmissions handles GET /{tenant}/hospital/admissions?status=
func (h *InpatientHandler) ListAdmissions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListAdmissions(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list admissions")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetAdmission handles GET /{tenant}/hospital/admissions/{admissionID}
func (h *InpatientHandler) GetAdmission(w http.ResponseWriter, r *http.Request) {
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
	adm, err := h.svc.GetAdmission(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "admission not found")
		return
	}
	respondJSON(w, http.StatusOK, adm)
}

type transferRequest struct {
	TransferType          string `json:"transfer_type"`
	ToWardID              string `json:"to_ward_id,omitempty"`
	ToBedID               string `json:"to_bed_id,omitempty"`
	ReceivingFacilityName string `json:"receiving_facility_name,omitempty"`
	ReferralID            string `json:"referral_id,omitempty"`
	AmbulanceBookingID    string `json:"ambulance_booking_id,omitempty"`
	Reason                string `json:"reason,omitempty"`
	OverrideReason        string `json:"override_reason,omitempty"`
}

// Transfer handles POST /{tenant}/hospital/admissions/{admissionID}/transfer — intra-facility
// (ward/bed move) or inter-facility (transfer-out, closes the admission). An inter-facility
// transfer with an outstanding balance additionally requires PermBillingOverrideSettlement to
// supply override_reason, mirroring Discharge's own gate exactly.
func (h *InpatientHandler) Transfer(w http.ResponseWriter, r *http.Request) {
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
	var in transferRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.OverrideReason != "" && !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingOverrideSettlement) {
		respondError(w, http.StatusForbidden, "you do not have permission to override settlement")
		return
	}
	req := inpatient.TransferRequest{
		TransferType:          in.TransferType,
		ReceivingFacilityName: in.ReceivingFacilityName,
		Reason:                in.Reason,
		OverrideReason:        in.OverrideReason,
		TransferredBy:         currentUserID(r),
	}
	if in.ToWardID != "" {
		if id, perr := uuid.Parse(in.ToWardID); perr == nil {
			req.ToWardID = &id
		}
	}
	if in.ToBedID != "" {
		if id, perr := uuid.Parse(in.ToBedID); perr == nil {
			req.ToBedID = &id
		}
	}
	if in.ReferralID != "" {
		if id, perr := uuid.Parse(in.ReferralID); perr == nil {
			req.ReferralID = &id
		}
	}
	if in.AmbulanceBookingID != "" {
		if id, perr := uuid.Parse(in.AmbulanceBookingID); perr == nil {
			req.AmbulanceBookingID = &id
		}
	}
	adm, acct, err := h.svc.Transfer(r.Context(), tenantID, admissionID, req)
	if err != nil {
		if outstanding, isOutstanding := err.(*inpatient.ErrOutstandingBalance); isOutstanding {
			respondJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "account": outstanding.Account})
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"admission": adm, "account": acct})
}

type dischargeRequest struct {
	Summary              string `json:"summary,omitempty"`
	OverrideReason       string `json:"override_reason,omitempty"`
	DischargeDiagnosis   string `json:"discharge_diagnosis,omitempty"`
	ProceduresPerformed  string `json:"procedures_performed,omitempty"`
	DischargeMedications string `json:"discharge_medications,omitempty"`
	FollowUpInstructions string `json:"follow_up_instructions,omitempty"`
	ConditionAtDischarge string `json:"condition_at_discharge,omitempty"`
}

// Discharge handles POST /{tenant}/hospital/admissions/{admissionID}/discharge
func (h *InpatientHandler) Discharge(w http.ResponseWriter, r *http.Request) {
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
	var in dischargeRequest
	_ = json.NewDecoder(r.Body).Decode(&in) // summary/override are optional — an empty body is fine
	if in.OverrideReason != "" && !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingOverrideSettlement) {
		respondError(w, http.StatusForbidden, "you do not have permission to override settlement")
		return
	}
	adm, acct, err := h.svc.Discharge(r.Context(), tenantID, admissionID, inpatient.DischargeRequest{
		DischargedBy:         currentUserID(r),
		Summary:              in.Summary,
		OverrideReason:       in.OverrideReason,
		DischargeDiagnosis:   in.DischargeDiagnosis,
		ProceduresPerformed:  in.ProceduresPerformed,
		DischargeMedications: in.DischargeMedications,
		FollowUpInstructions: in.FollowUpInstructions,
		ConditionAtDischarge: in.ConditionAtDischarge,
	})
	if err != nil {
		if outstanding, isOutstanding := err.(*inpatient.ErrOutstandingBalance); isOutstanding {
			respondJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "account": outstanding.Account})
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"admission": adm, "account": acct})
}

// ListTransfers handles GET /{tenant}/hospital/admissions/{admissionID}/transfers
func (h *InpatientHandler) ListTransfers(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListTransfersByAdmission(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list transfers")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// ── Nursing vitals chart / doctor's ward rounds ─────────────────────────────────────────────

type recordVitalsChartRequest struct {
	BPSystolic         *int     `json:"bp_systolic,omitempty"`
	BPDiastolic        *int     `json:"bp_diastolic,omitempty"`
	TemperatureCelsius *float64 `json:"temperature_celsius,omitempty"`
	PulseBPM           *int     `json:"pulse_bpm,omitempty"`
	RespirationRate    *int     `json:"respiration_rate,omitempty"`
	SpO2Percent        *float64 `json:"spo2_percent,omitempty"`
	PainScore          *int     `json:"pain_score,omitempty"`
	Notes              string   `json:"notes,omitempty"`
}

// RecordVitalsChart handles POST /{tenant}/hospital/admissions/{admissionID}/vitals-chart
func (h *InpatientHandler) RecordVitalsChart(w http.ResponseWriter, r *http.Request) {
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
	var in recordVitalsChartRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry, err := h.svc.RecordVitalsChart(r.Context(), tenantID, inpatient.RecordVitalsChartRequest{
		AdmissionID: admissionID, RecordedBy: currentUserID(r),
		BPSystolic: in.BPSystolic, BPDiastolic: in.BPDiastolic, TemperatureCelsius: in.TemperatureCelsius,
		PulseBPM: in.PulseBPM, RespirationRate: in.RespirationRate, SpO2Percent: in.SpO2Percent,
		PainScore: in.PainScore, Notes: in.Notes,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, entry)
}

// ListVitalsChart handles GET /{tenant}/hospital/admissions/{admissionID}/vitals-chart
func (h *InpatientHandler) ListVitalsChart(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListVitalsChart(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list vitals chart")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type recordWardRoundRequest struct {
	Notes         string `json:"notes"`
	DiagnosisCode string `json:"diagnosis_code,omitempty"`
	DiagnosisName string `json:"diagnosis_name,omitempty"`
}

// RecordWardRound handles POST /{tenant}/hospital/admissions/{admissionID}/ward-rounds
func (h *InpatientHandler) RecordWardRound(w http.ResponseWriter, r *http.Request) {
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
	var in recordWardRoundRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	note, err := h.svc.RecordWardRound(r.Context(), tenantID, inpatient.RecordWardRoundRequest{
		AdmissionID: admissionID, ClinicianID: currentUserID(r),
		Notes: in.Notes, DiagnosisCode: in.DiagnosisCode, DiagnosisName: in.DiagnosisName,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, note)
}

// ListWardRounds handles GET /{tenant}/hospital/admissions/{admissionID}/ward-rounds
func (h *InpatientHandler) ListWardRounds(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListWardRounds(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list ward rounds")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}
