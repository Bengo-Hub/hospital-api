// Package consultation implements Sprint 2: doctor/dental/MCH/specialist consultation +
// examination recording, the diagnosis catalogue (global + tenant-custom), and referrals to
// lab/pharmacy/another facility.
package consultation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/diagnosiscatalogdefault"
	"github.com/bengobox/hospital-service/internal/ent/diagnosiscatalogentry"
	"github.com/bengobox/hospital-service/internal/ent/examinationrecord"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/referral"
)

// Service implements consultation/examination/diagnosis-catalog/referral business logic.
type Service struct {
	client *ent.Client
	log    *zap.Logger
}

// NewService creates a new consultation service.
func NewService(client *ent.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log.Named("consultation.service")}
}

// RecordExaminationRequest is the input to RecordExamination.
type RecordExaminationRequest struct {
	VisitID        uuid.UUID
	ClinicianID    uuid.UUID
	QueueType      string // doctor|dental|mch|specialist
	ChiefComplaint string
	DiagnosisCode  string
	DiagnosisName  string
	Notes          string
	Complete       bool // true = record is final (status "completed"); false = still "in_progress"
}

// RecordExamination records a consultation note (optionally with a final diagnosis) against a
// visit. Completing an examination with no pending referral advances the visit to "completed";
// a subsequent CreateReferral call (lab/pharmacy) instead moves it to "awaiting_lab"/"prescribed".
func (s *Service) RecordExamination(ctx context.Context, tenantID uuid.UUID, req RecordExaminationRequest) (*ent.ExaminationRecord, error) {
	if req.VisitID == uuid.Nil {
		return nil, fmt.Errorf("consultation: visit_id is required")
	}
	visit, err := s.client.PatientVisit.Query().
		Where(patientvisit.ID(req.VisitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: visit not found: %w", err)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	queueType := examinationrecord.QueueTypeDoctor
	if v := examinationrecord.QueueType(req.QueueType); v != "" {
		queueType = v
	}
	create := tx.ExaminationRecord.Create().
		SetTenantID(tenantID).
		SetVisitID(req.VisitID).
		SetClinicianID(req.ClinicianID).
		SetQueueType(queueType).
		SetChiefComplaint(req.ChiefComplaint).
		SetDiagnosisCode(req.DiagnosisCode).
		SetDiagnosisName(req.DiagnosisName).
		SetNotes(req.Notes)
	if req.Complete {
		create = create.SetStatus(examinationrecord.StatusCompleted)
	}

	rec, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: create examination record: %w", err)
	}

	if req.Complete && visit.Status != patientvisit.StatusCompleted {
		if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).SetStatus(patientvisit.StatusCompleted).Save(ctx); err != nil {
			return nil, fmt.Errorf("consultation: advance visit status: %w", err)
		}
	} else if !req.Complete && visit.Status == patientvisit.StatusTriaged {
		if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).SetStatus(patientvisit.StatusInExamination).Save(ctx); err != nil {
			return nil, fmt.Errorf("consultation: advance visit status: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("consultation: commit examination: %w", err)
	}
	return rec, nil
}

// GetExamination fetches an examination record by ID, tenant-scoped.
func (s *Service) GetExamination(ctx context.Context, tenantID, id uuid.UUID) (*ent.ExaminationRecord, error) {
	return s.client.ExaminationRecord.Query().
		Where(examinationrecord.ID(id), examinationrecord.TenantID(tenantID)).
		Only(ctx)
}

// DiagnosisEntry is the merged global+tenant diagnosis catalogue row returned to callers.
type DiagnosisEntry struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Category string    `json:"category,omitempty"`
	IsGlobal bool      `json:"is_global"`
}

