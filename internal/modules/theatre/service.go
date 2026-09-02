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
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/theatrebooking"
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
