// Package theatre implements Sprint 7's surgery-scheduling half (Afya Hospital tier): theatre
// bookings with conflict detection, a pre-op checklist, and a prepayment gate mirroring lab's own
// requires_prepayment pattern exactly (see internal/modules/lab/service.go). ICU critical-care
// monitoring is the sibling internal/modules/icu package — the two share Sprint 7 and the
// theatre_module subscription feature, but are functionally distinct clinical workflows.
package theatre

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/billableitemcatalog"
	"github.com/bengobox/hospital-service/internal/ent/operativenote"
	"github.com/bengobox/hospital-service/internal/ent/pacustay"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/theatrebooking"
	"github.com/bengobox/hospital-service/internal/ent/theatrestaffassignment"
	"github.com/bengobox/hospital-service/internal/modules/billing"
)

// Service implements theatre-booking business logic.
type Service struct {
	client  *ent.Client
	billing *billing.Service
	log     *zap.Logger
}

// NewService creates a new theatre Service.
func NewService(client *ent.Client, billingSvc *billing.Service, log *zap.Logger) *Service {
	return &Service{client: client, billing: billingSvc, log: log.Named("theatre.service")}
}

// theatreCatalogItem resolves the tenant's THEATRE_FEE catalog row, if configured.
func (s *Service) theatreCatalogItem(ctx context.Context, tenantID uuid.UUID) *ent.BillableItemCatalog {
	item, err := s.client.BillableItemCatalog.Query().
		Where(
			billableitemcatalog.TenantID(tenantID),
			billableitemcatalog.DepartmentEQ(billableitemcatalog.DepartmentTheatre),
			billableitemcatalog.Code("THEATRE_FEE"),
			billableitemcatalog.IsActive(true),
		).
		Only(ctx)
	if err != nil {
		return nil
	}
	return item
}

// CreateBookingRequest is the input to CreateBooking.
type CreateBookingRequest struct {
	VisitID         uuid.UUID
	TheatreRoom     string
	SurgeryType     string
	SurgeonID       *uuid.UUID
	ScheduledAt     time.Time
	DurationMinutes int
	// FeeAmount is the quoted procedure fee — THEATRE_FEE has no fixed catalog price (procedure
	// fees vary too widely), so this is captured explicitly at booking time, same reasoning as
	// LabOrderLine's own catalogue-value snapshot. Nil/zero = no charge posted for this booking
	// (some tenants may price theatre entirely through other departments' charges).
	FeeAmount *float64
	CreatedBy uuid.UUID
}

// hasOverlap reports whether [from, from+duration) overlaps any existing non-cancelled booking in
// the same room. Fetches by room only (a single theatre room's booking volume is always small) and
// checks the overlap in Go rather than a DB range query — conflict detection is correctness-
// critical but does not need to scale past a handful of bookings per room.
func (s *Service) hasOverlap(ctx context.Context, tenantID uuid.UUID, room string, from time.Time, durationMinutes int, excludeID uuid.UUID) (bool, error) {
	existing, err := s.client.TheatreBooking.Query().
		Where(
			theatrebooking.TenantID(tenantID),
			theatrebooking.TheatreRoom(room),
			theatrebooking.StatusNEQ(theatrebooking.StatusCancelled),
		).
		All(ctx)
	if err != nil {
		return false, fmt.Errorf("theatre: query existing bookings: %w", err)
	}
	to := from.Add(time.Duration(durationMinutes) * time.Minute)
	for _, b := range existing {
		if b.ID == excludeID {
			continue
		}
		bTo := b.ScheduledAt.Add(time.Duration(b.DurationMinutes) * time.Minute)
		if from.Before(bTo) && b.ScheduledAt.Before(to) {
			return true, nil
		}
	}
	return false, nil
}

