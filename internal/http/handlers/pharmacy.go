package handlers

import (
	"encoding/json"
	"net/http"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/pharmacy"
)

// PharmacyHandler implements Sprint 4's prescription lifecycle/dispensing HTTP surface.
type PharmacyHandler struct {
	svc *pharmacy.Service
}

// NewPharmacyHandler creates a new PharmacyHandler.
func NewPharmacyHandler(svc *pharmacy.Service) *PharmacyHandler {
	return &PharmacyHandler{svc: svc}
}

type prescriptionLineRequest struct {
	InventoryItemSKU   string  `json:"inventory_item_sku,omitempty"`
	DrugName           string  `json:"drug_name"`
	Dosage             string  `json:"dosage,omitempty"`
	Form               string  `json:"form,omitempty"`
	Instructions       string  `json:"instructions,omitempty"`
	QuantityPrescribed float64 `json:"quantity_prescribed"`
	UnitPrice          float64 `json:"unit_price,omitempty"`
}

type createPrescriptionRequest struct {
	PatientID            string                    `json:"patient_id,omitempty"`
	VisitID              string                    `json:"visit_id,omitempty"`
	ExaminationID        string                    `json:"examination_id,omitempty"`
	ExternalFacilityName string                    `json:"external_facility_name,omitempty"`
	PrescriberName       string                    `json:"prescriber_name,omitempty"`
	PrescriberLicense    string                    `json:"prescriber_license,omitempty"`
	PatientName          string                    `json:"patient_name,omitempty"`
	PatientIDNumber      string                    `json:"patient_id_number,omitempty"`
	AllergyFlags         []string                  `json:"allergy_flags,omitempty"`
	OutletID             string                    `json:"outlet_id,omitempty"`
	Lines                []prescriptionLineRequest `json:"lines"`
}

func parseOptionalUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	if id, err := uuid.Parse(s); err == nil {
		return &id
	}
	return nil
}

// CreatePrescription handles POST /{tenant}/hospital/prescriptions
func (h *PharmacyHandler) CreatePrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in createPrescriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lines := make([]pharmacy.PrescriptionLineInput, 0, len(in.Lines))
	for _, l := range in.Lines {
		lines = append(lines, pharmacy.PrescriptionLineInput{
			InventoryItemSKU: l.InventoryItemSKU, DrugName: l.DrugName, Dosage: l.Dosage,
			Form: l.Form, Instructions: l.Instructions,
			QuantityPrescribed: l.QuantityPrescribed, UnitPrice: l.UnitPrice,
		})
	}
	outletID := currentOutletID(r)
	if in.OutletID != "" {
		if id, err := uuid.Parse(in.OutletID); err == nil {
			outletID = id
		}
	}
	rx, err := h.svc.CreatePrescription(r.Context(), tenantID, pharmacy.CreatePrescriptionRequest{
		OutletID: outletID, PatientID: parseOptionalUUID(in.PatientID), VisitID: parseOptionalUUID(in.VisitID),
		ExaminationID: parseOptionalUUID(in.ExaminationID), ExternalFacilityName: in.ExternalFacilityName,
		PrescriberName: in.PrescriberName, PrescriberLicense: in.PrescriberLicense,
		PatientName: in.PatientName, PatientIDNumber: in.PatientIDNumber,
		AllergyFlags: in.AllergyFlags, Lines: lines,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, rx)
}

