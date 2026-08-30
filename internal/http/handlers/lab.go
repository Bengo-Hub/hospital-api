package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/lab"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// LabHandler implements Sprint 3's lab ordering/worklist/result-capture HTTP surface.
type LabHandler struct {
	svc     *lab.Service
	rbacSvc outletmw.PermissionChecker
}

// NewLabHandler creates a new LabHandler.
func NewLabHandler(svc *lab.Service, rbacSvc outletmw.PermissionChecker) *LabHandler {
	return &LabHandler{svc: svc, rbacSvc: rbacSvc}
}

type createLabOrderRequest struct {
	VisitID       string   `json:"visit_id"`
	ExaminationID string   `json:"examination_id,omitempty"`
	TestCodes     []string `json:"test_codes"`
	Notes         string   `json:"notes,omitempty"`
}

// CreateOrder handles POST /{tenant}/hospital/lab-orders
func (h *LabHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in createLabOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visitID, err := uuid.Parse(in.VisitID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit_id")
		return
	}
	req := lab.CreateOrderRequest{VisitID: visitID, OrderedBy: currentUserID(r), TestCodes: in.TestCodes, Notes: in.Notes}
	if in.ExaminationID != "" {
		if id, perr := uuid.Parse(in.ExaminationID); perr == nil {
			req.ExaminationID = &id
		}
	}
	order, err := h.svc.CreateOrder(r.Context(), tenantID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, order)
}

// ListWorklist handles GET /{tenant}/hospital/lab-orders?status=
func (h *LabHandler) ListWorklist(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListWorklist(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list lab orders")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetOrder handles GET /{tenant}/hospital/lab-orders/{orderID}
func (h *LabHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid order ID")
		return
	}
	order, lines, err := h.svc.GetOrder(r.Context(), tenantID, orderID)
	if err != nil {
		respondError(w, http.StatusNotFound, "lab order not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"order": order, "lines": lines})
}

// ActivateIfPaid handles POST /{tenant}/hospital/lab-orders/{orderID}/activate
func (h *LabHandler) ActivateIfPaid(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid order ID")
		return
	}
	order, err := h.svc.ActivateIfPaid(r.Context(), tenantID, orderID)
	if err != nil {
		respondError(w, http.StatusPaymentRequired, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, order)
}

type cancelLabOrderRequest struct {
	Reason string `json:"reason,omitempty"`
}

// CancelOrder handles POST /{tenant}/hospital/lab-orders/{orderID}/cancel
func (h *LabHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid order ID")
		return
	}
	var in cancelLabOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&in) // reason is optional — a malformed/empty body is fine
	order, err := h.svc.CancelOrder(r.Context(), tenantID, orderID, in.Reason)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, order)
}

type labInsuranceClaimRequest struct {
	ProviderID string `json:"provider_id"`
	CoverageID string `json:"coverage_id,omitempty"`
	OutletID   string `json:"outlet_id,omitempty"`
}

// SubmitInsuranceClaim handles POST /{tenant}/hospital/lab-orders/{orderID}/insurance-claim —
// the insurance-path alternative to the cash CollectCharge+ActivateIfPaid flow: submits a claim
// covering this order's test charges and, if treasury-api accepts it, activates the order in the
// same call. Uses the same 402 convention as ActivateIfPaid for any failure (order not found,
// nothing to claim, transport error) since this is that same payment-gate action's insurance leg.
func (h *LabHandler) SubmitInsuranceClaim(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid order ID")
		return
	}
	// Same per-charge-module gate CollectCharge enforces on the cash path (see billing.go's
	// sourceModulePermission) — the route-level RequireServicePermission only checks collect_own
	// OR collect_any, which would let a collect_own-only holder (e.g. a nurse) claim LAB charges
	// via insurance without actually holding lab permission. A lab order's charges are always
	// source_module="lab", so this is a single fixed check rather than iterating a charge list.
	if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectAny) {
		if !outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermBillingCollectOwn) ||
			!outletmw.HasServicePermission(r, h.rbacSvc, rbac.PermLabAdd) {
			respondError(w, http.StatusForbidden, "you do not have permission to claim this order's charges")
			return
		}
	}
	var in labInsuranceClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	providerID, err := uuid.Parse(in.ProviderID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider_id")
		return
	}
	req := lab.SubmitInsuranceClaimRequest{ProviderID: providerID}
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
	order, claim, err := h.svc.SubmitInsuranceClaim(r.Context(), tenantID, orderID, req)
	if err != nil {
		respondError(w, http.StatusPaymentRequired, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"order": order, "claim": claim})
}

