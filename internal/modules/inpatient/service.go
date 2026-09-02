// Package inpatient implements Sprint 6: Ward/Bed/Admission — the admission-to-discharge
// lifecycle. Every department's charges during a stay accrue onto the SAME admission-scoped
// PatientAccount (see billing.Service.activeAdmissionAccount) rather than each posting a separate
// mini-invoice, per docs/architecture.md "Distributed Billing & Patient Accounts". The pricing
// model's Afya Clinic "Inpatient add-on" and Afya Facility/Hospital's core inpatient management
// are the SAME code path, gated by subscriptions-api's inpatient_module feature — see
// docs/sprints/sprint-6-inpatient.md.
package inpatient

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/admission"
	"github.com/bengobox/hospital-service/internal/ent/bed"
	"github.com/bengobox/hospital-service/internal/ent/patient"
	"github.com/bengobox/hospital-service/internal/ent/patienttransfer"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/ward"
	"github.com/bengobox/hospital-service/internal/events"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/sequence"
)

// Service implements ward/bed/admission business logic.
type Service struct {
	client  *ent.Client
	billing *billing.Service
	log     *zap.Logger
}

// NewService creates a new inpatient Service.
func NewService(client *ent.Client, billingSvc *billing.Service, log *zap.Logger) *Service {
	return &Service{client: client, billing: billingSvc, log: log.Named("inpatient.service")}
}

// ErrOutstandingBalance is returned by Discharge when the admission's account still has a
// positive balance and no settlement override was supplied. The handler surfaces this as 409 with
// Account attached so the UI can offer Record Payment / Apply Insurance / Write-Off / next-of-kin-
// settle actions — Sprint 5's existing collect/settle/override-settlement endpoints, not a new
// mechanism invented here.
type ErrOutstandingBalance struct {
	Account *ent.PatientAccount
}

func (e *ErrOutstandingBalance) Error() string {
	return fmt.Sprintf("inpatient: outstanding balance %.2f must be settled before discharge", e.Account.Balance)
}

// ── Ward / Bed ───────────────────────────────────────────────────────────────────────────────

// CreateWard adds a new ward for an outlet.
func (s *Service) CreateWard(ctx context.Context, tenantID, outletID uuid.UUID, name string, capacity int) (*ent.Ward, error) {
	if name == "" {
		return nil, fmt.Errorf("inpatient: ward name is required")
	}
	w, err := s.client.Ward.Create().
		SetTenantID(tenantID).
		SetOutletID(outletID).
		SetName(name).
		SetCapacity(capacity).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: create ward: %w", err)
	}
	return w, nil
}

// SetWardBillableItemCode names the BillableItemCatalog code (department=inpatient) that prices a
// day in this ward — e.g. BED_DAY_ICU vs BED_DAY_GENERAL. Leaving it unset falls back to the
// tenant's default WARD_DAY_RATE code at discharge-time billing.
func (s *Service) SetWardBillableItemCode(ctx context.Context, tenantID, wardID uuid.UUID, code string) (*ent.Ward, error) {
	exists, err := s.client.Ward.Query().Where(ward.ID(wardID), ward.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: check ward: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("inpatient: ward not found")
	}
	updated, err := s.client.Ward.UpdateOneID(wardID).SetBillableItemCode(code).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: set ward billable item code: %w", err)
	}
	return updated, nil
}

// ListWards lists active wards, optionally scoped to one outlet.
func (s *Service) ListWards(ctx context.Context, tenantID uuid.UUID, outletID *uuid.UUID) ([]*ent.Ward, error) {
	q := s.client.Ward.Query().Where(ward.TenantID(tenantID), ward.IsActive(true))
	if outletID != nil {
		q = q.Where(ward.OutletID(*outletID))
	}
	return q.Order(ent.Asc(ward.FieldName)).All(ctx)
}

// CreateBed adds a new bed to a ward.
func (s *Service) CreateBed(ctx context.Context, tenantID, wardID uuid.UUID, bedNumber string) (*ent.Bed, error) {
	if bedNumber == "" {
		return nil, fmt.Errorf("inpatient: bed_number is required")
	}
	exists, err := s.client.Ward.Query().Where(ward.ID(wardID), ward.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: check ward: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("inpatient: ward not found")
	}
	b, err := s.client.Bed.Create().
		SetTenantID(tenantID).
		SetWardID(wardID).
		SetBedNumber(bedNumber).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: create bed: %w", err)
	}
	return b, nil
}