// staffBookingConflict reports whether staffUserID (already booked as this booking's surgeon, or
// via a TheatreStaffAssignment row in ANY blocking role) has an overlapping booking in a
// DIFFERENT room — the same staff member obviously can't be in two theatres at once. Same-room
// conflicts are already caught by hasOverlap; this is a pure additive extension covering staff as
// a secondary resource, per real OR-scheduling practice (mvp-gap-backlog-2026-09-02 Sprint 7.1).
func (s *Service) staffBookingConflict(ctx context.Context, tenantID, staffUserID uuid.UUID, room string, from time.Time, durationMinutes int, excludeID uuid.UUID) (bool, error) {
	if staffUserID == uuid.Nil {
		return false, nil
	}
	to := from.Add(time.Duration(durationMinutes) * time.Minute)

	asSurgeon, err := s.client.TheatreBooking.Query().
		Where(theatrebooking.TenantID(tenantID), theatrebooking.SurgeonID(staffUserID), theatrebooking.StatusNEQ(theatrebooking.StatusCancelled)).
		All(ctx)
	if err != nil {
		return false, fmt.Errorf("theatre: query surgeon bookings: %w", err)
	}
	for _, b := range asSurgeon {
		if b.ID == excludeID || b.TheatreRoom == room {
			continue // same room already checked by hasOverlap
		}
		bTo := b.ScheduledAt.Add(time.Duration(b.DurationMinutes) * time.Minute)
		if from.Before(bTo) && b.ScheduledAt.Before(to) {
			return true, nil
		}
	}

	assignments, err := s.client.TheatreStaffAssignment.Query().
		Where(theatrestaffassignment.TenantID(tenantID), theatrestaffassignment.StaffUserID(staffUserID)).
		WithTheatreBooking().
		All(ctx)
	if err != nil {
		return false, fmt.Errorf("theatre: query staff assignments: %w", err)
	}
	for _, a := range assignments {
		b := a.Edges.TheatreBooking
		if b == nil || b.ID == excludeID || b.TheatreRoom == room || b.Status == theatrebooking.StatusCancelled {
			continue
		}
		bTo := b.ScheduledAt.Add(time.Duration(b.DurationMinutes) * time.Minute)
		if from.Before(bTo) && b.ScheduledAt.Before(to) {
			return true, nil
		}
	}
	return false, nil
}

// CreateBooking schedules a surgery, rejecting a room/time-slot conflict, and posts the procedure
// fee as a BillableCharge if one is known — starting the booking "awaiting_payment" if the
// tenant's theatre billing policy requires prepayment (mirrors lab.Service.CreateOrder exactly).
func (s *Service) CreateBooking(ctx context.Context, tenantID uuid.UUID, req CreateBookingRequest) (*ent.TheatreBooking, error) {
	if req.VisitID == uuid.Nil || req.TheatreRoom == "" || req.SurgeryType == "" || req.ScheduledAt.IsZero() {
		return nil, fmt.Errorf("theatre: visit_id, theatre_room, surgery_type and scheduled_at are required")
	}
	duration := req.DurationMinutes
	if duration <= 0 {
		duration = 60
	}
	visit, err := s.client.PatientVisit.Query().
		Where(patientvisit.ID(req.VisitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: visit not found: %w", err)
	}

	conflict, err := s.hasOverlap(ctx, tenantID, req.TheatreRoom, req.ScheduledAt, duration, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, fmt.Errorf("theatre: %s is already booked for an overlapping time slot", req.TheatreRoom)
	}
	if req.SurgeonID != nil {
		staffConflict, serr := s.staffBookingConflict(ctx, tenantID, *req.SurgeonID, req.TheatreRoom, req.ScheduledAt, duration, uuid.Nil)
		if serr != nil {
			return nil, serr
		}
		if staffConflict {
			return nil, fmt.Errorf("theatre: the assigned surgeon is already booked in another theatre for an overlapping time slot")
		}
	}

	item := s.theatreCatalogItem(ctx, tenantID)
	var amount float64
	if req.FeeAmount != nil && *req.FeeAmount > 0 {
		amount = *req.FeeAmount
	} else if item != nil && item.Price != nil {
		amount = *item.Price
	}
	requiresPrepayment := item == nil || item.RequiresPrepayment // fail toward the safer gated default if unconfigured

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	status := theatrebooking.StatusScheduled
	if amount > 0 && requiresPrepayment {
		status = theatrebooking.StatusAwaitingPayment
	}

	create := tx.TheatreBooking.Create().
		SetTenantID(tenantID).
		SetOutletID(visit.OutletID).
		SetPatientVisitID(req.VisitID).
		SetPatientID(visit.PatientID).
		SetTheatreRoom(req.TheatreRoom).
		SetSurgeryType(req.SurgeryType).
		SetScheduledAt(req.ScheduledAt).
		SetDurationMinutes(duration).
		SetStatus(status)
	if req.SurgeonID != nil {
		create = create.SetSurgeonID(*req.SurgeonID)
	}
	if amount > 0 {
		create = create.SetFeeAmount(amount)
	}
	if req.CreatedBy != uuid.Nil {
		create = create.SetCreatedBy(req.CreatedBy)
	}
	booking, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: create booking: %w", err)
	}

	if amount > 0 {
		bookingID := booking.ID
		if _, cerr := s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
			PatientID:     visit.PatientID,
			VisitID:       req.VisitID,
			SourceModule:  "theatre",
			SourceRefID:   &bookingID,
			Description:   "Theatre: " + req.SurgeryType,
			Amount:        amount,
			CreatedByUser: req.CreatedBy,
		}); cerr != nil {
			err = cerr
			return nil, fmt.Errorf("theatre: post procedure fee: %w", cerr)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("theatre: commit booking: %w", err)
	}
	return booking, nil
}

