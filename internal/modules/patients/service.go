// Package patients implements Sprint 1: patient registration, OPD visit check-in/queue, and
// triage recording — the spine every later clinical module (Consultation, Lab, Pharmacy,
// Inpatient) attaches to via PatientVisit. Migrated in meaning from pos-api's pharmacy/clinical
// handlers (see migration-pos-pharmacy.md), not copy-pasted.
package patients

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/patient"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	events "github.com/bengobox/hospital-service/internal/events"
	"github.com/bengobox/hospital-service/internal/modules/sequence"
)

// Service implements patient/visit/triage business logic.
type Service struct {
	client *ent.Client
	log    *zap.Logger
}

// NewService creates a new patients service.
func NewService(client *ent.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log.Named("patients.service")}
}

// RegisterPatientRequest is the input to RegisterPatient.
type RegisterPatientRequest struct {
	FullName  string
	DOB       *time.Time
	Sex       string
	Phone     string
	IDNumber  string
	Address   string
	NextOfKin string
	Allergies []string
	OutletID  uuid.UUID
}

// RegisterPatient creates a new Patient with a sequence-allocated MRN.
func (s *Service) RegisterPatient(ctx context.Context, tenantID uuid.UUID, req RegisterPatientRequest) (*ent.Patient, error) {
	if req.FullName == "" {
		return nil, fmt.Errorf("patients: full_name is required")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	mrn, err := sequence.Next(ctx, tx, tenantID, sequence.KindMRN, "MRN", 6)
	if err != nil {
		return nil, fmt.Errorf("patients: allocate mrn: %w", err)
	}

	create := tx.Patient.Create().
		SetTenantID(tenantID).
		SetOutletID(req.OutletID).
		SetMrn(mrn).
		SetFullName(req.FullName).
		SetSex(req.Sex).
		SetPhone(req.Phone).
		SetIDNumber(req.IDNumber).
		SetAddress(req.Address).
		SetNextOfKin(req.NextOfKin)
	if req.DOB != nil {
		create = create.SetDob(*req.DOB)
	}
	if req.Allergies != nil {
		create = create.SetAllergyFlags(req.Allergies)
	}

	p, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: create patient: %w", err)
	}

	if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, p.ID.String(), events.EventPatientCreated, map[string]any{
		"patient_id": p.ID.String(),
		"mrn":        p.Mrn,
		"full_name":  p.FullName,
		"outlet_id":  p.OutletID.String(),
	}); pubErr != nil {
		s.log.Warn("publish patient.created failed", zap.Error(pubErr))
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("patients: commit registration: %w", err)
	}
	return p, nil
}

// GetPatient fetches a patient by ID, tenant-scoped.
func (s *Service) GetPatient(ctx context.Context, tenantID, patientID uuid.UUID) (*ent.Patient, error) {
	return s.client.Patient.Query().
		Where(patient.ID(patientID), patient.TenantID(tenantID)).
		Only(ctx)
}

// ListPatientsRequest filters the patient search/list.
type ListPatientsRequest struct {
	Query  string // matches full_name/phone/id_number/mrn
	Limit  int
	Offset int
}

// ListPatients searches/lists patients for a tenant.
func (s *Service) ListPatients(ctx context.Context, tenantID uuid.UUID, req ListPatientsRequest) ([]*ent.Patient, error) {
	q := s.client.Patient.Query().Where(patient.TenantID(tenantID))
	if req.Query != "" {
		q = q.Where(patient.Or(
			patient.FullNameContainsFold(req.Query),
			patient.PhoneContainsFold(req.Query),
			patient.IDNumberContainsFold(req.Query),
			patient.MrnContainsFold(req.Query),
		))
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return q.Order(ent.Desc(patient.FieldCreatedAt)).Limit(limit).Offset(req.Offset).All(ctx)
}

// CheckInVisitRequest is the input to CheckInVisit.
type CheckInVisitRequest struct {
	PatientID      uuid.UUID
	OutletID       uuid.UUID
	VisitType      string // "OPD" | "IPD" — defaults to OPD
	ChiefComplaint string
	RegisteredBy   *uuid.UUID
}

// CheckInVisit opens a new PatientVisit episode of care for an already-registered patient.
func (s *Service) CheckInVisit(ctx context.Context, tenantID uuid.UUID, req CheckInVisitRequest) (*ent.PatientVisit, error) {
	if req.PatientID == uuid.Nil {
		return nil, fmt.Errorf("patients: patient_id is required")
	}
	// Confirm the patient exists in this tenant before opening a visit against it.
	if _, err := s.GetPatient(ctx, tenantID, req.PatientID); err != nil {
		return nil, fmt.Errorf("patients: patient not found: %w", err)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	visitNo, err := sequence.Next(ctx, tx, tenantID, sequence.KindVisitNumber, "V", 6)
	if err != nil {
		return nil, fmt.Errorf("patients: allocate visit_number: %w", err)
	}

	visitType := resolveVisitType(req.VisitType)

	create := tx.PatientVisit.Create().
		SetTenantID(tenantID).
		SetOutletID(req.OutletID).
		SetPatientID(req.PatientID).
		SetVisitNumber(visitNo).
		SetVisitType(visitType).
		SetChiefComplaint(req.ChiefComplaint)
	if req.RegisteredBy != nil {
		create = create.SetRegisteredBy(*req.RegisteredBy)
	}

	v, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: create visit: %w", err)
	}

	if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, v.ID.String(), events.EventVisitAdmitted, map[string]any{
		"visit_id":     v.ID.String(),
		"patient_id":   v.PatientID.String(),
		"visit_number": v.VisitNumber,
		"outlet_id":    v.OutletID.String(),
	}); pubErr != nil {
		s.log.Warn("publish visit.admitted failed", zap.Error(pubErr))
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("patients: commit check-in: %w", err)
	}
	return v, nil
}