// ListBeds lists every bed in a ward.
func (s *Service) ListBeds(ctx context.Context, tenantID, wardID uuid.UUID) ([]*ent.Bed, error) {
	return s.client.Bed.Query().
		Where(bed.TenantID(tenantID), bed.WardID(wardID)).
		Order(ent.Asc(bed.FieldBedNumber)).
		All(ctx)
}

// SetBedStatus updates a bed's housekeeping/turnover status (available/cleaning/out_of_service —
// a lightweight status field, not a full housekeeping module). "occupied" may only be set by
// Admit/Transfer, never directly through this endpoint.
func (s *Service) SetBedStatus(ctx context.Context, tenantID, bedID uuid.UUID, status string) (*ent.Bed, error) {
	b, err := s.client.Bed.Query().Where(bed.ID(bedID), bed.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: bed not found: %w", err)
	}
	switch status {
	case string(bed.StatusAvailable), string(bed.StatusCleaning), string(bed.StatusOutOfService):
	case string(bed.StatusOccupied):
		return nil, fmt.Errorf("inpatient: bed occupancy is set by admitting or transferring a patient, not this endpoint")
	default:
		return nil, fmt.Errorf("inpatient: invalid bed status %q", status)
	}
	if b.Status == bed.StatusOccupied {
		return nil, fmt.Errorf("inpatient: cannot change an occupied bed's status directly; discharge or transfer the patient first")
	}
	updated, err := s.client.Bed.UpdateOneID(bedID).SetStatus(bed.Status(status)).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: update bed status: %w", err)
	}
	return updated, nil
}

// BedOccupancy is one row of a ward-occupancy view — a bed, and (if occupied) the active
// admission and a denormalized patient name/MRN so hospital-ui's occupancy board needs no
// client-side join.
type BedOccupancy struct {
	Bed         *ent.Bed       `json:"bed"`
	Admission   *ent.Admission `json:"admission,omitempty"`
	PatientName string         `json:"patient_name,omitempty"`
	PatientMRN  string         `json:"patient_mrn,omitempty"`
}

// GetWardOccupancy returns a ward plus every bed's live occupancy state.
func (s *Service) GetWardOccupancy(ctx context.Context, tenantID, wardID uuid.UUID) (*ent.Ward, []BedOccupancy, error) {
	w, err := s.client.Ward.Query().Where(ward.ID(wardID), ward.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: ward not found: %w", err)
	}
	beds, err := s.client.Bed.Query().
		Where(bed.TenantID(tenantID), bed.WardID(wardID)).
		Order(ent.Asc(bed.FieldBedNumber)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: list beds: %w", err)
	}

	bedIDs := make([]uuid.UUID, len(beds))
	for i, b := range beds {
		bedIDs[i] = b.ID
	}
	admissions, err := s.client.Admission.Query().
		Where(admission.TenantID(tenantID), admission.BedIDIn(bedIDs...), admission.StatusEQ(admission.StatusActive)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: list active admissions: %w", err)
	}
	admByBed := make(map[uuid.UUID]*ent.Admission, len(admissions))
	patientIDs := make([]uuid.UUID, 0, len(admissions))
	for _, a := range admissions {
		admByBed[a.BedID] = a
		patientIDs = append(patientIDs, a.PatientID)
	}

	patientByID := make(map[uuid.UUID]*ent.Patient, len(patientIDs))
	if len(patientIDs) > 0 {
		patients, perr := s.client.Patient.Query().Where(patient.TenantID(tenantID), patient.IDIn(patientIDs...)).All(ctx)
		if perr != nil {
			return nil, nil, fmt.Errorf("inpatient: list patients: %w", perr)
		}
		for _, p := range patients {
			patientByID[p.ID] = p
		}
	}

	result := make([]BedOccupancy, len(beds))
	for i, b := range beds {
		bo := BedOccupancy{Bed: b}
		if a, ok := admByBed[b.ID]; ok {
			bo.Admission = a
			if p, ok := patientByID[a.PatientID]; ok {
				bo.PatientName = p.FullName
				bo.PatientMRN = p.Mrn
			}
		}
		result[i] = bo
	}
	return w, result, nil
}

// ── Admission ────────────────────────────────────────────────────────────────────────────────