// ListPrescriptions handles GET /{tenant}/hospital/prescriptions?status=
func (h *PharmacyHandler) ListPrescriptions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListPrescriptions(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list prescriptions")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetPrescription handles GET /{tenant}/hospital/prescriptions/{prescriptionID}
func (h *PharmacyHandler) GetPrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	rx, err := h.svc.GetPrescription(r.Context(), tenantID, id)
	if err != nil {
		respondError(w, http.StatusNotFound, "prescription not found")
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

type approveRequest struct {
	OverrideReason string `json:"override_reason,omitempty"`
}

// ApprovePrescription handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/approve
func (h *PharmacyHandler) ApprovePrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	var in approveRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	rx, err := h.svc.ApprovePrescription(r.Context(), tenantID, id, currentUserID(r), in.OverrideReason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

// LockPrescription handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/lock
func (h *PharmacyHandler) LockPrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	rx, err := h.svc.LockPrescription(r.Context(), tenantID, id)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

type reasonRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RejectPrescription handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/reject
func (h *PharmacyHandler) RejectPrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	var in reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	rx, err := h.svc.RejectPrescription(r.Context(), tenantID, id, in.Reason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

// CancelPrescription handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/cancel
func (h *PharmacyHandler) CancelPrescription(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	var in reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	rx, err := h.svc.CancelPrescription(r.Context(), tenantID, id, in.Reason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

type dispenseLineRequest struct {
	LineID             string  `json:"line_id"`
	QuantityToDispense float64 `json:"quantity_to_dispense"`
	RequiresWitness    bool    `json:"requires_witness,omitempty"`
	// WitnessToken is the short-lived token minted by POST .../pharmacy/verify-witness after
	// the witness re-authenticated their OWN credentials — see pharmacy.VerifyWitness. There is
	// deliberately no witness_staff_id field here any more: that field let any dispensing user
	// name ANY staff UUID as the "witness" with zero verification (the vulnerability this
	// endpoint used to have), so the client-suppliable identity path is fully closed rather than
	// left as a fallback.
	WitnessToken string `json:"witness_token,omitempty"`
}

type dispenseRequest struct {
	PatientName     string                `json:"patient_name,omitempty"`
	PatientIDNumber string                `json:"patient_id_number,omitempty"`
	OutletID        string                `json:"outlet_id,omitempty"`
	Lines           []dispenseLineRequest `json:"lines"`
}

// Dispense handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/dispense
func (h *PharmacyHandler) Dispense(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	var in dispenseRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lines := make([]pharmacy.DispenseLineInput, 0, len(in.Lines))
	for _, l := range in.Lines {
		lineID, perr := uuid.Parse(l.LineID)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "invalid line_id")
			return
		}
		line := pharmacy.DispenseLineInput{
			LineID: lineID, QuantityToDispense: l.QuantityToDispense,
			RequiresWitness: l.RequiresWitness,
		}
		if l.WitnessToken != "" {
			token := l.WitnessToken
			line.WitnessToken = &token
		}
		lines = append(lines, line)
	}
	outletID := currentOutletID(r)
	if in.OutletID != "" {
		if oid, perr := uuid.Parse(in.OutletID); perr == nil {
			outletID = oid
		}
	}
	rx, err := h.svc.Dispense(r.Context(), tenantID, id, pharmacy.DispenseRequest{
		DispensedBy: currentUserID(r), OutletID: outletID,
		PatientName: in.PatientName, PatientIDNumber: in.PatientIDNumber, Lines: lines,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rx)
}

type verifyWitnessRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

// VerifyWitness handles POST /{tenant}/hospital/pharmacy/verify-witness — Step 1 of the
// controlled-substance dual-witness fix: re-authenticates the witness with THEIR OWN
// email+password (never the calling/dispensing user's), verified server-side against auth-api's
// public /auth/login. On success this mints a short-lived witness_token that Dispense (Step 2)
// consumes for any line with requires_witness=true — see pharmacy.Service.VerifyWitness for the
// full identity/tenant/distinct-person/permission verification chain.
//
// All rejections use 403 (never an ambiguous fallback) — mirrors pos-api's step-up handler,
// which deliberately avoids 401 here so a wrong witness credential can never be mistaken by the
// frontend's global auth interceptor for the CALLING user's own session being invalid.
func (h *PharmacyHandler) VerifyWitness(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var in verifyWitnessRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.svc.VerifyWitness(r.Context(), tenantID, claims.GetTenantSlug(), currentUserID(r), pharmacy.VerifyWitnessRequest{
		Email:    in.Email,
		Password: in.Password,
		TOTPCode: in.TOTPCode,
	})
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	if result.MFARequired {
		respondJSON(w, http.StatusOK, map[string]any{
			"mfa_required": true,
			"mfa_method":   result.MFAMethod,
			"user_id":      result.MFAUserID,
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"witness_token": result.WitnessToken,
		"witness_name":  result.WitnessName,
		"expires_in":    result.ExpiresIn,
	})
}

type pharmacyInsuranceClaimRequest struct {
	ProviderID string   `json:"provider_id"`
	CoverageID string   `json:"coverage_id,omitempty"`
	OutletID   string   `json:"outlet_id,omitempty"`
	LineIDs    []string `json:"line_ids,omitempty"`
}

// SubmitInsuranceClaim handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/insurance-claim
// — the insurance-settlement alternative to a cash billing/charges/{id}/collect for this
// prescription's dispensed lines.
func (h *PharmacyHandler) SubmitInsuranceClaim(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	var in pharmacyInsuranceClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	providerID, err := uuid.Parse(in.ProviderID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider_id")
		return
	}
	req := pharmacy.SubmitInsuranceClaimRequest{ProviderID: providerID}
	if in.CoverageID != "" {
		if cid, perr := uuid.Parse(in.CoverageID); perr == nil {
			req.CoverageID = &cid
		}
	}
	if in.OutletID != "" {
		if oid, perr := uuid.Parse(in.OutletID); perr == nil {
			req.OutletID = &oid
		}
	} else if outletID := currentOutletID(r); outletID != uuid.Nil {
		req.OutletID = &outletID
	}
	for _, ls := range in.LineIDs {
		if lid, perr := uuid.Parse(ls); perr == nil {
			req.LineIDs = append(req.LineIDs, lid)
		}
	}
	rx, claim, err := h.svc.SubmitInsuranceClaim(r.Context(), tenantID, id, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"prescription": rx, "claim": claim})
}

// ListControlledSubstanceLogs handles GET /{tenant}/hospital/pharmacy/controlled-substances
func (h *PharmacyHandler) ListControlledSubstanceLogs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListControlledSubstanceLogs(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list controlled substance logs")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}
