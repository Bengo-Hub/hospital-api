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
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/admission"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/billableitemcatalog"
	"github.com/bengobox/hospital-service/internal/ent/patientaccount"
	"github.com/bengobox/hospital-service/internal/ent/patientnextofkin"
	"github.com/bengobox/hospital-service/internal/ent/walkinsale"
	"github.com/bengobox/hospital-service/internal/modules/sequence"
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

// EnsureAccountForAdmission returns the running ledger for an inpatient admission, creating it
// (status "open", settlement_required_before "discharge") on first use — Admission's own
// counterpart to EnsureAccountForVisit. See docs/architecture.md "Distributed Billing & Patient
// Accounts".
func (s *Service) EnsureAccountForAdmission(ctx context.Context, tx *ent.Tx, tenantID, patientID, admissionID uuid.UUID) (*ent.PatientAccount, error) {
	acct, err := tx.PatientAccount.Query().
		Where(patientaccount.TenantID(tenantID), patientaccount.AdmissionID(admissionID)).
		Only(ctx)
	if err == nil {
		return acct, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("billing: query admission account: %w", err)
	}
	return tx.PatientAccount.Create().
		SetTenantID(tenantID).
		SetPatientID(patientID).
		SetAdmissionID(admissionID).
		SetSettlementRequiredBefore(patientaccount.SettlementRequiredBeforeDischarge).
		Save(ctx)
}