// AdmitRequest is the input to Admit.
type AdmitRequest struct {
	VisitID    uuid.UUID
	BedID      uuid.UUID
	AdmittedBy uuid.UUID
}

// Admit opens a new inpatient admission: validates the bed is available and the visit has no
// existing active admission, occupies the bed, flips the visit to IPD/admitted, and opens the
// admission's own PatientAccount so a Get Account view works immediately even before any charge
// posts.
func (s *Service) Admit(ctx context.Context, tenantID uuid.UUID, req AdmitRequest) (*ent.Admission, error) {
	visit, err := s.client.PatientVisit.Query().
		Where(patientvisit.ID(req.VisitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: visit not found: %w", err)
	}
	alreadyAdmitted, err := s.client.Admission.Query().
		Where(admission.TenantID(tenantID), admission.PatientVisitID(req.VisitID), admission.StatusEQ(admission.StatusActive)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: check existing admission: %w", err)
	}
	if alreadyAdmitted {
		return nil, fmt.Errorf("inpatient: visit already has an active admission")
	}

	b, err := s.client.Bed.Query().Where(bed.ID(req.BedID), bed.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: bed not found: %w", err)
	}
	if b.Status != bed.StatusAvailable {
		return nil, fmt.Errorf("inpatient: bed is not available (status=%s)", b.Status)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	admNo, err := sequence.Next(ctx, tx, tenantID, "admission_number", "ADM", 6)
	if err != nil {
		return nil, fmt.Errorf("inpatient: allocate admission_number: %w", err)
	}

	create := tx.Admission.Create().
		SetTenantID(tenantID).
		SetOutletID(visit.OutletID).
		SetPatientVisitID(req.VisitID).
		SetPatientID(visit.PatientID).
		SetAdmissionNumber(admNo).
		SetWardID(b.WardID).
		SetBedID(req.BedID)
	if req.AdmittedBy != uuid.Nil {
		create = create.SetAdmittedBy(req.AdmittedBy)
	}
	adm, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: create admission: %w", err)
	}

	if _, err = tx.Bed.UpdateOneID(req.BedID).SetStatus(bed.StatusOccupied).Save(ctx); err != nil {
		return nil, fmt.Errorf("inpatient: occupy bed: %w", err)
	}
	if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).
		SetVisitType(patientvisit.VisitTypeIPD).
		SetStatus(patientvisit.StatusAdmitted).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("inpatient: update visit: %w", err)
	}
	if _, err = s.billing.EnsureAccountForAdmission(ctx, tx, tenantID, visit.PatientID, adm.ID); err != nil {
		return nil, fmt.Errorf("inpatient: open admission account: %w", err)
	}

	if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, adm.ID.String(), events.EventAdmissionCreated, map[string]any{
		"admission_id":     adm.ID.String(),
		"visit_id":         req.VisitID.String(),
		"patient_id":       visit.PatientID.String(),
		"ward_id":          b.WardID.String(),
		"bed_id":           req.BedID.String(),
		"admission_number": admNo,
	}); pubErr != nil {
		s.log.Warn("publish admission.created failed", zap.Error(pubErr))
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("inpatient: commit admission: %w", err)
	}
	return adm, nil
}

// GetAdmission fetches an admission by ID, tenant-scoped.
func (s *Service) GetAdmission(ctx context.Context, tenantID, admissionID uuid.UUID) (*ent.Admission, error) {
	return s.client.Admission.Query().Where(admission.ID(admissionID), admission.TenantID(tenantID)).Only(ctx)
}

// ListAdmissions is the inpatient worklist — every current inpatient by default, or a specific
// status ("active"/"discharged").
func (s *Service) ListAdmissions(ctx context.Context, tenantID uuid.UUID, status string) ([]*ent.Admission, error) {
	q := s.client.Admission.Query().Where(admission.TenantID(tenantID))
	if status != "" {
		q = q.Where(admission.StatusEQ(admission.Status(status)))
	} else {
		q = q.Where(admission.StatusEQ(admission.StatusActive))
	}
	return q.Order(ent.Desc(admission.FieldAdmittedAt)).Limit(200).All(ctx)
}

