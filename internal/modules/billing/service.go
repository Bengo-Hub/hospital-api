// Package billing implements the Distributed Billing & Patient Accounts ledger (Sprint 5 core):
// any department can post a charge; a department holding collect_own may collect what it
// charged directly, the Billing desk (collect_any) is the universal fallback. hospital-api owns
// this ledger only — the actual money (invoices/payments) is always created via treasury-api's
// S2S API (internal/modules/treasury), never stored locally. See
// docs/architecture.md "Distributed Billing & Patient Accounts".
package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/patientaccount"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

// Service implements billing ledger business logic.
type Service struct {
	client   *ent.Client
	treasury *treasury.Client
	log      *zap.Logger
}

// NewService creates a new billing service.
func NewService(client *ent.Client, treasuryClient *treasury.Client, log *zap.Logger) *Service {
	return &Service{client: client, treasury: treasuryClient, log: log.Named("billing.service")}
}

// EnsureAccountForVisit returns the patient's account for an OPD visit, creating it (status
// "open") on first use. Called by other modules when they post their first charge for a visit.
func (s *Service) EnsureAccountForVisit(ctx context.Context, tx *ent.Tx, tenantID, patientID, visitID uuid.UUID) (*ent.PatientAccount, error) {
	acct, err := tx.PatientAccount.Query().
		Where(patientaccount.TenantID(tenantID), patientaccount.VisitID(visitID)).
		Only(ctx)
	if err == nil {
		return acct, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("billing: query account: %w", err)
	}
	return tx.PatientAccount.Create().
		SetTenantID(tenantID).
		SetPatientID(patientID).
		SetVisitID(visitID).
		Save(ctx)
}

// PostChargeRequest is the input to PostCharge — the primitive every other module (records,
// triage, consultation, lab, pharmacy) calls at its own billable step, instead of building its
// own payment logic.
type PostChargeRequest struct {
	PatientID      uuid.UUID
	VisitID        uuid.UUID
	SourceModule   string // records|triage|consultation|lab|pharmacy|theatre|inpatient
	SourceRefID    *uuid.UUID
	Description    string
	Amount         float64
	CreatedByUser  uuid.UUID
	BillableItemID *uuid.UUID
}

// PostCharge posts one charge against the patient's account (creating the account on first
// use), within the caller's own transaction so the charge and its triggering domain write
// commit atomically.
func (s *Service) PostCharge(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, req PostChargeRequest) (*ent.BillableCharge, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("billing: amount must be positive")
	}
	acct, err := s.EnsureAccountForVisit(ctx, tx, tenantID, req.PatientID, req.VisitID)
	if err != nil {
		return nil, err
	}

	create := tx.BillableCharge.Create().
		SetTenantID(tenantID).
		SetPatientAccountID(acct.ID).
		SetSourceModule(req.SourceModule).
		SetDescription(req.Description).
		SetAmount(req.Amount)
	if req.SourceRefID != nil {
		create = create.SetSourceReferenceID(*req.SourceRefID)
	}
	if req.BillableItemID != nil {
		create = create.SetBillableItemID(*req.BillableItemID)
	}
	if req.CreatedByUser != uuid.Nil {
		create = create.SetCreatedByUserID(req.CreatedByUser)
	}
	charge, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: create charge: %w", err)
	}

	if _, err := tx.PatientAccount.UpdateOneID(acct.ID).
		AddTotalCharged(req.Amount).
		AddBalance(req.Amount).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("billing: update account totals: %w", err)
	}
	return charge, nil
}

// PeekCharge fetches a single charge by ID, tenant-scoped — used by the HTTP layer to resolve
// a charge's source_module before deciding whether the caller may collect it.
func (s *Service) PeekCharge(ctx context.Context, tenantID, chargeID uuid.UUID) (*ent.BillableCharge, error) {
	return s.client.BillableCharge.Query().
		Where(billablecharge.ID(chargeID), billablecharge.TenantID(tenantID)).
		Only(ctx)
}