// activeAdmissionAccount resolves the account a visit-level charge should actually post to: if
// the visit has a currently-active (non-discharged) inpatient Admission, every department's
// charges accrue onto that ONE admission account (day-rate, lab, pharmacy, everything) rather than
// each posting a separate per-visit mini-invoice — the whole point of Sprint 6's design. Returns
// (nil, nil) when there's no active admission, so callers fall back to the ordinary per-visit
// account. This is the ONLY place that distinction is made, so triage/consultation/lab/pharmacy
// never need their own admission-awareness — they just call PostCharge with a visit_id as always.
func (s *Service) activeAdmissionAccount(ctx context.Context, tx *ent.Tx, tenantID, visitID uuid.UUID) (*ent.PatientAccount, error) {
	adm, err := tx.Admission.Query().
		Where(admission.TenantID(tenantID), admission.PatientVisitID(visitID), admission.StatusEQ(admission.StatusActive)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("billing: query active admission: %w", err)
	}
	return s.EnsureAccountForAdmission(ctx, tx, tenantID, adm.PatientID, adm.ID)
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
	acct, err := s.activeAdmissionAccount(ctx, tx, tenantID, req.VisitID)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		acct, err = s.EnsureAccountForVisit(ctx, tx, tenantID, req.PatientID, req.VisitID)
		if err != nil {
			return nil, err
		}
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

// GetAccountByAdmission fetches the account for an inpatient admission (Sprint 6) — the
// admission's own counterpart to GetAccountByVisit, used by the admission detail page.
func (s *Service) GetAccountByAdmission(ctx context.Context, tenantID, admissionID uuid.UUID) (*ent.PatientAccount, []*ent.BillableCharge, error) {
	acct, err := s.client.PatientAccount.Query().
		Where(patientaccount.TenantID(tenantID), patientaccount.AdmissionID(admissionID)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("billing: account not found for admission: %w", err)
	}
	return s.GetAccount(ctx, tenantID, acct.ID)
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

	// eTIMS (2026-09-02) — best-effort, opt-in per tenant. SignSaleNow returns (nil, nil) when the
	// tenant isn't eTIMS-activated (treasury-api owns that gate, same as pos-api's own checkout
	// call), so there is no local activation flag to check here. A failure never undoes an already-
	// collected payment — the sale stays collected either way, treasury-api's async worker retries
	// fiscalization independently of this call.
	s.signSaleBestEffort(ctx, tenantID, "hospital_billable_charge", charge.ID, charge.Description, charge.Amount, req)

	return updated, nil
}

// signSaleBestEffort calls treasury.SignSaleNow for a completed collection, logging any failure
// rather than propagating it — fiscalization is opt-in/async by design (docs/integrations.md's
// eTIMS ADR), never a reason to fail a real, already-collected payment.
func (s *Service) signSaleBestEffort(ctx context.Context, tenantID uuid.UUID, orderNumberPrefix string, sourceID uuid.UUID, description string, amount float64, req CollectChargeRequest) {
	if !s.treasury.Enabled() {
		return
	}
	_, err := s.treasury.SignSaleNow(ctx, tenantID, treasury.SignSaleRequest{
		OrderID:     sourceID,
		OrderNumber: orderNumberPrefix + "-" + sourceID.String(),
		TotalAmount: amount,
		Currency:    "KES",
		OutletID:    req.OutletID,
		Items: []treasury.ETIMSSaleItem{{
			ItemType:   "service",
			Name:       description,
			Quantity:   1,
			UnitPrice:  amount,
			TotalPrice: amount,
		}},
		SellingScheme: "cash",
		Tenders:       []treasury.ETIMSSaleTender{{Type: req.PaymentMethod, Amount: amount}},
	})
	if err != nil {
		s.log.Warn("etims sign sale failed (best-effort, payment already collected)",
			zap.String("source_id", sourceID.String()), zap.Error(err))
	}
}

// WaiveCharge writes off a pending/invoiced charge without collecting payment for it — the
// facility chose not to charge (distinct from `exempted`, which means an insurance claim
// covered it). Used directly (a manual write-off) and by CancelOrder-style flows that need to
// void a charge whose originating clinical action was cancelled before it happened.
func (s *Service) WaiveCharge(ctx context.Context, tenantID, chargeID uuid.UUID) (*ent.BillableCharge, error) {
	charge, err := s.client.BillableCharge.Query().
		Where(billablecharge.ID(chargeID), billablecharge.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: charge not found: %w", err)
	}
	if charge.Status != billablecharge.StatusPending && charge.Status != billablecharge.StatusInvoiced {
		return nil, fmt.Errorf("billing: only a pending/invoiced charge can be waived (status=%s)", charge.Status)
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

	updated, err := tx.BillableCharge.UpdateOneID(chargeID).SetStatus(billablecharge.StatusWaived).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: mark charge waived: %w", err)
	}
	// The patient never owes this amount going forward — reduce the outstanding balance, but
	// (unlike CollectCharge) never touch total_paid: no money actually changed hands.
	if _, err = tx.PatientAccount.UpdateOneID(charge.PatientAccountID).
		AddBalance(-charge.Amount).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("billing: update account totals: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("billing: commit waive: %w", err)
	}
	return updated, nil
}

// ── WalkInSale (Chemist-tier ledgerless checkout) ──────────────────────────────────────────
//
// A Chemist tenant cannot create a Patient/Visit (feature-gated off both — see
// subscriptions-api's hospChemistCore()), so a walk-in prescription dispense has neither and
// PostCharge's PatientAccount ledger doesn't apply. These four methods are the ledgerless
// counterpart: CreateWalkInSale/CollectWalkInSale/WaiveWalkInSale/ListWalkInSales mirror
// PostCharge/CollectCharge/WaiveCharge/ListPendingCharges exactly, minus every PatientAccount
// touch. See docs/architecture.md "Distributed Billing & Patient Accounts" — "Chemist -> Billing
// module is just Walk-in Sale, no PatientAccount complexity at all."

// CreateWalkInSaleRequest is the input to CreateWalkInSale.
type CreateWalkInSaleRequest struct {
	OutletID           uuid.UUID
	PrescriptionID     uuid.UUID
	PrescriptionNumber string
	PatientName        string
	Amount             float64
	LineItems          []map[string]any
	CreatedByUser      uuid.UUID
}

// CreateWalkInSale records one till transaction for a nil-patient/nil-visit prescription dispense
// — called from pharmacy.Service.Dispense within its own transaction so the sale and the stock/
// line writes it accompanies commit atomically.
func (s *Service) CreateWalkInSale(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, req CreateWalkInSaleRequest) (*ent.WalkInSale, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("billing: walk-in sale amount must be positive")
	}
	saleNo, err := sequence.Next(ctx, tx, tenantID, "walk_in_sale_number", "WS", 6)
	if err != nil {
		return nil, fmt.Errorf("billing: allocate walk_in_sale_number: %w", err)
	}
	create := tx.WalkInSale.Create().
		SetTenantID(tenantID).
		SetOutletID(req.OutletID).
		SetPrescriptionID(req.PrescriptionID).
		SetPrescriptionNumber(req.PrescriptionNumber).
		SetSaleNumber(saleNo).
		SetPatientName(req.PatientName).
		SetLineItems(req.LineItems).
		SetAmount(req.Amount)
	if req.CreatedByUser != uuid.Nil {
		create = create.SetCreatedByUserID(req.CreatedByUser)
	}
	sale, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: create walk-in sale: %w", err)
	}
	return sale, nil
}

