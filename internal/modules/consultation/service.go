// Package consultation implements Sprint 2: doctor/dental/MCH/specialist consultation +
// examination recording, the diagnosis catalogue (global + tenant-custom), and referrals to
// lab/pharmacy/another facility.
package consultation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billableitemcatalog"
	"github.com/bengobox/hospital-service/internal/ent/diagnosiscatalogdefault"
	"github.com/bengobox/hospital-service/internal/ent/diagnosiscatalogentry"
	"github.com/bengobox/hospital-service/internal/ent/examinationrecord"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/referral"
	"github.com/bengobox/hospital-service/internal/ent/schema"
	"github.com/bengobox/hospital-service/internal/modules/billing"
)

// Service implements consultation/examination/diagnosis-catalog/referral business logic.
type Service struct {
	client  *ent.Client
	billing *billing.Service
	log     *zap.Logger
}

// NewService creates a new consultation service.
func NewService(client *ent.Client, billingSvc *billing.Service, log *zap.Logger) *Service {
	return &Service{client: client, billing: billingSvc, log: log.Named("consultation.service")}
}

// RecordExaminationRequest is the input to RecordExamination.
type RecordExaminationRequest struct {
	VisitID              uuid.UUID
	ClinicianID          uuid.UUID
	QueueType            string // doctor|dental|mch|specialist
	ChiefComplaint       string
	DiagnosisCode        string
	DiagnosisName        string
	ReviewOfSystems      map[string]string
	PhysicalExamFindings map[string]string
	TreatmentPlan        string
	Notes                string
	Complete             bool // true = record is final (status "completed"); false = still "in_progress"
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

	// Counted BEFORE creating this record, so a visit's very first examination sees 0 — the
	// consultation fee is charged once per visit, not once per examination NOTE (a visit can
	// accumulate several ExaminationRecord rows over its lifetime, e.g. resumed after lab
	// results return — see lab.Service.EnterResult's reopen-to-in_progress behaviour).
	priorExamCount, cerr := tx.ExaminationRecord.Query().
		Where(examinationrecord.TenantID(tenantID), examinationrecord.VisitID(req.VisitID)).
		Count(ctx)
	if cerr != nil {
		s.log.Warn("consultation fee: count prior examinations failed", zap.Error(cerr))
		priorExamCount = -1 // suppress charging below rather than risk a double-charge on a failed count
	}

	// Latest prior record for THIS visit (if any) — carries forward diagnosis_history, and its
	// own diagnosis_code/name is the baseline this write's diagnosis is compared against.
	prior, perr := tx.ExaminationRecord.Query().
		Where(examinationrecord.TenantID(tenantID), examinationrecord.VisitID(req.VisitID)).
		Order(ent.Desc(examinationrecord.FieldExaminedAt)).
		First(ctx)
	if perr != nil && !ent.IsNotFound(perr) {
		s.log.Warn("diagnosis history: fetch prior examination failed", zap.Error(perr))
	}
	diagnosisHistory := appendDiagnosisHistory(prior, req.DiagnosisCode, req.DiagnosisName, req.ClinicianID)

	queueType := resolveQueueType(req.QueueType)
	create := tx.ExaminationRecord.Create().
		SetTenantID(tenantID).
		SetVisitID(req.VisitID).
		SetClinicianID(req.ClinicianID).
		SetQueueType(queueType).
		SetChiefComplaint(req.ChiefComplaint).
		SetDiagnosisCode(req.DiagnosisCode).
		SetDiagnosisName(req.DiagnosisName).
		SetDiagnosisHistory(diagnosisHistory).
		SetReviewOfSystems(req.ReviewOfSystems).
		SetPhysicalExamFindings(req.PhysicalExamFindings).
		SetTreatmentPlan(req.TreatmentPlan).
		SetNotes(req.Notes)
	if req.Complete {
		create = create.SetStatus(examinationrecord.StatusCompleted)
	}

	rec, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: create examination record: %w", err)
	}

	if priorExamCount == 0 {
		s.chargeConsultationFee(ctx, tx, tenantID, visit.PatientID, req.VisitID, rec.ID, req.ClinicianID)
	}

	if next, ok := nextVisitStatusAfterExamination(visit.Status, req.Complete); ok {
		if _, err = tx.PatientVisit.UpdateOneID(req.VisitID).SetStatus(next).Save(ctx); err != nil {
			return nil, fmt.Errorf("consultation: advance visit status: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("consultation: commit examination: %w", err)
	}
	return rec, nil
}

// chargeConsultationFee posts the tenant's configured consultation fee for a visit's FIRST
// examination record only (see RecordExamination's priorExamCount==0 guard) — best-effort,
// mirroring patients.Service.chargeRegistrationFee's never-block-the-primary-action semantics: a
// tenant with no configured/active catalog row, or any billing failure, must never block
// recording the examination itself.
func (s *Service) chargeConsultationFee(ctx context.Context, tx *ent.Tx, tenantID, patientID, visitID, examID, clinicianID uuid.UUID) {
	if s.billing == nil {
		return
	}
	item, err := tx.BillableItemCatalog.Query().
		Where(billableitemcatalog.TenantID(tenantID), billableitemcatalog.DepartmentEQ(billableitemcatalog.DepartmentConsultation),
			billableitemcatalog.AppliesToEQ(billableitemcatalog.AppliesToAll), billableitemcatalog.IsActive(true)).
		First(ctx)
	if err != nil {
		return // nothing configured/active for this tenant — nothing to charge, not an error
	}
	if item.Price == nil || *item.Price <= 0 {
		return // priced elsewhere or free — nothing to post
	}
	if _, err := s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
		PatientID: patientID, VisitID: visitID, SourceModule: "consultation", SourceRefID: &examID,
		Description: item.Name, Amount: *item.Price, CreatedByUser: clinicianID, BillableItemID: &item.ID,
	}); err != nil {
		s.log.Warn("consultation fee: post charge failed", zap.Error(err))
	}
}

