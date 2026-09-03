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

type updateBookingRequest struct {
	TheatreRoom     *string `json:"theatre_room,omitempty"`
	SurgeryType     *string `json:"surgery_type,omitempty"`
	SurgeonID       *string `json:"surgeon_id,omitempty"`
	ClearSurgeonID  bool    `json:"clear_surgeon_id,omitempty"`
	ScheduledAt     *string `json:"scheduled_at,omitempty"`
	DurationMinutes *int    `json:"duration_minutes,omitempty"`
}

// UpdateBooking handles PUT /{tenant}/hospital/theatre-bookings/{bookingID} — reschedules a
// not-yet-started booking's core fields. See theatre.BookingUpdate's doc comment for why
// fee_amount is deliberately not accepted here.
func (h *TheatreHandler) UpdateBooking(w http.ResponseWriter, r *http.Request) {
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
	var in updateBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := theatre.BookingUpdate{
		TheatreRoom: in.TheatreRoom, SurgeryType: in.SurgeryType,
		DurationMinutes: in.DurationMinutes, ClearSurgeonID: in.ClearSurgeonID,
	}
	if in.ScheduledAt != nil {
		scheduledAt, perr := time.Parse(time.RFC3339, *in.ScheduledAt)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "invalid scheduled_at (expected RFC3339)")
			return
		}
		req.ScheduledAt = &scheduledAt
	}
	if in.SurgeonID != nil && *in.SurgeonID != "" {
		id, perr := uuid.Parse(*in.SurgeonID)
		if perr != nil {
			respondError(w, http.StatusBadRequest, "invalid surgeon_id")
			return
		}
		req.SurgeonID = &id
	}
	booking, err := h.svc.UpdateBooking(r.Context(), tenantID, bookingID, req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, booking)
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

// ── Surgical team assignment ────────────────────────────────────────────────────────────────

type assignStaffRequest struct {
	StaffUserID string `json:"staff_user_id"`
	Role        string `json:"role"`
}

// AssignStaff handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/staff
func (h *TheatreHandler) AssignStaff(w http.ResponseWriter, r *http.Request) {
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
	var in assignStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	staffUserID, err := uuid.Parse(in.StaffUserID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid staff_user_id")
		return
	}
	assignment, err := h.svc.AssignStaff(r.Context(), tenantID, bookingID, theatre.AssignStaffRequest{
		StaffUserID: staffUserID, Role: in.Role,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, assignment)
}

// ListStaffAssignments handles GET /{tenant}/hospital/theatre-bookings/{bookingID}/staff
func (h *TheatreHandler) ListStaffAssignments(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListStaffAssignments(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list staff assignments")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// RemoveStaffAssignment handles DELETE /{tenant}/hospital/theatre-bookings/{bookingID}/staff/{assignmentID}
func (h *TheatreHandler) RemoveStaffAssignment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	assignmentID, err := uuid.Parse(chi.URLParam(r, "assignmentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid assignment ID")
		return
	}
	if err := h.svc.RemoveStaffAssignment(r.Context(), tenantID, assignmentID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ── PACU ─────────────────────────────────────────────────────────────────────────────────────

type admitToPacuRequest struct {
	BayLabel string `json:"bay_label,omitempty"`
}

// AdmitToPacu handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/pacu
func (h *TheatreHandler) AdmitToPacu(w http.ResponseWriter, r *http.Request) {
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
	var in admitToPacuRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	stay, err := h.svc.AdmitToPacu(r.Context(), tenantID, bookingID, theatre.AdmitToPacuRequest{BayLabel: in.BayLabel})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, stay)
}

// ListPacuStays handles GET /{tenant}/hospital/theatre-bookings/{bookingID}/pacu
func (h *TheatreHandler) ListPacuStays(w http.ResponseWriter, r *http.Request) {
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
	list, err := h.svc.ListPacuStays(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list pacu stays")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

type dischargeFromPacuRequest struct {
	Disposition     string `json:"disposition"`
	MonitoringNotes string `json:"monitoring_notes,omitempty"`
}

// DischargeFromPacu handles POST /{tenant}/hospital/pacu-stays/{pacuStayID}/discharge
func (h *TheatreHandler) DischargeFromPacu(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	pacuStayID, err := uuid.Parse(chi.URLParam(r, "pacuStayID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid pacu stay ID")
		return
	}
	var in dischargeFromPacuRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stay, err := h.svc.DischargeFromPacu(r.Context(), tenantID, pacuStayID, theatre.DischargeFromPacuRequest{
		Disposition: in.Disposition, MonitoringNotes: in.MonitoringNotes,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, stay)
}

// ── Operative note ───────────────────────────────────────────────────────────────────────────

type operativeNoteRequest struct {
	SurgeonID            string   `json:"surgeon_id,omitempty"`
	ProcedurePerformed   string   `json:"procedure_performed"`
	Findings             string   `json:"findings,omitempty"`
	Complications        string   `json:"complications,omitempty"`
	EstimatedBloodLossML *float64 `json:"estimated_blood_loss_ml,omitempty"`
	ImplantsUsed         string   `json:"implants_used,omitempty"`
	SpecimensSent        bool     `json:"specimens_sent,omitempty"`
	SpecimensDescription string   `json:"specimens_description,omitempty"`
	PostOpDiagnosis      string   `json:"post_op_diagnosis,omitempty"`
}

// RecordOperativeNote handles POST /{tenant}/hospital/theatre-bookings/{bookingID}/operative-note
// (creates on first call, amends on subsequent calls — see theatre.Service.RecordOperativeNote).
func (h *TheatreHandler) RecordOperativeNote(w http.ResponseWriter, r *http.Request) {
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
	var in operativeNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	surgeonID := parseOptionalUUID(in.SurgeonID)
	note, err := h.svc.RecordOperativeNote(r.Context(), tenantID, bookingID, theatre.OperativeNoteRequest{
		SurgeonID: surgeonID, ProcedurePerformed: in.ProcedurePerformed, Findings: in.Findings,
		Complications: in.Complications, EstimatedBloodLossML: in.EstimatedBloodLossML,
		ImplantsUsed: in.ImplantsUsed, SpecimensSent: in.SpecimensSent,
		SpecimensDescription: in.SpecimensDescription, PostOpDiagnosis: in.PostOpDiagnosis,
		AuthoredBy: currentUserID(r),
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, note)
}

// GetOperativeNote handles GET /{tenant}/hospital/theatre-bookings/{bookingID}/operative-note
func (h *TheatreHandler) GetOperativeNote(w http.ResponseWriter, r *http.Request) {
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
	note, err := h.svc.GetOperativeNote(r.Context(), tenantID, bookingID)
	if err != nil {
		respondError(w, http.StatusNotFound, "no operative note recorded for this booking yet")
		return
	}
	respondJSON(w, http.StatusOK, note)
}