// ListWalkInSales is a chemist's "Today's Sales" / collect-queue source, optionally filtered by
// status (pending|paid|waived).
func (s *Service) ListWalkInSales(ctx context.Context, tenantID uuid.UUID, status string) ([]*ent.WalkInSale, error) {
	q := s.client.WalkInSale.Query().Where(walkinsale.TenantID(tenantID))
	if status != "" {
		q = q.Where(walkinsale.StatusEQ(walkinsale.Status(status)))
	}
	return q.Order(ent.Desc(walkinsale.FieldCreatedAt)).Limit(200).All(ctx)
}

// CollectWalkInSaleRequest is the input to CollectWalkInSale.
type CollectWalkInSaleRequest struct {
	PaymentMethod string
	PhoneNumber   string
	CollectedBy   uuid.UUID
	OutletID      *uuid.UUID
}

// CollectWalkInSale is CollectCharge's ledgerless counterpart: creates a treasury invoice +
// payment intent for one pending walk-in sale and marks it paid. No PatientAccount is touched —
// there isn't one for a walk-in sale, so (unlike CollectCharge) a single update needs no
// transaction wrapper. eTIMS signing (SignSaleNow) is wired in separately — see
// docs/sprints/sprint-5-billing-insurance.md's eTIMS DoD item — not in this initial cut.
func (s *Service) CollectWalkInSale(ctx context.Context, tenantID, saleID uuid.UUID, req CollectWalkInSaleRequest) (*ent.WalkInSale, error) {
	sale, err := s.client.WalkInSale.Query().
		Where(walkinsale.ID(saleID), walkinsale.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: walk-in sale not found: %w", err)
	}
	if sale.Status != walkinsale.StatusPending {
		return nil, fmt.Errorf("billing: walk-in sale is not pending (status=%s)", sale.Status)
	}
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}

	inv, err := s.treasury.CreateInvoice(ctx, tenantID, treasury.CreateInvoiceRequest{
		CustomerName:  sale.PatientName,
		ReferenceID:   &sale.ID,
		ReferenceType: "hospital_walk_in_sale",
		OutletID:      req.OutletID,
		Lines: []treasury.InvoiceLine{{
			Description: "Walk-in sale " + sale.SaleNumber,
			Quantity:    1,
			UnitPrice:   sale.Amount,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("billing: create invoice: %w", err)
	}

	intent, err := s.treasury.CreateIntent(ctx, tenantID, treasury.CreatePaymentIntentRequest{
		ReferenceID:   sale.ID,
		ReferenceType: "hospital_walk_in_sale",
		PaymentMethod: req.PaymentMethod,
		Amount:        sale.Amount,
		Currency:      "KES",
		PhoneNumber:   req.PhoneNumber,
		OutletID:      req.OutletID,
		SourceService: "hospital-api",
	})
	if err != nil {
		return nil, fmt.Errorf("billing: create payment intent: %w", err)
	}

	upd := s.client.WalkInSale.UpdateOneID(saleID).
		SetStatus(walkinsale.StatusPaid).
		SetPaymentMethod(req.PaymentMethod).
		SetTreasuryInvoiceID(inv.ID).
		SetTreasuryPaymentIntentID(intent.ID).
		SetPaidAt(time.Now())
	if req.CollectedBy != uuid.Nil {
		upd = upd.SetCollectedBy(req.CollectedBy)
	}

	// eTIMS (2026-09-02) — best-effort, opt-in per tenant (treasury-api owns the activation gate,
	// SignSaleNow no-ops for a non-activated tenant). This is the real fiscalization the
	// hospital_sale Source value was added for — a chemist's walk-in sale is exactly the "hospital
	// sale" case treasury-api's eTIMS attribution distinguishes from a POS sale.
	if s.treasury.Enabled() {
		items := make([]treasury.ETIMSSaleItem, 0, len(sale.LineItems))
		for _, li := range sale.LineItems {
			name, _ := li["drug_name"].(string)
			sku, _ := li["sku"].(string)
			qty, _ := li["quantity"].(float64)
			unitPrice, _ := li["unit_price"].(float64)
			lineTotal, _ := li["line_total"].(float64)
			items = append(items, treasury.ETIMSSaleItem{
				SKU: sku, ItemType: "drug", Name: name, Quantity: qty, UnitPrice: unitPrice, TotalPrice: lineTotal,
			})
		}
		if len(items) == 0 {
			items = append(items, treasury.ETIMSSaleItem{ItemType: "service", Name: "Walk-in sale " + sale.SaleNumber, Quantity: 1, UnitPrice: sale.Amount, TotalPrice: sale.Amount})
		}
		fiscal, ferr := s.treasury.SignSaleNow(ctx, tenantID, treasury.SignSaleRequest{
			OrderID: sale.ID, OrderNumber: sale.SaleNumber, TotalAmount: sale.Amount, Currency: "KES",
			OutletID: req.OutletID, Items: items, SellingScheme: "cash",
			Tenders: []treasury.ETIMSSaleTender{{Type: req.PaymentMethod, Amount: sale.Amount}},
		})
		if ferr != nil {
			s.log.Warn("etims sign sale failed (best-effort, walk-in sale already collected)",
				zap.String("sale_id", saleID.String()), zap.Error(ferr))
		} else if fiscal != nil && fiscal.Signed {
			upd = upd.SetEtimsInvoiceNumber(fiscal.InvoiceNumber).SetEtimsQrCodeURL(fiscal.QRCodeURL)
		}
	}

	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: mark walk-in sale paid: %w", err)
	}
	return updated, nil
}

// WaiveWalkInSale writes off a pending walk-in sale without collecting payment for it — a
// customer who walks away, mirrors WaiveCharge.
func (s *Service) WaiveWalkInSale(ctx context.Context, tenantID, saleID uuid.UUID) (*ent.WalkInSale, error) {
	sale, err := s.client.WalkInSale.Query().
		Where(walkinsale.ID(saleID), walkinsale.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: walk-in sale not found: %w", err)
	}
	if sale.Status != walkinsale.StatusPending {
		return nil, fmt.Errorf("billing: only a pending walk-in sale can be waived (status=%s)", sale.Status)
	}
	return s.client.WalkInSale.UpdateOneID(saleID).SetStatus(walkinsale.StatusWaived).Save(ctx)
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

// ── PatientNextOfKin (2026-08-30) ───────────────────────────────────────────────────────────
//
// Was a dead feature end to end before this: SettleAccount already consumed a next_of_kin_id,
// but nothing anywhere ever created one, forcing the Settle Account UI to ask a cashier to type
// a raw UUID nobody could ever have. These two methods close that: list a patient's recorded
// next-of-kin (so the UI can offer a picker before falling back to "add new"), and create one.

// ListNextOfKin returns every next-of-kin recorded for a patient, primary first.
func (s *Service) ListNextOfKin(ctx context.Context, tenantID, patientID uuid.UUID) ([]*ent.PatientNextOfKin, error) {
	return s.client.PatientNextOfKin.Query().
		Where(patientnextofkin.TenantID(tenantID), patientnextofkin.PatientID(patientID)).
		Order(ent.Desc(patientnextofkin.FieldIsPrimary), ent.Asc(patientnextofkin.FieldCreatedAt)).
		All(ctx)
}

// NextOfKinInput is the input to CreateNextOfKin.
type NextOfKinInput struct {
	Name         string
	Phone        string
	Relationship string
	IDNumber     string
	IsPrimary    bool
}

// CreateNextOfKin records a new next-of-kin for a patient. If IsPrimary is set, any existing
// primary record for this patient is demoted first (at most one primary per patient).
func (s *Service) CreateNextOfKin(ctx context.Context, tenantID, patientID uuid.UUID, in NextOfKinInput) (*ent.PatientNextOfKin, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("billing: next-of-kin name is required")
	}
	if in.IsPrimary {
		if _, err := s.client.PatientNextOfKin.Update().
			Where(patientnextofkin.TenantID(tenantID), patientnextofkin.PatientID(patientID), patientnextofkin.IsPrimary(true)).
			SetIsPrimary(false).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("billing: demote existing primary next-of-kin: %w", err)
		}
	}
	return s.client.PatientNextOfKin.Create().
		SetTenantID(tenantID).
		SetPatientID(patientID).
		SetName(in.Name).
		SetPhone(in.Phone).
		SetRelationship(in.Relationship).
		SetIDNumber(in.IDNumber).
		SetIsPrimary(in.IsPrimary).
		Save(ctx)
}

