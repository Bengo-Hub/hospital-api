package patients

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Bengo-Hub/shared-service-client/crm"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/modules/auditlog"
)

// Patients join the tenant's customer list in the CRM only when they agree to it (Kenya Data
// Protection Act). Only the name and phone are ever sent: never the MRN, date of birth, ID
// numbers or anything clinical. A CRM problem never stops a patient from being registered or
// updated; the patient is simply saved without a link.

// AuditActionCRMConsentWithdrawn is the audit log action written when a patient withdraws consent.
const AuditActionCRMConsentWithdrawn = "patient.crm_consent_withdrawn"

// crmUpserter is the part of the CRM client this module uses.
type crmUpserter interface {
	UpsertStrict(ctx context.Context, in crm.UpsertInput) (crm.Contact, error)
}

// CRMLinkResult tells the caller what happened to the CRM link on a save.
type CRMLinkResult struct {
	// Linked is true when the patient now points at a CRM contact.
	Linked bool
	// Notice is a short message for the person at the desk when consent was given but the
	// link could not be made. Empty when there is nothing to say.
	Notice string
}

// SetCRMClient wires the CRM client. Optional: without it consent is still recorded but no
// patient is linked.
func (s *Service) SetCRMClient(c *crm.Client) {
	if c == nil {
		s.crm = nil
		return
	}
	s.crm = c
}

// SetAuditWriter wires the audit log used to record consent withdrawals. Optional.
func (s *Service) SetAuditWriter(w *auditlog.Writer) {
	s.audit = w
}

// linkToCRM finds or creates the tenant's CRM contact for a consenting patient and stores its
// id on the patient. It sends the name and phone only. Every failure leaves the patient saved
// and unlinked.
func (s *Service) linkToCRM(ctx context.Context, tenantID uuid.UUID, p *ent.Patient) (*ent.Patient, CRMLinkResult) {
	if p == nil || p.CrmConsentAt == nil {
		return p, CRMLinkResult{}
	}
	if p.CrmContactID != nil {
		return p, CRMLinkResult{Linked: true}
	}
	// No phone means nothing the CRM can key on, so there is no link and nothing to report.
	if strings.TrimSpace(p.Phone) == "" || s.crm == nil {
		return p, CRMLinkResult{}
	}

	first, last := crm.SplitName(p.FullName)
	contact, err := s.crm.UpsertStrict(ctx, crm.UpsertInput{
		TenantID:  tenantID,
		Phone:     p.Phone,
		FirstName: first,
		LastName:  last,
	})
	if err != nil {
		return p, s.crmFailure(p.ID, err)
	}
	if contact.ID == uuid.Nil {
		s.log.Warn("crm link: contact id missing in reply", zap.String("patient_id", p.ID.String()))
		return p, CRMLinkResult{Notice: crmUnavailableNotice}
	}

	updated, err := s.client.Patient.UpdateOneID(p.ID).SetCrmContactID(contact.ID).Save(ctx)
	if err != nil {
		s.log.Warn("crm link: store contact id failed", zap.String("patient_id", p.ID.String()), zap.Error(err))
		return p, CRMLinkResult{Notice: crmUnavailableNotice}
	}
	return updated, CRMLinkResult{Linked: true}
}

const crmUnavailableNotice = "The patient was saved, but we could not reach the customer list just now. Save the patient again later to add them."

// crmFailure turns a CRM error into a result. A refusal (400, 404, 409, 422) is shown to the
// user with the CRM's reason. Anything else means the CRM is unavailable and is logged without
// personal data.
func (s *Service) crmFailure(patientID uuid.UUID, err error) CRMLinkResult {
	switch {
	case errors.Is(err, crm.ErrDisabled), errors.Is(err, crm.ErrNoIdentifier):
		return CRMLinkResult{}
	case errors.Is(err, crm.ErrNotFound):
		return CRMLinkResult{Notice: refusalNotice("")}
	}
	var ce *crm.Error
	if errors.As(err, &ce) {
		switch ce.Status {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
			return CRMLinkResult{Notice: refusalNotice(ce.Message)}
		}
		s.log.Warn("crm link: crm unavailable",
			zap.String("patient_id", patientID.String()), zap.Int("status", ce.Status), zap.String("code", ce.Code))
		return CRMLinkResult{Notice: crmUnavailableNotice}
	}
	s.log.Warn("crm link: crm call failed", zap.String("patient_id", patientID.String()))
	return CRMLinkResult{Notice: crmUnavailableNotice}
}

func refusalNotice(reason string) string {
	msg := "The patient was saved, but the customer list did not accept their details"
	if r := strings.TrimSpace(reason); r != "" {
		return msg + ": " + r
	}
	return msg + "."
}

// recordConsentWithdrawal writes a withdrawal to the audit log. The CRM contact itself is left in
// place, since it may also hold the same person's purchases elsewhere.
func (s *Service) recordConsentWithdrawal(ctx context.Context, tenantID, actorID uuid.UUID, before *ent.Patient, at time.Time) {
	if s.audit == nil || before == nil {
		return
	}
	prev := map[string]any{}
	if before.CrmConsentAt != nil {
		prev["crm_consent_at"] = before.CrmConsentAt.UTC().Format(time.RFC3339)
	}
	if before.CrmContactID != nil {
		prev["crm_contact_id"] = before.CrmContactID.String()
	}
	s.audit.Record(ctx, auditlog.Entry{
		TenantID:   tenantID,
		ActorID:    actorID,
		Action:     AuditActionCRMConsentWithdrawn,
		TargetType: "patient",
		TargetID:   before.ID,
		Before:     prev,
		After:      map[string]any{"crm_consent_at": nil, "crm_contact_id": nil, "withdrawn_at": at.UTC().Format(time.RFC3339)},
	})
}