// GetAccount fetches a patient account with its charges, tenant-scoped.
func (s *Service) GetAccount(ctx context.Context, tenantID, accountID uuid.UUID) (*ent.PatientAccount, []*ent.BillableCharge, error) {
	acct, err := s.client.PatientAccount.Query().
		Where(patientaccount.ID(accountID), patientaccount.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("billing: account not found: %w", err)
	}
	charges, err := s.client.BillableCharge.Query().
		Where(billablecharge.TenantID(tenantID), billablecharge.PatientAccountID(accountID)).
		Order(ent.Desc(billablecharge.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("billing: list charges: %w", err)
	}
	return acct, charges, nil
}

// GetAccountByVisit fetches the account for a visit (the common lookup for OPD callers).
func (s *Service) GetAccountByVisit(ctx context.Context, tenantID, visitID uuid.UUID) (*ent.PatientAccount, []*ent.BillableCharge, error) {
	acct, err := s.client.PatientAccount.Query().
		Where(patientaccount.TenantID(tenantID), patientaccount.VisitID(visitID)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("billing: account not found for visit: %w", err)
	}
	return s.GetAccount(ctx, tenantID, acct.ID)
}

// ListPendingCharges is the Billing desk's queue — every unpaid charge across every
// department/patient, optionally scoped to one department.
func (s *Service) ListPendingCharges(ctx context.Context, tenantID uuid.UUID, department string) ([]*ent.BillableCharge, error) {
	q := s.client.BillableCharge.Query().
		Where(billablecharge.TenantID(tenantID), billablecharge.StatusEQ(billablecharge.StatusPending))
	if department != "" {
		q = q.Where(billablecharge.SourceModule(department))
	}
	return q.Order(ent.Asc(billablecharge.FieldCreatedAt)).Limit(200).All(ctx)
}

// CollectChargeRequest is the input to CollectCharge.
type CollectChargeRequest struct {
	PaymentMethod string // mpesa|card|cash|bank_transfer|paystack
	PhoneNumber   string
	CollectedBy   uuid.UUID
	OutletID      *uuid.UUID
}

// CollectCharge creates a treasury invoice + payment intent for one pending charge and marks it
// paid. Permission (collect_own vs collect_any) is enforced by the caller (HTTP handler) before
// this is invoked — this method assumes the caller is already authorized to collect this charge.
func (s *Service) CollectCharge(ctx context.Context, tenantID, chargeID uuid.UUID, req CollectChargeRequest) (*ent.BillableCharge, error) {
	charge, err := s.client.BillableCharge.Query().
		Where(billablecharge.ID(chargeID), billablecharge.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: charge not found: %w", err)
	}
	if charge.Status != billablecharge.StatusPending {
		return nil, fmt.Errorf("billing: charge is not pending (status=%s)", charge.Status)
	}
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}

	inv, err := s.treasury.CreateInvoice(ctx, tenantID, treasury.CreateInvoiceRequest{
		ReferenceID:   &charge.ID,
		ReferenceType: "hospital_billable_charge",
		OutletID:      req.OutletID,
		Lines: []treasury.InvoiceLine{{
			Description: charge.Description,
			Quantity:    1,
			UnitPrice:   charge.Amount,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("billing: create invoice: %w", err)
	}

	intent, err := s.treasury.CreateIntent(ctx, tenantID, treasury.CreatePaymentIntentRequest{
		ReferenceID:   charge.ID,
		ReferenceType: "hospital_billable_charge",
		PaymentMethod: req.PaymentMethod,
		Amount:        charge.Amount,
		Currency:      "KES",
		PhoneNumber:   req.PhoneNumber,
		OutletID:      req.OutletID,
		SourceService: "hospital-api",
	})
	if err != nil {
		return nil, fmt.Errorf("billing: create payment intent: %w", err)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()
	updated, err := tx.BillableCharge.UpdateOneID(chargeID).
		SetStatus(billablecharge.StatusPaid).
		SetTreasuryInvoiceID(inv.ID).
		SetTreasuryPaymentIntentID(intent.ID).
		SetPaidAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: mark charge paid: %w", err)
	}

	if _, err = tx.PatientAccount.UpdateOneID(charge.PatientAccountID).
		AddTotalPaid(charge.Amount).
		AddBalance(-charge.Amount).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("billing: update account totals: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("billing: commit collection: %w", err)
	}
	return updated, nil
}

// SettleAccount pays off every remaining pending charge on an account in one call (the
// discharge/body-release flow), optionally recording who settled it on the patient's behalf.
func (s *Service) SettleAccount(ctx context.Context, tenantID, accountID uuid.UUID, req CollectChargeRequest, nextOfKinID *uuid.UUID) (*ent.PatientAccount, error) {
	acct, charges, err := s.GetAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	for _, c := range charges {
		if c.Status != billablecharge.StatusPending {
			continue
		}
		if _, err := s.CollectCharge(ctx, tenantID, c.ID, req); err != nil {
			return nil, fmt.Errorf("billing: settle charge %s: %w", c.ID, err)
		}
	}
	upd := s.client.PatientAccount.UpdateOneID(acct.ID).SetStatus(patientaccount.StatusSettled)
	if nextOfKinID != nil {
		upd = upd.SetNextOfKinID(*nextOfKinID)
	}
	return upd.Save(ctx)
}

// OverrideSettlement releases a patient/body with an outstanding balance — an audited escape
// hatch, never a silent bypass. Callers must enforce PermBillingOverrideSettlement + a non-empty
// reason before invoking this.
func (s *Service) OverrideSettlement(ctx context.Context, tenantID, accountID uuid.UUID, overriddenBy uuid.UUID, reason string) (*ent.PatientAccount, error) {
	if reason == "" {
		return nil, fmt.Errorf("billing: a reason is required to override settlement")
	}
	acct, err := s.client.PatientAccount.Query().
		Where(patientaccount.ID(accountID), patientaccount.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: account not found: %w", err)
	}
	s.log.Warn("settlement override",
		zap.String("account_id", accountID.String()),
		zap.String("tenant_id", tenantID.String()),
		zap.String("overridden_by", overriddenBy.String()),
		zap.Float64("outstanding_balance", acct.Balance),
		zap.String("reason", reason),
	)
	return s.client.PatientAccount.UpdateOneID(accountID).
		SetStatus(patientaccount.StatusWrittenOff).
		Save(ctx)
}
