package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