// appendDiagnosisHistory carries forward the prior examination record's diagnosis_history (or
// starts a fresh one if this is the visit's first record) and appends a new entry when this
// write's diagnosis differs from the prior record's — giving a "previously: X, changed to: Y"
// trail without a full provisional/final field split. An unset/unchanged diagnosis never spams a
// duplicate entry. prior may be nil (no earlier record for this visit yet, or the lookup failed).
func appendDiagnosisHistory(prior *ent.ExaminationRecord, newCode, newName string, changedBy uuid.UUID) []schema.DiagnosisHistoryEntry {
	var history []schema.DiagnosisHistoryEntry
	var priorCode, priorName string
	if prior != nil {
		history = append(history, prior.DiagnosisHistory...)
		priorCode, priorName = prior.DiagnosisCode, prior.DiagnosisName
	}
	if newCode == "" && newName == "" {
		return history // nothing diagnosed on this write — leave the trail exactly as inherited
	}
	if newCode == priorCode && newName == priorName {
		return history // unchanged from the prior record — not a new diagnosis event
	}
	return append(history, schema.DiagnosisHistoryEntry{
		Code: newCode, Name: newName, ChangedBy: changedBy, ChangedAt: time.Now(),
	})
}

// resolveQueueType normalizes the requested queue type to an examinationrecord.QueueType,
// defaulting to "doctor" when the caller didn't specify one. It does NOT validate the value
// against the enum's legal set (doctor/dental/mch/specialist) — an unrecognised non-empty string
// passes through unchanged, exactly as before this extraction, and is rejected later by ent's
// generated QueueTypeValidator at Save time.
func resolveQueueType(reqQueueType string) examinationrecord.QueueType {
	if v := examinationrecord.QueueType(reqQueueType); v != "" {
		return v
	}
	return examinationrecord.QueueTypeDoctor
}

