package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/lab"
)

// LabHandler implements Sprint 3's lab ordering/worklist/result-capture HTTP surface.
type LabHandler struct {
	svc *lab.Service
}

// NewLabHandler creates a new LabHandler.
func NewLabHandler(svc *lab.Service) *LabHandler {
	return &LabHandler{svc: svc}
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