type enterResultRequest struct {
	ResultValue    string `json:"result_value"`
	Unit           string `json:"unit,omitempty"`
	ReferenceRange string `json:"reference_range,omitempty"`
	Flag           string `json:"flag,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

// EnterResult handles POST /{tenant}/hospital/lab-orders/lines/{lineID}/result
func (h *LabHandler) EnterResult(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	lineID, err := uuid.Parse(chi.URLParam(r, "lineID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid line ID")
		return
	}
	var in enterResultRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	line, err := h.svc.EnterResult(r.Context(), tenantID, lineID, lab.EnterResultRequest{
		ResultValue: in.ResultValue, Unit: in.Unit, ReferenceRange: in.ReferenceRange,
		Flag: in.Flag, Notes: in.Notes, ResultedBy: currentUserID(r),
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, line)
}

// ListCatalog handles GET /{tenant}/hospital/lab-test-catalog
func (h *LabHandler) ListCatalog(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	list, err := h.svc.ListCatalog(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list lab test catalog")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// ── Tenant Lab Test Catalog admin CRUD (2026-08-30) ─────────────────────────────────────────

// ListTenantCatalogEntries handles GET /{tenant}/hospital/lab-test-catalog/entries?include_inactive=1
func (h *LabHandler) ListTenantCatalogEntries(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	includeInactive := r.URL.Query().Get("include_inactive") == "1" || r.URL.Query().Get("include_inactive") == "true"
	list, err := h.svc.ListTenantCatalogEntries(r.Context(), tenantID, includeInactive)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list lab test catalog entries")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type labTestEntryRequest struct {
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	SpecimenType    string  `json:"specimen_type,omitempty"`
	ReferenceRange  string  `json:"reference_range,omitempty"`
	Unit            string  `json:"unit,omitempty"`
	TurnaroundHours *int    `json:"turnaround_hours,omitempty"`
	Price           float64 `json:"price,omitempty"`
}

// CreateLabTestEntry handles POST /{tenant}/hospital/lab-test-catalog/entries
func (h *LabHandler) CreateLabTestEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in labTestEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry, err := h.svc.CreateLabTestEntry(r.Context(), tenantID, lab.LabTestEntryInput{
		Code: in.Code, Name: in.Name, SpecimenType: in.SpecimenType, ReferenceRange: in.ReferenceRange,
		Unit: in.Unit, TurnaroundHours: in.TurnaroundHours, Price: in.Price,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, entry)
}

type labTestEntryUpdateRequest struct {
	Name            *string  `json:"name,omitempty"`
	SpecimenType    *string  `json:"specimen_type,omitempty"`
	ReferenceRange  *string  `json:"reference_range,omitempty"`
	Unit            *string  `json:"unit,omitempty"`
	TurnaroundHours *int     `json:"turnaround_hours,omitempty"`
	Price           *float64 `json:"price,omitempty"`
	IsActive        *bool    `json:"is_active,omitempty"`
}

// UpdateLabTestEntry handles PUT /{tenant}/hospital/lab-test-catalog/entries/{entryID}
func (h *LabHandler) UpdateLabTestEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	entryID, err := uuid.Parse(chi.URLParam(r, "entryID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid entry ID")
		return
	}
	var in labTestEntryUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry, err := h.svc.UpdateLabTestEntry(r.Context(), tenantID, entryID, lab.LabTestEntryUpdate{
		Name: in.Name, SpecimenType: in.SpecimenType, ReferenceRange: in.ReferenceRange,
		Unit: in.Unit, TurnaroundHours: in.TurnaroundHours, Price: in.Price, IsActive: in.IsActive,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, entry)
}

// DeactivateLabTestEntry handles POST /{tenant}/hospital/lab-test-catalog/entries/{entryID}/deactivate
func (h *LabHandler) DeactivateLabTestEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	entryID, err := uuid.Parse(chi.URLParam(r, "entryID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid entry ID")
		return
	}
	entry, err := h.svc.DeactivateLabTestEntry(r.Context(), tenantID, entryID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, entry)
}