// ListDiagnosisCatalog returns the global default catalogue UNIONed with the tenant's own
// custom entries.
func (s *Service) ListDiagnosisCatalog(ctx context.Context, tenantID uuid.UUID) ([]DiagnosisEntry, error) {
	defaults, err := s.client.DiagnosisCatalogDefault.Query().
		Where(diagnosiscatalogdefault.IsActive(true)).
		Order(ent.Asc(diagnosiscatalogdefault.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: list default diagnoses: %w", err)
	}
	entries, err := s.client.DiagnosisCatalogEntry.Query().
		Where(diagnosiscatalogentry.TenantID(tenantID), diagnosiscatalogentry.IsActive(true)).
		Order(ent.Asc(diagnosiscatalogentry.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: list tenant diagnoses: %w", err)
	}

	out := make([]DiagnosisEntry, 0, len(defaults)+len(entries))
	for _, d := range defaults {
		out = append(out, DiagnosisEntry{ID: d.ID, Code: d.Code, Name: d.Name, Category: d.Category, IsGlobal: true})
	}
	for _, e := range entries {
		out = append(out, DiagnosisEntry{ID: e.ID, Code: e.Code, Name: e.Name, Category: e.Category, IsGlobal: false})
	}
	return out, nil
}

// CreateDiagnosisEntryRequest is the input to CreateDiagnosisEntry.
type CreateDiagnosisEntryRequest struct {
	Code     string
	Name     string
	Category string
}

// CreateDiagnosisEntry adds a tenant-custom diagnosis catalogue entry.
func (s *Service) CreateDiagnosisEntry(ctx context.Context, tenantID uuid.UUID, req CreateDiagnosisEntryRequest) (*ent.DiagnosisCatalogEntry, error) {
	if req.Code == "" || req.Name == "" {
		return nil, fmt.Errorf("consultation: code and name are required")
	}
	return s.client.DiagnosisCatalogEntry.Create().
		SetTenantID(tenantID).
		SetCode(req.Code).
		SetName(req.Name).
		SetCategory(req.Category).
		Save(ctx)
}

// CreateReferralRequest is the input to CreateReferral.
type CreateReferralRequest struct {
	VisitID    uuid.UUID
	ReferredTo string // lab|pharmacy|external_facility|specialist
	Reason     string
	ReferredBy uuid.UUID
}

// CreateReferral records a hand-off from consultation to another stage and advances the
// visit's status accordingly (lab -> awaiting_lab, pharmacy -> prescribed).
func (s *Service) CreateReferral(ctx context.Context, tenantID uuid.UUID, req CreateReferralRequest) (*ent.Referral, error) {
	if req.VisitID == uuid.Nil || req.ReferredTo == "" {
		return nil, fmt.Errorf("consultation: visit_id and referred_to are required")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	referralCreate := tx.Referral.Create().
		SetTenantID(tenantID).
		SetVisitID(req.VisitID).
		SetReferredTo(req.ReferredTo).
		SetReason(req.Reason)
	if req.ReferredBy != uuid.Nil {
		referralCreate = referralCreate.SetReferredBy(req.ReferredBy)
	}
	r, err := referralCreate.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: create referral: %w", err)
	}

	var nextStatus patientvisit.Status
	switch req.ReferredTo {
	case "lab":
		nextStatus = patientvisit.StatusAwaitingLab
	case "pharmacy":
		nextStatus = patientvisit.StatusPrescribed
	}
	if nextStatus != "" {
		if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).
			Where(patientvisit.TenantID(tenantID)).
			SetStatus(nextStatus).Save(ctx); err != nil {
			return nil, fmt.Errorf("consultation: advance visit status: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("consultation: commit referral: %w", err)
	}
	return r, nil
}

// ListReferrals returns every referral raised for a visit.
func (s *Service) ListReferrals(ctx context.Context, tenantID, visitID uuid.UUID) ([]*ent.Referral, error) {
	return s.client.Referral.Query().
		Where(referral.TenantID(tenantID), referral.VisitID(visitID)).
		Order(ent.Desc(referral.FieldCreatedAt)).
		All(ctx)
}
