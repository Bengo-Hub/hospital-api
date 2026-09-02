package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/modules/theatre"
)

// TheatreHandler implements Sprint 7's surgery-scheduling HTTP surface.
type TheatreHandler struct {
	svc *theatre.Service
}

// NewTheatreHandler creates a new TheatreHandler.
func NewTheatreHandler(svc *theatre.Service) *TheatreHandler {
	return &TheatreHandler{svc: svc}
}

type createBookingRequest struct {
	VisitID         string   `json:"visit_id"`
	TheatreRoom     string   `json:"theatre_room"`
	SurgeryType     string   `json:"surgery_type"`
	SurgeonID       string   `json:"surgeon_id,omitempty"`
	ScheduledAt     string   `json:"scheduled_at"`
	DurationMinutes int      `json:"duration_minutes,omitempty"`
	FeeAmount       *float64 `json:"fee_amount,omitempty"`
}

// CreateBooking handles POST /{tenant}/hospital/theatre-bookings
func (h *TheatreHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var in createBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visitID, err := uuid.Parse(in.VisitID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid visit_id")
		return
	}
	scheduledAt, err := time.Parse(time.RFC3339, in.ScheduledAt)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid scheduled_at (expected RFC3339)")
		return
	}
	req := theatre.CreateBookingRequest{
		VisitID: visitID, TheatreRoom: in.TheatreRoom, SurgeryType: in.SurgeryType,
		ScheduledAt: scheduledAt, DurationMinutes: in.DurationMinutes, FeeAmount: in.FeeAmount,
		CreatedBy: currentUserID(r),
	}
	if in.SurgeonID != "" {
		if id, perr := uuid.Parse(in.SurgeonID); perr == nil {
			req.SurgeonID = &id
		}
	}
	booking, err := h.svc.CreateBooking(r.Context(), tenantID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, booking)
}

// ListSchedule handles GET /{tenant}/hospital/theatre-bookings?date=2026-09-02
func (h *TheatreHandler) ListSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	var datePtr *time.Time
	if dateStr := r.URL.Query().Get("date"); dateStr != "" {
		if d, perr := time.Parse("2006-01-02", dateStr); perr == nil {
			datePtr = &d
		}
	}
	list, err := h.svc.ListSchedule(r.Context(), tenantID, datePtr)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list theatre bookings")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// GetBooking handles GET /{tenant}/hospital/theatre-bookings/{bookingID}
func (h *TheatreHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	booking, err := h.svc.GetBooking(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusNotFound, "booking not found")
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

// ActivateIfPaid handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/activate
func (h *TheatreHandler) ActivateIfPaid(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	booking, err := h.svc.ActivateIfPaid(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusPaymentRequired, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

// UpdateChecklist handles PUT /{tenant}/hospital/theatre-bookings/{bookingID}/checklist
func (h *TheatreHandler) UpdateChecklist(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	var checklist map[string]bool
	if err := json.NewDecoder(r.Body).Decode(&checklist); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	booking, err := h.svc.UpdateChecklist(r.Context(), tenantID, bookingID, checklist)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

type setBookingEquipmentRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

// SetEquipment handles PUT /{tenant}/hospital/theatre-bookings/{bookingID}/equipment
func (h *TheatreHandler) SetEquipment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	var in setBookingEquipmentRequest
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
	booking, err := h.svc.SetEquipment(r.Context(), tenantID, bookingID, assetIDs)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

// StartSurgery handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/start
func (h *TheatreHandler) StartSurgery(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	booking, err := h.svc.StartSurgery(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

// CompleteSurgery handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/complete
func (h *TheatreHandler) CompleteSurgery(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	booking, err := h.svc.CompleteSurgery(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}

// CancelBooking handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/cancel
func (h *TheatreHandler) CancelBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "bookingID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid booking ID")
		return
	}
	booking, err := h.svc.CancelBooking(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
}
