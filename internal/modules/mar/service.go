// Package mar implements the Medication Administration Record (MAR): per-dose nurse charting for
// an admitted patient, distinct from pharmacy.Service's own dispense event. See
// mvp-gap-backlog-2026-09-02.md Sprint 4 item 1 and internal/ent/schema/
// medication_administration.go's own doc comment for why this is a new small module rather than a
// PrescriptionLine/TriageRecord extension.
package mar

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/admission"
	"github.com/bengobox/hospital-service/internal/ent/medicationadministration"
	"github.com/bengobox/hospital-service/internal/ent/prescription"
)

// Service implements MAR business logic.
type Service struct {
	client *ent.Client
	log    *zap.Logger
}

// NewService creates a new mar service.
func NewService(client *ent.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log.Named("mar.service")}
}

// ChartDoseRequest is the input to ChartDose.
type ChartDoseRequest struct {
	AdmissionID        uuid.UUID
	PrescriptionLineID uuid.UUID
	ScheduledTime      *time.Time // defaults to now if nil — the common on-time-charting case
	Status             string     // given|refused|missed|held
	AdministeredBy     uuid.UUID
	Notes              string
}

// ChartDose records one dose event against an admission's prescription line — charted on-demand
// by a nurse (see this package's own doc comment for why there's no pre-populated schedule to
// check off instead). Validates the line actually belongs to a prescription written for THIS
// admission's own visit, so a dose can't be charted against a different patient's medication.
func (s *Service) ChartDose(ctx context.Context, tenantID uuid.UUID, req ChartDoseRequest) (*ent.MedicationAdministration, error) {
	if req.AdmissionID == uuid.Nil || req.PrescriptionLineID == uuid.Nil {
		return nil, fmt.Errorf("mar: admission_id and prescription_line_id are required")
	}
	status := medicationadministration.Status(req.Status)
	switch status {
	case medicationadministration.StatusGiven, medicationadministration.StatusRefused,
		medicationadministration.StatusMissed, medicationadministration.StatusHeld:
	default:
		return nil, fmt.Errorf("mar: status must be one of given|refused|missed|held")
	}

	adm, err := s.client.Admission.Query().
		Where(admission.ID(req.AdmissionID), admission.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("mar: admission not found: %w", err)
	}
	line, err := s.client.PrescriptionLine.Get(ctx, req.PrescriptionLineID)
	if err != nil {
		return nil, fmt.Errorf("mar: prescription line not found: %w", err)
	}
	rx, err := line.QueryPrescription().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("mar: parent prescription not found: %w", err)
	}
	if rx.TenantID != tenantID || rx.VisitID == nil || *rx.VisitID != adm.PatientVisitID {
		return nil, fmt.Errorf("mar: prescription line does not belong to this admission's visit")
	}

	create := s.client.MedicationAdministration.Create().
		SetTenantID(tenantID).
		SetAdmissionID(req.AdmissionID).
		SetPrescriptionLineID(req.PrescriptionLineID).
		SetStatus(status).
		SetNotes(req.Notes)
	if req.ScheduledTime != nil {
		create = create.SetScheduledTime(*req.ScheduledTime)
	}
	if req.AdministeredBy != uuid.Nil {
		create = create.SetAdministeredBy(req.AdministeredBy)
	}
	if status == medicationadministration.StatusGiven {
		create = create.SetAdministeredAt(time.Now())
	}
	return create.Save(ctx)
}

// ListByAdmission returns every charted dose for an admission, newest first — the nurse-facing
// MAR history for the admission detail page.
func (s *Service) ListByAdmission(ctx context.Context, tenantID, admissionID uuid.UUID) ([]*ent.MedicationAdministration, error) {
	return s.client.MedicationAdministration.Query().
		Where(medicationadministration.TenantID(tenantID), medicationadministration.AdmissionID(admissionID)).
		Order(ent.Desc(medicationadministration.FieldCreatedAt)).
		All(ctx)
}

// ListActivePrescriptionLines returns the prescription lines available to chart a dose against
// for an admission — every non-cancelled/non-rejected prescription written for the admission's
// own visit, with its lines eager-loaded. Powers the "chart a dose" screen's drug picker.
func (s *Service) ListActivePrescriptionLines(ctx context.Context, tenantID, admissionID uuid.UUID) ([]*ent.Prescription, error) {
	adm, err := s.client.Admission.Query().
		Where(admission.ID(admissionID), admission.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("mar: admission not found: %w", err)
	}
	return s.client.Prescription.Query().
		Where(
			prescription.TenantID(tenantID),
			prescription.VisitID(adm.PatientVisitID),
			prescription.StatusNotIn(prescription.StatusRejected, prescription.StatusCancelled),
		).
		WithLines().
		Order(ent.Desc(prescription.FieldCreatedAt)).
		All(ctx)
}