// ── BillableItemCatalog admin CRUD (Gap 3 — a tenant admin isn't stuck editing rows via direct ──
// DB access; the starter set itself comes from refdata.SeedFacilityBillableItems) ─────────────

// ListBillableItemCatalog lists a tenant's catalog, active-only by default.
func (s *Service) ListBillableItemCatalog(ctx context.Context, tenantID uuid.UUID, includeInactive bool) ([]*ent.BillableItemCatalog, error) {
	q := s.client.BillableItemCatalog.Query().Where(billableitemcatalog.TenantID(tenantID))
	if !includeInactive {
		q = q.Where(billableitemcatalog.IsActive(true))
	}
	return q.Order(ent.Asc(billableitemcatalog.FieldDepartment), ent.Asc(billableitemcatalog.FieldCode)).All(ctx)
}

// GetCatalogItemByCode fetches one active tenant-configured catalog row by department+code —
// used by other modules (e.g. inpatient's ward/day-rate lookup, see refdata.SeedFacilityBillableItems'
// WARD_DAY_RATE seed) that need a specific line's current price without listing the whole catalog.
func (s *Service) GetCatalogItemByCode(ctx context.Context, tenantID uuid.UUID, department, code string) (*ent.BillableItemCatalog, error) {
	return s.client.BillableItemCatalog.Query().
		Where(
			billableitemcatalog.TenantID(tenantID),
			billableitemcatalog.DepartmentEQ(billableitemcatalog.Department(department)),
			billableitemcatalog.Code(code),
			billableitemcatalog.IsActive(true),
		).
		Only(ctx)
}