// BookingUpdate is a partial reschedule of a not-yet-started booking's core fields. Deliberately
// excludes FeeAmount: CreateBooking already posts a BillableCharge for the procedure fee at
// creation time (before any payment gate), so allowing the fee to be edited afterward here would
// silently desync the booking's displayed fee from the charge a patient may have already been
// asked to pay — that correction belongs to billing's own charge-adjustment tools
// (WaiveCharge/UpdateCatalogItem), not a booking reschedule. Cancel-and-recreate is the documented
// escape hatch for "the fee was wrong," same as before this endpoint existed.
type BookingUpdate struct {
	TheatreRoom     *string
	SurgeryType     *string
	ScheduledAt     *time.Time
	DurationMinutes *int
	SurgeonID       *uuid.UUID
	ClearSurgeonID  bool
}

// UpdateBooking reschedules a booking's core fields (room/type/time/duration/surgeon) — only
// while it's still awaiting_payment or scheduled (not yet started). Re-runs the exact same
// room/staff conflict checks CreateBooking does, excluding this booking's own ID, whenever the
// room/time/duration/surgeon actually changes.
func (s *Service) UpdateBooking(ctx context.Context, tenantID, bookingID uuid.UUID, in BookingUpdate) (*ent.TheatreBooking, error) {
	existing, err := s.client.TheatreBooking.Query().
		Where(theatrebooking.ID(bookingID), theatrebooking.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if existing.Status != theatrebooking.StatusAwaitingPayment && existing.Status != theatrebooking.StatusScheduled {
		return nil, fmt.Errorf("theatre: booking can only be rescheduled while awaiting payment or scheduled (current status: %s)", existing.Status)
	}

	room := existing.TheatreRoom
	if in.TheatreRoom != nil {
		if *in.TheatreRoom == "" {
			return nil, fmt.Errorf("theatre: theatre_room cannot be empty")
		}
		room = *in.TheatreRoom
	}
	scheduledAt := existing.ScheduledAt
	if in.ScheduledAt != nil {
		scheduledAt = *in.ScheduledAt
	}
	duration := existing.DurationMinutes
	if in.DurationMinutes != nil && *in.DurationMinutes > 0 {
		duration = *in.DurationMinutes
	}
	surgeonID := existing.SurgeonID
	if in.ClearSurgeonID {
		surgeonID = nil
	} else if in.SurgeonID != nil {
		surgeonID = in.SurgeonID
	}

	roomOrTimeChanged := room != existing.TheatreRoom || !scheduledAt.Equal(existing.ScheduledAt) || duration != existing.DurationMinutes
	surgeonChanged := (surgeonID == nil) != (existing.SurgeonID == nil) ||
		(surgeonID != nil && existing.SurgeonID != nil && *surgeonID != *existing.SurgeonID)
	if roomOrTimeChanged {
		conflict, cerr := s.hasOverlap(ctx, tenantID, room, scheduledAt, duration, bookingID)
		if cerr != nil {
			return nil, cerr
		}
		if conflict {
			return nil, fmt.Errorf("theatre: %s is already booked for an overlapping time slot", room)
		}
	}
	if (roomOrTimeChanged || surgeonChanged) && surgeonID != nil {
		staffConflict, serr := s.staffBookingConflict(ctx, tenantID, *surgeonID, room, scheduledAt, duration, bookingID)
		if serr != nil {
			return nil, serr
		}
		if staffConflict {
			return nil, fmt.Errorf("theatre: the assigned surgeon is already booked in another theatre for an overlapping time slot")
		}
	}

	upd := s.client.TheatreBooking.UpdateOneID(existing.ID).
		SetTheatreRoom(room).
		SetSurgeryType(existing.SurgeryType).
		SetScheduledAt(scheduledAt).
		SetDurationMinutes(duration)
	if in.SurgeryType != nil && *in.SurgeryType != "" {
		upd = upd.SetSurgeryType(*in.SurgeryType)
	}
	if in.ClearSurgeonID {
		upd = upd.ClearSurgeonID()
	} else if surgeonID != nil {
		upd = upd.SetSurgeonID(*surgeonID)
	}
	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: reschedule booking: %w", err)
	}
	return updated, nil
}

