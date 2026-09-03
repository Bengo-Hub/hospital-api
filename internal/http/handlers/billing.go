package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// BillingHandler implements Sprint 5's billing-ledger HTTP surface — GetAccount (the "receipt
// every department can see"), the collect/queue/settle/override actions.
type BillingHandler struct {
	svc     *billing.Service
	rbacSvc outletmw.PermissionChecker
}

// NewBillingHandler creates a new BillingHandler.
func NewBillingHandler(svc *billing.Service, rbacSvc outletmw.PermissionChecker) *BillingHandler {
	return &BillingHandler{svc: svc, rbacSvc: rbacSvc}
}

// sourceModulePermission maps a BillableCharge.source_module to the permission that lets a
// collect_own holder collect a charge from that department — a nurse can collect a
// triage-sourced charge only if they also hold hospital.triage.add, etc.
func sourceModulePermission(module string) string {
	switch module {
	case "records":
		return rbac.PermRecordsAdd
	case "reception":
		return rbac.PermReceptionAdd
	case "triage":
		return rbac.PermTriageAdd
	case "consultation":
		return rbac.PermConsultationAdd
	case "lab":
		return rbac.PermLabAdd
	case "pharmacy":
		return rbac.PermPharmacyDispense
	case "theatre":
		return rbac.PermTheatreAdd
	case "inpatient":
		return rbac.PermInpatientAdd
	default:
		return "" // unknown module: only collect_any can touch it
	}
}