func (s *Service) getActiveAdmission(ctx context.Context, tenantID, admissionID uuid.UUID) (*ent.Admission, error) {
	adm, err := s.GetAdmission(ctx, tenantID, admissionID)
	if err != nil {
		return nil, fmt.Errorf("inpatient: admission not found: %w", err)
	}
	if adm.Status != admission.StatusActive {
		return nil, fmt.Errorf("inpatient: admission is not active (status=%s)", adm.Status)
	}
	return adm, nil
}

func (s *Service) accountForAdmission(ctx context.Context, tenantID, admissionID uuid.UUID) (*ent.PatientAccount, error) {
	acct, _, err := s.billing.GetAccountByAdmission(ctx, tenantID, admissionID)
	if err != nil {
		return nil, fmt.Errorf("inpatient: %w", err)
	}
	return acct, nil
}

// TransferRequest is the input to Transfer — either an intra-facility ward/bed move (ToWardID/
// ToBedID required, admission stays open) or an inter-facility transfer-out (ReceivingFacilityName
// required, closes the admission via the same discharge-gate core as Discharge).
type TransferRequest struct {
	TransferType          string // "intra_facility" | "inter_facility"
	ToWardID              *uuid.UUID
	ToBedID               *uuid.UUID
	ReceivingFacilityName string
	ReferralID            *uuid.UUID
	AmbulanceBookingID    *uuid.UUID
	Reason                string
	TransferredBy         uuid.UUID
	// OverrideReason is only meaningful for an inter_facility transfer with an outstanding
	// balance — same contract as DischargeRequest.OverrideReason.
	OverrideReason string
}

// Transfer routes to the intra- or inter-facility path by TransferType.
func (s *Service) Transfer(ctx context.Context, tenantID, admissionID uuid.UUID, req TransferRequest) (*ent.Admission, *ent.PatientAccount, error) {
	adm, err := s.getActiveAdmission(ctx, tenantID, admissionID)
	if err != nil {
		return nil, nil, err
	}
	switch req.TransferType {
	case string(patienttransfer.TransferTypeIntraFacility):
		return s.transferIntraFacility(ctx, tenantID, adm, req)
	case string(patienttransfer.TransferTypeInterFacility):
		return s.transferInterFacility(ctx, tenantID, adm, req)
	default:
		return nil, nil, fmt.Errorf("inpatient: transfer_type must be intra_facility or inter_facility")
	}
}

// transferIntraFacility moves an active admission to a different bed/ward (e.g. general ward to
// ICU-lite, or a bed-turnover move within the same ward) and records a PatientTransfer row so
// discharge-time billing can bill each ward's rate for the nights actually spent there.
func (s *Service) transferIntraFacility(ctx context.Context, tenantID uuid.UUID, adm *ent.Admission, req TransferRequest) (*ent.Admission, *ent.PatientAccount, error) {
	if req.ToBedID == nil {
		return nil, nil, fmt.Errorf("inpatient: to_bed_id is required for an intra-facility transfer")
	}
	newBed, err := s.client.Bed.Query().Where(bed.ID(*req.ToBedID), bed.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: destination bed not found: %w", err)
	}
	if newBed.ID == adm.BedID {
		return nil, nil, fmt.Errorf("inpatient: patient is already in this bed")
	}
	if newBed.Status != bed.StatusAvailable {
		return nil, nil, fmt.Errorf("inpatient: destination bed is not available (status=%s)", newBed.Status)
	}
	oldBedID, oldWardID := adm.BedID, adm.WardID

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Bed.UpdateOneID(oldBedID).SetStatus(bed.StatusCleaning).Save(ctx); err != nil {
		return nil, nil, fmt.Errorf("inpatient: release old bed: %w", err)
	}
	if _, err = tx.Bed.UpdateOneID(newBed.ID).SetStatus(bed.StatusOccupied).Save(ctx); err != nil {
		return nil, nil, fmt.Errorf("inpatient: occupy new bed: %w", err)
	}
	updated, err := tx.Admission.UpdateOneID(adm.ID).
		SetWardID(newBed.WardID).
		SetBedID(newBed.ID).
		Save(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: update admission: %w", err)
	}

	if err = s.recordTransfer(ctx, tx, tenantID, adm.ID, patienttransfer.TransferTypeIntraFacility,
		oldWardID, oldBedID, &newBed.WardID, &newBed.ID, "", req); err != nil {
		return nil, nil, err
	}

	if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, adm.ID.String(), events.EventAdmissionTransferred, map[string]any{
		"admission_id": adm.ID.String(),
		"from_bed_id":  oldBedID.String(),
		"to_bed_id":    newBed.ID.String(),
		"to_ward_id":   newBed.WardID.String(),
		"reason":       req.Reason,
	}); pubErr != nil {
		s.log.Warn("publish admission.transferred failed", zap.Error(pubErr))
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("inpatient: commit transfer: %w", err)
	}
	acct, _ := s.accountForAdmission(ctx, tenantID, adm.ID)
	return updated, acct, nil
}