// GetBooking fetches a booking by ID, tenant-scoped.
func (s *Service) GetBooking(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.TheatreBooking, error) {
	return s.client.TheatreBooking.Query().
		Where(theatrebooking.ID(bookingID), theatrebooking.TenantID(tenantID)).
		Only(ctx)
}

// ListSchedule returns bookings, optionally scoped to one calendar date (server-local time), for
// the theatre-room availability/schedule view.
func (s *Service) ListSchedule(ctx context.Context, tenantID uuid.UUID, date *time.Time) ([]*ent.TheatreBooking, error) {
	q := s.client.TheatreBooking.Query().Where(theatrebooking.TenantID(tenantID))
	if date != nil {
		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
		end := start.Add(24 * time.Hour)
		q = q.Where(theatrebooking.ScheduledAtGTE(start), theatrebooking.ScheduledAtLT(end))
	}
	return q.Order(ent.Asc(theatrebooking.FieldScheduledAt)).Limit(200).All(ctx)
}

// ── Surgical team assignment ────────────────────────────────────────────────────────────────

// AssignStaffRequest is the input to AssignStaff.
type AssignStaffRequest struct {
	StaffUserID uuid.UUID
	Role        string // surgeon|assistant_surgeon|anaesthetist|scrub_nurse|circulating_nurse|other
}

// AssignStaff adds one staff member to a booking's surgical team, checking the same staff-conflict
// rule CreateBooking applies to the primary surgeon — a scrub nurse or anaesthetist double-booked
// across two concurrent theatres is exactly as real a conflict as a double-booked surgeon.
func (s *Service) AssignStaff(ctx context.Context, tenantID, bookingID uuid.UUID, req AssignStaffRequest) (*ent.TheatreStaffAssignment, error) {
	if req.StaffUserID == uuid.Nil || req.Role == "" {
		return nil, fmt.Errorf("theatre: staff_user_id and role are required")
	}
	booking, err := s.GetBooking(ctx, tenantID, bookingID)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	conflict, err := s.staffBookingConflict(ctx, tenantID, req.StaffUserID, booking.TheatreRoom, booking.ScheduledAt, booking.DurationMinutes, bookingID)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, fmt.Errorf("theatre: this staff member is already booked in another theatre for an overlapping time slot")
	}
	return s.client.TheatreStaffAssignment.Create().
		SetTenantID(tenantID).
		SetTheatreBookingID(bookingID).
		SetStaffUserID(req.StaffUserID).
		SetRole(theatrestaffassignment.Role(req.Role)).
		Save(ctx)
}