// CatalogItemInput is the input to CreateBillableItem.
type CatalogItemInput struct {
	Department         string
	Code               string
	Name               string
	Price              *float64 // nil = priced elsewhere (drugs/lab tests — see the schema's own doc comment)
	AppliesTo          string   // first_visit|return_visit|all — defaults to "all" when empty
	RequiresPrepayment bool
	CollectionMode     string // direct|billing_queue|either — defaults to "billing_queue" when empty
}

// CreateBillableItem adds one tenant-configured catalog row (the unique(tenant_id, code) index
// enforces no duplicate code per tenant).
func (s *Service) CreateBillableItem(ctx context.Context, tenantID uuid.UUID, in CatalogItemInput) (*ent.BillableItemCatalog, error) {
	if in.Code == "" || in.Name == "" {
		return nil, fmt.Errorf("billing: code and name are required")
	}
	if in.Department == "" {
		return nil, fmt.Errorf("billing: department is required")
	}
	create := s.client.BillableItemCatalog.Create().
		SetTenantID(tenantID).
		SetDepartment(billableitemcatalog.Department(in.Department)).
		SetCode(in.Code).
		SetName(in.Name).
		SetRequiresPrepayment(in.RequiresPrepayment).
		SetIsActive(true)
	if in.Price != nil {
		create = create.SetPrice(*in.Price)
	}
	if in.AppliesTo != "" {
		create = create.SetAppliesTo(billableitemcatalog.AppliesTo(in.AppliesTo))
	}
	if in.CollectionMode != "" {
		create = create.SetCollectionMode(billableitemcatalog.CollectionMode(in.CollectionMode))
	}
	item, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: create catalog item: %w", err)
	}
	return item, nil
}