// GetVisit fetches a visit by ID, tenant-scoped.
func (s *Service) GetVisit(ctx context.Context, tenantID, visitID uuid.UUID) (*ent.PatientVisit, error) {
	return s.client.PatientVisit.Query().
		Where(patientvisit.ID(visitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
}

// ListVisitsRequest filters the OPD queue.
type ListVisitsRequest struct {
	Status   string // empty = all open (not completed/cancelled)
	OutletID *uuid.UUID
	Limit    int
}

// ListVisits returns the OPD queue for a tenant, optionally filtered by status/outlet.
func (s *Service) ListVisits(ctx context.Context, tenantID uuid.UUID, req ListVisitsRequest) ([]*ent.PatientVisit, error) {
	q := s.client.PatientVisit.Query().Where(patientvisit.TenantID(tenantID))
	if req.Status != "" {
		q = q.Where(patientvisit.StatusEQ(patientvisit.Status(req.Status)))
	} else {
		q = q.Where(patientvisit.StatusNotIn(patientvisit.StatusCompleted, patientvisit.StatusCancelled))
	}
	if req.OutletID != nil {
		q = q.Where(patientvisit.OutletID(*req.OutletID))
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return q.Order(ent.Asc(patientvisit.FieldCreatedAt)).Limit(limit).All(ctx)
}

// RecordTriageRequest is the input to RecordTriage.
type RecordTriageRequest struct {
	VisitID            uuid.UUID
	TakenBy            uuid.UUID
	BPSystolic         *int
	BPDiastolic        *int
	TemperatureCelsius *float64
	PulseBPM           *int
	RespirationRate    *int
	SpO2Percent        *float64
	WeightKg           *float64
	HeightCm           *float64
	Priority           string
	Notes              string
}

// RecordTriage records a vitals/acuity reading for a visit and advances its status to
// "triaged" (unless the visit has already moved past that stage — a re-triage doesn't rewind
// the workflow).
func (s *Service) RecordTriage(ctx context.Context, tenantID uuid.UUID, req RecordTriageRequest) (*ent.TriageRecord, error) {
	visit, err := s.GetVisit(ctx, tenantID, req.VisitID)
	if err != nil {
		return nil, fmt.Errorf("patients: visit not found: %w", err)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	create := tx.TriageRecord.Create().
		SetTenantID(tenantID).
		SetVisitID(req.VisitID).
		SetTakenBy(req.TakenBy).
		SetPriority(req.Priority).
		SetNotes(req.Notes)
	if req.BPSystolic != nil {
		create = create.SetBpSystolic(*req.BPSystolic)
	}
	if req.BPDiastolic != nil {
		create = create.SetBpDiastolic(*req.BPDiastolic)
	}
	if req.TemperatureCelsius != nil {
		create = create.SetTemperatureCelsius(*req.TemperatureCelsius)
	}
	if req.PulseBPM != nil {
		create = create.SetPulseBpm(*req.PulseBPM)
	}
	if req.RespirationRate != nil {
		create = create.SetRespirationRate(*req.RespirationRate)
	}
	if req.SpO2Percent != nil {
		create = create.SetSpo2Percent(*req.SpO2Percent)
	}
	if req.WeightKg != nil {
		create = create.SetWeightKg(*req.WeightKg)
	}
	if req.HeightCm != nil {
		create = create.SetHeightCm(*req.HeightCm)
	}

	t, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("patients: create triage record: %w", err)
	}

	if next, ok := nextVisitStatusAfterTriage(visit.Status); ok {
		if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).SetStatus(next).Save(ctx); err != nil {
			return nil, fmt.Errorf("patients: advance visit status: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("patients: commit triage: %w", err)
	}
	return t, nil
}

// resolveVisitType normalizes the requested visit type to a valid patientvisit.VisitType. Only
// an exact "IPD" match opts into inpatient; anything else (empty string, "OPD", or a garbage
// value) defaults to OPD, matching CheckInVisit's pre-extraction inline behaviour.
func resolveVisitType(reqVisitType string) patientvisit.VisitType {
	if reqVisitType == string(patientvisit.VisitTypeIPD) {
		return patientvisit.VisitTypeIPD
	}
	return patientvisit.VisitTypeOPD
}

// nextVisitStatusAfterTriage decides whether recording a triage reading should advance the
// visit's status to "triaged", and returns ok=false when it shouldn't. A re-triage on a visit
// that has already moved past the initial "registered" stage does not rewind the workflow — the
// caller leaves the visit's status untouched in that case.
func nextVisitStatusAfterTriage(current patientvisit.Status) (next patientvisit.Status, ok bool) {
	if current == patientvisit.StatusRegistered {
		return patientvisit.StatusTriaged, true
	}
	return "", false
}