// nextVisitStatusAfterExamination decides whether recording an examination should advance the
// visit's status, and to what, returning ok=false when it shouldn't move at all. Completing an
// examination (complete=true) advances the visit to "completed" unless it's already there.
// Saving an in-progress note (complete=false) only advances a freshly-triaged visit into
// "in_examination" — it leaves a visit that's already further along (or not yet triaged)
// untouched.
func nextVisitStatusAfterExamination(current patientvisit.Status, complete bool) (next patientvisit.Status, ok bool) {
	switch {
	case complete && current != patientvisit.StatusCompleted:
		return patientvisit.StatusCompleted, true
	case !complete && current == patientvisit.StatusTriaged:
		return patientvisit.StatusInExamination, true
	default:
		return "", false
	}
}

// GetExamination fetches an examination record by ID, tenant-scoped.
func (s *Service) GetExamination(ctx context.Context, tenantID, id uuid.UUID) (*ent.ExaminationRecord, error) {
	return s.client.ExaminationRecord.Query().
		Where(examinationrecord.ID(id), examinationrecord.TenantID(tenantID)).
		Only(ctx)
}

// GetLatestExaminationByVisit returns a visit's most recent ExaminationRecord (by examined_at),
// or ent.IsNotFound if the visit has never been examined yet. Lets the examination UI show the
// diagnosis_history trail and any already-recorded structured findings when a case is reopened
// (e.g. after lab results return, see lab.Service.EnterResult's reopen-to-in_progress behaviour)
// instead of only ever seeing history after this session's own save.
func (s *Service) GetLatestExaminationByVisit(ctx context.Context, tenantID, visitID uuid.UUID) (*ent.ExaminationRecord, error) {
	return s.client.ExaminationRecord.Query().
		Where(examinationrecord.TenantID(tenantID), examinationrecord.VisitID(visitID)).
		Order(ent.Desc(examinationrecord.FieldExaminedAt)).
		First(ctx)
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

	if nextStatus, ok := nextVisitStatusAfterReferral(req.ReferredTo); ok {
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

// nextVisitStatusAfterReferral maps a referral's destination to the visit status it should
// advance to. Destinations with no defined status transition ("external_facility", "specialist",
// or anything unrecognised) return ok=false and leave the visit's status untouched.
func nextVisitStatusAfterReferral(referredTo string) (next patientvisit.Status, ok bool) {
	switch referredTo {
	case "lab":
		return patientvisit.StatusAwaitingLab, true
	case "pharmacy":
		return patientvisit.StatusPrescribed, true
	default:
		return "", false
	}
}

// ListReferrals returns every referral raised for a visit.
func (s *Service) ListReferrals(ctx context.Context, tenantID, visitID uuid.UUID) ([]*ent.Referral, error) {
	return s.client.Referral.Query().
		Where(referral.TenantID(tenantID), referral.VisitID(visitID)).
		Order(ent.Desc(referral.FieldCreatedAt)).
		All(ctx)
}

// CancelReferral cancels a referral made in error, before it's been acted on (a LabOrder/
// Prescription created against it — see lab.Service.CreateOrder/pharmacy.Service.
// CreatePrescription's own referral-closing logic). Mirrors CancelOrder/CancelPrescription's
// pattern in their own packages: a pre-terminal-only cancel, never touching an already-actioned
// referral.
func (s *Service) CancelReferral(ctx context.Context, tenantID, referralID uuid.UUID) (*ent.Referral, error) {
	ref, err := s.client.Referral.Query().
		Where(referral.ID(referralID), referral.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultation: referral not found: %w", err)
	}
	if ref.Status != "pending" {
		return nil, fmt.Errorf("consultation: only a pending referral can be cancelled (status=%s)", ref.Status)
	}
	return s.client.Referral.UpdateOneID(referralID).SetStatus("cancelled").Save(ctx)
}