// CatalogItemUpdate is the input to UpdateBillableItem — every field is a pointer so only fields
// the caller actually sent are changed. ClearPrice is a separate explicit flag (rather than
// overloading Price==nil) because "don't change the price" and "set the price back to nil/priced-
// elsewhere" are both real, distinct requests.
type CatalogItemUpdate struct {
	Name               *string
	Price              *float64
	ClearPrice         bool
	AppliesTo          *string
	RequiresPrepayment *bool
	CollectionMode     *string
	IsActive           *bool
}

// UpdateBillableItem applies a partial update to one tenant-scoped catalog row.
func (s *Service) UpdateBillableItem(ctx context.Context, tenantID, itemID uuid.UUID, in CatalogItemUpdate) (*ent.BillableItemCatalog, error) {
	existing, err := s.client.BillableItemCatalog.Query().
		Where(billableitemcatalog.ID(itemID), billableitemcatalog.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: catalog item not found: %w", err)
	}
	upd := s.client.BillableItemCatalog.UpdateOneID(existing.ID)
	if in.Name != nil {
		upd = upd.SetName(*in.Name)
	}
	if in.ClearPrice {
		upd = upd.ClearPrice()
	} else if in.Price != nil {
		upd = upd.SetPrice(*in.Price)
	}
	if in.AppliesTo != nil {
		upd = upd.SetAppliesTo(billableitemcatalog.AppliesTo(*in.AppliesTo))
	}
	if in.RequiresPrepayment != nil {
		upd = upd.SetRequiresPrepayment(*in.RequiresPrepayment)
	}
	if in.CollectionMode != nil {
		upd = upd.SetCollectionMode(billableitemcatalog.CollectionMode(*in.CollectionMode))
	}
	if in.IsActive != nil {
		upd = upd.SetIsActive(*in.IsActive)
	}
	item, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: update catalog item: %w", err)
	}
	return item, nil
}

// DeactivateBillableItem soft-deletes a catalog row (is_active=false) — never a hard delete, so
// existing BillableCharge rows that reference it (billable_item_id) keep resolving.
func (s *Service) DeactivateBillableItem(ctx context.Context, tenantID, itemID uuid.UUID) (*ent.BillableItemCatalog, error) {
	existing, err := s.client.BillableItemCatalog.Query().
		Where(billableitemcatalog.ID(itemID), billableitemcatalog.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: catalog item not found: %w", err)
	}
	return s.client.BillableItemCatalog.UpdateOneID(existing.ID).SetIsActive(false).Save(ctx)
}

// ── Insurance (Sprint 5 remainder — wires the treasury.Client eligibility/claim methods that ──
// existed since Phase 0 but were never called from a real clinical flow) ──────────────────────

// CheckEligibility checks a patient's insurance eligibility against treasury-api's connector for
// the given provider. A transport/config failure is returned as an error — callers (lab/pharmacy
// insurance-claim actions) must treat that as "unknown, offer cash instead," never as
// "ineligible," per docs/integrations.md §2.4.
func (s *Service) CheckEligibility(ctx context.Context, tenantID, providerID uuid.UUID, fields map[string]string) (treasury.EligibilityResult, error) {
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}
	return s.treasury.CheckEligibility(ctx, tenantID, providerID, fields)
}