// GetAccountByVisit handles GET /{tenant}/hospital/visits/{visitID}/account
func (h *BillingHandler) GetAccountByVisit(w http.ResponseWriter, r *http.Request) {
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
	acct, charges, err := h.svc.GetAccountByVisit(r.Context(), tenantID, visitID)
	if err != nil {
		respondError(w, http.StatusNotFound, "account not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"account": acct, "charges": charges})
}

// GetAccountByAdmission handles GET /{tenant}/hospital/admissions/{admissionID}/account —
// GetAccountByVisit's Sprint 6 counterpart for an inpatient stay's running ledger.
func (h *BillingHandler) GetAccountByAdmission(w http.ResponseWriter, r *http.Request) {
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
	acct, charges, err := h.svc.GetAccountByAdmission(r.Context(), tenantID, admissionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "account not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"account": acct, "charges": charges})
}

// ListPendingCharges handles GET /{tenant}/hospital/billing/queue?department=
func (h *BillingHandler) ListPendingCharges(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListPendingCharges(r.Context(), tenantID, r.URL.Query().Get("department"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list pending charges")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type collectChargeRequest struct {
	PaymentMethod string `json:"payment_method"`
	PhoneNumber   string `json:"phone_number,omitempty"`
	OutletID      string `json:"outlet_id,omitempty"`
}

// CollectCharge handles POST /{tenant}/hospital/billing/charges/{chargeID}/collect
//
// Permission resolution: collect_any (Billing desk) may collect anything. collect_own requires
// the caller to ALSO hold the charge's own source-module permission (e.g. a nurse needs
// hospital.triage.add to collect a triage charge) — enforced here rather than at the router
// layer since it depends on the specific charge being collected, not just the route.
func (h *BillingHandler) CollectCharge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	chargeID, err := uuid.Parse(chi.URLParam(r, "chargeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid charge ID")
		return
	}

	if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectAny) {
		charge, cerr := h.svc.PeekCharge(r.Context(), tenantID, chargeID)
		if cerr != nil {
			respondError(w, http.StatusNotFound, "charge not found")
			return
		}
		modulePerm := sourceModulePermission(charge.SourceModule)
		hasCollectOwn := outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectOwn)
		hasModulePerm := modulePerm != "" && outletmw.HasServicePermission(r, h.rbacSvc, modulePerm)
		if !hasCollectOwn || !hasModulePerm {
			respondError(w, http.StatusForbidden, "you do not have permission to collect this charge")
			return
		}
	}

	var in collectChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outletID := currentOutletID(r)
	req := billing.CollectChargeRequest{
		PaymentMethod: in.PaymentMethod, PhoneNumber: in.PhoneNumber,
		CollectedBy: currentUserID(r), OutletID: &outletID,
	}
	charge, err := h.svc.CollectCharge(r.Context(), tenantID, chargeID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, charge)
}

type settleAccountRequest struct {
	PaymentMethod string `json:"payment_method"`
	PhoneNumber   string `json:"phone_number,omitempty"`
	NextOfKinID   string `json:"next_of_kin_id,omitempty"`
}

// ListNextOfKin handles GET /{tenant}/hospital/patients/{patientID}/next-of-kin — the picker
// source for the Settle Account modal (offer existing contacts before falling back to "add new").
func (h *BillingHandler) ListNextOfKin(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListNextOfKin(r.Context(), tenantID, patientID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list next-of-kin")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type createNextOfKinRequest struct {
	Name         string `json:"name"`
	Phone        string `json:"phone,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	IDNumber     string `json:"id_number,omitempty"`
	IsPrimary    bool   `json:"is_primary,omitempty"`
}

// CreateNextOfKin handles POST /{tenant}/hospital/patients/{patientID}/next-of-kin
func (h *BillingHandler) CreateNextOfKin(w http.ResponseWriter, r *http.Request) {
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
	var in createNextOfKinRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kin, err := h.svc.CreateNextOfKin(r.Context(), tenantID, patientID, billing.NextOfKinInput{
		Name: in.Name, Phone: in.Phone, Relationship: in.Relationship, IDNumber: in.IDNumber, IsPrimary: in.IsPrimary,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, kin)
}

// WaiveCharge handles POST /{tenant}/hospital/billing/charges/{chargeID}/waive
func (h *BillingHandler) WaiveCharge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	chargeID, err := uuid.Parse(chi.URLParam(r, "chargeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid charge ID")
		return
	}
	charge, err := h.svc.WaiveCharge(r.Context(), tenantID, chargeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, charge)
}

// IssueRefund handles POST /{tenant}/hospital/billing/charges/{chargeID}/refund
func (h *BillingHandler) IssueRefund(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	chargeID, err := uuid.Parse(chi.URLParam(r, "chargeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid charge ID")
		return
	}
	var in reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	charge, err := h.svc.IssueRefund(r.Context(), tenantID, chargeID, in.Reason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, charge)
}

// DownloadReceipt handles GET /{tenant}/hospital/billing/charges/{chargeID}/receipt.pdf
func (h *BillingHandler) DownloadReceipt(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	chargeID, err := uuid.Parse(chi.URLParam(r, "chargeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid charge ID")
		return
	}
	pdfBytes, contentType, err := h.svc.DownloadReceiptPDF(r.Context(), tenantID, chargeID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="receipt.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// SettleAccount handles POST /{tenant}/hospital/billing/accounts/{accountID}/settle
func (h *BillingHandler) SettleAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid account ID")
		return
	}
	var in settleAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var nextOfKinID *uuid.UUID
	if in.NextOfKinID != "" {
		if id, perr := uuid.Parse(in.NextOfKinID); perr == nil {
			nextOfKinID = &id
		}
	}
	outletID := currentOutletID(r)
	acct, err := h.svc.SettleAccount(r.Context(), tenantID, accountID, billing.CollectChargeRequest{
		PaymentMethod: in.PaymentMethod, PhoneNumber: in.PhoneNumber,
		CollectedBy: currentUserID(r), OutletID: &outletID,
	}, nextOfKinID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, acct)
}

// ── BillableItemCatalog admin CRUD (Gap 3) ──────────────────────────────────────────────────

// ListCatalog handles GET /{tenant}/hospital/billing/catalog?include_inactive=1
func (h *BillingHandler) ListCatalog(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	includeInactive := r.URL.Query().Get("include_inactive") == "1" || r.URL.Query().Get("include_inactive") == "true"
	list, err := h.svc.ListBillableItemCatalog(r.Context(), tenantID, includeInactive)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list billable item catalog")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type catalogItemRequest struct {
	Department         string   `json:"department"`
	Code               string   `json:"code"`
	Name               string   `json:"name"`
	Price              *float64 `json:"price,omitempty"`
	AppliesTo          string   `json:"applies_to,omitempty"`
	RequiresPrepayment bool     `json:"requires_prepayment,omitempty"`
	CollectionMode     string   `json:"collection_mode,omitempty"`
}

// CreateCatalogItem handles POST /{tenant}/hospital/billing/catalog
func (h *BillingHandler) CreateCatalogItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in catalogItemRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.svc.CreateBillableItem(r.Context(), tenantID, billing.CatalogItemInput{
		Department: in.Department, Code: in.Code, Name: in.Name, Price: in.Price,
		AppliesTo: in.AppliesTo, RequiresPrepayment: in.RequiresPrepayment, CollectionMode: in.CollectionMode,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, item)
}

type catalogItemUpdateRequest struct {
	Name               *string  `json:"name,omitempty"`
	Price              *float64 `json:"price,omitempty"`
	ClearPrice         bool     `json:"clear_price,omitempty"`
	AppliesTo          *string  `json:"applies_to,omitempty"`
	RequiresPrepayment *bool    `json:"requires_prepayment,omitempty"`
	CollectionMode     *string  `json:"collection_mode,omitempty"`
	IsActive           *bool    `json:"is_active,omitempty"`
}

// UpdateCatalogItem handles PUT /{tenant}/hospital/billing/catalog/{itemID}
func (h *BillingHandler) UpdateCatalogItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid item ID")
		return
	}
	var in catalogItemUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.svc.UpdateBillableItem(r.Context(), tenantID, itemID, billing.CatalogItemUpdate{
		Name: in.Name, Price: in.Price, ClearPrice: in.ClearPrice, AppliesTo: in.AppliesTo,
		RequiresPrepayment: in.RequiresPrepayment, CollectionMode: in.CollectionMode, IsActive: in.IsActive,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, item)
}

// DeactivateCatalogItem handles POST /{tenant}/hospital/billing/catalog/{itemID}/deactivate
func (h *BillingHandler) DeactivateCatalogItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid item ID")
		return
	}
	item, err := h.svc.DeactivateBillableItem(r.Context(), tenantID, itemID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, item)
}

// ── Insurance (Sprint 5 remainder) ──────────────────────────────────────────────────────────

// ListInsuranceProviders handles GET /{tenant}/hospital/insurance/providers — shared picker
// source for Lab, Pharmacy and Billing's own insurance actions (all three need the same list).
func (h *BillingHandler) ListInsuranceProviders(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	providers, err := h.svc.ListInsuranceProviders(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": providers})
}

type checkEligibilityRequest struct {
	ProviderID string            `json:"provider_id"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// CheckEligibility handles POST /{tenant}/hospital/visits/{visitID}/insurance/check-eligibility
func (h *BillingHandler) CheckEligibility(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in checkEligibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	providerID, err := uuid.Parse(in.ProviderID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider_id")
		return
	}
	// visitID may be absent/invalid on a caller that doesn't have one yet — CheckEligibility
	// treats uuid.Nil as "skip beneficiary-number auto-populate", not an error.
	visitID, _ := uuid.Parse(chi.URLParam(r, "visitID"))
	result, err := h.svc.CheckEligibility(r.Context(), tenantID, providerID, visitID, in.Fields)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

type submitInsuranceClaimRequest struct {
	ProviderID string   `json:"provider_id"`
	CoverageID string   `json:"coverage_id,omitempty"`
	OutletID   string   `json:"outlet_id,omitempty"`
	ChargeIDs  []string `json:"charge_ids,omitempty"`
}

// SubmitInsuranceClaim handles POST /{tenant}/hospital/visits/{visitID}/insurance/submit-claim
//
// Aggregates every currently-pending charge on the visit's account (or, when charge_ids is
// supplied, exactly those charges) into ONE treasury-api insurance claim — the visit-scoped
// counterpart to lab/pharmacy's own order/prescription-scoped insurance-claim actions, for
// settling whatever else is outstanding on the visit (e.g. a registration/consultation fee) the
// same way.
func (h *BillingHandler) SubmitInsuranceClaim(w http.ResponseWriter, r *http.Request) {
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
	var in submitInsuranceClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	providerID, err := uuid.Parse(in.ProviderID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider_id")
		return
	}
	acct, charges, err := h.svc.GetAccountByVisit(r.Context(), tenantID, visitID)
	if err != nil {
		respondError(w, http.StatusNotFound, "account not found")
		return
	}
	req := billing.SubmitInsuranceClaimRequest{ProviderID: providerID}
	if in.CoverageID != "" {
		if id, perr := uuid.Parse(in.CoverageID); perr == nil {
			req.CoverageID = &id
		}
	}
	if in.OutletID != "" {
		if id, perr := uuid.Parse(in.OutletID); perr == nil {
			req.OutletID = &id
		}
	} else if outletID := currentOutletID(r); outletID != uuid.Nil {
		req.OutletID = &outletID
	}
	for _, cs := range in.ChargeIDs {
		if id, perr := uuid.Parse(cs); perr == nil {
			req.ChargeIDs = append(req.ChargeIDs, id)
		}
	}

	// Same per-charge source-module gate CollectCharge enforces on the cash path — without this,
	// a collect_own-only holder (e.g. a nurse) could claim charges from ANY department on the
	// visit via insurance, not just the ones they're allowed to collect themselves.
	if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectAny) {
		hasCollectOwn := outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectOwn)
		wanted := make(map[uuid.UUID]bool, len(req.ChargeIDs))
		for _, id := range req.ChargeIDs {
			wanted[id] = true
		}
		for _, c := range charges {
			if c.Status != billablecharge.StatusPending {
				continue
			}
			if len(wanted) > 0 && !wanted[c.ID] {
				continue
			}
			modulePerm := sourceModulePermission(c.SourceModule)
			hasModulePerm := modulePerm != "" && outletmw.HasServicePermission(r, h.rbacSvc, modulePerm)
			if !hasCollectOwn || !hasModulePerm {
				respondError(w, http.StatusForbidden, "you do not have permission to claim one or more of these charges")
				return
			}
		}
	}

	result, err := h.svc.SubmitInsuranceClaim(r.Context(), tenantID, acct.ID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// PollInsuranceClaim handles GET /{tenant}/hospital/insurance/claims/{claimID}/status
func (h *BillingHandler) PollInsuranceClaim(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	claimID, err := uuid.Parse(chi.URLParam(r, "claimID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid claim ID")
		return
	}
	claim, err := h.svc.PollInsuranceClaim(r.Context(), tenantID, claimID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, claim)
}

type overrideSettlementRequest struct {
	Reason string `json:"reason"`
}

// OverrideSettlement handles POST /{tenant}/hospital/billing/accounts/{accountID}/override-settlement
func (h *BillingHandler) OverrideSettlement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid account ID")
		return
	}
	var in overrideSettlementRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Reason == "" {
		respondError(w, http.StatusBadRequest, "a reason is required")
		return
	}
	acct, err := h.svc.OverrideSettlement(r.Context(), tenantID, accountID, currentUserID(r), in.Reason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, acct)
}