// ListStaffAssignments returns a booking's surgical team.
func (s *Service) ListStaffAssignments(ctx context.Context, tenantID, bookingID uuid.UUID) ([]*ent.TheatreStaffAssignment, error) {
	return s.client.TheatreStaffAssignment.Query().
		Where(theatrestaffassignment.TenantID(tenantID), theatrestaffassignment.TheatreBookingID(bookingID)).
		Order(ent.Asc(theatrestaffassignment.FieldAssignedAt)).
		All(ctx)
}

// RemoveStaffAssignment removes one team member from a booking.
func (s *Service) RemoveStaffAssignment(ctx context.Context, tenantID, assignmentID uuid.UUID) error {
	n, err := s.client.TheatreStaffAssignment.Delete().
		Where(theatrestaffassignment.ID(assignmentID), theatrestaffassignment.TenantID(tenantID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("theatre: remove staff assignment: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("theatre: staff assignment not found")
	}
	return nil
}

// ── PACU (post-anaesthesia care unit) ───────────────────────────────────────────────────────

// AdmitToPacuRequest is the input to AdmitToPacu.
type AdmitToPacuRequest struct {
	BayLabel string
}

// AdmitToPacu opens a PACU stay for a completed booking — a short, lower-acuity waypoint between
// the operating room and a ward/home/ICU, deliberately not a reuse of ICUEpisode (see PacuStay's
// own doc comment).
func (s *Service) AdmitToPacu(ctx context.Context, tenantID, bookingID uuid.UUID, req AdmitToPacuRequest) (*ent.PacuStay, error) {
	if _, err := s.GetBooking(ctx, tenantID, bookingID); err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	return s.client.PacuStay.Create().
		SetTenantID(tenantID).
		SetTheatreBookingID(bookingID).
		SetBayLabel(req.BayLabel).
		Save(ctx)
}

// DischargeFromPacuRequest is the input to DischargeFromPacu.
type DischargeFromPacuRequest struct {
	Disposition     string // to_ward|to_icu|home|deceased
	MonitoringNotes string
}

// DischargeFromPacu closes a PACU stay. A to_icu disposition is a workflow signal for the caller
// to separately start a real ICUEpisode — PACU itself never auto-creates one, since most PACU
// patients are not critically ill and shouldn't land on the ICU board by default.
func (s *Service) DischargeFromPacu(ctx context.Context, tenantID, pacuStayID uuid.UUID, req DischargeFromPacuRequest) (*ent.PacuStay, error) {
	stay, err := s.client.PacuStay.Query().Where(pacustay.ID(pacuStayID), pacustay.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: pacu stay not found: %w", err)
	}
	if stay.DischargedAt != nil {
		return nil, fmt.Errorf("theatre: pacu stay already discharged")
	}
	update := s.client.PacuStay.UpdateOneID(pacuStayID).
		SetDischargedAt(time.Now()).
		SetMonitoringNotes(req.MonitoringNotes)
	if v := pacustay.DischargeDisposition(req.Disposition); v != "" {
		update = update.SetDischargeDisposition(v)
	}
	return update.Save(ctx)
}

// ListPacuStays returns every PACU stay recorded for a booking (normally at most one, but not
// enforced — a re-admission to PACU after a complication is a real, if rare, case).
func (s *Service) ListPacuStays(ctx context.Context, tenantID, bookingID uuid.UUID) ([]*ent.PacuStay, error) {
	return s.client.PacuStay.Query().
		Where(pacustay.TenantID(tenantID), pacustay.TheatreBookingID(bookingID)).
		Order(ent.Desc(pacustay.FieldAdmittedAt)).
		All(ctx)
}

// ── Operative note ───────────────────────────────────────────────────────────────────────────

// OperativeNoteRequest is the input to RecordOperativeNote.
type OperativeNoteRequest struct {
	SurgeonID            *uuid.UUID
	ProcedurePerformed   string
	Findings             string
	Complications        string
	EstimatedBloodLossML *float64
	ImplantsUsed         string
	SpecimensSent        bool
	SpecimensDescription string
	PostOpDiagnosis      string
	AuthoredBy           uuid.UUID
}

// RecordOperativeNote creates the structured post-op report for a booking — one-to-one, so a
// second call for the same booking updates the existing note rather than erroring, letting a
// surgeon amend it after initial authoring.
func (s *Service) RecordOperativeNote(ctx context.Context, tenantID, bookingID uuid.UUID, req OperativeNoteRequest) (*ent.OperativeNote, error) {
	if req.ProcedurePerformed == "" {
		return nil, fmt.Errorf("theatre: procedure_performed is required")
	}
	if _, err := s.GetBooking(ctx, tenantID, bookingID); err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	existing, err := s.client.OperativeNote.Query().
		Where(operativenote.TenantID(tenantID), operativenote.TheatreBookingID(bookingID)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("theatre: check existing operative note: %w", err)
	}

	if existing != nil {
		update := s.client.OperativeNote.UpdateOneID(existing.ID).
			SetProcedurePerformed(req.ProcedurePerformed).
			SetFindings(req.Findings).
			SetComplications(req.Complications).
			SetImplantsUsed(req.ImplantsUsed).
			SetSpecimensSent(req.SpecimensSent).
			SetSpecimensDescription(req.SpecimensDescription).
			SetPostOpDiagnosis(req.PostOpDiagnosis)
		if req.SurgeonID != nil {
			update = update.SetSurgeonID(*req.SurgeonID)
		}
		if req.EstimatedBloodLossML != nil {
			update = update.SetEstimatedBloodLossMl(*req.EstimatedBloodLossML)
		}
		return update.Save(ctx)
	}

	create := s.client.OperativeNote.Create().
		SetTenantID(tenantID).
		SetTheatreBookingID(bookingID).
		SetProcedurePerformed(req.ProcedurePerformed).
		SetFindings(req.Findings).
		SetComplications(req.Complications).
		SetImplantsUsed(req.ImplantsUsed).
		SetSpecimensSent(req.SpecimensSent).
		SetSpecimensDescription(req.SpecimensDescription).
		SetPostOpDiagnosis(req.PostOpDiagnosis)
	if req.SurgeonID != nil {
		create = create.SetSurgeonID(*req.SurgeonID)
	}
	if req.EstimatedBloodLossML != nil {
		create = create.SetEstimatedBloodLossMl(*req.EstimatedBloodLossML)
	}
	if req.AuthoredBy != uuid.Nil {
		create = create.SetAuthoredBy(req.AuthoredBy)
	}
	return create.Save(ctx)
}

// GetOperativeNote fetches a booking's operative note, if one has been authored yet.
func (s *Service) GetOperativeNote(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.OperativeNote, error) {
	return s.client.OperativeNote.Query().
		Where(operativenote.TenantID(tenantID), operativenote.TheatreBookingID(bookingID)).
		Only(ctx)
}

func (s *Service) chargeForBooking(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.BillableCharge, error) {
	return s.client.BillableCharge.Query().
		Where(billablecharge.TenantID(tenantID), billablecharge.SourceReferenceID(bookingID), billablecharge.SourceModule("theatre")).
		Only(ctx)
}

// ActivateIfPaid flips an awaiting_payment booking to "scheduled" once its procedure-fee charge is
// paid or exempted — mirrors lab.Service.ActivateIfPaid exactly.
func (s *Service) ActivateIfPaid(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.TheatreBooking, error) {
	booking, err := s.GetBooking(ctx, tenantID, bookingID)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if booking.Status != theatrebooking.StatusAwaitingPayment {
		return booking, nil
	}
	charge, cerr := s.chargeForBooking(ctx, tenantID, bookingID)
	if cerr != nil {
		return nil, fmt.Errorf("theatre: no charge found for booking")
	}
	if charge.Status != billablecharge.StatusPaid && charge.Status != billablecharge.StatusExempted {
		return nil, fmt.Errorf("theatre: the procedure fee must be paid for (cash or insurance) before this booking is confirmed")
	}
	return s.client.TheatreBooking.UpdateOneID(bookingID).SetStatus(theatrebooking.StatusScheduled).Save(ctx)
}

// UpdateChecklist replaces the pre-op checklist map — the caller sends the full current state
// (same "replace, not merge" contract as billing's role-permission update), simplest correct
// contract for a small, UI-driven checklist.
func (s *Service) UpdateChecklist(ctx context.Context, tenantID, bookingID uuid.UUID, checklist map[string]bool) (*ent.TheatreBooking, error) {
	if _, err := s.GetBooking(ctx, tenantID, bookingID); err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	return s.client.TheatreBooking.UpdateOneID(bookingID).SetChecklist(checklist).Save(ctx)
}

// SetEquipment replaces the list of inventory-api Asset IDs (e.g. an anaesthesia machine) linked
// to this booking — reference only, see docs/architecture.md's "Biomedical Equipment / Asset
// Integration" section.
func (s *Service) SetEquipment(ctx context.Context, tenantID, bookingID uuid.UUID, assetIDs []uuid.UUID) (*ent.TheatreBooking, error) {
	if _, err := s.GetBooking(ctx, tenantID, bookingID); err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if assetIDs == nil {
		assetIDs = []uuid.UUID{}
	}
	updated, err := s.client.TheatreBooking.UpdateOneID(bookingID).SetEquipmentAssetIds(assetIDs).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("theatre: set equipment: %w", err)
	}
	return updated, nil
}

// StartSurgery transitions a scheduled booking to in_progress.
func (s *Service) StartSurgery(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.TheatreBooking, error) {
	booking, err := s.GetBooking(ctx, tenantID, bookingID)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if booking.Status != theatrebooking.StatusScheduled {
		return nil, fmt.Errorf("theatre: only a scheduled booking can start (status=%s)", booking.Status)
	}
	now := time.Now()
	return s.client.TheatreBooking.UpdateOneID(bookingID).SetStatus(theatrebooking.StatusInProgress).SetStartedAt(now).Save(ctx)
}

// CompleteSurgery transitions an in_progress booking to completed.
func (s *Service) CompleteSurgery(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.TheatreBooking, error) {
	booking, err := s.GetBooking(ctx, tenantID, bookingID)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if booking.Status != theatrebooking.StatusInProgress {
		return nil, fmt.Errorf("theatre: only an in-progress booking can complete (status=%s)", booking.Status)
	}
	now := time.Now()
	return s.client.TheatreBooking.UpdateOneID(bookingID).SetStatus(theatrebooking.StatusCompleted).SetCompletedAt(now).Save(ctx)
}

// CancelBooking cancels a not-yet-completed booking, waiving any pending procedure-fee charge —
// mirrors lab.Service.CancelOrder's waive-pending-charge pattern.
func (s *Service) CancelBooking(ctx context.Context, tenantID, bookingID uuid.UUID) (*ent.TheatreBooking, error) {
	booking, err := s.GetBooking(ctx, tenantID, bookingID)
	if err != nil {
		return nil, fmt.Errorf("theatre: booking not found: %w", err)
	}
	if booking.Status == theatrebooking.StatusCancelled {
		return booking, nil
	}
	if booking.Status == theatrebooking.StatusCompleted {
		return nil, fmt.Errorf("theatre: a completed booking cannot be cancelled")
	}
	if charge, cerr := s.chargeForBooking(ctx, tenantID, bookingID); cerr == nil {
		if charge.Status == billablecharge.StatusPending || charge.Status == billablecharge.StatusInvoiced {
			if _, werr := s.billing.WaiveCharge(ctx, tenantID, charge.ID); werr != nil {
				return nil, fmt.Errorf("theatre: waive charge for cancelled booking: %w", werr)
			}
		}
	}
	return s.client.TheatreBooking.UpdateOneID(bookingID).SetStatus(theatrebooking.StatusCancelled).Save(ctx)
}