// ListInsuranceProviders lists a tenant's configured insurance providers — the picker source
// for the eligibility-check/claim-submission UI (Lab, Pharmacy, and Billing's own visit-level
// insurance actions all need this same list).
func (s *Service) ListInsuranceProviders(ctx context.Context, tenantID uuid.UUID) ([]treasury.Provider, error) {
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}
	return s.treasury.ListProviders(ctx, tenantID)
}

// PollInsuranceClaim polls a previously submitted claim's async adjudication status — a thin
// proxy to treasury-api (see docs/sprints/sprint-5-billing-insurance.md's
// GET .../insurance/claims/{claimID}/status). Does not touch any BillableCharge: hospital-api
// does not persist a charge->claim linkage (see SubmitInsuranceClaim's doc comment), so finalizing
// a charge once a pending claim is later approved means calling SubmitInsuranceClaim again —
// treasury-api's claim creation is idempotent on its own reference (order_id/prescription_id), so
// a resubmission for the same order/prescription is safe and simply reports the now-approved
// status instead of creating a duplicate claim.
func (s *Service) PollInsuranceClaim(ctx context.Context, tenantID, claimID uuid.UUID) (*treasury.Claim, error) {
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}
	return s.treasury.PollClaimStatus(ctx, tenantID, claimID)
}

// claimAccepted reports whether a treasury-api Claim.Status represents a terminal, fully-covered
// outcome. treasury-api's real InsuranceClaim.status enum (internal/ent/schema/
// insuranceclaim.go): draft | eligibility_checked | preauthorized | submitted | adjudicated |
// paid | rejected | reversed. Only "paid" is a safe terminal-accepted signal — "adjudicated" is
// a mid-pipeline status (a payer's adjudication can still resolve to paid OR rejected, per the
// schema's own "queued/approved/rejected/in-review/payment-completed/payment-declined" comment),
// so treating it as accepted here would prematurely mark a charge exempted before the payer has
// actually agreed to pay. Any other status ("submitted"/"preauthorized"/"rejected"/"reversed"/
// unknown) is treated as NOT yet accepted: the charge stays pending and the caller polls/
// resubmits later.
func claimAccepted(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "paid")
}

// SubmitInsuranceClaimRequest is the input to SubmitInsuranceClaim.
type SubmitInsuranceClaimRequest struct {
	ProviderID uuid.UUID
	CoverageID *uuid.UUID
	OutletID   *uuid.UUID
	// OrderID/PrescriptionID are passed through to treasury.SubmitClaimRequest verbatim (the
	// S2S-relevant subset of treasury-api's insurance.SaleClaimInput) so treasury-api's claim
	// record links back to whichever hospital-api order/prescription generated it. Optional —
	// leave nil for a claim not tied to one specific order/prescription (e.g. a mixed-charges
	// visit-level claim).
	OrderID        *uuid.UUID
	PrescriptionID *uuid.UUID
	// ChargeIDs selects exactly which of the account's PENDING charges this claim should cover
	// (the KenyaEMR-validated "claim references already-posted charges, never re-derives them"
	// shape — see docs/architecture.md). Empty = every pending charge on the account, mirroring
	// SettleAccount's "settle everything outstanding" behavior for the cash path.
	ChargeIDs []uuid.UUID
}

// InsuranceClaimResult is the outcome of SubmitInsuranceClaim.
type InsuranceClaimResult struct {
	Claim    *treasury.Claim       `json:"claim"`
	Accepted bool                  `json:"accepted"`
	Charges  []*ent.BillableCharge `json:"charges"` // exempted if Accepted, otherwise still pending
}