// transferInterFacility hands the patient off to another facility — from this facility's point of
// view that IS a discharge (the bed frees, the visit completes, the account must settle), so it
// reuses closeAdmission's exact gate/settlement logic rather than duplicating it, additionally
// recording a PatientTransfer row naming the receiving facility.
func (s *Service) transferInterFacility(ctx context.Context, tenantID uuid.UUID, adm *ent.Admission, req TransferRequest) (*ent.Admission, *ent.PatientAccount, error) {
	if req.ReceivingFacilityName == "" {
		return nil, nil, fmt.Errorf("inpatient: receiving_facility_name is required for an inter-facility transfer")
	}
	summary := req.Reason
	if summary == "" {
		summary = fmt.Sprintf("Transferred to %s", req.ReceivingFacilityName)
	}
	return s.closeAdmission(ctx, tenantID, adm, closeParams{
		by: req.TransferredBy, summary: summary, overrideReason: req.OverrideReason,
		transferOut: &req,
	})
}

// recordTransfer writes one PatientTransfer row within tx. toWardID/toBedID are nil for an
// inter-facility transfer (the patient leaves this facility's bed register); receivingFacility is
// only set for that case.
func (s *Service) recordTransfer(ctx context.Context, tx *ent.Tx, tenantID, admissionID uuid.UUID, transferType patienttransfer.TransferType, fromWardID, fromBedID uuid.UUID, toWardID, toBedID *uuid.UUID, receivingFacility string, req TransferRequest) error {
	create := tx.PatientTransfer.Create().
		SetTenantID(tenantID).
		SetAdmissionID(admissionID).
		SetTransferType(transferType).
		SetFromWardID(fromWardID).
		SetFromBedID(fromBedID).
		SetReason(req.Reason)
	if toWardID != nil {
		create = create.SetToWardID(*toWardID)
	}
	if toBedID != nil {
		create = create.SetToBedID(*toBedID)
	}
	if receivingFacility != "" {
		create = create.SetReceivingFacilityName(receivingFacility)
	}
	if req.TransferredBy != uuid.Nil {
		create = create.SetTransferredBy(req.TransferredBy)
	}
	if req.ReferralID != nil {
		create = create.SetReferralID(*req.ReferralID)
	}
	if req.AmbulanceBookingID != nil {
		create = create.SetAmbulanceBookingID(*req.AmbulanceBookingID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("inpatient: record transfer: %w", err)
	}
	return nil
}

// nightsStayed rounds a stay up to whole nights, minimum 1 — a same-day admission/discharge still
// occupied a bed for (at least) one night's charge.
func nightsStayed(admittedAt, now time.Time) int {
	nights := int(math.Ceil(now.Sub(admittedAt).Hours() / 24))
	if nights < 1 {
		nights = 1
	}
	return nights
}

// wardSegment is one span of time an admission spent under a single ward's rate.
type wardSegment struct {
	wardID uuid.UUID
	from   time.Time
	to     time.Time
}

// buildWardSegments reconstructs which ward the admission was in during each span of its stay,
// from admission to `until`, using its intra-facility PatientTransfer history. An admission with
// no transfers is a single segment under its current (only) ward.
func (s *Service) buildWardSegments(ctx context.Context, tenantID uuid.UUID, adm *ent.Admission, until time.Time) ([]wardSegment, error) {
	transfers, err := s.client.PatientTransfer.Query().
		Where(patienttransfer.TenantID(tenantID), patienttransfer.AdmissionID(adm.ID), patienttransfer.TransferTypeEQ(patienttransfer.TransferTypeIntraFacility)).
		Order(ent.Asc(patienttransfer.FieldTransferredAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpatient: list transfer history: %w", err)
	}

	segments := make([]wardSegment, 0, len(transfers)+1)
	cursor := adm.AdmittedAt
	currentWard := adm.WardID
	if len(transfers) > 0 {
		currentWard = transfers[0].FromWardID
	}
	for _, t := range transfers {
		segments = append(segments, wardSegment{wardID: currentWard, from: cursor, to: t.TransferredAt})
		cursor = t.TransferredAt
		if t.ToWardID != nil {
			currentWard = *t.ToWardID
		}
	}
	segments = append(segments, wardSegment{wardID: currentWard, from: cursor, to: until})
	return segments, nil
}

// postWardCharges posts the admission's ward/day-rate charge(s) exactly once (guarded by
// ward_charge_posted), split by ward if an intra-facility transfer changed the rate mid-stay.
// Total nights charged always equals nightsStayed(admitted_at, until) — identical to a never-
// transferred stay's total — allocated to each ward segment by whole elapsed days, with any
// remainder attributed to the FINAL segment (the ward the patient ends up in). This is a
// deliberate, documented simplification: true calendar-day/midnight-census attribution is real
// hospital practice but meaningfully more complex to get right correctly; this is exact for the
// (by far most common) no-transfer case and a fair, non-double-counting approximation otherwise.
// A ward with no matching BillableItemCatalog rate configured (via its own billable_item_code, or
// the tenant default WARD_DAY_RATE) is charged nothing for that segment, not an error — some
// tenants price inpatient stays entirely through per-department charges instead.
func (s *Service) postWardCharges(ctx context.Context, tenantID uuid.UUID, adm *ent.Admission, postedBy uuid.UUID, until time.Time) error {
	segments, err := s.buildWardSegments(ctx, tenantID, adm, until)
	if err != nil {
		return err
	}

	totalNights := nightsStayed(adm.AdmittedAt, until)
	floors := make([]int, len(segments))
	sumFloors := 0
	for i, sg := range segments {
		f := int(sg.to.Sub(sg.from).Hours() / 24)
		if f < 0 {
			f = 0
		}
		floors[i] = f
		sumFloors += f
	}
	if remainder := totalNights - sumFloors; remainder > 0 {
		floors[len(floors)-1] += remainder
	}

	// Aggregate nights by ward in first-seen order (a patient could transfer back to an earlier
	// ward, so a plain map alone would lose deterministic charge-line ordering).
	order := make([]uuid.UUID, 0, len(segments))
	nightsByWard := make(map[uuid.UUID]int, len(segments))
	for i, sg := range segments {
		if floors[i] <= 0 {
			continue
		}
		if _, seen := nightsByWard[sg.wardID]; !seen {
			order = append(order, sg.wardID)
		}
		nightsByWard[sg.wardID] += floors[i]
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("inpatient: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	admID := adm.ID
	for _, wardID := range order {
		nights := nightsByWard[wardID]
		code := "WARD_DAY_RATE"
		wardName := "Ward"
		if w, werr := s.client.Ward.Get(ctx, wardID); werr == nil {
			wardName = w.Name
			if w.BillableItemCode != nil && *w.BillableItemCode != "" {
				code = *w.BillableItemCode
			}
		}
		item, ierr := s.billing.GetCatalogItemByCode(ctx, tenantID, "inpatient", code)
		if ierr != nil || item.Price == nil {
			continue
		}
		label := "night"
		if nights != 1 {
			label = "nights"
		}
		itemID := item.ID
		if _, err = s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
			PatientID:      adm.PatientID,
			VisitID:        adm.PatientVisitID,
			SourceModule:   "inpatient",
			SourceRefID:    &admID,
			Description:    fmt.Sprintf("Ward charge — %s (%d %s)", wardName, nights, label),
			Amount:         float64(nights) * (*item.Price),
			CreatedByUser:  postedBy,
			BillableItemID: &itemID,
		}); err != nil {
			return fmt.Errorf("inpatient: post ward charge: %w", err)
		}
	}

	if _, err = tx.Admission.UpdateOneID(adm.ID).SetWardChargePosted(true).Save(ctx); err != nil {
		return fmt.Errorf("inpatient: mark ward charge posted: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("inpatient: commit ward charges: %w", err)
	}
	return nil
}

// DischargeRequest is the input to Discharge.
type DischargeRequest struct {
	DischargedBy uuid.UUID
	Summary      string
	// OverrideReason, when set, releases the patient despite an outstanding balance — the caller
	// (HTTP handler) must already have checked the caller holds
	// rbac.PermBillingOverrideSettlement before setting this; Discharge itself does not re-check
	// permissions, matching billing.Service.OverrideSettlement's own contract.
	OverrideReason string
}

// Discharge posts the final ward/day-rate charge(s) (once), then blocks (returning
// ErrOutstandingBalance) while the admission account's balance is positive and no override was
// given — surfacing Record Payment / Apply Insurance / Write-Off / next-of-kin-settle options via
// Sprint 5's existing billing endpoints. Once settled (or overridden), discharges the patient:
// frees the bed to "cleaning" (a housekeeping step must run before it's available again),
// completes the visit, and publishes hospital.visit.discharged.
func (s *Service) Discharge(ctx context.Context, tenantID, admissionID uuid.UUID, req DischargeRequest) (*ent.Admission, *ent.PatientAccount, error) {
	adm, err := s.getActiveAdmission(ctx, tenantID, admissionID)
	if err != nil {
		return nil, nil, err
	}
	return s.closeAdmission(ctx, tenantID, adm, closeParams{
		by: req.DischargedBy, summary: req.Summary, overrideReason: req.OverrideReason,
	})
}

// closeAdmission is the shared gate/settlement/close core for both an ordinary Discharge and an
// inter-facility Transfer's transfer-out (which IS a discharge from this facility's point of
// view) — see docs/architecture.md "Referral, Transfer & Ambulance Billing".
type closeParams struct {
	by             uuid.UUID
	summary        string
	overrideReason string
	// transferOut, when set, means this close is an inter-facility transfer-out: a PatientTransfer
	// row is recorded alongside the ordinary discharge writes.
	transferOut *TransferRequest
}

func (s *Service) closeAdmission(ctx context.Context, tenantID uuid.UUID, adm *ent.Admission, p closeParams) (*ent.Admission, *ent.PatientAccount, error) {
	if !adm.WardChargePosted {
		if err := s.postWardCharges(ctx, tenantID, adm, p.by, time.Now()); err != nil {
			return nil, nil, err
		}
	}

	acct, err := s.accountForAdmission(ctx, tenantID, adm.ID)
	if err != nil {
		return nil, nil, err
	}

	if acct.Balance > 0 {
		if p.overrideReason == "" {
			return nil, acct, &ErrOutstandingBalance{Account: acct}
		}
		if _, err = s.billing.OverrideSettlement(ctx, tenantID, acct.ID, p.by, p.overrideReason); err != nil {
			return nil, nil, fmt.Errorf("inpatient: override settlement: %w", err)
		}
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()
	update := tx.Admission.UpdateOneID(adm.ID).
		SetStatus(admission.StatusDischarged).
		SetDischargedAt(now).
		SetDischargeSummary(p.summary)
	if p.by != uuid.Nil {
		update = update.SetDischargedBy(p.by)
	}
	updatedAdm, err := update.Save(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("inpatient: update admission: %w", err)
	}
	if _, err = tx.Bed.UpdateOneID(adm.BedID).SetStatus(bed.StatusCleaning).Save(ctx); err != nil {
		return nil, nil, fmt.Errorf("inpatient: release bed: %w", err)
	}
	if _, err = tx.PatientVisit.UpdateOneID(adm.PatientVisitID).
		SetStatus(patientvisit.StatusCompleted).
		SetDischargedAt(now).
		Save(ctx); err != nil {
		return nil, nil, fmt.Errorf("inpatient: complete visit: %w", err)
	}

	if p.transferOut != nil {
		if err = s.recordTransfer(ctx, tx, tenantID, adm.ID, patienttransfer.TransferTypeInterFacility,
			adm.WardID, adm.BedID, nil, nil, p.transferOut.ReceivingFacilityName, *p.transferOut); err != nil {
			return nil, nil, err
		}
	}

	if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, adm.PatientVisitID.String(), events.EventVisitDischarged, map[string]any{
		"admission_id":    adm.ID.String(),
		"visit_id":        adm.PatientVisitID.String(),
		"patient_id":      adm.PatientID.String(),
		"transferred_out": p.transferOut != nil,
	}); pubErr != nil {
		s.log.Warn("publish visit.discharged failed", zap.Error(pubErr))
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("inpatient: commit discharge: %w", err)
	}

	acct, acctErr := s.accountForAdmission(ctx, tenantID, adm.ID)
	if acctErr != nil {
		acct = nil
	}
	return updatedAdm, acct, nil
}
