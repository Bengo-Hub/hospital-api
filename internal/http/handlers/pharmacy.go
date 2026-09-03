package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/identity"
	"github.com/bengobox/hospital-service/internal/modules/pharmacy"
	"github.com/bengobox/hospital-service/internal/modules/printing"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// PharmacyHandler implements Sprint 4's prescription lifecycle/dispensing HTTP surface.
type PharmacyHandler struct {
	svc         *pharmacy.Service
	rbacSvc     outletmw.PermissionChecker
	identitySvc *identity.Service
}

// NewPharmacyHandler creates a new PharmacyHandler.
func NewPharmacyHandler(svc *pharmacy.Service, rbacSvc outletmw.PermissionChecker) *PharmacyHandler {
	return &PharmacyHandler{svc: svc, rbacSvc: rbacSvc}
}

// SetIdentityService wires the identity service used to auto-thread an internal clinician's
// professional_registration_number into CreatePrescription — late-bound (mirrors identity.
// Service.SetRBACService/SetAuditWriter's own optional, always-safe contract), since
// PharmacyHandler is constructed before nothing else here needs identitySvc.
func (h *PharmacyHandler) SetIdentityService(svc *identity.Service) {
	h.identitySvc = svc
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
	// Auto-thread a professional registration number for an internal clinician (not an external/
	// chemist walk-in prescriber, which already carries its own free-text prescriber_license) who
	// didn't already supply one — mvp-gap-backlog-2026-09-02.md's RBAC/user-management item.
	if in.ExternalFacilityName == "" && in.PrescriberLicense == "" && h.identitySvc != nil {
		if user, uerr := h.identitySvc.GetUserByAuthServiceID(r.Context(), tenantID, currentUserID(r)); uerr == nil {
			in.PrescriberLicense = user.ProfessionalRegistrationNumber
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

type recheckInteractionsRequest struct {
	AllergyFlags []string `json:"allergy_flags,omitempty"`
}

// RecheckInteractions handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/recheck-interactions
func (h *PharmacyHandler) RecheckInteractions(w http.ResponseWriter, r *http.Request) {
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
	var in recheckInteractionsRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	check, rx, err := h.svc.RecheckInteractions(r.Context(), tenantID, id, in.AllergyFlags)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"check": check, "prescription": rx})
}

// CreateRefill handles POST /{tenant}/hospital/prescriptions/{prescriptionID}/refill
func (h *PharmacyHandler) CreateRefill(w http.ResponseWriter, r *http.Request) {
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
	rx, err := h.svc.CreateRefill(r.Context(), tenantID, id)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, rx)
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

// PrintLabel handles GET /{tenant}/hospital/prescriptions/{prescriptionID}/lines/{lineID}/label.pdf
// — a dispensing label for one dispensed line (2026-08-30, previously absent on both ends). See
// internal/modules/printing/dispensing_label.go's doc comment for why this is a PDF rather than
// pos-api's original ESC/POS print-agent job (hospital-api has no print-agent/printer-profile
// infrastructure to reuse for that).
func (h *PharmacyHandler) PrintLabel(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	rxID, err := uuid.Parse(chi.URLParam(r, "prescriptionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid prescription ID")
		return
	}
	lineID, err := uuid.Parse(chi.URLParam(r, "lineID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid line ID")
		return
	}
	rx, err := h.svc.GetPrescription(r.Context(), tenantID, rxID)
	if err != nil {
		respondError(w, http.StatusNotFound, "prescription not found")
		return
	}
	var line *ent.PrescriptionLine
	for _, l := range rx.Edges.Lines {
		if l.ID == lineID {
			line = l
			break
		}
	}
	if line == nil {
		respondError(w, http.StatusNotFound, "prescription line not found")
		return
	}
	if line.QuantityDispensed <= 0 {
		respondError(w, http.StatusBadRequest, "this line has not been dispensed yet")
		return
	}

	tenantName := httpware.GetTenantSlug(r.Context())
	dispensedBy := ""
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		dispensedBy = claims.Email
	}
	pdfBytes, err := printing.BuildDispensingLabelPDF(printing.DispensingLabelData{
		TenantName:     tenantName,
		DrugName:       line.DrugName,
		Dosage:         line.Dosage,
		Form:           line.Form,
		Instructions:   line.Instructions,
		Quantity:       line.QuantityDispensed,
		PatientName:    rx.PatientName,
		LotNumber:      line.LotNumber,
		ExpiryDate:     line.ExpiryDate,
		PrescriberName: rx.PrescriberName,
		DispensedBy:    dispensedBy,
		// PrescriptionLine has no created_at/dispensed_at timestamp of its own — this is the
		// label's print time, not the exact dispense moment, which is close enough for a
		// physical drug label (it's normally printed right at dispense).
		DispensedAt:    time.Now(),
		PrescriptionNo: rx.PrescriptionNumber,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to render label")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\"dispensing-label.pdf\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
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
	// Same per-charge-module gate CollectCharge enforces on the cash path (see billing.go's
	// sourceModulePermission) — the route-level RequireServicePermission only checks collect_own
	// OR collect_any, which would let a collect_own-only holder claim PHARMACY charges via
	// insurance without holding pharmacy-dispensing permission. A prescription's charges are
	// always source_module="pharmacy", so this is a single fixed check, not a charge-list scan.
	if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectAny) {
		if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectOwn) ||
			!outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermPharmacyDispense) {
			respondError(w, http.StatusForbidden, "you do not have permission to claim this prescription's charges")
			return
		}
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

// SearchDrugs handles GET /{tenant}/hospital/pharmacy/drug-search?q= — a thin proxy to
// inventory-api's real item catalog so a prescription line can be picked from a live search
// instead of hand-typed free text, and so the frontend never needs its own inventory-api
// credentials.
func (h *PharmacyHandler) SearchDrugs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	items, err := h.svc.SearchDrugs(r.Context(), tenantID, r.URL.Query().Get("q"))
	if err != nil {
		respondError(w, http.StatusBadGateway, "drug search failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

// ListWalkInSales handles GET /{tenant}/hospital/pharmacy/walk-in-sales?status=
func (h *PharmacyHandler) ListWalkInSales(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListWalkInSales(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list walk-in sales")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type collectWalkInSaleRequest struct {
	PaymentMethod string `json:"payment_method"`
	PhoneNumber   string `json:"phone_number,omitempty"`
	OutletID      string `json:"outlet_id,omitempty"`
}

// CollectWalkInSale handles POST /{tenant}/hospital/pharmacy/walk-in-sales/{saleID}/collect
//
// Same fine-grained gate CollectCharge/SubmitInsuranceClaim already enforce for a pharmacy
// charge: collect_any (Billing desk) may collect anything, collect_own additionally requires
// pharmacy.dispense since a walk-in sale is always pharmacy-sourced by construction.
func (h *PharmacyHandler) CollectWalkInSale(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	saleID, err := uuid.Parse(chi.URLParam(r, "saleID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid sale ID")
		return
	}
	if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectAny) {
		if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectOwn) ||
			!outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermPharmacyDispense) {
			respondError(w, http.StatusForbidden, "you do not have permission to collect this walk-in sale")
			return
		}
	}
	var in collectWalkInSaleRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outletID := currentOutletID(r)
	req := billing.CollectWalkInSaleRequest{
		PaymentMethod: in.PaymentMethod, PhoneNumber: in.PhoneNumber,
		CollectedBy: currentUserID(r), OutletID: &outletID,
	}
	sale, err := h.svc.CollectWalkInSale(r.Context(), tenantID, saleID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, sale)
}

// WaiveWalkInSale handles POST /{tenant}/hospital/pharmacy/walk-in-sales/{saleID}/waive
func (h *PharmacyHandler) WaiveWalkInSale(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	saleID, err := uuid.Parse(chi.URLParam(r, "saleID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid sale ID")
		return
	}
	sale, err := h.svc.WaiveWalkInSale(r.Context(), tenantID, saleID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, sale)
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