// SubmitInsuranceClaim is the insurance-settlement counterpart to CollectCharge (cash): instead
// of creating a treasury invoice+payment intent, it submits ONE treasury-api insurance claim
// covering the selected pending charges' total amount. treasury-api's SubmitClaim call is
// best-effort against the external payer but always records the claim locally treasury-side (see
// treasury.Client.SubmitClaim's doc comment) — so a claim that comes back non-terminal (SHA's
// async mediator_id pattern) is not an error here, it just leaves the charge(s) pending for a
// later PollInsuranceClaim + resubmit.
//
// Design note (claim-id tracking): BillableCharge has no dedicated "claim id" column (adding one
// was out of this session's scope — only the `exempted` status was requested). Once a claim IS
// accepted, this reuses the existing nullable treasury_payment_intent_id column to record the
// treasury Claim.ID — same purpose as its cash-path use ("which treasury record settled this
// charge"), just a claim id instead of a payment-intent id for the insurance path. This avoids a
// second migration while still leaving an audit trail on the charge.
func (s *Service) SubmitInsuranceClaim(ctx context.Context, tenantID, accountID uuid.UUID, req SubmitInsuranceClaimRequest) (*InsuranceClaimResult, error) {
	if !s.treasury.Enabled() {
		return nil, fmt.Errorf("billing: treasury client not configured")
	}
	if req.ProviderID == uuid.Nil {
		return nil, fmt.Errorf("billing: provider_id is required")
	}
	acct, allCharges, err := s.GetAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}

	var wanted map[uuid.UUID]bool
	if len(req.ChargeIDs) > 0 {
		wanted = make(map[uuid.UUID]bool, len(req.ChargeIDs))
		for _, id := range req.ChargeIDs {
			wanted[id] = true
		}
	}
	target := make([]*ent.BillableCharge, 0, len(allCharges))
	var amount float64
	for _, c := range allCharges {
		if c.Status != billablecharge.StatusPending {
			continue
		}
		if wanted != nil && !wanted[c.ID] {
			continue
		}
		target = append(target, c)
		amount += c.Amount
	}
	if len(target) == 0 {
		return nil, fmt.Errorf("billing: no pending charges to claim")
	}

	claim, err := s.treasury.SubmitClaim(ctx, tenantID, treasury.SubmitClaimRequest{
		ProviderID:     req.ProviderID,
		CoverageID:     req.CoverageID,
		OutletID:       req.OutletID,
		OrderID:        req.OrderID,
		PrescriptionID: req.PrescriptionID,
		Amount:         amount,
	})
	if err != nil {
		return nil, fmt.Errorf("billing: submit insurance claim: %w", err)
	}

	result := &InsuranceClaimResult{Claim: claim, Charges: target}
	if !claimAccepted(claim.Status) {
		return result, nil
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
	updated := make([]*ent.BillableCharge, 0, len(target))
	var totalExempted float64
	for _, c := range target {
		u, uerr := tx.BillableCharge.UpdateOneID(c.ID).
			SetStatus(billablecharge.StatusExempted).
			SetTreasuryPaymentIntentID(claim.ID).
			SetPaidAt(now).
			Save(ctx)
		if uerr != nil {
			err = uerr
			return nil, fmt.Errorf("billing: mark charge exempted: %w", uerr)
		}
		updated = append(updated, u)
		totalExempted += c.Amount
	}

	// Deliberately AddBalance only, NOT AddTotalPaid: total_paid tracks cash actually collected
	// from the patient (see CollectCharge's AddTotalPaid+AddBalance pair above) — an exempted
	// charge was covered by insurance, not paid by the patient, so it must reduce what's owed
	// without inflating a "cash collected" figure a cashier/report might read literally.
	updatedAcct, err := tx.PatientAccount.UpdateOneID(acct.ID).
		AddBalance(-totalExempted).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: update account totals: %w", err)
	}
	if updatedAcct.Balance <= 0 && updatedAcct.Status == patientaccount.StatusOpen {
		if _, err = tx.PatientAccount.UpdateOneID(acct.ID).SetStatus(patientaccount.StatusSettled).Save(ctx); err != nil {
			return nil, fmt.Errorf("billing: mark account settled: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("billing: commit insurance claim: %w", err)
	}
	result.Accepted = true
	result.Charges = updated
	return result, nil
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
